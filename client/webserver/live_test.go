//go:build live && !nolgpl

// Run a test server with
// go test -v -tags live -run Server -timeout 60m
// test server will run for 1 hour and serve randomness.

package webserver

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	mrand "math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/asset/polygon"
	"decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/mnemonic"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	firstDEX           = "somedex.com"
	secondDEX          = "thisdexwithalongname.com"
	unsupportedAssetID = 141 // kmd
)

var (
	tCtx                  context.Context
	maxDelay                     = time.Second * 4
	epochDuration                = time.Second * 30 // milliseconds
	feedPeriod                   = time.Second * 10
	creationPendingAsset  uint32 = 0xFFFFFFFF
	forceDisconnectWallet bool
	wipeWalletBalance     bool
	gapWidthFactor        = 1.0 // Should be 0 < gapWidthFactor <= 1.0
	randomPokes           = false
	randomNotes           = false
	numUserOrders         = 10
	conversionFactor      = dexbtc.UnitInfo.Conventional.ConversionFactor
	delayBalance          = false
	doubleCreateAsyncErr  = false
	randomizeOrdersCount  = false
	initErrors            = false
	mmConnectErrors       = false
	enableActions         = false
	actions               []*asset.ActionRequiredNote

	rand   = mrand.New(mrand.NewSource(time.Now().UnixNano()))
	titler = cases.Title(language.AmericanEnglish)
)

func dummySettings() map[string]string {
	return map[string]string{
		"rpcuser":     "dexuser",
		"rpcpassword": "dexpass",
		"rpcbind":     "127.0.0.1:54321",
		"rpcport":     "",
		"fallbackfee": "20",
		"txsplit":     "0",
		"username":    "dexuser",
		"password":    "dexpass",
		"rpclisten":   "127.0.0.1:54321",
		"rpccert":     "/home/me/dex/rpc.cert",
	}
}

func randomDelay() {
	time.Sleep(time.Duration(rand.Float64() * float64(maxDelay)))
}

// A random number with a random order of magnitude.
func randomMagnitude(low, high int) float64 {
	exponent := rand.Intn(high-low) + low
	mantissa := rand.Float64() * 10
	return mantissa * math.Pow10(exponent)
}

func userOrders(mktID string) (ords []*core.Order) {
	orderCount := rand.Intn(numUserOrders)
	for i := 0; i < orderCount; i++ {
		midGap, maxQty := getMarketStats(mktID)
		sell := rand.Intn(2) > 0
		ord := randomOrder(sell, maxQty, midGap, gapWidthFactor*midGap, false)
		qty := uint64(ord.Qty * 1e8)
		filled := uint64(rand.Float64() * float64(qty))
		orderType := order.OrderType(rand.Intn(2) + 1)
		status := order.OrderStatusEpoch
		epoch := uint64(time.Now().UnixMilli()) / uint64(epochDuration.Milliseconds())
		isLimit := orderType == order.LimitOrderType
		if rand.Float32() > 0.5 {
			epoch -= 1
			if isLimit {
				status = order.OrderStatusBooked
			} else {
				status = order.OrderStatusExecuted
			}
		}
		var tif order.TimeInForce
		var rate uint64
		if isLimit {
			rate = uint64(ord.Rate * 1e8)
			if rand.Float32() < 0.25 {
				tif = order.ImmediateTiF
			} else {
				tif = order.StandingTiF
			}
		}
		ords = append(ords, &core.Order{
			ID:     ordertest.RandomOrderID().Bytes(),
			Type:   orderType,
			Stamp:  uint64(time.Now().UnixMilli()) - uint64(rand.Float64()*600_000),
			Status: status,
			Epoch:  epoch,
			Rate:   rate,
			Qty:    qty,
			Sell:   sell,
			Filled: filled,
			Matches: []*core.Match{
				{
					MatchID: ordertest.RandomMatchID().Bytes(),
					Rate:    uint64(ord.Rate * 1e8),
					Qty:     uint64(rand.Float64() * float64(filled)),
					Status:  order.MatchComplete,
				},
			},
			TimeInForce: tif,
		})
	}
	return
}

var marketStats = make(map[string][2]float64)

func getMarketStats(mktID string) (midGap, maxQty float64) {
	stats := marketStats[mktID]
	return stats[0], stats[1]
}

func mkMrkt(base, quote string) *core.Market {
	baseID, _ := dex.BipSymbolID(base)
	quoteID, _ := dex.BipSymbolID(quote)
	mktID := base + "_" + quote
	assetOrder := rand.Intn(5) + 6
	lotSize := uint64(math.Pow10(assetOrder)) * uint64(rand.Intn(9)+1)
	rateStep := lotSize / 1e3
	if _, exists := marketStats[mktID]; !exists {
		midGap := float64(rateStep) * float64(rand.Intn(1e6))
		maxQty := float64(lotSize) * float64(rand.Intn(1e3))
		marketStats[mktID] = [2]float64{midGap, maxQty}
	}

	rate := uint64(rand.Intn(1e3)) * rateStep
	change24 := rand.Float64()*0.3 - .15

	mkt := &core.Market{
		Name:            fmt.Sprintf("%s_%s", base, quote),
		BaseID:          baseID,
		BaseSymbol:      base,
		QuoteID:         quoteID,
		QuoteSymbol:     quote,
		LotSize:         lotSize,
		ParcelSize:      10,
		RateStep:        rateStep,
		MarketBuyBuffer: rand.Float64() + 1,
		EpochLen:        uint64(epochDuration.Milliseconds()),
		SpotPrice: &msgjson.Spot{
			Stamp:   uint64(time.Now().UnixMilli()),
			BaseID:  baseID,
			QuoteID: quoteID,
			Rate:    rate,
			// BookVolume: ,
			Change24: change24,
			Vol24:    lotSize * uint64(50000*rand.Float32()),
		},
	}

	if (baseID != unsupportedAssetID) && (quoteID != unsupportedAssetID) {
		mkt.Orders = userOrders(mktID)
	}

	return mkt
}

func mkSupportedAsset(symbol string, state *core.WalletState) *core.SupportedAsset {
	assetID, _ := dex.BipSymbolID(symbol)
	winfo := winfos[assetID]
	var name string
	var unitInfo dex.UnitInfo
	if winfo == nil {
		name = tinfos[assetID].Name
		unitInfo = tinfos[assetID].UnitInfo
	} else {
		name = winfo.Name
		unitInfo = winfo.UnitInfo
	}

	return &core.SupportedAsset{
		ID:                    assetID,
		Symbol:                symbol,
		Info:                  winfo,
		Wallet:                state,
		Token:                 tinfos[assetID],
		Name:                  name,
		UnitInfo:              unitInfo,
		WalletCreationPending: assetID == atomic.LoadUint32(&creationPendingAsset),
	}
}

func mkDexAsset(symbol string) *dex.Asset {
	assetID, _ := dex.BipSymbolID(symbol)
	ui, err := asset.UnitInfo(assetID)
	if err != nil /* unknown asset*/ {
		ui = dex.UnitInfo{
			AtomicUnit: "Sats",
			Conventional: dex.Denomination{
				ConversionFactor: 1e8,
				Unit:             strings.ToUpper(symbol),
			},
		}
	}
	a := &dex.Asset{
		ID:         assetID,
		Symbol:     symbol,
		Version:    0,
		MaxFeeRate: uint64(rand.Intn(10) + 1),
		SwapConf:   uint32(rand.Intn(5) + 2),
		UnitInfo:   ui,
	}
	return a
}

func mkid(b, q uint32) string {
	return unbip(b) + "_" + unbip(q)
}

func getEpoch() uint64 {
	return uint64(time.Now().UnixMilli()) / uint64(epochDuration.Milliseconds())
}

func randomOrder(sell bool, maxQty, midGap, marketWidth float64, epoch bool) *core.MiniOrder {
	var epochIdx uint64
	var rate float64
	var limitRate = midGap - rand.Float64()*marketWidth
	if sell {
		limitRate = midGap + rand.Float64()*marketWidth
	}
	if epoch {
		epochIdx = getEpoch()
		// Epoch orders might be market orders.
		if rand.Float32() < 0.5 {
			rate = limitRate
		}
	} else {
		rate = limitRate
	}

	qty := uint64(math.Exp(-rand.Float64()*5) * maxQty)

	return &core.MiniOrder{
		Qty:       float64(qty) / float64(conversionFactor),
		QtyAtomic: qty,
		Rate:      float64(rate) / float64(conversionFactor),
		MsgRate:   uint64(rate),
		Sell:      sell,
		Token:     nextToken(),
		Epoch:     epochIdx,
	}
}

func miniOrderFromCoreOrder(ord *core.Order) *core.MiniOrder {
	var epoch uint64 = 555
	if ord.Status > order.OrderStatusEpoch {
		epoch = 0
	}
	return &core.MiniOrder{
		Qty:       float64(ord.Qty) / float64(conversionFactor),
		QtyAtomic: ord.Qty,
		Rate:      float64(ord.Rate) / float64(conversionFactor),
		MsgRate:   ord.Rate,
		Sell:      ord.Sell,
		Token:     ord.ID[:4].String(),
		Epoch:     epoch,
	}
}

var dexAssets = map[uint32]*dex.Asset{
	0:                  mkDexAsset("btc"),
	2:                  mkDexAsset("ltc"),
	42:                 mkDexAsset("dcr"),
	22:                 mkDexAsset("mona"),
	28:                 mkDexAsset("vtc"),
	unsupportedAssetID: mkDexAsset("kmd"),
	3:                  mkDexAsset("doge"),
	145:                mkDexAsset("bch"),
	60:                 mkDexAsset("eth"),
	60001:              mkDexAsset("usdc.eth"),
	133:                mkDexAsset("zec"),
	966001:             mkDexAsset("usdc.polygon"),
}

var tExchanges = map[string]*core.Exchange{
	firstDEX: {
		Host:   "somedex.com",
		Assets: dexAssets,
		AcctID: "abcdef0123456789",
		Markets: map[string]*core.Market{
			mkid(42, 0):       mkMrkt("dcr", "btc"),
			mkid(145, 42):     mkMrkt("bch", "dcr"),
			mkid(60, 42):      mkMrkt("eth", "dcr"),
			mkid(2, 42):       mkMrkt("ltc", "dcr"),
			mkid(3, 42):       mkMrkt("doge", "dcr"),
			mkid(22, 42):      mkMrkt("mona", "dcr"),
			mkid(28, 0):       mkMrkt("vtc", "btc"),
			mkid(60001, 42):   mkMrkt("usdc.eth", "dcr"),
			mkid(60, 60001):   mkMrkt("eth", "usdc.eth"),
			mkid(133, 966001): mkMrkt("zec", "usdc.polygon"),
		},
		ConnectionStatus: comms.Connected,
		CandleDurs:       []string{"1h", "24h"},
		Auth: core.ExchangeAuth{
			PendingBonds: []*core.PendingBondState{},
			BondAssetID:  42,
			TargetTier:   0,
			MaxBondedAmt: 100e8,
		},
		BondAssets: map[string]*core.BondAsset{
			"dcr": {
				ID:    42,
				Confs: 2,
				Amt:   1,
			},
		},
		ViewOnly: true,
		MaxScore: 60,
	},
	secondDEX: {
		Host:   "thisdexwithalongname.com",
		Assets: dexAssets,
		AcctID: "0123456789abcdef",
		Markets: map[string]*core.Market{
			mkid(42, 28):                 mkMrkt("dcr", "vtc"),
			mkid(0, 2):                   mkMrkt("btc", "ltc"),
			mkid(22, unsupportedAssetID): mkMrkt("mona", "kmd"),
		},
		ConnectionStatus: comms.Connected,
		CandleDurs:       []string{"5m", "1h", "24h"},
		Auth: core.ExchangeAuth{
			PendingBonds: []*core.PendingBondState{},
			BondAssetID:  42,
			TargetTier:   0,
			MaxBondedAmt: 100e8,
		},
		BondAssets: map[string]*core.BondAsset{
			"dcr": {
				ID:    42,
				Confs: 2,
				Amt:   1,
			},
		},
		ViewOnly: true,
	},
}

type tCoin struct {
	id       []byte
	confs    uint32
	confsErr error
}

func (c *tCoin) ID() dex.Bytes {
	return c.id
}

func (c *tCoin) String() string {
	return hex.EncodeToString(c.id)
}

func (c *tCoin) TxID() string {
	return hex.EncodeToString(c.id)
}

func (c *tCoin) Value() uint64 {
	return 0
}

func (c *tCoin) Confirmations(context.Context) (uint32, error) {
	return c.confs, c.confsErr
}

type tWalletState struct {
	walletType   string
	open         bool
	running      bool
	disabled     bool
	settings     map[string]string
	syncProgress uint32
}

type tBookFeed struct {
	core *TCore
	c    chan *core.BookUpdate
}

func (t *tBookFeed) Next() <-chan *core.BookUpdate {
	return t.c
}

func (t *tBookFeed) Close() {}

func (t *tBookFeed) Candles(dur string) error {
	t.core.sendCandles(dur)
	return nil
}

type TCore struct {
	inited    bool
	mtx       sync.RWMutex
	wallets   map[uint32]*tWalletState
	balances  map[uint32]*core.WalletBalance
	dexAddr   string
	marketID  string
	base      uint32
	quote     uint32
	candleDur struct {
		dur time.Duration
		str string
	}

	bookFeed    *tBookFeed
	killFeed    context.CancelFunc
	buys        map[string]*core.MiniOrder
	sells       map[string]*core.MiniOrder
	noteFeed    chan core.Notification
	orderMtx    sync.Mutex
	epochOrders []*core.BookUpdate
	fiatSources map[string]bool
	validAddr   bool
	lang        string
}

// TDriver implements the interface required of all exchange wallets.
type TDriver struct{}

func (drv *TDriver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	return true, nil
}

func (drv *TDriver) Create(*asset.CreateWalletParams) error {
	return nil
}

func (*TDriver) Open(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error) {
	return nil, nil
}

func (*TDriver) DecodeCoinID(coinID []byte) (string, error) {
	return asset.DecodeCoinID(0, coinID) // btc decoder
}

func (*TDriver) Info() *asset.WalletInfo {
	return &asset.WalletInfo{
		SupportedVersions: []uint32{0},
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: 1e8,
			},
		},
	}
}

func newTCore() *TCore {
	return &TCore{
		wallets: make(map[uint32]*tWalletState),
		balances: map[uint32]*core.WalletBalance{
			0:      randomWalletBalance(0),
			2:      randomWalletBalance(2),
			42:     randomWalletBalance(42),
			22:     randomWalletBalance(22),
			3:      randomWalletBalance(3),
			28:     randomWalletBalance(28),
			60:     randomWalletBalance(60),
			145:    randomWalletBalance(145),
			60001:  randomWalletBalance(60001),
			966:    randomWalletBalance(966),
			966001: randomWalletBalance(966001),
			133:    randomWalletBalance(133),
		},
		noteFeed: make(chan core.Notification, 1),
		fiatSources: map[string]bool{
			"dcrdata":     true,
			"Messari":     true,
			"Coinpaprika": true,
		},
		lang: "en-US",
	}
}

func (c *TCore) trySend(u *core.BookUpdate) {
	select {
	case c.bookFeed.c <- u:
	default:
	}
}

func (c *TCore) Network() dex.Network { return dex.Mainnet }

func (c *TCore) Exchanges() map[string]*core.Exchange { return tExchanges }

func (c *TCore) Exchange(host string) (*core.Exchange, error) {
	exchange, ok := tExchanges[host]
	if !ok {
		return nil, fmt.Errorf("no exchange at %v", host)
	}
	return exchange, nil
}

func (c *TCore) InitializeClient(pw []byte, seed *string) (string, error) {
	randomDelay()
	c.inited = true
	var mnemonicSeed string
	if seed == nil {
		_, mnemonicSeed = mnemonic.New()
	}
	return mnemonicSeed, nil
}
func (c *TCore) GetDEXConfig(host string, certI any) (*core.Exchange, error) {
	if xc := tExchanges[host]; xc != nil {
		return xc, nil
	}
	return tExchanges[firstDEX], nil
}

func (c *TCore) AddDEX(appPW []byte, dexAddr string, certI any) error {
	randomDelay()
	if initErrors {
		return fmt.Errorf("forced init error")
	}
	return nil
}

// DiscoverAccount - use secondDEX = "thisdexwithalongname.com" to get paid = true.
func (c *TCore) DiscoverAccount(dexAddr string, pw []byte, certI any) (*core.Exchange, bool, error) {
	xc := tExchanges[dexAddr]
	if xc == nil {
		xc = tExchanges[firstDEX]
	}
	if dexAddr == secondDEX {
		// c.reg = &core.RegisterForm{}
	}
	return tExchanges[firstDEX], dexAddr == secondDEX, nil
}
func (c *TCore) PostBond(form *core.PostBondForm) (*core.PostBondResult, error) {
	xc, exists := tExchanges[form.Addr]
	if !exists {
		return nil, fmt.Errorf("server %q not known", form.Addr)
	}
	symbol := dex.BipIDSymbol(*form.Asset)
	ba := xc.BondAssets[symbol]
	tier := form.Bond / ba.Amt
	xc.Auth.BondAssetID = *form.Asset
	xc.Auth.TargetTier = tier
	xc.Auth.Rep.BondedTier = int64(tier)
	xc.Auth.EffectiveTier = int64(tier)
	xc.ViewOnly = false
	return &core.PostBondResult{
		BondID:      "abc",
		ReqConfirms: uint16(ba.Confs),
	}, nil
}
func (c *TCore) RedeemPrepaidBond(appPW []byte, code []byte, host string, certI any) (tier uint64, err error) {
	return 1, nil
}
func (c *TCore) UpdateBondOptions(form *core.BondOptionsForm) error {
	xc := tExchanges[form.Host]
	xc.ViewOnly = false
	xc.Auth.TargetTier = *form.TargetTier
	xc.Auth.BondAssetID = *form.BondAssetID
	xc.Auth.EffectiveTier = int64(*form.TargetTier)
	xc.Auth.Rep.BondedTier = int64(*form.TargetTier)
	return nil
}
func (c *TCore) BondsFeeBuffer(assetID uint32) (uint64, error) {
	return 222, nil
}
func (c *TCore) ValidateAddress(address string, assetID uint32) (bool, error) {
	return len(address) > 10, nil
}
func (c *TCore) EstimateSendTxFee(addr string, assetID uint32, value uint64, subtract, maxWithdraw bool) (fee uint64, isValidAddress bool, err error) {
	return uint64(float64(value) * 0.01), len(addr) > 10, nil
}
func (c *TCore) Login([]byte) error  { return nil }
func (c *TCore) IsInitialized() bool { return c.inited }
func (c *TCore) Logout() error       { return nil }
func (c *TCore) Notifications(n int) (notes, pokes []*db.Notification, _ error) {
	return []*db.Notification{}, []*db.Notification{}, nil
}

var orderAssets = []string{"dcr", "btc", "ltc", "doge", "mona", "vtc", "usdc.eth"}

func (c *TCore) Orders(filter *core.OrderFilter) ([]*core.Order, error) {
	var spacing uint64 = 60 * 60 * 1000 / 2 // half an hour
	t := uint64(time.Now().UnixMilli())

	if randomizeOrdersCount {
		if rand.Float32() < 0.25 {
			return []*core.Order{}, nil
		}
		filter.N = rand.Intn(filter.N + 1)
	}

	cords := make([]*core.Order, 0, filter.N)
	for i := 0; i < int(filter.N); i++ {
		cord := makeCoreOrder()

		cord.Stamp = t
		// Make it a little older.
		t -= spacing

		cords = append(cords, cord)
	}
	return cords, nil
}

func (c *TCore) MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error) {
	mktID, _ := dex.MarketName(base, quote)
	lotSize := tExchanges[host].Markets[mktID].LotSize
	midGap, maxQty := getMarketStats(mktID)
	ord := randomOrder(rand.Float32() > 0.5, maxQty, midGap, gapWidthFactor*midGap, false)
	qty := ord.QtyAtomic
	quoteQty := calc.BaseToQuote(rate, qty)
	return &core.MaxOrderEstimate{
		Swap: &asset.SwapEstimate{
			Lots:               qty / lotSize,
			Value:              quoteQty,
			MaxFees:            quoteQty / 100,
			RealisticWorstCase: quoteQty / 200,
			RealisticBestCase:  quoteQty / 300,
		},
		Redeem: &asset.RedeemEstimate{
			RealisticWorstCase: qty / 300,
			RealisticBestCase:  qty / 400,
		},
	}, nil
}

func (c *TCore) MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error) {
	mktID, _ := dex.MarketName(base, quote)
	lotSize := tExchanges[host].Markets[mktID].LotSize
	midGap, maxQty := getMarketStats(mktID)
	ord := randomOrder(rand.Float32() > 0.5, maxQty, midGap, gapWidthFactor*midGap, false)
	qty := ord.QtyAtomic

	quoteQty := calc.BaseToQuote(uint64(midGap), qty)

	return &core.MaxOrderEstimate{
		Swap: &asset.SwapEstimate{
			Lots:               qty / lotSize,
			Value:              qty,
			MaxFees:            qty / 100,
			RealisticWorstCase: qty / 200,
			RealisticBestCase:  qty / 300,
		},
		Redeem: &asset.RedeemEstimate{
			RealisticWorstCase: quoteQty / 300,
			RealisticBestCase:  quoteQty / 400,
		},
	}, nil
}

func (c *TCore) PreOrder(*core.TradeForm) (*core.OrderEstimate, error) {
	return &core.OrderEstimate{
		Swap: &asset.PreSwap{
			Estimate: &asset.SwapEstimate{
				Lots:               5,
				Value:              5e8,
				MaxFees:            1600,
				RealisticWorstCase: 12010,
				RealisticBestCase:  6008,
			},
			Options: []*asset.OrderOption{
				{
					ConfigOption: asset.ConfigOption{
						Key:          "moredough",
						DisplayName:  "Get More Dough",
						Description:  "Cast a magical incantation to double the amount of XYZ received.",
						DefaultValue: true,
					},
					Boolean: &asset.BooleanConfig{
						Reason: "Cuz why not?",
					},
				},
				{
					ConfigOption: asset.ConfigOption{
						Key:          "awesomeness",
						DisplayName:  "More Awesomeness",
						Description:  "Crank up the awesomeness for next-level trading.",
						DefaultValue: 1.0,
					},
					XYRange: &asset.XYRange{
						Start: asset.XYRangePoint{
							Label: "Low",
							X:     1,
							Y:     3,
						},
						End: asset.XYRangePoint{
							Label: "High",
							X:     10,
							Y:     30,
						},
						XUnit: "X",
						YUnit: "kBTC",
					},
				},
			},
		},
		Redeem: &asset.PreRedeem{
			Estimate: &asset.RedeemEstimate{
				RealisticBestCase:  2800,
				RealisticWorstCase: 6500,
			},
			Options: []*asset.OrderOption{
				{
					ConfigOption: asset.ConfigOption{
						Key:          "lesshassle",
						DisplayName:  "Smoother Experience",
						Description:  "Select this option for a super-elite VIP DEX experience.",
						DefaultValue: false,
					},
					Boolean: &asset.BooleanConfig{
						Reason: "Half the time, twice the service",
					},
				},
			},
		},
	}, nil
}

func (c *TCore) AccountExport(pw []byte, host string) (*core.Account, []*db.Bond, error) {
	return nil, nil, nil
}
func (c *TCore) AccountImport(pw []byte, account *core.Account, bond []*db.Bond) error {
	return nil
}
func (c *TCore) ToggleAccountStatus(pw []byte, host string, disable bool) error { return nil }

func (c *TCore) TxHistory(assetID uint32, n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	return nil, nil
}

func coreCoin() *core.Coin {
	b := make([]byte, 36)
	copy(b[:], encode.RandomBytes(32))
	binary.BigEndian.PutUint32(b[32:], uint32(rand.Intn(15)))
	return core.NewCoin(0, b)
}

func coreSwapCoin() *core.Coin {
	c := coreCoin()
	c.SetConfirmations(int64(rand.Intn(3)), 2)
	return c
}

func makeCoreOrder() *core.Order {
	// sell := rand.Float32() < 0.5
	// center := randomMagnitude(-2, 4)
	// ord := randomOrder(sell, randomMagnitude(-2, 4), center, gapWidthFactor*center, true)
	host := firstDEX
	if rand.Float32() > 0.5 {
		host = secondDEX
	}
	mkts := make([]*core.Market, 0, len(tExchanges[host].Markets))
	for _, mkt := range tExchanges[host].Markets {
		if mkt.BaseID == unsupportedAssetID || mkt.QuoteID == unsupportedAssetID {
			continue
		}
		mkts = append(mkts, mkt)
	}
	mkt := mkts[rand.Intn(len(mkts))]
	rate := uint64(rand.Intn(1e3)) * mkt.RateStep
	baseQty := uint64(rand.Intn(1e3)) * mkt.LotSize
	isMarket := rand.Float32() > 0.5
	sell := rand.Float32() > 0.5
	numMatches := rand.Intn(13)
	orderQty := baseQty
	orderRate := rate
	matchQ := baseQty / 13
	matchQ -= matchQ % mkt.LotSize
	tif := order.TimeInForce(rand.Intn(int(order.StandingTiF)))

	if isMarket {
		orderRate = 0
		if !sell {
			orderQty = calc.BaseToQuote(rate, baseQty)
		}
	}

	numCoins := rand.Intn(5) + 1
	fundingCoins := make([]*core.Coin, 0, numCoins)
	for i := 0; i < numCoins; i++ {
		coinID := make([]byte, 36)
		copy(coinID[:], encode.RandomBytes(32))
		coinID[35] = byte(rand.Intn(8))
		fundingCoins = append(fundingCoins, core.NewCoin(0, coinID))
	}

	status := order.OrderStatus(rand.Intn(int(order.OrderStatusRevoked-1))) + 1

	stamp := func() uint64 {
		return uint64(time.Now().Add(-time.Second * time.Duration(rand.Intn(60*60))).UnixMilli())
	}

	cord := &core.Order{
		Host:        host,
		BaseID:      mkt.BaseID,
		BaseSymbol:  mkt.BaseSymbol,
		QuoteID:     mkt.QuoteID,
		QuoteSymbol: mkt.QuoteSymbol,
		MarketID:    mkt.BaseSymbol + "_" + mkt.QuoteSymbol,
		Type:        order.OrderType(rand.Intn(int(order.MarketOrderType))) + 1,
		Stamp:       stamp(),
		ID:          ordertest.RandomOrderID().Bytes(),
		Status:      status,
		Qty:         orderQty,
		Sell:        sell,
		Filled:      uint64(rand.Float64() * float64(orderQty)),
		Canceled:    status == order.OrderStatusCanceled,
		Rate:        orderRate,
		TimeInForce: tif,
		FeesPaid: &core.FeeBreakdown{
			Swap:       orderQty / 100,
			Redemption: mkt.RateStep * 100,
		},
		FundingCoins: fundingCoins,
	}

	for i := 0; i < numMatches; i++ {
		userMatch := ordertest.RandomUserMatch()
		matchQty := matchQ
		if i == numMatches-1 {
			matchQty = baseQty - (matchQ * (uint64(numMatches) - 1))
		}
		status := userMatch.Status
		side := userMatch.Side
		match := &core.Match{
			MatchID: userMatch.MatchID[:],
			Status:  userMatch.Status,
			Rate:    rate,
			Qty:     matchQty,
			Side:    userMatch.Side,
			Stamp:   stamp(),
		}

		if (status >= order.MakerSwapCast && side == order.Maker) ||
			(status >= order.TakerSwapCast && side == order.Taker) {

			match.Swap = coreSwapCoin()
		}

		refund := rand.Float32() < 0.1
		if refund {
			match.Refund = coreCoin()
		} else {
			if (status >= order.TakerSwapCast && side == order.Maker) ||
				(status >= order.MakerSwapCast && side == order.Taker) {

				match.CounterSwap = coreSwapCoin()
			}

			if (status >= order.MakerRedeemed && side == order.Maker) ||
				(status >= order.MatchComplete && side == order.Taker) {

				match.Redeem = coreCoin()
			}

			if (status >= order.MakerRedeemed && side == order.Taker) ||
				(status >= order.MatchComplete && side == order.Maker) {

				match.CounterRedeem = coreCoin()
			}
		}

		cord.Matches = append(cord.Matches, match)
	}
	return cord
}

func (c *TCore) Order(dex.Bytes) (*core.Order, error) {
	return makeCoreOrder(), nil
}

func (c *TCore) SyncBook(dexAddr string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error) {
	mktID, _ := dex.MarketName(base, quote)
	c.mtx.Lock()
	c.dexAddr = dexAddr
	c.marketID = mktID
	c.base = base
	c.quote = quote
	c.candleDur.dur = 0
	c.mtx.Unlock()

	xc := tExchanges[dexAddr]
	mkt := xc.Markets[mkid(base, quote)]

	usrOrds := tExchanges[dexAddr].Markets[mktID].Orders
	isUserOrder := func(tkn string) bool {
		for _, ord := range usrOrds {
			if tkn == ord.ID[:4].String() {
				return true
			}
		}
		return false
	}

	if c.bookFeed != nil {
		c.killFeed()
	}

	c.bookFeed = &tBookFeed{
		core: c,
		c:    make(chan *core.BookUpdate, 1),
	}
	var ctx context.Context
	ctx, c.killFeed = context.WithCancel(tCtx)
	go func() {
		tick := time.NewTicker(feedPeriod)
	out:
		for {
			select {
			case <-tick.C:
				// Send a random order to the order feed. Slighly biased away from
				// unbook_order and towards book_order.
				r := rand.Float32()
				switch {
				case r < 0.80:
					// Book order
					sell := rand.Float32() < 0.5
					midGap, maxQty := getMarketStats(mktID)
					ord := randomOrder(sell, maxQty, midGap, gapWidthFactor*midGap, true)
					c.orderMtx.Lock()
					side := c.buys
					if sell {
						side = c.sells
					}
					side[ord.Token] = ord
					epochOrder := &core.BookUpdate{
						Action:   msgjson.EpochOrderRoute,
						Host:     c.dexAddr,
						MarketID: mktID,
						Payload:  ord,
					}
					c.trySend(epochOrder)
					c.epochOrders = append(c.epochOrders, epochOrder)
					c.orderMtx.Unlock()
				default:
					// Unbook order
					sell := rand.Float32() < 0.5
					c.orderMtx.Lock()
					side := c.buys
					if sell {
						side = c.sells
					}
					var tkn string
					for tkn = range side {
						break
					}
					if tkn == "" {
						c.orderMtx.Unlock()
						continue
					}
					if isUserOrder(tkn) {
						// Our own order. Don't remove.
						c.orderMtx.Unlock()
						continue
					}
					delete(side, tkn)
					c.orderMtx.Unlock()

					c.trySend(&core.BookUpdate{
						Action:   msgjson.UnbookOrderRoute,
						Host:     c.dexAddr,
						MarketID: mktID,
						Payload:  &core.MiniOrder{Token: tkn},
					})
				}

				// Send a candle update.
				c.mtx.RLock()
				dur := c.candleDur.dur
				durStr := c.candleDur.str
				c.mtx.RUnlock()
				if dur == 0 {
					continue
				}
				c.trySend(&core.BookUpdate{
					Action:   core.CandleUpdateAction,
					Host:     dexAddr,
					MarketID: mktID,
					Payload: &core.CandleUpdate{
						Dur:          durStr,
						DurMilliSecs: uint64(dur.Milliseconds()),
						Candle:       candle(mkt, dur, time.Now()),
					},
				})

			case <-ctx.Done():
				break out
			}

		}
	}()

	c.bookFeed.c <- &core.BookUpdate{
		Action:   core.FreshBookAction,
		Host:     dexAddr,
		MarketID: mktID,
		Payload: &core.MarketOrderBook{
			Base:  base,
			Quote: quote,
			Book:  c.book(dexAddr, mktID),
		},
	}

	return nil, c.bookFeed, nil
}

func candle(mkt *core.Market, dur time.Duration, stamp time.Time) *msgjson.Candle {
	high, low, start, end, vol := candleStats(mkt.LotSize, mkt.RateStep, dur, stamp)
	quoteVol := calc.BaseToQuote(end, vol)

	return &msgjson.Candle{
		StartStamp:  uint64(stamp.Truncate(dur).UnixMilli()),
		EndStamp:    uint64(stamp.UnixMilli()),
		MatchVolume: vol,
		QuoteVolume: quoteVol,
		HighRate:    high,
		LowRate:     low,
		StartRate:   start,
		EndRate:     end,
	}
}

func candleStats(lotSize, rateStep uint64, candleDur time.Duration, stamp time.Time) (high, low, start, end, vol uint64) {
	freq := math.Pi * 2 / float64(candleDur.Milliseconds()*20)
	maxVol := 1e5 * float64(lotSize)
	volFactor := (math.Sin(float64(stamp.UnixMilli())*freq/2) + 1) / 2
	vol = uint64(maxVol * volFactor)

	waveFactor := (math.Sin(float64(stamp.UnixMilli())*freq) + 1) / 2
	priceVariation := 1e5 * float64(rateStep)
	priceFloor := 0.5 * priceVariation
	startWaveFactor := (math.Sin(float64(stamp.Truncate(candleDur).UnixMilli())*freq) + 1) / 2
	start = uint64(startWaveFactor*priceVariation + priceFloor)
	end = uint64(waveFactor*priceVariation + priceFloor)

	if start > end {
		diff := (start - end) / 2
		high = start + diff
		low = end - diff
	} else {
		diff := (end - start) / 2
		high = end + diff
		low = start - diff
	}
	return
}

func (c *TCore) sendCandles(durStr string) {
	randomDelay()
	dur, err := time.ParseDuration(durStr)
	if err != nil {
		panic("sendCandles ParseDuration error: " + err.Error())
	}

	c.mtx.RLock()
	c.candleDur.dur = dur
	c.candleDur.str = durStr
	dexAddr := c.dexAddr
	mktID := c.marketID
	xc := tExchanges[c.dexAddr]
	mkt := xc.Markets[mkid(c.base, c.quote)]
	c.mtx.RUnlock()

	tNow := time.Now()
	iStartTime := tNow.Add(-dur * candles.CacheSize).Truncate(dur)
	candles := make([]msgjson.Candle, 0, candles.CacheSize)

	for iStartTime.Before(tNow) {
		candles = append(candles, *candle(mkt, dur, iStartTime.Add(dur-1)))
		iStartTime = iStartTime.Add(dur)
	}

	c.bookFeed.c <- &core.BookUpdate{
		Action:   core.FreshCandlesAction,
		Host:     dexAddr,
		MarketID: mktID,
		Payload: &core.CandlesPayload{
			Dur:          durStr,
			DurMilliSecs: uint64(dur.Milliseconds()),
			Candles:      candles,
		},
	}
}

var numBuys = 80
var numSells = 80
var tokenCounter uint32

func nextToken() string {
	return strconv.Itoa(int(atomic.AddUint32(&tokenCounter, 1)))
}

// Book randomizes an order book.
func (c *TCore) book(dexAddr, mktID string) *core.OrderBook {
	midGap, maxQty := getMarketStats(mktID)
	// Set the market width to about 5% of midGap.
	var buys, sells []*core.MiniOrder
	c.orderMtx.Lock()
	c.buys = make(map[string]*core.MiniOrder, numBuys)
	c.sells = make(map[string]*core.MiniOrder, numSells)
	c.epochOrders = nil

	mkt := tExchanges[dexAddr].Markets[mktID]
	for _, ord := range mkt.Orders {
		if ord.Status != order.OrderStatusBooked {
			continue
		}
		ord := miniOrderFromCoreOrder(ord)
		if ord.Sell {
			sells = append(sells, ord)
			c.sells[ord.Token] = ord
		} else {
			buys = append(buys, ord)
			c.buys[ord.Token] = ord
		}
	}

	for i := 0; i < numSells; i++ {
		ord := randomOrder(true, maxQty, midGap, gapWidthFactor*midGap, false)
		sells = append(sells, ord)
		c.sells[ord.Token] = ord
	}
	for i := 0; i < numBuys; i++ {
		// For buys the rate must be smaller than midGap.
		ord := randomOrder(false, maxQty, midGap, gapWidthFactor*midGap, false)
		buys = append(buys, ord)
		c.buys[ord.Token] = ord
	}
	recentMatches := make([]*orderbook.MatchSummary, 0, 25)
	tNow := time.Now()
	for i := 0; i < 25; i++ {
		ord := randomOrder(rand.Float32() > 0.5, maxQty, midGap, gapWidthFactor*midGap, false)
		recentMatches = append(recentMatches, &orderbook.MatchSummary{
			Rate:  ord.MsgRate,
			Qty:   ord.QtyAtomic,
			Stamp: uint64(tNow.Add(-time.Duration(i) * time.Minute).UnixMilli()),
			Sell:  ord.Sell,
		})
	}
	c.orderMtx.Unlock()
	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })
	return &core.OrderBook{
		Buys:          buys,
		Sells:         sells,
		RecentMatches: recentMatches,
	}
}

func (c *TCore) Unsync(dex string, base, quote uint32) {
	if c.bookFeed != nil {
		c.killFeed()
	}
}

func randomWalletBalance(assetID uint32) *core.WalletBalance {
	avail := randomBalance()
	if assetID == 42 && avail < 20e8 {
		// Make Decred >= 1e8, to accommodate the registration fee.
		avail = 20e8
	}

	return &core.WalletBalance{
		Balance: &db.Balance{
			Balance: asset.Balance{
				Available: avail,
				Immature:  randomBalance(),
				Locked:    randomBalance(),
			},
			Stamp: time.Now().Add(-time.Duration(int64(2 * float64(time.Hour) * rand.Float64()))),
		},
		ContractLocked: randomBalance(),
		BondLocked:     randomBalance(),
	}
}

func randomBalance() uint64 {
	return uint64(rand.Float64() * math.Pow10(rand.Intn(4)+8))

}

func randomBalanceNote(assetID uint32) *core.BalanceNote {
	return &core.BalanceNote{
		Notification: db.NewNotification(core.NoteTypeBalance, core.TopicBalanceUpdated, "", "", db.Data),
		AssetID:      assetID,
		Balance:      randomWalletBalance(assetID),
	}
}

// random number logarithmically between [0, 10^x].
func tenToThe(x int) float64 {
	return math.Pow(10, float64(x)*rand.Float64())
}

func (c *TCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	balNote := randomBalanceNote(assetID)
	balNote.Balance.Stamp = time.Now()
	c.noteFeed <- balNote
	// c.mtx.Lock()
	// c.balances[assetID] = balNote.Balances
	// c.mtx.Unlock()
	return balNote.Balance, nil
}

func (c *TCore) AckNotes(ids []dex.Bytes) {}

var configOpts = []*asset.ConfigOption{
	{
		DisplayName: "RPC Server",
		Description: "RPC Server",
		Key:         "rpc_server",
	},
}
var winfos = map[uint32]*asset.WalletInfo{
	0:  btc.WalletInfo,
	2:  ltc.WalletInfo,
	42: dcr.WalletInfo,
	22: {
		SupportedVersions: []uint32{0},
		UnitInfo: dex.UnitInfo{
			AtomicUnit: "atoms",
			Conventional: dex.Denomination{
				Unit:             "MONA",
				ConversionFactor: 1e8,
			},
		},
		Name: "Monacoin",
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:   "1",
				Tab:    "Native",
				Seeded: true,
			},
			{
				Type:       "2",
				Tab:        "External",
				ConfigOpts: configOpts,
			},
		},
	},
	3: {
		SupportedVersions: []uint32{0},
		UnitInfo: dex.UnitInfo{
			AtomicUnit: "atoms",
			Conventional: dex.Denomination{
				Unit:             "DOGE",
				ConversionFactor: 1e8,
			},
		},
		Name: "Dogecoin",
		AvailableWallets: []*asset.WalletDefinition{{
			Type:       "2",
			Tab:        "External",
			ConfigOpts: configOpts,
		}},
	},
	28: {
		SupportedVersions: []uint32{0},
		UnitInfo: dex.UnitInfo{
			AtomicUnit: "Sats",
			Conventional: dex.Denomination{
				Unit:             "VTC",
				ConversionFactor: 1e8,
			},
		},
		Name: "Vertcoin",
		AvailableWallets: []*asset.WalletDefinition{{
			Type:       "2",
			Tab:        "External",
			ConfigOpts: configOpts,
		}},
	},
	60: &eth.WalletInfo,
	145: {
		SupportedVersions: []uint32{0},
		Name:              "Bitcoin Cash",
		UnitInfo:          dexbch.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:       "2",
			Tab:        "External",
			ConfigOpts: configOpts,
		}},
	},
	133: zec.WalletInfo,
	966: &polygon.WalletInfo,
}

var tinfos map[uint32]*asset.Token

func unitInfo(assetID uint32) dex.UnitInfo {
	if tinfo, found := tinfos[assetID]; found {
		return tinfo.UnitInfo
	}
	return winfos[assetID].UnitInfo
}

func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.walletState(assetID)
}

// walletState should be called with the c.mtx at least RLock'ed.
func (c *TCore) walletState(assetID uint32) *core.WalletState {
	w := c.wallets[assetID]
	if w == nil {
		return nil
	}

	traits := asset.WalletTrait(rand.Uint32())
	if assetID == 42 {
		traits |= asset.WalletTraitFundsMixer | asset.WalletTraitTicketBuyer
	}

	syncPct := atomic.LoadUint32(&w.syncProgress)
	return &core.WalletState{
		Symbol:       unbip(assetID),
		AssetID:      assetID,
		WalletType:   w.walletType,
		Open:         w.open,
		Running:      w.running,
		Address:      ordertest.RandomAddress(),
		Balance:      c.balances[assetID],
		Units:        unitInfo(assetID).AtomicUnit,
		Encrypted:    true,
		PeerCount:    10,
		Synced:       syncPct == 100,
		SyncProgress: float32(syncPct) / 100,
		SyncStatus:   &asset.SyncStatus{Synced: syncPct == 100, TargetHeight: 100, Blocks: uint64(syncPct)},
		Traits:       traits,
	}
}

func (c *TCore) CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error {
	randomDelay()
	if initErrors {
		return fmt.Errorf("forced init error")
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()

	// If this is a token, simulate parent syncing.
	token := asset.TokenInfo(form.AssetID)
	if token == nil || form.ParentForm == nil {
		c.createWallet(form, false)
		return nil
	}

	atomic.StoreUint32(&creationPendingAsset, form.AssetID)

	synced := c.createWallet(form.ParentForm, false)

	c.noteFeed <- &core.WalletCreationNote{
		Notification: db.NewNotification(core.NoteTypeCreateWallet, core.TopicCreationQueued, "", "", db.Data),
		AssetID:      form.AssetID,
	}

	go func() {
		<-synced
		defer atomic.StoreUint32(&creationPendingAsset, 0xFFFFFFFF)
		if doubleCreateAsyncErr {
			c.noteFeed <- &core.WalletCreationNote{
				Notification: db.NewNotification(core.NoteTypeCreateWallet, core.TopicQueuedCreationFailed,
					"Test Error", "This failed because doubleCreateAsyncErr is true in live_test.go", db.Data),
				AssetID: form.AssetID,
			}
			return
		}
		c.createWallet(form, true)
	}()
	return nil
}

func (c *TCore) createWallet(form *core.WalletForm, synced bool) (done chan struct{}) {
	done = make(chan struct{})

	tWallet := &tWalletState{
		walletType: form.Type,
		running:    true,
		open:       true,
		settings:   form.Config,
	}
	c.wallets[form.AssetID] = tWallet

	w := c.walletState(form.AssetID)
	var regFee uint64
	r, found := tExchanges[firstDEX].BondAssets[w.Symbol]
	if found {
		regFee = r.Amt
	}

	sendWalletState := func() {
		wCopy := *w
		c.noteFeed <- &core.WalletStateNote{
			Notification: db.NewNotification(core.NoteTypeWalletState, core.TopicWalletState, "", "", db.Data),
			Wallet:       &wCopy,
		}
	}

	defer func() {
		sendWalletState()
		if asset.TokenInfo(form.AssetID) != nil {
			c.noteFeed <- &core.WalletCreationNote{
				Notification: db.NewNotification(core.NoteTypeCreateWallet, core.TopicQueuedCreationSuccess, "", "", db.Data),
				AssetID:      form.AssetID,
			}
		}
		if delayBalance {
			time.AfterFunc(time.Second*10, func() {
				avail := w.Balance.Available
				if avail < regFee {
					avail = 2 * regFee
					w.Balance.Available = avail
				}
				w.Balance.Available = regFee
				c.noteFeed <- &core.BalanceNote{
					Notification: db.NewNotification(core.NoteTypeBalance, core.TopicBalanceUpdated, "", "", db.Data),
					AssetID:      form.AssetID,
					Balance: &core.WalletBalance{
						Balance: &db.Balance{
							Balance: asset.Balance{
								Available: avail,
							},
							Stamp: time.Now(),
						},
					},
				}
			})
		}
	}()

	if !delayBalance {
		w.Balance.Available = regFee
	}

	w.Synced = synced
	if synced {
		atomic.StoreUint32(&tWallet.syncProgress, 100)
		w.SyncProgress = 1
		close(done)
		return
	}

	w.SyncProgress = 0.0

	tStart := time.Now()
	syncDuration := float64(time.Second * 6)

	syncProgress := func() float32 {
		progress := float64(time.Since(tStart)) / syncDuration
		if progress > 1 {
			progress = 1
		}
		return float32(progress)
	}

	setProgress := func() bool {
		progress := syncProgress()
		atomic.StoreUint32(&tWallet.syncProgress, uint32(math.Round(float64(progress)*100)))
		c.mtx.Lock()
		defer c.mtx.Unlock()
		w.SyncProgress = progress
		synced := progress == 1
		w.Synced = synced
		sendWalletState()
		return synced
	}

	go func() {
		defer close(done)
		for {
			select {
			case <-time.After(time.Millisecond * 1013):
				if setProgress() {
					return
				}
			case <-tCtx.Done():
				return
			}
		}
	}()

	return
}

func (c *TCore) RescanWallet(assetID uint32, force bool) error {
	return nil
}

func (c *TCore) OpenWallet(assetID uint32, pw []byte) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to open non-existent test wallet for asset ID %d", assetID)
	}
	if wallet.disabled {
		return fmt.Errorf("wallet is disabled")
	}
	wallet.running = true
	wallet.open = true
	return nil
}

func (c *TCore) ConnectWallet(assetID uint32) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to connect to non-existent test wallet for asset ID %d", assetID)
	}
	if wallet.disabled {
		return fmt.Errorf("wallet is disabled")
	}
	wallet.running = true
	return nil
}

func (c *TCore) CloseWallet(assetID uint32) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to close non-existent test wallet")
	}

	if forceDisconnectWallet {
		wallet.running = false
	}

	wallet.open = false
	return nil
}

func (c *TCore) Wallets() []*core.WalletState {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	states := make([]*core.WalletState, 0, len(c.wallets))
	for assetID, wallet := range c.wallets {
		states = append(states, &core.WalletState{
			Symbol:    unbip(assetID),
			AssetID:   assetID,
			Open:      wallet.open,
			Running:   wallet.running,
			Disabled:  wallet.disabled,
			Address:   ordertest.RandomAddress(),
			Balance:   c.balances[assetID],
			Units:     unitInfo(assetID).AtomicUnit,
			Encrypted: true,
			Traits:    asset.WalletTrait(rand.Uint32()),
		})
	}
	return states
}
func (c *TCore) AccelerateOrder(pw []byte, oidB dex.Bytes, newFeeRate uint64) (string, error) {
	return "", nil
}
func (c *TCore) AccelerationEstimate(oidB dex.Bytes, newFeeRate uint64) (uint64, error) {
	return 0, nil
}
func (c *TCore) PreAccelerateOrder(oidB dex.Bytes) (*core.PreAccelerate, error) {
	return nil, nil
}
func (c *TCore) WalletSettings(assetID uint32) (map[string]string, error) {
	return c.wallets[assetID].settings, nil
}

func (c *TCore) ReconfigureWallet(aPW, nPW []byte, form *core.WalletForm) error {
	c.wallets[form.AssetID].settings = form.Config
	return nil
}

func (c *TCore) ToggleWalletStatus(assetID uint32, disable bool) error {
	w, ok := c.wallets[assetID]
	if !ok {
		return fmt.Errorf("wallet with id %d not found", assetID)
	}

	var err error
	if disable {
		err = c.CloseWallet(assetID)
		c.mtx.Lock()
		w.disabled = disable
		c.mtx.Unlock()
	} else {
		c.mtx.Lock()
		w.disabled = disable
		c.mtx.Unlock()
		err = c.OpenWallet(assetID, []byte(""))
	}
	if err != nil {
		return err
	}

	return nil
}

func (c *TCore) ChangeAppPass(appPW, newAppPW []byte) error {
	return nil
}

func (c *TCore) ResetAppPass(newAppPW []byte, seed string) error {
	return nil
}

func (c *TCore) NewDepositAddress(assetID uint32) (string, error) {
	return ordertest.RandomAddress(), nil
}

func (c *TCore) AddressUsed(assetID uint32, addr string) (bool, error) {
	return rand.Float32() > 0.5, nil
}

func (c *TCore) SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error { return nil }

func (c *TCore) User() *core.User {
	user := &core.User{
		Exchanges:   tExchanges,
		Initialized: c.inited,
		Assets:      c.SupportedAssets(),
		FiatRates: map[uint32]float64{
			0:      64_551.61, // btc
			2:      59.08,     // ltc
			42:     25.46,     // dcr
			22:     0.5117,    // mona
			28:     0.1599,    // vtc
			141:    0.2048,    // kmd
			3:      0.06769,   // doge
			145:    114.68,    // bch
			60:     1_209.51,  // eth
			60001:  0.999,     // usdc.eth
			133:    26.75,
			966:    0.7001,
			966001: 1.001,
		},
		Actions: actions,
	}
	return user
}

func (c *TCore) AutoWalletConfig(assetID uint32, walletType string) (map[string]string, error) {
	return map[string]string{
		"username": "tacotime",
		"password": "abc123",
	}, nil
}

func (c *TCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return map[uint32]*core.SupportedAsset{
		0:      mkSupportedAsset("btc", c.walletState(0)),
		42:     mkSupportedAsset("dcr", c.walletState(42)),
		2:      mkSupportedAsset("ltc", c.walletState(2)),
		22:     mkSupportedAsset("mona", c.walletState(22)),
		3:      mkSupportedAsset("doge", c.walletState(3)),
		28:     mkSupportedAsset("vtc", c.walletState(28)),
		60:     mkSupportedAsset("eth", c.walletState(60)),
		145:    mkSupportedAsset("bch", c.walletState(145)),
		60001:  mkSupportedAsset("usdc.eth", c.walletState(60001)),
		966:    mkSupportedAsset("polygon", c.walletState(966)),
		966001: mkSupportedAsset("usdc.polygon", c.walletState(966001)),
		133:    mkSupportedAsset("zec", c.walletState(133)),
	}
}

func (c *TCore) Send(pw []byte, assetID uint32, value uint64, address string, subtract bool) (asset.Coin, error) {
	return &tCoin{id: []byte{0xde, 0xc7, 0xed}}, nil
}
func (c *TCore) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	return c.trade(form), nil
}
func (c *TCore) TradeAsync(pw []byte, form *core.TradeForm) (*core.InFlightOrder, error) {
	return &core.InFlightOrder{
		Order:       c.trade(form),
		TemporaryID: uint64(rand.Int63()),
	}, nil
}
func (c *TCore) trade(form *core.TradeForm) *core.Order {
	c.OpenWallet(form.Quote, []byte(""))
	c.OpenWallet(form.Base, []byte(""))
	oType := order.LimitOrderType
	if !form.IsLimit {
		oType = order.MarketOrderType
	}
	return &core.Order{
		ID:    ordertest.RandomOrderID().Bytes(),
		Type:  oType,
		Stamp: uint64(time.Now().UnixMilli()),
		Rate:  form.Rate,
		Qty:   form.Qty,
		Sell:  form.Sell,
	}
}

func (c *TCore) Cancel(oid dex.Bytes) error {
	for _, xc := range tExchanges {
		for _, mkt := range xc.Markets {
			for _, ord := range mkt.Orders {
				if ord.ID.String() == oid.String() {
					ord.Cancelling = true
				}
			}
		}
	}
	return nil
}

func (c *TCore) NotificationFeed() *core.NoteFeed {
	return &core.NoteFeed{
		C: c.noteFeed,
	}
}

func (c *TCore) runEpochs() {
	epochTick := time.NewTimer(time.Second).C
out:
	for {
		select {
		case <-epochTick:
			epochTick = time.NewTimer(epochDuration - time.Since(time.Now().Truncate(epochDuration))).C
			c.mtx.RLock()
			dexAddr := c.dexAddr
			mktID := c.marketID
			baseID := c.base
			quoteID := c.quote
			baseConnected := false
			if w := c.wallets[baseID]; w != nil && w.running {
				baseConnected = true
			}
			quoteConnected := false
			if w := c.wallets[quoteID]; w != nil && w.running {
				quoteConnected = true
			}
			c.mtx.RUnlock()

			if c.dexAddr == "" {
				continue
			}

			c.noteFeed <- &core.EpochNotification{
				Host:         dexAddr,
				MarketID:     mktID,
				Notification: db.NewNotification(core.NoteTypeEpoch, core.TopicEpoch, "", "", db.Data),
				Epoch:        getEpoch(),
			}

			rateStep := tExchanges[dexAddr].Markets[mktID].RateStep
			rate := uint64(rand.Intn(1e3)) * rateStep
			change24 := rand.Float64()*0.3 - .15

			c.noteFeed <- &core.SpotPriceNote{
				Host:         dexAddr,
				Notification: db.NewNotification(core.NoteTypeSpots, core.TopicSpotsUpdate, "", "", db.Data),
				Spots: map[string]*msgjson.Spot{mktID: {
					Stamp:   uint64(time.Now().UnixMilli()),
					BaseID:  baseID,
					QuoteID: quoteID,
					Rate:    rate,
					// BookVolume: ,
					Change24: change24,
					// Vol24: ,
				}},
			}

			// randomize the balance
			if baseID != unsupportedAssetID && baseConnected { // komodo unsupported
				c.noteFeed <- randomBalanceNote(baseID)
			}
			if quoteID != unsupportedAssetID && quoteConnected { // komodo unsupported
				c.noteFeed <- randomBalanceNote(quoteID)
			}

			c.orderMtx.Lock()
			// Send limit orders as newly booked.
			for _, o := range c.epochOrders {
				miniOrder := o.Payload.(*core.MiniOrder)
				if miniOrder.Rate > 0 {
					miniOrder.Epoch = 0
					o.Action = msgjson.BookOrderRoute
					c.trySend(o)
					if miniOrder.Sell {
						c.sells[miniOrder.Token] = miniOrder
					} else {
						c.buys[miniOrder.Token] = miniOrder
					}
				}
			}
			c.epochOrders = nil
			c.orderMtx.Unlock()

			// Small chance of randomly generating a required action
			if enableActions && rand.Float32() < 0.05 {
				c.noteFeed <- &core.WalletNote{
					Notification: db.NewNotification(core.NoteTypeWalletNote, core.TopicWalletNotification, "", "", db.Data),
					Payload:      makeRequiredAction(baseID, "missingNonces"),
				}
			}
		case <-tCtx.Done():
			break out
		}
	}
}

var (
	randChars = []byte("abcd efgh ijkl mnop qrst uvwx yz123")
	numChars  = len(randChars)
)

func randStr(minLen, maxLen int) string {
	strLen := rand.Intn(maxLen-minLen) + minLen
	b := make([]byte, 0, strLen)
	for i := 0; i < strLen; i++ {
		b = append(b, randChars[rand.Intn(numChars)])
	}
	return strings.Trim(string(b), " ")
}

func (c *TCore) runRandomPokes() {
	nextWait := func() time.Duration {
		return time.Duration(float64(time.Second)*rand.Float64()) * 10
	}
	for {
		select {
		case <-time.NewTimer(nextWait()).C:
			note := db.NewNotification(randStr(5, 30), core.Topic(randStr(5, 30)), titler.String(randStr(5, 30)), randStr(5, 100), db.Poke)
			c.noteFeed <- &note
		case <-tCtx.Done():
			return
		}
	}
}

func (c *TCore) runRandomNotes() {
	nextWait := func() time.Duration {
		return time.Duration(float64(time.Second)*rand.Float64()) * 5
	}
	for {
		select {
		case <-time.NewTimer(nextWait()).C:
			roll := rand.Float32()
			severity := db.Success
			if roll < 0.05 {
				severity = db.ErrorLevel
			} else if roll < 0.10 {
				severity = db.WarningLevel
			}

			note := db.NewNotification(randStr(5, 30), core.Topic(randStr(5, 30)), titler.String(randStr(5, 30)), randStr(5, 100), severity)
			c.noteFeed <- &note
		case <-tCtx.Done():
			return
		}
	}
}

func (c *TCore) ExportSeed(pw []byte) (string, error) {
	return "copper life simple hello fit manage dune curve argue gadget erosion fork theme chase broccoli", nil
}
func (c *TCore) WalletLogFilePath(uint32) (string, error) {
	return "", nil
}
func (c *TCore) RecoverWallet(uint32, []byte, bool) error {
	return nil
}
func (c *TCore) UpdateCert(string, []byte) error {
	return nil
}
func (c *TCore) UpdateDEXHost(string, string, []byte, any) (*core.Exchange, error) {
	return nil, nil
}
func (c *TCore) WalletRestorationInfo(pw []byte, assetID uint32) ([]*asset.WalletRestoration, error) {
	return nil, nil
}
func (c *TCore) ToggleRateSourceStatus(src string, disable bool) error {
	c.fiatSources[src] = !disable
	return nil
}
func (c *TCore) FiatRateSources() map[string]bool {
	return c.fiatSources
}
func (c *TCore) DeleteArchivedRecordsWithBackup(olderThan *time.Time, saveMatchesToFile, saveOrdersToFile bool) (string, int, error) {
	return "/path/to/records", 10, nil
}
func (c *TCore) WalletPeers(assetID uint32) ([]*asset.WalletPeer, error) {
	return nil, nil
}
func (c *TCore) AddWalletPeer(assetID uint32, address string) error {
	return nil
}
func (c *TCore) RemoveWalletPeer(assetID uint32, address string) error {
	return nil
}
func (c *TCore) ApproveToken(appPW []byte, assetID uint32, dexAddr string, onConfirm func()) (string, error) {
	return "", nil
}
func (c *TCore) UnapproveToken(appPW []byte, assetID uint32, version uint32) (string, error) {
	return "", nil
}
func (c *TCore) ApproveTokenFee(assetID uint32, version uint32, approval bool) (uint64, error) {
	return 0, nil
}

func (c *TCore) StakeStatus(assetID uint32) (*asset.TicketStakingStatus, error) {
	res := asset.TicketStakingStatus{
		TicketPrice:   24000000000,
		VotingSubsidy: 1200000,
		VSP:           "",
		IsRPC:         false,
		Tickets:       []*asset.Ticket{},
		Stances: asset.Stances{
			Agendas:        []*asset.TBAgenda{},
			TreasurySpends: []*asset.TBTreasurySpend{},
		},
		Stats: asset.TicketStats{},
	}
	return &res, nil
}

func (c *TCore) SetVSP(assetID uint32, addr string) error {
	return nil
}

func (c *TCore) PurchaseTickets(assetID uint32, pw []byte, n int) error {
	return nil
}

func (c *TCore) SetVotingPreferences(assetID uint32, choices, tSpendPolicy, treasuryPolicy map[string]string) error {
	return nil
}

func (c *TCore) ListVSPs(assetID uint32) ([]*asset.VotingServiceProvider, error) {
	vsps := []*asset.VotingServiceProvider{
		{
			URL:           "https://example.com",
			FeePercentage: 0.1,
			Voting:        12345,
		},
	}
	return vsps, nil
}

func (c *TCore) TicketPage(assetID uint32, scanStart int32, n, skipN int) ([]*asset.Ticket, error) {
	return nil, nil
}

func (c *TCore) FundsMixingStats(assetID uint32) (*asset.FundsMixingStats, error) {
	return nil, nil
}

func (c *TCore) ConfigureFundsMixer(appPW []byte, assetID uint32, enabled bool) error {
	return nil
}

func (c *TCore) SetLanguage(lang string) error {
	c.lang = lang
	return nil
}

func (c *TCore) Language() string {
	return c.lang
}

func (c *TCore) TakeAction(assetID uint32, actionID string, actionB json.RawMessage) error {
	if rand.Float32() < 0.25 {
		return fmt.Errorf("it didn't work")
	}
	for i, req := range actions {
		if req.ActionID == actionID && req.AssetID == assetID {
			copy(actions[i:], actions[i+1:])
			actions = actions[:len(actions)-1]
			c.noteFeed <- &core.WalletNote{
				Notification: db.NewNotification(core.NoteTypeWalletNote, core.TopicWalletNotification, "", "", db.Data),
				Payload:      makeActionResolved(assetID, req.UniqueID),
			}
			break
		}
	}
	return nil
}

func (c *TCore) RedeemGeocode(appPW, code []byte, msg string) (dex.Bytes, uint64, error) {
	coinID, _ := hex.DecodeString("308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad900000001")
	return coinID, 100e8, nil
}

func (*TCore) ExtensionModeConfig() *core.ExtensionModeConfig {
	return nil
}

func newMarketDay() *libxc.MarketDay {
	avgPrice := tenToThe(7)
	return &libxc.MarketDay{
		Vol:            tenToThe(7),
		QuoteVol:       tenToThe(7),
		PriceChange:    tenToThe(7) - 2*tenToThe(7),
		PriceChangePct: 0.15 - rand.Float64()*0.3,
		AvgPrice:       avgPrice,
		LastPrice:      avgPrice * (1 + (0.05 - 0.1*rand.Float64())),
		OpenPrice:      avgPrice * (1 + (0.05 - 0.1*rand.Float64())),
		HighPrice:      avgPrice * (1 + 0.15 + (0.1 - 0.2*rand.Float64())),
		LowPrice:       avgPrice * (1 - 0.15 + (0.1 - 0.2*rand.Float64())),
	}
}

var binanceMarkets = map[string]*libxc.Market{
	"dcr_btc": {
		BaseID:  42,
		QuoteID: 0,
		Day:     newMarketDay(),
	},
	"eth_dcr": {
		BaseID:  60,
		QuoteID: 42,
		Day:     newMarketDay(),
	},
	"zec_usdc.polygon": {
		BaseID:  133,
		QuoteID: 966001,
		Day:     newMarketDay(),
	},
	"eth_usdc.eth": {
		BaseID:  60,
		QuoteID: 60001,
		Day:     newMarketDay(),
	},
}

type TMarketMaker struct {
	core *TCore
	cfg  *mm.MarketMakingConfig

	runningBotsMtx sync.RWMutex
	runningBots    map[mm.MarketWithHost]int64 // mkt -> startTime
}

func tLotFees() *mm.LotFees {
	return &mm.LotFees{
		Swap:   randomBalance() / 100,
		Redeem: randomBalance() / 100,
		Refund: randomBalance() / 100,
	}
}

func randomProfitLoss(baseID, quoteID uint32) *mm.ProfitLoss {
	return &mm.ProfitLoss{
		Initial: map[uint32]*mm.Amount{
			baseID:  mm.NewAmount(baseID, int64(randomBalance()), tenToThe(5)),
			quoteID: mm.NewAmount(quoteID, int64(randomBalance()), tenToThe(5)),
		},
		InitialUSD: tenToThe(5),
		Mods: map[uint32]*mm.Amount{
			baseID:  mm.NewAmount(baseID, int64(randomBalance()), tenToThe(5)),
			quoteID: mm.NewAmount(quoteID, int64(randomBalance()), tenToThe(5)),
		},
		ModsUSD: tenToThe(5),
		Final: map[uint32]*mm.Amount{
			baseID:  mm.NewAmount(baseID, int64(randomBalance()), tenToThe(5)),
			quoteID: mm.NewAmount(quoteID, int64(randomBalance()), tenToThe(5)),
		},
		FinalUSD:    tenToThe(5),
		Profit:      tenToThe(5),
		ProfitRatio: 0.2 - rand.Float64()*0.4,
	}
}

func (m *TMarketMaker) MarketReport(host string, baseID, quoteID uint32) (*mm.MarketReport, error) {
	baseFiatRate := math.Pow10(3 - rand.Intn(6))
	quoteFiatRate := math.Pow10(3 - rand.Intn(6))
	price := baseFiatRate / quoteFiatRate
	mktID := dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID)
	midGap, _ := getMarketStats(mktID)
	return &mm.MarketReport{
		BaseFiatRate:  baseFiatRate,
		QuoteFiatRate: quoteFiatRate,
		Price:         price,
		Oracles: []*mm.OracleReport{
			{
				Host:     "bittrex.com",
				USDVol:   tenToThe(7),
				BestBuy:  midGap * 99 / 100,
				BestSell: midGap * 101 / 100,
			},
			{
				Host:     "binance.com",
				USDVol:   tenToThe(7),
				BestBuy:  midGap * 98 / 100,
				BestSell: midGap * 102 / 100,
			},
		},
		BaseFees: &mm.LotFeeRange{
			Max:       tLotFees(),
			Estimated: tLotFees(),
		},
		QuoteFees: &mm.LotFeeRange{
			Max:       tLotFees(),
			Estimated: tLotFees(),
		},
	}, nil
}

func (m *TMarketMaker) StartBot(startCfg *mm.StartConfig, alternateConfigPath *string, appPW []byte, overrideLotSizeUpdate bool) (err error) {
	m.runningBotsMtx.Lock()
	defer m.runningBotsMtx.Unlock()

	mkt := startCfg.MarketWithHost
	_, running := m.runningBots[mkt]
	if running {
		return fmt.Errorf("bot already running for %s", mkt)
	}
	startTime := time.Now().Unix()
	m.runningBots[mkt] = startTime

	m.core.noteFeed <- &struct {
		db.Notification
		Host      string       `json:"host"`
		Base      uint32       `json:"baseID"`
		Quote     uint32       `json:"quoteID"`
		StartTime int64        `json:"startTime"`
		Stats     *mm.RunStats `json:"stats"`
	}{
		Notification: db.NewNotification("runstats", "", "", "", db.Data),
		Host:         mkt.Host,
		Base:         mkt.BaseID,
		Quote:        mkt.QuoteID,
		StartTime:    startTime,
		Stats: &mm.RunStats{
			InitialBalances: map[uint32]uint64{
				mkt.BaseID: randomBalance(),
				mkt.BaseID: randomBalance(),
			},
			DEXBalances: map[uint32]*mm.BotBalance{
				mkt.BaseID: {
					Available: randomBalance(),
					Locked:    randomBalance(),
					Pending:   randomBalance(),
					Reserved:  randomBalance(),
				},
				mkt.BaseID: {
					Available: randomBalance(),
					Locked:    randomBalance(),
					Pending:   randomBalance(),
					Reserved:  randomBalance(),
				},
			},
			CEXBalances: map[uint32]*mm.BotBalance{
				mkt.BaseID: {
					Available: randomBalance(),
					Locked:    randomBalance(),
					Pending:   randomBalance(),
					Reserved:  randomBalance(),
				},
				mkt.BaseID: {
					Available: randomBalance(),
					Locked:    randomBalance(),
					Pending:   randomBalance(),
					Reserved:  randomBalance(),
				},
			},
			ProfitLoss:         randomProfitLoss(mkt.BaseID, mkt.QuoteID),
			StartTime:          startTime,
			PendingDeposits:    rand.Intn(3),
			PendingWithdrawals: rand.Intn(3),
			CompletedMatches:   uint32(math.Pow(10, 3*rand.Float64())),
			TradedUSD:          math.Pow(10, 3*rand.Float64()),
			FeeGap:             randomFeeGapStats(),
		},
	}
	return nil
}

func (m *TMarketMaker) StopBot(mkt *mm.MarketWithHost) error {
	m.runningBotsMtx.Lock()
	startTime, running := m.runningBots[*mkt]
	if !running {
		m.runningBotsMtx.Unlock()
		return fmt.Errorf("bot not running for %s", mkt.String())
	}
	delete(m.runningBots, *mkt)
	m.runningBotsMtx.Unlock()

	m.core.noteFeed <- &struct {
		db.Notification
		Host      string       `json:"host"`
		Base      uint32       `json:"baseID"`
		Quote     uint32       `json:"quoteID"`
		StartTime int64        `json:"startTime"`
		Stats     *mm.RunStats `json:"stats"`
	}{
		Notification: db.NewNotification("runstats", "", "", "", db.Data),
		Host:         mkt.Host,
		Base:         mkt.BaseID,
		Quote:        mkt.QuoteID,
		StartTime:    startTime,
		Stats:        nil,
	}
	return nil
}

func (m *TMarketMaker) UpdateCEXConfig(updatedCfg *mm.CEXConfig) error {
	for i := 0; i < len(m.cfg.CexConfigs); i++ {
		cfg := m.cfg.CexConfigs[i]
		if cfg.Name == updatedCfg.Name {
			m.cfg.CexConfigs[i] = updatedCfg
			return nil
		}
	}
	m.cfg.CexConfigs = append(m.cfg.CexConfigs, updatedCfg)
	return nil
}

func (m *TMarketMaker) UpdateBotConfig(updatedCfg *mm.BotConfig) error {
	for i := 0; i < len(m.cfg.BotConfigs); i++ {
		botCfg := m.cfg.BotConfigs[i]
		if botCfg.Host == updatedCfg.Host && botCfg.BaseID == updatedCfg.BaseID && botCfg.QuoteID == updatedCfg.QuoteID {
			m.cfg.BotConfigs[i] = updatedCfg
			return nil
		}
	}
	m.cfg.BotConfigs = append(m.cfg.BotConfigs, updatedCfg)
	return nil
}

func (m *TMarketMaker) UpdateRunningBot(updatedCfg *mm.BotConfig, balanceDiffs *mm.BotInventoryDiffs, saveUpdate bool) error {
	return m.UpdateBotConfig(updatedCfg)
}

func (m *TMarketMaker) RemoveBotConfig(host string, baseID, quoteID uint32) error {
	for i := 0; i < len(m.cfg.BotConfigs); i++ {
		botCfg := m.cfg.BotConfigs[i]
		if botCfg.Host == host && botCfg.BaseID == baseID && botCfg.QuoteID == quoteID {
			copy(m.cfg.BotConfigs[i:], m.cfg.BotConfigs[i+1:])
			m.cfg.BotConfigs = m.cfg.BotConfigs[:len(m.cfg.BotConfigs)-1]
		}
	}
	return nil
}

func (m *TMarketMaker) CEXBalance(cexName string, assetID uint32) (*libxc.ExchangeBalance, error) {
	bal := randomWalletBalance(assetID)
	return &libxc.ExchangeBalance{
		Available: bal.Available,
		Locked:    bal.Locked,
	}, nil
}

func randomFeeGapStats() *mm.FeeGapStats {
	return &mm.FeeGapStats{
		BasisPrice:    uint64(tenToThe(8) * 1e6),
		RemoteGap:     uint64(tenToThe(8) * 1e6),
		FeeGap:        uint64(tenToThe(8) * 1e6),
		RoundTripFees: uint64(tenToThe(8) * 1e6),
	}
}

func (m *TMarketMaker) Status() *mm.Status {
	status := &mm.Status{
		CEXes: make(map[string]*mm.CEXStatus, len(m.cfg.CexConfigs)),
		Bots:  make([]*mm.BotStatus, 0, len(m.cfg.BotConfigs)),
	}
	for _, botCfg := range m.cfg.BotConfigs {
		m.runningBotsMtx.RLock()
		_, running := m.runningBots[mm.MarketWithHost{Host: botCfg.Host, BaseID: botCfg.BaseID, QuoteID: botCfg.QuoteID}]
		m.runningBotsMtx.RUnlock()
		var stats *mm.RunStats
		if running {
			stats = &mm.RunStats{
				InitialBalances: make(map[uint32]uint64),
				DEXBalances: map[uint32]*mm.BotBalance{
					botCfg.BaseID:  {Available: randomBalance()},
					botCfg.QuoteID: {Available: randomBalance()},
				},
				CEXBalances: map[uint32]*mm.BotBalance{
					botCfg.BaseID:  {Available: randomBalance()},
					botCfg.QuoteID: {Available: randomBalance()},
				},
				ProfitLoss:         randomProfitLoss(botCfg.BaseID, botCfg.QuoteID),
				StartTime:          time.Now().Add(-time.Duration(float64(time.Hour*10) * rand.Float64())).Unix(),
				PendingDeposits:    rand.Intn(3),
				PendingWithdrawals: rand.Intn(3),
				CompletedMatches:   uint32(rand.Intn(200)),
				TradedUSD:          rand.Float64() * 10_000,
				FeeGap:             randomFeeGapStats(),
			}
		}
		status.Bots = append(status.Bots, &mm.BotStatus{
			Config:   botCfg,
			Running:  stats != nil,
			RunStats: stats,
		})
	}
	bals := make(map[uint32]*libxc.ExchangeBalance)
	for _, mkt := range binanceMarkets {
		for _, assetID := range []uint32{mkt.BaseID, mkt.QuoteID} {
			if _, found := bals[assetID]; !found {
				bals[assetID] = &libxc.ExchangeBalance{
					Available: randomBalance(),
					Locked:    randomBalance(),
				}
			}
		}
	}
	for _, cexCfg := range m.cfg.CexConfigs {
		status.CEXes[cexCfg.Name] = &mm.CEXStatus{
			Config:    cexCfg,
			Connected: rand.Float32() < 0.5,
			// ConnectionError: "test connection error",
			Markets:  binanceMarkets,
			Balances: bals,
		}
	}
	return status
}

var gapStrategies = []mm.GapStrategy{
	mm.GapStrategyMultiplier,
	mm.GapStrategyAbsolute,
	mm.GapStrategyAbsolutePlus,
	mm.GapStrategyPercent,
	mm.GapStrategyPercentPlus,
}

func randomBotConfig(mkt *mm.MarketWithHost) *mm.BotConfig {
	cfg := &mm.BotConfig{
		Host:    mkt.Host,
		BaseID:  mkt.BaseID,
		QuoteID: mkt.QuoteID,
	}
	newPlacements := func(gapStategy mm.GapStrategy) (lots []uint64, gapFactors []float64) {
		n := rand.Intn(3)
		lots, gapFactors = make([]uint64, 0, n), make([]float64, 0, n)
		maxQty := math.Pow(10, 6+rand.Float64()*6)
		for i := 0; i < n; i++ {
			var gapFactor float64
			switch gapStategy {
			case mm.GapStrategyAbsolute, mm.GapStrategyAbsolutePlus:
				gapFactor = math.Exp(-rand.Float64()*5) * maxQty
			case mm.GapStrategyPercent, mm.GapStrategyPercentPlus:
				gapFactor = 0.01 + rand.Float64()*0.09
			default: // multiplier
				gapFactor = 1 + rand.Float64()
			}
			lots = append(lots, uint64(rand.Intn(100)))
			gapFactors = append(gapFactors, gapFactor)
		}
		return
	}

	typeRoll := rand.Float32()
	switch {
	case typeRoll < 0.33: // basic MM
		gapStrategy := gapStrategies[rand.Intn(len(gapStrategies))]
		basicCfg := &mm.BasicMarketMakingConfig{
			GapStrategy:    gapStrategies[rand.Intn(len(gapStrategies))],
			DriftTolerance: rand.Float64() * 0.01,
		}
		cfg.BasicMMConfig = basicCfg
		lots, gapFactors := newPlacements(gapStrategy)
		for i := 0; i < len(lots); i++ {
			p := &mm.OrderPlacement{Lots: lots[i], GapFactor: gapFactors[i]}
			basicCfg.BuyPlacements = append(basicCfg.BuyPlacements, p)
			basicCfg.SellPlacements = append(basicCfg.SellPlacements, p)
		}
	case typeRoll < 0.67: // arb-mm
		arbMMCfg := &mm.ArbMarketMakerConfig{
			Profit:             rand.Float64()*0.03 + 0.005,
			DriftTolerance:     rand.Float64() * 0.01,
			NumEpochsLeaveOpen: uint64(rand.Intn(100)),
		}
		cfg.ArbMarketMakerConfig = arbMMCfg
		lots, gapFactors := newPlacements(mm.GapStrategyMultiplier)
		for i := 0; i < len(lots); i++ {
			p := &mm.ArbMarketMakingPlacement{Lots: lots[i], Multiplier: gapFactors[i]}
			arbMMCfg.BuyPlacements = append(arbMMCfg.BuyPlacements, p)
			arbMMCfg.SellPlacements = append(arbMMCfg.SellPlacements, p)
		}
	default: // simple-arb
		cfg.SimpleArbConfig = &mm.SimpleArbConfig{
			ProfitTrigger:      rand.Float64()*0.03 + 0.005,
			MaxActiveArbs:      1 + uint32(rand.Intn(100)),
			NumEpochsLeaveOpen: uint32(rand.Intn(100)),
		}
	}
	return cfg
}

func (m *TMarketMaker) RunOverview(startTime int64, mkt *mm.MarketWithHost) (*mm.MarketMakingRunOverview, error) {
	endTime := time.Unix(startTime, 0).Add(time.Hour * 5).Unix()
	run := &mm.MarketMakingRunOverview{
		EndTime: &endTime,
		Cfgs: []*mm.CfgUpdate{
			{
				Cfg:       randomBotConfig(mkt),
				Timestamp: startTime,
			},
		},
		InitialBalances: make(map[uint32]uint64),
		ProfitLoss:      randomProfitLoss(mkt.BaseID, mkt.QuoteID),
	}

	for _, assetID := range []uint32{mkt.BaseID, mkt.QuoteID} {
		run.InitialBalances[assetID] = randomBalance()
		if tkn := asset.TokenInfo(assetID); tkn != nil {
			run.InitialBalances[tkn.ParentID] = randomBalance()
		}
	}

	return run, nil
}

func (m *TMarketMaker) ArchivedRuns() ([]*mm.MarketMakingRun, error) {
	n := rand.Intn(25)
	supportedAssets := m.core.SupportedAssets()
	runs := make([]*mm.MarketMakingRun, 0, n)
	for i := 0; i < n; i++ {
		host := firstDEX
		if rand.Float32() < 0.5 {
			host = secondDEX
		}
		xc := tExchanges[host]
		mkts := make([]*core.Market, 0, len(xc.Markets))
		for _, mkt := range xc.Markets {
			mkts = append(mkts, mkt)
		}
		mkt := mkts[rand.Intn(len(mkts))]
		if supportedAssets[mkt.BaseID] == nil || supportedAssets[mkt.QuoteID] == nil {
			continue
		}
		marketWithHost := &mm.MarketWithHost{
			Host:    host,
			BaseID:  mkt.BaseID,
			QuoteID: mkt.QuoteID,
		}
		runs = append(runs, &mm.MarketMakingRun{
			StartTime: time.Now().Add(-time.Hour * 5 * time.Duration(i)).Unix(),
			Market:    marketWithHost,
		})
	}
	return runs, nil
}

func randomWalletTransaction(txType asset.TransactionType, qty uint64) *asset.WalletTransaction {
	tx := &asset.WalletTransaction{
		Type:      txType,
		ID:        ordertest.RandomOrderID().String(),
		Amount:    qty,
		Fees:      uint64(float64(qty) * 0.01 * rand.Float64()),
		Confirmed: rand.Float32() < 0.05,
	}
	switch txType {
	case asset.Redeem, asset.Receive, asset.SelfSend:
		addr := ordertest.RandomAddress()
		tx.Recipient = &addr
	}
	return tx
}

func (m *TMarketMaker) RunLogs(startTime int64, mkt *mm.MarketWithHost, n uint64, refID *uint64, filters *mm.RunLogFilters) ([]*mm.MarketMakingEvent, []*mm.MarketMakingEvent, *mm.MarketMakingRunOverview, error) {
	if n == 0 {
		n = uint64(rand.Intn(100))
	}
	events := make([]*mm.MarketMakingEvent, 0, n)
	endTime := time.Now().Add(-time.Hour * time.Duration(rand.Intn(1000)))
	mktID := dex.BipIDSymbol(mkt.BaseID) + "_" + dex.BipIDSymbol(mkt.QuoteID)
	midGap, maxQty := getMarketStats(mktID)
	for i := uint64(0); i < n; i++ {
		ev := &mm.MarketMakingEvent{
			ID:        i,
			TimeStamp: endTime.Add(-time.Hour * time.Duration(i)).Unix(),
			BalanceEffects: &mm.BalanceEffects{
				Settled: map[uint32]int64{
					mkt.BaseID:  int64(maxQty * (-0.5 + rand.Float64())),
					mkt.QuoteID: int64(maxQty * (0.5 + rand.Float64())),
				},
				Pending: map[uint32]uint64{
					mkt.BaseID:  uint64(maxQty * (-0.5 + rand.Float64())),
					mkt.QuoteID: uint64(maxQty * (0.5 + rand.Float64())),
				},
				Locked: map[uint32]uint64{
					mkt.BaseID:  uint64(maxQty * (-0.5 + rand.Float64())),
					mkt.QuoteID: uint64(maxQty * (0.5 + rand.Float64())),
				},
				Reserved: map[uint32]uint64{},
			},
			Pending: i < 10 && rand.Float32() < 0.3,
			// DEXOrderEvent   *DEXOrderEvent   `json:"dexOrderEvent,omitempty"`
			// CEXOrderEvent   *CEXOrderEvent   `json:"cexOrderEvent,omitempty"`
			// DepositEvent    *DepositEvent    `json:"depositEvent,omitempty"`
			// WithdrawalEvent *WithdrawalEvent `json:"withdrawalEvent,omitempty"`
		}
		typeRoll := rand.Float32()
		switch {
		case typeRoll < 0.25: // dex order
			sell := rand.Intn(2) > 0
			ord := randomOrder(sell, maxQty, midGap, gapWidthFactor*midGap, false)
			orderEvent := &mm.DEXOrderEvent{
				ID:   ordertest.RandomOrderID().String(),
				Rate: ord.MsgRate,
				Qty:  ord.QtyAtomic,
				Sell: sell,
			}
			ev.DEXOrderEvent = orderEvent
			if rand.Float32() < 0.7 {
				orderEvent.Transactions = append(orderEvent.Transactions, randomWalletTransaction(asset.Swap, ord.QtyAtomic))
				if rand.Float32() < 0.9 {
					orderEvent.Transactions = append(orderEvent.Transactions, randomWalletTransaction(asset.Redeem, ord.QtyAtomic))
				} else {
					orderEvent.Transactions = append(orderEvent.Transactions, randomWalletTransaction(asset.Refund, ord.QtyAtomic))
				}
			}
		case typeRoll < 0.5: // cex order
			sell := rand.Intn(2) > 0
			ord := randomOrder(sell, maxQty, midGap, gapWidthFactor*midGap, false)
			ev.CEXOrderEvent = &mm.CEXOrderEvent{
				ID:   ordertest.RandomOrderID().String(),
				Rate: ord.MsgRate,
				Qty:  ord.QtyAtomic,
				Sell: sell,
			}
		case typeRoll < 0.75: // deposit
			assetID := mkt.BaseID
			if rand.Float32() < 0.5 {
				assetID = mkt.QuoteID
			}
			amt := uint64(maxQty * 0.2 * rand.Float64())
			ev.DepositEvent = &mm.DepositEvent{
				Transaction: randomWalletTransaction(asset.Send, amt),
				AssetID:     assetID,
				CEXCredit:   amt,
			}
		default: // withdrawal
			assetID := mkt.BaseID
			if rand.Float32() < 0.5 {
				assetID = mkt.QuoteID
			}
			amt := uint64(maxQty * 0.2 * rand.Float64())
			ev.WithdrawalEvent = &mm.WithdrawalEvent{
				Transaction: randomWalletTransaction(asset.Receive, amt),
				AssetID:     assetID,
				CEXDebit:    amt,
			}
		}
		events = append(events, ev)
	}

	overview, err := m.RunOverview(startTime, mkt)
	if err != nil {
		return nil, nil, nil, err
	}

	return events, nil, overview, nil
}

func (m *TMarketMaker) CEXBook(host string, baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	mktID := dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID)
	book := m.core.book(host, mktID)
	return book.Buys, book.Sells, nil
}

func makeRequiredAction(assetID uint32, actionID string) *asset.ActionRequiredNote {
	txID := dex.Bytes(encode.RandomBytes(32)).String()
	var payload any
	if actionID == core.ActionIDRedeemRejected {
		payload = core.RejectedTxData{
			AssetID: assetID,
			CoinID:  encode.RandomBytes(32),
			CoinFmt: "0x8909ec4aa707df569e62e2f8e2040094e2c88fe192b3b3e2dadfa383a41aa645",
			TxType:  "redeem",
		}
	} else {
		payload = &eth.TransactionActionNote{
			Tx:      randomWalletTransaction(asset.TransactionType(1+rand.Intn(15)), randomBalance()/10), // 1 to 15
			Nonce:   uint64(rand.Float64() * 500),
			NewFees: uint64(rand.Float64() * math.Pow10(rand.Intn(8))),
		}
	}
	n := &asset.ActionRequiredNote{
		ActionID: actionID,
		UniqueID: txID,
		Payload:  payload,
	}
	n.AssetID = assetID
	n.Route = "actionRequired"
	return n
}

func makeActionResolved(assetID uint32, uniqueID string) *asset.ActionResolvedNote {
	n := &asset.ActionResolvedNote{
		UniqueID: uniqueID,
	}
	n.AssetID = assetID
	n.Route = "actionResolved"
	return n
}

func TestServer(t *testing.T) {
	// Register dummy drivers for unimplemented assets.
	asset.Register(22, &TDriver{})                 // mona
	asset.Register(28, &TDriver{})                 // vtc
	asset.Register(unsupportedAssetID, &TDriver{}) // kmd
	asset.Register(3, &TDriver{})                  // doge

	tinfos = map[uint32]*asset.Token{
		60001:  asset.TokenInfo(60001),
		966001: asset.TokenInfo(966001),
	}

	numBuys = 10
	numSells = 10
	feedPeriod = 5000 * time.Millisecond
	initialize := false
	register := false
	forceDisconnectWallet = true
	gapWidthFactor = 0.2
	randomPokes = false
	randomNotes = false
	numUserOrders = 40
	delayBalance = true
	doubleCreateAsyncErr = false
	randomizeOrdersCount = true

	if enableActions {
		actions = []*asset.ActionRequiredNote{
			makeRequiredAction(0, "missingNonces"),
			makeRequiredAction(42, "lostNonce"),
			makeRequiredAction(60, "tooCheap"),
			makeRequiredAction(60, "redeemRejected"),
		}
	}

	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := dex.StdOutLogger("TEST", dex.LevelTrace)
	tCore := newTCore()

	if initialize {
		tCore.InitializeClient([]byte(""), nil)
	}

	if register {
		// initialize is implied and forced if register = true.
		if !initialize {
			tCore.InitializeClient([]byte(""), nil)
		}
		var assetID uint32 = 42
		tCore.PostBond(&core.PostBondForm{Addr: firstDEX, Bond: 1, Asset: &assetID})
	}

	s, err := New(&Config{
		Core: tCore,
		MarketMaker: &TMarketMaker{
			core:        tCore,
			cfg:         &mm.MarketMakingConfig{},
			runningBots: make(map[mm.MarketWithHost]int64),
		},
		Addr:     "127.0.0.3:54321",
		Logger:   logger,
		NoEmbed:  true, // use files on disk, and reload on each page load
		HttpProf: true,
	})
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	cm := dex.NewConnectionMaster(s)
	err = cm.Connect(tCtx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	go tCore.runEpochs()
	if randomPokes {
		go tCore.runRandomPokes()
	}

	if randomNotes {
		go tCore.runRandomNotes()
	}

	cm.Wait()
}
