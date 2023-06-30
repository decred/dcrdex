// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

type botBalance struct {
	mtx      sync.RWMutex
	balances map[uint32]uint64
}

// orderInfo stores the necessary information the balanceHandler needs for a
// particular order.
type orderInfo struct {
	bot                string
	order              *core.Order
	initialFundsLocked uint64
	lotSize            uint64
	// initialRedeemFeesLocked will be > 0 for assets that are account lockers
	// (ETH). This means that the redeem fees will be initially locked, then
	// the complete redeemed amount will be sent on redemption.
	initialRedeemFeesLocked   uint64
	initialRefundFeesLocked   uint64
	singleLotSwapFees         uint64
	singleLotRedeemFees       uint64
	unusedLockedFundsReturned bool
	excessFeesReturned        bool
	matchesRefunded           map[order.MatchID]struct{}
}

// finishedProcessing returns true when the balanceHandler no longer needs to
// track an order.
func (o *orderInfo) finishedProcessing() bool {
	if !o.unusedLockedFundsReturned || !o.excessFeesReturned {
		return false
	}

	for _, match := range o.order.Matches {
		var matchID order.MatchID
		copy(matchID[:], match.MatchID)
		if _, found := o.matchesRefunded[matchID]; !found {
			return false
		}
	}

	return true
}

// balanceHandler keeps track of the bot balances.
// When the balance handler is created, it will allocate the proper amount of
// funds for each bot. Then, as the bot makes trades, the balance handler will
// decrease and increase the bot's balances as needed.
// Below is a breakdown of how the botHandler increases and decreases a bot's
// balances during a trade.
//
// 1. A trade is made:
//
//   - FromAsset:
//     DECREASE: LockedFunds + FundingFees(i.e. SplitTx fees)
//     if isAccountLocker, RefundFeesLockedFunds
//
//   - ToAsset:
//     DECREASE: if isAccountLocker, RedeemFeesLockedFunds
//
// 2. MatchConfirmed:
//
//   - FromAsset:
//     INCREASE: RefundedAmount - RefundFees
//     if isAccountLocker, RefundFeesLockedFunds
//
//   - ToAsset:
//     INCREASE: if isAccountLocker, RedeemedAmount
//     else RedeemedAmount - MaxRedeemFeesForLotsRedeemed
//     (the redeem fees are tracked on the core.Order, so we
//     do not know the exact amount used for this match. The
//     difference will be returned to the bot when all fees
//     are confirmed.)
//
// 3. Match Refunded:
//
//   - FromAsset:
//     INCREASE: if isAccountLocker, RefundedAmount
//     else RefundedAmount - MaxRefundFees
//
// 4. Order Status > Booked:
//
//   - FromAsset:
//     INCREASE: OverLockedAmount (LockedFunds - FilledAmount - MaxSwapFees)
//
// 5. All Fees Confirmed:
//
//   - FromAsset:
//     INCREASE: ExcessSwapFees (MaxSwapFees - ActualSwapFees)
//     if isAccountLocker, ExcessRefundFees (RefundFeesLockedFunds - ActualRefundFees)
//     else ExcessRefundFees (MaxRefundFees - ActualRefundFees)
//
//   - ToAsset:
//     INCREASE: if isAccountLocker, ExcessRedeemFees (RedeemFeesLockedFunds - ActualRedeemFees)
//     else ExcessRedeemFees (MaxRedeemFeesForLotsRedeemed - ActualRedeemFees)
type balanceHandler struct {
	ctx         context.Context
	botBalances map[string]*botBalance
	core        clientCore
	log         dex.Logger

	mtx    sync.RWMutex
	orders map[order.OrderID]*orderInfo
}

// isAccountLocker returns if the asset is an account locker.
func (b *balanceHandler) isAccountLocker(assetID uint32) bool {
	walletState := b.core.WalletState(assetID)
	if walletState == nil {
		b.log.Errorf("isAccountLocker: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsAccountLocker()
}

// increaseBotBalance increases a bot's balance of an asset.
func (b *balanceHandler) increaseBotBalance(botID string, assetID uint32, amount uint64, oidB dex.Bytes) {
	b.log.Tracef("oid: %s, increase asset %d, amount %d", hex.EncodeToString(oidB), assetID, amount)

	bb := b.botBalances[botID]
	if bb == nil {
		b.log.Errorf("increaseBotBalance: bot %s not found", botID)
		return
	}

	bb.mtx.Lock()
	defer bb.mtx.Unlock()

	if _, found := bb.balances[assetID]; found {
		bb.balances[assetID] += amount
	} else {
		b.log.Errorf("increaseBotBalance: asset %d not found for bot %s", assetID, botID)
	}
}

// decreaseBotBalance decreases a bot's balance of an asset.
func (b *balanceHandler) decreaseBotBalance(botID string, assetID uint32, amount uint64, oidB dex.Bytes) {
	b.log.Tracef("oid: %s, decrease asset %d, amount %d", hex.EncodeToString(oidB), assetID, amount)

	bb := b.botBalances[botID]
	if bb == nil {
		b.log.Errorf("decreaseBalance: bot %s not found", botID)
		return
	}

	bb.mtx.Lock()
	defer bb.mtx.Unlock()

	if _, found := bb.balances[assetID]; found {
		if bb.balances[assetID] < amount {
			b.log.Errorf("decreaseBalance: bot %s has insufficient balance for asset %d. "+
				"balance: %d, amount: %d", botID, assetID, bb.balances[assetID], amount)
			bb.balances[assetID] = 0
			return
		}

		bb.balances[assetID] -= amount
	} else {
		b.log.Errorf("decreaseBalance: asset %d not found for bot %s", assetID, botID)
	}
}

// allBotBalances returns a map of each bot's balances for each asset.
func (b *balanceHandler) allBotBalances() map[string]map[uint32]uint64 {
	allBalances := make(map[string]map[uint32]uint64, len(b.botBalances))

	for botID, bb := range b.botBalances {
		balances := make(map[uint32]uint64, len(bb.balances))

		bb.mtx.RLock()
		for assetID, balance := range bb.balances {
			balances[assetID] = balance
		}
		bb.mtx.RUnlock()

		allBalances[botID] = balances
	}

	return allBalances
}

// botBalance returns a bot's balance of an asset.
func (b *balanceHandler) botBalance(botID string, assetID uint32) uint64 {
	bb := b.botBalances[botID]
	if bb == nil {
		b.log.Errorf("balance: bot %s not found", botID)
		return 0
	}

	bb.mtx.RLock()
	defer bb.mtx.RUnlock()

	if _, found := bb.balances[assetID]; found {
		return bb.balances[assetID]
	}

	b.log.Errorf("balance: asset %d not found for bot %s", assetID, botID)
	return 0
}

// wrappedCoreForBot returns a coreWithSegregatedBalance for the specified bot.
func (b *balanceHandler) wrappedCoreForBot(botID string) *coreWithSegregatedBalance {
	return &coreWithSegregatedBalance{
		// We embed core to avoid having to reimplement pass through methods,
		// but also have core as a field to avoid recursions.
		clientCore:     b.core,
		core:           b.core,
		balanceHandler: b,
		botID:          botID,
		log:            b.log,
	}
}

func (b *balanceHandler) getOrderInfo(id dex.Bytes) *orderInfo {
	var oid order.OrderID
	copy(oid[:], id)

	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.orders[oid]
}

func (b *balanceHandler) removeOrderInfo(id dex.Bytes) {
	b.log.Tracef("oid %s - finished handling", hex.EncodeToString(id))

	var oid order.OrderID
	copy(oid[:], id[:])

	b.mtx.Lock()
	defer b.mtx.Unlock()
	delete(b.orders, oid)
}

// handleMatchUpdate adds the redeem/refund amount to the bot's balance if the
// match is in the confirmed state.
func (b *balanceHandler) handleMatchUpdate(match *core.Match, oid dex.Bytes) {
	if match.Status != order.MatchConfirmed && match.Refund == nil {
		return
	}

	orderInfo := b.getOrderInfo(oid)
	if orderInfo == nil {
		b.log.Errorf("did not find order info for order %s", oid)
		return
	}

	var matchID order.MatchID
	copy(matchID[:], match.MatchID)
	if _, handled := orderInfo.matchesRefunded[matchID]; handled {
		return
	}

	orderInfo.matchesRefunded[matchID] = struct{}{}

	if match.Refund != nil {
		// TODO: Currently refunds are not handled properly. Core gives no way to
		// retrieve the refund fee. Core will need to make this information available,
		// and then the fee will need to be taken into account before increasing the
		// bot's balance.
		refundAsset := orderInfo.order.BaseID
		refundQty := match.Qty
		if !orderInfo.order.Sell {
			refundAsset = orderInfo.order.QuoteID
			refundQty = calc.BaseToQuote(match.Rate, refundQty)
		}
		b.log.Tracef("oid: %s, increasing balance due to refund")
		b.increaseBotBalance(orderInfo.bot, refundAsset, refundQty, oid)
	} else {
		redeemAsset := orderInfo.order.BaseID
		redeemQty := match.Qty
		if orderInfo.order.Sell {
			redeemAsset = orderInfo.order.QuoteID
			redeemQty = calc.BaseToQuote(match.Rate, redeemQty)
		}

		var maxRedeemFees uint64
		if orderInfo.initialRedeemFeesLocked == 0 {
			numLots := match.Qty / orderInfo.lotSize
			maxRedeemFees = numLots * orderInfo.singleLotRedeemFees
		}
		b.log.Tracef("oid: %s, increasing balance due to redeem, redeemQty - %v, maxRedeemFees - %v", oid, redeemQty, maxRedeemFees)
		b.increaseBotBalance(orderInfo.bot, redeemAsset, redeemQty-maxRedeemFees, oid)
	}

	if orderInfo.finishedProcessing() {
		b.removeOrderInfo(oid)
	}
}

// handleOrderNotification checks if any funds are ready to be made available
// for use by a bot depending on the order's state.
//   - If any matches are
//     in the confirmed state that have not yet been processed, the redeemed
//     or refunded amount is added to the bot's balance.
//   - If the order is no longer booked, the difference between the order's
//     quantity and the amount that was matched can be returned to the bot.
//   - If all fees have been confirmed, the rest of the difference between
//     the amount that was initially locked and the amount that was used
//     can be returned.
func (b *balanceHandler) handleOrderUpdate(o *core.Order) {
	orderInfo := b.getOrderInfo(o.ID)
	if orderInfo == nil {
		return
	}

	orderInfo.order = o

	// Step 2/3 (from balanceHandler doc): add redeem/refund amount to balance
	for _, match := range o.Matches {
		b.handleMatchUpdate(match, o.ID)
	}

	if o.Status <= order.OrderStatusBooked || // Not ready to process
		(orderInfo.unusedLockedFundsReturned && orderInfo.excessFeesReturned) || // Complete
		(orderInfo.unusedLockedFundsReturned && !orderInfo.excessFeesReturned && !o.AllFeesConfirmed) { // Step 1 complete, but not ready for step 2
		return
	}

	fromAsset, toAsset := o.BaseID, o.QuoteID
	if !o.Sell {
		fromAsset, toAsset = toAsset, fromAsset
	}

	var filledQty, filledLots uint64
	for _, match := range o.Matches {
		if match.IsCancel {
			continue
		}

		filledLots += match.Qty / orderInfo.lotSize
		if fromAsset == o.QuoteID {
			filledQty += calc.BaseToQuote(match.Rate, match.Qty)
		} else {
			filledQty += match.Qty
		}
	}

	// Step 4 (from balanceHandler doc): OrderStatus > Booked - return over locked amount
	if !orderInfo.unusedLockedFundsReturned {
		maxSwapFees := filledLots * orderInfo.singleLotSwapFees
		usedFunds := filledQty + maxSwapFees
		if usedFunds < orderInfo.initialFundsLocked {
			b.log.Tracef("oid: %s, returning unused locked funds, initialFundsLocked %v, filledQty %v, filledLots %v, maxSwapFees %v",
				o.ID, orderInfo.initialFundsLocked, filledQty, filledLots, maxSwapFees)
			b.increaseBotBalance(orderInfo.bot, fromAsset, orderInfo.initialFundsLocked-usedFunds, o.ID)
		} else {
			b.log.Errorf("oid: %v - usedFunds %d >= initialFundsLocked %d",
				hex.EncodeToString(o.ID), orderInfo.initialFundsLocked)
		}

		orderInfo.unusedLockedFundsReturned = true
	}

	// Step 5 (from balanceHandler doc): All Fees Confirmed - return excess swap and redeem fees
	if !orderInfo.excessFeesReturned && o.AllFeesConfirmed {
		// Return excess swap fees
		maxSwapFees := filledLots * orderInfo.singleLotSwapFees
		if maxSwapFees > o.FeesPaid.Swap {
			b.log.Tracef("oid: %s, return excess swap fees, maxSwapFees %v, swap fees %v", o.ID, maxSwapFees, o.FeesPaid.Swap)
			b.increaseBotBalance(orderInfo.bot, fromAsset, maxSwapFees-o.FeesPaid.Swap, o.ID)
		} else {
			b.log.Errorf("oid: %v - maxSwapFees %d > swap fees %d", hex.EncodeToString(o.ID), maxSwapFees, o.FeesPaid.Swap)
		}

		// Return excess redeem fees
		if orderInfo.initialRedeemFeesLocked > 0 { // AccountLocker
			if orderInfo.initialRedeemFeesLocked > o.FeesPaid.Redemption {
				b.log.Tracef("oid: %s, return excess redeem fees (accountLocker), initialRedeemFeesLocked %v, redemption fees %v",
					o.ID, orderInfo.initialRedeemFeesLocked, o.FeesPaid.Redemption)
				b.increaseBotBalance(orderInfo.bot, toAsset, orderInfo.initialRedeemFeesLocked-o.FeesPaid.Redemption, o.ID)
			} else {
				b.log.Errorf("oid: %v - initialRedeemFeesLocked %d > redemption fees %d",
					hex.EncodeToString(o.ID), orderInfo.initialRedeemFeesLocked, o.FeesPaid.Redemption)
			}
		} else {
			maxRedeemFees := filledLots * orderInfo.singleLotRedeemFees
			if maxRedeemFees > o.FeesPaid.Redemption {
				b.log.Tracef("oid: %s, return excess redeem fees, maxRedeemFees %v, redemption fees %v", o.ID, maxRedeemFees, o.FeesPaid.Redemption)
				b.increaseBotBalance(orderInfo.bot, toAsset, maxRedeemFees-o.FeesPaid.Redemption, o.ID)
			} else {
				b.log.Errorf("oid: %v - maxRedeemFees %d > redemption fees %d",
					hex.EncodeToString(o.ID), maxRedeemFees, o.FeesPaid.Redemption)
			}
		}

		orderInfo.excessFeesReturned = true
	}

	if orderInfo.finishedProcessing() {
		b.removeOrderInfo(o.ID)
	}
}

func (b *balanceHandler) handleNotification(n core.Notification) {
	switch note := n.(type) {
	case *core.OrderNote:
		b.handleOrderUpdate(note.Order)
	case *core.MatchNote:
		b.handleMatchUpdate(note.Match, note.OrderID)
	}
}

func (b *balanceHandler) run(ctx context.Context) {
	b.ctx = ctx

	feed := b.core.NotificationFeed()
	defer feed.ReturnFeed()

	for {
		select {
		case <-ctx.Done():
			return
		case n := <-feed.C:
			b.handleNotification(n)
		}
	}
}

func newBalanceHandler(cfgs []*BotConfig, core clientCore, log dex.Logger) (*balanceHandler, error) {
	bh := &balanceHandler{
		botBalances: make(map[string]*botBalance),
		orders:      make(map[order.OrderID]*orderInfo),
		core:        core,
		log:         log,
	}

	type trackedBalance struct {
		balanceAvailable uint64
		balanceReserved  uint64
	}

	balanceTracker := make(map[uint32]*trackedBalance)

	for _, cfg := range cfgs {
		if _, found := balanceTracker[cfg.BaseAsset]; !found {
			bal, err := core.AssetBalance(cfg.BaseAsset)
			if err != nil {
				return nil, fmt.Errorf("failed to get balance for asset %d: %v", cfg.BaseAsset, err)
			}
			balanceTracker[cfg.BaseAsset] = &trackedBalance{
				balanceAvailable: bal.Available,
			}
		}

		if _, found := balanceTracker[cfg.QuoteAsset]; !found {
			bal, err := core.AssetBalance(cfg.QuoteAsset)
			if err != nil {
				return nil, fmt.Errorf("failed to get balance for asset %d: %v", cfg.QuoteAsset, err)
			}
			balanceTracker[cfg.QuoteAsset] = &trackedBalance{
				balanceAvailable: bal.Available,
			}
		}

		baseBalance := balanceTracker[cfg.BaseAsset]
		quoteBalance := balanceTracker[cfg.QuoteAsset]

		var baseRequired, quoteRequired uint64
		if cfg.BaseBalanceType == Percentage {
			baseRequired = baseBalance.balanceAvailable * cfg.BaseBalance / 100
		} else {
			baseRequired = cfg.BaseBalance
		}

		if cfg.QuoteBalanceType == Percentage {
			quoteRequired = quoteBalance.balanceAvailable * cfg.QuoteBalance / 100
		} else {
			quoteRequired = cfg.QuoteBalance
		}

		if baseRequired == 0 && quoteRequired == 0 {
			return nil, fmt.Errorf("both base and quote balance are zero for market %s-%d-%d",
				cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		}

		if baseRequired > baseBalance.balanceAvailable-baseBalance.balanceReserved {
			return nil, fmt.Errorf("insufficient balance for asset %d", cfg.BaseAsset)
		}
		if quoteRequired > quoteBalance.balanceAvailable-quoteBalance.balanceReserved {
			return nil, fmt.Errorf("insufficient balance for asset %d", cfg.QuoteAsset)
		}

		baseBalance.balanceReserved += baseRequired
		quoteBalance.balanceReserved += quoteRequired

		mktID := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		bh.botBalances[mktID] = &botBalance{
			balances: map[uint32]uint64{
				cfg.BaseAsset:  baseRequired,
				cfg.QuoteAsset: quoteRequired,
			},
		}
	}

	return bh, nil
}

// coreWithSegregatedBalance implements the clientCore interface. A separate
// instance should be created for each bot, and the core functions will behave
// as if the entire balance of the wallet is the amount that has been reserved
// for the bot.
type coreWithSegregatedBalance struct {
	clientCore

	balanceHandler *balanceHandler
	botID          string
	core           clientCore
	log            dex.Logger
}

var _ clientCore = (*coreWithSegregatedBalance)(nil)

// Trade checks that the bot has enough balance for the trade, and if not,
// immediately returns an error. Otherwise, it forwards the call to the
// underlying core. Then, the bot's balance in the balance handler is
// updated to reflect the trade, and the balanceHandler will start tracking
// updates to the order to ensure that the bot's balance is updated.
func (c *coreWithSegregatedBalance) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	if !form.IsLimit {
		return nil, fmt.Errorf("only limit orders are supported")
	}

	enough, err := c.sufficientBalanceForTrade(form.Host, form.Base, form.Quote, form.Sell, form.Rate, form.Qty, form.Options)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	singleLotSwapFees, singleLotRedeemFees, err := c.core.SingleLotFees(&core.SingleLotFeesForm{
		Host:          form.Host,
		Base:          form.Base,
		Quote:         form.Quote,
		Sell:          form.Sell,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return nil, err
	}

	mkt, err := c.core.ExchangeMarket(form.Host, form.Base, form.Quote)
	if err != nil {
		return nil, err
	}

	o, err := c.core.Trade(pw, form)
	if err != nil {
		return nil, err
	}

	var orderID order.OrderID
	copy(orderID[:], o.ID)

	c.balanceHandler.mtx.Lock()
	c.balanceHandler.orders[orderID] = &orderInfo{
		bot:                     c.botID,
		order:                   o,
		initialFundsLocked:      o.LockedAmt,
		initialRedeemFeesLocked: o.RedeemLockedAmt,
		initialRefundFeesLocked: o.RefundLockedAmt,
		singleLotSwapFees:       singleLotSwapFees,
		singleLotRedeemFees:     singleLotRedeemFees,
		lotSize:                 mkt.LotSize,
		matchesRefunded:         make(map[order.MatchID]struct{}),
	}
	c.balanceHandler.mtx.Unlock()

	fromAsset, toAsset := form.Quote, form.Base
	if form.Sell {
		fromAsset, toAsset = toAsset, fromAsset
	}

	var fundingFees uint64
	if o.FeesPaid != nil {
		fundingFees = o.FeesPaid.Funding
	}

	c.balanceHandler.decreaseBotBalance(c.botID, fromAsset, o.LockedAmt+fundingFees, o.ID)
	if o.RedeemLockedAmt > 0 {
		c.balanceHandler.decreaseBotBalance(c.botID, toAsset, o.RedeemLockedAmt, o.ID)
	}

	return o, nil
}

func (c *coreWithSegregatedBalance) MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error) {
	enough, err := c.sufficientBalanceForTrades(form.Host, form.Base, form.Quote, form.Sell, form.Placements, form.Options)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	singleLotSwapFees, singleLotRedeemFees, err := c.core.SingleLotFees(&core.SingleLotFeesForm{
		Host:          form.Host,
		Base:          form.Base,
		Quote:         form.Quote,
		Sell:          form.Sell,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return nil, err
	}

	mkt, err := c.core.ExchangeMarket(form.Host, form.Base, form.Quote)
	if err != nil {
		return nil, err
	}

	fromAsset := form.Quote
	if form.Sell {
		fromAsset = form.Base
	}
	form.MaxLock = c.balanceHandler.botBalance(c.botID, fromAsset)

	orders, err := c.core.MultiTrade(pw, form)
	if err != nil {
		return nil, err
	}

	for _, o := range orders {
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		c.balanceHandler.mtx.Lock()
		c.balanceHandler.orders[orderID] = &orderInfo{
			bot:                     c.botID,
			order:                   o,
			initialFundsLocked:      o.LockedAmt,
			initialRedeemFeesLocked: o.RedeemLockedAmt,
			initialRefundFeesLocked: o.RefundLockedAmt,
			singleLotSwapFees:       singleLotSwapFees,
			singleLotRedeemFees:     singleLotRedeemFees,
			lotSize:                 mkt.LotSize,
			matchesRefunded:         make(map[order.MatchID]struct{}),
		}
		c.balanceHandler.mtx.Unlock()

		fromAsset, toAsset := form.Quote, form.Base
		if form.Sell {
			fromAsset, toAsset = toAsset, fromAsset
		}

		var fundingFees uint64
		if o.FeesPaid != nil {
			fundingFees = o.FeesPaid.Funding
		}

		c.balanceHandler.decreaseBotBalance(c.botID, fromAsset, o.LockedAmt+fundingFees, o.ID)
		if o.RedeemLockedAmt > 0 {
			c.balanceHandler.decreaseBotBalance(c.botID, toAsset, o.RedeemLockedAmt, o.ID)
		}
	}

	return orders, nil
}

func (c *coreWithSegregatedBalance) maxBuyQty(host string, base, quote uint32, rate uint64, options map[string]string) (uint64, error) {
	baseBalance := c.balanceHandler.botBalance(c.botID, base)
	quoteBalance := c.balanceHandler.botBalance(c.botID, quote)

	mkt, err := c.core.ExchangeMarket(host, base, quote)
	if err != nil {
		return 0, err
	}

	fundingFees, err := c.core.MaxFundingFees(quote, 1, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	if quoteBalance > fundingFees {
		quoteBalance -= fundingFees
	} else {
		quoteBalance = 0
	}

	lotSizeQuote := calc.BaseToQuote(rate, mkt.LotSize)
	maxLots := quoteBalance / (lotSizeQuote + swapFees)

	if redeemFees > 0 && c.balanceHandler.isAccountLocker(base) {
		maxBaseLots := baseBalance / redeemFees
		if maxLots > maxBaseLots {
			maxLots = maxBaseLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

// MayBuy returns the maximum quantity of the base asset that the bot can
// buy for rate using its balance of the quote asset.
func (c *coreWithSegregatedBalance) MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error) {
	maxQty, err := c.maxBuyQty(host, base, quote, rate, nil)
	if err != nil {
		return nil, err
	}

	if maxQty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := c.core.PreOrder(&core.TradeForm{
		Host:    host,
		IsLimit: true,
		Base:    base,
		Quote:   quote,
		Qty:     maxQty,
		Rate:    rate,
		// TODO: handle options. need new option for split if remaining balance < certain amount.
	})
	if err != nil {
		return nil, err
	}

	return &core.MaxOrderEstimate{
		Swap:   orderEstimate.Swap.Estimate,
		Redeem: orderEstimate.Redeem.Estimate,
	}, nil
}

func (c *coreWithSegregatedBalance) maxSellQty(host string, base, quote uint32, options map[string]string) (uint64, error) {
	baseBalance := c.balanceHandler.botBalance(c.botID, base)
	quoteBalance := c.balanceHandler.botBalance(c.botID, quote)

	mkt, err := c.core.ExchangeMarket(host, base, quote)
	if err != nil {
		return 0, err
	}

	fundingFees, err := c.core.MaxFundingFees(base, 1, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		Sell:          true,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	baseBalance -= fundingFees
	maxLots := baseBalance / (mkt.LotSize + swapFees)
	if c.balanceHandler.isAccountLocker(quote) && redeemFees > 0 {
		maxQuoteLots := quoteBalance / redeemFees
		if maxLots > maxQuoteLots {
			maxLots = maxQuoteLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

// MaxSell returned the maximum quantity of the base asset that the bot can
// sell.
func (c *coreWithSegregatedBalance) MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error) {
	qty, err := c.maxSellQty(host, base, quote, nil)
	if err != nil {
		return nil, err
	}
	if qty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := c.core.PreOrder(&core.TradeForm{
		Host:    host,
		IsLimit: true,
		Sell:    true,
		Base:    base,
		Quote:   quote,
		Qty:     qty,
	})
	if err != nil {
		return nil, err
	}

	return &core.MaxOrderEstimate{
		Swap:   orderEstimate.Swap.Estimate,
		Redeem: orderEstimate.Redeem.Estimate,
	}, nil
}

// AssetBalance returns the bot's balance for a specific asset.
func (c *coreWithSegregatedBalance) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	bal := c.balanceHandler.botBalance(c.botID, assetID)

	return &core.WalletBalance{
		Balance: &db.Balance{
			Balance: asset.Balance{
				Available: bal,
			},
		},
	}, nil
}

func (c *coreWithSegregatedBalance) sufficientBalanceForTrade(host string, base, quote uint32, sell bool, rate, qty uint64, options map[string]string) (bool, error) {
	var maxQty uint64
	var err error
	if sell {
		maxQty, err = c.maxSellQty(host, base, quote, options)
		if err != nil {
			return false, err
		}
	} else {
		maxQty, err = c.maxBuyQty(host, base, quote, rate, options)
		if err != nil {
			return false, err
		}
	}

	return maxQty >= qty, nil
}

func (c *coreWithSegregatedBalance) sufficientBalanceForTrades(host string, base, quote uint32, sell bool, placements []*core.QtyRate, options map[string]string) (bool, error) {
	if sell {
		var totalQty uint64
		for _, placement := range placements {
			totalQty += placement.Qty
		}
		maxQty, err := c.maxSellQty(host, base, quote, options)
		if err != nil {
			return false, err
		}
		return maxQty >= totalQty, nil
	}

	baseBalance := c.balanceHandler.botBalance(c.botID, base)
	quoteBalance := c.balanceHandler.botBalance(c.botID, quote)

	mkt, err := c.core.ExchangeMarket(host, base, quote)
	if err != nil {
		return false, err
	}

	swapFees, redeemFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return false, err
	}

	fundingFees, err := c.core.MaxFundingFees(base, uint32(len(placements)), options)
	if err != nil {
		return false, err
	}

	var totalLots uint64
	remainingBalance := quoteBalance - fundingFees
	for _, placement := range placements {
		quoteQty := calc.BaseToQuote(placement.Rate, placement.Qty)
		numLots := placement.Qty / mkt.LotSize
		totalLots += numLots
		req := quoteQty + (numLots * swapFees)
		if remainingBalance < req {
			return false, nil
		}
		remainingBalance -= req
	}

	if c.balanceHandler.isAccountLocker(base) && baseBalance < redeemFees*totalLots {
		return false, nil
	}

	return true, nil
}

// PreOrder checks if the bot's balance is sufficient for the trade, and if it
// is, forwards the request to the underlying core.
func (c *coreWithSegregatedBalance) PreOrder(form *core.TradeForm) (*core.OrderEstimate, error) {
	enough, err := c.sufficientBalanceForTrade(form.Host, form.Base, form.Quote, form.Sell, form.Rate, form.Qty, form.Options)
	if err != nil {
		return nil, err
	}

	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	return c.core.PreOrder(form)
}
