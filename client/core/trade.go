// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
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
	failErr        error
	refundErr      error
	prefix         *order.Prefix
	trade          *order.Trade
	counterSwap    asset.AuditInfo
	contractExpiry time.Time // for one time logging in isRefundable, may never be set
	id             order.MatchID
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
	return &trackedTrade{
		Order:         ord,
		metaData:      dbOrder.MetaData,
		dc:            dc,
		db:            db,
		latencyQ:      latencyQ,
		wallets:       wallets,
		preImg:        preImg,
		mktID:         marketName(ord.Base(), ord.Quote()),
		coins:         mapifyCoins(coins),
		lockTimeTaker: lockTimeTaker,
		lockTimeMaker: lockTimeMaker,
		matches:       make(map[order.MatchID]*matchTracker),
		notify:        notify,
		epochLen:      epochLen,
		fromAssetID:   fromID,
	}
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
	prefix, trade := t.Prefix(), t.Trade()
	cancelling := t.cancel != nil
	canceled := cancelling && t.cancel.matches.maker != nil

	orderEpoch := t.dc.marketEpoch(t.mktID, prefix.ServerTime)
	var tif order.TimeInForce
	if lo, ok := t.Order.(*order.LimitOrder); ok {
		tif = lo.Force
	}
	corder := &Order{
		Host:        t.dc.acct.host,
		MarketID:    t.mktID,
		Type:        prefix.OrderType,
		ID:          t.ID().String(),
		Stamp:       encode.UnixMilliU(prefix.ServerTime),
		Sig:         t.metaData.Proof.DEXSig,
		Status:      t.metaData.Status,
		Epoch:       orderEpoch,
		Rate:        t.rate(),
		Qty:         trade.Quantity,
		Sell:        trade.Sell,
		Filled:      trade.Filled(),
		Cancelling:  cancelling,
		Canceled:    canceled,
		TimeInForce: tif,
	}
	for _, match := range t.matches {
		dbMatch := match.Match
		corder.Matches = append(corder.Matches, &Match{
			MatchID: match.id.String(),
			Status:  dbMatch.Status,
			Rate:    dbMatch.Rate,
			Qty:     dbMatch.Quantity,
			Side:    dbMatch.Side,
		})
	}
	var cancelOrder *Order
	if t.cancel != nil {
		cancelOrder = &Order{
			Host:     t.dc.acct.host,
			MarketID: t.mktID,
			Type:     order.CancelOrderType,
			Stamp:    encode.UnixMilliU(t.cancel.ServerTime),
			Epoch:    orderEpoch,
			TargetID: t.cancel.TargetOrderID.String(),
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
func (t *trackedTrade) nomatch(oid order.OrderID) error {
	// Check if this is the cancel order.
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.ID() != oid {
		if t.cancel == nil || t.cancel.ID() != oid {
			return newError(unknownOrderErr, "nomatch order ID %s does not match trade or cancel order", oid)
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
		return t.db.UpdateOrderStatus(cid, order.OrderStatusExecuted)
	}
	// This is the trade. Return coins and set status based on whether this is
	// a standing limit order or not.
	if t.metaData.Status != order.OrderStatusEpoch {
		return fmt.Errorf("nomatch sent for non-epoch order %s", oid)
	}
	if lo, ok := t.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
		log.Infof("Standing order %s did not match and is now booked.", t.token())
		t.metaData.Status = order.OrderStatusBooked
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("Order booked", "", db.Data, corder))
	} else {
		err := t.wallets.fromWallet.ReturnCoins(t.coinList())
		if err != nil {
			log.Warnf("unable to return %s funding coins: %v", unbip(t.wallets.fromAsset.ID), err)
		}
		log.Infof("Non-standing order %s did not match.", t.token())
		t.metaData.Status = order.OrderStatusExecuted
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("No match", "", db.Data, corder))
	}
	return t.db.UpdateOrderStatus(t.ID(), t.metaData.Status)
}

// readConnectMatches resolves the matches reported by the server in the
// 'connect' response against those in the match tracker map.
func (t *trackedTrade) readConnectMatches(msgMatches []*msgjson.Match) {
	ids := make(map[order.MatchID]bool)
	var extras []*msgjson.Match
	var missing []order.MatchID
	corder, _ := t.coreOrder()
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	for _, msgMatch := range msgMatches {
		var matchID order.MatchID
		copy(matchID[:], msgMatch.MatchID)
		ids[matchID] = true
		_, found := t.matches[matchID]
		if !found {
			extras = append(extras, msgMatch)
			continue
		}
	}

	for _, match := range t.matches {
		if !ids[match.id] && match.MetaData.Status < order.MatchComplete {
			log.Debugf("missing match: %v", match.id)
			missing = append(missing, match.id)
			match.failErr = fmt.Errorf("order not reported by the server on connect")
		}
	}

	host := t.dc.acct.host
	if len(missing) > 0 {
		details := fmt.Sprintf("%d matches for order %s were not reported by %q and are in a failed state",
			len(missing), t.ID(), host)
		corder, _ := t.coreOrderInternal()
		t.notify(newOrderNote("Missing matches", details, db.ErrorLevel, corder))
		for _, mid := range missing {
			log.Errorf("%s did not report active match %s on order %s", host, mid, t.ID())
		}
	}
	if len(extras) > 0 {
		details := fmt.Sprintf("%d matches reported by %s were not found for %s.", len(extras), host, t.token())
		t.notify(newOrderNote("Match resolution error", details, db.ErrorLevel, corder))
		for _, extra := range extras {
			log.Errorf("%s reported match %s which is not a known active match for order %s", host, extra.MatchID, extra.OrderID)
		}
	}
}

// negotiate creates and stores matchTrackers for the []*msgjson.Match, and
// updates (UserMatch).Filled. Match negotiation can then be progressed by
// calling (*trackedTrade).tick when a relevant event occurs, such as a request
// from the DEX or a tip change.
func (t *trackedTrade) negotiate(msgMatches []*msgjson.Match) error {
	// Add the matches to the match map and update the database.
	trade := t.Trade()
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var newFill uint64
	var cancelMatch *msgjson.Match
	newTrackers := make([]*matchTracker, 0, len(msgMatches))
	for _, msgMatch := range msgMatches {
		if len(msgMatch.MatchID) != order.MatchIDSize {
			return fmt.Errorf("match id of incorrect length. expected %d, got %d",
				order.MatchIDSize, len(msgMatch.MatchID))
		}
		var mid order.MatchID
		copy(mid[:], msgMatch.MatchID)
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		if oid != t.ID() {
			return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.ID())
		}

		// First check if this is a match with a cancel order, in which case the
		// counterparty Address field would be empty. If the user placed a
		// cancel order, that order will be recorded in t.cancel on cancel order
		// creation via (*dexConnection).tryCancel or restore from DB via
		// (*Core).dbTrackers.
		if t.cancel != nil && msgMatch.Address == "" {
			log.Infof("Maker notification for cancel order received for order %s. match id = %s",
				oid, msgMatch.MatchID)
			cancelMatch = msgMatch
			newFill += msgMatch.Quantity // should push this trade to 100% filled
			continue
		}

		match := &matchTracker{
			id:        mid,
			prefix:    t.Prefix(),
			trade:     trade,
			MetaMatch: *t.makeMetaMatch(msgMatch),
		}

		var qty uint64
		if t.Type() == order.MarketOrderType && !trade.Sell {
			qty = calc.BaseToQuote(msgMatch.Rate, msgMatch.Quantity)
		} else {
			qty = msgMatch.Quantity
		}
		newFill += qty

		log.Infof("Starting negotiation for match %v for order %v with swap fee rate = %v, quantity = %v",
			mid, oid, match.MetaMatch.Match.FeeRateSwap, qty)
		match.SetStatus(order.NewlyMatched) // these must be new matches
		newTrackers = append(newTrackers, match)
	}

	// Record any cancel order Match and update order status.
	if cancelMatch != nil {
		t.cancel.matches.maker = cancelMatch // taker is stored via processCancelMatch before negotiate
		// Set the order status for both orders.
		t.metaData.Status = order.OrderStatusCanceled
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
	// trackedTrade and the DB, and update each order's status and fill amount.
	for _, match := range newTrackers {
		t.matches[match.id] = match
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			return fmt.Errorf("failed to update match in db: %w", err)
		}
	}

	// Calculate the new filled value for the order.
	isMarketBuy := t.Type() == order.MarketOrderType && !trade.Sell
	var filled uint64
	// If the order has been canceled, add that to the filled.
	if cancelMatch != nil {
		filled += cancelMatch.Quantity
	}
	for _, mt := range t.matches {
		if isMarketBuy {
			filled += calc.BaseToQuote(mt.Match.Rate, mt.Match.Quantity)
		} else {
			filled += mt.Match.Quantity
		}
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
	haveNewSwaps := len(newTrackers) > 0
	if cancelMatch != nil {
		details := fmt.Sprintf("%s order on %s-%s at %s has been canceled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), t.dc.acct.host, t.token())
		t.notify(newOrderNote("Order canceled", details, db.Success, corder))
		// Also send out a data notification with the cancel order information.
		t.notify(newOrderNote("cancel", "", db.Data, cancelOrder))

		// If the order's backing coins/change are unused, unlock them,
		// otherwise they have been or will be spent by a swap.
		if !haveNewSwaps && !t.hasUnswappedMatches() {
			if t.change == nil && len(t.matches) == 0 {
				err := t.wallets.fromWallet.ReturnCoins(t.coinList())
				if err != nil {
					log.Warnf("Unable to return %s funding coins: %v", unbip(t.wallets.fromAsset.ID), err)
				}
			} else if t.changeLocked {
				err := t.wallets.fromWallet.ReturnCoins(asset.Coins{t.change})
				if err != nil {
					log.Warnf("Unable to return %s change coin %v: %v", unbip(t.wallets.fromAsset.ID), t.change, err)
				}
			}
		}
	}
	if haveNewSwaps {
		fillPct := 100 * float64(filled) / float64(trade.Quantity)
		details := fmt.Sprintf("%s order on %s-%s %.1f%% filled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), fillPct, t.token())
		log.Debugf("Trade order %v matched with %d orders: +%d filled, total fill %d / %d (%.1f%%)",
			t.ID(), len(msgMatches), newFill, filled, trade.Quantity, fillPct)
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

// isSwappable will be true if the match is ready for a swap transaction to be
// broadcast.
func (t *trackedTrade) isSwappable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || proof.IsRevoked {
		log.Debugf("Match %v not swappable: failErr = %v, revoked = %v",
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
		// This might be ready to swap. Check the confirmations on the maker's
		// swap.
		coin := match.counterSwap.Coin()
		confs, err := coin.Confirmations()
		if err != nil {
			log.Errorf("error getting confirmations for match %s on order %s for coin %s",
				match.id, t.UID(), coin)
			return false
		}
		assetCfg := t.wallets.fromAsset
		ok := confs >= assetCfg.SwapConf
		if !ok {
			log.Debugf("Match %v not yet swappable: current confs = %d, required confs = %d",
				match.id, confs, assetCfg.SwapConf)
		}
		return ok
	}
	if dbMatch.Side == order.Maker && metaData.Status == order.NewlyMatched {
		return true
	}
	return false
}

// isActive will be true if the trade is booked or epoch, or if any of the
// matches are still negotiating.
func (t *trackedTrade) isActive() bool {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	// Status of the order itself.
	if t.metaData.Status == order.OrderStatusBooked || t.metaData.Status == order.OrderStatusEpoch {
		return true
	}

	// Status of all matches for the order.
	for _, match := range t.matches {
		log.Tracef("Checking match %v (%v) in status %v. Order: %v, Refund coin: %v, Script: %x", match.id,
			match.Match.Side, match.MetaData.Status, t.ID(), match.MetaData.Proof.RefundCoin, match.MetaData.Proof.Script)
		if match.MetaData.Status == order.MatchComplete {
			continue
		}

		// Refunded matches are inactive regardless of status.
		if len(match.MetaData.Proof.RefundCoin) > 0 {
			continue
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
				continue
			}
		}

		return true
	}
	return false
}

// isRedeemable will be true if the match is ready for our redemption to be
// broadcast.
func (t *trackedTrade) isRedeemable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || len(proof.RefundCoin) != 0 {
		log.Debugf("Match %v not redeemable: failErr = %v, RefundCoin = %v",
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
		coin := match.counterSwap.Coin()
		confs, err := coin.Confirmations()
		if err != nil {
			log.Errorf("error getting confirmations for match %s on order %s for coin %s",
				match.id, t.UID(), coin)
			return false
		}
		assetCfg := t.wallets.toAsset
		ok := confs >= assetCfg.SwapConf
		if !ok {
			log.Debugf("Match %v not yet redeemable: current confs = %d, required confs = %d",
				match.id, confs, assetCfg.SwapConf)
		}
		return ok
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
//   not been notified of Maker's redeem.
// - Our swap's locktime has expired.
//
// Those checks are skipped and isRefundable is false if we've already
// executed a refund or our refund-to wallet is locked.
func (t *trackedTrade) isRefundable(match *matchTracker) bool {
	dbMatch, _, proof, _ := match.parts()
	if match.refundErr != nil || len(proof.RefundCoin) != 0 {
		log.Debugf("Match %v not refundable: refundErr = %v, RefundCoin = %v",
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
	if match.contractExpiry.IsZero() {
		// First check for this match.
		match.contractExpiry = contractExpiry
	} else if (time.Until(contractExpiry) % time.Hour) > t.dc.tickInterval {
		// Already logged, and it's not the first tick of the hour.
		return false
	}

	swapCoinID := proof.TakerSwap
	if dbMatch.Side == order.Maker {
		swapCoinID = proof.MakerSwap
	}
	from := t.wallets.fromAsset
	log.Infof("Contract for match %v with swap coin %v (%s) has an expiry time of %v (%v), not yet expired.",
		match.id, coinIDString(from.ID, swapCoinID), from.Symbol,
		contractExpiry, time.Until(contractExpiry).Round(time.Second))

	return false
}

// tick will check for and perform any match actions necessary.
func (t *trackedTrade) tick() (assetCounter, error) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var swaps, redeems, refunds []*matchTracker
	counts := make(assetCounter)
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

		case t.isRefundable(match):
			log.Debugf("Refundable match %v for order %v (%v)", match.id, t.ID(), side)
			refunds = append(refunds, match)
		default:
			log.Tracef("Match not ready for action as %v: %v", side, match.id)
		}
	}

	fromID := t.wallets.fromAsset.ID
	nSwapsAndRefunds := len(swaps) + len(refunds)
	if nSwapsAndRefunds > 0 {
		counts[fromID] = nSwapsAndRefunds
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
		counts[toAsset] = len(redeems)
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

	return counts, errs.ifAny()
}

// resendPendingRequests checks all matches for this order to re-attempt
// sending the `init` or `redeem` request where necessary.
func (t *trackedTrade) resendPendingRequests() error {
	errs := newErrorSet("resendPendingRequest: order %s - ", t.ID())

	for _, match := range t.matches {
		dbMatch, _, proof, auth := match.parts()
		// do not resend pending requests for revoked matches or matches where
		// we've refunded our swap.
		if match.failErr != nil || proof.IsRevoked || len(proof.RefundCoin) != 0 {
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

// revoke sets the status as revoked. If there are no unswapped matches, the
// trades funding coins or change coin will be returned.
func (t *trackedTrade) revoke() {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	if t.metaData.Status == order.OrderStatusEpoch ||
		t.metaData.Status == order.OrderStatusBooked {

		metaOrder := t.metaOrder()
		metaOrder.MetaData.Status = order.OrderStatusRevoked
		err := t.db.UpdateOrder(metaOrder)
		if err != nil {
			log.Errorf("unable to update order: %v", err)
		}
	}
	if !t.hasUnswappedMatches() {
		var err error
		if t.change == nil {
			err = t.wallets.fromWallet.ReturnCoins(t.coinList())
		} else {
			err = t.wallets.fromWallet.ReturnCoins(asset.Coins{t.change})
		}
		if err != nil {
			log.Warnf("unable to return %s funding coins: %v", unbip(t.wallets.fromAsset.ID), err)
		}
	}
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

	errs := newErrorSet("handleRevokeMatchMsg (order %v, match %v): ", t.ID(), matchID)

	// Set the match as revoked.
	revokedMatch.MetaMatch.MetaData.Proof.IsRevoked = true
	err := t.db.UpdateMatch(&revokedMatch.MetaMatch)
	if err != nil {
		errs.add("unable to update match: %v", err)
	}

	// Notify the user of the failed match.
	corder, _ := t.coreOrderInternal() // no cancel order
	t.notify(newOrderNote("Match revoked", fmt.Sprintf("Match %s has been revoked",
		t.token()), db.WarningLevel, corder))

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

	log.Warnf("Match %v revoked in status %v for order %v", matchID, revokedMatch.Match.Status, t.ID())
	return nil
}

// swapMatches will send a transaction with swap outputs for the specified
// matches.
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

	// Figure out how many have already been swapped.
	var alreadySwapped int
	for _, match := range t.matches {
		if (match.Match.Side == order.Maker && match.Match.Status >= order.MakerSwapCast) ||
			(match.Match.Side == order.Taker && match.Match.Status >= order.TakerSwapCast) {
			alreadySwapped += 1
		}
	}
	// If these are the last swaps, and the order is filled or canceled or
	// revoked, then we don't need to lock the change coin.
	isLastSwaps := len(matches)+alreadySwapped == len(t.matches)
	if isLastSwaps && t.metaData.Status > order.OrderStatusBooked {
		t.changeLocked = false
	} else {
		t.changeLocked = true
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

	// Send the swap. If the swap fails, set the failErr flag for all matches.
	// A more sophisticated solution might involve tracking the error time too
	// and trying again in certain circumstances.
	swaps := &asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    highestFeeRate,
		LockChange: t.changeLocked,
	}
	receipts, change, err := t.wallets.fromWallet.Swap(swaps)
	if err != nil {
		// Set the error on the matches.
		for _, match := range matches {
			match.failErr = err
		}
		return errs.add("error sending swap transaction: %v", err)
	}

	log.Infof("Broadcasted transaction with %d swap contracts for order %v. Fee rate = %d. Receipts: %v",
		len(receipts), t.ID(), swaps.FeeRate, receipts)

	if change == nil {
		t.metaData.ChangeCoin = nil
	} else {
		t.coins[hex.EncodeToString(change.ID())] = change
		t.metaData.ChangeCoin = []byte(change.ID())
	}
	t.change = change
	log.Debugf("Saving change coin %v (%v) to DB for order %v",
		coinIDString(fromAsset.ID, t.metaData.ChangeCoin), fromAsset.Symbol, t.ID())
	t.db.SetChangeCoin(t.ID(), t.metaData.ChangeCoin)

	// Process the swap for each match by sending the `init` request
	// to the DEX and updating the match with swap details.
	// Add any errors encountered to `errs` and proceed to next match
	// to ensure that swap details are saved for all matches.
	for i, receipt := range receipts {
		match := matches[i]
		coin := receipt.Coin()
		log.Infof("Contract coin %v (%s), value = %d, refundable at %v (script = %v)",
			coin, t.wallets.fromAsset.Symbol, coin.Value(), receipt.Expiration(), coin.Redeem())
		if secret := match.MetaData.Proof.Secret; len(secret) > 0 {
			log.Tracef("Contract coin %v secret = %x", coin, secret)
		}
		err := t.finalizeSwapAction(match, coin.ID(), coin.Redeem())
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
	coinIDs, outCoin, err := redeemWallet.Redeem(redemptions)
	// If an error was encountered, fail all of the matches. A failed match will
	// not run again on during ticks.
	if err != nil {
		for _, match := range matches {
			match.failErr = err
		}
		return errs.addErr(err)
	}

	log.Infof("Broadcasted redeem transaction spending %d contracts for order %v, paying to %s:%s",
		len(redemptions), t.ID(), redeemAsset.Symbol, outCoin)

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
func (t *trackedTrade) finalizeRedeemAction(match *matchTracker, coinID []byte) error {
	_, _, proof, auth := match.parts()
	if len(auth.RedeemSig) != 0 {
		return fmt.Errorf("'redeem' already sent for match %v", match.id)
	}
	errs := newErrorSet("")

	// attempt to send `redeem` request and validate server ack.
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

// refundMatches will send refund transactions for the specified matches.
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
		var swapCoinID order.CoinID
		var matchFailureReason string
		switch {
		case dbMatch.Side == order.Maker && dbMatch.Status == order.MakerSwapCast:
			swapCoinID = proof.MakerSwap
			matchFailureReason = "no valid counterswap received from Taker"
		case dbMatch.Side == order.Maker && dbMatch.Status == order.TakerSwapCast && len(proof.MakerRedeem) == 0:
			swapCoinID = proof.MakerSwap
			matchFailureReason = "unable to redeem Taker's swap"
		case dbMatch.Side == order.Taker && dbMatch.Status == order.TakerSwapCast:
			swapCoinID = proof.TakerSwap
			matchFailureReason = "no valid redemption received from Maker"
		default:
			log.Errorf("attempted to execute invalid refund for match %s, side %s, status %s",
				match.id, dbMatch.Side, dbMatch.Status)
			continue
		}

		swapCoinString := coinIDString(refundAsset.ID, swapCoinID)
		log.Infof("Failed match, %s, refunding %s contract %s",
			matchFailureReason, unbip(refundAsset.ID), swapCoinString)

		refundCoin, err := refundWallet.Refund(dex.Bytes(swapCoinID), contractToRefund)
		if err != nil {
			if err == asset.CoinNotFoundError {
				// Could not find the contract coin, which means it has been spent.
				// TODO: begin find redemption
				// NOTE: may be that swap was actually refunded, so FindRedemption
				// should account for that.
			}
			errs.add("error sending refund tx for match %s, swap coin %s: %v",
				match.id, swapCoinString, err)
			match.refundErr = err
			continue
		}
		if t.Trade().Sell {
			refundedQty += match.Match.Quantity
		} else {
			refundedQty += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)
		}
		proof.RefundCoin = []byte(refundCoin)
		//match.SetStatus(order.MatchRefunded)
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
			errChan <- ExpirationErr(fmt.Sprintf("timed out waiting for AuditContract coin %x",
				coinIDString(t.wallets.fromAsset.ID, audit.CoinID)))
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
	reqLockTime := encode.DropMilliseconds(matchTime.Add(t.lockTimeMaker)) // counterparty = maker, their locktime = 48 hours.
	if dbMatch.Side == order.Maker {
		// Check that the secret hash is correct.
		if !bytes.Equal(proof.SecretHash, auditInfo.SecretHash()) {
			return errs.add("secret hash mismatch. expected %x, got %x", proof.SecretHash, auditInfo.SecretHash())
		}
		match.SetStatus(order.TakerSwapCast)
		proof.TakerSwap = []byte(audit.CoinID)
		// counterparty = taker, their locktime = 24 hours.
		reqLockTime = encode.DropMilliseconds(matchTime.Add(t.lockTimeTaker))
	} else {
		proof.SecretHash = auditInfo.SecretHash()
		match.SetStatus(order.MakerSwapCast)
		proof.MakerSwap = []byte(audit.CoinID)
	}

	if auditInfo.Expiration().Before(reqLockTime) {
		return errs.add("lock time too early. Need %s, got %s", reqLockTime, auditInfo.Expiration())
	}

	log.Infof("Audited contract (%s: %v) paying to %s for order %v...",
		t.wallets.toAsset.Symbol, auditInfo.Coin(), auditInfo.Recipient(), audit.OrderID)

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
	// If the order was loaded from the DB, we're the maker, and we've already
	// redeemed, the counterSwap (AuditInfo) will not have been retrieved.
	if match.Match.Side == order.Taker && match.counterSwap == nil {
		return errs.add("redemption received before audit request")
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
	dbMatch, _, proof, auth := match.parts()
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
	if dbMatch.Status != order.TakerSwapCast {
		return fmt.Errorf("maker redemption received at incorrect step %d", dbMatch.Status)
	}
	wallet := t.wallets.toWallet
	if !wallet.ValidateSecret(redemption.Secret, proof.SecretHash) {
		// TODO: Run FindRedemption here? Or wait for broadcast timeout?
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

// hasUnswappedMatches will be true if the order has any remaining matches for
// which our swap has not been broadcast. hasUnswappedMatches should be called
// with the matchMtx locked.
func (t *trackedTrade) hasUnswappedMatches() bool {
	for _, match := range t.matches {
		dbMatch := match.Match
		if (dbMatch.Side == order.Taker && dbMatch.Status < order.TakerSwapCast) ||
			(dbMatch.Side == order.Maker && dbMatch.Status < order.MakerSwapCast) {

			return true
		}
	}
	return false
}

// coinList makes a slice of the coin map values.
func (t *trackedTrade) coinList() []asset.Coin {
	coins := make([]asset.Coin, 0, len(t.coins))
	for _, coin := range t.coins {
		coins = append(coins, coin)
	}
	return coins
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
