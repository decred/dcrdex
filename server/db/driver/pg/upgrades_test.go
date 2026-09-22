//go:build pgonline

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

func loadDBSnap(t *testing.T, file string) {
	t.Helper()
	pgSnapGZ, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer pgSnapGZ.Close()

	r, err := gzip.NewReader(pgSnapGZ)
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, strings.TrimSuffix(file, ".gz"))
	dbFile, err := os.Create(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(dbPath)

	_, err = io.Copy(dbFile, r)
	dbFile.Close()
	if err != nil {
		t.Fatal(err)
	}

	var out, stderr bytes.Buffer
	cmd := exec.Command("psql", "-U", PGTestsUser, "-h", PGTestsHost, "-d", PGTestsDBName, "-a", "-f", dbPath)
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v / output: %+v\n: %+v", err, out.String(), stderr.String())
	}
}

func Test_upgradeDB(t *testing.T) {
	ctx := context.Background()

	tryUpgrade := func(gzFile string) error {
		log.Info("start")
		// Get a clean slate.
		err := nukeAll(archie.db)
		if err != nil {
			return fmt.Errorf("nukeAll: %w", err)
		}

		// Import the data into the test db.
		loadDBSnap(t, gzFile)

		// Run the upgrades.
		err = upgradeDB(ctx, archie.db)
		if err != nil {
			return fmt.Errorf("upgradeDB: %w", err)
		}
		return nil
	}

	// These are all positive path tests (no corruption in DB).
	snaps := []string{
		"dcrdex_test_db_v0-master.sql.gz",              // v0 DB with no meta table
		"dcrdex_test_db_v0-release-0.1.sql.gz",         // v0 DB with a meta table with state_hash from release-0.1
		"dcrdex_test_db_v0-release-0.1-matches.sql.gz", // v0 with meta table and a ton of matches for v2 upgrade
	}

	for _, snap := range snaps {
		if err := tryUpgrade(snap); err != nil {
			t.Errorf("upgrade of DB snapshot %q failed: %v", snap, err)
		}
	}

	// The last snapshot contains matches, so v9 should add a genesis event.
	t.Run("v9", func(t *testing.T) {
		version, err := DBVersion(archie.db)
		if err != nil || version != dbVersion {
			t.Fatalf("DBVersion = (%d, %v), want %d", version, err, dbVersion)
		}
		entries, err := archie.EventLogEntriesAfter(ctx, 0, 2)
		if err != nil || len(entries) != 1 {
			t.Fatalf("event log entries = (%+v, %v), want exactly one", entries, err)
		}
		if entry := entries[0]; entry.Seq != 1 || entry.Kind != db.MeshGenesisKind {
			t.Fatalf("first event = (%d, %q), want (1, %q)", entry.Seq, entry.Kind, db.MeshGenesisKind)
		}
	})

	// Try with canceled context.
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	cancel()
	// To try hitting the Exec/Query/etc. errors, attempt to cancel mid upgrade:
	// go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	err := tryUpgrade(snaps[2]) // the bigger upgrade
	if !errors.Is(err, context.Canceled) && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("wrong error for canceled context, got %v", err)
	}
	t.Logf("Got an expected error: %v", err)
	// NOTE: That was a very limited test of cancellation as the transaction
	// wasn't even started and thus was not rolled back, but it does stop the
	// upgrade chain cleanly and there should be no schema_version.
	_, err = DBVersion(archie.db)
	if err == nil {
		t.Errorf("expected error from DBVersion with no meta table")
	}
}

func TestV9ReputationConversion(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := archie.db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	newID := func() order.OrderID { return order.OrderID(encode.RandomBytes(32)) }
	user, emptyUser, convertedUser := randomAccountID(), randomAccountID(), randomAccountID()
	for _, u := range []account.AccountID{user, emptyUser, convertedUser} {
		exec(`INSERT INTO accounts (account_id, reputation_ver) VALUES ($1, 0)`, u)
	}
	exec(`UPDATE accounts SET reputation_ver = 1 WHERE account_id = $1`, convertedUser)
	unchangedLink := newID()
	exec(`INSERT INTO points (account, link, class, outcome) VALUES ($1, $2, $3, $4)`,
		convertedUser, unchangedLink, db.OutcomeClassMatch, db.OutcomeNoSwapAsMaker)

	type point struct {
		Link    order.OrderID
		Outcome db.Outcome
	}
	want := make(map[db.OutcomeClass][]point)
	addWant := func(class db.OutcomeClass, link order.OrderID, outcome db.Outcome) {
		want[class] = append(want[class], point{link, outcome})
	}
	schemas := []string{marketSchema(mktInfo.Name), marketSchema(mktInfo2.Name)}
	// Interleave both markets. The first eight completed orders fall outside
	// the 100-order window once the three newer cancels are included.
	for i := 0; i < 105; i++ {
		oid := newID()
		exec(fmt.Sprintf(`INSERT INTO %s.orders_archived
			(oid, account_id, status, epoch_idx, epoch_dur, complete_time)
			VALUES ($1, $2, $3, $4, 1000, $5)`, schemas[i%2]),
			oid, user, orderStatusExecuted, i, (i+1)*1000)
		if i >= 8 {
			addWant(db.OutcomeClassOrder, oid, db.OutcomeOrderComplete)
		}
		// Three newer preimage results leave room for 37 completed orders.
		if i >= 68 {
			addWant(db.OutcomeClassPreimage, oid, db.OutcomePreimageSuccess)
		}
	}
	missed := newID()
	exec(fmt.Sprintf(`INSERT INTO %s.orders_archived (oid, account_id, status, epoch_idx, epoch_dur)
		VALUES ($1, $2, $3, 105, 1000)`, schemas[0]), missed, user, orderStatusRevoked)
	addWant(db.OutcomeClassPreimage, missed, db.OutcomePreimageMiss)

	for i, gap := range []int{0, 2} {
		oid := newID()
		epoch := 107 + i
		exec(fmt.Sprintf(`INSERT INTO %s.epochs (epoch_idx, epoch_dur, match_time)
			VALUES ($1, 1000, $2)`, schemas[i]), epoch, (epoch+1)*1000)
		exec(fmt.Sprintf(`INSERT INTO %s.cancels_archived
			(oid, account_id, target_order, status, epoch_idx, epoch_dur, epoch_gap, commit)
			VALUES ($1, $2, $3, $4, $5, 1000, $6, $7)`, schemas[i]),
			oid, user, newID(), orderStatusExecuted, epoch, gap, oid[:])
		outcome := db.OutcomeOrderCanceled
		if gap == 2 {
			outcome = db.OutcomeOrderComplete
		}
		addWant(db.OutcomeClassOrder, oid, outcome)
		addWant(db.OutcomeClassPreimage, oid, db.OutcomePreimageSuccess)
	}
	// Exempt revokes must be filtered before LIMIT, otherwise these 101 newer
	// revokes hide the one older counted revoke.
	for i := 0; i < 102; i++ {
		oid := newID()
		epoch := exemptEpochIdx
		if i == 0 {
			epoch = countedEpochIdx
			addWant(db.OutcomeClassOrder, oid, db.OutcomeOrderComplete)
		}
		exec(fmt.Sprintf(`INSERT INTO %s.cancels_archived
			(oid, account_id, target_order, status, epoch_idx, server_time)
			VALUES ($1, $2, $3, $4, $5, $6)`, schemas[0]),
			oid, user, newID(), orderStatusRevoked, epoch, time.UnixMilli(int64(110+i)*1000))
	}

	// One failure after 60 successes checks conversion of both outcomes and
	// removal of the oldest match when the 60-match window is exceeded.
	for i := 0; i < 61; i++ {
		mid := newID()
		status, outcome := order.MatchComplete, db.OutcomeSwapSuccess
		if i == 60 {
			status, outcome = order.NewlyMatched, db.OutcomeNoSwapAsMaker
		}
		exec(fmt.Sprintf(`INSERT INTO %s.matches
			(matchid, active, takerSell, makerAccount, takerAccount, status, epochIdx, epochDur, quantity)
			VALUES ($1, false, true, $2, $3, $4, $5, 1000, 1)`, schemas[i%2]), mid, user, convertedUser, status, i)
		if i > 0 {
			addWant(db.OutcomeClassMatch, order.OrderID(mid), outcome)
		}
	}
	if err := setDBVersion(archie.db, 8); err != nil {
		t.Fatal(err)
	}
	// Force failure after points have been inserted but before conversion
	// completes. All migration writes, including genesis, must roll back.
	exec(`CREATE FUNCTION fail_reputation_conversion() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF EXISTS (SELECT 1 FROM points WHERE account = NEW.account_id) THEN
				RAISE EXCEPTION 'test conversion failure';
			END IF;
			RETURN NEW;
		END $$`)
	t.Cleanup(func() { exec(`DROP FUNCTION IF EXISTS fail_reputation_conversion() CASCADE`) })
	exec(`CREATE TRIGGER fail_reputation_conversion BEFORE UPDATE ON accounts
		FOR EACH ROW EXECUTE FUNCTION fail_reputation_conversion()`)
	if err := upgradeDB(ctx, archie.db); err == nil || !strings.Contains(err.Error(), "test conversion failure") {
		t.Fatalf("upgrade error = %v, want injected failure", err)
	}
	var points, oldAccounts, events int
	if err := archie.db.QueryRow(`SELECT
		(SELECT count(*) FROM points),
		(SELECT count(*) FROM accounts WHERE reputation_ver = 0),
		(SELECT count(*) FROM event_log)`).Scan(&points, &oldAccounts, &events); err != nil {
		t.Fatal(err)
	}
	version, err := DBVersion(archie.db)
	if err != nil || version != 8 || points != 1 || oldAccounts != 2 || events != 0 {
		t.Fatalf("failed upgrade left version=%d, points=%d, v0 accounts=%d, events=%d, err=%v", version, points, oldAccounts, events, err)
	}
	exec(`DROP FUNCTION fail_reputation_conversion() CASCADE`)

	// Retry, then rerun startup's version check to ensure no duplication.
	for i := 0; i < 2; i++ {
		if err := upgradeDB(ctx, archie.db); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := archie.db.Query(`SELECT account, id, link, class, outcome FROM points ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[db.OutcomeClass][]point)
	var preserved int
	for rows.Next() {
		var u account.AccountID
		var id int64
		var p point
		var class db.OutcomeClass
		if err := rows.Scan(&u, &id, &p.Link, &class, &p.Outcome); err != nil {
			t.Fatal(err)
		}
		switch u {
		case user:
			got[class] = append(got[class], p)
		case convertedUser:
			preserved++
			if id != 1 || class != db.OutcomeClassMatch || p != (point{unchangedLink, db.OutcomeNoSwapAsMaker}) {
				t.Fatalf("changed existing point: id=%d, class=%d, point=%+v", id, class, p)
			}
		default:
			t.Fatalf("unexpected points for account %s", u)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(got, want) || preserved != 1 {
		t.Fatalf("converted points mismatch:\ngot: %+v\nwant: %+v\npreserved: %d", got, want, preserved)
	}
	if err := archie.db.QueryRow(`SELECT count(*) FROM accounts WHERE reputation_ver != 1`).Scan(&oldAccounts); err != nil || oldAccounts != 0 {
		t.Fatalf("accounts not converted = %d, err=%v", oldAccounts, err)
	}
	version, err = DBVersion(archie.db)
	if err != nil || version != 9 {
		t.Fatalf("version = %d, err=%v", version, err)
	}
	entries, err := archie.EventLogEntriesAfter(ctx, 0, 2)
	if err != nil || len(entries) != 1 || entries[0].Kind != db.MeshGenesisKind {
		t.Fatalf("genesis = %+v, err=%v", entries, err)
	}
}
