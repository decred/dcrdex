// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// ArbMarketMakingPlacement is the configuration for an order placement
// on the DEX order book based on the existing orders on a CEX order book.
type ArbMarketMakingPlacement struct {
	Lots       uint64  `json:"lots"`
	Multiplier float64 `json:"multiplier"`
}

// ArbMarketMakerConfig is the configuration for a market maker that places
// orders on both sides of the DEX order book, at rates where there are
// profitable counter trades on a CEX order book. Whenever a DEX order is
// filled, the opposite trade will immediately be made on the CEX.
//
// Each placement in BuyPlacements and SellPlacements represents an order
// that will be made on the DEX order book. The first placement will be
// placed at a rate closest to the CEX mid-gap, and each subsequent one
// will get farther.
//
// The bot calculates the extrema rate on the CEX order book where it can
// buy or sell the quantity of lots specified in the placement multiplied
// by the multiplier amount. This will be the rate of the expected counter
// trade. The bot will then place an order on the DEX order book where if
// both trades are filled, the bot will earn the profit specified in the
// configuration.
//
// The multiplier is important because it ensures that even if some of the
// trades closest to the mid-gap on the CEX order book are filled before
// the bot's orders on the DEX are matched, the bot will still be able to
// earn the expected profit.
//
// Consider the following example:
//
//	Market:
//		DCR/BTC, lot size = 10 DCR.
//
//	Sell Placements:
//		1. { Lots: 1, Multiplier: 1.5 }
//		2. { Lots 1, Multiplier: 1.0 }
//
//	 Profit:
//	   0.01 (1%)
//
//	CEX Asks:
//		1. 10 DCR @ .005 BTC/DCR
//		2. 10 DCR @ .006 BTC/DCR
//		3. 10 DCR @ .007 BTC/DCR
//
// For the first placement, the bot will find the rate at which it can
// buy 15 DCR (1 lot * 1.5 multiplier). This rate is .006 BTC/DCR. Therefore,
// it will place place a sell order at .00606 BTC/DCR (.006 BTC/DCR * 1.01).
//
// For the second placement, the bot will go deeper into the CEX order book
// and find the rate at which it can buy 25 DCR. This is the previous 15 DCR
// used for the first placement plus the Quantity * Multiplier of the second
// placement. This rate is .007 BTC/DCR. Therefore it will place a sell order
// at .00707 BTC/DCR (.007 BTC/DCR * 1.01).
type ArbMarketMakerConfig struct {
	BuyPlacements      []*ArbMarketMakingPlacement `json:"buyPlacements"`
	SellPlacements     []*ArbMarketMakingPlacement `json:"sellPlacements"`
	Profit             float64                     `json:"profit"`
	DriftTolerance     float64                     `json:"driftTolerance"`
	NumEpochsLeaveOpen uint64                      `json:"orderPersistence"`
	BaseOptions        map[string]string           `json:"baseOptions"`
	QuoteOptions       map[string]string           `json:"quoteOptions"`
	// AutoRebalance determines how the bot will handle rebalancing of the
	// assets between the dex and the cex. If nil, no rebalancing will take
	// place.
	AutoRebalance *AutoRebalanceConfig `json:"autoRebalance"`
}

// autoRebalanceReserves keeps track of the amount of the balances that are
// reserved for an upcoming rebalance. These will be deducted from the
// available balance when placing new orders.
type autoRebalanceReserves struct {
	baseDexReserves  uint64
	baseCexReserves  uint64
	quoteDexReserves uint64
	quoteCexReserves uint64
}

func (r *autoRebalanceReserves) get(base, cex bool) uint64 {
	if base {
		if cex {
			return r.baseCexReserves
		}
		return r.baseDexReserves
	}
	if cex {
		return r.quoteCexReserves
	}
	return r.quoteDexReserves
}

func (r *autoRebalanceReserves) set(base, cex bool, amt uint64) {
	if base {
		if cex {
			r.baseCexReserves = amt
		} else {
			r.baseDexReserves = amt
		}
	} else {
		if cex {
			r.quoteCexReserves = amt
		} else {
			r.quoteDexReserves = amt
		}
	}
}

func (r *autoRebalanceReserves) zero() {
	r.baseDexReserves = 0
	r.baseCexReserves = 0
	r.quoteDexReserves = 0
	r.quoteCexReserves = 0
}

type arbMarketMaker struct {
	ctx              context.Context
	host             string
	baseID           uint32
	quoteID          uint32
	cex              cex
	core             clientCore
	log              dex.Logger
	cfg              *ArbMarketMakerConfig
	mkt              *core.Market
	book             dexOrderBook
	rebalanceRunning atomic.Bool
	currEpoch        atomic.Uint64

	ordMtx         sync.RWMutex
	ords           map[order.OrderID]*core.Order
	oidToPlacement map[order.OrderID]int

	matchesMtx  sync.RWMutex
	matchesSeen map[order.MatchID]bool

	cexTradesMtx sync.RWMutex
	cexTrades    map[string]uint64

	feesMtx  sync.RWMutex
	buyFees  *orderFees
	sellFees *orderFees

	reserves autoRebalanceReserves

	pendingBaseRebalance  atomic.Bool
	pendingQuoteRebalance atomic.Bool
}

// groupedOrders returns the buy and sell orders grouped by placement index.
func (m *arbMarketMaker) groupedOrders() (buys, sells map[int][]*groupedOrder) {
	m.ordMtx.RLock()
	defer m.ordMtx.RUnlock()
	return groupOrders(m.ords, m.oidToPlacement, m.mkt.LotSize)
}

func (a *arbMarketMaker) handleCEXTradeUpdate(update *libxc.TradeUpdate) {
	a.log.Debugf("CEX trade update: %+v", update)

	if update.Complete {
		a.cexTradesMtx.Lock()
		delete(a.cexTrades, update.TradeID)
		a.cexTradesMtx.Unlock()
		return
	}
}

// processDEXMatch checks to see if this is the first time the bot has seen
// this match. If so, it sends a trade to the CEX.
func (a *arbMarketMaker) processDEXMatch(o *core.Order, match *core.Match) {
	var matchID order.MatchID
	copy(matchID[:], match.MatchID)

	a.matchesMtx.Lock()
	if a.matchesSeen[matchID] {
		a.matchesMtx.Unlock()
		return
	}
	a.matchesSeen[matchID] = true
	a.matchesMtx.Unlock()

	var cexRate uint64
	if o.Sell {
		cexRate = uint64(float64(match.Rate) / (1 + a.cfg.Profit))
	} else {
		cexRate = uint64(float64(match.Rate) * (1 + a.cfg.Profit))
	}
	cexRate = steppedRate(cexRate, a.mkt.RateStep)

	a.cexTradesMtx.Lock()
	defer a.cexTradesMtx.Unlock()

	tradeID, err := a.cex.Trade(a.ctx, a.baseID, a.quoteID, !o.Sell, cexRate, match.Qty)
	if err != nil {
		a.log.Errorf("Error sending trade to CEX: %v", err)
		return
	}

	// Keep track of the epoch in which the trade was sent to the CEX. This way
	// the bot can cancel the trade if it is not filled after a certain number
	// of epochs.
	a.cexTrades[tradeID] = a.currEpoch.Load()
}

func (a *arbMarketMaker) processDEXMatchNote(note *core.MatchNote) {
	var oid order.OrderID
	copy(oid[:], note.OrderID)

	a.ordMtx.RLock()
	o, found := a.ords[oid]
	a.ordMtx.RUnlock()
	if !found {
		return
	}

	a.processDEXMatch(o, note.Match)
}

func (a *arbMarketMaker) processDEXOrderNote(note *core.OrderNote) {
	var oid order.OrderID
	copy(oid[:], note.Order.ID)

	a.ordMtx.Lock()
	o, found := a.ords[oid]
	if !found {
		a.ordMtx.Unlock()
		return
	}
	a.ords[oid] = note.Order
	a.ordMtx.Unlock()

	for _, match := range note.Order.Matches {
		a.processDEXMatch(o, match)
	}

	if !note.Order.Status.IsActive() {
		a.ordMtx.Lock()
		delete(a.ords, oid)
		delete(a.oidToPlacement, oid)
		a.ordMtx.Unlock()

		a.matchesMtx.Lock()
		for _, match := range note.Order.Matches {
			var matchID order.MatchID
			copy(matchID[:], match.MatchID)
			delete(a.matchesSeen, matchID)
		}
		a.matchesMtx.Unlock()
	}
}

func (a *arbMarketMaker) vwap(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	return a.cex.VWAP(a.baseID, a.quoteID, sell, qty)
}

type arbMMRebalancer interface {
	vwap(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	groupedOrders() (buys, sells map[int][]*groupedOrder)
}

func (a *arbMarketMaker) placeMultiTrade(placements []*rateLots, sell bool) {
	qtyRates := make([]*core.QtyRate, 0, len(placements))
	for _, p := range placements {
		qtyRates = append(qtyRates, &core.QtyRate{
			Qty:  p.lots * a.mkt.LotSize,
			Rate: p.rate,
		})
	}

	var options map[string]string
	if sell {
		options = a.cfg.BaseOptions
	} else {
		options = a.cfg.QuoteOptions
	}

	orders, err := a.core.MultiTrade(nil, &core.MultiTradeForm{
		Host:       a.host,
		Sell:       sell,
		Base:       a.baseID,
		Quote:      a.quoteID,
		Placements: qtyRates,
		Options:    options,
	})
	if err != nil {
		a.log.Errorf("Error placing rebalancing order: %v", err)
		return
	}

	a.ordMtx.Lock()
	for i, ord := range orders {
		var oid order.OrderID
		copy(oid[:], ord.ID)
		a.ords[oid] = ord
		a.oidToPlacement[oid] = placements[i].placementIndex
	}
	a.ordMtx.Unlock()
}

// cancelExpiredCEXTrades cancels any trades on the CEX that have been open for
// more than the number of epochs specified in the config.
func (a *arbMarketMaker) cancelExpiredCEXTrades() {
	currEpoch := a.currEpoch.Load()

	a.cexTradesMtx.RLock()
	defer a.cexTradesMtx.RUnlock()

	for tradeID, epoch := range a.cexTrades {
		if currEpoch-epoch >= a.cfg.NumEpochsLeaveOpen {
			err := a.cex.CancelTrade(a.ctx, a.baseID, a.quoteID, tradeID)
			if err != nil {
				a.log.Errorf("Error canceling CEX trade %s: %v", tradeID, err)
			}
		}
	}
}

func arbMarketMakerRebalance(newEpoch uint64, a arbMMRebalancer, c clientCore, cex cex, cfg *ArbMarketMakerConfig, mkt *core.Market, buyFees,
	sellFees *orderFees, reserves *autoRebalanceReserves, log dex.Logger) (cancels []dex.Bytes, buyOrders, sellOrders []*rateLots) {

	existingBuys, existingSells := a.groupedOrders()

	withinTolerance := func(rate, target uint64) bool {
		driftTolerance := uint64(float64(target) * cfg.DriftTolerance)
		lowerBound := target - driftTolerance
		upperBound := target + driftTolerance
		return rate >= lowerBound && rate <= upperBound
	}

	cancels = make([]dex.Bytes, 0, 1)
	addCancel := func(o *groupedOrder) {
		if newEpoch-o.epoch < 2 {
			log.Debugf("rebalance: skipping cancel not past free cancel threshold")
			return
		}
		cancels = append(cancels, o.id[:])
	}

	baseDEXBalance, err := c.AssetBalance(mkt.BaseID)
	if err != nil {
		log.Errorf("error getting base DEX balance: %v", err)
		return
	}

	quoteDEXBalance, err := c.AssetBalance(mkt.QuoteID)
	if err != nil {
		log.Errorf("error getting quote DEX balance: %v", err)
		return
	}

	baseCEXBalance, err := cex.Balance(mkt.BaseID)
	if err != nil {
		log.Errorf("error getting base CEX balance: %v", err)
		return
	}

	quoteCEXBalance, err := cex.Balance(mkt.QuoteID)
	if err != nil {
		log.Errorf("error getting quote CEX balance: %v", err)
		return
	}

	processSide := func(sell bool) []*rateLots {
		var cfgPlacements []*ArbMarketMakingPlacement
		var existingOrders map[int][]*groupedOrder
		var remainingDEXBalance, remainingCEXBalance, fundingFees uint64
		if sell {
			cfgPlacements = cfg.SellPlacements
			existingOrders = existingSells
			remainingDEXBalance = baseDEXBalance.Available
			remainingCEXBalance = quoteCEXBalance.Available
			fundingFees = sellFees.funding
		} else {
			cfgPlacements = cfg.BuyPlacements
			existingOrders = existingBuys
			remainingDEXBalance = quoteDEXBalance.Available
			remainingCEXBalance = baseCEXBalance.Available
			fundingFees = buyFees.funding
		}

		cexReserves := reserves.get(!sell, true)
		if cexReserves > remainingCEXBalance {
			log.Debugf("rebalance: not enough CEX balance to cover reserves")
			return nil
		}
		remainingCEXBalance -= cexReserves

		dexReserves := reserves.get(sell, false)
		if dexReserves > remainingDEXBalance {
			log.Debugf("rebalance: not enough DEX balance to cover reserves")
			return nil
		}
		remainingDEXBalance -= dexReserves

		// Enough balance on the CEX needs to be maintained for counter-trades
		// for each existing trade on the DEX. Here, we reduce the available
		// balance on the CEX by the amount required for each order on the
		// DEX books.
		for _, ordersForPlacement := range existingOrders {
			for _, o := range ordersForPlacement {
				var requiredOnCEX uint64
				if sell {
					rate := uint64(float64(o.rate) / (1 + cfg.Profit))
					requiredOnCEX = calc.BaseToQuote(rate, o.lots*mkt.LotSize)
				} else {
					requiredOnCEX = o.lots * mkt.LotSize
				}
				if requiredOnCEX <= remainingCEXBalance {
					remainingCEXBalance -= requiredOnCEX
				} else {
					log.Warnf("rebalance: not enough CEX balance to cover existing order. cancelling.")
					addCancel(o)
					remainingCEXBalance = 0
				}
			}
		}
		if remainingCEXBalance == 0 {
			log.Debug("rebalance: not enough CEX balance to place new orders")
			return nil
		}

		if remainingDEXBalance <= fundingFees {
			log.Debug("rebalance: not enough DEX balance to pay funding fees")
			return nil
		}
		remainingDEXBalance -= fundingFees

		// For each placement, we check the rate at which the counter trade can
		// be made on the CEX for the cumulatively required lots * multipliers
		// of the current and all previous placements. If any orders currently
		// on the books are outside of the drift tolerance, they will be
		// cancelled, and if there are less than the required lots on the DEX
		// books, new orders will be added.
		placements := make([]*rateLots, 0, len(cfgPlacements))
		var cumulativeCEXDepth uint64
		for i, cfgPlacement := range cfgPlacements {
			cumulativeCEXDepth += uint64(float64(cfgPlacement.Lots*mkt.LotSize) * cfgPlacement.Multiplier)
			_, extrema, filled, err := a.vwap(sell, cumulativeCEXDepth)
			if err != nil {
				log.Errorf("error calculating vwap: %v", err)
			}
			if !filled {
				log.Infof("CEX %s side has < %d %s on the orderbook.", map[bool]string{true: "sell", false: "buy"}[sell], cumulativeCEXDepth, mkt.BaseSymbol)
				break
			}

			var placementRate uint64
			if sell {
				placementRate = steppedRate(uint64(float64(extrema)*(1+cfg.Profit)), mkt.RateStep)
			} else {
				placementRate = steppedRate(uint64(float64(extrema)/(1+cfg.Profit)), mkt.RateStep)
			}

			ordersForPlacement := existingOrders[i]
			var existingLots uint64
			for _, o := range ordersForPlacement {
				existingLots += o.lots
				if !withinTolerance(o.rate, placementRate) {
					addCancel(o)
				}
			}

			if cfgPlacement.Lots <= existingLots {
				continue
			}
			lotsToPlace := cfgPlacement.Lots - existingLots

			// TODO: handle redeem/refund fees for account lockers
			var requiredOnDEX, requiredOnCEX uint64
			if sell {
				requiredOnDEX = mkt.LotSize * lotsToPlace
				requiredOnDEX += sellFees.swap * lotsToPlace
				requiredOnCEX = calc.BaseToQuote(extrema, mkt.LotSize*lotsToPlace)
			} else {
				requiredOnDEX = calc.BaseToQuote(placementRate, lotsToPlace*mkt.LotSize)
				requiredOnDEX += buyFees.swap * lotsToPlace
				requiredOnCEX = mkt.LotSize * lotsToPlace
			}
			if requiredOnDEX > remainingDEXBalance {
				log.Debugf("not enough DEX balance to place %d lots", lotsToPlace)
				continue
			}
			if requiredOnCEX > remainingCEXBalance {
				log.Debugf("not enough CEX balance to place %d lots", lotsToPlace)
				continue
			}
			remainingDEXBalance -= requiredOnDEX
			remainingCEXBalance -= requiredOnCEX

			placements = append(placements, &rateLots{
				rate:           placementRate,
				lots:           lotsToPlace,
				placementIndex: i,
			})
		}

		return placements
	}

	buys := processSide(false)
	sells := processSide(true)

	return cancels, buys, sells
}

// fundsLockedInOrders returns the total amount of the asset that is
// currently locked in a booked order on the DEX.
func (a *arbMarketMaker) fundsLockedInOrders(base bool) uint64 {
	buys, sells := a.groupedOrders()
	var locked uint64

	var orders map[int][]*groupedOrder
	if base {
		orders = sells
	} else {
		orders = buys
	}

	for _, ordersForPlacement := range orders {
		for _, o := range ordersForPlacement {
			locked += o.lockedAmt
		}
	}

	return locked
}

// dexToCexQty returns the amount of backing asset on the CEX that is required
// for a DEX order of the specified quantity and rate. dexSell indicates that
// we are selling on the DEX, and therefore buying on the CEX.
func (a *arbMarketMaker) dexToCexQty(qty, rate uint64, dexSell bool) uint64 {
	if dexSell {
		cexRate := uint64(float64(rate) * (1 + a.cfg.Profit))
		return calc.BaseToQuote(cexRate, qty)
	}
	return qty
}

// cexBalanceBackingDexOrders returns the amount of the asset on the CEX that
// is required so that if all the orders on the DEX were filled, counter
// trades could be made on the CEX.
func (a *arbMarketMaker) cexBalanceBackingDexOrders(base bool) uint64 {
	buys, sells := a.groupedOrders()
	var orders map[int][]*groupedOrder
	if base {
		orders = buys
	} else {
		orders = sells
	}

	var locked uint64
	for _, ordersForPlacement := range orders {
		for _, o := range ordersForPlacement {
			locked += a.dexToCexQty(o.lots*a.mkt.LotSize, o.rate, !base)
		}
	}

	return locked
}

// freeUpFunds cancels active orders to free up the specified amount of funds
// for a rebalance between the dex and the cex. The orders are cancelled in
// reverse order of priority.
func (a *arbMarketMaker) freeUpFunds(base, cex bool, amt uint64) {
	buys, sells := a.groupedOrders()
	var orders map[int][]*groupedOrder
	if base && !cex || !base && cex {
		orders = sells
	} else {
		orders = buys
	}

	highToLowIndexes := make([]int, 0, len(orders))
	for i := range orders {
		highToLowIndexes = append(highToLowIndexes, i)
	}
	sort.Slice(highToLowIndexes, func(i, j int) bool {
		return highToLowIndexes[i] > highToLowIndexes[j]
	})

	currEpoch := a.currEpoch.Load()

	for _, index := range highToLowIndexes {
		ordersForPlacement := orders[index]
		for _, o := range ordersForPlacement {
			// If the order is too recent, just wait for the next epoch to
			// cancel. We still count this order towards the freedAmt in
			// order to not cancel a higher priority trade.
			if currEpoch-o.epoch >= 2 {
				err := a.core.Cancel(o.id[:])
				if err != nil {
					a.log.Errorf("error cancelling order: %v", err)
					continue
				}
			}
			var freedAmt uint64
			if cex {
				freedAmt = a.dexToCexQty(o.lots*a.mkt.LotSize, o.rate, !base)
			} else {
				freedAmt = o.lockedAmt
			}
			if freedAmt >= amt {
				return
			}
			amt -= freedAmt
		}
	}
}

// rebalanceAssets checks if funds on either the CEX or the DEX are below the
// minimum amount, and if so, initiates either withdrawal or deposit to bring
// them to equal. If some funds that need to be transferred are either locked
// in an order on the DEX, or backing a potential order on the CEX, some orders
// are cancelled to free up funds, and the transfer happens in the next epoch.
func (a *arbMarketMaker) rebalanceAssets() {
	rebalanceAsset := func(base bool) {
		var assetID uint32
		var minAmount uint64
		var minTransferAmount uint64
		if base {
			assetID = a.baseID
			minAmount = a.cfg.AutoRebalance.MinBaseAmt
			minTransferAmount = a.cfg.AutoRebalance.MinBaseTransfer
		} else {
			assetID = a.quoteID
			minAmount = a.cfg.AutoRebalance.MinQuoteAmt
			minTransferAmount = a.cfg.AutoRebalance.MinQuoteTransfer
		}
		symbol := dex.BipIDSymbol(assetID)

		dexAvailableBalance, err := a.core.AssetBalance(assetID)
		if err != nil {
			a.log.Errorf("Error getting %s balance: %v", symbol, err)
			return
		}

		totalDexBalance := dexAvailableBalance.Available + a.fundsLockedInOrders(base)

		cexBalance, err := a.cex.Balance(assetID)
		if err != nil {
			a.log.Errorf("Error getting %s balance on cex: %v", symbol, err)
			return
		}

		if (totalDexBalance+cexBalance.Available)/2 < minAmount {
			a.log.Warnf("Cannot rebalance %s because balance is too low on both DEX and CEX", symbol)
			return
		}

		var requireDeposit bool
		if cexBalance.Available < minAmount {
			requireDeposit = true
		} else if totalDexBalance >= minAmount {
			// No need for withdrawal or deposit.
			return
		}

		onConfirm := func() {
			if base {
				a.pendingBaseRebalance.Store(false)
			} else {
				a.pendingQuoteRebalance.Store(false)
			}
		}

		if requireDeposit {
			amt := (totalDexBalance+cexBalance.Available)/2 - cexBalance.Available
			if amt < minTransferAmount {
				a.log.Warnf("Amount required to rebalance %s (%d) is less than the min transfer amount %v",
					symbol, amt, minTransferAmount)
				return
			}

			// If we need to cancel some orders to send the required amount to
			// the CEX, cancel some orders, and then try again on the next
			// epoch.
			if amt > dexAvailableBalance.Available {
				a.reserves.set(base, false, amt)
				a.freeUpFunds(base, false, amt-dexAvailableBalance.Available)
				return
			}

			err = a.cex.Deposit(a.ctx, assetID, amt, onConfirm)
			if err != nil {
				a.log.Errorf("Error depositing %d to cex: %v", assetID, err)
				return
			}
		} else {
			amt := (totalDexBalance+cexBalance.Available)/2 - totalDexBalance
			if amt < minTransferAmount {
				a.log.Warnf("Amount required to rebalance %s (%d) is less than the min transfer amount %v",
					symbol, amt, minTransferAmount)
				return
			}

			cexBalanceBackingDexOrders := a.cexBalanceBackingDexOrders(base)
			if cexBalance.Available < cexBalanceBackingDexOrders {
				a.log.Errorf("cex reported balance %d is less than amount required to back dex orders %d",
					cexBalance.Available, cexBalanceBackingDexOrders)
				// this is a bug, how to recover?
				return
			}

			if amt > cexBalance.Available-cexBalanceBackingDexOrders {
				a.reserves.set(base, true, amt)
				a.freeUpFunds(base, true, amt-(cexBalance.Available-cexBalanceBackingDexOrders))
				return
			}

			err = a.cex.Withdraw(a.ctx, assetID, amt, onConfirm)
			if err != nil {
				a.log.Errorf("Error withdrawing %d from cex: %v", assetID, err)
				return
			}
		}

		if base {
			a.pendingBaseRebalance.Store(true)
		} else {
			a.pendingQuoteRebalance.Store(true)
		}
	}

	if a.cfg.AutoRebalance == nil {
		return
	}

	a.cexTradesMtx.Lock()
	if len(a.cexTrades) > 0 {
		a.cexTradesMtx.Unlock()
		return
	}
	a.cexTradesMtx.Unlock()

	a.reserves.zero()

	if !a.pendingBaseRebalance.Load() {
		rebalanceAsset(true)
	}
	if !a.pendingQuoteRebalance.Load() {
		rebalanceAsset(false)
	}
}

// rebalance is called on each new epoch. It determines what orders need to be
// placed, cancelled, and what funds need to be transferred between the DEX and
// the CEX.
func (a *arbMarketMaker) rebalance(epoch uint64) {
	if !a.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer a.rebalanceRunning.Store(false)

	currEpoch := a.currEpoch.Load()
	if epoch <= currEpoch {
		return
	}
	a.currEpoch.Store(epoch)

	cancels, buyOrders, sellOrders := arbMarketMakerRebalance(epoch, a, a.core,
		a.cex, a.cfg, a.mkt, a.buyFees, a.sellFees, &a.reserves, a.log)

	for _, cancel := range cancels {
		err := a.core.Cancel(cancel)
		if err != nil {
			a.log.Errorf("Error canceling order %s: %v", cancel, err)
			return
		}
	}
	if len(buyOrders) > 0 {
		a.placeMultiTrade(buyOrders, false)
	}
	if len(sellOrders) > 0 {
		a.placeMultiTrade(sellOrders, true)
	}

	a.cancelExpiredCEXTrades()
	a.rebalanceAssets()
}

func (a *arbMarketMaker) handleNotification(note core.Notification) {
	switch n := note.(type) {
	case *core.MatchNote:
		a.processDEXMatchNote(n)
	case *core.OrderNote:
		a.processDEXOrderNote(n)
	case *core.EpochNotification:
		go a.rebalance(n.Epoch)
	}
}

func (a *arbMarketMaker) cancelAllOrders() {
	a.ordMtx.Lock()
	defer a.ordMtx.Unlock()
	for oid := range a.ords {
		if err := a.core.Cancel(oid[:]); err != nil {
			a.log.Errorf("error cancelling order: %v", err)
		}
	}
	a.ords = make(map[order.OrderID]*core.Order)
	a.oidToPlacement = make(map[order.OrderID]int)
}

func (a *arbMarketMaker) updateFeeRates() error {
	buySwapFees, buyRedeemFees, buyRefundFees, err := a.core.SingleLotFees(&core.SingleLotFeesForm{
		Host:          a.host,
		Base:          a.baseID,
		Quote:         a.quoteID,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
	})
	if err != nil {
		return fmt.Errorf("failed to get fees: %v", err)
	}

	sellSwapFees, sellRedeemFees, sellRefundFees, err := a.core.SingleLotFees(&core.SingleLotFeesForm{
		Host:          a.host,
		Base:          a.baseID,
		Quote:         a.quoteID,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
		Sell:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to get fees: %v", err)
	}

	buyFundingFees, err := a.core.MaxFundingFees(a.quoteID, a.host, uint32(len(a.cfg.BuyPlacements)), a.cfg.QuoteOptions)
	if err != nil {
		return fmt.Errorf("failed to get funding fees: %v", err)
	}

	sellFundingFees, err := a.core.MaxFundingFees(a.baseID, a.host, uint32(len(a.cfg.SellPlacements)), a.cfg.BaseOptions)
	if err != nil {
		return fmt.Errorf("failed to get funding fees: %v", err)
	}

	a.feesMtx.Lock()
	defer a.feesMtx.Unlock()

	a.buyFees = &orderFees{
		swap:       buySwapFees,
		redemption: buyRedeemFees,
		funding:    buyFundingFees,
		refund:     buyRefundFees,
	}
	a.sellFees = &orderFees{
		swap:       sellSwapFees,
		redemption: sellRedeemFees,
		funding:    sellFundingFees,
		refund:     sellRefundFees,
	}

	return nil
}

func (a *arbMarketMaker) run() {
	book, bookFeed, err := a.core.SyncBook(a.host, a.baseID, a.quoteID)
	if err != nil {
		a.log.Errorf("Failed to sync book: %v", err)
		return
	}
	a.book = book

	err = a.updateFeeRates()
	if err != nil {
		a.log.Errorf("Failed to get fees: %v", err)
		return
	}

	err = a.cex.SubscribeMarket(a.ctx, a.baseID, a.quoteID)
	if err != nil {
		a.log.Errorf("Failed to subscribe to cex market: %v", err)
		return
	}

	tradeUpdates, unsubscribe := a.cex.SubscribeTradeUpdates()
	defer unsubscribe()

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-bookFeed.Next():
				// Really nothing to do with the updates. We just need to keep
				// the subscription live in order to get VWAP on dex orderbook.
			case <-a.ctx.Done():
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case update := <-tradeUpdates:
				a.handleCEXTradeUpdate(update)
			case <-a.ctx.Done():
				return
			}
		}
	}()

	noteFeed := a.core.NotificationFeed()
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer noteFeed.ReturnFeed()
		for {
			select {
			case n := <-noteFeed.C:
				a.handleNotification(n)
			case <-a.ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	a.cancelAllOrders()
}

func RunArbMarketMaker(ctx context.Context, cfg *BotConfig, c clientCore, cex cex, log dex.Logger) {
	if cfg.ArbMarketMakerConfig == nil {
		// implies bug in caller
		log.Errorf("No arb market maker config provided. Exiting.")
		return
	}

	mkt, err := c.ExchangeMarket(cfg.Host, cfg.BaseID, cfg.QuoteID)
	if err != nil {
		log.Errorf("Failed to get market: %v", err)
		return
	}

	(&arbMarketMaker{
		ctx:            ctx,
		host:           cfg.Host,
		baseID:         cfg.BaseID,
		quoteID:        cfg.QuoteID,
		cex:            cex,
		core:           c,
		log:            log,
		cfg:            cfg.ArbMarketMakerConfig,
		mkt:            mkt,
		ords:           make(map[order.OrderID]*core.Order),
		oidToPlacement: make(map[order.OrderID]int),
		matchesSeen:    make(map[order.MatchID]bool),
		cexTrades:      make(map[string]uint64),
	}).run()
}
