package mm

import (
	"context"
	"fmt"
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
// The multiplier is important because it ensures that even some of the
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
	CEXName            string                      `json:"cexName"`
	BuyPlacements      []*ArbMarketMakingPlacement `json:"buyPlacements"`
	SellPlacements     []*ArbMarketMakingPlacement `json:"sellPlacements"`
	Profit             float64                     `json:"profit"`
	DriftTolerance     float64                     `json:"driftTolerance"`
	NumEpochsLeaveOpen uint64                      `json:"numEpochsLeaveOpen"`
	BaseOptions        map[string]string           `json:"baseOptions"`
	QuoteOptions       map[string]string           `json:"quoteOptions"`
}

type arbMarketMaker struct {
	ctx   context.Context
	host  string
	base  uint32
	quote uint32
	cex   libxc.CEX
	// cexTradeUpdatesID is passed to the Trade function of the cex
	// so that the cex knows to send update notifications for the
	// trade back to this bot.
	cexTradeUpdatesID int
	core              clientCore
	log               dex.Logger
	cfg               *ArbMarketMakerConfig
	mkt               *core.Market
	book              dexOrderBook
	rebalanceRunning  atomic.Bool
	currEpoch         atomic.Uint64

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
	if _, seen := a.matchesSeen[matchID]; seen {
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

	tradeID, err := a.cex.Trade(a.ctx, dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), !o.Sell, cexRate, match.Qty, a.cexTradeUpdatesID)
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
	if !found {
		a.ordMtx.RUnlock()
		return
	}
	a.ordMtx.RUnlock()

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
	}
}

func (a *arbMarketMaker) vwap(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	return a.cex.VWAP(dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), sell, qty)
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
		Base:       a.base,
		Quote:      a.quote,
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

func (a *arbMarketMaker) cancelCEXTrades() {
	currEpoch := a.currEpoch.Load()

	a.cexTradesMtx.RLock()
	defer a.cexTradesMtx.RUnlock()

	for tradeID, epoch := range a.cexTrades {
		if currEpoch-epoch >= a.cfg.NumEpochsLeaveOpen {
			err := a.cex.CancelTrade(a.ctx, dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote), tradeID)
			if err != nil {
				a.log.Errorf("Error canceling CEX trade %s: %v", tradeID, err)
			}
		}
	}
}

func arbMarketMakerRebalance(newEpoch uint64, a arbMMRebalancer, c clientCore, cex libxc.CEX, cfg *ArbMarketMakerConfig, mkt *core.Market, buyFees,
	sellFees *orderFees, log dex.Logger) (cancels []dex.Bytes, buyOrders, sellOrders []*rateLots) {

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

	baseCEXBalance, err := cex.Balance(mkt.BaseSymbol)
	if err != nil {
		log.Errorf("error getting base CEX balance: %v", err)
		return
	}

	quoteCEXBalance, err := cex.Balance(mkt.QuoteSymbol)
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

		// Enough balance on the CEX needs to maintained for counter-trades
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
				if requiredOnCEX < remainingCEXBalance {
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

	cancels, buyOrders, sellOrders := arbMarketMakerRebalance(epoch, a, a.core, a.cex, a.cfg, a.mkt, a.buyFees, a.sellFees, a.log)

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

	a.cancelCEXTrades()
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
		Base:          a.base,
		Quote:         a.quote,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
	})
	if err != nil {
		return fmt.Errorf("failed to get fees: %v", err)
	}

	sellSwapFees, sellRedeemFees, sellRefundFees, err := a.core.SingleLotFees(&core.SingleLotFeesForm{
		Host:          a.host,
		Base:          a.base,
		Quote:         a.quote,
		UseMaxFeeRate: true,
		UseSafeTxSize: true,
		Sell:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to get fees: %v", err)
	}

	buyFundingFees, err := a.core.MaxFundingFees(a.quote, a.host, uint32(len(a.cfg.BuyPlacements)), a.cfg.QuoteOptions)
	if err != nil {
		return fmt.Errorf("failed to get funding fees: %v", err)
	}

	sellFundingFees, err := a.core.MaxFundingFees(a.base, a.host, uint32(len(a.cfg.SellPlacements)), a.cfg.BaseOptions)
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
	book, bookFeed, err := a.core.SyncBook(a.host, a.base, a.quote)
	if err != nil {
		a.log.Errorf("Failed to sync book: %v", err)
		return
	}
	a.book = book

	a.updateFeeRates()

	err = a.cex.SubscribeMarket(a.ctx, dex.BipIDSymbol(a.base), dex.BipIDSymbol(a.quote))
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

func RunArbMarketMaker(ctx context.Context, cfg *BotConfig, c clientCore, cex libxc.CEX, log dex.Logger) {
	if cfg.ArbMarketMakerConfig == nil {
		// implies bug in caller
		log.Errorf("No arb market maker config provided. Exiting.")
		return
	}

	mkt, err := c.ExchangeMarket(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
	if err != nil {
		log.Errorf("Failed to get market: %v", err)
		return
	}

	(&arbMarketMaker{
		ctx:            ctx,
		host:           cfg.Host,
		base:           cfg.BaseAsset,
		quote:          cfg.QuoteAsset,
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
