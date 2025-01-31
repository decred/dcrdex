// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/utils"
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
}

func (a *ArbMarketMakerConfig) copy() *ArbMarketMakerConfig {
	c := *a

	c.BuyPlacements = make([]*ArbMarketMakingPlacement, 0, len(a.BuyPlacements))
	for _, p := range a.BuyPlacements {
		c.BuyPlacements = append(c.BuyPlacements, &ArbMarketMakingPlacement{
			Lots:       p.Lots,
			Multiplier: p.Multiplier,
		})
	}

	c.SellPlacements = make([]*ArbMarketMakingPlacement, 0, len(a.SellPlacements))
	for _, p := range a.SellPlacements {
		c.SellPlacements = append(c.SellPlacements, &ArbMarketMakingPlacement{
			Lots:       p.Lots,
			Multiplier: p.Multiplier,
		})
	}

	return &c
}

// updateLotSize modifies the number of lots in each placement in the event
// of a lot size change. It will place as many lots as possible without
// exceeding the total quantity placed using the original lot size.
//
// This function is NOT thread safe.
func (c *ArbMarketMakerConfig) updateLotSize(originalLotSize, newLotSize uint64) {
	b2a := func(p *OrderPlacement) *ArbMarketMakingPlacement {
		return &ArbMarketMakingPlacement{
			Lots:       p.Lots,
			Multiplier: p.GapFactor,
		}
	}
	a2b := func(p *ArbMarketMakingPlacement) *OrderPlacement {
		return &OrderPlacement{
			Lots:      p.Lots,
			GapFactor: p.Multiplier,
		}
	}
	update := func(placements []*ArbMarketMakingPlacement) []*ArbMarketMakingPlacement {
		return utils.Map(updateLotSize(utils.Map(placements, a2b), originalLotSize, newLotSize), b2a)
	}
	c.SellPlacements = update(c.SellPlacements)
	c.BuyPlacements = update(c.BuyPlacements)
}

func (a *ArbMarketMakerConfig) placementLots() *placementLots {
	var baseLots, quoteLots uint64
	for _, p := range a.BuyPlacements {
		quoteLots += p.Lots
	}
	for _, p := range a.SellPlacements {
		baseLots += p.Lots
	}
	return &placementLots{
		baseLots:  baseLots,
		quoteLots: quoteLots,
	}
}

type placementLots struct {
	baseLots  uint64
	quoteLots uint64
}

type arbMarketMaker struct {
	*unifiedExchangeAdaptor
	cex              botCexAdaptor
	core             botCoreAdaptor
	book             dexOrderBook
	rebalanceRunning atomic.Bool
	currEpoch        atomic.Uint64

	matchesMtx    sync.Mutex
	matchesSeen   map[order.MatchID]bool
	pendingOrders map[order.OrderID]uint64 // orderID -> rate for counter trade on cex

	cexTradesMtx sync.RWMutex
	cexTrades    map[string]uint64
}

var _ bot = (*arbMarketMaker)(nil)

func (a *arbMarketMaker) cfg() *ArbMarketMakerConfig {
	return a.botCfg().ArbMarketMakerConfig
}

func (a *arbMarketMaker) handleCEXTradeUpdate(update *libxc.Trade) {
	if update.Complete {
		a.cexTradesMtx.Lock()
		delete(a.cexTrades, update.ID)
		a.cexTradesMtx.Unlock()
		return
	}
}

// tradeOnCEX executes a trade on the CEX.
func (a *arbMarketMaker) tradeOnCEX(rate, qty uint64, sell bool) {
	a.cexTradesMtx.Lock()
	defer a.cexTradesMtx.Unlock()

	cexTrade, err := a.cex.CEXTrade(a.ctx, a.baseID, a.quoteID, sell, rate, qty)
	if err != nil {
		a.log.Errorf("Error sending trade to CEX: %v", err)
		return
	}

	// Keep track of the epoch in which the trade was sent to the CEX. This way
	// the bot can cancel the trade if it is not filled after a certain number
	// of epochs.
	a.cexTrades[cexTrade.ID] = a.currEpoch.Load()
}

func (a *arbMarketMaker) processDEXOrderUpdate(o *core.Order) {
	var orderID order.OrderID
	copy(orderID[:], o.ID)

	a.matchesMtx.Lock()
	defer a.matchesMtx.Unlock()

	cexRate, found := a.pendingOrders[orderID]
	if !found {
		return
	}

	for _, match := range o.Matches {
		var matchID order.MatchID
		copy(matchID[:], match.MatchID)

		if !a.matchesSeen[matchID] {
			a.matchesSeen[matchID] = true
			a.tradeOnCEX(cexRate, match.Qty, !o.Sell)
		}
	}

	if !o.Status.IsActive() {
		delete(a.pendingOrders, orderID)
		for _, match := range o.Matches {
			var matchID order.MatchID
			copy(matchID[:], match.MatchID)
			delete(a.matchesSeen, matchID)
		}
	}
}

// cancelExpiredCEXTrades cancels any trades on the CEX that have been open for
// more than the number of epochs specified in the config.
func (a *arbMarketMaker) cancelExpiredCEXTrades() {
	currEpoch := a.currEpoch.Load()

	a.cexTradesMtx.RLock()
	defer a.cexTradesMtx.RUnlock()

	for tradeID, epoch := range a.cexTrades {
		if currEpoch-epoch >= a.cfg().NumEpochsLeaveOpen {
			err := a.cex.CancelTrade(a.ctx, a.baseID, a.quoteID, tradeID)
			if err != nil {
				a.log.Errorf("Error canceling CEX trade %s: %v", tradeID, err)
			}

			a.log.Infof("Cex trade %s was cancelled before it was filled", tradeID)
		}
	}
}

// dexPlacementRate calculates the rate at which an order should be placed on
// the DEX order book based on the rate of the counter trade on the CEX. The
// rate is calculated so that the difference in rates between the DEX and the
// CEX will pay for the network fees and still leave the configured profit.
func dexPlacementRate(cexRate uint64, sell bool, profitRate float64, mkt *market, feesInQuoteUnits uint64, log dex.Logger) (uint64, error) {
	var unadjustedRate uint64
	if sell {
		unadjustedRate = uint64(math.Round(float64(cexRate) * (1 + profitRate)))
	} else {
		unadjustedRate = uint64(math.Round(float64(cexRate) / (1 + profitRate)))
	}

	lotSize, rateStep := mkt.lotSize.Load(), mkt.rateStep.Load()
	rateAdj := rateAdjustment(feesInQuoteUnits, lotSize)

	if log.Level() <= dex.LevelTrace {
		log.Tracef("%s %s placement rate: cexRate = %s, profitRate = %.3f, unadjustedRate = %s, rateAdj = %s, fees = %s",
			mkt.name, sellStr(sell), mkt.fmtRate(cexRate), profitRate, mkt.fmtRate(unadjustedRate), mkt.fmtRate(rateAdj), mkt.fmtQuoteFees(feesInQuoteUnits),
		)
	}

	if sell {
		return steppedRate(unadjustedRate+rateAdj, rateStep), nil
	}

	if rateAdj > unadjustedRate {
		return 0, fmt.Errorf("rate adjustment required for fees %d > rate %d", rateAdj, unadjustedRate)
	}

	return steppedRate(unadjustedRate-rateAdj, rateStep), nil
}

func rateAdjustment(feesInQuoteUnits, lotSize uint64) uint64 {
	return uint64(math.Round(float64(feesInQuoteUnits) / float64(lotSize) * calc.RateEncodingFactor))
}

// dexPlacementRate calculates the rate at which an order should be placed on
// the DEX order book based on the rate of the counter trade on the CEX. The
// logic is in the dexPlacementRate function, so that it can be separately
// tested.
func (a *arbMarketMaker) dexPlacementRate(cexRate uint64, sell bool) (uint64, error) {
	feesInQuoteUnits, err := a.OrderFeesInUnits(sell, false, cexRate)
	if err != nil {
		return 0, fmt.Errorf("error getting fees in quote units: %w", err)
	}
	return dexPlacementRate(cexRate, sell, a.cfg().Profit, a.market, feesInQuoteUnits, a.log)
}

type arbMMPlacementReason struct {
	Depth         uint64 `json:"depth"`
	CEXTooShallow bool   `json:"cexFilled"`
}

func (a *arbMarketMaker) ordersToPlace() (buys, sells []*TradePlacement, err error) {
	orders := func(cfgPlacements []*ArbMarketMakingPlacement, sellOnDEX bool) ([]*TradePlacement, error) {
		newPlacements := make([]*TradePlacement, 0, len(cfgPlacements))
		var cumulativeCEXDepth uint64
		for i, cfgPlacement := range cfgPlacements {
			cumulativeCEXDepth += uint64(float64(cfgPlacement.Lots*a.lotSize.Load()) * cfgPlacement.Multiplier)
			_, extrema, filled, err := a.CEX.VWAP(a.baseID, a.quoteID, sellOnDEX, cumulativeCEXDepth)
			if err != nil {
				return nil, fmt.Errorf("error getting CEX VWAP: %w", err)
			}

			if a.log.Level() == dex.LevelTrace {
				a.log.Tracef("%s placement orders: %s placement # %d, lots = %d, extrema = %s, filled = %t",
					a.name, sellStr(sellOnDEX), i, cfgPlacement.Lots, a.fmtRate(extrema), filled,
				)
			}

			if !filled {
				a.log.Infof("CEX %s side has < %s on the orderbook.", sellStr(!sellOnDEX), a.fmtBase(cumulativeCEXDepth))
				newPlacements = append(newPlacements, &TradePlacement{})
				continue
			}

			placementRate, err := a.dexPlacementRate(extrema, sellOnDEX)
			if err != nil {
				return nil, fmt.Errorf("error calculating DEX placement rate: %w", err)
			}

			newPlacements = append(newPlacements, &TradePlacement{
				Rate:             placementRate,
				Lots:             cfgPlacement.Lots,
				CounterTradeRate: extrema,
			})
		}

		return newPlacements, nil
	}

	buys, err = orders(a.cfg().BuyPlacements, false)
	if err != nil {
		return
	}

	sells, err = orders(a.cfg().SellPlacements, true)
	return
}

// distribution parses the current inventory distribution and checks if better
// distributions are possible via deposit or withdrawal.
func (a *arbMarketMaker) distribution() (dist *distribution, err error) {
	placements := a.cfg().placementLots()
	if placements.baseLots == 0 && placements.quoteLots == 0 {
		return nil, errors.New("zero placement lots?")
	}
	dexSellLots, dexBuyLots := placements.baseLots, placements.quoteLots
	dexBuyRate, dexSellRate, err := a.cexCounterRates(dexSellLots, dexBuyLots)
	if err != nil {
		return nil, fmt.Errorf("error getting cex counter-rates: %w", err)
	}
	adjustedBuy, err := a.dexPlacementRate(dexBuyRate, false)
	if err != nil {
		return nil, fmt.Errorf("error getting adjusted buy rate: %v", err)
	}
	adjustedSell, err := a.dexPlacementRate(dexSellRate, true)
	if err != nil {
		return nil, fmt.Errorf("error getting adjusted sell rate: %v", err)
	}

	perLot, err := a.lotCosts(adjustedBuy, adjustedSell)
	if perLot == nil {
		return nil, fmt.Errorf("error getting lot costs: %w", err)
	}
	dist = a.newDistribution(perLot)
	a.optimizeTransfers(dist, dexSellLots, dexBuyLots, dexSellLots, dexBuyLots)
	return dist, nil
}

// rebalance is called on each new epoch. It will calculate the rates orders
// need to be placed on the DEX orderbook based on the CEX orderbook, and
// potentially update the orders on the DEX orderbook. It will also process
// and potentially needed withdrawals and deposits, and finally cancel any
// trades on the CEX that have been open for more than the number of epochs
// specified in the config.
func (a *arbMarketMaker) rebalance(epoch uint64, book *orderbook.OrderBook) {
	if !a.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer a.rebalanceRunning.Store(false)
	a.log.Tracef("rebalance: epoch %d", epoch)

	currEpoch := a.currEpoch.Load()
	if epoch <= currEpoch {
		return
	}
	a.currEpoch.Store(epoch)

	if !a.checkBotHealth(epoch) {
		a.tryCancelOrders(a.ctx, &epoch, false)
		return
	}

	actionTaken, err := a.tryTransfers(currEpoch)
	if err != nil {
		a.log.Errorf("Error performing transfers: %v", err)
	} else if actionTaken {
		return
	}

	var buysReport, sellsReport *OrderReport
	buyOrders, sellOrders, determinePlacementsErr := a.ordersToPlace()
	if determinePlacementsErr != nil {
		a.tryCancelOrders(a.ctx, &epoch, false)
	} else {
		var buys, sells map[order.OrderID]*dexOrderInfo
		buys, buysReport = a.multiTrade(buyOrders, false, a.cfg().DriftTolerance, currEpoch)
		for id, ord := range buys {
			a.matchesMtx.Lock()
			a.pendingOrders[id] = ord.counterTradeRate
			a.matchesMtx.Unlock()
		}

		sells, sellsReport = a.multiTrade(sellOrders, true, a.cfg().DriftTolerance, currEpoch)
		for id, ord := range sells {
			a.matchesMtx.Lock()
			a.pendingOrders[id] = ord.counterTradeRate
			a.matchesMtx.Unlock()
		}
	}

	epochReport := &EpochReport{
		BuysReport:  buysReport,
		SellsReport: sellsReport,
		EpochNum:    epoch,
	}
	epochReport.setPreOrderProblems(determinePlacementsErr)
	a.updateEpochReport(epochReport)

	a.cancelExpiredCEXTrades()
	a.registerFeeGap()
}

func (a *arbMarketMaker) tryTransfers(currEpoch uint64) (actionTaken bool, err error) {
	dist, err := a.distribution()
	if err != nil {
		a.log.Errorf("distribution calculation error: %v", err)
		return
	}
	return a.transfer(dist, currEpoch)
}

func feeGap(core botCoreAdaptor, cex libxc.CEX, baseID, quoteID uint32, lotSize uint64) (*FeeGapStats, error) {
	s := &FeeGapStats{
		BasisPrice: cex.MidGap(baseID, quoteID),
	}
	_, buy, filled, err := cex.VWAP(baseID, quoteID, false, lotSize)
	if err != nil {
		return nil, fmt.Errorf("VWAP buy error: %w", err)
	}
	if !filled {
		return s, nil
	}
	_, sell, filled, err := cex.VWAP(baseID, quoteID, true, lotSize)
	if err != nil {
		return nil, fmt.Errorf("VWAP sell error: %w", err)
	}
	if !filled {
		return s, nil
	}
	s.RemoteGap = sell - buy
	sellFeesInBaseUnits, err := core.OrderFeesInUnits(true, true, sell)
	if err != nil {
		return nil, fmt.Errorf("error getting sell fees: %w", err)
	}
	buyFeesInBaseUnits, err := core.OrderFeesInUnits(false, true, buy)
	if err != nil {
		return nil, fmt.Errorf("error getting buy fees: %w", err)
	}
	s.RoundTripFees = sellFeesInBaseUnits + buyFeesInBaseUnits
	feesInQuoteUnits := calc.BaseToQuote((sell+buy)/2, s.RoundTripFees)
	s.FeeGap = rateAdjustment(feesInQuoteUnits, lotSize)
	return s, nil
}

func (a *arbMarketMaker) registerFeeGap() {
	feeGap, err := feeGap(a.core, a.CEX, a.baseID, a.quoteID, a.lotSize.Load())
	if err != nil {
		a.log.Warnf("error getting fee-gap stats: %v", err)
		return
	}
	a.unifiedExchangeAdaptor.registerFeeGap(feeGap)
}

func (a *arbMarketMaker) botLoop(ctx context.Context) (*sync.WaitGroup, error) {
	book, bookFeed, err := a.core.SyncBook(a.host, a.baseID, a.quoteID)
	if err != nil {
		return nil, fmt.Errorf("failed to sync book: %v", err)
	}
	a.book = book

	err = a.cex.SubscribeMarket(ctx, a.baseID, a.quoteID)
	if err != nil {
		bookFeed.Close()
		return nil, fmt.Errorf("failed to subscribe to cex market: %v", err)
	}

	tradeUpdates := a.cex.SubscribeTradeUpdates()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer bookFeed.Close()
		for {
			select {
			case ni := <-bookFeed.Next():
				switch epoch := ni.Payload.(type) {
				case *core.ResolvedEpoch:
					a.rebalance(epoch.Current, book)
				}
			case <-ctx.Done():
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
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		orderUpdates := a.core.SubscribeOrderUpdates()
		for {
			select {
			case n := <-orderUpdates:
				a.processDEXOrderUpdate(n)
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
	}()

	a.registerFeeGap()

	return &wg, nil
}

func newArbMarketMaker(cfg *BotConfig, adaptorCfg *exchangeAdaptorCfg, log dex.Logger) (*arbMarketMaker, error) {
	if cfg.ArbMarketMakerConfig == nil {
		// implies bug in caller
		return nil, errors.New("no arb market maker config provided")
	}

	adaptor, err := newUnifiedExchangeAdaptor(adaptorCfg)
	if err != nil {
		return nil, fmt.Errorf("error constructing exchange adaptor: %w", err)
	}

	arbMM := &arbMarketMaker{
		unifiedExchangeAdaptor: adaptor,
		cex:                    adaptor,
		core:                   adaptor,
		matchesSeen:            make(map[order.MatchID]bool),
		pendingOrders:          make(map[order.OrderID]uint64),
		cexTrades:              make(map[string]uint64),
	}

	adaptor.setBotLoop(arbMM.botLoop)
	return arbMM, nil
}
