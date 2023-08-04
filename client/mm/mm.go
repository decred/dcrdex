// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	NotificationFeed() *core.NoteFeed
	ExchangeMarket(host string, base, quote uint32) (*core.Market, error)
	SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, error)
	Cancel(oidB dex.Bytes) error
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	PreOrder(form *core.TradeForm) (*core.OrderEstimate, error)
	WalletState(assetID uint32) *core.WalletState
	MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error)
	MaxFundingFees(fromAsset uint32, numTrades uint32, options map[string]string) (uint64, error)
	User() *core.User
	Login(pw []byte) error
	OpenWallet(assetID uint32, appPW []byte) error
}

var _ clientCore = (*core.Core)(nil)

// dexOrderBook is satisfied by orderbook.OrderBook.
// Avoids having to mock the entire orderbook in tests.
type dexOrderBook interface {
	MidGap() (uint64, error)
}

var _ dexOrderBook = (*orderbook.OrderBook)(nil)

// botBalance keeps track of the amount of funds available for a
// bot's use, and the amount that is currently locked/pending for
// various reasons. Only the Available balance matters for the
// behavior of the bots. The others are just tracked to inform the
// user.
type botBalance struct {
	Available     uint64 `json:"available"`
	FundingOrder  uint64 `json:"fundingOrder"`
	PendingRedeem uint64 `json:"pendingRedeem"`
	PendingRefund uint64 `json:"pendingRefund"`
}

// botBalance keeps track of the bot balances.
// When the MarketMaker is created, it will allocate the proper amount of
// funds for each bot. Then, as the bot makes trades, each bot's balances
// will be increased and decreased as needed.
// Below is of how the balances are adjusted during trading.
//
// 1. A trade is made:
//
//   - FromAsset:
//     DECREASE: LockedFunds + FundingFees
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
//     (the redeemed amount is tracked on the core.Order, so we
//     do not know the exact amount used for this match. The
//     difference is handled later.)
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
type botBalances struct {
	mtx      sync.RWMutex
	balances map[uint32]*botBalance
}

// orderInfo stores the necessary information the MarketMaker needs for a
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
	matchesSeen               map[order.MatchID]struct{}
	matchesSettled            map[order.MatchID]struct{}
}

// finishedProcessing returns true when the MarketMaker no longer needs to
// track an order.
func (o *orderInfo) finishedProcessing() bool {
	if !o.unusedLockedFundsReturned || !o.excessFeesReturned {
		return false
	}

	for _, match := range o.order.Matches {
		var matchID order.MatchID
		copy(matchID[:], match.MatchID)
		if _, found := o.matchesSettled[matchID]; !found {
			return false
		}
	}

	return true
}

// MarketMaker handles the market making process. It supports running different
// strategies on different markets.
type MarketMaker struct {
	ctx                   context.Context
	die                   context.CancelFunc
	running               atomic.Bool
	log                   dex.Logger
	core                  clientCore
	doNotKillWhenBotsStop bool // used for testing
	botBalances           map[string]*botBalances

	ordersMtx sync.RWMutex
	orders    map[order.OrderID]*orderInfo
}

// NewMarketMaker creates a new MarketMaker.
func NewMarketMaker(c clientCore, log dex.Logger) (*MarketMaker, error) {
	return &MarketMaker{
		core:    c,
		log:     log,
		running: atomic.Bool{},
		orders:  make(map[order.OrderID]*orderInfo),
	}, nil
}

// Running returns true if the MarketMaker is running.
func (m *MarketMaker) Running() bool {
	return m.running.Load()
}

func marketsRequiringPriceOracle(cfgs []*BotConfig) []*mkt {
	mkts := make([]*mkt, 0, len(cfgs))

	for _, cfg := range cfgs {
		if cfg.requiresPriceOracle() {
			mkts = append(mkts, &mkt{base: cfg.BaseAsset, quote: cfg.QuoteAsset})
		}
	}

	return mkts
}

// duplicateBotConfig returns an error if there is more than one bot config for
// the same market on the same dex host.
func duplicateBotConfig(cfgs []*BotConfig) error {
	mkts := make(map[string]struct{})

	for _, cfg := range cfgs {
		mkt := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		if _, found := mkts[mkt]; found {
			return fmt.Errorf("duplicate bot config for market %s", mkt)
		}
		mkts[mkt] = struct{}{}
	}

	return nil
}

func priceOracleFromConfigs(ctx context.Context, cfgs []*BotConfig, log dex.Logger) (*priceOracle, error) {
	var oracle *priceOracle
	var err error
	marketsRequiringOracle := marketsRequiringPriceOracle(cfgs)
	if len(marketsRequiringOracle) > 0 {
		oracle, err = newPriceOracle(ctx, marketsRequiringOracle, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create PriceOracle: %v", err)
		}
	}

	return oracle, nil
}

func (m *MarketMaker) loginAndUnlockWallets(pw []byte, cfgs []*BotConfig) error {
	err := m.core.Login(pw)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}
	unlocked := make(map[uint32]interface{})
	for _, cfg := range cfgs {
		if _, done := unlocked[cfg.BaseAsset]; !done {
			err := m.core.OpenWallet(cfg.BaseAsset, pw)
			if err != nil {
				return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.BaseAsset, err)
			}
			unlocked[cfg.BaseAsset] = true
		}

		if _, done := unlocked[cfg.QuoteAsset]; !done {
			err := m.core.OpenWallet(cfg.QuoteAsset, pw)
			if err != nil {
				return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.QuoteAsset, err)
			}
			unlocked[cfg.QuoteAsset] = true
		}
	}

	return nil
}

func validateAndFilterEnabledConfigs(cfgs []*BotConfig) ([]*BotConfig, error) {
	enabledCfgs := make([]*BotConfig, 0, len(cfgs))
	for _, cfg := range cfgs {
		if !cfg.Disabled {
			enabledCfgs = append(enabledCfgs, cfg)
		}
	}
	if len(enabledCfgs) == 0 {
		return nil, errors.New("no enabled bots")
	}
	if err := duplicateBotConfig(enabledCfgs); err != nil {
		return nil, err
	}
	return enabledCfgs, nil
}

// setupBalances makes sure there is sufficient balance to cover all the bots,
// and populates the botBalances map.
func (m *MarketMaker) setupBalances(cfgs []*BotConfig) error {
	m.botBalances = make(map[string]*botBalances, len(cfgs))

	type trackedBalance struct {
		balanceAvailable uint64
		balanceReserved  uint64
	}

	balanceTracker := make(map[uint32]*trackedBalance)
	trackAsset := func(assetID uint32) error {
		if _, found := balanceTracker[assetID]; found {
			return nil
		}
		bal, err := m.core.AssetBalance(assetID)
		if err != nil {
			return fmt.Errorf("failed to get balance for asset %d: %v", assetID, err)
		}
		balanceTracker[assetID] = &trackedBalance{
			balanceAvailable: bal.Available,
		}
		return nil
	}

	for _, cfg := range cfgs {
		err := trackAsset(cfg.BaseAsset)
		if err != nil {
			return err
		}
		err = trackAsset(cfg.QuoteAsset)
		if err != nil {
			return err
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
			return fmt.Errorf("both base and quote balance are zero for market %s-%d-%d",
				cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		}

		if baseRequired > baseBalance.balanceAvailable-baseBalance.balanceReserved {
			return fmt.Errorf("insufficient balance for asset %d", cfg.BaseAsset)
		}
		if quoteRequired > quoteBalance.balanceAvailable-quoteBalance.balanceReserved {
			return fmt.Errorf("insufficient balance for asset %d", cfg.QuoteAsset)
		}

		baseBalance.balanceReserved += baseRequired
		quoteBalance.balanceReserved += quoteRequired

		mktID := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		m.botBalances[mktID] = &botBalances{
			balances: map[uint32]*botBalance{
				cfg.BaseAsset: {
					Available: baseRequired,
				},
				cfg.QuoteAsset: {
					Available: quoteRequired,
				},
			},
		}
	}

	return nil
}

// isAccountLocker returns if the asset is an account locker.
func (m *MarketMaker) isAccountLocker(assetID uint32) bool {
	walletState := m.core.WalletState(assetID)
	if walletState == nil {
		m.log.Errorf("isAccountLocker: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsAccountLocker()
}

type botBalanceType uint8

const (
	balTypeAvailable botBalanceType = iota
	balTypeFundingOrder
	balTypePendingRedeem
	balTypePendingRefund
)

const (
	balanceModIncrease = true
	balanceModDecrease = false
)

// balanceMod is passed to modifyBotBalance to increase or decrease one
// of the bot's balances for an asset.
type balanceMod struct {
	increase bool
	assetID  uint32
	typ      botBalanceType
	amount   uint64
}

// modifyBotBalance does modifications to the various bot balances.
func (m *MarketMaker) modifyBotBalance(botID string, mods []*balanceMod) {
	bb := m.botBalances[botID]
	if bb == nil {
		m.log.Errorf("increaseBotBalance: bot %s not found", botID)
		return
	}

	bb.mtx.Lock()
	defer bb.mtx.Unlock()

	for _, mod := range mods {
		assetBalance, found := bb.balances[mod.assetID]
		if !found {
			m.log.Errorf("modifyBotBalance: asset %d not found for bot %s", mod.assetID, botID)
			continue
		}

		newFieldValue := func(balanceType string, initialValue uint64) uint64 {
			if mod.increase {
				return initialValue + mod.amount
			} else {
				if assetBalance.Available < mod.amount {
					m.log.Errorf("modifyBotBalance: bot %s has insufficient %s for asset %d. "+
						"balance: %d, amount: %d", botID, balanceType, mod.assetID, initialValue, mod.amount)
					return 0
				}
				return initialValue - mod.amount
			}
		}

		switch mod.typ {
		case balTypeAvailable:
			assetBalance.Available = newFieldValue("available balance", assetBalance.Available)
		case balTypeFundingOrder:
			assetBalance.FundingOrder = newFieldValue("funding order", assetBalance.FundingOrder)
		case balTypePendingRedeem:
			assetBalance.PendingRedeem = newFieldValue("pending redeem", assetBalance.PendingRedeem)
		case balTypePendingRefund:
			assetBalance.PendingRefund = newFieldValue("pending refund", assetBalance.PendingRefund)
		}
	}
}

// botBalance returns a bot's balance of an asset.
func (m *MarketMaker) botBalance(botID string, assetID uint32) uint64 {
	bb := m.botBalances[botID]
	if bb == nil {
		m.log.Errorf("balance: bot %s not found", botID)
		return 0
	}

	bb.mtx.RLock()
	defer bb.mtx.RUnlock()

	if _, found := bb.balances[assetID]; found {
		return bb.balances[assetID].Available
	}

	m.log.Errorf("balance: asset %d not found for bot %s", assetID, botID)
	return 0
}

func (m *MarketMaker) getOrderInfo(id dex.Bytes) *orderInfo {
	var oid order.OrderID
	copy(oid[:], id)

	m.ordersMtx.RLock()
	defer m.ordersMtx.RUnlock()
	return m.orders[oid]
}

func (m *MarketMaker) removeOrderInfo(id dex.Bytes) {
	m.log.Tracef("oid %s - finished handling", hex.EncodeToString(id))

	var oid order.OrderID
	copy(oid[:], id[:])

	m.ordersMtx.Lock()
	defer m.ordersMtx.Unlock()
	delete(m.orders, oid)
}

// handleMatchUpdate adds the redeem/refund amount to the bot's balance if the
// match is in the confirmed state.
func (m *MarketMaker) handleMatchUpdate(match *core.Match, oid dex.Bytes) {
	var matchID order.MatchID
	copy(matchID[:], match.MatchID)

	orderInfo := m.getOrderInfo(oid)
	if orderInfo == nil {
		m.log.Errorf("did not find order info for order %s", oid)
		return
	}

	if _, seen := orderInfo.matchesSeen[matchID]; !seen {
		orderInfo.matchesSeen[matchID] = struct{}{}

		var maxRedeemFees uint64
		if orderInfo.initialRedeemFeesLocked == 0 {
			numLots := match.Qty / orderInfo.lotSize
			maxRedeemFees = numLots * orderInfo.singleLotRedeemFees
		}

		var balanceMods []*balanceMod
		if orderInfo.order.Sell {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.BaseID, balTypeFundingOrder, match.Qty},
				{balanceModIncrease, orderInfo.order.QuoteID, balTypePendingRedeem, calc.BaseToQuote(match.Rate, match.Qty) - maxRedeemFees},
			}
		} else {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.QuoteID, balTypeFundingOrder, calc.BaseToQuote(match.Rate, match.Qty)},
				{balanceModIncrease, orderInfo.order.BaseID, balTypePendingRedeem, match.Qty - maxRedeemFees},
			}
		}

		m.modifyBotBalance(orderInfo.bot, balanceMods)
	}

	if match.Status != order.MatchConfirmed && match.Refund == nil {
		return
	}

	if _, handled := orderInfo.matchesSettled[matchID]; handled {
		return
	}

	orderInfo.matchesSettled[matchID] = struct{}{}

	if match.Refund != nil {
		// TODO: Currently refunds are not handled properly. Core gives no way to
		// retrieve the refund fee. Core will need to make this information available,
		// and then the fee will need to be taken into account before increasing the
		// bot's balance. Also, currently we are not detecting that a refund will happen,
		// only that it has already happened. When a match has been revoked, the bot's
		// PendingRefund balance must be increased, and the PendingRedeem amount must be
		// decreased.

		var maxRedeemFees uint64
		if orderInfo.initialRedeemFeesLocked == 0 {
			numLots := match.Qty / orderInfo.lotSize
			maxRedeemFees = numLots * orderInfo.singleLotRedeemFees
		}

		var balanceMods []*balanceMod
		if orderInfo.order.Sell {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.QuoteID, balTypePendingRedeem, calc.BaseToQuote(match.Rate, match.Qty) - maxRedeemFees},
				{balanceModIncrease, orderInfo.order.BaseID, balTypeAvailable, match.Qty},
			}
		} else {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.BaseID, balTypePendingRedeem, match.Qty - maxRedeemFees},
				{balanceModIncrease, orderInfo.order.QuoteID, balTypeAvailable, calc.BaseToQuote(match.Rate, match.Qty)},
			}
		}
		m.log.Tracef("oid: %s, increasing balance due to refund")
		m.modifyBotBalance(orderInfo.bot, balanceMods)
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
		m.log.Tracef("oid: %s, increasing balance due to redeem, redeemQty - %v, maxRedeemFees - %v", oid, redeemQty, maxRedeemFees)

		balanceMods := []*balanceMod{
			{balanceModDecrease, redeemAsset, balTypePendingRedeem, redeemQty - maxRedeemFees},
			{balanceModIncrease, redeemAsset, balTypeAvailable, redeemQty - maxRedeemFees},
		}
		m.modifyBotBalance(orderInfo.bot, balanceMods)
	}

	if orderInfo.finishedProcessing() {
		m.removeOrderInfo(oid)
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
//     the amount the at was initially locked and the amount that was used
//     can be returned.
func (m *MarketMaker) handleOrderUpdate(o *core.Order) {
	orderInfo := m.getOrderInfo(o.ID)
	if orderInfo == nil {
		return
	}

	orderInfo.order = o

	// Step 2/3 (from botBalance doc): add redeem/refund amount to balance
	for _, match := range o.Matches {
		m.handleMatchUpdate(match, o.ID)
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

	// Step 4 (from botBalance doc): OrderStatus > Booked - return over locked amount
	if !orderInfo.unusedLockedFundsReturned {
		maxSwapFees := filledLots * orderInfo.singleLotSwapFees
		usedFunds := filledQty + maxSwapFees
		if usedFunds < orderInfo.initialFundsLocked {
			m.log.Tracef("oid: %s, returning unused locked funds, initialFundsLocked %v, filledQty %v, filledLots %v, maxSwapFees %v",
				o.ID, orderInfo.initialFundsLocked, filledQty, filledLots, maxSwapFees)

			balanceMods := []*balanceMod{
				{balanceModIncrease, fromAsset, balTypeAvailable, orderInfo.initialFundsLocked - usedFunds},
				{balanceModDecrease, fromAsset, balTypeFundingOrder, orderInfo.initialFundsLocked - usedFunds},
			}
			m.modifyBotBalance(orderInfo.bot, balanceMods)
		} else {
			m.log.Errorf("oid: %v - usedFunds %d >= initialFundsLocked %d",
				hex.EncodeToString(o.ID), orderInfo.initialFundsLocked)
		}

		orderInfo.unusedLockedFundsReturned = true
	}

	// Step 5 (from botBalance doc): All Fees Confirmed - return excess swap and redeem fees
	if !orderInfo.excessFeesReturned && o.AllFeesConfirmed {
		// Return excess swap fees
		maxSwapFees := filledLots * orderInfo.singleLotSwapFees
		if maxSwapFees > o.FeesPaid.Swap {
			m.log.Tracef("oid: %s, return excess swap fees, maxSwapFees %v, swap fees %v", o.ID, maxSwapFees, o.FeesPaid.Swap)
			balanceMods := []*balanceMod{
				{balanceModIncrease, fromAsset, balTypeAvailable, maxSwapFees - o.FeesPaid.Swap},
				{balanceModDecrease, fromAsset, balTypeFundingOrder, maxSwapFees},
			}
			m.modifyBotBalance(orderInfo.bot, balanceMods)
		} else if maxSwapFees < o.FeesPaid.Swap {
			m.log.Errorf("oid: %v - maxSwapFees %d < swap fees %d", hex.EncodeToString(o.ID), maxSwapFees, o.FeesPaid.Swap)
		}

		// Return excess redeem fees
		if orderInfo.initialRedeemFeesLocked > 0 { // AccountLocker
			if orderInfo.initialRedeemFeesLocked > o.FeesPaid.Redemption {
				m.log.Tracef("oid: %s, return excess redeem fees (accountLocker), initialRedeemFeesLocked %v, redemption fees %v",
					o.ID, orderInfo.initialRedeemFeesLocked, o.FeesPaid.Redemption)
				balanceMods := []*balanceMod{
					{balanceModIncrease, toAsset, balTypeAvailable, orderInfo.initialRedeemFeesLocked - o.FeesPaid.Redemption},
					{balanceModDecrease, toAsset, balTypeFundingOrder, orderInfo.initialRedeemFeesLocked},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			} else {
				m.log.Errorf("oid: %v - initialRedeemFeesLocked %d > redemption fees %d",
					hex.EncodeToString(o.ID), orderInfo.initialRedeemFeesLocked, o.FeesPaid.Redemption)
			}
		} else {
			maxRedeemFees := filledLots * orderInfo.singleLotRedeemFees
			if maxRedeemFees > o.FeesPaid.Redemption {
				m.log.Tracef("oid: %s, return excess redeem fees, maxRedeemFees %v, redemption fees %v", o.ID, maxRedeemFees, o.FeesPaid.Redemption)
				balanceMods := []*balanceMod{
					{balanceModIncrease, toAsset, balTypeAvailable, maxRedeemFees - o.FeesPaid.Redemption},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			} else if maxRedeemFees < o.FeesPaid.Redemption {
				m.log.Errorf("oid: %v - maxRedeemFees %d < redemption fees %d",
					hex.EncodeToString(o.ID), maxRedeemFees, o.FeesPaid.Redemption)
			}
		}

		orderInfo.excessFeesReturned = true
	}

	if orderInfo.finishedProcessing() {
		m.removeOrderInfo(o.ID)
	}
}

func (m *MarketMaker) handleNotification(n core.Notification) {
	switch note := n.(type) {
	case *core.OrderNote:
		m.handleOrderUpdate(note.Order)
	case *core.MatchNote:
		m.handleMatchUpdate(note.Match, note.OrderID)
	}
}

// Run starts the MarketMaker. There can only be one BotConfig per dex market.
func (m *MarketMaker) Run(ctx context.Context, cfgs []*BotConfig, pw []byte) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("market making is already running")
	}

	var startedMarketMaking bool
	defer func() {
		if !startedMarketMaking {
			m.running.Store(false)
		}
	}()

	m.ctx, m.die = context.WithCancel(ctx)

	enabledCfgs, err := validateAndFilterEnabledConfigs(cfgs)
	if err != nil {
		return err
	}

	if err := m.loginAndUnlockWallets(pw, enabledCfgs); err != nil {
		return err
	}

	oracle, err := priceOracleFromConfigs(m.ctx, enabledCfgs, m.log.SubLogger("PriceOracle"))
	if err != nil {
		return err
	}

	if err := m.setupBalances(enabledCfgs); err != nil {
		return err
	}

	startedMarketMaking = true

	wg := new(sync.WaitGroup)

	// Listen for core notifications.
	wg.Add(1)
	go func() {
		defer wg.Done()
		feed := m.core.NotificationFeed()
		defer feed.ReturnFeed()

		for {
			select {
			case <-m.ctx.Done():
				return
			case n := <-feed.C:
				m.handleNotification(n)
			}
		}
	}()

	// Start each bot.
	for _, cfg := range enabledCfgs {
		switch {
		case cfg.MMCfg != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				logger := m.log.SubLogger(fmt.Sprintf("MarketMaker-%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset))
				mktID := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
				RunBasicMarketMaker(m.ctx, cfg, m.wrappedCoreForBot(mktID), oracle, pw, logger)
				wg.Done()
			}(cfg)
		default:
			m.log.Errorf("Only basic market making is supported at this time. Skipping %s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		}
	}

	go func() {
		wg.Wait()
		m.log.Infof("All bots have stopped running.")
		m.running.Store(false)
	}()

	return nil
}

// Stop stops the MarketMaker.
func (m *MarketMaker) Stop() {
	if m.die != nil {
		m.die()
	}
}
