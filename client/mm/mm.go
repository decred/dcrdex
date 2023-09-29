// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
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
	SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, uint64, error)
	Cancel(oidB dex.Bytes) error
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	PreOrder(form *core.TradeForm) (*core.OrderEstimate, error)
	WalletState(assetID uint32) *core.WalletState
	MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error)
	MaxFundingFees(fromAsset uint32, host string, numTrades uint32, fromSettings map[string]string) (uint64, error)
	User() *core.User
	Login(pw []byte) error
	OpenWallet(assetID uint32, appPW []byte) error
	Broadcast(core.Notification)
	FiatConversionRates() map[uint32]float64
}

var _ clientCore = (*core.Core)(nil)

// dexOrderBook is satisfied by orderbook.OrderBook.
// Avoids having to mock the entire orderbook in tests.
type dexOrderBook interface {
	MidGap() (uint64, error)
	VWAP(lots, lotSize uint64, sell bool) (avg, extrema uint64, filled bool, err error)
}

var _ dexOrderBook = (*orderbook.OrderBook)(nil)

// botBalance keeps track of the amount of funds available for a
// bot's use, and the amount that is currently locked/pending redemption. Only
// the Available balance matters for the behavior of the bots. The others are
// just tracked to inform the user.
type botBalance struct {
	Available     uint64 `json:"available"`
	FundingOrder  uint64 `json:"fundingOrder"`
	PendingRedeem uint64 `json:"pendingRedeem"`
}

// botBalance keeps track of the bot balances.
// When the MarketMaker is created, it will allocate the proper amount of
// funds for each bot. Then, as the bot makes trades, each bot's balances
// will be increased and decreased as needed.
// Below is of how the balances are adjusted during trading. This only
// outlines the changes to the Available balance.
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
// 2a. MatchConfirmed, Redeemed:
//   - ToAsset:
//     INCREASE: if isAccountLocker, RedeemedAmount
//     else RedeemedAmount - MaxRedeemFeesForLotsRedeemed
//     (the redeemed amount is tracked on the core.Order, so we
//     do not know the exact amount used for this match. The
//     difference is handled later.)
//
// 2b. MatchConfirmed, Refunded:
//   - FromAsset:
//     INCREASE: RefundedAmount - RefundFees
//     if isAccountLocker, RefundFeesLockedFunds
//
// 4. order.LockedAmount == 0: (This means no more swap tx will be made, over lock can be returned)
//
//   - FromAsset:
//     INCREASE: OverLockedAmount (LockedFunds - SwappedAmount - MaxSwapFees)
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
	singleLotRefundFees       uint64
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
	dir                   string
	core                  clientCore
	doNotKillWhenBotsStop bool // used for testing
	botBalances           map[string]*botBalances
	cfgPath               string
	// syncedOracle is only available while the MarketMaker is running. It
	// periodically refreshes the prices for the markets that have bots
	// running on them.
	syncedOracleMtx sync.RWMutex
	syncedOracle    *priceOracle

	// unsyncedOracle is always available and can be used to query prices on
	// all markets. It does not periodically refresh the prices, and queries
	// them on demand.
	unsyncedOracle *priceOracle

	runningBotsMtx sync.RWMutex
	runningBots    map[MarketWithHost]interface{}

	ordersMtx sync.RWMutex
	orders    map[order.OrderID]*orderInfo
}

// NewMarketMaker creates a new MarketMaker.
func NewMarketMaker(c clientCore, cfgPath string, log dex.Logger) (*MarketMaker, error) {
	if _, err := os.Stat(cfgPath); err != nil {
		cfg := new(MarketMakingConfig)
		cfgB, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal empty config file: %w", err)
		}
		err = os.WriteFile(cfgPath, cfgB, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to write empty config file: %w", err)
		}
	}

	return &MarketMaker{
		core:           c,
		log:            log,
		cfgPath:        cfgPath,
		running:        atomic.Bool{},
		orders:         make(map[order.OrderID]*orderInfo),
		runningBots:    make(map[MarketWithHost]interface{}),
		unsyncedOracle: newUnsyncedPriceOracle(log),
	}, nil
}

// Running returns true if the MarketMaker is running.
func (m *MarketMaker) Running() bool {
	return m.running.Load()
}

// MarketWithHost represents a market on a specific dex server.
type MarketWithHost struct {
	Host    string `json:"host"`
	BaseID  uint32 `json:"base"`
	QuoteID uint32 `json:"quote"`
}

func (m *MarketWithHost) String() string {
	return fmt.Sprintf("%s-%d-%d", m.Host, m.BaseID, m.QuoteID)
}

// RunningBots returns the markets on which a bot is running.
func (m *MarketMaker) RunningBots() []MarketWithHost {
	m.runningBotsMtx.RLock()
	defer m.runningBotsMtx.RUnlock()

	mkts := make([]MarketWithHost, 0, len(m.runningBots))
	for mkt := range m.runningBots {
		mkts = append(mkts, mkt)
	}

	return mkts
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
		oracle, err = newAutoSyncPriceOracle(ctx, marketsRequiringOracle, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create PriceOracle: %v", err)
		}
	}

	return oracle, nil
}

func (m *MarketMaker) markBotAsRunning(mkt MarketWithHost, running bool) {
	m.runningBotsMtx.Lock()
	defer m.runningBotsMtx.Unlock()
	if running {
		m.runningBots[mkt] = struct{}{}
	} else {
		delete(m.runningBots, mkt)
	}

	if len(m.runningBots) == 0 {
		m.die()
	}
}

// MarketReport returns information about the oracle rates on a market
// pair and the fiat rates of the base and quote assets.
func (m *MarketMaker) MarketReport(base, quote uint32) (*MarketReport, error) {
	fiatRates := m.core.FiatConversionRates()
	baseFiatRate := fiatRates[base]
	quoteFiatRate := fiatRates[quote]

	m.syncedOracleMtx.RLock()
	if m.syncedOracle != nil {
		price, oracles, err := m.syncedOracle.getOracleInfo(base, quote)
		if err != nil && !errors.Is(err, errUnsyncedMarket) {
			m.log.Errorf("failed to get oracle info for market %d-%d: %v", base, quote, err)
		}
		if err == nil {
			m.syncedOracleMtx.RUnlock()
			return &MarketReport{
				Price:         price,
				Oracles:       oracles,
				BaseFiatRate:  baseFiatRate,
				QuoteFiatRate: quoteFiatRate,
			}, nil
		}
	}
	m.syncedOracleMtx.RUnlock()

	price, oracles, err := m.unsyncedOracle.getOracleInfo(base, quote)
	if err != nil {
		return nil, err
	}

	return &MarketReport{
		Price:         price,
		Oracles:       oracles,
		BaseFiatRate:  baseFiatRate,
		QuoteFiatRate: quoteFiatRate,
	}, nil
}

func (m *MarketMaker) loginAndUnlockWallets(pw []byte, cfgs []*BotConfig) error {
	err := m.core.Login(pw)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}
	unlocked := make(map[uint32]any)
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
				if initialValue < mod.amount {
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
	m.log.Tracef("Removing oid %s from tracked orders", id)

	var oid order.OrderID
	copy(oid[:], id[:])

	m.ordersMtx.Lock()
	defer m.ordersMtx.Unlock()
	delete(m.orders, oid)
}

// handleMatchUpdate updates the bots balances based on a match's status.
// Balances are updated due to a match two times, once when the match is
// first seen, and once when the match is settled.
//
//   - When a match is seen, it is assumed that the match will eventually be
//     redeemed, so funding balance is decreased and pending redeem balance
//     is increased.
//   - When a match is settles, the balances are updated differently depending
//     on whether the match was refunded or redeemed.
func (m *MarketMaker) handleMatchUpdate(match *core.Match, oid dex.Bytes) {
	orderInfo := m.getOrderInfo(oid)
	if orderInfo == nil {
		m.log.Debugf("did not find order info for order %s", oid)
		return
	}

	var maxRedeemFees uint64
	if orderInfo.initialRedeemFeesLocked == 0 {
		numLots := match.Qty / orderInfo.lotSize
		maxRedeemFees = numLots * orderInfo.singleLotRedeemFees
	}

	var matchID order.MatchID
	copy(matchID[:], match.MatchID)

	if _, seen := orderInfo.matchesSeen[matchID]; !seen {
		orderInfo.matchesSeen[matchID] = struct{}{}
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

	unconfirmed := match.Status != order.MatchConfirmed
	notRefunded := match.Refund == nil
	revokedPreSwap := match.Revoked && match.Swap == nil
	if unconfirmed && notRefunded && !revokedPreSwap {
		return
	}

	if _, settled := orderInfo.matchesSettled[matchID]; settled {
		return
	}
	orderInfo.matchesSettled[matchID] = struct{}{}

	if match.Refund != nil {
		var singleLotRefundFees uint64
		if orderInfo.initialRefundFeesLocked == 0 {
			singleLotRefundFees = orderInfo.singleLotRefundFees
		}
		var balanceMods []*balanceMod
		if orderInfo.order.Sell {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.QuoteID, balTypePendingRedeem, calc.BaseToQuote(match.Rate, match.Qty) - maxRedeemFees},
				{balanceModIncrease, orderInfo.order.BaseID, balTypeAvailable, match.Qty - singleLotRefundFees},
			}
		} else {
			balanceMods = []*balanceMod{
				{balanceModDecrease, orderInfo.order.BaseID, balTypePendingRedeem, match.Qty - maxRedeemFees},
				{balanceModIncrease, orderInfo.order.QuoteID, balTypeAvailable, calc.BaseToQuote(match.Rate, match.Qty) - singleLotRefundFees},
			}
		}
		m.modifyBotBalance(orderInfo.bot, balanceMods)
	} else if match.Redeem != nil {
		redeemAsset := orderInfo.order.BaseID
		redeemQty := match.Qty
		if orderInfo.order.Sell {
			redeemAsset = orderInfo.order.QuoteID
			redeemQty = calc.BaseToQuote(match.Rate, redeemQty)
		}
		balanceMods := []*balanceMod{
			{balanceModDecrease, redeemAsset, balTypePendingRedeem, redeemQty - maxRedeemFees},
			{balanceModIncrease, redeemAsset, balTypeAvailable, redeemQty - maxRedeemFees},
		}
		m.modifyBotBalance(orderInfo.bot, balanceMods)
	} else if match.Swap != nil {
		// Something went wrong.. we made a swap tx, but did not get a refund or redeem.
		m.log.Errorf("oid: %s, match %s is in confirmed state, but no refund or redeem", oid, matchID)
		redeemAsset := orderInfo.order.BaseID
		redeemQty := match.Qty
		if orderInfo.order.Sell {
			redeemAsset = orderInfo.order.QuoteID
			redeemQty = calc.BaseToQuote(match.Rate, redeemQty)
		}
		balanceMods := []*balanceMod{
			{balanceModDecrease, redeemAsset, balTypePendingRedeem, redeemQty - maxRedeemFees},
		}
		m.modifyBotBalance(orderInfo.bot, balanceMods)
	} else {
		// We did not even make a swap tx. The modifications here are the
		// opposite of what happened when the match was first seen.
		var balanceMods []*balanceMod
		if orderInfo.order.Sell {
			balanceMods = []*balanceMod{
				{balanceModIncrease, orderInfo.order.BaseID, balTypeFundingOrder, match.Qty},
				{balanceModDecrease, orderInfo.order.QuoteID, balTypePendingRedeem, calc.BaseToQuote(match.Rate, match.Qty) - maxRedeemFees},
			}
		} else {
			balanceMods = []*balanceMod{
				{balanceModIncrease, orderInfo.order.QuoteID, balTypeFundingOrder, calc.BaseToQuote(match.Rate, match.Qty)},
				{balanceModDecrease, orderInfo.order.BaseID, balTypePendingRedeem, match.Qty - maxRedeemFees},
			}
		}
		m.modifyBotBalance(orderInfo.bot, balanceMods)
	}

	if orderInfo.finishedProcessing() {
		m.removeOrderInfo(oid)
	}
}

// handleOrderNotification checks if any funds are ready to be made available
// for use by a bot depending on the order's state.
//   - First, any updates to the balances based on the state of the matches
//     are made.
//   - If the order is no longer booked, the difference between the order's
//     quantity and the amount that was matched can be returned to the bot.
//   - If all fees have been confirmed, the rest of the difference between
//     the amount that was either initially locked or max possible to be used
//     and the amount that was actually used can be returned.
func (m *MarketMaker) handleOrderUpdate(o *core.Order) {
	orderInfo := m.getOrderInfo(o.ID)
	if orderInfo == nil {
		return
	}

	orderInfo.order = o

	for _, match := range o.Matches {
		m.handleMatchUpdate(match, o.ID)
	}

	notReadyToReturnOverLock := o.LockedAmt > 0
	returnedOverLockButNotReadyToReturnExcessFees := orderInfo.unusedLockedFundsReturned && !orderInfo.excessFeesReturned && !o.AllFeesConfirmed
	complete := orderInfo.unusedLockedFundsReturned && orderInfo.excessFeesReturned
	if notReadyToReturnOverLock || returnedOverLockButNotReadyToReturnExcessFees || complete {
		return
	}

	fromAsset, toAsset := o.BaseID, o.QuoteID
	if !o.Sell {
		fromAsset, toAsset = toAsset, fromAsset
	}

	var swappedLots, swappedMatches, swappedQty, redeemedLots, refundedMatches uint64
	for _, match := range o.Matches {
		if match.IsCancel {
			continue
		}
		numLots := match.Qty / orderInfo.lotSize
		fromAssetQty := match.Qty
		if fromAsset == o.QuoteID {
			fromAssetQty = calc.BaseToQuote(match.Rate, fromAssetQty)
		}
		if match.Swap != nil {
			swappedLots += numLots
			swappedMatches++
			swappedQty += fromAssetQty
		}
		if match.Refund != nil {
			refundedMatches++
		}
		if match.Redeem != nil {
			redeemedLots += numLots
		}
	}

	if !orderInfo.unusedLockedFundsReturned {
		maxSwapFees := swappedMatches * orderInfo.singleLotSwapFees
		usedFunds := swappedQty + maxSwapFees
		if usedFunds < orderInfo.initialFundsLocked {
			overLock := orderInfo.initialFundsLocked - usedFunds
			balanceMods := []*balanceMod{
				{balanceModIncrease, fromAsset, balTypeAvailable, overLock},
				{balanceModDecrease, fromAsset, balTypeFundingOrder, overLock},
			}
			m.modifyBotBalance(orderInfo.bot, balanceMods)
		} else {
			m.log.Errorf("oid: %s - usedFunds %d >= initialFundsLocked %d",
				o.ID, orderInfo.initialFundsLocked)
		}
		orderInfo.unusedLockedFundsReturned = true
	}

	if !orderInfo.excessFeesReturned && o.AllFeesConfirmed {
		// Return excess swap fees
		maxSwapFees := swappedMatches * orderInfo.singleLotSwapFees
		if maxSwapFees > o.FeesPaid.Swap {
			balanceMods := []*balanceMod{
				{balanceModIncrease, fromAsset, balTypeAvailable, maxSwapFees - o.FeesPaid.Swap},
				{balanceModDecrease, fromAsset, balTypeFundingOrder, maxSwapFees},
			}
			m.modifyBotBalance(orderInfo.bot, balanceMods)
		} else if maxSwapFees < o.FeesPaid.Swap {
			m.log.Errorf("oid: %s - maxSwapFees %d < swap fees %d", o.ID, maxSwapFees, o.FeesPaid.Swap)
		}

		// Return excess redeem fees
		if orderInfo.initialRedeemFeesLocked > 0 { // AccountLocker
			if orderInfo.initialRedeemFeesLocked > o.FeesPaid.Redemption {
				balanceMods := []*balanceMod{
					{balanceModIncrease, toAsset, balTypeAvailable, orderInfo.initialRedeemFeesLocked - o.FeesPaid.Redemption},
					{balanceModDecrease, toAsset, balTypeFundingOrder, orderInfo.initialRedeemFeesLocked},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			} else {
				m.log.Errorf("oid: %s - initialRedeemFeesLocked %d > redemption fees %d",
					o.ID, orderInfo.initialRedeemFeesLocked, o.FeesPaid.Redemption)
			}
		} else {
			maxRedeemFees := redeemedLots * orderInfo.singleLotRedeemFees
			if maxRedeemFees > o.FeesPaid.Redemption {
				balanceMods := []*balanceMod{
					{balanceModIncrease, toAsset, balTypeAvailable, maxRedeemFees - o.FeesPaid.Redemption},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			} else if maxRedeemFees < o.FeesPaid.Redemption {
				m.log.Errorf("oid: %v - maxRedeemFees %d < redemption fees %d",
					hex.EncodeToString(o.ID), maxRedeemFees, o.FeesPaid.Redemption)
			}
		}

		// Return excess refund fees
		if orderInfo.initialRefundFeesLocked > 0 { // AccountLocker
			if orderInfo.initialRefundFeesLocked > o.FeesPaid.Refund {
				balanceMods := []*balanceMod{
					{balanceModIncrease, fromAsset, balTypeAvailable, orderInfo.initialRefundFeesLocked - o.FeesPaid.Refund},
					{balanceModDecrease, fromAsset, balTypeFundingOrder, orderInfo.initialRefundFeesLocked},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			}
		} else {
			maxRefundFees := refundedMatches * orderInfo.singleLotRefundFees
			if maxRefundFees > o.FeesPaid.Refund {
				balanceMods := []*balanceMod{
					{balanceModIncrease, fromAsset, balTypeAvailable, maxRefundFees - o.FeesPaid.Refund},
				}
				m.modifyBotBalance(orderInfo.bot, balanceMods)
			} else if maxRefundFees < o.FeesPaid.Refund {
				m.log.Errorf("oid: %s - max refund fees %d < refund fees %d",
					o.ID, maxRefundFees, o.FeesPaid.Refund)
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
func (m *MarketMaker) Run(ctx context.Context, pw []byte, alternateConfigPath *string) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("market making is already running")
	}
	path := m.cfgPath
	if alternateConfigPath != nil {
		path = *alternateConfigPath
	}
	cfg, err := getMarketMakingConfig(path)
	if err != nil {
		return fmt.Errorf("error getting market making config: %v", err)
	}

	var startedMarketMaking bool
	defer func() {
		if !startedMarketMaking {
			m.running.Store(false)
		}
	}()

	m.ctx, m.die = context.WithCancel(ctx)

	enabledBots, err := validateAndFilterEnabledConfigs(cfg.BotConfigs)
	if err != nil {
		return err
	}

	if err := m.loginAndUnlockWallets(pw, enabledBots); err != nil {
		return err
	}

	oracle, err := priceOracleFromConfigs(m.ctx, enabledBots, m.log.SubLogger("PriceOracle"))
	if err != nil {
		return err
	}

	m.syncedOracleMtx.Lock()
	m.syncedOracle = oracle
	m.syncedOracleMtx.Unlock()
	defer func() {
		m.syncedOracleMtx.Lock()
		m.syncedOracle = nil
		m.syncedOracleMtx.Unlock()
	}()

	if err := m.setupBalances(enabledBots); err != nil {
		return err
	}

	user := m.core.User()
	cexes := make(map[string]libxc.CEX)
	cexCMs := make(map[string]*dex.ConnectionMaster)

	startedMarketMaking = true
	m.core.Broadcast(newMMStartStopNote(true))

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

	var cexCfgMap map[string]*CEXConfig
	if len(cfg.CexConfigs) > 0 {
		cexCfgMap = make(map[string]*CEXConfig, len(cfg.CexConfigs))
		for _, cexCfg := range cfg.CexConfigs {
			cexCfgMap[cexCfg.Name] = cexCfg
		}
	}

	getConnectedCEX := func(cexName string) (libxc.CEX, error) {
		var cex libxc.CEX
		var found bool
		if cex, found = cexes[cexName]; !found {
			cexCfg := cexCfgMap[cexName]
			if cexCfg == nil {
				return nil, fmt.Errorf("no CEX config provided for %s", cexName)
			}
			logger := m.log.SubLogger(fmt.Sprintf("CEX-%s", cexName))
			cex, err = libxc.NewCEX(cexName, cexCfg.APIKey, cexCfg.APISecret, logger, dex.Simnet)
			if err != nil {
				return nil, fmt.Errorf("failed to create CEX: %v", err)
			}
			cm := dex.NewConnectionMaster(cex)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to CEX: %v", err)
			}
			cexCMs[cexName] = cm
			err = cm.Connect(m.ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to CEX: %v", err)
			}
			cexes[cexName] = cex
		}
		return cex, nil
	}

	for _, cfg := range enabledBots {
		switch {
		case cfg.BasicMMConfig != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				defer wg.Done()
				mkt := MarketWithHost{cfg.Host, cfg.BaseAsset, cfg.QuoteAsset}
				m.markBotAsRunning(mkt, true)
				defer func() {
					m.markBotAsRunning(mkt, false)
				}()

				m.core.Broadcast(newBotStartStopNote(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset, true))
				defer func() {
					m.core.Broadcast(newBotStartStopNote(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset, false))
				}()
				logger := m.log.SubLogger(fmt.Sprintf("MarketMaker-%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset))
				mktID := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
				var baseFiatRate, quoteFiatRate float64
				if user != nil {
					baseFiatRate = user.FiatRates[cfg.BaseAsset]
					quoteFiatRate = user.FiatRates[cfg.QuoteAsset]
				}
				RunBasicMarketMaker(m.ctx, cfg, m.wrappedCoreForBot(mktID), oracle, baseFiatRate, quoteFiatRate, logger)
			}(cfg)
		case cfg.SimpleArbConfig != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				defer wg.Done()
				logger := m.log.SubLogger(fmt.Sprintf("Arbitrage-%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset))
				cex, err := getConnectedCEX(cfg.SimpleArbConfig.CEXName)
				if err != nil {
					logger.Errorf("failed to connect to CEX: %v", err)
					return
				}
				RunSimpleArbBot(m.ctx, cfg, m.core, cex, logger)
			}(cfg)
		default:
			m.log.Errorf("No bot config provided. Skipping %s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		}
	}

	go func() {
		wg.Wait()
		for cexName, cm := range cexCMs {
			m.log.Infof("Shutting down connection to %s", cexName)
			cm.Wait()
			m.log.Infof("Connection to %s shut down", cexName)
		}
		m.running.Store(false)
		m.core.Broadcast(newMMStartStopNote(false))
	}()

	return nil
}

func getMarketMakingConfig(path string) (*MarketMakingConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("no config file provided")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &MarketMakingConfig{}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// GetMarketMakingConfig returns the market making config.
func (m *MarketMaker) GetMarketMakingConfig() (*MarketMakingConfig, error) {
	return getMarketMakingConfig(m.cfgPath)
}

// UpdateMarketMakingConfig updates the configuration for one of the bots.
func (m *MarketMaker) UpdateBotConfig(updatedCfg *BotConfig) (*MarketMakingConfig, error) {
	cfg, err := m.GetMarketMakingConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting market making config: %v", err)
	}

	var updated bool
	for i, c := range cfg.BotConfigs {
		if c.Host == updatedCfg.Host && c.QuoteAsset == updatedCfg.QuoteAsset && c.BaseAsset == updatedCfg.BaseAsset {
			cfg.BotConfigs[i] = updatedCfg
			updated = true
			break
		}
	}
	if !updated {
		cfg.BotConfigs = append(cfg.BotConfigs, updatedCfg)
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling market making config: %v", err)
	}

	err = os.WriteFile(m.cfgPath, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("error writing market making config: %v", err)
	}
	return cfg, nil
}

// RemoveConfig removes a bot config from the market making config.
func (m *MarketMaker) RemoveBotConfig(host string, baseID, quoteID uint32) (*MarketMakingConfig, error) {
	cfg, err := m.GetMarketMakingConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting market making config: %v", err)
	}

	var updated bool
	for i, c := range cfg.BotConfigs {
		if c.Host == host && c.QuoteAsset == quoteID && c.BaseAsset == baseID {
			cfg.BotConfigs = append(cfg.BotConfigs[:i], cfg.BotConfigs[i+1:]...)
			updated = true
			break
		}
	}
	if !updated {
		return nil, fmt.Errorf("config not found")
	}

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling market making config: %v", err)
	}

	err = os.WriteFile(m.cfgPath, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("error writing market making config: %v", err)
	}
	return cfg, nil
}

// Stop stops the MarketMaker.
func (m *MarketMaker) Stop() {
	if m.die != nil {
		m.die()
	}
}
