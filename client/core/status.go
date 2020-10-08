// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// statusResolutionID is just a string with the basic information about a match.
// This is used often while logging during status resolution.
func statusResolutionID(dc *dexConnection, trade *trackedTrade, match *matchTracker) string {
	return fmt.Sprintf("host = %s, order = %s, match = %s", dc.acct.host, trade.ID(), match.id)
}

// resolveMatchConflicts accepts a dexConnection and a slice of match status
// conflicts. resolveMatchConflicts will block until the call to match_status
// returns, but then goroutines are started to handle the actual resolution.
// The trackedTrades' mutexes will remain locked until resolution completes.
func (c *Core) resolveMatchConflicts(dc *dexConnection, statusConflicts []*matchStatusConflict) {

	statusRequests := make([]*msgjson.MatchRequest, 0, len(statusConflicts))
	// Lock the trade mutexes now until we're done. The other option would be
	// to lock the trade mutexes in resolveConflictWithServerData, but that
	// might allow a tick to occur between the match_status request and
	// resolveConflictWithServerData. Our resolvers never send any requests or
	// or responses. The longest running functions only request some wallet
	// data, so I wouldn't expect these locks to be held for long. Regardless,
	// we'll run each resolveConflictWithServerData below as a goroutine just to
	// make sure we're not stacking those wallet calls.
	for _, conflict := range statusConflicts {
		conflict.trade.mtx.Lock()
		statusRequests = append(statusRequests, &msgjson.MatchRequest{
			Base:    conflict.trade.Base(),
			Quote:   conflict.trade.Quote(),
			MatchID: conflict.match.id[:],
		})
	}

	var msgStatuses []*msgjson.MatchStatusResult
	err := sendRequest(dc.WsConn, msgjson.MatchStatusRoute, statusRequests, &msgStatuses, DefaultResponseTimeout)
	if err != nil {
		for _, conflict := range statusConflicts {
			conflict.trade.mtx.Unlock()
		}
		c.log.Errorf("match_status request error for %s requesting %d match statuses. %s: %v", dc.acct.host, len(statusRequests), err)
		return
	}

	// Index the matches by match ID.
	resMap := make(map[order.MatchID]*msgjson.MatchStatusResult, len(msgStatuses))
	for _, msgStatus := range msgStatuses {
		var matchID order.MatchID
		copy(matchID[:], msgStatus.MatchID)
		resMap[matchID] = msgStatus
	}

	for _, conflict := range statusConflicts {
		srvData := resMap[conflict.match.id]
		if srvData == nil {
			// I don't really know how this would happen, considering the server
			// reported the match as active in the connect response. I'm also
			// not sure what action to take. Maybe just revoke the match.
			c.log.Errorf("Server did not report a status for match during resolution. %s", statusResolutionID(dc, conflict.trade, conflict.match))
			// revokeMatch only returns an error for a missing match ID,
			// and we already checked in compareServerMatches.
			conflict.trade.revokeMatch(conflict.match.id, false)
			conflict.trade.mtx.Unlock()
			continue
		}

		if order.MatchStatus(srvData.Status) != order.MatchComplete && !srvData.Active {
			// Server has revoked the match. We'll still go through
			// resolveConflictWithServerData to collect any extra data the
			// server has, but setting ServerRevoked will prevent us from
			// trying to update the state with the server.
			conflict.match.MetaData.Proof.ServerRevoked = true
		}
		go func(trade *trackedTrade, match *matchTracker) {
			c.resolveConflictWithServerData(dc, trade, match, srvData)
			trade.mtx.Unlock()
		}(conflict.trade, conflict.match)
	}
}

// The matchConflictResolver is unique to a MatchStatus pair and handles
// attempts to resolve a conflict between our match state and the state reported
// by the server. A matchConflictResolver may update the MetaMatch, but need
// not save the changes to persistent storage. Changes will be saved by
// resolveConflictWithServerData.
type matchConflictResolver func(*dexConnection, *trackedTrade, *matchTracker, *msgjson.MatchStatusResult)

// conflictResolvers are the resolvers specified for each MatchStatus combo.
var conflictResolvers = []struct {
	ours, servers order.MatchStatus
	resolver      matchConflictResolver
}{
	// Our status         Server's status      Resolver
	{order.NewlyMatched, order.MakerSwapCast, resolveMissedMakerAudit},
	{order.MakerSwapCast, order.NewlyMatched, resolveServerMissedMakerInit},
	{order.MakerSwapCast, order.TakerSwapCast, resolveMissedTakerAudit},
	{order.TakerSwapCast, order.MakerSwapCast, resolveServerMissedTakerInit},
	{order.TakerSwapCast, order.MakerRedeemed, resolveMissedMakerRedemption},
	{order.MakerRedeemed, order.TakerSwapCast, resolveServerMissedMakerRedeem},
	{order.MakerRedeemed, order.MatchComplete, resolveMatchComplete},
	{order.MatchComplete, order.MakerRedeemed, resolveServerMissedTakerRedeem},
}

// conflictResolver is a getter for a matchConflictResolver for the specified
// MatchStatus combination. If there is no resolver for this combination, a
// nil resolver will be returned.
func conflictResolver(ours, servers order.MatchStatus) matchConflictResolver {
	for _, r := range conflictResolvers {
		if r.ours == ours && r.servers == servers {
			return r.resolver
		}
	}
	return nil
}

// resolveConflictWithServerData compares the match status with the server's
// match_status data. The trackedTrade.mtx is locked for the duration of
// resolution. If the conflict cannot be resolved, the match will be
// self-revoked.
func (c *Core) resolveConflictWithServerData(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	srvStatus := order.MatchStatus(srvData.Status)

	if srvStatus == match.MetaData.Status || match.MetaData.Proof.IsRevoked() {
		// On startup, there's no chance for a tick between the connect request
		// and the match_status request, so this would be unlikely. But if not
		// during startup, and a tick has snuck in and resolved our status
		// conflict already (either by refunding or via resendPendingRequests),
		// that's OK.
		return
	}

	logID := statusResolutionID(dc, trade, match)

	resolver := conflictResolver(match.MetaData.Status, srvStatus)
	if resolver != nil {
		resolver(dc, trade, match, srvData)
	} else {
		// We don't know how to handle this. Set the failErr, and self-revoke
		// the match. This condition would be virtually impossible, because it
		// would mean that the client and server were at least two steps out of
		// sync.
		match.failErr = fmt.Errorf("status conflict (%s -> %s) has no handler. %s",
			match.MetaData.Status, srvStatus, logID)
		c.log.Error(match.failErr)
		match.MetaData.Proof.SelfRevoked = true
		err := c.db.UpdateMatch(&match.MetaMatch)
		if err != nil {
			c.log.Errorf("error updating database after self revocation for no conflict handler for %s: %v", logID, err)
		}
	}

	err := c.db.UpdateMatch(&match.MetaMatch)
	if err != nil {
		c.log.Errorf("error updating database after successful match resolution for %s: %v", logID, err)
	}
}

// resolveMissedMakerAudit is a matchConflictResolver to handle the case when
// our status is NewlyMatched, but the server is at MakerSwapCast. If we are the
// taker, we likely missed an audit request and we can process the match_status
// data to get caught up.
func resolveMissedMakerAudit(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	var err error
	defer func() {
		if err != nil {
			match.MetaData.Proof.SelfRevoked = true
			dc.log.Error(err)
		}
	}()

	// We can handle this if we're the taker.
	if match.Match.Side == order.Maker {
		err = fmt.Errorf("Server is reporting match in MakerSwapCast, but we're the maker and haven't sent a swap. %s", logID)
		return
	}
	// We probably missed an audit request.
	if len(srvData.MakerSwap) == 0 {
		err = fmt.Errorf("Server is reporting a match with status MakerSwapCast, but didn't include a coin ID for the swap. %s", logID)
		return
	}
	if len(srvData.MakerContract) == 0 {
		err = fmt.Errorf("Server is reporting a match with status MakerSwapCast, but didn't include the contract data. %s", logID)
		return
	}

	err = trade.auditContract(match, srvData.MakerSwap, srvData.MakerContract)
	if err != nil {
		err = fmt.Errorf("auditContract error during match status resolution. %s: %v", logID, err)
	}
}

// resolveMissedTakerAudit is a matchConflictResolver to handle the case when
// our status is MakerSwapCast, but the server is at TakerSwapCast. If we are
// the maker, we likely missed an audit request and we can process the
// match_status data to get caught up.
func resolveMissedTakerAudit(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	var err error
	defer func() {
		if err != nil {
			match.MetaData.Proof.SelfRevoked = true
			dc.log.Error(err)
		}
	}()
	// This is nonsensical if we're the taker.
	if match.Match.Side == order.Taker {
		err = fmt.Errorf("Server is reporting match in TakerSwapCast, but we're the taker and haven't sent a swap. %s", logID)
		return
	}
	// We probably missed an audit request.
	if len(srvData.TakerSwap) == 0 {
		err = fmt.Errorf("Server is reporting a match with status TakerSwapCast, but didn't include a coin ID for the swap. %s", logID)
		return
	}
	if len(srvData.TakerContract) == 0 {
		err = fmt.Errorf("Server is reporting a match with status TakerSwapCast, but didn't include the contract data. %s", logID)
		return
	}
	err = trade.auditContract(match, srvData.TakerSwap, srvData.TakerContract)
	if err != nil {
		err = fmt.Errorf("auditContract error during match status resolution. %s: %v", logID, err)
	}
}

// resolveServerMissedMakerInit is a matchConflictResolver to handle the case
// when our status is MakerSwapCast, but the server is at NewlyMatched. If we're
// the maker, we probably encountered an issue while sending our init request,
// so we'll defer to resendPendingRequests to handle it in the next tick.
func resolveServerMissedMakerInit(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	// If we're not the maker, there's nothing we can do.
	if match.Match.Side != order.Maker {
		dc.log.Errorf("Server reporting no maker swap, but they've already sent us the swap info. self-revoking. %s", logID)
		match.MetaData.Proof.SelfRevoked = true
		return

	}
	// If we don't have a server acknowledgment, that case will be picked up in
	// resendPendingRequests at the next tick.
	if len(match.MetaData.Proof.Auth.InitSig) == 0 {
		return
	}
	// On the other hand, if we do have an acknowledgement from the server,
	// this appears to be a server error, and we should just revoke the match
	// and wait to refund.
	dc.log.Errorf("Server appears to have lost our (maker's) init data after acknowledgement. self-revoking order. %s", logID)
	match.MetaData.Proof.SelfRevoked = true
}

// resolveServerMissedTakerInit is a matchConflictResolver to handle the case
// when our status is TakerSwapCast, but the server is at MakerSwapCast. If
// we're the taker, the server likely missed our init request, so we'll defer to
// resendPendingRequests to handle it in the next tick.
func resolveServerMissedTakerInit(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	// If we're not the taker, there's nothing we can do.
	if match.Match.Side != order.Taker {
		dc.log.Errorf("Server reporting no taker swap, but they've already sent us the swap info. self-revoking. %s", logID)
		match.MetaData.Proof.SelfRevoked = true
		return
	}
	// If we don't have a server acknowledgment, that case will be picked up in
	// resendPendingRequests at the next tick.
	if len(match.MetaData.Proof.Auth.InitSig) == 0 {
		return
	}
	// On the other hand, if we do have an acknowledgement from the server,
	// this appears to be a server error, and we should just revoke the match
	// and wait to refund.
	dc.log.Errorf("Server appears to have lost our (taker's) init data after acknowledgement. self-revoking order. %s", logID)
	match.MetaData.Proof.SelfRevoked = true
}

// resolveMissedMakerRedemption is a matchConflictResolver to handle the case
// when our status is TakerSwapCast, but the server is at MakerRedeemed. If
// we're the taker, we probably missed the redemption request from the server,
// and we can process the match_status data to get caught up.
func resolveMissedMakerRedemption(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	var err error
	defer func() {
		if err != nil {
			match.MetaData.Proof.SelfRevoked = true
			dc.log.Error(err)
		}
	}()
	// If we're the maker, this state is nonsense. Just revoke the match for
	// good measure.
	if match.Match.Side == order.Maker {
		coinStr, _ := asset.DecodeCoinID(trade.wallets.toAsset.ID, srvData.MakerRedeem)
		err = fmt.Errorf("server reported match status MakerRedeemed, but we're the maker and we don't have redemption data."+
			" self-revoking. %s, reported coin = %s", logID, coinStr)
		return
	}
	// If we're the taker, grab the redemption data and progress the status.
	if len(srvData.MakerRedeem) == 0 {
		err = fmt.Errorf("Server reporting status MakerRedeemed, but not reporting "+
			"a redemption coin ID. self-revoking. %s", logID)
		return
	}
	if len(srvData.Secret) == 0 {
		err = fmt.Errorf("Server reporting status MakerRedeemed, but not reporting "+
			"a secret. self-revoking. %s", logID)
		return
	}
	if err = trade.processMakersRedemption(match, srvData.MakerRedeem, srvData.Secret); err != nil {
		err = fmt.Errorf("error processing maker's redemption data during match status resolution. "+
			"self-revoking. %s", logID)
	}
}

// resolveMatchComplete is a matchConflictResolver to handle the case when our
// status is MakerRedeemed, but the server is at MatchComplete. Since the server
// does not send redemption requests to the maker following taker redeem, this
// indicates the match status was just not updated after sending our redeem.
func resolveMatchComplete(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	var err error
	defer func() {
		if err != nil {
			match.MetaData.Proof.SelfRevoked = true
			dc.log.Error(err)
		}
	}()
	// If we're the taker, this state is nonsense. Just revoke the match for
	// good measure.
	if match.Match.Side == order.Taker {
		coinStr, _ := asset.DecodeCoinID(trade.wallets.toAsset.ID, srvData.TakerRedeem)
		err = fmt.Errorf("server reported match status MatchComplete, but we're the taker and we don't have redemption data."+
			" self-revoking. %s, reported coin = %s", logID, coinStr)
		return
	}
	// As maker, set it to MatchComplete. We no longer expect to receive taker
	// redeem info.
	dc.log.Warnf("Server reporting MatchComplete while we (maker) have it as MakerRedeemed. Resolved. Detail: %v", logID)
	match.SetStatus(order.MatchComplete)
}

// resolveServerMissedMakerRedeem is a matchConflictResolver to handle the case
// when our status is MakerRedeemed, but the server is at TakerSwapCast. If
// we're the maker, the server probably missed our redeem request, so we'll
// defer to resendPendingRequests to handle it in the next tick.
func resolveServerMissedMakerRedeem(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	// If we're not the maker, we can't do anything about this.
	if match.Match.Side != order.Maker {
		dc.log.Errorf("server reporting no maker redeem, but they've already sent us the redemption info. self-revoking. %s", logID)
		match.MetaData.Proof.SelfRevoked = true
		return
	}
	// We are the maker, if we don't have an ack from the server, this will be
	// picked up in resendPendingRequests during the next tick.
	if len(match.MetaData.Proof.Auth.RedeemSig) == 0 {
		return
	}
	// Otherwise, it appears that the server has acked, and then lost our redeem
	// data. Just revoke.
	dc.log.Errorf("server reporting no maker redeem, but we are the maker and we have a valid ack. self-revoking. %s", logID)
	match.MetaData.Proof.SelfRevoked = true
}

// resolveServerMissedTakerRedeem is a matchConflictResolver to handle the case
// when our status is MatchComplete, but the server is at MakerRedeemed. If
// we're the taker, the server probably missed our redeem request, so we'll
// defer to resendPendingRequests to handle it in the next tick.
func resolveServerMissedTakerRedeem(dc *dexConnection, trade *trackedTrade, match *matchTracker, srvData *msgjson.MatchStatusResult) {
	logID := statusResolutionID(dc, trade, match)
	// If we're not the Taker, we can't do anything about this.
	if match.Match.Side != order.Taker {
		dc.log.Errorf("server reporting no taker redeem, but they've already sent us the redemption info. self-revoking. %s", logID)
		return
	}
	// We are the taker, if we don't have an ack from the server, this will be
	// picked up in resendPendingRequests during the next tick.
	if len(match.MetaData.Proof.Auth.RedeemSig) == 0 {
		return
	}
	// Otherwise, it appears that the server has acked, and then lost our redeem
	// data. Just revoke.
	dc.log.Errorf("server reporting no taker redeem, but we are the taker and we have a valid ack. self-revoking. %s", logID)
	match.MetaData.Proof.SelfRevoked = true
}
