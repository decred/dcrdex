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

// botBalance keeps track of the amount of funds available for a
// bot's use, locked to fund orders, and pending.
type botBalance struct {
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
	swap       uint64
	redemption uint64
	refund     uint64
	funding    uint64
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
	OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error)
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
	SubscribeTradeUpdates() (updates <-chan *libxc.Trade, unsubscribe func())
	CEXTrade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error)
	SufficientBalanceForCEXTrade(baseID, quoteID uint32, sell bool, rate, qty uint64) (bool, error)
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	PrepareRebalance(ctx context.Context, assetID uint32) (rebalance int64, dexReserves, cexReserves uint64)
	FreeUpFunds(assetID uint32, cex bool, amt uint64, currEpoch uint64)
	Deposit(ctx context.Context, assetID uint32, amount uint64) error
	Withdraw(ctx context.Context, assetID uint32, amount uint64) error
}

// pendingWithdrawal represents a withdrawal from a CEX that has been
// initiated, but the DEX has not yet received.
type pendingWithdrawal struct {
	assetID uint32
	// amtWithdrawn is the amount the CEX balance is decreased by.
	// It will not be the same as the amount received in the dex wallet.
	amtWithdrawn uint64
}

// pendingDeposit represents a deposit to a CEX that has not yet been
// confirmed.
type pendingDeposit struct {
	assetID uint32
	amtSent uint64

	mtx         sync.RWMutex
	fee         uint64
	amtCredited uint64
	// feeConfirmed is relevant assets with dynamic fees where the fee is
	// not known until the tx is mined.
	feeConfirmed bool
	cexConfirmed bool
	// cexConfirmed == true && success == false means the CEX did not credit the
	// deposit for some reason.
	success bool
}

// pendingDEXOrder keeps track of the balance effects of a pending DEX order.
// The actual order is not stored here, only its effects on the balance.
type pendingDEXOrder struct {
	balancesMtx   sync.RWMutex
	availableDiff map[uint32]int64
	locked        map[uint32]uint64
	pending       map[uint32]uint64
	order         *core.Order

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

	subscriptionIDMtx sync.RWMutex
	subscriptionID    *int

	withdrawalNoncePrefix string
	withdrawalNonce       atomic.Uint64

	feesMtx  sync.RWMutex
	buyFees  *orderFees
	sellFees *orderFees

	balancesMtx sync.RWMutex
	// baseDEXBalance/baseCEXBalance are the balances the bots have before
	// taking into account any pending actions. These are updated whenever
	// a pending action is completed.
	baseDexBalances    map[uint32]uint64
	baseCexBalances    map[uint32]uint64
	pendingDEXOrders   map[order.OrderID]*pendingDEXOrder
	pendingCEXOrders   map[string]*libxc.Trade
	pendingWithdrawals map[string]*pendingWithdrawal
	pendingDeposits    map[string]*pendingDeposit

	// If pendingBaseRebalance/pendingQuoteRebalance are true, it means
	// there is a pending deposit/withdrawal of the base/quote asset,
	// and no other deposits/withdrawals of that asset should happen
	// until it is complete.
	pendingBaseRebalance  atomic.Bool
	pendingQuoteRebalance atomic.Bool
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
			bal, err := u.DEXBalance(assetID)
			if err != nil {
				return false, err
			}
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
	fees := buyFees
	if sell {
		fees = sellFees
	}

	if balances[fromFeeAsset] < fees.funding {
		return false, nil
	}
	balances[fromFeeAsset] -= fees.funding

	fromQty := qty
	if !sell {
		fromQty = calc.BaseToQuote(rate, qty)
	}
	if balances[fromAsset] < fromQty {
		return false, nil
	}
	balances[fromAsset] -= fromQty

	numLots := qty / mkt.LotSize
	if balances[fromFeeAsset] < numLots*fees.swap {
		return false, nil
	}
	balances[fromFeeAsset] -= numLots * fees.swap

	if u.isAccountLocker(fromAsset) {
		if balances[fromFeeAsset] < numLots*fees.refund {
			return false, nil
		}
		balances[fromFeeAsset] -= numLots * fees.refund
	}

	if u.isAccountLocker(toAsset) {
		if balances[toFeeAsset] < numLots*fees.redemption {
			return false, nil
		}
		balances[toFeeAsset] -= numLots * fees.redemption
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

	fromAssetBal, err := u.CEXBalance(fromAssetID)
	if err != nil {
		return false, err
	}

	return fromAssetBal.Available >= fromAssetQty, nil
}

// dexOrderInfo is used by MultiTrade to keep track of the placement index
// and counter trade rate of an order.
type dexOrderInfo struct {
	placementIndex   uint64
	counterTradeRate uint64
}

// addPendingDexOrders adds new orders to the pendingDEXOrders map. balancesMtx
// must be locked before calling this method. idToInfo maps orders to their
// placement index and counter trade rate.
func (u *unifiedExchangeAdaptor) addPendingDexOrders(orders []*core.Order, idToInfo map[order.OrderID]dexOrderInfo) {
	for _, o := range orders {
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		fromAsset, fromFeeAsset, _, toFeeAsset := orderAssets(o.BaseID, o.QuoteID, o.Sell)

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

		orderInfo := idToInfo[orderID]
		u.pendingDEXOrders[orderID] = &pendingDEXOrder{
			swaps:            make(map[string]*asset.WalletTransaction),
			redeems:          make(map[string]*asset.WalletTransaction),
			refunds:          make(map[string]*asset.WalletTransaction),
			availableDiff:    availableDiff,
			locked:           locked,
			pending:          map[uint32]uint64{},
			order:            o,
			placementIndex:   orderInfo.placementIndex,
			counterTradeRate: orderInfo.counterTradeRate,
		}
	}
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
		u.log.Errorf("GroupedMultiTrade: error getting market: %v", err)
		return nil
	}
	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		u.log.Errorf("GroupedMultiTrade: error getting order fees: %v", err)
		return nil
	}
	fees := buyFees
	if sell {
		fees = sellFees
	}
	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(mkt.BaseID, mkt.QuoteID, sell)

	// First, determine the amount of balances the bot has available to place
	// DEX trades taking into account dexReserves.
	remainingBalances := map[uint32]uint64{}
	for _, assetID := range []uint32{fromAsset, fromFeeAsset, toAsset, toFeeAsset} {
		if _, found := remainingBalances[assetID]; !found {
			bal, err := u.DEXBalance(assetID)
			if err != nil {
				u.log.Errorf("GroupedMultiTrade: error getting dex balance: %v", err)
				return nil
			}
			availableBalance := bal.Available
			if dexReserves != nil {
				if dexReserves[assetID] > availableBalance {
					u.log.Errorf("GroupedMultiTrade: insufficient dex balance for reserves. required: %d, have: %d", dexReserves[assetID], availableBalance)
					return nil
				}
				availableBalance -= dexReserves[assetID]
			}
			remainingBalances[assetID] = availableBalance
		}
	}
	if remainingBalances[fromFeeAsset] < fees.funding {
		u.log.Debugf("GroupedMultiTrade: insufficient balance for funding fees. required: %d, have: %d", fees.funding, remainingBalances[fromFeeAsset])
		return nil
	}
	remainingBalances[fromFeeAsset] -= fees.funding

	// If the placements include a counterTradeRate, the CEX balance must also
	// be taken into account to determine how many trades can be placed.
	accountForCEXBal := placements[0].counterTradeRate > 0
	var remainingCEXBal uint64
	if accountForCEXBal {
		cexBal, err := u.CEXBalance(toAsset)
		if err != nil {
			u.log.Errorf("GroupedMultiTrade: error getting cex balance: %v", err)
			return nil
		}
		remainingCEXBal = cexBal.Available
		reserves := cexReserves[toAsset]
		if remainingCEXBal < reserves {
			u.log.Errorf("GroupedMultiTrade: insufficient CEX balance for reserves. required: %d, have: %d", cexReserves, remainingCEXBal)
			return nil
		}
		remainingCEXBal -= reserves
	}

	u.balancesMtx.RLock()

	cancels := make([]dex.Bytes, 0, len(placements))
	addCancel := func(o *core.Order) {
		if currEpoch-o.Epoch < 2 { // TODO: check epoch
			u.log.Debugf("GroupedMultiTrade: skipping cancel not past free cancel threshold")
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
			u.log.Errorf("GroupedMultiTrade: insufficient CEX balance for counter trades. required: %d, have: %d", reserved, remainingCEXBal)
			return nil
		}
		remainingCEXBal -= reserved
	}

	rateCausesSelfMatch := u.rateCausesSelfMatchFunc(sell)

	// corePlacements will be the placements that are passed to core's
	// MultiTrade function.
	corePlacements := make([]*core.QtyRate, 0, len(requiredPlacements))
	orderInfos := make([]*dexOrderInfo, 0, len(requiredPlacements))
	for i, placement := range requiredPlacements {
		if placement.lots == 0 {
			continue
		}

		if rateCausesSelfMatch(placement.rate) {
			u.log.Warnf("GroupedMultiTrade: rate %d causes self match. Placements should be farther from mid-gap.", placement.rate)
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

			if remainingBalances[fromFeeAsset] < fees.swap {
				break
			}
			remainingBalances[fromFeeAsset] -= fees.swap

			if u.isAccountLocker(fromAsset) {
				if remainingBalances[fromFeeAsset] < fees.refund {
					break
				}
				remainingBalances[fromFeeAsset] -= fees.refund
			}

			if u.isAccountLocker(toAsset) {
				if remainingBalances[toFeeAsset] < fees.redemption {
					break
				}
				remainingBalances[toFeeAsset] -= fees.redemption
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
			corePlacements = append(corePlacements, &core.QtyRate{
				Qty:  lotsToPlace * mkt.LotSize,
				Rate: placement.rate,
			})
			orderInfos = append(orderInfos, &dexOrderInfo{
				placementIndex:   uint64(i),
				counterTradeRate: placement.counterTradeRate,
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
			u.log.Errorf("GroupedMultiTrade: error canceling order %s: %v", cancel, err)
		}
	}

	if len(corePlacements) > 0 {
		var walletOptions map[string]string
		if sell {
			walletOptions = u.baseWalletOptions
		} else {
			walletOptions = u.quoteWalletOptions
		}

		fromBalance, err := u.DEXBalance(fromAsset)
		if err != nil {
			u.log.Errorf("GroupedMultiTrade: error getting dex balance: %v", err)
			return nil
		}

		multiTradeForm := &core.MultiTradeForm{
			Host:       u.market.Host,
			Base:       u.market.BaseID,
			Quote:      u.market.QuoteID,
			Sell:       sell,
			Placements: corePlacements,
			Options:    walletOptions,
			MaxLock:    fromBalance.Available,
		}

		u.balancesMtx.Lock()
		defer u.balancesMtx.Unlock()

		orders, err := u.clientCore.MultiTrade([]byte{}, multiTradeForm)
		if err != nil {
			u.log.Errorf("GroupedMultiTrade: error placing orders: %v", err)
			return nil
		}

		// The return value contains a nil order ID if no order was made for the placement
		// at that index.
		toReturn := make([]*order.OrderID, len(placements))
		idToInfo := make(map[order.OrderID]dexOrderInfo)
		for i, o := range orders {
			info := orderInfos[i]
			var orderID order.OrderID
			copy(orderID[:], o.ID)
			toReturn[info.placementIndex] = &orderID
			idToInfo[orderID] = *info
		}

		u.addPendingDexOrders(orders, idToInfo)

		return toReturn
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

	fromAsset := u.market.QuoteID
	if sell {
		fromAsset = u.market.BaseID
	}
	fromBalance, err := u.DEXBalance(fromAsset)
	if err != nil {
		return nil, err
	}

	var walletOptions map[string]string
	if sell {
		walletOptions = u.baseWalletOptions
	} else {
		walletOptions = u.quoteWalletOptions
	}

	multiTradeForm := &core.MultiTradeForm{
		Host:  u.market.Host,
		Base:  u.market.BaseID,
		Quote: u.market.QuoteID,
		Sell:  sell,
		Placements: []*core.QtyRate{
			{
				Qty:  qty,
				Rate: rate,
			},
		},
		Options: walletOptions,
		MaxLock: fromBalance.Available,
	}

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	// MultiTrade is used instead of Trade because Trade does not support
	// maxLock.
	orders, err := u.clientCore.MultiTrade([]byte{}, multiTradeForm)
	if err != nil {
		return nil, err
	}

	u.addPendingDexOrders(orders, map[order.OrderID]dexOrderInfo{})

	if len(orders) == 0 {
		return nil, fmt.Errorf("no orders placed")
	}
	return orders[0], nil
}

// DEXBalance returns the bot's balance for a specific asset on the DEX.
func (u *unifiedExchangeAdaptor) DEXBalance(assetID uint32) (*botBalance, error) {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	bal, found := u.baseDexBalances[assetID]
	if !found {
		return &botBalance{}, nil
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
			totalAvailableDiff -= int64(pendingDeposit.amtSent)
		}

		pendingDeposit.mtx.RLock()
		fee := int64(pendingDeposit.fee)
		pendingDeposit.mtx.RUnlock()
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

	return &botBalance{
		Available: availableBalance,
		Locked:    totalLocked,
		Pending:   totalPending,
	}, nil
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

// CEXBalance returns the balance of the bot on the CEX.
func (u *unifiedExchangeAdaptor) CEXBalance(assetID uint32) (*botBalance, error) {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	var available, totalFundingOrder, totalPending uint64
	var totalAvailableDiff int64
	for _, tx := range u.pendingCEXOrders {
		if tx.BaseID == assetID || tx.QuoteID == assetID {
			if tx.Complete {
				u.log.Errorf("pending cex order %s is complete", tx.ID)
				continue
			}
			availableDiff, fundingOrder := incompleteCexTradeBalanceEffects(tx)
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
		deposit.mtx.RLock()
		if deposit.assetID == assetID {
			totalPending += deposit.amtCredited
		}
		deposit.mtx.RUnlock()
	}

	if totalAvailableDiff < 0 && u.baseCexBalances[assetID] < uint64(-totalAvailableDiff) {
		u.log.Errorf("bot %s asset %d available balance %d is less than total available diff %d", u.botID, assetID, u.baseCexBalances[assetID], totalAvailableDiff)
		available = 0
	} else {
		available = u.baseCexBalances[assetID] + uint64(totalAvailableDiff)
	}

	return &botBalance{
		Available: available,
		Locked:    totalFundingOrder,
		Pending:   totalPending,
	}, nil
}

// updatePendingDeposit applies the balance effects of the deposit to the bot's
// dex and cex base balances after both the fees of the deposit transaction and
// the amount credited by the CEX are confirmed.
func (u *unifiedExchangeAdaptor) updatePendingDeposit(txID string, deposit *pendingDeposit) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	_, found := u.pendingDeposits[txID]
	if !found {
		u.log.Errorf("%s not found among pending deposits", txID)
		return
	}

	deposit.mtx.RLock()
	defer deposit.mtx.RUnlock()

	if !deposit.cexConfirmed || !deposit.feeConfirmed {
		u.pendingDeposits[txID] = deposit
		return
	}

	if u.baseDexBalances[deposit.assetID] < deposit.amtSent {
		u.log.Errorf("%s balance on dex %d < deposit amt %d",
			dex.BipIDSymbol(deposit.assetID), u.baseDexBalances[deposit.assetID], deposit.amtSent)
		u.baseDexBalances[deposit.assetID] = 0
	} else {
		u.baseDexBalances[deposit.assetID] -= deposit.amtSent
	}

	var feeAsset uint32
	token := asset.TokenInfo(deposit.assetID)
	if token != nil {
		feeAsset = token.ParentID
	} else {
		feeAsset = deposit.assetID
	}
	if u.baseDexBalances[feeAsset] < deposit.fee {
		u.log.Errorf("%s balance on dex %d < deposit fee %d",
			dex.BipIDSymbol(feeAsset), u.baseDexBalances[feeAsset], deposit.fee)
		u.baseDexBalances[feeAsset] = 0
	} else {
		u.baseDexBalances[feeAsset] -= deposit.fee
	}

	if deposit.success {
		u.baseCexBalances[deposit.assetID] += deposit.amtCredited
	}

	delete(u.pendingDeposits, txID)

	if deposit.assetID == u.market.BaseID {
		u.pendingBaseRebalance.Store(false)
	} else {
		u.pendingQuoteRebalance.Store(false)
	}

	dexDiffs := map[uint32]int64{}
	cexDiffs := map[uint32]int64{}
	dexDiffs[deposit.assetID] -= int64(deposit.amtSent)
	dexDiffs[feeAsset] -= int64(deposit.fee)
	msg := fmt.Sprintf("Deposit %s complete.", txID)
	if deposit.success {
		cexDiffs[deposit.assetID] += int64(deposit.amtCredited)
	} else {
		msg = fmt.Sprintf("Deposit %s complete, but not successful.", txID)
	}
	u.logBalanceAdjustments(dexDiffs, cexDiffs, msg)
}

func (u *unifiedExchangeAdaptor) confirmWalletTransaction(ctx context.Context, assetID uint32, txID string, confirmedTx func(*asset.WalletTransaction)) {
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
				confirmedTx(tx)
				return
			}

			timer.Reset(10 * time.Minute)
		case <-ctx.Done():
			return
		}
	}
}

// Deposit deposits funds to the CEX. The deposited funds will be removed from
// the bot's wallet balance and allocated to the bot's CEX balance. After both
// the fees of the deposit transaction are confirmed by the wallet and the
// CEX confirms the amount they received, the onConfirm callback is called.
func (u *unifiedExchangeAdaptor) Deposit(ctx context.Context, assetID uint32, amount uint64) error {
	balance, err := u.DEXBalance(assetID)
	if err != nil {
		return err
	}
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

	getAmtAndFee := func() (uint64, uint64, bool) {
		tx, err := u.clientCore.WalletTransaction(assetID, coin.TxID())
		if err != nil {
			u.log.Errorf("Error getting wallet transaction: %v", err)
			return amount, 0, false
		}
		return tx.Amount, tx.Fees, tx.Confirmed
	}

	amt, fee, _ := getAmtAndFee()
	isDynamicSwapper := u.isDynamicSwapper(assetID)
	deposit := &pendingDeposit{
		assetID:      assetID,
		amtSent:      amt,
		fee:          fee,
		feeConfirmed: !isDynamicSwapper,
	}

	u.balancesMtx.Lock()
	u.pendingDeposits[coin.TxID()] = deposit
	u.balancesMtx.Unlock()

	if u.isDynamicSwapper(assetID) {
		confirmedTx := func(tx *asset.WalletTransaction) {
			deposit.mtx.Lock()
			deposit.amtSent = tx.Amount
			deposit.fee = tx.Fees
			deposit.feeConfirmed = true
			deposit.mtx.Unlock()

			u.updatePendingDeposit(coin.TxID(), deposit)
		}

		go u.confirmWalletTransaction(ctx, assetID, coin.TxID(), confirmedTx)
	}

	cexConfirmedDeposit := func(success bool, creditedAmt uint64) {
		deposit.mtx.Lock()
		deposit.cexConfirmed = true
		deposit.success = success
		deposit.amtCredited = creditedAmt

		if success && deposit.amtCredited != deposit.amtSent {
			u.log.Warnf("CEX credited less to deposit %s than was sent. sent: %d, credited: %d", coin.TxID(), deposit.amtSent, deposit.amtCredited)
		}
		if !success {
			u.log.Warnf("CEX did not credit deposit %s. Check with the CEX about the reason.", coin.TxID())
		}
		deposit.mtx.Unlock()

		u.updatePendingDeposit(coin.TxID(), deposit)
	}

	u.CEX.ConfirmDeposit(ctx, coin.TxID(), cexConfirmedDeposit)

	return nil
}

// pendingWithdrawalComplete is called after a withdrawal has been confirmed.
// The amount received is applied to the base balance, and the withdrawal is
// removed from the pending map.
func (u *unifiedExchangeAdaptor) pendingWithdrawalComplete(id string, amtReceived uint64) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	withdrawal, found := u.pendingWithdrawals[id]
	if !found {
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
	u.baseDexBalances[withdrawal.assetID] += amtReceived
	delete(u.pendingWithdrawals, id)

	dexDiffs := map[uint32]int64{withdrawal.assetID: int64(amtReceived)}
	cexDiffs := map[uint32]int64{withdrawal.assetID: -int64(withdrawal.amtWithdrawn)}
	u.logBalanceAdjustments(dexDiffs, cexDiffs, fmt.Sprintf("Withdrawal %s complete.", id))
}

// Withdraw withdraws funds from the CEX. After withdrawing, the CEX is queried
// for the transaction ID. After the transaction ID is available, the wallet is
// queried for the amount received.
func (u *unifiedExchangeAdaptor) Withdraw(ctx context.Context, assetID uint32, amount uint64) error {
	symbol := dex.BipIDSymbol(assetID)

	balance, err := u.CEXBalance(assetID)
	if err != nil {
		return err
	}
	if balance.Available < amount {
		return fmt.Errorf("bot has insufficient balance to withdraw %s. required: %v, have: %v", symbol, amount, balance.Available)
	}

	addr, err := u.clientCore.NewDepositAddress(assetID)
	if err != nil {
		return err
	}

	withdrawalID := fmt.Sprintf("%s%d", u.withdrawalNoncePrefix, u.withdrawalNonce.Add(1))

	confirmWithdrawal := func(amt uint64, txID string) {
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
					u.pendingWithdrawalComplete(withdrawalID, tx.Amount)
					return
				}

				timer = time.NewTimer(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	err = u.CEX.Withdraw(ctx, assetID, amount, addr, confirmWithdrawal)
	if err != nil {
		return err
	}

	if assetID == u.market.BaseID {
		u.pendingBaseRebalance.Store(true)
	} else {
		u.pendingQuoteRebalance.Store(true)
	}

	u.pendingWithdrawals[withdrawalID] = &pendingWithdrawal{
		assetID:      assetID,
		amtWithdrawn: amount,
	}

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
		bal, err := u.CEXBalance(assetID)
		if err != nil {
			u.log.Errorf("Error getting %s balance on cex: %v", dex.BipIDSymbol(assetID), err)
			return
		}
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
		bal, err := u.DEXBalance(assetID)
		if err != nil {
			u.log.Errorf("Error getting %s balance: %v", dex.BipIDSymbol(assetID), err)
			return
		}
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
	dexBalance, err := u.DEXBalance(assetID)
	if err != nil {
		u.log.Errorf("Error getting %s balance: %v", dex.BipIDSymbol(assetID), err)
		return
	}
	totalDexBalance := dexBalance.Available + dexBalance.Locked

	cexBalance, err := u.CEXBalance(assetID)
	if err != nil {
		u.log.Errorf("Error getting %s balance on cex: %v", dex.BipIDSymbol(assetID), err)
		return
	}

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
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	if _, found := u.pendingCEXOrders[trade.ID]; !found {
		return
	}

	if !trade.Complete {
		u.pendingCEXOrders[trade.ID] = trade
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

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	trade, err := u.CEX.Trade(ctx, baseID, quoteID, sell, rate, qty, *subscriptionID)
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
		u.pendingCEXOrders[trade.ID] = trade
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
	baseRate := u.fiatRate(u.market.BaseID)
	quoteRate := u.fiatRate(u.market.QuoteID)
	if baseRate == 0 || quoteRate == 0 {
		return 0
	}

	market, err := u.ExchangeMarket(u.market.Host, u.market.BaseID, u.market.QuoteID)
	if err != nil {
		u.log.Errorf("Error getting market: %v", err)
		return 0
	}

	return market.ConventionalRateToMsg(baseRate / quoteRate)
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

// OrderFeesInUnits returns the swap and redemption fees for either a buy or
// sell order in units of either the base or quote asset. If either the base
// or quote asset is a token, the fees are converted using fiat rates.
// Otherwise, the rate parameter is used for the conversion.
func (u *unifiedExchangeAdaptor) OrderFeesInUnits(sell, base bool, rate uint64) (uint64, error) {
	convertTokenAssetFees := func(fees uint64, token *asset.Token) (uint64, error) {
		var unitsAsset uint32
		if base {
			unitsAsset = u.market.BaseID
		} else {
			unitsAsset = u.market.QuoteID
		}

		parentFiatRate := u.fiatRate(token.ParentID)
		if parentFiatRate == 0 {
			return 0, fmt.Errorf("no fiat rate available for asset %d", token.ParentID)
		}
		fiatRate := u.fiatRate(unitsAsset)
		if fiatRate == 0 {
			return 0, fmt.Errorf("no fiat rate available for asset %d", unitsAsset)
		}
		baseParentUnitInfo, err := asset.UnitInfo(token.ParentID)
		if err != nil {
			return 0, fmt.Errorf("error getting unit info: %v", err)
		}
		unitInfo, err := asset.UnitInfo(unitsAsset)
		if err != nil {
			return 0, fmt.Errorf("error getting unit info: %v", err)
		}

		feeConv := float64(fees) / float64(baseParentUnitInfo.Conventional.ConversionFactor)
		receivingAssetConv := feeConv * parentFiatRate / fiatRate
		return uint64(receivingAssetConv * float64(unitInfo.Conventional.ConversionFactor)), nil
	}

	buyFees, sellFees, err := u.orderFees()
	if err != nil {
		return 0, fmt.Errorf("error getting order fees: %v", err)
	}

	var baseFees, quoteFees uint64
	if sell {
		baseFees += sellFees.swap
		quoteFees += sellFees.redemption
	} else {
		baseFees += buyFees.redemption
		quoteFees += buyFees.swap
	}

	var baseFeesInUnits, quoteFeesInUnits uint64

	baseToken := asset.TokenInfo(u.market.BaseID)
	if baseToken != nil {
		baseFeesInUnits, err = convertTokenAssetFees(baseFees, baseToken)
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
		quoteFeesInUnits, err = convertTokenAssetFees(quoteFees, quoteToken)
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

	availableDiff[fromAsset] -= int64(o.LockedAmt)
	availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
	availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
	if o.FeesPaid != nil {
		availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
	}

	locked[fromAsset] += o.LockedAmt
	locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	locked[toFeeAsset] += o.RedeemLockedAmt

	for _, tx := range pendingOrder.swaps {
		availableDiff[fromAsset] -= int64(tx.Amount)
		availableDiff[fromFeeAsset] -= int64(tx.Fees)
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
	pendingOrder.balancesMtx.Unlock()

	orderUpdates := u.orderUpdates.Load()
	if orderUpdates != nil {
		orderUpdates.(chan *core.Order) <- o
	}

	// If complete, remove the order from the pending list, and update the
	// bot's balance.
	if dexOrderComplete(o) {
		u.balancesMtx.Lock()
		defer u.balancesMtx.Unlock()

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

		if adjustedBals {
			u.logBalanceAdjustments(availableDiff, nil, fmt.Sprintf("DEX order %s complete.", orderID))
		}
	}
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
	case *core.FiatRatesNote:
		u.fiatRates.Store(note.FiatRates)
	}
}

// updateFeeRates updates the cached fee rates for placing orders on the market
// specified by the exchangeAdaptorCfg used to create the unifiedExchangeAdaptor.
func (u *unifiedExchangeAdaptor) updateFeeRates() error {
	buySwapFees, buyRedeemFees, buyRefundFees, err := u.clientCore.SingleLotFees(&core.SingleLotFeesForm{
		Host:          u.market.Host,
		Base:          u.market.BaseID,
		Quote:         u.market.QuoteID,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
	})
	if err != nil {
		return fmt.Errorf("failed to get buy single lot fees: %v", err)
	}

	sellSwapFees, sellRedeemFees, sellRefundFees, err := u.clientCore.SingleLotFees(&core.SingleLotFeesForm{
		Host:          u.market.Host,
		Base:          u.market.BaseID,
		Quote:         u.market.QuoteID,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
		Sell:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to get sell single lot fees: %v", err)
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
		swap:       buySwapFees,
		redemption: buyRedeemFees,
		refund:     buyRefundFees,
		funding:    buyFundingFees,
	}
	u.sellFees = &orderFees{
		swap:       sellSwapFees,
		redemption: sellRedeemFees,
		refund:     sellRefundFees,
		funding:    sellFundingFees,
	}

	return nil
}

func (u *unifiedExchangeAdaptor) run(ctx context.Context) {
	u.fiatRates.Store(u.clientCore.FiatConversionRates())

	err := u.updateFeeRates()
	if err != nil {
		u.log.Errorf("Error updating fee rates: %v", err)
	}

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
}

// unifiedExchangeAdaptorForBot returns a unifiedExchangeAdaptor for the specified bot.
func unifiedExchangeAdaptorForBot(cfg *exchangeAdaptorCfg) *unifiedExchangeAdaptor {
	return &unifiedExchangeAdaptor{
		clientCore:         cfg.core,
		CEX:                cfg.cex,
		botID:              cfg.botID,
		log:                cfg.log,
		maxBuyPlacements:   cfg.maxBuyPlacements,
		maxSellPlacements:  cfg.maxSellPlacements,
		baseWalletOptions:  cfg.baseWalletOptions,
		quoteWalletOptions: cfg.quoteWalletOptions,
		rebalanceCfg:       cfg.rebalanceCfg,

		baseDexBalances:    cfg.baseDexBalances,
		baseCexBalances:    cfg.baseCexBalances,
		pendingDEXOrders:   make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:   make(map[string]*libxc.Trade),
		pendingDeposits:    make(map[string]*pendingDeposit),
		pendingWithdrawals: make(map[string]*pendingWithdrawal),
		market:             cfg.market,
	}
}
