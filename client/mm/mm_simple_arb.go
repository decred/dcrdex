// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"bytes"
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

// SimpleArbConfig is the configuration for an arbitrage bot that only places
// orders when there is a profitable arbitrage opportunity.
type SimpleArbConfig struct {
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

func (c *SimpleArbConfig) Validate() error {
	if c.ProfitTrigger <= 0 || c.ProfitTrigger > 1 {
		return fmt.Errorf("profit trigger must be 0 < t <= 1, but got %v", c.ProfitTrigger)
	}

	if c.MaxActiveArbs == 0 {
		return fmt.Errorf("must allow at least 1 active arb")
	}

	if c.NumEpochsLeaveOpen < 2 {
		return fmt.Errorf("arbs must be left open for at least 2 epochs")
	}

	return nil
}

// arbSequence represents an attempted arbitrage sequence.
type arbSequence struct {
	dexOrder       *core.Order
	cexOrderID     string
	dexRate        uint64
	cexRate        uint64
	cexOrderFilled bool
	dexOrderFilled bool
	sellOnDEX      bool
	startEpoch     uint64
}

type simpleArbMarketMaker struct {
	ctx              context.Context
	host             string
	baseID           uint32
	quoteID          uint32
	cex              botCexAdaptor
	core             botCoreAdaptor
	log              dex.Logger
	cfg              *SimpleArbConfig
	mkt              *core.Market
	book             dexOrderBook
	rebalanceRunning atomic.Bool

	activeArbsMtx sync.RWMutex
	activeArbs    []*arbSequence
}

// arbExists checks if an arbitrage opportunity exists.
func (a *simpleArbMarketMaker) arbExists() (exists, sellOnDex bool, lotsToArb, dexRate, cexRate uint64) {
	sellOnDex = false
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex)
	if exists {
		return
	}

	sellOnDex = true
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex)
	return
}

// arbExistsOnSide checks if an arbitrage opportunity exists either when
// buying or selling on the dex.
func (a *simpleArbMarketMaker) arbExistsOnSide(sellOnDEX bool) (exists bool, lotsToArb, dexRate, cexRate uint64) {
	noArb := func() (bool, uint64, uint64, uint64) {
		return false, 0, 0, 0
	}

	lotSize := a.mkt.LotSize
	var prevProfit uint64

	for numLots := uint64(1); ; numLots++ {
		dexAvg, dexExtrema, dexFilled, err := a.book.VWAP(numLots, a.mkt.LotSize, !sellOnDEX)
		if err != nil {
			a.log.Errorf("error calculating dex VWAP: %v", err)
			break
		}
		if !dexFilled {
			break
		}

		cexAvg, cexExtrema, cexFilled, err := a.cex.VWAP(a.baseID, a.quoteID, sellOnDEX, numLots*lotSize)
		if err != nil {
			a.log.Errorf("error calculating cex VWAP: %v", err)
			break
		}
		if !cexFilled {
			break
		}

		var buyRate, sellRate, buyAvg, sellAvg uint64
		if sellOnDEX {
			buyRate = cexExtrema
			sellRate = dexExtrema
			buyAvg = cexAvg
			sellAvg = dexAvg
		} else {
			buyRate = dexExtrema
			sellRate = cexExtrema
			buyAvg = dexAvg
			sellAvg = cexAvg
		}
		if buyRate >= sellRate {
			break
		}

		enough, err := a.core.SufficientBalanceForDEXTrade(dexExtrema, numLots*lotSize, sellOnDEX)
		if err != nil {
			a.log.Errorf("error checking sufficient balance: %v", err)
			break
		}
		if !enough {
			break
		}

		enough, err = a.cex.SufficientBalanceForCEXTrade(a.baseID, a.quoteID, !sellOnDEX, cexExtrema, numLots*lotSize)
		if err != nil {
			a.log.Errorf("error checking sufficient balance: %v", err)
			break
		}
		if !enough {
			break
		}

		feesInQuoteUnits, err := a.core.OrderFeesInUnits(sellOnDEX, false, dexAvg)
		if err != nil {
			a.log.Errorf("error calculating fees: %v", err)
			break
		}

		qty := numLots * lotSize
		quoteForBuy := calc.BaseToQuote(buyAvg, qty)
		quoteFromSell := calc.BaseToQuote(sellAvg, qty)
		if quoteFromSell-quoteForBuy <= feesInQuoteUnits {
			break
		}
		profitInQuote := quoteFromSell - quoteForBuy - feesInQuoteUnits
		profitInBase := calc.QuoteToBase((buyRate+sellRate)/2, profitInQuote)
		if profitInBase < prevProfit || float64(profitInBase)/float64(qty) < a.cfg.ProfitTrigger {
			break
		}

		prevProfit = profitInBase
		lotsToArb = numLots
		dexRate = dexExtrema
		cexRate = cexExtrema
	}

	if lotsToArb > 0 {
		a.log.Infof("arb opportunity - sellOnDex: %v, lotsToArb: %v, dexRate: %v, cexRate: %v: profit: %d", sellOnDEX, lotsToArb, dexRate, cexRate, prevProfit)
		return true, lotsToArb, dexRate, cexRate
	}

	return noArb()
}

// executeArb will execute an arbitrage sequence by placing orders on the dex
// and cex. An entry will be added to the a.activeArbs slice if both orders
// are successfully placed.
func (a *simpleArbMarketMaker) executeArb(sellOnDex bool, lotsToArb, dexRate, cexRate, epoch uint64) {
	a.log.Debugf("executing arb opportunity - sellOnDex: %v, lotsToArb: %v, dexRate: %v, cexRate: %v", sellOnDex, lotsToArb, dexRate, cexRate)

	a.activeArbsMtx.RLock()
	numArbs := len(a.activeArbs)
	a.activeArbsMtx.RUnlock()
	if numArbs >= int(a.cfg.MaxActiveArbs) {
		a.log.Infof("cannot execute arb because already at max arbs")
		return
	}

	if a.selfMatch(sellOnDex, dexRate) {
		a.log.Infof("cannot execute arb opportunity due to self-match")
		return
	}
	// also check self-match on CEX?

	// Hold the lock for this entire process because updates to the cex trade
	// may come even before the Trade function has returned, and in order to
	// be able to process them, the new arbSequence struct must already be in
	// the activeArbs slice.
	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	// Place cex order first. If placing dex order fails then can freely cancel cex order.
	cexTrade, err := a.cex.CEXTrade(a.ctx, a.baseID, a.quoteID, !sellOnDex, cexRate, lotsToArb*a.mkt.LotSize)
	if err != nil {
		a.log.Errorf("error placing cex order: %v", err)
		return
	}

	dexOrder, err := a.core.DEXTrade(dexRate, lotsToArb*a.mkt.LotSize, sellOnDex)
	if err != nil {
		if err != nil {
			a.log.Errorf("error placing dex order: %v", err)
		}

		err := a.cex.CancelTrade(a.ctx, a.baseID, a.quoteID, cexTrade.ID)
		if err != nil {
			a.log.Errorf("error canceling cex order: %v", err)
			// TODO: keep retrying failed cancel
		}
		return
	}

	a.activeArbs = append(a.activeArbs, &arbSequence{
		dexOrder:   dexOrder,
		dexRate:    dexRate,
		cexOrderID: cexTrade.ID,
		cexRate:    cexRate,
		sellOnDEX:  sellOnDex,
		startEpoch: epoch,
	})
}

func (a *simpleArbMarketMaker) sortedOrders() (buys, sells []*core.Order) {
	buys, sells = make([]*core.Order, 0), make([]*core.Order, 0)

	a.activeArbsMtx.RLock()
	for _, arb := range a.activeArbs {
		if arb.sellOnDEX {
			sells = append(sells, arb.dexOrder)
		} else {
			buys = append(buys, arb.dexOrder)
		}
	}
	a.activeArbsMtx.RUnlock()

	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })

	return buys, sells
}

// selfMatch checks if a order could match with any other orders
// already placed on the dex.
func (a *simpleArbMarketMaker) selfMatch(sell bool, rate uint64) bool {
	buys, sells := a.sortedOrders()

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
func (a *simpleArbMarketMaker) cancelArbSequence(arb *arbSequence) {
	if !arb.cexOrderFilled {
		err := a.cex.CancelTrade(a.ctx, a.baseID, a.quoteID, arb.cexOrderID)
		if err != nil {
			a.log.Errorf("failed to cancel cex trade ID %s: %v", arb.cexOrderID, err)
		}
	}

	if !arb.dexOrderFilled {
		err := a.core.Cancel(arb.dexOrder.ID)
		if err != nil {
			a.log.Errorf("failed to cancel dex order ID %s: %v", arb.dexOrder.ID, err)
		}
	}

	// keep retrying if failed to cancel?
}

// removeActiveArb removes the active arb at index i.
//
// activeArbsMtx MUST be held when calling this function.
func (a *simpleArbMarketMaker) removeActiveArb(i int) {
	a.activeArbs[i] = a.activeArbs[len(a.activeArbs)-1]
	a.activeArbs = a.activeArbs[:len(a.activeArbs)-1]
}

// handleCEXTradeUpdate is called when the CEX sends a notification that the
// status of a trade has changed.
func (a *simpleArbMarketMaker) handleCEXTradeUpdate(update *libxc.Trade) {
	if !update.Complete {
		return
	}

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for i, arb := range a.activeArbs {
		if arb.cexOrderID == update.ID {
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
func (a *simpleArbMarketMaker) handleDEXOrderUpdate(o *core.Order) {
	if o.Status <= order.OrderStatusBooked {
		return
	}

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for i, arb := range a.activeArbs {
		if bytes.Equal(arb.dexOrder.ID, o.ID) {
			arb.dexOrderFilled = true
			if arb.cexOrderFilled {
				a.removeActiveArb(i)
			}
			return
		}
	}
}

func (a *simpleArbMarketMaker) cancelAllOrders() {
	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for _, arb := range a.activeArbs {
		a.cancelArbSequence(arb)
	}
}

// rebalance checks if there is an arbitrage opportunity between the dex and cex,
// and if so, executes trades to capitalize on it.
func (a *simpleArbMarketMaker) rebalance(newEpoch uint64) {
	if !a.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer a.rebalanceRunning.Store(false)
	a.log.Tracef("rebalance: epoch %d", newEpoch)

	exists, sellOnDex, lotsToArb, dexRate, cexRate := a.arbExists()
	if exists {
		// Execution will not happen if it would cause a self-match.
		a.executeArb(sellOnDex, lotsToArb, dexRate, cexRate, newEpoch)
	}

	a.activeArbsMtx.Lock()
	remainingArbs := make([]*arbSequence, 0, len(a.activeArbs))
	for _, arb := range a.activeArbs {
		expired := newEpoch-arb.startEpoch > uint64(a.cfg.NumEpochsLeaveOpen)
		oppositeDirectionArbFound := exists && sellOnDex != arb.sellOnDEX

		if expired || oppositeDirectionArbFound {
			a.cancelArbSequence(arb)
		} else {
			remainingArbs = append(remainingArbs, arb)
		}
	}
	a.activeArbs = remainingArbs
	attemptRebalance := len(a.activeArbs) == 0
	a.activeArbsMtx.Unlock()

	if !attemptRebalance {
		return
	}

	// There will be no reserves, as this will only be called when there
	// are no active orders.
	rebalanceBase, _, _ := a.cex.PrepareRebalance(a.ctx, a.baseID)
	if rebalanceBase > 0 {
		err := a.cex.Deposit(a.ctx, a.baseID, uint64(rebalanceBase))
		if err != nil {
			a.log.Errorf("error depositing base asset: %v", err)
		}
	}
	if rebalanceBase < 0 {
		err := a.cex.Withdraw(a.ctx, a.baseID, uint64(-rebalanceBase))
		if err != nil {
			a.log.Errorf("error withdrawing base asset: %v", err)
		}
	}

	rebalanceQuote, _, _ := a.cex.PrepareRebalance(a.ctx, a.quoteID)
	if rebalanceQuote > 0 {
		err := a.cex.Deposit(a.ctx, a.quoteID, uint64(rebalanceQuote))
		if err != nil {
			a.log.Errorf("error depositing quote asset: %v", err)
		}
	}
	if rebalanceQuote < 0 {
		err := a.cex.Withdraw(a.ctx, a.quoteID, uint64(-rebalanceQuote))
		if err != nil {
			a.log.Errorf("error withdrawing quote asset: %v", err)
		}
	}

	a.registerFeeGap()
}

func (a *simpleArbMarketMaker) run(cfgUpdateManager *botCfgUpdateManager) {
	book, bookFeed, err := a.core.SyncBook(a.host, a.baseID, a.quoteID)
	if err != nil {
		a.log.Errorf("Failed to sync book: %v", err)
		return
	}
	a.book = book

	err = a.cex.SubscribeMarket(a.ctx, a.baseID, a.quoteID)
	if err != nil {
		a.log.Errorf("Failed to subscribe to cex market: %v", err)
		return
	}

	tradeUpdates, unsubscribe := a.cex.SubscribeTradeUpdates()
	defer unsubscribe()

	pauseEpochs := make(chan interface{})
	resumeEpochs := make(chan interface{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case n := <-bookFeed.Next():
				if n.Action == core.EpochMatchSummary {
					payload := n.Payload.(*core.EpochMatchSummaryPayload)
					a.rebalance(payload.Epoch + 1)
				}
			case <-pauseEpochs:
				<-resumeEpochs
			case <-a.ctx.Done():
				return
			}
		}
	}()

	pauseCEXUpdates := make(chan interface{})
	resumeCEXUpdates := make(chan interface{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case update := <-tradeUpdates:
				a.handleCEXTradeUpdate(update)
			case <-pauseCEXUpdates:
				<-resumeCEXUpdates
			case <-a.ctx.Done():
				return
			}
		}
	}()

	pauseDEXUpdates := make(chan interface{})
	resumeDEXUpdates := make(chan interface{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		orderUpdates := a.core.SubscribeOrderUpdates()
		for {
			select {
			case n := <-orderUpdates:
				a.handleDEXOrderUpdate(n)
			case <-pauseDEXUpdates:
				<-resumeDEXUpdates
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
			case cfg := <-cfgUpdateManager.updateBot:
				pauseEpochs <- struct{}{}
				pauseCEXUpdates <- struct{}{}
				pauseDEXUpdates <- struct{}{}
				cfgUpdateManager.botPaused <- struct{}{}
				a.cfg = cfg.SimpleArbConfig
				<-cfgUpdateManager.resumeBot
				resumeEpochs <- struct{}{}
				resumeCEXUpdates <- struct{}{}
				resumeDEXUpdates <- struct{}{}
			case <-a.ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	a.cancelAllOrders()
}

func (a *simpleArbMarketMaker) registerFeeGap() {
	feeGap, err := feeGap(a.core, a.cex, a.mkt.BaseID, a.mkt.QuoteID, a.mkt.LotSize)
	if err != nil {
		a.log.Warnf("error getting fee-gap stats: %v", err)
		return
	}
	a.core.registerFeeGap(feeGap)
}

func RunSimpleArbBot(ctx context.Context, cfg *BotConfig, c botCoreAdaptor, cex botCexAdaptor, cfgUpdateManager *botCfgUpdateManager, log dex.Logger) {
	if cfg.SimpleArbConfig == nil {
		// implies bug in caller
		log.Errorf("No arb config provided. Exiting.")
		return
	}

	mkt, err := c.ExchangeMarket(cfg.Host, cfg.BaseID, cfg.QuoteID)
	if err != nil {
		log.Errorf("Failed to get market: %v", err)
		return
	}

	(&simpleArbMarketMaker{
		ctx:        ctx,
		host:       cfg.Host,
		baseID:     cfg.BaseID,
		quoteID:    cfg.QuoteID,
		cex:        cex,
		core:       c,
		log:        log,
		mkt:        mkt,
		cfg:        cfg.SimpleArbConfig,
		activeArbs: make([]*arbSequence, 0),
	}).run(cfgUpdateManager)
}
