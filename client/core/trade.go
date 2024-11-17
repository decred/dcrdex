// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
)

// ExpirationErr indicates that the wait.TickerQueue has expired a waiter, e.g.
// a reported coin was not found before the set expiration time.
type ExpirationErr string

// Error satisfies the error interface for ExpirationErr.
func (err ExpirationErr) Error() string { return string(err) }

// Ensure matchTracker satisfies the Stringer interface.
var _ (fmt.Stringer) = (*matchTracker)(nil)

// A matchTracker is used to negotiate a match.
type matchTracker struct {
	// counterConfirms records the last known confirms of the counterparty swap.
	// This is set in isSwappable for taker, isRedeemable for maker. -1 means
	// the confirms have not yet been checked (or logged). swapConfirms is for
	// your own swap. For safe access, use the matchTracker methods: confirms,
	// setSwapConfirms, and setCounterConfirms.
	counterConfirms int64 // atomic
	swapConfirms    int64 // atomic

	// sendingInitAsync indicates if this match's init request is being sent to
	// the server and awaiting a response. No attempts will be made to send
	// another init request for this match while one is already active.
	sendingInitAsync uint32 // atomic
	// sendingRedeemAsync indicates if this match's redeem request is being sent
	// to the server and awaiting a response. No attempts will be made to send
	// another redeem request for this match while one is already active.
	sendingRedeemAsync uint32 // atomic

	// The first group of fields below should be accessed with the parent
	// trackedTrade's mutex locked, excluding the atomic fields.

	db.MetaMatch
	// swapErr is an error set when we have given up hope on broadcasting a swap
	// tx for a match. This can happen if 1) the swap has been attempted
	// (repeatedly), but could not be successfully broadcast before the
	// broadcast timeout, or 2) a match data was found to be in a nonsensical
	// state during startup.
	swapErr error
	// swapErrCount counts the swap attempts. It is used in recovery.
	swapErrCount int
	// redeemErrCount counts the redeem attempts. It is used in recovery.
	redeemErrCount int
	// suspectSwap is a flag to indicate that there was a problem encountered
	// trying to send a swap contract for this match. If suspectSwap is true,
	// the match will not be grouped when attempting future swaps.
	suspectSwap bool
	// suspectRedeem is a flag to indicate that there was a problem encountered
	// trying to redeem this match. If suspectRedeem is true, the match will not
	// be grouped when attempting future redemptions.
	suspectRedeem bool
	// refundErr will be set to true if we attempt a refund and get a
	// CoinNotFoundError, indicating there is nothing to refund and the
	// counterparty redemption search should be attempted. Prevents retries.
	refundErr   error
	prefix      *order.Prefix
	trade       *order.Trade
	counterSwap *asset.AuditInfo
	// cancelRedemptionSearch should be set when taker starts searching for
	// maker's redemption. Required to cancel a find redemption attempt if
	// taker successfully executes a refund.
	cancelRedemptionSearch context.CancelFunc

	// confirmRedemptionNumTries is just used for logging.
	confirmRedemptionNumTries int
	// redemptionConfs and redemptionConfsReq are updated while the redemption
	// confirmation process is running. Their values are not updated after the
	// match reaches MatchConfirmed status.
	redemptionConfs    uint64
	redemptionConfsReq uint64
	// redemptionRejected will be true if a redemption tx was rejected. A
	// a rejected tx may indicate a serious internal issue, so we will seek
	// user approval before replacing the tx.
	redemptionRejected bool
	// matchCompleteSent precludes sending another redeem to the server if we
	// we are retrying after rejection and they already accepted our first
	// request. Additional requests will just error and they don't really care
	// if we redeem as taker anyway.
	matchCompleteSent bool

	// confirmRefundNumTries is just used for logging.
	confirmRefundNumTries int
	// refundConfs and refundConfsReq are updated while the refund
	// confirmation process is running. Their values are not updated after the
	// match reaches MatchConfirmed status.
	refundConfs    uint64
	refundConfsReq uint64
	// refundRejected will be true if a refund tx was rejected. A
	// a rejected tx may indicate a serious internal issue, so we will seek
	// user approval before replacing the tx.
	refundRejected bool

	// The fields below need to be modified without the parent trackedTrade's
	// mutex being write locked, so they have dedicated mutexes.

	swapSpentTimeMtx sync.Mutex
	swapSpentTime    time.Time

	// lastExpireDur is the most recently logged time until expiry of the
	// party's own contract. This may be negative if expiry has passed, but it
	// is not yet refundable due to other consensus rules. This is used only by
	// isRefundable. Initialize this to a very large value to guarantee that it
	// will be logged on the first check or when 0. This facilitates useful
	// logging, while not being spammy.
	lastExpireDurMtx sync.Mutex
	lastExpireDur    time.Duration

	// Certain exceptions that control swap actions are commonly accessed
	// together, and these share a single mutex. See the exceptions and
	// delayTicks methods.
	exceptionMtx sync.RWMutex
	// tickGovernor can be set non-nil to prevent swaps or redeems from being
	// attempted for a match. Typically, the Timer comes from an AfterFunc that
	// itself nils the tickGovernor. Guarded by exceptionMtx.
	tickGovernor *time.Timer
	// checkServerRevoke is set to make sure that a taker will not prematurely
	// send an initialization until it is confirmed with the server (see
	// authDEX) that the match is not revoked. This should be set on reconnect
	// for all taker matches in MakerSwapCast. Guarded by exceptionMtx.
	checkServerRevoke bool
}

// matchTime returns the match's match time as a time.Time.
func (m *matchTracker) matchTime() time.Time {
	return time.UnixMilli(int64(m.MetaData.Proof.Auth.MatchStamp)).UTC()
}

func (m *matchTracker) swapSpentAgo() time.Duration {
	m.swapSpentTimeMtx.Lock()
	defer m.swapSpentTimeMtx.Unlock()
	if m.swapSpentTime.IsZero() {
		return 0
	}
	return time.Since(m.swapSpentTime)
}

func (m *matchTracker) swapSpent() {
	m.swapSpentTimeMtx.Lock()
	defer m.swapSpentTimeMtx.Unlock()
	if !m.swapSpentTime.IsZero() {
		return // already noted
	}
	m.swapSpentTime = time.Now()
}

// setExpireDur records the last known duration until expiry if the difference
// from the previous recorded duration is at least the provided log interval
// threshold. The return indicates if it was updated (and should be logged by
// the caller).
func (m *matchTracker) setExpireDur(expireDur, logInterval time.Duration) (intervalPassed bool) {
	m.lastExpireDurMtx.Lock()
	defer m.lastExpireDurMtx.Unlock()
	if m.lastExpireDur-expireDur < logInterval {
		return false // too soon
	}
	m.lastExpireDur = expireDur
	return true // ok to log
}

func (m *matchTracker) exceptions() (ticksGoverned, checkServerRevoke bool) {
	m.exceptionMtx.RLock()
	defer m.exceptionMtx.RUnlock()
	return m.tickGovernor != nil, m.checkServerRevoke
}

// delayTicks sets the tickGovernor to prevent retrying too quickly after an
// error.
func (m *matchTracker) delayTicks(waitTime time.Duration) {
	m.exceptionMtx.Lock()
	m.tickGovernor = time.AfterFunc(waitTime, func() {
		m.exceptionMtx.Lock()
		m.tickGovernor = nil
		m.exceptionMtx.Unlock()
	})
	m.exceptionMtx.Unlock()
}

func (m *matchTracker) confirms() (mine, theirs int64) {
	return atomic.LoadInt64(&m.swapConfirms), atomic.LoadInt64(&m.counterConfirms)
}

func (m *matchTracker) setSwapConfirms(mine int64) {
	atomic.StoreInt64(&m.swapConfirms, mine)
}

func (m *matchTracker) setCounterConfirms(theirs int64) (was int64) {
	return atomic.SwapInt64(&m.counterConfirms, theirs)
}

// token returns a shortened representation of the match ID.
func (m *matchTracker) token() string {
	return hex.EncodeToString(m.MatchID[:4])
}

// trackedCancel is information necessary to track a cancel order. A
// trackedCancel is always associated with a trackedTrade.
type trackedCancel struct {
	order.CancelOrder
	epochLen uint64
	matches  struct {
		maker *msgjson.Match
		taker *msgjson.Match
	}
}

type feeStamped struct {
	sync.RWMutex
	rate  uint64
	stamp time.Time
}

func (fs *feeStamped) get() uint64 {
	fs.RLock()
	defer fs.RUnlock()
	return fs.rate
}

const (
	// freshRedeemFeeAge is the expiry age for cached redeem fee rates, past
	// which fetchFeeFromOracle should be used to refresh the rate. See
	// cacheRedemptionFeeSuggestion.
	freshRedeemFeeAge = time.Minute

	// spentAgoThreshNormal is how long to wait after we as taker observer our
	// swap spent by the maker without receiving a redemption request from the
	// server before initiating a redemption search and auto-redeem.
	spentAgoThreshNormal = 10 * time.Minute
	// spentAgoThreshSelfGoverned is like spentAgoThreshNormal, but for a
	// self-governed trade. We are less patient if the server is down or
	// lacking the market or asset configs involved.
	spentAgoThreshSelfGoverned = time.Minute
)

// trackedTrade is an order (issued by this client), its matches, and its cancel
// order, if applicable. The trackedTrade has methods for handling requests
// from the DEX to progress match negotiation.
type trackedTrade struct {
	// redeemFeeSuggestion is cached fee suggestion for redemption. We can't
	// request a fee suggestion at redeem time because it would require making
	// the full redemption routine async (TODO?). This fee suggestion is
	// intentionally not stored as part of the db.OrderMetaData, and should be
	// repopulated if the client is restarted. refundFeeSuggestion is the
	// same but for Refunds.
	redeemFeeSuggestion, refundFeeSuggestion feeStamped

	selfGoverned uint32 // (atomic) server either lacks this market or is down

	tickLock sync.Mutex // prevent multiple concurrent ticks, but allow them to queue

	order.Order

	db                 db.DB
	dc                 *dexConnection
	latencyQ           *wait.TickerQueue
	mktID              string // convenience for marketName(t.Base(), t.Quote())
	lockTimeTaker      time.Duration
	lockTimeMaker      time.Duration
	notify             func(Notification)
	formatDetails      func(Topic, ...any) (string, string)
	fromAssetID        uint32            // wallets.fromWallet.AssetID
	options            map[string]string // metaData.Options (immutable) for Redeem and Swap
	redemptionReserves uint64            // metaData.RedemptionReserves (immutable)
	refundReserves     uint64            // metaData.RefundReserves (immutable)
	preImg             order.Preimage

	csumMtx      sync.RWMutex
	csum         dex.Bytes // the commitment checksum provided in the preimage request
	cancelCsum   dex.Bytes
	cancelPreimg order.Preimage

	// mtx protects all read-write fields of the trackedTrade and the
	// matchTrackers in the matches map.
	mtx              sync.RWMutex
	metaData         *db.OrderMetaData
	wallets          *walletSet
	coins            map[string]asset.Coin
	coinsLocked      bool
	change           asset.Coin
	changeLocked     bool
	cancel           *trackedCancel
	matches          map[order.MatchID]*matchTracker
	redemptionLocked uint64 // remaining locked of redemptionReserves
	refundLocked     uint64 // remaining locked of refundReserves
	readyToTick      bool   // this will be false if either of the wallets cannot be connected and unlocked
}

// newTrackedTrade is a constructor for a trackedTrade.
func newTrackedTrade(dbOrder *db.MetaOrder, preImg order.Preimage, dc *dexConnection,
	lockTimeTaker, lockTimeMaker time.Duration, db db.DB, latencyQ *wait.TickerQueue, wallets *walletSet,
	coins asset.Coins, notify func(Notification), formatDetails func(Topic, ...any) (string, string)) *trackedTrade {

	fromID := dbOrder.Order.Quote()
	if dbOrder.Order.Trade().Sell {
		fromID = dbOrder.Order.Base()
	}

	ord := dbOrder.Order
	t := &trackedTrade{
		Order:              ord,
		metaData:           dbOrder.MetaData,
		dc:                 dc,
		db:                 db,
		latencyQ:           latencyQ,
		wallets:            wallets,
		preImg:             preImg,
		mktID:              marketName(ord.Base(), ord.Quote()),
		coins:              mapifyCoins(coins), // must not be nil even if empty
		coinsLocked:        len(coins) > 0,
		lockTimeTaker:      lockTimeTaker,
		lockTimeMaker:      lockTimeMaker,
		matches:            make(map[order.MatchID]*matchTracker),
		notify:             notify,
		fromAssetID:        fromID,
		formatDetails:      formatDetails,
		options:            dbOrder.MetaData.Options,
		redemptionReserves: dbOrder.MetaData.RedemptionReserves,
		refundReserves:     dbOrder.MetaData.RefundReserves,
		readyToTick:        true,
	}
	return t
}

func (t *trackedTrade) isSelfGoverned() bool {
	return atomic.LoadUint32(&t.selfGoverned) == 1
}

func (t *trackedTrade) spentAgoThresh() time.Duration {
	if t.isSelfGoverned() {
		return spentAgoThreshSelfGoverned
	}
	return spentAgoThreshNormal // longer
}

func (t *trackedTrade) setSelfGoverned(is bool) (changed bool) {
	if is {
		return atomic.CompareAndSwapUint32(&t.selfGoverned, 0, 1)
	}
	return atomic.CompareAndSwapUint32(&t.selfGoverned, 1, 0)
}

func (t *trackedTrade) status() order.OrderStatus {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.metaData.Status
}

// cacheRedemptionFeeSuggestion sets the redeemFeeSuggestion and refundFeeSuggestion
// for the trackedTrade. If a request to the server for the fee suggestion must
// be made, the request will be run in a goroutine, i.e. the field is not necessarily
// set when this method returns. If there is a synced book, the estimate will always
// be updated. If there is no synced book, but a non-zero fee suggestion is
// already cached, no new requests will be made.
//
// The trackedTrade mutex should be held for reads for safe access to the
// walletSet and the readyToTick flag.
func (t *trackedTrade) cacheFeeSuggestions() {
	now := time.Now()

	cache := func(fs *feeStamped, w *xcWallet) {
		fs.Lock()
		defer fs.Unlock()

		if now.Sub(fs.stamp) < freshRedeemFeeAge {
			return
		}

		set := func(rate uint64) {
			fs.rate = rate
			fs.stamp = now
		}

		// Use the wallet's rate first. Note that this could make a costly request
		// to an external fee oracle if an internal estimate is not available and
		// the wallet settings permit external API requests.
		toWallet := t.wallets.toWallet
		if t.readyToTick && toWallet.connected() {
			if feeRate := toWallet.feeRate(); feeRate != 0 {
				set(feeRate)
				return
			}
		}

		// Check any book that might have the fee recorded from an epoch_report note
		// (requires a book subscription).
		asset := w.AssetID
		feeSuggestion := t.dc.bestBookFeeSuggestion(asset)
		if feeSuggestion > 0 {
			set(feeSuggestion)
			return
		}

		// Fetch it from the server. Last resort!
		go func() {
			feeSuggestion = t.dc.fetchFeeRate(asset)
			if feeSuggestion > 0 {
				fs.Lock()
				set(feeSuggestion)
				fs.Unlock()
			}
		}()
	}

	// Cache redeem fee.
	cache(&t.redeemFeeSuggestion, t.wallets.toWallet)

	// Cache refund fee.
	cache(&t.refundFeeSuggestion, t.wallets.fromWallet)
}

// accountRedeemer is equivalent to calling
// xcWallet.Wallet.(asset.AccountLocker) on the to-wallet.
func (t *trackedTrade) accountRedeemer() (asset.AccountLocker, bool) {
	ar, is := t.wallets.toWallet.Wallet.(asset.AccountLocker)
	return ar, is
}

// accountRefunder is equivalent to calling
// xcWallet.Wallet.(asset.AccountLocker) on the from-wallet.
func (t *trackedTrade) accountRefunder() (asset.AccountLocker, bool) {
	ar, is := t.wallets.fromWallet.Wallet.(asset.AccountLocker)
	return ar, is
}

// lockRefundFraction locks the specified fraction of the available
// refund reserves. Subsequent calls are additive. If a call to
// lockRefundFraction would put the locked reserves > available reserves,
// nothing will be reserved, and an error message is logged.
func (t *trackedTrade) lockRefundFraction(num, denom uint64) {
	refunder, is := t.accountRefunder()
	if !is {
		return
	}
	newReserved := t.reservesToLock(num, denom, t.refundReserves, t.refundLocked)
	if newReserved == 0 {
		return
	}

	if err := refunder.ReReserveRefund(newReserved); err != nil {
		t.dc.log.Errorf("error re-reserving refund %d %s for order %s: %v",
			newReserved, t.wallets.fromWallet.unitInfo().AtomicUnit, t.ID(), err)
		return
	}
	t.refundLocked += newReserved
}

// lockRedemptionFraction locks the specified fraction of the available
// redemption reserves. Subsequent calls are additive. If a call to
// lockRedemptionFraction would put the locked reserves > available reserves,
// nothing will be reserved, and an error message is logged.
func (t *trackedTrade) lockRedemptionFraction(num, denom uint64) {
	redeemer, is := t.accountRedeemer()
	if !is {
		return
	}
	newReserved := t.reservesToLock(num, denom, t.redemptionReserves, t.redemptionLocked)
	if newReserved == 0 {
		return
	}

	if err := redeemer.ReReserveRedemption(newReserved); err != nil {
		t.dc.log.Errorf("error re-reserving redemption %d %s for order %s: %v",
			newReserved, t.wallets.toWallet.unitInfo().AtomicUnit, t.ID(), err)
		return
	}
	t.redemptionLocked += newReserved
}

// reservesToLock is a helper function used by lockRedemptionFraction and
// lockRefundFraction to determine the amount of funds to lock.
func (t *trackedTrade) reservesToLock(num, denom, reserves, reservesLocked uint64) uint64 {
	newReserved := applyFraction(num, denom, reserves)
	if reservesLocked+newReserved > reserves {
		t.dc.log.Errorf("attempting to mark as active more reserves than available for order %s:"+
			"%d available, %d already reserved, %d requested", t.ID(), t.redemptionReserves, t.redemptionLocked, newReserved)
		return 0
	}
	return newReserved
}

// unlockRefundFraction unlocks the specified fraction of the refund
// reserves. t.mtx should be locked if this trackedTrade is in the dc.trades
// map. If the requested unlock would put the locked reserves < 0, an error
// message is logged and the remaining locked reserves will be unlocked instead.
// If the remaining locked reserves after this unlock is determined to be
// "dust", it will be unlocked too.
func (t *trackedTrade) unlockRefundFraction(num, denom uint64) {
	refunder, is := t.accountRefunder()
	if !is {
		return
	}
	unlock := t.reservesToUnlock(num, denom, t.refundReserves, t.refundLocked)
	t.refundLocked -= unlock
	refunder.UnlockRefundReserves(unlock)
}

// unlockRedemptionFraction unlocks the specified fraction of the redemption
// reserves. t.mtx should be locked if this trackedTrade is in the dc.trades
// map. If the requested unlock would put the locked reserves < 0, an error
// message is logged and the remaining locked reserves will be unlocked instead.
// If the remaining locked reserves after this unlock is determined to be
// "dust", it will be unlocked too.
func (t *trackedTrade) unlockRedemptionFraction(num, denom uint64) {
	redeemer, is := t.accountRedeemer()
	if !is {
		return
	}
	unlock := t.reservesToUnlock(num, denom, t.redemptionReserves, t.redemptionLocked)
	t.redemptionLocked -= unlock
	redeemer.UnlockRedemptionReserves(unlock)
}

// reservesToUnlock is a helper function used by unlockRedemptionFraction and
// unlockRefundFraction to determine the amount of funds to unlock.
func (t *trackedTrade) reservesToUnlock(num, denom, reserves, reservesLocked uint64) uint64 {
	unlock := applyFraction(num, denom, reserves)
	if unlock > reservesLocked {
		t.dc.log.Errorf("attempting to unlock more than is reserved for order %s. unlocking reserved amount instead: "+
			"%d reserved, unlocking %d", t.ID(), reservesLocked, unlock)
		unlock = reservesLocked
	}

	reservesLocked -= unlock

	// Can be dust. Clean it up.
	var isDust bool
	if t.isMarketBuy() {
		isDust = reservesLocked < applyFraction(1, uint64(2*len(t.matches)), reserves)
	} else if t.metaData.Status > order.OrderStatusBooked && len(t.matches) > 0 {
		// Order is executed, so no changes should be expected. If there were
		// zero matches, the return is expected to be fraction 1 / 1, so no
		// reason to add handling for that case.
		mkt := t.dc.marketConfig(t.mktID)
		if mkt == nil {
			t.dc.log.Errorf("reservesToUnlock: could not find market: %v", t.mktID)
			return 0
		}
		lotSize := mkt.LotSize
		qty := t.Trade().Quantity
		// Dust if remaining reserved is less than the amount needed to
		// reserve one lot, which would be the smallest trade. Flooring
		// to avoid rounding issues.
		isDust = reservesLocked < uint64(math.Floor(float64(lotSize)/float64(qty)*float64(reserves)))
	}
	if isDust {
		unlock += reservesLocked
	}
	return unlock
}

func (t *trackedTrade) isMarketBuy() bool {
	trade := t.Trade()
	if trade == nil {
		return false
	}
	return t.Type() == order.MarketOrderType && !trade.Sell
}

func (t *trackedTrade) epochLen() uint64 {
	return t.metaData.EpochDur
}

func (t *trackedTrade) epochIdx() uint64 {
	// Guard against bizarre circumstances with both an old order without epoch
	// duration stored, AND a server that is either down or missing the market.
	if t.epochLen() == 0 {
		return 0
	}
	return uint64(t.Prefix().ServerTime.UnixMilli()) / t.epochLen()
}

// cancelEpochIdx gives the epoch index of any cancel associated cancel order.
// The mutex must be at least read locked.
func (t *trackedTrade) cancelEpochIdx() uint64 {
	if t.cancel == nil {
		return 0
	}
	epochLen := t.cancel.epochLen
	if epochLen == 0 {
		epochLen = t.epochLen()
	}
	if epochLen == 0 {
		// In these strange circumstances, the cancel should be declared stale
		// anyway (see hasStaleCancelOrder).
		return 0
	}
	return uint64(t.cancel.Prefix().ServerTime.UnixMilli()) / epochLen
}

func (t *trackedTrade) verifyCSum(vsum dex.Bytes, epochIdx uint64) error {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	t.csumMtx.RLock()
	csum, cancelCsum := t.csum, t.cancelCsum
	t.csumMtx.RUnlock()

	// First check the trade's recorded csum, if it is in this epoch.
	if epochIdx == t.epochIdx() && !bytes.Equal(vsum, csum) {
		return fmt.Errorf("checksum %s != trade order preimage request checksum %s for trade order %v",
			csum, csum, t.ID())
	}

	if t.cancel == nil {
		return nil // no linked cancel order
	}

	// Check the linked cancel order if it is for this epoch.
	if epochIdx == t.cancelEpochIdx() && !bytes.Equal(vsum, cancelCsum) {
		return fmt.Errorf("checksum %s != cancel order preimage request checksum %s for cancel order %v",
			vsum, cancelCsum, t.cancel.ID())
	}

	return nil // includes not in epoch
}

// rate returns the order's rate, or zero if a market or cancel order.
func (t *trackedTrade) rate() uint64 {
	if ord, ok := t.Order.(*order.LimitOrder); ok {
		return ord.Rate
	}
	return 0
}

// broadcastTimeout gets associated DEX's configured broadcast timeout. If the
// trade's dexConnection was unable to be established, 0 is returned.
func (t *trackedTrade) broadcastTimeout() time.Duration {
	t.dc.cfgMtx.RLock()
	defer t.dc.cfgMtx.RUnlock()
	// If the dexConnection was never established, we have no config.
	if t.dc.cfg == nil {
		return 0
	}
	return time.Millisecond * time.Duration(t.dc.cfg.BroadcastTimeout)
}

// coreOrder constructs a *core.Order for the tracked order.Order. If the trade
// has a cancel order associated with it, the cancel order will be returned,
// otherwise the second returned *Order will be nil.
func (t *trackedTrade) coreOrder() *Order {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.coreOrderInternal()
}

// coreOrderInternal constructs a *core.Order for the tracked order.Order. If
// the trade has a cancel order associated with it, the cancel order will be
// returned, otherwise the second returned *Order will be nil. coreOrderInternal
// should be called with the mtx >= RLocked.
func (t *trackedTrade) coreOrderInternal() *Order {
	corder := coreOrderFromTrade(t.Order, t.metaData)

	corder.Epoch = t.dc.marketEpoch(t.mktID, t.Prefix().ServerTime)
	corder.LockedAmt = t.lockedAmount()
	corder.ParentAssetLockedAmt = t.parentLockedAmt()
	corder.ReadyToTick = t.readyToTick
	corder.RedeemLockedAmt = t.redemptionLocked
	corder.RefundLockedAmt = t.refundLocked

	allFeesConfirmed := true
	for _, mt := range t.matches {
		if !mt.MetaData.Proof.SwapFeeConfirmed || !mt.MetaData.Proof.RedemptionFeeConfirmed {
			allFeesConfirmed = false
		}
		swapConfs, counterConfs := mt.confirms()
		corder.Matches = append(corder.Matches, matchFromMetaMatchWithConfs(t, &mt.MetaMatch,
			swapConfs, int64(t.metaData.FromSwapConf),
			counterConfs, int64(t.metaData.ToSwapConf),
			int64(mt.redemptionConfs), int64(mt.redemptionConfsReq),
			int64(mt.refundConfs), int64(mt.refundConfsReq)))
	}
	corder.AllFeesConfirmed = allFeesConfirmed

	return corder
}

// hasFundingCoins indicates if either funding or change coins are locked.
// This should be called with the mtx at least read locked.
func (t *trackedTrade) hasFundingCoins() bool {
	return t.changeLocked || t.coinsLocked
}

// lockedAmount is the total value of all coins currently locked for this trade.
// Returns the value sum of the initial funding coins if no swap has been sent,
// otherwise, the value of the locked change coin is returned.
// NOTE: This amount only applies to the wallet from which swaps are sent. This
// is the BASE asset wallet for a SELL order and the QUOTE asset wallet for a
// BUY order.
// lockedAmount should be called with the mtx >= RLocked.
func (t *trackedTrade) lockedAmount() (locked uint64) {
	if t.coinsLocked {
		// This implies either no swap has been sent, or the trade has been
		// resumed on restart after a swap that produced locked change (partial
		// fill and still booked) since restarting loads into coins/coinsLocked.
		for _, coin := range t.coins {
			locked += coin.Value()
		}
	} else if t.changeLocked && t.change != nil { // change may be returned but unlocked if the last swap has been sent
		locked = t.change.Value()
	}
	return
}

// parentLockedAmt returns the total amount of the parent asset locked for
// funding swaps in this order.
//
// NOTE: This amount only applies to the wallet from which swaps are sent. This
// is the BASE asset wallet for a SELL order and the QUOTE asset wallet for a
// BUY order.
// parentLockedAmt should be called with the mtx >= RLocked.
func (t *trackedTrade) parentLockedAmt() (locked uint64) {
	if t.coinsLocked {
		// This implies either no swap has been sent, or the trade has been
		// resumed on restart after a swap that produced locked change (partial
		// fill and still booked) since restarting loads into coins/coinsLocked.
		for _, coin := range t.coins {
			if tokenCoin, is := coin.(asset.TokenCoin); is {
				locked += tokenCoin.Fees()
			}
		}
	} else if t.changeLocked && t.change != nil { // change may be returned but unlocked if the last swap has been sent
		if tokenCoin, is := t.change.(asset.TokenCoin); is {
			locked += tokenCoin.Fees()
		}
	}
	return
}

// token is a string representation of the order ID.
func (t *trackedTrade) token() string {
	return (t.ID().String())
}

// clearCancel clears the unmatched cancel and deletes the cancel checksum and
// link to the trade in the dexConnection. clearCancel must be called with the
// trackedTrade.mtx locked.
func (t *trackedTrade) clearCancel(preImg order.Preimage) {
	if t.cancel != nil {
		t.dc.deleteCancelLink(t.cancel.ID())
		t.cancel = nil
	}
	t.csumMtx.Lock()
	t.cancelCsum = nil
	t.cancelPreimg = preImg
	t.csumMtx.Unlock()
}

// cancelTrade sets the cancellation data with the order and its preimage.
// cancelTrade must be called with the mtx write-locked.
func (t *trackedTrade) cancelTrade(co *order.CancelOrder, preImg order.Preimage, epochLen uint64) error {
	t.clearCancel(preImg)
	t.cancel = &trackedCancel{
		CancelOrder: *co,
		epochLen:    epochLen,
	}
	cid := co.ID()
	oid := t.ID()
	t.dc.registerCancelLink(cid, oid)
	err := t.db.LinkOrder(oid, cid)
	if err != nil {
		return fmt.Errorf("error linking cancel order %s for trade %s: %w", cid, oid, err)
	}
	t.metaData.LinkedOrder = cid
	return nil
}

// nomatch sets the appropriate order status and returns funding coins.
func (t *trackedTrade) nomatch(oid order.OrderID) (assetMap, error) {
	assets := make(assetMap)
	// Check if this is the cancel order.
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.ID() != oid {
		if t.cancel == nil || t.cancel.ID() != oid {
			return assets, newError(unknownOrderErr, "nomatch order ID %s does not match trade or cancel order", oid)
		}
		// This is a cancel order. Cancel status goes to executed, but the trade
		// status will not be canceled. Remove the trackedCancel and remove the
		// DB linked order from the trade, but not the cancel.
		t.dc.log.Warnf("Cancel order %s targeting trade %s did not match.", oid, t.ID())
		err := t.db.LinkOrder(t.ID(), order.OrderID{})
		if err != nil {
			t.dc.log.Errorf("DB error unlinking cancel order %s for trade %s: %v", oid, t.ID(), err)
		}
		// Clearing the trackedCancel allows this order to be canceled again.
		t.clearCancel(order.Preimage{})
		t.metaData.LinkedOrder = order.OrderID{}

		subject, details := t.formatDetails(TopicMissedCancel, makeOrderToken(t.token()))
		t.notify(newOrderNote(TopicMissedCancel, subject, details, db.WarningLevel, t.coreOrderInternal()))
		return assets, t.db.UpdateOrderStatus(oid, order.OrderStatusExecuted)
	}

	// This is the trade. Return coins and set status based on whether this is
	// a standing limit order or not.
	if t.metaData.Status != order.OrderStatusEpoch {
		return assets, fmt.Errorf("nomatch sent for non-epoch order %s", oid)
	}
	if lo, ok := t.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
		t.dc.log.Infof("Standing order %s did not match and is now booked.", t.token())
		t.metaData.Status = order.OrderStatusBooked
		t.notify(newOrderNote(TopicOrderBooked, "", "", db.Data, t.coreOrderInternal()))
	} else {
		t.returnCoins()
		t.unlockRedemptionFraction(1, 1)
		t.unlockRefundFraction(1, 1)
		assets.count(t.wallets.fromWallet.AssetID)
		t.dc.log.Infof("Non-standing order %s did not match.", t.token())
		t.metaData.Status = order.OrderStatusExecuted
		t.notify(newOrderNote(TopicNoMatch, "", "", db.Data, t.coreOrderInternal()))
	}
	return assets, t.db.UpdateOrderStatus(t.ID(), t.metaData.Status)
}

// negotiate creates and stores matchTrackers for the []*msgjson.Match, and
// updates (UserMatch).Filled. Match negotiation can then be progressed by
// calling (*trackedTrade).tick when a relevant event occurs, such as a request
// from the DEX or a tip change.
func (t *trackedTrade) negotiate(msgMatches []*msgjson.Match) error {
	trade := t.Trade()
	// Validate matches and check if a cancel match is included.
	// Non-cancel matches should be negotiated and are added to
	// the newTrackers slice.
	var cancelMatch *msgjson.Match
	newTrackers := make([]*matchTracker, 0, len(msgMatches))
	for _, msgMatch := range msgMatches {
		if len(msgMatch.MatchID) != order.MatchIDSize {
			return fmt.Errorf("match id of incorrect length. expected %d, got %d",
				order.MatchIDSize, len(msgMatch.MatchID))
		}
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		if oid != t.ID() {
			return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.ID())
		}

		var mid order.MatchID
		copy(mid[:], msgMatch.MatchID)
		// Do not process matches with existing matchTrackers. e.g. In case we
		// start "extra" matches from the 'connect' response negotiating via
		// authDEX>readConnectMatches, and a subsequent resent 'match' request
		// leads us here again or vice versa. Or just duplicate match requests.
		if t.matches[mid] != nil {
			t.dc.log.Warnf("Skipping match %v that is already negotiating.", mid)
			continue
		}

		// Check if this is a match with a cancel order, in which case the
		// counterparty Address field would be empty. If the user placed a
		// cancel order, that order will be recorded in t.cancel on cancel
		// order creation via (*dexConnection).tryCancel or restored from DB
		// via (*Core).dbTrackers.
		if t.cancel != nil && msgMatch.Address == "" {
			cancelMatch = msgMatch
			continue
		}

		match := &matchTracker{
			prefix:          t.Prefix(),
			trade:           trade,
			MetaMatch:       *t.makeMetaMatch(msgMatch),
			counterConfirms: -1, // initially unknown, log first check
			lastExpireDur:   365 * 24 * time.Hour,
		}
		match.Status = order.NewlyMatched // these must be new matches
		newTrackers = append(newTrackers, match)
	}

	// Record any cancel order Match and update order status.
	var metaCancelMatch *db.MetaMatch
	if cancelMatch != nil {
		t.dc.log.Infof("Order %s canceled. match id = %s",
			t.ID(), cancelMatch.MatchID)

		// Set this order status to Canceled and unlock any locked coins
		// if there are no new matches and there's no need to send swap
		// for any previous match.
		t.metaData.Status = order.OrderStatusCanceled
		if len(newTrackers) == 0 {
			t.maybeReturnCoins()
		}

		// Note: In TopicNewMatch later, it must be status complete to agree
		// with coreOrderFromMetaOrder, which pulls match data *from the DB*.
		cancelMatch.Status = uint8(order.MatchComplete) // we're completing it now
		cancelMatch.Address = ""                        // not a trade match

		t.cancel.matches.maker = cancelMatch // taker is stored via processCancelMatch before negotiate
		// Set the order status for the cancel order.
		err := t.db.UpdateOrderStatus(t.cancel.ID(), order.OrderStatusExecuted)
		if err != nil {
			t.dc.log.Errorf("Failed to update status of cancel order %v to executed: %v",
				t.cancel.ID(), err)
			// Try to store the match anyway.
		}
		// Store a completed maker cancel match in the DB.
		metaCancelMatch = t.makeMetaMatch(cancelMatch)
		err = t.db.UpdateMatch(metaCancelMatch)
		if err != nil {
			return fmt.Errorf("failed to update match in db: %w", err)
		}
	}

	// Now that each Match in msgMatches has been validated, store them in the
	// trackedTrade and the DB, and update the newFill amount.
	var newFill uint64
	for _, match := range newTrackers {
		var qty uint64
		if t.isMarketBuy() {
			qty = calc.BaseToQuote(match.Rate, match.Quantity)
		} else {
			qty = match.Quantity
		}
		newFill += qty

		if trade.Filled()+newFill > trade.Quantity {
			t.dc.log.Errorf("Match %s would put order %s fill over quantity. Revoking the match.",
				match, t.ID())
			match.MetaData.Proof.SelfRevoked = true
		}

		// If this order has no funding coins, block swaps attempts on the new
		// match. Do not revoke however since the user may be able to resolve
		// wallet configuration issues and restart to restore funding coins.
		// Otherwise the server will end up revoking these matches.
		if !t.hasFundingCoins() {
			t.dc.log.Errorf("Unable to begin swap negotiation for unfunded order %v", t.ID())
			match.swapErr = errors.New("no funding coins for swap")
		}

		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			// Don't abandon other matches because of this error, attempt
			// to negotiate the other matches.
			t.dc.log.Errorf("failed to update match %s in db: %v", match, err)
			continue
		}

		// Only add this match to the map if the db update succeeds, so
		// funds don't get stuck if user restarts Core after sending a
		// swap because negotiations will not be resumed for this match
		// and auto-refund cannot be performed.
		// TODO: Maybe allow? This match can be restored from the DEX's
		// connect response on restart IF it is not revoked.
		t.matches[match.MatchID] = match
		t.dc.log.Infof("Starting negotiation for match %s for order %v with swap fee rate = %v, quantity = %v",
			match, t.ID(), match.FeeRateSwap, qty)
	}

	// If the order has been canceled, add that to filled and newFill.
	preCancelFilled, canceled := t.recalcFilled()
	filled := preCancelFilled + canceled
	if cancelMatch != nil {
		newFill += cancelMatch.Quantity
	}
	// The filled amount includes all of the trackedTrade's matches, so the
	// filled amount must be set, not just increased.
	trade.SetFill(filled)

	// Before we update any order statuses, check if this is a market sell
	// order or an immediate TiF limit order which has just been executed. We
	// can return reserves for the remaining part of an order which will not
	// filled in the future if the order is a market sell, an immediate TiF
	// limit order, or if the order was cancelled.
	var completedMarketSell, completedImmediateTiF bool
	completedMarketSell = trade.Sell && t.Type() == order.MarketOrderType && t.metaData.Status < order.OrderStatusExecuted
	lo, ok := t.Order.(*order.LimitOrder)
	if ok {
		completedImmediateTiF = lo.Force == order.ImmediateTiF && t.metaData.Status < order.OrderStatusExecuted
	}
	if remain := trade.Quantity - preCancelFilled; remain > 0 && (completedMarketSell || completedImmediateTiF || cancelMatch != nil) {
		t.unlockRedemptionFraction(remain, trade.Quantity)
		t.unlockRefundFraction(remain, trade.Quantity)
	}

	// Set the order as executed depending on type and fill.
	if t.metaData.Status != order.OrderStatusCanceled && t.metaData.Status != order.OrderStatusRevoked {
		if lo, ok := t.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF && filled < trade.Quantity {
			t.metaData.Status = order.OrderStatusBooked
		} else {
			t.metaData.Status = order.OrderStatusExecuted
		}
	}

	// Send notifications.
	corder := t.coreOrderInternal()
	if metaCancelMatch != nil {
		topic := TopicBuyOrderCanceled
		if trade.Sell {
			topic = TopicSellOrderCanceled
		}
		subject, details := t.formatDetails(topic, unbip(t.Base()), unbip(t.Quote()), t.dc.acct.host, makeOrderToken(t.token()))

		t.notify(newOrderNote(topic, subject, details, db.Poke, corder))
		// Also send out a data notification with the cancel order information.
		t.notify(newOrderNote(TopicCancel, "", "", db.Data, corder))
		t.notify(newMatchNote(TopicNewMatch, "", "", db.Data, t, &matchTracker{
			prefix:    t.Prefix(),
			trade:     trade,
			MetaMatch: *metaCancelMatch,
		}))
	}
	if len(newTrackers) > 0 {
		fillPct := 100 * float64(filled) / float64(trade.Quantity)
		t.dc.log.Debugf("Trade order %v matched with %d orders: +%d filled, total fill %d / %d (%.1f%%)",
			t.ID(), len(newTrackers), newFill, filled, trade.Quantity, fillPct)

		// Match notifications.
		for _, match := range newTrackers {
			t.notify(newMatchNote(TopicNewMatch, "", "", db.Data, t, match))
		}

		// A single order notification.
		topic := TopicBuyMatchesMade
		if trade.Sell {
			topic = TopicSellMatchesMade
		}
		subject, details := t.formatDetails(topic, unbip(t.Base()), unbip(t.Quote()), fillPct, makeOrderToken(t.token()))
		t.notify(newOrderNote(topic, subject, details, db.Poke, corder))
	}

	err := t.db.UpdateOrder(t.metaOrder())
	if err != nil {
		return fmt.Errorf("failed to update order in db: %w", err)
	}
	return nil
}

func (t *trackedTrade) recalcFilled() (matchFilled, canceled uint64) {
	for _, mt := range t.matches {
		if t.isMarketBuy() {
			matchFilled += calc.BaseToQuote(mt.Rate, mt.Quantity)
		} else {
			matchFilled += mt.Quantity
		}
	}
	if t.cancel != nil && t.cancel.matches.maker != nil {
		canceled = t.cancel.matches.maker.Quantity
	}
	t.Trade().SetFill(matchFilled + canceled)
	return
}

func (t *trackedTrade) metaOrder() *db.MetaOrder {
	return &db.MetaOrder{
		MetaData: t.metaData,
		Order:    t.Order,
	}
}

func (t *trackedTrade) makeMetaMatch(msgMatch *msgjson.Match) *db.MetaMatch {
	// Contract txn asset: buy means quote, sell means base. NOTE: msgjson.Match
	// could instead have just FeeRateSwap for the recipient, but the other fee
	// rate could be of value for auditing the counter party's contract txn.
	feeRateSwap := msgMatch.FeeRateQuote
	if t.Trade().Sell {
		feeRateSwap = msgMatch.FeeRateBase
	}

	// Consider: bump fee rate here based on a user setting in dexConnection.
	// feeRateSwap = feeRateSwap * 11 / 10
	// maxFeeRate := t.dc.assets[swapAssetID].MaxFeeRate // swapAssetID according to t.Trade().Sell and t.Base()/Quote()
	// if feeRateSwap > maxFeeRate {
	// 	feeRateSwap = maxFeeRate
	// }

	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	var mid order.MatchID
	copy(mid[:], msgMatch.MatchID)
	return &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Proof: db.MatchProof{
				Auth: db.MatchAuth{
					MatchSig:   msgMatch.Sig,
					MatchStamp: msgMatch.ServerTime,
				},
			},
			DEX:   t.dc.acct.host,
			Base:  t.Base(),
			Quote: t.Quote(),
			Stamp: msgMatch.ServerTime,
		},
		UserMatch: &order.UserMatch{
			OrderID:     oid,
			MatchID:     mid,
			Quantity:    msgMatch.Quantity,
			Rate:        msgMatch.Rate,
			Address:     msgMatch.Address,
			Status:      order.MatchStatus(msgMatch.Status),
			Side:        order.MatchSide(msgMatch.Side),
			FeeRateSwap: feeRateSwap,
		},
	}
}

// processCancelMatch should be called with the message for the match on a
// cancel order.
func (t *trackedTrade) processCancelMatch(msgMatch *msgjson.Match) error {
	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	var mid order.MatchID
	copy(mid[:], msgMatch.MatchID)
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.cancel == nil {
		return fmt.Errorf("no cancel order recorded for order %v", oid)
	}
	if oid != t.cancel.ID() {
		return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.cancel.ID())
	}
	// Maker notification is logged at info.
	t.dc.log.Debugf("Taker notification for cancel order %v received. Match id = %s", oid, mid)
	t.cancel.matches.taker = msgMatch
	// Store the completed taker cancel match.
	takerCancelMeta := t.makeMetaMatch(t.cancel.matches.taker)
	takerCancelMeta.Status = order.MatchComplete
	takerCancelMeta.Address = "" // not a trade match
	err := t.db.UpdateMatch(takerCancelMeta)
	if err != nil {
		return fmt.Errorf("failed to update match in db: %w", err)
	}
	return nil
}

// Get the required and current confirmation count on the counterparty's swap
// contract transaction for the provided match. If the count has not changed
// since the previous check, changed will be false.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) counterPartyConfirms(ctx context.Context, match *matchTracker) (have, needed uint32, changed, spent, expired bool, err error) {
	fail := func(err error) (uint32, uint32, bool, bool, bool, error) {
		return 0, 0, false, false, false, err
	}

	// Counter-party's swap is the "to" asset.
	needed = t.metaData.ToSwapConf

	// Check the confirmations on the counter-party's swap. If counterSwap is
	// not set, we shouldn't be here, but catch this just in case.
	if match.counterSwap == nil {
		return fail(errors.New("no AuditInfo available to check"))
	}

	wallet := t.wallets.toWallet
	coin := match.counterSwap.Coin

	if !wallet.connected() {
		return fail(errWalletNotConnected)
	}

	_, lockTime, err := wallet.ContractLockTimeExpired(ctx, match.MetaData.Proof.CounterContract)
	if err != nil {
		return fail(fmt.Errorf("error checking if locktime has expired on taker's contract on order %s, "+
			"match %s: %w", t.ID(), match, err))
	}
	expired = time.Until(lockTime) < 0 // not necessarily refundable, but can be at any moment

	have, spent, err = wallet.swapConfirmations(ctx, coin.ID(),
		match.MetaData.Proof.CounterContract, match.MetaData.Stamp)
	if err != nil {
		return fail(fmt.Errorf("failed to get confirmations of the counter-party's swap %s (%s) "+
			"for match %s, order %v: %w",
			coin, t.wallets.toWallet.Symbol, match, t.UID(), err))
	}

	// Log the pending swap status at new heights only.
	was := match.setCounterConfirms(int64(have))
	if changed = was != int64(have); changed {
		t.notify(newMatchNote(TopicCounterConfirms, "", "", db.Data, t, match))
	}

	return
}

// deleteCancelOrder will clear any associated trackedCancel, and set the status
// of the cancel order as revoked in the DB so that it will not be loaded with
// other active orders on startup. The trackedTrade's OrderMetaData.LinkedOrder
// is also zeroed, but the caller is responsible for updating the trade's DB
// entry in a way that is appropriate for the caller (e.g. with LinkOrder,
// UpdateOrder, or UpdateOrderStatus).
//
// This is to be used in trade status resolution only, since normally the fate
// of cancel orders is determined by match/nomatch and status set to executed
// (see nomatch and negotiate). A missed preimage request for the cancel order
// that results in a revoke_order message for the cancel order should also use
// this method to unlink and retire the failed cancel order. Similarly, cancel
// orders detected as "stale" with the two-epochs-old heuristic use this.
//
// This method MUST be called with the trackedTrade mutex lock held for writes.
func (t *trackedTrade) deleteCancelOrder() {
	if t.cancel == nil {
		return
	}
	cid := t.cancel.ID()
	err := t.db.UpdateOrderStatus(cid, order.OrderStatusRevoked) // could actually be OrderStatusExecuted
	if err != nil {
		t.dc.log.Errorf("Error updating status in db for cancel order %v to revoked: %v", cid, err)
	}
	// Unlink the cancel order from the trade.
	t.clearCancel(order.Preimage{})
	t.metaData.LinkedOrder = order.OrderID{} // NOTE: caller may wish to update the trades's DB entry
}

func (t *trackedTrade) hasStaleCancelOrder() bool {
	if t.cancel == nil || t.metaData.Status != order.OrderStatusBooked {
		return false
	}

	epoch := order.EpochID{Idx: t.cancelEpochIdx(), Dur: t.epochLen()}
	epochEnd := epoch.End()

	return time.Since(epochEnd) >= preimageReqTimeout
}

// deleteStaleCancelOrder checks if this trade has an associated cancel order,
// and deletes the cancel order if the cancel order stays at Epoch status for
// more than 2 epochs. Deleting the stale cancel order from this trade makes
// it possible for the client to re- attempt cancelling the order.
//
// NOTE:
// Stale cancel orders would be Executed if their preimage was sent or Revoked
// if their preimages was not sent. We cannot currently tell whether the cancel
// order's preimage was revealed, so assume that the cancel order is Executed
// but unmatched. Consider adding a order.PreimageRevealed field to ensure that
// the correct final status is set for the cancel order; or allow the server to
// check and return status of cancel orders.
//
// This method MUST be called with the trackedTrade mutex lock held for writes.
func (t *trackedTrade) deleteStaleCancelOrder() {
	if !t.hasStaleCancelOrder() {
		return
	}

	t.dc.log.Infof("Cancel order %v in epoch status with server time stamp %v (%v old) considered executed and unmatched.",
		t.cancel.ID(), t.cancel.ServerTime, time.Since(t.cancel.ServerTime))

	// Clear the trackedCancel, allowing this order to be canceled again, and
	// set the cancel order's status as revoked.
	cancelOrd := t.cancel
	t.deleteCancelOrder()
	err := t.db.LinkOrder(t.ID(), order.OrderID{})
	if err != nil {
		t.dc.log.Errorf("DB error unlinking cancel order %s for trade %s: %v", cancelOrd.ID(), t.ID(), err)
	}

	subject, details := t.formatDetails(TopicFailedCancel, makeOrderToken(t.token()))
	t.notify(newOrderNote(TopicFailedCancel, subject, details, db.WarningLevel, t.coreOrderInternal()))
}

// isActive will be true if the trade is booked or epoch, or if any of the
// matches are still negotiating.
func (t *trackedTrade) isActive() bool {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	// Status of the order itself.
	if t.metaData.Status == order.OrderStatusBooked ||
		t.metaData.Status == order.OrderStatusEpoch {
		return true
	}

	// Status of all matches for the order.
	for _, match := range t.matches {
		// For debugging issues with match status and steps:
		// proof := &match.MetaData.Proof
		// t.dc.log.Tracef("Checking match %s (%v) in status %v. "+
		// 	"Order: %v, Refund coin: %v, ContractData: %x, Revoked: %v", match,
		// 	match.Side, match.Status, t.ID(),
		// 	proof.RefundCoin, proof.ContractData, proof.IsRevoked())
		if t.matchIsActive(match) {
			return true
		}
	}
	return false
}

// matchIsRevoked checks if the match is revoked, RLocking the trackedTrade.
func (t *trackedTrade) matchIsRevoked(match *matchTracker) bool {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return match.MetaData.Proof.IsRevoked()
}

// Matches are inactive if: (1) status is confirmed, (2) it is refunded, or (3)
// it is revoked and this side of the match requires no further action.
func (t *trackedTrade) matchIsActive(match *matchTracker) bool {
	proof := &match.MetaData.Proof
	isActive := db.MatchIsActive(match.UserMatch, proof)
	if proof.IsRevoked() && !isActive {
		t.dc.log.Tracef("Revoked match %s (%v) in status %v considered inactive.",
			match, match.Side, match.Status)
	}
	return isActive
}

// activeMatches returns active matches.
func (t *trackedTrade) activeMatches() []*matchTracker {
	var actives []*matchTracker
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	for _, match := range t.matches {
		if t.matchIsActive(match) {
			actives = append(actives, match)
		}
	}
	return actives
}

// unspentContractAmounts returns the total amount locked in unspent swaps.
// NOTE: This amount only applies to the wallet from which swaps are sent. This
// is the BASE asset wallet for a SELL order and the QUOTE asset wallet for a
// BUY order.
// unspentContractAmounts should be called with the mtx >= RLocked.
func (t *trackedTrade) unspentContractAmounts() (amount uint64) {
	swapSentFromQuoteAsset := t.fromAssetID == t.Quote()
	for _, match := range t.matches {
		side, status := match.Side, match.Status
		if status >= order.MakerRedeemed || len(match.MetaData.Proof.RefundCoin) != 0 {
			// Any redemption or own refund implies our swap is spent.
			// Even if we're Maker and our swap has not been redeemed
			// by Taker, we should consider it spent.
			continue
		}
		if (side == order.Maker && status >= order.MakerSwapCast) ||
			(side == order.Taker && status == order.TakerSwapCast) {
			swapAmount := match.Quantity
			if swapSentFromQuoteAsset {
				swapAmount = calc.BaseToQuote(match.Rate, match.Quantity)
			}
			amount += swapAmount
		}
	}
	return
}

// isSwappable will be true if the match is ready for a swap transaction to be
// broadcast.
//
// In certain situations, the match should be revoked and the return will
// indicate this. In particular, the situations are when the match is in
// MakerSwapCast on the taker side and the maker's swap is found to be either
// spent or expired, or if our future contract would have an expiry in the past.
// Such matches are also not swappable.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) isSwappable(ctx context.Context, match *matchTracker) (ready, shouldRevoke bool) {
	// Quick status check before we bother with the wallet.
	switch match.Status {
	case order.TakerSwapCast, order.MakerRedeemed, order.MatchComplete:
		return false, false // all swaps already sent
	}

	if match.swapErr != nil || match.MetaData.Proof.IsRevoked() {
		// t.dc.log.Tracef("Match %s not swappable: swapErr = %v, revoked = %v",
		// 	match, match.swapErr, match.MetaData.Proof.IsRevoked())
		return false, false
	}
	if ticksGoverned, checkServerRevoke := match.exceptions(); ticksGoverned || checkServerRevoke {
		// t.dc.log.Tracef("Match %s not swappable: metered = %t, checkServerRevoke = %v",
		// 	match, ticksGoverned, checkServerRevoke)
		return false, false
	}

	wallet := t.wallets.fromWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually swappables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("Order %s, match %s is not swappable because %s wallet is not unlocked",
			t.ID(), match, unbip(wallet.AssetID))
		return false, false
	}

	defer func() {
		// We should never try to init when the server is known to be down or
		// lacks this market, but allow all the checks to run first for the sake
		// of confirmation notifications and conditional self-revocation.
		ready = ready && !t.isSelfGoverned() && t.dc.status() == comms.Connected // NOTE: swapMatchGroup rechecks dc conn anyway
	}()

	switch match.Status {
	case order.NewlyMatched:
		return match.Side == order.Maker, false
	case order.MakerSwapCast:
		// Get the confirmation count on the maker's coin.
		if match.Side == order.Taker {
			// If the maker is the counterparty, we can determine swappability
			// based on the confirmations.
			confs, req, changed, spent, expired, err := t.counterPartyConfirms(ctx, match)
			if err != nil {
				if !errors.Is(err, asset.ErrSwapNotInitiated) {
					// We cannot get the swap data yet but there is no need
					// to log an error if swap not initiated as this is
					// expected for newly made swaps involving contracts.
					t.dc.log.Errorf("isSwappable: %v", err)
				}
				return false, false
			}
			if spent {
				t.dc.log.Errorf("Counter-party's swap is spent before we could broadcast our own. REVOKING!")
				return false, true // REVOKE!
			}
			if expired {
				t.dc.log.Errorf("Counter-party's swap expired before we could broadcast our own. REVOKING!")
				return false, true // REVOKE!
			}
			matchTime := match.matchTime()
			if lockTime := matchTime.Add(t.lockTimeTaker); time.Until(lockTime) < 0 {
				t.dc.log.Errorf("Our contract would expire in the past (%v). REVOKING!", lockTime)
				return false, true // REVOKE!
			}
			ready = confs >= req
			if changed && !ready {
				t.dc.log.Debugf("Match %s not yet swappable: current confs = %d, required confs = %d",
					match, confs, req)
			}
			return ready, false
		}

		// If we're the maker, check the confirmations anyway so we can notify.
		confs, spent, err := wallet.swapConfirmations(ctx, match.MetaData.Proof.MakerSwap,
			match.MetaData.Proof.ContractData, match.MetaData.Stamp)
		if err != nil && !errors.Is(err, asset.ErrSwapNotInitiated) {
			// No need to log an error if swap not initiated as this
			// is expected for newly made swaps involving contracts.
			t.dc.log.Errorf("isSwappable: error getting confirmation for our own swap transaction: %v", err)
		}
		if spent { // This should NEVER happen for maker in MakerSwapCast unless revoked and refunded!
			t.dc.log.Errorf("Our (maker) swap for match %s is being reported as spent before taker's swap was broadcast!", match)
		}
		match.setSwapConfirms(int64(confs))
		t.notify(newMatchNote(TopicConfirms, "", "", db.Data, t, match))
		return false, false
	}

	return false, false
}

// checkSwapFeeConfirms returns whether the swap fee confirmations should be
// checked.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) checkSwapFeeConfirms(match *matchTracker) bool {
	if match.MetaData.Proof.SwapFeeConfirmed {
		return false
	}
	_, dynamic := t.wallets.fromWallet.Wallet.(asset.DynamicSwapper)
	if !dynamic {
		// Confirmed will be set in the db.
		return true
	}
	// Waiting until the swap is definitely confirmed in order to not
	// keep calling the fee checker before the swap is confirmed.
	mySwapConfs, _ := match.confirms()
	if match.Side == order.Maker {
		return match.Status > order.MakerSwapCast || mySwapConfs > 0
	}
	return match.Status > order.TakerSwapCast || mySwapConfs > 0
}

// checkRedemptionFeeConfirms returns whether the swap fee confirmations should
// be checked.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) checkRedemptionFeeConfirms(match *matchTracker) bool {
	if match.MetaData.Proof.RedemptionFeeConfirmed || match.redemptionRejected {
		return false
	}
	_, dynamic := t.wallets.toWallet.Wallet.(asset.DynamicSwapper)
	if !dynamic {
		// Confirmed will be set in the db.
		return true
	}
	if match.Side == order.Maker {
		return match.Status >= order.MakerRedeemed
	}
	return match.Status >= order.MatchComplete
}

// updateDynamicSwapOrRedemptionFeesPaid updates the fees used for dynamic fee
// checker transactions. We do not know the exact fees the tx will use until
// they are mined, so this waits until they are mined and updates the value for
// the entire trade.
//
// NOTE: As long as init and redemption confirms add up to more than two this
// method will fire as expected before the swap is determined in Confirmed
// status. Swaps naturally require a certain number of redemption confirms
// before they are confirmed so this is currently ensured.
func (t *trackedTrade) updateDynamicSwapOrRedemptionFeesPaid(ctx context.Context, match *matchTracker, isInit bool) {
	wallet := t.wallets.fromWallet
	if !isInit {
		wallet = t.wallets.toWallet
	}
	stopChecks := func() {
		if isInit {
			match.MetaData.Proof.SwapFeeConfirmed = true
		} else {
			match.MetaData.Proof.RedemptionFeeConfirmed = true
		}
		err := t.db.UpdateOrderMetaData(t.ID(), t.metaData)
		if err != nil {
			t.dc.log.Errorf("Error updating order metadata for order %s: %v", t.ID(), err)
		}
	}
	feeChecker, dynamic := wallet.Wallet.(asset.DynamicSwapper)
	if !dynamic {
		stopChecks()
		return
	}
	txType := "swap"
	if !isInit {
		txType = "redemption"
	}
	var coinID, contractData []byte
	// Check if a swap or redeem coin id has been populated in the
	// match tracker. If it has we ask the wallet for the fees paid
	// and add that to either the total swap or redeem fees for the
	// trade.
	if isInit {
		coinID = []byte(match.MetaData.Proof.MakerSwap)
		if match.Side != order.Maker {
			coinID = []byte(match.MetaData.Proof.TakerSwap)
		}
		contractData = match.MetaData.Proof.ContractData
	} else {
		coinID = []byte(match.MetaData.Proof.MakerRedeem)
		if match.Side != order.Maker {
			coinID = []byte(match.MetaData.Proof.TakerRedeem)
		}
		contractData = match.MetaData.Proof.CounterContract
	}
	secretHash := match.MetaData.Proof.SecretHash
	if len(coinID) == 0 {
		// If there is no coin ID yet and the match was revoked, assume
		// the transaction will never happen.
		if match.MetaData.Proof.IsRevoked() {
			stopChecks()
		}
		return
	}
	checkFees := feeChecker.DynamicSwapFeesPaid
	if !isInit {
		checkFees = feeChecker.DynamicRedemptionFeesPaid
	}
	actualFees, secrets, err := checkFees(ctx, coinID, contractData)
	if err != nil {
		if errors.Is(err, asset.CoinNotFoundError) || errors.Is(err, asset.ErrNotEnoughConfirms) {
			return
		}
		t.dc.log.Errorf("Failed to determine actual %s transaction fees paid for "+
			"match %s: %v", txType, match, err)
		return
	}
	// Only add the tx fee once.
	if !bytes.Equal(secrets[0], secretHash) {
		stopChecks()
		return
	}
	if isInit {
		t.metaData.SwapFeesPaid += actualFees
	} else {
		t.metaData.RedemptionFeesPaid += actualFees
	}
	stopChecks()
	t.notify(newOrderNote(TopicOrderStatusUpdate, "", "", db.Data, t.coreOrderInternal()))
}

// isRedeemable will be true if the match is ready for our redemption to be
// broadcast.
//
// In certain situations, the match should be revoked and the return will
// indicate this. In particular, the situations are when the match is in
// TakerSwapCast on the Maker side and the taker's swap is found to be either
// spent or expired. Such matches are also not redeemable.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) isRedeemable(ctx context.Context, match *matchTracker) (ready, shouldRevoke bool) {
	// Quick status check before we bother with the wallet.
	switch match.Status {
	case order.NewlyMatched, order.MakerSwapCast:
		return false, false // all swaps not yet sent
	}

	if match.swapErr != nil || len(match.MetaData.Proof.RefundCoin) != 0 {
		t.dc.log.Tracef("Match %s not redeemable: swapErr = %v, RefundCoin = %v",
			match, match.swapErr, match.MetaData.Proof.RefundCoin)
		return false, false
	}
	if ticksGoverned, _ := match.exceptions(); ticksGoverned {
		t.dc.log.Tracef("Match %s not redeemable: ticks metered", match)
		return false, false
	}
	// NOTE: Taker must be able to redeem when revoked!  As maker, only block
	// redeem if we have determined that the counterparty swap was either spent
	// or expired, as indicated by SelfRevoked. (maybe)
	//
	// if match.Side == order.Maker && match.MetaData.Proof.SelfRevoked {
	// 	t.dc.log.Debugf("Revoked match %s not redeemable as maker.", match)
	// 	return false, false
	// }

	wallet := t.wallets.toWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually redeemables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("not checking if order %s, match %s is redeemable because %s wallet is locked or disabled",
			t.ID(), match, unbip(wallet.AssetID))
		return false, false
	}

	switch match.Status {
	case order.TakerSwapCast:
		if match.Side == order.Maker {
			// Check the confirmations on the taker's swap.
			confs, req, changed, spent, expired, err := t.counterPartyConfirms(ctx, match)
			if err != nil {
				if !errors.Is(err, asset.ErrSwapNotInitiated) {
					// We cannot get the swap data yet but there is no need
					// to log an error if swap not initiated as this is
					// expected for newly made swaps involving contracts.
					t.dc.log.Errorf("isRedeemable: %v", err)
				}
				return false, false
			}
			if spent {
				if match.MetaData.Proof.SelfRevoked {
					return false, false // already self-revoked
				}
				// Here we can check to see if this is a redeem we failed to record...
				t.dc.log.Warnf("Order %s, match %s counter-party's swap is spent before we could redeem", t.ID(), match)
				return false, true // REVOKE!
			}
			if expired {
				if match.MetaData.Proof.SelfRevoked {
					return false, false // already self-revoked
				}
				t.dc.log.Warnf("Order %s, match %s counter-party's swap expired before we could redeem", t.ID(), match)
				return false, true // REVOKE!
			}
			// NOTE: We'll redeem even if the market has vanished - taker will
			// find it. We'll keep trying to send the redeem request. If the
			// server/market never reappears, we should self-revoke and retire
			// after taker lock time has expired and server would have revoked.
			ready = confs >= req
			if changed && !ready {
				t.dc.log.Infof("Match %s not yet redeemable: current confs = %d, required confs = %d",
					match, confs, req)
			}
			return ready, false
		}

		// If we're the taker, check the confirmations anyway so we can notify.
		confs, spent, err := t.wallets.fromWallet.swapConfirmations(ctx, match.MetaData.Proof.TakerSwap,
			match.MetaData.Proof.ContractData, match.MetaData.Stamp)
		if err != nil && !errors.Is(err, asset.ErrSwapNotInitiated) {
			// No need to log an error if swap not initiated as this
			// is expected for newly made swaps involving contracts.
			t.dc.log.Errorf("isRedeemable: error getting confirmation for our own swap transaction: %v", err)
		}
		if spent {
			t.dc.log.Debugf("Our (taker) swap for match %s is being reported as spent, "+
				"but we have not seen the counter-party's redemption yet. This could just"+
				" be network latency.", match)
			// Record this time if this is the first time we have observed that
			// it's spent.
			match.swapSpent()
		}
		match.setSwapConfirms(int64(confs))
		t.notify(newMatchNote(TopicConfirms, "", "", db.Data, t, match))
		return false, false

	case order.MakerRedeemed:
		return match.Side == order.Taker, false
	}

	return false, false
}

// isRefundable will be true if all of the following are true:
//   - We have broadcasted a swap contract (matchProof.ContractData != nil).
//   - Neither party has redeemed (matchStatus < order.MakerRedeemed).
//     For Maker, this means we've not redeemed. For Taker, this means we've
//     not been notified of / we haven't yet found the Maker's redeem.
//   - Our swap's locktime has expired.
//
// Those checks are skipped and isRefundable is false if we've already
// executed a refund or our refund-to wallet is locked.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) isRefundable(ctx context.Context, match *matchTracker) bool {
	if match.refundErr != nil || len(match.MetaData.Proof.RefundCoin) != 0 {
		t.dc.log.Tracef("Match %s not refundable: refundErr = %v, RefundCoin = %v",
			match, match.refundErr, match.MetaData.Proof.RefundCoin)
		return false
	}

	wallet := t.wallets.fromWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually refundables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("not checking if order %s, match %s is refundable because %s wallet is locked or disabled",
			t.ID(), match, unbip(wallet.AssetID))
		return false
	}

	// Return if we've NOT sent a swap OR a redeem has been
	// executed by either party.
	if len(match.MetaData.Proof.ContractData) == 0 || match.Status >= order.MakerRedeemed {
		return false
	}

	// Issue a refund if our swap's locktime has expired.
	swapLocktimeExpired, contractExpiry, err := wallet.ContractLockTimeExpired(ctx, match.MetaData.Proof.ContractData)
	if err != nil {
		if !errors.Is(err, asset.ErrSwapNotInitiated) {
			// No need to log an error as this is expected for newly
			// made swaps involving contracts.
			t.dc.log.Errorf("error checking if locktime has expired for %s contract on order %s, match %s: %v",
				match.Side, t.ID(), match, err)
		}
		return false
	}

	if swapLocktimeExpired {
		return true
	}

	// Log contract expiry info on intervals: hourly when not expired, otherwise
	// every 5 minutes until the refund occurs.
	expiresIn := time.Until(contractExpiry) // may be negative
	logInterval := time.Hour
	if expiresIn <= 0 {
		logInterval = 5 * time.Minute
	}
	if !match.setExpireDur(expiresIn, logInterval) {
		return false // too recently logged
	}

	swapCoinID := match.MetaData.Proof.TakerSwap
	if match.Side == order.Maker {
		swapCoinID = match.MetaData.Proof.MakerSwap
	}
	symbol, assetID := t.wallets.fromWallet.Symbol, t.wallets.fromWallet.AssetID
	remainingTime := expiresIn.Round(time.Second)
	assetSymb := strings.ToUpper(symbol)
	var expireDetails string
	if remainingTime > 0 {
		expireDetails = fmt.Sprintf("expires at %v (%v).", contractExpiry, remainingTime)
	} else {
		expireDetails = fmt.Sprintf("expired %v ago, but additional blocks are required by the %s network.",
			-remainingTime, assetSymb)
	}
	t.dc.log.Infof("Contract for match %s with swap coin %v (%s) %s",
		match, coinIDString(assetID, swapCoinID), assetSymb, expireDetails)

	return false
}

// shouldBeginFindRedemption will be true if we are the Taker on this match,
// we've broadcasted a swap, our swap has gotten the required confs, we've not
// refunded our swap, and either the match was revoked (without receiving a
// valid notification of Maker's redeem) or the match is self-governed and it
// has been a while since our swap was spent. The revoked status is provided as
// in input since it may not be flagged as revoked in the MatchProof yet.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) shouldBeginFindRedemption(ctx context.Context, match *matchTracker, revoked bool) bool {
	proof := &match.MetaData.Proof // revoked flags may not be updated yet, so we use an input arg
	swapCoinID := proof.TakerSwap
	if match.Side != order.Taker || len(swapCoinID) == 0 || len(proof.MakerRedeem) > 0 || len(proof.RefundCoin) > 0 {
		// t.dc.log.Tracef(
		// 	"Not finding redemption for match %s: side = %s, swapErr = %v, TakerSwap = %v RefundCoin = %v",
		// 	match, match.Side, match.swapErr, proof.TakerSwap, proof.RefundCoin)
		return false
	}
	// We are taker and have published our contract, there is no known maker
	// redeem, and we have not refunded. We may want to search for a maker
	// redeem if this match is revoked or our swap has been spent some time ago.
	if match.cancelRedemptionSearch != nil { // already finding redemption
		return false
	}

	confs, spent, err := t.wallets.fromWallet.swapConfirmations(ctx, swapCoinID, proof.ContractData, match.MetaData.Stamp)
	if err != nil {
		if !errors.Is(err, asset.ErrSwapNotInitiated) {
			// No need to log an error if swap not initiated as this
			// is expected for newly made swaps involving contracts.
			t.dc.log.Errorf("Failed to get confirmations of the taker's swap %s (%s) for match %s, order %v: %v",
				coinIDString(t.wallets.fromWallet.AssetID, swapCoinID), t.wallets.fromWallet.Symbol, match, t.UID(), err)
		}
		return false
	}
	if spent {
		match.swapSpent() // noted.
		// NOTE: spent may not be accurate for SPV wallet (false negative), so
		// this should not be a requirement. (specifically... 0-conf?)
		t.dc.log.Infof("Swap contract for match %s, order %s is spent. "+
			"Search for counterparty redemption may begin soon.", match, t.ID())
	}
	if revoked { // no delays if it's revoked
		return spent || confs >= t.metaData.FromSwapConf
	}
	// Even if not revoked, go find that redeem if it was spent a while ago.
	return spent && match.swapSpentAgo() > t.spentAgoThresh()
}

// shouldConfirmRedemption will return true if a redemption transaction
// has been broadcast, but it has not yet been confirmed.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func shouldConfirmRedemption(match *matchTracker) bool {
	if match.Status == order.MatchConfirmed {
		return false
	}

	if (match.Side == order.Maker && match.Status < order.MakerRedeemed) ||
		(match.Side == order.Taker && match.Status < order.MatchComplete) {
		return false
	}

	if match.redemptionRejected {
		return false
	}

	proof := &match.MetaData.Proof
	if match.Side == order.Maker {
		return len(proof.MakerRedeem) > 0
	}
	return len(proof.TakerRedeem) > 0
}

// shouldConfirmRefund will return true if a refund transaction has been
// broadcast, but it has not yet been confirmed.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func shouldConfirmRefund(match *matchTracker) bool {
	if match.Status == order.MatchConfirmed {
		return false
	}

	if match.refundRejected {
		return false
	}

	return len(match.MetaData.Proof.RefundCoin) > 0
}

// tick will check for and perform any match actions necessary.
func (c *Core) tick(t *trackedTrade) (assetMap, error) {
	assets := make(assetMap) // callers expect non-nil map even on error :(

	tStart := time.Now()
	var tLock time.Duration
	defer func() {
		if eTime := time.Since(tStart); eTime > 500*time.Millisecond {
			c.log.Debugf("Slow tick: trade %v processed in %v, blocked for %v",
				t.ID(), eTime, tLock)
		}
	}()

	// Another tick may be running for this trade. We have a mutex just for this
	// so we don't have to write-lock t.mtx.Lock, which would block many other
	// actions such as isActive. We can't just run concurrent checks since the
	// results may become inaccurate when/if the other goroutine begins acting,
	// and we MUST NOT take the same action twice.
	t.tickLock.Lock()
	defer t.tickLock.Unlock()
	tLock = time.Since(tStart)

	var swaps, redeems, refunds, revokes, searches, redemptionConfirms,
		refundConfirms, dynamicSwapFeeConfirms, dynamicRedemptionFeeConfirms []*matchTracker
	var sent, quoteSent, received, quoteReceived uint64

	checkMatch := func(match *matchTracker) error { // only errors on context.DeadlineExceeded or context.Canceled
		side := match.Side
		if match.Status == order.MatchConfirmed {
			return nil
		}
		if match.Address == "" {
			return nil // a cancel order match
		}
		if !t.matchIsActive(match) {
			return nil // either refunded or revoked requiring no action on this side of the match
		}

		// Inform shouldBeginFindRedemption without modifying the MatchProof.
		revoked := match.MetaData.Proof.IsRevoked()

		// The trackedTrade mutex is locked, so we must not hang forever. Give
		// this a generous timeout because it may be necessary to retrieve full
		// blocks, and catch timeout/shutdown after each check. Individual
		// requests can have shorter timeouts of their own. This is cumulative.
		ctx, cancel := context.WithTimeout(c.ctx, 40*time.Second)
		defer cancel()

		ok, revoke := t.isSwappable(ctx, match) // rejects revoked matches
		if ok {
			c.log.Debugf("Swappable match %s for order %v (%v)", match, t.ID(), side)
			swaps = append(swaps, match)
			sent += match.Quantity
			quoteSent += calc.BaseToQuote(match.Rate, match.Quantity)
			return nil
		}
		if revoke {
			revokes = append(revokes, match) // may still need refund/redeem, continue
			revoked = true
		}
		if ctx.Err() != nil { // may be here because of timeout or shutdown
			return ctx.Err()
		}

		if t.checkSwapFeeConfirms(match) {
			dynamicSwapFeeConfirms = append(dynamicSwapFeeConfirms, match)
		}

		ok, revoke = t.isRedeemable(ctx, match) // does not reject revoked matches
		if ok {
			c.log.Debugf("Redeemable match %s for order %v (%v)", match, t.ID(), side)
			redeems = append(redeems, match)
			received += match.Quantity
			quoteReceived += calc.BaseToQuote(match.Rate, match.Quantity)
			return nil
		}
		if revoke {
			revokes = append(revokes, match) // may still need refund/redeem, continue
			revoked = true
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if t.checkRedemptionFeeConfirms(match) {
			dynamicRedemptionFeeConfirms = append(dynamicRedemptionFeeConfirms, match)
		}

		// Check refundability before checking if to start finding redemption.
		// Ensures that redemption search is not started if locktime has expired.
		// If we've already started redemption search for this match, the search
		// will be aborted if/when auto-refund succeeds.
		if t.isRefundable(ctx, match) { // does not matter if revoked
			c.log.Debugf("Refundable match %s for order %v (%v)", match, t.ID(), side)
			refunds = append(refunds, match)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if t.shouldBeginFindRedemption(ctx, match, revoked /* consider new pending self-revoke */) {
			c.log.Debugf("Ready to find counter-party redemption for match %s, order %v (%v)", match, t.ID(), side)
			searches = append(searches, match)
			return nil
		}

		if shouldConfirmRedemption(match) {
			redemptionConfirms = append(redemptionConfirms, match)
			return nil
		}

		if shouldConfirmRefund(match) {
			refundConfirms = append(refundConfirms, match)
			return nil
		}

		// For certain "self-governed" trades where the market or server has
		// vanished, we should revoke the match to allow it to retire without
		// having sent any pending redeem requests. Note that self-governed is
		// not necessarily a permanent state, so we delay this action.
		if !revoked && t.isSelfGoverned() && time.Since(match.matchTime()) > t.lockTimeTaker {
			c.log.Warnf("Revoking old self-governed match %v for market %v, host %v.",
				match, t.mktID, t.dc.acct.host)
			revokes = append(revokes, match)
			// NOTE: If the trade is in booked status, the order still won't
			// retire. We need a way to force-cancel such orders.
		}

		return ctx.Err()
	}

	c.loginMtx.Lock()
	loggedIn := c.loggedIn
	c.loginMtx.Unlock()

	// Begin checks under read-only lock.
	t.mtx.RLock()

	// Make sure we have a redemption and refund fee suggestion cached.
	t.cacheFeeSuggestions()

	if !t.readyToTick {
		t.mtx.RUnlock()
		return assets, nil
	}

	// Check all matches for and resend pending requests as necessary.
	// It's possible we're not logged in if we receive a tipChange
	// notification before we connect to dex servers.
	if loggedIn {
		c.resendPendingRequests(t)
	}

	// Check all matches and then swap, redeem, or refund as necessary.
	var err error
	for _, match := range t.matches {
		if err = checkMatch(match); err != nil {
			break
		}
	}

	rmCancel := t.hasStaleCancelOrder()

	// End checks under read-only lock.
	t.mtx.RUnlock()

	if err != nil {
		if len(revokes) != 0 {
			// Still flag any "should revoke"s for IsRevoked() and to fast track
			// the next tick. NOTE: See the TODO below regarding revokeMatch.
			t.mtx.Lock()
			defer t.mtx.Unlock()
			for _, rm := range revokes {
				rm.MetaData.Proof.SelfRevoked = true
			}
		}
		return assets, err
	}

	if len(swaps) > 0 || len(refunds) > 0 {
		assets.count(t.wallets.fromWallet.AssetID)
	}
	if len(redeems) > 0 {
		assets.count(t.wallets.toWallet.AssetID)
		assets.count(t.wallets.fromWallet.AssetID) // update ContractLocked balance
	}

	if !rmCancel && len(swaps) == 0 && len(refunds) == 0 && len(redeems) == 0 &&
		len(revokes) == 0 && len(searches) == 0 && len(redemptionConfirms) == 0 &&
		len(refundConfirms) == 0 && len(dynamicSwapFeeConfirms) == 0 &&
		len(dynamicRedemptionFeeConfirms) == 0 {
		return assets, nil // nothing to do, don't acquire the write-lock
	}

	// Wallet requests below may still hang if there are no internal timeouts.
	// We should consider giving each asset.Wallet method a context arg.
	// However, if the requests in the checks above just succeeded, the wallets
	// are likely to be responsive below.

	// Take the actions that will modify the match.
	errs := newErrorSet("%s tick: ", t.dc.acct.host)
	t.mtx.Lock()
	defer t.mtx.Unlock()

	if rmCancel {
		t.deleteStaleCancelOrder()
	}

	for _, match := range revokes {
		match.MetaData.Proof.SelfRevoked = true
		// TODO: maybe revokeMatch() instead of just setting the flag? If this
		// match is in refunds or redeems (or a redemption search is running),
		// the match will still be updated after those actions are taken.
		// Otherwise, we'll be waiting for a revokeMatch call from either
		// handleRevokeMatchMsg or resolveMatchConflicts (on reconnect).
	}

	if len(swaps) > 0 {
		didUnlock, err := t.wallets.fromWallet.refreshUnlock()
		if err != nil { // Just log it and try anyway.
			c.log.Errorf("refreshUnlock error swapping %s: %v", t.wallets.fromWallet.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet to send a swap", t.wallets.fromWallet.Symbol)
		}
		qty := sent
		if !t.Trade().Sell {
			qty = quoteSent
		}
		err = c.swapMatches(t, swaps)
		corder := t.coreOrderInternal() // after swapMatches modifies matches
		ui := t.wallets.fromWallet.Info().UnitInfo
		if err != nil {
			errs.addErr(err)
			subject, details := c.formatDetails(TopicSwapSendError, ui.ConventionalString(qty), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicSwapSendError, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicSwapsInitiated, ui.ConventionalString(qty), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicSwapsInitiated, subject, details, db.Poke, corder))
		}
	}

	if len(redeems) > 0 {
		didUnlock, err := t.wallets.toWallet.refreshUnlock()
		if err != nil { // Just log it and try anyway.
			c.log.Errorf("refreshUnlock error redeeming %s: %v", t.wallets.toWallet.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet to send a redemption", t.wallets.toWallet.Symbol)
		}
		qty := received
		if t.Trade().Sell {
			qty = quoteReceived
		}
		err = c.redeemMatches(t, redeems)
		corder := t.coreOrderInternal()
		ui := t.wallets.toWallet.Info().UnitInfo
		if err != nil {
			errs.addErr(err)
			subject, details := c.formatDetails(TopicRedemptionError,
				ui.ConventionalString(qty), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicRedemptionError, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicMatchComplete,
				ui.ConventionalString(qty), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicMatchComplete, subject, details, db.Poke, corder))
		}
	}

	if len(refunds) > 0 {
		didUnlock, err := t.wallets.fromWallet.refreshUnlock()
		if err != nil { // Just log it and try anyway.
			c.log.Errorf("refreshUnlock error refunding %s: %v", t.wallets.fromWallet.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet while sending a refund", t.wallets.fromWallet.Symbol)
		}
		refunded, err := c.refundMatches(t, refunds)
		corder := t.coreOrderInternal()
		ui := t.wallets.fromWallet.Info().UnitInfo
		if err != nil {
			errs.addErr(err)
			subject, details := c.formatDetails(TopicRefundFailure,
				ui.ConventionalString(refunded), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicRefundFailure, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicMatchesRefunded,
				ui.ConventionalString(refunded), ui.Conventional.Unit, makeOrderToken(t.token()))
			t.notify(newOrderNote(TopicMatchesRefunded, subject, details, db.WarningLevel, corder))
		}
	}

	if len(searches) > 0 {
		for _, match := range searches {
			t.findMakersRedemption(c.ctx, match) // async search, just set cancelRedemptionSearch
		}
	}

	if len(redemptionConfirms) > 0 {
		c.confirmRedemptions(t, redemptionConfirms)
	}

	if len(refundConfirms) > 0 {
		for _, match := range refundConfirms {
			if _, err := c.confirmRefund(t, match); err != nil {
				t.dc.log.Errorf("Unable to confirm refund: %v", err)
			}
		}
	}

	for _, match := range dynamicSwapFeeConfirms {
		t.updateDynamicSwapOrRedemptionFeesPaid(c.ctx, match, true)
	}

	for _, match := range dynamicRedemptionFeeConfirms {
		t.updateDynamicSwapOrRedemptionFeesPaid(c.ctx, match, false)
	}

	return assets, errs.ifAny()
}

// resendPendingRequests checks all matches for this order to re-attempt
// sending the `init` or `redeem` request where necessary.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (c *Core) resendPendingRequests(t *trackedTrade) {
	if t.isSelfGoverned() {
		return
	}

	for _, match := range t.matches {
		proof, auth := &match.MetaData.Proof, &match.MetaData.Proof.Auth
		// Do not resend pending requests for revoked matches.
		// Matches where we've refunded our swap or we auto-redeemed maker's
		// swap will be set to revoked and will be skipped as well.
		if match.swapErr != nil || proof.IsRevoked() {
			continue
		}
		side, status := match.Side, match.Status
		var swapCoinID, redeemCoinID []byte
		switch {
		case side == order.Maker && status == order.MakerSwapCast:
			swapCoinID = proof.MakerSwap
		case side == order.Taker && status == order.TakerSwapCast:
			swapCoinID = proof.TakerSwap
		case side == order.Maker && status >= order.MakerRedeemed:
			redeemCoinID = proof.MakerRedeem
		case side == order.Taker && status >= order.MatchComplete:
			redeemCoinID = proof.TakerRedeem
		}
		if len(swapCoinID) != 0 && len(auth.InitSig) == 0 { // resend pending `init` request
			c.sendInitAsync(t, match, swapCoinID, proof.ContractData)
		} else if len(redeemCoinID) != 0 && len(auth.RedeemSig) == 0 { // resend pending `redeem` request
			c.sendRedeemAsync(t, match, redeemCoinID, proof.Secret)
		}
	}
}

// revoke sets the trade status to Revoked, either because the market is
// suspended with persist=false or because the order is revoked and unbooked
// by the server.
// Funding coins or change coin will be returned IF there are no matches that
// MAY later require sending swaps.
func (t *trackedTrade) revoke() {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	if t.metaData.Status >= order.OrderStatusExecuted {
		// Executed, canceled or already revoked orders cannot be (re)revoked.
		t.dc.log.Errorf("revoke() wrongly called for order %v, status %s", t.ID(), t.metaData.Status)
		return
	}

	t.dc.log.Warnf("Revoking order %v", t.ID())

	metaOrder := t.metaOrder()
	metaOrder.MetaData.Status = order.OrderStatusRevoked
	err := t.db.UpdateOrder(metaOrder)
	if err != nil {
		t.dc.log.Errorf("unable to update order: %v", err)
	}

	// Return coins if there are no matches that MAY later require sending swaps.
	t.maybeReturnCoins()

	if t.isMarketBuy() { // Is this even possible?
		t.unlockRedemptionFraction(1, 1)
		t.unlockRefundFraction(1, 1)
	} else {
		t.unlockRedemptionFraction(t.Trade().Remaining(), t.Trade().Quantity)
		t.unlockRefundFraction(t.Trade().Remaining(), t.Trade().Quantity)
	}
}

// revokeMatch sets the status as revoked for the specified match, emits an
// Order note with TopicMatchRevoked, returns any unneeded funding coins, and
// unlocks and reserves for refunds and redeems (for AccountLocker wallet
// types like eth). revokeMatch must be called with the mtx write-locked.
func (t *trackedTrade) revokeMatch(matchID order.MatchID, fromServer bool) error {
	var revokedMatch *matchTracker
	for _, match := range t.matches {
		if match.MatchID == matchID {
			revokedMatch = match
			break
		}
	}
	if revokedMatch == nil {
		return fmt.Errorf("no match found with id %s for order %v", matchID, t.ID())
	}

	// Set the match as revoked.
	if fromServer {
		revokedMatch.MetaData.Proof.ServerRevoked = true
	} else {
		revokedMatch.MetaData.Proof.SelfRevoked = true
	}
	err := t.db.UpdateMatch(&revokedMatch.MetaMatch)
	if err != nil {
		t.dc.log.Errorf("db update error for revoked match %v, order %v: %v", matchID, t.ID(), err)
	}

	// Notify the user of the failed match.
	corder := t.coreOrderInternal() // no cancel order
	subject, details := t.formatDetails(TopicMatchRevoked, token(matchID[:]))
	t.notify(newOrderNote(TopicMatchRevoked, subject, details, db.WarningLevel, corder))

	// Unlock coins if we're not expecting future matches for this trade and
	// there are no matches that MAY later require sending swaps.
	t.maybeReturnCoins()

	// Return unused and unneeded redemption reserves.
	if (revokedMatch.Side == order.Taker && (revokedMatch.Status < order.TakerSwapCast)) ||
		(revokedMatch.Side == order.Maker && revokedMatch.Status < order.MakerSwapCast) {

		if t.isMarketBuy() {
			t.unlockRedemptionFraction(1, uint64(len(t.matches)))
			t.unlockRefundFraction(1, uint64(len(t.matches)))
		} else {
			t.unlockRedemptionFraction(revokedMatch.Quantity, t.Trade().Quantity)
			t.unlockRefundFraction(revokedMatch.Quantity, t.Trade().Quantity)
		}
	}

	t.dc.log.Warnf("Match %v revoked in status %v for order %v", matchID, revokedMatch.Status, t.ID())
	return nil
}

// swapMatches will send a transaction with swaps for the specified matches.
// The matches will be de-grouped so that matches marked as suspect are swapped
// individually and separate from the non-suspect group.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) swapMatches(t *trackedTrade, matches []*matchTracker) (err error) {
	errs := newErrorSet("swapMatches order %s - ", t.ID())
	groupables := make([]*matchTracker, 0, len(matches)) // Over-allocating if there are suspect matches
	var suspects []*matchTracker
	for _, m := range matches {
		if m.suspectSwap {
			suspects = append(suspects, m)
		} else {
			groupables = append(groupables, m)
		}
	}
	feeRate := t.bestSwapGroupFeeRate(matches)
	if len(groupables) > 0 {
		var maxSwapsInTx int
		if counter, is := t.wallets.fromWallet.Wallet.(asset.MaxMatchesCounter); is {
			if maxSwapsInTx, err = counter.MaxSwaps(t.metaData.FromVersion, feeRate); err != nil {
				t.dc.log.Meter("failed_swap_count", time.Minute*10).Warn("Failed to count swap txs: %v", err)
			}
		}
		if maxSwapsInTx <= 0 || len(groupables) < maxSwapsInTx {
			c.swapMatchGroup(t, groupables, feeRate, errs)
		} else {
			for i := 0; i < len(groupables); i += maxSwapsInTx {
				if i+maxSwapsInTx < len(groupables) {
					c.swapMatchGroup(t, groupables[i:i+maxSwapsInTx], feeRate, errs)
				} else {
					c.swapMatchGroup(t, groupables[i:], feeRate, errs)
				}
			}
		}
	}
	for _, m := range suspects {
		c.swapMatchGroup(t, []*matchTracker{m}, feeRate, errs)
	}
	return errs.ifAny()
}

// bestSwapGroupRate gets the most appropriate fee rate for a group of swaps.
func (t *trackedTrade) bestSwapGroupFeeRate(matches []*matchTracker) uint64 {
	var highestFeeRate uint64
	for _, match := range matches {
		if match.FeeRateSwap > highestFeeRate {
			highestFeeRate = match.FeeRateSwap
		}
	}
	// Use a higher swap fee rate if a local estimate is higher than the
	// prescribed rate, but not higher than the funded (max) rate.
	if highestFeeRate < t.metaData.MaxFeeRate {
		freshRate := t.wallets.fromWallet.feeRate()
		if freshRate == 0 { // either not a FeeRater, or FeeRate failed
			freshRate = t.dc.bestBookFeeSuggestion(t.wallets.fromWallet.AssetID)
		}
		if freshRate > t.metaData.MaxFeeRate {
			freshRate = t.metaData.MaxFeeRate
		}
		if highestFeeRate < freshRate {
			t.dc.log.Infof("Prescribed %v fee rate %v looks low, using %v",
				t.wallets.fromWallet.Symbol, highestFeeRate, freshRate)
			highestFeeRate = freshRate
		}
	}
	return highestFeeRate
}

// swapMatchGroup will send a transaction with swap outputs for the specified
// matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) swapMatchGroup(t *trackedTrade, matches []*matchTracker, highestFeeRate uint64, errs *errorSet) {
	// Ensure swap is not sent with a zero fee rate.
	if highestFeeRate == 0 {
		errs.add("swap cannot proceed with a zero fee rate")
		return
	}

	// Prepare the asset.Contracts.
	contracts := make([]*asset.Contract, len(matches))
	// These matches may have different fee rates, matched in different epochs.

	for i, match := range matches {
		value := match.Quantity
		if !match.trade.Sell {
			value = calc.BaseToQuote(match.Rate, match.Quantity)
		}
		matchTime := match.matchTime()
		lockTime := matchTime.Add(t.lockTimeTaker).UTC().Unix()
		if match.Side == order.Maker {
			match.MetaData.Proof.Secret = encode.RandomBytes(32)
			secretHash := sha256.Sum256(match.MetaData.Proof.Secret)
			match.MetaData.Proof.SecretHash = secretHash[:]
			lockTime = matchTime.Add(t.lockTimeMaker).UTC().Unix()
		}

		contracts[i] = &asset.Contract{
			Address:    match.Address,
			Value:      value,
			SecretHash: match.MetaData.Proof.SecretHash,
			LockTime:   uint64(lockTime),
		}
	}

	lockChange := true
	// If the order is executed, canceled or revoked, and these are the last
	// swaps, then we don't need to lock the change coin.
	if t.metaData.Status > order.OrderStatusBooked {
		var matchesRequiringSwaps int
		for _, match := range t.matches {
			if match.MetaData.Proof.IsRevoked() {
				// Revoked matches don't require swaps.
				continue
			}
			if (match.Side == order.Maker && match.Status < order.MakerSwapCast) ||
				(match.Side == order.Taker && match.Status < order.TakerSwapCast) {
				matchesRequiringSwaps++
			}
		}
		if len(matches) == matchesRequiringSwaps { // last swaps
			lockChange = false
		}
	}

	// Fund the swap. If this isn't the first swap, use the change coin from the
	// previous swaps.
	fromWallet := t.wallets.fromWallet
	coinIDs := t.Trade().Coins
	if len(t.metaData.ChangeCoin) > 0 {
		coinIDs = []order.CoinID{t.metaData.ChangeCoin}
		c.log.Debugf("Using stored change coin %v (%v) for order %v matches",
			coinIDString(fromWallet.AssetID, coinIDs[0]), fromWallet.Symbol, t.ID())
	}

	inputs := make([]asset.Coin, len(coinIDs))
	for i, coinID := range coinIDs {
		coin, found := t.coins[hex.EncodeToString(coinID)]
		if !found {
			errs.add("%s coin %s not found", fromWallet.Symbol, coinIDString(fromWallet.AssetID, coinID))
			return
		}
		inputs[i] = coin
	}

	if t.dc.IsDown() {
		errs.add("not broadcasting swap while DEX %s connection is down (could be revoked)", t.dc.acct.host)
		return
	}

	// swapMatches is no longer idempotent after this point.

	// Send the swap. If the swap fails, set the swapErr flag for all matches.
	// A more sophisticated solution might involve tracking the error time too
	// and trying again in certain circumstances.
	swaps := &asset.Swaps{
		AssetVersion: t.metaData.FromVersion,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      highestFeeRate,
		LockChange:   lockChange,
		Options:      t.options,
	}
	receipts, change, fees, err := fromWallet.Swap(swaps)
	if err != nil {
		bTimeout, tickInterval := t.broadcastTimeout(), t.dc.ticker.Dur() // bTimeout / tickCheckInterval
		for _, match := range matches {
			// Mark the matches as suspect to prevent them being grouped again.
			match.suspectSwap = true
			match.swapErrCount++
			// If we can still swap before the broadcast timeout, allow retries
			// soon.
			auditStamp := match.MetaData.Proof.Auth.AuditStamp
			lastActionTime := match.matchTime()
			if match.Side == order.Taker {
				// It is possible that AuditStamp could be zero if we're
				// recovering during startup or after a DEX reconnect. In that
				// case, allow three retries before giving up.
				lastActionTime = time.UnixMilli(int64(auditStamp))
			}
			if time.Since(lastActionTime) < bTimeout ||
				(auditStamp == 0 && match.swapErrCount < tickCheckDivisions) {
				match.delayTicks(tickInterval * 3 / 4)
			} else {
				// If we can't get a swap out before the broadcast timeout, just
				// quit. We could also self-revoke here, but we're also
				// expecting a revocation from the server, so relying on that
				// one for now.
				match.swapErr = err
			}
		}
		errs.add("error sending %s swap transaction: %v", fromWallet.Symbol, err)
		return
	}

	refundTxs := ""
	for i, r := range receipts {
		rawRefund := r.SignedRefund()
		if len(rawRefund) == 0 { // e.g. eth
			continue // in case others are not empty for some reason
		}
		refundTxs = fmt.Sprintf("%s%q: %s", refundTxs, r.Coin(), rawRefund)
		if i != len(receipts)-1 {
			refundTxs = fmt.Sprintf("%s, ", refundTxs)
		}
	}

	// Log the swap receipts. It is important to print the receipts as a
	// Stringer to provide important data, such as the secret hash and contract
	// address with ETH since it allows manually refunding.
	c.log.Infof("Broadcasted transaction with %d swap contracts for order %v. "+
		"Assigned fee rate = %d. Receipts (%s): %v.",
		len(receipts), t.ID(), swaps.FeeRate, fromWallet.Symbol, receipts)
	if refundTxs != "" {
		c.log.Infof("The following are contract identifiers mapped to raw refund "+
			"transactions that are only valid after the swap contract expires. "+
			"These are fallback transactions that can be used to return funds "+
			"to your wallet in the case the wallet software no longer functions. They should "+
			"NOT be used if Bison Wallet is operable. The wallet will refund failed "+
			"contracts automatically.\nRefund Txs: {%s}", refundTxs)
	}

	// If this is the first swap (and even if not), the funding coins
	// would have been spent and unlocked.
	t.coinsLocked = false
	t.changeLocked = lockChange
	if _, dynamic := fromWallet.Wallet.(asset.DynamicSwapper); !dynamic {
		t.metaData.SwapFeesPaid += fees // dynamic tx wallets don't know the fees paid until mining
	}

	if change == nil {
		t.metaData.ChangeCoin = nil
	} else {
		cid := change.ID()
		if rc, is := change.(asset.RecoveryCoin); is {
			cid = rc.RecoveryID()
		}
		t.coins[cid.String()] = change
		t.metaData.ChangeCoin = []byte(cid)
		c.log.Debugf("Saving change coin %v (%v) to DB for order %v",
			coinIDString(fromWallet.AssetID, t.metaData.ChangeCoin), fromWallet.Symbol, t.ID())
	}
	t.change = change
	err = t.db.UpdateOrderMetaData(t.ID(), t.metaData)
	if err != nil {
		c.log.Errorf("Error updating order metadata for order %s: %v", t.ID(), err)
	}

	// Process the swap for each match by updating the match with swap
	// details and sending the `init` request to the DEX.
	// Saving the swap details now makes it possible to resend the `init`
	// request at a later time if sending it now fails OR to refund the
	// swap after locktime expires if the trade does not progress as expected.
	for i, receipt := range receipts {
		match := matches[i]
		coin := receipt.Coin()
		c.log.Infof("Contract coin %v (%s), value = %d, refundable at %v (receipt = %v), match = %v",
			coin, fromWallet.Symbol, coin.Value(), receipt.Expiration(), receipt.String(), match)
		if secret := match.MetaData.Proof.Secret; len(secret) > 0 {
			c.log.Tracef("Contract coin %v secret = %x", coin, secret)
		}

		// Update the match db data with the swap details before attempting
		// to notify the server of the swap.
		proof := &match.MetaData.Proof
		contract, coinID := receipt.Contract(), []byte(coin.ID())
		// NOTE: receipt.Contract() uniquely identifies this swap. Only the
		// asset backend can decode this information, which may be a redeem
		// script with UTXO assets, or a secret hash + contract version for
		// contracts on account-based assets.
		proof.ContractData = contract
		if match.Side == order.Taker {
			proof.TakerSwap = coinID
			match.Status = order.TakerSwapCast
		} else {
			proof.MakerSwap = coinID
			match.Status = order.MakerSwapCast
		}

		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			errs.add("error storing swap details in database for match %s, coin %s: %v",
				match, coinIDString(fromWallet.AssetID, coinID), err)
		}

		c.sendInitAsync(t, match, coin.ID(), contract)
	}
}

// sendInitAsync starts a goroutine to send an `init` request for the specified
// match and save the server's ack sig to db. Sends a notification if an error
// occurs while sending the request or validating the server's response.
func (c *Core) sendInitAsync(t *trackedTrade, match *matchTracker, coinID, contract []byte) {
	if !atomic.CompareAndSwapUint32(&match.sendingInitAsync, 0, 1) {
		return
	}

	c.log.Debugf("Notifying DEX %s of our %s swap contract %v for match %s",
		t.dc.acct.host, t.wallets.fromWallet.Symbol, coinIDString(t.wallets.fromWallet.AssetID, coinID), match)

	// Send the init request asynchronously.
	c.wg.Add(1) // So Core does not shut down until we're done with this request.
	go func() {
		defer c.wg.Done() // bottom of the stack
		var err error
		defer func() {
			atomic.StoreUint32(&match.sendingInitAsync, 0)
			if err != nil {
				corder := t.coreOrder()
				subject, details := c.formatDetails(TopicInitError, match, err)
				t.notify(newOrderNote(TopicInitError, subject, details, db.ErrorLevel, corder))
			}
		}()

		ack := new(msgjson.Acknowledgement)
		init := &msgjson.Init{
			OrderID:  t.ID().Bytes(),
			MatchID:  match.MatchID[:],
			CoinID:   coinID,
			Contract: contract,
		}
		// The DEX may wait up to its configured broadcast timeout, but we will
		// retry on timeout or other error.
		timeout := t.broadcastTimeout() / 4
		if timeout < time.Minute { // sane minimum, or if we lack server config for any reason
			// Send would fail right away anyway if the server is really down,
			// but at least attempt it with a non-zero timeout.
			timeout = time.Minute
		}
		err = t.dc.signAndRequest(init, msgjson.InitRoute, ack, timeout)
		if err != nil {
			var msgErr *msgjson.Error
			if errors.As(err, &msgErr) {
				if msgErr.Code == msgjson.SettlementSequenceError {
					c.log.Errorf("Starting match status resolution for 'init' request SettlementSequenceError")
					c.resolveMatchConflicts(t.dc, map[order.OrderID]*matchStatusConflict{
						t.ID(): {
							trade:   t,
							matches: []*matchTracker{match},
						},
					})
				} else if msgErr.Code == msgjson.RPCUnknownMatch {
					t.mtx.Lock()
					oid := t.ID()
					c.log.Warnf("DEX %s did not report active match %s on order %s - assuming revoked, status %v.",
						t.dc.acct.host, match, oid, match.Status)
					// We must have missed the revoke notification. Flag to allow recovery
					// and subsequent retirement of the match and parent trade.
					match.MetaData.Proof.SelfRevoked = true
					if err := c.db.UpdateMatch(&match.MetaMatch); err != nil {
						c.log.Errorf("Failed to update missing/revoked match: %v", err)
					}
					t.mtx.Unlock()
					numMissing := 1
					subject, details := c.formatDetails(TopicMissingMatches,
						numMissing, makeOrderToken(t.token()), t.dc.acct.host)
					c.notify(newOrderNote(TopicMissingMatches, subject, details, db.ErrorLevel, t.coreOrderInternal()))
				}
			}
			err = fmt.Errorf("error sending 'init' message: %w", err)
			return
		}

		// Validate server ack.
		err = t.dc.acct.checkSig(init.Serialize(), ack.Sig)
		if err != nil {
			err = fmt.Errorf("'init' ack signature error: %v", err)
			return
		}

		c.log.Debugf("Received valid ack for 'init' request for match %s", match)

		// Save init ack sig.
		t.mtx.Lock()
		auth := &match.MetaData.Proof.Auth
		auth.InitSig = ack.Sig
		auth.InitStamp = uint64(time.Now().UnixMilli())
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			err = fmt.Errorf("error storing init ack sig in database: %v", err)
		}
		t.mtx.Unlock()
	}()
}

// redeemMatches will send a transaction redeeming the specified matches.
// The matches will be de-grouped so that matches marked as suspect are redeemed
// individually and separate from the non-suspect group.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) redeemMatches(t *trackedTrade, matches []*matchTracker) (err error) {
	errs := newErrorSet("redeemMatches order %s - ", t.ID())
	groupables := make([]*matchTracker, 0, len(matches)) // Over-allocating if there are suspect matches
	var suspects []*matchTracker
	for _, m := range matches {
		if m.suspectRedeem {
			suspects = append(suspects, m)
		} else {
			groupables = append(groupables, m)
		}
	}
	if len(groupables) > 0 {
		if !t.wallets.toWallet.connected() {
			return errWalletNotConnected // don't ungroup, just return
		}
		var maxRedeemsInTx int
		if counter, is := t.wallets.fromWallet.Wallet.(asset.MaxMatchesCounter); is {
			if maxRedeemsInTx, err = counter.MaxRedeems(t.metaData.FromVersion); err != nil {
				t.dc.log.Meter("failed_redeem_count", time.Minute*10).Warn("Failed to count redeem txs: %v", err)
			}
		}
		if maxRedeemsInTx <= 0 || len(groupables) < maxRedeemsInTx {
			c.redeemMatchGroup(t, groupables, errs)
		} else {
			for i := 0; i < len(groupables); i += maxRedeemsInTx {
				if i+maxRedeemsInTx < len(groupables) {
					c.redeemMatchGroup(t, groupables[i:i+maxRedeemsInTx], errs)
				} else {
					c.redeemMatchGroup(t, groupables[i:], errs)
				}
			}
		}
	}
	for _, m := range suspects {
		c.redeemMatchGroup(t, []*matchTracker{m}, errs)
	}
	return errs.ifAny()
}

// lcm finds the Least Common Multiple (LCM) via GCD. Use to add fractions. The
// last two returns should be used to multiply the numerators when adding. a
// and b cannot be zero.
func lcm(a, b uint64) (lowest, multA, multB uint64) {
	// greatest common divisor (GCD) via Euclidean algorithm
	gcd := func(a, b uint64) uint64 {
		for b != 0 {
			t := b
			b = a % b
			a = t
		}
		return a
	}
	cd := gcd(a, b)
	return a * b / cd, b / cd, a / cd
}

// redeemMatchGroup will send a transaction redeeming the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) redeemMatchGroup(t *trackedTrade, matches []*matchTracker, errs *errorSet) {
	// Collect an asset.Redemption for each match into a slice of redemptions that
	// will be grouped into a single transaction.
	redemptions := make([]*asset.Redemption, 0, len(matches))
	for _, match := range matches {
		redemptions = append(redemptions, &asset.Redemption{
			Spends: match.counterSwap,
			Secret: match.MetaData.Proof.Secret,
		})
	}

	// Send the transaction.
	redeemWallet := t.wallets.toWallet // this is our redeem
	if !redeemWallet.connected() {
		errs.add("%v", errWalletNotConnected)
		return
	}
	coinIDs, outCoin, fees, err := redeemWallet.Redeem(&asset.RedeemForm{
		Redemptions:   redemptions,
		FeeSuggestion: t.redeemFee(), // fallback - wallet will try to get a rate internally for configured redeem conf target
		Options:       t.options,
	})
	// If an error was encountered, fail all of the matches. A failed match will
	// not run again on during ticks.
	if err != nil {
		// Retry delays are based in part on this server's broadcast timeout.
		bTimeout, tickInterval := t.broadcastTimeout(), t.dc.ticker.Dur() // bTimeout / tickCheckInterval
		// If we lack bTimeout or tickInterval, we likely have no server config
		// on account of server down, so fallback to reasonable delay values.
		if bTimeout == 0 || tickInterval == 0 {
			tickInterval = defaultTickInterval
			bTimeout = 30 * time.Minute // don't declare missed too soon
		}
		// The caller will notify the user that there is a problem. We really
		// have no way of knowing whether this is recoverable (so we can't set
		// swapErr), but we do want to prevent redemptions every tick.
		for _, match := range matches {
			// Mark these matches as suspect. Suspect matches will not be
			// grouped for redemptions in future attempts.
			match.suspectRedeem = true
			match.redeemErrCount++
			// If we can still make a broadcast timeout, allow retries soon. It
			// is possible for RedemptionStamp or AuditStamp to be zero if we're
			// recovering during startup or after a DEX reconnect. In that case,
			// allow three retries before giving up.
			lastActionStamp := match.MetaData.Proof.Auth.AuditStamp
			if match.Side == order.Taker {
				lastActionStamp = match.MetaData.Proof.Auth.RedemptionStamp
			}
			lastActionTime := time.UnixMilli(int64(lastActionStamp))
			// Try to wait until about the next auto-tick to try again.
			waitTime := tickInterval * 3 / 4
			if time.Since(lastActionTime) > bTimeout ||
				(lastActionStamp == 0 && match.redeemErrCount >= tickCheckDivisions) {
				// If we already missed the broadcast timeout, we're not in as
				// much of a hurry. but keep trying and sending errors, because
				// we do want the user to recover.
				waitTime = 15 * time.Minute
			}
			match.delayTicks(waitTime)
		}
		errs.add("error sending redeem transaction: %v", err)
		return
	}

	c.log.Infof("Broadcasted redeem transaction spending %d contracts for order %v, paying to %s (%s)",
		len(redemptions), t.ID(), outCoin, redeemWallet.Symbol)

	if _, dynamic := t.wallets.toWallet.Wallet.(asset.DynamicSwapper); !dynamic {
		t.metaData.RedemptionFeesPaid += fees // dynamic tx wallets don't know the fees paid until mining
	}

	err = t.db.UpdateOrderMetaData(t.ID(), t.metaData)
	if err != nil {
		c.log.Errorf("Error updating order metadata for order %s: %v", t.ID(), err)
	}

	for _, match := range matches {
		c.log.Infof("Match %s complete: %s %d %s", match, sellString(t.Trade().Sell),
			match.Quantity, unbip(t.Prefix().BaseAsset),
		)
	}

	// Find the least common multiplier to use as the denom for adding
	// reserve fractions.
	denom, marketMult, limitMult := lcm(uint64(len(t.matches)), t.Trade().Quantity)
	var refundNum, redeemNum uint64

	// Save redemption details and send the redeem message to the DEX.
	// Saving the redemption details now makes it possible to resend the
	// `redeem` request at a later time if sending it now fails.
	for i, match := range matches {
		proof := &match.MetaData.Proof
		coinID := []byte(coinIDs[i])
		if match.Side == order.Taker {
			// The match won't be retired before the redeem request succeeds
			// because RedeemSig is required unless the match is revoked.
			match.Status = order.MatchComplete
			proof.TakerRedeem = coinID
		} else {
			// If we are taker we already released the refund
			// reserves when maker's redemption was found.
			if t.isMarketBuy() {
				refundNum += marketMult // * 1
			} else {
				refundNum += match.Quantity * limitMult
			}
			match.Status = order.MakerRedeemed
			proof.MakerRedeem = coinID
		}
		if t.isMarketBuy() {
			redeemNum += marketMult // * 1
		} else {
			redeemNum += match.Quantity * limitMult
		}
		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			errs.add("error storing swap details in database for match %s, coin %s: %v",
				match, coinIDString(t.wallets.fromWallet.AssetID, coinID), err)
		}
		if !match.matchCompleteSent {
			c.sendRedeemAsync(t, match, coinIDs[i], proof.Secret)
		}
	}
	if refundNum != 0 {
		t.unlockRefundFraction(refundNum, denom)
	}
	if redeemNum != 0 {
		t.unlockRedemptionFraction(redeemNum, denom)
	}
}

// sendRedeemAsync starts a goroutine to send a `redeem` request for the specified
// match and save the server's ack sig to db. Sends a notification if an error
// occurs while sending the request or validating the server's response.
func (c *Core) sendRedeemAsync(t *trackedTrade, match *matchTracker, coinID, secret []byte) {
	if !atomic.CompareAndSwapUint32(&match.sendingRedeemAsync, 0, 1) {
		return
	}

	c.log.Debugf("Notifying DEX %s of our %s swap redemption %v for match %s",
		t.dc.acct.host, t.wallets.toWallet.Symbol, coinIDString(t.wallets.toWallet.AssetID, coinID), match)

	// Send the redeem request asynchronously.
	c.wg.Add(1) // So Core does not shut down until we're done with this request.
	go func() {
		defer c.wg.Done() // bottom of the stack
		var err error
		defer func() {
			atomic.StoreUint32(&match.sendingRedeemAsync, 0)
			if err != nil {
				corder := t.coreOrder()
				subject, details := c.formatDetails(TopicReportRedeemError, match, err)
				t.notify(newOrderNote(TopicReportRedeemError, subject, details, db.ErrorLevel, corder))
			}
		}()

		msgRedeem := &msgjson.Redeem{
			OrderID: t.ID().Bytes(),
			MatchID: match.MatchID.Bytes(),
			CoinID:  coinID,
			Secret:  secret,
		}
		ack := new(msgjson.Acknowledgement)
		// The DEX may wait up to its configured broadcast timeout, but we will
		// retry on timeout or other error.
		timeout := t.broadcastTimeout() / 4
		if timeout < time.Minute { // sane minimum, or if we lack server config for any reason
			// Send would fail right away anyway if the server is really down,
			// but at least attempt it with a non-zero timeout.
			timeout = time.Minute
		}
		err = t.dc.signAndRequest(msgRedeem, msgjson.RedeemRoute, ack, timeout)
		if err != nil {
			var msgErr *msgjson.Error
			if errors.As(err, &msgErr) {
				if msgErr.Code == msgjson.SettlementSequenceError {
					c.log.Errorf("Starting match status resolution for 'redeem' request SettlementSequenceError")
					c.resolveMatchConflicts(t.dc, map[order.OrderID]*matchStatusConflict{
						t.ID(): {
							trade:   t,
							matches: []*matchTracker{match},
						},
					})
				} else if msgErr.Code == msgjson.RPCUnknownMatch {
					t.mtx.Lock()
					oid := t.ID()
					c.log.Warnf("DEX %s did not report active match %s on order %s - assuming revoked, status %v.",
						t.dc.acct.host, match, oid, match.Status)
					// We must have missed the revoke notification. Flag to allow recovery
					// and subsequent retirement of the match and parent trade.
					match.MetaData.Proof.SelfRevoked = true
					if err := c.db.UpdateMatch(&match.MetaMatch); err != nil {
						c.log.Errorf("Failed to update missing/revoked match: %v", err)
					}
					t.mtx.Unlock()
					numMissing := 1
					subject, details := c.formatDetails(TopicMissingMatches,
						numMissing, makeOrderToken(t.token()), t.dc.acct.host)
					c.notify(newOrderNote(TopicMissingMatches, subject, details, db.ErrorLevel, t.coreOrderInternal()))
				}
			}
			err = fmt.Errorf("error sending 'redeem' message: %w", err)
			return
		}

		// Validate server ack.
		err = t.dc.acct.checkSig(msgRedeem.Serialize(), ack.Sig)
		if err != nil {
			err = fmt.Errorf("'redeem' ack signature error: %v", err)
			return
		}

		c.log.Debugf("Received valid ack for 'redeem' request for match %s)", match)

		// Save redeem ack sig.
		t.mtx.Lock()
		auth := &match.MetaData.Proof.Auth
		auth.RedeemSig = ack.Sig
		auth.RedeemStamp = uint64(time.Now().UnixMilli())
		if match.Side == order.Maker && match.Status < order.MatchComplete {
			// As maker, this is the end. However, this diverges from server,
			// which still needs taker's redeem.
			if conf := match.redemptionConfs; conf > 0 && conf >= match.redemptionConfsReq {
				match.Status = order.MatchConfirmed // redeem tx already confirmed before redeem request accepted by server
			} else {
				match.Status = order.MatchComplete
			}
		} else if match.Side == order.Taker {
			match.matchCompleteSent = true
		}
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			err = fmt.Errorf("error storing redeem ack sig in database: %v", err)
		}
		if match.Status == order.MatchConfirmed {
			subject, details := t.formatDetails(TopicRedemptionConfirmed, match.token(), makeOrderToken(t.token()))
			note := newMatchNote(TopicRedemptionConfirmed, subject, details, db.Success, t, match)
			t.notify(note)
		}
		t.mtx.Unlock()
	}()
}

func (t *trackedTrade) redeemFee() uint64 {
	// Try not to use (*Core).feeSuggestion here, since it can incur an RPC
	// request to the server. t.redeemFeeSuggestion is updated every tick and
	// uses a rate directly from our wallet, if available. Only go looking for
	// one if we don't have one cached.
	var feeSuggestion uint64
	if _, is := t.accountRedeemer(); is {
		feeSuggestion = t.metaData.RedeemMaxFeeRate
	} else {
		feeSuggestion = t.redeemFeeSuggestion.get()
	}
	if feeSuggestion == 0 {
		feeSuggestion = t.dc.bestBookFeeSuggestion(t.wallets.toWallet.AssetID)
	}
	return feeSuggestion
}

func (t *trackedTrade) refundFee() uint64 {
	// Try not to use (*Core).feeSuggestion here, since it can incur an RPC
	// request to the server. t.refundFeeSuggestion is updated every tick and
	// uses a rate directly from our wallet, if available. Only go looking for
	// one if we don't have one cached.
	var feeSuggestion uint64
	if _, is := t.accountRefunder(); is {
		feeSuggestion = t.metaData.MaxFeeRate
	} else {
		feeSuggestion = t.refundFeeSuggestion.get()
	}
	if feeSuggestion == 0 {
		feeSuggestion = t.dc.bestBookFeeSuggestion(t.wallets.fromWallet.AssetID)
	}
	return feeSuggestion
}

// confirmRedemption attempts to confirm the redemptions for each match, and
// then return any refund addresses that we won't be using.
func (c *Core) confirmRedemptions(t *trackedTrade, matches []*matchTracker) {
	var refundContracts [][]byte
	for _, m := range matches {
		if confirmed, err := c.confirmRedemption(t, m); err != nil {
			t.dc.log.Errorf("Unable to confirm redemption: %v", err)
		} else if confirmed {
			refundContracts = append(refundContracts, m.MetaData.Proof.ContractData)
		}
	}
	if len(refundContracts) == 0 {
		return
	}
	if ar, is := t.wallets.fromWallet.Wallet.(asset.AddressReturner); is {
		ar.ReturnRefundContracts(refundContracts)
	}
}

// confirmRedemption checks if the user's redemption has been confirmed,
// and if so, updates the match's status to MatchConfirmed.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) confirmRedemption(t *trackedTrade, match *matchTracker) (bool, error) {
	if confs := match.redemptionConfs; confs > 0 && confs >= match.redemptionConfsReq { // already there, stop checking
		if len(match.MetaData.Proof.Auth.RedeemSig) == 0 && (!t.isSelfGoverned() && !match.MetaData.Proof.IsRevoked()) {
			return false, nil // waiting on redeem request to succeed
		}
		// Redeem request just succeeded or we gave up on the server.
		if match.Status == order.MatchConfirmed {
			return true, nil // raced with concurrent sendRedeemAsync
		}
		match.Status = order.MatchConfirmed
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("failed to update match in db: %v", err)
		}
		subject, details := t.formatDetails(TopicRedemptionConfirmed, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRedemptionConfirmed, subject, details, db.Success, t, match)
		t.notify(note)
		return true, nil
	}

	// In some cases the wallet will need to send a new redeem transaction.
	toWallet := t.wallets.toWallet

	if err := toWallet.checkPeersAndSyncStatus(); err != nil {
		return false, err
	}

	didUnlock, err := toWallet.refreshUnlock()
	if err != nil { // Just log it and try anyway.
		t.dc.log.Errorf("refreshUnlock error checking redeem %s: %v", toWallet.Symbol, err)
	}
	if didUnlock {
		t.dc.log.Warnf("Unexpected unlock needed for the %s wallet to check a redemption", toWallet.Symbol)
	}

	proof := &match.MetaData.Proof
	var redeemCoinID order.CoinID
	if match.Side == order.Maker {
		redeemCoinID = proof.MakerRedeem
	} else {
		redeemCoinID = proof.TakerRedeem
	}

	match.confirmRedemptionNumTries++

	redemptionStatus, err := toWallet.Wallet.ConfirmTransaction(dex.Bytes(redeemCoinID),
		asset.NewRedeemConfTx(match.counterSwap, proof.Secret), t.redeemFee())
	switch {
	case err == nil:
	case errors.Is(err, asset.ErrSwapRefunded):
		subject, details := t.formatDetails(TopicSwapRefunded, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicSwapRefunded, subject, details, db.ErrorLevel, t, match)
		t.notify(note)
		match.Status = order.MatchConfirmed
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("Failed to update match in db %v", err)
		}
		return false, errors.New("swap was already refunded by the counterparty")

	case errors.Is(err, asset.ErrTxRejected):
		match.redemptionRejected = true
		// We need to seek user approval before trying again, since new fees
		// could be incurred.
		actionRequest, note := newRejectedTxNote(toWallet.AssetID, t.ID(), redeemCoinID, asset.CTRedeem)
		t.notify(note)
		c.requestedActionMtx.Lock()
		c.requestedActions[dex.Bytes(redeemCoinID).String()] = actionRequest
		c.requestedActionMtx.Unlock()
		return false, fmt.Errorf("%s transaction %s was rejected. Seeking user approval before trying again",
			unbip(toWallet.AssetID), coinIDString(toWallet.AssetID, redeemCoinID))
	case errors.Is(err, asset.ErrTxLost):
		// The transaction was nonce-replaced or otherwise lost without
		// rejection or with user acknowlegement. Try again.
		var coinID order.CoinID
		if match.Side == order.Taker {
			coinID = match.MetaData.Proof.TakerRedeem
			match.MetaData.Proof.TakerRedeem = nil
			match.Status = order.MakerRedeemed
		} else {
			coinID = match.MetaData.Proof.MakerRedeem
			match.MetaData.Proof.MakerRedeem = nil
			match.Status = order.TakerSwapCast
		}
		c.log.Infof("Redemption %s (%s) has been noted as lost.", coinID, unbip(toWallet.AssetID))

		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			t.dc.log.Errorf("failed to update match after lost tx reported: %v", err)
		}
		return false, nil
	default:
		match.delayTicks(time.Minute * 15)
		return false, fmt.Errorf("error confirming redemption for coin %v. already tried %d times, will retry later: %v",
			redeemCoinID, match.confirmRedemptionNumTries, err)
	}

	var redemptionResubmitted, redemptionConfirmed bool
	if !bytes.Equal(redeemCoinID, redemptionStatus.CoinID) {
		redemptionResubmitted = true
		if match.Side == order.Maker {
			proof.MakerRedeem = order.CoinID(redemptionStatus.CoinID)
		} else {
			proof.TakerRedeem = order.CoinID(redemptionStatus.CoinID)
		}
	}

	match.redemptionConfs, match.redemptionConfsReq = redemptionStatus.Confs, redemptionStatus.Req

	if redemptionStatus.Confs >= redemptionStatus.Req &&
		(len(match.MetaData.Proof.Auth.RedeemSig) > 0 || t.isSelfGoverned()) {
		redemptionConfirmed = true
		match.Status = order.MatchConfirmed
	}

	if redemptionResubmitted || redemptionConfirmed {
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("failed to update match in db: %v", err)
		}
	}

	if redemptionResubmitted {
		subject, details := t.formatDetails(TopicRedemptionResubmitted, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRedemptionResubmitted, subject, details, db.WarningLevel, t, match)
		t.notify(note)
	}

	if redemptionConfirmed {
		subject, details := t.formatDetails(TopicRedemptionConfirmed, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRedemptionConfirmed, subject, details, db.Success, t, match)
		t.notify(note)
	} else {
		note := newMatchNote(TopicConfirms, "", "", db.Data, t, match)
		t.notify(note)
	}
	return redemptionConfirmed, nil
}

// confirmRefund checks if the user's refund has been confirmed,
// and if so, updates the match's status to MatchConfirmed.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) confirmRefund(t *trackedTrade, match *matchTracker) (bool, error) {
	if confs := match.refundConfs; confs > 0 && confs >= match.refundConfsReq { // already there, stop checking
		if match.Status == order.MatchConfirmed {
			return true, nil
		}
		match.Status = order.MatchConfirmed
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("failed to update match in db: %v", err)
		}
		subject, details := t.formatDetails(TopicRefundConfirmed, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRefundConfirmed, subject, details, db.Success, t, match)
		t.notify(note)
		return true, nil
	}

	// In some cases the wallet will need to send a new refund transaction.
	fromWallet := t.wallets.fromWallet

	if err := fromWallet.checkPeersAndSyncStatus(); err != nil {
		return false, err
	}

	didUnlock, err := fromWallet.refreshUnlock()
	if err != nil { // Just log it and try anyway.
		t.dc.log.Errorf("refreshUnlock error checking refund %s: %v", fromWallet.Symbol, err)
	}
	if didUnlock {
		t.dc.log.Warnf("Unexpected unlock needed for the %s wallet to check a refund", fromWallet.Symbol)
	}

	proof := &match.MetaData.Proof
	refundCoinID := proof.RefundCoin
	secretHash := proof.SecretHash
	var swapCoinID dex.Bytes
	if match.Side == order.Maker {
		swapCoinID = dex.Bytes(match.MetaData.Proof.MakerSwap)
	} else {
		swapCoinID = dex.Bytes(match.MetaData.Proof.TakerSwap)
	}
	contractToRefund := match.MetaData.Proof.ContractData

	match.confirmRedemptionNumTries++

	refundStatus, err := fromWallet.Wallet.ConfirmTransaction(dex.Bytes(refundCoinID),
		asset.NewRefundConfTx(swapCoinID, contractToRefund, secretHash), t.refundFee())
	switch {
	case err == nil:
	case errors.Is(err, asset.ErrSwapRedeemed):
		subject, details := t.formatDetails(TopicSwapRedeemed, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicSwapRedeemed, subject, details, db.ErrorLevel, t, match)
		t.notify(note)
		match.Status = order.MatchConfirmed
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("Failed to update match in db %v", err)
		}
		return false, errors.New("swap was already redeemed by the counterparty")

	case errors.Is(err, asset.ErrTxRejected):
		match.refundRejected = true
		// We need to seek user approval before trying again, since new fees
		// could be incurred.
		actionRequest, note := newRejectedTxNote(fromWallet.AssetID, t.ID(), refundCoinID, asset.CTRefund)
		t.notify(note)
		c.requestedActionMtx.Lock()
		c.requestedActions[dex.Bytes(refundCoinID).String()] = actionRequest
		c.requestedActionMtx.Unlock()
		return false, fmt.Errorf("%s transaction %s was rejected. Seeking user approval before trying again",
			unbip(fromWallet.AssetID), coinIDString(fromWallet.AssetID, refundCoinID))
	case errors.Is(err, asset.ErrTxLost):
		// The transaction was nonce-replaced or otherwise lost without
		// rejection or with user acknowlegement. Try again.
		match.MetaData.Proof.RefundCoin = nil
		c.log.Infof("Redemption %s (%s) has been noted as lost.", refundCoinID, unbip(fromWallet.AssetID))

		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			t.dc.log.Errorf("failed to update match after lost tx reported: %v", err)
		}
		return false, nil
	default:
		match.delayTicks(time.Minute * 15)
		return false, fmt.Errorf("error confirming refund for coin %v. already tried %d times, will retry later: %v",
			refundCoinID, match.confirmRefundNumTries, err)
	}

	var refundResubmitted, refundConfirmed bool
	if !bytes.Equal(refundCoinID, refundStatus.CoinID) {
		refundResubmitted = true
		match.MetaData.Proof.RefundCoin = order.CoinID(refundStatus.CoinID)
	}

	match.refundConfs, match.refundConfsReq = refundStatus.Confs, refundStatus.Req

	if refundStatus.Confs >= refundStatus.Req {
		refundConfirmed = true
		match.Status = order.MatchConfirmed
	}
	if refundResubmitted || refundConfirmed {
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("failed to update match in db: %v", err)
		}
	}

	if refundResubmitted {
		subject, details := t.formatDetails(TopicRefundResubmitted, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRefundResubmitted, subject, details, db.WarningLevel, t, match)
		t.notify(note)
	}

	if refundConfirmed {
		subject, details := t.formatDetails(TopicRefundConfirmed, match.token(), makeOrderToken(t.token()))
		note := newMatchNote(TopicRefundConfirmed, subject, details, db.Success, t, match)
		t.notify(note)
	} else {
		note := newMatchNote(TopicConfirms, "", "", db.Data, t, match)
		t.notify(note)
	}
	return refundConfirmed, nil
}

// findMakersRedemption starts a goroutine to search for the redemption of
// taker's contract.
//
// This method modifies trackedTrade fields and MUST be called with the
// trackedTrade mutex lock held for writes.
func (t *trackedTrade) findMakersRedemption(ctx context.Context, match *matchTracker) {
	if match.cancelRedemptionSearch != nil {
		return
	}

	// NOTE: Use Core's ctx to auto-cancel this search when Core is shut down.
	ctx, cancel := context.WithCancel(ctx)
	match.cancelRedemptionSearch = cancel
	swapCoinID := dex.Bytes(match.MetaData.Proof.TakerSwap)
	swapContract := dex.Bytes(match.MetaData.Proof.ContractData)

	wallet := t.wallets.fromWallet
	if !wallet.connected() {
		t.dc.log.Errorf("Cannot find redemption with wallet not connected")
		return
	}

	// Run redemption finder in goroutine.
	go func() {
		defer cancel() // don't leak the context when we reset match.cancelRedemptionSearch
		redemptionCoinID, secret, err := wallet.FindRedemption(ctx, swapCoinID, swapContract)

		// Redemption search done, with or without error.
		// Keep the mutex locked for the remainder of this goroutine execution to
		// read and write match fields while processing the find redemption result.
		t.mtx.Lock()
		defer t.mtx.Unlock()

		match.cancelRedemptionSearch = nil
		symbol, assetID := wallet.Symbol, wallet.AssetID

		if err != nil {
			// Ignore the error if we've refunded, the error would likely be context
			// canceled or secret parse error (if redemption search encountered the
			// refund before the search could be canceled).
			if len(match.MetaData.Proof.RefundCoin) == 0 {
				t.dc.log.Errorf("Error finding redemption of taker's %s contract %s (%s) for order %s, match %s: %v.",
					symbol, coinIDString(assetID, swapCoinID), swapContract, t.ID(), match, err)
			}
			return
		}

		if match.Status != order.TakerSwapCast {
			t.dc.log.Errorf("Received find redemption result at wrong step, order %s, match %s, status %s.",
				t.ID(), match, match.Status)
			return
		}

		proof := &match.MetaData.Proof
		if !t.wallets.toWallet.ValidateSecret(secret, proof.SecretHash) {
			t.dc.log.Errorf("Found invalid redemption of taker's %s contract %s (%s) for order %s, match %s: invalid secret %s, hash %s.",
				symbol, coinIDString(assetID, swapCoinID), swapContract, t.ID(), match, secret, proof.SecretHash)
			return
		}

		if t.isMarketBuy() {
			t.unlockRefundFraction(1, uint64(len(t.matches)))
		} else {
			t.unlockRefundFraction(match.Quantity, t.Trade().Quantity)
		}

		// Update the match status and set the secret so that Maker's swap
		// will be redeemed in the next call to trade.tick().
		match.Status = order.MakerRedeemed
		proof.MakerRedeem = []byte(redemptionCoinID)
		proof.Secret = secret
		proof.SelfRevoked = true // Set match as revoked.
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			t.dc.log.Errorf("waitForRedemptions: error storing match info in database: %v", err)
		}

		t.dc.log.Infof("Found redemption of contract %s (%s) for order %s, match %s. Redeem: %v",
			coinIDString(assetID, swapCoinID), symbol, t.ID(), match,
			coinIDString(assetID, redemptionCoinID))

		subject, details := t.formatDetails(TopicMatchRecovered,
			symbol, coinIDString(assetID, redemptionCoinID), match)
		t.notify(newOrderNote(TopicMatchRecovered, subject, details, db.Poke, t.coreOrderInternal()))
	}()
}

// refundMatches will send refund transactions for the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) refundMatches(t *trackedTrade, matches []*matchTracker) (uint64, error) {
	errs := newErrorSet("refundMatches: order %s - ", t.ID())

	refundWallet := t.wallets.fromWallet // refunding to our wallet
	symbol, assetID := refundWallet.Symbol, refundWallet.AssetID
	var refundedQty uint64

	for _, match := range matches {
		if len(match.MetaData.Proof.RefundCoin) != 0 {
			c.log.Errorf("attempted to execute duplicate refund for match %s, side %s, status %s",
				match, match.Side, match.Status)
			continue
		}
		contractToRefund := match.MetaData.Proof.ContractData
		var swapCoinID dex.Bytes
		var matchFailureReason string
		switch {
		case match.Side == order.Maker && match.Status == order.MakerSwapCast:
			swapCoinID = dex.Bytes(match.MetaData.Proof.MakerSwap)
			matchFailureReason = "no valid counterswap received from Taker"
		case match.Side == order.Maker && match.Status == order.TakerSwapCast && len(match.MetaData.Proof.MakerRedeem) == 0:
			swapCoinID = dex.Bytes(match.MetaData.Proof.MakerSwap)
			matchFailureReason = "unable to redeem Taker's swap"
		case match.Side == order.Taker && match.Status == order.TakerSwapCast:
			swapCoinID = dex.Bytes(match.MetaData.Proof.TakerSwap)
			matchFailureReason = "no valid redemption received from Maker"
		default:
			c.log.Errorf("attempted to execute invalid refund for match %s, side %s, status %s",
				match, match.Side, match.Status)
			continue
		}

		swapCoinString := coinIDString(assetID, swapCoinID)
		c.log.Infof("Refunding %s contract %s for match %s (%s)",
			symbol, swapCoinString, match, matchFailureReason)

		var feeRate uint64
		if _, is := t.accountRefunder(); is {
			feeRate = t.metaData.MaxFeeRate
		}
		if feeRate == 0 {
			feeRate = c.feeSuggestionAny(assetID) // includes wallet itself
		}

		refundCoin, err := refundWallet.Refund(swapCoinID, contractToRefund, feeRate)
		if err != nil {
			// CRITICAL - Refund must indicate if the swap is spent (i.e.
			// redeemed already) so that as taker we will start the
			// auto-redemption path.
			if errors.Is(err, asset.CoinNotFoundError) && match.Side == order.Taker {
				match.refundErr = err
				// Could not find the contract coin, which means it has been
				// spent. Unless the locktime is expired, we would have already
				// started FindRedemption for this contract.
				c.log.Debugf("Failed to refund %s contract %s, already redeemed. Beginning find redemption.",
					symbol, swapCoinString)
				t.findMakersRedemption(c.ctx, match)
			} else {
				match.delayTicks(time.Minute * 5)
				errs.add("error sending refund tx for match %s, swap coin %s: %v",
					match, swapCoinString, err)
				if match.Status == order.TakerSwapCast && match.Side == order.Taker {
					// Check for a redeem even though Refund did not indicate it
					// was spent via CoinNotFoundError, but do not set refundErr
					// so that a refund can be tried again.
					t.findMakersRedemption(c.ctx, match)
				}
			}
			continue
		}

		if t.isMarketBuy() {
			t.unlockRedemptionFraction(1, uint64(len(t.matches)))
			t.unlockRefundFraction(1, uint64(len(t.matches)))
		} else {
			t.unlockRedemptionFraction(match.Quantity, t.Trade().Quantity)
			t.unlockRefundFraction(match.Quantity, t.Trade().Quantity)
		}

		// Refund successful, cancel any previously started attempt to find
		// counter-party's redemption.
		if match.cancelRedemptionSearch != nil {
			match.cancelRedemptionSearch()
		}
		if t.Trade().Sell {
			refundedQty += match.Quantity
		} else {
			refundedQty += calc.BaseToQuote(match.Rate, match.Quantity)
		}
		match.MetaData.Proof.RefundCoin = []byte(refundCoin)
		match.MetaData.Proof.SelfRevoked = true // Set match as revoked.
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			errs.add("error storing match info in database: %v", err)
		}
	}

	return refundedQty, errs.ifAny()
}

// processAuditMsg processes the audit request from the server. A non-nil error
// is only returned if the match referenced by the Audit message is not known.
func (t *trackedTrade) processAuditMsg(msgID uint64, audit *msgjson.Audit) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	// Find the match and check the server's signature.
	var mid order.MatchID
	copy(mid[:], audit.MatchID)
	match, found := t.matches[mid]
	if !found {
		return fmt.Errorf("processAuditMsg: match %v not found for order %s", mid, t.ID())
	}
	// Check the server signature.
	sigMsg := audit.Serialize()
	err := t.dc.acct.checkSig(sigMsg, audit.Sig)
	if err != nil {
		// Log, but don't quit. If the audit passes, great.
		t.dc.log.Warnf("Server audit signature error: %v", err)
	}

	// Start searching for and audit the contract. This can take some time
	// depending on node connectivity, so this is run in a goroutine. If the
	// contract and coin (amount) are successfully validated, the matchTracker
	// data are updated.
	go func() {
		// Search until it's known to be revoked.
		err := t.auditContract(match, audit.CoinID, audit.Contract, audit.TxData)
		if err != nil {
			contractID := coinIDString(t.wallets.toWallet.AssetID, audit.CoinID)
			t.dc.log.Errorf("Failed to audit contract coin %v (%s) for match %s: %v",
				contractID, t.wallets.toWallet.Symbol, match, err)
			// Don't revoke in case server sends a revised audit request before
			// the match is revoked.
			return
		}

		// The audit succeeded. Update and store match data.
		t.mtx.Lock()
		auth := &match.MetaData.Proof.Auth
		auth.AuditStamp, auth.AuditSig = audit.Time, audit.Sig
		t.notify(newMatchNote(TopicAudit, "", "", db.Data, t, match))
		err = t.db.UpdateMatch(&match.MetaMatch)
		t.mtx.Unlock()
		if err != nil {
			t.dc.log.Errorf("Error updating database for match %s: %v", match, err)
		}

		// Respond to DEX, but this is not consequential.
		err = t.dc.ack(msgID, mid, audit)
		if err != nil {
			t.dc.log.Debugf("Error acknowledging audit to server (not necessarily an error): %v", err)
			// The server's response timeout may have just passed, but we got
			// what we needed to do our swap or redeem if the match is still
			// live, so do not log this as an error.
		}
	}()

	return nil
}

// searchAuditInfo tries to obtain the asset.AuditInfo from the ExchangeWallet.
// Handle network latency or other transient node errors. The coin waiter will
// run once every recheckInterval until successful or until the match is
// revoked. The client is asked by the server to audit a contract transaction,
// and they have until broadcast timeout to do it before they get penalized and
// the match revoked. Thus, there is no reason to give up on the request sooner
// since the server will not ask again and the client will not solicit the
// counterparty contract data again except on reconnect. This may block for a
// long time and should be run in a goroutine. The trackedTrade mtx must NOT be
// locked.
//
// NOTE: This assumes the Wallet's AuditContract method may need to actually
// locate the contract transaction on the network. However, for some (or all)
// assets, the audit may be performed with just txData, which makes this
// "search" obsolete. We may wish to remove the latencyQ and have this be a
// single call to AuditContract. Leaving as-is for now.
func (t *trackedTrade) searchAuditInfo(match *matchTracker, coinID []byte, contract, txData []byte) (*asset.AuditInfo, error) {
	errChan := make(chan error, 1)
	var auditInfo *asset.AuditInfo
	var tries int
	toWallet := t.wallets.toWallet
	if !toWallet.connected() {
		return nil, errWalletNotConnected
	}
	contractID, contractSymb := coinIDString(toWallet.AssetID, coinID), toWallet.Symbol
	tLastWarning := time.Now()
	t.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(24 * time.Hour), // effectively forever
		TryFunc: func() wait.TryDirective {
			var err error
			auditInfo, err = toWallet.AuditContract(coinID, contract, txData, true)
			if err == nil {
				// Success.
				errChan <- nil
				return wait.DontTryAgain
			}
			if errors.Is(err, asset.CoinNotFoundError) {
				// Didn't find it that time.
				t.dc.log.Tracef("Still searching for counterparty's contract coin %v (%s) for match %s.", contractID, contractSymb, match)
				if t.matchIsRevoked(match) {
					errChan <- ExpirationErr(fmt.Sprintf("match revoked while waiting to find counterparty contract coin %v (%s). "+
						"Check your internet and wallet connections!", contractID, contractSymb))
					return wait.DontTryAgain
				}
				if time.Since(tLastWarning) > 30*time.Minute {
					tLastWarning = time.Now()
					subject, detail := t.formatDetails(TopicAuditTrouble, contractID, contractSymb, match)
					t.notify(newOrderNote(TopicAuditTrouble, subject, detail, db.WarningLevel, t.coreOrder()))
				}
				tries++
				return wait.TryAgain
			}
			// Even retry for unrecognized errors, at least for a little while.
			// With a default recheckInterval of 5 seconds, this is 2 minutes.
			if tries < 24 {
				t.dc.log.Errorf("Unexpected audit contract %v (%s) error (will try again): %v", contractID, contractSymb, err)
				tries++
				return wait.TryAgain
			}
			errChan <- err
			return wait.DontTryAgain

		},
		ExpireFunc: func() {
			errChan <- ExpirationErr(fmt.Sprintf("failed to find counterparty contract coin %v (%s). "+
				"Check your internet and wallet connections!", contractID, contractSymb))
		},
	})

	// Wait for the coin waiter to find and audit the contract coin, or timeout.
	err := <-errChan
	if err != nil {
		return nil, err
	}
	return auditInfo, nil
}

// auditContract audits the contract for the match and relevant MatchProof
// fields are set. This may block for a long period, and should be run in a
// goroutine. The trackedTrade mtx must NOT be locked. The match is updated in
// the DB if the audit succeeds.
func (t *trackedTrade) auditContract(match *matchTracker, coinID, contract, txData []byte) error {
	auditInfo, err := t.searchAuditInfo(match, coinID, contract, txData)
	if err != nil {
		return err
	}

	assetID, contractSymb := t.wallets.toWallet.AssetID, t.wallets.toWallet.Symbol
	contractID := coinIDString(assetID, coinID)

	// Audit the contract.
	// 1. Recipient Address
	// 2. Contract value
	// 3. Secret hash: maker compares, taker records
	if auditInfo.Recipient != t.Trade().Address {
		return fmt.Errorf("swap recipient %s in contract coin %v (%s) is not the order address %s",
			auditInfo.Recipient, contractID, contractSymb, t.Trade().Address)
	}

	auditQty := match.Quantity
	if t.Trade().Sell {
		auditQty = calc.BaseToQuote(match.Rate, auditQty)
	}
	if auditInfo.Coin.Value() < auditQty {
		return fmt.Errorf("swap contract coin %v (%s) value %d was lower than expected %d",
			contractID, contractSymb, auditInfo.Coin.Value(), auditQty)
	}

	// TODO: Consider having the server supply the contract txn's fee rate to
	// improve the taker's audit with a check of the maker's contract fee rate.
	// The server should be checking the fee rate, but the client should not
	// trust it. The maker could also check the taker's contract txn fee rate,
	// but their contract is already broadcasted, so the check is of less value
	// as they can only wait for it to be mined to redeem it, in which case the
	// fee rate no longer matters, or wait for the lock time to expire to refund.

	// Check and store the counterparty contract data.
	matchTime := match.matchTime()
	reqLockTime := encode.DropMilliseconds(matchTime.Add(t.lockTimeMaker)) // counterparty == maker
	if match.Side == order.Maker {
		reqLockTime = encode.DropMilliseconds(matchTime.Add(t.lockTimeTaker)) // counterparty == taker
	}
	if auditInfo.Expiration.Before(reqLockTime) {
		return fmt.Errorf("lock time too early. Need %s, got %s", reqLockTime, auditInfo.Expiration)
	}

	t.mtx.Lock()
	defer t.mtx.Unlock()
	proof := &match.MetaData.Proof
	if match.Side == order.Maker {
		// Check that the secret hash is correct.
		if !bytes.Equal(proof.SecretHash, auditInfo.SecretHash) {
			return fmt.Errorf("secret hash mismatch for contract coin %v (%s), contract %v. expected %x, got %v",
				auditInfo.Coin, contractSymb, contract, proof.SecretHash, auditInfo.SecretHash)
		}
		// Audit successful. Update status and other match data.
		match.Status = order.TakerSwapCast
		proof.TakerSwap = coinID
	} else {
		proof.SecretHash = auditInfo.SecretHash
		match.Status = order.MakerSwapCast
		proof.MakerSwap = coinID
	}
	proof.CounterTxData = txData
	proof.CounterContract = contract
	match.counterSwap = auditInfo

	err = t.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		t.dc.log.Errorf("Error updating database for match %v: %s", match, err)
	}

	t.dc.log.Infof("Audited contract (%s: %v) paying to %s for order %s, match %s, "+
		"with tx data = %t. Script: %x", contractSymb, auditInfo.Coin,
		auditInfo.Recipient, t.ID(), match, len(txData) > 0, contract)

	return nil
}

// processRedemption processes the redemption request from the server.
func (t *trackedTrade) processRedemption(msgID uint64, redemption *msgjson.Redemption) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var mid order.MatchID
	copy(mid[:], redemption.MatchID)
	errs := newErrorSet("processRedemption order %s, match %s - ", t.ID(), mid)
	match, found := t.matches[mid]
	if !found {
		return errs.add("match not known")
	}

	// Validate that this request satisfies expected preconditions if
	// we're the Taker. Not necessary if we're maker as redemption
	// requests are pretty much just a formality for Maker. Also, if
	// the order was loaded from the DB and we've already redeemed
	// Taker's swap, the counterSwap (AuditInfo for Taker's swap) will
	// not have been retrieved.
	if match.Side == order.Taker {
		switch {
		case match.counterSwap == nil:
			return errs.add("redemption received before audit request")
		case match.Status == order.TakerSwapCast:
			// redemption requests should typically arrive when the match
			// is at TakerSwapCast
		case match.Status > order.TakerSwapCast && len(match.MetaData.Proof.Auth.RedemptionSig) == 0:
			// status might have moved 1+ steps forward if this redemption
			// request is received after we've already found the redemption
		default:
			return fmt.Errorf("maker redemption received at incorrect step %d", match.Status)
		}
	} else {
		if match.Status < order.MakerRedeemed { // only makes sense if we've redeemed
			return fmt.Errorf("redemption request received as maker for match %v in status %v",
				mid, match.Status)
		}
		t.dc.log.Tracef("Received a courtesy redemption request for match %v as maker", mid)
	}

	// Respond to the DEX.
	err := t.dc.ack(msgID, match.MatchID, redemption)
	if err != nil {
		return errs.add("Audit - %v", err)
	}

	// Update the database.
	match.MetaData.Proof.Auth.RedemptionSig = redemption.Sig
	match.MetaData.Proof.Auth.RedemptionStamp = redemption.Time

	if match.Side == order.Taker {
		// As taker, this step is important because we validate that the
		// provided secret corresponds to the secret hash in our contract.
		err = t.processMakersRedemption(match, redemption.CoinID, redemption.Secret)
		if err != nil {
			errs.addErr(err)
		}
	} else {
		// Historically, the server does not send a redemption request to the
		// maker for the taker's redeem since match negotiation is complete
		// client-side at time of redeem. Just store the redeem CoinID as
		// TakerRedeem. Our own match negotiation has or will advance the status
		// and handle coin unlocking as needed.
		match.MetaData.Proof.TakerRedeem = order.CoinID(redemption.CoinID)
	}

	err = t.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		errs.add("error storing match info in database: %v", err)
	}
	return errs.ifAny()
}

func (t *trackedTrade) processMakersRedemption(match *matchTracker, coinID, secret []byte) error {
	if match.Side == order.Maker {
		return fmt.Errorf("processMakersRedemption called when we are the maker, which is nonsense. order = %s, match = %s", t.ID(), match)
	}

	proof := &match.MetaData.Proof
	secretHash := proof.SecretHash
	wallet := t.wallets.toWallet
	if !wallet.ValidateSecret(secret, secretHash) {
		return fmt.Errorf("secret %x received does not hash to the reported secret hash, %x",
			secret, secretHash)
	}

	t.dc.log.Infof("Notified of maker's redemption (%s: %v) and validated secret for order %v...",
		t.wallets.fromWallet.Symbol, coinIDString(t.wallets.fromWallet.AssetID, coinID), t.ID())

	if match.Status < order.MakerRedeemed {
		if t.isMarketBuy() {
			t.unlockRefundFraction(1, uint64(len(t.matches)))
		} else {
			t.unlockRefundFraction(match.Quantity, t.Trade().Quantity)
		}
	}

	match.Status = order.MakerRedeemed
	proof.MakerRedeem = coinID
	proof.Secret = secret
	return nil
}

// Coins will be returned if
//   - the trade status is not OrderStatusEpoch or OrderStatusBooked, that is to
//     say, there won't be future matches for this order.
//   - there are no matches in the trade that MAY later require sending swaps,
//     that is to say, all matches have been either swapped or revoked.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) maybeReturnCoins() bool {
	// Status of the order itself.
	if t.metaData.Status < order.OrderStatusExecuted {
		// Booked and epoch orders may get matched any moment from
		// now, keep the coins locked.
		t.dc.log.Tracef("Not unlocking coins for order with status %s", t.metaData.Status)
		return false
	}

	// Status of all matches for the order. If a match exists for
	// which a swap MAY be sent later, keep the coins locked.
	for _, match := range t.matches {
		if match.MetaData.Proof.IsRevoked() {
			// Won't be sending swap for this match regardless of
			// the match's status.
			continue
		}

		status, side := match.Status, match.Side
		if side == order.Maker && status < order.MakerSwapCast ||
			side == order.Taker && status < order.TakerSwapCast {
			// Match is active (not revoked, not refunded) and client
			// is yet to execute swap. Keep coins locked.
			t.dc.log.Tracef("Not unlocking coins for order %v with match side %s, status %s", t.ID(), side, status)
			return false
		}
	}

	// Safe to unlock coins now.
	t.returnCoins()
	return true
}

// returnCoins unlocks this trade's funding coins (if unspent) or the change
// coin if a previous swap created a change coin that is locked.
// Coins are auto-unlocked once spent in a swap tx, including intermediate
// change coins, such that only the last change coin (if locked), will need
// to be unlocked.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) returnCoins() {
	if !t.wallets.fromWallet.connected() {
		t.dc.log.Warnf("Unable to return %s funding coins: %v", t.wallets.fromWallet.Symbol, errWalletNotConnected)
		return
	}
	if t.change == nil && t.coinsLocked {
		fundingCoins := make([]asset.Coin, 0, len(t.coins))
		for _, coin := range t.coins {
			fundingCoins = append(fundingCoins, coin)
		}
		err := t.wallets.fromWallet.ReturnCoins(fundingCoins)
		if err != nil {
			t.dc.log.Warnf("Unable to return %s funding coins: %v", t.wallets.fromWallet.Symbol, err)
		} else {
			t.coinsLocked = false
		}
		if returner, is := t.wallets.toWallet.Wallet.(asset.AddressReturner); is {
			returner.ReturnRedemptionAddress(t.Trade().Address)
		}
	} else if t.change != nil && t.changeLocked {
		err := t.wallets.fromWallet.ReturnCoins(asset.Coins{t.change})
		if err != nil {
			t.dc.log.Warnf("Unable to return %s change coin %v: %v", t.wallets.fromWallet.Symbol, t.change, err)
		} else {
			t.changeLocked = false
		}
	}
}

// requiredForRemainingSwaps determines the amount of the from asset that is
// still needed in order initiate the remaining swaps in the order.
func (t *trackedTrade) requiredForRemainingSwaps() (uint64, error) {
	mkt := t.dc.marketConfig(t.mktID)
	if mkt == nil {
		return 0, fmt.Errorf("could not find market: %v", t.mktID)
	}
	lotSize := mkt.LotSize

	accelWallet, ok := t.wallets.fromWallet.Wallet.(asset.Accelerator)
	if !ok {
		return 0, fmt.Errorf("the %s wallet is not an accelerator", t.wallets.fromWallet.Symbol)
	}

	var requiredForRemainingSwaps uint64
	var maxSwapsRemaining uint64

	if t.metaData.Status <= order.OrderStatusExecuted {
		if !t.Trade().Sell {
			requiredForRemainingSwaps += calc.BaseToQuote(t.rate(), t.Trade().Remaining())
		} else {
			requiredForRemainingSwaps += t.Trade().Remaining()
		}
		maxSwapsRemaining += t.Trade().Remaining() / lotSize
	}

	for _, match := range t.matches {
		if (match.Side == order.Maker && match.Status < order.MakerSwapCast) ||
			(match.Side == order.Taker && match.Status < order.TakerSwapCast) {
			if !t.Trade().Sell {
				requiredForRemainingSwaps += calc.BaseToQuote(match.Rate, match.Quantity)
			} else {
				requiredForRemainingSwaps += match.Quantity
			}
			maxSwapsRemaining++
		}
	}

	// Add the fees.
	requiredForRemainingSwaps += accelWallet.FeesForRemainingSwaps(maxSwapsRemaining, t.metaData.MaxFeeRate)

	return requiredForRemainingSwaps, nil
}

// orderAccelerationParameters returns the parameters needed to accelerate the
// swap transactions in this trade.
// MUST be called with the trackedTrade mutex held.
func (t *trackedTrade) orderAccelerationParameters() (swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps uint64, err error) {
	makeError := func(err error) ([]dex.Bytes, []dex.Bytes, dex.Bytes, uint64, error) {
		return nil, nil, nil, 0, err
	}

	if t.metaData.ChangeCoin == nil {
		return makeError(fmt.Errorf("order does not have change which can be accelerated"))
	}

	if len(t.metaData.AccelerationCoins) >= 10 {
		return makeError(fmt.Errorf("order has already been accelerated too many times"))
	}

	requiredForRemainingSwaps, err = t.requiredForRemainingSwaps()
	if err != nil {
		return makeError(err)
	}

	swapCoins = make([]dex.Bytes, 0, len(t.matches))
	for _, match := range t.matches {
		var swapCoinID order.CoinID
		if match.Side == order.Maker && match.Status >= order.MakerSwapCast {
			swapCoinID = match.MetaData.Proof.MakerSwap
		} else if match.Side == order.Taker && match.Status >= order.TakerSwapCast {
			swapCoinID = match.MetaData.Proof.TakerSwap
		} else {
			continue
		}

		swapCoins = append(swapCoins, dex.Bytes(swapCoinID))
	}

	if len(swapCoins) == 0 {
		return makeError(fmt.Errorf("cannot accelerate an order without any swaps"))
	}

	accelerationCoins = make([]dex.Bytes, 0, len(t.metaData.AccelerationCoins))
	for _, coin := range t.metaData.AccelerationCoins {
		accelerationCoins = append(accelerationCoins, dex.Bytes(coin))
	}

	return swapCoins, accelerationCoins, dex.Bytes(t.metaData.ChangeCoin), requiredForRemainingSwaps, nil
}

func (t *trackedTrade) likelyTaker(midGap uint64) bool {
	if t.Type() == order.MarketOrderType {
		return true
	}
	lo := t.Order.(*order.LimitOrder)
	if lo.Force == order.ImmediateTiF {
		return true
	}

	if midGap == 0 {
		return false
	}

	if lo.Sell {
		return lo.Rate < midGap
	}

	return lo.Rate > midGap
}

func (t *trackedTrade) baseQty(midGap, lotSize uint64) uint64 {
	qty := t.Trade().Quantity

	if t.Type() == order.MarketOrderType && !t.Trade().Sell {
		if midGap == 0 {
			qty = lotSize
		} else {
			qty = calc.QuoteToBase(midGap, qty)
		}
	}

	return qty
}

func (t *trackedTrade) epochWeight(midGap, lotSize uint64) uint64 {
	if t.status() >= order.OrderStatusBooked {
		return 0
	}

	if t.likelyTaker(midGap) {
		return 2 * t.baseQty(midGap, lotSize)
	}

	return t.baseQty(midGap, lotSize)
}

func (t *trackedTrade) bookedWeight() uint64 {
	if t.status() != order.OrderStatusBooked {
		return 0
	}

	return t.Trade().Remaining()
}

func (t *trackedTrade) settlingWeight() (weight uint64) {
	for _, match := range t.matches {
		if (match.Side == order.Maker && match.Status >= order.MakerRedeemed) ||
			(match.Side == order.Taker && match.Status >= order.MatchComplete) {
			continue
		}
		weight += match.Quantity
	}
	return
}

func (t *trackedTrade) isEpochOrder() bool {
	return t.status() == order.OrderStatusEpoch
}

func (t *trackedTrade) marketWeight(midGap, lotSize uint64) uint64 {
	return t.epochWeight(midGap, lotSize) + t.bookedWeight() + t.settlingWeight()
}

// mapifyCoins converts the slice of coins to a map keyed by hex coin ID.
func mapifyCoins(coins asset.Coins) map[string]asset.Coin {
	coinMap := make(map[string]asset.Coin, len(coins))
	for _, c := range coins {
		var cid string
		if rc, is := c.(asset.RecoveryCoin); is {
			// Account coins are keyed by a coin that includes
			// address and value. The ID only returns address.
			cid = hex.EncodeToString(rc.RecoveryID())
		} else {
			cid = hex.EncodeToString(c.ID())
		}
		coinMap[cid] = c
	}
	return coinMap
}

func sellString(sell bool) string {
	if sell {
		return "sell"
	}
	return "buy"
}

func applyFraction(num, denom, target uint64) uint64 {
	return uint64(math.Round(float64(num) / float64(denom) * float64(target)))
}
