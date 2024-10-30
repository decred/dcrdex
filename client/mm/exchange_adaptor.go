// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/json"
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

func (b *BotBalance) copy() *BotBalance {
	return &BotBalance{
		Available: b.Available,
		Locked:    b.Locked,
		Pending:   b.Pending,
		Reserved:  b.Reserved,
	}
}

// OrderFees represents the fees that will be required for a single lot of a
// dex order.
type OrderFees struct {
	*LotFeeRange
	Funding uint64 `json:"funding"`
	// bookingFeesPerLot is the amount of fee asset that needs to be reserved
	// for fees, per ordered lot. For all assets, this will include
	// LotFeeRange.Max.Swap. For non-token EVM assets (eth, matic) Max.Refund
	// will be added. If the asset is the parent chain of a token counter-asset,
	// Max.Redeem is added. This is a commonly needed sum in various validation
	// and optimization functions.
	BookingFeesPerLot uint64 `json:"bookingFeesPerLot"`
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
	SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, map[uint32]uint64, error)
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
	SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, map[uint32]uint64)
	MidGap(baseID, quoteID uint32) uint64
	Book() (buys, sells []*core.MiniOrder, _ error)
}

// BalanceEffects represents the effects that a market making event has on
// the bot's balances.
type BalanceEffects struct {
	Settled  map[uint32]int64  `json:"settled"`
	Locked   map[uint32]uint64 `json:"locked"`
	Pending  map[uint32]uint64 `json:"pending"`
	Reserved map[uint32]uint64 `json:"reserved"`
}

func newBalanceEffects() *BalanceEffects {
	return &BalanceEffects{
		Settled:  make(map[uint32]int64),
		Locked:   make(map[uint32]uint64),
		Pending:  make(map[uint32]uint64),
		Reserved: make(map[uint32]uint64),
	}
}

type balanceEffectsDiff struct {
	settled  map[uint32]int64
	locked   map[uint32]int64
	pending  map[uint32]int64
	reserved map[uint32]int64
}

func newBalanceEffectsDiff() *balanceEffectsDiff {
	return &balanceEffectsDiff{
		settled:  make(map[uint32]int64),
		locked:   make(map[uint32]int64),
		pending:  make(map[uint32]int64),
		reserved: make(map[uint32]int64),
	}
}

func (b *BalanceEffects) sub(other *BalanceEffects) *balanceEffectsDiff {
	res := newBalanceEffectsDiff()

	for assetID, v := range b.Settled {
		res.settled[assetID] = v
	}
	for assetID, v := range b.Locked {
		res.locked[assetID] = int64(v)
	}
	for assetID, v := range b.Pending {
		res.pending[assetID] = int64(v)
	}
	for assetID, v := range b.Reserved {
		res.reserved[assetID] = int64(v)
	}

	for assetID, v := range other.Settled {
		res.settled[assetID] -= v
	}
	for assetID, v := range other.Locked {
		res.locked[assetID] -= int64(v)
	}
	for assetID, v := range other.Pending {
		res.pending[assetID] -= int64(v)
	}
	for assetID, v := range other.Reserved {
		res.reserved[assetID] -= int64(v)
	}

	return res
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

	txMtx sync.RWMutex
	txID  string
	tx    *asset.WalletTransaction
}

func withdrawalBalanceEffects(tx *asset.WalletTransaction, cexDebit uint64, assetID uint32) (dex, cex *BalanceEffects) {
	dex = newBalanceEffects()
	cex = newBalanceEffects()

	cex.Settled[assetID] = -int64(cexDebit)

	if tx != nil {
		if tx.Confirmed {
			dex.Settled[assetID] += int64(tx.Amount)
		} else {
			dex.Pending[assetID] += tx.Amount
		}
	} else {
		dex.Pending[assetID] += cexDebit
	}

	return
}

func (w *pendingWithdrawal) balanceEffects() (dex, cex *BalanceEffects) {
	w.txMtx.RLock()
	defer w.txMtx.RUnlock()

	return withdrawalBalanceEffects(w.tx, w.amtWithdrawn, w.assetID)
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

func depositBalanceEffects(assetID uint32, tx *asset.WalletTransaction, cexConfirmed bool) (dex, cex *BalanceEffects) {
	feeAsset := assetID
	token := asset.TokenInfo(assetID)
	if token != nil {
		feeAsset = token.ParentID
	}

	dex, cex = newBalanceEffects(), newBalanceEffects()

	dex.Settled[assetID] -= int64(tx.Amount)
	dex.Settled[feeAsset] -= int64(tx.Fees)

	if cexConfirmed {
		cex.Settled[assetID] += int64(tx.Amount)
	} else {
		cex.Pending[assetID] += tx.Amount
	}

	return dex, cex
}

func (d *pendingDeposit) balanceEffects() (dex, cex *BalanceEffects) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()

	return depositBalanceEffects(d.assetID, d.tx, d.cexConfirmed)
}

type dexOrderState struct {
	dexBalanceEffects *BalanceEffects
	cexBalanceEffects *BalanceEffects
	order             *core.Order
	counterTradeRate  uint64
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

func (p *pendingDEXOrder) cexBalanceEffects() *BalanceEffects {
	return p.currentState().cexBalanceEffects
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
	buyFees  *OrderFees
	sellFees *OrderFees

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
		completedMatches atomic.Uint32
		tradedUSD        struct {
			sync.Mutex
			v float64
		}
		feeGapStats atomic.Value
	}

	epochReport atomic.Value // *EpochReport

	cexProblemsMtx sync.RWMutex
	cexProblems    *CEXProblems
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

// logBalanceAdjustments logs a trace log of balance adjustments and updated
// settled balances.
//
// balancesMtx must be read locked when calling this function.
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
func (u *unifiedExchangeAdaptor) SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, map[uint32]uint64, error) {
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
		return false, nil, err
	}

	reqBals := make(map[uint32]uint64)

	// Funding Fees
	fees, fundingFees := buyFees.Max, buyFees.Funding
	if sell {
		fees, fundingFees = sellFees.Max, sellFees.Funding
	}
	reqBals[fromFeeAsset] += fundingFees

	// Trade Qty
	fromQty := qty
	if !sell {
		fromQty = calc.BaseToQuote(rate, qty)
	}
	reqBals[fromAsset] += fromQty

	// Swap Fees
	numLots := qty / u.lotSize
	reqBals[fromFeeAsset] += numLots * fees.Swap

	// Refund Fees
	if u.isAccountLocker(fromAsset) {
		reqBals[fromFeeAsset] += numLots * fees.Refund
	}

	// Redeem Fees
	if u.isAccountLocker(toAsset) {
		reqBals[toFeeAsset] += numLots * fees.Redeem
	}

	sufficient := true
	deficiencies := make(map[uint32]uint64)

	for assetID, reqBal := range reqBals {
		if bal, found := balances[assetID]; found && bal >= reqBal {
			continue
		} else {
			deficiencies[assetID] = reqBal - bal
			sufficient = false
		}
	}

	return sufficient, deficiencies, nil
}

// SufficientBalanceOnCEXTrade returns whether the bot has sufficient balance
// to place a CEX trade.
func (u *unifiedExchangeAdaptor) SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, map[uint32]uint64) {
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

	if fromAssetBal.Available < fromAssetQty {
		return false, map[uint32]uint64{fromAssetID: fromAssetQty - fromAssetBal.Available}
	}

	return true, nil
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
	o.txsMtx.RLock()
	transactions := make([]*asset.WalletTransaction, 0, len(o.swaps)+len(o.redeems)+len(o.refunds))
	addTxs := func(txs map[string]*asset.WalletTransaction) {
		for _, tx := range txs {
			transactions = append(transactions, tx)
		}
	}
	addTxs(o.swaps)
	addTxs(o.redeems)
	addTxs(o.refunds)
	state := o.currentState()
	o.txsMtx.RUnlock()

	e := &MarketMakingEvent{
		ID:             o.eventLogID,
		TimeStamp:      o.timestamp,
		Pending:        !complete,
		BalanceEffects: combineBalanceEffects(state.dexBalanceEffects, state.cexBalanceEffects),
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

func cexOrderEvent(trade *libxc.Trade, eventID uint64, timestamp int64) *MarketMakingEvent {
	return &MarketMakingEvent{
		ID:             eventID,
		TimeStamp:      timestamp,
		Pending:        !trade.Complete,
		BalanceEffects: cexTradeBalanceEffects(trade),
		CEXOrderEvent: &CEXOrderEvent{
			ID:          trade.ID,
			Rate:        trade.Rate,
			Qty:         trade.Qty,
			Sell:        trade.Sell,
			BaseFilled:  trade.BaseFilled,
			QuoteFilled: trade.QuoteFilled,
		},
	}
}

// updateCEXOrderEvent updates the event log with the current state of a
// pending CEX order and sends an event notification.
func (u *unifiedExchangeAdaptor) updateCEXOrderEvent(trade *libxc.Trade, eventID uint64, timestamp int64) {
	event := cexOrderEvent(trade, eventID, timestamp)
	u.eventLogDB.storeEvent(u.startTime.Load(), u.mwh, event, u.balanceState())
	u.notifyEvent(event)
}

// updateDepositEvent updates the event log with the current state of a
// pending deposit and sends an event notification.
func (u *unifiedExchangeAdaptor) updateDepositEvent(deposit *pendingDeposit) {
	deposit.mtx.RLock()
	e := &MarketMakingEvent{
		ID:             deposit.eventLogID,
		TimeStamp:      deposit.timestamp,
		BalanceEffects: combineBalanceEffects(deposit.balanceEffects()),
		Pending:        !deposit.cexConfirmed || !deposit.feeConfirmed,
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

func combineBalanceEffects(dex, cex *BalanceEffects) *BalanceEffects {
	effects := newBalanceEffects()
	for assetID, v := range dex.Settled {
		effects.Settled[assetID] += v
	}
	for assetID, v := range dex.Locked {
		effects.Locked[assetID] += v
	}
	for assetID, v := range dex.Pending {
		effects.Pending[assetID] += v
	}
	for assetID, v := range dex.Reserved {
		effects.Reserved[assetID] += v
	}

	for assetID, v := range cex.Settled {
		effects.Settled[assetID] += v
	}
	for assetID, v := range cex.Locked {
		effects.Locked[assetID] += v
	}
	for assetID, v := range cex.Pending {
		effects.Pending[assetID] += v
	}
	for assetID, v := range cex.Reserved {
		effects.Reserved[assetID] += v
	}

	return effects

}

// updateWithdrawalEvent updates the event log with the current state of a
// pending withdrawal and sends an event notification.
func (u *unifiedExchangeAdaptor) updateWithdrawalEvent(withdrawal *pendingWithdrawal, tx *asset.WalletTransaction) {
	complete := tx != nil && tx.Confirmed
	e := &MarketMakingEvent{
		ID:             withdrawal.eventLogID,
		TimeStamp:      withdrawal.timestamp,
		BalanceEffects: combineBalanceEffects(withdrawal.balanceEffects()),
		Pending:        !complete,
		WithdrawalEvent: &WithdrawalEvent{
			AssetID:     withdrawal.assetID,
			ID:          withdrawal.withdrawalID,
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
func reservedForCounterTrade(sellOnDEX bool, counterTradeRate, remainingQty uint64) uint64 {
	if counterTradeRate == 0 {
		return 0
	}

	if sellOnDEX {
		return calc.BaseToQuote(counterTradeRate, remainingQty)
	}

	return remainingQty
}

func withinTolerance(rate, target uint64, driftTolerance float64) bool {
	tolerance := uint64(float64(target) * driftTolerance)
	lowerBound := target - tolerance
	upperBound := target + tolerance
	return rate >= lowerBound && rate <= upperBound
}

func (u *unifiedExchangeAdaptor) placeMultiTrade(placements []*dexOrderInfo, sell bool) []*core.MultiTradeResult {
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

	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(u.baseID, u.quoteID, sell)
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

	results := u.clientCore.MultiTrade([]byte{}, multiTradeForm)

	if len(placements) != len(results) {
		u.log.Errorf("unexpected number of results. expected %d, got %d", len(placements), len(results))
		return results
	}

	for i, res := range results {
		if res.Error != nil {
			continue
		}

		o := res.Order
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		dexEffects, cexEffects := newBalanceEffects(), newBalanceEffects()

		dexEffects.Settled[fromAsset] -= int64(o.LockedAmt)
		dexEffects.Settled[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
		dexEffects.Settled[toFeeAsset] -= int64(o.RedeemLockedAmt)

		dexEffects.Locked[fromAsset] += o.LockedAmt
		dexEffects.Locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
		dexEffects.Locked[toFeeAsset] += o.RedeemLockedAmt

		if o.FeesPaid != nil && o.FeesPaid.Funding > 0 {
			dexEffects.Settled[fromFeeAsset] -= int64(o.FeesPaid.Funding)
		}

		reserved := reservedForCounterTrade(o.Sell, placements[i].counterTradeRate, o.Qty)
		cexEffects.Settled[toAsset] -= int64(reserved)
		cexEffects.Reserved[toAsset] = reserved

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
				order:             o,
				dexBalanceEffects: dexEffects,
				cexBalanceEffects: cexEffects,
				counterTradeRate:  pendingOrder.counterTradeRate,
			})
		u.pendingDEXOrders[orderID] = pendingOrder
		newPendingDEXOrders = append(newPendingDEXOrders, u.pendingDEXOrders[orderID])
	}

	return results
}

// TradePlacement represents a placement to be made on a DEX order book
// using the MultiTrade function. A non-zero counterTradeRate indicates that
// the bot intends to make a counter-trade on a CEX when matches are made on
// the DEX, and this must be taken into consideration in combination with the
// bot's balance on the CEX when deciding how many lots to place. This
// information is also used when considering deposits and withdrawals.
type TradePlacement struct {
	Rate             uint64            `json:"rate"`
	Lots             uint64            `json:"lots"`
	StandingLots     uint64            `json:"standingLots"`
	OrderedLots      uint64            `json:"orderedLots"`
	CounterTradeRate uint64            `json:"counterTradeRate"`
	RequiredDEX      map[uint32]uint64 `json:"requiredDex"`
	RequiredCEX      uint64            `json:"requiredCex"`
	UsedDEX          map[uint32]uint64 `json:"usedDex"`
	UsedCEX          uint64            `json:"usedCex"`
	Order            *core.Order       `json:"order"`
	Error            *BotProblems      `json:"error"`
}

func (tp *TradePlacement) setError(err error) {
	if err == nil {
		tp.Error = nil
		return
	}
	tp.OrderedLots = 0
	tp.UsedDEX = make(map[uint32]uint64)
	tp.UsedCEX = 0
	problems := &BotProblems{}
	updateBotProblemsBasedOnError(problems, err)
	tp.Error = problems
}

func (tp *TradePlacement) requiredLots() uint64 {
	if tp.Lots > tp.StandingLots {
		return tp.Lots - tp.StandingLots
	}
	return 0
}

// OrderReport summarizes the results of a MultiTrade operation.
type OrderReport struct {
	Placements       []*TradePlacement      `json:"placements"`
	Fees             *OrderFees             `json:"buyFees"`
	AvailableDEXBals map[uint32]*BotBalance `json:"availableDexBals"`
	RequiredDEXBals  map[uint32]uint64      `json:"requiredDexBals"`
	UsedDEXBals      map[uint32]uint64      `json:"usedDexBals"`
	RemainingDEXBals map[uint32]uint64      `json:"remainingDexBals"`
	AvailableCEXBal  *BotBalance            `json:"availableCexBal"`
	RequiredCEXBal   uint64                 `json:"requiredCexBal"`
	UsedCEXBal       uint64                 `json:"usedCexBal"`
	RemainingCEXBal  uint64                 `json:"remainingCexBal"`
	Error            *BotProblems           `json:"error"`
}

func (or *OrderReport) setError(err error) {
	if or.Error == nil {
		or.Error = &BotProblems{}
	}
	updateBotProblemsBasedOnError(or.Error, err)
}

func newOrderReport(placements []*TradePlacement) *OrderReport {
	for _, p := range placements {
		p.StandingLots = 0
		p.OrderedLots = 0
		p.RequiredDEX = make(map[uint32]uint64)
		p.UsedDEX = make(map[uint32]uint64)
		p.UsedCEX = 0
		p.Order = nil
		p.Error = nil
	}

	return &OrderReport{
		AvailableDEXBals: make(map[uint32]*BotBalance),
		RequiredDEXBals:  make(map[uint32]uint64),
		RemainingDEXBals: make(map[uint32]uint64),
		UsedDEXBals:      make(map[uint32]uint64),
		Placements:       placements,
	}
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
	placements []*TradePlacement,
	sell bool,
	driftTolerance float64,
	currEpoch uint64,
) (_ map[order.OrderID]*dexOrderInfo, or *OrderReport) {
	or = newOrderReport(placements)
	if len(placements) == 0 {
		return nil, or
	}

	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		or.setError(err)
		return nil, or
	}
	or.Fees = buyFees
	if sell {
		or.Fees = sellFees
	}

	fromID, fromFeeID, toID, toFeeID := orderAssets(u.baseID, u.quoteID, sell)
	fees, fundingFees := or.Fees.Max, or.Fees.Funding

	// First, determine the amount of balances the bot has available to place
	// DEX trades taking into account dexReserves.
	for _, assetID := range []uint32{fromID, fromFeeID, toID, toFeeID} {
		if _, found := or.RemainingDEXBals[assetID]; !found {
			or.AvailableDEXBals[assetID] = u.DEXBalance(assetID).copy()
			or.RemainingDEXBals[assetID] = or.AvailableDEXBals[assetID].Available
		}
	}

	// If the placements include a counterTradeRate, the CEX balance must also
	// be taken into account to determine how many trades can be placed.
	accountForCEXBal := placements[0].CounterTradeRate > 0
	if accountForCEXBal {
		or.AvailableCEXBal = u.CEXBalance(toID).copy()
		or.RemainingCEXBal = or.AvailableCEXBal.Available
	}

	cancels := make([]dex.Bytes, 0, len(placements))

	addCancel := func(o *core.Order) {
		if currEpoch-o.Epoch < 2 { // TODO: check epoch
			u.log.Debugf("multiTrade: skipping cancel not past free cancel threshold")
			return
		}
		cancels = append(cancels, o.ID)
	}

	// keptOrders is a list of pending orders that are not being cancelled, in
	// decreasing order of placementIndex. If the bot doesn't have enough
	// balance to place an order with a higher priority (lower placementIndex)
	// then the lower priority orders in this list will be cancelled.
	keptOrders := make([]*pendingDEXOrder, 0, len(placements))

	for _, groupedOrders := range u.groupedBookedOrders(sell) {
		for _, o := range groupedOrders {
			order := o.currentState().order
			if o.placementIndex >= uint64(len(or.Placements)) {
				// This will happen if there is a reconfig in which there are
				// now less placements than before.
				addCancel(order)
				continue
			}

			mustCancel := !withinTolerance(order.Rate, placements[o.placementIndex].Rate, driftTolerance)
			or.Placements[o.placementIndex].StandingLots += (order.Qty - order.Filled) / u.lotSize
			if or.Placements[o.placementIndex].StandingLots > or.Placements[o.placementIndex].Lots {
				mustCancel = true
			}

			if mustCancel {
				u.log.Tracef("%s cancel with order rate = %s, placement rate = %s, drift tolerance = %.4f%%",
					u.mwh, u.fmtRate(order.Rate), u.fmtRate(placements[o.placementIndex].Rate), driftTolerance*100,
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
			if or.RemainingDEXBals[assetID] < v {
				return false
			}
		}
		return or.RemainingCEXBal >= cexReq
	}

	orderInfos := make([]*dexOrderInfo, 0, len(or.Placements))

	// Calculate required balances for each placement and the total required.
	placementRequired := false
	for _, placement := range or.Placements {
		if placement.requiredLots() == 0 {
			continue
		}
		placementRequired = true
		dexReq, cexReq := fundingReq(placement.Rate, placement.requiredLots(), placement.CounterTradeRate)
		for assetID, v := range dexReq {
			placement.RequiredDEX[assetID] = v
			or.RequiredDEXBals[assetID] += v
		}
		placement.RequiredCEX = cexReq
		or.RequiredCEXBal += cexReq
	}
	if placementRequired {
		or.RequiredDEXBals[fromFeeID] += fundingFees
	}

	or.RemainingDEXBals[fromFeeID] = utils.SafeSub(or.RemainingDEXBals[fromFeeID], fundingFees)
	for i, placement := range or.Placements {
		if placement.requiredLots() == 0 {
			continue
		}

		if rateCausesSelfMatch(placement.Rate) {
			u.log.Warnf("multiTrade: rate %d causes self match. Placements should be farther from mid-gap.", placement.Rate)
			placement.Error = &BotProblems{CausesSelfMatch: true}
			continue
		}

		searchN := int(placement.requiredLots() + 1)
		lotsPlus1 := sort.Search(searchN, func(lotsi int) bool {
			return !canAffordLots(placement.Rate, uint64(lotsi), placement.CounterTradeRate)
		})

		var lotsToPlace uint64
		if lotsPlus1 > 1 {
			lotsToPlace = uint64(lotsPlus1) - 1
			placement.UsedDEX, placement.UsedCEX = fundingReq(placement.Rate, lotsToPlace, placement.CounterTradeRate)
			placement.OrderedLots = lotsToPlace
			for assetID, v := range placement.UsedDEX {
				or.RemainingDEXBals[assetID] -= v
				or.UsedDEXBals[assetID] += v
			}
			or.RemainingCEXBal -= placement.UsedCEX
			or.UsedCEXBal += placement.UsedCEX

			orderInfos = append(orderInfos, &dexOrderInfo{
				placementIndex:   uint64(i),
				counterTradeRate: placement.CounterTradeRate,
				placement: &core.QtyRate{
					Qty:  lotsToPlace * u.lotSize,
					Rate: placement.Rate,
				},
			})
		}

		// If there is insufficient balance to place a higher priority order,
		// cancel the lower priority orders.
		if lotsToPlace < placement.requiredLots() {
			u.log.Tracef("multiTrade(%s,%d) out of funds for more placements. %d of %d lots for rate %s",
				sellStr(sell), i, lotsToPlace, placement.requiredLots(), u.fmtRate(placement.Rate))
			for _, o := range keptOrders {
				if o.placementIndex > uint64(i) {
					order := o.currentState().order
					addCancel(order)
				}
			}

			break
		}
	}

	if len(orderInfos) > 0 {
		or.UsedDEXBals[fromFeeID] += fundingFees
	}

	for _, cancel := range cancels {
		if err := u.Cancel(cancel); err != nil {
			u.log.Errorf("multiTrade: error canceling order %s: %v", cancel, err)
		}
	}

	if len(orderInfos) > 0 {
		results := u.placeMultiTrade(orderInfos, sell)
		ordered := make(map[order.OrderID]*dexOrderInfo, len(placements))
		for i, res := range results {
			if res.Error != nil {
				or.Placements[orderInfos[i].placementIndex].setError(res.Error)
				continue
			}
			var orderID order.OrderID
			copy(orderID[:], res.Order.ID)
			ordered[orderID] = orderInfos[i]
		}

		return ordered, or
	}

	return nil, or
}

// DEXTrade places a single order on the DEX order book.
func (u *unifiedExchangeAdaptor) DEXTrade(rate, qty uint64, sell bool) (*core.Order, error) {
	enough, _, err := u.SufficientBalanceForDEXTrade(rate, qty, sell)
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
	results := u.placeMultiTrade(placements, sell)
	if len(results) == 0 {
		return nil, fmt.Errorf("no orders placed")
	}
	if results[0].Error != nil {
		return nil, results[0].Error
	}

	return results[0].Order, nil
}

type BotBalances struct {
	DEX *BotBalance `json:"dex"`
	CEX *BotBalance `json:"cex"`
}

// dexBalance must be called with the balancesMtx read locked.
func (u *unifiedExchangeAdaptor) dexBalance(assetID uint32) *BotBalance {
	bal, found := u.baseDexBalances[assetID]
	if !found {
		return &BotBalance{}
	}

	totalEffects := newBalanceEffects()
	addEffects := func(effects *BalanceEffects) {
		for assetID, v := range effects.Settled {
			totalEffects.Settled[assetID] += v
		}
		for assetID, v := range effects.Locked {
			totalEffects.Locked[assetID] += v
		}
		for assetID, v := range effects.Pending {
			totalEffects.Pending[assetID] += v
		}
		for assetID, v := range effects.Reserved {
			totalEffects.Reserved[assetID] += v
		}
	}

	for _, pendingOrder := range u.pendingDEXOrders {
		addEffects(pendingOrder.currentState().dexBalanceEffects)
	}

	for _, pendingDeposit := range u.pendingDeposits {
		dexEffects, _ := pendingDeposit.balanceEffects()
		addEffects(dexEffects)
	}

	for _, pendingWithdrawal := range u.pendingWithdrawals {
		dexEffects, _ := pendingWithdrawal.balanceEffects()
		addEffects(dexEffects)
	}

	availableBalance := bal + totalEffects.Settled[assetID]
	if availableBalance < 0 {
		u.log.Errorf("negative dex balance for %s: %d", dex.BipIDSymbol(assetID), availableBalance)
		availableBalance = 0
	}

	return &BotBalance{
		Available: uint64(availableBalance),
		Locked:    totalEffects.Locked[assetID],
		Pending:   totalEffects.Pending[assetID],
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
		pendingOrder.updateState(state.order, u.clientCore.WalletTransaction, u.baseTraits, u.quoteTraits)
		pendingOrder.txsMtx.Unlock()
	}

	for _, pendingDeposit := range pendingDeposits {
		pendingDeposit.mtx.RLock()
		id := pendingDeposit.tx.ID
		pendingDeposit.mtx.RUnlock()
		u.confirmDeposit(ctx, id)
	}

	for _, pendingWithdrawal := range pendingWithdrawals {
		u.confirmWithdrawal(ctx, pendingWithdrawal.withdrawalID)
	}

	for _, pendingOrder := range pendingCEXOrders {
		pendingOrder.tradeMtx.RLock()
		id, baseID, quoteID := pendingOrder.trade.ID, pendingOrder.trade.BaseID, pendingOrder.trade.QuoteID
		pendingOrder.tradeMtx.RUnlock()

		trade, err := u.CEX.TradeStatus(ctx, id, baseID, quoteID)
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
func cexTradeBalanceEffects(trade *libxc.Trade) (effects *BalanceEffects) {
	effects = newBalanceEffects()

	if trade.Complete {
		if trade.Sell {
			effects.Settled[trade.BaseID] = -int64(trade.BaseFilled)
			effects.Settled[trade.QuoteID] = int64(trade.QuoteFilled)
		} else {
			effects.Settled[trade.QuoteID] = -int64(trade.QuoteFilled)
			effects.Settled[trade.BaseID] = int64(trade.BaseFilled)
		}
		return
	}

	if trade.Sell {
		effects.Settled[trade.BaseID] = -int64(trade.Qty)
		effects.Locked[trade.BaseID] = trade.Qty - trade.BaseFilled
		effects.Settled[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		effects.Settled[trade.QuoteID] = -int64(calc.BaseToQuote(trade.Rate, trade.Qty))
		effects.Locked[trade.QuoteID] = calc.BaseToQuote(trade.Rate, trade.Qty) - trade.QuoteFilled
		effects.Settled[trade.BaseID] = int64(trade.BaseFilled)
	}

	return
}

// cexBalance must be called with the balancesMtx read locked.
func (u *unifiedExchangeAdaptor) cexBalance(assetID uint32) *BotBalance {
	totalEffects := newBalanceEffects()
	addEffects := func(effects *BalanceEffects) {
		for assetID, v := range effects.Settled {
			totalEffects.Settled[assetID] += v
		}
		for assetID, v := range effects.Locked {
			totalEffects.Locked[assetID] += v
		}
		for assetID, v := range effects.Pending {
			totalEffects.Pending[assetID] += v
		}
		for assetID, v := range effects.Reserved {
			totalEffects.Reserved[assetID] += v
		}
	}

	for _, pendingOrder := range u.pendingCEXOrders {
		pendingOrder.tradeMtx.RLock()
		trade := pendingOrder.trade
		pendingOrder.tradeMtx.RUnlock()

		addEffects(cexTradeBalanceEffects(trade))
	}

	for _, withdrawal := range u.pendingWithdrawals {
		_, cexEffects := withdrawal.balanceEffects()
		addEffects(cexEffects)
	}

	// Credited deposits generally should already be part of the base balance,
	// but just in case the amount was credited before the wallet confirmed the
	// fee.
	for _, deposit := range u.pendingDeposits {
		_, cexEffects := deposit.balanceEffects()
		addEffects(cexEffects)
	}

	for _, pendingDEXOrder := range u.pendingDEXOrders {
		addEffects(pendingDEXOrder.currentState().cexBalanceEffects)
	}

	available := u.baseCexBalances[assetID] + totalEffects.Settled[assetID]
	if available < 0 {
		u.log.Errorf("negative CEX balance for %s: %d", dex.BipIDSymbol(assetID), available)
		available = 0
	}

	return &BotBalance{
		Available: uint64(available),
		Locked:    totalEffects.Locked[assetID],
		Pending:   totalEffects.Pending[assetID],
		Reserved:  totalEffects.Reserved[assetID],
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
			Reserved:  dexBal.Reserved + cexBal.Reserved,
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

	u.balancesMtx.RLock()
	u.logBalanceAdjustments(dexDiffs, cexDiffs, msg)
	u.balancesMtx.RUnlock()
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

	dexEffects, cexEffects := withdrawal.balanceEffects()
	u.baseDexBalances[withdrawal.assetID] += dexEffects.Settled[withdrawal.assetID]
	u.baseCexBalances[withdrawal.assetID] += cexEffects.Settled[withdrawal.assetID]
	u.balancesMtx.Unlock()

	u.updateWithdrawalEvent(withdrawal, tx)
	u.sendStatsUpdate()

	dexDiffs := map[uint32]int64{withdrawal.assetID: dexEffects.Settled[withdrawal.assetID]}
	cexDiffs := map[uint32]int64{withdrawal.assetID: cexEffects.Settled[withdrawal.assetID]}

	u.balancesMtx.RLock()
	u.logBalanceAdjustments(dexDiffs, cexDiffs, fmt.Sprintf("Withdrawal %s complete.", id))
	u.balancesMtx.RUnlock()
}

func (u *unifiedExchangeAdaptor) confirmWithdrawal(ctx context.Context, id string) bool {
	u.balancesMtx.RLock()
	withdrawal, found := u.pendingWithdrawals[id]
	u.balancesMtx.RUnlock()
	if !found {
		u.log.Errorf("Withdrawal %s not found among pending withdrawals", id)
		return false
	}

	withdrawal.txMtx.RLock()
	txID := withdrawal.txID
	withdrawal.txMtx.RUnlock()

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

		withdrawal.txMtx.Lock()
		withdrawal.txID = txID
		withdrawal.txMtx.Unlock()
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

	withdrawal.txMtx.Lock()
	withdrawal.tx = tx
	withdrawal.txMtx.Unlock()

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

	// Pull transparent address out of unified address. There may be a different
	// field "exchangeAddress" once we add support for the new special encoding
	// required on binance global for zec and firo.
	if strings.HasPrefix(addr, "unified:") {
		var addrs struct {
			Transparent string `json:"transparent"`
		}
		if err := json.Unmarshal([]byte(addr[len("unified:"):]), &addrs); err != nil {
			return fmt.Errorf("error decoding unified address %q: %v", addr, err)
		}
		addr = addrs.Transparent
	}

	u.balancesMtx.Lock()
	withdrawalID, err := u.CEX.Withdraw(ctx, assetID, amount, addr)
	if err != nil {
		u.balancesMtx.Unlock()
		return err
	}

	u.log.Infof("Withdrew %s", u.fmtQty(assetID, amount))
	if assetID == u.baseID {
		u.pendingBaseRebalance.Store(true)
	} else {
		u.pendingQuoteRebalance.Store(true)
	}
	withdrawal := &pendingWithdrawal{
		eventLogID:   u.eventLogID.Add(1),
		timestamp:    time.Now().Unix(),
		assetID:      assetID,
		amtWithdrawn: amount,
		withdrawalID: withdrawalID,
	}
	u.pendingWithdrawals[withdrawalID] = withdrawal
	u.balancesMtx.Unlock()

	u.updateWithdrawalEvent(withdrawal, nil)
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
			freeable += o.dexBalanceEffects.Locked[assetID]
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
			return o.dexBalanceEffects.Locked[assetID], calc.BaseToQuote(o.counterTradeRate, o.order.Qty)
		}
		return o.dexBalanceEffects.Locked[assetID], o.order.Qty
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

	balanceEffects := cexTradeBalanceEffects(trade)
	for assetID, v := range balanceEffects.Settled {
		u.baseCexBalances[assetID] += v
		diffs[assetID] = v
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
	sufficient, _ := u.SufficientBalanceForCEXTrade(baseID, quoteID, sell, rate, qty)
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

	trade, err := u.CEX.Trade(ctx, baseID, quoteID, sell, rate, qty, *subscriptionID)
	u.updateCEXProblems(cexTradeProblem, u.baseID, err)
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
func (u *unifiedExchangeAdaptor) orderFees() (buyFees, sellFees *OrderFees, err error) {
	u.feesMtx.RLock()
	buyFees, sellFees = u.buyFees, u.sellFees
	u.feesMtx.RUnlock()

	if u.buyFees == nil || u.sellFees == nil {
		return u.updateFeeRates()
	}

	return buyFees, sellFees, nil
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

// tryCancelOrders cancels all booked DEX orders that are past the free cancel
// threshold. If cancelCEXOrders is true, it will also cancel CEX orders. True
// is returned if all orders have been cancelled. If cancelCEXOrders is false,
// false will always be returned.
func (u *unifiedExchangeAdaptor) tryCancelOrders(ctx context.Context, epoch *uint64, cancelCEXOrders bool) ([]dex.Bytes, bool) {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	done := true

	freeCancel := func(orderEpoch uint64) bool {
		if epoch == nil {
			return true
		}
		return *epoch-orderEpoch >= 2
	}

	cancels := make([]dex.Bytes, 0, len(u.pendingDEXOrders))

	for _, pendingOrder := range u.pendingDEXOrders {
		o := pendingOrder.currentState().order

		orderLatestState, err := u.clientCore.Order(o.ID)
		if err != nil {
			u.log.Errorf("Error fetching order %s: %v", o.ID, err)
			continue
		}
		if orderLatestState.Status > order.OrderStatusBooked {
			continue
		}

		done = false
		if freeCancel(o.Epoch) {
			err := u.clientCore.Cancel(o.ID)
			if err != nil {
				u.log.Errorf("Error canceling order %s: %v", o.ID, err)
			} else {
				cancels = append(cancels, o.ID)
			}
		}
	}

	if !cancelCEXOrders {
		return cancels, false
	}

	for _, pendingOrder := range u.pendingCEXOrders {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		pendingOrder.tradeMtx.RLock()
		tradeID, baseID, quoteID := pendingOrder.trade.ID, pendingOrder.trade.BaseID, pendingOrder.trade.QuoteID
		pendingOrder.tradeMtx.RUnlock()

		tradeStatus, err := u.CEX.TradeStatus(ctx, tradeID, baseID, quoteID)
		if err != nil {
			u.log.Errorf("Error getting CEX trade status: %v", err)
			continue
		}
		if tradeStatus.Complete {
			continue
		}

		done = false
		err = u.CEX.CancelTrade(ctx, baseID, quoteID, tradeID)
		if err != nil {
			u.log.Errorf("Error canceling CEX trade %s: %v", tradeID, err)
		}
	}

	return cancels, done
}

func (u *unifiedExchangeAdaptor) cancelAllOrders(ctx context.Context) {
	book, bookFeed, err := u.clientCore.SyncBook(u.host, u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("Error syncing book for cancellations: %v", err)
		u.tryCancelOrders(ctx, nil, true)
		return
	}
	defer bookFeed.Close()

	mktCfg, err := u.clientCore.ExchangeMarket(u.host, u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("Error getting market configuration: %v", err)
		u.tryCancelOrders(ctx, nil, true)
		return
	}

	currentEpoch := book.CurrentEpoch()
	if _, done := u.tryCancelOrders(ctx, &currentEpoch, true); done {
		return
	}

	timeout := time.Millisecond * time.Duration(3*mktCfg.EpochLen)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	i := 0
	for {
		select {
		case ni := <-bookFeed.Next():
			switch epoch := ni.Payload.(type) {
			case *core.ResolvedEpoch:
				if _, done := u.tryCancelOrders(ctx, &epoch.Current, true); done {
					return
				}
				timer.Reset(timeout)
				i++
			}
		case <-timer.C:
			u.tryCancelOrders(ctx, nil, true)
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

func dexOrderEffects(o *core.Order, swaps, redeems, refunds map[string]*asset.WalletTransaction, counterTradeRate uint64, baseTraits, quoteTraits asset.WalletTrait) (dex, cex *BalanceEffects) {
	dex, cex = newBalanceEffects(), newBalanceEffects()

	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(o.BaseID, o.QuoteID, o.Sell)

	// Account for pending funds locked in swaps awaiting confirmation.
	for _, match := range o.Matches {
		if match.Swap == nil || match.Redeem != nil || match.Refund != nil {
			continue
		}

		if match.Revoked {
			swapTx, found := swaps[match.Swap.ID.String()]
			if found {
				dex.Pending[fromAsset] += swapTx.Amount
			}
			continue
		}

		var redeemAmt uint64
		if o.Sell {
			redeemAmt = calc.BaseToQuote(match.Rate, match.Qty)
		} else {
			redeemAmt = match.Qty
		}
		dex.Pending[toAsset] += redeemAmt
	}

	dex.Settled[fromAsset] -= int64(o.LockedAmt)
	dex.Settled[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
	dex.Settled[toFeeAsset] -= int64(o.RedeemLockedAmt)

	dex.Locked[fromAsset] += o.LockedAmt
	dex.Locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	dex.Locked[toFeeAsset] += o.RedeemLockedAmt

	if o.FeesPaid != nil {
		dex.Settled[fromFeeAsset] -= int64(o.FeesPaid.Funding)
	}

	for _, tx := range swaps {
		dex.Settled[fromAsset] -= int64(tx.Amount)
		dex.Settled[fromFeeAsset] -= int64(tx.Fees)
	}

	var reedeemIsDynamicSwapper, refundIsDynamicSwapper bool
	if o.Sell {
		reedeemIsDynamicSwapper = quoteTraits.IsDynamicSwapper()
		refundIsDynamicSwapper = baseTraits.IsDynamicSwapper()
	} else {
		reedeemIsDynamicSwapper = baseTraits.IsDynamicSwapper()
		refundIsDynamicSwapper = quoteTraits.IsDynamicSwapper()
	}

	for _, tx := range redeems {
		if tx.Confirmed {
			dex.Settled[toAsset] += int64(tx.Amount)
			dex.Settled[toFeeAsset] -= int64(tx.Fees)
			continue
		}

		dex.Pending[toAsset] += tx.Amount
		if reedeemIsDynamicSwapper {
			dex.Settled[toFeeAsset] -= int64(tx.Fees)
		} else if dex.Pending[toFeeAsset] >= tx.Fees {
			dex.Pending[toFeeAsset] -= tx.Fees
		}
	}

	for _, tx := range refunds {
		if tx.Confirmed {
			dex.Settled[fromAsset] += int64(tx.Amount)
			dex.Settled[fromFeeAsset] -= int64(tx.Fees)
			continue
		}

		dex.Pending[fromAsset] += tx.Amount
		if refundIsDynamicSwapper {
			dex.Settled[fromFeeAsset] -= int64(tx.Fees)
		} else if dex.Pending[fromFeeAsset] >= tx.Fees {
			dex.Pending[fromFeeAsset] -= tx.Fees
		}
	}

	if counterTradeRate > 0 {
		reserved := reservedForCounterTrade(o.Sell, counterTradeRate, o.Qty-o.Filled)
		cex.Settled[toAsset] -= int64(reserved)
		cex.Reserved[toAsset] += reserved
	}

	return
}

// updateState should only be called with txsMtx write locked. The dex order
// state is stored as an atomic.Value in order to allow reads without locking.
// The mutex only needs to be locked for reading if the caller wants a consistent
// view of the transactions and the state.
func (p *pendingDEXOrder) updateState(o *core.Order, getTx func(uint32, string) (*asset.WalletTransaction, error), baseTraits, quoteTraits asset.WalletTrait) {
	swaps, redeems, refunds := orderTransactions(o)
	// Add new txs to tx cache
	fromAsset, _, toAsset, _ := orderAssets(o.BaseID, o.QuoteID, o.Sell)
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
			tx, err := getTx(assetID, txID)
			if err != nil {
				// p.log.Errorf("Error getting tx %s: %v", txID, err)
				continue
			}
			m[txID] = tx
		}
	}

	processTxs(fromAsset, p.swaps, swaps)
	processTxs(toAsset, p.redeems, redeems)
	processTxs(fromAsset, p.refunds, refunds)

	dexEffects, cexEffects := dexOrderEffects(o, p.swaps, p.redeems, p.refunds, p.counterTradeRate, baseTraits, quoteTraits)
	p.state.Store(&dexOrderState{
		order:             o,
		dexBalanceEffects: dexEffects,
		cexBalanceEffects: cexEffects,
		counterTradeRate:  p.counterTradeRate,
	})
}

// updatePendingDEXOrder updates a pending DEX order based its current state.
// If the order is complete, its effects are applied to the base balance,
// and it is removed from the pending list.
func (u *unifiedExchangeAdaptor) handleDEXOrderUpdate(o *core.Order) {
	var orderID order.OrderID
	copy(orderID[:], o.ID)

	u.balancesMtx.RLock()
	pendingOrder, found := u.pendingDEXOrders[orderID]
	u.balancesMtx.RUnlock()
	if !found {
		return
	}

	pendingOrder.txsMtx.Lock()
	pendingOrder.updateState(o, u.clientCore.WalletTransaction, u.baseTraits, u.quoteTraits)
	dexEffects := pendingOrder.currentState().dexBalanceEffects
	var havePending bool
	for _, v := range dexEffects.Pending {
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
		for assetID, diff := range dexEffects.Settled {
			adjustedBals = adjustedBals || diff != 0
			u.baseDexBalances[assetID] += diff
		}

		if adjustedBals {
			u.logBalanceAdjustments(dexEffects.Settled, nil, fmt.Sprintf("DEX order %s complete.", orderID))
		}
		u.balancesMtx.Unlock()
	}

	u.updateDEXOrderEvent(pendingOrder, complete)
}

func (u *unifiedExchangeAdaptor) handleDEXNotification(n core.Notification) {
	switch note := n.(type) {
	case *core.OrderNote:
		u.handleDEXOrderUpdate(note.Order)
	case *core.MatchNote:
		o, err := u.clientCore.Order(note.OrderID)
		if err != nil {
			u.log.Errorf("handleDEXNotification: failed to get order %s: %v", note.OrderID, err)
			return
		}
		u.handleDEXOrderUpdate(o)
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
		perLot.dexBase += sellFees.BookingFeesPerLot
	}
	perLot.cexBase = u.lotSize
	perLot.baseRedeem = buyFees.Max.Redeem
	perLot.baseFunding = sellFees.Funding

	dexQuoteLot := calc.BaseToQuote(sellVWAP, u.lotSize)
	cexQuoteLot := calc.BaseToQuote(buyVWAP, u.lotSize)
	perLot.dexQuote = dexQuoteLot
	if u.quoteID == u.quoteFeeID {
		perLot.dexQuote += buyFees.BookingFeesPerLot
	}
	perLot.cexQuote = cexQuoteLot
	perLot.quoteRedeem = sellFees.Max.Redeem
	perLot.quoteFunding = buyFees.Funding
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
		err := u.deposit(u.ctx, u.baseID, baseInv.toDeposit)
		u.updateCEXProblems(cexDepositProblem, u.baseID, err)
		if err != nil {
			return false, fmt.Errorf("error depositing base: %w", err)
		}
	} else if baseInv.toWithdraw > 0 {
		err := u.withdraw(u.ctx, u.baseID, baseInv.toWithdraw)
		u.updateCEXProblems(cexWithdrawProblem, u.baseID, err)
		if err != nil {
			return false, fmt.Errorf("error withdrawing base: %w", err)
		}
	}

	if quoteInv.toDeposit > 0 {
		err := u.deposit(u.ctx, u.quoteID, quoteInv.toDeposit)
		u.updateCEXProblems(cexDepositProblem, u.quoteID, err)
		if err != nil {
			return false, fmt.Errorf("error depositing quote: %w", err)
		}
	} else if quoteInv.toWithdraw > 0 {
		err := u.withdraw(u.ctx, u.quoteID, quoteInv.toWithdraw)
		u.updateCEXProblems(cexWithdrawProblem, u.quoteID, err)
		if err != nil {
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
		err = errors.New("cex book too empty to get a counter-rate estimate")
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
func (u *unifiedExchangeAdaptor) updateFeeRates() (buyFees, sellFees *OrderFees, err error) {
	defer func() {
		if err == nil {
			return
		}

		// In case of an error, clear the cached fees to avoid using stale data.
		u.feesMtx.Lock()
		defer u.feesMtx.Unlock()
		u.buyFees = nil
		u.sellFees = nil
	}()

	maxBaseFees, maxQuoteFees, err := marketFees(u.clientCore, u.host, u.baseID, u.quoteID, true)
	if err != nil {
		return nil, nil, err
	}

	estBaseFees, estQuoteFees, err := marketFees(u.clientCore, u.host, u.baseID, u.quoteID, false)
	if err != nil {
		return nil, nil, err
	}

	botCfg := u.botCfg()
	maxBuyPlacements, maxSellPlacements := botCfg.maxPlacements()

	buyFundingFees, err := u.clientCore.MaxFundingFees(u.quoteID, u.host, maxBuyPlacements, botCfg.QuoteWalletOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get buy funding fees: %v", err)
	}

	sellFundingFees, err := u.clientCore.MaxFundingFees(u.baseID, u.host, maxSellPlacements, botCfg.BaseWalletOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get sell funding fees: %v", err)
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

	u.buyFees = &OrderFees{
		LotFeeRange: &LotFeeRange{
			Max: maxBuyFees,
			Estimated: &LotFees{
				Swap:   estQuoteFees.Swap,
				Redeem: estBaseFees.Redeem,
				Refund: estQuoteFees.Refund,
			},
		},
		Funding:           buyFundingFees,
		BookingFeesPerLot: buyBookingFeesPerLot,
	}

	u.sellFees = &OrderFees{
		LotFeeRange: &LotFeeRange{
			Max: maxSellFees,
			Estimated: &LotFees{
				Swap:   estBaseFees.Swap,
				Redeem: estQuoteFees.Redeem,
				Refund: estBaseFees.Refund,
			},
		},
		Funding:           sellFundingFees,
		BookingFeesPerLot: sellBookingFeesPerLot,
	}

	return u.buyFees, u.sellFees, nil
}

func (u *unifiedExchangeAdaptor) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	u.ctx = ctx
	fiatRates := u.clientCore.FiatConversionRates()
	u.fiatRates.Store(fiatRates)

	_, _, err := u.updateFeeRates()
	if err != nil {
		return nil, fmt.Errorf("failed to getting fee rates: %v", err)
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
				_, _, err := u.updateFeeRates()
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
	Diffs       map[uint32]*Amount `json:"diffs"`
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
		Diffs:   make(map[uint32]*Amount, len(initialBalances)),
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
		diff := int64(finalBalances[assetID]) - int64(initialBalances[assetID]) - mods[assetID]
		pl.Diffs[assetID] = NewAmount(assetID, diff, fiatRate)
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

func (u *unifiedExchangeAdaptor) latestCEXProblems() *CEXProblems {
	u.cexProblemsMtx.RLock()
	defer u.cexProblemsMtx.RUnlock()
	if u.cexProblems == nil {
		return nil
	}
	return u.cexProblems.copy()
}

func (u *unifiedExchangeAdaptor) latestEpoch() *EpochReport {
	reportI := u.epochReport.Load()
	if reportI == nil {
		return nil
	}
	return reportI.(*EpochReport)
}

func (u *unifiedExchangeAdaptor) updateEpochReport(report *EpochReport) {
	u.epochReport.Store(report)
	u.clientCore.Broadcast(newEpochReportNote(u.host, u.baseID, u.quoteID, report))
}

// tradingLimitNotReached returns true if the user has not reached their trading
// limit. If it has, it updates the epoch report with the problems.
func (u *unifiedExchangeAdaptor) tradingLimitNotReached(epochNum uint64) bool {
	var tradingLimitReached bool
	var err error
	defer func() {
		if err == nil && !tradingLimitReached {
			return
		}

		u.updateEpochReport(&EpochReport{
			PreOrderProblems: &BotProblems{
				UserLimitTooLow: tradingLimitReached,
				UnknownError:    err,
			},
			EpochNum: epochNum,
		})
	}()

	userParcels, parcelLimit, err := u.clientCore.TradingLimits(u.host)
	if err != nil {
		return false
	}

	tradingLimitReached = userParcels >= parcelLimit
	return !tradingLimitReached
}

type cexProblemType uint16

const (
	cexTradeProblem cexProblemType = iota
	cexDepositProblem
	cexWithdrawProblem
)

func (u *unifiedExchangeAdaptor) updateCEXProblems(typ cexProblemType, assetID uint32, err error) {
	u.cexProblemsMtx.RLock()
	existingErrNil := func() bool {
		switch typ {
		case cexTradeProblem:
			return u.cexProblems.TradeErr == nil
		case cexDepositProblem:
			return u.cexProblems.DepositErr[assetID] == nil
		case cexWithdrawProblem:
			return u.cexProblems.WithdrawErr[assetID] == nil
		default:
			return true
		}
	}
	if existingErrNil() && err == nil {
		u.cexProblemsMtx.RUnlock()
		return
	}
	u.cexProblemsMtx.RUnlock()

	u.cexProblemsMtx.Lock()
	defer u.cexProblemsMtx.Unlock()

	switch typ {
	case cexTradeProblem:
		if err == nil {
			u.cexProblems.TradeErr = nil
		} else {
			u.cexProblems.TradeErr = newStampedError(err)
		}
	case cexDepositProblem:
		if err == nil {
			delete(u.cexProblems.DepositErr, assetID)
		} else {
			u.cexProblems.DepositErr[assetID] = newStampedError(err)
		}
	case cexWithdrawProblem:
		if err == nil {
			delete(u.cexProblems.WithdrawErr, assetID)
		} else {
			u.cexProblems.WithdrawErr[assetID] = newStampedError(err)
		}
	}

	u.clientCore.Broadcast(newCexProblemsNote(u.host, u.baseID, u.quoteID, u.cexProblems))
}

// checkBotHealth returns true if the bot is healthy and can continue trading.
// If it is not healthy, it updates the epoch report with the problems.
func (u *unifiedExchangeAdaptor) checkBotHealth(epochNum uint64) (healthy bool) {
	var err error
	var baseAssetNotSynced, baseAssetNoPeers, quoteAssetNotSynced, quoteAssetNoPeers, accountSuspended bool

	defer func() {
		if healthy {
			return
		}
		problems := &BotProblems{
			NoWalletPeers: map[uint32]bool{
				u.baseID:  baseAssetNoPeers,
				u.quoteID: quoteAssetNoPeers,
			},
			WalletNotSynced: map[uint32]bool{
				u.baseID:  baseAssetNotSynced,
				u.quoteID: quoteAssetNotSynced,
			},
			AccountSuspended: accountSuspended,
			UnknownError:     err,
		}
		u.updateEpochReport(&EpochReport{
			PreOrderProblems: problems,
			EpochNum:         epochNum,
		})
	}()

	baseWallet := u.clientCore.WalletState(u.baseID)
	if baseWallet == nil {
		err = fmt.Errorf("base asset %d wallet not found", u.baseID)
		return false
	}

	baseAssetNotSynced = !baseWallet.Synced
	baseAssetNoPeers = baseWallet.PeerCount == 0

	quoteWallet := u.clientCore.WalletState(u.quoteID)
	if quoteWallet == nil {
		err = fmt.Errorf("quote asset %d wallet not found", u.quoteID)
		return false
	}

	quoteAssetNotSynced = !quoteWallet.Synced
	quoteAssetNoPeers = quoteWallet.PeerCount == 0

	exchange, err := u.clientCore.Exchange(u.host)
	if err != nil {
		err = fmt.Errorf("error getting exchange: %w", err)
		return false
	}
	accountSuspended = exchange.Auth.EffectiveTier <= 0

	return !(baseAssetNotSynced || baseAssetNoPeers || quoteAssetNotSynced || quoteAssetNoPeers || accountSuspended)
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
		cexProblems:        newCEXProblems(),
	}

	adaptor.fiatRates.Store(map[uint32]float64{})
	adaptor.botCfgV.Store(cfg.botCfg)

	return adaptor, nil
}
