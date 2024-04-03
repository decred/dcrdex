package mm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"

	_ "decred.org/dcrdex/client/asset/btc"     // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr"     // register dcr asset
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon asset
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type tBookFeed struct {
	c chan *core.BookUpdate
}

func (t *tBookFeed) Next() <-chan *core.BookUpdate { return t.c }
func (t *tBookFeed) Close()                        {}
func (t *tBookFeed) Candles(dur string) error      { return nil }

var _ core.BookFeed = (*tBookFeed)(nil)

type tCoin struct {
	coinID []byte
	value  uint64
}

var _ asset.Coin = (*tCoin)(nil)

func (c *tCoin) ID() dex.Bytes {
	return c.coinID
}
func (c *tCoin) String() string {
	return hex.EncodeToString(c.coinID)
}
func (c *tCoin) Value() uint64 {
	return c.value
}
func (c *tCoin) TxID() string {
	return hex.EncodeToString(c.coinID)
}

type sendArgs struct {
	assetID  uint32
	value    uint64
	address  string
	subtract bool
}

type tCore struct {
	assetBalances     map[uint32]*core.WalletBalance
	assetBalanceErr   error
	market            *core.Market
	singleLotSellFees *orderFees
	singleLotBuyFees  *orderFees
	singleLotFeesErr  error
	multiTradeResult  []*core.Order
	noteFeed          chan core.Notification
	isAccountLocker   map[uint32]bool
	isWithdrawer      map[uint32]bool
	isDynamicSwapper  map[uint32]bool
	cancelsPlaced     []dex.Bytes
	buysPlaced        []*core.TradeForm
	sellsPlaced       []*core.TradeForm
	multiTradesPlaced []*core.MultiTradeForm
	maxFundingFees    uint64
	book              *orderbook.OrderBook
	bookFeed          *tBookFeed
	lastSendArgs      *sendArgs
	sendCoin          *tCoin
	newDepositAddress string
	orders            map[order.OrderID]*core.Order
	walletTxsMtx      sync.Mutex
	walletTxs         map[string]*asset.WalletTransaction
	fiatRates         map[uint32]float64
}

func newTCore() *tCore {
	return &tCore{
		assetBalances:    make(map[uint32]*core.WalletBalance),
		noteFeed:         make(chan core.Notification),
		isAccountLocker:  make(map[uint32]bool),
		isWithdrawer:     make(map[uint32]bool),
		isDynamicSwapper: make(map[uint32]bool),
		cancelsPlaced:    make([]dex.Bytes, 0),
		bookFeed: &tBookFeed{
			c: make(chan *core.BookUpdate, 1),
		},
		walletTxs: make(map[string]*asset.WalletTransaction),
	}
}

var _ clientCore = (*tCore)(nil)

func (c *tCore) NotificationFeed() *core.NoteFeed {
	return &core.NoteFeed{C: c.noteFeed}
}
func (c *tCore) ExchangeMarket(host string, base, quote uint32) (*core.Market, error) {
	return c.market, nil
}

func (t *tCore) SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error) {
	return t.book, t.bookFeed, nil
}
func (*tCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	return nil
}
func (c *tCore) SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, uint64, error) {
	if c.singleLotFeesErr != nil {
		return 0, 0, 0, c.singleLotFeesErr
	}
	if c.singleLotSellFees == nil && c.singleLotBuyFees == nil {
		return 0, 0, 0, fmt.Errorf("no fees set")
	}

	if form.Sell {
		return c.singleLotSellFees.swap, c.singleLotSellFees.redemption, c.singleLotSellFees.refund, nil
	}
	return c.singleLotBuyFees.swap, c.singleLotBuyFees.redemption, c.singleLotBuyFees.refund, nil
}
func (c *tCore) Cancel(oidB dex.Bytes) error {
	c.cancelsPlaced = append(c.cancelsPlaced, oidB)
	return nil
}
func (c *tCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	return c.assetBalances[assetID], c.assetBalanceErr
}
func (c *tCore) MultiTrade(pw []byte, forms *core.MultiTradeForm) ([]*core.Order, error) {
	c.multiTradesPlaced = append(c.multiTradesPlaced, forms)
	return c.multiTradeResult, nil
}
func (c *tCore) WalletState(assetID uint32) *core.WalletState {
	isAccountLocker := c.isAccountLocker[assetID]
	isWithdrawer := c.isWithdrawer[assetID]
	isDynamicSwapper := c.isDynamicSwapper[assetID]

	var traits asset.WalletTrait
	if isAccountLocker {
		traits |= asset.WalletTraitAccountLocker
	}
	if isWithdrawer {
		traits |= asset.WalletTraitWithdrawer
	}
	if isDynamicSwapper {
		traits |= asset.WalletTraitDynamicSwapper
	}

	return &core.WalletState{
		Traits: traits,
	}
}
func (c *tCore) MaxFundingFees(fromAsset uint32, host string, numTrades uint32, options map[string]string) (uint64, error) {
	return c.maxFundingFees, nil
}
func (c *tCore) Login(pw []byte) error {
	return nil
}
func (c *tCore) OpenWallet(assetID uint32, pw []byte) error {
	return nil
}
func (c *tCore) User() *core.User {
	return nil
}
func (c *tCore) WalletTransaction(assetID uint32, txID string) (*asset.WalletTransaction, error) {
	c.walletTxsMtx.Lock()
	defer c.walletTxsMtx.Unlock()
	return c.walletTxs[txID], nil
}

func (c *tCore) Network() dex.Network {
	return dex.Simnet
}

func (c *tCore) FiatConversionRates() map[uint32]float64 {
	return c.fiatRates
}
func (c *tCore) Broadcast(core.Notification) {

}

func (c *tCore) Send(pw []byte, assetID uint32, value uint64, address string, subtract bool) (asset.Coin, error) {
	c.lastSendArgs = &sendArgs{
		assetID:  assetID,
		value:    value,
		address:  address,
		subtract: subtract,
	}
	return c.sendCoin, nil
}
func (c *tCore) NewDepositAddress(assetID uint32) (string, error) {
	return c.newDepositAddress, nil
}
func (c *tCore) Order(id dex.Bytes) (*core.Order, error) {
	var oid order.OrderID
	copy(oid[:], id)
	if o, found := c.orders[oid]; found {
		return o, nil
	}
	return nil, fmt.Errorf("order %s not found", id)
}

func (c *tCore) setAssetBalances(balances map[uint32]uint64) {
	c.assetBalances = make(map[uint32]*core.WalletBalance)
	for assetID, bal := range balances {
		c.assetBalances[assetID] = &core.WalletBalance{
			Balance: &db.Balance{
				Balance: asset.Balance{
					Available: bal,
				},
			},
		}
	}
}

type dexOrder struct {
	rate uint64
	qty  uint64
	sell bool
}

type tBotCoreAdaptor struct {
	clientCore
	tCore *tCore

	balances            map[uint32]*BotBalance
	groupedBuys         map[uint64][]*core.Order
	groupedSells        map[uint64][]*core.Order
	orderUpdates        chan *core.Order
	buyFees             *orderFees
	sellFees            *orderFees
	fiatExchangeRate    uint64
	buyFeesInBase       uint64
	sellFeesInBase      uint64
	buyFeesInQuote      uint64
	sellFeesInQuote     uint64
	lastMultiTradeSells []*multiTradePlacement
	lastMultiTradeBuys  []*multiTradePlacement
	multiTradeResults   [][]*core.Order
	sellsDEXReserves    map[uint32]uint64
	sellsCEXReserves    map[uint32]uint64
	buysDEXReserves     map[uint32]uint64
	buysCEXReserves     map[uint32]uint64
	maxBuyQty           uint64
	maxSellQty          uint64
	lastTradePlaced     *dexOrder
	tradeResult         *core.Order
}

func (c *tBotCoreAdaptor) DEXBalance(assetID uint32) (*BotBalance, error) {
	if c.tCore.assetBalanceErr != nil {
		return nil, c.tCore.assetBalanceErr
	}
	return c.balances[assetID], nil
}

func (c *tBotCoreAdaptor) GroupedBookedOrders() (buys, sells map[uint64][]*core.Order) {
	return c.groupedBuys, c.groupedSells
}

func (c *tBotCoreAdaptor) CancelAllOrders() bool { return false }

func (c *tBotCoreAdaptor) ExchangeRateFromFiatSources() uint64 {
	return c.fiatExchangeRate
}

func (c *tBotCoreAdaptor) OrderFees() (buyFees, sellFees *orderFees, err error) {
	return c.buyFees, c.sellFees, nil
}

func (c *tBotCoreAdaptor) SubscribeOrderUpdates() (updates <-chan *core.Order) {
	return c.orderUpdates
}

func (c *tBotCoreAdaptor) OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error) {
	if sell && base {
		return c.sellFeesInBase, nil
	}
	if sell && !base {
		return c.sellFeesInQuote, nil
	}
	if !sell && base {
		return c.buyFeesInBase, nil
	}
	return c.buyFeesInQuote, nil
}

func (c *tBotCoreAdaptor) SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, error) {
	if sell {
		return qty <= c.maxSellQty, nil
	}
	return qty <= c.maxBuyQty, nil
}

func (c *tBotCoreAdaptor) MultiTrade(placements []*multiTradePlacement, sell bool, driftTolerance float64, currEpoch uint64, dexReserves, cexReserves map[uint32]uint64) []*order.OrderID {
	if sell {
		c.lastMultiTradeSells = placements
		for assetID, reserve := range cexReserves {
			c.sellsCEXReserves[assetID] = reserve
		}
		for assetID, reserve := range dexReserves {
			c.sellsDEXReserves[assetID] = reserve
		}
	} else {
		c.lastMultiTradeBuys = placements
		for assetID, reserve := range cexReserves {
			c.buysCEXReserves[assetID] = reserve
		}
		for assetID, reserve := range dexReserves {
			c.buysDEXReserves[assetID] = reserve
		}
	}
	return nil
}

func (c *tBotCoreAdaptor) DEXTrade(rate, qty uint64, sell bool) (*core.Order, error) {
	c.lastTradePlaced = &dexOrder{
		rate: rate,
		qty:  qty,
		sell: sell,
	}
	return c.tradeResult, nil
}

func newTBotCoreAdaptor(c *tCore) *tBotCoreAdaptor {
	return &tBotCoreAdaptor{
		clientCore:        c,
		tCore:             c,
		orderUpdates:      make(chan *core.Order),
		multiTradeResults: make([][]*core.Order, 0),
		buysCEXReserves:   make(map[uint32]uint64),
		buysDEXReserves:   make(map[uint32]uint64),
		sellsCEXReserves:  make(map[uint32]uint64),
		sellsDEXReserves:  make(map[uint32]uint64),
	}
}

var _ botCoreAdaptor = (*tBotCoreAdaptor)(nil)

type tOrderBook struct {
	midGap    uint64
	midGapErr error

	bidsVWAP map[uint64]vwapResult
	asksVWAP map[uint64]vwapResult
	vwapErr  error
}

var _ dexOrderBook = (*tOrderBook)(nil)

func (t *tOrderBook) VWAP(numLots, _ uint64, sell bool) (avg, extrema uint64, filled bool, err error) {
	if t.vwapErr != nil {
		return 0, 0, false, t.vwapErr
	}

	if sell {
		res, found := t.asksVWAP[numLots]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := t.bidsVWAP[numLots]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}

func (o *tOrderBook) MidGap() (uint64, error) {
	if o.midGapErr != nil {
		return 0, o.midGapErr
	}
	return o.midGap, nil
}

type tOracle struct {
	marketPrice float64
}

func (o *tOracle) getMarketPrice(base, quote uint32) float64 {
	return o.marketPrice
}

func TestInitialBaseBalances(t *testing.T) {
	dcrBtcID := fmt.Sprintf("%s-%d-%d", "host1", 42, 0)
	dcrEthID := fmt.Sprintf("%s-%d-%d", "host1", 42, 60)

	type ttest struct {
		name string
		cfgs []*BotConfig

		assetBalances map[uint32]uint64
		cexBalances   map[string]map[uint32]uint64

		wantReserves    map[string]map[uint32]uint64
		wantCEXReserves map[string]map[uint32]uint64
		wantErr         bool
	}
	tests := []*ttest{
		// "percentages only, ok"
		{
			name: "percentages only, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 500,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},
		},
		// "50% + 51% error"
		{
			name: "50% + 51% error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      51,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantErr: true,
		},
		// "combine amount and percentages, ok"
		{
			name: "combine amount and percentages, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Amount,
					BaseBalance:      499,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 499,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},
		},
		// "combine amount and percentages, too high error"
		{
			name: "combine amount and percentages, too high error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Amount,
					BaseBalance:      501,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantErr: true,
		},
		// "CEX percentages only, ok"
		{
			name: "CEX percentages only, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
					CEXCfg: &BotCEXCfg{
						Name:             "Kraken",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     100,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					42: 2000,
					0:  3000,
				},
				"Kraken": {
					42: 4000,
					60: 2000,
				},
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 500,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},

			wantCEXReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  1500,
					42: 1000,
				},
				dcrEthID: {
					42: 2000,
					60: 2000,
				},
			},
		},
		// "CEX 50% + 51% error"
		{
			name: "CEX 50% + 51% error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      51,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					42: 2000,
					60: 1000,
					0:  3000,
				},
			},

			wantErr: true,
		},
		// "CEX combine amount and percentages, ok"
		{
			name: "CEX combine amount and percentages, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Amount,
						BaseBalance:      600,
						QuoteBalanceType: Percentage,
						QuoteBalance:     100,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					42: 2000,
					0:  3000,
					60: 2000,
				},
				"Kraken": {
					42: 4000,
					60: 2000,
				},
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 500,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},

			wantCEXReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  1500,
					42: 1000,
				},
				dcrEthID: {
					42: 600,
					60: 2000,
				},
			},
		},
		// "CEX combine amount and percentages"
		{
			name: "CEX combine amount and percentages, too high error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:             "host1",
					BaseID:           42,
					QuoteID:          60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Amount,
						BaseBalance:      1501,
						QuoteBalanceType: Percentage,
						QuoteBalance:     100,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					42: 2000,
					0:  3000,
					60: 2000,
				},
				"Kraken": {
					42: 4000,
					60: 2000,
				},
			},

			wantErr: true,
		},
		// "CEX same asset on different chains"
		{
			name: "CEX same asset on different chains",
			cfgs: []*BotConfig{
				{
					Host:                    "host1",
					BaseID:                  60001,
					QuoteID:                 0,
					BaseBalanceType:         Percentage,
					BaseBalance:             50,
					QuoteBalanceType:        Percentage,
					QuoteBalance:            50,
					BaseFeeAssetBalanceType: Amount,
					BaseFeeAssetBalance:     500,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:                     "host1",
					BaseID:                   60,
					QuoteID:                  966001,
					BaseBalanceType:          Percentage,
					BaseBalance:              50,
					QuoteBalanceType:         Percentage,
					QuoteBalance:             50,
					QuoteFeeAssetBalanceType: Amount,
					QuoteFeeAssetBalance:     500,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:      1000,
				60:     2000,
				966:    1000,
				60001:  2000,
				966001: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					0:      3000,
					60:     2000,
					60001:  2000,
					966001: 2000,
					61001:  2000,
				},
			},

			wantReserves: map[string]map[uint32]uint64{
				dexMarketID("host1", 60001, 0): {
					60001: 1000,
					0:     500,
					60:    500,
				},
				dexMarketID("host1", 60, 966001): {
					966001: 1000,
					966:    500,
					60:     1000,
				},
			},

			wantCEXReserves: map[string]map[uint32]uint64{
				dexMarketID("host1", 60001, 0): {
					60001: 1000,
					0:     1500,
				},
				dexMarketID("host1", 60, 966001): {
					966001: 1000,
					60:     1000,
				},
			},
		},
		// "CEX same asset on different chains, too high error"
		{
			name: "CEX same asset on different chains, too high error",
			cfgs: []*BotConfig{
				{
					Host:                    "host1",
					BaseID:                  60001,
					QuoteID:                 0,
					BaseBalanceType:         Percentage,
					BaseBalance:             50,
					QuoteBalanceType:        Percentage,
					QuoteBalance:            50,
					BaseFeeAssetBalanceType: Amount,
					BaseFeeAssetBalance:     1,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:                    "host1",
					BaseID:                  966001,
					QuoteID:                 60,
					BaseBalanceType:         Percentage,
					BaseBalance:             50,
					QuoteBalanceType:        Percentage,
					QuoteBalance:            100,
					BaseFeeAssetBalanceType: Amount,
					BaseFeeAssetBalance:     1,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      51,
						QuoteBalanceType: Percentage,
						QuoteBalance:     100,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:      1000,
				60:     2000,
				966:    1000,
				60001:  2000,
				966001: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					0:      3000,
					60:     2000,
					60001:  2000,
					966001: 2000,
					61001:  2000,
				},
			},

			wantErr: true,
		},
		// "No base fee asset specified, error"
		{
			name: "No base fee asset specified, error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseID:           60001,
					QuoteID:          0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:                     "host1",
					BaseID:                   60,
					QuoteID:                  966001,
					BaseBalanceType:          Percentage,
					BaseBalance:              50,
					QuoteBalanceType:         Percentage,
					QuoteBalance:             50,
					QuoteFeeAssetBalanceType: Amount,
					QuoteFeeAssetBalance:     500,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:      1000,
				60:     2000,
				966:    1000,
				60001:  2000,
				966001: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					0:      3000,
					60:     2000,
					60001:  2000,
					966001: 2000,
					61001:  2000,
				},
			},

			wantErr: true,
		},
		// "No quote fee asset specified, error"
		{
			name: "No quote fee asset specified, error",
			cfgs: []*BotConfig{
				{
					Host:                    "host1",
					BaseID:                  60001,
					QuoteID:                 0,
					BaseBalanceType:         Percentage,
					BaseBalance:             50,
					QuoteBalanceType:        Percentage,
					QuoteBalance:            50,
					BaseFeeAssetBalanceType: Amount,
					BaseFeeAssetBalance:     500,
					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:             "host1",
					BaseID:           60,
					QuoteID:          966001,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:      1000,
				60:     2000,
				966:    1000,
				60001:  2000,
				966001: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					0:      3000,
					60:     2000,
					60001:  2000,
					966001: 2000,
					61001:  2000,
				},
			},

			wantErr: true,
		},
		// "Token asset insufficient balance, error"
		{
			name: "Token asset insufficient balance, error",
			cfgs: []*BotConfig{
				{
					Host:                    "host1",
					BaseID:                  60001,
					QuoteID:                 0,
					BaseBalanceType:         Percentage,
					BaseBalance:             50,
					QuoteBalanceType:        Percentage,
					QuoteBalance:            50,
					BaseFeeAssetBalanceType: Percentage,
					BaseFeeAssetBalance:     51,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
				{
					Host:                     "host1",
					BaseID:                   60,
					QuoteID:                  966001,
					BaseBalanceType:          Percentage,
					BaseBalance:              50,
					QuoteBalanceType:         Percentage,
					QuoteBalance:             50,
					QuoteFeeAssetBalanceType: Amount,
					QuoteFeeAssetBalance:     500,

					CEXCfg: &BotCEXCfg{
						Name:             "Binance",
						BaseBalanceType:  Percentage,
						BaseBalance:      50,
						QuoteBalanceType: Percentage,
						QuoteBalance:     50,
					},
				},
			},

			assetBalances: map[uint32]uint64{
				0:      1000,
				60:     2000,
				966:    1000,
				60001:  2000,
				966001: 2000,
			},

			cexBalances: map[string]map[uint32]uint64{
				"Binance": {
					0:      3000,
					60:     2000,
					60001:  2000,
					966001: 2000,
					61001:  2000,
				},
			},

			wantErr: true,
		},
	}

	runTest := func(test *ttest) {
		tCore := newTCore()
		tCore.setAssetBalances(test.assetBalances)

		cexes := make(map[string]*centralizedExchange)
		for cexName, balances := range test.cexBalances {
			cex := newTCEX()
			cexes[cexName] = &centralizedExchange{CEX: cex}
			cex.balances = make(map[uint32]*libxc.ExchangeBalance)
			for assetID, balance := range balances {
				cex.balances[assetID] = &libxc.ExchangeBalance{
					Available: balance,
				}
			}
		}

		dexBalances, cexBalances, err := botInitialBaseBalances(test.cfgs, tCore, cexes)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", test.name)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		for botID, wantReserve := range test.wantReserves {

			botDexBalances := dexBalances[botID]
			for assetID, wantReserve := range wantReserve {
				if botDexBalances[assetID] != wantReserve {
					t.Fatalf("%s: unexpected reserve for bot %s, asset %d. "+
						"want %d, got %d", test.name, botID, assetID, wantReserve,
						botDexBalances[assetID])
				}
			}

			wantCEXReserves := test.wantCEXReserves[botID]
			cexBalances := cexBalances[botID]
			for assetID, wantReserve := range wantCEXReserves {
				if cexBalances[assetID] != wantReserve {
					t.Fatalf("%s: unexpected cex reserve for bot %s, asset %d. "+
						"want %d, got %d", test.name, botID, assetID, wantReserve,
						cexBalances[assetID])
				}
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

type vwapResult struct {
	avg     uint64
	extrema uint64
}

type withdrawArgs struct {
	address string
	amt     uint64
	assetID uint32
	txID    string
}

type tCEX struct {
	bidsVWAP                  map[uint64]vwapResult
	asksVWAP                  map[uint64]vwapResult
	vwapErr                   error
	balances                  map[uint32]*libxc.ExchangeBalance
	balanceErr                error
	tradeID                   string
	tradeErr                  error
	lastTrade                 *libxc.Trade
	cancelledTrades           []string
	cancelTradeErr            error
	tradeUpdates              chan *libxc.Trade
	tradeUpdatesID            int
	lastConfirmDepositTx      string
	confirmDeposit            chan uint64
	confirmDepositComplete    chan bool
	depositAddress            string
	lastWithdrawArgs          *withdrawArgs
	confirmWithdrawal         chan *withdrawArgs
	confirmWithdrawalComplete chan bool
}

func newTCEX() *tCEX {
	return &tCEX{
		bidsVWAP:                  make(map[uint64]vwapResult),
		asksVWAP:                  make(map[uint64]vwapResult),
		balances:                  make(map[uint32]*libxc.ExchangeBalance),
		cancelledTrades:           make([]string, 0),
		tradeUpdates:              make(chan *libxc.Trade),
		confirmDeposit:            make(chan uint64),
		confirmDepositComplete:    make(chan bool),
		confirmWithdrawal:         make(chan *withdrawArgs),
		confirmWithdrawalComplete: make(chan bool),
	}
}

var _ libxc.CEX = (*tCEX)(nil)

func (c *tCEX) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return nil, nil
}
func (c *tCEX) Balances() (map[uint32]*libxc.ExchangeBalance, error) {
	return nil, nil
}
func (c *tCEX) Markets(ctx context.Context) ([]*libxc.Market, error) {
	return nil, nil
}
func (c *tCEX) Balance(assetID uint32) (*libxc.ExchangeBalance, error) {
	return c.balances[assetID], c.balanceErr
}
func (c *tCEX) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, updaterID int) (*libxc.Trade, error) {
	if c.tradeErr != nil {
		return nil, c.tradeErr
	}
	c.lastTrade = &libxc.Trade{
		ID:      c.tradeID,
		BaseID:  baseID,
		QuoteID: quoteID,
		Rate:    rate,
		Sell:    sell,
		Qty:     qty,
	}
	return c.lastTrade, nil
}
func (c *tCEX) CancelTrade(ctx context.Context, seID, quoteID uint32, tradeID string) error {
	if c.cancelTradeErr != nil {
		return c.cancelTradeErr
	}
	c.cancelledTrades = append(c.cancelledTrades, tradeID)
	return nil
}
func (c *tCEX) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	return nil
}
func (c *tCEX) UnsubscribeMarket(baseID, quoteID uint32) error {
	return nil
}
func (c *tCEX) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if c.vwapErr != nil {
		return 0, 0, false, c.vwapErr
	}

	if sell {
		res, found := c.asksVWAP[qty]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := c.bidsVWAP[qty]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}
func (c *tCEX) SubscribeTradeUpdates() (<-chan *libxc.Trade, func(), int) {
	return c.tradeUpdates, func() {}, c.tradeUpdatesID
}
func (c *tCEX) SubscribeCEXUpdates() (<-chan interface{}, func()) {
	return nil, func() {}
}
func (c *tCEX) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	return c.depositAddress, nil
}

func (c *tCEX) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string, onComplete func(string)) (string, error) {
	c.lastWithdrawArgs = &withdrawArgs{
		address: address,
		amt:     qty,
		assetID: assetID,
	}

	go func() {
		withdrawal := <-c.confirmWithdrawal
		onComplete(withdrawal.txID)
		c.confirmWithdrawalComplete <- true
	}()

	return "", nil
}

func (c *tCEX) ConfirmDeposit(ctx context.Context, txID string, onConfirm func(uint64)) {
	c.lastConfirmDepositTx = txID

	go func() {
		confirmDepositAmt := <-c.confirmDeposit
		onConfirm(confirmDepositAmt)
		c.confirmDepositComplete <- true
	}()
}

type prepareRebalanceResult struct {
	rebalance   int64
	cexReserves uint64
	dexReserves uint64
}

type tBotCexAdaptor struct {
	bidsVWAP                map[uint64]*vwapResult
	asksVWAP                map[uint64]*vwapResult
	vwapErr                 error
	balances                map[uint32]*BotBalance
	balanceErr              error
	tradeID                 string
	tradeErr                error
	lastTrade               *libxc.Trade
	cancelledTrades         []string
	cancelTradeErr          error
	tradeUpdates            chan *libxc.Trade
	lastWithdrawArgs        *withdrawArgs
	lastDepositArgs         *withdrawArgs
	prepareRebalanceResults map[uint32]*prepareRebalanceResult
	maxBuyQty               uint64
	maxSellQty              uint64
}

func newTBotCEXAdaptor() *tBotCexAdaptor {
	return &tBotCexAdaptor{
		bidsVWAP:                make(map[uint64]*vwapResult),
		asksVWAP:                make(map[uint64]*vwapResult),
		balances:                make(map[uint32]*BotBalance),
		cancelledTrades:         make([]string, 0),
		tradeUpdates:            make(chan *libxc.Trade),
		prepareRebalanceResults: make(map[uint32]*prepareRebalanceResult),
	}
}

var _ botCexAdaptor = (*tBotCexAdaptor)(nil)

var tLogger = dex.StdOutLogger("mm_TEST", dex.LevelTrace)

func (c *tBotCexAdaptor) CEXBalance(assetID uint32) (*BotBalance, error) {
	return c.balances[assetID], c.balanceErr
}
func (c *tBotCexAdaptor) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	if c.cancelTradeErr != nil {
		return c.cancelTradeErr
	}
	c.cancelledTrades = append(c.cancelledTrades, tradeID)
	return nil
}
func (c *tBotCexAdaptor) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	return nil
}
func (c *tBotCexAdaptor) SubscribeTradeUpdates() (updates <-chan *libxc.Trade, unsubscribe func()) {
	return c.tradeUpdates, func() {}
}
func (c *tBotCexAdaptor) CEXTrade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error) {
	if c.tradeErr != nil {
		return nil, c.tradeErr
	}

	c.lastTrade = &libxc.Trade{
		ID:      c.tradeID,
		BaseID:  baseID,
		QuoteID: quoteID,
		Rate:    rate,
		Sell:    sell,
		Qty:     qty,
	}
	return c.lastTrade, nil
}
func (c *tBotCexAdaptor) FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64) {
}

func (c *tBotCexAdaptor) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if c.vwapErr != nil {
		return 0, 0, false, c.vwapErr
	}

	if sell {
		res, found := c.asksVWAP[qty]
		if !found {
			return 0, 0, false, nil
		}
		return res.avg, res.extrema, true, nil
	}

	res, found := c.bidsVWAP[qty]
	if !found {
		return 0, 0, false, nil
	}
	return res.avg, res.extrema, true, nil
}
func (c *tBotCexAdaptor) Deposit(ctx context.Context, assetID uint32, amount uint64) error {
	c.lastDepositArgs = &withdrawArgs{
		assetID: assetID,
		amt:     amount,
	}
	return nil
}
func (c *tBotCexAdaptor) Withdraw(ctx context.Context, assetID uint32, amount uint64) error {
	c.lastWithdrawArgs = &withdrawArgs{
		assetID: assetID,
		amt:     amount,
	}
	return nil
}
func (c *tBotCexAdaptor) SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error) {
	if sell {
		return qty <= c.maxSellQty, nil
	}
	return qty <= c.maxBuyQty, nil
}

func (c *tBotCexAdaptor) PrepareRebalance(ctx context.Context, assetID uint32) (rebalance int64, dexReserves, cexReserves uint64) {
	res := c.prepareRebalanceResults[assetID]
	return res.rebalance, res.dexReserves, res.cexReserves
}
