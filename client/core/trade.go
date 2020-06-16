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

// txWaitExpiration is the longest the AuthManager will wait for a coin
// waiter. This could be thought of as the maximum allowable backend latency.
var txWaitExpiration = time.Minute

// ExpirationErr indicates that the wait.TickerQueue has expired a waiter, e.g.
// a reported coin was not found before txWaitExpiration.
type ExpirationErr string

// Error satisfies the error interface for ExpirationErr.
func (err ExpirationErr) Error() string { return string(err) }

// A matchTracker is used to negotiate a match.
type matchTracker struct {
	db.MetaMatch
	failErr     error
	refundErr   error
	prefix      *order.Prefix
	trade       *order.Trade
	counterSwap asset.AuditInfo
	id          order.MatchID
}

// The status is part of both the UserMatch and the MatchMetaData used to
// update the DB. Set them both together.
func (match *matchTracker) setStatus(status order.MatchStatus) {
	match.Match.Status = status
	match.MetaData.Status = status
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
	cancel        *trackedCancel
	matchMtx      sync.RWMutex
	matches       map[order.MatchID]*matchTracker
	notify        func(Notification)
	epochLen      uint64
}

// newTrackedTrade is a constructor for a trackedTrade.
func newTrackedTrade(dbOrder *db.MetaOrder, preImg order.Preimage, dc *dexConnection, epochLen uint64,
	lockTimeTaker, lockTimeMaker time.Duration, db db.DB, latencyQ *wait.TickerQueue, wallets *walletSet,
	coins asset.Coins, notify func(Notification)) *trackedTrade {

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
	}
}

// rate returns the order's rate, or zero if a market or cancel order.
func (t *trackedTrade) rate() uint64 {
	if ord, ok := t.Order.(*order.LimitOrder); ok {
		return ord.Rate
	}
	return 0
}

// coreOrder constructs a *core.Order for the tracked order.Order. If the trade
// has a cancel order associated with it, the cancel order will be returned,
// otherwise the second returned *Order will be nil.
func (t *trackedTrade) coreOrder() (*Order, *Order) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	t.matchMtx.RLock()
	defer t.matchMtx.RUnlock()
	return t.coreOrderInternal()
}

// coreOrderInternal constructs a *core.Order for the tracked order.Order. If
// the trade has a cancel order associated with it, the cancel order will be
// returned, otherwise the second returned *Order will be nil. coreOrderInternal
// should be called with both the mtx and the matchMtx locked.
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
func (t *trackedTrade) cancelTrade(co *order.CancelOrder, preImg order.Preimage) {
	t.mtx.Lock()
	t.cancel = &trackedCancel{
		CancelOrder: *co,
		preImg:      preImg,
	}
	t.mtx.Unlock()
}

// readConnectMatches resolves the matches reported by the server in the
// 'connect' response against those in the match tracker map.
func (t *trackedTrade) readConnectMatches(msgMatches []*msgjson.Match) {
	ids := make(map[order.MatchID]bool)
	var extras []*msgjson.Match
	var missing []order.MatchID
	corder, _ := t.coreOrder()
	t.matchMtx.RLock()
	defer t.matchMtx.RUnlock()
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
		if !ids[match.id] {
			missing = append(missing, match.id)
			match.failErr = fmt.Errorf("order not reported by the server on connect")
		}
	}

	host := t.dc.acct.host
	if len(missing) > 0 {
		details := fmt.Sprintf("%d matches for order %s were not reported by %q and are in a failed state",
			len(missing), t.ID(), host)
		corder, _ := t.coreOrder()
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
// from the DEX or a tip change. negotiate calls tick internally.
func (t *trackedTrade) negotiate(msgMatches []*msgjson.Match) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	// Add the matches to the match map and update the database.
	trade := t.Trade()
	var includesCancellation, includesTrades bool
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
		match := &matchTracker{
			id:     mid,
			prefix: t.Prefix(),
			trade:  t.Trade(),
			MetaMatch: db.MetaMatch{
				MetaData: &db.MatchMetaData{
					Status: order.NewlyMatched,
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
					OrderID:  oid,
					MatchID:  mid,
					Quantity: msgMatch.Quantity,
					Rate:     msgMatch.Rate,
					Address:  msgMatch.Address,
					Status:   order.MatchStatus(msgMatch.Status),
					Side:     order.MatchSide(msgMatch.Side),
				},
			},
		}
		// First check that this isn't a match on its own cancel order. I'm not crazy
		// about this, but I am detecting this case right now based on the Address
		// field being an empty string.
		isCancel := t.cancel != nil && msgMatch.Address == ""
		if isCancel {
			match.setStatus(order.MatchComplete)
			includesCancellation = true
			log.Infof("maker notification for cancel order received for order %s. match id = %s", oid, msgMatch.MatchID)
			t.cancel.matches.maker = msgMatch
		} else {
			includesTrades = true
			t.matchMtx.Lock()
			t.matches[match.id] = match
			t.matchMtx.Unlock()
		}
		err := t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			return err
		}
	}

	// Calculate the new filled value for the order.
	isMarketBuy := t.Type() == order.MarketOrderType && !trade.Sell
	var filled uint64
	// If the order has been canceled, add that to the filled.
	if t.cancel != nil && t.cancel.matches.maker != nil {
		filled += t.cancel.matches.maker.Quantity
	}
	t.matchMtx.RLock()
	for _, n := range t.matches {
		if isMarketBuy {
			filled += calc.BaseToQuote(n.Match.Rate, n.Match.Quantity)
		} else {
			filled += n.Match.Quantity
		}
	}
	// The filled amount includes all of the trackedTrade's matches, so the
	// filled amount must be set, not just increased.
	trade.SetFill(filled)
	// Set the order as executed depending on type and fill.
	if t.metaData.Status != order.OrderStatusCanceled {
		switch t.Prefix().Type() {
		case order.LimitOrderType:
			if trade.Filled() == trade.Quantity {
				t.metaData.Status = order.OrderStatusExecuted
			}
		case order.CancelOrderType:
			t.metaData.Status = order.OrderStatusCanceled
		case order.MarketOrderType:
			t.metaData.Status = order.OrderStatusExecuted
		}
	}
	corder, cancelOrder := t.coreOrderInternal()
	t.matchMtx.RUnlock()

	err := t.db.UpdateOrder(t.metaOrder())
	if err != nil {
		return fmt.Errorf("Failed to update order in db")
	}

	// Send notifications.
	if includesCancellation {
		details := fmt.Sprintf("%s order on %s-%s at %s has been canceled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), t.dc.acct.host, t.token())
		t.notify(newOrderNote("Order canceled", details, db.Success, corder))
		// Also send out a data notification with the cancel order information.
		t.notify(newOrderNote("cancel", "", db.Data, cancelOrder))
		// Set the order status for both orders.
		t.metaData.Status = order.OrderStatusCanceled
		t.db.UpdateOrderStatus(t.cancel.ID(), order.OrderStatusExecuted)
	}
	if includesTrades {
		fillRatio := float64(trade.Filled()) / float64(trade.Quantity)
		details := fmt.Sprintf("%s order on %s-%s %.1f%% filled (%s)",
			strings.Title(sellString(trade.Sell)), unbip(t.Base()), unbip(t.Quote()), fillRatio*100, t.token())
		log.Debugf("trade order %v matched with %d orders", t.ID(), len(msgMatches))
		t.notify(newOrderNote("Matches made", details, db.Poke, corder))
	}
	_, err = t.tick()
	return err
}

func (t *trackedTrade) metaOrder() *db.MetaOrder {
	return &db.MetaOrder{
		MetaData: t.metaData,
		Order:    t.Order,
	}
}

// processCancelMatch should be called with the message for the match on a
// cancel order.
func (t *trackedTrade) processCancelMatch(msgMatch *msgjson.Match) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var oid order.OrderID
	copy(oid[:], msgMatch.OrderID)
	if oid != t.cancel.ID() {
		return fmt.Errorf("negotiate called for wrong order. %s != %s", oid, t.cancel.ID())
	}
	log.Infof("taker notification for cancel order received for order %s. match id = %s", oid, msgMatch.MatchID)
	t.cancel.matches.taker = msgMatch
	return nil
}

// isSwappable will be true if the match is ready for a swap transaction to be
// broadcast.
func (t *trackedTrade) isSwappable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || proof.IsRevoked {
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
		return confs >= assetCfg.SwapConf
	}
	if dbMatch.Side == order.Maker && metaData.Status == order.NewlyMatched {
		return true
	}
	return false
}

// isRedeemable will be true if the match is ready for our redemption to be
// broadcast.
func (t *trackedTrade) isRedeemable(match *matchTracker) bool {
	dbMatch, metaData, proof, _ := match.parts()
	if match.failErr != nil || proof.RefundCoin != nil {
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
		return confs >= assetCfg.SwapConf
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
	if match.refundErr != nil || proof.RefundCoin != nil {
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
	if proof.Script == nil || dbMatch.Status >= order.MakerRedeemed {
		return false
	}

	// Issue a refund if our swap's locktime has expired.
	swapLocktimeExpired, err := wallet.LocktimeExpired(proof.Script)
	if err != nil {
		log.Errorf("error checking if locktime has expired for %s contract on order %s, match %s: %v",
			dbMatch.Side, t.ID(), match.id, err)
		return false
	}
	return swapLocktimeExpired
}

// tick will check for and perform any match actions necessary.
func (t *trackedTrade) tick() (assetCounter, error) {
	var swaps, redeems, refunds []*matchTracker
	counts := make(assetCounter)
	errs := newErrorSet(t.dc.acct.host + " tick: ")

	// Check all matches for and resend pending requests as necessary.
	if err := t.resendPendingRequests(); err != nil {
		errs.addErr(err)
	}

	// Check all matches and send swap, redeem or refund as necessary.
	var sent, quoteSent, received, quoteReceived uint64
	t.matchMtx.Lock()
	defer t.matchMtx.Unlock()
	for _, match := range t.matches {
		switch {
		case t.isSwappable(match):
			swaps = append(swaps, match)
			sent += match.Match.Quantity
			quoteSent += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)

		case t.isRedeemable(match):
			redeems = append(redeems, match)
			received += match.Match.Quantity
			quoteReceived += calc.BaseToQuote(match.Match.Rate, match.Match.Quantity)

		case t.isRefundable(match):
			refunds = append(refunds, match)
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

	return counts, errs.ifany()
}

// resendPendingRequests checks all matches for this order to re-attempt
// sending the `init` or `redeem` request where necessary.
func (t *trackedTrade) resendPendingRequests() error {
	errs := newErrorSet("resendPendingRequest: order %s - ", t.ID())

	t.matchMtx.Lock()
	defer t.matchMtx.Unlock()
	for _, match := range t.matches {
		dbMatch, _, proof, auth := match.parts()
		// do not resend pending requests for revoked matches or matches where
		// we've refunded our swap.
		if match.failErr != nil || proof.IsRevoked || proof.RefundCoin != nil {
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
		if swapCoinID != nil && auth.InitSig == nil { // resend pending `init` request
			err = t.finalizeSwapAction(match, swapCoinID, proof.Script)
		} else if redeemCoinID != nil && auth.RedeemSig == nil { // resend pending `redeem` request
			err = t.finalizeRedeemAction(match, swapCoinID)
		}
		if err != nil {
			errs.addErr(err)
		}
	}

	return errs.ifany()
}

// swapMatches will send a transaction with swap outputs for the specified
// matches.
func (t *trackedTrade) swapMatches(matches []*matchTracker) error {
	errs := newErrorSet("swapMatches order %s - ", t.ID())
	// Prepare the asset.Contracts.
	swaps := new(asset.Swaps)
	for _, match := range matches {
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

		contract := &asset.Contract{
			Address:    dbMatch.Address,
			Value:      value,
			SecretHash: proof.SecretHash,
			LockTime:   uint64(lockTime),
		}
		swaps.Contracts = append(swaps.Contracts, contract)
	}

	// Fund the swap. If this isn't the first swap, use the change coin from the
	// previous swaps.
	coinIDs := t.Trade().Coins
	if len(t.metaData.ChangeCoin) > 0 {
		coinIDs = []order.CoinID{t.metaData.ChangeCoin}
	}

	swaps.Inputs = make([]asset.Coin, 0, len(coinIDs))
	for _, coinID := range coinIDs {
		coin, found := t.coins[hex.EncodeToString(coinID)]
		if !found {
			fromID := t.wallets.fromAsset.ID
			return errs.add("%s coin %s not found", unbip(fromID), coinIDString(fromID, coinID))
		}
		swaps.Inputs = append(swaps.Inputs, coin)
	}

	// Send the swap. If the swap fails, set the failErr flag for all matches.
	// A more sophisticated solution might involve tracking the error time too
	// and trying again in certain circumstances.
	receipts, change, err := t.wallets.fromWallet.Swap(swaps, t.wallets.fromAsset)
	if err != nil {
		// Set the error on the matches.
		for _, match := range matches {
			match.failErr = err
		}
		return errs.add("error sending swap transaction: %v", err)
	}
	t.change = change
	t.coins[hex.EncodeToString(change.ID())] = change
	t.metaData.ChangeCoin = []byte(change.ID())
	t.db.SetChangeCoin(t.ID(), t.metaData.ChangeCoin)

	// Process the swap for each match by sending the `init` request
	// to the DEX and updating the match with swap details.
	// Add any errors encountered to `errs` and proceed to next match
	// to ensure that swap details are saved for all matches.
	coinStrs := make([]string, len(receipts))
	for i, receipt := range receipts {
		match := matches[i]
		coin := receipt.Coin()
		coinStrs[i] = coin.String()
		err := t.finalizeSwapAction(match, coin.ID(), coin.Redeem())
		if err != nil {
			errs.addErr(err)
		}
	}

	log.Infof("Broadcasted %d swap transactions for order %v. Contract coins: %v",
		len(receipts), t.ID(), coinStrs)

	return errs.ifany()
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
	if auth.InitSig != nil {
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
	if err := t.dc.signAndRequest(init, msgjson.InitRoute, ack); err != nil {
		errs.add("error sending 'init' message for match %s: %v", match.id, err)
	} else if err := t.dc.acct.checkSig(init.Serialize(), ack.Sig); err != nil {
		errs.add("'init' ack signature error for match %s: %v", match.id, err)
	}

	// Update the match db data with the swap details.
	proof.Script = contract
	if match.Match.Side == order.Taker {
		proof.TakerSwap = coinID
		match.setStatus(order.TakerSwapCast)
	} else {
		proof.MakerSwap = coinID
		match.setStatus(order.MakerSwapCast)
	}
	if ack.Sig != nil {
		auth.InitSig = ack.Sig
		auth.InitStamp = encode.UnixMilliU(time.Now())
	}
	if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
		errs.add("error storing swap details in database for match %s, coin %s: %v",
			match.id, coinIDString(t.wallets.fromAsset.ID, coinID), err)
	}
	return errs.ifany()
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
	coinIDs, outCoin, err := redeemWallet.Redeem(redemptions, redeemAsset)
	// If an error was encountered, fail all of the matches. A failed match will
	// not run again on during ticks.
	if err != nil {
		for _, match := range matches {
			match.failErr = err
		}
		return errs.addErr(err)
	}
	for _, match := range matches {
		log.Infof("match %s complete: %s %d %s",
			match.id, sellString(t.Trade().Sell), match.Match.Quantity, unbip(t.Prefix().BaseAsset),
		)
	}

	// Send the redemption information to the DEX.
	for i, match := range matches {
		err := t.finalizeRedeemAction(match, coinIDs[i])
		if err != nil {
			errs.addErr(err)
		}
	}

	log.Infof("Broadcasted redeem transaction spending %d contracts for order %v, paying to %s:%s",
		len(redemptions), t.ID(), redeemAsset.Symbol, outCoin)

	return errs.ifany()
}

// finalizeRedeemAction sends a `redeem` request for the specified match,
// waits for and validates the server's acknowledgement, then saves the
// redeem details to db.
// The redeem details are always saved even if sending the `redeem` request
// errors or a valid ack is not received from the server. This makes it
// possible to resend the `redeem` request at a later time.
func (t *trackedTrade) finalizeRedeemAction(match *matchTracker, coinID []byte) error {
	_, _, proof, auth := match.parts()
	if auth.RedeemSig != nil {
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
	if err := t.dc.signAndRequest(msgRedeem, msgjson.RedeemRoute, ack); err != nil {
		errs.add("error sending 'redeem' message for match %s: %v", match.id, err)
	} else if err := t.dc.acct.checkSig(msgRedeem.Serialize(), ack.Sig); err != nil {
		errs.add("'redeem' ack signature error for match %s: %v", match.id, err)
	}

	// Update the match db data with the redeem details.
	if ack.Sig != nil {
		auth.RedeemSig = ack.Sig
		auth.RedeemStamp = encode.UnixMilliU(time.Now())
	}
	if match.Match.Side == order.Taker {
		match.setStatus(order.MatchComplete)
		proof.TakerRedeem = coinID
	} else {
		match.setStatus(order.MakerRedeemed)
		proof.MakerRedeem = coinID
	}
	if err := t.db.UpdateMatch(&match.MetaMatch); err != nil {
		errs.add("error storing redeem details in database for match %s, coin %s: %v",
			match.id, coinIDString(t.wallets.toAsset.ID, coinID), err)
	}
	return errs.ifany()
}

// refundMatches will send refund transactions for the specified matches.
func (t *trackedTrade) refundMatches(matches []*matchTracker) (uint64, error) {
	errs := newErrorSet("refundMatches: order %s - ", t.ID())

	refundWallet, refundAsset := t.wallets.fromWallet, t.wallets.fromAsset // refunding to our wallet
	var refundedQty uint64

	for _, match := range matches {
		dbMatch, _, proof, _ := match.parts()
		if proof.RefundCoin != nil {
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
		case dbMatch.Side == order.Maker && dbMatch.Status == order.TakerSwapCast && proof.MakerRedeem == nil:
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
		log.Infof("failed match, %s, refunding %s contract %s",
			matchFailureReason, unbip(refundAsset.ID), swapCoinString)

		refundCoin, err := refundWallet.Refund(dex.Bytes(swapCoinID), contractToRefund, refundAsset)
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
		err = t.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			errs.add("error storing match info in database: %v", err)
		}
	}

	return refundedQty, errs.ifany()
}

// processAudit processes the audit request from the server.
func (t *trackedTrade) processAudit(msgID uint64, audit *msgjson.Audit) error {
	// Find the match and check the server's signature.
	var mid order.MatchID
	copy(mid[:], audit.MatchID)
	errs := newErrorSet("processAudit - order %s, match %s -", t.ID(), mid)
	t.matchMtx.Lock()
	defer t.matchMtx.Unlock()
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
	// until expiration after txWaitExpiration.
	errChan := make(chan error, 1)
	var auditInfo asset.AuditInfo
	t.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(txWaitExpiration),
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
			errChan <- ExpirationErr(fmt.Sprintf("timed out waiting for AuditContract coin %x", audit.CoinID))
		},
	})
	err = extractError(errChan, requestTimeout, "AuditContract")
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
		match.setStatus(order.TakerSwapCast)
		proof.TakerSwap = []byte(audit.CoinID)
		// counterparty = taker, their locktime = 24 hours.
		reqLockTime = encode.DropMilliseconds(matchTime.Add(t.lockTimeTaker))
	} else {
		proof.SecretHash = auditInfo.SecretHash()
		match.setStatus(order.MakerSwapCast)
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
	var mid order.MatchID
	copy(mid[:], redemption.MatchID)
	errs := newErrorSet("processRedemption order %s, match %s - ", t.ID(), mid)
	t.matchMtx.Lock()
	defer t.matchMtx.Unlock()
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
		match.setStatus(order.MatchComplete)
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

	match.setStatus(order.MakerRedeemed)
	proof.MakerRedeem = []byte(redemption.CoinID)
	proof.Secret = redemption.Secret
	err = t.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		return errs.add("error storing match info in database: %v", err)
	}
	return nil
}

// processEpoch processes the new epoch index. If an existing trade has an epoch
// index < the new index and has a status of OrderStatusEpoch, the order will
// be assumed to have matched without error, and the status set to executed or
// booked, depending on the order type.
func (t *trackedTrade) processEpoch(epochIdx uint64) bool {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	tradeEpoch := uint64(t.Time()) / t.epochLen
	if t.metaData.Status != order.OrderStatusEpoch || epochIdx <= tradeEpoch {
		return false
	}
	t.metaData.Status = order.OrderStatusExecuted
	if lo, ok := t.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
		t.metaData.Status = order.OrderStatusBooked
	}
	t.db.UpdateOrderStatus(t.ID(), t.metaData.Status)
	return true
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
