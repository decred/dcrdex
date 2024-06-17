// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/utils"
)

// BotBalance keeps track of the amount of funds available for a
// bot's use, locked to fund orders, and pending.
type BotBalance struct {
	Available uint64 `json:"available"`
	Locked    uint64 `json:"locked"`
	Pending   uint64 `json:"pending"`
	Reserved  uint64 `json:"reserved"`
}

// multiTradePlacement represents a placement to be made on a DEX order book
// using the MultiTrade function. A non-zero counterTradeRate indicates that
// the bot intends to make a counter-trade on a CEX when matches are made on
// the DEX, and this must be taken into consideration in combination with the
// bot's balance on the CEX when deciding how many lots to place. This
// information is also used when considering deposits and withdrawals.
type multiTradePlacement struct {
	lots             uint64
	rate             uint64
	counterTradeRate uint64
}

// orderFees represents the fees that will be required for a single lot of a
// dex order.
type orderFees struct {
	*LotFeeRange
	funding uint64
	// bookingFeesPerLot is the amount of fee asset that needs to be reserved
	// for fees, per ordered lot. For all assets, this will include
	// LotFeeRange.Max.Swap. For non-token EVM assets (eth, matic) Max.Refund
	// will be added. If the asset is the parent chain of a token counter-asset,
	// Max.Redeem is added. This is a commonly needed sum in various validation
	// and optimization functions.
	bookingFeesPerLot uint64
}

// botCoreAdaptor is an interface used by bots to access DEX related
// functions. Common functionality used by multiple market making
// strategies is implemented here. The functions in this interface
// do not need to take assetID parameters, as the bot will only be
// trading on a single DEX market.
type botCoreAdaptor interface {
	SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error)
	Cancel(oidB dex.Bytes) error
	DEXTrade(rate, qty uint64, sell bool) (*core.Order, error)
	ExchangeMarket(host string, baseID, quoteID uint32) (*core.Market, error)
	ExchangeRateFromFiatSources() uint64
	OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error) // estimated fees, not max
	SubscribeOrderUpdates() (updates <-chan *core.Order)
	SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, error)
}

// botCexAdaptor is an interface used by bots to access CEX related
// functions. Common functionality used by multiple market making
// strategies is implemented here. The functions in this interface
// take assetID parameters, unlike botCoreAdaptor, to support a
// multi-hop strategy.
type botCexAdaptor interface {
	CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error
	SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error
	SubscribeTradeUpdates() <-chan *libxc.Trade
	CEXTrade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error)
	SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error)
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	MidGap(baseID, quoteID uint32) uint64
	Book() (buys, sells []*core.MiniOrder, _ error)
}

// pendingWithdrawal represents a withdrawal from a CEX that has been
// initiated, but the DEX has not yet received.
type pendingWithdrawal struct {
	eventLogID   uint64
	timestamp    int64
	withdrawalID string
	assetID      uint32
	// amtWithdrawn is the amount the CEX balance is decreased by.
	// It will not be the same as the amount received in the dex wallet.
	amtWithdrawn uint64

	txIDMtx sync.RWMutex
	txID    string
}

// pendingDeposit represents a deposit to a CEX that has not yet been
// confirmed.
type pendingDeposit struct {
	eventLogID      uint64
	timestamp       int64
	assetID         uint32
	amtConventional float64

	mtx          sync.RWMutex
	tx           *asset.WalletTransaction
	feeConfirmed bool
	cexConfirmed bool
	amtCredited  uint64
}

type balanceEffects struct {
	settled map[uint32]int64
	locked  map[uint32]uint64
	pending map[uint32]uint64
	fees    map[uint32]uint64
}

func newBalanceEffects() *balanceEffects {
	return &balanceEffects{
		settled: make(map[uint32]int64),
		locked:  make(map[uint32]uint64),
		pending: make(map[uint32]uint64),
		fees:    make(map[uint32]uint64),
	}
}

type dexOrderState struct {
	balanceEffects   *balanceEffects
	order            *core.Order
	counterTradeRate uint64
}

// pendingDEXOrder keeps track of the balance effects of a pending DEX order.
// The actual order is not stored here, only its effects on the balance.
type pendingDEXOrder struct {
	eventLogID uint64
	timestamp  int64

	// swaps, redeems, and refunds are caches of transactions. This avoids
	// having to query the wallet for transactions that are already confirmed.
	txsMtx  sync.RWMutex
	swaps   map[string]*asset.WalletTransaction
	redeems map[string]*asset.WalletTransaction
	refunds map[string]*asset.WalletTransaction
	// txsMtx is required to be locked for writes to state
	state atomic.Value // *dexOrderState

	// placementIndex/counterTradeRate are used by MultiTrade to know
	// which orders to place/cancel.
	placementIndex   uint64
	counterTradeRate uint64
}

// updateState should only be called with txsMtx write locked. The dex order
// state is stored as an atomic.Value in order to allow reads without locking.
// The mutex only needs to be locked for reading if the caller wants a consistent
// view of the transactions and the state.
func (p *pendingDEXOrder) updateState(order *core.Order, balanceEffects *balanceEffects, counterTradeRate uint64) {
	p.state.Store(&dexOrderState{
		order:            order,
		balanceEffects:   balanceEffects,
		counterTradeRate: counterTradeRate,
	})
}

// currentState can be called without locking, but to get a consistent view of
// the transactions and the state, txsMtx should be read locked.
func (p *pendingDEXOrder) currentState() *dexOrderState {
	return p.state.Load().(*dexOrderState)
}

// counterTradeAsset is the asset that the bot will need to trade on the CEX
// to arbitrage matches on the DEX.
func (p *pendingDEXOrder) counterTradeAsset() uint32 {
	o := p.currentState().order
	if o.Sell {
		return o.QuoteID
	}
	return o.BaseID
}

type pendingCEXOrder struct {
	eventLogID uint64
	timestamp  int64

	tradeMtx sync.RWMutex
	trade    *libxc.Trade
}

// market is the market-related data for the unifiedExchangeAdaptor and the
// calculators. market provides a number of methods for conversions and
// formatting.
type market struct {
	host        string
	name        string
	rateStep    uint64
	lotSize     uint64
	baseID      uint32
	baseTicker  string
	bui         dex.UnitInfo
	baseFeeID   uint32
	baseFeeUI   dex.UnitInfo
	quoteID     uint32
	quoteTicker string
	qui         dex.UnitInfo
	quoteFeeID  uint32
	quoteFeeUI  dex.UnitInfo
}

func parseMarket(host string, mkt *core.Market) (*market, error) {
	bui, err := asset.UnitInfo(mkt.BaseID)
	if err != nil {
		return nil, err
	}
	baseFeeID := mkt.BaseID
	baseFeeUI := bui
	if tkn := asset.TokenInfo(mkt.BaseID); tkn != nil {
		baseFeeID = tkn.ParentID
		baseFeeUI, err = asset.UnitInfo(tkn.ParentID)
		if err != nil {
			return nil, err
		}
	}
	qui, err := asset.UnitInfo(mkt.QuoteID)
	if err != nil {
		return nil, err
	}
	quoteFeeID := mkt.QuoteID
	quoteFeeUI := qui
	if tkn := asset.TokenInfo(mkt.QuoteID); tkn != nil {
		quoteFeeID = tkn.ParentID
		quoteFeeUI, err = asset.UnitInfo(tkn.ParentID)
		if err != nil {
			return nil, err
		}
	}
	return &market{
		host:        host,
		name:        mkt.Name,
		rateStep:    mkt.RateStep,
		lotSize:     mkt.LotSize,
		baseID:      mkt.BaseID,
		baseTicker:  bui.Conventional.Unit,
		bui:         bui,
		baseFeeID:   baseFeeID,
		baseFeeUI:   baseFeeUI,
		quoteID:     mkt.QuoteID,
		quoteTicker: qui.Conventional.Unit,
		qui:         qui,
		quoteFeeID:  quoteFeeID,
		quoteFeeUI:  quoteFeeUI,
	}, nil
}

func (m *market) fmtRate(msgRate uint64) string {
	r := calc.ConventionalRate(msgRate, m.bui, m.qui)
	s := strconv.FormatFloat(r, 'f', 8, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
func (m *market) fmtBase(atoms uint64) string {
	return m.bui.FormatAtoms(atoms)
}
func (m *market) fmtQuote(atoms uint64) string {
	return m.qui.FormatAtoms(atoms)
}
func (m *market) fmtQty(assetID uint32, atoms uint64) string {
	if assetID == m.baseID {
		return m.fmtBase(atoms)
	}
	return m.fmtQuote(atoms)
}

func (m *market) fmtBaseFees(atoms uint64) string {
	return m.baseFeeUI.FormatAtoms(atoms)
}

func (m *market) fmtQuoteFees(atoms uint64) string {
	return m.quoteFeeUI.FormatAtoms(atoms)
}
func (m *market) fmtFees(assetID uint32, atoms uint64) string {
	if assetID == m.baseID {
		return m.fmtBaseFees(atoms)
	}
	return m.fmtQuoteFees(atoms)
}

func (m *market) msgRate(convRate float64) uint64 {
	return calc.MessageRate(convRate, m.bui, m.qui)
}

// unifiedExchangeAdaptor implements both botCoreAdaptor and botCexAdaptor.
type unifiedExchangeAdaptor struct {
	*market
	clientCore
	libxc.CEX

	ctx             context.Context
	wg              sync.WaitGroup
	botID           string
	log             dex.Logger
	fiatRates       atomic.Value // map[uint32]float64
	orderUpdates    atomic.Value // chan *core.Order
	mwh             *MarketWithHost
	eventLogDB      eventLogDB
	botCfgV         atomic.Value // *BotConfig
	initialBalances map[uint32]uint64
	baseTraits      asset.WalletTrait
	quoteTraits     asset.WalletTrait

	botLooper dex.Connector
	botLoop   *dex.ConnectionMaster
	paused    atomic.Bool

	autoRebalanceCfg *AutoRebalanceConfig

	subscriptionIDMtx sync.RWMutex
	subscriptionID    *int

	feesMtx  sync.RWMutex
	buyFees  *orderFees
	sellFees *orderFees

	startTime  atomic.Int64
	eventLogID atomic.Uint64

	balancesMtx sync.RWMutex
	// baseDEXBalance/baseCEXBalance are the balances the bots have before
	// taking into account any pending actions. These are updated whenever
	// a pending action is completed. They may become negative if a balance
	// is decreased during an update while there are pending actions that
	// positively affect the available balance.
	baseDexBalances    map[uint32]int64
	baseCexBalances    map[uint32]int64
	pendingDEXOrders   map[order.OrderID]*pendingDEXOrder
	pendingCEXOrders   map[string]*pendingCEXOrder
	pendingWithdrawals map[string]*pendingWithdrawal
	pendingDeposits    map[string]*pendingDeposit
	inventoryMods      map[uint32]int64

	// If pendingBaseRebalance/pendingQuoteRebalance are true, it means
	// there is a pending deposit/withdrawal of the base/quote asset,
	// and no other deposits/withdrawals of that asset should happen
	// until it is complete.
	pendingBaseRebalance  atomic.Bool
	pendingQuoteRebalance atomic.Bool

	// The following are updated whenever a pending action is complete.
	// For accurate run stats, the pending actions must be taken into
	// account.
	runStats struct {
		baseBalanceDelta  atomic.Int64  // unused
		quoteBalanceDelta atomic.Int64  // unused
		baseFees          atomic.Uint64 // unused
		quoteFees         atomic.Uint64 // unused
		completedMatches  atomic.Uint32
		tradedUSD         struct {
			sync.Mutex
			v float64
		}
		feeGapStats atomic.Value
	}
}

var _ botCoreAdaptor = (*unifiedExchangeAdaptor)(nil)
var _ botCexAdaptor = (*unifiedExchangeAdaptor)(nil)

func (u *unifiedExchangeAdaptor) botCfg() *BotConfig {
	return u.botCfgV.Load().(*BotConfig)
}

// botLooper is just a dex.Connector for a function.
type botLooper func(context.Context) (*sync.WaitGroup, error)

func (f botLooper) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return f(ctx)
}

// setBotLoop sets the loop that must be shut down for configuration updates.
// Every bot should call setBotLoop during construction.
func (u *unifiedExchangeAdaptor) setBotLoop(f botLooper) {
	u.botLooper = f
}

func (u *unifiedExchangeAdaptor) runBotLoop(ctx context.Context) error {
	if u.botLooper == nil {
		return errors.New("no bot looper set")
	}
	u.botLoop = dex.NewConnectionMaster(u.botLooper)
	return u.botLoop.ConnectOnce(ctx)
}

// withPause runs a function with the bot loop paused.
func (u *unifiedExchangeAdaptor) withPause(f func() error) error {
	if !u.paused.CompareAndSwap(false, true) {
		return errors.New("already paused")
	}
	defer u.paused.Store(false)

	u.botLoop.Disconnect()
	if err := f(); err != nil {
		return err
	}
	if u.ctx.Err() != nil { // Make sure we weren't shut down during pause.
		return u.ctx.Err()
	}
	return u.botLoop.ConnectOnce(u.ctx)
}

func (u *unifiedExchangeAdaptor) logBalanceAdjustments(dexDiffs, cexDiffs map[uint32]int64, reason string) {
	if u.log.Level() > dex.LevelTrace {
		return
	}

	var msg strings.Builder
	writeLine := func(s string, a ...interface{}) {
		msg.WriteString("\n" + fmt.Sprintf(s, a...))
	}
	writeLine("")
	writeLine("Balance adjustments(%s):", reason)

	format := func(assetID uint32, v int64, plusSign bool) string {
		ui, err := asset.UnitInfo(assetID)
		if err != nil {
			return "<what the asset?>"
		}
		return ui.FormatSignedAtoms(v, plusSign)
	}

	if len(dexDiffs) > 0 {
		writeLine("  DEX:")
		for assetID, dexDiff := range dexDiffs {
			writeLine("    " + format(assetID, dexDiff, true))
		}
	}

	if len(cexDiffs) > 0 {
		writeLine("  CEX:")
		for assetID, cexDiff := range cexDiffs {
			writeLine("    " + format(assetID, cexDiff, true))
		}
	}

	writeLine("Updated settled balances:")
	writeLine("  DEX:")
	for assetID, bal := range u.baseDexBalances {
		writeLine("    " + format(assetID, bal, false))
	}

	if len(u.baseCexBalances) > 0 {
		writeLine("  CEX:")
		for assetID, bal := range u.baseCexBalances {
			writeLine("    " + format(assetID, bal, false))
		}
	}

	dexPending := make(map[uint32]uint64)
	addDexPending := func(assetID uint32) {
		if v := u.dexBalance(assetID).Pending; v > 0 {
			dexPending[assetID] = v
		}
	}
	cexPending := make(map[uint32]uint64)
	addCexPending := func(assetID uint32) {
		if v := u.cexBalance(assetID).Pending; v > 0 {
			cexPending[assetID] = v
		}
	}
	addDexPending(u.baseID)
	addCexPending(u.baseID)
	addDexPending(u.quoteID)
	addCexPending(u.quoteID)
	if u.baseFeeID != u.baseID {
		addCexPending(u.baseFeeID)
		addCexPending(u.baseFeeID)
	}
	if u.quoteFeeID != u.quoteID && u.quoteFeeID != u.baseID {
		addCexPending(u.quoteFeeID)
		addCexPending(u.quoteFeeID)
	}
	if len(dexPending) > 0 {
		writeLine("  DEX pending:")
		for assetID, v := range dexPending {
			writeLine("    " + format(assetID, int64(v), true))
		}
	}
	if len(cexPending) > 0 {
		writeLine("  CEX pending:")
		for assetID, v := range cexPending {
			writeLine("    " + format(assetID, int64(v), true))
		}
	}

	writeLine("")
	u.log.Tracef(msg.String())
}

// SufficientBalanceForDEXTrade returns whether the bot has sufficient balance
// to place a DEX trade.
func (u *unifiedExchangeAdaptor) SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, error) {
	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(u.baseID, u.quoteID, sell)
	balances := map[uint32]uint64{}
	for _, assetID := range []uint32{fromAsset, fromFeeAsset, toAsset, toFeeAsset} {
		if _, found := balances[assetID]; !found {
			bal := u.DEXBalance(assetID)
			balances[assetID] = bal.Available
		}
	}

	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		return false, err
	}
	fees, fundingFees := buyFees.Max, buyFees.funding
	if sell {
		fees, fundingFees = sellFees.Max, sellFees.funding
	}

	if balances[fromFeeAsset] < fundingFees {
		return false, nil
	}
	balances[fromFeeAsset] -= fundingFees

	fromQty := qty
	if !sell {
		fromQty = calc.BaseToQuote(rate, qty)
	}
	if balances[fromAsset] < fromQty {
		return false, nil
	}
	balances[fromAsset] -= fromQty

	numLots := qty / u.lotSize
	if balances[fromFeeAsset] < numLots*fees.Swap {
		return false, nil
	}
	balances[fromFeeAsset] -= numLots * fees.Swap

	if u.isAccountLocker(fromAsset) {
		if balances[fromFeeAsset] < numLots*fees.Refund {
			return false, nil
		}
		balances[fromFeeAsset] -= numLots * fees.Refund
	}

	if u.isAccountLocker(toAsset) {
		if balances[toFeeAsset] < numLots*fees.Redeem {
			return false, nil
		}
		balances[toFeeAsset] -= numLots * fees.Redeem
	}

	return true, nil
}

// SufficientBalanceOnCEXTrade returns whether the bot has sufficient balance
// to place a CEX trade.
func (u *unifiedExchangeAdaptor) SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error) {
	var fromAssetID uint32
	var fromAssetQty uint64
	if sell {
		fromAssetID = u.baseID
		fromAssetQty = qty
	} else {
		fromAssetID = u.quoteID
		fromAssetQty = calc.BaseToQuote(rate, qty)
	}

	fromAssetBal := u.CEXBalance(fromAssetID)
	return fromAssetBal.Available >= fromAssetQty, nil
}

// dexOrderInfo is used by MultiTrade to keep track of the placement index
// and counter trade rate of an order.
type dexOrderInfo struct {
	placementIndex   uint64
	counterTradeRate uint64
	placement        *core.QtyRate
}

// updateDEXOrderEvent updates the event log with the current state of a
// pending DEX order and sends an event notification.
func (u *unifiedExchangeAdaptor) updateDEXOrderEvent(o *pendingDEXOrder, complete bool) {
	transactions := make([]*asset.WalletTransaction, 0, len(o.swaps)+len(o.redeems)+len(o.refunds))
	addTxs := func(txs map[string]*asset.WalletTransaction) {
		for _, tx := range txs {
			transactions = append(transactions, tx)
		}
	}

	o.txsMtx.RLock()
	addTxs(o.swaps)
	addTxs(o.redeems)
	addTxs(o.refunds)
	state := o.currentState()
	o.txsMtx.RUnlock()

	e := &MarketMakingEvent{
		ID:         o.eventLogID,
		TimeStamp:  o.timestamp,
		Pending:    !complete,
		BaseDelta:  state.balanceEffects.settled[u.baseID],
		QuoteDelta: state.balanceEffects.settled[u.quoteID],
		BaseFees:   state.balanceEffects.fees[u.baseFeeID],
		QuoteFees:  state.balanceEffects.fees[u.quoteFeeID],
		DEXOrderEvent: &DEXOrderEvent{
			ID:           state.order.ID.String(),
			Rate:         state.order.Rate,
			Qty:          state.order.Qty,
			Sell:         state.order.Sell,
			Transactions: transactions,
		},
	}

	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
	u.notifyEvent(e)
}

// updateCEXOrderEvent updates the event log with the current state of a
// pending CEX order and sends an event notification.
func (u *unifiedExchangeAdaptor) updateCEXOrderEvent(trade *libxc.Trade, eventID uint64, timestamp int64) {
	var baseDelta, quoteDelta int64
	if trade.Sell {
		baseDelta = -int64(trade.BaseFilled)
		quoteDelta = int64(trade.QuoteFilled)
	} else {
		baseDelta = int64(trade.BaseFilled)
		quoteDelta = -int64(trade.QuoteFilled)
	}

	e := &MarketMakingEvent{
		ID:         eventID,
		TimeStamp:  timestamp,
		Pending:    !trade.Complete,
		BaseDelta:  baseDelta,
		QuoteDelta: quoteDelta,
		CEXOrderEvent: &CEXOrderEvent{
			ID:          trade.ID,
			Rate:        trade.Rate,
			Qty:         trade.Qty,
			Sell:        trade.Sell,
			BaseFilled:  trade.BaseFilled,
			QuoteFilled: trade.QuoteFilled,
		},
	}

	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
	u.notifyEvent(e)
}

// updateDepositEvent updates the event log with the current state of a
// pending deposit and sends an event notification.
func (u *unifiedExchangeAdaptor) updateDepositEvent(deposit *pendingDeposit) {
	var baseDelta, quoteDelta, diff int64
	var baseFees, quoteFees uint64

	deposit.mtx.RLock()
	if deposit.cexConfirmed && deposit.tx.Amount > deposit.amtCredited {
		diff = int64(deposit.tx.Amount - deposit.amtCredited)
	}
	if deposit.assetID == u.baseID {
		baseDelta = -diff
		baseFees = deposit.tx.Fees
	} else {
		quoteDelta = -diff
		quoteFees = deposit.tx.Fees
	}
	e := &MarketMakingEvent{
		ID:         deposit.eventLogID,
		TimeStamp:  deposit.timestamp,
		BaseDelta:  baseDelta,
		QuoteDelta: quoteDelta,
		BaseFees:   baseFees,
		QuoteFees:  quoteFees,
		Pending:    !deposit.cexConfirmed || !deposit.feeConfirmed,
		DepositEvent: &DepositEvent{
			AssetID:     deposit.assetID,
			Transaction: deposit.tx,
			CEXCredit:   deposit.amtCredited,
		},
	}
	deposit.mtx.RUnlock()

	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
	u.notifyEvent(e)
}

func (u *unifiedExchangeAdaptor) updateConfigEvent(updatedCfg *BotConfig) {
	e := &MarketMakingEvent{
		ID:           u.eventLogID.Add(1),
		TimeStamp:    time.Now().Unix(),
		UpdateConfig: updatedCfg,
	}
	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
}

func (u *unifiedExchangeAdaptor) updateInventoryEvent(inventoryMods map[uint32]int64) {
	e := &MarketMakingEvent{
		ID:              u.eventLogID.Add(1),
		TimeStamp:       time.Now().Unix(),
		UpdateInventory: &inventoryMods,
	}
	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
}

// updateWithdrawalEvent updates the event log with the current state of a
// pending withdrawal and sends an event notification.
func (u *unifiedExchangeAdaptor) updateWithdrawalEvent(withdrawalID string, withdrawal *pendingWithdrawal, tx *asset.WalletTransaction) {
	var baseFees, quoteFees uint64
	complete := tx != nil && tx.Confirmed
	if complete && withdrawal.amtWithdrawn > tx.Amount {
		if withdrawal.assetID == u.baseID {
			baseFees = withdrawal.amtWithdrawn - tx.Amount
		} else {
			quoteFees = withdrawal.amtWithdrawn - tx.Amount
		}
	}

	e := &MarketMakingEvent{
		ID:        withdrawal.eventLogID,
		TimeStamp: withdrawal.timestamp,
		BaseFees:  baseFees,
		QuoteFees: quoteFees,
		Pending:   !complete,
		WithdrawalEvent: &WithdrawalEvent{
			AssetID:     withdrawal.assetID,
			ID:          withdrawalID,
			Transaction: tx,
			CEXDebit:    withdrawal.amtWithdrawn,
		},
	}

	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, e, u.balanceState())
	u.notifyEvent(e)
}

// groupedBookedOrders returns pending dex orders grouped by the placement
// index used to create them when they were placed with MultiTrade.
func (u *unifiedExchangeAdaptor) groupedBookedOrders(sells bool) (orders map[uint64][]*pendingDEXOrder) {
	orders = make(map[uint64][]*pendingDEXOrder)

	groupPendingOrder := func(pendingOrder *pendingDEXOrder) {
		o := pendingOrder.currentState().order
		if o.Status > order.OrderStatusBooked {
			return
		}

		pi := pendingOrder.placementIndex
		if sells == o.Sell {
			if orders[pi] == nil {
				orders[pi] = []*pendingDEXOrder{}
			}
			orders[pi] = append(orders[pi], pendingOrder)
		}
	}

	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	for _, pendingOrder := range u.pendingDEXOrders {
		groupPendingOrder(pendingOrder)
	}

	return
}

// rateCausesSelfMatchFunc returns a function that can be called to determine
// whether a rate would cause a self match. The sell parameter indicates whether
// the returned function will support sell or buy orders.
func (u *unifiedExchangeAdaptor) rateCausesSelfMatchFunc(sell bool) func(rate uint64) bool {
	var highestExistingBuy, lowestExistingSell uint64 = 0, math.MaxUint64

	for _, groupedOrders := range u.groupedBookedOrders(!sell) {
		for _, o := range groupedOrders {
			order := o.currentState().order
			if sell && order.Rate > highestExistingBuy {
				highestExistingBuy = order.Rate
			}
			if !sell && order.Rate < lowestExistingSell {
				lowestExistingSell = order.Rate
			}
		}
	}

	return func(rate uint64) bool {
		if sell {
			return rate <= highestExistingBuy
		}
		return rate >= lowestExistingSell
	}
}

// reservedForCounterTrade returns the amount that is required to be reserved
// on the CEX in order for this order to be counter traded when matched.
func reservedForCounterTrade(o *pendingDEXOrder) uint64 {
	order := o.currentState().order
	if order.Sell {
		return calc.BaseToQuote(o.counterTradeRate, order.Qty-order.Filled)
	}

	return order.Qty - order.Filled
}

func withinTolerance(rate, target uint64, driftTolerance float64) bool {
	tolerance := uint64(float64(target) * driftTolerance)
	lowerBound := target - tolerance
	upperBound := target + tolerance
	return rate >= lowerBound && rate <= upperBound
}

func (u *unifiedExchangeAdaptor) placeMultiTrade(placements []*dexOrderInfo, sell bool) ([]*core.Order, error) {
	corePlacements := make([]*core.QtyRate, 0, len(placements))
	for _, p := range placements {
		corePlacements = append(corePlacements, p.placement)
	}

	botCfg := u.botCfg()
	var walletOptions map[string]string
	if sell {
		walletOptions = botCfg.BaseWalletOptions
	} else {
		walletOptions = botCfg.QuoteWalletOptions
	}

	fromAsset, fromFeeAsset, _, toFeeAsset := orderAssets(u.baseID, u.quoteID, sell)
	multiTradeForm := &core.MultiTradeForm{
		Host:       u.host,
		Base:       u.baseID,
		Quote:      u.quoteID,
		Sell:       sell,
		Placements: corePlacements,
		Options:    walletOptions,
		MaxLock:    u.DEXBalance(fromAsset).Available,
	}

	newPendingDEXOrders := make([]*pendingDEXOrder, 0, len(placements))
	defer func() {
		// defer until u.balancesMtx is unlocked
		for _, o := range newPendingDEXOrders {
			u.updateDEXOrderEvent(o, false)
		}
		u.sendStatsUpdate()
	}()

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	orders, err := u.clientCore.MultiTrade([]byte{}, multiTradeForm)
	if err != nil {
		return nil, err
	}

	for i, o := range orders {
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		effects := newBalanceEffects()

		effects.locked[fromAsset] += o.LockedAmt
		effects.locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
		effects.locked[toFeeAsset] += o.RedeemLockedAmt

		if o.FeesPaid != nil && o.FeesPaid.Funding > 0 {
			effects.fees[fromFeeAsset] += o.FeesPaid.Funding
		}

		pendingOrder := &pendingDEXOrder{
			eventLogID:       u.eventLogID.Add(1),
			timestamp:        time.Now().Unix(),
			swaps:            make(map[string]*asset.WalletTransaction),
			redeems:          make(map[string]*asset.WalletTransaction),
			refunds:          make(map[string]*asset.WalletTransaction),
			placementIndex:   placements[i].placementIndex,
			counterTradeRate: placements[i].counterTradeRate,
		}
		pendingOrder.state.Store(
			&dexOrderState{
				order:            o,
				balanceEffects:   effects,
				counterTradeRate: pendingOrder.counterTradeRate,
			})
		u.pendingDEXOrders[orderID] = pendingOrder
		newPendingDEXOrders = append(newPendingDEXOrders, u.pendingDEXOrders[orderID])
	}

	return orders, nil
}

// MultiTrade places multiple orders on the DEX order book. The placements
// arguments does not represent the trades that should be placed at this time,
// but rather the amount of lots that the caller expects consistently have on
// the orderbook at various rates. It is expected that the caller will
// periodically (each epoch) call this function with the same number of
// placements in the same order, with the rates updated to reflect the current
// market conditions.
//
// When an order is placed, the index of the placement that initiated the order
// is tracked. On subsequent calls, as the rates change, the placements will be
// compared with prior trades with the same placement index. If the trades on
// the books differ from the rates in the placements by greater than
// driftTolerance, the orders will be cancelled. As orders get filled, and there
// are less than the number of lots specified in the placement on the books,
// new trades will be made.
//
// The caller can pass a rate of 0 for any placement to indicate that all orders
// that were made during previous calls to MultiTrade with the same placement index
// should be cancelled.
//
// dexReserves and cexReserves are the amount of funds that should not be used to
// place orders. These are used in case the bot is about to make a deposit or
// withdrawal, and does not want those funds to get locked up in a trade.
//
// The placements must be passed in decreasing priority order. If there is not
// enough balance to place all of the orders, the lower priority trades that
// were made in previous calls will be cancelled.
func (u *unifiedExchangeAdaptor) multiTrade(
	placements []*multiTradePlacement,
	sell bool,
	driftTolerance float64,
	currEpoch uint64,
) map[order.OrderID]*dexOrderInfo {
	if len(placements) == 0 {
		return nil
	}
	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		u.log.Errorf("multiTrade: error getting order fees: %v", err)
		return nil
	}

	fromID, fromFeeID, toID, toFeeID := orderAssets(u.baseID, u.quoteID, sell)
	fees, fundingFees := buyFees.Max, buyFees.funding
	if sell {
		fees, fundingFees = sellFees.Max, sellFees.funding
	}

	// First, determine the amount of balances the bot has available to place
	// DEX trades taking into account dexReserves.
	remainingBalances := map[uint32]uint64{}
	for _, assetID := range []uint32{fromID, fromFeeID, toID, toFeeID} {
		if _, found := remainingBalances[assetID]; !found {
			remainingBalances[assetID] = u.DEXBalance(assetID).Available
		}
	}
	if remainingBalances[fromFeeID] < fundingFees {
		u.log.Debugf("multiTrade: insufficient balance for funding fees. required: %d, have: %d",
			fundingFees, remainingBalances[fromFeeID])
		return nil
	}
	remainingBalances[fromFeeID] -= fundingFees

	// If the placements include a counterTradeRate, the CEX balance must also
	// be taken into account to determine how many trades can be placed.
	accountForCEXBal := placements[0].counterTradeRate > 0
	var remainingCEXBal uint64
	if accountForCEXBal {
		remainingCEXBal = u.CEXBalance(toID).Available
	}

	cancels := make([]dex.Bytes, 0, len(placements))
	addCancel := func(o *core.Order) {
		if currEpoch-o.Epoch < 2 { // TODO: check epoch
			u.log.Debugf("multiTrade: skipping cancel not past free cancel threshold")
			return
		}
		cancels = append(cancels, o.ID)
	}

	pendingOrders := u.groupedBookedOrders(sell)

	// requiredPlacements is a copy of placements where the lots field is
	// adjusted to take into account pending orders that are already on
	// the books.
	requiredPlacements := make([]*multiTradePlacement, 0, len(placements))
	// keptOrders is a list of pending orders that are not being cancelled, in
	// decreasing order of placementIndex. If the bot doesn't have enough
	// balance to place an order with a higher priority (lower placementIndex)
	// then the lower priority orders in this list will be cancelled.
	keptOrders := make([]*pendingDEXOrder, 0, len(placements))
	for _, p := range placements {
		pCopy := *p
		requiredPlacements = append(requiredPlacements, &pCopy)
	}
	for _, groupedOrders := range pendingOrders {
		for _, o := range groupedOrders {
			order := o.currentState().order
			if o.placementIndex >= uint64(len(requiredPlacements)) {
				// This will happen if there is a reconfig in which there are
				// now less placements than before.
				addCancel(order)
				continue
			}

			mustCancel := !withinTolerance(order.Rate, placements[o.placementIndex].rate, driftTolerance)
			if requiredPlacements[o.placementIndex].lots >= (order.Qty-order.Filled)/u.lotSize {
				requiredPlacements[o.placementIndex].lots -= (order.Qty - order.Filled) / u.lotSize
			} else {
				// This will happen if there is a reconfig in which this
				// placement index now requires less lots than before.
				mustCancel = true
				requiredPlacements[o.placementIndex].lots = 0
			}

			if mustCancel {
				u.log.Tracef("%s cancel with order rate = %s, placement rate = %s, drift tolerance = %.4f%%",
					u.mwh, u.fmtRate(order.Rate), u.fmtRate(placements[o.placementIndex].rate), driftTolerance*100,
				)
				addCancel(order)
			} else {
				keptOrders = append([]*pendingDEXOrder{o}, keptOrders...)
			}
		}
	}

	rateCausesSelfMatch := u.rateCausesSelfMatchFunc(sell)

	fundingReq := func(rate, lots, counterTradeRate uint64) (dexReq map[uint32]uint64, cexReq uint64) {
		qty := u.lotSize * lots
		if !sell {
			qty = calc.BaseToQuote(rate, qty)
		}
		dexReq = make(map[uint32]uint64)
		dexReq[fromID] += qty
		dexReq[fromFeeID] += fees.Swap * lots
		if u.isAccountLocker(fromID) {
			dexReq[fromFeeID] += fees.Refund * lots
		}
		if u.isAccountLocker(toID) {
			dexReq[toFeeID] += fees.Redeem * lots
		}
		if accountForCEXBal {
			if sell {
				cexReq = calc.BaseToQuote(counterTradeRate, u.lotSize*lots)
			} else {
				cexReq = u.lotSize * lots
			}
		}
		return
	}

	canAffordLots := func(rate, lots, counterTradeRate uint64) bool {
		dexReq, cexReq := fundingReq(rate, lots, counterTradeRate)
		for assetID, v := range dexReq {
			if remainingBalances[assetID] < v {
				return false
			}
		}
		return remainingCEXBal >= cexReq
	}

	orderInfos := make([]*dexOrderInfo, 0, len(requiredPlacements))

	for i, placement := range requiredPlacements {
		if placement.lots == 0 {
			continue
		}

		if rateCausesSelfMatch(placement.rate) {
			u.log.Warnf("multiTrade: rate %d causes self match. Placements should be farther from mid-gap.", placement.rate)
			continue
		}

		searchN := int(placement.lots) + 1
		lotsPlus1 := sort.Search(searchN, func(lotsi int) bool {
			return !canAffordLots(placement.rate, uint64(lotsi), placement.counterTradeRate)
		})

		var lotsToPlace uint64
		if lotsPlus1 > 1 {
			lotsToPlace = uint64(lotsPlus1) - 1
			dexReq, cexReq := fundingReq(placement.rate, lotsToPlace, placement.counterTradeRate)
			for assetID, v := range dexReq {
				remainingBalances[assetID] -= v
			}
			remainingCEXBal -= cexReq
			orderInfos = append(orderInfos, &dexOrderInfo{
				placementIndex:   uint64(i),
				counterTradeRate: placement.counterTradeRate,
				placement: &core.QtyRate{
					Qty:  lotsToPlace * u.lotSize,
					Rate: placement.rate,
				},
			})
		}

		// If there is insufficient balance to place a higher priority order,
		// cancel the lower priority orders.
		if lotsToPlace < placement.lots {
			u.log.Tracef("multiTrade(%s,%d) out of funds for more placements. %d of %d lots for rate %s",
				sellStr(sell), i, lotsToPlace, placement.lots, u.fmtRate(placement.rate))
			for _, o := range keptOrders {
				if o.placementIndex > uint64(i) {
					order := o.currentState().order
					addCancel(order)
				}
			}

			break
		}
	}

	for _, cancel := range cancels {
		if err := u.Cancel(cancel); err != nil {
			u.log.Errorf("multiTrade: error canceling order %s: %v", cancel, err)
		}
	}

	if len(orderInfos) > 0 {
		orders, err := u.placeMultiTrade(orderInfos, sell)
		if err != nil {
			u.log.Errorf("multiTrade: error placing orders: %v", err)
			return nil
		}

		ordered := make(map[order.OrderID]*dexOrderInfo, len(placements))
		for i, o := range orders {
			var orderID order.OrderID
			copy(orderID[:], o.ID)
			ordered[orderID] = orderInfos[i]
		}
		return ordered
	}

	return nil
}

// DEXTrade places a single order on the DEX order book.
func (u *unifiedExchangeAdaptor) DEXTrade(rate, qty uint64, sell bool) (*core.Order, error) {
	enough, err := u.SufficientBalanceForDEXTrade(rate, qty, sell)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	placements := []*dexOrderInfo{{
		placement: &core.QtyRate{
			Qty:  qty,
			Rate: rate,
		},
	}}

	// multiTrade is used instead of Trade because Trade does not support
	// maxLock.
	orders, err := u.placeMultiTrade(placements, sell)
	if err != nil {
		return nil, err
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("no orders placed")
	}

	return orders[0], nil
}

type BotBalances struct {
	DEX *BotBalance `json:"dex"`
	CEX *BotBalance `json:"cex"`
}

// dexBalance must be called with the balancesMtx locked.
func (u *unifiedExchangeAdaptor) dexBalance(assetID uint32) *BotBalance {
	bal, found := u.baseDexBalances[assetID]
	if !found {
		return &BotBalance{}
	}

	var totalAvailableDiff int64
	var totalLocked, totalPending uint64

	for _, pendingOrder := range u.pendingDEXOrders {
		effects := pendingOrder.currentState().balanceEffects
		totalAvailableDiff += effects.settled[assetID]
		locked := effects.locked[assetID]
		totalAvailableDiff -= int64(locked)
		totalLocked += locked
		totalAvailableDiff -= int64(effects.fees[assetID])
		totalPending += effects.pending[assetID]
	}

	for _, pendingDeposit := range u.pendingDeposits {
		pendingDeposit.mtx.RLock()
		if pendingDeposit.assetID == assetID {
			totalAvailableDiff -= int64(pendingDeposit.tx.Amount)
		}
		fee := int64(pendingDeposit.tx.Fees)
		token := asset.TokenInfo(pendingDeposit.assetID)
		if token != nil && token.ParentID == assetID {
			totalAvailableDiff -= fee
		} else if token == nil && pendingDeposit.assetID == assetID {
			totalAvailableDiff -= fee
		}
		pendingDeposit.mtx.RUnlock()
	}

	for _, pendingWithdrawal := range u.pendingWithdrawals {
		if pendingWithdrawal.assetID == assetID {
			totalPending += pendingWithdrawal.amtWithdrawn
		}
	}

	availableBalance := bal + totalAvailableDiff
	if availableBalance < 0 {
		u.log.Errorf("negative dex balance for %s: %d", dex.BipIDSymbol(assetID), availableBalance)
		availableBalance = 0
	}

	return &BotBalance{
		Available: uint64(availableBalance),
		Locked:    totalLocked,
		Pending:   totalPending,
	}
}

// DEXBalance returns the balance of the bot on the DEX.
func (u *unifiedExchangeAdaptor) DEXBalance(assetID uint32) *BotBalance {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	return u.dexBalance(assetID)
}

func (u *unifiedExchangeAdaptor) timeStart() int64 {
	return u.startTime.Load()
}

// refreshAllPendingEvents updates the state of all pending events so that
// balances will be up to date.
func (u *unifiedExchangeAdaptor) refreshAllPendingEvents(ctx context.Context) {
	// Make copies of all maps to avoid locking balancesMtx for the entire process
	u.balancesMtx.Lock()
	pendingDEXOrders := make(map[order.OrderID]*pendingDEXOrder, len(u.pendingDEXOrders))
	for oid, pendingOrder := range u.pendingDEXOrders {
		pendingDEXOrders[oid] = pendingOrder
	}
	pendingCEXOrders := make(map[string]*pendingCEXOrder, len(u.pendingCEXOrders))
	for tradeID, pendingOrder := range u.pendingCEXOrders {
		pendingCEXOrders[tradeID] = pendingOrder
	}
	pendingWithdrawals := make(map[string]*pendingWithdrawal, len(u.pendingWithdrawals))
	for withdrawalID, pendingWithdrawal := range u.pendingWithdrawals {
		pendingWithdrawals[withdrawalID] = pendingWithdrawal
	}
	pendingDeposits := make(map[string]*pendingDeposit, len(u.pendingDeposits))
	for txID, pendingDeposit := range u.pendingDeposits {
		pendingDeposits[txID] = pendingDeposit
	}
	u.balancesMtx.Unlock()

	for _, pendingOrder := range pendingDEXOrders {
		pendingOrder.txsMtx.Lock()
		state := pendingOrder.currentState()
		u.checkPendingDEXOrderTxs(pendingOrder, state.order)
		pendingOrder.txsMtx.Unlock()
	}

	for _, pendingDeposit := range pendingDeposits {
		u.confirmDeposit(ctx, pendingDeposit.tx.ID)
	}

	for _, pendingWithdrawal := range pendingWithdrawals {
		u.confirmWithdrawal(ctx, pendingWithdrawal.withdrawalID)
	}

	for _, pendingOrder := range pendingCEXOrders {
		trade, err := u.CEX.TradeStatus(ctx, pendingOrder.trade.ID, pendingOrder.trade.BaseID, pendingOrder.trade.QuoteID)
		if err != nil {
			u.log.Errorf("error getting CEX trade status: %v", err)
			continue
		}
		u.handleCEXTradeUpdate(trade)
	}
}

// incompleteCexTradeBalanceEffects returns the balance effects of an
// incomplete CEX trade. As soon as a CEX trade is complete, it is removed
// from the pending map, so completed trades do not need to be calculated.
func incompleteCexTradeBalanceEffects(trade *libxc.Trade) (availableDiff map[uint32]int64, locked map[uint32]uint64) {
	availableDiff = make(map[uint32]int64)
	locked = make(map[uint32]uint64)

	if trade.Sell {
		availableDiff[trade.BaseID] = -int64(trade.Qty)
		locked[trade.BaseID] = trade.Qty - trade.BaseFilled
		availableDiff[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		availableDiff[trade.QuoteID] = -int64(calc.BaseToQuote(trade.Rate, trade.Qty))
		locked[trade.QuoteID] = calc.BaseToQuote(trade.Rate, trade.Qty) - trade.QuoteFilled
		availableDiff[trade.BaseID] = int64(trade.BaseFilled)
	}

	return
}

// cexBalance must be called with the balancesMtx locked.
func (u *unifiedExchangeAdaptor) cexBalance(assetID uint32) *BotBalance {
	var totalFundingOrder, totalReserved, totalPending uint64
	var totalAvailableDiff int64
	for _, pendingOrder := range u.pendingCEXOrders {
		pendingOrder.tradeMtx.RLock()
		trade := pendingOrder.trade
		pendingOrder.tradeMtx.RUnlock()

		if trade.BaseID == assetID || trade.QuoteID == assetID {
			if trade.Complete {
				u.log.Errorf("pending cex order %s is complete", trade.ID)
				continue
			}
			availableDiff, fundingOrder := incompleteCexTradeBalanceEffects(trade)
			totalAvailableDiff += availableDiff[assetID]
			totalFundingOrder += fundingOrder[assetID]
		}
	}

	for _, withdrawal := range u.pendingWithdrawals {
		if withdrawal.assetID == assetID {
			totalAvailableDiff -= int64(withdrawal.amtWithdrawn)
		}
	}

	// Credited deposits generally should already be part of the base balance,
	// but just in case the amount was credited before the wallet confirmed the
	// fee.
	for _, deposit := range u.pendingDeposits {
		if deposit.assetID == assetID {
			deposit.mtx.RLock()
			totalPending += deposit.tx.Amount
			deposit.mtx.RUnlock()
		}
	}

	for _, pendingDEXOrder := range u.pendingDEXOrders {
		if pendingDEXOrder.counterTradeRate == 0 {
			continue
		}
		if pendingDEXOrder.counterTradeAsset() != assetID {
			continue
		}
		reserved := reservedForCounterTrade(pendingDEXOrder)
		totalAvailableDiff -= int64(reserved)
		totalReserved += reserved
	}

	available := u.baseCexBalances[assetID] + totalAvailableDiff
	if available < 0 {
		u.log.Errorf("negative CEX balance for %s: %d", dex.BipIDSymbol(assetID), available)
		available = 0
	}

	return &BotBalance{
		Available: uint64(available),
		Locked:    totalFundingOrder,
		Pending:   totalPending,
		Reserved:  totalReserved,
	}
}

// CEXBalance returns the balance of the bot on the CEX.
func (u *unifiedExchangeAdaptor) CEXBalance(assetID uint32) *BotBalance {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	return u.cexBalance(assetID)
}

func (u *unifiedExchangeAdaptor) balanceState() *BalanceState {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	fromAsset, toAsset, fromFeeAsset, toFeeAsset := orderAssets(u.baseID, u.quoteID, true)

	balances := make(map[uint32]*BotBalance, 4)
	assets := []uint32{fromAsset, toAsset}
	if fromFeeAsset != fromAsset {
		assets = append(assets, fromFeeAsset)
	}
	if toFeeAsset != toAsset {
		assets = append(assets, toFeeAsset)
	}

	for _, assetID := range assets {
		dexBal := u.dexBalance(assetID)
		cexBal := u.cexBalance(assetID)
		balances[assetID] = &BotBalance{
			Available: dexBal.Available + cexBal.Available,
			Locked:    dexBal.Locked + cexBal.Locked,
			Pending:   dexBal.Pending + cexBal.Pending,
		}
	}

	mods := make(map[uint32]int64, len(u.inventoryMods))
	for assetID, mod := range u.inventoryMods {
		mods[assetID] = mod
	}

	return &BalanceState{
		FiatRates:     u.fiatRates.Load().(map[uint32]float64),
		Balances:      balances,
		InventoryMods: mods,
	}
}

func (u *unifiedExchangeAdaptor) pendingDepositComplete(deposit *pendingDeposit) {
	deposit.mtx.RLock()
	tx := deposit.tx
	assetID := deposit.assetID
	amtCredited := deposit.amtCredited
	deposit.mtx.RUnlock()

	u.balancesMtx.Lock()
	if _, found := u.pendingDeposits[tx.ID]; !found {
		u.balancesMtx.Unlock()
		return
	}

	delete(u.pendingDeposits, tx.ID)
	u.baseDexBalances[assetID] -= int64(tx.Amount)
	var feeAssetID uint32
	token := asset.TokenInfo(assetID)
	if token != nil {
		feeAssetID = token.ParentID
	} else {
		feeAssetID = assetID
	}
	u.baseDexBalances[feeAssetID] -= int64(tx.Fees)
	u.baseCexBalances[assetID] += int64(amtCredited)
	u.balancesMtx.Unlock()

	var diff int64
	if tx.Amount > amtCredited {
		diff = int64(tx.Amount - amtCredited)
	}
	if assetID == u.baseID {
		u.runStats.baseBalanceDelta.Add(-diff)
		u.runStats.baseFees.Add(tx.Fees)
	} else {
		u.runStats.quoteBalanceDelta.Add(-diff)
		u.runStats.quoteFees.Add(tx.Fees)
	}

	if assetID == u.baseID {
		u.pendingBaseRebalance.Store(false)
	} else {
		u.pendingQuoteRebalance.Store(false)
	}

	dexDiffs := map[uint32]int64{}
	cexDiffs := map[uint32]int64{}
	dexDiffs[assetID] -= int64(tx.Amount)
	dexDiffs[feeAssetID] -= int64(tx.Fees)
	cexDiffs[assetID] += int64(amtCredited)

	var msg string
	if amtCredited > 0 {
		msg = fmt.Sprintf("Deposit %s complete.", tx.ID)
	} else {
		msg = fmt.Sprintf("Deposit %s complete, but not successful.", tx.ID)
	}

	u.sendStatsUpdate()
	u.logBalanceAdjustments(dexDiffs, cexDiffs, msg)
}

func (u *unifiedExchangeAdaptor) confirmDeposit(ctx context.Context, txID string) bool {
	u.balancesMtx.RLock()
	deposit, found := u.pendingDeposits[txID]
	u.balancesMtx.RUnlock()
	if !found {
		return true
	}

	var updated bool
	defer func() {
		if updated {
			u.updateDepositEvent(deposit)
		}
	}()

	deposit.mtx.RLock()
	cexConfirmed, feeConfirmed := deposit.cexConfirmed, deposit.feeConfirmed
	deposit.mtx.RUnlock()

	if !cexConfirmed {
		complete, amtCredited := u.CEX.ConfirmDeposit(ctx, &libxc.DepositData{
			AssetID:            deposit.assetID,
			TxID:               txID,
			AmountConventional: deposit.amtConventional,
		})

		deposit.mtx.Lock()
		deposit.amtCredited = amtCredited
		if complete {
			updated = true
			cexConfirmed = true
			deposit.cexConfirmed = true
		}
		deposit.mtx.Unlock()
	}

	if !feeConfirmed {
		tx, err := u.clientCore.WalletTransaction(deposit.assetID, txID)
		if err != nil {
			u.log.Errorf("Error confirming deposit: %v", err)
			return false
		}

		deposit.mtx.Lock()
		deposit.tx = tx
		if tx.Confirmed {
			feeConfirmed = true
			deposit.feeConfirmed = true
			updated = true
		}
		deposit.mtx.Unlock()
	}

	if feeConfirmed && cexConfirmed {
		u.pendingDepositComplete(deposit)
		return true
	}

	return false
}

// deposit deposits funds to the CEX. The deposited funds will be removed from
// the bot's wallet balance and allocated to the bot's CEX balance. After both
// the fees of the deposit transaction are confirmed by the wallet and the
// CEX confirms the amount they received, the onConfirm callback is called.
func (u *unifiedExchangeAdaptor) deposit(ctx context.Context, assetID uint32, amount uint64) error {
	balance := u.DEXBalance(assetID)
	// TODO: estimate fee and make sure we have enough to cover it.
	if balance.Available < amount {
		return fmt.Errorf("bot has insufficient balance to deposit %d. required: %v, have: %v", assetID, amount, balance.Available)
	}

	addr, err := u.CEX.GetDepositAddress(ctx, assetID)
	if err != nil {
		return err
	}
	coin, err := u.clientCore.Send([]byte{}, assetID, amount, addr, u.isWithdrawer(assetID))
	if err != nil {
		return err
	}

	if assetID == u.baseID {
		u.pendingBaseRebalance.Store(true)
	} else {
		u.pendingQuoteRebalance.Store(true)
	}

	tx, err := u.clientCore.WalletTransaction(assetID, coin.TxID())
	if err != nil {
		// If the wallet does not know about the transaction it just sent,
		// this is a serious bug in the wallet. Should Send be updated to
		// return asset.WalletTransaction?
		return err
	}

	u.log.Infof("Deposited %s. TxID = %s", u.fmtQty(assetID, amount), tx.ID)

	eventID := u.eventLogID.Add(1)
	ui, _ := asset.UnitInfo(assetID)
	deposit := &pendingDeposit{
		eventLogID:      eventID,
		timestamp:       time.Now().Unix(),
		tx:              tx,
		assetID:         assetID,
		feeConfirmed:    !u.isDynamicSwapper(assetID),
		amtConventional: float64(amount) / float64(ui.Conventional.ConversionFactor),
	}
	u.updateDepositEvent(deposit)

	u.balancesMtx.Lock()
	u.pendingDeposits[tx.ID] = deposit
	u.balancesMtx.Unlock()

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				if u.confirmDeposit(ctx, tx.ID) {
					return
				}
				timer = time.NewTimer(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// pendingWithdrawalComplete is called after a withdrawal has been confirmed.
// The amount received is applied to the base balance, and the withdrawal is
// removed from the pending map.
func (u *unifiedExchangeAdaptor) pendingWithdrawalComplete(id string, tx *asset.WalletTransaction) {
	u.balancesMtx.Lock()
	withdrawal, found := u.pendingWithdrawals[id]
	if !found {
		u.balancesMtx.Unlock()
		return
	}
	delete(u.pendingWithdrawals, id)

	if withdrawal.assetID == u.baseID {
		u.pendingBaseRebalance.Store(false)
	} else {
		u.pendingQuoteRebalance.Store(false)
	}

	u.baseCexBalances[withdrawal.assetID] -= int64(withdrawal.amtWithdrawn)
	u.baseDexBalances[withdrawal.assetID] += int64(tx.Amount)
	u.balancesMtx.Unlock()

	dexDiffs := map[uint32]int64{withdrawal.assetID: int64(tx.Amount)}
	cexDiffs := map[uint32]int64{withdrawal.assetID: -int64(withdrawal.amtWithdrawn)}
	if withdrawal.assetID == u.baseID {
		u.runStats.baseFees.Add(withdrawal.amtWithdrawn - tx.Amount)
	} else {
		u.runStats.quoteFees.Add(withdrawal.amtWithdrawn - tx.Amount)
	}

	u.updateWithdrawalEvent(id, withdrawal, tx)
	u.sendStatsUpdate()
	u.logBalanceAdjustments(dexDiffs, cexDiffs, fmt.Sprintf("Withdrawal %s complete.", id))
}

func (u *unifiedExchangeAdaptor) confirmWithdrawal(ctx context.Context, id string) bool {
	u.balancesMtx.RLock()
	withdrawal, found := u.pendingWithdrawals[id]
	u.balancesMtx.RUnlock()
	if !found {
		u.log.Errorf("Withdrawal %s not found among pending withdrawals", id)
		return false
	}

	withdrawal.txIDMtx.RLock()
	txID := withdrawal.txID
	withdrawal.txIDMtx.RUnlock()

	if txID == "" {
		var err error
		_, txID, err = u.CEX.ConfirmWithdrawal(ctx, id, withdrawal.assetID)
		if errors.Is(err, libxc.ErrWithdrawalPending) {
			return false
		}
		if err != nil {
			u.log.Errorf("Error confirming withdrawal: %v", err)
			return false
		}

		withdrawal.txIDMtx.Lock()
		withdrawal.txID = txID
		withdrawal.txIDMtx.Unlock()
	}

	tx, err := u.clientCore.WalletTransaction(withdrawal.assetID, txID)
	if errors.Is(err, asset.CoinNotFoundError) {
		u.log.Warnf("%s wallet does not know about withdrawal tx: %s", dex.BipIDSymbol(withdrawal.assetID), id)
		return false
	}
	if err != nil {
		u.log.Errorf("Error getting wallet transaction: %v", err)
		return false
	}

	if tx.Confirmed {
		u.pendingWithdrawalComplete(id, tx)
		return true
	}

	return false
}

// withdraw withdraws funds from the CEX. After withdrawing, the CEX is queried
// for the transaction ID. After the transaction ID is available, the wallet is
// queried for the amount received.
func (u *unifiedExchangeAdaptor) withdraw(ctx context.Context, assetID uint32, amount uint64) error {
	symbol := dex.BipIDSymbol(assetID)

	balance := u.CEXBalance(assetID)
	if balance.Available < amount {
		return fmt.Errorf("bot has insufficient balance to withdraw %s. required: %v, have: %v", symbol, amount, balance.Available)
	}

	addr, err := u.clientCore.NewDepositAddress(assetID)
	if err != nil {
		return err
	}

	u.balancesMtx.Lock()
	withdrawalID, err := u.CEX.Withdraw(ctx, assetID, amount, addr)
	if err != nil {
		return err
	}
	u.log.Infof("Withdrew %s", u.fmtQty(assetID, amount))
	if assetID == u.baseID {
		u.pendingBaseRebalance.Store(true)
	} else {
		u.pendingQuoteRebalance.Store(true)
	}
	u.pendingWithdrawals[withdrawalID] = &pendingWithdrawal{
		eventLogID:   u.eventLogID.Add(1),
		timestamp:    time.Now().Unix(),
		assetID:      assetID,
		amtWithdrawn: amount,
		withdrawalID: withdrawalID,
	}
	u.balancesMtx.Unlock()

	u.updateWithdrawalEvent(withdrawalID, u.pendingWithdrawals[withdrawalID], nil)
	u.sendStatsUpdate()

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				if u.confirmWithdrawal(ctx, withdrawalID) {
					// TODO: Trigger a rebalance here somehow. Same with
					// confirmed deposit. Maybe confirmWithdrawal should be
					// checked as part of the rebalance sequence instead of in
					// a goroutine.
					return
				}
				timer = time.NewTimer(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (u *unifiedExchangeAdaptor) reversePriorityOrders(sell bool) []*dexOrderState {
	orderMap := u.groupedBookedOrders(sell)
	orderGroups := utils.MapItems(orderMap)

	sort.Slice(orderGroups, func(i, j int) bool {
		return orderGroups[i][0].placementIndex > orderGroups[j][0].placementIndex
	})
	orders := make([]*dexOrderState, 0, len(orderGroups))
	for _, g := range orderGroups {
		// Order the group by smallest order first.
		states := make([]*dexOrderState, len(g))
		for i, o := range g {
			states[i] = o.currentState()
		}
		sort.Slice(states, func(i, j int) bool {
			return (states[i].order.Qty - states[i].order.Filled) < (states[j].order.Qty - states[j].order.Filled)
		})
		orders = append(orders, states...)
	}
	return orders
}

// freeUpFunds identifies cancelable orders to free up funds for a proposed
// transfer. Identified orders are sorted in reverse order of priority. For
// orders with the same placement index, smaller orders are first. minToFree
// specifies a minimum amount of funds to liberate. pruneMatchableTo is the
// counter-asset quantity for some amount of cex balance, and we'll continue to
// add cancel orders until we're not over-matching. freeUpFunds does not
// actually cancel any orders. It just identifies orders that can be canceled
// immediately to satisfy the conditions specified.
func (u *unifiedExchangeAdaptor) freeUpFunds(
	assetID uint32,
	minToFree uint64,
	pruneMatchableTo uint64,
	currEpoch uint64,
) ([]*dexOrderState, bool) {

	orders := u.reversePriorityOrders(assetID == u.baseID)
	var matchableCounterQty, freeable, persistentMatchable uint64
	for _, o := range orders {
		var matchable uint64
		if assetID == o.order.BaseID {
			matchable += calc.BaseToQuote(o.counterTradeRate, o.order.Qty)
		} else {
			matchable += o.order.Qty
		}
		matchableCounterQty += matchable
		if currEpoch-o.order.Epoch >= 2 {
			freeable += o.balanceEffects.locked[assetID]
		} else {
			persistentMatchable += matchable
		}
	}

	if freeable < minToFree {
		return nil, false
	}
	if persistentMatchable > pruneMatchableTo {
		return nil, false
	}

	if minToFree == 0 && matchableCounterQty <= pruneMatchableTo {
		return nil, true
	}

	amtFreedByCancellingOrder := func(o *dexOrderState) (locked, counterQty uint64) {
		if assetID == o.order.BaseID {
			return o.balanceEffects.locked[assetID], calc.BaseToQuote(o.counterTradeRate, o.order.Qty)
		}
		return o.balanceEffects.locked[assetID], o.order.Qty
	}

	unfreed := minToFree
	cancels := make([]*dexOrderState, 0, len(orders))
	for _, o := range orders {
		if currEpoch-o.order.Epoch < 2 {
			continue
		}
		cancels = append(cancels, o)
		freed, counterQty := amtFreedByCancellingOrder(o)
		if freed >= unfreed {
			unfreed = 0
		} else {
			unfreed -= freed
		}
		matchableCounterQty -= counterQty
		if matchableCounterQty <= pruneMatchableTo && unfreed == 0 {
			break
		}
	}
	return cancels, true
}

// handleCEXTradeUpdate handles a trade update from the CEX. If the trade is in
// the pending map, it will be updated. If the trade is complete, the base balances
// will be updated.
func (u *unifiedExchangeAdaptor) handleCEXTradeUpdate(trade *libxc.Trade) {
	var currCEXOrder *pendingCEXOrder
	defer func() {
		if currCEXOrder != nil {
			u.updateCEXOrderEvent(trade, currCEXOrder.eventLogID, currCEXOrder.timestamp)
			u.sendStatsUpdate()
		}
	}()

	u.balancesMtx.Lock()
	currCEXOrder, found := u.pendingCEXOrders[trade.ID]
	u.balancesMtx.Unlock()
	if !found {
		return
	}

	if !trade.Complete {
		currCEXOrder.tradeMtx.Lock()
		currCEXOrder.trade = trade
		currCEXOrder.tradeMtx.Unlock()
		return
	}

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()
	delete(u.pendingCEXOrders, trade.ID)

	if trade.BaseFilled == 0 && trade.QuoteFilled == 0 {
		u.log.Infof("CEX trade %s completed with zero filled amount", trade.ID)
		return
	}

	diffs := make(map[uint32]int64)

	if trade.Sell {
		u.baseCexBalances[trade.BaseID] -= int64(trade.BaseFilled)
		u.baseCexBalances[trade.QuoteID] += int64(trade.QuoteFilled)
		u.runStats.baseBalanceDelta.Add(-int64(trade.BaseFilled))
		u.runStats.quoteBalanceDelta.Add(int64(trade.QuoteFilled))
		diffs[trade.BaseID] = -int64(trade.BaseFilled)
		diffs[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		u.baseCexBalances[trade.QuoteID] -= int64(trade.QuoteFilled)
		u.baseCexBalances[trade.BaseID] += int64(trade.BaseFilled)
		u.runStats.baseBalanceDelta.Add(int64(trade.BaseFilled))
		u.runStats.quoteBalanceDelta.Add(-int64(trade.QuoteFilled))
		diffs[trade.QuoteID] = -int64(trade.QuoteFilled)
		diffs[trade.BaseID] = int64(trade.BaseFilled)
	}

	u.logBalanceAdjustments(nil, diffs, fmt.Sprintf("CEX trade %s completed.", trade.ID))
}

// SubscribeTradeUpdates subscribes to trade updates for the bot's trades on
// the CEX. This should be called before making any trades, and only once.
func (w *unifiedExchangeAdaptor) SubscribeTradeUpdates() <-chan *libxc.Trade {
	w.subscriptionIDMtx.Lock()
	defer w.subscriptionIDMtx.Unlock()
	if w.subscriptionID != nil {
		w.log.Errorf("SubscribeTradeUpdates called more than once by bot %s", w.botID)
		return nil
	}

	updates, unsubscribe, subscriptionID := w.CEX.SubscribeTradeUpdates()
	w.subscriptionID = &subscriptionID

	forwardUpdates := make(chan *libxc.Trade, 256)
	go func() {
		for {
			select {
			case <-w.ctx.Done():
				unsubscribe()
				return
			case note := <-updates:
				w.handleCEXTradeUpdate(note)
				select {
				case forwardUpdates <- note:
				default:
					w.log.Errorf("CEX trade update channel full")
				}
			}
		}
	}()

	return forwardUpdates
}

// Trade executes a trade on the CEX. The trade will be executed using the
// bot's CEX balance.
func (u *unifiedExchangeAdaptor) CEXTrade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error) {
	sufficient, err := u.SufficientBalanceForCEXTrade(baseID, quoteID, sell, rate, qty)
	if err != nil {
		return nil, err
	}
	if !sufficient {
		return nil, fmt.Errorf("insufficient balance")
	}

	u.subscriptionIDMtx.RLock()
	subscriptionID := u.subscriptionID
	u.subscriptionIDMtx.RUnlock()
	if subscriptionID == nil {
		return nil, fmt.Errorf("trade called before SubscribeTradeUpdates")
	}

	var trade *libxc.Trade
	now := time.Now().Unix()
	eventID := u.eventLogID.Add(1)
	defer func() {
		if trade != nil {
			u.updateCEXOrderEvent(trade, eventID, now)
			u.sendStatsUpdate()
		}
	}()

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	trade, err = u.CEX.Trade(ctx, baseID, quoteID, sell, rate, qty, *subscriptionID)
	if err != nil {
		return nil, err
	}

	if trade.Complete {
		diffs := make(map[uint32]int64)
		if trade.Sell {
			u.baseCexBalances[trade.BaseID] -= int64(trade.BaseFilled)
			u.baseCexBalances[trade.QuoteID] += int64(trade.QuoteFilled)
			diffs[trade.BaseID] = -int64(trade.BaseFilled)
			diffs[trade.QuoteID] = int64(trade.QuoteFilled)
		} else {
			u.baseCexBalances[trade.BaseID] += int64(trade.BaseFilled)
			u.baseCexBalances[trade.QuoteID] -= int64(trade.QuoteFilled)
			diffs[trade.BaseID] = int64(trade.BaseFilled)
			diffs[trade.QuoteID] = -int64(trade.QuoteFilled)
		}
		u.logBalanceAdjustments(nil, diffs, fmt.Sprintf("CEX trade %s completed.", trade.ID))
	} else {
		u.pendingCEXOrders[trade.ID] = &pendingCEXOrder{
			trade:      trade,
			eventLogID: eventID,
			timestamp:  now,
		}
	}

	return trade, nil
}

func (u *unifiedExchangeAdaptor) fiatRate(assetID uint32) float64 {
	rates := u.fiatRates.Load()
	if rates == nil {
		return 0
	}

	return rates.(map[uint32]float64)[assetID]
}

// ExchangeRateFromFiatSources returns market's exchange rate using fiat sources.
func (u *unifiedExchangeAdaptor) ExchangeRateFromFiatSources() uint64 {
	atomicCFactor, err := u.atomicConversionRateFromFiat(u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("Error genrating atomic conversion rate: %v", err)
		return 0
	}
	return uint64(math.Round(atomicCFactor * calc.RateEncodingFactor))
}

// atomicConversionRateFromFiat generates a conversion rate suitable for
// converting from atomic units of one asset to atomic units of another.
// This is the same as a message-rate, but without the RateEncodingFactor,
// hence a float.
func (u *unifiedExchangeAdaptor) atomicConversionRateFromFiat(fromID, toID uint32) (float64, error) {
	fromRate := u.fiatRate(fromID)
	toRate := u.fiatRate(toID)
	if fromRate == 0 || toRate == 0 {
		return 0, fmt.Errorf("missing fiat rate. rate for %d = %f, rate for %d = %f", fromID, fromRate, toID, toRate)
	}

	fromUI, err := asset.UnitInfo(fromID)
	if err != nil {
		return 0, fmt.Errorf("exchangeRates from asset %d not found", fromID)
	}
	toUI, err := asset.UnitInfo(toID)
	if err != nil {
		return 0, fmt.Errorf("exchangeRates to asset %d not found", toID)
	}

	// v_to_atomic = v_from_atomic / from_conv_factor * convConversionRate / to_conv_factor
	return 1 / float64(fromUI.Conventional.ConversionFactor) * fromRate / toRate * float64(toUI.Conventional.ConversionFactor), nil
}

// OrderFees returns the fees for a buy and sell order. The order fees are for
// placing orders on the market specified by the exchangeAdaptorCfg used to
// create the unifiedExchangeAdaptor.
func (u *unifiedExchangeAdaptor) orderFees() (buyFees, sellFees *orderFees, err error) {
	u.feesMtx.RLock()
	defer u.feesMtx.RUnlock()

	if u.buyFees == nil || u.sellFees == nil {
		return nil, nil, fmt.Errorf("order fees not available")
	}

	return u.buyFees, u.sellFees, nil
}

// OrderFeesInUnits returns the estimated swap and redemption fees for either a
// buy or sell order in units of either the base or quote asset. If either the
// base or quote asset is a token, the fees are converted using fiat rates.
// Otherwise, the rate parameter is used for the conversion.
func (u *unifiedExchangeAdaptor) OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error) {
	buyFeeRange, sellFeeRange, err := u.orderFees()
	if err != nil {
		return 0, fmt.Errorf("error getting order fees: %v", err)
	}
	buyFees, sellFees := buyFeeRange.Estimated, sellFeeRange.Estimated
	baseFees, quoteFees := buyFees.Redeem, buyFees.Swap
	if sell {
		baseFees, quoteFees = sellFees.Swap, sellFees.Redeem
	}

	convertViaFiat := func(fees uint64, fromID, toID uint32) (uint64, error) {
		atomicCFactor, err := u.atomicConversionRateFromFiat(fromID, toID)
		if err != nil {
			return 0, err
		}
		return uint64(math.Round(float64(fees) * atomicCFactor)), nil
	}

	var baseFeesInUnits, quoteFeesInUnits uint64
	if tkn := asset.TokenInfo(u.baseID); tkn != nil {
		baseFees, err = convertViaFiat(baseFees, tkn.ParentID, u.baseID)
		if err != nil {
			return 0, err
		}
	}
	if tkn := asset.TokenInfo(u.quoteID); tkn != nil {
		quoteFees, err = convertViaFiat(quoteFees, tkn.ParentID, u.quoteID)
		if err != nil {
			return 0, err
		}
	}

	if base {
		baseFeesInUnits = baseFees
	} else {
		baseFeesInUnits = calc.BaseToQuote(rate, baseFees)
	}

	if base {
		quoteFeesInUnits = calc.QuoteToBase(rate, quoteFees)
	} else {
		quoteFeesInUnits = quoteFees
	}

	return baseFeesInUnits + quoteFeesInUnits, nil
}

func (u *unifiedExchangeAdaptor) cancelAllOrders(ctx context.Context) {
	doCancels := func(epoch *uint64, dexOrderBooked func(oid order.OrderID, sell bool) bool) bool {
		u.balancesMtx.RLock()
		defer u.balancesMtx.RUnlock()

		done := true

		freeCancel := func(orderEpoch uint64) bool {
			if epoch == nil {
				return true
			}
			return *epoch-orderEpoch >= 2
		}

		for _, pendingOrder := range u.pendingDEXOrders {
			o := pendingOrder.currentState().order

			var oid order.OrderID
			copy(oid[:], o.ID)
			// We need to look in the order book to see if the cancel succeeded
			// because the epoch summary note comes before the order statuses
			// are updated.
			if !dexOrderBooked(oid, o.Sell) {
				continue
			}

			done = false
			if freeCancel(o.Epoch) {
				err := u.clientCore.Cancel(o.ID)
				if err != nil {
					u.log.Errorf("Error canceling order %s: %v", o.ID, err)
				}
			}
		}

		for _, pendingOrder := range u.pendingCEXOrders {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			tradeStatus, err := u.CEX.TradeStatus(ctx, pendingOrder.trade.ID, pendingOrder.trade.BaseID, pendingOrder.trade.QuoteID)
			if err != nil {
				u.log.Errorf("Error getting CEX trade status: %v", err)
				continue
			}
			if tradeStatus.Complete {
				continue
			}

			done = false
			err = u.CEX.CancelTrade(ctx, u.baseID, u.quoteID, pendingOrder.trade.ID)
			if err != nil {
				u.log.Errorf("Error canceling CEX trade %s: %v", pendingOrder.trade.ID, err)
			}
		}

		return done
	}

	// Use this in case the order book is not available.
	orderAlwaysBooked := func(_ order.OrderID, _ bool) bool {
		return true
	}

	book, bookFeed, err := u.clientCore.SyncBook(u.host, u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("Error syncing book for cancellations: %v", err)
		doCancels(nil, orderAlwaysBooked)
		return
	}

	mktCfg, err := u.clientCore.ExchangeMarket(u.host, u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("Error getting market configuration: %v", err)
		doCancels(nil, orderAlwaysBooked)
		return
	}

	currentEpoch := book.CurrentEpoch()
	if doCancels(&currentEpoch, book.OrderIsBooked) {
		return
	}

	i := 0

	timeout := time.Millisecond * time.Duration(3*mktCfg.EpochLen)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case n := <-bookFeed.Next():
			if n.Action == core.EpochMatchSummary {
				payload := n.Payload.(*core.EpochMatchSummaryPayload)
				currentEpoch := payload.Epoch + 1
				if doCancels(&currentEpoch, book.OrderIsBooked) {
					return
				}
				timer.Reset(timeout)
				i++
			}
		case <-timer.C:
			doCancels(nil, orderAlwaysBooked)
			return
		}

		if i >= 3 {
			return
		}
	}
}

// SubscribeOrderUpdates returns a channel that sends updates for orders placed
// on the DEX. This function should be called only once.
func (u *unifiedExchangeAdaptor) SubscribeOrderUpdates() <-chan *core.Order {
	orderUpdates := make(chan *core.Order, 128)
	u.orderUpdates.Store(orderUpdates)
	return orderUpdates
}

// isAccountLocker returns if the asset's wallet is an asset.AccountLocker.
func (u *unifiedExchangeAdaptor) isAccountLocker(assetID uint32) bool {
	if assetID == u.baseID {
		return u.baseTraits.IsAccountLocker()
	}
	return u.quoteTraits.IsAccountLocker()
}

// isDynamicSwapper returns if the asset's wallet is an asset.DynamicSwapper.
func (u *unifiedExchangeAdaptor) isDynamicSwapper(assetID uint32) bool {
	if assetID == u.baseID {
		return u.baseTraits.IsDynamicSwapper()
	}
	return u.quoteTraits.IsDynamicSwapper()
}

// isWithdrawer returns if the asset's wallet is an asset.Withdrawer.
func (u *unifiedExchangeAdaptor) isWithdrawer(assetID uint32) bool {
	if assetID == u.baseID {
		return u.baseTraits.IsWithdrawer()
	}
	return u.quoteTraits.IsWithdrawer()
}

func orderAssets(baseID, quoteID uint32, sell bool) (fromAsset, fromFeeAsset, toAsset, toFeeAsset uint32) {
	if sell {
		fromAsset = baseID
		toAsset = quoteID
	} else {
		fromAsset = quoteID
		toAsset = baseID
	}
	if token := asset.TokenInfo(fromAsset); token != nil {
		fromFeeAsset = token.ParentID
	} else {
		fromFeeAsset = fromAsset
	}
	if token := asset.TokenInfo(toAsset); token != nil {
		toFeeAsset = token.ParentID
	} else {
		toFeeAsset = toAsset
	}
	return
}

func feeAssetID(assetID uint32) uint32 {
	if token := asset.TokenInfo(assetID); token != nil {
		return token.ParentID
	}
	return assetID
}

func dexOrderComplete(o *core.Order) bool {
	if o.Status.IsActive() {
		return false
	}

	for _, match := range o.Matches {
		if match.Active {
			return false
		}
	}

	return o.AllFeesConfirmed
}

// orderTransactions returns all of the swap, redeem, and refund transactions
// involving a dex order.
func orderTransactions(o *core.Order) (swaps map[string]bool, redeems map[string]bool, refunds map[string]bool) {
	swaps = make(map[string]bool)
	redeems = make(map[string]bool)
	refunds = make(map[string]bool)

	for _, match := range o.Matches {
		if match.Swap != nil {
			swaps[match.Swap.ID.String()] = true
		}
		if match.Redeem != nil {
			redeems[match.Redeem.ID.String()] = true
		}
		if match.Refund != nil {
			refunds[match.Refund.ID.String()] = true
		}
	}

	return
}

func (u *unifiedExchangeAdaptor) checkPendingDEXOrderTxs(pendingOrder *pendingDEXOrder, o *core.Order) (effects *balanceEffects) {
	effects = newBalanceEffects()
	swaps, redeems, refunds := orderTransactions(o)
	// Add new txs to tx cache
	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(o.BaseID, o.QuoteID, o.Sell)
	processTxs := func(assetID uint32, m map[string]*asset.WalletTransaction, txs map[string]bool) {
		// Add new txs to tx cache
		for txID := range txs {
			if _, found := m[txID]; !found {
				m[txID] = &asset.WalletTransaction{}
			}
		}
		// Query the wallet regarding all unconfirmed transactions
		for txID, oldTx := range m {
			if oldTx.Confirmed {
				continue
			}
			tx, err := u.clientCore.WalletTransaction(assetID, txID)
			if err != nil {
				u.log.Errorf("Error getting tx %s: %v", txID, err)
				continue
			}
			m[txID] = tx
		}
	}

	processTxs(fromAsset, pendingOrder.swaps, swaps)
	processTxs(toAsset, pendingOrder.redeems, redeems)
	processTxs(fromAsset, pendingOrder.refunds, refunds)

	// Account for pending funds locked in swaps awaiting confirmation.
	for _, match := range o.Matches {
		if match.Swap == nil || match.Redeem != nil || match.Refund != nil {
			continue
		}

		if match.Revoked {
			swapTx, found := pendingOrder.swaps[match.Swap.ID.String()]
			if found {
				effects.pending[fromAsset] += swapTx.Amount
			}
		} else {
			var redeemAmt uint64
			if o.Sell {
				redeemAmt = calc.BaseToQuote(match.Rate, match.Qty)
			} else {
				redeemAmt = match.Qty
			}
			effects.pending[toAsset] += redeemAmt
		}
	}

	effects.locked[fromAsset] += o.LockedAmt
	effects.locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	effects.locked[toFeeAsset] += o.RedeemLockedAmt
	if o.FeesPaid != nil {
		effects.fees[fromFeeAsset] += o.FeesPaid.Funding
	}

	for _, tx := range pendingOrder.swaps {
		effects.settled[fromAsset] -= int64(tx.Amount)
		effects.fees[fromFeeAsset] += tx.Fees
	}

	// for _, m := range o.Matches {
	// 	if m.Side == order.Maker && m.Status == order.MakerSwapCast || m.Side == order.Taker && m.Status == order.TakerSwapCast {
	// 		if o.Sell {
	// 			// Could also choose the asset based on m.Revoked, but meh.
	// 			effects.pending[toAsset] += calc.BaseToQuote(m.Rate, m.Qty)
	// 		} else {
	// 			effects.pending[toAsset] += m.Qty
	// 		}
	// 	}
	// }

	for _, tx := range pendingOrder.redeems {
		effects.fees[toFeeAsset] += tx.Fees

		if tx.Confirmed {
			effects.settled[toAsset] += int64(tx.Amount)
		} else {
			effects.pending[toAsset] += tx.Amount
		}
	}
	for _, tx := range pendingOrder.refunds {
		effects.fees[fromFeeAsset] += tx.Fees

		if tx.Confirmed {
			effects.settled[fromAsset] += int64(tx.Amount)
		} else {
			effects.pending[fromAsset] += tx.Amount
		}
	}

	pendingOrder.updateState(o, effects, pendingOrder.counterTradeRate)
	return
}

// updatePendingDEXOrder updates a pending DEX order based its current state.
// If the order is complete, its effects are applied to the base balance,
// and it is removed from the pending list.
func (u *unifiedExchangeAdaptor) updatePendingDEXOrder(o *core.Order) {
	var orderID order.OrderID
	copy(orderID[:], o.ID)

	u.balancesMtx.RLock()
	pendingOrder, found := u.pendingDEXOrders[orderID]
	u.balancesMtx.RUnlock()
	if !found {
		return
	}

	pendingOrder.txsMtx.Lock()
	effects := u.checkPendingDEXOrderTxs(pendingOrder, o)
	var havePending bool
	for _, v := range effects.pending {
		if v > 0 {
			havePending = true
			break
		}
	}
	pendingOrder.txsMtx.Unlock()

	orderUpdates := u.orderUpdates.Load()
	if orderUpdates != nil {
		orderUpdates.(chan *core.Order) <- o
	}

	complete := !havePending && dexOrderComplete(o)
	// If complete, remove the order from the pending list, and update the
	// bot's balance.

	if complete { // TODO: complete when all fees are confirmed
		u.balancesMtx.Lock()
		delete(u.pendingDEXOrders, orderID)

		adjustedBals := false
		for assetID, diff := range effects.settled {
			adjustedBals = adjustedBals || diff != 0
			u.baseDexBalances[assetID] += diff
		}
		for assetID, fees := range effects.fees {
			adjustedBals = adjustedBals || fees != 0
			u.baseDexBalances[assetID] -= int64(fees)
		}

		u.runStats.baseBalanceDelta.Add(effects.settled[u.baseID])
		u.runStats.quoteBalanceDelta.Add(effects.settled[u.quoteID])
		u.runStats.baseFees.Add(effects.fees[u.baseFeeID])
		u.runStats.quoteFees.Add(effects.fees[u.quoteFeeID])

		if adjustedBals {
			u.logBalanceAdjustments(effects.settled, nil, fmt.Sprintf("DEX order %s complete.", orderID))
		}
		u.balancesMtx.Unlock()
	}

	u.updateDEXOrderEvent(pendingOrder, complete)
}

func (u *unifiedExchangeAdaptor) handleDEXNotification(n core.Notification) {
	switch note := n.(type) {
	case *core.OrderNote:
		u.updatePendingDEXOrder(note.Order)
	case *core.MatchNote:
		o, err := u.clientCore.Order(note.OrderID)
		if err != nil {
			u.log.Errorf("handleDEXNotification: failed to get order %s: %v", note.OrderID, err)
			return
		}
		u.updatePendingDEXOrder(o)
		cfg := u.botCfg()
		if cfg.Host != note.Host || u.mwh.ID() != note.MarketID {
			return
		}
		if note.Topic() == core.TopicRedemptionConfirmed {
			u.runStats.completedMatches.Add(1)
			fiatRates := u.fiatRates.Load().(map[uint32]float64)
			if r := fiatRates[cfg.BaseID]; r > 0 && note.Match != nil {
				ui, _ := asset.UnitInfo(cfg.BaseID)
				u.runStats.tradedUSD.Lock()
				u.runStats.tradedUSD.v += float64(note.Match.Qty) / float64(ui.Conventional.ConversionFactor) * r
				u.runStats.tradedUSD.Unlock()
			}
		}
	case *core.FiatRatesNote:
		u.fiatRates.Store(note.FiatRates)
	}
}

// Lot costs are the reserves and fees associated with current market rates. The
// per-lot reservations estimates include booking fees, and redemption fees if
// the asset is the counter-asset's parent asset. The quote estimates are based
// on vwap estimates using cexCounterRates,
type lotCosts struct {
	dexBase, dexQuote,
	cexBase, cexQuote uint64
	baseRedeem, quoteRedeem   uint64
	baseFunding, quoteFunding uint64 // per multi-order
}

func (u *unifiedExchangeAdaptor) lotCosts(sellVWAP, buyVWAP uint64) (*lotCosts, error) {
	perLot := new(lotCosts)
	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		return nil, fmt.Errorf("error getting order fees: %w", err)
	}
	perLot.dexBase = u.lotSize
	if u.baseID == u.baseFeeID {
		perLot.dexBase += sellFees.bookingFeesPerLot
	}
	perLot.cexBase = u.lotSize
	perLot.baseRedeem = buyFees.Max.Redeem
	perLot.baseFunding = sellFees.funding

	dexQuoteLot := calc.BaseToQuote(sellVWAP, u.lotSize)
	cexQuoteLot := calc.BaseToQuote(buyVWAP, u.lotSize)
	perLot.dexQuote = dexQuoteLot
	if u.quoteID == u.quoteFeeID {
		perLot.dexQuote += buyFees.bookingFeesPerLot
	}
	perLot.cexQuote = cexQuoteLot
	perLot.quoteRedeem = sellFees.Max.Redeem
	perLot.quoteFunding = buyFees.funding
	return perLot, nil
}

// distribution is a collection of asset distributions and per-lot estimates.
type distribution struct {
	baseInv  *assetInventory
	quoteInv *assetInventory
	perLot   *lotCosts
}

func (u *unifiedExchangeAdaptor) newDistribution(perLot *lotCosts) *distribution {
	return &distribution{
		baseInv:  u.inventory(u.baseID, perLot.dexBase, perLot.cexBase),
		quoteInv: u.inventory(u.quoteID, perLot.dexQuote, perLot.cexQuote),
		perLot:   perLot,
	}
}

// optimizeTransfers populates the toDeposit and toWithdraw fields of the base
// and quote assetDistribution. To find the best asset distribution, a series
// of possible target configurations are tested and the distribution that
// results in the highest matchability is chosen.
func (u *unifiedExchangeAdaptor) optimizeTransfers(dist *distribution, dexSellLots, dexBuyLots, maxSellLots, maxBuyLots uint64) {
	baseInv, quoteInv := dist.baseInv, dist.quoteInv
	perLot := dist.perLot

	if u.autoRebalanceCfg == nil {
		return
	}
	minBaseTransfer, minQuoteTransfer := u.autoRebalanceCfg.MinBaseTransfer, u.autoRebalanceCfg.MinQuoteTransfer

	additionalBaseFees, additionalQuoteFees := perLot.baseFunding, perLot.quoteFunding
	if u.baseID == u.quoteFeeID {
		additionalBaseFees += perLot.baseRedeem * dexBuyLots
	}
	if u.quoteID == u.baseFeeID {
		additionalQuoteFees += perLot.quoteRedeem * dexSellLots
	}
	var baseAvail, quoteAvail uint64
	if baseInv.total > additionalBaseFees {
		baseAvail = baseInv.total - additionalBaseFees
	}
	if quoteInv.total > additionalQuoteFees {
		quoteAvail = quoteInv.total - additionalQuoteFees
	}

	// matchability is the number of lots that can be matched with a specified
	// asset distribution.
	matchability := func(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots uint64) uint64 {
		sells := utils.Min(dexBaseLots, cexQuoteLots, maxSellLots)
		buys := utils.Min(dexQuoteLots, cexBaseLots, maxBuyLots)
		return buys + sells
	}

	currentScore := matchability(baseInv.dexLots, quoteInv.dexLots, baseInv.cexLots, quoteInv.cexLots)

	// targetedSplit finds a distribution that targets a specified ratio of
	// dex-to-cex.
	targetedSplit := func(avail, dexTarget, cexTarget, dexLot, cexLot uint64) (dexLots, cexLots, extra uint64) {
		if dexTarget+cexTarget == 0 {
			return
		}
		cexR := float64(cexTarget*cexLot) / float64(dexTarget*dexLot+cexTarget*cexLot)
		cexLots = uint64(math.Round(cexR*float64(avail))) / cexLot
		cexBal := cexLots * cexLot
		dexLots = (avail - cexBal) / dexLot
		dexBal := dexLots * dexLot
		if cexLot < dexLot {
			cexLots = (avail - dexBal) / cexLot
			cexBal = cexLots * cexLot
		}
		extra = avail - cexBal - dexBal
		return
	}

	baseSplit := func(dexTarget, cexTarget uint64) (dexLots, cexLots, extra uint64) {
		return targetedSplit(baseAvail, utils.Min(dexTarget, maxSellLots), utils.Min(cexTarget, maxBuyLots), perLot.dexBase, perLot.cexBase)
	}
	quoteSplit := func(dexTarget, cexTarget uint64) (dexLots, cexLots, extra uint64) {
		return targetedSplit(quoteAvail, utils.Min(dexTarget, maxBuyLots), utils.Min(cexTarget, maxSellLots), perLot.dexQuote, perLot.cexQuote)
	}

	// We'll keep track of any distributions that have a matchability score
	// better than the score for the current distribution.
	type scoredSplit struct {
		score uint64 // matchability
		// spread is just the minimum of baseDeposit, baseWithdraw, quoteDeposit
		// and quoteWithdraw. This is tertiary criteria for prioritizing splits,
		// with a higher spread being preferable.
		spread                      uint64
		fees                        uint64
		baseDeposit, baseWithdraw   uint64
		quoteDeposit, quoteWithdraw uint64
	}
	baseSplits := [][2]uint64{
		{baseInv.dex, baseInv.cex},   // current
		{dexSellLots, dexBuyLots},    // ideal
		{quoteInv.cex, quoteInv.dex}, // match the counter asset
	}
	quoteSplits := [][2]uint64{
		{quoteInv.dex, quoteInv.cex},
		{dexBuyLots, dexSellLots},
		{baseInv.cex, baseInv.dex},
	}

	splits := make([]*scoredSplit, 0)
	// scoreSplit gets a score for the proposed asset distribution and, if the
	// score is higher than currentScore, saves the result to the splits slice.
	scoreSplit := func(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots, extraBase, extraQuote uint64) {
		score := matchability(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots)
		if score <= currentScore {
			return
		}

		var fees uint64
		var baseDeposit, baseWithdraw, quoteDeposit, quoteWithdraw uint64
		if dexBaseLots != baseInv.dexLots || cexBaseLots != baseInv.cexLots {
			fees++
			dexTarget := dexBaseLots*perLot.dexBase + additionalBaseFees + extraBase
			cexTarget := cexBaseLots * perLot.cexBase

			if dexTarget > baseInv.dex {
				if withdraw := dexTarget - baseInv.dex; withdraw >= minBaseTransfer {
					baseWithdraw = withdraw
				} else {
					return
				}
			} else if cexTarget > baseInv.cex {
				if deposit := cexTarget - baseInv.cex; deposit >= minBaseTransfer {
					baseDeposit = deposit
				} else {
					return
				}
			}
			// TODO: Use actual fee estimates.
			if u.baseID == 0 || u.baseID == 42 {
				fees++
			}
		}
		if dexQuoteLots != quoteInv.dexLots || cexQuoteLots != quoteInv.cexLots {
			fees++
			dexTarget := dexQuoteLots*perLot.dexQuote + additionalQuoteFees + (extraQuote / 2)
			cexTarget := cexQuoteLots*perLot.cexQuote + (extraQuote / 2)
			if dexTarget > quoteInv.dex {
				if withdraw := dexTarget - quoteInv.dex; withdraw >= minQuoteTransfer {
					quoteWithdraw = withdraw
				} else {
					return
				}

			} else if cexTarget > quoteInv.cex {
				if deposit := cexTarget - quoteInv.cex; deposit >= minQuoteTransfer {
					quoteDeposit = deposit
				} else {
					return
				}
			}
			if u.quoteID == 0 || u.quoteID == 60 {
				fees++
			}
		}

		splits = append(splits, &scoredSplit{
			score:         score,
			fees:          fees,
			baseDeposit:   baseDeposit,
			baseWithdraw:  baseWithdraw,
			quoteDeposit:  quoteDeposit,
			quoteWithdraw: quoteWithdraw,
			spread:        utils.Min(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots),
		})
	}

	// Try to hit all possible combinations.
	for _, b := range baseSplits {
		dexBaseLots, cexBaseLots, extraBase := baseSplit(b[0], b[1])
		dexQuoteLots, cexQuoteLots, extraQuote := quoteSplit(cexBaseLots, dexBaseLots)
		scoreSplit(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots, extraBase, extraQuote)
		for _, q := range quoteSplits {
			dexQuoteLots, cexQuoteLots, extraQuote = quoteSplit(q[0], q[1])
			scoreSplit(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots, extraBase, extraQuote)
		}
	}
	// Try in both directions.
	for _, q := range quoteSplits {
		dexQuoteLots, cexQuoteLots, extraQuote := quoteSplit(q[0], q[1])
		dexBaseLots, cexBaseLots, extraBase := baseSplit(cexQuoteLots, dexQuoteLots)
		scoreSplit(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots, extraBase, extraQuote)
		for _, b := range baseSplits {
			dexBaseLots, cexBaseLots, extraBase := baseSplit(b[0], b[1])
			scoreSplit(dexBaseLots, dexQuoteLots, cexBaseLots, cexQuoteLots, extraBase, extraQuote)
		}
	}

	if len(splits) == 0 {
		return
	}

	// Sort by score, then fees, then spread.
	sort.Slice(splits, func(ii, ji int) bool {
		i, j := splits[ii], splits[ji]
		return i.score > j.score || (i.score == j.score && (i.fees < j.fees || i.spread > j.spread))
	})
	split := splits[0]
	baseInv.toDeposit = split.baseDeposit
	baseInv.toWithdraw = split.baseWithdraw
	quoteInv.toDeposit = split.quoteDeposit
	quoteInv.toWithdraw = split.quoteWithdraw
}

// transfer attempts to perform the transers specified in the distribution.
func (u *unifiedExchangeAdaptor) transfer(dist *distribution, currEpoch uint64) (actionTaken bool, err error) {
	baseInv, quoteInv := dist.baseInv, dist.quoteInv
	if baseInv.toDeposit+baseInv.toWithdraw+quoteInv.toDeposit+quoteInv.toWithdraw == 0 {
		return false, nil
	}

	var cancels []*dexOrderState
	if baseInv.toDeposit > 0 || quoteInv.toWithdraw > 0 {
		var toFree uint64
		if baseInv.dexAvail < baseInv.toDeposit {
			if baseInv.dexAvail+baseInv.dexPending >= baseInv.toDeposit {
				u.log.Tracef("Waiting on pending balance for base deposit")
				return false, nil
			}
			toFree = baseInv.toDeposit - baseInv.dexAvail
		}
		counterQty := quoteInv.cex - quoteInv.toWithdraw + quoteInv.toDeposit
		cs, ok := u.freeUpFunds(u.baseID, toFree, counterQty, currEpoch)
		if !ok {
			if u.log.Level() == dex.LevelTrace {
				u.log.Tracef(
					"Unable to free up funds for deposit = %s, withdraw = %s, "+
						"counter-quantity = %s, to free = %s, dex pending = %s",
					u.fmtBase(baseInv.toDeposit), u.fmtQuote(quoteInv.toWithdraw), u.fmtQuote(counterQty),
					u.fmtBase(toFree), u.fmtBase(baseInv.dexPending),
				)
			}
			return false, nil
		}
		cancels = cs
	}
	if quoteInv.toDeposit > 0 || baseInv.toWithdraw > 0 {
		var toFree uint64
		if quoteInv.dexAvail < quoteInv.toDeposit {
			if quoteInv.dexAvail+quoteInv.dexPending >= quoteInv.toDeposit {
				// waiting on pending
				u.log.Tracef("Waiting on pending balance for quote deposit")
				return false, nil
			}
			toFree = quoteInv.toDeposit - quoteInv.dexAvail
		}
		counterQty := baseInv.cex - baseInv.toWithdraw + baseInv.toDeposit
		cs, ok := u.freeUpFunds(u.quoteID, toFree, counterQty, currEpoch)
		if !ok {
			if u.log.Level() == dex.LevelTrace {
				u.log.Tracef(
					"Unable to free up funds for deposit = %s, withdraw = %s, "+
						"counter-quantity = %s, to free = %s, dex pending = %s",
					u.fmtQuote(quoteInv.toDeposit), u.fmtBase(baseInv.toWithdraw), u.fmtBase(counterQty),
					u.fmtQuote(toFree), u.fmtQuote(quoteInv.dexPending),
				)
			}
			return false, nil
		}
		cancels = append(cancels, cs...)
	}

	if len(cancels) > 0 {
		for _, o := range cancels {
			if err := u.Cancel(o.order.ID); err != nil {
				return false, fmt.Errorf("error canceling order: %w", err)
			}
		}
		return true, nil
	}

	if baseInv.toDeposit > 0 {
		if err := u.deposit(u.ctx, u.baseID, baseInv.toDeposit); err != nil {
			return false, fmt.Errorf("error depositing base: %w", err)
		}
	} else if baseInv.toWithdraw > 0 {
		if err := u.withdraw(u.ctx, u.baseID, baseInv.toWithdraw); err != nil {
			return false, fmt.Errorf("error withdrawing base: %w", err)
		}
	}

	if quoteInv.toDeposit > 0 {
		if err := u.deposit(u.ctx, u.quoteID, quoteInv.toDeposit); err != nil {
			return false, fmt.Errorf("error depositing quote: %w", err)
		}
	} else if quoteInv.toWithdraw > 0 {
		if err := u.withdraw(u.ctx, u.quoteID, quoteInv.toWithdraw); err != nil {
			return false, fmt.Errorf("error withdrawing quote: %w", err)
		}
	}
	return true, nil
}

// assetInventory is an accounting of the distribution of base- or quote-asset
// funding.
type assetInventory struct {
	dex        uint64
	dexAvail   uint64
	dexPending uint64
	dexLocked  uint64
	dexLots    uint64

	cex         uint64
	cexAvail    uint64
	cexPending  uint64
	cexReserved uint64
	cexLocked   uint64
	cexLots     uint64

	total uint64

	toDeposit  uint64
	toWithdraw uint64
}

// inventory generates a current view of the the bot's asset distribution.
// Use optimizeTransfers to set toDeposit and toWithdraw.
func (u *unifiedExchangeAdaptor) inventory(assetID uint32, dexLot, cexLot uint64) (b *assetInventory) {
	b = new(assetInventory)
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	dexBalance := u.dexBalance(assetID)
	b.dexAvail = dexBalance.Available
	b.dexPending = dexBalance.Pending
	b.dexLocked = dexBalance.Locked
	b.dex = dexBalance.Available + dexBalance.Locked + dexBalance.Pending
	b.dexLots = b.dex / dexLot
	cexBalance := u.cexBalance(assetID)
	b.cexAvail = cexBalance.Available
	b.cexPending = cexBalance.Pending
	b.cexReserved = cexBalance.Reserved
	b.cexLocked = cexBalance.Locked
	b.cex = cexBalance.Available + cexBalance.Reserved + cexBalance.Pending
	b.cexLots = b.cex / cexLot
	b.total = b.dex + b.cex
	return
}

// cexCounterRates attempts to get vwap estimates for the cex book for a
// specified number of lots. If the book is too empty for the specified number
// of lots, a 1-lot estimate will be attempted too.
func (u *unifiedExchangeAdaptor) cexCounterRates(cexBuyLots, cexSellLots uint64) (dexBuyRate, dexSellRate uint64, err error) {
	tryLots := func(b, s uint64) (uint64, uint64, bool, error) {
		buyRate, _, filled, err := u.CEX.VWAP(u.baseID, u.quoteID, true, u.lotSize*s)
		if err != nil {
			return 0, 0, false, fmt.Errorf("error calculating dex buy price for quote conversion: %w", err)
		}
		if !filled {
			return 0, 0, false, nil
		}
		sellRate, _, filled, err := u.CEX.VWAP(u.baseID, u.quoteID, false, u.lotSize*b)
		if err != nil {
			return 0, 0, false, fmt.Errorf("error calculating dex sell price for quote conversion: %w", err)
		}
		if !filled {
			return 0, 0, false, nil
		}
		return buyRate, sellRate, true, nil
	}
	var filled bool
	if dexBuyRate, dexSellRate, filled, err = tryLots(cexBuyLots, cexSellLots); err != nil || filled {
		return
	}
	u.log.Tracef("Failed to get cex counter-rate for requested lots. Trying 1 lot estimate")
	dexBuyRate, dexSellRate, filled, err = tryLots(1, 1)
	if err != nil {
		return
	}
	if !filled {
		err = errors.New("cex book to empty to get a counter-rate estimate")
	}
	return
}

// bookingFees are the per-lot fees that have to be available before placing an
// order.
func (u *unifiedExchangeAdaptor) bookingFees(buyFees, sellFees *LotFees) (buyBookingFeesPerLot, sellBookingFeesPerLot uint64) {
	buyBookingFeesPerLot = buyFees.Swap
	// If we're redeeming on the same chain, add redemption fees.
	if u.quoteFeeID == u.baseFeeID {
		buyBookingFeesPerLot += buyFees.Redeem
	}
	// EVM assets need to reserve refund gas.
	if u.quoteTraits.IsAccountLocker() {
		buyBookingFeesPerLot += buyFees.Refund
	}
	sellBookingFeesPerLot = sellFees.Swap
	if u.baseFeeID == u.quoteFeeID {
		sellBookingFeesPerLot += sellFees.Redeem
	}
	if u.baseTraits.IsAccountLocker() {
		sellBookingFeesPerLot += sellFees.Refund
	}
	return
}

// updateFeeRates updates the cached fee rates for placing orders on the market
// specified by the exchangeAdaptorCfg used to create the unifiedExchangeAdaptor.
func (u *unifiedExchangeAdaptor) updateFeeRates() error {
	maxBaseFees, maxQuoteFees, err := marketFees(u.clientCore, u.host, u.baseID, u.quoteID, true)
	if err != nil {
		return err
	}

	estBaseFees, estQuoteFees, err := marketFees(u.clientCore, u.host, u.baseID, u.quoteID, false)
	if err != nil {
		return err
	}

	botCfg := u.botCfg()
	maxBuyPlacements, maxSellPlacements := botCfg.maxPlacements()

	buyFundingFees, err := u.clientCore.MaxFundingFees(u.quoteID, u.host, maxBuyPlacements, botCfg.QuoteWalletOptions)
	if err != nil {
		return fmt.Errorf("failed to get buy funding fees: %v", err)
	}

	sellFundingFees, err := u.clientCore.MaxFundingFees(u.baseID, u.host, maxSellPlacements, botCfg.BaseWalletOptions)
	if err != nil {
		return fmt.Errorf("failed to get sell funding fees: %v", err)
	}

	maxBuyFees := &LotFees{
		Swap:   maxQuoteFees.Swap,
		Redeem: maxBaseFees.Redeem,
		Refund: maxQuoteFees.Refund,
	}
	maxSellFees := &LotFees{
		Swap:   maxBaseFees.Swap,
		Redeem: maxQuoteFees.Redeem,
		Refund: maxBaseFees.Refund,
	}

	buyBookingFeesPerLot, sellBookingFeesPerLot := u.bookingFees(maxBuyFees, maxSellFees)

	u.feesMtx.Lock()
	defer u.feesMtx.Unlock()

	u.buyFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: maxBuyFees,
			Estimated: &LotFees{
				Swap:   estQuoteFees.Swap,
				Redeem: estBaseFees.Redeem,
				Refund: estQuoteFees.Refund,
			},
		},
		funding:           buyFundingFees,
		bookingFeesPerLot: buyBookingFeesPerLot,
	}
	u.sellFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: maxSellFees,
			Estimated: &LotFees{
				Swap:   estBaseFees.Swap,
				Redeem: estQuoteFees.Redeem,
				Refund: estBaseFees.Refund,
			},
		},
		funding:           sellFundingFees,
		bookingFeesPerLot: sellBookingFeesPerLot,
	}

	return nil
}

func (u *unifiedExchangeAdaptor) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	u.ctx = ctx
	fiatRates := u.clientCore.FiatConversionRates()
	u.fiatRates.Store(fiatRates)

	err := u.updateFeeRates()
	if err != nil {
		u.log.Errorf("Error updating fee rates: %v", err)
	}

	startTime := time.Now().Unix()
	u.startTime.Store(startTime)

	err = u.eventLogDB.storeNewRun(startTime, u.mwh, u.botCfg(), u.balanceState())
	if err != nil {
		return nil, fmt.Errorf("failed to store new run in event log db: %v", err)
	}

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		<-ctx.Done()
		u.eventLogDB.endRun(startTime, u.mwh, time.Now().Unix())
	}()

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		<-ctx.Done()
		u.cancelAllOrders(ctx)
	}()

	// Listen for core notifications
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		feed := u.clientCore.NotificationFeed()
		defer feed.ReturnFeed()

		for {
			select {
			case <-ctx.Done():
				return
			case n := <-feed.C:
				u.handleDEXNotification(n)
			}
		}
	}()

	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		refreshTime := time.Minute * 10
		for {
			select {
			case <-time.NewTimer(refreshTime).C:
				err := u.updateFeeRates()
				if err != nil {
					u.log.Error(err)
					refreshTime = time.Minute
				} else {
					refreshTime = time.Minute * 10
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := u.runBotLoop(ctx); err != nil {
		return nil, fmt.Errorf("error starting bot loop: %w", err)
	}

	u.sendStatsUpdate()

	return &u.wg, nil
}

// RunStats is a snapshot of the bot's balances and performance at a point in
// time.
type RunStats struct {
	InitialBalances    map[uint32]uint64      `json:"initialBalances"`
	DEXBalances        map[uint32]*BotBalance `json:"dexBalances"`
	CEXBalances        map[uint32]*BotBalance `json:"cexBalances"`
	ProfitLoss         *ProfitLoss            `json:"profitLoss"`
	StartTime          int64                  `json:"startTime"`
	PendingDeposits    int                    `json:"pendingDeposits"`
	PendingWithdrawals int                    `json:"pendingWithdrawals"`
	CompletedMatches   uint32                 `json:"completedMatches"`
	TradedUSD          float64                `json:"tradedUSD"`
	FeeGap             *FeeGapStats           `json:"feeGap"`
}

// Amount contains the conversions and formatted strings associated with an
// amount of asset and a fiat exchange rate.
type Amount struct {
	Atoms        int64   `json:"atoms"`
	Conventional float64 `json:"conventional"`
	Fmt          string  `json:"fmt"`
	USD          float64 `json:"usd"`
	FmtUSD       string  `json:"fmtUSD"`
	FiatRate     float64 `json:"fiatRate"`
}

// NewAmount generates an Amount for a known asset.
func NewAmount(assetID uint32, atoms int64, fiatRate float64) *Amount {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return &Amount{}
	}
	conv := float64(atoms) / float64(ui.Conventional.ConversionFactor)
	usd := conv * fiatRate
	return &Amount{
		Atoms:        atoms,
		Conventional: conv,
		USD:          usd,
		Fmt:          ui.FormatSignedAtoms(atoms),
		FmtUSD:       strconv.FormatFloat(usd, 'f', 2, 64) + " USD",
		FiatRate:     fiatRate,
	}
}

// ProfitLoss is a breakdown of the profit calculations.
type ProfitLoss struct {
	Initial     map[uint32]*Amount `json:"initial"`
	InitialUSD  float64            `json:"initialUSD"`
	Mods        map[uint32]*Amount `json:"mods"`
	ModsUSD     float64            `json:"modsUSD"`
	Final       map[uint32]*Amount `json:"final"`
	FinalUSD    float64            `json:"finalUSD"`
	Profit      float64            `json:"profit"`
	ProfitRatio float64            `json:"profitRatio"`
}

func newProfitLoss(
	initialBalances,
	finalBalances map[uint32]uint64,
	mods map[uint32]int64,
	fiatRates map[uint32]float64,
) *ProfitLoss {

	pl := &ProfitLoss{
		Initial: make(map[uint32]*Amount, len(initialBalances)),
		Mods:    make(map[uint32]*Amount, len(mods)),
		Final:   make(map[uint32]*Amount, len(finalBalances)),
	}
	for assetID, v := range initialBalances {
		if v == 0 {
			continue
		}
		fiatRate := fiatRates[assetID]
		init := NewAmount(assetID, int64(v), fiatRate)
		pl.Initial[assetID] = init
		mod := NewAmount(assetID, mods[assetID], fiatRate)
		pl.InitialUSD += init.USD
		pl.ModsUSD += mod.USD
	}
	for assetID, v := range finalBalances {
		if v == 0 {
			continue
		}
		fin := NewAmount(assetID, int64(v), fiatRates[assetID])
		pl.Final[assetID] = fin
		pl.FinalUSD += fin.USD
	}

	basis := pl.InitialUSD + pl.ModsUSD
	pl.Profit = pl.FinalUSD - basis
	pl.ProfitRatio = pl.Profit / basis
	return pl
}

func (u *unifiedExchangeAdaptor) stats() *RunStats {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	dexBalances := make(map[uint32]*BotBalance)
	cexBalances := make(map[uint32]*BotBalance)
	totalBalances := make(map[uint32]uint64)

	for assetID := range u.baseDexBalances {
		bal := u.dexBalance(assetID)
		dexBalances[assetID] = bal
		totalBalances[assetID] = bal.Available + bal.Locked + bal.Pending + bal.Reserved
	}

	for assetID := range u.baseCexBalances {
		bal := u.cexBalance(assetID)
		cexBalances[assetID] = bal
		totalBalances[assetID] += bal.Available + bal.Locked + bal.Pending + bal.Reserved
	}

	fiatRates := u.fiatRates.Load().(map[uint32]float64)

	var feeGap *FeeGapStats
	if feeGapI := u.runStats.feeGapStats.Load(); feeGapI != nil {
		feeGap = feeGapI.(*FeeGapStats)
	}

	u.runStats.tradedUSD.Lock()
	tradedUSD := u.runStats.tradedUSD.v
	u.runStats.tradedUSD.Unlock()

	// Effects of pendingWithdrawals are applied when the withdrawal is
	// complete.
	return &RunStats{
		InitialBalances:    u.initialBalances,
		DEXBalances:        dexBalances,
		CEXBalances:        cexBalances,
		ProfitLoss:         newProfitLoss(u.initialBalances, totalBalances, u.inventoryMods, fiatRates),
		StartTime:          u.startTime.Load(),
		PendingDeposits:    len(u.pendingDeposits),
		PendingWithdrawals: len(u.pendingWithdrawals),
		CompletedMatches:   u.runStats.completedMatches.Load(),
		TradedUSD:          tradedUSD,
		FeeGap:             feeGap,
	}
}

func (u *unifiedExchangeAdaptor) sendStatsUpdate() {
	u.clientCore.Broadcast(newRunStatsNote(u.host, u.baseID, u.quoteID, u.stats()))
}

func (u *unifiedExchangeAdaptor) notifyEvent(e *MarketMakingEvent) {
	u.clientCore.Broadcast(newRunEventNote(u.host, u.baseID, u.quoteID, u.startTime.Load(), e))
}

func (u *unifiedExchangeAdaptor) registerFeeGap(feeGap *FeeGapStats) {
	u.runStats.feeGapStats.Store(feeGap)
}

func (u *unifiedExchangeAdaptor) applyInventoryDiffs(balanceDiffs *BotInventoryDiffs) map[uint32]int64 {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	mods := map[uint32]int64{}

	for assetID, diff := range balanceDiffs.DEX {
		if diff < 0 {
			balance := u.dexBalance(assetID)
			if balance.Available < uint64(-diff) {
				u.log.Errorf("attempting to decrease %s balance by more than available balance. Setting balance to 0.", dex.BipIDSymbol(assetID))
				diff = -int64(balance.Available)
			}
		}
		u.baseDexBalances[assetID] += diff
		mods[assetID] = diff
	}

	for assetID, diff := range balanceDiffs.CEX {
		if diff < 0 {
			balance := u.cexBalance(assetID)
			if balance.Available < uint64(-diff) {
				u.log.Errorf("attempting to decrease %s balance by more than available balance. Setting balance to 0.", dex.BipIDSymbol(assetID))
				diff = -int64(balance.Available)
			}
		}
		u.baseCexBalances[assetID] += diff
		mods[assetID] += diff
	}

	for assetID, diff := range mods {
		u.inventoryMods[assetID] += diff
	}

	u.logBalanceAdjustments(balanceDiffs.DEX, balanceDiffs.CEX, "Inventory updated")
	u.log.Debugf("Aggregate inventory mods: %+v", u.inventoryMods)

	return mods
}

func (u *unifiedExchangeAdaptor) updateConfig(cfg *BotConfig) {
	u.botCfgV.Store(cfg)
	u.updateConfigEvent(cfg)
}

func (u *unifiedExchangeAdaptor) updateInventory(balanceDiffs *BotInventoryDiffs) {
	u.updateInventoryEvent(u.applyInventoryDiffs(balanceDiffs))
}

func (u *unifiedExchangeAdaptor) Book() (buys, sells []*core.MiniOrder, _ error) {
	if u.CEX == nil {
		return nil, nil, errors.New("not a cex-connected bot")
	}
	return u.CEX.Book(u.baseID, u.quoteID)
}

type exchangeAdaptorCfg struct {
	botID               string
	mwh                 *MarketWithHost
	baseDexBalances     map[uint32]uint64
	baseCexBalances     map[uint32]uint64
	autoRebalanceConfig *AutoRebalanceConfig
	core                clientCore
	cex                 libxc.CEX
	log                 dex.Logger
	eventLogDB          eventLogDB
	botCfg              *BotConfig
}

// newUnifiedExchangeAdaptor is the constructor for a unifiedExchangeAdaptor.
func newUnifiedExchangeAdaptor(cfg *exchangeAdaptorCfg) (*unifiedExchangeAdaptor, error) {
	initialBalances := make(map[uint32]uint64, len(cfg.baseDexBalances))
	for assetID, balance := range cfg.baseDexBalances {
		initialBalances[assetID] = balance
	}
	for assetID, balance := range cfg.baseCexBalances {
		initialBalances[assetID] += balance
	}

	baseDEXBalances := make(map[uint32]int64, len(cfg.baseDexBalances))
	for assetID, balance := range cfg.baseDexBalances {
		baseDEXBalances[assetID] = int64(balance)
	}
	baseCEXBalances := make(map[uint32]int64, len(cfg.baseCexBalances))
	for assetID, balance := range cfg.baseCexBalances {
		baseCEXBalances[assetID] = int64(balance)
	}

	coreMkt, err := cfg.core.ExchangeMarket(cfg.mwh.Host, cfg.mwh.BaseID, cfg.mwh.QuoteID)
	if err != nil {
		return nil, err
	}

	mkt, err := parseMarket(cfg.mwh.Host, coreMkt)
	if err != nil {
		return nil, err
	}

	baseTraits, err := cfg.core.WalletTraits(mkt.baseID)
	if err != nil {
		return nil, fmt.Errorf("wallet trait error for base asset %d", mkt.baseID)
	}
	quoteTraits, err := cfg.core.WalletTraits(mkt.quoteID)
	if err != nil {
		return nil, fmt.Errorf("wallet trait error for quote asset %d", mkt.quoteID)
	}

	adaptor := &unifiedExchangeAdaptor{
		market:           mkt,
		clientCore:       cfg.core,
		CEX:              cfg.cex,
		botID:            cfg.botID,
		log:              cfg.log,
		eventLogDB:       cfg.eventLogDB,
		initialBalances:  initialBalances,
		baseTraits:       baseTraits,
		quoteTraits:      quoteTraits,
		autoRebalanceCfg: cfg.autoRebalanceConfig,

		baseDexBalances:    baseDEXBalances,
		baseCexBalances:    baseCEXBalances,
		pendingDEXOrders:   make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:   make(map[string]*pendingCEXOrder),
		pendingDeposits:    make(map[string]*pendingDeposit),
		pendingWithdrawals: make(map[string]*pendingWithdrawal),
		mwh:                cfg.mwh,
		inventoryMods:      make(map[uint32]int64),
	}

	adaptor.fiatRates.Store(map[uint32]float64{})
	adaptor.botCfgV.Store(cfg.botCfg)

	return adaptor, nil
}
