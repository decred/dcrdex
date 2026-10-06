//go:build pgonline

package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/meshevents"
)

func randomReputationOrderID() (oid order.OrderID) {
	copy(oid[:], encode.RandomBytes(32))
	return
}

func midPtr(mid order.MatchID) *order.MatchID { return &mid }

func randomReputationMatchID() (mid order.MatchID) {
	copy(mid[:], encode.RandomBytes(32))
	return
}

func captureRepListener(t *testing.T) *[][]account.AccountID {
	t.Helper()
	var calls [][]account.AccountID
	archie.repListenerMtx.Lock()
	prev := archie.repListener
	archie.repListener = func(users ...account.AccountID) {
		calls = append(calls, append([]account.AccountID(nil), users...))
	}
	archie.repListenerMtx.Unlock()
	t.Cleanup(func() {
		archie.repListenerMtx.Lock()
		archie.repListener = prev
		archie.repListenerMtx.Unlock()
	})
	return &calls
}

func requireRepListenerCall(t *testing.T, calls [][]account.AccountID, wantCalls int, wantUsers ...account.AccountID) {
	t.Helper()
	if len(calls) != wantCalls {
		t.Fatalf("listener called %d times, want %d: %v", len(calls), wantCalls, calls)
	}
	if wantCalls == 0 {
		return
	}
	last := calls[len(calls)-1]
	if len(last) != len(wantUsers) {
		t.Fatalf("last notification = %v, want users %v", last, wantUsers)
	}
	notified := make(map[account.AccountID]bool, len(last))
	for _, user := range last {
		notified[user] = true
	}
	for _, user := range wantUsers {
		if !notified[user] {
			t.Fatalf("last notification = %v, missing user %v", last, user)
		}
	}
}

func seedReputationOutcome(t *testing.T, ctx context.Context, user account.AccountID, link [32]byte, class db.OutcomeClass, outcome db.Outcome) {
	t.Helper()
	stmt := "INSERT INTO " + archie.tables.points + " (account, link, class, outcome) VALUES ($1, $2, $3, $4)"
	if _, err := archie.db.ExecContext(ctx, stmt, user, order.OrderID(link), class, outcome); err != nil {
		t.Fatalf("insert reputation outcome: %v", err)
	}
}

func TestApplyReputationForgivenEvent(t *testing.T) {
	for _, tt := range []struct {
		name         string
		scope        meshevents.ReputationForgivenessScope
		active       bool
		missing      bool
		wrongTip     bool
		wantForgiven []bool // One result per application, including repeats.
	}{
		{name: "user", scope: meshevents.ReputationForgivenessScopeUser, wantForgiven: []bool{true, false}},
		{name: "match", scope: meshevents.ReputationForgivenessScopeMatch, wantForgiven: []bool{true, true}},
		{name: "active match", scope: meshevents.ReputationForgivenessScopeMatch, active: true, wantForgiven: []bool{false}},
		{name: "unknown match", scope: meshevents.ReputationForgivenessScopeMatch, missing: true, wantForgiven: []bool{false}},
		{name: "user rollback", scope: meshevents.ReputationForgivenessScopeUser, wrongTip: true, wantForgiven: []bool{false}},
		{name: "match rollback", scope: meshevents.ReputationForgivenessScopeMatch, wrongTip: true, wantForgiven: []bool{false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}
			ctx := context.Background()
			user, otherUser := randomAccountID(), randomAccountID()
			mid := db.MarketMatchID{MatchID: randomReputationMatchID()}
			if !tt.missing {
				pair := generateMatch(t, order.NewlyMatched, tt.active, user, otherUser)
				mid = testMarketMatchID(pair.match)
			}

			// Seed a penalty and success in each class, plus another failed match
			// that match-scoped forgiveness must leave alone.
			seedReputationOutcome(t, ctx, user, randomReputationOrderID(), db.OutcomeClassPreimage, db.OutcomePreimageMiss)
			seedReputationOutcome(t, ctx, user, randomReputationOrderID(), db.OutcomeClassPreimage, db.OutcomePreimageSuccess)
			seedReputationOutcome(t, ctx, user, mid.MatchID, db.OutcomeClassMatch, db.OutcomeNoSwapAsMaker)
			seedReputationOutcome(t, ctx, user, randomReputationMatchID(), db.OutcomeClassMatch, db.OutcomeSwapSuccess)
			seedReputationOutcome(t, ctx, user, randomReputationMatchID(), db.OutcomeClassMatch, db.OutcomeNoRedeemAsMaker)
			seedReputationOutcome(t, ctx, user, randomReputationOrderID(), db.OutcomeClassOrder, db.OutcomeOrderCanceled)
			seedReputationOutcome(t, ctx, user, randomReputationOrderID(), db.OutcomeClassOrder, db.OutcomeOrderComplete)
			beforePreimages, beforeMatches, beforeOrders, err := archie.GetUserReputationData(ctx, user, 10, 10, 10)
			if err != nil {
				t.Fatalf("GetUserReputationData: %v", err)
			}
			if len(beforePreimages) != 2 || len(beforeMatches) != 3 || len(beforeOrders) != 2 {
				t.Fatal("unexpected seeded reputation outcomes")
			}

			// The other account shares these links, so deleting by link alone
			// would incorrectly remove its penalties too.
			seedReputationOutcome(t, ctx, otherUser, beforePreimages[0].OrderID, db.OutcomeClassPreimage, db.OutcomePreimageMiss)
			seedReputationOutcome(t, ctx, otherUser, mid.MatchID, db.OutcomeClassMatch, db.OutcomeNoSwapAsTaker)
			seedReputationOutcome(t, ctx, otherUser, beforeOrders[0].OrderID, db.OutcomeClassOrder, db.OutcomeOrderCanceled)
			otherPreimages, otherMatches, otherOrders, err := archie.GetUserReputationData(ctx, otherUser, 10, 10, 10)
			if err != nil {
				t.Fatalf("GetUserReputationData for other account: %v", err)
			}

			calls := captureRepListener(t)
			update := &meshevents.ReputationForgivenEvent{AccountID: user, Scope: tt.scope}
			if tt.scope == meshevents.ReputationForgivenessScopeMatch {
				update.MatchID = midPtr(mid.MatchID)
			}
			wantPreimages, wantMatches, wantOrders := beforePreimages, beforeMatches, beforeOrders
			var tip []byte
			for i, wantForgiven := range tt.wantForgiven {
				seq := uint64(i + 1)
				event := []byte(tt.name)
				wantTip := testEventApplyTip(t, tip, seq, meshevents.EventKindReputationForgiven, event, update)
				meta := &db.EventLogMeta{Seq: seq, Event: event, ExpectedTipHash: wantTip}
				if tt.wrongTip {
					meta.ExpectedTipHash = wrongEventTip()
				}
				result, err := archie.ApplyReputationForgivenEvent(ctx, meta, update)
				if tt.wrongTip {
					var divergence *db.EventLogDivergenceError
					if !errors.As(err, &divergence) || result != nil {
						t.Fatalf("result = %+v, error = %v, want nil result and divergence", result, err)
					}
					assertEventLogFrontier(t, ctx, 0, nil)
					requireRepListenerCall(t, *calls, 0)
				} else {
					if err != nil {
						t.Fatalf("application %d: %v", seq, err)
					}
					if result == nil || result.Forgiven != wantForgiven {
						t.Fatalf("application %d: result = %+v, want forgiven %v", seq, result, wantForgiven)
					}
					requireEventApplyLog(t, result.Log, seq, meshevents.EventKindReputationForgiven, event, wantTip, update)
					assertEventLogFrontier(t, ctx, seq, wantTip)
					requireRepListenerCall(t, *calls, i+1, user)
					tip = wantTip

					if wantForgiven {
						if tt.scope == meshevents.ReputationForgivenessScopeUser {
							// User forgiveness keeps only the success in each class.
							wantPreimages = beforePreimages[1:]
							wantMatches = beforeMatches[1:2]
							wantOrders = beforeOrders[1:]
						} else {
							// Match forgiveness removes only the targeted failure.
							wantMatches = beforeMatches[1:]
						}
					}
				}

				preimages, matches, orders, err := archie.GetUserReputationData(ctx, user, 10, 10, 10)
				if err != nil {
					t.Fatalf("GetUserReputationData after application %d: %v", seq, err)
				}
				if !reflect.DeepEqual(preimages, wantPreimages) || !reflect.DeepEqual(matches, wantMatches) || !reflect.DeepEqual(orders, wantOrders) {
					t.Fatalf("unexpected reputation outcomes after application %d", seq)
				}
				preimages, matches, orders, err = archie.GetUserReputationData(ctx, otherUser, 10, 10, 10)
				if err != nil {
					t.Fatalf("GetUserReputationData for other account: %v", err)
				}
				if !reflect.DeepEqual(preimages, otherPreimages) || !reflect.DeepEqual(matches, otherMatches) || !reflect.DeepEqual(orders, otherOrders) {
					t.Fatalf("other account's reputation changed after application %d", seq)
				}
				if !tt.missing {
					matchForgiven := tt.scope == meshevents.ReputationForgivenessScopeMatch && !tt.wrongTip && wantForgiven
					requireMatchForgiven(t, ctx, mid, matchForgiven)
					if matchForgiven {
						fails, err := archie.UserMatchFails(user, 10)
						if err != nil {
							t.Fatalf("UserMatchFails: %v", err)
						}
						if len(fails) != 0 {
							t.Fatalf("UserMatchFails = %+v, want none", fails)
						}
					}
				}
			}
		})
	}
}

func requireMatchForgiven(t *testing.T, ctx context.Context, mid db.MarketMatchID, want bool) {
	t.Helper()
	schema, err := archie.marketSchema(mid.Base, mid.Quote)
	if err != nil {
		t.Fatalf("marketSchema: %v", err)
	}
	var forgiven bool
	stmt := "SELECT COALESCE(forgiven, FALSE) FROM " + fullMatchesTableName(archie.dbName, schema) + " WHERE matchid = $1"
	if err := archie.db.QueryRowContext(ctx, stmt, mid.MatchID).Scan(&forgiven); err != nil {
		t.Fatalf("query forgiven status: %v", err)
	}
	if forgiven != want {
		t.Fatalf("match forgiven = %v, want %v", forgiven, want)
	}
}

func TestApplyRepEventTx(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
	ctx := context.Background()
	calls := captureRepListener(t)
	userA, userB := randomAccountID(), randomAccountID()
	userBPreimage, userBOrder := randomReputationOrderID(), randomReputationOrderID()
	policy := &db.ReputationOutcomePolicy{PreimageLimit: 1, OrderLimit: 1}

	// A failed callback must not record the batch or notify listeners.
	applyErr := errors.New("apply failed")
	_, err := archie.applyRepEventTx(ctx, &db.EventLogMeta{Event: []byte("rep-listener-fail")},
		"test_reputation", []byte("rep-listener-fail-tx"), policy,
		func(_ *sql.Tx, batch *reputationOutcomeBatch) error {
			batch.orders = []*reputationOrderOutcome{{user: userA, oid: randomReputationOrderID()}}
			return applyErr
		})
	if !errors.Is(err, applyErr) {
		t.Fatalf("applyRepEventTx error = %v, want %v", err, applyErr)
	}
	requireRepListenerCall(t, *calls, 0)

	_, err = archie.applyRepEventTx(ctx, &db.EventLogMeta{Event: []byte("rep-listener-commit")},
		"test_reputation", []byte("rep-listener-commit-tx"), policy,
		func(_ *sql.Tx, batch *reputationOutcomeBatch) error {
			batch.preimages = []*reputationPreimageOutcome{
				{user: userA, oid: randomReputationOrderID(), miss: true},
				{user: userB, oid: userBPreimage},
			}
			batch.orders = []*reputationOrderOutcome{
				{user: userA, oid: randomReputationOrderID()},
				{user: userB, oid: userBOrder},
			}
			return nil
		})
	if err != nil {
		t.Fatalf("applyRepEventTx error: %v", err)
	}
	requireRepListenerCall(t, *calls, 1, userA, userB)

	preimages, matches, orders, err := archie.GetUserReputationData(ctx, userA, 10, 10, 10)
	if err != nil {
		t.Fatalf("GetUserReputationData error: %v", err)
	}
	if len(preimages) != 1 || len(orders) != 1 || len(matches) != 0 {
		t.Fatalf("userA outcomes preimages=%d orders=%d matches=%d, want 1/1/0", len(preimages), len(orders), len(matches))
	}

	// Retain the newest outcomes for userA without pruning userB's outcomes.
	keepPreimage, keepOrder := randomReputationOrderID(), randomReputationOrderID()
	_, err = archie.applyRepEventTx(ctx, &db.EventLogMeta{Event: []byte("rep-prune")},
		"test_reputation", []byte("rep-prune-tx"), policy,
		func(_ *sql.Tx, batch *reputationOutcomeBatch) error {
			batch.preimages = []*reputationPreimageOutcome{
				{user: userA, oid: randomReputationOrderID(), miss: true},
				{user: userA, oid: keepPreimage},
			}
			batch.orders = []*reputationOrderOutcome{
				{user: userA, oid: randomReputationOrderID(), penalizedCancel: true},
				{user: userA, oid: keepOrder},
			}
			return nil
		})
	if err != nil {
		t.Fatalf("applyRepEventTx prune: %v", err)
	}
	requireRepListenerCall(t, *calls, 2, userA)
	for _, want := range []struct {
		user                     account.AccountID
		preimageOrderID, orderID order.OrderID
	}{
		{userA, keepPreimage, keepOrder},
		{userB, userBPreimage, userBOrder},
	} {
		// Request more than the retention limit so read-side trimming cannot hide a failure to prune.
		preimages, _, orders, err := archie.GetUserReputationData(ctx, want.user, 10, 10, 10)
		if err != nil {
			t.Fatalf("GetUserReputationData: %v", err)
		}
		if len(preimages) != 1 || preimages[0].OrderID != want.preimageOrderID || preimages[0].Miss {
			t.Fatalf("user %v preimages = %+v, want success %v", want.user, preimages, want.preimageOrderID)
		}
		if len(orders) != 1 || orders[0].OrderID != want.orderID || orders[0].Canceled {
			t.Fatalf("user %v orders = %+v, want completion %v", want.user, orders, want.orderID)
		}
	}

	// Empty batch: committed, no notify.
	_, err = archie.applyRepEventTx(ctx, &db.EventLogMeta{Event: []byte("rep-listener-empty")},
		"test_reputation", []byte("rep-listener-empty-tx"), policy,
		func(_ *sql.Tx, batch *reputationOutcomeBatch) error { return nil })
	if err != nil {
		t.Fatalf("applyRepEventTx empty error: %v", err)
	}
	requireRepListenerCall(t, *calls, 2, userA)
}

// TestApplyRepEventTxCancellation checks that canceling a blocked reputation
// insert returns a cancellation error without marking the database backend as
// failed, committing an event-log entry, or notifying reputation listeners.
func TestApplyRepEventTxCancellation(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
	calls := captureRepListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Hold the points table lock so insertion waits until cancellation.
	lockTx, err := archie.db.Begin()
	if err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	defer lockTx.Rollback()
	if _, err := lockTx.Exec(fmt.Sprintf("LOCK TABLE %s IN ACCESS EXCLUSIVE MODE", archie.tables.points)); err != nil {
		t.Fatalf("lock points table: %v", err)
	}

	user := randomAccountID()
	_, err = archie.applyRepEventTx(ctx, &db.EventLogMeta{Event: []byte("rep-canceled")},
		"test_reputation", []byte("rep-canceled-tx"), &db.ReputationOutcomePolicy{OrderLimit: 1},
		func(_ *sql.Tx, batch *reputationOutcomeBatch) error {
			batch.orders = []*reputationOrderOutcome{{user: user, oid: randomReputationOrderID()}}
			timer := time.AfterFunc(100*time.Millisecond, cancel)
			t.Cleanup(func() { timer.Stop() })
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("applyRepEventTx error = %v, want context.Canceled", err)
	}
	if err := archie.LastErr(); err != nil {
		t.Fatalf("cancellation marked backend failed: %v", err)
	}
	requireRepListenerCall(t, *calls, 0)
	assertEventLogFrontier(t, context.Background(), 0, nil)
}
