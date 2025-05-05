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

// MultiHopCfg is the configuration for a multi-hop market maker. It
// specifies the two markets on the CEX that the bot will use to make
// an arbitrage trade. The BaseAssetMarket is the market on which the
// base asset of the DEX market is traded. The QuoteAssetMarket is the
// market on which the quote asset of the DEX market is traded. The
// other asset (other than the DEX base or quote asset) on each market
// must be the same.
type MultiHopCfg struct {
	BaseAssetMarket  [2]uint32 `json:"baseAssetMarket"`
	QuoteAssetMarket [2]uint32 `json:"quoteAssetMarket"`
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

// multiHopArbCompletionParams is called when a trade is completed on the
// CEX. It checks if the completion of this trade should trigger another
// trade on the CEX that will complete the multi-hop arbitrage and returns
// the parameters of that trade.
func (a *arbMarketMaker) multiHopArbCompletionParams(update *libxc.Trade) (makeTrade bool, baseID, quoteID uint32, sell bool, qty uint64) {
	if !update.Market {
		// multi-hop arbs always use market trades on the CEX.
		return
	}

	multiHopCfg := a.cfg().MultiHop
	baseAssetMarket := a.baseID == update.BaseID || a.baseID == update.QuoteID
	quoteAssetMarket := a.quoteID == update.BaseID || a.quoteID == update.QuoteID

	if baseAssetMarket {
		baseID = multiHopCfg.QuoteAssetMarket[0]
		quoteID = multiHopCfg.QuoteAssetMarket[1]
		sell = multiHopCfg.QuoteAssetMarket[1] == a.quoteID
		if a.baseID == update.BaseID {
			qty = update.QuoteFilled
			makeTrade = update.Sell
		} else {
			qty = update.BaseFilled
			makeTrade = !update.Sell
		}
	} else if quoteAssetMarket {
		baseID = multiHopCfg.BaseAssetMarket[0]
		quoteID = multiHopCfg.BaseAssetMarket[1]
		sell = multiHopCfg.BaseAssetMarket[1] == a.baseID
		if a.quoteID == update.QuoteID {
			qty = update.BaseFilled
			makeTrade = !update.Sell
		} else {
			qty = update.QuoteFilled
			makeTrade = update.Sell
		}
	} else {
		a.log.Errorf("multiHopArbCompletionParams: trade is on unknown market: %+v", update)
		return false, 0, 0, false, 0
	}

	if !makeTrade {
		return false, 0, 0, false, 0
	}

	return
}

func (a *arbMarketMaker) handleCEXTradeUpdate(update *libxc.Trade) {
	if !update.Complete {
		return
	}

	a.cexTradesMtx.Lock()
	if _, ok := a.cexTrades[update.ID]; !ok {
		a.cexTradesMtx.Unlock()
		return
	}
	delete(a.cexTrades, update.ID)
	a.cexTradesMtx.Unlock()

	cfg := a.cfg()
	if !cfg.isMultiHop() {
		return
	}

	if !update.Market {
		a.log.Errorf("multi hop bot cex trade is not a market trade: %+v", update)
		return
	}

	makeTrade, baseID, quoteID, sell, qty := a.multiHopArbCompletionParams(update)
	if makeTrade {
		a.tradeOnCEX(baseID, quoteID, 0, qty, sell, libxc.OrderTypeMarket)
	}
}

// tradeOnCEX executes a trade on the CEX.
func (a *arbMarketMaker) tradeOnCEX(baseID, quoteID uint32, rate, qty uint64, sell bool, orderType libxc.OrderType) {
	a.cexTradesMtx.Lock()

	cexTrade, err := a.cex.CEXTrade(a.ctx, baseID, quoteID, sell, rate, qty, orderType)
	if err != nil {
		a.cexTradesMtx.Unlock()
		a.log.Errorf("Error sending trade to CEX: %v", err)
		return
	}

	// Keep track of the epoch in which the trade was sent to the CEX. This way
	// the bot can cancel the trade if it is not filled after a certain number
	// of epochs.
	a.cexTrades[cexTrade.ID] = a.currEpoch.Load()
	a.cexTradesMtx.Unlock()

	a.handleCEXTradeUpdate(cexTrade)
}

// initiateMultiHopArb is called when a DEX order is matched and a multi-hop
// arb should be started. The second trade of the multi-hop arb is executed
// when this trade is complete.
func (a *arbMarketMaker) initiateMultiHopArb(dexSell bool, match *core.Match) {
	cfg := a.cfg()
	var baseID, quoteID uint32
	var sell bool
	var qty uint64

	if dexSell {
		baseID = cfg.MultiHop.QuoteAssetMarket[0]
		quoteID = cfg.MultiHop.QuoteAssetMarket[1]
		sell = a.quoteID == baseID
		qty = calc.BaseToQuote(match.Rate, match.Qty)
	} else {
		baseID = cfg.MultiHop.BaseAssetMarket[0]
		quoteID = cfg.MultiHop.BaseAssetMarket[1]
		sell = a.baseID == baseID
		qty = match.Qty
	}

	a.tradeOnCEX(baseID, quoteID, 0, qty, sell, libxc.OrderTypeMarket)
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

			if a.cfg().isMultiHop() {
				a.initiateMultiHopArb(o.Sell, match)
			} else {
				a.tradeOnCEX(a.baseID, a.quoteID, cexRate, match.Qty, !o.Sell, libxc.OrderTypeLimit)
			}
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

// aggregateRates returns the rate on a DEX market based on the rates on two
// CEX markets. One of the CEX markets contains the DEX market's base asset,
// and the other contains the DEX market's quote asset. The two CEX markets
// must both contain the same third asset.
func aggregateRates(baseMarketRate, quoteMarketRate uint64, mkt *market, baseMarket, quoteMarket [2]uint32) uint64 {
	convBaseRate := convRate(baseMarketRate, baseMarket[0], baseMarket[1])
	convQuoteRate := convRate(quoteMarketRate, quoteMarket[0], quoteMarket[1])
	if mkt.baseID != baseMarket[0] {
		convBaseRate = 1 / convBaseRate
	}
	if mkt.quoteID != quoteMarket[1] {
		convQuoteRate = 1 / convQuoteRate
	}
	convAggRate := convBaseRate * convQuoteRate
	return msgRate(convAggRate, mkt.baseID, mkt.quoteID)
}

type vwapFunc func(baseID, quoteID uint32, sell bool, qty uint64) (vwap uint64, extrema uint64, filled bool, err error)

// multiHopPriceExtrema calculates the extrema price for buying or selling a
// certain asset on one of the multi hop markets.
//
//   - assetID is the asset to which assetQty and receiveAsset refer.
//   - receiveAsset specifies whether you want to receive the asset or use it
//     to acquire the counter asset.
//     We use "receiveAsset" rather than "sell" to avoid confusion, because the
//     asset may be the base or quote asset of the market.
//   - counterQty is the amount of the counter asset that will be received as a
//     result of the multi-hop trade.
//
// For example, on a DCR/USDT market, if assetID is USDT, and receiveAsset is false,
// this means that we will be buying DCR on the CEX using qty USDT.
func multiHopPriceExtrema(market [2]uint32, assetID uint32, depth uint64, receiveAsset bool,
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
		return 0, 0, false, fmt.Errorf("error getting VWAP: %w", err)
	}
	if !filled {
		return
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
	sell      bool
}

func multiHopArbTrades(mkt [2]uint32, sell bool, assetID uint32, minQty, maxQty, rate uint64) []*arbTradeArgs {
	qtys := [2]uint64{}
	if sell && (mkt[0] == assetID) {
		qtys[0] = minQty
		qtys[1] = maxQty
	} else if sell && (mkt[1] == assetID) {
		qtys[0] = calc.QuoteToBase(rate, minQty)
		qtys[1] = calc.QuoteToBase(rate, maxQty)
	} else if !sell && (mkt[0] == assetID) {
		qtys[0] = calc.BaseToQuote(rate, minQty)
		qtys[1] = calc.BaseToQuote(rate, maxQty)
	} else {
		qtys[0] = minQty
		qtys[1] = maxQty
	}
	return []*arbTradeArgs{
		{baseID: mkt[0], quoteID: mkt[1], sell: sell, qty: qtys[0], orderType: libxc.OrderTypeMarket},
		{baseID: mkt[0], quoteID: mkt[1], sell: sell, qty: qtys[1], orderType: libxc.OrderTypeMarket},
	}
}

// multiHopRate returns the aggregate rate that can be achieved by completing
// a multi-hop trade, and the minimum and maximum quantities for the trades
// on both the base and quote asset markets.
func multiHopRateAndTrades(sellOnDEX bool, depth, numLots uint64, multiHopCfg *MultiHopCfg, mkt *market, vwap, invVwap vwapFunc) (uint64, bool, []*arbTradeArgs, error) {
	intermediateAsset := multiHopCfg.BaseAssetMarket[0]
	if mkt.baseID == intermediateAsset {
		intermediateAsset = multiHopCfg.BaseAssetMarket[1]
	}

	// First, we find the extrema price for buying or selling the base asset on
	// the base asset market.
	// intAssetQty is either the quantity of the intermediate asset that will
	// be received by selling "depth" units of the base asset, or the amount
	// required to buy "depth" units of the base asset.
	receiveBase := !sellOnDEX
	baseRate, intAssetQty, filled, err := multiHopPriceExtrema(multiHopCfg.BaseAssetMarket, mkt.baseID, depth, receiveBase, vwap, invVwap)
	if err != nil {
		return 0, false, nil, fmt.Errorf("error getting intermediate market VWAP: %w", err)
	}
	if !filled {
		return 0, false, nil, nil
	}

	baseMktSell := receiveBase != (multiHopCfg.BaseAssetMarket[0] == mkt.baseID)
	lotSize := mkt.lotSize.Load()
	baseArbTrades := multiHopArbTrades(multiHopCfg.BaseAssetMarket, !baseMktSell, mkt.baseID, lotSize, lotSize*numLots, baseRate)

	// Next we find the rate on the quote asset market to buy or sell the
	// required amount of the intermediate asset.
	receiveIntermediate := !sellOnDEX
	quoteRate, _, filled, err := multiHopPriceExtrema(multiHopCfg.QuoteAssetMarket, intermediateAsset, intAssetQty, receiveIntermediate, vwap, invVwap)
	if err != nil {
		return 0, false, nil, fmt.Errorf("error getting target market VWAP: %w", err)
	}
	if !filled {
		return 0, false, nil, nil
	}

	quoteMktSell := receiveIntermediate != (multiHopCfg.QuoteAssetMarket[1] == mkt.quoteID)
	var intAssetMinQty uint64
	if multiHopCfg.BaseAssetMarket[0] == mkt.baseID {
		intAssetMinQty = calc.BaseToQuote(baseRate, lotSize)
	} else {
		intAssetMinQty = calc.QuoteToBase(baseRate, lotSize)
	}
	quoteArbTrades := multiHopArbTrades(multiHopCfg.QuoteAssetMarket, !quoteMktSell, intermediateAsset, intAssetMinQty, intAssetMinQty*numLots, quoteRate)
	allArbTrades := append(baseArbTrades, quoteArbTrades...)

	// Finally, we aggregate the rates.
	aggregatedRate := aggregateRates(baseRate, quoteRate, mkt, multiHopCfg.BaseAssetMarket, multiHopCfg.QuoteAssetMarket)
	return aggregatedRate, true, allArbTrades, nil
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
func arbMMExtremaAndTrades(sell bool, depth, numLots uint64, multiHopCfg *MultiHopCfg, mkt *market, vwap, invVwap vwapFunc) (uint64, bool, []*arbTradeArgs, error) {
	if multiHopCfg != nil {
		return multiHopRateAndTrades(sell, depth, numLots, multiHopCfg, mkt, vwap, invVwap)
	}

	return singleHopRateAndTrades(sell, depth, numLots, mkt, vwap, invVwap)
}

// validateArbTrades validates the potential arb trades that may be executed in
// order to avoid placing orders on the DEX for which the arb trade on the CEX
// would be invalid.
func (a *arbMarketMaker) validateArbTrades(arbTrades []*arbTradeArgs) error {
	for _, arbTrade := range arbTrades {
		err := a.cex.ValidateTrade(arbTrade.baseID, arbTrade.quoteID, arbTrade.sell, arbTrade.rate, arbTrade.qty, arbTrade.orderType)
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

			cexRate, filled, arbTrades, err := arbMMExtremaAndTrades(sellOnDEX,
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

// TODO: test fee gap with simple arb mm, nil cfg
func feeGap(core botCoreAdaptor, multiHopCfg *MultiHopCfg, cex libxc.CEX, mkt *market) (*FeeGapStats, error) {
	lotSize := mkt.lotSize.Load()
	s := &FeeGapStats{}
	buy, filled, _, err := arbMMExtremaAndTrades(false, lotSize, 1, multiHopCfg, mkt, cex.VWAP, cex.InvVWAP)
	if err != nil {
		return nil, fmt.Errorf("VWAP buy error: %w", err)
	}
	if !filled {
		return s, nil
	}
	sell, filled, _, err := arbMMExtremaAndTrades(true, lotSize, 1, multiHopCfg, mkt, cex.VWAP, cex.InvVWAP)
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
		pendingOrders:          make(map[order.OrderID]uint64),
		cexTrades:              make(map[string]uint64),
	}

	adaptor.setBotLoop(arbMM.botLoop)
	return arbMM, nil
}
