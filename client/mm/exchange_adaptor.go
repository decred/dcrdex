// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
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
	ExchangeMarket(host string, base, quote uint32) (*core.Market, error)
	MultiTrade(placements []*multiTradePlacement, sell bool, driftTolerance float64, currEpoch uint64, dexReserves, cexReserves map[uint32]uint64) []*order.OrderID
	CancelAllOrders() bool
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
	SubscribeTradeUpdates() (updates <-chan *libxc.Trade, unsubscribe func())
	CEXTrade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error)
	SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error)
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	MidGap(baseID, quoteID uint32) uint64
	PrepareRebalance(ctx context.Context, assetID uint32) (rebalance int64, dexReserves, cexReserves uint64)
	FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64)
	Deposit(ctx context.Context, assetID uint32, amount uint64) error
	Withdraw(ctx context.Context, assetID uint32, amount uint64) error
}

// pendingWithdrawal represents a withdrawal from a CEX that has been
// initiated, but the DEX has not yet received.
type pendingWithdrawal struct {
	eventLogID uint64
	timestamp  int64

	assetID uint32
	// amtWithdrawn is the amount the CEX balance is decreased by.
	// It will not be the same as the amount received in the dex wallet.
	amtWithdrawn uint64
}

// pendingDeposit represents a deposit to a CEX that has not yet been
// confirmed.
type pendingDeposit struct {
	assetID uint32
	tx      *asset.WalletTransaction
}

// pendingDEXOrder keeps track of the balance effects of a pending DEX order.
// The actual order is not stored here, only its effects on the balance.
type pendingDEXOrder struct {
	eventLogID uint64
	timestamp  int64

	balancesMtx       sync.RWMutex
	availableDiff     map[uint32]int64
	locked            map[uint32]uint64
	pending           map[uint32]uint64
	order             *core.Order
	baseDelta         int64
	quoteDelta        int64
	pendingBaseDelta  uint64
	pendingQuoteDelta uint64
	baseFees          uint64
	quoteFees         uint64

	// swaps, redeems, and refunds are caches of transactions. This avoids
	// having to query the wallet for transactions that are already confirmed.
	txsMtx  sync.RWMutex
	swaps   map[string]*asset.WalletTransaction
	redeems map[string]*asset.WalletTransaction
	refunds map[string]*asset.WalletTransaction

	// placementIndex/counterTradeRate are used by MultiTrade to know
	// which orders to place/cancel.
	placementIndex   uint64
	counterTradeRate uint64
}

type pendingCEXOrder struct {
	eventLogID uint64
	timestamp  int64
	trade      *libxc.Trade
}

// unifiedExchangeAdaptor implements both botCoreAdaptor and botCexAdaptor.
type unifiedExchangeAdaptor struct {
	clientCore
	libxc.CEX

	botID              string
	log                dex.Logger
	fiatRates          atomic.Value // map[uint32]float64
	orderUpdates       atomic.Value // chan *core.Order
	market             *MarketWithHost
	baseWalletOptions  map[string]string
	quoteWalletOptions map[string]string
	maxBuyPlacements   uint32
	maxSellPlacements  uint32
	rebalanceCfg       *AutoRebalanceConfig
	eventLogDB         eventLogDB
	botCfg             *BotConfig
	initialBalances    map[uint32]uint64

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
	// a pending action is completed.
	baseDexBalances    map[uint32]uint64
	baseCexBalances    map[uint32]uint64
	pendingDEXOrders   map[order.OrderID]*pendingDEXOrder
	pendingCEXOrders   map[string]*pendingCEXOrder
	pendingWithdrawals map[string]*pendingWithdrawal
	pendingDeposits    map[string]*pendingDeposit

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
		tradedUSD         atomic.Uint64
		feeGapStats       atomic.Value
	}
}

var _ botCoreAdaptor = (*unifiedExchangeAdaptor)(nil)
var _ botCexAdaptor = (*unifiedExchangeAdaptor)(nil)

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
	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(u.market.BaseID, u.market.QuoteID, sell)
	balances := map[uint32]uint64{}
	for _, assetID := range []uint32{fromAsset, fromFeeAsset, toAsset, toFeeAsset} {
		if _, found := balances[assetID]; !found {
			bal := u.DEXBalance(assetID)
			balances[assetID] = bal.Available
		}
	}

	mkt, err := u.ExchangeMarket(u.market.Host, u.market.BaseID, u.market.QuoteID)
	if err != nil {
		return false, err
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

	numLots := qty / mkt.LotSize
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
		fromAssetID = u.market.BaseID
		fromAssetQty = qty
	} else {
		fromAssetID = u.market.QuoteID
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
	o.txsMtx.RUnlock()

	o.balancesMtx.RLock()
	e := &MarketMakingEvent{
		ID:         o.eventLogID,
		TimeStamp:  o.timestamp,
		Pending:    !complete,
		BaseDelta:  o.baseDelta,
		QuoteDelta: o.quoteDelta,
		BaseFees:   o.baseFees,
		QuoteFees:  o.quoteFees,
		DEXOrderEvent: &DEXOrderEvent{
			ID:           o.order.ID.String(),
			Rate:         o.order.Rate,
			Qty:          o.order.Qty,
			Sell:         o.order.Sell,
			Transactions: transactions,
		},
	}
	o.balancesMtx.RUnlock()

	u.eventLogDB.storeEvent(u.startTime.Load(), u.market, e, u.balanceState())
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

	u.eventLogDB.storeEvent(u.startTime.Load(), u.market, e, u.balanceState())
	u.notifyEvent(e)
}

// updateDepositEvent updates the event log with the current state of a
// pending deposit and sends an event notification.
func (u *unifiedExchangeAdaptor) updateDepositEvent(eventID uint64, timestamp int64, assetID uint32, tx *asset.WalletTransaction, amtCredited uint64, complete bool) {
	var baseDelta, quoteDelta, diff int64
	var baseFees, quoteFees uint64
	if complete && tx.Amount > amtCredited {
		diff = int64(tx.Amount - amtCredited)
	}
	if assetID == u.market.BaseID {
		baseDelta = -diff
		baseFees = tx.Fees
	} else {
		quoteDelta = -diff
		quoteFees = tx.Fees
	}
	e := &MarketMakingEvent{
		ID:         eventID,
		TimeStamp:  timestamp,
		BaseDelta:  baseDelta,
		QuoteDelta: quoteDelta,
		BaseFees:   baseFees,
		QuoteFees:  quoteFees,
		Pending:    !complete,
		DepositEvent: &DepositEvent{
			AssetID:     assetID,
			Transaction: tx,
			CEXCredit:   amtCredited,
		},
	}
	u.eventLogDB.storeEvent(u.startTime.Load(), u.market, e, u.balanceState())
	u.notifyEvent(e)
}

// updateWithdrawalEvent updates the event log with the current state of a
// pending withdrawal and sends an event notification.
func (u *unifiedExchangeAdaptor) updateWithdrawalEvent(withdrawalID string, withdrawal *pendingWithdrawal, tx *asset.WalletTransaction) {
	var baseFees, quoteFees uint64
	complete := tx != nil && tx.Confirmed
	if complete && withdrawal.amtWithdrawn > tx.Amount {
		if withdrawal.assetID == u.market.BaseID {
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

	u.eventLogDB.storeEvent(u.startTime.Load(), u.market, e, u.balanceState())
	u.notifyEvent(e)
}

// groupedBookedOrders returns pending dex orders grouped by the placement
// index used to create them when they were placed with MultiTrade.
func (u *unifiedExchangeAdaptor) groupedBookedOrders(sells bool) (orders map[uint64][]*pendingDEXOrder) {
	orders = make(map[uint64][]*pendingDEXOrder)

	groupPendingOrder := func(pendingOrder *pendingDEXOrder) {
		pendingOrder.balancesMtx.RLock()
		defer pendingOrder.balancesMtx.RUnlock()

		if pendingOrder.order.Status > order.OrderStatusBooked {
			return
		}

		pi := pendingOrder.placementIndex
		if sells == pendingOrder.order.Sell {
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
			if sell && o.order.Rate > highestExistingBuy {
				highestExistingBuy = o.order.Rate
			}
			if !sell && o.order.Rate < lowestExistingSell {
				lowestExistingSell = o.order.Rate
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

// reservedForCounterTrade returns the amount of funds the CEX needs to make
// counter trades if all of the pending DEX orders are filled.
func reservedForCounterTrade(orderGroups map[uint64][]*pendingDEXOrder) uint64 {
	var reserved uint64
	for _, g := range orderGroups {
		for _, o := range g {
			if o.order.Sell {
				reserved += calc.BaseToQuote(o.counterTradeRate, o.order.Qty-o.order.Filled)
			} else {
				reserved += o.order.Qty - o.order.Filled
			}
		}
	}
	return reserved
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

	var walletOptions map[string]string
	if sell {
		walletOptions = u.baseWalletOptions
	} else {
		walletOptions = u.quoteWalletOptions
	}

	fromAsset, fromFeeAsset, _, toFeeAsset := orderAssets(u.market.BaseID, u.market.QuoteID, sell)
	multiTradeForm := &core.MultiTradeForm{
		Host:       u.market.Host,
		Base:       u.market.BaseID,
		Quote:      u.market.QuoteID,
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

		u.pendingDEXOrders[orderID] = &pendingDEXOrder{
			eventLogID:       u.eventLogID.Add(1),
			timestamp:        time.Now().Unix(),
			swaps:            make(map[string]*asset.WalletTransaction),
			redeems:          make(map[string]*asset.WalletTransaction),
			refunds:          make(map[string]*asset.WalletTransaction),
			availableDiff:    availableDiff,
			locked:           locked,
			pending:          map[uint32]uint64{},
			order:            o,
			placementIndex:   placements[i].placementIndex,
			counterTradeRate: placements[i].counterTradeRate,
			baseFees:         baseFees,
			quoteFees:        quoteFees,
		}
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
	mkt, err := u.ExchangeMarket(u.market.Host, u.market.BaseID, u.market.QuoteID)
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
			if err != nil {
				u.log.Errorf("MultiTrade: error getting dex balance: %v", err)
				return nil
			}
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
		if err != nil {
			u.log.Errorf("MultiTrade: error getting cex balance: %v", err)
			return nil
		}
		remainingCEXBal = cexBal.Available
		reserves := cexReserves[toAsset]
		if remainingCEXBal < reserves {
			u.log.Errorf("MultiTrade: insufficient CEX balance for reserves. required: %d, have: %d", cexReserves, remainingCEXBal)
			return nil
		}
		remainingCEXBal -= reserves
	}

	u.balancesMtx.RLock()

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
	// ordersWithinTolerance is a list of pending orders that are within
	// tolerance of their currently expected rate, in decreasing order
	// by placementIndex. If the bot doesn't have enough balance to place
	// an order with a higher priority (lower placementIndex) then the
	// lower priority orders will be cancelled.
	ordersWithinTolerance := make([]*pendingDEXOrder, 0, len(placements))
	for _, p := range placements {
		pCopy := *p
		requiredPlacements = append(requiredPlacements, &pCopy)
	}
	for _, groupedOrders := range pendingOrders {
		for _, o := range groupedOrders {
			if !withinTolerance(o.order.Rate, placements[o.placementIndex].rate, driftTolerance) {
				addCancel(o.order)
			} else {
				ordersWithinTolerance = append([]*pendingDEXOrder{o}, ordersWithinTolerance...)
			}

			if requiredPlacements[o.placementIndex].lots > (o.order.Qty-o.order.Filled)/mkt.LotSize {
				requiredPlacements[o.placementIndex].lots -= (o.order.Qty - o.order.Filled) / mkt.LotSize
			} else {
				requiredPlacements[o.placementIndex].lots = 0
			}
		}
	}

	if accountForCEXBal {
		reserved := reservedForCounterTrade(pendingOrders)
		if remainingCEXBal < reserved {
			u.log.Errorf("MultiTrade: insufficient CEX balance for counter trades. required: %d, have: %d", reserved, remainingCEXBal)
			return nil
		}
		remainingCEXBal -= reserved
	}

	rateCausesSelfMatch := u.rateCausesSelfMatchFunc(sell)

	orderInfos := make([]*dexOrderInfo, 0, len(requiredPlacements))
	for i, placement := range requiredPlacements {
		if placement.lots == 0 {
			continue
		}

		if rateCausesSelfMatch(placement.rate) {
			u.log.Warnf("MultiTrade: rate %d causes self match. Placements should be farther from mid-gap.", placement.rate)
			continue
		}

		var lotsToPlace uint64
		for l := 0; l < int(placement.lots); l++ {
			qty := mkt.LotSize
			if !sell {
				qty = calc.BaseToQuote(placement.rate, mkt.LotSize)
			}
			if remainingBalances[fromAsset] < qty {
				break
			}
			remainingBalances[fromAsset] -= qty

			if remainingBalances[fromFeeAsset] < fees.Swap {
				break
			}
			remainingBalances[fromFeeAsset] -= fees.Swap

			if u.isAccountLocker(fromAsset) {
				if remainingBalances[fromFeeAsset] < fees.Refund {
					break
				}
				remainingBalances[fromFeeAsset] -= fees.Refund
			}

			if u.isAccountLocker(toAsset) {
				if remainingBalances[toFeeAsset] < fees.Redeem {
					break
				}
				remainingBalances[toFeeAsset] -= fees.Redeem
			}

			if accountForCEXBal {
				counterTradeQty := mkt.LotSize
				if sell {
					counterTradeQty = calc.BaseToQuote(placement.counterTradeRate, mkt.LotSize)
				}
				if remainingCEXBal < counterTradeQty {
					break
				}
				remainingCEXBal -= counterTradeQty
			}

			lotsToPlace = uint64(l + 1)
		}

		if lotsToPlace > 0 {
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
			for _, o := range ordersWithinTolerance {
				if o.placementIndex > uint64(i) {
					addCancel(o.order)
				}
			}

			break
		}
	}

	u.balancesMtx.RUnlock()

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

// dexBalance returns the bot's balance for a specific asset on the DEX.
func (u *unifiedExchangeAdaptor) dexBalance(assetID uint32) *BotBalance {
	bal, found := u.baseDexBalances[assetID]
	if !found {
		return &BotBalance{}
	}

	var totalAvailableDiff int64
	var totalLocked, totalPending uint64

	for _, pendingOrder := range u.pendingDEXOrders {
		pendingOrder.balancesMtx.RLock()
		totalAvailableDiff += pendingOrder.availableDiff[assetID]
		totalLocked += pendingOrder.locked[assetID]
		totalPending += pendingOrder.pending[assetID]
		pendingOrder.balancesMtx.RUnlock()
	}

	for _, pendingDeposit := range u.pendingDeposits {
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
	}

	for _, pendingWithdrawal := range u.pendingWithdrawals {
		if pendingWithdrawal.assetID == assetID {
			totalPending += pendingWithdrawal.amtWithdrawn
		}
	}

	availableBalance := bal
	if totalAvailableDiff >= 0 {
		availableBalance += uint64(totalAvailableDiff)
	} else {
		if availableBalance < uint64(-totalAvailableDiff) {
			u.log.Errorf("bot %s totalAvailableDiff %d exceeds available balance %d", u.botID, totalAvailableDiff, availableBalance)
			availableBalance = 0
		} else {
			availableBalance -= uint64(-totalAvailableDiff)
		}
	}

	return &BotBalance{
		Available: availableBalance,
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

func (u *unifiedExchangeAdaptor) cexBalance(assetID uint32) *BotBalance {
	var available, totalFundingOrder, totalPending uint64
	var totalAvailableDiff int64
	for _, pendingOrder := range u.pendingCEXOrders {
		trade := pendingOrder.trade
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
			totalPending += deposit.tx.Amount
		}
	}

	if totalAvailableDiff < 0 && u.baseCexBalances[assetID] < uint64(-totalAvailableDiff) {
		u.log.Errorf("bot %s asset %d available balance %d is less than total available diff %d", u.botID, assetID, u.baseCexBalances[assetID], totalAvailableDiff)
		available = 0
	} else {
		available = u.baseCexBalances[assetID] + uint64(totalAvailableDiff)
	}

	return &BotBalance{
		Available: available,
		Locked:    totalFundingOrder,
		Pending:   totalPending,
	}
}

// CEXBalance returns the balance of the bot on the CEX.
func (u *unifiedExchangeAdaptor) CEXBalance(assetID uint32) *BotBalance {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	return u.cexBalance(assetID)
}

func (u *unifiedExchangeAdaptor) totalBalances() map[uint32]*BotBalance {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	fromAsset, toAsset, fromFeeAsset, toFeeAsset := orderAssets(u.market.BaseID, u.market.QuoteID, true)

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

	return balances
}

func (u *unifiedExchangeAdaptor) balanceState() *BalanceState {
	return &BalanceState{
		FiatRates: u.fiatRates.Load().(map[uint32]float64),
		Balances:  u.totalBalances(),
	}
}

// updatePendingDeposit applies the balance effects of the deposit to the bot's
// dex and cex base balances after both the fees of the deposit transaction and
// the amount credited by the CEX are confirmed.
func (u *unifiedExchangeAdaptor) updatePendingDeposit(assetID uint32, tx *asset.WalletTransaction, amtCredited, eventID uint64, timestamp int64, complete bool) {
	u.balancesMtx.Lock()
	defer func() {
		u.balancesMtx.Unlock()
		u.updateDepositEvent(eventID, timestamp, assetID, tx, amtCredited, complete)
	}()

	if !complete {
		u.pendingDeposits[tx.ID] = &pendingDeposit{
			assetID: assetID,
			tx:      tx,
		}
		return
	}

	delete(u.pendingDeposits, tx.ID)

	if u.baseDexBalances[assetID] < tx.Amount {
		u.log.Errorf("%s balance on dex %d < deposit amt %d",
			dex.BipIDSymbol(assetID), u.baseDexBalances[assetID], tx.Amount)
		u.baseDexBalances[assetID] = 0
	} else {
		u.baseDexBalances[assetID] -= tx.Amount
	}

	var feeAsset uint32
	token := asset.TokenInfo(assetID)
	if token != nil {
		feeAsset = token.ParentID
	} else {
		feeAsset = assetID
	}

	if u.baseDexBalances[feeAsset] < tx.Fees {
		u.log.Errorf("%s balance on dex %d < deposit fee %d",
			dex.BipIDSymbol(feeAsset), u.baseDexBalances[feeAsset], tx.Fees)
		u.baseDexBalances[feeAsset] = 0
	} else {
		u.baseDexBalances[feeAsset] -= tx.Fees
	}

	u.baseCexBalances[assetID] += amtCredited

	var diff int64
	if tx.Amount > amtCredited {
		diff = int64(tx.Amount - amtCredited)
	}
	if assetID == u.market.BaseID {
		u.runStats.baseBalanceDelta.Add(-diff)
		u.runStats.baseFees.Add(tx.Fees)
	} else {
		u.runStats.quoteBalanceDelta.Add(-diff)
		u.runStats.quoteFees.Add(tx.Fees)
	}

	if assetID == u.market.BaseID {
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

// confirmWalletTransaction is a long running function that waits for the wallet
// transaction to be confirmed. It only returns nil if the context is done.
func (u *unifiedExchangeAdaptor) confirmWalletTransaction(ctx context.Context, assetID uint32, txID string) *asset.WalletTransaction {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			tx, err := u.clientCore.WalletTransaction(assetID, txID)
			if errors.Is(err, asset.CoinNotFoundError) {
				u.log.Warnf("%s wallet does not know about deposit tx: %s", dex.BipIDSymbol(assetID), txID)
			} else if err != nil {
				u.log.Errorf("Error getting wallet transaction: %v", err)
			} else if tx.Confirmed {
				return tx
			}
			timer.Reset(1 * time.Minute)
		case <-ctx.Done():
			return nil
		}
	}
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

	if assetID == u.market.BaseID {
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
	depositTime := time.Now().Unix()
	u.updatePendingDeposit(assetID, tx, 0, eventID, depositTime, false)

	go func() {
		if u.isDynamicSwapper(assetID) {
			tx = u.confirmWalletTransaction(ctx, assetID, tx.ID)
			if tx == nil {
				return
			}
			u.updatePendingDeposit(assetID, tx, 0, eventID, depositTime, false)
		}

		cexConfirmedDeposit := func(creditedAmt uint64) {
			u.updatePendingDeposit(assetID, tx, creditedAmt, eventID, depositTime, true)
			u.sendStatsUpdate()
		}
		u.CEX.ConfirmDeposit(ctx, tx.ID, cexConfirmedDeposit)
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
		u.log.Errorf("Completed withdrawal not found among pending withdrawals")
		return
	}

	if withdrawal.assetID == u.market.BaseID {
		u.pendingBaseRebalance.Store(false)
	} else {
		u.pendingQuoteRebalance.Store(false)
	}

	if u.baseCexBalances[withdrawal.assetID] < withdrawal.amtWithdrawn {
		u.log.Errorf("%s balance on cex %d < withdrawn amt %d",
			dex.BipIDSymbol(withdrawal.assetID), u.baseCexBalances[withdrawal.assetID], withdrawal.amtWithdrawn)
		u.baseCexBalances[withdrawal.assetID] = 0
	} else {
		u.baseCexBalances[withdrawal.assetID] -= withdrawal.amtWithdrawn
	}
	u.baseDexBalances[withdrawal.assetID] += tx.Amount
	delete(u.pendingWithdrawals, id)

	dexDiffs := map[uint32]int64{withdrawal.assetID: int64(tx.Amount)}
	cexDiffs := map[uint32]int64{withdrawal.assetID: -int64(withdrawal.amtWithdrawn)}
	if withdrawal.assetID == u.market.BaseID {
		u.runStats.baseFees.Add(withdrawal.amtWithdrawn - tx.Amount)
	} else {
		u.runStats.quoteFees.Add(withdrawal.amtWithdrawn - tx.Amount)
	}

	u.balancesMtx.Unlock()

	u.updateWithdrawalEvent(id, withdrawal, tx)
	u.sendStatsUpdate()
	u.logBalanceAdjustments(dexDiffs, cexDiffs, fmt.Sprintf("Withdrawal %s complete.", id))
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

	var withdrawalID string
	confirmWithdrawal := func(txID string) {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				tx, err := u.clientCore.WalletTransaction(assetID, txID)
				if errors.Is(err, asset.CoinNotFoundError) {
					u.log.Warnf("%s wallet does not know about withdrawal tx: %s", dex.BipIDSymbol(assetID), txID)
					continue
				}
				if err != nil {
					u.log.Errorf("Error getting wallet transaction: %v", err)
				} else if tx.Confirmed {
					u.pendingWithdrawalComplete(withdrawalID, tx)
					return
				}

				timer = time.NewTimer(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}

	u.balancesMtx.Lock()
	withdrawalID, err = u.CEX.Withdraw(ctx, assetID, amount, addr, confirmWithdrawal)
	if err != nil {
		return err
	}
	if assetID == u.market.BaseID {
		u.pendingBaseRebalance.Store(true)
	} else {
		u.pendingQuoteRebalance.Store(true)
	}
	u.pendingWithdrawals[withdrawalID] = &pendingWithdrawal{
		eventLogID:   u.eventLogID.Add(1),
		timestamp:    time.Now().Unix(),
		assetID:      assetID,
		amtWithdrawn: amount,
	}
	u.balancesMtx.Unlock()

	u.updateWithdrawalEvent(withdrawalID, u.pendingWithdrawals[withdrawalID], nil)
	u.sendStatsUpdate()

	return nil
}

// FreeUpFunds cancels active orders to free up the specified amount of funds
// for a rebalance between the dex and the cex. If the cex parameter is true,
// it means we are freeing up funds for withdrawal. DEX orders that require a
// counter-trade on the CEX are cancelled. The orders are cancelled in reverse
// order of priority (determined by the order in which they were passed into
// MultiTrade).
func (u *unifiedExchangeAdaptor) FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64) {
	var base bool
	if assetID == u.market.BaseID {
		base = true
	} else if assetID != u.market.QuoteID {
		u.log.Errorf("Asset %d is not the base or quote asset of the market", assetID)
		return
	}

	if cex {
		bal := u.CEXBalance(assetID)
		backingBalance := u.cexBalanceBackingDexOrders(assetID)
		if bal.Available < backingBalance {
			u.log.Errorf("CEX balance %d < backing balance %d", bal.Available, backingBalance)
			return
		}

		freeBalance := bal.Available - backingBalance
		if freeBalance >= amt {
			return
		}
		amt -= freeBalance
	} else {
		bal := u.DEXBalance(assetID)
		if bal.Available >= amt {
			return
		}
		amt -= bal.Available
	}

	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

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
		o.balancesMtx.RLock()
		defer o.balancesMtx.RUnlock()

		if cex {
			if o.order.Sell {
				return calc.BaseToQuote(o.counterTradeRate, o.order.Qty-o.order.Filled)
			}
			return o.order.Qty - o.order.Filled
		}

		return o.locked[assetID]
	}

	var totalFreedAmt uint64
	for _, index := range highToLowIndexes {
		ordersForPlacement := orders[index]
		for _, o := range ordersForPlacement {
			// If the order is too recent, just wait for the next epoch to
			// cancel. We still count this order towards the freedAmt in
			// order to not cancel a higher priority trade.
			if currEpoch-o.order.Epoch >= 2 {
				err := u.Cancel(o.order.ID)
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
		dex.BipIDSymbol(assetID), dex.BipIDSymbol(u.market.QuoteID), amt, amt+u.rebalanceCfg.MinQuoteTransfer)
}

func (u *unifiedExchangeAdaptor) cexBalanceBackingDexOrders(assetID uint32) uint64 {
	var base bool
	if assetID == u.market.BaseID {
		base = true
	}

	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	var locked uint64
	for _, pendingOrder := range u.pendingDEXOrders {
		if base == pendingOrder.order.Sell || pendingOrder.counterTradeRate == 0 {
			// sell orders require a buy counter-trade on the CEX, which makes
			// use of the quote asset on the CEX.
			continue
		}

		remaining := pendingOrder.order.Qty - pendingOrder.order.Filled
		if pendingOrder.order.Sell {
			locked += calc.BaseToQuote(pendingOrder.counterTradeRate, remaining)
		} else {
			locked += remaining
		}
	}

	return locked
}

func (u *unifiedExchangeAdaptor) rebalanceAsset(ctx context.Context, assetID uint32, minAmount, minTransferAmount uint64) (toSend int64, dexReserves, cexReserves uint64) {
	dexBalance := u.DEXBalance(assetID)
	totalDexBalance := dexBalance.Available + dexBalance.Locked
	cexBalance := u.CEXBalance(assetID)

	// Don't take into account locked funds on CEX, because we don't do
	// rebalancing while there are active orders on the CEX.
	if (totalDexBalance+cexBalance.Available)/2 < minAmount {
		u.log.Warnf("Cannot rebalance %s because balance is too low on both DEX and CEX. Min amount: %v, CEX balance: %v, DEX Balance: %v",
			dex.BipIDSymbol(assetID), minAmount, cexBalance.Available, totalDexBalance)
		return
	}

	var requireDeposit bool
	if cexBalance.Available < minAmount {
		requireDeposit = true
	} else if totalDexBalance >= minAmount {
		// No need for withdrawal or deposit.
		return
	}

	if requireDeposit {
		amt := (totalDexBalance+cexBalance.Available)/2 - cexBalance.Available
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
		amt := (dexBalance.Available+cexBalance.Available)/2 - dexBalance.Available
		if amt < minTransferAmount {
			u.log.Warnf("Amount required to rebalance %s (%d) is less than the min transfer amount %v",
				dex.BipIDSymbol(assetID), amt, minTransferAmount)
			return
		}

		cexBalanceBackingDexOrders := u.cexBalanceBackingDexOrders(assetID)
		if cexBalance.Available < cexBalanceBackingDexOrders {
			u.log.Errorf("cex reported balance %d is less than amount required to back dex orders %d",
				cexBalance.Available, cexBalanceBackingDexOrders)
			// this is a bug, how to recover?
			return
		}

		if amt > cexBalance.Available-cexBalanceBackingDexOrders {
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
	if u.rebalanceCfg == nil {
		return
	}

	var minAmount uint64
	var minTransferAmount uint64
	if assetID == u.market.BaseID {
		if u.pendingBaseRebalance.Load() {
			return
		}
		minAmount = u.rebalanceCfg.MinBaseAmt
		minTransferAmount = u.rebalanceCfg.MinBaseTransfer
	} else {
		if assetID != u.market.QuoteID {
			u.log.Errorf("assetID %d is not the base or quote asset ID of the market", assetID)
			return
		}
		if u.pendingQuoteRebalance.Load() {
			return
		}
		minAmount = u.rebalanceCfg.MinQuoteAmt
		minTransferAmount = u.rebalanceCfg.MinQuoteTransfer
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
	defer u.balancesMtx.Unlock()

	var found bool
	currCEXOrder, found = u.pendingCEXOrders[trade.ID]
	if !found {
		return
	}

	if !trade.Complete {
		u.pendingCEXOrders[trade.ID].trade = trade
		return
	}

	delete(u.pendingCEXOrders, trade.ID)

	if trade.BaseFilled == 0 && trade.QuoteFilled == 0 {
		u.log.Infof("CEX trade %s completed with zero filled amount", trade.ID)
		return
	}

	diffs := make(map[uint32]int64)

	if trade.Sell {
		if trade.BaseFilled <= u.baseCexBalances[trade.BaseID] {
			u.baseCexBalances[trade.BaseID] -= trade.BaseFilled
		} else {
			u.log.Errorf("CEX trade %s filled %d more than available balance %d of %s.",
				trade.ID, trade.BaseFilled, u.baseCexBalances[trade.BaseID], dex.BipIDSymbol(trade.BaseID))
			u.baseCexBalances[trade.BaseID] = 0
		}
		u.baseCexBalances[trade.QuoteID] += trade.QuoteFilled
		u.runStats.baseBalanceDelta.Add(-int64(trade.BaseFilled))
		u.runStats.quoteBalanceDelta.Add(int64(trade.QuoteFilled))

		diffs[trade.BaseID] = -int64(trade.BaseFilled)
		diffs[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		if trade.QuoteFilled <= u.baseCexBalances[trade.QuoteID] {
			u.baseCexBalances[trade.QuoteID] -= trade.QuoteFilled
		} else {
			u.log.Errorf("CEX trade %s filled %d more than available balance %d of %s.",
				trade.ID, trade.QuoteFilled, u.baseCexBalances[trade.QuoteID], dex.BipIDSymbol(trade.QuoteID))
			u.baseCexBalances[trade.QuoteID] = 0
		}
		u.baseCexBalances[trade.BaseID] += trade.BaseFilled
		u.runStats.baseBalanceDelta.Add(int64(trade.BaseFilled))
		u.runStats.quoteBalanceDelta.Add(-int64(trade.QuoteFilled))

		diffs[trade.QuoteID] = -int64(trade.QuoteFilled)
		diffs[trade.BaseID] = int64(trade.BaseFilled)
	}

	u.logBalanceAdjustments(nil, diffs, fmt.Sprintf("CEX trade %s completed.", trade.ID))
}

// SubscribeTradeUpdates subscribes to trade updates for the bot's trades on
// the CEX. This should be called before making any trades, and only once.
func (w *unifiedExchangeAdaptor) SubscribeTradeUpdates() (<-chan *libxc.Trade, func()) {
	w.subscriptionIDMtx.Lock()
	defer w.subscriptionIDMtx.Unlock()
	if w.subscriptionID != nil {
		w.log.Errorf("SubscribeTradeUpdates called more than once by bot %s", w.botID)
		return nil, nil
	}

	updates, unsubscribe, subscriptionID := w.CEX.SubscribeTradeUpdates()
	w.subscriptionID = &subscriptionID

	ctx, cancel := context.WithCancel(context.Background())
	forwardUnsubscribe := func() {
		cancel()
		unsubscribe()
	}
	forwardUpdates := make(chan *libxc.Trade, 256)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case note := <-updates:
				w.handleCEXTradeUpdate(note)
				forwardUpdates <- note
			}
		}
	}()

	return forwardUpdates, forwardUnsubscribe
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
			u.baseCexBalances[trade.BaseID] -= trade.BaseFilled
			u.baseCexBalances[trade.QuoteID] += trade.QuoteFilled
			diffs[trade.BaseID] = -int64(trade.BaseFilled)
			diffs[trade.QuoteID] = int64(trade.QuoteFilled)
		} else {
			u.baseCexBalances[trade.BaseID] += trade.BaseFilled
			u.baseCexBalances[trade.QuoteID] -= trade.QuoteFilled
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
	atomicCFactor, err := u.atomicConversionRateFromFiat(u.market.BaseID, u.market.QuoteID)
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
	toID := u.market.QuoteID
	if base {
		toID = u.market.BaseID
	}

	convertViaFiat := func(fees uint64, fromID uint32) (uint64, error) {
		atomicCFactor, err := u.atomicConversionRateFromFiat(fromID, toID)
		if err != nil {
			return 0, err
		}
		return uint64(math.Round(float64(fees) * atomicCFactor)), nil
	}

	var baseFeesInUnits, quoteFeesInUnits uint64

	baseToken := asset.TokenInfo(u.market.BaseID)
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

	quoteToken := asset.TokenInfo(u.market.QuoteID)
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

// CancelAllOrders cancels all booked orders. True is returned no orders
// needed to be cancelled.
func (u *unifiedExchangeAdaptor) CancelAllOrders() bool {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	noCancels := true

	for _, pendingOrder := range u.pendingDEXOrders {
		pendingOrder.balancesMtx.RLock()
		if pendingOrder.order.Status <= order.OrderStatusBooked {
			err := u.clientCore.Cancel(pendingOrder.order.ID)
			if err != nil {
				u.log.Errorf("Error canceling order %s: %v", pendingOrder.order.ID, err)
			}
			noCancels = false
		}
		pendingOrder.balancesMtx.RUnlock()
	}

	return noCancels
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

	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(o.BaseID, o.QuoteID, o.Sell)

	pendingOrder.txsMtx.Lock()

	// Add new txs to tx cache
	swaps, redeems, refunds := orderTransactions(o)

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
	availableDiff := make(map[uint32]int64)
	locked := make(map[uint32]uint64)
	pending := make(map[uint32]uint64)

	var baseDelta, quoteDelta int64
	var pendingBaseDelta, pendingQuoteDelta, baseFees, quoteFees uint64

	addFees := func(fromAsset bool, fee uint64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			baseFees += fee
		} else {
			quoteFees += fee
		}
	}
	addDelta := func(fromAsset bool, delta int64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			baseDelta += delta
		} else {
			quoteDelta += delta
		}
	}
	addPendingDelta := func(fromAsset bool, delta uint64) {
		if fromAsset && o.Sell || !fromAsset && !o.Sell {
			pendingBaseDelta += delta
		} else {
			pendingQuoteDelta += delta
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

	availableDiff[fromAsset] -= int64(o.LockedAmt)
	availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
	availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
	if o.FeesPaid != nil {
		availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
		addFees(true, o.FeesPaid.Funding)
	}

	locked[fromAsset] += o.LockedAmt
	locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	locked[toFeeAsset] += o.RedeemLockedAmt

	for _, tx := range pendingOrder.swaps {
		availableDiff[fromAsset] -= int64(tx.Amount)
		availableDiff[fromFeeAsset] -= int64(tx.Fees)
		addFees(true, tx.Fees)
		addDelta(true, -int64(tx.Amount))
	}
	for _, tx := range pendingOrder.redeems {
		isDynamicSwapper := u.isDynamicSwapper(toAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			availableDiff[toFeeAsset] -= int64(tx.Fees)
		} else {
			pending[toFeeAsset] -= tx.Fees
		}
		addFees(false, tx.Fees)
		addDelta(false, int64(tx.Amount))

		if tx.Confirmed {
			availableDiff[toAsset] += int64(tx.Amount)
		} else {
			pending[toAsset] += tx.Amount
		}
	}
	for _, tx := range pendingOrder.refunds {
		isDynamicSwapper := u.isDynamicSwapper(fromAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			availableDiff[fromFeeAsset] -= int64(tx.Fees)
		} else {
			pending[fromFeeAsset] -= tx.Fees
		}
		addFees(true, tx.Fees)
		addDelta(true, int64(tx.Amount))

		if tx.Confirmed {
			availableDiff[fromAsset] += int64(tx.Amount)
		} else {
			pending[fromAsset] += tx.Amount
		}
	}
	pendingOrder.txsMtx.Unlock()

	// Update the balances
	pendingOrder.balancesMtx.Lock()
	pendingOrder.availableDiff = availableDiff
	pendingOrder.locked = locked
	pendingOrder.pending = pending
	pendingOrder.order = o
	pendingOrder.baseDelta = baseDelta
	pendingOrder.quoteDelta = quoteDelta
	pendingOrder.pendingBaseDelta = pendingBaseDelta
	pendingOrder.pendingQuoteDelta = pendingQuoteDelta
	pendingOrder.baseFees = baseFees
	pendingOrder.quoteFees = quoteFees
	pendingOrder.balancesMtx.Unlock()

	orderUpdates := u.orderUpdates.Load()
	if orderUpdates != nil {
		orderUpdates.(chan *core.Order) <- o
	}

	complete := dexOrderComplete(o)
	// If complete, remove the order from the pending list, and update the
	// bot's balance.

	if complete { // TODO: complete when all fees are confirmed
		u.balancesMtx.Lock()
		delete(u.pendingDEXOrders, orderID)

		adjustedBals := false
		for assetID, diff := range availableDiff {
			adjustedBals = adjustedBals || diff != 0
			if diff > 0 {
				u.baseDexBalances[assetID] += uint64(diff)
			} else {
				if u.baseDexBalances[assetID] < uint64(-diff) {
					u.log.Errorf("bot %s diff %d exceeds available balance %d. Setting balance to 0.", u.botID, diff, u.baseDexBalances[assetID])
					u.baseDexBalances[assetID] = 0
				} else {
					u.baseDexBalances[assetID] -= uint64(-diff)
				}
			}
		}

		u.runStats.baseBalanceDelta.Add(baseDelta)
		u.runStats.quoteBalanceDelta.Add(quoteDelta)
		u.runStats.baseFees.Add(baseFees)
		u.runStats.quoteFees.Add(quoteFees)

		if pendingBaseDelta != 0 || pendingQuoteDelta != 0 {
			u.log.Errorf("bot %s pending delta not zero after order %s complete. base: %d, quote: %d",
				u.botID, orderID, pendingBaseDelta, pendingQuoteDelta)
		}

		if adjustedBals {
			u.logBalanceAdjustments(availableDiff, nil, fmt.Sprintf("DEX order %s complete.", orderID))
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
		if u.botCfg.Host != note.Host || u.market.ID() != note.MarketID {
			return
		}
		if note.Topic() == core.TopicRedemptionConfirmed {
			u.runStats.completedMatches.Add(1)
			fiatRates := u.fiatRates.Load().(map[uint32]float64)
			if r := fiatRates[u.botCfg.BaseID]; r > 0 && note.Match != nil {
				ui, _ := asset.UnitInfo(u.botCfg.BaseID)
				tradedUSD := float64(note.Match.Qty) / float64(ui.Conventional.ConversionFactor) * r
				u.runStats.tradedUSD.Add(math.Float64bits(tradedUSD))
			}
		}
	case *core.FiatRatesNote:
		u.fiatRates.Store(note.FiatRates)
	}
}

// updateFeeRates updates the cached fee rates for placing orders on the market
// specified by the exchangeAdaptorCfg used to create the unifiedExchangeAdaptor.
func (u *unifiedExchangeAdaptor) updateFeeRates() error {
	maxBaseFees, maxQuoteFees, err := marketFees(u.clientCore, u.market.Host, u.market.BaseID, u.market.QuoteID, true)
	if err != nil {
		return err
	}

	estBaseFees, estQuoteFees, err := marketFees(u.clientCore, u.market.Host, u.market.BaseID, u.market.QuoteID, false)
	if err != nil {
		return err
	}

	buyFundingFees, err := u.clientCore.MaxFundingFees(u.market.QuoteID, u.market.Host, u.maxBuyPlacements, u.baseWalletOptions)
	if err != nil {
		return fmt.Errorf("failed to get buy funding fees: %v", err)
	}

	sellFundingFees, err := u.clientCore.MaxFundingFees(u.market.BaseID, u.market.Host, u.maxSellPlacements, u.quoteWalletOptions)
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

// run must complete running before calls are made to other methods.
func (u *unifiedExchangeAdaptor) run(ctx context.Context) error {
	fiatRates := u.clientCore.FiatConversionRates()
	u.fiatRates.Store(fiatRates)

	err := u.updateFeeRates()
	if err != nil {
		u.log.Errorf("Error updating fee rates: %v", err)
	}

	startTime := time.Now().Unix()
	u.startTime.Store(startTime)

	err = u.eventLogDB.storeNewRun(startTime, u.market, u.botCfg, u.balanceState())
	if err != nil {
		return fmt.Errorf("failed to store new run in event log db: %v", err)
	}

	go func() {
		<-ctx.Done()
		u.eventLogDB.endRun(startTime, u.market, time.Now().Unix())
	}()

	// Listen for core notifications
	go func() {
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

	go func() {
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

	return nil
}

// RunStats is a snapshot of the bot's balances and performance at a point in
// time.
type RunStats struct {
	InitialBalances    map[uint32]uint64      `json:"initialBalances"`
	DEXBalances        map[uint32]*BotBalance `json:"dexBalances"`
	CEXBalances        map[uint32]*BotBalance `json:"cexBalances"`
	ProfitLoss         float64                `json:"profitLoss"`
	StartTime          int64                  `json:"startTime"`
	PendingDeposits    int                    `json:"pendingDeposits"`
	PendingWithdrawals int                    `json:"pendingWithdrawals"`
	CompletedMatches   uint32                 `json:"completedMatches"`
	TradedUSD          float64                `json:"tradedUSD"`
	FeeGap             *FeeGapStats           `json:"feeGap"`
}

func calcRunProfitLoss(initialBalances, finalBalances map[uint32]uint64, fiatRates map[uint32]float64) float64 {
	var profitLoss float64

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

	for assetID := range assetIDs {
		initialBalance, err := convertToConventional(assetID, int64(initialBalances[assetID]))
		if err != nil {
			continue
		}
		finalBalance, err := convertToConventional(assetID, int64(finalBalances[assetID]))
		if err != nil {
			continue
		}

		fiatRate := fiatRates[assetID]
		if fiatRate == 0 {
			continue
		}

		profitLoss += (finalBalance - initialBalance) * fiatRate
	}

	return profitLoss
}

func (u *unifiedExchangeAdaptor) stats() (*RunStats, error) {
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

	// Effects of pendingWithdrawals are applied when the withdrawal is
	// complete.
	return &RunStats{
		InitialBalances:    u.initialBalances,
		DEXBalances:        dexBalances,
		CEXBalances:        cexBalances,
		ProfitLoss:         calcRunProfitLoss(u.initialBalances, totalBalances, fiatRates),
		StartTime:          u.startTime.Load(),
		PendingDeposits:    len(u.pendingDeposits),
		PendingWithdrawals: len(u.pendingWithdrawals),
		CompletedMatches:   u.runStats.completedMatches.Load(),
		TradedUSD:          math.Float64frombits(u.runStats.tradedUSD.Load()),
		FeeGap:             feeGap,
	}, nil
}

func (u *unifiedExchangeAdaptor) sendStatsUpdate() {
	stats, err := u.stats()
	if err != nil {
		u.log.Errorf("Error getting stats: %v", err)
		return
	}

	u.clientCore.Broadcast(newRunStatsNote(u.market.Host, u.market.BaseID, u.market.QuoteID, stats))
}

func (u *unifiedExchangeAdaptor) notifyEvent(e *MarketMakingEvent) {
	u.clientCore.Broadcast(newRunEventNote(u.market.Host, u.market.BaseID, u.market.QuoteID, u.startTime.Load(), e))
}

func (u *unifiedExchangeAdaptor) registerFeeGap(s *FeeGapStats) {
	u.runStats.feeGapStats.Store(s)
}

type exchangeAdaptorCfg struct {
	botID              string
	market             *MarketWithHost
	baseDexBalances    map[uint32]uint64
	baseCexBalances    map[uint32]uint64
	core               clientCore
	cex                libxc.CEX
	maxBuyPlacements   uint32
	maxSellPlacements  uint32
	baseWalletOptions  map[string]string
	quoteWalletOptions map[string]string
	rebalanceCfg       *AutoRebalanceConfig
	log                dex.Logger
	eventLogDB         eventLogDB
	botCfg             *BotConfig
}

// unifiedExchangeAdaptorForBot returns a unifiedExchangeAdaptor for the specified bot.
func unifiedExchangeAdaptorForBot(cfg *exchangeAdaptorCfg) *unifiedExchangeAdaptor {
	initialBalances := make(map[uint32]uint64, len(cfg.baseDexBalances))
	for assetID, balance := range cfg.baseDexBalances {
		initialBalances[assetID] = balance
	}
	for assetID, balance := range cfg.baseCexBalances {
		initialBalances[assetID] += balance
	}

	adaptor := &unifiedExchangeAdaptor{
		clientCore:         cfg.core,
		CEX:                cfg.cex,
		botID:              cfg.botID,
		log:                cfg.log,
		maxBuyPlacements:   cfg.maxBuyPlacements,
		maxSellPlacements:  cfg.maxSellPlacements,
		baseWalletOptions:  cfg.baseWalletOptions,
		quoteWalletOptions: cfg.quoteWalletOptions,
		rebalanceCfg:       cfg.rebalanceCfg,
		eventLogDB:         cfg.eventLogDB,
		botCfg:             cfg.botCfg,
		initialBalances:    initialBalances,

		baseDexBalances:    cfg.baseDexBalances,
		baseCexBalances:    cfg.baseCexBalances,
		pendingDEXOrders:   make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:   make(map[string]*pendingCEXOrder),
		pendingDeposits:    make(map[string]*pendingDeposit),
		pendingWithdrawals: make(map[string]*pendingWithdrawal),
		market:             cfg.market,
	}

	adaptor.fiatRates.Store(map[uint32]float64{})

	return adaptor
}
