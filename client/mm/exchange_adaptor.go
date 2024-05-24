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
	MultiTrade(placements []*multiTradePlacement, sell bool, driftTolerance float64, currEpoch uint64, dexReserves, cexReserves map[uint32]uint64) []*order.OrderID
	ExchangeRateFromFiatSources() uint64
	OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error) // estimated fees, not max
	SubscribeOrderUpdates() (updates <-chan *core.Order)
	SufficientBalanceForDEXTrade(rate, qty uint64, sell bool) (bool, error)
	registerFeeGap(*FeeGapStats)
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
	PrepareRebalance(ctx context.Context, assetID uint32) (rebalance int64, dexReserves, cexReserves uint64)
	FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64)
	Deposit(ctx context.Context, assetID uint32, amount uint64) error
	Withdraw(ctx context.Context, assetID uint32, amount uint64) error
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
	availableDiff     map[uint32]int64
	locked            map[uint32]uint64
	pending           map[uint32]uint64
	baseDelta         int64
	quoteDelta        int64
	pendingBaseDelta  uint64
	pendingQuoteDelta uint64
	baseFees          uint64
	quoteFees         uint64
}

type dexOrderState struct {
	balanceEffects *balanceEffects
	order          *core.Order
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
func (p *pendingDEXOrder) updateState(order *core.Order, balanceEffects *balanceEffects) {
	p.state.Store(&dexOrderState{
		order:          order,
		balanceEffects: balanceEffects,
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
		s = strings.TrimRight(s, "0.")
	}
	return s
}
func (m *market) fmtBase(atoms uint64) string {
	return m.bui.FormatAtoms(atoms)
}

func (m *market) fmtBaseFees(atoms uint64) string {
	return m.baseFeeUI.FormatAtoms(atoms)
}

func (m *market) fmtQuoteFees(atoms uint64) string {
	return m.quoteFeeUI.FormatAtoms(atoms)
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
	botID           string
	log             dex.Logger
	fiatRates       atomic.Value // map[uint32]float64
	orderUpdates    atomic.Value // chan *core.Order
	mwh             *MarketWithHost
	eventLogDB      eventLogDB
	botCfgV         atomic.Value // *BotConfig
	initialBalances map[uint32]uint64

	botLooper dex.Connector
	botLoop   *dex.ConnectionMaster
	paused    atomic.Bool

	autoRebalanceConfig *AutoRebalanceConfig

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
	var msg strings.Builder
	msg.WriteString("\n" + reason)
	msg.WriteString("\n  Balance adjustments:")
	if len(dexDiffs) > 0 {
		msg.WriteString("\n    DEX: ")
		i := 0
		for assetID, diff := range dexDiffs {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), diff))
			if i < len(dexDiffs)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	if len(cexDiffs) > 0 {
		msg.WriteString("\n    CEX: ")
		i := 0
		for assetID, diff := range cexDiffs {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), diff))
			if i < len(cexDiffs)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	msg.WriteString("\n\n  Updated base balances:\n    DEX: ")
	i := 0
	for assetID, bal := range u.baseDexBalances {
		msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), bal))
		if i < len(u.baseDexBalances)-1 {
			msg.WriteString(", ")
		}
		i++
	}

	if len(u.baseCexBalances) > 0 {
		msg.WriteString("\n    CEX: ")
		i = 0
		for assetID, bal := range u.baseCexBalances {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), bal))
			if i < len(u.baseCexBalances)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	u.log.Infof(msg.String())
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
		BaseDelta:  state.balanceEffects.baseDelta,
		QuoteDelta: state.balanceEffects.quoteDelta,
		BaseFees:   state.balanceEffects.baseFees,
		QuoteFees:  state.balanceEffects.quoteFees,
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

		availableDiff := map[uint32]int64{}
		availableDiff[fromAsset] -= int64(o.LockedAmt)
		availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
		availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
		if o.FeesPaid != nil {
			availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
		}

		locked := map[uint32]uint64{}
		locked[fromAsset] += o.LockedAmt
		locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
		locked[toFeeAsset] += o.RedeemLockedAmt

		var baseFees, quoteFees uint64
		if o.FeesPaid != nil && o.FeesPaid.Funding > 0 {
			if o.Sell {
				baseFees = o.FeesPaid.Funding
			} else {
				quoteFees = o.FeesPaid.Funding
			}
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
				order: o,
				balanceEffects: &balanceEffects{
					availableDiff: availableDiff,
					locked:        locked,
					pending:       map[uint32]uint64{},
					baseFees:      baseFees,
					quoteFees:     quoteFees,
				},
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
func (u *unifiedExchangeAdaptor) MultiTrade(placements []*multiTradePlacement, sell bool, driftTolerance float64, currEpoch uint64, dexReserves, cexReserves map[uint32]uint64) []*order.OrderID {
	if len(placements) == 0 {
		return nil
	}
	mkt, err := u.ExchangeMarket(u.host, u.baseID, u.quoteID)
	if err != nil {
		u.log.Errorf("MultiTrade: error getting market: %v", err)
		return nil
	}
	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		u.log.Errorf("MultiTrade: error getting order fees: %v", err)
		return nil
	}
	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(mkt.BaseID, mkt.QuoteID, sell)
	fees, fundingFees := buyFees.Max, buyFees.funding
	if sell {
		fees, fundingFees = sellFees.Max, sellFees.funding
	}

	// First, determine the amount of balances the bot has available to place
	// DEX trades taking into account dexReserves.
	remainingBalances := map[uint32]uint64{}
	for _, assetID := range []uint32{fromAsset, fromFeeAsset, toAsset, toFeeAsset} {
		if _, found := remainingBalances[assetID]; !found {
			bal := u.DEXBalance(assetID)
			availableBalance := bal.Available
			if dexReserves != nil {
				if dexReserves[assetID] > availableBalance {
					u.log.Errorf("MultiTrade: insufficient dex balance for reserves. required: %d, have: %d", dexReserves[assetID], availableBalance)
					return nil
				}
				availableBalance -= dexReserves[assetID]
			}
			remainingBalances[assetID] = availableBalance
		}
	}
	if remainingBalances[fromFeeAsset] < fundingFees {
		u.log.Debugf("MultiTrade: insufficient balance for funding fees. required: %d, have: %d", fundingFees, remainingBalances[fromFeeAsset])
		return nil
	}
	remainingBalances[fromFeeAsset] -= fundingFees

	// If the placements include a counterTradeRate, the CEX balance must also
	// be taken into account to determine how many trades can be placed.
	accountForCEXBal := placements[0].counterTradeRate > 0
	var remainingCEXBal uint64
	if accountForCEXBal {
		cexBal := u.CEXBalance(toAsset)
		remainingCEXBal = cexBal.Available
		reserves := cexReserves[toAsset]
		if remainingCEXBal < reserves {
			u.log.Errorf("MultiTrade: insufficient CEX balance for reserves. required: %d, have: %d", cexReserves, remainingCEXBal)
			return nil
		}
		remainingCEXBal -= reserves
	}

	cancels := make([]dex.Bytes, 0, len(placements))
	addCancel := func(o *core.Order) {
		if currEpoch-o.Epoch < 2 { // TODO: check epoch
			u.log.Debugf("MultiTrade: skipping cancel not past free cancel threshold")
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
			if requiredPlacements[o.placementIndex].lots >= (order.Qty-order.Filled)/mkt.LotSize {
				requiredPlacements[o.placementIndex].lots -= (order.Qty - order.Filled) / mkt.LotSize
			} else {
				// This will happen if there is a reconfig in which this
				// placement index now requires less lots than before.
				mustCancel = true
				requiredPlacements[o.placementIndex].lots = 0
			}

			if mustCancel {
				u.log.Tracef("%s cancel with rate = %s, placement rate = %s, drift tolerance = %.4f%%",
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
		qty := mkt.LotSize * lots
		if !sell {
			qty = calc.BaseToQuote(rate, qty)
		}
		dexReq = make(map[uint32]uint64)
		dexReq[fromAsset] += qty
		dexReq[fromFeeAsset] += fees.Swap * lots
		if u.isAccountLocker(fromAsset) {
			dexReq[fromFeeAsset] += fees.Refund * lots
		}
		if u.isAccountLocker(toAsset) {
			dexReq[toFeeAsset] += fees.Redeem * lots
		}
		if accountForCEXBal {
			if sell {
				cexReq = calc.BaseToQuote(counterTradeRate, mkt.LotSize*lots)
			} else {
				cexReq = mkt.LotSize * lots
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
			u.log.Warnf("MultiTrade: rate %d causes self match. Placements should be farther from mid-gap.", placement.rate)
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
					Qty:  lotsToPlace * mkt.LotSize,
					Rate: placement.rate,
				},
			})
		}

		// If there is insufficient balance to place a higher priority order,
		// cancel the lower priority orders.
		if lotsToPlace < placement.lots {
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
			u.log.Errorf("MultiTrade: error canceling order %s: %v", cancel, err)
		}
	}

	if len(orderInfos) > 0 {
		orders, err := u.placeMultiTrade(orderInfos, sell)
		if err != nil {
			u.log.Errorf("MultiTrade: error placing orders: %v", err)
			return nil
		}

		orderIDs := make([]*order.OrderID, len(placements))
		for i, o := range orders {
			info := orderInfos[i]
			var orderID order.OrderID
			copy(orderID[:], o.ID)
			orderIDs[info.placementIndex] = &orderID
		}
		return orderIDs
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

	// MultiTrade is used instead of Trade because Trade does not support
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
		totalAvailableDiff += effects.availableDiff[assetID]
		totalLocked += effects.locked[assetID]
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
	var feeAsset uint32
	token := asset.TokenInfo(assetID)
	if token != nil {
		feeAsset = token.ParentID
	} else {
		feeAsset = assetID
	}
	u.baseDexBalances[feeAsset] -= int64(tx.Fees)
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
	dexDiffs[feeAsset] -= int64(tx.Fees)
	cexDiffs[assetID] += int64(amtCredited)

	var msg string
	if amtCredited > 0 {
		msg = fmt.Sprintf("Deposit %s complete.", tx.ID)
	} else {
		msg = fmt.Sprintf("Deposit %s complete, but not successful.", tx.ID)
	}

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

// Deposit deposits funds to the CEX. The deposited funds will be removed from
// the bot's wallet balance and allocated to the bot's CEX balance. After both
// the fees of the deposit transaction are confirmed by the wallet and the
// CEX confirms the amount they received, the onConfirm callback is called.
func (u *unifiedExchangeAdaptor) Deposit(ctx context.Context, assetID uint32, amount uint64) error {
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

	go func() {
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

// Withdraw withdraws funds from the CEX. After withdrawing, the CEX is queried
// for the transaction ID. After the transaction ID is available, the wallet is
// queried for the amount received.
func (u *unifiedExchangeAdaptor) Withdraw(ctx context.Context, assetID uint32, amount uint64) error {
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

	go func() {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				if u.confirmWithdrawal(ctx, withdrawalID) {
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

// FreeUpFunds cancels active orders to free up the specified amount of funds
// for a rebalance between the dex and the cex. If the cex parameter is true,
// it means we are freeing up funds for withdrawal. DEX orders that require a
// counter-trade on the CEX are cancelled. The orders are cancelled in reverse
// order of priority (determined by the order in which they were passed into
// MultiTrade).
func (u *unifiedExchangeAdaptor) FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64) {
	if u.autoRebalanceConfig == nil {
		return
	}

	var base bool
	if assetID == u.baseID {
		base = true
	} else if assetID != u.quoteID {
		u.log.Errorf("Asset %d is not the base or quote asset of the market", assetID)
		return
	}

	if cex {
		bal := u.CEXBalance(assetID)
		if bal.Available >= amt {
			return
		}
		amt -= bal.Available
	} else {
		bal := u.DEXBalance(assetID)
		if bal.Available >= amt {
			return
		}
		amt -= bal.Available
	}

	sells := base != cex
	orders := u.groupedBookedOrders(sells)

	highToLowIndexes := make([]uint64, 0, len(orders))
	for i := range orders {
		highToLowIndexes = append(highToLowIndexes, i)
	}
	sort.Slice(highToLowIndexes, func(i, j int) bool {
		return highToLowIndexes[i] > highToLowIndexes[j]
	})

	amtFreedByCancellingOrder := func(o *pendingDEXOrder) uint64 {
		state := o.currentState()
		if cex {
			if state.order.Sell {
				return calc.BaseToQuote(o.counterTradeRate, state.order.Qty-state.order.Filled)
			}
			return state.order.Qty - state.order.Filled
		}

		return state.balanceEffects.locked[assetID]
	}

	var totalFreedAmt uint64
	for _, index := range highToLowIndexes {
		ordersForPlacement := orders[index]
		for _, o := range ordersForPlacement {
			order := o.currentState().order
			// If the order is too recent, just wait for the next epoch to
			// cancel. We still count this order towards the freedAmt in
			// order to not cancel a higher priority trade.
			if currEpoch-order.Epoch >= 2 {
				err := u.Cancel(order.ID)
				if err != nil {
					u.log.Errorf("Error cancelling order: %v", err)
					continue
				}
			}

			totalFreedAmt += amtFreedByCancellingOrder(o)
			if totalFreedAmt >= amt {
				return
			}
		}
	}

	u.log.Warnf("Could not free up enough funds for %s %s rebalance. Freed %d, needed %d",
		dex.BipIDSymbol(assetID), dex.BipIDSymbol(u.quoteID), amt, amt+u.autoRebalanceConfig.MinQuoteTransfer)
}

func (u *unifiedExchangeAdaptor) rebalanceAsset(ctx context.Context, assetID uint32, minAmount, minTransferAmount uint64) (toSend int64, dexReserves, cexReserves uint64) {
	dexBalance := u.DEXBalance(assetID)
	totalDexBalance := dexBalance.Available + dexBalance.Locked
	cexBalance := u.CEXBalance(assetID)
	totalCexBalance := cexBalance.Available + cexBalance.Reserved

	// Don't take into account locked funds on CEX, because we don't do
	// rebalancing while there are active orders on the CEX.
	if (totalDexBalance+totalCexBalance)/2 < minAmount {
		u.log.Warnf("Cannot rebalance %s because balance is too low on both DEX and CEX. Min amount: %v, CEX balance: %v, DEX Balance: %v",
			dex.BipIDSymbol(assetID), minAmount, totalCexBalance, totalDexBalance)
		return
	}

	var requireDeposit bool
	if totalCexBalance < minAmount {
		requireDeposit = true
	} else if totalDexBalance >= minAmount {
		// No need for withdrawal or deposit.
		return
	}

	if requireDeposit {
		amt := (totalDexBalance+totalCexBalance)/2 - totalCexBalance
		if amt < minTransferAmount {
			u.log.Warnf("Amount required to rebalance %s (%d) is less than the min transfer amount %v",
				dex.BipIDSymbol(assetID), amt, minTransferAmount)
			return
		}

		// If we need to cancel some orders to send the required amount to
		// the CEX, cancel some orders, and then try again on the next
		// epoch.
		if amt > dexBalance.Available {
			dexReserves = amt
			return
		}

		toSend = int64(amt)
	} else {
		amt := (totalDexBalance+totalCexBalance)/2 - totalDexBalance
		if amt < minTransferAmount {
			u.log.Warnf("Amount required to rebalance %s (%d) is less than the min transfer amount %v",
				dex.BipIDSymbol(assetID), amt, minTransferAmount)
			return
		}

		if amt > cexBalance.Available {
			cexReserves = amt
			return
		}

		toSend = -int64(amt)
	}

	return
}

// PrepareRebalance returns the amount that needs to be either deposited to or
// withdrawn from the CEX to rebalance the specified asset. If the amount is
// positive, it is the amount that needs to be deposited to the CEX. If the
// amount is negative, it is the amount that needs to be withdrawn from the
// CEX. If the amount is zero, no rebalancing is required.
//
// The dexReserves and cexReserves return values are the amount of funds that
// need to be freed from pending orders before a deposit or withdrawal can be
// made. This value should be passed to FreeUpFunds to cancel the pending
// orders that are obstructing the transfer, and also should be passed to any
// calls to MultiTrade to make sure the funds needed for rebalancing are not
// used to place orders.
func (u *unifiedExchangeAdaptor) PrepareRebalance(ctx context.Context, assetID uint32) (rebalance int64, dexReserves, cexReserves uint64) {
	if u.autoRebalanceConfig == nil {
		return
	}

	var minAmount uint64
	var minTransferAmount uint64
	if assetID == u.baseID {
		if u.pendingBaseRebalance.Load() {
			return
		}
		minAmount = u.autoRebalanceConfig.MinBaseAmt
		minTransferAmount = u.autoRebalanceConfig.MinBaseTransfer
	} else {
		if assetID != u.quoteID {
			u.log.Errorf("assetID %d is not the base or quote asset ID of the market", assetID)
			return
		}
		if u.pendingQuoteRebalance.Load() {
			return
		}
		minAmount = u.autoRebalanceConfig.MinQuoteAmt
		minTransferAmount = u.autoRebalanceConfig.MinQuoteTransfer
	}

	return u.rebalanceAsset(ctx, assetID, minAmount, minTransferAmount)
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
	if !found {
		u.balancesMtx.Unlock()
		return
	}
	u.balancesMtx.Unlock()

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
	toID := u.quoteID
	if base {
		toID = u.baseID
	}

	convertViaFiat := func(fees uint64, fromID uint32) (uint64, error) {
		atomicCFactor, err := u.atomicConversionRateFromFiat(fromID, toID)
		if err != nil {
			return 0, err
		}
		return uint64(math.Round(float64(fees) * atomicCFactor)), nil
	}

	var baseFeesInUnits, quoteFeesInUnits uint64

	baseToken := asset.TokenInfo(u.baseID)
	if baseToken != nil {
		baseFeesInUnits, err = convertViaFiat(baseFees, baseToken.ParentID)
		if err != nil {
			return 0, err
		}
	} else {
		if base {
			baseFeesInUnits = baseFees
		} else {
			baseFeesInUnits = calc.BaseToQuote(rate, baseFees)
		}
	}

	quoteToken := asset.TokenInfo(u.quoteID)
	if quoteToken != nil {
		quoteFeesInUnits, err = convertViaFiat(quoteFees, quoteToken.ParentID)
		if err != nil {
			return 0, err
		}
	} else {
		if base {
			quoteFeesInUnits = calc.QuoteToBase(rate, quoteFees)
		} else {
			quoteFeesInUnits = quoteFees
		}
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
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isAccountLocker: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsAccountLocker()
}

// isDynamicSwapper returns if the asset's wallet is an asset.DynamicSwapper.
func (u *unifiedExchangeAdaptor) isDynamicSwapper(assetID uint32) bool {
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isDynamicSwapper: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsDynamicSwapper()
}

// isWithdrawer returns if the asset's wallet is an asset.Withdrawer.
func (u *unifiedExchangeAdaptor) isWithdrawer(assetID uint32) bool {
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isWithdrawer: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsWithdrawer()
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

func feeAsset(assetID uint32) uint32 {
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

func (u *unifiedExchangeAdaptor) checkPendingDEXOrderTxs(pendingOrder *pendingDEXOrder, o *core.Order) {
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

	// Calculate balance effects based on current state of the order.
	effects := &balanceEffects{
		availableDiff: make(map[uint32]int64),
		locked:        make(map[uint32]uint64),
		pending:       make(map[uint32]uint64),
	}

	addFees := func(fromAsset bool, fee uint64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			effects.baseFees += fee
		} else {
			effects.quoteFees += fee
		}
	}
	addDelta := func(fromAsset bool, delta int64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			effects.baseDelta += delta
		} else {
			effects.quoteDelta += delta
		}
	}
	addPendingDelta := func(fromAsset bool, delta uint64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			effects.pendingBaseDelta += delta
		} else {
			effects.pendingQuoteDelta += delta
		}
	}

	for _, match := range o.Matches {
		if match.Swap == nil || match.Redeem != nil || match.Refund != nil {
			continue
		}

		if match.Revoked {
			swapTx, found := pendingOrder.swaps[match.Swap.ID.String()]
			if found {
				addPendingDelta(true, swapTx.Amount)
			}
		} else {
			var redeemAmt uint64
			if o.Sell {
				redeemAmt = calc.BaseToQuote(match.Rate, match.Qty)
			} else {
				redeemAmt = match.Qty
			}
			addPendingDelta(false, redeemAmt)
		}
	}

	effects.availableDiff[fromAsset] -= int64(o.LockedAmt)
	effects.availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
	effects.availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
	if o.FeesPaid != nil {
		effects.availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
		addFees(true, o.FeesPaid.Funding)
	}

	effects.locked[fromAsset] += o.LockedAmt
	effects.locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	effects.locked[toFeeAsset] += o.RedeemLockedAmt

	for _, tx := range pendingOrder.swaps {
		effects.availableDiff[fromAsset] -= int64(tx.Amount)
		effects.availableDiff[fromFeeAsset] -= int64(tx.Fees)
		addFees(true, tx.Fees)
		addDelta(true, -int64(tx.Amount))
	}
	for _, tx := range pendingOrder.redeems {
		isDynamicSwapper := u.isDynamicSwapper(toAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			effects.availableDiff[toFeeAsset] -= int64(tx.Fees)
		} else {
			effects.pending[toFeeAsset] -= tx.Fees
		}
		addFees(false, tx.Fees)
		addDelta(false, int64(tx.Amount))

		if tx.Confirmed {
			effects.availableDiff[toAsset] += int64(tx.Amount)
		} else {
			effects.pending[toAsset] += tx.Amount
		}
	}
	for _, tx := range pendingOrder.refunds {
		isDynamicSwapper := u.isDynamicSwapper(fromAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			effects.availableDiff[fromFeeAsset] -= int64(tx.Fees)
		} else {
			effects.pending[fromFeeAsset] -= tx.Fees
		}
		addFees(true, tx.Fees)
		addDelta(true, int64(tx.Amount))

		if tx.Confirmed {
			effects.availableDiff[fromAsset] += int64(tx.Amount)
		} else {
			effects.pending[fromAsset] += tx.Amount
		}
	}

	pendingOrder.updateState(o, effects)
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
	u.checkPendingDEXOrderTxs(pendingOrder, o)
	pendingOrder.txsMtx.Unlock()

	orderUpdates := u.orderUpdates.Load()
	if orderUpdates != nil {
		orderUpdates.(chan *core.Order) <- o
	}

	complete := dexOrderComplete(o)
	// If complete, remove the order from the pending list, and update the
	// bot's balance.

	if complete { // TODO: complete when all fees are confirmed
		effects := pendingOrder.currentState().balanceEffects

		u.balancesMtx.Lock()
		delete(u.pendingDEXOrders, orderID)

		adjustedBals := false
		for assetID, diff := range effects.availableDiff {
			adjustedBals = adjustedBals || diff != 0
			u.baseDexBalances[assetID] += diff
		}

		u.runStats.baseBalanceDelta.Add(effects.baseDelta)
		u.runStats.quoteBalanceDelta.Add(effects.quoteDelta)
		u.runStats.baseFees.Add(effects.baseFees)
		u.runStats.quoteFees.Add(effects.quoteFees)

		if effects.pendingBaseDelta != 0 || effects.pendingQuoteDelta != 0 {
			u.log.Errorf("bot %s pending delta not zero after order %s complete. base: %d, quote: %d",
				u.botID, orderID, effects.pendingBaseDelta, effects.pendingQuoteDelta)
		}

		if adjustedBals {
			u.logBalanceAdjustments(effects.availableDiff, nil, fmt.Sprintf("DEX order %s complete.", orderID))
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

	buyFundingFees, err := u.clientCore.MaxFundingFees(u.quoteID, u.host, maxBuyPlacements, botCfg.BaseWalletOptions)
	if err != nil {
		return fmt.Errorf("failed to get buy funding fees: %v", err)
	}

	sellFundingFees, err := u.clientCore.MaxFundingFees(u.baseID, u.host, maxSellPlacements, botCfg.QuoteWalletOptions)
	if err != nil {
		return fmt.Errorf("failed to get sell funding fees: %v", err)
	}

	u.feesMtx.Lock()
	defer u.feesMtx.Unlock()

	u.buyFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Swap:   maxQuoteFees.Swap,
				Redeem: maxBaseFees.Redeem,
				Refund: maxQuoteFees.Refund,
			},
			Estimated: &LotFees{
				Swap:   estQuoteFees.Swap,
				Redeem: estBaseFees.Redeem,
				Refund: estQuoteFees.Refund,
			},
		},
		funding: buyFundingFees,
	}
	u.sellFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Swap:   maxBaseFees.Swap,
				Redeem: maxQuoteFees.Redeem,
				Refund: maxBaseFees.Refund,
			},
			Estimated: &LotFees{
				Swap:   estBaseFees.Swap,
				Redeem: estQuoteFees.Redeem,
				Refund: estBaseFees.Refund,
			},
		},
		funding: sellFundingFees,
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

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		u.eventLogDB.endRun(startTime, u.mwh, time.Now().Unix())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		u.cancelAllOrders(ctx)
	}()

	// Listen for core notifications
	wg.Add(1)
	go func() {
		defer wg.Done()
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

	wg.Add(1)
	go func() {
		defer wg.Done()
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

	return &wg, nil
}

// RunStats is a snapshot of the bot's balances and performance at a point in
// time.
type RunStats struct {
	InitialBalances    map[uint32]uint64      `json:"initialBalances"`
	DEXBalances        map[uint32]*BotBalance `json:"dexBalances"`
	CEXBalances        map[uint32]*BotBalance `json:"cexBalances"`
	ProfitLoss         float64                `json:"profitLoss"`
	ProfitRatio        float64                `json:"profitRatio"`
	StartTime          int64                  `json:"startTime"`
	PendingDeposits    int                    `json:"pendingDeposits"`
	PendingWithdrawals int                    `json:"pendingWithdrawals"`
	CompletedMatches   uint32                 `json:"completedMatches"`
	TradedUSD          float64                `json:"tradedUSD"`
	FeeGap             *FeeGapStats           `json:"feeGap"`
}

func calcRunProfitLoss(initialBalances, finalBalances map[uint32]uint64, mods map[uint32]int64, fiatRates map[uint32]float64) (deltaUSD, profitRatio float64) {
	assetIDs := make(map[uint32]struct{}, len(initialBalances))
	for assetID := range initialBalances {
		assetIDs[assetID] = struct{}{}
	}
	for assetID := range finalBalances {
		assetIDs[assetID] = struct{}{}
	}

	convertToConventional := func(assetID uint32, amount int64) (float64, error) {
		unitInfo, err := asset.UnitInfo(assetID)
		if err != nil {
			return 0, err
		}
		return float64(amount) / float64(unitInfo.Conventional.ConversionFactor), nil
	}

	var initialBalanceUSD, finalBalanceUSD float64

	for assetID := range assetIDs {
		initialBalance, err := convertToConventional(assetID, int64(initialBalances[assetID]))
		if err != nil {
			continue
		}
		finalBalance, err := convertToConventional(assetID, int64(finalBalances[assetID]))
		if err != nil {
			continue
		}

		if mod := mods[assetID]; mod != 0 {
			if modConv, err := convertToConventional(assetID, mod); err == nil {
				initialBalance += modConv
			}
		}

		fiatRate := fiatRates[assetID]
		if fiatRate == 0 {
			continue
		}

		initialBalanceUSD += initialBalance * fiatRate
		finalBalanceUSD += finalBalance * fiatRate
	}

	return finalBalanceUSD - initialBalanceUSD, (finalBalanceUSD / initialBalanceUSD) - 1
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
		totalBalances[assetID] = bal.Available + bal.Locked + bal.Pending
	}

	for assetID := range u.baseCexBalances {
		bal := u.cexBalance(assetID)
		cexBalances[assetID] = bal
		totalBalances[assetID] += bal.Available + bal.Locked + bal.Pending
	}

	fiatRates := u.fiatRates.Load().(map[uint32]float64)

	var feeGap *FeeGapStats
	if feeGapI := u.runStats.feeGapStats.Load(); feeGapI != nil {
		feeGap = feeGapI.(*FeeGapStats)
	}

	profitLoss, profitRatio := calcRunProfitLoss(u.initialBalances, totalBalances, u.inventoryMods, fiatRates)
	u.runStats.tradedUSD.Lock()
	tradedUSD := u.runStats.tradedUSD.v
	u.runStats.tradedUSD.Unlock()

	// Effects of pendingWithdrawals are applied when the withdrawal is
	// complete.
	return &RunStats{
		InitialBalances:    u.initialBalances,
		DEXBalances:        dexBalances,
		CEXBalances:        cexBalances,
		ProfitLoss:         profitLoss,
		ProfitRatio:        profitRatio,
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

func (u *unifiedExchangeAdaptor) registerFeeGap(s *FeeGapStats) {
	u.runStats.feeGapStats.Store(s)
}

func (u *unifiedExchangeAdaptor) updateBalances(balanceDiffs *BotInventoryDiffs) map[uint32]int64 {
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

	u.logBalanceAdjustments(balanceDiffs.DEX, balanceDiffs.CEX, "Balances updated")
	u.log.Debugf("Aggregate inventory mods: %+v", u.inventoryMods)

	return mods
}

func (u *unifiedExchangeAdaptor) updateConfig(cfg *BotConfig) {
	u.botCfgV.Store(cfg)
	u.updateConfigEvent(cfg)
}

func (u *unifiedExchangeAdaptor) updateInventory(balanceDiffs *BotInventoryDiffs) {
	u.updateInventoryEvent(u.updateBalances(balanceDiffs))
}

func (u *unifiedExchangeAdaptor) Book() (buys, sells []*core.MiniOrder, _ error) {
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

	adaptor := &unifiedExchangeAdaptor{
		market:          mkt,
		clientCore:      cfg.core,
		CEX:             cfg.cex,
		botID:           cfg.botID,
		log:             cfg.log,
		eventLogDB:      cfg.eventLogDB,
		initialBalances: initialBalances,

		baseDexBalances:     baseDEXBalances,
		baseCexBalances:     baseCEXBalances,
		autoRebalanceConfig: cfg.autoRebalanceConfig,
		pendingDEXOrders:    make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:    make(map[string]*pendingCEXOrder),
		pendingDeposits:     make(map[string]*pendingDeposit),
		pendingWithdrawals:  make(map[string]*pendingWithdrawal),
		mwh:                 cfg.mwh,
		inventoryMods:       make(map[uint32]int64),
	}

	adaptor.fiatRates.Store(map[uint32]float64{})
	adaptor.botCfgV.Store(cfg.botCfg)

	return adaptor, nil
}
