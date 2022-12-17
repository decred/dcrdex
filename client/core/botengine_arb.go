package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// ArbEngineCfg is the configuration for an ArbEngine.
type ArbEngineCfg struct {
	// CEXName is the name of the cex that the bot will arbitrage.
	CEXName string `json:"cexName"`
	// ProfitTrigger is the minimum profit before a cross-exchange trade
	// sequence is initiated. Range: 0 < ProfitTrigger << 1. For example, if
	// the ProfitTrigger is 0.01 and a trade sequence would produce a 1% profit
	// or better, a trade sequence will be initiated.
	ProfitTrigger float64 `json:"profitTrigger"`
	// MaxActiveArbs sets a limit on the number of active arbitrage sequences
	// that can be open simultaneously.
	MaxActiveArbs uint32 `json:"maxActiveArbs"`
	// NumEpochsLeaveOpen is the number of epochs an arbitrage sequence will
	// stay open if one or both of the orders were not filled.
	NumEpochsLeaveOpen uint32 `json:"numEpochsLeaveOpen"`
}

func (cfg *ArbEngineCfg) validate() error {
	if cfg.ProfitTrigger <= 0 || cfg.ProfitTrigger > 1 {
		return fmt.Errorf("profit trigger must be 0 < t <= 1, but got %v", cfg.ProfitTrigger)
	}

	if cfg.MaxActiveArbs == 0 {
		return fmt.Errorf("must allow at least 1 active arb")
	}

	if cfg.NumEpochsLeaveOpen < 2 {
		return fmt.Errorf("arbs must be left open for at least 2 epochs")
	}

	return nil
}

// arbSequence represents an attempted arbitrage sequence.
type arbSequence struct {
	dexOrderID     order.OrderID
	cexOrderID     string
	dexRate        uint64
	cexRate        uint64
	cexOrderFilled bool
	dexOrderFilled bool
	sellOnDEX      bool
	startEpoch     uint64
}

// arbEngineInputs are the input functions required an ArbEngine to function.
type arbEngineInputs interface {
	lotSize() uint64
	maxBuy(rate uint64) (*MaxOrderEstimate, error)
	maxSell() (*MaxOrderEstimate, error)
	vwap(numLots uint64, sell bool) (avgRate uint64, extrema uint64, filled bool, err error)
	sortedOrders() (buys, sells []*Order)
	placeOrder(lots, rate uint64, sell bool) (order.OrderID, error)
	cancelOrder(oid order.OrderID) error
}

type arbEngine struct {
	ctx               context.Context
	cex               libxc.CEX
	cexTradeUpdatesID int
	inputs            arbEngineInputs
	cfgV              atomic.Value
	log               dex.Logger

	base  uint32
	quote uint32

	activeArbsMtx sync.RWMutex
	activeArbs    []*arbSequence

	rebalanceRunning uint32 //atomic
}

func newArbEngine(cfg *ArbEngineCfg, inputs arbEngineInputs, log dex.Logger, net dex.Network, base, quote uint32, cex libxc.CEX) (botEngine, error) {
	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	a := &arbEngine{
		inputs:     inputs,
		log:        log,
		activeArbs: make([]*arbSequence, 0, 8),
		base:       base,
		quote:      quote,
		cex:        cex,
	}
	a.cfgV.Store(cfg)
	return (botEngine)(a), nil
}

func (a *arbEngine) cfg() *ArbEngineCfg {
	return a.cfgV.Load().(*ArbEngineCfg)
}

// run starts the arb engine.
func (a *arbEngine) run(ctx context.Context) (*sync.WaitGroup, error) {
	a.ctx = ctx
	cfg := a.cfg()

	markets, err := a.cex.Markets()
	if err != nil {
		return nil, err
	}
	var foundMarket bool
	for _, market := range markets {
		if market.Base == a.base && market.Quote == a.quote {
			foundMarket = true
		}
	}
	if !foundMarket {
		return nil, fmt.Errorf("%s does not have a %s-%s market", cfg.CEXName, dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote))
	}

	err = a.cex.SubscribeMarket(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to %s %s-%s market: %v",
			a.cfg().CEXName, dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), err)
	}

	tradeUpdates, tradeUpdatesID := a.cex.SubscribeTradeUpdates()
	a.cexTradeUpdatesID = tradeUpdatesID

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case update := <-tradeUpdates:
				a.log.Tracef("received CEX trade update: %+v", update)
				a.handleCEXTradeUpdate(update)
			case <-ctx.Done():
				return
			}
		}
	}()

	return wg, nil
}

// stop stops the arb engine. All orders on the CEX are cancelled. The makerBot
// cancels the orders on the DEX.
func (a *arbEngine) stop() {
	a.activeArbsMtx.RLock()
	defer a.activeArbsMtx.RUnlock()

	for _, arb := range a.activeArbs {
		if !arb.cexOrderFilled {
			err := a.cex.CancelTrade(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), arb.cexOrderID)
			if err != nil {
				a.log.Errorf("error cancelling CEX order id %s: %v", arb.cexOrderID, err)
			}
		}
	}
	a.activeArbs = make([]*arbSequence, 0)

	err := a.cex.UnsubscribeMarket(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote))
	if err != nil {
		a.log.Errorf("failed to unsubscribe market: %v", err)
	}
}

// update updates the configuration of the bot engine.
func (a *arbEngine) update(cfgB []byte) error {
	cfg := new(ArbEngineCfg)

	err := json.Unmarshal(cfgB, &cfg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal gap engine config: %w", err)
	}

	err = cfg.validate()
	if err != nil {
		return fmt.Errorf("failed to validate gap engine config: %w", err)
	}

	oldCfg := a.cfg()
	if oldCfg.CEXName != cfg.CEXName {
		return errors.New("the CEX cannot be updated")
	}

	a.cfgV.Store(cfg)

	return nil
}

// notify notifies the engine about DEX events.
func (a *arbEngine) notify(note interface{}) {
	switch n := note.(type) {
	case newEpochEngineNote:
		a.rebalance(uint64(n))
	case orderUpdateEngineNote:
		a.handleDEXOrderUpdate(n)
	}
}

// initialLotsRequired returns the amount of lots of a combination of the
// base and quote assets that are required to be in the user's wallets before
// starting the bot.
func (a *arbEngine) initialLotsRequired() uint64 {
	// At least have one lot of something on the DEX...
	return 1
}

// rebalance checks if there is an arbitrage opportunity between the dex and cex,
// and if so, executes trades to capitalize on it.
func (a *arbEngine) rebalance(newEpoch uint64) {
	if !atomic.CompareAndSwapUint32(&a.rebalanceRunning, 0, 1) {
		return
	}
	defer atomic.StoreUint32(&a.rebalanceRunning, 0)

	a.log.Tracef("arbEngine rebalance epoch: %v", newEpoch)

	cfg := a.cfg()

	exists, sellOnDex, lotsToArb, dexRate, cexRate := a.arbExists()
	if exists {
		// Execution will not happen if will cause a self-match. Orders that
		// can cause a self-match will be cancelled below when cancelling
		// arbs in the opposite direction.
		a.executeArb(sellOnDex, lotsToArb, dexRate, cexRate, newEpoch, cfg)
	}

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	remainingArbs := make([]*arbSequence, 0, len(a.activeArbs))
	for _, arb := range a.activeArbs {
		expired := newEpoch-arb.startEpoch > uint64(cfg.NumEpochsLeaveOpen)
		oppositeDirectionArbFound := exists && sellOnDex != arb.sellOnDEX

		if expired || oppositeDirectionArbFound {
			a.cancelArbSequence(arb)
		} else {
			remainingArbs = append(remainingArbs, arb)
		}
	}

	a.activeArbs = remainingArbs
}

// arbExists checks if an arbitrage opportunity exists.
func (a *arbEngine) arbExists() (exists, sellOnDex bool, lotsToArb, dexRate, cexRate uint64) {
	cfg := a.cfg()

	cexBaseBalance, err := a.cex.Balance(dex.BipIDSymbol(a.base))
	if err != nil {
		a.log.Errorf("failed to get cex balance for %v", dex.BipIDSymbol(a.base))
		return false, false, 0, 0, 0
	}

	cexQuoteBalance, err := a.cex.Balance(dex.BipIDSymbol(a.quote))
	if err != nil {
		a.log.Errorf("failed to get cex balance for %v", dex.BipIDSymbol(a.quote))
		return false, false, 0, 0, 0
	}

	sellOnDex = false
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex, cexBaseBalance.Available, cexQuoteBalance.Available, cfg)
	if exists {
		return
	}

	sellOnDex = true
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex, cexBaseBalance.Available, cexQuoteBalance.Available, cfg)
	return
}

// arbExistsOnSide checks if an arbitrage opportunity exists either when
// buying or selling on the dex.
func (a *arbEngine) arbExistsOnSide(sellOnDEX bool, cexBaseBalance uint64, cexQuoteBalance uint64, cfg *ArbEngineCfg) (exists bool, lotsToArb, dexRate, cexRate uint64) {
	noArb := func() (bool, uint64, uint64, uint64) {
		return false, 0, 0, 0
	}

	lotSize := a.inputs.lotSize()

	a.log.Tracef("arbExistsOnSide - sellOnDex: %v", sellOnDEX)

	// maxLots is the max amount of lots of the base asset that can be traded
	// on the exchange where the base asset is being sold.
	var maxLots uint64
	if sellOnDEX {
		maxOrder, err := a.inputs.maxSell()
		if err != nil {
			a.log.Errorf("maxSell error: %v", err)
			return noArb()
		}
		maxLots = maxOrder.Swap.Lots
	} else {
		maxLots = cexBaseBalance / lotSize
	}
	if maxLots == 0 {
		a.log.Infof("not enough balance to arb 1 lot")
		return noArb()
	}

	a.log.Tracef("arbExistsOnSide - num lots of base available to sell: %v", maxLots)

	for numLots := uint64(1); numLots <= maxLots; numLots++ {
		a.log.Tracef("checking arb for numLots=%v", numLots)

		dexAvg, dexExtrema, dexFilled, err := a.inputs.vwap(numLots, !sellOnDEX)
		if err != nil {
			a.log.Errorf("error calculating dex VWAP: %v", err)
			return noArb()
		}
		a.log.Tracef("DEX VWAP - avg: %v, extrema: %v, filled: %v", dexAvg, dexExtrema, dexFilled)
		if !dexFilled {
			break
		}
		// If buying on dex, check that we have enough to buy at this rate.
		if !sellOnDEX {
			maxBuy, err := a.inputs.maxBuy(dexExtrema)
			if err != nil {
				a.log.Errorf("maxBuy error: %v")
				return noArb()
			}
			if maxBuy.Swap.Lots < numLots {
				a.log.Infof("maxBuy.Swap.Lots %v < numLots %v", maxBuy.Swap.Lots, numLots)
				break
			}
		}

		cexAvg, cexExtrema, cexFilled, err := a.cex.VWAP(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), sellOnDEX, numLots*lotSize)
		if err != nil {
			a.log.Errorf("error calculating cex VWAP: %v", err)
			return
		}
		a.log.Tracef("CEX VWAP - avg: %v, extrema: %v, filled: %v", cexAvg, cexExtrema, cexFilled)
		if !cexFilled {
			return
		}
		// If buying on cex, make sure we have enough to buy at this rate
		amountNeeded := calc.BaseToQuote(cexExtrema, numLots*lotSize)
		if sellOnDEX && (amountNeeded > cexQuoteBalance) {
			break
		}

		var priceRatio float64
		if sellOnDEX {
			priceRatio = float64(dexAvg) / float64(cexAvg)
		} else {
			priceRatio = float64(cexAvg) / float64(dexAvg)
		}

		a.log.Tracef("priceRatio - %v", priceRatio)

		// Is there an opportunity?
		if priceRatio > (1 + cfg.ProfitTrigger) {
			lotsToArb = numLots
			dexRate = dexExtrema
			cexRate = cexExtrema
		} else {
			break
		}
	}

	if lotsToArb > 0 {
		return true, lotsToArb, dexRate, cexRate
	}

	return noArb()
}

// executeArb will execute an arbitrage sequence by placing orders on the dex
// and cex. An entry will be added to the a.activeArbs slice if both orders
// are successfully placed.
func (a *arbEngine) executeArb(sellOnDex bool, lotsToArb, dexRate, cexRate, epoch uint64, cfg *ArbEngineCfg) {
	a.activeArbsMtx.RLock()
	numArbs := len(a.activeArbs)
	a.activeArbsMtx.RUnlock()
	if numArbs >= int(cfg.MaxActiveArbs) {
		a.log.Infof("cannot execute arb because already at max arbs")
		return
	}

	if a.selfMatch(sellOnDex, dexRate) {
		a.log.Infof("cannot execute arb opportunity due to self-match")
		return
	}
	// also check self-match on CEX?

	// Hold the lock for this entire process because updates to the cex trade
	// may come even before the Trade function is complete, and in order to
	// be able to process them, the new arbSequence struct must already be in
	// the activeArbs slice.
	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	// Place cex order first. If placing dex order fails then can freely cancel cex order.
	cexTradeID := a.cex.GenerateTradeID()
	err := a.cex.Trade(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), !sellOnDex, cexRate, lotsToArb*a.inputs.lotSize(), a.cexTradeUpdatesID, cexTradeID)
	if err != nil {
		a.log.Errorf("error placing cex order: %v", err)
		return
	}

	dexOrderID, err := a.inputs.placeOrder(lotsToArb, dexRate, sellOnDex)
	if err != nil {
		a.log.Errorf("error placing dex order: %v", err)

		err := a.cex.CancelTrade(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), cexTradeID)
		if err != nil {
			a.log.Errorf("error canceling cex order: %v", err)
		}
		return
	}

	a.activeArbs = append(a.activeArbs, &arbSequence{
		dexOrderID: dexOrderID,
		dexRate:    dexRate,
		cexOrderID: cexTradeID,
		cexRate:    cexRate,
		sellOnDEX:  sellOnDex,
		startEpoch: epoch,
	})
}

// selfMatch checks if a order will match with any other orders
// already placed on the dex.
func (a *arbEngine) selfMatch(sell bool, rate uint64) bool {
	buys, sells := a.inputs.sortedOrders()

	if sell && len(buys) > 0 && buys[0].Rate >= rate {
		return true
	}

	if !sell && len(sells) > 0 && sells[0].Rate <= rate {
		return true
	}

	return false
}

// cancelArbSequence will cancel both the dex and cex orders in an arb sequence
// if they have not yet been filled.
func (a *arbEngine) cancelArbSequence(arb *arbSequence) {
	if !arb.cexOrderFilled {
		err := a.cex.CancelTrade(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), arb.cexOrderID)
		if err != nil {
			a.log.Errorf("failed to cancel cex trade ID %s: %v", arb.cexOrderID, err)
		}
	}

	if !arb.dexOrderFilled {
		err := a.inputs.cancelOrder(arb.dexOrderID)
		if err != nil {
			a.log.Errorf("failed to cancel dex order ID %s: %v", arb.dexOrderID, err)
		}
	}

	// keep retrying if failed to cancel?
}

// handleCEXTradeUpdate is called when the CEX sends a notification that the
// status of a trade has changed.
func (a *arbEngine) handleCEXTradeUpdate(update *libxc.TradeUpdate) {
	if !update.Complete {
		return
	}

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for i, arb := range a.activeArbs {
		if arb.cexOrderID == update.TradeID {
			arb.cexOrderFilled = true
			if arb.dexOrderFilled {
				a.removeActiveArb(i)
			}
			return
		}
	}
}

// handleDEXOrderUpdate is called when the DEX sends a notification that the
// status of an order has changed.
func (a *arbEngine) handleDEXOrderUpdate(o *Order) {
	if o.Status <= order.OrderStatusBooked {
		return
	}

	var oid order.OrderID
	copy(oid[:], o.ID)

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for i, arb := range a.activeArbs {
		if arb.dexOrderID == oid {
			arb.dexOrderFilled = true
			if arb.cexOrderFilled {
				a.removeActiveArb(i)
			}
			return
		}
	}
}

// removeActiveArb removes the active arb at index i.
//
// activeArbsMtx MUST be held when calling this function.
func (a *arbEngine) removeActiveArb(i int) {
	a.log.Tracef("removing active arb - %+v", a.activeArbs[i])
	a.activeArbs[i] = a.activeArbs[len(a.activeArbs)-1]
	a.activeArbs = a.activeArbs[:len(a.activeArbs)-1]
}
