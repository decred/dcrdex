// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/market"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

const (
	// miaSweepInterval schedules checks of booked users more often than
	// regular peer connectivity queries.
	miaSweepInterval = 15 * time.Second

	// peerConnectedQueryInterval controls how often cached peer connections
	// are refreshed. An additional query may precede a revocation.
	peerConnectedQueryInterval = time.Minute

	// peerConnectedQueryTimeout bounds one peer connectivity query.
	peerConnectedQueryTimeout = 30 * time.Second
)

// presenceMeshService is the mesh surface the unbooker needs.
type presenceMeshService interface {
	QueryClientConnected(ctx context.Context, users []account.AccountID) ([]account.AccountID, error)
	ApplyEvent(context.Context, *mesh.Event) (any, error)
}

// bookedUserSource reports accounts with booked orders on one market.
type bookedUserSource interface {
	BookedUsers() map[account.AccountID]int
}

// accountStatusSource reports local connectivity and account reputation.
type accountStatusSource interface {
	AcctRepStatus(user account.AccountID) (connected bool, rep *account.Reputation, err error)
	ConnectedAmong(users []account.AccountID) []account.AccountID
}

// presenceUnbooker revokes booked orders for unknown accounts, users with
// penalties and an effective tier below 1, and users whose absence timers
// expire. Expired bonds without penalties do not trigger penalty revocation.
// Failed peer queries are treated as no confirmed peer connection.
type presenceUnbooker struct {
	log      dex.Logger
	markets  map[string]bookedUserSource
	accounts accountStatusSource
	timeout  time.Duration
	mesh     presenceMeshService

	// Master sweep state (runMaster goroutine only).
	absentSince   map[account.AccountID]time.Time
	peerConnected map[account.AccountID]struct{}
	lastQuery     time.Time
}

func newPresenceUnbooker(log dex.Logger, markets map[string]*market.Market, accounts accountStatusSource, miaTimeout time.Duration) *presenceUnbooker {
	bookSources := make(map[string]bookedUserSource, len(markets))
	for name, mkt := range markets {
		bookSources[name] = mkt
	}
	return &presenceUnbooker{
		log:         log,
		markets:     bookSources,
		accounts:    accounts,
		timeout:     miaTimeout,
		absentSince: make(map[account.AccountID]time.Time),
	}
}

// masterWorker returns the unbooker worker. Register it after market workers
// so startup order cleanup finishes before the first sweep.
func (p *presenceUnbooker) masterWorker() mesh.MasterWorker {
	return mesh.MasterWorker{
		Name: "Unbooker",
		Run:  p.runMaster,
	}
}

// runMaster checks booked users until ctx is canceled. It resets absence timers
// and skips peer connectivity queries during the initial sweep.
func (p *presenceUnbooker) runMaster(ctx context.Context, reportReady func(error)) {
	now := time.Now()
	p.absentSince = make(map[account.AccountID]time.Time)
	p.peerConnected = nil
	p.lastQuery = now

	p.sweep(ctx, now)
	reportReady(nil)

	ticker := time.NewTicker(miaSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.sweep(ctx, time.Now())
		case <-ctx.Done():
			return
		}
	}
}

// queryPeerConnected returns the peer's connected subset. ok is false when
// the query fails; callers treat that as no confirmed peer connection.
func (p *presenceUnbooker) queryPeerConnected(ctx context.Context, users []account.AccountID) (map[account.AccountID]struct{}, bool) {
	queryCtx, cancel := context.WithTimeout(ctx, peerConnectedQueryTimeout)
	defer cancel()
	connected, err := p.mesh.QueryClientConnected(queryCtx, users)
	if err != nil {
		p.log.Debugf("Peer connectivity query for %d users failed: %v", len(users), err)
		return nil, false
	}
	set := make(map[account.AccountID]struct{}, len(connected))
	for _, user := range connected {
		set[user] = struct{}{}
	}
	return set, true
}

// refreshPeerConnected refreshes cached peer connections when due. It returns
// true only if it performs a successful query. Failed queries also advance the
// query time.
func (p *presenceUnbooker) refreshPeerConnected(ctx context.Context, now time.Time, candidates []account.AccountID) bool {
	if len(candidates) == 0 || now.Sub(p.lastQuery) < peerConnectedQueryInterval {
		return false
	}
	p.lastQuery = now
	var ok bool
	p.peerConnected, ok = p.queryPeerConnected(ctx, candidates)
	return ok
}

func (p *presenceUnbooker) sweep(ctx context.Context, now time.Time) {
	booked := p.bookedUsers()
	p.pruneAbsent(booked)

	tierRevokes, candidates := p.classifyBookedUsers(booked)
	peerRefreshed := p.refreshPeerConnected(ctx, now, candidates)
	disconnectedUsers := p.expiredAbsences(candidates, now)
	disconnectedUsers = p.recheckPeerConnections(ctx, disconnectedUsers, peerRefreshed)

	p.applyRevokes(ctx, now, booked, tierRevokes, meshevents.OrderRevokeReasonPenalty)
	p.applyRevokes(ctx, now, booked, disconnectedUsers, meshevents.OrderRevokeReasonDisconnected)
}

func (p *presenceUnbooker) bookedUsers() map[account.AccountID]int {
	booked := make(map[account.AccountID]int)
	for _, mkt := range p.markets {
		for user, count := range mkt.BookedUsers() {
			booked[user] += count
		}
	}
	return booked
}

func (p *presenceUnbooker) pruneAbsent(booked map[account.AccountID]int) {
	for user := range p.absentSince {
		if booked[user] == 0 {
			delete(p.absentSince, user)
		}
	}
}

// classifyBookedUsers separates users requiring penalty revocation from users
// disconnected locally. Local connections clear absence timers. Reputation
// lookup failures skip the penalty check but do not exempt disconnected users.
func (p *presenceUnbooker) classifyBookedUsers(booked map[account.AccountID]int) (tierRevokes []account.AccountID, candidates []account.AccountID) {
	var loadErrs, bondExpired int
	var lastErr error
	for user := range booked {
		connected, rep, err := p.accounts.AcctRepStatus(user)
		if err != nil {
			loadErrs++
			lastErr = err
		} else if shouldUnbookForReputation(rep) {
			delete(p.absentSince, user)
			tierRevokes = append(tierRevokes, user)
			continue
		} else if rep.EffectiveTier() < 1 {
			bondExpired++
		}
		if connected {
			delete(p.absentSince, user)
			continue
		}
		candidates = append(candidates, user)
	}
	if bondExpired > 0 {
		p.log.Debugf("%d of %d booked users are below tier 1 from bond expiry only; not unbooking for reputation.",
			bondExpired, len(booked))
	}
	if loadErrs > 0 {
		p.log.Warnf("Reputation unavailable for %d of %d booked users; skipping their tier check this sweep. Last error: %v",
			loadErrs, len(booked), lastErr)
	}
	return tierRevokes, candidates
}

// shouldUnbookForReputation reports whether the account is unknown or has
// penalties and an effective tier below 1.
func shouldUnbookForReputation(rep *account.Reputation) bool {
	return rep == nil || (rep.EffectiveTier() < 1 && rep.Penalties > 0)
}

// expiredAbsences updates absence timers using cached peer connections and
// returns users whose absence timers have expired.
func (p *presenceUnbooker) expiredAbsences(candidates []account.AccountID, now time.Time) []account.AccountID {
	var expired []account.AccountID
	for _, user := range candidates {
		if _, found := p.peerConnected[user]; found {
			delete(p.absentSince, user)
			continue
		}
		since, tracked := p.absentSince[user]
		if !tracked {
			p.absentSince[user] = now
			continue
		}
		if now.Sub(since) >= p.timeout {
			expired = append(expired, user)
		}
	}
	return expired
}

// recheckPeerConnections queries users due for revocation unless this sweep
// already queried them successfully. Connected users have their absence
// timers cleared and are added to the cache for subsequent sweeps.
func (p *presenceUnbooker) recheckPeerConnections(ctx context.Context, users []account.AccountID, peerRefreshed bool) []account.AccountID {
	if len(users) == 0 || peerRefreshed {
		return users
	}
	fresh, ok := p.queryPeerConnected(ctx, users)
	if !ok {
		return users
	}
	confirmed := make([]account.AccountID, 0, len(users))
	for _, user := range users {
		if _, found := fresh[user]; found {
			delete(p.absentSince, user)
			continue
		}
		confirmed = append(confirmed, user)
	}
	if len(fresh) == 0 {
		return confirmed
	}
	if p.peerConnected == nil {
		p.peerConnected = make(map[account.AccountID]struct{}, len(fresh))
	}
	for user := range fresh {
		p.peerConnected[user] = struct{}{}
	}
	return confirmed
}

// applyRevokes rechecks penalty eligibility or local connectivity before
// submitting each revocation event.
// Failed applications are retried on a later sweep if the user still qualifies.
func (p *presenceUnbooker) applyRevokes(ctx context.Context, now time.Time, booked map[account.AccountID]int, users []account.AccountID, reason meshevents.OrderRevokeReason) {
	for _, user := range users {
		if ctx.Err() != nil {
			return
		}
		// Connectivity and reputation may have changed while peer queries or
		// earlier revocations were in progress.
		switch reason {
		case meshevents.OrderRevokeReasonPenalty:
			_, rep, err := p.accounts.AcctRepStatus(user)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				p.log.Warnf("Skipping penalty revocation for user %v: reputation unavailable: %v", user, err)
				continue
			}
			if !shouldUnbookForReputation(rep) {
				continue
			}
			p.log.Infof("Revoking booked orders of user %v: account unknown or penalties reduce tier below 1 (%d booked orders).",
				user, booked[user])
		case meshevents.OrderRevokeReasonDisconnected:
			if len(p.accounts.ConnectedAmong([]account.AccountID{user})) != 0 {
				delete(p.absentSince, user)
				continue
			}
			p.log.Infof("Revoking booked orders of user %v: no connection confirmed for %v (%d booked orders).",
				user, p.timeout, booked[user])
		}
		event, err := mesh.NewEvent(meshevents.NewOrdersRevokedForUserEvent(user, reason, now.UTC()))
		if err != nil {
			p.log.Errorf("Failed to build orders_revoked event for user %v (reason %d): %v", user, reason, err)
			continue
		}
		if _, err := p.mesh.ApplyEvent(ctx, event); err != nil {
			p.log.Errorf("Failed to apply orders_revoked event for user %v (reason %d): %v", user, reason, err)
			continue
		}
		if reason == meshevents.OrderRevokeReasonDisconnected {
			delete(p.absentSince, user)
		}
	}
}
