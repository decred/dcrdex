// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
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

// ExpirationErr indicates that the wait.TickerQueue has expired a waiter, e.g.
// a reported coin was not found before the set expiration time.
type ExpirationErr string

// Error satisfies the error interface for ExpirationErr.
func (err ExpirationErr) Error() string { return string(err) }

// A matchTracker is used to negotiate a match.
type matchTracker struct {
	db.MetaMatch
	id          order.MatchID
	failErr     error
	refundErr   error
	prefix      *order.Prefix
	trade       *order.Trade
	counterSwap asset.AuditInfo

	// cancelRedemptionSearch should be set when taker starts searching for
	// maker's redemption. Required to cancel a find redemption attempt if
	// taker successfully executes a refund.
	cancelRedemptionSearch context.CancelFunc

	// The following fields facilitate useful logging, while not being spammy.

	// counterConfirms records the last known confirms of the counterparty swap.
	// This is set in isSwappable for taker, isRedeemable for maker. -1 means
	// the confirms have not yet been checked (or logged).
	counterConfirms int64
	// lastExpireDur is the most recently logged time until expiry of the
	// party's own contract. This may be negative if expiry has passed, but it
	// is not yet refundable due to other consensus rules. This is set in
	// isRefundable. Initialize this to a very large value to guarantee that it
	// will be logged on the first check or when 0.
	lastExpireDur time.Duration
}

// parts is a getter for pointers to commonly used struct fields in the
// matchTracker.
func (match *matchTracker) parts() (*order.UserMatch, *db.MatchMetaData, *db.MatchProof, *db.MatchAuth) {
	dbMatch, metaData := match.Match, match.MetaData
	proof, auth := &metaData.Proof, &metaData.Proof.Auth
	return dbMatch, metaData, proof, auth
}

// trackedCancel is information necessary to track a cancel order. A
// trackedCancel is always associated with a trackedTrade.
type trackedCancel struct {
	order.CancelOrder
	preImg  order.Preimage
	matches struct {
		maker *msgjson.Match
		taker *msgjson.Match
	}
}

// trackedTrade is an order, its matches, and its cancel order, if applicable.
// The trackedTrade has methods for handling requests from the DEX to progress
// match negotiation.
type trackedTrade struct {
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
	epochLen      uint64
	fromAssetID   uint32
}

// newTrackedTrade is a constructor for a trackedTrade.
func newTrackedTrade(dbOrder *db.MetaOrder, preImg order.Preimage, dc *dexConnection, epochLen uint64,
	lockTimeTaker, lockTimeMaker time.Duration, db db.DB, latencyQ *wait.TickerQueue, wallets *walletSet,
	coins asset.Coins, notify func(Notification)) *trackedTrade {

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
		coins:         mapifyCoins(coins),
		coinsLocked:   true,
		lockTimeTaker: lockTimeTaker,
		lockTimeMaker: lockTimeMaker,
		matches:       make(map[order.MatchID]*matchTracker),
		notify:        notify,
		epochLen:      epochLen,
		fromAssetID:   fromID,
	}
	return t
}

// rate returns the order's rate, or zero if a market or cancel order.
func (t *trackedTrade) rate() uint64 {
	if ord, ok := t.Order.(*order.LimitOrder); ok {
		return ord.Rate
	}
	return 0
}

// broadcastTimeout gets associated DEX's configured broadcast timeout.
func (t *trackedTrade) broadcastTimeout() time.Duration {
	return time.Millisecond * time.Duration(t.dc.cfg.BroadcastTimeout)
}

// coreOrder constructs a *core.Order for the tracked order.Order. If the trade
// has a cancel order associated with it, the cancel order will be returned,
// otherwise the second returned *Order will be nil.
func (t *trackedTrade) coreOrder() (*Order, *Order) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return t.coreOrderInternal()
}

// coreOrderInternal constructs a *core.Order for the tracked order.Order. If
// the trade has a cancel order associated with it, the cancel order will be
// returned, otherwise the second returned *Order will be nil. coreOrderInternal
// should be called with the mtx >= RLocked.
func (t *trackedTrade) coreOrderInternal() (*Order, *Order) {
	corder := coreOrderFromTrade(t.Order, t.metaData)
	corder.Epoch = t.dc.marketEpoch(t.mktID, t.Prefix().ServerTime)

	for _, mt := range t.matches {
		corder.Matches = append(corder.Matches, matchFromMetaMatch(&mt.MetaMatch))
	}
	var cancelOrder *Order
	if t.cancel != nil {
		cancelOrder = &Order{
			Host:     t.dc.acct.host,
			MarketID: t.mktID,
			Type:     order.CancelOrderType,
			Stamp:    encode.UnixMilliU(t.cancel.ServerTime),
			Epoch:    t.dc.marketEpoch(t.mktID, t.Prefix().ServerTime),
			TargetID: t.cancel.TargetOrderID[:],
		}
	}
	return corder, cancelOrder
}

// token is a shortened representation of the order ID.
func (t *trackedTrade) token() string {
	id := t.ID()
	return hex.EncodeToString(id[:4])
}

// cancelTrade sets the cancellation data with the order and its preimage.
func (t *trackedTrade) cancelTrade(co *order.CancelOrder, preImg order.Preimage) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
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
		cid := t.cancel.ID()
		log.Warnf("cancel order %s did not match for order %s.", cid, t.ID())
		err := t.db.LinkOrder(t.ID(), order.OrderID{})
		if err != nil {
			log.Errorf("DB error unlinking cancel order %s for trade %s: %w", cid, t.ID(), err)
		}
		// Clearing the trackedCancel allows this order to be canceled again.
		t.cancel = nil
		t.metaData.LinkedOrder = order.OrderID{}

		details := fmt.Sprintf("Cancel order did not match for order %s. This can happen if the cancel order is submitted in the same epoch as the trade or if the target order is fully executed before matching with the cancel order.", t.token())
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("Missed cancel", details, db.WarningLevel, corder))
		return assets, t.db.UpdateOrderStatus(cid, order.OrderStatusExecuted)
	}
	// This is the trade. Return coins and set status based on whether this is
	// a standing limit order or not.
	if t.metaData.Status != order.OrderStatusEpoch {
		return assets, fmt.Errorf("nomatch sent for non-epoch order %s", oid)
	}
	if lo, ok := t.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
		log.Infof("Standing order %s did not match and is now booked.", t.token())
		t.metaData.Status = order.OrderStatusBooked
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("Order booked", "", db.Data, corder))
	} else {
		t.returnCoins()
		assets.count(t.wallets.fromAsset.ID)
		log.Infof("Non-standing order %s did not match.", t.token())
		t.metaData.Status = order.OrderStatusExecuted
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("No match", "", db.Data, corder))
	}
	return assets, t.db.UpdateOrderStatus(t.ID(), t.metaData.Status)
}

// negotiate creates and stores matchTrackers for the []*msgjson.Match, and
// updates (UserMatch).Filled. Match negotiation can then be progressed by
// calling (*trackedTrade).tick when a relevant event occurs, such as a request
// from the DEX or a tip change.
func (t *trackedTrade) negotiate(msgMatches []*msgjson.Match) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()

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
			log.Warnf("Skipping match %v that is already negotiating.", mid)
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
			id:              mid,
			prefix:          t.Prefix(),
			trade:           trade,
			MetaMatch:       *t.makeMetaMatch(msgMatch),
			counterConfirms: -1,
			lastExpireDur:   365 * 24 * time.Hour,
		}
		match.SetStatus(order.NewlyMatched) // these must be new matches
		newTrackers = append(newTrackers, match)
	}

	// Record any cancel order Match and update order status.
	if cancelMatch != nil {
		log.Infof("Maker notification for cancel order received for order %s. match id = %s",
			t.ID(), cancelMatch.MatchID)

		// Set this order status to Canceled and unlock any locked coins
		// if there are no new matches and there's no need to send swap
		// for any previous match.
		t.metaData.Status = order.OrderStatusCanceled
		if len(newTrackers) == 0 {
			t.maybeReturnCoins()
		}

		t.cancel.matches.maker = cancelMatch // taker is stored via processCancelMatch before negotiate
		// Set the order status for the canceled order.
		t.db.UpdateOrderStatus(t.cancel.ID(), order.OrderStatusExecuted)
		// Store a completed maker cancel match in the DB.
		makerCancelMeta := t.makeMetaMatch(cancelMatch)
		makerCancelMeta.SetStatus(order.MatchComplete)
		err := t.db.UpdateMatch(makerCancelMeta)
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
			qty = calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)
		} else {
			qty = match.Match.Quantity
		}
		newFill += qty

		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			// Don't abandon other matches because of this error, attempt
			// to negotiate the other matches.
			log.Errorf("failed to update match %s in db: %v", match.id, err)
			continue
		}

		// Only add this match to the map if the db update succeeds, so
		// funds don't get stuck if user restarts Core after sending a
		// swap because negotiations will not be resumed for this match
		// and auto-refund cannot be performed.
		// TODO: Maybe allow? This match can be restored from the DEX's
		// connect response on restart IF it is not revoked.
		t.matches[match.id] = match
		log.Infof("Starting negotiation for match %v for order %v with swap fee rate = %v, quantity = %v",
			match.id, t.ID(), match.Match.FeeRateSwap, qty)
	}

	// Calculate and set the new filled value for the order.
	var filled uint64
	for _, mt := range t.matches {
		if isMarketBuy {
			filled += calc.BaseToQuote(mt.Match.Rate, mt.Match.Quantity)
		} else {
			filled += mt.Match.Quantity
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
	corder, cancelOrder := t.coreOrderInternal()
	if cancelMatch != nil {
		details := fmt.Sprintf("%s order on %s-%s at %s has been canceled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), t.dc.acct.host, t.token())
		t.notify(newOrderNote("Order canceled", details, db.Success, corder))
		// Also send out a data notification with the cancel order information.
		t.notify(newOrderNote("cancel", "", db.Data, cancelOrder))
	}
	if len(newTrackers) > 0 {
		fillPct := 100 * float64(filled) / float64(trade.Quantity)
		details := fmt.Sprintf("%s order on %s-%s %.1f%% filled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), fillPct, t.token())
		log.Debugf("Trade order %v matched with %d orders: +%d filled, total fill %d / %d (%.1f%%)",
			t.ID(), len(newTrackers), newFill, filled, trade.Quantity, fillPct)
		t.notify(newOrderNote("Matches made", details, db.Poke, corder))
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

	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	var mid order.MatchID
	copy(mid[:], msgMatch.MatchID)
	return &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Status: order.MatchStatus(msgMatch.Status),
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
		Match: &order.UserMatch{
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
	log.Infof("Taker notification for cancel order %v received. Match id = %s", oid, mid)
	t.cancel.matches.taker = msgMatch
	// Store the completed taker cancel match.
	takerCancelMeta := t.makeMetaMatch(t.cancel.matches.taker)
	takerCancelMeta.SetStatus(order.MatchComplete)
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
func (t *trackedTrade) counterPartyConfirms(match *matchTracker) (have, needed uint32, changed bool) {
	// Counter-party's swap is the "to" asset.
	needed = t.wallets.toAsset.SwapConf

	// Check the confirmations on the counter-party's swap.
	coin := match.counterSwap.Coin()
	var err error
	have, err = coin.Confirmations()
	if err != nil {
		log.Errorf("Failed to get confirmations of the counter-party's swap %s (%s) for match %v, order %v",
			coin, t.wallets.toAsset.Symbol, match.id, t.UID())
		have = 0 // should already be
		return
	}

	// Log the pending swap status at new heights only.
	if match.counterConfirms != int64(have) {
		match.counterConfirms = int64(have)
		changed = true
	}
	return
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
		log.Tracef("Checking match %v (%v) in status %v. "+
			"Order: %v, Refund coin: %v, Script: %x, Revoked: %v", match.id,
			match.Match.Side, match.MetaData.Status, t.ID(),
			proof.RefundCoin, proof.Script, proof.IsRevoked)
		if match.isActive() {
			return true
		}
	}
	return false
}

// Matches are inactive if: (1) status is complete, (2) it is refunded, or (3)
// it is revoked and this side of the match requires no further action like
// refund or auto-redeem.
func (match *matchTracker) isActive() bool {
	if match.MetaData.Status == order.MatchComplete {
		return false
	}

	// Refunded matches are inactive regardless of status.
	if len(match.MetaData.Proof.RefundCoin) > 0 {
		return false
	}

	// Revoked matches may need to be refunded or auto-redeemed first.
	if match.MetaData.Proof.IsRevoked {
		// - NewlyMatched requires no further action from either side
		// - MakerSwapCast requires no further action from the taker
		// - (TakerSwapCast requires action on both sides)
		// - MakerRedeemed requires no further action from the maker
		status, side := match.MetaData.Status, match.Match.Side
		if status == order.NewlyMatched ||
			(status == order.MakerSwapCast && side == order.Taker) ||
			(status == order.MakerRedeemed && side == order.Maker) {
			log.Tracef("Revoked match %v (%v) in status %v considered inactive.",
				match.id, side, status)
			return false
		}
	}
	return true
}

func (t *trackedTrade) activeMatches() []*matchTracker {
	var actives []*matchTracker
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	for _, match := range t.matches {
		if match.isActive() {
			actives = append(actives, match)
		}
	}
	return actives
}

// unspentContractAmounts returns the total amount locked in unspent swaps.
func (t *trackedTrade) unspentContractAmounts(assetID uint32) (amount uint64) {
	if t.fromAssetID != assetID {
		// Only swaps sent from the specified assetID should count.
		return 0
	}
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	swapSentFromQuoteAsset := t.fromAssetID == t.Quote()
	for _, match := range t.matches {
		side, status := match.Match.Side, match.Match.Status
		if status >= order.MakerRedeemed || len(match.MetaData.Proof.RefundCoin) != 0 {
			// Any redemption or own refund implies our swap is spent.
			// Even if we're Maker and our swap has not been redeemed
			// by Taker, we should consider it spent.
			continue
		}
		if (side == order.Maker && status >= order.MakerSwapCast) ||
			(side == order.Taker && status == order.TakerSwapCast) {
			swapAmount := match.Match.Quantity
			if swapSentFromQuoteAsset {
				swapAmount = calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)
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
// mutex lock held for reads.
func (t *trackedTrade) isSwappable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || proof.IsRevoked {
		log.Tracef("Match %v not swappable: failErr = %v, revoked = %v",
			match.id, match.failErr, proof.IsRevoked)
		return false
	}

	wallet := t.wallets.fromWallet
	if !wallet.unlocked() {
		log.Errorf("cannot swap order %s, match %s, because %s wallet is not unlocked",
			t.ID(), match.id, unbip(wallet.AssetID))
		return false
	}

	if dbMatch.Side == order.Taker && metaData.Status == order.MakerSwapCast {
		// Check the confirmations on the maker's swap.
		confs, req, changed := t.counterPartyConfirms(match)
		ready := confs >= req
		if changed && !ready {
			log.Debugf("Match %v not yet swappable: current confs = %d, required confs = %d",
				match.id, confs, req)
		}
		return ready
	}
	if dbMatch.Side == order.Maker && metaData.Status == order.NewlyMatched {
		return true
	}
	return false
}

// isRedeemable will be true if the match is ready for our redemption to be
// broadcast.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) isRedeemable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || len(proof.RefundCoin) != 0 {
		log.Tracef("Match %v not redeemable: failErr = %v, RefundCoin = %v",
			match.id, match.failErr, proof.RefundCoin)
		return false
	}

	wallet := t.wallets.toWallet
	if !wallet.unlocked() {
		log.Errorf("cannot redeem order %s, match %s, because %s wallet is not unlocked",
			t.ID(), match.id, unbip(wallet.AssetID))
		return false
	}

	if dbMatch.Side == order.Maker && metaData.Status == order.TakerSwapCast {
		// Check the confirmations on the taker's swap.
		confs, req, changed := t.counterPartyConfirms(match)
		ready := confs >= req
		if changed && !ready {
			log.Debugf("Match %v not yet redeemable: current confs = %d, required confs = %d",
				match.id, confs, req)
		}
		return ready
	}
	if dbMatch.Side == order.Taker && metaData.Status == order.MakerRedeemed {
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
	dbMatch, _, proof, _ := match.parts()
	if match.refundErr != nil || len(proof.RefundCoin) != 0 {
		log.Tracef("Match %v not refundable: refundErr = %v, RefundCoin = %v",
			match.id, match.refundErr, proof.RefundCoin)
		return false
	}

	wallet := t.wallets.fromWallet
	if !wallet.unlocked() {
		log.Errorf("cannot refund order %s, match %s, because %s wallet is not unlocked",
			t.ID(), match.id, unbip(wallet.AssetID))
		return false
	}

	// Return if we've NOT sent a swap OR a redeem has been
	// executed by either party.
	if len(proof.Script) == 0 || dbMatch.Status >= order.MakerRedeemed {
		return false
	}

	// Issue a refund if our swap's locktime has expired.
	swapLocktimeExpired, contractExpiry, err := wallet.LocktimeExpired(proof.Script)
	if err != nil {
		log.Errorf("error checking if locktime has expired for %s contract on order %s, match %s: %v",
			dbMatch.Side, t.ID(), match.id, err)
		return false
	}
	if swapLocktimeExpired {
		return true
	}

	// For the first check or hourly tick, log the time until expiration.
	expiresIn := time.Until(contractExpiry) // may be negative
	if match.lastExpireDur-expiresIn < time.Hour {
		// Logged less than an hour ago.
		return false
	}

	// Record this log event's expiry duration.
	match.lastExpireDur = expiresIn

	swapCoinID := proof.TakerSwap
	if dbMatch.Side == order.Maker {
		swapCoinID = proof.MakerSwap
	}
	from := t.wallets.fromAsset
	remainingTime := expiresIn.Round(time.Second)
	var but string
	if remainingTime <= 0 {
		// Since reaching expiry time does not necessarily mean it is spendable
		// by consensus rules (e.g. 11 block median time must be greater than
		// lock time with BTC), include a "but" in the message.
		but = "but "
	}
	log.Infof("Contract for match %v with swap coin %v (%s) has an expiry time of %v (%v), %snot yet expired.",
		match.id, coinIDString(from.ID, swapCoinID), from.Symbol,
		contractExpiry, remainingTime, but)

	return false
}

// shouldBeginFindRedemption will be true if we are the Taker on this match,
// we've broadcasted a swap, our swap has gotten the required confs, the match
// was revoked without receiving a valid notification of Maker's redeem and
// we've not refunded our swap.
//
// This method accesses match fields and MUST be called with the trackedTrade
// mutex lock held for reads.
func (t *trackedTrade) shouldBeginFindRedemption(match *matchTracker) bool {
	proof := &match.MetaData.Proof
	if !proof.IsRevoked {
		return false // Only auto-find redemption for revoked/failed matches.
	}
	swapCoinID := proof.TakerSwap
	if match.Match.Side != order.Taker || len(swapCoinID) == 0 || len(proof.MakerRedeem) > 0 || len(proof.RefundCoin) > 0 {
		log.Tracef(
			"Not finding redemption for match %v: side = %s, failErr = %v, TakerSwap = %v RefundCoin = %v",
			match.id, match.Match.Side, match.failErr, proof.TakerSwap, proof.RefundCoin)
		return false
	}
	if match.cancelRedemptionSearch != nil { // already finding redemption
		return false
	}

	confs, err := t.wallets.fromWallet.Confirmations([]byte(swapCoinID))
	if err != nil {
		log.Errorf("Failed to get confirmations of the taker's swap %s (%s) for match %v, order %v",
			coinIDString(t.wallets.fromAsset.ID, swapCoinID), t.wallets.fromAsset.Symbol, match.id, t.UID())
		return false
	}
	return confs >= t.wallets.fromAsset.SwapConf
}

// tick will check for and perform any match actions necessary.
func (t *trackedTrade) tick() (assetMap, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var swaps, redeems, refunds []*matchTracker
	assets := make(assetMap)
	errs := newErrorSet(t.dc.acct.host + " tick: ")

	// Check all matches for and resend pending requests as necessary.
	if err := t.resendPendingRequests(); err != nil {
		errs.addErr(err)
	}

	// Check all matches and send swap, redeem or refund as necessary.
	var sent, quoteSent, received, quoteReceived uint64
	for _, match := range t.matches {
		side := match.Match.Side
		if (side == order.Maker && match.MetaData.Status >= order.MakerRedeemed) ||
			(side == order.Taker && match.MetaData.Status >= order.MatchComplete) {
			continue
		}
		if match.Match.Address == "" {
			continue // a cancel order match
		}
		if !match.isActive() {
			continue // either refunded or revoked requiring no action on this side of the match
		}

		switch {
		case t.isSwappable(match):
			log.Debugf("Swappable match %v for order %v (%v)", match.id, t.ID(), side)
			swaps = append(swaps, match)
			sent += match.Match.Quantity
			quoteSent += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)

		case t.isRedeemable(match):
			log.Debugf("Redeemable match %v for order %v (%v)", match.id, t.ID(), side)
			redeems = append(redeems, match)
			received += match.Match.Quantity
			quoteReceived += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)

		// Check refundability before checking if to start finding redemption.
		// Ensures that redemption search is not started if locktime has expired.
		// If we've already started redemption search for this match, the search
		// will be aborted if/when auto-refund succeeds.
		case t.isRefundable(match):
			log.Debugf("Refundable match %v for order %v (%v)", match.id, t.ID(), side)
			refunds = append(refunds, match)

		case t.shouldBeginFindRedemption(match):
			log.Debugf("Ready to find counter-party redemption for match %v, order %v (%v)", match.id, t.ID(), side)
			t.findMakersRedemption(match)
		}
	}

	fromID := t.wallets.fromAsset.ID
	if len(swaps) > 0 || len(refunds) > 0 {
		assets.count(fromID)
	}

	if len(swaps) > 0 {
		qty := sent
		if !t.Trade().Sell {
			qty = quoteSent
		}
		err := t.swapMatches(swaps)

		// swapMatches might modify the matches, so don't get the *Order for
		// notifications before swapMatches.
		corder, _ := t.coreOrderInternal()
		if err != nil {
			errs.addErr(err)
			details := fmt.Sprintf("Error encountered sending a swap output(s) worth %.8f %s on order %s",
				float64(qty)/conversionFactor, unbip(fromID), t.token())
			t.notify(newOrderNote("Swap error", details, db.ErrorLevel, corder))
		} else {
			details := fmt.Sprintf("Sent swaps worth %.8f %s on order %s",
				float64(qty)/conversionFactor, unbip(fromID), t.token())
			t.notify(newOrderNote("Swaps initiated", details, db.Poke, corder))
		}
	}

	if len(redeems) > 0 {
		toAsset := t.wallets.toAsset.ID
		assets.count(toAsset)
		assets.count(t.fromAssetID) // update the from wallet balance to reduce contractlocked balance
		qty := received
		if t.Trade().Sell {
			qty = quoteReceived
		}
		err := t.redeemMatches(redeems)
		corder, _ := t.coreOrderInternal()
		if err != nil {
			errs.addErr(err)
			details := fmt.Sprintf("Error encountered sending redemptions worth %.8f %s on order %s",
				float64(qty)/conversionFactor, unbip(toAsset), t.token())
			t.notify(newOrderNote("Redemption error", details, db.ErrorLevel, corder))
		} else {
			details := fmt.Sprintf("Redeemed %.8f %s on order %s",
				float64(qty)/conversionFactor, unbip(toAsset), t.token())
			t.notify(newOrderNote("Match complete", details, db.Poke, corder))
		}
	}

	if len(refunds) > 0 {
		refunded, err := t.refundMatches(refunds)
		corder, _ := t.coreOrderInternal()
		details := fmt.Sprintf("Refunded %.8f %s on order %s",
			float64(refunded)/conversionFactor, unbip(fromID), t.token())
		if err != nil {
			errs.addErr(err)
			t.notify(newOrderNote("Refund Failure", details+", with some errors", db.ErrorLevel, corder))
		} else {
			t.notify(newOrderNote("Matches Refunded", details, db.WarningLevel, corder))
		}
	}

	return assets, errs.ifAny()
}

// resendPendingRequests checks all matches for this order to re-attempt
// sending the `init` or `redeem` request where necessary.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) resendPendingRequests() error {
	errs := newErrorSet("resendPendingRequest: order %s - ", t.ID())

	for _, match := range t.matches {
		dbMatch, _, proof, auth := match.parts()
		// Do not resend pending requests for revoked matches.
		// Matches where we've refunded our swap or we auto-redeemed maker's
		// swap will be set to revoked and will be skipped as well.
		if match.failErr != nil || proof.IsRevoked {
			continue
		}
		side, status := dbMatch.Side, dbMatch.Status
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
		var err error
		if len(swapCoinID) != 0 && len(auth.InitSig) == 0 { // resend pending `init` request
			err = t.finalizeSwapAction(match, swapCoinID, proof.Script)
		} else if len(redeemCoinID) != 0 && len(auth.RedeemSig) == 0 { // resend pending `redeem` request
			err = t.finalizeRedeemAction(match, swapCoinID)
		}
		if err != nil {
			errs.addErr(err)
		}
	}

	return errs.ifAny()
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
		log.Errorf("revoke() wrongly called for order %v, status %s", t.ID(), t.metaData.Status)
		return
	}

	metaOrder := t.metaOrder()
	metaOrder.MetaData.Status = order.OrderStatusRevoked
	err := t.db.UpdateOrder(metaOrder)
	if err != nil {
		log.Errorf("unable to update order: %v", err)
	}

	// Return coins if there are no matches that MAY later require sending swaps.
	t.maybeReturnCoins()
}

// revokeMatch sets the status as revoked for the specified match.
func (t *trackedTrade) revokeMatch(matchID order.MatchID) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var revokedMatch *matchTracker
	for _, match := range t.matches {
		if match.id == matchID {
			revokedMatch = match
			break
		}
	}
	if revokedMatch == nil {
		return fmt.Errorf("no match found with id %s for order %v", matchID, t.ID())
	}

	// Set the match as revoked.
	revokedMatch.MetaMatch.MetaData.Proof.IsRevoked = true
	err := t.db.UpdateMatch(&revokedMatch.MetaMatch)
	if err != nil {
		log.Errorf("db update error for revoked match %v, order %v: %v", matchID, t.ID(), err)
	}

	// Notify the user of the failed match.
	corder, _ := t.coreOrderInternal() // no cancel order
	t.notify(newOrderNote("Match revoked", fmt.Sprintf("Match %s has been revoked",
		token(matchID[:])), db.WarningLevel, corder))

	// Send out a data notification with the revoke information.
	cancelOrder := &Order{
		Host:     t.dc.acct.host,
		MarketID: t.mktID,
		Type:     order.CancelOrderType,
		Stamp:    encode.UnixMilliU(time.Now()),
		Epoch:    corder.Epoch,
		TargetID: corder.ID, // the important part for the frontend
	}
	t.notify(newOrderNote("revoke", "", db.Data, cancelOrder))

	// Unlock coins if we're not expecting future matches for this
	// trade and there are no matches that MAY later require sending
	// swaps.
	t.maybeReturnCoins()

	log.Warnf("Match %v revoked in status %v for order %v", matchID, revokedMatch.Match.Status, t.ID())
	return nil
}

// swapMatches will send a transaction with swap outputs for the specified
// matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) swapMatches(matches []*matchTracker) error {
	errs := newErrorSet("swapMatches order %s - ", t.ID())

	// Prepare the asset.Contracts.
	contracts := make([]*asset.Contract, len(matches))
	// These matches may have different fee rates, matched in different epochs.
	var highestFeeRate uint64
	for i, match := range matches {
		dbMatch, _, proof, auth := match.parts()
		value := dbMatch.Quantity
		if !match.trade.Sell {
			value = calc.BaseToQuote(dbMatch.Rate, dbMatch.Quantity)
		}
		matchTime := encode.UnixTimeMilli(int64(auth.MatchStamp))
		lockTime := matchTime.Add(t.lockTimeTaker).UTC().Unix()
		if dbMatch.Side == order.Maker {
			proof.Secret = encode.RandomBytes(32)
			secretHash := sha256.Sum256(proof.Secret)
			proof.SecretHash = secretHash[:]
			lockTime = matchTime.Add(t.lockTimeMaker).UTC().Unix()
		}

		contracts[i] = &asset.Contract{
			Address:    dbMatch.Address,
			Value:      value,
			SecretHash: proof.SecretHash,
			LockTime:   uint64(lockTime),
		}

		if match.Match.FeeRateSwap > highestFeeRate {
			highestFeeRate = match.Match.FeeRateSwap
		}
	}

	lockChange := true
	// If the order is executed, canceled or revoked, and these are the last
	// swaps, then we don't need to lock the change coin.
	if t.metaData.Status > order.OrderStatusBooked {
		var matchesRequiringSwaps int
		for _, match := range t.matches {
			if match.MetaData.Proof.IsRevoked {
				// Revoked matches don't require swaps.
				continue
			}
			if (match.Match.Side == order.Maker && match.Match.Status < order.MakerSwapCast) ||
				(match.Match.Side == order.Taker && match.Match.Status < order.TakerSwapCast) {
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
		log.Debugf("Using stored change coin %v (%v) for order %v matches",
			coinIDString(fromAsset.ID, coinIDs[0]), fromAsset.Symbol, t.ID())
	}

	inputs := make([]asset.Coin, len(coinIDs))
	for i, coinID := range coinIDs {
		coin, found := t.coins[hex.EncodeToString(coinID)]
		if !found {
			return errs.add("%s coin %s not found", fromAsset.Symbol, coinIDString(fromAsset.ID, coinID))
		}
		inputs[i] = coin
	}

	if t.dc.IsDown() {
		return errs.add("not broadcasting swap while DEX %s connection is down (could be revoked)", t.dc.acct.host)
	}
	// swapMatches is no longer idempotent after this point.

	// Send the swap. If the swap fails, set the failErr flag for all matches.
	// A more sophisticated solution might involve tracking the error time too
	// and trying again in certain circumstances.
	swaps := &asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    highestFeeRate,
		LockChange: lockChange,
	}
	receipts, change, fees, err := t.wallets.fromWallet.Swap(swaps)
	if err != nil {
		// Set the error on the matches.
		for _, match := range matches {
			match.failErr = err
		}
		return errs.add("error sending swap transaction: %v", err)
	}

	log.Infof("Broadcasted transaction with %d swap contracts for order %v. Fee rate = %d. Receipts (%s): %v",
		len(receipts), t.ID(), swaps.FeeRate, t.wallets.fromAsset.Symbol, receipts)

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
		log.Debugf("Saving change coin %v (%v) to DB for order %v",
			coinIDString(fromAsset.ID, t.metaData.ChangeCoin), fromAsset.Symbol, t.ID())
	}
	t.change = change
	t.db.UpdateOrderMetaData(t.ID(), t.metaData)

	// Process the swap for each match by sending the `init` request
	// to the DEX and updating the match with swap details.
	// Add any errors encountered to `errs` and proceed to next match
	// to ensure that swap details are saved for all matches.
	for i, receipt := range receipts {
		match := matches[i]
		coin := receipt.Coin()
		log.Infof("Contract coin %v (%s), value = %d, refundable at %v (script = %v)",
			coin, t.wallets.fromAsset.Symbol, coin.Value(), receipt.Expiration(), receipt.Contract())
		if secret := match.MetaData.Proof.Secret; len(secret) > 0 {
			log.Tracef("Contract coin %v secret = %x", coin, secret)
		}
		err := t.finalizeSwapAction(match, coin.ID(), receipt.Contract())
		if err != nil {
			errs.addErr(err)
		}
	}

	return errs.ifAny()
}

// finalizeSwapAction sends an `init` request for the specified match, waits
// for and validates the server's acknowledgement, then saves the swap details
// to db.
// The swap details are always saved even if sending the `init` request errors
// or a valid ack is not received from the server. This makes it possible to
// resend the `init` request at a later time OR refund the swap after locktime
// expires if the trade does not progress as expected.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) finalizeSwapAction(match *matchTracker, coinID, contract []byte) error {
	_, _, proof, auth := match.parts()
	if len(auth.InitSig) != 0 {
		return fmt.Errorf("'init' already sent for match %v", match.id)
	}
	errs := newErrorSet("")

	// attempt to send `init` request and validate server ack.
	ack := new(msgjson.Acknowledgement)
	init := &msgjson.Init{
		OrderID:  t.ID().Bytes(),
		MatchID:  match.id[:],
		CoinID:   coinID,
		Contract: contract,
	}
	// The DEX may wait up to its configured broadcast timeout to locate the
	// contract txn, so wait at least that long for a response. Note that the
	// server presently waits an unspecified amount of time that is shorter than
	// this, which gives the client an msgjson.TransactionUndiscovered error to
	// signal to the client to try broadcasting again or check their asset
	// backend connectivity before hitting the broadcast timeout (and being
	// penalized).
	timeout := t.broadcastTimeout()
	if err := t.dc.signAndRequest(init, msgjson.InitRoute, ack, timeout); err != nil {
		errs.add("error sending 'init' message for match %s: %v", match.id, err)
	} else if err := t.dc.acct.checkSig(init.Serialize(), ack.Sig); err != nil {
		errs.add("'init' ack signature error for match %s: %v", match.id, err)
	}

	// Update the match db data with the swap details.
	proof.Script = contract
	if match.Match.Side == order.Taker {
		proof.TakerSwap = coinID
		match.SetStatus(order.TakerSwapCast)
	} else {
		proof.MakerSwap = coinID
		match.SetStatus(order.MakerSwapCast)
	}
	if len(ack.Sig) != 0 {
		auth.InitSig = ack.Sig
		auth.InitStamp = encode.UnixMilliU(time.Now())
	}
	if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
		errs.add("error storing swap details in database for match %s, coin %s: %v",
			match.id, coinIDString(t.wallets.fromAsset.ID, coinID), err)
	}
	return errs.ifAny()
}

// redeemMatches will send a transaction redeeming the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) redeemMatches(matches []*matchTracker) error {
	errs := newErrorSet("redeemMatches - order %s - ", t.ID())
	// Collect a asset.Redemption for each match into a slice of redemptions that
	// will be grouped into a single transaction.
	redemptions := make([]*asset.Redemption, 0, len(matches))
	for _, match := range matches {
		redemptions = append(redemptions, &asset.Redemption{
			Spends: match.counterSwap,
			Secret: match.MetaData.Proof.Secret,
		})
	}

	// Send the transaction.
	redeemWallet, redeemAsset := t.wallets.toWallet, t.wallets.toAsset // this is our redeem
	coinIDs, outCoin, fees, err := redeemWallet.Redeem(redemptions)
	// If an error was encountered, fail all of the matches. A failed match will
	// not run again on during ticks.
	if err != nil {
		for _, match := range matches {
			match.failErr = err
		}
		return errs.addErr(err)
	}

	log.Infof("Broadcasted redeem transaction spending %d contracts for order %v, paying to %s (%s)",
		len(redemptions), t.ID(), outCoin, redeemAsset.Symbol)

	t.metaData.RedemptionFeesPaid += fees

	err = t.db.UpdateOrderMetaData(t.ID(), t.metaData)
	if err != nil {
		log.Errorf("error updating order metadata for order %s: %w", t.ID(), err)
	}

	for _, match := range matches {
		log.Infof("Match %s complete: %s %d %s", match.id,
			sellString(t.Trade().Sell), match.Match.Quantity, unbip(t.Prefix().BaseAsset),
		)
	}

	// Send the redemption information to the DEX.
	for i, match := range matches {
		err := t.finalizeRedeemAction(match, coinIDs[i])
		if err != nil {
			errs.addErr(err)
		}
	}

	return errs.ifAny()
}

// finalizeRedeemAction sends a `redeem` request for the specified match,
// waits for and validates the server's acknowledgement, then saves the
// redeem details to db.
// The redeem details are always saved even if sending the `redeem` request
// errors or a valid ack is not received from the server. This makes it
// possible to resend the `redeem` request at a later time.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) finalizeRedeemAction(match *matchTracker, coinID []byte) error {
	_, _, proof, auth := match.parts()
	if len(auth.RedeemSig) != 0 {
		return fmt.Errorf("'redeem' already sent for match %v", match.id)
	}
	errs := newErrorSet("")

	// Attempt to send `redeem` request and validate server ack.
	// Not necessary for revoked matches.
	if !proof.IsRevoked {
		msgRedeem := &msgjson.Redeem{
			OrderID: t.ID().Bytes(),
			MatchID: match.id.Bytes(),
			CoinID:  coinID,
			Secret:  proof.Secret,
		}
		ack := new(msgjson.Acknowledgement)
		// The DEX may wait up to its configured broadcast timeout, so wait at least
		// that long for a response.
		timeout := t.broadcastTimeout()
		if err := t.dc.signAndRequest(msgRedeem, msgjson.RedeemRoute, ack, timeout); err != nil {
			errs.add("error sending 'redeem' message for match %s: %v", match.id, err)
		} else if err := t.dc.acct.checkSig(msgRedeem.Serialize(), ack.Sig); err != nil {
			errs.add("'redeem' ack signature error for match %s: %v", match.id, err)
		}
		// Update the match db data with the redeem details.
		if len(ack.Sig) != 0 {
			auth.RedeemSig = ack.Sig
			auth.RedeemStamp = encode.UnixMilliU(time.Now())
		}
	}

	if match.Match.Side == order.Taker {
		match.SetStatus(order.MatchComplete)
		proof.TakerRedeem = coinID
	} else {
		match.SetStatus(order.MakerRedeemed)
		proof.MakerRedeem = coinID
	}
	if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
		errs.add("error storing redeem details in database for match %s, coin %s: %v",
			match.id, coinIDString(t.wallets.toAsset.ID, coinID), err)
	}
	return errs.ifAny()
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
				log.Errorf("Error finding redemption of taker's %s contract %s for order %s, match %s: %v.",
					fromAsset.Symbol, coinIDString(fromAsset.ID, swapCoinID), t.ID(), match.id, err)
			}
			return
		}

		if match.Match.Status != order.TakerSwapCast {
			log.Errorf("Received find redemption result at wrong step, order %s, match %s, status %s.",
				t.ID(), match.id, match.Match.Status)
			return
		}

		proof := &match.MetaData.Proof
		if !t.wallets.toWallet.ValidateSecret(secret, proof.SecretHash) {
			log.Errorf("Found invalid redemption of taker's %s contract %s for order %s, match %s: invalid secret %s, hash %s.",
				fromAsset.Symbol, coinIDString(fromAsset.ID, swapCoinID), t.ID(), match.id, secret, proof.SecretHash)
			return
		}

		// Update the match status and set the secret so that Maker's swap
		// will be redeemed in the next call to trade.tick().
		match.SetStatus(order.MakerRedeemed)
		proof.MakerRedeem = []byte(redemptionCoinID)
		proof.Secret = secret
		proof.IsRevoked = true // Set match as revoked.
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			log.Errorf("waitForRedemptions: error storing match info in database: %v", err)
		}

		details := fmt.Sprintf("Found maker's redemption (%s: %v) and validated secret for match %s",
			fromAsset.Symbol, coinIDString(fromAsset.ID, redemptionCoinID), match.id)
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("Match recovered", details, db.Poke, corder))
	}()
}

// refundMatches will send refund transactions for the specified matches.
//
// This method modifies match fields and MUST be called with the trackedTrade
// mutex lock held for writes.
func (t *trackedTrade) refundMatches(matches []*matchTracker) (uint64, error) {
	errs := newErrorSet("refundMatches: order %s - ", t.ID())

	refundWallet, refundAsset := t.wallets.fromWallet, t.wallets.fromAsset // refunding to our wallet
	var refundedQty uint64

	for _, match := range matches {
		dbMatch, _, proof, _ := match.parts()
		if len(proof.RefundCoin) != 0 {
			log.Errorf("attempted to execute duplicate refund for match %s, side %s, status %s",
				match.id, dbMatch.Side, dbMatch.Status)
			continue
		}
		contractToRefund := proof.Script
		var swapCoinID dex.Bytes
		var matchFailureReason string
		switch {
		case dbMatch.Side == order.Maker && dbMatch.Status == order.MakerSwapCast:
			swapCoinID = dex.Bytes(proof.MakerSwap)
			matchFailureReason = "no valid counterswap received from Taker"
		case dbMatch.Side == order.Maker && dbMatch.Status == order.TakerSwapCast && len(proof.MakerRedeem) == 0:
			swapCoinID = dex.Bytes(proof.MakerSwap)
			matchFailureReason = "unable to redeem Taker's swap"
		case dbMatch.Side == order.Taker && dbMatch.Status == order.TakerSwapCast:
			swapCoinID = dex.Bytes(proof.TakerSwap)
			matchFailureReason = "no valid redemption received from Maker"
		default:
			log.Errorf("attempted to execute invalid refund for match %s, side %s, status %s",
				match.id, dbMatch.Side, dbMatch.Status)
			continue
		}

		swapCoinString := coinIDString(refundAsset.ID, swapCoinID)
		log.Infof("Refunding %s contract %s for match %v (%s)",
			refundAsset.Symbol, swapCoinString, match.id, matchFailureReason)

		refundCoin, err := refundWallet.Refund(swapCoinID, contractToRefund)
		if err != nil {
			match.refundErr = err
			if err == asset.CoinNotFoundError {
				// Could not find the contract coin, which means it has been spent.
				// We should have already started FindRedemption for this contract,
				// but let's do it again to ensure we find the secret.
				log.Debugf("Failed to refund %s contract %s, already redeemed. Beginning find redemption.",
					refundAsset.Symbol, swapCoinString)
				t.findMakersRedemption(match)
			} else {
				errs.add("error sending refund tx for match %s, swap coin %s: %v",
					match.id, swapCoinString, err)
			}
			continue
		}
		// Refund successful, cancel any previously started attempt to find
		// counter-party's redemption.
		if match.cancelRedemptionSearch != nil {
			match.cancelRedemptionSearch()
		}
		if t.Trade().Sell {
			refundedQty += match.Match.Quantity
		} else {
			refundedQty += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)
		}
		proof.RefundCoin = []byte(refundCoin)
		proof.IsRevoked = true // Set match as revoked.
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			errs.add("error storing match info in database: %v", err)
		}
	}

	return refundedQty, errs.ifAny()
}

// processAudit processes the audit request from the server.
func (t *trackedTrade) processAudit(msgID uint64, audit *msgjson.Audit) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	// Find the match and check the server's signature.
	var mid order.MatchID
	copy(mid[:], audit.MatchID)
	errs := newErrorSet("processAudit - order %s, match %s -", t.ID(), mid)
	match, found := t.matches[mid]
	if !found {
		return errs.add("match ID not known")
	}
	// Check the server signature.
	sigMsg := audit.Serialize()
	err := t.dc.acct.checkSig(sigMsg, audit.Sig)
	if err != nil {
		// Log, but don't quit.
		errs.add("server audit signature error: %v", err)
	}

	// Get the asset.AuditInfo from the ExchangeWallet. Handle network latency.
	// The coin waiter will run once every recheckInterval until successful or
	// until expiration after broadcast timeout. The client is asked by the
	// server to audit a contract transaction, and they have until broadcast
	// timeout to do it before they get penalized and the match revoked. Thus,
	// there is no reason to give up on the request sooner since the server will
	// not ask again.
	errChan := make(chan error, 1)
	var auditInfo asset.AuditInfo
	t.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(t.broadcastTimeout()),
		TryFunc: func() bool {
			var err error
			auditInfo, err = t.wallets.toWallet.AuditContract(audit.CoinID, audit.Contract)
			if err != nil {
				if err == asset.CoinNotFoundError {
					return wait.TryAgain
				}
				errChan <- err
				return wait.DontTryAgain
			}
			errChan <- nil
			return wait.DontTryAgain
		},
		ExpireFunc: func() {
			errChan <- ExpirationErr(fmt.Sprintf("timed out waiting for AuditContract coin %v",
				coinIDString(t.wallets.toAsset.ID, audit.CoinID)))
		},
	})
	// Wait for the coin waiter to find and audit the contract coin, or timeout.
	err = <-errChan
	if err != nil {
		return errs.addErr(err)
	}

	// Audit the contract.
	// 1. Recipient Address
	// 2. Contract value
	// 3. Secret hash: maker compares, taker records
	dbMatch, _, proof, auth := match.parts()
	match.counterSwap = auditInfo
	if auditInfo.Recipient() != t.Trade().Address {
		return errs.add("swap recipient %s is not the order address %s.", auditInfo.Recipient(), t.Trade().Address)
	}

	auditQty := dbMatch.Quantity
	if t.Trade().Sell {
		auditQty = calc.BaseToQuote(dbMatch.Rate, auditQty)
	}
	if auditInfo.Coin().Value() < auditQty {
		return errs.add("swap contract value %d was lower than expected %d", auditInfo.Coin().Value(), auditQty)
	}

	// TODO: Consider having the server supply the contract txn's fee rate to
	// improve the taker's audit with a check of the maker's contract fee rate.
	// The server should be checking the fee rate, but the client should not
	// trust it. The maker could also check the taker's contract txn fee rate,
	// but their contract is already broadcasted, so the check is of less value
	// as they can only wait for it to be mined to redeem it, in which case the
	// fee rate no longer matters, or wait for the lock time to expire to refund.

	// Check or store the secret hash and update the database.
	auth.AuditStamp = audit.Time
	auth.AuditSig = audit.Sig
	proof.CounterScript = audit.Contract
	matchTime := encode.UnixTimeMilli(int64(auth.MatchStamp))
	reqLockTime := encode.DropMilliseconds(matchTime.Add(t.lockTimeMaker)) // counterparty = maker
	if dbMatch.Side == order.Maker {
		// Check that the secret hash is correct.
		if !bytes.Equal(proof.SecretHash, auditInfo.SecretHash()) {
			return errs.add("secret hash mismatch for contract coin %v (%s), contract %v. expected %x, got %v",
				auditInfo.Coin(), t.wallets.toAsset.Symbol, audit.Contract, proof.SecretHash, auditInfo.SecretHash())
		}
		match.SetStatus(order.TakerSwapCast)
		proof.TakerSwap = []byte(audit.CoinID)
		// counterparty = taker
		reqLockTime = encode.DropMilliseconds(matchTime.Add(t.lockTimeTaker))
	} else {
		proof.SecretHash = auditInfo.SecretHash()
		match.SetStatus(order.MakerSwapCast)
		proof.MakerSwap = []byte(audit.CoinID)
	}

	if auditInfo.Expiration().Before(reqLockTime) {
		return errs.add("lock time too early. Need %s, got %s", reqLockTime, auditInfo.Expiration())
	}

	log.Infof("Audited contract (%s: %v) paying to %s for order %v, match %v",
		t.wallets.toAsset.Symbol, auditInfo.Coin(), auditInfo.Recipient(), audit.OrderID, audit.MatchID)

	err = t.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		return errs.add("error updating database: %v", err)
	}

	// Respond to DEX.
	err = t.dc.ack(msgID, match.id, audit)
	if err != nil {
		return errs.add("Audit error: %v", err)
	}
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

	dbMatch, _, proof, auth := match.parts()

	// Validate that this request satisfies expected preconditions if
	// we're the Taker. Not necessary if we're maker as redemption
	// requests are pretty much just a formality for Maker. Also, if
	// the order was loaded from the DB and we've already redeemed
	// Taker's swap, the counterSwap (AuditInfo for Taker's swap) will
	// not have been retrieved.
	if dbMatch.Side == order.Taker {
		switch {
		case match.counterSwap == nil:
			return errs.add("redemption received before audit request")
		case dbMatch.Status == order.TakerSwapCast:
			// redemption requests should typically arrive when the match
			// is at TakerSwapCast
		case dbMatch.Status > order.TakerSwapCast && len(auth.RedemptionSig) == 0:
			// status might have moved 1+ steps forward if this redemption
			// request is received after we've already found the redemption
		default:
			return fmt.Errorf("maker redemption received at incorrect step %d", dbMatch.Status)
		}
	}

	sigMsg := redemption.Serialize()
	err := t.dc.acct.checkSig(sigMsg, redemption.Sig)
	if err != nil {
		// Log, but don't quit.
		errs.add("server redemption signature error: %v", err)
	}
	// Respond to the DEX.
	err = t.dc.ack(msgID, match.id, redemption)
	if err != nil {
		return errs.add("Audit - %v", err)
	}

	// Update the database.
	auth.RedemptionSig = redemption.Sig
	auth.RedemptionStamp = redemption.Time
	redeemAsset := t.wallets.fromAsset // this redeem is the other party's
	if dbMatch.Side == order.Maker {
		// You are the maker, this is the taker's redeem.
		log.Debugf("Notified of taker's redemption (%s: %v) for order %v...",
			redeemAsset.Symbol, coinIDString(redeemAsset.ID, redemption.CoinID), t.ID())
		match.SetStatus(order.MatchComplete)
		proof.TakerRedeem = []byte(redemption.CoinID)
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			return errs.add("error storing match info in database: %v", err)
		}
		return nil
	}

	// You are the taker, this is the maker's redeem. The maker is the
	// initiator, so validate the revealed secret.
	wallet := t.wallets.toWallet
	if !wallet.ValidateSecret(redemption.Secret, proof.SecretHash) {
		return errs.add("secret %s received does not hash to the reported secret hash, %s",
			proof.Secret, proof.SecretHash)
	}

	log.Infof("Notified of maker's redemption (%s: %v) and validated secret for order %v...",
		redeemAsset.Symbol, coinIDString(redeemAsset.ID, redemption.CoinID), t.ID())

	match.SetStatus(order.MakerRedeemed)
	proof.MakerRedeem = []byte(redemption.CoinID)
	proof.Secret = redemption.Secret
	err = t.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		return errs.add("error storing match info in database: %v", err)
	}
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
		log.Tracef("not unlocking coins for order with status %s", t.metaData.Status)
		return false
	}

	// Status of all matches for the order. If a match exists for
	// which a swap MAY be sent later, keep the coins locked.
	for _, match := range t.matches {
		if match.MetaData.Proof.IsRevoked {
			// Won't be sending swap for this match regardless of
			// the match's status.
			continue
		}

		status, side := match.Match.Status, match.Match.Side
		if side == order.Maker && status < order.MakerSwapCast ||
			side == order.Taker && status < order.TakerSwapCast {
			// Match is active (not revoked, not refunded) and client
			// is yet to execute swap. Keep coins locked.
			log.Tracef("not unlocking coins for order with match side %s, status", side, status)
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
			log.Warnf("Unable to return %s funding coins: %v", unbip(t.wallets.fromAsset.ID), err)
		} else {
			t.coinsLocked = false
		}
	} else if t.change != nil && t.changeLocked {
		err := t.wallets.fromWallet.ReturnCoins(asset.Coins{t.change})
		if err != nil {
			log.Warnf("Unable to return %s change coin %v: %v", unbip(t.wallets.fromAsset.ID), t.change, err)
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
