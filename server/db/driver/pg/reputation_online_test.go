//go:build pgonline

package pg

import (
	"context"
	"errors"
	"reflect"
	"testing"

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
