//go:build pgonline

package pg

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/meshevents"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var tPubKey = []byte{
	0x02, 0x04, 0x98, 0x8a, 0x49, 0x8d, 0x5d, 0x19, 0x51, 0x4b, 0x21, 0x7e, 0x87,
	0x2b, 0x4d, 0xbd, 0x1c, 0xf0, 0x71, 0xd3, 0x65, 0xc4, 0x87, 0x9e, 0x64, 0xed,
	0x59, 0x19, 0x88, 0x1c, 0x97, 0xeb, 0x19,
}

var tAcctID = account.AccountID{
	0x0a, 0x99, 0x12, 0x20, 0x5b, 0x2c, 0xba, 0xb0, 0xc2, 0x5c, 0x2d, 0xe3, 0x0b,
	0xda, 0x90, 0x74, 0xde, 0x0a, 0xe2, 0x3b, 0x06, 0x54, 0x89, 0xa9, 0x91, 0x99,
	0xba, 0xd7, 0x63, 0xf1, 0x02, 0xcc,
}

func tNewAccount(t *testing.T) *account.Account {
	acct, err := account.NewAccountFromPubKey(tPubKey)
	if err != nil {
		t.Fatalf("error creating account from pubkey: %v", err)
	}
	if acct.ID != tAcctID {
		t.Fatalf("unexpected account ID. wanted %x, got %x", tAcctID, acct.ID)
	}
	return acct
}

func tAccountFromPrivKey(t *testing.T, keyByte byte) *account.Account {
	t.Helper()

	var priv [account.PrivKeySize]byte
	priv[len(priv)-1] = keyByte
	acct, err := account.NewAccountFromPubKey(secp256k1.PrivKeyFromBytes(priv[:]).PubKey().SerializeCompressed())
	if err != nil {
		t.Fatalf("error creating account from privkey byte %d: %v", keyByte, err)
	}
	return acct
}

func tAssertBond(t *testing.T, got, want *db.Bond) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bond = %+v, want %+v", got, want)
	}
}

func tBondPostedEvent(acct *account.Account, bond *db.Bond) *meshevents.BondPostedEvent {
	return meshevents.NewBondPostedEvent(acct, &meshevents.Bond{
		Version:  bond.Version,
		AssetID:  bond.AssetID,
		CoinID:   bond.CoinID,
		Amount:   bond.Amount,
		Strength: bond.Strength,
		LockTime: bond.LockTime,
	})
}

func tBondPostedTip(t *testing.T, prevTip []byte, seq uint64, event []byte, acct *account.Account, bond *db.Bond) []byte {
	t.Helper()

	posted := tBondPostedEvent(acct, bond)
	txData, err := posted.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	req, err := newEventLogAppend(&db.EventLogMeta{Seq: seq, Event: event}, meshevents.EventKindBondPosted, txData)
	if err != nil {
		t.Fatalf("newEventLogAppend error: %v", err)
	}
	return eventLogHash(prevTip, seq, req.entry.Kind, req.entry.Event, req.entry.TxData)
}

func tPrepaidBondsCreatedTip(t *testing.T, prevTip []byte, seq uint64, event []byte, created *meshevents.PrepaidBondsCreatedEvent) []byte {
	t.Helper()

	txData, err := created.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	req, err := newEventLogAppend(&db.EventLogMeta{Seq: seq, Event: event}, meshevents.EventKindPrepaidBondsCreated, txData)
	if err != nil {
		t.Fatalf("newEventLogAppend error: %v", err)
	}
	return eventLogHash(prevTip, seq, req.entry.Kind, req.entry.Event, req.entry.TxData)
}

func tApplyPrepaidBondsCreated(t *testing.T, ctx context.Context, event []byte, bonds ...*meshevents.PrepaidBond) *db.EventLogEntry {
	t.Helper()

	created := &meshevents.PrepaidBondsCreatedEvent{Bonds: bonds}
	tip := tPrepaidBondsCreatedTip(t, nil, 1, event, created)
	logEntry, err := archie.ApplyPrepaidBondsCreatedEvent(ctx, &db.EventLogMeta{Event: event}, created)
	if err != nil {
		t.Fatalf("ApplyPrepaidBondsCreatedEvent error: %v", err)
	}
	if logEntry.Seq != 1 || logEntry.Kind != meshevents.EventKindPrepaidBondsCreated || !bytes.Equal(logEntry.TipHash, tip) {
		t.Fatalf("prepaid created log = %+v, want seq 1 kind %q tip %x",
			logEntry, meshevents.EventKindPrepaidBondsCreated, tip)
	}
	return logEntry
}

func requireEventFrontier(t *testing.T, ctx context.Context, seq uint64, tip []byte) {
	t.Helper()

	frontier, err := archie.EventLogFrontier(ctx)
	if err != nil {
		t.Fatalf("EventLogFrontier error: %v", err)
	}
	if frontier.Seq != seq || !bytes.Equal(frontier.TipHash, tip) {
		t.Fatalf("frontier = (%d, %x), want (%d, %x)", frontier.Seq, frontier.TipHash, seq, tip)
	}
}

func TestApplyBondPostedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	calls := captureRepListener(t)
	acct := tNewAccount(t)
	bond := &db.Bond{
		Version:  1,
		AssetID:  AssetDCR,
		CoinID:   []byte("apply-bond-posted-coin"),
		Amount:   5_0000_0000,
		Strength: 5,
		LockTime: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}
	posted := tBondPostedEvent(acct, bond)
	// Only the latest outcome in each class should be returned.
	seedReputationOutcome(t, ctx, acct.ID, randomReputationOrderID(), db.OutcomeClassPreimage, db.OutcomePreimageSuccess)
	seedReputationOutcome(t, ctx, acct.ID, randomReputationMatchID(), db.OutcomeClassMatch, db.OutcomeSwapSuccess)
	seedReputationOutcome(t, ctx, acct.ID, randomReputationOrderID(), db.OutcomeClassOrder, db.OutcomeOrderComplete)
	preimageID := randomReputationOrderID()
	matchID := randomReputationMatchID()
	orderID := randomReputationOrderID()
	seedReputationOutcome(t, ctx, acct.ID, preimageID, db.OutcomeClassPreimage, db.OutcomePreimageMiss)
	seedReputationOutcome(t, ctx, acct.ID, matchID, db.OutcomeClassMatch, db.OutcomeNoSwapAsTaker)
	seedReputationOutcome(t, ctx, acct.ID, orderID, db.OutcomeClassOrder, db.OutcomeOrderCanceled)
	seedReputationOutcome(t, ctx, tAccountFromPrivKey(t, 3).ID, orderID, db.OutcomeClassOrder, db.OutcomeOrderComplete)

	applyBondPosted := func(meta *db.EventLogMeta) *db.BondPostedResult {
		t.Helper()

		res, err := archie.ApplyBondPostedEvent(ctx, meta, posted, 1, 1, 1)
		if err != nil {
			t.Fatalf("ApplyBondPostedEvent error: %v", err)
		}
		if res == nil || res.Log == nil {
			t.Fatalf("ApplyBondPostedEvent returned nil result or log: %+v", res)
		}
		if len(res.Bonds) != 1 {
			t.Fatalf("returned %d bonds, want 1", len(res.Bonds))
		}
		tAssertBond(t, res.Bonds[0], bond)
		if len(res.Preimages) != 1 || res.Preimages[0].OrderID != preimageID || !res.Preimages[0].Miss {
			t.Fatalf("unexpected returned preimage outcomes: %+v", res.Preimages)
		}
		if len(res.Matches) != 1 || res.Matches[0].MatchID != matchID || res.Matches[0].MatchOutcome != db.OutcomeNoSwapAsTaker {
			t.Fatalf("unexpected returned match outcomes: %+v", res.Matches)
		}
		if len(res.Orders) != 1 || res.Orders[0].OrderID != orderID || !res.Orders[0].Canceled {
			t.Fatalf("unexpected returned order outcomes: %+v", res.Orders)
		}
		return res
	}

	requireLog := func(res *db.BondPostedResult, seq uint64, event, tip []byte) {
		t.Helper()

		wantTxData, err := posted.EventTxData()
		if err != nil {
			t.Fatalf("EventTxData error: %v", err)
		}
		if res.Log.Seq != seq || res.Log.Kind != meshevents.EventKindBondPosted || !bytes.Equal(res.Log.Event, event) {
			t.Fatalf("log = %+v, want seq %d kind %q event %q", res.Log, seq, meshevents.EventKindBondPosted, event)
		}
		if !bytes.Equal(res.Log.TipHash, tip) {
			t.Fatalf("log tip = %x, want %x", res.Log.TipHash, tip)
		}
		if !bytes.Equal(res.Log.TxData, wantTxData) {
			t.Fatalf("tx data = %x, want %x", res.Log.TxData, wantTxData)
		}
	}

	requireStoredBond := func() {
		t.Helper()

		storedAcct, bonds, err := archie.Account(ctx, acct.ID, time.Unix(bond.LockTime-1, 0))
		if err != nil {
			t.Fatalf("Account error: %v", err)
		}
		if storedAcct == nil {
			t.Fatalf("account %v was not stored", acct.ID)
		}
		if !bytes.Equal(storedAcct.PubKey.SerializeCompressed(), acct.PubKey.SerializeCompressed()) {
			t.Fatalf("stored pubkey = %x, want %x", storedAcct.PubKey.SerializeCompressed(), acct.PubKey.SerializeCompressed())
		}
		if len(bonds) != 1 {
			t.Fatalf("stored %d bonds, want 1", len(bonds))
		}
		tAssertBond(t, bonds[0], bond)
	}

	// A new bond creates the account if necessary, stores the bond, and appends
	// the first bond_posted event log entry.
	firstEvent := []byte("bond-posted-event")
	firstTip := tBondPostedTip(t, nil, 1, firstEvent, acct, bond)
	firstRes := applyBondPosted(&db.EventLogMeta{Event: firstEvent})
	if !firstRes.BondAdded {
		t.Fatalf("BondAdded = false, want true")
	}
	requireLog(firstRes, 1, firstEvent, firstTip)
	requireStoredBond()
	requireRepListenerCall(t, *calls, 1, acct.ID)

	// Reapplying the same account/bond is idempotent for the bond table, but
	// the replicated event is still recorded in the event log.
	duplicateEvent := []byte("bond-posted-duplicate")
	duplicateTip := tBondPostedTip(t, firstRes.Log.TipHash, 2, duplicateEvent, acct, bond)
	duplicateRes := applyBondPosted(&db.EventLogMeta{
		Seq:             2,
		Event:           duplicateEvent,
		ExpectedTipHash: duplicateTip,
	})
	if duplicateRes.BondAdded {
		t.Fatalf("duplicate BondAdded = true, want false")
	}
	requireLog(duplicateRes, 2, duplicateEvent, duplicateTip)
	requireStoredBond()
	requireRepListenerCall(t, *calls, 2, acct.ID) // duplicate commit still notifies

	// A bond coin already associated with this account cannot be replayed for a
	// different account, and the rejected apply must not advance the log.
	otherAcct := tAccountFromPrivKey(t, 2)
	_, err := archie.ApplyBondPostedEvent(ctx, &db.EventLogMeta{
		Seq:   3,
		Event: []byte("bond-posted-conflict"),
	}, tBondPostedEvent(otherAcct, bond), 10, 10, 10)
	if err == nil {
		t.Fatalf("conflicting ApplyBondPostedEvent succeeded")
	}
	storedOtherAcct, otherBonds, err := archie.Account(ctx, otherAcct.ID, time.Unix(bond.LockTime-1, 0))
	if err != nil {
		t.Fatalf("Account error: %v", err)
	}
	if storedOtherAcct != nil || len(otherBonds) != 0 {
		t.Fatalf("conflicting apply stored account %v bonds %v", storedOtherAcct, otherBonds)
	}
	frontier, err := archie.EventLogFrontier(ctx)
	if err != nil {
		t.Fatalf("EventLogFrontier error: %v", err)
	}
	if frontier.Seq != 2 || !bytes.Equal(frontier.TipHash, duplicateRes.Log.TipHash) {
		t.Fatalf("frontier = (%d, %x), want (2, %x)", frontier.Seq, frontier.TipHash, duplicateRes.Log.TipHash)
	}
	requireRepListenerCall(t, *calls, 2, acct.ID) // rejected apply: no notify
}

func TestApplyPrepaidBondsCreatedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	created := &meshevents.PrepaidBondsCreatedEvent{
		Bonds: []*meshevents.PrepaidBond{
			{
				CoinID:   []byte("prepaid-create-one"),
				Strength: 2,
				LockTime: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			},
			{
				CoinID:   []byte("prepaid-create-two"),
				Strength: 4,
				LockTime: time.Date(2100, 2, 1, 0, 0, 0, 0, time.UTC).Unix(),
			},
		},
	}
	event := []byte("prepaid-bonds-created-event")
	tip := tPrepaidBondsCreatedTip(t, nil, 1, event, created)

	logEntry, err := archie.ApplyPrepaidBondsCreatedEvent(ctx, &db.EventLogMeta{Event: event}, created)
	if err != nil {
		t.Fatalf("ApplyPrepaidBondsCreatedEvent error: %v", err)
	}
	if logEntry.Seq != 1 || logEntry.Kind != meshevents.EventKindPrepaidBondsCreated ||
		!bytes.Equal(logEntry.Event, event) || !bytes.Equal(logEntry.TipHash, tip) {
		t.Fatalf("log entry = %+v, want seq 1 kind %q event %q tip %x",
			logEntry, meshevents.EventKindPrepaidBondsCreated, event, tip)
	}
	wantTxData, err := created.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	if !bytes.Equal(logEntry.TxData, wantTxData) {
		t.Fatalf("tx data = %x, want %x", logEntry.TxData, wantTxData)
	}
	for _, bond := range created.Bonds {
		strength, lockTime, err := archie.FetchPrepaidBond(bond.CoinID)
		if err != nil {
			t.Fatalf("FetchPrepaidBond %x error: %v", bond.CoinID, err)
		}
		if strength != bond.Strength || lockTime != bond.LockTime {
			t.Fatalf("stored prepaid bond %x = (%d, %d), want (%d, %d)",
				bond.CoinID, strength, lockTime, bond.Strength, bond.LockTime)
		}
	}
}

func TestApplyBondPostedEventPrepaid(t *testing.T) {
	t.Run("redeem and retry", func(t *testing.T) {
		if err := cleanTables(archie.db); err != nil {
			t.Fatalf("cleanTables: %v", err)
		}

		ctx := context.Background()
		acct := tNewAccount(t)
		coinID := []byte("prepaid-posted-coin")
		bond := &db.Bond{
			Version:  0,
			AssetID:  account.PrepaidBondID,
			CoinID:   coinID,
			Amount:   0,
			Strength: 3,
			LockTime: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		}
		createdLog := tApplyPrepaidBondsCreated(t, ctx, []byte("prepaid-created-for-posted-event"), &meshevents.PrepaidBond{
			CoinID:   coinID,
			Strength: bond.Strength,
			LockTime: bond.LockTime,
		})

		event := []byte("prepaid-bond-posted-event")
		tip := tBondPostedTip(t, createdLog.TipHash, 2, event, acct, bond)
		res, err := archie.ApplyBondPostedEvent(ctx, &db.EventLogMeta{
			Seq:             2,
			Event:           event,
			ExpectedTipHash: tip,
		}, tBondPostedEvent(acct, bond), 10, 10, 10)
		if err != nil {
			t.Fatalf("ApplyBondPostedEvent prepaid error: %v", err)
		}
		if res == nil || res.Log == nil || !res.BondAdded {
			t.Fatalf("prepaid apply result = %+v, want added log", res)
		}
		if res.Log.Seq != 2 || res.Log.Kind != meshevents.EventKindBondPosted || !bytes.Equal(res.Log.TipHash, tip) {
			t.Fatalf("prepaid apply log = %+v, want seq 2 kind %q tip %x", res.Log, meshevents.EventKindBondPosted, tip)
		}
		storedAcct, bonds, err := archie.Account(ctx, acct.ID, time.Unix(bond.LockTime-1, 0))
		if err != nil {
			t.Fatalf("Account error: %v", err)
		}
		if storedAcct == nil || len(bonds) != 1 {
			t.Fatalf("stored prepaid account %v bonds %v, want one bond", storedAcct, bonds)
		}
		tAssertBond(t, bonds[0], bond)
		if _, _, err := archie.FetchPrepaidBond(coinID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("consumed pre-paid bond lookup error = %v, want sql.ErrNoRows", err)
		}

		duplicateEvent := []byte("prepaid-bond-posted-duplicate")
		duplicateTip := tBondPostedTip(t, res.Log.TipHash, 3, duplicateEvent, acct, bond)
		duplicateRes, err := archie.ApplyBondPostedEvent(ctx, &db.EventLogMeta{
			Seq:             3,
			Event:           duplicateEvent,
			ExpectedTipHash: duplicateTip,
		}, tBondPostedEvent(acct, bond), 10, 10, 10)
		if err != nil {
			t.Fatalf("duplicate prepaid ApplyBondPostedEvent error: %v", err)
		}
		if duplicateRes.BondAdded {
			t.Fatalf("duplicate prepaid BondAdded = true, want false")
		}
		requireEventFrontier(t, ctx, 3, duplicateRes.Log.TipHash)
	})

	tests := []struct {
		name                  string
		missingToken          bool
		strengthDelta         uint32
		lockTimeDelta         int64
		reputationReadFailure bool
	}{
		{name: "missing token", missingToken: true},
		{name: "strength mismatch", strengthDelta: 1},
		{name: "lock time mismatch", lockTimeDelta: 86400},
		{name: "reputation read fails", reputationReadFailure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}

			ctx := context.Background()
			acct := tNewAccount(t)
			bond := &db.Bond{
				AssetID:  account.PrepaidBondID,
				CoinID:   []byte("prepaid-rejected-coin"),
				Strength: 3,
				LockTime: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			}
			token := &meshevents.PrepaidBond{
				CoinID:   bond.CoinID,
				Strength: bond.Strength + tt.strengthDelta,
				LockTime: bond.LockTime + tt.lockTimeDelta,
			}
			var priorLog *db.EventLogEntry
			if !tt.missingToken {
				priorLog = tApplyPrepaidBondsCreated(t, ctx, []byte("prepaid-created-"+tt.name), token)
			}

			if tt.reputationReadFailure {
				// Fail the read after the account, bond, and token changes have been made.
				stmt, err := archie.db.Prepare("SELECT $1::bytea")
				if err != nil {
					t.Fatal(err)
				}
				stmt.Close()
				original := archie.queries.selectPoints
				archie.queries.selectPoints = stmt
				t.Cleanup(func() { archie.queries.selectPoints = original })
			}

			_, err := archie.ApplyBondPostedEvent(ctx, &db.EventLogMeta{Event: []byte(tt.name)}, tBondPostedEvent(acct, bond), 10, 10, 10)
			if err == nil {
				t.Fatalf("ApplyBondPostedEvent %s succeeded", tt.name)
			}
			if priorLog == nil {
				requireEventFrontier(t, ctx, 0, nil)
			} else {
				requireEventFrontier(t, ctx, priorLog.Seq, priorLog.TipHash)
			}

			storedAcct, bonds, err := archie.Account(ctx, acct.ID, time.Unix(bond.LockTime-1, 0))
			if err != nil {
				t.Fatalf("Account error: %v", err)
			}
			if storedAcct != nil || len(bonds) != 0 {
				t.Fatalf("rejected prepaid apply stored account %v bonds %v", storedAcct, bonds)
			}
			if !tt.missingToken {
				strength, lockTime, err := archie.FetchPrepaidBond(token.CoinID)
				if err != nil {
					t.Fatalf("rejected prepaid apply consumed token: %v", err)
				}
				if strength != token.Strength || lockTime != token.LockTime {
					t.Fatalf("stored token after rejection = (%d, %d), want (%d, %d)",
						strength, lockTime, token.Strength, token.LockTime)
				}
			}
		})
	}
}
