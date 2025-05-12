//go:build pgonline

package pg

import (
	"context"
	"fmt"
	"testing"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

func TestReputation(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acct := tNewAccount(t)
	user := acct.ID

	if err := archie.CreateAccountWithBond(acct, &db.Bond{}); err != nil {
		t.Fatalf("Error creating account: %v", err)
	}

	// Set reputation version to zero
	query := fmt.Sprintf(internal.UpdateReputationVersion, archie.tables.accounts)
	if _, err := archie.db.ExecContext(ctx, query, 0, user); err != nil {
		t.Fatalf("Error zeroing reputation version: %v", err)
	}

	if ver, err := archie.GetUserReputationVersion(ctx, user); err != nil {
		t.Fatalf("Error retrieving zeroed reputation version: %v", err)
	} else if ver != 0 {
		t.Fatalf("Reputation version not updated. Expected 0, got %d", ver)
	}

	randomOrderID := func() (oid order.OrderID) {
		copy(oid[:], encode.RandomBytes(32))
		return
	}

	pimgs := []*db.PreimageOutcome{
		{OrderID: randomOrderID(), Miss: true},
		{OrderID: randomOrderID(), Miss: false},
		{OrderID: randomOrderID(), Miss: true},
		{OrderID: randomOrderID(), Miss: false},
	}

	randomMatchID := func() (mid order.MatchID) {
		copy(mid[:], encode.RandomBytes(32))
		return
	}

	matches := []*db.MatchResult{
		{MatchID: randomMatchID(), MatchOutcome: db.OutcomeNoRedeemAsMaker},
		{MatchID: randomMatchID(), MatchOutcome: db.OutcomeNoRedeemAsTaker},
		{MatchID: randomMatchID(), MatchOutcome: db.OutcomeNoSwapAsMaker},
		{MatchID: randomMatchID(), MatchOutcome: db.OutcomeSwapSuccess},
	}

	ords := []*db.OrderOutcome{
		{OrderID: randomOrderID(), Canceled: true},
		{OrderID: randomOrderID(), Canceled: false},
		{OrderID: randomOrderID(), Canceled: true},
		{OrderID: randomOrderID(), Canceled: false},
	}

	_, _, _, err := archie.UpgradeUserReputationV1(ctx, user, pimgs, matches, ords)
	if err != nil {
		t.Fatalf("Error upgrading user: %v", err)
	}

	if ver, err := archie.GetUserReputationVersion(ctx, user); err != nil {
		t.Fatalf("Error retrieving updated reputation version: %v", err)
	} else if ver != 1 {
		t.Fatalf("Reputation version not updated. Expected 1, got %d", ver)
	}

	loadedPimgs, loadedMatches, loadedOrds, err := archie.GetUserReputationData(ctx, user, 100, 100, 100)
	if err != nil {
		t.Fatalf("Error loading reputation data: %v", err)
	}

	if len(loadedPimgs) != len(pimgs) {
		t.Fatalf("Wrong number of preimage outcomes loaded. Expected %d, got %d", len(pimgs), len(loadedPimgs))
	}
	for i, pimg := range pimgs {
		loadedPimg := loadedPimgs[i]
		if pimg.DBID == 0 || loadedPimg.DBID != pimg.DBID {
			t.Fatalf("Incorrect DB ID upgraded preimage outcome %d %d", pimg.DBID, loadedPimg.DBID)
		}
		if pimg.OrderID != loadedPimg.OrderID {
			t.Fatalf("Wrong order ID for loaded preimage outcome %d", i)
		}
		if pimg.Miss != loadedPimg.Miss {
			t.Fatalf("Wrong miss value for loaded preimage outcome")
		}
	}

	if len(loadedMatches) != len(matches) {
		t.Fatalf("Wrong number of match outcomes loaded. Expected %d, got %d", len(matches), len(loadedMatches))
	}
	for i, match := range matches {
		loadedMatch := loadedMatches[i]
		if match.DBID == 0 || loadedMatch.DBID != match.DBID {
			t.Fatalf("Incorrect DB ID upgraded match outcome %d %d", match.DBID, loadedMatch.DBID)
		}
		if match.MatchID != loadedMatch.MatchID {
			t.Fatalf("Wrong match ID for loaded match outcome %d", i)
		}
		if match.MatchOutcome != loadedMatch.MatchOutcome {
			t.Fatalf("Wrong outcome value for loaded match outcome")
		}
	}

	if len(loadedOrds) != len(ords) {
		t.Fatalf("Wrong number of order outcomes loaded. Expected %d, got %d", len(ords), len(loadedOrds))
	}

	for i, ord := range ords {
		loadedOrd := loadedOrds[i]
		if ord.DBID == 0 || loadedOrd.DBID != ord.DBID {
			t.Fatalf("Incorrect DB ID upgraded order outcome %d %d", ord.DBID, loadedOrd.DBID)
		}
		if ord.OrderID != loadedOrd.OrderID {
			t.Fatalf("Wrong order ID for loaded order outcome %d", i)
		}
		if ord.Canceled != loadedOrd.Canceled {
			t.Fatalf("Wrong canceled value for loaded order outcome")
		}
	}

	if err := archie.PruneOutcomes(ctx, user, db.OutcomeClassPreimage, pimgs[2].DBID); err != nil {
		t.Fatalf("Error pruning preimage outcomes: %v", err)
	}
	if err := archie.PruneOutcomes(ctx, user, db.OutcomeClassMatch, matches[2].DBID); err != nil {
		t.Fatalf("Error pruning match outcomes: %v", err)
	}
	if err := archie.PruneOutcomes(ctx, user, db.OutcomeClassOrder, ords[2].DBID); err != nil {
		t.Fatalf("Error pruning order outcomes: %v", err)
	}

	loadedPimgs, loadedMatches, loadedOrds, _ = archie.GetUserReputationData(ctx, user, 100, 100, 100)
	if len(loadedPimgs) != 1 || loadedPimgs[0].DBID != pimgs[3].DBID {
		t.Fatal("Pruning preimages failed")
	}
	if len(loadedMatches) != 1 || loadedMatches[0].DBID != matches[3].DBID {
		t.Fatal("Pruning matches failed")
	}
	if len(loadedOrds) != 1 || loadedOrds[0].DBID != ords[3].DBID {
		t.Fatal("Pruning orders failed")
	}

	// Only one success left for each class.
	// Add a fail for each class.

	oid := randomOrderID()
	if pimg, err := archie.AddPreimageOutcome(ctx, user, oid, true); err != nil {
		t.Fatalf("Error adding preimage outcome: %v", err)
	} else if pimg.OrderID != oid || !pimg.Miss || pimg.DBID == 0 {
		t.Fatalf("Bad added preimage outcome return")
	}
	mid := randomMatchID()
	outcome := db.OutcomeNoRedeemAsMaker
	if match, err := archie.AddMatchOutcome(ctx, user, mid, outcome); err != nil {
		t.Fatalf("Error adding match outcome: %v", err)
	} else if match.MatchID != mid || match.MatchOutcome != outcome || match.DBID == 0 {
		t.Fatalf("Bad added match outcome return")
	}
	if ord, err := archie.AddOrderOutcome(ctx, user, oid, true); err != nil {
		t.Fatalf("Error adding order outcome: %v", err)
	} else if ord.OrderID != oid || !ord.Canceled || ord.DBID == 0 {
		t.Fatalf("Bad added order outcome return")
	}

	loadedPimgs, loadedMatches, loadedOrds, _ = archie.GetUserReputationData(ctx, user, 100, 100, 100)
	if len(loadedPimgs) != 2 || len(loadedMatches) != 2 || len(loadedOrds) != 2 {
		t.Fatal("Wrong number of loaded outcomes", len(loadedPimgs), len(loadedMatches), len(loadedOrds))
	}

	if err := archie.ForgiveUser(ctx, user); err != nil {
		t.Fatalf("Error forgiving user: %v", err)
	}

	loadedPimgs, loadedMatches, loadedOrds, _ = archie.GetUserReputationData(ctx, user, 100, 100, 100)
	if len(loadedPimgs) != 1 || len(loadedMatches) != 1 || len(loadedOrds) != 1 {
		t.Fatal("Wrong number of loaded outcomes after forgiveness", len(loadedPimgs), len(loadedMatches), len(loadedOrds))
	}
	if loadedPimgs[0].Miss || loadedMatches[0].MatchOutcome != db.OutcomeSwapSuccess || loadedOrds[0].Canceled {
		t.Fatal("Forgiving didn't forgive", loadedPimgs[0].Miss, loadedMatches[0].MatchOutcome, loadedOrds[0].Canceled)
	}
}
