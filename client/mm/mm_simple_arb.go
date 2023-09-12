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
	// BaseOptions are the multi-order options for the base asset wallet.
	BaseOptions map[string]string `json:"baseOptions"`
	// QuoteOptions are the multi-order options for the quote asset wallet.
	QuoteOptions map[string]string `json:"quoteOptions"`
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
	ctx     context.Context
	host    string
	baseID  uint32
	quoteID uint32
	cex     libxc.CEX
	// cexTradeUpdatesID is passed to the Trade function of the cex
	// so that the cex knows to send update notifications for the
	// trade back to this bot.
	cexTradeUpdatesID int
	core              clientCore
	log               dex.Logger
	cfg               *SimpleArbConfig
	mkt               *core.Market
	book              dexOrderBook
	rebalanceRunning  atomic.Bool

	activeArbsMtx sync.RWMutex
	activeArbs    []*arbSequence
}

// rebalance checks if there is an arbitrage opportunity between the dex and cex,
// and if so, executes trades to capitalize on it.
func (a *simpleArbMarketMaker) rebalance(newEpoch uint64) {
	if !a.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer a.rebalanceRunning.Store(false)

	exists, sellOnDex, lotsToArb, dexRate, cexRate := a.arbExists()
	if exists {
		// Execution will not happen if it would cause a self-match.
		a.executeArb(sellOnDex, lotsToArb, dexRate, cexRate, newEpoch)
	}

	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

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
}

// arbExists checks if an arbitrage opportunity exists.
func (a *simpleArbMarketMaker) arbExists() (exists, sellOnDex bool, lotsToArb, dexRate, cexRate uint64) {
	cexBaseBalance, err := a.cex.Balance(dex.BipIDSymbol(a.baseID))
	if err != nil {
		a.log.Errorf("failed to get cex balance for %v: %v", dex.BipIDSymbol(a.baseID), err)
		return false, false, 0, 0, 0
	}

	cexQuoteBalance, err := a.cex.Balance(dex.BipIDSymbol(a.quoteID))
	if err != nil {
		a.log.Errorf("failed to get cex balance for %v: %v", dex.BipIDSymbol(a.quoteID), err)
		return false, false, 0, 0, 0
	}

	sellOnDex = false
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex, cexBaseBalance.Available, cexQuoteBalance.Available)
	if exists {
		return
	}

	sellOnDex = true
	exists, lotsToArb, dexRate, cexRate = a.arbExistsOnSide(sellOnDex, cexBaseBalance.Available, cexQuoteBalance.Available)
	return
}

// arbExistsOnSide checks if an arbitrage opportunity exists either when
// buying or selling on the dex.
func (a *simpleArbMarketMaker) arbExistsOnSide(sellOnDEX bool, cexBaseBalance, cexQuoteBalance uint64) (exists bool, lotsToArb, dexRate, cexRate uint64) {
	noArb := func() (bool, uint64, uint64, uint64) {
		return false, 0, 0, 0
	}

	lotSize := a.mkt.LotSize

	// maxLots is the max amount of lots of the base asset that can be traded
	// on the exchange where the base asset is being sold.
	var maxLots uint64
	if sellOnDEX {
		maxOrder, err := a.core.MaxSell(a.host, a.baseID, a.quoteID)
		if err != nil {
			a.log.Errorf("MaxSell error: %v", err)
			return noArb()
		}
		maxLots = maxOrder.Swap.Lots
	} else {
		maxLots = cexBaseBalance / lotSize
	}
	if maxLots == 0 {
		return noArb()
	}

	for numLots := uint64(1); numLots <= maxLots; numLots++ {
		dexAvg, dexExtrema, dexFilled, err := a.book.VWAP(numLots, a.mkt.LotSize, !sellOnDEX)
		if err != nil {
			a.log.Errorf("error calculating dex VWAP: %v", err)
			return noArb()
		}
		if !dexFilled {
			break
		}
		// If buying on dex, check that we have enough to buy at this rate.
		if !sellOnDEX {
			maxBuy, err := a.core.MaxBuy(a.host, a.baseID, a.quoteID, dexExtrema)
			if err != nil {
				a.log.Errorf("maxBuy error: %v")
				return noArb()
			}
			if maxBuy.Swap.Lots < numLots {
				break
			}
		}

		cexAvg, cexExtrema, cexFilled, err := a.cex.VWAP(dex.BipIDSymbol(a.baseID), dex.BipIDSymbol(a.quoteID), sellOnDEX, numLots*lotSize)
		if err != nil {
			a.log.Errorf("error calculating cex VWAP: %v", err)
			return
		}
		if !cexFilled {
			break
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

		// Even if the average price ratio is > profit trigger, we still need
		// check if the current lot is profitable.
		var currLotProfitable bool
		if sellOnDEX {
			currLotProfitable = dexExtrema > cexExtrema
		} else {
			currLotProfitable = cexExtrema > dexExtrema
		}

		if priceRatio > (1+a.cfg.ProfitTrigger) && currLotProfitable {
			lotsToArb = numLots
			dexRate = dexExtrema
			cexRate = cexExtrema
		} else {
			break
		}
	}

	if lotsToArb > 0 {
		a.log.Infof("arb opportunity - sellOnDex: %v, lotsToArb: %v, dexRate: %v, cexRate: %v", sellOnDEX, lotsToArb, dexRate, cexRate)
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
	cexTradeID, err := a.cex.Trade(a.ctx, dex.BipIDSymbol(a.baseID), dex.BipIDSymbol(a.quoteID), !sellOnDex, cexRate, lotsToArb*a.mkt.LotSize, a.cexTradeUpdatesID)
	if err != nil {
		a.log.Errorf("error placing cex order: %v", err)
		return
	}

	var options map[string]string
	if sellOnDex {
		options = a.cfg.BaseOptions
	} else {
		options = a.cfg.QuoteOptions
	}

	dexOrders, err := a.core.MultiTrade(nil, &core.MultiTradeForm{
		Host:  a.host,
		Sell:  sellOnDex,
		Base:  a.baseID,
		Quote: a.quoteID,
		Placements: []*core.QtyRate{
			{
				Qty:  lotsToArb * a.mkt.LotSize,
				Rate: dexRate,
			},
		},
		Options: options,
	})
	if err != nil || len(dexOrders) != 1 {
		if err != nil {
			a.log.Errorf("error placing dex order: %v", err)
		}
		if len(dexOrders) != 1 {
			a.log.Errorf("expected 1 dex order, got %v", len(dexOrders))
		}

		err := a.cex.CancelTrade(a.ctx, dex.BipIDSymbol(a.baseID), dex.BipIDSymbol(a.quoteID), cexTradeID)
		if err != nil {
			a.log.Errorf("error canceling cex order: %v", err)
			// TODO: keep retrying failed cancel
		}
		return
	}

	a.activeArbs = append(a.activeArbs, &arbSequence{
		dexOrder:   dexOrders[0],
		dexRate:    dexRate,
		cexOrderID: cexTradeID,
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
		err := a.cex.CancelTrade(a.ctx, dex.BipIDSymbol(a.baseID), dex.BipIDSymbol(a.quoteID), arb.cexOrderID)
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
func (a *simpleArbMarketMaker) handleCEXTradeUpdate(update *libxc.TradeUpdate) {
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

func (m *simpleArbMarketMaker) handleNotification(note core.Notification) {
	switch n := note.(type) {
	case *core.OrderNote:
		ord := n.Order
		if ord == nil {
			return
		}
		m.handleDEXOrderUpdate(ord)
	}
}

func (a *simpleArbMarketMaker) run() {
	book, bookFeed, err := a.core.SyncBook(a.host, a.baseID, a.quoteID)
	if err != nil {
		a.log.Errorf("Failed to sync book: %v", err)
		return
	}
	a.book = book

	err = a.cex.SubscribeMarket(a.ctx, dex.BipIDSymbol(a.baseID), dex.BipIDSymbol(a.quoteID))
	if err != nil {
		a.log.Errorf("Failed to subscribe to cex market: %v", err)
		return
	}

	tradeUpdates, unsubscribe, tradeUpdatesID := a.cex.SubscribeTradeUpdates()
	defer unsubscribe()
	a.cexTradeUpdatesID = tradeUpdatesID

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

func (a *simpleArbMarketMaker) cancelAllOrders() {
	a.activeArbsMtx.Lock()
	defer a.activeArbsMtx.Unlock()

	for _, arb := range a.activeArbs {
		a.cancelArbSequence(arb)
	}
}

func RunSimpleArbBot(ctx context.Context, cfg *BotConfig, c clientCore, cex libxc.CEX, log dex.Logger) {
	if cfg.ArbCfg == nil {
		// implies bug in caller
		log.Errorf("No arb config provided. Exiting.")
		return
	}

	mkt, err := c.ExchangeMarket(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
	if err != nil {
		log.Errorf("Failed to get market: %v", err)
		return
	}

	(&simpleArbMarketMaker{
		ctx:        ctx,
		host:       cfg.Host,
		baseID:     cfg.BaseAsset,
		quoteID:    cfg.QuoteAsset,
		cex:        cex,
		core:       c,
		log:        log,
		cfg:        cfg.ArbCfg,
		mkt:        mkt,
		activeArbs: make([]*arbSequence, 0),
	}).run()
}
