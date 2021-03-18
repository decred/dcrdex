// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/db/bolt"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/decred/go-socks/socks"
)

const (
	keyParamsKey      = "keyParams"
	conversionFactor  = 1e8
	regFeeAssetSymbol = "dcr" // Hard-coded to Decred for registration fees, for now.

	// regConfirmationsPaid is used to indicate completed registration to
	// (*Core).setRegConfirms.
	regConfirmationsPaid uint32 = math.MaxUint32
	// tickCheckDivisions is how many times to tick trades per broadcast timeout
	// interval. e.g. 12 min btimeout / 8 divisions = 90 sec between checks.
	tickCheckDivisions = 8
)

var (
	unbip = dex.BipIDSymbol
	// The coin waiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// When waiting for a wallet to sync, a SyncStatus check will be performed
	// ever syncTickerPeriod. var instead of const for testing purposes.
	syncTickerPeriod = 10 * time.Second
)

// dexConnection is the websocket connection and the DEX configuration.
type dexConnection struct {
	comms.WsConn
	connMaster   *dex.ConnectionMaster
	log          dex.Logger
	tickInterval time.Duration
	acct         *dexAccount
	notify       func(Notification)

	assetsMtx sync.RWMutex
	assets    map[uint32]*dex.Asset

	cfgMtx sync.RWMutex
	cfg    *msgjson.ConfigResult

	booksMtx sync.RWMutex
	books    map[string]*bookie

	// tradeMtx is used to synchronize access to the trades map.
	tradeMtx sync.RWMutex
	trades   map[order.OrderID]*trackedTrade

	epochMtx sync.RWMutex
	epoch    map[string]uint64
	// connected is a best guess on the ws connection status.
	connected uint32

	regConfMtx  sync.RWMutex
	regConfirms *uint32 // nil regConfirms means no pending registration.
}

// DefaultResponseTimeout is the default timeout for responses after a request is
// successfully sent.
const (
	DefaultResponseTimeout = comms.DefaultResponseTimeout
	fundingTxWait          = 2 * time.Minute
)

// running returns the status of the provided market.
func (dc *dexConnection) running(mkt string) bool {
	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()
	mktCfg := dc.findMarketConfig(mkt)
	if mktCfg == nil {
		return false // not found means not running
	}
	return mktCfg.Running()
}

// marketConfig is the market's configuration, as returned by the server in the
// 'config' response.
func (dc *dexConnection) marketConfig(mktID string) *msgjson.Market {
	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()
	return dc.findMarketConfig(mktID)
}

// marketMap creates a map of this DEX's *Market keyed by name/ID,
// [base]_[quote].
func (dc *dexConnection) marketMap() map[string]*Market {
	dc.cfgMtx.RLock()
	mktConfigs := dc.cfg.Markets
	dc.cfgMtx.RUnlock()

	marketMap := make(map[string]*Market, len(mktConfigs))
	for _, mkt := range mktConfigs {
		// The presence of the asset for every market was already verified when the
		// dexConnection was created in connectDEX.
		dc.assetsMtx.RLock()
		base, quote := dc.assets[mkt.Base], dc.assets[mkt.Quote]
		dc.assetsMtx.RUnlock()
		mkt := &Market{
			Name:            mkt.Name,
			BaseID:          base.ID,
			BaseSymbol:      base.Symbol,
			QuoteID:         quote.ID,
			QuoteSymbol:     quote.Symbol,
			EpochLen:        mkt.EpochLen,
			StartEpoch:      mkt.StartEpoch,
			MarketBuyBuffer: mkt.MarketBuyBuffer,
		}
		mktID := mkt.marketName()

		for _, trade := range dc.marketTrades(mktID) {
			mkt.Orders = append(mkt.Orders, trade.coreOrder())
		}

		marketMap[mktID] = mkt
	}

	return marketMap
}

// marketTrades is a slice of trades in the trades map.
func (dc *dexConnection) marketTrades(mktID string) []*trackedTrade {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	trades := make([]*trackedTrade, 0, len(dc.trades))
	for _, trade := range dc.trades {
		if trade.mktID == mktID {
			trades = append(trades, trade)
		}
	}
	return trades
}

// getRegConfirms returns the number of confirmations received for the
// dex registration or nil if the registration is completed
func (dc *dexConnection) getRegConfirms() *uint32 {
	dc.regConfMtx.RLock()
	defer dc.regConfMtx.RUnlock()
	return dc.regConfirms
}

// setRegConfirms sets the number of confirmations received
// for the dex registration
func (dc *dexConnection) setRegConfirms(confs uint32) {
	dc.regConfMtx.Lock()
	defer dc.regConfMtx.Unlock()
	if confs == regConfirmationsPaid {
		// A nil regConfirms indicates that there is no pending registration.
		dc.regConfirms = nil
		return
	}
	dc.regConfirms = &confs
}

// hasActiveAssetOrders checks whether there are any active orders or negotiating
// matches for the specified asset.
func (dc *dexConnection) hasActiveAssetOrders(assetID uint32) bool {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for _, trade := range dc.trades {
		if (trade.Base() == assetID || trade.Quote() == assetID) &&
			trade.isActive() {
			return true
		}
	}
	return false
}

// hasActiveOrders checks whether there are any active orders for the dexConnection.
func (dc *dexConnection) hasActiveOrders() bool {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()

	for _, trade := range dc.trades {
		if trade.isActive() {
			return true
		}
	}
	return false
}

// findOrder returns the tracker and preimage for an order ID, and a boolean
// indicating whether this is a cancel order.
func (dc *dexConnection) findOrder(oid order.OrderID) (tracker *trackedTrade, preImg order.Preimage, isCancel bool) {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	// Try to find the order as a trade.
	if tracker, found := dc.trades[oid]; found {
		return tracker, tracker.preImg, false
	}
	// Search the cancel order IDs.
	for _, tracker := range dc.trades {
		if tracker.cancel != nil && tracker.cancel.ID() == oid {
			return tracker, tracker.cancel.preImg, true
		}
	}
	return
}

// tryCancel will look for an order with the specified order ID, and attempt to
// cancel the order. It is not an error if the order is not found.
func (c *Core) tryCancel(dc *dexConnection, oid order.OrderID) (found bool, err error) {
	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return // false, nil
	}
	return true, c.tryCancelTrade(dc, tracker)
}

// tryCancelTrade attempts to cancel the order.
func (c *Core) tryCancelTrade(dc *dexConnection, tracker *trackedTrade) error {
	oid := tracker.ID()
	if lo, ok := tracker.Order.(*order.LimitOrder); !ok || lo.Force != order.StandingTiF {
		return fmt.Errorf("cannot cancel %s order %s that is not a standing limit order", tracker.Type(), oid)
	}

	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()

	if status := tracker.metaData.Status; status != order.OrderStatusEpoch && status != order.OrderStatusBooked {
		return fmt.Errorf("order %v not cancellable in status %v", oid, status)
	}

	if tracker.cancel != nil {
		// Existing cancel might be stale. Deleting it now allows this
		// cancel attempt to proceed.
		tracker.deleteStaleCancelOrder()

		if tracker.cancel != nil {
			return fmt.Errorf("order %s - only one cancel order can be submitted per order per epoch. "+
				"still waiting on cancel order %s to match", oid, tracker.cancel.ID())
		}
	}

	// Construct the order.
	prefix := tracker.Prefix()
	preImg := newPreimage()
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  prefix.AccountID,
			BaseAsset:  prefix.BaseAsset,
			QuoteAsset: prefix.QuoteAsset,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Now(),
			Commit:     preImg.Commit(),
		},
		TargetOrderID: oid,
	}
	err := order.ValidateOrder(co, order.OrderStatusEpoch, 0)
	if err != nil {
		return err
	}

	// Create and send the order message. Check the response before using it.
	route, msgOrder := messageOrder(co, nil)
	var result = new(msgjson.OrderResult)
	err = dc.signAndRequest(msgOrder, route, result, DefaultResponseTimeout)
	if err != nil {
		return err
	}
	err = validateOrderResponse(dc, result, co, msgOrder)
	if err != nil {
		return fmt.Errorf("Abandoning order. preimage: %x, server time: %d: %w",
			preImg[:], result.ServerTime, err)
	}

	// Store the cancel order with the tracker.
	err = tracker.cancelTrade(co, preImg)
	if err != nil {
		return fmt.Errorf("error storing cancel order info %s: %w", co.ID(), err)
	}

	// Now that the trackedTrade is updated, sync with the preimage request.
	c.syncOrderPlaced(co.ID())

	// Store the cancel order.
	err = c.db.UpdateOrder(&db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			Host:   dc.acct.host,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
			LinkedOrder: oid,
		},
		Order: co,
	})
	if err != nil {
		return fmt.Errorf("failed to store order in database: %w", err)
	}

	c.log.Infof("Cancel order %s targeting order %s at %s has been placed",
		co.ID(), oid, dc.acct.host)

	c.notify(newOrderNote(SubjectCancellingOrder, "A cancel order has been submitted for order "+tracker.token(), db.Poke, tracker.coreOrderInternal()))

	return nil
}

// Synchronize with the preimage request, in case that request came before
// we had an order ID and added this order to the trades map or cancel field.
func (c *Core) syncOrderPlaced(oid order.OrderID) {
	c.piSyncMtx.Lock()
	syncChan, found := c.piSyncers[oid]
	if found {
		// If we found the channel, the preimage request is already waiting.
		// Close the channel to allow that request to proceed.
		close(syncChan)
		delete(c.piSyncers, oid)
	} else {
		// If there is no channel, the preimage request hasn't come yet. Add the
		// channel to signal readiness.
		c.piSyncers[oid] = make(chan struct{})
	}
	c.piSyncMtx.Unlock()
}

// signAndRequest signs and sends the request, unmarshaling the response into
// the provided interface.
func (dc *dexConnection) signAndRequest(signable msgjson.Signable, route string, result interface{}, timeout time.Duration) error {
	if dc.acct.locked() {
		return fmt.Errorf("cannot sign: %s account locked", dc.acct.host)
	}
	sign(dc.acct.privKey, signable)
	return sendRequest(dc.WsConn, route, signable, result, timeout)
}

// ack sends an Acknowledgement for a match-related request.
func (dc *dexConnection) ack(msgID uint64, matchID order.MatchID, signable msgjson.Signable) (err error) {
	ack := &msgjson.Acknowledgement{
		MatchID: matchID[:],
	}
	sigMsg := signable.Serialize()
	ack.Sig, err = dc.acct.sign(sigMsg)
	if err != nil {
		return fmt.Errorf("sign error - %w", err)
	}
	msg, err := msgjson.NewResponse(msgID, ack, nil)
	if err != nil {
		return fmt.Errorf("NewResponse error - %w", err)
	}
	err = dc.Send(msg)
	if err != nil {
		return fmt.Errorf("Send error - %w", err)
	}
	return nil
}

// serverMatches are an intermediate structure used by the dexConnection to
// sort incoming match notifications.
type serverMatches struct {
	tracker    *trackedTrade
	msgMatches []*msgjson.Match
	cancel     *msgjson.Match
}

// parseMatches sorts the list of matches and associates them with a trade. This
// may be called from handleMatchRoute on receipt of a new 'match' request, or
// by authDEX with the list of active matches returned by the 'connect' request.
func (dc *dexConnection) parseMatches(msgMatches []*msgjson.Match, checkSigs bool) (map[order.OrderID]*serverMatches, []msgjson.Acknowledgement, error) {
	var acks []msgjson.Acknowledgement
	matches := make(map[order.OrderID]*serverMatches)
	var errs []string
	for _, msgMatch := range msgMatches {
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		tracker, _, isCancel := dc.findOrder(oid)
		if tracker == nil {
			errs = append(errs, "order "+oid.String()+" not found")
			continue
		}

		// Check the fee rate against the maxfeerate recorded at order time.
		swapRate := msgMatch.FeeRateQuote
		if tracker.Trade().Sell {
			swapRate = msgMatch.FeeRateBase
		}
		if !isCancel && swapRate > tracker.metaData.MaxFeeRate {
			errs = append(errs, fmt.Sprintf("rejecting match %s for order %s because assigned rate (%d) is > MaxFeeRate (%d)",
				msgMatch.MatchID, msgMatch.OrderID, swapRate, tracker.metaData.MaxFeeRate))
			continue
		}

		sigMsg := msgMatch.Serialize()
		if checkSigs {
			err := dc.acct.checkSig(sigMsg, msgMatch.Sig)
			if err != nil {
				// If the caller (e.g. handleMatchRoute) requests signature
				// verification, this is fatal.
				return nil, nil, fmt.Errorf("parseMatches: match signature verification failed: %w", err)
			}
		}
		sig, err := dc.acct.sign(sigMsg)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		// Success. Add the serverMatch and the Acknowledgement.
		acks = append(acks, msgjson.Acknowledgement{
			MatchID: msgMatch.MatchID,
			Sig:     sig,
		})

		trackerID := tracker.ID()
		match := matches[trackerID]
		if match == nil {
			match = &serverMatches{
				tracker: tracker,
			}
			matches[trackerID] = match
		}
		if isCancel {
			match.cancel = msgMatch // taker match
		} else {
			match.msgMatches = append(match.msgMatches, msgMatch)
		}

		status := order.MatchStatus(msgMatch.Status)
		dc.log.Debugf("Registering match %v for order %v (%v) in status %v",
			msgMatch.MatchID, oid, order.MatchSide(msgMatch.Side), status)
	}

	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("parseMatches errors: %s", strings.Join(errs, ", "))
	}
	// A non-nil error only means that at least one match failed to parse, so we
	// must return the successful matches and acks for further processing.
	return matches, acks, err
}

// matchDiscreps specifies a trackedTrades's missing and extra matches compared
// to the server's list of active matches as returned in the connect response.
type matchDiscreps struct {
	trade   *trackedTrade
	missing []*matchTracker
	extra   []*msgjson.Match
}

// matchStatusConflict is a conflict between our status, and the status returned
// by the server in the connect response.
type matchStatusConflict struct {
	trade   *trackedTrade
	matches []*matchTracker
}

// compareServerMatches resolves the matches reported by the server in the
// 'connect' response against those marked incomplete in the matchTracker map
// for each serverMatch.
// Reported matches with missing trackers are already checked by parseMatches,
// but we also must check for incomplete matches that the server is not
// reporting.
func (dc *dexConnection) compareServerMatches(srvMatches map[order.OrderID]*serverMatches) (
	exceptions map[order.OrderID]*matchDiscreps, statusConflicts map[order.OrderID]*matchStatusConflict) {

	exceptions = make(map[order.OrderID]*matchDiscreps)
	statusConflicts = make(map[order.OrderID]*matchStatusConflict)

	// Identify extra matches named by the server response that we do not
	// recognize.
	for oid, match := range srvMatches {
		var extra []*msgjson.Match
		match.tracker.mtx.RLock()
		for _, msgMatch := range match.msgMatches {
			var matchID order.MatchID
			copy(matchID[:], msgMatch.MatchID)
			mt := match.tracker.matches[matchID]
			if mt == nil {
				extra = append(extra, msgMatch)
				continue
			}
			if mt.Status != order.MatchStatus(msgMatch.Status) {
				conflict := statusConflicts[oid]
				if conflict == nil {
					conflict = &matchStatusConflict{trade: match.tracker}
					statusConflicts[oid] = conflict
				}
				conflict.matches = append(conflict.matches, mt)
			}
		}
		match.tracker.mtx.RUnlock()
		if len(extra) > 0 {
			exceptions[match.tracker.ID()] = &matchDiscreps{
				trade: match.tracker,
				extra: extra,
			}
		}
	}

	in := func(matches []*msgjson.Match, mid []byte) bool {
		for _, m := range matches {
			if bytes.Equal(m.MatchID, mid) {
				return true
			}
		}
		return false
	}

	setMissing := func(trade *trackedTrade, missing []*matchTracker) {
		if tt, found := exceptions[trade.ID()]; found {
			tt.missing = missing
		} else {
			exceptions[trade.ID()] = &matchDiscreps{
				trade:   trade,
				missing: missing,
			}
		}
	}

	// Identify active matches that are missing from server's response.
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for oid, trade := range dc.trades {
		activeMatches := trade.activeMatches()
		if len(activeMatches) == 0 {
			continue
		}
		tradeMatches, found := srvMatches[oid]
		if !found {
			// ALL of this trade's active matches are missing.
			setMissing(trade, activeMatches)
			continue // check next local trade
		}
		// Check this local trade's active matches against server's reported
		// matches for this trade.
		var missing []*matchTracker
		for _, match := range activeMatches { // each local match
			if !in(tradeMatches.msgMatches, match.MatchID[:]) { // against reported matches
				missing = append(missing, match)
			}
		}
		if len(missing) > 0 {
			setMissing(trade, missing)
		}
	}

	return
}

// reconcileTrades compares the statuses of orders in the dc.trades map to the
// statuses returned by the server on `connect`, updating the statuses of the
// tracked trades where applicable e.g.
// - Booked orders that were tracked as Epoch are updated to status Booked.
// - Orders thought to be active in the dc.trades map but not returned by the
//   server are updated to Executed, Canceled or Revoked.
// Setting the order status appropriately now, especially for inactive orders,
// ensures that...
// - the affected trades can be retired once the trade ticker (in core.listen)
//   observes that there are no active matches for the trades.
// - coins are unlocked either as the affected trades' matches are swapped or
//   revoked (for trades with active matches), or when the trades are retired.
// Also purges "stale" cancel orders if the targeted order is returned in the
// server's `connect` response. See *trackedTrade.deleteStaleCancelOrder for
// the definition of a stale cancel order.
func (dc *dexConnection) reconcileTrades(srvOrderStatuses []*msgjson.OrderStatus) (unknownOrdersCount, reconciledOrdersCount int) {
	dc.tradeMtx.RLock()
	// Check for unknown orders reported as active by the server. If such
	// exists, could be that they were known to the client but were thought
	// to be inactive and thus were not loaded from db or were retired.
	srvActiveOrderStatuses := make(map[order.OrderID]*msgjson.OrderStatus, len(srvOrderStatuses))
	for _, srvOrderStatus := range srvOrderStatuses {
		var oid order.OrderID
		copy(oid[:], srvOrderStatus.ID)
		if _, tracked := dc.trades[oid]; tracked {
			srvActiveOrderStatuses[oid] = srvOrderStatus
		} else {
			dc.log.Warnf("Unknown order %v reported by DEX %s as active", oid, dc.acct.host)
			unknownOrdersCount++
		}
	}
	knownActiveTrades := make(map[order.OrderID]*trackedTrade)
	for oid, trade := range dc.trades {
		trade.mtx.RLock()
		if trade.metaData.Status == order.OrderStatusEpoch || trade.metaData.Status == order.OrderStatusBooked {
			knownActiveTrades[oid] = trade
		} else if srvOrderStatus := srvActiveOrderStatuses[oid]; srvOrderStatus != nil {
			dc.log.Warnf("Inactive order %v, status %q reported by DEX %s as active, status %q",
				oid, trade.metaData.Status, dc.acct.host, order.OrderStatus(srvOrderStatus.Status))
		}
		trade.mtx.RUnlock()
	}
	dc.tradeMtx.RUnlock()

	updateOrder := func(trade *trackedTrade, srvOrderStatus *msgjson.OrderStatus) {
		reconciledOrdersCount++
		oid := trade.ID()
		previousStatus := trade.metaData.Status
		newStatus := order.OrderStatus(srvOrderStatus.Status)
		trade.metaData.Status = newStatus
		// If there is an associated cancel order, and we are revising the
		// status of the targeted order to anything other than canceled, we can
		// infer the cancel order is done. Update the status of the cancel order
		// and unlink it from the trade. If the targeted order is reported as
		// canceled, that indicates we submitted a cancel order preimage but
		// missed the match notification, so the cancel order is executed.
		if newStatus != order.OrderStatusCanceled {
			trade.deleteCancelOrder()
		} else if trade.cancel != nil {
			cid := trade.cancel.ID()
			err := trade.db.UpdateOrderStatus(cid, order.OrderStatusExecuted)
			if err != nil {
				dc.log.Errorf("Failed to update status of executed cancel order %v: %v", cid, err)
			}
		}
		// Now update the trade.
		if err := trade.db.UpdateOrder(trade.metaOrder()); err != nil {
			dc.log.Errorf("Error updating status in db for order %v from %v to %v", oid, previousStatus, newStatus)
		} else {
			dc.log.Warnf("Order %v updated from recorded status %q to new status %q reported by DEX %s",
				oid, previousStatus, newStatus, dc.acct.host)
		}

		details := fmt.Sprintf("Status of order %v revised from %v to %v", trade.token(), previousStatus, newStatus)
		dc.notify(newOrderNote(SubjectOrderStatusUpdate, details, db.WarningLevel, trade.coreOrderInternal()))
	}

	// Compare the status reported by the server for each known active trade. Orders
	// for which the server did not return a status are no longer active (now Executed,
	// Canceled or Revoked). Use the order_status route to determine the correct status
	// for such orders and update accordingly.
	var orderStatusRequests []*msgjson.OrderStatusRequest
	for oid, trade := range knownActiveTrades {
		srvOrderStatus := srvActiveOrderStatuses[oid]
		if srvOrderStatus == nil {
			// Order status not returned by server. Must be inactive now.
			// Request current status from the DEX.
			orderStatusRequests = append(orderStatusRequests, &msgjson.OrderStatusRequest{
				Base:    trade.Base(),
				Quote:   trade.Quote(),
				OrderID: trade.ID().Bytes(),
			})
			continue
		}

		trade.mtx.Lock()

		// Server reports this order as active. Delete any associated cancel
		// order if the cancel order's epoch has passed.
		trade.deleteStaleCancelOrder()

		serverStatus := order.OrderStatus(srvOrderStatus.Status)
		if trade.metaData.Status == serverStatus {
			dc.log.Tracef("Status reconciliation not required for order %v, status %q, server-reported status %q",
				oid, trade.metaData.Status, serverStatus)
		} else if trade.metaData.Status == order.OrderStatusEpoch && serverStatus == order.OrderStatusBooked {
			// Only standing orders can move from Epoch to Booked. This must have
			// happened in the client's absence (maybe a missed nomatch message).
			if lo, ok := trade.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
				updateOrder(trade, srvOrderStatus)
			} else {
				dc.log.Warnf("Incorrect status %q reported for non-standing order %v by DEX %s, client status = %q",
					serverStatus, oid, dc.acct.host, trade.metaData.Status)
			}
		} else {
			dc.log.Warnf("Inconsistent status %q reported for order %v by DEX %s, client status = %q",
				serverStatus, oid, dc.acct.host, trade.metaData.Status)
		}

		trade.mtx.Unlock()
	}

	if len(orderStatusRequests) > 0 {
		dc.log.Debugf("Requesting statuses for %d orders from DEX %s", len(orderStatusRequests), dc.acct.host)

		// Send the 'order_status' request.
		var orderStatusResults []*msgjson.OrderStatus
		err := sendRequest(dc.WsConn, msgjson.OrderStatusRoute, orderStatusRequests, &orderStatusResults, DefaultResponseTimeout)
		if err != nil {
			dc.log.Errorf("Error retreiving order statuses from DEX %s: %v", dc.acct.host, err)
			return
		}

		if len(orderStatusResults) != len(orderStatusRequests) {
			dc.log.Errorf("Retrieved statuses for %d out of %d orders from order_status route",
				len(orderStatusResults), len(orderStatusRequests))
		}

		// Update the orders with the statuses received.
		for _, srvOrderStatus := range orderStatusResults {
			var oid order.OrderID
			copy(oid[:], srvOrderStatus.ID)
			trade := knownActiveTrades[oid] // no need to lock dc.tradeMtx
			trade.mtx.Lock()
			updateOrder(trade, srvOrderStatus)
			trade.mtx.Unlock()
		}
	}

	return
}

// tickAsset checks open matches related to a specific asset for needed action.
func (c *Core) tickAsset(dc *dexConnection, assetID uint32) assetMap {
	dc.tradeMtx.RLock()
	assetTrades := make([]*trackedTrade, 0, len(dc.trades))
	for _, trade := range dc.trades {
		if trade.Base() == assetID || trade.Quote() == assetID {
			assetTrades = append(assetTrades, trade)
		}
	}
	dc.tradeMtx.RUnlock()

	updateChan := make(chan assetMap)
	for _, trade := range assetTrades {
		trade := trade // bad go, bad
		go func() {
			newUpdates, err := c.tick(trade)
			if err != nil {
				c.log.Errorf("%s tick error: %v", dc.acct.host, err)
			}
			updateChan <- newUpdates
		}()
	}

	updated := make(assetMap)
	for range assetTrades {
		updated.merge(<-updateChan)
	}
	return updated
}

// Get the *dexConnection and connection status for the the host.
func (c *Core) dex(addr string) (*dexConnection, bool, error) {
	host, err := addrHost(addr)
	if err != nil {
		return nil, false, newError(addressParseErr, "error parsing address: %v", err)
	}

	// Get the dexConnection and the dex.Asset for each asset.
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	connected := found && atomic.LoadUint32(&dc.connected) == 1
	if !found {
		return nil, false, fmt.Errorf("unknown DEX %s", addr)
	}
	return dc, connected, nil
}

// Get the *dexConnection for the the host. Return an error if the DEX is not
// connected.
func (c *Core) connectedDEX(addr string) (*dexConnection, error) {
	dc, connected, err := c.dex(addr)
	if err != nil {
		return nil, err
	}

	if dc.acct.locked() {
		return nil, fmt.Errorf("cannot place order on a locked %s account. Are you logged in?", dc.acct.host)
	}

	if !connected {
		return nil, fmt.Errorf("currently disconnected from %s. Cannot place order", dc.acct.host)
	}
	return dc, nil
}

// setEpoch sets the epoch. If the passed epoch is greater than the highest
// previously passed epoch, an epoch notification is sent to all subscribers and
// true is returned.
func (dc *dexConnection) setEpoch(mktID string, epochIdx uint64) bool {
	dc.epochMtx.Lock()
	defer dc.epochMtx.Unlock()
	if epochIdx > dc.epoch[mktID] {
		dc.epoch[mktID] = epochIdx
		dc.notify(newEpochNotification(dc.acct.host, mktID, epochIdx))
		return true
	}
	return false
}

// marketEpochDuration gets the market's epoch duration. If the market is not
// known, an error is logged and 0 is returned.
func (dc *dexConnection) marketEpochDuration(mktID string) uint64 {
	mkt := dc.marketConfig(mktID)
	if mkt == nil {
		dc.log.Errorf("marketEpoch called for unknown market %s", mktID)
		return 0
	}
	return mkt.EpochLen
}

// marketEpoch gets the epoch index for the specified market and time stamp. If
// the market is not known, 0 is returned.
func (dc *dexConnection) marketEpoch(mktID string, stamp time.Time) uint64 {
	epochLen := dc.marketEpochDuration(mktID)
	if epochLen == 0 {
		return 0
	}
	return encode.UnixMilliU(stamp) / epochLen
}

// blockWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations. The blockWaiter is similar to
// dcrdex/server/blockWaiter.Waiter, but is different enough to warrant a
// separate type.
type blockWaiter struct {
	assetID uint32
	trigger func() (bool, error)
	action  func(error)
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// Net is the current network.
	Net dex.Network
	// Logger is the Core's logger and is also used to create the sub-loggers
	// for the asset backends.
	Logger dex.Logger
	// TorProxy specifies the address of a Tor proxy server.
	TorProxy string
	// TorIsolation specifies whether to enable Tor circuit isolation.
	TorIsolation bool
}

// Core is the core client application. Core manages DEX connections, wallets,
// database access, match negotiation and more.
type Core struct {
	ctx           context.Context
	wg            sync.WaitGroup
	ready         chan struct{}
	cfg           *Config
	log           dex.Logger
	db            db.DB
	net           dex.Network
	lockTimeTaker time.Duration
	lockTimeMaker time.Duration

	wsConstructor func(*comms.WsCfg) (comms.WsConn, error)
	newCrypter    func([]byte) encrypt.Crypter
	reCrypter     func([]byte, []byte) (encrypt.Crypter, error)
	latencyQ      *wait.TickerQueue

	connMtx sync.RWMutex
	conns   map[string]*dexConnection

	walletMtx sync.RWMutex
	wallets   map[uint32]*xcWallet

	waiterMtx    sync.RWMutex
	blockWaiters map[string]*blockWaiter

	tickSchedMtx sync.Mutex
	tickSched    map[order.OrderID]*time.Timer

	noteMtx   sync.RWMutex
	noteChans []chan Notification

	piSyncMtx sync.Mutex
	piSyncers map[order.OrderID]chan struct{}
}

// New is the constructor for a new Core.
func New(cfg *Config) (*Core, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("Core.Config must specify a Logger")
	}
	db, err := bolt.NewDB(cfg.DBPath, cfg.Logger.SubLogger("DB"))
	if err != nil {
		return nil, fmt.Errorf("database initialization error: %w", err)
	}
	if cfg.TorProxy != "" {
		if _, _, err = net.SplitHostPort(cfg.TorProxy); err != nil {
			return nil, err
		}
	}

	core := &Core{
		cfg:           cfg,
		ready:         make(chan struct{}),
		log:           cfg.Logger,
		db:            db,
		conns:         make(map[string]*dexConnection),
		wallets:       make(map[uint32]*xcWallet),
		net:           cfg.Net,
		lockTimeTaker: dex.LockTimeTaker(cfg.Net),
		lockTimeMaker: dex.LockTimeMaker(cfg.Net),
		blockWaiters:  make(map[string]*blockWaiter),
		piSyncers:     make(map[order.OrderID]chan struct{}),
		tickSched:     make(map[order.OrderID]*time.Timer),
		// Allowing to change the constructor makes testing a lot easier.
		wsConstructor: comms.NewWsConn,
		newCrypter:    encrypt.NewCrypter,
		reCrypter:     encrypt.Deserialize,
		latencyQ:      wait.NewTickerQueue(recheckInterval),
	}

	// Populate the initial user data. User won't include any DEX info yet, as
	// those are retrieved when Run is called and the core connects to the DEXes.
	core.log.Debugf("new client core created")
	return core, nil
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	c.log.Infof("Starting DEX client core")
	// Store the context as a field, since we will need to spawn new DEX threads
	// when new accounts are registered.
	c.ctx = ctx
	c.initialize() // connectDEX gets ctx for the wsConn
	close(c.ready)

	// The DB starts first and stops last.
	ctxDB, stopDB := context.WithCancel(context.Background())
	var dbWG sync.WaitGroup
	dbWG.Add(1)
	go func() {
		defer dbWG.Done()
		c.db.Run(ctxDB)
	}()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.latencyQ.Run(ctx)
	}()

	c.wg.Wait() // block here until all goroutines except DB complete

	// Stop the DB after dexConnections and other goroutines are done.
	stopDB()
	dbWG.Wait()

	// Lock and disconnect the wallets.
	c.walletMtx.Lock()
	for assetID, wallet := range c.wallets {
		delete(c.wallets, assetID)
		if !wallet.connected() {
			continue
		}
		symb := strings.ToUpper(unbip(assetID))
		c.log.Infof("Locking %s wallet", symb)
		if err := wallet.Lock(); err != nil {
			c.log.Errorf("Failed to lock %v wallet: %v", symb, err)
		}
		wallet.Disconnect()
	}
	c.walletMtx.Unlock()

	// Clear account private keys and wait for the DEX ws connections that began
	// shutting down on context cancellation.
	c.connMtx.Lock()
	defer c.connMtx.Unlock()
	for _, dc := range c.conns {
		// context should be canceled allowing just a Wait(), but just in case
		// use Disconnect otherwise this could hang forever.
		dc.connMaster.Disconnect()
		dc.acct.lock()
	}

	c.log.Infof("DEX client core off")
}

// Ready returns a channel that is closed when Run completes its initialization
// tasks and Core becomes ready for use.
func (c *Core) Ready() <-chan struct{} {
	return c.ready
}

const defaultDEXPort = "7232"

// addrHost returns the host or url:port pair for an address.
func addrHost(addr string) (string, error) {
	const defaultHost = "localhost"
	const missingPort = "missing port in address"
	// Empty addresses are localhost.
	if addr == "" {
		return defaultHost + ":" + defaultDEXPort, nil
	}
	host, port, splitErr := net.SplitHostPort(addr)
	_, portErr := strconv.ParseUint(port, 10, 16)

	// net.SplitHostPort will error on anything not in the format
	// string:string or :string or if a colon is in an unexpected position,
	// such as in the scheme.
	// If the port isn't a port, it must also be parsed.
	if splitErr != nil || portErr != nil {
		// Any address with no colons is appended with the default port.
		var addrErr *net.AddrError
		if errors.As(splitErr, &addrErr) && addrErr.Err == missingPort {
			host = strings.Trim(addrErr.Addr, "[]") // JoinHostPort expects no brackets for ipv6 hosts
			return net.JoinHostPort(host, defaultDEXPort), nil
		}
		// These are addresses with at least one colon in an unexpected
		// position.
		a, err := url.Parse(addr)
		// This address is of an unknown format.
		if err != nil {
			return "", fmt.Errorf("addrHost: unable to parse address '%s'", addr)
		}
		host, port = a.Hostname(), a.Port()
		// If the address parses but there is no port, append the default port.
		if port == "" {
			return net.JoinHostPort(host, defaultDEXPort), nil
		}
	}
	// We have a port but no host. Replace with localhost.
	if host == "" {
		host = defaultHost
	}
	return net.JoinHostPort(host, port), nil
}

// Network returns the current DEX network.
func (c *Core) Network() dex.Network {
	return c.net
}

// Exchanges creates a map of *Exchange keyed by host, including markets and
// orders.
func (c *Core) Exchanges() map[string]*Exchange {
	return c.exchangeMap()
}

// dexConnections creates a slice of the *dexConnection in c.conns.
func (c *Core) dexConnections() []*dexConnection {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	conns := make([]*dexConnection, 0, len(c.conns))
	for _, conn := range c.conns {
		conns = append(conns, conn)
	}
	return conns
}

// exchangeMap creates a map of *Exchange keyed by host, including markets and
// orders.
func (c *Core) exchangeMap() map[string]*Exchange {
	infos := make(map[string]*Exchange, len(c.conns))
	for _, dc := range c.dexConnections() {
		dc.cfgMtx.RLock()
		requiredConfs := uint32(dc.cfg.RegFeeConfirms)
		dc.cfgMtx.RUnlock()

		dc.assetsMtx.RLock()
		assets := make(map[uint32]*dex.Asset, len(dc.assets))
		for assetID, dexAsset := range dc.assets {
			assets[assetID] = dexAsset
		}
		dc.assetsMtx.RUnlock()

		// Set AcctID to empty string if not initialized.
		acctID := dc.acct.ID().String()
		var emptyAcctID account.AccountID
		if dc.acct.ID() == emptyAcctID {
			acctID = ""
		}
		infos[dc.acct.host] = &Exchange{
			Host:          dc.acct.host,
			AcctID:        acctID,
			Markets:       dc.marketMap(),
			Assets:        assets,
			FeePending:    dc.acct.feePending(),
			Connected:     atomic.LoadUint32(&dc.connected) == 1,
			ConfsRequired: requiredConfs,
			RegConfirms:   dc.getRegConfirms(),
		}
	}
	return infos
}

// wallet gets the wallet for the specified asset ID in a thread-safe way.
func (c *Core) wallet(assetID uint32) (*xcWallet, bool) {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	w, found := c.wallets[assetID]
	return w, found
}

// encryptionKey retrieves the application encryption key. The key itself is
// encrypted using an encryption key derived from the user's password.
func (c *Core) encryptionKey(pw []byte) (encrypt.Crypter, error) {
	keyParams, err := c.db.Get(keyParamsKey)
	if err != nil {
		return nil, fmt.Errorf("key retrieval error: %w", err)
	}
	crypter, err := c.reCrypter(pw, keyParams)
	if err != nil {
		return nil, fmt.Errorf("encryption key deserialization error: %w", err)
	}
	return crypter, nil
}

func (c *Core) storeDepositAddress(wdbID []byte, addr string) error {
	// Store the new address in the DB.
	dbWallet, err := c.db.Wallet(wdbID)
	if err != nil {
		return fmt.Errorf("error retreiving DB wallet: %w", err)
	}
	dbWallet.Address = addr
	return c.db.UpdateWallet(dbWallet)
}

func (c *Core) connectAndUpdateWallet(w *xcWallet) error {
	assetID := w.AssetID
	c.log.Infof("Connecting wallet for %s", unbip(assetID))
	addr := w.currentDepositAddress()
	newAddr, err := c.connectWallet(w)
	if err != nil {
		return err // core.Error with code connectWalletErr
	}
	if newAddr != addr {
		c.log.Infof("New deposit address for %v wallet: %v", unbip(assetID), newAddr)
		if err = c.storeDepositAddress(w.dbID, newAddr); err != nil {
			return err
		}
	}
	// First update balances since it is included in WalletState. Ignore errors
	// because some wallets may not reveal balance until unlocked.
	_, err = c.updateWalletBalance(w)
	if err != nil {
		// Warn because the balances will be stale.
		c.log.Warnf("Could not retrieve balances from %s wallet: %v", unbip(assetID), err)
	}

	c.notify(newWalletStateNote(w.state()))
	return nil
}

// connectedWallet fetches a wallet and will connect the wallet if it is not
// already connected. If the wallet gets connected, this also emits WalletState
// and WalletBalance notification.
func (c *Core) connectedWallet(assetID uint32) (*xcWallet, error) {
	wallet, exists := c.wallet(assetID)
	if !exists {
		return nil, newError(missingWalletErr, "no configured wallet found %s (%d)", unbip(assetID), assetID)
	}
	if !wallet.connected() {
		err := c.connectAndUpdateWallet(wallet)
		if err != nil {
			return nil, err
		}
	}
	return wallet, nil
}

// connectWallet connects to the wallet and returns the deposit address
// validated by the xcWallet after connecting. If the wallet backend is still
// synching, this also starts a goroutine to monitor sync status, emitting
// WalletStateNotes on each progress update.
func (c *Core) connectWallet(w *xcWallet) (depositAddr string, err error) {
	err = w.Connect() // ensures valid deposit address
	if err != nil {
		return "", codedError(connectWalletErr, err)
	}

	w.mtx.RLock()
	defer w.mtx.RUnlock()
	depositAddr = w.address

	// If the wallet is not synced, start a loop to check the sync status until
	// it is.
	if !w.synced {
		// If the wallet is shut down before sync is complete, exit the syncer
		// loop.
		innerCtx, cancel := context.WithCancel(c.ctx)
		go func() {
			w.connector.Wait()
			cancel()
		}()
		// The syncer loop.
		go func() {
			ticker := time.NewTicker(syncTickerPeriod)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					synced, progress, err := w.SyncStatus()
					if err != nil {
						c.log.Errorf("Unable to get wallet/node sync status for %s: %v", unbip(w.AssetID), err)
						continue // keep trying since the loop will break if connection really drops
					}
					w.mtx.Lock()
					w.synced = synced
					w.syncProgress = progress
					w.mtx.Unlock()
					c.notify(newWalletStateNote(w.state()))
					if synced {
						return
					}

				case <-innerCtx.Done():
					return
				}
			}
		}()
	}

	return
}

// Connect to the wallet if not already connected. Unlock the wallet if not
// already unlocked.
func (c *Core) connectAndUnlock(crypter encrypt.Crypter, wallet *xcWallet) error {
	if !wallet.connected() {
		c.log.Infof("Connecting wallet for %s", unbip(wallet.AssetID))
		err := c.connectAndUpdateWallet(wallet)
		if err != nil {
			return err
		}
	}
	// Unlock if either the backend itself is locked or if we lack a cached
	// unencrypted password for encrypted wallets.
	if !wallet.unlocked() {
		// Note that in cases where we already had the cached decrypted password
		// but it was just the backend reporting as locked, only unlocking the
		// backend is needed but this redecrypts the password using the provided
		// crypter. This case could instead be handled with a refreshUnlock.
		err := wallet.Unlock(crypter)
		if err != nil {
			return newError(walletAuthErr, "failed to unlock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}
	return nil
}

// walletBalance gets the xcWallet's current WalletBalance, which includes the
// db.Balance plus order/contract locked amounts. The data is not stored. Use
// updateWalletBalance instead to also update xcWallet.balance and the DB.
func (c *Core) walletBalance(wallet *xcWallet) (*WalletBalance, error) {
	bal, err := wallet.Balance()
	if err != nil {
		return nil, err
	}
	contractLockedAmt, orderLockedAmt := c.lockedAmounts(wallet.AssetID)
	return &WalletBalance{
		Balance: &db.Balance{
			Balance: *bal,
			Stamp:   time.Now(),
		},
		OrderLocked:    orderLockedAmt,
		ContractLocked: contractLockedAmt,
	}, nil
}

// updateWalletBalance retrieves balances for the wallet, updates
// xcWallet.balance and the balance in the DB, and emits a BalanceNote.
func (c *Core) updateWalletBalance(wallet *xcWallet) (*WalletBalance, error) {
	walletBal, err := c.walletBalance(wallet)
	if err != nil {
		return nil, err
	}
	wallet.setBalance(walletBal)

	// Store the db.Balance.
	err = c.db.UpdateBalance(wallet.dbID, walletBal.Balance)
	if err != nil {
		return nil, fmt.Errorf("error updating %s balance in database: %w", unbip(wallet.AssetID), err)
	}

	c.notify(newBalanceNote(wallet.AssetID, walletBal))
	return walletBal, nil
}

// lockedAmounts returns the total amount locked in unredeemed and unrefunded
// swaps (contractLocked) and the total amount locked by orders for future
// swaps (orderLocked). Only applies to trades where the specified assetID is
// the fromAssetID.
func (c *Core) lockedAmounts(assetID uint32) (contractLocked, orderLocked uint64) {
	for _, dc := range c.dexConnections() {
		dc.tradeMtx.RLock()
		for _, tracker := range dc.trades {
			tracker.mtx.RLock()
			if tracker.fromAssetID == assetID {
				contractLocked += tracker.unspentContractAmounts()
				orderLocked += tracker.lockedAmount()
			}
			tracker.mtx.RUnlock()
		}
		dc.tradeMtx.RUnlock()
	}
	return
}

// updateBalances updates the balance for every key in the counter map.
// Notifications are sent.
func (c *Core) updateBalances(assets assetMap) {
	if len(assets) == 0 {
		return
	}
	for assetID := range assets {
		w, exists := c.wallet(assetID)
		if !exists {
			// This should never be the case, but log an error in case I'm
			// wrong or something changes.
			c.log.Errorf("non-existent %d wallet should exist", assetID)
			continue
		}
		_, err := c.updateWalletBalance(w)
		if err != nil {
			c.log.Errorf("error updating %q balance: %v", unbip(assetID), err)
			continue
		}
	}
}

// updateAssetBalance updates the balance for the specified asset. A
// notification is sent.
func (c *Core) updateAssetBalance(assetID uint32) {
	c.updateBalances(assetMap{assetID: struct{}{}})
}

// xcWallets creates a slice of the c.wallets xcWallets.
func (c *Core) xcWallets() []*xcWallet {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	wallets := make([]*xcWallet, 0, len(c.wallets))
	for _, wallet := range c.wallets {
		wallets = append(wallets, wallet)
	}
	return wallets
}

// Wallets creates a slice of WalletState for all known wallets.
func (c *Core) Wallets() []*WalletState {
	wallets := c.xcWallets()
	state := make([]*WalletState, 0, len(wallets))
	for _, wallet := range wallets {
		state = append(state, wallet.state())
	}
	return state
}

// SupportedAssets returns a map of asset information for supported assets.
func (c *Core) SupportedAssets() map[uint32]*SupportedAsset {
	return c.assetMap()
}

// assetMap returns a map of asset information for supported assets.
func (c *Core) assetMap() map[uint32]*SupportedAsset {
	supported := asset.Assets()
	assets := make(map[uint32]*SupportedAsset, len(supported))
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	for assetID, asset := range supported {
		var wallet *WalletState
		w, found := c.wallets[assetID]
		if found {
			wallet = w.state()
		}
		assets[assetID] = &SupportedAsset{
			ID:     assetID,
			Symbol: asset.Symbol,
			Wallet: wallet,
			Info:   asset.Info,
		}
	}
	return assets
}

// User is a thread-safe getter for the User.
func (c *Core) User() *User {
	initialized, err := c.IsInitialized()
	if err != nil {
		c.log.Errorf("User: error checking if app is initialized: %v", err)
	}
	return &User{
		Assets:      c.assetMap(),
		Exchanges:   c.exchangeMap(),
		Initialized: initialized,
	}
}

// CreateWallet creates a new exchange wallet.
func (c *Core) CreateWallet(appPW, walletPW []byte, form *WalletForm) error {
	assetID := form.AssetID
	symbol := unbip(assetID)
	_, exists := c.wallet(assetID)
	if exists {
		return fmt.Errorf("%s wallet already exists", symbol)
	}

	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	var encPW []byte
	if len(walletPW) > 0 {
		encPW, err = crypter.Encrypt(walletPW)
		if err != nil {
			return fmt.Errorf("wallet password encryption error: %w", err)
		}
	}

	walletInfo, err := asset.Info(assetID)
	if err != nil {
		// Only possible error is unknown asset.
		return fmt.Errorf("asset with BIP ID %d is unknown. Did you _ import your asset packages?", assetID)
	}

	// Remove unused key-values from parsed settings before saving to db.
	// Especially necessary if settings was parsed from a config file, b/c
	// config files usually define more key-values than we need.
	// Expected keys should be lowercase because config.Parse returns lowercase
	// keys.
	expectedKeys := make(map[string]bool, len(walletInfo.ConfigOpts))
	for _, option := range walletInfo.ConfigOpts {
		expectedKeys[strings.ToLower(option.Key)] = true
	}
	for key := range form.Config {
		if !expectedKeys[key] {
			delete(form.Config, key)
		}
	}

	dbWallet := &db.Wallet{
		AssetID:     assetID,
		Settings:    form.Config,
		EncryptedPW: encPW,
		// Balance and Address are set after connect.
	}

	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return fmt.Errorf("error loading wallet for %d -> %s: %w", assetID, symbol, err)
	}

	dbWallet.Address, err = c.connectWallet(wallet)
	if err != nil {
		return err
	}

	initErr := func(s string, a ...interface{}) error {
		_ = wallet.Lock() // just try, but don't confuse the user with an error
		wallet.Disconnect()
		return fmt.Errorf(s, a...)
	}

	err = wallet.Unlock(crypter)
	if err != nil {
		wallet.Disconnect()
		return fmt.Errorf("%s wallet authentication error: %w", symbol, err)
	}

	balances, err := c.walletBalance(wallet)
	if err != nil {
		return initErr("error getting wallet balance for %s: %w", symbol, err)
	}
	wallet.balance = balances           // update xcWallet's WalletBalance
	dbWallet.Balance = balances.Balance // store the db.Balance

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		return initErr("error storing wallet credentials: %w", err)
	}

	c.log.Infof("Created %s wallet. Balance available = %d / "+
		"locked = %d / locked in contracts = %d, Deposit address = %s",
		symbol, balances.Available, balances.Locked, balances.ContractLocked,
		dbWallet.Address)

	// The wallet has been successfully created. Store it.
	c.walletMtx.Lock()
	c.wallets[assetID] = wallet
	c.walletMtx.Unlock()

	c.notify(newWalletStateNote(wallet.state()))

	return nil
}

// loadWallet uses the data from the database to construct a new exchange
// wallet. The returned wallet is running but not connected.
func (c *Core) loadWallet(dbWallet *db.Wallet) (*xcWallet, error) {
	// Create the client/asset.Wallet.
	assetID := dbWallet.AssetID
	walletCfg := &asset.WalletConfig{
		Settings: dbWallet.Settings,
		TipChange: func(err error) {
			// asset.Wallet implementations should not need wait for the
			// callback, as they don't know what it is, and will likely launch
			// TipChange as a goroutine. However, guard against the possibility
			// of deadlocking a Core method that calls Wallet.Disconnect.
			go c.tipChange(assetID, err)
		},
	}
	logger := c.log.SubLogger(unbip(assetID))
	w, err := asset.Setup(assetID, walletCfg, logger, c.net)
	if err != nil {
		return nil, fmt.Errorf("error creating wallet: %w", err)
	}

	// Construct the unconnected xcWallet.
	contractLockedAmt, orderLockedAmt := c.lockedAmounts(assetID)
	return &xcWallet{
		Wallet:    w,
		connector: dex.NewConnectionMaster(w),
		AssetID:   assetID,
		balance: &WalletBalance{
			Balance:        dbWallet.Balance,
			OrderLocked:    orderLockedAmt,
			ContractLocked: contractLockedAmt,
		},
		encPW:   dbWallet.EncryptedPW,
		address: dbWallet.Address,
		dbID:    dbWallet.ID(),
	}, nil
}

// WalletState returns the *WalletState for the asset ID.
func (c *Core) WalletState(assetID uint32) *WalletState {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, has := c.wallets[assetID]
	if !has {
		c.log.Tracef("wallet status requested for unknown asset %d -> %s", assetID, unbip(assetID))
		return nil
	}
	return wallet.state()
}

// OpenWallet opens (unlocks) the wallet for use.
func (c *Core) OpenWallet(assetID uint32, appPW []byte) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("OpenWallet: wallet not found for %d -> %s: %w", assetID, unbip(assetID), err)
	}
	err = wallet.Unlock(crypter)
	if err != nil {
		return newError(walletAuthErr, "failed to unlock %s wallet: %v", unbip(assetID), err)
	}

	state := wallet.state()
	balances, err := c.updateWalletBalance(wallet)
	if err != nil {
		return err
	}
	c.log.Infof("Connected to and unlocked %s wallet. Balance available "+
		"= %d / locked = %d / locked in contracts = %d, Deposit address = %s",
		state.Symbol, balances.Available, balances.Locked, balances.ContractLocked,
		state.Address)

	if dcrID, _ := dex.BipSymbolID("dcr"); assetID == dcrID {
		go c.checkUnpaidFees(wallet)
	}

	c.notify(newWalletStateNote(state))

	return nil
}

// CloseWallet closes the wallet for the specified asset. The wallet cannot be
// closed if there are active negotiations for the asset.
func (c *Core) CloseWallet(assetID uint32) error {
	for _, dc := range c.dexConnections() {
		if dc.hasActiveAssetOrders(assetID) {
			return fmt.Errorf("cannot lock %s wallet with active swap negotiations", unbip(assetID))
		}
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("wallet not found for %d -> %s: %w", assetID, unbip(assetID), err)
	}
	err = wallet.Lock()
	if err != nil {
		return err
	}

	c.notify(newWalletStateNote(wallet.state()))

	return nil
}

// ConnectWallet connects to the wallet without unlocking.
func (c *Core) ConnectWallet(assetID uint32) error {
	wallet, err := c.connectedWallet(assetID)
	c.notify(newWalletStateNote(wallet.state()))
	return err
}

// WalletSettings fetches the current wallet configuration details from the
// database.
func (c *Core) WalletSettings(assetID uint32) (map[string]string, error) {
	wallet, found := c.wallet(assetID)
	if !found {
		return nil, newError(missingWalletErr, "%d -> %s wallet not found", assetID, unbip(assetID))
	}
	// Get the settings from the database.
	dbWallet, err := c.db.Wallet(wallet.dbID)
	if err != nil {
		return nil, codedError(dbErr, err)
	}
	return dbWallet.Settings, nil
}

// ReconfigureWallet updates the wallet configuration settings, it also updates
// the password if newWalletPW is non-nil. Do not make concurrent calls to
// ReconfigureWallet for the same asset.
func (c *Core) ReconfigureWallet(appPW, newWalletPW []byte, assetID uint32, cfg map[string]string) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return newError(authErr, "ReconfigureWallet password error: %v", err)
	}
	oldWallet, found := c.wallet(assetID)
	if !found {
		return newError(missingWalletErr, "%d -> %s wallet not found",
			assetID, unbip(assetID))
	}
	dbWallet := &db.Wallet{
		AssetID:     oldWallet.AssetID,
		Settings:    cfg,
		Balance:     &db.Balance{}, // in case retrieving new balance after connect fails
		EncryptedPW: oldWallet.encPW,
		Address:     oldWallet.currentDepositAddress(),
	}
	// Reload the wallet with the new settings.
	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return newError(walletErr, "error loading wallet for %d -> %s: %v",
			assetID, unbip(assetID), err)
	}

	isSettingNewPW := newWalletPW != nil // Includes empty but non-nil
	// If newWalletPW is non-nil, update the wallet's password.
	if isSettingNewPW {
		err = c.setWalletPassword(wallet, newWalletPW, crypter)
		if err != nil {
			return err
		}
	}

	// Must connect to ensure settings are good.
	dbWallet.Address, err = c.connectWallet(wallet)
	if err != nil {
		return err
	}

	// If the password was not changed, carry over any cached password
	// regardless of backend lock state. loadWallet already copied encPW, so
	// this will decrypt pw rather than actually copying it, and it will
	// ensure the backend is also unlocked.
	if !isSettingNewPW && oldWallet.locallyUnlocked() {
		err := wallet.Unlock(crypter)
		if err != nil {
			wallet.Disconnect()
			return newError(walletAuthErr, "wallet successfully connected, but failed to unlock. "+
				"reconfiguration not saved: %v", err)
		}
	}

	balances, err := c.walletBalance(wallet)
	if err != nil {
		c.log.Warnf("Error getting balance for wallet %s: %v", unbip(assetID), err)
		// Do not fail in case this requires an unlocked wallet.
	} else {
		wallet.balance = balances           // update xcWallet's WalletBalance
		dbWallet.Balance = balances.Balance // store the db.Balance
	}

	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		wallet.Disconnect()
		return newError(dbErr, "error saving wallet configuration: %v", err)
	}

	// Update all relevant trackedTrades' toWallet and fromWallet.
	for _, dc := range c.dexConnections() {
		dc.tradeMtx.RLock()
		for _, tracker := range dc.trades {
			tracker.mtx.Lock()
			if tracker.wallets.fromWallet.AssetID == assetID {
				tracker.wallets.fromWallet = wallet
			} else if tracker.wallets.toWallet.AssetID == assetID {
				tracker.wallets.toWallet = wallet
			}
			tracker.mtx.Unlock()
		}
		dc.tradeMtx.RUnlock()
	}

	c.walletMtx.Lock()
	c.wallets[assetID] = wallet
	c.walletMtx.Unlock()

	if oldWallet.connected() {
		// NOTE: Cannot lock the wallet backend because it may be the same as
		// the one just connected.
		go oldWallet.Disconnect()
	}

	c.notify(newBalanceNote(assetID, balances)) // redundant with wallet config note?
	details := fmt.Sprintf("Configuration for %s wallet has been updated. Deposit address = %s",
		unbip(assetID), wallet.address)
	c.notify(newWalletConfigNote(SubjectWalletConfigurationUpdated,
		details, db.Success, wallet.state()))

	// Clear any existing tickGovernors for suspect matches.
	for _, dc := range c.dexConnections() {
		dc.tradeMtx.RLock()
		for _, t := range dc.trades {
			if t.Base() != assetID && t.Quote() != assetID {
				continue
			}
			isFromAsset := t.wallets.fromAsset.ID == assetID
			t.mtx.Lock()
			for _, m := range t.matches {
				if m.tickGovernor != nil &&
					((m.suspectSwap && isFromAsset) || (m.suspectRedeem && !isFromAsset)) {

					m.tickGovernor.Stop()
					m.tickGovernor = nil
				}
			}
			t.mtx.Unlock()
		}
		dc.tradeMtx.RUnlock()
	}

	return nil
}

// SetWalletPassword updates the (encrypted) password for the wallet.
// Returns passwordErr if provided newPW is nil.
func (c *Core) SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error {
	// Ensure newPW isn't nil.
	if newPW == nil {
		return newError(passwordErr, "SetWalletPassword password can't be nil")
	}

	// Check the app password and get the crypter.
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return newError(authErr, "SetWalletPassword password error: %v", err)
	}

	// Check that the specified wallet exists.
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, found := c.wallets[assetID]
	if !found {
		return newError(missingWalletErr, "wallet for %s (%d) is not known", unbip(assetID), assetID)
	}

	// Set new password.
	err = c.setWalletPassword(wallet, newPW, crypter)
	if err != nil {
		return err
	}

	return nil
}

// setWalletPassword updates the (encrypted) password for the wallet.
func (c *Core) setWalletPassword(wallet *xcWallet, newPW []byte, crypter encrypt.Crypter) error {
	// Connect if necessary.
	wasConnected := wallet.connected()
	if !wasConnected {
		if err := c.connectAndUpdateWallet(wallet); err != nil {
			return newError(connectionErr, "SetWalletPassword connection error: %v", err)
		}
	}

	wasUnlocked := wallet.unlocked()
	newPasswordSet := len(newPW) > 0 // excludes empty but non-nil

	// Check that the new password works. If the new password is empty, skip
	// this step, since an empty password signifies an unencrypted wallet.
	// TODO: find a way to verify that the wallet actually is unencrypted or
	// otherwise does not require a password. Perhaps an
	// asset.Wallet.RequiresPassword wallet method?
	if newPasswordSet {
		// Encrypt password if it's not an empty string
		encNewPW, err := crypter.Encrypt(newPW)
		if err != nil {
			return newError(encryptionErr, "encryption error: %v", err)
		}
		err = wallet.Wallet.Unlock(string(newPW))
		if err != nil {
			return newError(authErr,
				"setWalletPassword unlocking wallet error, is the new password correct?: %v", err)
		}
		wallet.encPW = encNewPW
	} else {
		wallet.encPW = nil
	}

	err := c.db.SetWalletPassword(wallet.dbID, wallet.encPW)
	if err != nil {
		return codedError(dbErr, err)
	}

	// Re-lock the wallet if it was previously locked.
	if !wasUnlocked {
		if err = wallet.Lock(); err != nil {
			c.log.Warnf("Unable to relock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}
	// Disconnect if it was not previously connected.
	if !wasConnected {
		wallet.Disconnect()
	}

	details := fmt.Sprintf("Password for %s wallet has been updated.",
		unbip(wallet.AssetID))
	c.notify(newWalletConfigNote(SubjectWalletPasswordUpdated, details,
		db.Success, wallet.state()))

	return nil
}

// NewDepositAddress retrieves a new deposit address from the specified asset's
// wallet, saves it to the database, and emits a notification.
func (c *Core) NewDepositAddress(assetID uint32) (string, error) {
	w, exists := c.wallet(assetID)
	if !exists {
		return "", newError(missingWalletErr, "no wallet found for %s", unbip(assetID))
	}

	// Retrieve a fresh deposit address.
	addr, err := w.refreshDepositAddress()
	if err != nil {
		return "", err
	}

	if err = c.storeDepositAddress(w.dbID, addr); err != nil {
		return "", err
	}

	// Update wallet state in the User data struct and emit a WalletStateNote.
	c.notify(newWalletStateNote(w.state()))

	return addr, nil
}

// AutoWalletConfig attempts to load setting from a wallet package's
// asset.WalletInfo.DefaultConfigPath. If settings are not found, an empty map
// is returned.
func (c *Core) AutoWalletConfig(assetID uint32) (map[string]string, error) {
	winfo, err := asset.Info(assetID)
	if err != nil {
		return nil, fmt.Errorf("asset.Info error: %w", err)
	}
	settings, err := config.Parse(winfo.DefaultConfigPath)
	c.log.Infof("%d %s configuration settings loaded from file at default location %s", len(settings), unbip(assetID), winfo.DefaultConfigPath)
	if err != nil {
		c.log.Debugf("config.Parse could not load settings from default path: %v", err)
		return make(map[string]string), nil
	}
	return settings, nil
}

func (c *Core) isRegistered(host string) bool {
	c.connMtx.RLock()
	_, found := c.conns[host]
	c.connMtx.RUnlock()
	return found
}

// GetFee creates a connection to the specified DEX Server and fetches the
// registration fee. The connection is closed after the fee is retrieved.
// Returns an error if user is already registered to the DEX. A TLS certificate,
// certI, can be provided as either a string filename, or []byte file contents.
func (c *Core) GetFee(dexAddr string, certI interface{}) (uint64, error) {
	host, err := addrHost(dexAddr)
	if err != nil {
		return 0, newError(addressParseErr, "error parsing address: %v", err)
	}
	cert, err := parseCert(host, certI)
	if err != nil {
		return 0, newError(fileReadErr, "failed to read certificate file from %s: %v", cert, err)
	}
	if c.isRegistered(host) {
		return 0, newError(dupeDEXErr, "already registered at %s", dexAddr)
	}

	dc, err := c.connectDEX(&db.AccountInfo{
		Host: host,
		Cert: cert,
	})
	if err != nil {
		return 0, codedError(connectionErr, err)
	}
	defer dc.connMaster.Disconnect()

	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()

	return dc.cfg.Fee, nil
}

// Register registers an account with a new DEX. If an error occurs while
// fetching the DEX configuration or creating the fee transaction, it will be
// returned immediately.
// A thread will be started to wait for the requisite confirmations and send
// the fee notification to the server. Any error returned from that thread is
// sent as a notification.
func (c *Core) Register(form *RegisterForm) (*RegisterResult, error) {
	// Make sure the app has been initialized. This condition would error when
	// attempting to retrieve the encryption key below as well, but the
	// messaging may be confusing.
	if initialized, err := c.IsInitialized(); err != nil {
		return nil, fmt.Errorf("error checking if app is initialized: %w", err)
	} else if !initialized {
		return nil, fmt.Errorf("cannot register DEX because app has not been initialized")
	}

	// Check the app password.
	crypter, err := c.encryptionKey(form.AppPass)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	if form.Addr == "" {
		return nil, newError(emptyHostErr, "no dex address specified")
	}
	host, err := addrHost(form.Addr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}
	if c.isRegistered(host) {
		return nil, newError(dupeDEXErr, "already registered at %s", form.Addr)
	}

	regFeeAssetID, _ := dex.BipSymbolID(regFeeAssetSymbol)
	wallet, err := c.connectedWallet(regFeeAssetID)
	if err != nil {
		// Wrap the error from connectedWallet, a core.Error coded as
		// missingWalletErr or connectWalletErr.
		return nil, fmt.Errorf("cannot connect to %s wallet to pay fee: %w", regFeeAssetSymbol, err)
	}

	if !wallet.unlocked() {
		err = wallet.Unlock(crypter)
		if err != nil {
			return nil, newError(walletAuthErr, "failed to unlock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}

	cert, err := parseCert(host, form.Cert)
	if err != nil {
		return nil, newError(fileReadErr, "failed to read certificate file from %s: %v", cert, err)
	}

	dc, err := c.connectDEX(&db.AccountInfo{
		Host: host,
		Cert: cert,
	})
	if err != nil {
		return nil, codedError(connectionErr, err)
	}

	// close the connection to the dex server if the registration fails.
	var registrationComplete bool
	defer func() {
		if !registrationComplete {
			dc.connMaster.Disconnect()
		}
	}()

	dc.assetsMtx.RLock()
	regAsset, found := dc.assets[regFeeAssetID]
	dc.assetsMtx.RUnlock()
	if !found {
		return nil, newError(assetSupportErr, "dex server does not support %s asset", regFeeAssetSymbol)
	}

	privKey, err := dc.acct.setupEncryption(crypter)
	if err != nil {
		return nil, codedError(acctKeyErr, err)
	}

	// Prepare and sign the registration payload.
	// The account ID is generated from the public key.
	pkBytes := privKey.PubKey().SerializeCompressed()
	dexReg := &msgjson.Register{
		PubKey: pkBytes,
		Time:   encode.UnixMilliU(time.Now()),
	}
	regRes := new(msgjson.RegisterResult)
	err = dc.signAndRequest(dexReg, msgjson.RegisterRoute, regRes, DefaultResponseTimeout)
	if err != nil {
		return nil, codedError(registerErr, err)
	}

	// Check the DEX server's signature.
	msg := regRes.Serialize()
	dexPubKey, err := checkSigS256(msg, regRes.DEXPubKey, regRes.Sig)
	if err != nil {
		return nil, newError(signatureErr, "DEX signature validation error: %v", err)
	}

	dc.cfgMtx.RLock()
	fee := dc.cfg.Fee
	dc.cfgMtx.RUnlock()

	// Check that the fee is non-zero.
	if regRes.Fee == 0 {
		return nil, newError(zeroFeeErr, "zero registration fees not allowed")
	}
	if regRes.Fee != fee {
		return nil, newError(feeMismatchErr, "DEX 'register' result fee doesn't match the 'config' value. %d != %d", regRes.Fee, fee)
	}
	if regRes.Fee != form.Fee {
		return nil, newError(feeMismatchErr, "registration fee provided to Register does not match the DEX registration fee. %d != %d", form.Fee, regRes.Fee)
	}

	// Pay the registration fee.
	acctID := account.NewID(pkBytes)
	c.log.Infof("Attempting registration fee payment to %s, account ID %v, of %d units of %s. "+
		"Do NOT manually send funds to this address even if this fails.",
		regRes.Address, acctID, regRes.Fee, regAsset.Symbol)
	coin, err := wallet.PayFee(regRes.Address, regRes.Fee)
	if err != nil {
		return nil, newError(feeSendErr, "error paying registration fee: %v", err)
	}

	// Registration complete.
	registrationComplete = true
	c.connMtx.Lock()
	c.conns[host] = dc
	c.connMtx.Unlock()

	// Set the dexConnection account fields and save account info to db.
	dc.acct.dexPubKey = dexPubKey
	dc.acct.feeCoin = coin.ID()
	err = c.db.CreateAccount(dc.acct.dbInfo())
	if err != nil {
		c.log.Errorf("error saving account: %v", err)
		// Don't abandon registration. The fee is already paid.
	}

	c.updateAssetBalance(regFeeAssetID)

	dc.cfgMtx.RLock()
	requiredConfs := dc.cfg.RegFeeConfirms
	dc.cfgMtx.RUnlock()

	details := fmt.Sprintf("Waiting for %d confirmations before trading at %s", requiredConfs, dc.acct.host)
	c.notify(newFeePaymentNote(SubjectFeePaymentInProgress, details, db.Success, dc.acct.host))

	// Set up the coin waiter, which waits for the required number of
	// confirmations to notify the DEX and establish an authenticated
	// connection.
	c.verifyRegistrationFee(wallet.AssetID, dc, coin.ID(), 0)

	res := &RegisterResult{FeeID: coin.String(), ReqConfirms: requiredConfs}
	return res, nil
}

// verifyRegistrationFee waits the required amount of confirmations for the
// registration fee payment. Once the requirement is met the server is notified.
// If the server acknowledgment is successful, the account is set as 'paid' in
// the database. Notifications about confirmations increase, errors and success
// events are broadcasted to all subscribers.
func (c *Core) verifyRegistrationFee(assetID uint32, dc *dexConnection, coinID []byte, confs uint32) {
	dc.cfgMtx.RLock()
	reqConfs := uint32(dc.cfg.RegFeeConfirms)
	dc.cfgMtx.RUnlock()

	dc.setRegConfirms(confs)

	trigger := func() (bool, error) {
		// We already know the wallet is there by now.
		wallet, _ := c.wallet(assetID)
		confs, _, err := wallet.Confirmations(c.ctx, coinID)
		if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
			return false, fmt.Errorf("Error getting confirmations for %s: %w", coinIDString(wallet.AssetID, coinID), err)
		}
		details := fmt.Sprintf("Fee payment confirmations %v/%v", confs, reqConfs)

		if confs < reqConfs {
			dc.setRegConfirms(confs)
			c.notify(newFeePaymentNoteWithConfirmations(SubjectRegUpdate, details, db.Data, confs, dc.acct.host))
		}

		return confs >= reqConfs, nil
	}

	c.wait(coinID, assetID, trigger, func(err error) {
		wallet, _ := c.wallet(assetID)
		c.log.Debugf("Registration fee txn %s now has %d confirmations.", coinIDString(wallet.AssetID, coinID), reqConfs)
		defer func() {
			if err != nil {
				details := fmt.Sprintf("Error encountered while paying fees to %s: %v", dc.acct.host, err)
				c.notify(newFeePaymentNote(SubjectFeePaymentError, details, db.ErrorLevel, dc.acct.host))
			} else {
				details := fmt.Sprintf("You may now trade at %s", dc.acct.host)
				dc.setRegConfirms(regConfirmationsPaid)
				c.notify(newFeePaymentNote(SubjectAccountRegistered, details, db.Success, dc.acct.host))
			}
		}()
		if err != nil {
			return
		}
		c.log.Infof("Notifying dex %s of fee payment.", dc.acct.host)
		err = c.notifyFee(dc, coinID)
		if err != nil {
			return
		}
		dc.acct.markFeePaid()
		err = c.authDEX(dc)
		if err != nil {
			c.log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.host, err)
		}
	})

}

// IsInitialized checks if the app is already initialized.
func (c *Core) IsInitialized() (bool, error) {
	return c.db.ValueExists(keyParamsKey)
}

// InitializeClient sets the initial app-wide password for the client.
func (c *Core) InitializeClient(pw []byte) error {
	if initialized, err := c.IsInitialized(); err != nil {
		return fmt.Errorf("error checking if app is already initialized: %w", err)
	} else if initialized {
		return fmt.Errorf("already initialized, login instead")
	}

	if len(pw) == 0 {
		return fmt.Errorf("empty password not allowed")
	}

	crypter := c.newCrypter(pw)
	err := c.db.Store(keyParamsKey, crypter.Serialize())
	if err != nil {
		return fmt.Errorf("error storing key parameters: %w", err)
	}
	return nil
}

// Login logs the user in, decrypting the account keys for all known DEXes.
func (c *Core) Login(pw []byte) (*LoginResult, error) {
	// Make sure the app has been initialized. This condition would error when
	// attempting to retrieve the encryption key below as well, but the
	// messaging may be confusing.
	if initialized, err := c.IsInitialized(); err != nil {
		return nil, fmt.Errorf("error checking if app is initialized: %w", err)
	} else if !initialized {
		return nil, fmt.Errorf("cannot log in because app has not been initialized")
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, err
	}

	// Attempt to connect to and retrieve balance from all known wallets. It is
	// not an error if we can't connect, unless we need the wallet for active
	// trades, but that condition is checked later in resolveActiveTrades.
	// Ignore updateWalletBalance errors here too, to accommodate wallets that
	// must be unlocked to get the balance. We won't try to unlock here, but if
	// the wallet is needed for active trades, it will be unlocked in
	// resolveActiveTrades and the balance updated there.
	var wg sync.WaitGroup
	var connectCount uint32
	wallets := c.xcWallets()
	walletCount := len(wallets)
	for _, wallet := range wallets {
		wg.Add(1)
		go func(wallet *xcWallet) {
			defer wg.Done()
			if !wallet.connected() {
				err := c.connectAndUpdateWallet(wallet)
				if err != nil {
					c.log.Errorf("Unable to connect to %s wallet (start and sync wallets BEFORE starting dex!): %v",
						unbip(wallet.AssetID), err)
					return
				}
			}
			atomic.AddUint32(&connectCount, 1)
		}(wallet)
	}
	wg.Wait()
	if walletCount > 0 {
		c.log.Infof("Connected to %d of %d wallets.", connectCount, walletCount)
	}

	loaded := c.resolveActiveTrades(crypter)
	if loaded > 0 {
		c.log.Infof("loaded %d incomplete orders", loaded)
	}

	dexStats := c.initializeDEXConnections(crypter)
	notes, err := c.db.NotificationsN(100)
	if err != nil {
		c.log.Errorf("Login -> NotificationsN error: %v", err)
	}

	result := &LoginResult{
		Notifications: notes,
		DEXes:         dexStats,
	}
	return result, nil
}

// Logout logs the user out
func (c *Core) Logout() error {

	// Check active orders
	conns := c.dexConnections()
	for _, dc := range conns {
		if dc.hasActiveOrders() {
			return fmt.Errorf("cannot log out with active orders")
		}
	}

	// Lock wallets
	for _, w := range c.xcWallets() {
		if w.connected() {
			if err := w.Lock(); err != nil {
				return err
			}
		}
	}

	// With no open orders for any of the dex connections, and all wallets locked,
	// lock each dex account.
	for _, dc := range conns {
		dc.acct.lock()
	}

	return nil
}

// Orders fetches a batch of user orders, filtered with the provided
// OrderFilter.
func (c *Core) Orders(filter *OrderFilter) ([]*Order, error) {
	var oid order.OrderID
	if len(filter.Offset) > 0 {
		if len(filter.Offset) != order.OrderIDSize {
			return nil, fmt.Errorf("invalid offset order ID length. wanted %d, got %d", order.OrderIDSize, len(filter.Offset))
		}
		copy(oid[:], filter.Offset)
	}

	ords, err := c.db.Orders(&db.OrderFilter{
		N:        filter.N,
		Offset:   oid,
		Hosts:    filter.Hosts,
		Assets:   filter.Assets,
		Statuses: filter.Statuses,
	})
	if err != nil {
		return nil, fmt.Errorf("UserOrders error: %w", err)
	}

	cords := make([]*Order, 0, len(ords))
	for _, mOrd := range ords {
		corder, err := c.coreOrderFromMetaOrder(mOrd)
		if err != nil {
			return nil, err
		}
		cords = append(cords, corder)
	}

	return cords, nil
}

// coreOrderFromMetaOrder creates an *Order from a *db.MetaOrder, including
// loading matches from the database. The order is presumed to be inactive, so
// swap coin confirmations will not be set. For active orders, get the
// *trackedTrade and use the coreOrder method.
func (c *Core) coreOrderFromMetaOrder(mOrd *db.MetaOrder) (*Order, error) {
	corder := coreOrderFromTrade(mOrd.Order, mOrd.MetaData)
	oid := mOrd.Order.ID()
	matches, err := c.db.MatchesForOrder(oid)
	if err != nil {
		return nil, fmt.Errorf("MatchesForOrder error loading matches for %s: %w", oid, err)
	}
	corder.Matches = make([]*Match, 0, len(matches))
	for _, match := range matches {
		corder.Matches = append(corder.Matches, matchFromMetaMatch(mOrd.Order, match))
	}
	return corder, nil
}

// Order fetches a single user order.
func (c *Core) Order(oidB dex.Bytes) (*Order, error) {
	if len(oidB) != order.OrderIDSize {
		return nil, fmt.Errorf("wrong oid string length. wanted %d, got %d", order.OrderIDSize, len(oidB))
	}
	var oid order.OrderID
	copy(oid[:], oidB)
	// See if its an active order first.
	var tracker *trackedTrade
	for _, dc := range c.dexConnections() {
		tracker, _, _ = dc.findOrder(oid)
		if tracker != nil {
			break
		}
	}
	if tracker != nil {
		return tracker.coreOrder(), nil
	}
	// Must not be an active order. Get it from the database.
	mOrd, err := c.db.Order(oid)
	if err != nil {
		return nil, fmt.Errorf("error retrieving order %s: %w", oid, err)
	}
	return c.coreOrderFromMetaOrder(mOrd)
}

// marketWallets gets the 2 *dex.Assets and 2 *xcWallet associated with a
// market. The wallets will be connected, but not necessarily unlocked.
func (c *Core) marketWallets(host string, base, quote uint32) (ba, qa *dex.Asset, bw, qw *xcWallet, err error) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("Unknown host: %s", host)
	}

	ba, found = dc.assets[base]
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("%s not supported by %s", unbip(base), host)
	}
	qa, found = dc.assets[quote]
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("%s not supported by %s", unbip(quote), host)
	}

	bw, err = c.connectedWallet(base)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s wallet error: %v", unbip(base), err)
	}
	qw, err = c.connectedWallet(quote)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s wallet error: %v", unbip(quote), err)
	}
	return
}

// MaxBuy is the maximum-sized *OrderEstimate for a buy order on the specified
// market. An order rate must be provided, since the number of lots available
// for trading will vary based on the rate for a buy order (unlike a sell
// order).
func (c *Core) MaxBuy(host string, base, quote uint32, rate uint64) (*MaxOrderEstimate, error) {
	baseAsset, quoteAsset, baseWallet, quoteWallet, err := c.marketWallets(host, base, quote)
	if err != nil {
		return nil, err
	}

	quoteLotEst := calc.BaseToQuote(rate, baseAsset.LotSize)
	maxBuy, err := quoteWallet.MaxOrder(quoteLotEst, quoteAsset)
	if err != nil {
		return nil, fmt.Errorf("%s wallet MaxOrder error: %v", unbip(quote), err)
	}

	preRedeem, err := baseWallet.PreRedeem(&asset.PreRedeemForm{
		LotSize: baseAsset.LotSize,
		Lots:    maxBuy.Lots,
	})
	if err != nil {
		return nil, fmt.Errorf("%s PreRedeem error: %v", unbip(base), err)
	}

	return &MaxOrderEstimate{
		Swap:   maxBuy,
		Redeem: preRedeem.Estimate,
	}, nil
}

// MaxSell is the maximum-sized *OrderEstimate for a sell order on the specified
// market.
func (c *Core) MaxSell(host string, base, quote uint32) (*MaxOrderEstimate, error) {
	baseAsset, _, baseWallet, quoteWallet, err := c.marketWallets(host, base, quote)
	if err != nil {
		return nil, err
	}

	maxSell, err := baseWallet.MaxOrder(baseAsset.LotSize, baseAsset)
	if err != nil {
		return nil, fmt.Errorf("%s wallet MaxOrder error: %v", unbip(base), err)
	}

	dc, _, err := c.dex(host)
	if err != nil {
		return nil, err
	}

	// Estimate a quote-converted lot size.
	mktID := marketName(base, quote)
	book := dc.bookie(mktID)
	if book == nil {
		return nil, fmt.Errorf("no book synced for %s at %s", mktID, host)
	}
	midGap, err := book.MidGap()
	if err != nil {
		return nil, fmt.Errorf("error calculating market rate for %s at %s: %v", mktID, host, err)
	}
	lotSize := calc.BaseToQuote(midGap, baseAsset.LotSize)

	preRedeem, err := quoteWallet.PreRedeem(&asset.PreRedeemForm{
		LotSize: lotSize,
		Lots:    maxSell.Lots,
	})
	if err != nil {
		return nil, fmt.Errorf("%s PreRedeem error: %v", unbip(quote), err)
	}

	return &MaxOrderEstimate{
		Swap:   maxSell,
		Redeem: preRedeem.Estimate,
	}, nil
}

// initializeDEXConnections connects to the DEX servers in the conns map and
// authenticates the connection. If registration is incomplete, reFee is run and
// the connection will be authenticated once the `notifyfee` request is sent.
// If an account is not found on the dex server upon dex authentication the
// account is disabled and the corresponding entry in c.conns is removed
// which will result in the user being prompted to register again.
func (c *Core) initializeDEXConnections(crypter encrypt.Crypter) []*DEXBrief {
	// Connections will be attempted in parallel, so we'll need to protect the
	// errorSet.
	var wg sync.WaitGroup
	results := make([]*DEXBrief, 0, len(c.conns))
	var reconcileConnectionsWg sync.WaitGroup
	reconcileConnectionsWg.Add(1)
	disabledAccountHostChan := make(chan string)
	// If an account has been disabled the entry is removed from c.conns.
	go func() {
		defer reconcileConnectionsWg.Done()
		var disabledAccountHosts []string
		for disabledAccountHost := range disabledAccountHostChan {
			disabledAccountHosts = append(disabledAccountHosts, disabledAccountHost)
		}
		if len(disabledAccountHosts) > 0 {
			c.connMtx.Lock()
			for _, disabledAccountHost := range disabledAccountHosts {
				c.conns[disabledAccountHost].connMaster.Disconnect()
				delete(c.conns, disabledAccountHost)
				c.log.Warnf("Account at dex %v not found. The account has been disabled. "+
					"It is disconnected and has been removed from core connections.",
					disabledAccountHost)
			}
			c.connMtx.Unlock()
		}
	}()

	for _, dc := range c.dexConnections() {
		dc.tradeMtx.RLock()
		tradeIDs := make([]string, 0, len(dc.trades))
		for tradeID := range dc.trades {
			tradeIDs = append(tradeIDs, tradeID.String())
		}
		dc.tradeMtx.RUnlock()
		result := &DEXBrief{
			Host:     dc.acct.host,
			TradeIDs: tradeIDs,
			// AcctID might be set to a zeroed string here, but will be reset
			// below if we have to auth.
			AcctID: dc.acct.ID().String(),
		}
		results = append(results, result)
		// Unlock before checking auth and continuing, because if the user
		// logged out and didn't shut down, the account is still authed, but
		// locked, and needs unlocked.
		err := dc.acct.unlock(crypter)
		if err != nil {
			details := fmt.Sprintf("error unlocking account for %s: %v", dc.acct.host, err)
			c.notify(newFeePaymentNote(SubjectAccountUnlockError, details, db.ErrorLevel, dc.acct.host))
			result.AuthErr = details
			continue
		}
		if dc.acct.authed() {
			result.Authed = true
			result.AcctID = dc.acct.ID().String()
			continue // authDEX already done
		}
		result.AcctID = dc.acct.ID().String()
		dcrID, _ := dex.BipSymbolID("dcr")
		if !dc.acct.feePaid() {
			if len(dc.acct.feeCoin) == 0 {
				details := fmt.Sprintf("Empty fee coin for %s.", dc.acct.host)
				c.notify(newFeePaymentNote(SubjectFeeCoinError, details, db.ErrorLevel, dc.acct.host))
				result.AuthErr = details
				continue
			}
			// Try to unlock the Decred wallet, which should run the reFee cycle, and
			// in turn will run authDEX.
			dcrWallet, err := c.connectedWallet(dcrID)
			if err != nil {
				c.log.Debugf("Failed to connect for reFee at %s with error: %v", dc.acct.host, err)
				details := fmt.Sprintf("Incomplete registration detected for %s, but failed to connect to the Decred wallet", dc.acct.host)
				c.notify(newFeePaymentNote(SubjectWalletConnectionWarning, details, db.WarningLevel, dc.acct.host))
				result.AuthErr = details
				continue
			}
			if !dcrWallet.unlocked() {
				err = dcrWallet.Unlock(crypter)
				if err != nil {
					details := fmt.Sprintf("Connected to Decred wallet to complete registration at %s, but failed to unlock: %v", dc.acct.host, err)
					c.notify(newFeePaymentNote(SubjectWalletUnlockError, details, db.ErrorLevel, dc.acct.host))
					result.AuthErr = details
					continue
				}
			}
			c.reFee(dcrWallet, dc)
			continue
		}
		wg.Add(1)
		go func(dc *dexConnection) {
			defer wg.Done()
			err := c.authDEX(dc)
			if err != nil {
				details := fmt.Sprintf("%s: %v", dc.acct.host, err)
				c.notify(newDEXAuthNote(SubjectDexAuthError, dc.acct.host, false, details, db.ErrorLevel))
				result.AuthErr = details
				// Disable account on AccountNotFoundError.
				var mErr *msgjson.Error
				if errors.As(err, &mErr) &&
					mErr.Code == msgjson.AccountNotFoundError {
					acctInfo, err := c.db.Account(dc.acct.host)
					if err != nil {
						c.log.Errorf("Error retrieving account: %v", err)
						return
					}
					err = c.db.DisableAccount(acctInfo)
					if err != nil {
						c.log.Errorf("Error disabling account: %v", err)
						return
					}
					disabledAccountHostChan <- dc.acct.host
					return
				}
				return
			}
			result.Authed = true
		}(dc)
	}
	wg.Wait()
	close(disabledAccountHostChan)
	reconcileConnectionsWg.Wait()
	return results
}

// resolveActiveTrades loads order and match data from the database. Only active
// orders and orders with active matches are loaded. Also, only active matches
// are loaded, even if there are inactive matches for the same order, but it may
// be desirable to load all matches, so this behavior may change.
func (c *Core) resolveActiveTrades(crypter encrypt.Crypter) (loaded int) {
	failed := make(map[uint32]struct{})
	relocks := make(assetMap)
	for _, dc := range c.dexConnections() {
		// loadDBTrades can add to the failed map.
		ready, err := c.loadDBTrades(dc, crypter, failed)
		if err != nil {
			details := fmt.Sprintf("Some orders failed to load from the database: %v", err)
			c.notify(newOrderNote(SubjectOrderLoadFailure, details, db.ErrorLevel, nil))
			// Keep going since some trades may still have loaded.
		}
		if len(ready) > 0 {
			locks := c.resumeTrades(dc, ready) // fills out dc.trades
			relocks.merge(locks)
		}
		loaded += len(ready)
	}
	c.updateBalances(relocks)
	return loaded
}

func (c *Core) wait(coinID []byte, assetID uint32, trigger func() (bool, error), action func(error)) {
	c.waiterMtx.Lock()
	defer c.waiterMtx.Unlock()
	c.blockWaiters[coinIDString(assetID, coinID)] = &blockWaiter{
		assetID: assetID,
		trigger: trigger,
		action:  action,
	}
}

func (c *Core) notifyFee(dc *dexConnection, coinID []byte) error {
	if dc.acct.locked() {
		return fmt.Errorf("%s account locked. cannot notify fee. log in first", dc.acct.host)
	}
	// Notify the server of the fee coin once there are enough confirmations.
	req := &msgjson.NotifyFee{
		AccountID: dc.acct.id[:],
		CoinID:    coinID,
	}
	// Timestamp and sign the request.
	stampAndSign(dc.acct.privKey, req)
	msg, err := msgjson.NewRequest(dc.NextID(), msgjson.NotifyFeeRoute, req)
	if err != nil {
		return fmt.Errorf("failed to create notifyfee request: %w", err)
	}

	// Make the 'notifyfee' request and wait for the response. The server waits
	// an unspecified amount of time to discover the transaction, so time out
	// after this DEX's configured broadcast timeout plus a healthy buffer for
	// communication and server processing latencies. There is no reason to give
	// up too soon.
	timeout := time.Millisecond*time.Duration(dc.cfg.BroadcastTimeout) + 10*time.Second
	errChan := make(chan error, 1)
	err = dc.RequestWithTimeout(msg, func(resp *msgjson.Message) {
		ack := new(msgjson.Acknowledgement)
		// Do NOT capture err in this closure.
		if err := resp.UnmarshalResult(ack); err != nil {
			errChan <- fmt.Errorf("notify fee result error: %w", err)
			return
		}
		err := dc.acct.checkSig(req.Serialize(), ack.Sig)
		if err != nil {
			c.log.Warnf("Account was registered, but DEX signature could not be verified: %v", err)
		}
		errChan <- c.db.AccountPaid(&db.AccountProof{
			Host:  dc.acct.host,
			Stamp: req.Time,
			Sig:   ack.Sig,
		})
	}, timeout, func() {
		errChan <- fmt.Errorf("timed out waiting for '%s' response", msgjson.NotifyFeeRoute)
	})
	if err != nil {
		return fmt.Errorf("Sending the 'notifyfee' request failed: %w", err)
	}

	// The request was sent. Wait for a response or timeout.
	return <-errChan
}

// Withdraw initiates a withdraw from an exchange wallet. The client password
// must be provided as an additional verification.
func (c *Core) Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("Withdraw password error: %w", err)
	}
	if value == 0 {
		return nil, fmt.Errorf("%s zero withdraw", unbip(assetID))
	}
	wallet, found := c.wallet(assetID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(assetID))
	}
	err = c.connectAndUnlock(crypter, wallet)
	if err != nil {
		return nil, err
	}
	coin, err := wallet.Withdraw(address, value)
	if err != nil {
		details := fmt.Sprintf("Error encountered during %s withdraw: %v", unbip(assetID), err)
		c.notify(newWithdrawNote(SubjectWithdrawError, details, db.ErrorLevel))
		return nil, err
	}

	details := fmt.Sprintf("Withdraw of %s has completed successfully. Coin ID = %s", unbip(assetID), coin)
	c.notify(newWithdrawNote(SubjectWithdrawSend, details, db.Success))

	c.updateAssetBalance(assetID)
	return coin, nil
}

func (c *Core) PreOrder(form *TradeForm) (*OrderEstimate, error) {
	dc, err := c.connectedDEX(form.Host)
	if err != nil {
		return nil, err
	}

	wallets, err := c.walletSet(dc, form.Base, form.Quote, form.Sell)
	if err != nil {
		return nil, err
	}

	// So here's the thing. Our assets thus far don't require the wallet to be
	// unlocked to get order estimation (listunspent works on locked wallet),
	// but if we run into an asset that breaks that assumption, we may need
	// to require a password here before estimation.

	// We need the wallets to be connected.
	if !wallets.fromWallet.connected() {
		err := c.connectAndUpdateWallet(wallets.fromWallet)
		if err != nil {
			c.log.Errorf("Error connecting to %s wallet: %v", wallets.fromAsset.Symbol, err)
			return nil, fmt.Errorf("Error connecting to %s wallet", wallets.fromAsset.Symbol)
		}
	}

	if !wallets.toWallet.connected() {
		err := c.connectAndUpdateWallet(wallets.toWallet)
		if err != nil {
			c.log.Errorf("Error connecting to %s wallet: %v", wallets.toAsset.Symbol, err)
			return nil, fmt.Errorf("Error connecting to %s wallet", wallets.toAsset.Symbol)
		}
	}

	// Fund the order and prepare the coins.
	lotSize := wallets.baseAsset.LotSize
	lots := form.Qty / lotSize
	rate := form.Rate

	if !form.IsLimit {
		// If this is a market order, we'll predict the fill price.
		book := dc.bookie(marketName(form.Base, form.Quote))
		if book == nil {
			return nil, fmt.Errorf("Cannot estimate market order without a synced book")
		}

		var fills []*orderbook.Fill
		var filled bool
		if form.Sell {
			fills, filled = book.BestFill(form.Sell, form.Qty)
		} else {
			fills, filled = book.BestFillMarketBuy(form.Qty, lotSize)
		}

		if !filled {
			return nil, fmt.Errorf("Market is too thin to estimate market order")
		}

		// Get an average rate.
		var qtySum, product uint64
		for _, fill := range fills {
			product += fill.Quantity * fill.Rate
			qtySum += fill.Quantity
		}
		rate = product / qtySum
		if !form.Sell {
			lots = qtySum / lotSize
		}
	}

	fromLotSize, toLotSize := lotSize, calc.BaseToQuote(rate, lotSize)
	if !form.Sell {
		fromLotSize, toLotSize = toLotSize, fromLotSize
	}

	swapEstimate, err := wallets.fromWallet.PreSwap(&asset.PreSwapForm{
		LotSize:     fromLotSize,
		Lots:        lots,
		AssetConfig: wallets.fromAsset,
		Immediate:   (form.IsLimit && form.TifNow),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting swap estimate: %v", err)
	}

	redeemEstimate, err := wallets.toWallet.PreRedeem(&asset.PreRedeemForm{
		LotSize: toLotSize,
		Lots:    lots,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting redemption estimate: %v", err)
	}

	return &OrderEstimate{
		Swap:   swapEstimate,
		Redeem: redeemEstimate,
	}, nil
}

// Trade is used to place a market or limit order.
func (c *Core) Trade(pw []byte, form *TradeForm) (*Order, error) {
	// Check the user password.
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("Trade password error: %w", err)
	}

	dc, err := c.connectedDEX(form.Host)
	if err != nil {
		return nil, err
	}

	corder, fromID, err := c.prepareTrackedTrade(dc, form, crypter)
	if err != nil {
		return nil, err
	}

	c.updateAssetBalance(fromID)

	return corder, nil
}

// Send an order, process result, prepare and store the trackedTrade.
func (c *Core) prepareTrackedTrade(dc *dexConnection, form *TradeForm, crypter encrypt.Crypter) (*Order, uint32, error) {
	mktID := marketName(form.Base, form.Quote)
	if dc.marketConfig(mktID) == nil {
		return nil, 0, newError(marketErr, "order placed for unknown market %q", mktID)
	}

	// Proceed with the order if there is no trade suspension
	// scheduled for the market.
	if !dc.running(mktID) {
		return nil, 0, newError(marketErr, "%s market trading is suspended", mktID)
	}

	rate, qty := form.Rate, form.Qty
	if form.IsLimit && rate == 0 {
		return nil, 0, newError(orderParamsErr, "zero-rate order not allowed")
	}

	wallets, err := c.walletSet(dc, form.Base, form.Quote, form.Sell)
	if err != nil {
		return nil, 0, err
	}
	fromWallet, toWallet := wallets.fromWallet, wallets.toWallet

	prepareWallet := func(w *xcWallet) error {
		// NOTE: If the wallet is already internally unlocked (the decrypted
		// password cached in xcWallet.pw), this could be done without the
		// crypter via refreshUnlock.
		err := c.connectAndUnlock(crypter, w)
		if err != nil {
			return fmt.Errorf("%s connectAndUnlock error: %w",
				wallets.fromAsset.Symbol, err)
		}
		w.mtx.RLock()
		defer w.mtx.RUnlock()
		if !w.synced {
			return fmt.Errorf("%s still syncing. progress = %.2f", unbip(w.AssetID),
				w.syncProgress)
		}
		return nil
	}

	err = prepareWallet(fromWallet)
	if err != nil {
		return nil, 0, err
	}
	err = prepareWallet(toWallet)
	if err != nil {
		return nil, 0, err
	}

	// Get an address for the swap contract.
	addr, err := toWallet.Address()
	if err != nil {
		return nil, 0, codedError(walletErr, fmt.Errorf("%s Address error: %w", wallets.toAsset.Symbol, err))
	}

	// Fund the order and prepare the coins.
	fundQty := qty
	lots := qty / wallets.baseAsset.LotSize
	if form.IsLimit && !form.Sell {
		fundQty = calc.BaseToQuote(rate, fundQty)
	}

	isImmediate := (!form.IsLimit || form.TifNow)

	// Market buy order
	if !form.IsLimit && !form.Sell {
		// There is some ambiguity here about whether the specified quantity for
		// a market buy order should include projected fees, or whether fees
		// should be in addition to the quantity. If the fees should be
		// additional to the order quantity (the approach taken here), we should
		// try to estimate the number of lots based on the current market. If
		// the market is not synced, fall back to a single-lot estimate, with
		// the knowledge that such an estimate means that the specified amount
		// might not all be available for matching once fees are considered.
		lots = 1
		book := dc.bookie(mktID)
		if book != nil {
			midGap, err := book.MidGap()
			// An error is only returned when there are no orders on the book.
			// In that case, fall back to the 1 lot estimate for now.
			if err == nil {
				baseQty := calc.QuoteToBase(midGap, fundQty)
				lots = baseQty / wallets.baseAsset.LotSize
				if lots == 0 {
					err = newError(orderParamsErr,
						"order quantity is too low for current market rates. "+
							"qty = %d %s, mid-gap = %d, base-qty = %d %s, lot size = %d",
						qty, wallets.quoteAsset.Symbol, midGap, baseQty,
						wallets.baseAsset.Symbol, wallets.baseAsset.LotSize)
					return nil, 0, err
				}
			}
		}
	}

	if lots == 0 {
		return nil, 0, newError(orderParamsErr, "order quantity < 1 lot. qty = %d %s, rate = %d, lot size = %d",
			qty, wallets.baseAsset.Symbol, rate, wallets.baseAsset.LotSize)
	}

	coins, redeemScripts, err := fromWallet.FundOrder(&asset.Order{
		Value:        fundQty,
		MaxSwapCount: lots,
		DEXConfig:    wallets.fromAsset,
		Immediate:    isImmediate,
	})
	if err != nil {
		return nil, 0, codedError(walletErr, fmt.Errorf("FundOrder error for %s, funding quantity %d (%d lots): %w",
			wallets.fromAsset.Symbol, fundQty, lots, err))
	}
	coinIDs := make([]order.CoinID, 0, len(coins))
	for i := range coins {
		coinIDs = append(coinIDs, []byte(coins[i].ID()))
	}

	// The coins selected for this order will need to be unlocked
	// if the order does not get to the server successfully.
	unlockCoins := func() {
		err := fromWallet.ReturnCoins(coins)
		if err != nil {
			c.log.Warnf("Unable to return %s funding coins: %v", unbip(fromWallet.AssetID), err)
		}
	}

	// Construct the order.
	preImg := newPreimage()
	prefix := &order.Prefix{
		AccountID:  dc.acct.ID(),
		BaseAsset:  form.Base,
		QuoteAsset: form.Quote,
		OrderType:  order.MarketOrderType,
		ClientTime: time.Now(),
		Commit:     preImg.Commit(),
	}
	var ord order.Order
	if form.IsLimit {
		prefix.OrderType = order.LimitOrderType
		tif := order.StandingTiF
		if form.TifNow {
			tif = order.ImmediateTiF
		}
		ord = &order.LimitOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  addr,
			},
			Rate:  form.Rate,
			Force: tif,
		}
	} else {
		ord = &order.MarketOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  addr,
			},
		}
	}
	err = order.ValidateOrder(ord, order.OrderStatusEpoch, wallets.baseAsset.LotSize)
	if err != nil {
		unlockCoins()
		return nil, 0, fmt.Errorf("ValidateOrder error: %w", err)
	}

	msgCoins, err := messageCoins(wallets.fromWallet, coins, redeemScripts)
	if err != nil {
		unlockCoins()
		return nil, 0, fmt.Errorf("wallet %v failed to sign coins: %w", wallets.fromAsset.ID, err)
	}

	// Everything is ready. Send the order.
	route, msgOrder := messageOrder(ord, msgCoins)

	// Send and get the result.
	result := new(msgjson.OrderResult)
	err = dc.signAndRequest(msgOrder, route, result, fundingTxWait+DefaultResponseTimeout)
	if err != nil {
		unlockCoins()
		return nil, 0, fmt.Errorf("new order request with DEX server %v market %v failed: %w", dc.acct.host, mktID, err)
	}

	// If we encounter an error, perform some basic logging.
	//
	// TODO: Notify the client somehow.
	logAbandon := func(err interface{}) {
		c.log.Errorf("Abandoning order. "+
			"preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
	}

	err = validateOrderResponse(dc, result, ord, msgOrder)
	if err != nil {
		unlockCoins()
		c.log.Errorf("Abandoning order. preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
		return nil, 0, fmt.Errorf("validateOrderResponse error: %w", err)
	}

	// Store the order.
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status:     order.OrderStatusEpoch,
			Host:       dc.acct.host,
			MaxFeeRate: wallets.fromAsset.MaxFeeRate,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
		},
		Order: ord,
	}
	err = c.db.UpdateOrder(dbOrder)
	if err != nil {
		unlockCoins()
		logAbandon(fmt.Sprintf("failed to store order in database: %v", err))
		return nil, 0, fmt.Errorf("Order abandoned due to database error: %w", err)
	}

	// Prepare and store the tracker and get the core.Order to return.
	tracker := newTrackedTrade(dbOrder, preImg, dc, dc.marketEpochDuration(mktID), c.lockTimeTaker, c.lockTimeMaker,
		c.db, c.latencyQ, wallets, coins, c.notify)

	dc.tradeMtx.Lock()
	dc.trades[tracker.ID()] = tracker
	dc.tradeMtx.Unlock()

	// Now that the trades map is updated, sync with the preimage request.
	c.syncOrderPlaced(ord.ID())

	// Send a low-priority notification.
	corder := tracker.coreOrder()
	var details string
	if !form.IsLimit && !form.Sell {
		details = fmt.Sprintf("selling %.8f %s at market rate (%s)",
			float64(corder.Qty)/conversionFactor, unbip(form.Quote), tracker.token())
	} else {
		rateString := "market"
		if form.IsLimit {
			rateString = strconv.FormatFloat(float64(corder.Rate)/conversionFactor, 'f', 8, 64)
		}
		details = fmt.Sprintf("%sing %.8f %s, rate = %s (%s)",
			sellString(corder.Sell), float64(corder.Qty)/conversionFactor, unbip(form.Base), rateString, tracker.token())
	}
	c.notify(newOrderNote(SubjectOrderPlaced, details, db.Poke, corder))

	return corder, wallets.fromWallet.AssetID, nil
}

// walletSet is a pair of wallets with asset configurations identified in useful
// ways.
type walletSet struct {
	baseAsset  *dex.Asset
	quoteAsset *dex.Asset
	fromWallet *xcWallet
	fromAsset  *dex.Asset
	toWallet   *xcWallet
	toAsset    *dex.Asset
}

// walletSet constructs a walletSet.
func (c *Core) walletSet(dc *dexConnection, baseID, quoteID uint32, sell bool) (*walletSet, error) {
	dc.assetsMtx.RLock()
	baseAsset, baseFound := dc.assets[baseID]
	quoteAsset, quoteFound := dc.assets[quoteID]
	dc.assetsMtx.RUnlock()
	if !baseFound {
		return nil, fmt.Errorf("unknown base asset %d -> %s for %s", baseID, unbip(baseID), dc.acct.host)
	}
	if !quoteFound {
		return nil, fmt.Errorf("unknown quote asset %d -> %s for %s", quoteID, unbip(quoteID), dc.acct.host)
	}

	// Connect and open the wallets if needed.
	baseWallet, found := c.wallet(baseID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(baseID))
	}
	quoteWallet, found := c.wallet(quoteID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(quoteID))
	}

	// We actually care less about base/quote, and more about from/to, which
	// depends on whether this is a buy or sell order.
	fromAsset, toAsset := baseAsset, quoteAsset
	fromWallet, toWallet := baseWallet, quoteWallet
	if !sell {
		fromAsset, toAsset = quoteAsset, baseAsset
		fromWallet, toWallet = quoteWallet, baseWallet
	}

	return &walletSet{
		baseAsset:  baseAsset,
		quoteAsset: quoteAsset,
		fromWallet: fromWallet,
		fromAsset:  fromAsset,
		toWallet:   toWallet,
		toAsset:    toAsset,
	}, nil
}

// Cancel is used to send a cancel order which cancels a limit order.
func (c *Core) Cancel(pw []byte, oidB dex.Bytes) error {
	// Check the user password.
	_, err := c.encryptionKey(pw)
	if err != nil {
		return fmt.Errorf("Cancel password error: %w", err)
	}

	if len(oidB) != order.OrderIDSize {
		return fmt.Errorf("wrong order ID length. wanted %d, got %d", order.OrderIDSize, len(oidB))
	}
	var oid order.OrderID
	copy(oid[:], oidB)

	for _, dc := range c.dexConnections() {
		found, err := c.tryCancel(dc, oid)
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}

	return fmt.Errorf("Cancel: failed to find order %s", oidB)
}

// authDEX authenticates the connection for a DEX.
func (c *Core) authDEX(dc *dexConnection) error {
	// Prepare and sign the message for the 'connect' route.
	acctID := dc.acct.ID()
	payload := &msgjson.Connect{
		AccountID:  acctID[:],
		APIVersion: 0,
		Time:       encode.UnixMilliU(time.Now()),
	}
	sigMsg := payload.Serialize()
	sig, err := dc.acct.sign(sigMsg)
	if err != nil {
		return fmt.Errorf("signing error: %w", err)
	}
	payload.SetSig(sig)

	// Send the 'connect' request.
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.ConnectRoute, payload)
	if err != nil {
		return fmt.Errorf("error encoding 'connect' request: %w", err)
	}
	errChan := make(chan error, 1)
	result := new(msgjson.ConnectResult)
	err = dc.RequestWithTimeout(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	}, DefaultResponseTimeout, func() {
		errChan <- fmt.Errorf("timed out waiting for '%s' response", msgjson.ConnectRoute)
	})

	// Check the request error.
	if err != nil {
		return err
	}
	// Check the response error.
	err = <-errChan
	if err != nil {
		return fmt.Errorf("'connect' error: %w", err)
	}

	// Check the servers response signature.
	err = dc.acct.checkSig(sigMsg, result.Sig)
	if err != nil {
		return newError(signatureErr, "DEX signature validation error: %v", err)
	}

	// Set the account as authenticated.
	c.log.Debugf("Authenticated connection to %s, acct %v, %d active orders, %d active matches, score %d",
		dc.acct.host, acctID, len(result.ActiveOrderStatuses), len(result.ActiveMatches), result.Score)
	dc.acct.auth()

	// Associate the matches with known trades.
	matches, _, err := dc.parseMatches(result.ActiveMatches, false)
	if err != nil {
		c.log.Error(err)
	}

	exceptions, matchConflicts := dc.compareServerMatches(matches)
	updatedAssets := make(assetMap)
	for oid, matchAnomalies := range exceptions {
		trade := matchAnomalies.trade
		missing, extras := matchAnomalies.missing, matchAnomalies.extra

		trade.mtx.Lock()

		// Flag each of the missing matches as revoked.
		for _, match := range missing {
			c.log.Warnf("DEX %s did not report active match %s on order %s - assuming revoked.",
				dc.acct.host, match.MatchID, oid)
			// Must have been revoked while we were gone. Flag to allow recovery
			// and subsequent retirement of the match and parent trade.
			match.MetaData.Proof.SelfRevoked = true
			if err := c.db.UpdateMatch(&match.MetaMatch); err != nil {
				c.log.Errorf("Failed to update missing/revoked match: %v", err)
			}
		}

		// Send a "Missing matches" order note if there are missing match message.
		// Also, check if the now-Revoked matches were the last set of matches that
		// required sending swaps, and unlock coins if so.
		if len(missing) > 0 {
			if trade.maybeReturnCoins() {
				updatedAssets.count(trade.wallets.fromAsset.ID)
			}

			details := fmt.Sprintf("%d matches for order %s were not reported by %q and are considered revoked",
				len(missing), trade.token(), dc.acct.host)
			c.notify(newOrderNote(SubjectMissingMatches, details, db.ErrorLevel, trade.coreOrderInternal()))
		}

		// Start negotiation for extra matches for this trade.
		if len(extras) > 0 {
			err := trade.negotiate(extras)
			if err != nil {
				c.log.Errorf("Error negotiating one or more previously unknown matches for order %s reported by %s on connect: %v",
					oid, dc.acct.host, err)
				details := fmt.Sprintf("%d matches reported by %s were not found for %s.",
					len(extras), dc.acct.host, trade.token())
				c.notify(newOrderNote(SubjectMatchResolutionError, details, db.ErrorLevel, trade.coreOrderInternal()))
			} else {
				// For taker matches in MakerSwapCast, queue up match status
				// resolution to retrieve the maker's contract and coin.
				for _, extra := range extras {
					if order.MatchSide(extra.Side) == order.Taker && order.MatchStatus(extra.Status) == order.MakerSwapCast {
						var matchID order.MatchID
						copy(matchID[:], extra.MatchID)
						match, found := trade.matches[matchID]
						if !found {
							c.log.Errorf("Extra match %v was not registered by negotiate (db error?)", matchID)
							continue
						}
						c.log.Infof("Queueing match status resolution for newly discovered match %v (%s) "+
							"as taker to MakerSwapCast status.", matchID, match.Status) // had better be NewlyMatched!

						oid := trade.ID()
						conflicts := matchConflicts[oid]
						if conflicts == nil {
							conflicts = &matchStatusConflict{trade: trade}
							matchConflicts[oid] = conflicts
						}
						conflicts.matches = append(conflicts.matches, trade.matches[matchID])
					}
				}
			}
		}

		trade.mtx.Unlock()
	}

	// Compare the server-returned active orders with tracked trades, updating
	// the trade statuses where necessary. This is done after processing the
	// connect resp matches so that where possible, available match data can be
	// used to properly set order statuses and filled amount.
	unknownOrdersCount, reconciledOrdersCount := dc.reconcileTrades(result.ActiveOrderStatuses)
	if unknownOrdersCount > 0 {
		details := fmt.Sprintf("%d active orders reported by DEX %s were not found.",
			unknownOrdersCount, dc.acct.host)
		c.notify(newDEXAuthNote(SubjectUnknownOrders, dc.acct.host, false, details, db.Poke))
	}
	if reconciledOrdersCount > 0 {
		details := fmt.Sprintf("Statuses updated for %d orders.", reconciledOrdersCount)
		c.notify(newDEXAuthNote(SubjectOrdersReconciled, dc.acct.host, false, details, db.Poke))
	}

	if len(matchConflicts) > 0 {
		var n int
		for _, c := range matchConflicts {
			n += len(c.matches)
		}
		c.log.Warnf("Beginning match status resolution for %d matches...", n)
		c.resolveMatchConflicts(dc, matchConflicts)
	}

	// List and cancel standing limit orders that are in epoch or booked status,
	// but without funding coins for new matches. This should be done after the
	// order status resolution done above.
	var brokenTrades []*trackedTrade
	dc.tradeMtx.RLock()
	for _, trade := range dc.trades {
		if lo, ok := trade.Order.(*order.LimitOrder); !ok || lo.Force != order.StandingTiF {
			continue // only standing limit orders need to be canceled
		}
		trade.mtx.RLock()
		status := trade.metaData.Status
		if (status == order.OrderStatusEpoch || status == order.OrderStatusBooked) &&
			!trade.hasFundingCoins() {
			brokenTrades = append(brokenTrades, trade)
		}
		trade.mtx.RUnlock()
	}
	dc.tradeMtx.RUnlock()
	for _, trade := range brokenTrades {
		c.log.Warnf("Canceling unfunded standing limit order %v", trade.ID())
		if err = c.tryCancelTrade(dc, trade); err != nil {
			c.log.Warnf("Unable to cancel unfunded trade %v: %v", trade.ID(), err)
		}
	}

	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}

	return nil
}

// AssetBalance retrieves and updates the current wallet balance.
func (c *Core) AssetBalance(assetID uint32) (*WalletBalance, error) {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return nil, fmt.Errorf("%d -> %s wallet error: %w", assetID, unbip(assetID), err)
	}
	return c.updateWalletBalance(wallet)
}

// initialize pulls the known DEXes from the database and attempts to connect
// and retrieve the DEX configuration.
func (c *Core) initialize() {
	accts, err := c.db.Accounts()
	if err != nil {
		c.log.Errorf("Error retrieving accounts from database: %v", err) // panic?
	}
	var wg sync.WaitGroup
	for _, acct := range accts {
		wg.Add(1)
		go func(acct *db.AccountInfo) {
			defer wg.Done()
			if !c.verifyAccount(acct) {
				return
			}
		}(acct)
	}
	dbWallets, err := c.db.Wallets()
	if err != nil {
		c.log.Errorf("error loading wallets from database: %v", err)
	}
	c.walletMtx.Lock()
	for _, dbWallet := range dbWallets {
		wallet, err := c.loadWallet(dbWallet)
		aid := dbWallet.AssetID
		if err != nil {
			c.log.Errorf("error loading %d -> %s wallet: %v", aid, unbip(aid), err)
			continue
		}
		// Wallet is loaded from the DB, but not yet connected.
		c.log.Infof("Loaded %s wallet configuration.", unbip(aid))
		c.wallets[dbWallet.AssetID] = wallet
	}
	numWallets := len(c.wallets)
	c.walletMtx.Unlock()
	if len(dbWallets) > 0 {
		c.log.Infof("successfully loaded %d of %d wallets", numWallets, len(dbWallets))
	}
	// Wait for DEXes to be connected to ensure DEXes are ready
	// for authentication when Login is triggered.
	wg.Wait()
	c.log.Infof("Successfully connected to %d out of %d DEX servers", len(c.conns), len(accts))
	pluralize := func(n int) string {
		if n == 1 {
			return ""
		}
		return "s"
	}
	for _, dc := range c.dexConnections() {
		activeOrders, _ := c.dbOrders(dc) // non-nil error will load 0 orders, and any subsequent db error will cause a shutdown on dex auth or sooner
		if n := len(activeOrders); n > 0 {
			c.log.Warnf("\n\n\t ****  IMPORTANT: You have %d active order%s on %s. LOGIN immediately!  **** \n", n, pluralize(n), dc.acct.host)
		}
	}
}

// verifyAccount verifies AccountInfo by making a connection to the DEX.
func (c *Core) verifyAccount(acct *db.AccountInfo) bool {
	host, err := addrHost(acct.Host)
	if err != nil {
		c.log.Errorf("skipping loading of %s due to address parse error: %v", acct.Host, err)
		return false
	}
	dc, err := c.connectDEX(acct)
	if err != nil {
		c.log.Errorf("error connecting to DEX %s: %v", acct.Host, err)
		return false
	}
	c.log.Debugf("connectDEX for %s completed, checking account...", acct.Host)
	if !acct.Paid {
		if len(acct.FeeCoin) == 0 {
			// Register should have set this when creating the account
			// that was obtained via db.Accounts.
			c.log.Warnf("Incomplete registration without fee payment detected for DEX %s. "+
				"Discarding account.", acct.Host)
			return false
		}
	}
	c.connMtx.Lock()
	c.conns[host] = dc
	c.connMtx.Unlock()
	c.log.Debugf("dex connection to %s ready", acct.Host)
	return true
}

// feeLock is used to ensure that no more than one reFee check is running at a
// time.
var feeLock uint32

// checkUnpaidFees checks whether the registration fee info has an acceptable
// state, and tries to rectify any inconsistencies.
func (c *Core) checkUnpaidFees(dcrWallet *xcWallet) {
	if !atomic.CompareAndSwapUint32(&feeLock, 0, 1) {
		return
	}
	var wg sync.WaitGroup
	for _, dc := range c.dexConnections() {
		if dc.acct.feePaid() {
			continue
		}
		if len(dc.acct.feeCoin) == 0 {
			c.log.Errorf("empty fee coin found for unpaid account")
			continue
		}
		wg.Add(1)
		go func(dc *dexConnection) {
			c.reFee(dcrWallet, dc)
			wg.Done()
		}(dc)
	}
	wg.Wait()
	atomic.StoreUint32(&feeLock, 0)
}

// reFee attempts to finish the fee payment process for a DEX. reFee might be
// called if the client was shutdown after a fee was paid, but before it had the
// requisite confirmations for the 'notifyfee' message to be sent to the server.
func (c *Core) reFee(dcrWallet *xcWallet, dc *dexConnection) {

	// Return if the coin is already in blockWaiters.
	regFeeAssetID, _ := dex.BipSymbolID(regFeeAssetSymbol)
	if c.existsWaiter(coinIDString(regFeeAssetID, dc.acct.feeCoin)) {
		return
	}

	// Get the database account info.
	acctInfo, err := c.db.Account(dc.acct.host)
	if err != nil {
		c.log.Errorf("reFee %s - error retrieving account info: %v", dc.acct.host, err)
		return
	}
	// A couple sanity checks.
	if !bytes.Equal(acctInfo.FeeCoin, dc.acct.feeCoin) {
		c.log.Errorf("reFee %s - fee coin mismatch. %x != %x", dc.acct.host, acctInfo.FeeCoin, dc.acct.feeCoin)
		return
	}
	if acctInfo.Paid {
		c.log.Errorf("reFee %s - account for %x already marked paid", dc.acct.host, dc.acct.feeCoin)
		return
	}
	// Get the coin for the fee.
	confs, _, err := dcrWallet.Confirmations(c.ctx, acctInfo.FeeCoin)
	if err != nil {
		c.log.Errorf("reFee %s - error getting coin confirmations: %v", dc.acct.host, err)
		return
	}
	dc.cfgMtx.RLock()
	requiredConfs := uint32(dc.cfg.RegFeeConfirms)
	dc.cfgMtx.RUnlock()

	if confs >= requiredConfs {
		err := c.notifyFee(dc, acctInfo.FeeCoin)
		if err != nil {
			c.log.Errorf("reFee %s - notifyfee error: %v", dc.acct.host, err)
			details := fmt.Sprintf("Error encountered while paying fees to %s: %v", dc.acct.host, err)
			c.notify(newFeePaymentNote(SubjectFeePaymentError, details, db.ErrorLevel, dc.acct.host))
		} else {
			c.log.Infof("Fee paid at %s", dc.acct.host)
			details := fmt.Sprintf("You may now trade at %s.", dc.acct.host)
			c.notify(newFeePaymentNote(SubjectAccountRegistered, details, db.Success, dc.acct.host))
			// dc.acct.pay() and c.authDEX????
			dc.acct.markFeePaid()
			err = c.authDEX(dc)
			if err != nil {
				c.log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.host, err)
			}
		}
		return
	}
	c.verifyRegistrationFee(dcrWallet.AssetID, dc, acctInfo.FeeCoin, confs)
}

func (c *Core) dbOrders(dc *dexConnection) ([]*db.MetaOrder, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.db.ActiveDEXOrders(dc.acct.host)
	if err != nil {
		return nil, fmt.Errorf("database error when fetching orders for %s: %w", dc.acct.host, err)
	}
	c.log.Infof("Loaded %d active orders.", len(dbOrders))

	// It's possible for an order to not be active, but still have active matches.
	// Grab the orders for those too.
	haveOrder := func(oid order.OrderID) bool {
		for _, dbo := range dbOrders {
			if dbo.Order.ID() == oid {
				return true
			}
		}
		return false
	}

	activeMatchOrders, err := c.db.DEXOrdersWithActiveMatches(dc.acct.host)
	if err != nil {
		return nil, fmt.Errorf("database error fetching active match orders for %s: %w", dc.acct.host, err)
	}
	c.log.Infof("Loaded %d active match orders", len(activeMatchOrders))
	for _, oid := range activeMatchOrders {
		if haveOrder(oid) {
			continue
		}
		dbOrder, err := c.db.Order(oid)
		if err != nil {
			return nil, fmt.Errorf("database error fetching order %s for %s: %w", oid, dc.acct.host, err)
		}
		dbOrders = append(dbOrders, dbOrder)
	}

	return dbOrders, nil
}

// dbTrackers prepares trackedTrades based on active orders and matches in the
// database. Since dbTrackers may run before sign in when wallets are not
// connected or unlocked, wallets and coins are not added to the returned
// trackers. Use resumeTrades with the app Crypter to prepare wallets and coins.
func (c *Core) dbTrackers(dc *dexConnection) (map[order.OrderID]*trackedTrade, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.dbOrders(dc)
	if err != nil {
		return nil, err
	}

	// Index all of the cancel orders so we can account for them when loading
	// the trade orders. Whatever remains is orphaned.
	unknownCancels := make(map[order.OrderID]struct{})
	for _, dbOrder := range dbOrders {
		if dbOrder.Order.Type() == order.CancelOrderType {
			unknownCancels[dbOrder.Order.ID()] = struct{}{}
		}
	}

	trackers := make(map[order.OrderID]*trackedTrade, len(dbOrders))
	for _, dbOrder := range dbOrders {
		ord := dbOrder.Order
		oid := ord.ID()
		// Ignore cancel orders here. They'll be retrieved from LinkedOrder for
		// trade orders below.
		if ord.Type() == order.CancelOrderType {
			continue
		}

		mktID := marketName(ord.Base(), ord.Quote())
		if dc.marketConfig(mktID) == nil {
			c.log.Errorf("Active %s order retrieved for unknown market %s", oid, mktID)
			continue
		}

		var preImg order.Preimage
		copy(preImg[:], dbOrder.MetaData.Proof.Preimage)
		tracker := newTrackedTrade(dbOrder, preImg, dc, dc.marketEpochDuration(mktID), c.lockTimeTaker,
			c.lockTimeMaker, c.db, c.latencyQ, nil, nil, c.notify)
		trackers[dbOrder.Order.ID()] = tracker

		// Get matches.
		dbMatches, err := c.db.MatchesForOrder(oid)
		if err != nil {
			return nil, fmt.Errorf("error loading matches for order %s: %w", oid, err)
		}
		for _, dbMatch := range dbMatches {
			// Only trade matches are added to the matches map. Detect and skip
			// cancel order matches, which have an empty Address field.
			if dbMatch.Address == "" {
				// tracker.cancel is set from LinkedOrder with cancelTrade.
				continue
			}
			tracker.matches[dbMatch.MatchID] = &matchTracker{
				prefix:    tracker.Prefix(),
				trade:     tracker.Trade(),
				MetaMatch: *dbMatch,
				// Ensure logging on the first check of counterparty contract
				// confirms and own contract expiry.
				counterConfirms: -1,
				lastExpireDur:   365 * 24 * time.Hour,
			}
		}

		// Load any linked cancel order.
		cancelID := tracker.metaData.LinkedOrder
		if cancelID.IsZero() {
			continue
		}
		metaCancel, err := c.db.Order(cancelID)
		if err != nil {
			c.log.Errorf("cancel order %s not found for trade %s", cancelID, oid)
			continue
		}
		co, ok := metaCancel.Order.(*order.CancelOrder)
		if !ok {
			c.log.Errorf("linked order %s is not a cancel order", cancelID)
			continue
		}
		var pimg order.Preimage
		copy(pimg[:], metaCancel.MetaData.Proof.Preimage)
		err = tracker.cancelTrade(co, pimg) // set tracker.cancel and link
		if err != nil {
			c.log.Errorf("Error setting cancel order info %s: %v", co.ID(), err)
		}
		delete(unknownCancels, cancelID) // this one is known
		c.log.Debugf("Loaded cancel order %v for trade %v", cancelID, oid)
		// TODO: The trackedTrade.cancel.matches is not being repopulated on
		// startup. The consequences are that the Filled value will not include
		// the canceled portion, and the *CoreOrder generated by
		// coreOrderInternal will be Cancelling, but not Canceled. Instead of
		// using the matchTracker.matches msgjson.Match fields, we should be
		// storing the match data in the OrderMetaData so that it can be
		// tracked across sessions.
	}

	// Retire any remaining cancel orders that don't have active target orders.
	// This means we somehow already retired the trade, but not the cancel.
	for cid := range unknownCancels {
		c.log.Warnf("Retiring orphaned cancel order %v", cid)
		err = c.db.UpdateOrderStatus(cid, order.OrderStatusRevoked)
		if err != nil {
			c.log.Errorf("Failed to update status of orphaned cancel order %v: %v", cid, err)
		}
	}

	return trackers, nil
}

// loadDBTrades loads orders and matches from the database for the specified
// dexConnection. If there are active trades, the necessary wallets will be
// unlocked. To prevent spamming wallet connections, the 'failed' map will be
// populated with asset IDs for which the attempt to connect or unlock has
// failed. The failed map should be passed on subsequent calls for other dexes.
func (c *Core) loadDBTrades(dc *dexConnection, crypter encrypt.Crypter, failed map[uint32]struct{}) ([]*trackedTrade, error) {
	// Parse the active trades and see if any wallets need unlocking.
	trades, err := c.dbTrackers(dc)
	if err != nil {
		return nil, fmt.Errorf("error retreiving active matches: %w", err)
	}

	errs := newErrorSet(dc.acct.host + ": ")
	ready := make([]*trackedTrade, 0, len(trades))
	for _, trade := range trades {
		if !trade.isActive() {
			// In this event, there is a discrepancy between the active criteria
			// between dbTrackers and isActive that should be resolved.
			c.log.Warnf("Loaded inactive trade %v from the DB.", trade.ID())
			continue
		}
		base, quote := trade.Base(), trade.Quote()
		_, baseFailed := failed[base]
		_, quoteFailed := failed[quote]
		if !baseFailed {
			baseWallet, err := c.connectedWallet(base)
			if err != nil {
				baseFailed = true
				failed[base] = struct{}{}
				c.log.Errorf("Connecting to wallet %s failed: %v", unbip(base), err)
			} else if !baseWallet.unlocked() {
				err = baseWallet.Unlock(crypter)
				if err != nil {
					baseFailed = true
					failed[base] = struct{}{}
					c.log.Errorf("Unlock wallet %s failed: %v", unbip(base), err)
				}
			}
		}
		if !baseFailed && !quoteFailed {
			quoteWallet, err := c.connectedWallet(quote)
			if err != nil {
				quoteFailed = true
				failed[quote] = struct{}{}
				c.log.Errorf("Connecting to wallet %s failed: %v", unbip(quote), err)
			} else if !quoteWallet.unlocked() {
				err = quoteWallet.Unlock(crypter)
				if err != nil {
					quoteFailed = true
					failed[quote] = struct{}{}
					c.log.Errorf("Unlock wallet %s failed: %v", unbip(quote), err)
				}
			}
		}
		if baseFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(base))
			continue
		}
		if quoteFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(quote))
			continue
		}
		ready = append(ready, trade)
	}
	return ready, errs.ifAny()
}

// resumeTrades recovers the states of active trades and matches, including
// loading audit info needed to finish swaps and funding coins needed to create
// new matches on an order.
func (c *Core) resumeTrades(dc *dexConnection, trackers []*trackedTrade) assetMap {
	var tracker *trackedTrade
	notifyErr := func(subject, s string, a ...interface{}) {
		detail := fmt.Sprintf(s, a...)
		c.notify(newOrderNote(subject, detail, db.ErrorLevel, tracker.coreOrder()))
	}

	// markUnfunded is used to allow an unfunded order to enter the trades map
	// so that status resolution and match negotiation for unaffected matches
	// may continue. By not self-revoking, the user may have the opportunity to
	// resolve any wallet issues that may have lead to a failure to find the
	// funding coins. Otherwise the server will (or already did) revoke some or
	// all of the matches and the order itself.
	markUnfunded := func(trade *trackedTrade, matches []*matchTracker) {
		// Block negotiating new matches.
		trade.changeLocked = false
		trade.coinsLocked = false
		// Block swap txn attempts on matches needing funds.
		for _, match := range matches {
			match.swapErr = errors.New("no funding coins for swap")
		}
		// Will not be retired until revoke or cancel of the order and all
		// matches, which may happen on status resolution after authenticating
		// with the DEX server, or from a revoke_match/revoke_order notification
		// after timeout. However, the order should be unconditionally canceled.
	}

	relocks := make(assetMap)
	dc.tradeMtx.Lock()
	defer dc.tradeMtx.Unlock()
	for _, tracker = range trackers {
		// Never overwrite existing trackedTrades.
		if _, found := dc.trades[tracker.ID()]; found {
			c.log.Tracef("Trade %v already running.", tracker.ID())
			continue
		}

		// See if the order is 100% filled.
		trade := tracker.Trade()
		// Make sure we have the necessary wallets.
		wallets, err := c.walletSet(dc, tracker.Base(), tracker.Quote(), trade.Sell)
		if err != nil {
			notifyErr(SubjectWalletMissing, "Wallet retrieval error for active order %s: %v", tracker.token(), err)
			continue
		}

		tracker.wallets = wallets

		// If matches haven't redeemed, but the counter-swap has been received,
		// reload the audit info.
		isActive := tracker.metaData.Status == order.OrderStatusBooked || tracker.metaData.Status == order.OrderStatusEpoch
		var matchesNeedingCoins []*matchTracker
		for _, match := range tracker.matches {
			var needsAuditInfo bool
			var counterSwap []byte
			if match.Side == order.Maker {
				if match.Status < order.MakerSwapCast {
					matchesNeedingCoins = append(matchesNeedingCoins, match)
				}
				if match.Status == order.TakerSwapCast {
					needsAuditInfo = true // maker needs AuditInfo for takers contract
					counterSwap = match.MetaData.Proof.TakerSwap
				}
			} else { // Taker
				if match.Status < order.TakerSwapCast {
					matchesNeedingCoins = append(matchesNeedingCoins, match)
				}
				if match.Status < order.MatchComplete && match.Status >= order.MakerSwapCast {
					needsAuditInfo = true // taker needs AuditInfo for maker's contract
					counterSwap = match.MetaData.Proof.MakerSwap
				}
			}
			c.log.Tracef("Trade %v match %v needs coins = %v, needs audit info = %v",
				tracker.ID(), match.MatchID, len(matchesNeedingCoins) > 0, needsAuditInfo)
			if needsAuditInfo {
				// Check for unresolvable states.
				if len(counterSwap) == 0 {
					match.swapErr = fmt.Errorf("missing counter-swap, order %s, match %s", tracker.ID(), match.MatchID)
					notifyErr(SubjectMatchStatusError, "Match %s for order %s is in state %s, but has no maker swap coin.", match.Side, tracker.token(), match.Status)
					continue
				}
				counterContract := match.MetaData.Proof.CounterContract
				if len(counterContract) == 0 {
					match.swapErr = fmt.Errorf("missing counter-contract, order %s, match %s", tracker.ID(), match.MatchID)
					notifyErr(SubjectMatchStatusError, "Match %s for order %s is in state %s, but has no maker swap contract.", match.Side, tracker.token(), match.Status)
					continue
				}
				counterTxData := match.MetaData.Proof.CounterTxData

				// Obtaining AuditInfo will fail if it's unmined AND gone from
				// mempool, or the wallet is otherwise not ready. Note that this
				// does not actually audit the contract's value, recipient,
				// expiration, or secret hash (if maker), as that was already
				// done when it was initially stored as CounterScript.
				auditInfo, err := wallets.toWallet.AuditContract(counterSwap, counterContract, counterTxData)
				if err != nil {
					contractStr := coinIDString(wallets.toAsset.ID, counterSwap)
					c.log.Warnf("Starting search for counterparty contract %v (%s)", contractStr, unbip(wallets.toAsset.ID))
					// Start the audit retry waiter. Set swapErr to block tick
					// actions like counterSwap.Confirmations checks while it is
					// searching since matchTracker.counterSwap is not yet set.
					match.swapErr = fmt.Errorf("audit in progress, please wait") // don't frighten the users
					go func(tracker *trackedTrade, match *matchTracker) {
						auditInfo, err := tracker.searchAuditInfo(match, counterSwap, counterContract, counterTxData)
						tracker.mtx.Lock()
						defer tracker.mtx.Unlock()
						if err != nil {
							match.swapErr = fmt.Errorf("audit error: %w", err)
							c.log.Debugf("AuditContract error for match %v status %v, refunded = %v, revoked = %v: %v",
								match.MatchID, match.Status, len(match.MetaData.Proof.RefundCoin) > 0,
								match.MetaData.Proof.IsRevoked(), err)
							notifyErr(SubjectMatchRecoveryError, "Error auditing counter-party's swap contract (%s %v) during swap recovery on order %s: %v",
								unbip(wallets.toAsset.ID), contractStr, tracker.token(), err)
							// The match may become revoked by server.
							return
						}
						match.counterSwap = auditInfo
						match.swapErr = nil // unblock tick actions
						c.log.Infof("Successfully located and re-validated counterparty contract %v (%s)",
							contractStr, unbip(wallets.toAsset.ID))
					}(tracker, match)

					continue // leave auditInfo nil
				}
				match.counterSwap = auditInfo
				continue
			}
		}

		// Active orders and orders with matches with unsent swaps need funding
		// coin(s). If they are not found, block new matches and swap attempts.
		needsCoins := len(matchesNeedingCoins) > 0
		if isActive || needsCoins {
			coinIDs := trade.Coins
			if len(tracker.metaData.ChangeCoin) != 0 {
				coinIDs = []order.CoinID{tracker.metaData.ChangeCoin}
			}
			tracker.coins = map[string]asset.Coin{} // should already be
			if len(coinIDs) == 0 {
				notifyErr(SubjectOrderCoinError, "No funding coins recorded for active order %s", tracker.token())
				markUnfunded(tracker, matchesNeedingCoins) // bug - no user resolution
			} else {
				byteIDs := make([]dex.Bytes, 0, len(coinIDs))
				for _, cid := range coinIDs {
					byteIDs = append(byteIDs, []byte(cid))
				}
				coins, err := wallets.fromWallet.FundingCoins(byteIDs)
				if err != nil || len(coins) == 0 {
					notifyErr(SubjectOrderCoinError, "Source coins retrieval error for order %s (%s): %v",
						tracker.token(), unbip(wallets.fromAsset.ID), err)
					// Block matches needing funding coins.
					markUnfunded(tracker, matchesNeedingCoins)
					// Note: tracker is still added to trades map for (1) status
					// resolution, (2) continued settlement of matches that no
					// longer require funding coins, and (3) cancellation in
					// authDEX if the order is booked.
					c.log.Warnf("Check the status of your %s wallet and the coins logged above! "+
						"Resolve the wallet issue if possible and restart the DEX client.",
						strings.ToUpper(unbip(wallets.fromAsset.ID)))
					c.log.Warnf("Unfunded order %v will be canceled on connect, but %d active matches need funding coins!",
						tracker.ID(), len(matchesNeedingCoins))
					// If the funding coins are spent or inaccessible, the user
					// can only wait for match revocation.
				} else {
					// NOTE: change and changeLocked are not set even if the
					// funding coins were loaded from the DB's ChangeCoin.
					tracker.coinsLocked = true
					tracker.coins = mapifyCoins(coins)
				}
			}
		}

		// Balances should be updated for any orders with locked wallet coins,
		// or orders with funds locked in contracts.
		if isActive || needsCoins || tracker.unspentContractAmounts() > 0 {
			relocks.count(wallets.fromAsset.ID)
		}

		dc.trades[tracker.ID()] = tracker
		c.notify(newOrderNote(SubjectOrderLoaded, "", db.Data, tracker.coreOrder()))
	}
	return relocks
}

// generateDEXMaps creates the associated assets, market and epoch maps of the
// DEXs from the provided configuration.
func generateDEXMaps(host string, cfg *msgjson.ConfigResult) (map[uint32]*dex.Asset, map[string]uint64, error) {
	assets := make(map[uint32]*dex.Asset, len(cfg.Assets))
	for _, asset := range cfg.Assets {
		assets[asset.ID] = convertAssetInfo(asset)
	}
	// Validate the markets so we don't have to check every time later.
	for _, mkt := range cfg.Markets {
		_, ok := assets[mkt.Base]
		if !ok {
			return nil, nil, fmt.Errorf("%s reported a market with base "+
				"asset %d, but did not provide the asset info.", host, mkt.Base)
		}
		_, ok = assets[mkt.Quote]
		if !ok {
			return nil, nil, fmt.Errorf("%s reported a market with quote "+
				"asset %d, but did not provide the asset info.", host, mkt.Quote)
		}
	}

	epochMap := make(map[string]uint64)
	for _, mkt := range cfg.Markets {
		epochMap[mkt.Name] = 0
	}

	return assets, epochMap, nil
}

// runMatches runs the sorted matches returned from parseMatches.
func (c *Core) runMatches(tradeMatches map[order.OrderID]*serverMatches) (assetMap, error) {
	runMatch := func(sm *serverMatches) (assetMap, error) {
		updatedAssets := make(assetMap)
		tracker := sm.tracker
		oid := tracker.ID()

		// Verify and record any cancel Match targeting this trade.
		if sm.cancel != nil {
			err := tracker.processCancelMatch(sm.cancel)
			if err != nil {
				return updatedAssets, fmt.Errorf("processCancelMatch for cancel order %v targeting order %v failed: %w",
					sm.cancel.OrderID, oid, err)
			}
		}

		// Begin negotiation for any trade Matches.
		if len(sm.msgMatches) > 0 {
			tracker.mtx.Lock()
			err := tracker.negotiate(sm.msgMatches)
			tracker.mtx.Unlock()
			if err != nil {
				return updatedAssets, fmt.Errorf("negotiate order %v matches failed: %w", oid, err)
			}

			// Coins may be returned for canceled orders.
			tracker.mtx.RLock()
			if tracker.metaData.Status == order.OrderStatusCanceled {
				updatedAssets.count(tracker.fromAssetID)
			}
			tracker.mtx.RUnlock()

			// Try to tick the trade now, but do not interrupt on error. The
			// trade will tick again automatically.
			tickUpdatedAssets, err := c.tick(tracker)
			updatedAssets.merge(tickUpdatedAssets)
			if err != nil {
				return updatedAssets, fmt.Errorf("tick of order %v failed: %w", oid, err)
			}
		}

		return updatedAssets, nil
	}

	// Process the trades concurrently.
	type runMatchResult struct {
		updatedAssets assetMap
		err           error
	}
	resultChan := make(chan *runMatchResult)
	for _, trade := range tradeMatches {
		go func(trade *serverMatches) {
			assetsUpdated, err := runMatch(trade)
			resultChan <- &runMatchResult{assetsUpdated, err}
		}(trade)
	}

	errs := newErrorSet("runMatches - ")
	assetsUpdated := make(assetMap)
	for range tradeMatches {
		result := <-resultChan
		assetsUpdated.merge(result.updatedAssets) // assets might be updated even if an error occurs
		if result.err != nil {
			errs.addErr(result.err)
		}
	}

	return assetsUpdated, errs.ifAny()
}

// connectDEX establishes a ws connection to a DEX server using the provided
// account info, but does not authenticate the connection through the 'connect'
// route.
func (c *Core) connectDEX(acctInfo *db.AccountInfo) (*dexConnection, error) {
	// Get the host from the DEX URL.
	host, err := addrHost(acctInfo.Host)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}
	wsAddr := "wss://" + host + "/ws"
	wsURL, err := url.Parse(wsAddr)
	if err != nil {
		return nil, fmt.Errorf("error parsing ws address %s: %w", wsAddr, err)
	}

	wsCfg := comms.WsCfg{
		URL:      wsURL.String(),
		PingWait: 20 * time.Second, // larger than server's pingPeriod (server/comms/server.go)
		Cert:     acctInfo.Cert,
		ReconnectSync: func() {
			go c.handleReconnect(host)
		},
		ConnectEventFunc: func(connected bool) {
			go c.handleConnectEvent(host, connected)
		},
		Logger: c.log.SubLogger(wsURL.String()),
	}
	if c.cfg.TorProxy != "" {
		proxy := &socks.Proxy{
			Addr:         c.cfg.TorProxy,
			TorIsolation: c.cfg.TorIsolation,
		}
		wsCfg.NetDialContext = proxy.DialContext
	}

	// Create a websocket connection to the server.
	conn, err := c.wsConstructor(&wsCfg)
	if err != nil {
		return nil, err
	}

	connMaster := dex.NewConnectionMaster(conn)
	err = connMaster.Connect(c.ctx)
	// If the initial connection returned an error, shut it down to kill the
	// auto-reconnect cycle.
	if err != nil {
		connMaster.Disconnect()
		return nil, err
	}

	// Request the market configuration. Disconnect from the DEX server if the
	// configuration cannot be retrieved.
	dexCfg := new(msgjson.ConfigResult)
	err = sendRequest(conn, msgjson.ConfigRoute, nil, dexCfg, DefaultResponseTimeout)
	if err != nil {
		connMaster.Disconnect()
		return nil, fmt.Errorf("Error fetching DEX server config: %w", err)
	}

	assets, epochMap, err := generateDEXMaps(host, dexCfg)
	if err != nil {
		return nil, err
	}

	// Create the dexConnection and listen for incoming messages.
	bTimeout := time.Millisecond * time.Duration(dexCfg.BroadcastTimeout)
	dc := &dexConnection{
		WsConn:       conn,
		log:          c.log,
		connMaster:   connMaster,
		assets:       assets,
		cfg:          dexCfg,
		tickInterval: bTimeout / tickCheckDivisions,
		books:        make(map[string]*bookie),
		acct:         newDEXAccount(acctInfo),
		trades:       make(map[order.OrderID]*trackedTrade),
		notify:       c.notify,
		epoch:        epochMap,
		connected:    1,
	}

	c.log.Debugf("Broadcast timeout = %v, ticking every %v", bTimeout, dc.tickInterval)

	c.wg.Add(1)
	go c.listen(dc)
	c.log.Infof("Connected to DEX server at %s and listening for messages.", host)

	return dc, nil
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(host string) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		c.log.Errorf("handleReconnect: Unable to find previous connection to DEX at %s", host)
		return
	}

	// The server's configuration may have changed, so retrieve the current
	// server configuration.
	err := dc.refreshServerConfig()
	if err != nil {
		c.log.Errorf("handleReconnect: Unable to apply new configuration for DEX at %s: %v", host, err)
		return
	}

	err = c.authDEX(dc)
	if err != nil {
		c.log.Errorf("handleReconnect: Unable to authorize DEX at %s: %v", host, err)
		return
	}

	type market struct {
		name  string
		base  uint32
		quote uint32
	}

	resubMkt := func(mkt *market) {
		// Locate any bookie for this market.
		booky := dc.bookie(mkt.name)
		if booky == nil {
			// Was not previously subscribed with the server for this market.
			return
		}

		// Resubscribe since our old subscription was probably lost by the
		// server when the connection dropped.
		snap, err := dc.subscribe(mkt.base, mkt.quote)
		if err != nil {
			c.log.Errorf("handleReconnect: Failed to Subscribe to market %q 'orderbook': %v", mkt.name, err)
			return
		}

		// Create a fresh OrderBook for the bookie.
		err = booky.reset(snap)
		if err != nil {
			c.log.Errorf("handleReconnect: Failed to Sync market %q order book snapshot: %v", mkt.name, err)
		}

		// Send a FreshBookAction to the subscribers.
		booky.send(&BookUpdate{
			Action:   FreshBookAction,
			Host:     dc.acct.host,
			MarketID: mkt.name,
			Payload: &MarketOrderBook{
				Base:  mkt.base,
				Quote: mkt.quote,
				Book:  booky.book(),
			},
		})
	}

	// Create a list of books to check.
	dc.cfgMtx.RLock()
	mkts := make([]*market, 0, len(dc.cfg.Markets))
	for _, m := range dc.cfg.Markets {
		mkts = append(mkts, &market{
			name:  m.Name,
			base:  m.Base,
			quote: m.Quote,
		})
	}
	dc.cfgMtx.RUnlock()

	// For each market, resubscribe to any market books.
	for _, mkt := range mkts {
		resubMkt(mkt)
	}
}

// handleConnectEvent is called when a WsConn indicates that a connection was
// lost or established.
//
// NOTE: Disconnect event notifications may lag behind actual disconnections.
func (c *Core) handleConnectEvent(host string, connected bool) {
	c.connMtx.Lock()
	if dc, found := c.conns[host]; found {
		var v uint32
		if connected {
			v = 1
		}
		atomic.StoreUint32(&dc.connected, v)
	}
	c.connMtx.Unlock()
	statusStr := "connected"
	if !connected {
		statusStr = "disconnected"
	}
	details := fmt.Sprintf("DEX at %s has %s", host, statusStr)
	c.notify(newConnEventNote(fmt.Sprintf("DEX %s", statusStr), host, connected, details, db.Poke))
}

// handleMatchProofMsg is called when a match_proof notification is received.
func handleMatchProofMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.MatchProofNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("match proof note unmarshal error: %w", err)
	}

	// Expire the epoch
	dc.setEpoch(note.MarketID, note.Epoch+1)

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	return book.ValidateMatchProof(note)
}

// handleRevokeOrderMsg is called when a revoke_order message is received.
func handleRevokeOrderMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var revocation msgjson.RevokeOrder
	err := msg.Unmarshal(&revocation)
	if err != nil {
		return fmt.Errorf("revoke order unmarshal error: %w", err)
	}

	var oid order.OrderID
	copy(oid[:], revocation.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no order found with id %s", oid.String())
	}

	tracker.revoke()

	details := fmt.Sprintf("Order %s on market %s at %s has been revoked by the server", tracker.token(), tracker.mktID, dc.acct.host)
	c.notify(newOrderNote(SubjectOrderRevoked, details, db.ErrorLevel, tracker.coreOrder()))

	// Update market orders, and the balance to account for unlocked coins.
	c.updateAssetBalance(tracker.fromAssetID)
	return nil
}

// handleRevokeMatchMsg is called when a revoke_match message is received.
func handleRevokeMatchMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var revocation msgjson.RevokeMatch
	err := msg.Unmarshal(&revocation)
	if err != nil {
		return fmt.Errorf("revoke match unmarshal error: %w", err)
	}

	var oid order.OrderID
	copy(oid[:], revocation.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no order found with id %s (not an error if you've completed your side of the swap)", oid.String())
	}

	if len(revocation.MatchID) != order.MatchIDSize {
		return fmt.Errorf("invalid match ID %v", revocation.MatchID)
	}

	var matchID order.MatchID
	copy(matchID[:], revocation.MatchID)

	tracker.mtx.Lock()
	err = tracker.revokeMatch(matchID, true)
	tracker.mtx.Unlock()
	if err != nil {
		return fmt.Errorf("unable to revoke match %s for order %s: %w", matchID, tracker.ID(), err)
	}

	// Update market orders, and the balance to account for unlocked coins.
	c.updateAssetBalance(tracker.fromAssetID)
	return nil
}

// handleNotifyMsg is called when a notify notification is received.
func handleNotifyMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var txt string
	err := msg.Unmarshal(&txt)
	if err != nil {
		return fmt.Errorf("notify unmarshal error: %w", err)
	}
	txt = fmt.Sprintf("Message from DEX at %s:\n\n\"%s\"\n", dc.acct.host, txt)
	c.notify(newServerNotifyNote(dc.acct.host, txt, db.WarningLevel))
	return nil
}

// handlePenaltyMsg is called when a Penalty notification is received.
//
// TODO: Consider other steps needed to take immediately after being banned.
func handlePenaltyMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.PenaltyNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("penalty note unmarshal error: %w", err)
	}
	// Check the signature.
	err = dc.acct.checkSig(note.Serialize(), note.Sig)
	if err != nil {
		return newError(signatureErr, "handlePenaltyMsg: DEX signature validation error: %v", err)
	}
	t := encode.UnixTimeMilli(int64(note.Penalty.Time))
	// d := time.Duration(note.Penalty.Duration) * time.Millisecond
	details := fmt.Sprintf("Penalty from DEX at %s\nlast broken rule: %s\ntime: %v\ndetails:\n\"%s\"\n",
		dc.acct.host, note.Penalty.Rule, t, note.Penalty.Details)
	n := db.NewNotification("penalty", dc.acct.host, details, db.WarningLevel)
	c.notify(&n)
	return nil
}

// routeHandler is a handler for a message from the DEX.
type routeHandler func(*Core, *dexConnection, *msgjson.Message) error

var reqHandlers = map[string]routeHandler{
	msgjson.PreimageRoute:   handlePreimageRequest,
	msgjson.MatchRoute:      handleMatchRoute,
	msgjson.AuditRoute:      handleAuditRoute,
	msgjson.RedemptionRoute: handleRedemptionRoute, // TODO: to ntfn
}

var noteHandlers = map[string]routeHandler{
	msgjson.MatchProofRoute:      handleMatchProofMsg,
	msgjson.BookOrderRoute:       handleBookOrderMsg,
	msgjson.EpochOrderRoute:      handleEpochOrderMsg,
	msgjson.UnbookOrderRoute:     handleUnbookOrderMsg,
	msgjson.UpdateRemainingRoute: handleUpdateRemainingMsg,
	msgjson.EpochReportRoute:     handleEpochReportMsg,
	msgjson.SuspensionRoute:      handleTradeSuspensionMsg,
	msgjson.ResumptionRoute:      handleTradeResumptionMsg,
	msgjson.NotifyRoute:          handleNotifyMsg,
	msgjson.PenaltyRoute:         handlePenaltyMsg,
	msgjson.NoMatchRoute:         handleNoMatchRoute,
	msgjson.RevokeOrderRoute:     handleRevokeOrderMsg,
	msgjson.RevokeMatchRoute:     handleRevokeMatchMsg,
}

// listen monitors the DEX websocket connection for server requests and
// notifications.
func (c *Core) listen(dc *dexConnection) {
	defer c.wg.Done()
	msgs := dc.MessageSource()
	// Run a match check at the tick interval. NOTE: It's possible for the
	// broadcast timeout to change when the DEX config is updated.
	ticker := time.NewTicker(dc.tickInterval)
	defer ticker.Stop()
	lastTick := time.Now()

	// Messages must be run in the order in which they are received, but they
	// should not be blocking or run concurrently.
	type msgJob struct {
		hander routeHandler
		msg    *msgjson.Message
	}
	// Start a single runner goroutine to run jobs one at a time in the order
	// that they were received. Include the handler goroutine in the WaitGroup
	// to allow it to complete if the connection master desires.
	nextJob := make(chan *msgJob, 1024) // start blocking at this cap
	defer close(nextJob)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for job := range nextJob {
			if err := job.hander(c, dc, job.msg); err != nil {
				c.log.Errorf("Route '%v' %v handler error (DEX %s): %v", job.msg.Route,
					job.msg.Type, dc.acct.host, err)
			}
		}
	}()

	checkTrades := func() {
		var allTrades, doneTrades, activeTrades []*trackedTrade
		// NOTE: Don't lock tradeMtx while also locking a trackedTrade's mtx
		// since we risk blocking access to the trades map if there is lock
		// contention for even one trade.
		dc.tradeMtx.RLock()
		for _, trade := range dc.trades {
			allTrades = append(allTrades, trade)
		}
		dc.tradeMtx.RUnlock()

		for _, trade := range allTrades {
			if trade.isActive() {
				activeTrades = append(activeTrades, trade)
				continue
			}
			doneTrades = append(doneTrades, trade)
		}

		if len(doneTrades) > 0 {
			dc.tradeMtx.Lock()
			for _, trade := range doneTrades {
				c.notify(newOrderNote(SubjectOrderRetired, "", db.Data, trade.coreOrder()))
				delete(dc.trades, trade.ID())
			}
			dc.tradeMtx.Unlock()
		}

		// Unlock funding coins for retired orders for good measure, in case
		// there were not unlocked at an earlier time.
		updatedAssets := make(assetMap)
		for _, trade := range doneTrades {
			trade.mtx.Lock()
			c.log.Infof("Retiring inactive order %v in status %v", trade.ID(), trade.metaData.Status)
			trade.returnCoins()
			trade.mtx.Unlock()
			updatedAssets.count(trade.wallets.fromAsset.ID)
		}

		for _, trade := range activeTrades {
			newUpdates, err := c.tick(trade)
			if err != nil {
				c.log.Error(err)
			}
			updatedAssets.merge(newUpdates)
		}

		if len(updatedAssets) > 0 {
			c.updateBalances(updatedAssets)
		}
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-ticker.C:
				sinceLast := time.Since(lastTick)
				lastTick = time.Now()
				if sinceLast >= 2*dc.tickInterval {
					// The app likely just woke up from being suspended. Skip this
					// tick to let DEX connections reconnect and resync matches.
					c.log.Warnf("Long delay since previous trade check (just resumed?): %v. "+
						"Skipping this check to allow reconnect.", sinceLast)
					continue
				}

				checkTrades()

			case <-c.ctx.Done():
				return
			}
		}
	}()

out:
	for {
		select {
		case msg, ok := <-msgs:

			if !ok {
				c.log.Debugf("Connection closed for %s.", dc.acct.host)
				// TODO: This just means that wsConn, which created the
				// MessageSource channel, was shut down before this loop
				// returned via ctx.Done. It may be necessary to investigate the
				// most appropriate normal shutdown sequence (i.e. close all
				// connections before stopping Core).
				return
			}

			var handler routeHandler
			var found bool
			switch msg.Type {
			case msgjson.Request:
				handler, found = reqHandlers[msg.Route]
			case msgjson.Notification:
				handler, found = noteHandlers[msg.Route]
			case msgjson.Response:
				// client/comms.wsConn handles responses to requests we sent.
				c.log.Errorf("A response was received in the message queue: %s", msg)
				continue
			default:
				c.log.Errorf("Invalid message type %d from MessageSource", msg.Type)
				continue
			}
			// Until all the routes have handlers, check for nil too.
			if !found || handler == nil {
				c.log.Errorf("No handler found for route '%s'", msg.Route)
				continue
			}

			// Queue the handling of this message.
			nextJob <- &msgJob{handler, msg}

		case <-c.ctx.Done():
			break out
		}
	}
}

// handlePreimageRequest handles a DEX-originating request for an order
// preimage.
func handlePreimageRequest(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	req := new(msgjson.PreimageRequest)
	err := msg.Unmarshal(req)
	if err != nil {
		return fmt.Errorf("preimage request parsing error: %w", err)
	}

	var oid order.OrderID
	copy(oid[:], req.OrderID)

	// Sync with order placement.
	c.piSyncMtx.Lock()
	syncChan, found := c.piSyncers[oid]
	if found {
		// If we found the channel, the tracker is already in the map, and we
		// can go ahead.
		delete(c.piSyncers, oid)
	} else {
		// If we didn't find the channel, Trade is still running. Add the chan
		// and wait for Trade to close it.
		syncChan = make(chan struct{})
		c.piSyncers[oid] = syncChan
	}
	c.piSyncMtx.Unlock()

	deletePiSyncer := func() {
		c.piSyncMtx.Lock()
		delete(c.piSyncers, oid)
		c.piSyncMtx.Unlock()
	}

	if !found {
		select {
		case <-syncChan:
		case <-time.After(time.Minute):
			deletePiSyncer()
			return fmt.Errorf("timed out syncing preimage request for %s, order %s", dc.acct.host, oid)
		case <-c.ctx.Done():
			deletePiSyncer()
			return nil
		}
	}

	tracker, preImg, isCancel := dc.findOrder(oid)
	if tracker == nil {
		// Hack around the race in prepareTrackedTrade from submitting the
		// order, receiving the order result, and registering the tracked trade.
		// It's possible that the dc.trades map won't have the oid yet after
		// submitting the order, so if (1) we didn't find this oid, AND (2)
		// we're in that window, that order result is about to be stored, so
		// wait for it and check again.
		tracker, preImg, isCancel = dc.findOrder(oid)
		if tracker == nil {
			return fmt.Errorf("no active order found for preimage request for %s", oid)
		}
	}
	resp, err := msgjson.NewResponse(msg.ID, &msgjson.PreimageResponse{
		Preimage: preImg[:],
	}, nil)
	if err != nil {
		return fmt.Errorf("preimage response encoding error: %w", err)
	}
	err = dc.Send(resp)
	if err != nil {
		return fmt.Errorf("preimage send error: %w", err)
	}
	subject := SubjectPreimageSent
	if isCancel {
		subject = SubjectCancelPreimageSent
	}
	c.notify(newOrderNote(subject, "", db.Data, tracker.coreOrder()))
	return nil
}

// handleMatchRoute processes the DEX-originating match route request,
// indicating that a match has been made and needs to be negotiated.
func handleMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	msgMatches := make([]*msgjson.Match, 0)
	err := msg.Unmarshal(&msgMatches)
	if err != nil {
		return fmt.Errorf("match request parsing error: %w", err)
	}

	// TODO: If the dexConnection.acct is locked, prompt the user to login.
	// Maybe even spin here before failing with no hope of retrying the match
	// request handling.

	// Acknowledgements MUST be in the same orders as the msgjson.Matches.
	matches, acks, err := dc.parseMatches(msgMatches, true)
	if err != nil {
		// Even one failed match fails them all since the server requires acks
		// for them all, and in the same order. TODO: consider lifting this
		// requirement, which requires changes to the server's handling.
		return err
	}

	// Warn about new matches for unfunded orders. We still must ack all the
	// matches in the 'match' request for the server to accept it, although the
	// server doesn't require match acks. See (*Swapper).processMatchAcks.
	for oid, srvMatch := range matches {
		if !srvMatch.tracker.hasFundingCoins() {
			c.log.Warnf("Received new match for unfunded order %v!", oid)
			// In runMatches>tracker.negotiate we generate the matchTracker and
			// set swapErr after updating order status and filled amount, and
			// storing the match to the DB. It may still be possible for the
			// user to recover if the issue is just that the wrong wallet is
			// connected by fixing wallet config and restarting. p.s. Hopefully
			// we are maker.
		}
	}

	resp, err := msgjson.NewResponse(msg.ID, acks, nil)
	if err != nil {
		return err
	}

	// Send the match acknowledgments.
	err = dc.Send(resp)
	if err != nil {
		// Do not bail on the matches on error, just log it.
		c.log.Errorf("Send match response: %v", err)
	}

	// Begin match negotiation.
	updatedAssets, err := c.runMatches(matches)
	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}

	return err
}

// handleNoMatchRoute handles the DEX-originating nomatch request, which is sent
// when an order does not match during the epoch match cycle.
func handleNoMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	nomatchMsg := new(msgjson.NoMatch)
	err := msg.Unmarshal(nomatchMsg)
	if err != nil {
		return fmt.Errorf("nomatch request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], nomatchMsg.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return newError(unknownOrderErr, "nomatch request received for unknown order %v from %s", oid, dc.acct.host)
	}
	updatedAssets, err := tracker.nomatch(oid)
	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}
	return err
}

func (c *Core) schedTradeTick(tracker *trackedTrade) {
	oid := tracker.ID()
	c.tickSchedMtx.Lock()
	defer c.tickSchedMtx.Unlock()
	if _, found := c.tickSched[oid]; found {
		return // already going to tick this trade
	}

	tick := func() {
		assets, err := c.tick(tracker)
		if len(assets) > 0 {
			c.updateBalances(assets)
		}
		if err != nil {
			c.log.Errorf("tick error for order %v: %v", oid, err)
		}
	}

	numMatches := len(tracker.activeMatches())
	if numMatches == 1 {
		go tick()
		return
	}

	// Schedule a tick for this trade.
	delay := 2*time.Second + time.Duration(numMatches)*time.Second/10 // 1 sec extra delay for every 10 active matches
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	c.log.Debugf("Waiting %v to tick trade %v with %d active matches", delay, oid, numMatches)
	c.tickSched[oid] = time.AfterFunc(delay, func() {
		c.tickSchedMtx.Lock()
		defer c.tickSchedMtx.Unlock()
		defer delete(c.tickSched, oid)
		tick()
	})
}

// handleAuditRoute handles the DEX-originating audit request, which is sent
// when a match counter-party reports their initiation transaction.
func handleAuditRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	audit := new(msgjson.Audit)
	err := msg.Unmarshal(audit)
	if err != nil {
		return fmt.Errorf("audit request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], audit.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("audit request received for unknown order: %s", string(msg.Payload))
	}
	return tracker.processAuditMsg(msg.ID, audit)
}

// handleRedemptionRoute handles the DEX-originating redemption request, which
// is sent when a match counter-party reports their redemption transaction.
func handleRedemptionRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	redemption := new(msgjson.Redemption)
	err := msg.Unmarshal(redemption)
	if err != nil {
		return fmt.Errorf("redemption request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], redemption.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("redemption request received for unknown order: %s", string(msg.Payload))
	}
	err = tracker.processRedemption(msg.ID, redemption)
	if err != nil {
		return err
	}
	c.schedTradeTick(tracker)
	return nil
}

// existsWaiter returns true if the waiter already exists in the map.
func (c *Core) existsWaiter(id string) bool {
	c.waiterMtx.RLock()
	_, exists := c.blockWaiters[id]
	c.waiterMtx.RUnlock()
	return exists
}

// removeWaiter removes a blockWaiter from the map.
func (c *Core) removeWaiter(id string) {
	c.waiterMtx.Lock()
	delete(c.blockWaiters, id)
	c.waiterMtx.Unlock()
}

// tipChange is called by a wallet backend when the tip block changes, or when
// a connection error is encountered such that tip change reporting may be
// adversely affected.
func (c *Core) tipChange(assetID uint32, nodeErr error) {
	if nodeErr != nil {
		c.log.Errorf("%s wallet is reporting a failed state: %v", unbip(assetID), nodeErr)
		return
	}
	c.log.Tracef("processing tip change for %s", unbip(assetID))
	c.waiterMtx.Lock()
	for id, waiter := range c.blockWaiters {
		if waiter.assetID != assetID {
			continue
		}
		go func(id string, waiter *blockWaiter) {
			ok, err := waiter.trigger()
			if err != nil {
				waiter.action(err)
				c.removeWaiter(id)
			}
			if ok {
				waiter.action(nil)
				c.removeWaiter(id)
			}
		}(id, waiter)
	}
	c.waiterMtx.Unlock()

	assets := make(assetMap)
	for _, dc := range c.dexConnections() {
		newUpdates := c.tickAsset(dc, assetID)
		if len(newUpdates) > 0 {
			assets.merge(newUpdates)
		}
	}

	// Ensure we always at least update this asset's balance regardless of trade
	// status changes.
	assets.count(assetID)
	c.updateBalances(assets)
}

// PromptShutdown checks if there are active orders and asks confirmation to
// shutdown if there are. The return value indicates if it is safe to stop Core
// or if the user has confirmed they want to shutdown with active orders.
func (c *Core) PromptShutdown() bool {
	conns := c.dexConnections()
	var haveActiveOrders bool
	for _, dc := range conns {
		if dc.hasActiveOrders() {
			haveActiveOrders = true
			break
		}
	}

	if !haveActiveOrders {
		return true
	}

	fmt.Print("You have active orders. Shutting down now may result in failed swaps and account penalization.\n" +
		"Do you want to quit anyway? ('yes' to quit, or enter to abort shutdown): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan() // waiting for user input
	if err := scanner.Err(); err != nil {
		fmt.Printf("Input error: %v", err)
		return false
	}

	switch resp := strings.ToLower(scanner.Text()); resp {
	case "y", "yes":
		return true
	case "n", "no", "":
	default: // anything else aborts, but warn about it
		fmt.Printf("Unrecognized response %q. ", resp)
	}
	fmt.Println("Shutdown aborted.")
	return false
}

// convertAssetInfo converts from a *msgjson.Asset to the nearly identical
// *dex.Asset.
func convertAssetInfo(asset *msgjson.Asset) *dex.Asset {
	return &dex.Asset{
		ID:           asset.ID,
		Symbol:       asset.Symbol,
		LotSize:      asset.LotSize,
		RateStep:     asset.RateStep,
		MaxFeeRate:   asset.MaxFeeRate,
		SwapSize:     asset.SwapSize,
		SwapSizeBase: asset.SwapSizeBase,
		SwapConf:     uint32(asset.SwapConf),
	}
}

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg, pkBytes, sigBytes []byte) (*secp256k1.PublicKey, error) {
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding secp256k1 PublicKey from bytes: %w", err)
	}
	signature, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	if !signature.Verify(msg, pubKey) {
		return nil, fmt.Errorf("secp256k1 signature verification failed")
	}
	return pubKey, nil
}

// sign signs the msgjson.Signable with the provided private key.
func sign(privKey *secp256k1.PrivateKey, payload msgjson.Signable) {
	sigMsg := payload.Serialize()
	sig := ecdsa.Sign(privKey, sigMsg) // should we be signing the *hash* of the payload?
	payload.SetSig(sig.Serialize())
}

// stampAndSign time stamps the msgjson.Stampable, and signs it with the given
// private key.
func stampAndSign(privKey *secp256k1.PrivateKey, payload msgjson.Stampable) {
	payload.Stamp(encode.UnixMilliU(time.Now()))
	sign(privKey, payload)
}

// sendRequest sends a request via the specified ws connection and unmarshals
// the response into the provided interface.
// TODO: Modify to accept a context.Context argument so callers can pass core's
// context to break out of the reply wait when Core starts shutting down.
func sendRequest(conn comms.WsConn, route string, request, response interface{}, timeout time.Duration) error {
	reqMsg, err := msgjson.NewRequest(conn.NextID(), route, request)
	if err != nil {
		return fmt.Errorf("error encoding %q request: %w", route, err)
	}

	errChan := make(chan error, 1)
	err = conn.RequestWithTimeout(reqMsg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(response)
	}, timeout, func() {
		errChan <- fmt.Errorf("timed out waiting for %q response", route)
	})
	// Check the request error.
	if err != nil {
		return err
	}

	// Check the response error.
	return <-errChan
}

// newPreimage creates a random order commitment. If you require a matching
// commitment, generate a Preimage, then Preimage.Commit().
func newPreimage() (p order.Preimage) {
	copy(p[:], encode.RandomBytes(order.PreimageSize))
	return
}

// messagePrefix converts the order.Prefix to a msgjson.Prefix.
func messagePrefix(prefix *order.Prefix) *msgjson.Prefix {
	oType := uint8(msgjson.LimitOrderNum)
	switch prefix.OrderType {
	case order.MarketOrderType:
		oType = msgjson.MarketOrderNum
	case order.CancelOrderType:
		oType = msgjson.CancelOrderNum
	}
	return &msgjson.Prefix{
		AccountID:  prefix.AccountID[:],
		Base:       prefix.BaseAsset,
		Quote:      prefix.QuoteAsset,
		OrderType:  oType,
		ClientTime: encode.UnixMilliU(prefix.ClientTime),
		Commit:     prefix.Commit[:],
	}
}

// messageTrade converts the order.Trade to a msgjson.Trade, adding the coins.
func messageTrade(trade *order.Trade, coins []*msgjson.Coin) *msgjson.Trade {
	side := uint8(msgjson.BuyOrderNum)
	if trade.Sell {
		side = msgjson.SellOrderNum
	}
	return &msgjson.Trade{
		Side:     side,
		Quantity: trade.Quantity,
		Coins:    coins,
		Address:  trade.Address,
	}
}

// messageCoin converts the []asset.Coin to a []*msgjson.Coin, signing the coin
// IDs and retrieving the pubkeys too.
func messageCoins(wallet *xcWallet, coins asset.Coins, redeemScripts []dex.Bytes) ([]*msgjson.Coin, error) {
	msgCoins := make([]*msgjson.Coin, 0, len(coins))
	for i, coin := range coins {
		coinID := coin.ID()
		pubKeys, sigs, err := wallet.SignMessage(coin, coinID)
		if err != nil {
			return nil, fmt.Errorf("%s SignMessage error: %w", unbip(wallet.AssetID), err)
		}
		msgCoins = append(msgCoins, &msgjson.Coin{
			ID:      coinID,
			PubKeys: pubKeys,
			Sigs:    sigs,
			Redeem:  redeemScripts[i],
		})
	}
	return msgCoins, nil
}

// messageOrder converts an order.Order of any underlying type to an appropriate
// msgjson type used for submitting the order.
func messageOrder(ord order.Order, coins []*msgjson.Coin) (string, msgjson.Stampable) {
	prefix, trade := ord.Prefix(), ord.Trade()
	switch o := ord.(type) {
	case *order.LimitOrder:
		tifFlag := uint8(msgjson.StandingOrderNum)
		if o.Force == order.ImmediateTiF {
			tifFlag = msgjson.ImmediateOrderNum
		}
		return msgjson.LimitRoute, &msgjson.LimitOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
			Rate:   o.Rate,
			TiF:    tifFlag,
		}
	case *order.MarketOrder:
		return msgjson.MarketRoute, &msgjson.MarketOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
		}
	case *order.CancelOrder:
		return msgjson.CancelRoute, &msgjson.CancelOrder{
			Prefix:   *messagePrefix(prefix),
			TargetID: o.TargetOrderID[:],
		}
	default:
		panic("unknown order type")
	}
}

// validateOrderResponse validates the response against the order and the order
// message.
func validateOrderResponse(dc *dexConnection, result *msgjson.OrderResult, ord order.Order, msgOrder msgjson.Stampable) error {
	if result.ServerTime == 0 {
		return fmt.Errorf("OrderResult cannot have servertime = 0")
	}
	msgOrder.Stamp(result.ServerTime)
	msg := msgOrder.Serialize()
	err := dc.acct.checkSig(msg, result.Sig)
	if err != nil {
		return fmt.Errorf("signature error. order abandoned")
	}
	ord.SetTime(encode.UnixTimeMilli(int64(result.ServerTime)))
	// Check the order ID
	if len(result.OrderID) != order.OrderIDSize {
		return fmt.Errorf("failed ID length check. order abandoned")
	}
	var checkID order.OrderID
	copy(checkID[:], result.OrderID)
	oid := ord.ID()
	if oid != checkID {
		return fmt.Errorf("failed ID match. order abandoned")
	}
	return nil
}

// parseCert returns the (presumed to be) TLS certificate. If the certI is a
// string, it will be treated as a filepath and the raw file contents returned.
// if certI is already a []byte, it is presumed to be the raw file contents, and
// is returned unmodified.
func parseCert(host string, certI interface{}) ([]byte, error) {
	switch c := certI.(type) {
	case string:
		if len(c) == 0 {
			return CertStore[host], nil
		}
		cert, err := ioutil.ReadFile(c)
		if err != nil {
			return nil, newError(fileReadErr, "failed to read certificate file from %s: %v", c, err)
		}
		return cert, nil
	case []byte:
		if len(c) == 0 {
			return CertStore[host], nil
		}
		return c, nil
	}
	return nil, fmt.Errorf("not a valid certificate type %T", certI)
}
