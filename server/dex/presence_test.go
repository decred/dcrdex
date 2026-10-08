// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

type tBookSource struct {
	users map[account.AccountID]int
}

func (s *tBookSource) BookedUsers() map[account.AccountID]int {
	users := make(map[account.AccountID]int, len(s.users))
	for user, count := range s.users {
		users[user] = count
	}
	return users
}

// tPresenceMesh answers connectivity queries with the intersection of the
// queried users and the configured connected set, like a real peer would.
type tPresenceMesh struct {
	connected map[account.AccountID]struct{}
	queryErr  error
	failures  int // fail this many queries, then answer normally
	queries   [][]account.AccountID
	applied   []*mesh.Event
	applyErr  error
	onQuery   func()
}

func (m *tPresenceMesh) setConnected(users ...account.AccountID) {
	m.connected = make(map[account.AccountID]struct{}, len(users))
	for _, user := range users {
		m.connected[user] = struct{}{}
	}
}

func (m *tPresenceMesh) QueryClientConnected(_ context.Context, users []account.AccountID) ([]account.AccountID, error) {
	m.queries = append(m.queries, users)
	if m.onQuery != nil {
		m.onQuery()
	}
	if m.failures > 0 {
		m.failures--
		return nil, errors.New("transient query failure")
	}
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	var connected []account.AccountID
	for _, user := range users {
		if _, found := m.connected[user]; found {
			connected = append(connected, user)
		}
	}
	return connected, nil
}

func (m *tPresenceMesh) ApplyEvent(_ context.Context, event *mesh.Event) (any, error) {
	if m.applyErr != nil {
		return nil, m.applyErr
	}
	m.applied = append(m.applied, event)
	return nil, nil
}

func tAcctID(b byte) account.AccountID {
	var user account.AccountID
	user[0] = b
	return user
}

type tAccountStatus struct {
	connected bool
	rep       *account.Reputation
	err       error
}

type tAccountStatusSource map[account.AccountID]tAccountStatus

func (s tAccountStatusSource) AcctRepStatus(user account.AccountID) (bool, *account.Reputation, error) {
	status := s[user]
	return status.connected, status.rep, status.err
}

func (s tAccountStatusSource) ConnectedAmong(users []account.AccountID) []account.AccountID {
	var connected []account.AccountID
	for _, user := range users {
		if s[user].connected {
			connected = append(connected, user)
		}
	}
	return connected
}

func newTestUnbooker(books map[string]bookedUserSource, timeout time.Duration) (*presenceUnbooker, *tPresenceMesh, tAccountStatusSource) {
	accounts := make(tAccountStatusSource)
	for _, book := range books {
		for user := range book.BookedUsers() {
			accounts[user] = tAccountStatus{rep: &account.Reputation{BondedTier: 1}}
		}
	}
	meshSvc := new(tPresenceMesh)
	ub := &presenceUnbooker{
		log:         dex.StdOutLogger("TEST", dex.LevelOff),
		markets:     books,
		accounts:    accounts,
		timeout:     timeout,
		mesh:        meshSvc,
		absentSince: make(map[account.AccountID]time.Time),
	}
	return ub, meshSvc, accounts
}

func decodeUserRevoke(t *testing.T, event *mesh.Event) (account.AccountID, meshevents.OrderRevokeReason) {
	t.Helper()
	if event.Kind != meshevents.EventKindOrdersRevoked {
		t.Fatalf("event kind = %q, want %q", event.Kind, meshevents.EventKindOrdersRevoked)
	}
	payload, err := meshevents.DecodeOrdersRevokedEvent(event.Payload)
	if err != nil {
		t.Fatalf("failed to decode orders_revoked payload: %v", err)
	}
	return account.AccountID(payload.User), payload.Reason
}

func decodeRevokedUser(t *testing.T, event *mesh.Event) account.AccountID {
	t.Helper()
	user, reason := decodeUserRevoke(t, event)
	if reason != meshevents.OrderRevokeReasonDisconnected {
		t.Fatalf("reason = %d, want disconnected", reason)
	}
	return user
}

func TestSweepClassification(t *testing.T) {
	const noRevoke = meshevents.OrderRevokeReason(0)
	user := tAcctID(1)
	healthy := &account.Reputation{BondedTier: 1}
	expired := &account.Reputation{}
	loadErr := errors.New("db down")
	timeout := time.Minute
	tests := []struct {
		name        string
		status      tAccountStatus
		peerConn    bool
		pastTimeout bool
		wantRevoke  meshevents.OrderRevokeReason
		wantAbsent  bool
		wantQueries int
	}{
		{
			name:   "healthy, locally connected",
			status: tAccountStatus{connected: true, rep: healthy},
		},
		{
			name:   "unknown account",
			status: tAccountStatus{connected: true}, pastTimeout: true,
			wantRevoke: meshevents.OrderRevokeReasonPenalty,
		},
		{
			name:   "load error, locally connected",
			status: tAccountStatus{connected: true, err: loadErr},
		},
		{
			name:   "load error, absent",
			status: tAccountStatus{err: loadErr}, wantAbsent: true, wantQueries: 1,
		},
		{
			name:   "load error, past timeout but peer connected",
			status: tAccountStatus{err: loadErr}, peerConn: true, pastTimeout: true,
			wantQueries: 1,
		},
		{
			name:   "load error, disconnected past timeout",
			status: tAccountStatus{err: loadErr}, pastTimeout: true,
			wantRevoke: meshevents.OrderRevokeReasonDisconnected, wantQueries: 1,
		},
		{
			name:   "expired bond, locally connected",
			status: tAccountStatus{connected: true, rep: expired},
		},
		{
			name:   "expired bond, peer connected",
			status: tAccountStatus{rep: expired}, peerConn: true, wantQueries: 1,
		},
		{
			name:   "expired bond, absent",
			status: tAccountStatus{rep: expired}, wantAbsent: true, wantQueries: 1,
		},
		{
			name:   "expired bond, disconnected past timeout",
			status: tAccountStatus{rep: expired}, pastTimeout: true,
			wantRevoke: meshevents.OrderRevokeReasonDisconnected, wantQueries: 1,
		},
		{
			name:       "expired bond with penalties",
			status:     tAccountStatus{connected: true, rep: &account.Reputation{Penalties: 1, Score: -20}},
			wantRevoke: meshevents.OrderRevokeReasonPenalty,
		},
		{
			name:       "penalties with intact bond",
			status:     tAccountStatus{connected: true, rep: &account.Reputation{BondedTier: 1, Penalties: 1, Score: -20}},
			wantRevoke: meshevents.OrderRevokeReasonPenalty,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			books := map[string]bookedUserSource{
				"dcr_btc": &tBookSource{users: map[account.AccountID]int{user: 1}},
			}
			ub, meshSvc, accounts := newTestUnbooker(books, timeout)
			accounts[user] = tt.status
			if tt.peerConn {
				meshSvc.setConnected(user)
			}
			now := time.Now()
			if tt.pastTimeout {
				ub.absentSince[user] = now.Add(-2 * timeout)
			}
			ub.sweep(context.Background(), now)
			if tt.wantRevoke == noRevoke {
				if len(meshSvc.applied) != 0 {
					t.Fatalf("applied %d events, want 0", len(meshSvc.applied))
				}
			} else if len(meshSvc.applied) != 1 {
				t.Fatalf("applied %d events, want 1", len(meshSvc.applied))
			} else if gotUser, reason := decodeUserRevoke(t, meshSvc.applied[0]); gotUser != user || reason != tt.wantRevoke {
				t.Fatalf("revoked (%v, reason %d), want (%v, reason %d)", gotUser, reason, user, tt.wantRevoke)
			}
			if _, tracked := ub.absentSince[user]; tracked != tt.wantAbsent {
				t.Fatalf("absence tracked = %v, want %v", tracked, tt.wantAbsent)
			}
			if len(meshSvc.queries) != tt.wantQueries {
				t.Fatalf("issued %d peer queries, want %d", len(meshSvc.queries), tt.wantQueries)
			}
		})
	}
}

func TestMIASweep(t *testing.T) {
	miaUser, localUser, peerUser := tAcctID(1), tAcctID(2), tAcctID(3)
	books := map[string]bookedUserSource{
		"dcr_btc": &tBookSource{users: map[account.AccountID]int{
			miaUser:   2,
			localUser: 1,
			peerUser:  1,
		}},
	}
	timeout := time.Minute
	ub, meshSvc, accounts := newTestUnbooker(books, timeout)
	accounts[localUser] = tAccountStatus{connected: true, rep: &account.Reputation{BondedTier: 1}}
	meshSvc.setConnected(peerUser)

	ctx := context.Background()
	now := time.Now()

	// First sweep queries the peer for the two non-local users and arms the
	// absence deadline only for the user connected nowhere.
	ub.sweep(ctx, now)
	if len(meshSvc.applied) != 0 {
		t.Fatalf("revoked %d users on first sweep, want 0", len(meshSvc.applied))
	}
	if len(meshSvc.queries) != 1 || len(meshSvc.queries[0]) != 2 {
		t.Fatalf("peer queries = %v, want one for the 2 non-local users", meshSvc.queries)
	}
	if _, tracked := ub.absentSince[miaUser]; !tracked {
		t.Fatal("absent user deadline not armed")
	}
	if _, tracked := ub.absentSince[localUser]; tracked {
		t.Fatal("locally connected user deadline armed")
	}
	if _, tracked := ub.absentSince[peerUser]; tracked {
		t.Fatal("peer-connected user deadline armed")
	}

	// Before the timeout, still nothing.
	ub.sweep(ctx, now.Add(timeout/2))
	if len(meshSvc.applied) != 0 {
		t.Fatalf("revoked %d users before timeout, want 0", len(meshSvc.applied))
	}

	// A reconnect to the peer clears the deadline at the next refresh, which
	// happens before checking for expired absence timers.
	meshSvc.setConnected(peerUser, miaUser)
	ub.sweep(ctx, now.Add(peerConnectedQueryInterval))
	if _, tracked := ub.absentSince[miaUser]; tracked {
		t.Fatal("deadline not cleared on peer reconnect")
	}
	if len(meshSvc.applied) != 0 {
		t.Fatalf("revoked %d users despite peer reconnect, want 0", len(meshSvc.applied))
	}

	// Disconnect again: the absence timer restarts.
	meshSvc.setConnected(peerUser)
	rearm := now.Add(2 * timeout)
	ub.sweep(ctx, rearm)
	if len(meshSvc.applied) != 0 {
		t.Fatalf("revoked %d users right after re-arm, want 0", len(meshSvc.applied))
	}

	// Past the timeout, the user's orders are revoked with one user-form
	// event, and the deadline is cleared.
	ub.sweep(ctx, rearm.Add(timeout))
	if len(meshSvc.applied) != 1 {
		t.Fatalf("applied %d events, want 1", len(meshSvc.applied))
	}
	if user := decodeRevokedUser(t, meshSvc.applied[0]); user != miaUser {
		t.Fatalf("revoked user %v, want %v", user, miaUser)
	}
	if _, tracked := ub.absentSince[miaUser]; tracked {
		t.Fatal("deadline not cleared after revoke")
	}

}

func TestMIASweepQueryError(t *testing.T) {
	miaUser := tAcctID(1)
	books := map[string]bookedUserSource{
		"dcr_btc": &tBookSource{users: map[account.AccountID]int{miaUser: 1}},
	}
	timeout := 90 * time.Second
	ub, meshSvc, _ := newTestUnbooker(books, timeout)

	ctx := context.Background()
	now := time.Now()

	// The user is connected to the peer, but the query fails: treated as
	// absent, the deadline arms and the failure still counts as a refresh.
	meshSvc.setConnected(miaUser)
	meshSvc.queryErr = errors.New("peer down")
	ub.sweep(ctx, now)
	if _, tracked := ub.absentSince[miaUser]; !tracked {
		t.Fatal("deadline not armed on query error")
	}
	ub.sweep(ctx, now.Add(miaSweepInterval))
	if len(meshSvc.queries) != 1 {
		t.Fatalf("failed query retried within the interval: %d queries", len(meshSvc.queries))
	}

	// Another failed refresh past the interval, still before the timeout.
	ub.sweep(ctx, now.Add(timeout-miaSweepInterval))
	if len(meshSvc.queries) != 2 {
		t.Fatalf("peer queries = %d, want 2", len(meshSvc.queries))
	}

	// With the peer still unreachable past the timeout, the final query also
	// fails. The user's orders are still revoked.
	ub.sweep(ctx, now.Add(timeout))
	if len(meshSvc.queries) != 3 {
		t.Fatalf("peer queries = %d, want 3 (final query attempted)", len(meshSvc.queries))
	}
	if len(meshSvc.applied) != 1 {
		t.Fatalf("applied %d events with peer down, want 1", len(meshSvc.applied))
	}
	if user := decodeRevokedUser(t, meshSvc.applied[0]); user != miaUser {
		t.Fatalf("revoked user %v, want %v", user, miaUser)
	}
}

func TestSweepRechecksPeerConnections(t *testing.T) {
	user, other := tAcctID(1), tAcctID(2)
	tests := []struct {
		name          string
		lastQueryAgo  time.Duration
		queryFailures int
		wantQueries   int
	}{
		{name: "cached absence", lastQueryAgo: miaSweepInterval, wantQueries: 1},
		{name: "failed regular refresh", lastQueryAgo: peerConnectedQueryInterval, queryFailures: 1, wantQueries: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			books := map[string]bookedUserSource{
				"dcr_btc": &tBookSource{users: map[account.AccountID]int{user: 1, other: 1}},
			}
			ub, meshSvc, _ := newTestUnbooker(books, time.Minute)
			now := time.Now()
			ub.absentSince[user] = now.Add(-ub.timeout)
			ub.absentSince[other] = now
			ub.lastQuery = now.Add(-tt.lastQueryAgo)
			meshSvc.setConnected(user)
			meshSvc.failures = tt.queryFailures

			ub.sweep(context.Background(), now)
			if len(meshSvc.queries) != tt.wantQueries {
				t.Fatalf("peer queries = %d, want %d", len(meshSvc.queries), tt.wantQueries)
			}
			if got := meshSvc.queries[len(meshSvc.queries)-1]; len(got) != 1 || got[0] != user {
				t.Fatalf("final query = %v, want only the user due for revocation", got)
			}
			if len(meshSvc.applied) != 0 {
				t.Fatalf("applied %d events despite peer reconnect", len(meshSvc.applied))
			}
			if _, tracked := ub.absentSince[user]; tracked {
				t.Fatal("absence timer not cleared on peer reconnect")
			}

			// The refreshed connection also prevents the next sweep from
			// restarting the timer using the old cached absence.
			ub.sweep(context.Background(), now.Add(miaSweepInterval))
			if _, tracked := ub.absentSince[user]; tracked {
				t.Fatal("absence timer restarted after peer reconnect")
			}
			if len(meshSvc.queries) != tt.wantQueries {
				t.Fatal("extra query within the regular refresh interval")
			}
		})
	}
}

func TestSweepRechecksAccountStatus(t *testing.T) {
	user, other := tAcctID(1), tAcctID(2)
	healthy := &account.Reputation{BondedTier: 1}
	penalized := &account.Reputation{BondedTier: 1, Penalties: 1}
	tests := []struct {
		name                string
		before, after       tAccountStatus
		useCachedPeerAnswer bool
		wantRevoke          meshevents.OrderRevokeReason
	}{
		{
			name:   "local reconnect during regular query",
			before: tAccountStatus{rep: healthy}, after: tAccountStatus{connected: true, rep: healthy},
		},
		{
			name:   "local reconnect during final query",
			before: tAccountStatus{rep: healthy}, after: tAccountStatus{connected: true, rep: healthy},
			useCachedPeerAnswer: true,
		},
		{
			name:   "still disconnected",
			before: tAccountStatus{rep: healthy}, after: tAccountStatus{rep: healthy},
			wantRevoke: meshevents.OrderRevokeReasonDisconnected,
		},
		{
			name:   "trading tier restored",
			before: tAccountStatus{rep: penalized}, after: tAccountStatus{rep: healthy},
		},
		{
			name:   "reputation unavailable",
			before: tAccountStatus{rep: penalized}, after: tAccountStatus{err: errors.New("db down")},
		},
		{
			name:   "still penalized",
			before: tAccountStatus{rep: penalized}, after: tAccountStatus{rep: penalized},
			wantRevoke: meshevents.OrderRevokeReasonPenalty,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The other account ensures a peer query runs even when the
			// account under test is already selected for penalty revocation.
			books := map[string]bookedUserSource{
				"dcr_btc": &tBookSource{users: map[account.AccountID]int{user: 1, other: 1}},
			}
			ub, meshSvc, accounts := newTestUnbooker(books, time.Minute)
			accounts[user] = tt.before
			now := time.Now()
			ub.absentSince[user] = now.Add(-ub.timeout)
			if tt.useCachedPeerAnswer {
				ub.lastQuery = now.Add(-miaSweepInterval)
			}
			meshSvc.onQuery = func() { accounts[user] = tt.after }

			ub.sweep(context.Background(), now)
			if len(meshSvc.queries) != 1 {
				t.Fatalf("peer queries = %d, want 1", len(meshSvc.queries))
			}
			if tt.wantRevoke == 0 {
				if len(meshSvc.applied) != 0 {
					t.Fatalf("applied %d events after status changed", len(meshSvc.applied))
				}
			} else if len(meshSvc.applied) != 1 {
				t.Fatalf("applied %d events, want 1", len(meshSvc.applied))
			} else if got, reason := decodeUserRevoke(t, meshSvc.applied[0]); got != user || reason != tt.wantRevoke {
				t.Fatalf("revoked (%v, reason %d), want (%v, reason %d)", got, reason, user, tt.wantRevoke)
			}
			if _, tracked := ub.absentSince[user]; tracked {
				t.Fatal("absence timer retained after reconnect or revocation decision")
			}
		})
	}
}

func TestRunMasterSkipsPromotionQuery(t *testing.T) {
	miaUser := tAcctID(1)
	books := map[string]bookedUserSource{
		"dcr_btc": &tBookSource{users: map[account.AccountID]int{miaUser: 1}},
	}
	ub, meshSvc, _ := newTestUnbooker(books, time.Minute)

	// Stop after the initial sweep reports readiness.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var readyErr error
	ready := false
	ub.runMaster(ctx, func(err error) {
		ready, readyErr = true, err
		cancel()
	})

	if !ready || readyErr != nil {
		t.Fatalf("readiness = %v, %v; want reported with nil error", ready, readyErr)
	}
	// The promotion sweep never blocks readiness on the peer: no query, but
	// the absence timer is started for later sweeps.
	if len(meshSvc.queries) != 0 {
		t.Fatalf("promotion sweep issued %d peer queries, want 0", len(meshSvc.queries))
	}
	if _, tracked := ub.absentSince[miaUser]; !tracked {
		t.Fatal("initial sweep did not start the absence timer")
	}
}

func TestMIASweepRetryOnApplyError(t *testing.T) {
	miaUser := tAcctID(1)
	books := map[string]bookedUserSource{
		"dcr_btc": &tBookSource{users: map[account.AccountID]int{miaUser: 1}},
	}
	timeout := time.Minute
	ub, meshSvc, _ := newTestUnbooker(books, timeout)

	ctx := context.Background()
	now := time.Now()
	ub.sweep(ctx, now)

	// Apply fails: the deadline is retained for a retry.
	meshSvc.applyErr = context.DeadlineExceeded
	ub.sweep(ctx, now.Add(2*timeout))
	if _, tracked := ub.absentSince[miaUser]; !tracked {
		t.Fatal("deadline dropped after failed apply")
	}

	// Next sweep succeeds.
	meshSvc.applyErr = nil
	ub.sweep(ctx, now.Add(3*timeout))
	if len(meshSvc.applied) != 1 {
		t.Fatalf("applied %d events after retry, want 1", len(meshSvc.applied))
	}
	if _, tracked := ub.absentSince[miaUser]; tracked {
		t.Fatal("deadline not cleared after successful retry")
	}
}
