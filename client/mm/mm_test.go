package mm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
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
	cancelsPlaced     []order.OrderID
	multiTradesPlaced []*core.MultiTradeForm
	maxFundingFees    uint64
	book              *orderbook.OrderBook
	bookFeed          *tBookFeed
	sends             []*sendArgs
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
		cancelsPlaced:    make([]order.OrderID, 0),
		bookFeed: &tBookFeed{
			c: make(chan *core.BookUpdate, 1),
		},
		walletTxs: make(map[string]*asset.WalletTransaction),
		book:      &orderbook.OrderBook{},
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
		return c.singleLotSellFees.Max.Swap, c.singleLotSellFees.Max.Redeem, c.singleLotSellFees.Max.Refund, nil
	}
	return c.singleLotBuyFees.Max.Swap, c.singleLotBuyFees.Max.Redeem, c.singleLotBuyFees.Max.Refund, nil
}
func (c *tCore) Cancel(oidB dex.Bytes) error {
	var oid order.OrderID
	copy(oid[:], oidB)
	c.cancelsPlaced = append(c.cancelsPlaced, oid)
	return nil
}
func (c *tCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	return c.assetBalances[assetID], c.assetBalanceErr
}
func (c *tCore) MultiTrade(pw []byte, forms *core.MultiTradeForm) ([]*core.Order, error) {
	c.multiTradesPlaced = append(c.multiTradesPlaced, forms)
	return c.multiTradeResult, nil
}
func (c *tCore) WalletTraits(assetID uint32) (asset.WalletTrait, error) {
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

	return traits, nil
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
	c.sends = append(c.sends, &sendArgs{
		assetID:  assetID,
		value:    value,
		address:  address,
		subtract: subtract,
	})
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

	balances         map[uint32]*BotBalance
	groupedBuys      map[uint64][]*core.Order
	groupedSells     map[uint64][]*core.Order
	orderUpdates     chan *core.Order
	buyFees          *orderFees
	sellFees         *orderFees
	fiatExchangeRate uint64
	buyFeesInBase    uint64
	sellFeesInBase   uint64
	buyFeesInQuote   uint64
	sellFeesInQuote  uint64
	maxBuyQty        uint64
	maxSellQty       uint64
	lastTradePlaced  *dexOrder
	tradeResult      *core.Order
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

func (c *tBotCoreAdaptor) DEXTrade(rate, qty uint64, sell bool) (*core.Order, error) {
	c.lastTradePlaced = &dexOrder{
		rate: rate,
		qty:  qty,
		sell: sell,
	}
	return c.tradeResult, nil
}

func (u *tBotCoreAdaptor) registerFeeGap(s *FeeGapStats) {}

func newTBotCoreAdaptor(c *tCore) *tBotCoreAdaptor {
	return &tBotCoreAdaptor{
		clientCore:   c,
		tCore:        c,
		orderUpdates: make(chan *core.Order),
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
	bidsVWAP             map[uint64]vwapResult
	asksVWAP             map[uint64]vwapResult
	vwapErr              error
	balances             map[uint32]*libxc.ExchangeBalance
	balanceErr           error
	tradeID              string
	tradeErr             error
	lastTrade            *libxc.Trade
	cancelledTrades      []string
	cancelTradeErr       error
	tradeUpdates         chan *libxc.Trade
	tradeUpdatesID       int
	depositAddress       string
	withdrawals          []*withdrawArgs
	confirmWithdrawalMtx sync.Mutex
	confirmWithdrawal    *withdrawArgs
	withdrawalID         string
	confirmDepositMtx    sync.Mutex
	confirmedDeposit     *uint64
	tradeStatus          *libxc.Trade
}

func newTCEX() *tCEX {
	return &tCEX{
		bidsVWAP:        make(map[uint64]vwapResult),
		asksVWAP:        make(map[uint64]vwapResult),
		balances:        make(map[uint32]*libxc.ExchangeBalance),
		cancelledTrades: make([]string, 0),
		tradeUpdates:    make(chan *libxc.Trade),
	}
}

var _ libxc.CEX = (*tCEX)(nil)

func (c *tCEX) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, nil
}
func (c *tCEX) Balances() (map[uint32]*libxc.ExchangeBalance, error) {
	return nil, nil
}
func (c *tCEX) MatchedMarkets(ctx context.Context) ([]*libxc.MarketMatch, error) {
	return nil, nil
}
func (c *tCEX) Markets(ctx context.Context) (map[string]*libxc.Market, error) {
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
func (c *tCEX) MidGap(baseID, quoteID uint32) uint64 { return 0 }
func (c *tCEX) SubscribeTradeUpdates() (<-chan *libxc.Trade, func(), int) {
	return c.tradeUpdates, func() {}, c.tradeUpdatesID
}
func (c *tCEX) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	return c.depositAddress, nil
}

func (c *tCEX) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string) (string, error) {
	c.withdrawals = append(c.withdrawals, &withdrawArgs{
		address: address,
		amt:     qty,
		assetID: assetID,
	})

	return c.withdrawalID, nil
}

func (c *tCEX) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	c.confirmWithdrawalMtx.Lock()
	defer c.confirmWithdrawalMtx.Unlock()

	if c.confirmWithdrawal == nil {
		return 0, "", libxc.ErrWithdrawalPending
	}
	return c.confirmWithdrawal.amt, c.confirmWithdrawal.txID, nil
}

func (c *tCEX) ConfirmDeposit(ctx context.Context, deposit *libxc.DepositData) (bool, uint64) {
	c.confirmDepositMtx.Lock()
	defer c.confirmDepositMtx.Unlock()

	if c.confirmedDeposit != nil {
		return true, *c.confirmedDeposit
	}
	return false, 0
}

func (c *tCEX) TradeStatus(ctx context.Context, id string, baseID, quoteID uint32) (*libxc.Trade, error) {
	return c.tradeStatus, nil
}

func (c *tCEX) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	return nil, nil, nil
}

type prepareRebalanceResult struct {
	rebalance   int64
	cexReserves uint64
	dexReserves uint64
}

type tBotCexAdaptor struct {
	bidsVWAP        map[uint64]*vwapResult
	asksVWAP        map[uint64]*vwapResult
	vwapErr         error
	balances        map[uint32]*BotBalance
	balanceErr      error
	tradeID         string
	tradeErr        error
	lastTrade       *libxc.Trade
	cancelledTrades []string
	cancelTradeErr  error
	tradeUpdates    chan *libxc.Trade
	maxBuyQty       uint64
	maxSellQty      uint64
}

func newTBotCEXAdaptor() *tBotCexAdaptor {
	return &tBotCexAdaptor{
		bidsVWAP:        make(map[uint64]*vwapResult),
		asksVWAP:        make(map[uint64]*vwapResult),
		balances:        make(map[uint32]*BotBalance),
		cancelledTrades: make([]string, 0),
		tradeUpdates:    make(chan *libxc.Trade),
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
func (c *tBotCexAdaptor) SubscribeTradeUpdates() (updates <-chan *libxc.Trade) {
	return c.tradeUpdates
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
func (c *tBotCexAdaptor) MidGap(baseID, quoteID uint32) uint64 { return 0 }
func (c *tBotCexAdaptor) SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error) {
	if sell {
		return qty <= c.maxSellQty, nil
	}
	return qty <= c.maxBuyQty, nil
}

func (c *tBotCexAdaptor) Book() (_, _ []*core.MiniOrder, _ error) { return nil, nil, nil }

type tExchangeAdaptor struct {
	dexBalances map[uint32]*BotBalance
	cexBalances map[uint32]*BotBalance
	cfg         *BotConfig
}

var _ bot = (*tExchangeAdaptor)(nil)

func (t *tExchangeAdaptor) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, nil
}

func (t *tExchangeAdaptor) refreshAllPendingEvents(context.Context) {}
func (t *tExchangeAdaptor) balances() map[uint32]*BotBalances {
	return nil
}
func (t *tExchangeAdaptor) DEXBalance(assetID uint32) *BotBalance {
	if t.dexBalances[assetID] == nil {
		return &BotBalance{}
	}
	return t.dexBalances[assetID]
}
func (t *tExchangeAdaptor) CEXBalance(assetID uint32) *BotBalance {
	if t.cexBalances[assetID] == nil {
		return &BotBalance{}
	}
	return t.cexBalances[assetID]
}
func (t *tExchangeAdaptor) stats() *RunStats { return nil }
func (t *tExchangeAdaptor) updateConfig(cfg *BotConfig) error {
	t.cfg = cfg
	return nil
}
func (t *tExchangeAdaptor) updateInventory(diffs *BotInventoryDiffs) {}
func (t *tExchangeAdaptor) timeStart() int64                         { return 0 }
func (t *tExchangeAdaptor) Book() (buys, sells []*core.MiniOrder, _ error) {
	return nil, nil, nil
}
func (t *tExchangeAdaptor) sendStatsUpdate()             {}
func (t *tExchangeAdaptor) withPause(func() error) error { return nil }
func (t *tExchangeAdaptor) botCfg() *BotConfig           { return t.cfg }

func TestAvailableBalances(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tCore := newTCore()

	ethBtc := &MarketWithHost{
		Host:    "dex.com",
		BaseID:  60,
		QuoteID: 0,
	}

	dcrBtc := &MarketWithHost{
		Host:    "dex.com",
		BaseID:  42,
		QuoteID: 0,
	}

	dcrUsdc := &MarketWithHost{
		Host:    "dex.com",
		BaseID:  42,
		QuoteID: 60001,
	}

	btcUsdc := &MarketWithHost{
		Host:    "dex.com",
		BaseID:  0,
		QuoteID: 60001,
	}

	cfg := &MarketMakingConfig{
		BotConfigs: []*BotConfig{
			{
				Host:    "dex.com",
				BaseID:  42,
				QuoteID: 0,
			},
			{
				Host:    "dex.com",
				BaseID:  60,
				QuoteID: 0,
				CEXName: libxc.Binance,
			},
			{
				Host:    "dex.com",
				BaseID:  0,
				QuoteID: 60001,
				CEXName: libxc.Binance,
			},
			{
				Host:    "dex.com",
				BaseID:  42,
				QuoteID: 60001,
				CEXName: libxc.BinanceUS,
			},
		},
		CexConfigs: []*CEXConfig{
			{
				Name: libxc.Binance,
			},
			{
				Name: libxc.BinanceUS,
			},
		},
	}

	binance := newTCEX()
	binance.balances = map[uint32]*libxc.ExchangeBalance{
		60:    {Available: 9e5},
		0:     {Available: 8e5},
		60001: {Available: 6e5},
	}

	binanceUS := newTCEX()
	binanceUS.balances = map[uint32]*libxc.ExchangeBalance{
		42:    {Available: 7e5},
		60001: {Available: 6e5},
	}

	mm := MarketMaker{
		ctx:         ctx,
		log:         tLogger,
		core:        tCore,
		defaultCfg:  cfg,
		runningBots: make(map[MarketWithHost]*runningBot),
	}

	mm.cexes = map[string]*centralizedExchange{
		libxc.Binance:   {CEX: binance, CEXConfig: &CEXConfig{Name: libxc.Binance}},
		libxc.BinanceUS: {CEX: binanceUS, CEXConfig: &CEXConfig{Name: libxc.BinanceUS}},
	}

	tCore.setAssetBalances(map[uint32]uint64{
		42:    9e5,
		60:    8e5,
		0:     7e5,
		60001: 6e5,
	})

	checkAvailableBalances := func(mkt *MarketWithHost, expDex, expCex map[uint32]uint64) {
		t.Helper()
		dexBalances, cexBalances, err := mm.AvailableBalances(mkt, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(dexBalances, expDex) {
			t.Fatalf("unexpected dex balances. wanted %v, got %v", expDex, dexBalances)
		}
		if !reflect.DeepEqual(cexBalances, expCex) {
			t.Fatalf("unexpected cex balances. wanted %v, got %v", expCex, cexBalances)
		}
	}

	// No running bots
	checkAvailableBalances(dcrBtc, map[uint32]uint64{42: 9e5, 0: 7e5}, map[uint32]uint64{})
	checkAvailableBalances(ethBtc, map[uint32]uint64{60: 8e5, 0: 7e5}, map[uint32]uint64{60: 9e5, 0: 8e5})
	checkAvailableBalances(btcUsdc, map[uint32]uint64{0: 7e5, 60: 8e5, 60001: 6e5}, map[uint32]uint64{0: 8e5, 60001: 6e5})
	checkAvailableBalances(dcrUsdc, map[uint32]uint64{42: 9e5, 60: 8e5, 60001: 6e5}, map[uint32]uint64{42: 7e5, 60001: 6e5})

	rb := &runningBot{
		bot: &tExchangeAdaptor{
			dexBalances: map[uint32]*BotBalance{
				60:    {Available: 1e5},
				0:     {Available: 4e5},
				60001: {Available: 2e5},
			},
			cexBalances: map[uint32]*BotBalance{
				60001: {Available: 2e5},
				0:     {Available: 3e5},
			},
			cfg: cfg.BotConfigs[1],
		},
	}
	mm.runningBots[*btcUsdc] = rb

	checkAvailableBalances(dcrBtc, map[uint32]uint64{42: 9e5, 0: 3e5}, map[uint32]uint64{})
	checkAvailableBalances(ethBtc, map[uint32]uint64{60: 7e5, 0: 3e5}, map[uint32]uint64{60: 9e5, 0: 5e5})
	checkAvailableBalances(btcUsdc, map[uint32]uint64{0: 3e5, 60: 7e5, 60001: 4e5}, map[uint32]uint64{0: 5e5, 60001: 4e5})
	checkAvailableBalances(dcrUsdc, map[uint32]uint64{42: 9e5, 60: 7e5, 60001: 4e5}, map[uint32]uint64{42: 7e5, 60001: 6e5})
}
