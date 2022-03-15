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
	"strings"
	"testing"
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
