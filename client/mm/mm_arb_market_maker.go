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

	"decred.org/dcrdex/client/asset"
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

// MultiHopCfg is the configuration for a multi-hop market maker.
// A multi-hop arb market maker is a market maker that uses two
// markets on the CEX to make an arbitrage trade. For example, if
// there is a DCR/BTC market on the DEX, but does not have a DCR/BTC
// market, this bot can use the DCR/USDT and BTC/USDT markets on the CEX
// to make an arbitrage trade. The first leg of the multi-hop arb will
// be a Limit IOC order, then depending on the configuration, the second
// leg will be either a Market trade or a Limit order (which is open for
// NumEpochsLeaveOpen epochs) to limit the amount of funds stuck in the
// intermediate asset.
type MultiHopCfg struct {
	// BaseAssetMarket is the market on the CEX that the base asset of the
	// DEX market is traded on. The "other" asset on the market must be the
	// same as the "other" asset on the QuoteAssetMarket.
	BaseAssetMarket [2]uint32 `json:"baseAssetMarket"`
	// QuoteAssetMarket is the market on the CEX that the quote asset of the
	// DEX market is traded on. The other asset on the market must be the
	// same as the "other" asset on the BaseAssetMarket.
	QuoteAssetMarket [2]uint32 `json:"quoteAssetMarket"`
	// MarketOrders set to true means that the second leg of the multi-hop
	// arb will be a market order. This allows the bot to never have any
	// funds stuck in the intermediate asset, but may result in the bot
	// incurring losses if there is a sudden price change.
	MarketOrders bool `json:"marketOrders"`
	// LimitOrdersBuffer is only relevant if MarketOrders is false. It
	// specifies a percentage to adjust the limit order rate for the
	// second leg of the multi-hop arb. It will adjust the rate in
	// the less profitable direction, i.e. lower for sell orders and
	// higher for buy orders. The purpose of the buffer is to increase
	// the probability of the trade being filled in order to avoid having
	// funds stuck in the intermediate asset.
	LimitOrdersBuffer float64 `json:"limitOrdersBuffer"`
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
	MultiHop           *MultiHopCfg                `json:"multiHop"`
}

func (c *ArbMarketMakerConfig) isMultiHop() bool {
	return c.MultiHop != nil
}

func (a *ArbMarketMakerConfig) copy() *ArbMarketMakerConfig {
	c := *a

	copyArbMarketMakingPlacement := func(p *ArbMarketMakingPlacement) *ArbMarketMakingPlacement {
		return &ArbMarketMakingPlacement{
			Lots:       p.Lots,
			Multiplier: p.Multiplier,
		}
	}
	c.BuyPlacements = utils.Map(a.BuyPlacements, copyArbMarketMakingPlacement)
	c.SellPlacements = utils.Map(a.SellPlacements, copyArbMarketMakingPlacement)

	return &c
}

func (a *ArbMarketMakerConfig) validate(baseID, quoteID uint32) error {
	if len(a.BuyPlacements) == 0 && len(a.SellPlacements) == 0 {
		return fmt.Errorf("no placements")
	}

	if a.Profit <= 0 {
		return fmt.Errorf("profit must be greater than 0")
	}

	if a.DriftTolerance < 0 || a.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", a.DriftTolerance)
	}

	if a.NumEpochsLeaveOpen < 2 {
		return fmt.Errorf("arbs must be left open for at least 2 epochs")
	}

	if a.MultiHop != nil {
		if a.MultiHop.BaseAssetMarket[0] != baseID && a.MultiHop.BaseAssetMarket[1] != baseID {
			return fmt.Errorf("multi-hop base asset market must involve the DEX base asset")
		}
		if a.MultiHop.QuoteAssetMarket[0] != quoteID && a.MultiHop.QuoteAssetMarket[1] != quoteID {
			return fmt.Errorf("multi-hop quote asset market must involve the DEX quote asset")
		}
		var baseIntermediateID, quoteIntermediateID uint32
		if a.MultiHop.BaseAssetMarket[0] == baseID {
			baseIntermediateID = a.MultiHop.BaseAssetMarket[1]
		} else {
			baseIntermediateID = a.MultiHop.BaseAssetMarket[0]
		}
		if a.MultiHop.QuoteAssetMarket[0] == quoteID {
			quoteIntermediateID = a.MultiHop.QuoteAssetMarket[1]
		} else {
			quoteIntermediateID = a.MultiHop.QuoteAssetMarket[0]
		}
		if baseIntermediateID != quoteIntermediateID {
			return fmt.Errorf("multi-hop markets do not share an intermediate asset")
		}
	}

	return nil
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

type cexTradeInfo struct {
	epochPlaced       uint64
	followUpTradeRate uint64
}

type arbMarketMaker struct {
	*unifiedExchangeAdaptor
	cex              botCexAdaptor
	core             botCoreAdaptor
	book             dexOrderBook
	rebalanceRunning atomic.Bool
	currEpoch        atomic.Uint64

	matchesMtx  sync.Mutex
	matchesSeen map[order.MatchID]bool
	// orderArbRates maps from orderID to the counter trade rate(s) on the CEX.
	// If the bot is configured for multi-hop, there will be two rates, one for
	// the first leg and one for the second leg. Otherwise, only the first rate
	// will be populated.
	orderArbRates map[order.OrderID][2]uint64
	// orderIndex maps sell -> orderID -> placement index. This is used to
	// update the arb rates for existing orders.
	orderIndex map[bool]map[order.OrderID]int

	cexTradesMtx sync.RWMutex
	cexTrades    map[string]*cexTradeInfo
}

var _ bot = (*arbMarketMaker)(nil)

func (a *arbMarketMaker) cfg() *ArbMarketMakerConfig {
	return a.botCfg().ArbMarketMakerConfig
}

// worsenRate adjusts the rate in the less profitable direction, i.e. lower
// for sell orders and higher for buy orders.
func worsenRate(rate uint64, buffer float64, sell bool) uint64 {
	if buffer <= 0 {
		return rate
	}
	multiplier := 1.0
	if sell {
		multiplier -= buffer
	} else {
		multiplier += buffer
	}
	adjusted := float64(rate) * multiplier
	if sell {
		return uint64(math.Floor(adjusted))
	}
	return uint64(math.Ceil(adjusted))
}

// multiHopArbCompletionParams is called when a trade is completed on the
// CEX. It checks if the completion of this trade should trigger the second
// leg of the multi-hop arb, and if so, returns the parameters of that trade.
func (a *arbMarketMaker) multiHopArbCompletionParams(update *libxc.Trade, followUpRate uint64) (makeTrade bool, orderType libxc.OrderType, baseID, quoteID uint32, sell bool, qty, quoteQty, rate uint64) {
	fail := func(err error) (bool, libxc.OrderType, uint32, uint32, bool, uint64, uint64, uint64) {
		if err != nil {
			a.log.Error(err)
		}
		return false, libxc.OrderTypeLimit, 0, 0, false, 0, 0, 0
	}

	multiHopCfg := a.cfg().MultiHop
	updateOnBaseAssetMarket := a.baseID == update.BaseID || a.baseID == update.QuoteID
	updateOnQuoteAssetMarket := a.quoteID == update.BaseID || a.quoteID == update.QuoteID

	var market [2]uint32
	var fromAssetID, toAssetID uint32
	switch {
	case updateOnBaseAssetMarket:
		market = multiHopCfg.QuoteAssetMarket
		fromAssetID = a.baseID
		toAssetID = a.quoteID
	case updateOnQuoteAssetMarket:
		market = multiHopCfg.BaseAssetMarket
		fromAssetID = a.quoteID
		toAssetID = a.baseID
	default:
		return fail(fmt.Errorf("trade is not on the base or quote asset market: %+v", update))
	}
	baseID = market[0]
	quoteID = market[1]

	// Set sell, qty, quoteQty, and makeTrade
	sell = market[1] == toAssetID
	if fromAssetID == update.BaseID {
		qty = update.QuoteFilled
		makeTrade = update.Sell
	} else {
		qty = update.BaseFilled
		makeTrade = !update.Sell
	}
	if !makeTrade {
		return fail(nil)
	}
	if !sell {
		quoteQty = qty
		qty = 0
	}

	// Set the order type and rate.
	orderType = libxc.OrderTypeMarket
	rate = 0
	if !multiHopCfg.MarketOrders {
		orderType = libxc.OrderTypeLimit
		rate = worsenRate(followUpRate, multiHopCfg.LimitOrdersBuffer, sell)
		if rate == 0 {
			return fail(fmt.Errorf("worsenRate returned 0 rate"))
		}
	}

	return
}

func (a *arbMarketMaker) handleCEXTradeUpdate(update *libxc.Trade) {
	if !update.Complete {
		return
	}

	a.cexTradesMtx.Lock()
	cexTradeInfo, ok := a.cexTrades[update.ID]
	if !ok {
		a.cexTradesMtx.Unlock()
		return
	}
	delete(a.cexTrades, update.ID)
	a.cexTradesMtx.Unlock()

	cfg := a.cfg()
	if !cfg.isMultiHop() {
		return
	}

	makeTrade, orderType, baseID, quoteID, sell, qty, quoteQty, rate := a.multiHopArbCompletionParams(update, cexTradeInfo.followUpTradeRate)
	if makeTrade {
		a.tradeOnCEX(baseID, quoteID, rate, qty, quoteQty, sell, orderType, 0)
	}
}

// tradeOnCEX executes a trade on the CEX.
func (a *arbMarketMaker) tradeOnCEX(baseID, quoteID uint32, rate, qty, quoteQty uint64, sell bool, orderType libxc.OrderType, followUpRate uint64) {
	a.cexTradesMtx.Lock()

	cexTrade, err := a.cex.CEXTrade(a.ctx, baseID, quoteID, sell, rate, qty, quoteQty, orderType)
	if err != nil {
		a.cexTradesMtx.Unlock()
		a.log.Errorf("Error sending trade to CEX: %v", err)
		return
	}

	// Keep track of the epoch in which the trade was sent to the CEX. This way
	// the bot can cancel the trade if it is not filled after a certain number
	// of epochs.
	a.cexTrades[cexTrade.ID] = &cexTradeInfo{
		epochPlaced:       a.currEpoch.Load(),
		followUpTradeRate: followUpRate,
	}
	a.cexTradesMtx.Unlock()

	a.handleCEXTradeUpdate(cexTrade)
}

// initiateMultiHopArb is called when a DEX order is matched and a multi-hop
// arb should be started. The second trade of the multi-hop arb is executed
// when this trade is complete.
func (a *arbMarketMaker) initiateMultiHopArb(dexSell bool, matchRate, matchQty uint64, arbRates [2]uint64) {
	cfg := a.cfg().MultiHop

	// Determine the CEX market and trade direction.
	var cexMarket [2]uint32
	var sell bool
	if dexSell {
		cexMarket = cfg.QuoteAssetMarket
		sell = cexMarket[0] == a.quoteID
	} else {
		cexMarket = cfg.BaseAssetMarket
		sell = cexMarket[0] == a.baseID
	}

	// Calculate the quantity for the first CEX leg.
	// The qty is in the base asset of the CEX market.
	var cexQty uint64
	if dexSell {
		cexQty = calc.BaseToQuote(matchRate, matchQty)
	} else {
		cexQty = matchQty
	}
	if !sell {
		cexQty = calc.QuoteToBase(arbRates[0], cexQty)
	}

	// Execute the first leg as a Limit IOC order.
	a.tradeOnCEX(cexMarket[0], cexMarket[1], arbRates[0], cexQty, 0 /* quoteQty */, sell, libxc.OrderTypeLimitIOC, arbRates[1])
}

func (a *arbMarketMaker) processDEXOrderUpdate(o *core.Order) {
	var orderID order.OrderID
	copy(orderID[:], o.ID)

	// Determine the arb rates and the new matches.
	a.matchesMtx.Lock()
	cexRates, found := a.orderArbRates[orderID]
	if !found {
		a.matchesMtx.Unlock()
		return
	}
	newMatches := make([]*core.Match, 0, len(o.Matches))
	for _, match := range o.Matches {
		var matchID order.MatchID
		copy(matchID[:], match.MatchID)
		if a.matchesSeen[matchID] {
			continue
		}
		a.matchesSeen[matchID] = true
		newMatches = append(newMatches, match)
	}
	a.matchesMtx.Unlock()

	// Execute the cex trades.
	for _, match := range newMatches {
		if a.cfg().isMultiHop() {
			a.initiateMultiHopArb(o.Sell, match.Rate, match.Qty, cexRates)
		} else {
			a.tradeOnCEX(a.baseID, a.quoteID, cexRates[0], match.Qty, 0 /* quoteQty */, !o.Sell, libxc.OrderTypeLimit, 0)
		}
	}

	// If the order will have no more matches, remove it from memory.
	if !o.Status.IsActive() {
		a.matchesMtx.Lock()
		delete(a.orderArbRates, orderID)
		delete(a.orderIndex[o.Sell], orderID)
		for _, match := range o.Matches {
			var matchID order.MatchID
			copy(matchID[:], match.MatchID)
			delete(a.matchesSeen, matchID)
		}
		a.matchesMtx.Unlock()
	}
}

// cancelExpiredCEXTrades cancels any trades on the CEX that have been open for
// more than the number of epochs specified in the config.
func (a *arbMarketMaker) cancelExpiredCEXTrades() {
	currEpoch := a.currEpoch.Load()

	a.cexTradesMtx.RLock()
	defer a.cexTradesMtx.RUnlock()

	for tradeID, cexTradeInfo := range a.cexTrades {
		if currEpoch-cexTradeInfo.epochPlaced >= a.cfg().NumEpochsLeaveOpen {
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

func msgRate(rate float64, baseID, quoteID uint32) uint64 {
	baseUI, _ := asset.UnitInfo(baseID)
	quoteUI, _ := asset.UnitInfo(quoteID)
	return calc.MessageRate(rate, baseUI, quoteUI)
}

func convRate(rate uint64, baseID, quoteID uint32) float64 {
	baseUI, _ := asset.UnitInfo(baseID)
	quoteUI, _ := asset.UnitInfo(quoteID)
	return calc.ConventionalRate(rate, baseUI, quoteUI)
}

// aggregateRates computes the effective rate on a DEX market by combining rates
// from two CEX markets. One CEX market involves the DEX base asset, the other
// the DEX quote asset, sharing a common intermediate asset. Inversions are
// applied if the asset order on CEX markets differs from DEX expectations.
func aggregateRates(baseMarketRate, quoteMarketRate uint64, mkt *market, baseMarket, quoteMarket [2]uint32) uint64 {
	convBaseRate := convRate(baseMarketRate, baseMarket[0], baseMarket[1])
	if mkt.baseID != baseMarket[0] {
		convBaseRate = 1 / convBaseRate
	}

	convQuoteRate := convRate(quoteMarketRate, quoteMarket[0], quoteMarket[1])
	if mkt.quoteID != quoteMarket[1] {
		convQuoteRate = 1 / convQuoteRate
	}

	convAggRate := convBaseRate * convQuoteRate
	return msgRate(convAggRate, mkt.baseID, mkt.quoteID)
}

type vwapFunc func(baseID, quoteID uint32, sell bool, qty uint64) (vwap uint64, extrema uint64, filled bool, err error)

// tradeAssetPriceExtrema calculates the current extrema price for acquiring
// or spending a specified asset on a multi-hop market.
// - assetID identifies the target asset.
// - receiveAsset is true if acquiring the asset (buy), false if spending it (sell).
// - Returns the extrema rate, counter-asset quantity, fill status, and any error.
// If the fill status is false, the extrema rate is 0.
func tradeAssetPriceExtrema(market [2]uint32, assetID uint32, depth uint64, receiveAsset bool,
	vwapF, invVwapF vwapFunc) (extrema, counterQty uint64, filled bool, err error) {

	var f vwapFunc
	var sell bool
	if assetID == market[0] {
		f = vwapF
		sell = !receiveAsset
	} else {
		f = invVwapF
		sell = receiveAsset
	}

	_, extrema, filled, err = f(market[0], market[1], sell, depth)
	if err != nil {
		return 0, 0, false, fmt.Errorf("VWAP error: %w", err)
	}
	if !filled {
		return 0, 0, false, nil
	}

	if assetID == market[0] {
		counterQty = calc.BaseToQuote(extrema, depth)
	} else {
		counterQty = calc.QuoteToBase(extrema, depth)
	}

	return
}

type arbTradeArgs struct {
	baseID    uint32
	quoteID   uint32
	orderType libxc.OrderType
	rate      uint64
	qty       uint64
	quoteQty  uint64
	sell      bool
}

// multiHopArbTrades determines the parameters for the trades that will
// be executed on the DEX to make a multi-hop arb trade. It will return
// two trades, one for the min quantity and one for the max quantity.
func multiHopArbTrades(mkt [2]uint32, sell bool, qtyAssetID uint32, minQty, maxQty uint64, rate uint64, cfg *MultiHopCfg, isFirstLeg bool) []*arbTradeArgs {
	orderType := libxc.OrderTypeLimitIOC
	buffer := 0.0
	if !isFirstLeg {
		if cfg.MarketOrders {
			orderType = libxc.OrderTypeMarket
		} else {
			orderType = libxc.OrderTypeLimit
			buffer = cfg.LimitOrdersBuffer
		}
	}

	if orderType != libxc.OrderTypeMarket {
		rate = worsenRate(rate, buffer, sell)
	}

	// Only buy orders on the second leg of a multi-hop trade will be in the
	// quote asset, in order to be able to specify the total quantity that
	// should be traded.
	qtyIsQuote := !isFirstLeg && !sell

	qtys := []uint64{minQty, maxQty}
	if !qtyIsQuote && qtyAssetID != mkt[0] {
		for i := range qtys {
			qtys[i] = calc.QuoteToBase(rate, qtys[i])
		}
	} else if qtyIsQuote && qtyAssetID != mkt[1] {
		for i := range qtys {
			qtys[i] = calc.BaseToQuote(rate, qtys[i])
		}
	}

	if orderType == libxc.OrderTypeMarket {
		rate = 0
	}

	trades := make([]*arbTradeArgs, len(qtys))
	for i, q := range qtys {
		trades[i] = &arbTradeArgs{
			baseID:    mkt[0],
			quoteID:   mkt[1],
			orderType: orderType,
			rate:      rate,
			sell:      sell,
		}
		if qtyIsQuote {
			trades[i].quoteQty = q
		} else {
			trades[i].qty = q
		}
	}

	return trades
}

// multiHopRateAndTrades determines whether the trade can be filled, the
// aggregate rate of a multi-hop trade, the rates for the two legs of the
// multi-hop trade, and the arguments for the trades with the expected min
// and max quantities (which can be used for trade validation).
func multiHopRateAndTrades(sellOnDEX bool, depth, numLots uint64, multiHopCfg *MultiHopCfg, mkt *market, vwap, invVwap vwapFunc) (filled bool, aggregateRate uint64, multiHopRates [2]uint64, trades []*arbTradeArgs, err error) {
	fail := func(err error) (bool, uint64, [2]uint64, []*arbTradeArgs, error) {
		return false, 0, [2]uint64{}, nil, err
	}

	intermediateAsset := multiHopCfg.BaseAssetMarket[0]
	if mkt.baseID == intermediateAsset {
		intermediateAsset = multiHopCfg.BaseAssetMarket[1]
	}

	// Compute price extrema for base asset leg.
	receiveBaseOnDEX := !sellOnDEX
	baseRate, intAssetQty, filled, err := tradeAssetPriceExtrema(multiHopCfg.BaseAssetMarket, mkt.baseID, depth, receiveBaseOnDEX, vwap, invVwap)
	if err != nil {
		return fail(fmt.Errorf("error getting intermediate market VWAP: %w", err))
	}
	if !filled {
		return fail(nil)
	}

	// Compute price extrema for quote asset leg.
	receiveIntermediate := !sellOnDEX
	quoteRate, _, filled, err := tradeAssetPriceExtrema(multiHopCfg.QuoteAssetMarket, intermediateAsset, intAssetQty, receiveIntermediate, vwap, invVwap)
	if err != nil {
		return fail(fmt.Errorf("error getting target market VWAP: %w", err))
	}
	if !filled {
		return fail(nil)
	}

	// Aggregate the rates.
	aggregatedRate := aggregateRates(baseRate, quoteRate, mkt, multiHopCfg.BaseAssetMarket, multiHopCfg.QuoteAssetMarket)

	if sellOnDEX {
		multiHopRates = [2]uint64{quoteRate, baseRate}
	} else {
		multiHopRates = [2]uint64{baseRate, quoteRate}
	}

	// Determine the potential parameters for the trade on the base asset market
	receiveBaseOnCEX := !receiveBaseOnDEX
	baseMktSell := receiveBaseOnCEX != (multiHopCfg.BaseAssetMarket[0] == mkt.baseID)
	lotSize := mkt.lotSize.Load()
	firstLegIsBase := !sellOnDEX
	baseArbTrades := multiHopArbTrades(multiHopCfg.BaseAssetMarket, baseMktSell, mkt.baseID, lotSize, lotSize*numLots, baseRate, multiHopCfg, firstLegIsBase)

	// Determine the potential parameters for the trade on the quote asset market
	receiveIntermediateOnCEX := !receiveIntermediate
	quoteMktSell := receiveIntermediateOnCEX != (multiHopCfg.QuoteAssetMarket[1] == mkt.quoteID)
	var intAssetMinQty, intAssetMaxQty uint64
	if multiHopCfg.BaseAssetMarket[0] == mkt.baseID {
		intAssetMinQty = calc.BaseToQuote(baseRate, lotSize)
		intAssetMaxQty = calc.BaseToQuote(baseRate, lotSize*numLots)
	} else {
		intAssetMinQty = calc.QuoteToBase(baseRate, lotSize)
		intAssetMaxQty = calc.QuoteToBase(baseRate, lotSize*numLots)
	}
	quoteArbTrades := multiHopArbTrades(multiHopCfg.QuoteAssetMarket, quoteMktSell, intermediateAsset, intAssetMinQty, intAssetMaxQty, quoteRate, multiHopCfg, !firstLegIsBase)
	allArbTrades := append(baseArbTrades, quoteArbTrades...)

	return true, aggregatedRate, multiHopRates, allArbTrades, nil
}

// singleHopRateAndTrades returns the extrema rate for buying or selling
// depth units of the base asset on the CEX, and the minimum and maximum
// quantities of the trade.
func singleHopRateAndTrades(sell bool, depth, numLots uint64, mkt *market, vwap, invVwap vwapFunc) (uint64, bool, []*arbTradeArgs, error) {
	_, rate, filled, err := vwap(mkt.baseID, mkt.quoteID, sell, depth)
	lotSize := mkt.lotSize.Load()

	arbTrades := []*arbTradeArgs{
		// Min qty
		{
			baseID:    mkt.baseID,
			quoteID:   mkt.quoteID,
			sell:      !sell,
			rate:      rate,
			qty:       lotSize,
			orderType: libxc.OrderTypeLimit,
		},
		// Max qty
		{
			baseID:    mkt.baseID,
			quoteID:   mkt.quoteID,
			sell:      !sell,
			rate:      rate,
			qty:       lotSize * numLots,
			orderType: libxc.OrderTypeLimit,
		},
	}
	return rate, filled, arbTrades, err
}

// arbMMExtremaAndTrades returns the extrema price when buying or selling a
// certain quantity on the CEX, either directly on a matching market, or via
// a multi-hop trade. It also returns the potential arb trades that may
// be executed, with the minimum and maximum quantities for those arb trades.
func arbMMExtremaAndTrades(sell bool, depth, numLots uint64, multiHopCfg *MultiHopCfg, mkt *market, vwap, invVwap vwapFunc) (filled bool, cexRate uint64, multiHopRates [2]uint64, trades []*arbTradeArgs, err error) {
	if multiHopCfg != nil {
		return multiHopRateAndTrades(sell, depth, numLots, multiHopCfg, mkt, vwap, invVwap)
	}

	cexRate, filled, trades, err = singleHopRateAndTrades(sell, depth, numLots, mkt, vwap, invVwap)
	return
}

// validateArbTrades validates the potential arb trades that may be executed in
// order to avoid placing orders on the DEX for which the arb trade on the CEX
// would be invalid.
func (a *arbMarketMaker) validateArbTrades(arbTrades []*arbTradeArgs) error {
	for _, arbTrade := range arbTrades {
		err := a.cex.ValidateTrade(arbTrade.baseID, arbTrade.quoteID, arbTrade.sell, arbTrade.rate, arbTrade.qty, arbTrade.quoteQty, arbTrade.orderType)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *arbMarketMaker) ordersToPlace() (buys, sells []*TradePlacement, err error) {
	lotSize := a.lotSize.Load()
	orders := func(cfgPlacements []*ArbMarketMakingPlacement, sellOnDEX bool) ([]*TradePlacement, error) {
		newPlacements := make([]*TradePlacement, 0, len(cfgPlacements))
		var cumulativeCEXDepth uint64
		for i, cfgPlacement := range cfgPlacements {
			cumulativeCEXDepth += uint64(float64(cfgPlacement.Lots*lotSize) * cfgPlacement.Multiplier)

			filled, cexRate, multiHopRates, arbTrades, err := arbMMExtremaAndTrades(sellOnDEX,
				cumulativeCEXDepth, cfgPlacement.Lots, a.cfg().MultiHop,
				a.market, a.CEX.VWAP, a.CEX.InvVWAP)
			if err != nil {
				return nil, fmt.Errorf("error getting VWAP: %w", err)
			}

			if a.log.Level() == dex.LevelTrace {
				a.log.Tracef("%s placement orders: %s placement # %d, lots = %d, cex rate = %s, filled = %t",
					a.name, sellStr(sellOnDEX), i, cfgPlacement.Lots, a.fmtRate(cexRate), filled,
				)
			}

			if !filled {
				newPlacements = append(newPlacements, &TradePlacement{
					Error: &BotProblems{
						UnknownError: "no fill",
					},
				})
				continue
			}

			if err := a.validateArbTrades(arbTrades); err != nil {
				newPlacements = append(newPlacements, &TradePlacement{
					Error: &BotProblems{
						UnknownError: fmt.Sprintf("error validating arb trades: %v", err),
					},
				})
				continue
			}

			placementRate, err := a.dexPlacementRate(cexRate, sellOnDEX)
			if err != nil {
				return nil, fmt.Errorf("error calculating DEX placement rate: %w", err)
			}

			newPlacements = append(newPlacements, &TradePlacement{
				Rate:             placementRate,
				Lots:             cfgPlacement.Lots,
				CounterTradeRate: cexRate,
				MultiHopRates:    multiHopRates,
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
func (a *arbMarketMaker) distribution(additionalDEX, additionalCEX map[uint32]uint64) (dist *distribution, err error) {
	placements := a.cfg().placementLots()
	if placements.baseLots == 0 && placements.quoteLots == 0 {
		return nil, errors.New("zero placement lots?")
	}
	dexSellLots, dexBuyLots := placements.baseLots, placements.quoteLots
	dexBuyRate, dexSellRate, err := a.cexCounterRates(dexSellLots, dexBuyLots, a.cfg().MultiHop)
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
	a.optimizeTransfers(dist, dexSellLots, dexBuyLots, dexSellLots, dexBuyLots, additionalDEX, additionalCEX)
	return dist, nil
}

func (a *arbMarketMaker) updatePendingOrders(placements []*TradePlacement, placedOrders map[order.OrderID]*dexOrderInfo, sells bool) {
	a.matchesMtx.Lock()
	defer a.matchesMtx.Unlock()

	for id, ord := range placedOrders {
		a.orderIndex[sells][id] = int(ord.placementIndex)
	}

	cfg := a.cfg()

	for oid, index := range a.orderIndex[sells] {
		if len(placements) <= index {
			// Could be hit if there is a reconfig.
			continue
		}

		if cfg.isMultiHop() {
			a.orderArbRates[oid] = placements[index].MultiHopRates
		} else {
			a.orderArbRates[oid] = [2]uint64{placements[index].CounterTradeRate, 0}
		}
	}
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

	actionTaken, err := a.tryTransfers(currEpoch, a.distribution)
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
		// TODO: delete canceled orders from pending orders to avoid arbing something outside
		// drift tolerance??
		a.updatePendingOrders(buyOrders, buys, false)
		sells, sellsReport = a.multiTrade(sellOrders, true, a.cfg().DriftTolerance, currEpoch)
		a.updatePendingOrders(sellOrders, sells, true)
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

// TODO: test fee gap with simple arb mm, nil cfg
func feeGap(core botCoreAdaptor, multiHopCfg *MultiHopCfg, cex libxc.CEX, mkt *market) (*FeeGapStats, error) {
	lotSize := mkt.lotSize.Load()
	s := &FeeGapStats{}
	filled, buy, _, _, err := arbMMExtremaAndTrades(false, lotSize, 1, multiHopCfg, mkt, cex.VWAP, cex.InvVWAP)
	if err != nil {
		return nil, fmt.Errorf("VWAP buy error: %w", err)
	}
	if !filled {
		return s, nil
	}
	filled, sell, _, _, err := arbMMExtremaAndTrades(true, lotSize, 1, multiHopCfg, mkt, cex.VWAP, cex.InvVWAP)
	if err != nil {
		return nil, fmt.Errorf("VWAP sell error: %w", err)
	}
	if !filled {
		return s, nil
	}
	s.RemoteGap = sell - buy
	if multiHopCfg != nil {
		s.BasisPrice = (buy + sell) / 2
	} else {
		s.BasisPrice = cex.MidGap(mkt.baseID, mkt.quoteID)
	}
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
	feeGap, err := feeGap(a.core, a.cfg().MultiHop, a.CEX, a.market)
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

	cexMkts := make([][2]uint32, 0, 2)
	if a.cfg().isMultiHop() {
		cexMkts = append(cexMkts, a.cfg().MultiHop.BaseAssetMarket)
		cexMkts = append(cexMkts, a.cfg().MultiHop.QuoteAssetMarket)
	} else {
		cexMkts = append(cexMkts, [2]uint32{a.baseID, a.quoteID})
	}
	for _, mkt := range cexMkts {
		err = a.cex.SubscribeMarket(a.ctx, mkt[0], mkt[1])
		if err != nil {
			bookFeed.Close()
			return nil, fmt.Errorf("failed to subscribe to cex market: %v", err)
		}
	}

	tradeUpdates := a.cex.SubscribeTradeUpdates()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer bookFeed.Close()
		for {
			select {
			case ni, ok := <-bookFeed.Next():
				if !ok {
					a.log.Error("Stopping bot due to nil book feed.")
					a.kill()
					return
				}
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

		for _, mkt := range cexMkts {
			a.cex.UnsubscribeMarket(mkt[0], mkt[1])
		}
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
		orderArbRates:          make(map[order.OrderID][2]uint64),
		orderIndex:             make(map[bool]map[order.OrderID]int),
		cexTrades:              make(map[string]*cexTradeInfo),
	}
	arbMM.orderIndex[false] = make(map[order.OrderID]int)
	arbMM.orderIndex[true] = make(map[order.OrderID]int)

	adaptor.setBotLoop(arbMM.botLoop)
	return arbMM, nil
}
