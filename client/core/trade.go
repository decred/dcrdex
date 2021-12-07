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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
)

const confCheckTimeout = 2 * time.Second

// ExpirationErr indicates that the wait.TickerQueue has expired a waiter, e.g.
// a reported coin was not found before the set expiration time.
type ExpirationErr string

// Error satisfies the error interface for ExpirationErr.
func (err ExpirationErr) Error() string { return string(err) }

// Ensure matchTracker satisfies the Stringer interface.
var _ (fmt.Stringer) = (*matchTracker)(nil)

// A matchTracker is used to negotiate a match.
type matchTracker struct {
	db.MetaMatch
	// swapErr is an error set when we have given up hope on broadcasting a swap
	// tx for a match. This can happen if 1) the swap has been attempted
	// (repeatedly), but could not be successfully broadcast before the
	// broadcast timeout, or 2) a match data was found to be in a nonsensical
	// state during startup.
	swapErr error
	// tickGovernor can be set non-nil to prevent swaps or redeems from
	// being attempted for a match. Typically, the *Timer comes from an
	// AfterFunc that itself nils the tickGovernor.
	tickGovernor *time.Timer
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
	// sendingInitAsync indicates if this match's init request is being sent to
	// the server and awaiting a response. No attempts will be made to send
	// another init request for this match while one is already active.
	sendingInitAsync uint32 // atomic
	// sendingRedeemAsync indicates if this match's redeem request is being sent
	// to the server and awaiting a response. No attempts will be made to send
	// another redeem request for this match while one is already active.
	sendingRedeemAsync uint32 // atomic
	// refundErr will be set to true if we attempt a refund and get a
	// CoinNotFoundError, indicating there is nothing to refund. Prevents
	// retries.
	refundErr   error
	prefix      *order.Prefix
	trade       *order.Trade
	counterSwap *asset.AuditInfo

	// cancelRedemptionSearch should be set when taker starts searching for
	// maker's redemption. Required to cancel a find redemption attempt if
	// taker successfully executes a refund.
	cancelRedemptionSearch context.CancelFunc

	// The following fields facilitate useful logging, while not being spammy.

	// counterConfirms records the last known confirms of the counterparty swap.
	// This is set in isSwappable for taker, isRedeemable for maker. -1 means
	// the confirms have not yet been checked (or logged).
	counterConfirms int64
	swapConfirms    int64
	// lastExpireDur is the most recently logged time until expiry of the
	// party's own contract. This may be negative if expiry has passed, but it
	// is not yet refundable due to other consensus rules. This is set in
	// isRefundable. Initialize this to a very large value to guarantee that it
	// will be logged on the first check or when 0.
	lastExpireDur time.Duration
	// checkServerRevoke is set to make sure that a taker will not prematurely
	// send an initialization until it is confirmed with the server that the
	// match is not revoked.
	checkServerRevoke bool
}

// matchTime returns the match's match time as a time.Time.
func (m *matchTracker) matchTime() time.Time {
	return encode.UnixTimeMilli(int64(m.MetaData.Proof.Auth.MatchStamp)).UTC()
}

// trackedCancel is information necessary to track a cancel order. A
// trackedCancel is always associated with a trackedTrade.
type trackedCancel struct {
	order.CancelOrder
	preImg  order.Preimage
	csum    dex.Bytes // the commitment checksum provided in the preimage request
	matches struct {
		maker *msgjson.Match
		taker *msgjson.Match
	}
}

// trackedTrade is an order (issued by this client), its matches, and its cancel
// order, if applicable. The trackedTrade has methods for handling requests
// from the DEX to progress match negotiation.
type trackedTrade struct {
	// redeemFeeSuggestion is cached fee suggestion for redemption. We can't
	// request a fee suggestion at redeem time because it would require making
	// the full redemption routine async (TODO?). This fee suggestion is
	// intentionally not stored as part of the db.OrderMetaData, and should
	// be repopulated if the client is restarted.
	// Used with atomic. Leave redeemFeeSuggestion as the first field in
	// trackedTrade.
	redeemFeeSuggestion uint64
	order.Order
	// mtx protects all read-write fields of the trackedTrade and the
	// matchTrackers in the matches map.
	mtx           sync.RWMutex
	metaData      *db.OrderMetaData
	dc            *dexConnection
	db            db.DB
	latencyQ      *wait.TickerQueue
	wallets       *walletSet
	preImg        order.Preimage
	csum          dex.Bytes // the commitment checksum provided in the preimage request
	mktID         string
	coins         map[string]asset.Coin
	coinsLocked   bool
	lockTimeTaker time.Duration
	lockTimeMaker time.Duration
	change        asset.Coin
	changeLocked  bool
	cancel        *trackedCancel
	matches       map[order.MatchID]*matchTracker
	notify        func(Notification)
	formatDetails func(Topic, ...interface{}) (string, string)
	epochLen      uint64
	fromAssetID   uint32
}

// newTrackedTrade is a constructor for a trackedTrade.
func newTrackedTrade(dbOrder *db.MetaOrder, preImg order.Preimage, dc *dexConnection, epochLen uint64,
	lockTimeTaker, lockTimeMaker time.Duration, db db.DB, latencyQ *wait.TickerQueue, wallets *walletSet,
	coins asset.Coins, notify func(Notification), formatDetails func(Topic, ...interface{}) (string, string)) *trackedTrade {

	fromID := dbOrder.Order.Quote()
	if dbOrder.Order.Trade().Sell {
		fromID = dbOrder.Order.Base()
	}

	ord := dbOrder.Order
	t := &trackedTrade{
		Order:         ord,
		metaData:      dbOrder.MetaData,
		dc:            dc,
		db:            db,
		latencyQ:      latencyQ,
		wallets:       wallets,
		preImg:        preImg,
		mktID:         marketName(ord.Base(), ord.Quote()),
		coins:         mapifyCoins(coins), // must not be nil even if empty
		coinsLocked:   len(coins) > 0,
		lockTimeTaker: lockTimeTaker,
		lockTimeMaker: lockTimeMaker,
		matches:       make(map[order.MatchID]*matchTracker),
		notify:        notify,
		epochLen:      epochLen,
		fromAssetID:   fromID,
		formatDetails: formatDetails,
	}
	return t
}

func (t *trackedTrade) epochIdx() uint64 {
	return encode.UnixMilliU(t.Prefix().ServerTime) / t.epochLen
}

// cancelEpochIdx gives the epoch index of any cancel associated cancel order.
// The mutex must be at least read locked.
func (t *trackedTrade) cancelEpochIdx() uint64 {
	if t.cancel == nil {
		return 0
	}
	return encode.UnixMilliU(t.cancel.Prefix().ServerTime) / t.epochLen
}

func (t *trackedTrade) verifyCSum(csum []byte, epochIdx uint64) error {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	// First check the trade's recorded csum, if it is in this epoch.
	if epochIdx == t.epochIdx() && !bytes.Equal(csum, t.csum) {
		return fmt.Errorf("checksum %s != trade order preimage request checksum %s for trade order %v",
			csum, t.csum, t.ID())
	}

	if t.cancel == nil {
		return nil // no linked cancel order
	}

	// Check the linked cancel order if it is for this epoch.
	if epochIdx == t.cancelEpochIdx() && !bytes.Equal(csum, t.cancel.csum) {
		return fmt.Errorf("checksum %s != cancel order preimage request checksum %s for cancel order %v",
			csum, t.cancel.csum, t.cancel.ID())
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

// delayTicks sets the tickGovernor to prevent retrying too quickly after an
// error.
func (t *trackedTrade) delayTicks(m *matchTracker, waitTime time.Duration) {
	m.tickGovernor = time.AfterFunc(waitTime, func() {
		t.mtx.Lock()
		defer t.mtx.Unlock()
		m.tickGovernor = nil
	})
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

	for _, mt := range t.matches {
		corder.Matches = append(corder.Matches, matchFromMetaMatchWithConfs(t,
			&mt.MetaMatch, mt.swapConfirms, int64(t.wallets.fromAsset.SwapConf), mt.counterConfirms, int64(t.wallets.toAsset.SwapConf)))
	}
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

// token is a shortened representation of the order ID.
func (t *trackedTrade) token() string {
	id := t.ID()
	return hex.EncodeToString(id[:4])
}

// cancelTrade sets the cancellation data with the order and its preimage.
// cancelTrade must be called with the mtx write-locked.
func (t *trackedTrade) cancelTrade(co *order.CancelOrder, preImg order.Preimage) error {
	t.cancel = &trackedCancel{
		CancelOrder: *co,
		preImg:      preImg,
	}
	err := t.db.LinkOrder(t.ID(), co.ID())
	if err != nil {
		return fmt.Errorf("error linking cancel order %s for trade %s: %w", co.ID(), t.ID(), err)
	}
	t.metaData.LinkedOrder = co.ID()
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
		t.cancel = nil
		t.metaData.LinkedOrder = order.OrderID{}

		subject, details := t.formatDetails(TopicMissedCancel, t.token())
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
		assets.count(t.wallets.fromAsset.ID)
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
	isMarketBuy := t.Type() == order.MarketOrderType && !trade.Sell

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
			counterConfirms: -1,
			lastExpireDur:   365 * 24 * time.Hour,
		}
		match.Status = order.NewlyMatched // these must be new matches
		newTrackers = append(newTrackers, match)
	}

	// Record any cancel order Match and update order status.
	if cancelMatch != nil {
		t.dc.log.Infof("Maker notification for cancel order received for order %s. match id = %s",
			t.ID(), cancelMatch.MatchID)

		// Set this order status to Canceled and unlock any locked coins
		// if there are no new matches and there's no need to send swap
		// for any previous match.
		t.metaData.Status = order.OrderStatusCanceled
		if len(newTrackers) == 0 {
			t.maybeReturnCoins()
		}

		t.cancel.matches.maker = cancelMatch // taker is stored via processCancelMatch before negotiate
		// Set the order status for the cancel order.
		err := t.db.UpdateOrderStatus(t.cancel.ID(), order.OrderStatusExecuted)
		if err != nil {
			t.dc.log.Errorf("Failed to update status of cancel order %v to executed: %v",
				t.cancel.ID(), err)
			// Try to store the match anyway.
		}
		// Store a completed maker cancel match in the DB.
		makerCancelMeta := t.makeMetaMatch(cancelMatch)
		makerCancelMeta.Status = order.MatchComplete
		makerCancelMeta.Address = "" // not a trade match
		err = t.db.UpdateMatch(makerCancelMeta)
		if err != nil {
			return fmt.Errorf("failed to update match in db: %w", err)
		}
	}

	// Now that each Match in msgMatches has been validated, store them in the
	// trackedTrade and the DB, and update the newFill amount.
	var newFill uint64
	for _, match := range newTrackers {
		var qty uint64
		if isMarketBuy {
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

	// Calculate and set the new filled value for the order.
	var filled uint64
	for _, mt := range t.matches {
		if isMarketBuy {
			filled += calc.BaseToQuote(mt.Rate, mt.Quantity)
		} else {
			filled += mt.Quantity
		}
	}
	// If the order has been canceled, add that to filled and newFill.
	if cancelMatch != nil {
		filled += cancelMatch.Quantity
		newFill += cancelMatch.Quantity
	}
	// The filled amount includes all of the trackedTrade's matches, so the
	// filled amount must be set, not just increased.
	trade.SetFill(filled)

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
	if cancelMatch != nil {
		subject, details := t.formatDetails(TopicOrderCanceled,
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), t.dc.acct.host, t.token())

		t.notify(newOrderNote(TopicOrderCanceled, subject, details, db.Poke, corder))
		// Also send out a data notification with the cancel order information.
		t.notify(newOrderNote(TopicCancel, "", "", db.Data, corder))
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
		subject, details := t.formatDetails(TopicMatchesMade,
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), fillPct, t.token())
		t.notify(newOrderNote(TopicMatchesMade, subject, details, db.Poke, corder))
	}

	err := t.db.UpdateOrder(t.metaOrder())
	if err != nil {
		return fmt.Errorf("failed to update order in db: %w", err)
	}
	return nil
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
	t.dc.log.Infof("Taker notification for cancel order %v received. Match id = %s", oid, mid)
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
func (t *trackedTrade) counterPartyConfirms(ctx context.Context, match *matchTracker) (have, needed uint32, changed, spent bool) {
	// Counter-party's swap is the "to" asset.
	needed = t.wallets.toAsset.SwapConf

	// Check the confirmations on the counter-party's swap. If counterSwap is
	// not set, we shouldn't be here, but catch this just in case.
	if match.counterSwap == nil {
		t.dc.log.Warnf("counterPartyConfirms: No AuditInfo available to check!")
		return
	}
	coin := match.counterSwap.Coin

	var err error
	have, spent, err = t.wallets.toWallet.SwapConfirmations(ctx, coin.ID(),
		match.MetaData.Proof.CounterContract, match.MetaData.Stamp)
	if err != nil {
		t.dc.log.Errorf("Failed to get confirmations of the counter-party's swap %s (%s) for match %s, order %v: %v",
			coin, t.wallets.toAsset.Symbol, match, t.UID(), err)
		return
	}

	// Log the pending swap status at new heights only.
	if match.counterConfirms != int64(have) {
		match.counterConfirms = int64(have)
		changed = true
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
// (see nomatch and negotiate)
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
	t.cancel = nil
	t.metaData.LinkedOrder = order.OrderID{} // NOTE: caller may wish to update the trades's DB entry
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
	if t.cancel == nil || t.metaData.Status != order.OrderStatusBooked {
		return
	}

	stamp := t.cancel.ServerTime
	epoch := order.EpochID{Idx: encode.UnixMilliU(stamp) / t.epochLen, Dur: t.epochLen}
	epochEnd := epoch.End()
	if time.Since(epochEnd).Milliseconds() < int64(2*t.epochLen) {
		return // not stuck, yet
	}

	t.dc.log.Infof("Cancel order %v in epoch status with server time stamp %v, epoch end %v (%v ago) considered executed and unmatched.",
		t.cancel.ID(), t.cancel.ServerTime, epochEnd, time.Since(epochEnd))

	// Clear the trackedCancel, allowing this order to be canceled again, and
	// set the cancel order's status as revoked.
	t.deleteCancelOrder()
	err := t.db.LinkOrder(t.ID(), order.OrderID{})
	if err != nil {
		t.dc.log.Errorf("DB error unlinking cancel order %s for trade %s: %v", t.cancel.ID(), t.ID(), err)
	}

	subject, details := t.formatDetails(TopicFailedCancel, t.token())
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
		proof := &match.MetaData.Proof
		t.dc.log.Tracef("Checking match %s (%v) in status %v. "+
			"Order: %v, Refund coin: %v, Script: %x, Revoked: %v", match,
			match.Side, match.Status, t.ID(),
			proof.RefundCoin, proof.Script, proof.IsRevoked())
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

// Matches are inactive if: (1) status is complete, (2) it is refunded, or (3)
// it is revoked and this side of the match requires no further action like
// refund or auto-redeem. This should not be applied to cancel order matches.
func (t *trackedTrade) matchIsActive(match *matchTracker) bool {
	proof := &match.MetaData.Proof
	isActive := db.MatchIsActive(match.UserMatch, proof)
	if proof.IsRevoked() && !isActive {
		t.dc.log.Tracef("Revoked match %s (%v) in status %v considered inactive.",
			match, match.Side, match.Status)
	}
	return isActive
}

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
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) isSwappable(ctx context.Context, match *matchTracker) bool {
	if match.swapErr != nil || match.MetaData.Proof.IsRevoked() || match.tickGovernor != nil || match.checkServerRevoke {
		t.dc.log.Tracef("Match %s not swappable: swapErr = %v, revoked = %v, metered = %t, checkServerRevoke = %v",
			match, match.swapErr, match.MetaData.Proof.IsRevoked(), match.tickGovernor != nil, match.checkServerRevoke)
		return false
	}

	wallet := t.wallets.fromWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually swappables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("not checking if order %s, match %s is swappable because %s wallet is not unlocked",
			t.ID(), match, unbip(wallet.AssetID))
		return false
	}

	if match.Status == order.MakerSwapCast {
		// Get the confirmation count on the maker's coin.
		if match.Side == order.Taker {
			toAssetID := t.wallets.toAsset.ID
			t.dc.log.Tracef("Checking confirmations on COUNTERPARTY swap txn %v (%s)...",
				coinIDString(toAssetID, match.MetaData.Proof.MakerSwap), unbip(toAssetID))
			// If the maker is the counterparty, we can determine swappability
			// based on the confirmations.
			confs, req, changed, spent := t.counterPartyConfirms(ctx, match)
			ready := confs >= req
			if changed && !ready {
				t.dc.log.Debugf("Match %s not yet swappable: current confs = %d, required confs = %d",
					match, confs, req)
			}
			if spent {
				t.dc.log.Errorf("Counter-party's swap is spent before we could broadcast our own")
				match.MetaData.Proof.SelfRevoked = true
				return false
			}
			return ready
		}
		// If we're the maker, check the confirmations anyway so we can notify.
		t.dc.log.Tracef("Checking confirmations on our OWN swap txn %v (%s)...",
			coinIDString(wallet.AssetID, match.MetaData.Proof.MakerSwap), unbip(wallet.AssetID))
		confs, spent, err := wallet.SwapConfirmations(ctx, match.MetaData.Proof.MakerSwap,
			match.MetaData.Proof.Script, match.MetaData.Stamp)
		if err != nil {
			t.dc.log.Errorf("error getting confirmation for our own swap transaction: %v", err)
		}
		if spent {
			t.dc.log.Debugf("our (maker) swap for match %s is being reported as spent, "+
				"but we have not seen the counter-party's redemption yet. This could just"+
				" be network latency.", match)
		}
		match.swapConfirms = int64(confs)
		t.notify(newMatchNote(TopicConfirms, "", "", db.Data, t, match))
		return false
	}
	if match.Side == order.Maker && match.Status == order.NewlyMatched {
		return true
	}
	return false
}

// isRedeemable will be true if the match is ready for our redemption to be
// broadcast.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) isRedeemable(ctx context.Context, match *matchTracker) bool {
	if match.swapErr != nil || len(match.MetaData.Proof.RefundCoin) != 0 || match.tickGovernor != nil {
		t.dc.log.Tracef("Match %s not redeemable: swapErr = %v, RefundCoin = %v, metered = %t",
			match, match.swapErr, match.MetaData.Proof.RefundCoin, match.tickGovernor != nil)
		return false
	}

	wallet := t.wallets.toWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually redeemables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("not checking if order %s, match %s is redeemable because %s wallet is not unlocked",
			t.ID(), match, unbip(wallet.AssetID))
		return false
	}

	if match.Status == order.TakerSwapCast {
		if match.Side == order.Maker {
			// Check the confirmations on the taker's swap.
			confs, req, changed, spent := t.counterPartyConfirms(ctx, match)
			ready := confs >= req
			if changed && !ready {
				t.dc.log.Debugf("Match %s not yet redeemable: current confs = %d, required confs = %d",
					match, confs, req)
			}
			if spent {
				t.dc.log.Errorf("Order %s, match %s counter-party's swap is spent before we could redeem", t.ID(), match)
				match.MetaData.Proof.SelfRevoked = true
				return false
			}
			return ready
		}
		// If we're the taker, check the confirmations anyway so we can notify.
		confs, spent, err := t.wallets.fromWallet.SwapConfirmations(ctx, match.MetaData.Proof.TakerSwap,
			match.MetaData.Proof.Script, match.MetaData.Stamp)
		if err != nil {
			t.dc.log.Errorf("error getting confirmation for our own swap transaction: %v", err)
		}
		if spent {
			t.dc.log.Debugf("our (taker) swap for match %s is being reported as spent, "+
				"but we have not seen the counter-party's redemption yet. This could just"+
				" be network latency.", match)
		}
		match.swapConfirms = int64(confs)
		t.notify(newMatchNote(TopicConfirms, "", "", db.Data, t, match))
		return false
	}
	if match.Side == order.Taker && match.Status == order.MakerRedeemed {
		return true
	}
	return false
}

// isRefundable will be true if all of the following are true:
// - We have broadcasted a swap contract (matchProof.Script != nil).
// - Neither party has redeemed (matchStatus < order.MakerRedeemed).
//   For Maker, this means we've not redeemed. For Taker, this means we've
//   not been notified of / we haven't yet found the Maker's redeem.
// - Our swap's locktime has expired.
//
// Those checks are skipped and isRefundable is false if we've already
// executed a refund or our refund-to wallet is locked.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) isRefundable(match *matchTracker) bool {
	if match.refundErr != nil || len(match.MetaData.Proof.RefundCoin) != 0 {
		t.dc.log.Tracef("Match %s not refundable: refundErr = %v, RefundCoin = %v",
			match, match.refundErr, match.MetaData.Proof.RefundCoin)
		return false
	}

	wallet := t.wallets.fromWallet
	// Just a quick check here. We'll perform a more thorough check if there are
	// actually refundables.
	if !wallet.locallyUnlocked() {
		t.dc.log.Errorf("not checking if order %s, match %s is refundable because %s wallet is not unlocked",
			t.ID(), match, unbip(wallet.AssetID))
		return false
	}

	// Return if we've NOT sent a swap OR a redeem has been
	// executed by either party.
	if len(match.MetaData.Proof.Script) == 0 || match.Status >= order.MakerRedeemed {
		return false
	}

	// Issue a refund if our swap's locktime has expired.
	swapLocktimeExpired, contractExpiry, err := wallet.LocktimeExpired(match.MetaData.Proof.Script)
	if err != nil {
		t.dc.log.Errorf("error checking if locktime has expired for %s contract on order %s, match %s: %v",
			match.Side, t.ID(), match, err)
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
	if match.lastExpireDur-expiresIn < logInterval {
		return false
	}

	// Record this log event's expiry duration.
	match.lastExpireDur = expiresIn

	swapCoinID := match.MetaData.Proof.TakerSwap
	if match.Side == order.Maker {
		swapCoinID = match.MetaData.Proof.MakerSwap
	}
	from := t.wallets.fromAsset
	remainingTime := expiresIn.Round(time.Second)
	assetSymb := strings.ToUpper(from.Symbol)
	var expireDetails string
	if remainingTime > 0 {
		expireDetails = fmt.Sprintf("expires at %v (%v).", contractExpiry, remainingTime)
	} else {
		expireDetails = fmt.Sprintf("expired %v ago, but additional blocks are required by the %s network.",
			-remainingTime, assetSymb)
	}
	t.dc.log.Infof("Contract for match %s with swap coin %v (%s) %s",
		match, coinIDString(from.ID, swapCoinID), assetSymb, expireDetails)

	return false
}

// shouldBeginFindRedemption will be true if we are the Taker on this match,
// we've broadcasted a swap, our swap has gotten the required confs, the match
// was revoked without receiving a valid notification of Maker's redeem and
// we've not refunded our swap.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) shouldBeginFindRedemption(ctx context.Context, match *matchTracker) bool {
	proof := &match.MetaData.Proof
	if !proof.IsRevoked() {
		return false // Only auto-find redemption for revoked/failed matches.
	}
	swapCoinID := proof.TakerSwap
	if match.Side != order.Taker || len(swapCoinID) == 0 || len(proof.MakerRedeem) > 0 || len(proof.RefundCoin) > 0 {
		t.dc.log.Tracef(
			"Not finding redemption for match %s: side = %s, swapErr = %v, TakerSwap = %v RefundCoin = %v",
			match, match.Side, match.swapErr, proof.TakerSwap, proof.RefundCoin)
		return false
	}
	if match.cancelRedemptionSearch != nil { // already finding redemption
		return false
	}

	confs, spent, err := t.wallets.fromWallet.SwapConfirmations(ctx, swapCoinID, proof.Script, match.MetaData.Stamp)
	if err != nil {
		t.dc.log.Errorf("Failed to get confirmations of the taker's swap %s (%s) for match %s, order %v: %v",
			coinIDString(t.wallets.fromAsset.ID, swapCoinID), t.wallets.fromAsset.Symbol, match, t.UID(), err)
		return false
	}
	if spent {
		t.dc.log.Infof("Swap contract for revoked match %s, order %s is spent. Will begin search for redemption", match, t.ID())
	}
	return confs >= t.wallets.fromAsset.SwapConf || spent
}

// tick will check for and perform any match actions necessary.
func (c *Core) tick(t *trackedTrade) (assetMap, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var swaps, redeems, refunds []*matchTracker
	assets := make(assetMap)
	errs := newErrorSet(t.dc.acct.host + " tick: ")

	// Check all matches for and resend pending requests as necessary.
	c.resendPendingRequests(t)

	// Make sure we have a redemption fee suggestion cached.
	c.cacheRedemptionFeeSuggestion(t)

	// Check all matches and send swap, redeem or refund as necessary.
	var sent, quoteSent, received, quoteReceived uint64
	for _, match := range t.matches {
		side := match.Side
		if (side == order.Maker && match.Status >= order.MakerRedeemed) ||
			(side == order.Taker && match.Status >= order.MatchComplete) {
			continue
		}
		if match.Address == "" {
			continue // a cancel order match
		}
		if !t.matchIsActive(match) {
			continue // either refunded or revoked requiring no action on this side of the match
		}
		switch {
		case t.isSwappable(c.ctx, match):
			t.dc.log.Debugf("Swappable match %s for order %v (%v)", match, t.ID(), side)
			swaps = append(swaps, match)
			sent += match.Quantity
			quoteSent += calc.BaseToQuote(match.Rate, match.Quantity)

		case t.isRedeemable(c.ctx, match):
			t.dc.log.Debugf("Redeemable match %s for order %v (%v)", match, t.ID(), side)
			redeems = append(redeems, match)
			received += match.Quantity
			quoteReceived += calc.BaseToQuote(match.Rate, match.Quantity)

		// Check refundability before checking if to start finding redemption.
		// Ensures that redemption search is not started if locktime has expired.
		// If we've already started redemption search for this match, the search
		// will be aborted if/when auto-refund succeeds.
		case t.isRefundable(match):
			t.dc.log.Debugf("Refundable match %s for order %v (%v)", match, t.ID(), side)
			refunds = append(refunds, match)

		case t.shouldBeginFindRedemption(c.ctx, match):
			t.dc.log.Debugf("Ready to find counter-party redemption for match %s, order %v (%v)", match, t.ID(), side)
			t.findMakersRedemption(match)
		}
	}

	fromID := t.wallets.fromAsset.ID
	if len(swaps) > 0 || len(refunds) > 0 {
		assets.count(fromID)
	}

	if len(swaps) > 0 {
		didUnlock, err := t.wallets.fromWallet.refreshUnlock()
		if err != nil {
			// Just log it and try anyway.
			c.log.Errorf("refreshUnlock error swapping %s: %v", t.wallets.fromAsset.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet while sending a swap", t.wallets.fromAsset.Symbol)
		}
		qty := sent
		if !t.Trade().Sell {
			qty = quoteSent
		}
		err = c.swapMatches(t, swaps)

		// swapMatches might modify the matches, so don't get the *Order for
		// notifications before swapMatches.
		corder := t.coreOrderInternal()
		ui := t.wallets.fromWallet.Info().UnitInfo
		if err != nil {
			errs.addErr(err)
			subject, details := c.formatDetails(TopicSwapSendError, ui.ConventionalString(qty), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicSwapSendError, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicSwapsInitiated, ui.ConventionalString(qty), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicSwapsInitiated, subject, details, db.Poke, corder))
		}
	}

	if len(redeems) > 0 {
		didUnlock, err := t.wallets.toWallet.refreshUnlock()
		if err != nil {
			// Just log it and try anyway.
			c.log.Errorf("refreshUnlock error redeeming %s: %v", t.wallets.toAsset.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet while sending a redemption", t.wallets.toAsset.Symbol)
		}
		toAsset := t.wallets.toAsset.ID
		assets.count(toAsset)
		assets.count(t.fromAssetID) // update the from wallet balance to reduce contractlocked balance
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
				ui.ConventionalString(qty), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicRedemptionError, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicMatchComplete,
				ui.ConventionalString(qty), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicMatchComplete, subject, details, db.Poke, corder))
		}
	}

	if len(refunds) > 0 {
		didUnlock, err := t.wallets.fromWallet.refreshUnlock()
		if err != nil {
			// Just log it and try anyway.
			c.log.Errorf("refreshUnlock error refunding %s: %v", t.wallets.fromAsset.Symbol, err)
		}
		if didUnlock {
			c.log.Infof("Unexpected unlock needed for the %s wallet while sending a refund", t.wallets.fromAsset.Symbol)
		}
		refunded, err := c.refundMatches(t, refunds)
		corder := t.coreOrderInternal()
		ui := t.wallets.fromWallet.Info().UnitInfo
		if err != nil {
			errs.addErr(err)
			subject, details := c.formatDetails(TopicRefundFailure,
				ui.ConventionalString(refunded), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicRefundFailure, subject, details, db.ErrorLevel, corder))
		} else {
			subject, details := c.formatDetails(TopicMatchesRefunded,
				ui.ConventionalString(refunded), ui.Conventional.Unit, t.token())
			t.notify(newOrderNote(TopicMatchesRefunded, subject, details, db.WarningLevel, corder))
		}
	}

	return assets, errs.ifAny()
}

// resendPendingRequests checks all matches for this order to re-attempt
// sending the `init` or `redeem` request where necessary.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) resendPendingRequests(t *trackedTrade) {
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
		case side == order.Maker && status == order.MakerRedeemed:
			redeemCoinID = proof.MakerRedeem
		case side == order.Taker && status == order.MatchComplete:
			redeemCoinID = proof.TakerRedeem
		}
		if len(swapCoinID) != 0 && len(auth.InitSig) == 0 { // resend pending `init` request
			c.sendInitAsync(t, match, swapCoinID, proof.Script)
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
}

// revokeMatch sets the status as revoked for the specified match. revokeMatch
// must be called with the mtx write-locked.
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

	// Unlock coins if we're not expecting future matches for this
	// trade and there are no matches that MAY later require sending
	// swaps.
	t.maybeReturnCoins()

	t.dc.log.Warnf("Match %v revoked in status %v for order %v", matchID, revokedMatch.Status, t.ID())
	return nil
}

// swapMatches will send a transaction with swaps for the specified matches.
// The matches will be de-grouped so that matches marked as suspect are swapped
// individually and separate from the non-suspect group.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) swapMatches(t *trackedTrade, matches []*matchTracker) error {
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
	if len(groupables) > 0 {
		c.swapMatchGroup(t, groupables, errs)
	}
	for _, m := range suspects {
		c.swapMatchGroup(t, []*matchTracker{m}, errs)
	}
	return errs.ifAny()
}

// swapMatchGroup will send a transaction with swap outputs for the specified
// matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) swapMatchGroup(t *trackedTrade, matches []*matchTracker, errs *errorSet) {
	// Prepare the asset.Contracts.
	contracts := make([]*asset.Contract, len(matches))
	// These matches may have different fee rates, matched in different epochs.
	var highestFeeRate uint64
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

		if match.FeeRateSwap > highestFeeRate {
			highestFeeRate = match.FeeRateSwap
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
	fromAsset := t.wallets.fromAsset
	coinIDs := t.Trade().Coins
	if len(t.metaData.ChangeCoin) > 0 {
		coinIDs = []order.CoinID{t.metaData.ChangeCoin}
		c.log.Debugf("Using stored change coin %v (%v) for order %v matches",
			coinIDString(fromAsset.ID, coinIDs[0]), fromAsset.Symbol, t.ID())
	}

	inputs := make([]asset.Coin, len(coinIDs))
	for i, coinID := range coinIDs {
		coin, found := t.coins[hex.EncodeToString(coinID)]
		if !found {
			errs.add("%s coin %s not found", fromAsset.Symbol, coinIDString(fromAsset.ID, coinID))
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
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      highestFeeRate,
		LockChange:   lockChange,
		AssetVersion: t.metaData.FromVersion,
	}
	receipts, change, fees, err := t.wallets.fromWallet.Swap(swaps)
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
				lastActionTime = encode.UnixTimeMilli(int64(auditStamp))
			}
			if time.Since(lastActionTime) < bTimeout ||
				(auditStamp == 0 && match.swapErrCount < tickCheckDivisions) {
				t.delayTicks(match, tickInterval*3/4)
			} else {
				// If we can't get a swap out before the broadcast timeout, just
				// quit. We could also self-revoke here, but we're also
				// expecting a revocation from the server, so relying on that
				// one for now.
				match.swapErr = err
			}
		}
		errs.add("error sending swap transaction: %v", err)
		return
	}

	refundTxs := ""
	for i, r := range receipts {
		refundTxs = fmt.Sprintf("%s%q: %s", refundTxs, r.Coin(), r.SignedRefund())
		if i != len(receipts)-1 {
			refundTxs = fmt.Sprintf("%s, ", refundTxs)
		}
	}

	c.log.Infof("Broadcasted transaction with %d swap contracts for order %v. Fee rate = %d. Receipts (%s): %v.",
		len(receipts), t.ID(), swaps.FeeRate, t.wallets.fromAsset.Symbol, receipts)
	c.log.Infof("The following are contract identifiers mapped to raw refund "+
		"transactions that are only valid after the swap contract expires. "+
		"These are fallback transactions that can be used to return funds "+
		"to your wallet in the case dexc no longer functions. They SHOULD "+
		"NOT be used if dexc is running without error. dexc will refund "+
		"failed contracts automatically.\nRefund Txs: {%s}", refundTxs)

	// If this is the first swap (and even if not), the funding coins
	// would have been spent and unlocked.
	t.coinsLocked = false
	t.changeLocked = lockChange
	t.metaData.SwapFeesPaid += fees

	if change == nil {
		t.metaData.ChangeCoin = nil
	} else {
		t.coins[hex.EncodeToString(change.ID())] = change
		t.metaData.ChangeCoin = []byte(change.ID())
		c.log.Debugf("Saving change coin %v (%v) to DB for order %v",
			coinIDString(fromAsset.ID, t.metaData.ChangeCoin), fromAsset.Symbol, t.ID())
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
		c.log.Infof("Contract coin %v (%s), value = %d, refundable at %v (script = %v), match = %v",
			coin, t.wallets.fromAsset.Symbol, coin.Value(), receipt.Expiration(), receipt.Contract(), match)
		if secret := match.MetaData.Proof.Secret; len(secret) > 0 {
			c.log.Tracef("Contract coin %v secret = %x", coin, secret)
		}

		// Update the match db data with the swap details before attempting
		// to notify the server of the swap.
		proof := &match.MetaData.Proof
		contract, coinID := receipt.Contract(), []byte(coin.ID())
		proof.Script = contract
		if match.Side == order.Taker {
			proof.TakerSwap = coinID
			match.Status = order.TakerSwapCast
		} else {
			proof.MakerSwap = coinID
			match.Status = order.MakerSwapCast
		}
		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			errs.add("error storing swap details in database for match %s, coin %s: %v",
				match, coinIDString(t.wallets.fromAsset.ID, coinID), err)
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

	c.log.Infof("Notifying DEX %s of our %s swap contract %v for match %s",
		t.dc.acct.host, t.wallets.fromAsset.Symbol, coinIDString(t.fromAssetID, coinID), match)

	// Send the init request asynchronously.
	c.wg.Add(1) // So Core does not shut down until we're done with this request.
	go func() {
		var err error
		defer func() {
			c.wg.Done()
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
			if errors.As(err, &msgErr) && msgErr.Code == msgjson.SettlementSequenceError {
				c.log.Errorf("Starting match status resolution for 'init' request SettlementSequenceError")
				c.resolveMatchConflicts(t.dc, map[order.OrderID]*matchStatusConflict{
					t.ID(): {
						trade:   t,
						matches: []*matchTracker{match},
					},
				})
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
		auth.InitStamp = encode.UnixMilliU(time.Now())
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
func (c *Core) redeemMatches(t *trackedTrade, matches []*matchTracker) error {
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
		c.redeemMatchGroup(t, groupables, errs)
	}
	for _, m := range suspects {
		c.redeemMatchGroup(t, []*matchTracker{m}, errs)
	}
	return errs.ifAny()
}

// redeemMatchGroup will send a transaction redeeming the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) redeemMatchGroup(t *trackedTrade, matches []*matchTracker, errs *errorSet) {
	// Collect a asset.Redemption for each match into a slice of redemptions that
	// will be grouped into a single transaction.
	redemptions := make([]*asset.Redemption, 0, len(matches))
	for _, match := range matches {
		redemptions = append(redemptions, &asset.Redemption{
			Spends: match.counterSwap,
			Secret: match.MetaData.Proof.Secret,
		})
	}

	// Don't use (*Core).feeSuggestion here, since can incur an RPC request.
	// If we don't have a synced book, use t.redemption
	feeSuggestion := t.dc.bestBookFeeSuggestion(t.wallets.toAsset.ID)
	if feeSuggestion == 0 {
		feeSuggestion = atomic.LoadUint64(&t.redeemFeeSuggestion)
	}

	// Send the transaction.
	redeemWallet, redeemAsset := t.wallets.toWallet, t.wallets.toAsset // this is our redeem
	coinIDs, outCoin, fees, err := redeemWallet.Redeem(&asset.RedeemForm{
		Redemptions:   redemptions,
		FeeSuggestion: feeSuggestion,
		AssetVersion:  t.metaData.ToVersion,
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
			lastActionTime := encode.UnixTimeMilli(int64(lastActionStamp))
			// Try to wait until about the next auto-tick to try again.
			waitTime := tickInterval * 3 / 4
			if time.Since(lastActionTime) > bTimeout ||
				(lastActionStamp == 0 && match.redeemErrCount >= tickCheckDivisions) {
				// If we already missed the broadcast timeout, we're not in as
				// much of a hurry. but keep trying and sending errors, because
				// we do want the user to recover.
				waitTime = 15 * time.Minute
			}
			t.delayTicks(match, waitTime)
		}
		errs.add("error sending redeem transaction: %v", err)
		return
	}

	c.log.Infof("Broadcasted redeem transaction spending %d contracts for order %v, paying to %s (%s)",
		len(redemptions), t.ID(), outCoin, redeemAsset.Symbol)

	t.metaData.RedemptionFeesPaid += fees

	err = t.db.UpdateOrderMetaData(t.ID(), t.metaData)
	if err != nil {
		c.log.Errorf("Error updating order metadata for order %s: %v", t.ID(), err)
	}

	for _, match := range matches {
		c.log.Infof("Match %s complete: %s %d %s", match, sellString(t.Trade().Sell),
			match.Quantity, unbip(t.Prefix().BaseAsset),
		)
	}

	// Save redemption details and send the redeem message to the DEX.
	// Saving the redemption details now makes it possible to resend the
	// `redeem` request at a later time if sending it now fails.
	for i, match := range matches {
		proof := &match.MetaData.Proof
		coinID := []byte(coinIDs[i])
		if match.Side == order.Taker {
			match.Status = order.MatchComplete // could this cause the match to be retired before the `redeem` request succeeds?
			proof.TakerRedeem = coinID
		} else {
			match.Status = order.MakerRedeemed
			proof.MakerRedeem = coinID
		}
		if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
			errs.add("error storing swap details in database for match %s, coin %s: %v",
				match, coinIDString(t.wallets.fromAsset.ID, coinID), err)
		}

		c.sendRedeemAsync(t, match, coinIDs[i], proof.Secret)
	}
}

// sendRedeemAsync starts a goroutine to send a `redeem` request for the specified
// match and save the server's ack sig to db. Sends a notification if an error
// occurs while sending the request or validating the server's response.
func (c *Core) sendRedeemAsync(t *trackedTrade, match *matchTracker, coinID, secret []byte) {
	if !atomic.CompareAndSwapUint32(&match.sendingRedeemAsync, 0, 1) {
		return
	}

	c.log.Infof("Notifying DEX %s of our %s swap redemption %v for match %s",
		t.dc.acct.host, t.wallets.toAsset.Symbol, coinIDString(t.wallets.toAsset.ID, coinID), match)

	// Send the redeem request asynchronously.
	c.wg.Add(1) // So Core does not shut down until we're done with this request.
	go func() {
		var err error
		defer func() {
			c.wg.Done()
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
			if errors.As(err, &msgErr) && msgErr.Code == msgjson.SettlementSequenceError {
				c.log.Errorf("Starting match status resolution for 'redeem' request SettlementSequenceError")
				c.resolveMatchConflicts(t.dc, map[order.OrderID]*matchStatusConflict{
					t.ID(): {
						trade:   t,
						matches: []*matchTracker{match},
					},
				})
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
		auth.RedeemStamp = encode.UnixMilliU(time.Now())
		if match.Side == order.Maker {
			// As maker, this is the end. However, this diverges from server,
			// which still needs taker's redeem.
			match.Status = order.MatchComplete
		}
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			err = fmt.Errorf("error storing redeem ack sig in database: %v", err)
		}
		t.mtx.Unlock()
	}()
}

// findMakersRedemption starts a goroutine to search for the redemption of
// taker's contract.
//
// This method modifies trackedTrade fields and MUST be called with the
// trackedTrade mutex lock held for writes.
func (t *trackedTrade) findMakersRedemption(match *matchTracker) {
	if match.cancelRedemptionSearch != nil {
		return
	}

	// TODO: Should copy Core's ctx to auto-cancel this ctx when Core is shut down.
	ctx, cancel := context.WithCancel(context.TODO())
	match.cancelRedemptionSearch = cancel
	swapCoinID := dex.Bytes(match.MetaData.Proof.TakerSwap)

	// Run redemption finder in goroutine.
	go func() {
		redemptionCoinID, secret, err := t.wallets.fromWallet.FindRedemption(ctx, swapCoinID)

		// Redemption search done, with or without error.
		// Keep the mutex locked for the remainder of this goroutine execution to
		// read and write match fields while processing the find redemption result.
		t.mtx.Lock()
		defer t.mtx.Unlock()

		match.cancelRedemptionSearch = nil
		fromAsset := t.wallets.fromAsset

		if err != nil {
			// Ignore the error if we've refunded, the error would likely be context
			// canceled or secret parse error (if redemption search encountered the
			// refund before the search could be canceled).
			if ctx.Err() != nil || len(match.MetaData.Proof.RefundCoin) == 0 {
				t.dc.log.Errorf("Error finding redemption of taker's %s contract %s for order %s, match %s: %v.",
					fromAsset.Symbol, coinIDString(fromAsset.ID, swapCoinID), t.ID(), match, err)
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
			t.dc.log.Errorf("Found invalid redemption of taker's %s contract %s for order %s, match %s: invalid secret %s, hash %s.",
				fromAsset.Symbol, coinIDString(fromAsset.ID, swapCoinID), t.ID(), match, secret, proof.SecretHash)
			return
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

		subject, details := t.formatDetails(TopicMatchRecovered,
			fromAsset.Symbol, coinIDString(fromAsset.ID, redemptionCoinID), match)
		t.notify(newOrderNote(TopicMatchRecovered, subject, details, db.Poke, t.coreOrderInternal()))
	}()
}

// refundMatches will send refund transactions for the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (c *Core) refundMatches(t *trackedTrade, matches []*matchTracker) (uint64, error) {
	errs := newErrorSet("refundMatches: order %s - ", t.ID())

	refundWallet, refundAsset := t.wallets.fromWallet, t.wallets.fromAsset // refunding to our wallet
	var refundedQty uint64

	for _, match := range matches {
		if len(match.MetaData.Proof.RefundCoin) != 0 {
			t.dc.log.Errorf("attempted to execute duplicate refund for match %s, side %s, status %s",
				match, match.Side, match.Status)
			continue
		}
		contractToRefund := match.MetaData.Proof.Script
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
			t.dc.log.Errorf("attempted to execute invalid refund for match %s, side %s, status %s",
				match, match.Side, match.Status)
			continue
		}

		swapCoinString := coinIDString(refundAsset.ID, swapCoinID)
		t.dc.log.Infof("Refunding %s contract %s for match %s (%s)",
			refundAsset.Symbol, swapCoinString, match, matchFailureReason)

		feeSuggestion := c.feeSuggestion(t.dc, refundAsset.ID, refundAsset.ID == t.Base())
		refundCoin, err := refundWallet.Refund(swapCoinID, contractToRefund, feeSuggestion)
		if err != nil {
			if errors.Is(err, asset.CoinNotFoundError) && match.Side == order.Taker {
				match.refundErr = err
				// Could not find the contract coin, which means it has been spent.
				// We should have already started FindRedemption for this contract,
				// but let's do it again to ensure we find the secret.
				t.dc.log.Debugf("Failed to refund %s contract %s, already redeemed. Beginning find redemption.",
					refundAsset.Symbol, swapCoinString)
				t.findMakersRedemption(match)
			} else {
				t.delayTicks(match, time.Minute*5)
				errs.add("error sending refund tx for match %s, swap coin %s: %v",
					match, swapCoinString, err)
			}
			continue
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
			contractID := coinIDString(t.wallets.toAsset.ID, audit.CoinID)
			t.dc.log.Errorf("Failed to audit contract coin %v (%s) for match %s: %v",
				contractID, t.wallets.toAsset.Symbol, match, err)
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
	contractID, contractSymb := coinIDString(t.wallets.toAsset.ID, coinID), t.wallets.toAsset.Symbol
	tLastWarning := time.Now()
	t.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(24 * time.Hour), // effectively forever
		TryFunc: func() bool {
			var err error
			auditInfo, err = t.wallets.toWallet.AuditContract(coinID, contract, txData, true)
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

	contractID, contractSymb := coinIDString(t.wallets.toAsset.ID, coinID), t.wallets.toAsset.Symbol

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
				auditInfo.Coin, t.wallets.toAsset.Symbol, contract, proof.SecretHash, auditInfo.SecretHash)
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
		"with tx data = %t. Script: %x", t.wallets.toAsset.Symbol, auditInfo.Coin,
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
	}

	sigMsg := redemption.Serialize()
	err := t.dc.acct.checkSig(sigMsg, redemption.Sig)
	if err != nil {
		// Log, but don't quit.
		errs.add("server redemption signature error: %v", err)
	}
	// Respond to the DEX.
	err = t.dc.ack(msgID, match.MatchID, redemption)
	if err != nil {
		return errs.add("Audit - %v", err)
	}

	// Update the database.
	match.MetaData.Proof.Auth.RedemptionSig = redemption.Sig
	match.MetaData.Proof.Auth.RedemptionStamp = redemption.Time

	if match.Side == order.Taker {
		err = t.processMakersRedemption(match, redemption.CoinID, redemption.Secret)
		if err != nil {
			errs.addErr(err)
		}
	}
	// The server does not send a redemption request to the maker for the
	// taker's redeem since match negotiation is complete server-side when the
	// taker redeems, and it is complete client-side when we redeem. If we are
	// both sides, there is another trackedTrade and matchTracker for that side.

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

	redeemAsset := t.wallets.fromAsset
	t.dc.log.Infof("Notified of maker's redemption (%s: %v) and validated secret for order %v...",
		redeemAsset.Symbol, coinIDString(redeemAsset.ID, coinID), t.ID())

	match.Status = order.MakerRedeemed
	proof.MakerRedeem = coinID
	proof.Secret = secret
	return nil
}

// Coins will be returned if
// - the trade status is not OrderStatusEpoch or OrderStatusBooked, that is to
//   say, there won't be future matches for this order.
// - there are no matches in the trade that MAY later require sending swaps,
//   that is to say, all matches have been either swapped or revoked.
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
	if t.change == nil && t.coinsLocked {
		fundingCoins := make([]asset.Coin, 0, len(t.coins))
		for _, coin := range t.coins {
			fundingCoins = append(fundingCoins, coin)
		}
		err := t.wallets.fromWallet.ReturnCoins(fundingCoins)
		if err != nil {
			t.dc.log.Warnf("Unable to return %s funding coins: %v", unbip(t.wallets.fromAsset.ID), err)
		} else {
			t.coinsLocked = false
		}
	} else if t.change != nil && t.changeLocked {
		err := t.wallets.fromWallet.ReturnCoins(asset.Coins{t.change})
		if err != nil {
			t.dc.log.Warnf("Unable to return %s change coin %v: %v", unbip(t.wallets.fromAsset.ID), t.change, err)
		} else {
			t.changeLocked = false
		}
	}
}

// mapifyCoins converts the slice of coins to a map keyed by hex coin ID.
func mapifyCoins(coins asset.Coins) map[string]asset.Coin {
	coinMap := make(map[string]asset.Coin, len(coins))
	for _, c := range coins {
		cid := hex.EncodeToString(c.ID())
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
