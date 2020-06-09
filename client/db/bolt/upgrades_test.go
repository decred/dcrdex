// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

var dbUpgradeTests = [...]struct {
	verify   func(*testing.T, *bbolt.DB)
	filename string // in testdata directory
}{
	{verifyV1Upgrade, "v0.db.gz"},
}

func TestUpgrades(t *testing.T) {
	t.Parallel()

	d, err := ioutil.TempDir("", "dcrdex_test_upgrades")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("group", func(t *testing.T) {
		for i, test := range dbUpgradeTests {
			test := test
			name := fmt.Sprintf("test%d", i)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				testFile, err := os.Open(filepath.Join("testdata", test.filename))
				if err != nil {
					t.Fatal(err)
				}
				defer testFile.Close()
				r, err := gzip.NewReader(testFile)
				if err != nil {
					t.Fatal(err)
				}
				dbPath := filepath.Join(d, name+".db")
				fi, err := os.Create(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(fi, r)
				fi.Close()
				if err != nil {
					t.Fatal(err)
				}
				db, err := bbolt.Open(dbPath, 0600,
					&bbolt.Options{Timeout: 1 * time.Second})
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				err = upgradeDB(db)
				if err != nil {
					t.Fatalf("Upgrade failed: %v", err)
				}
				test.verify(t, db)
			})
		}
	})

	os.RemoveAll(d)
}

func verifyV1Upgrade(t *testing.T, db *bbolt.DB) {
	err := db.View(func(dbtx *bbolt.Tx) error {
		bkt := dbtx.Bucket(appBucket)
		if bkt == nil {
			return fmt.Errorf("appBucket not found")
		}
		versionB := bkt.Get(versionKey)
		if versionB == nil {
			return fmt.Errorf("expected a non-nil version value")
		}
		version := encode.BytesToUint32(versionB)
		if version != versionedDBVersion {
			return fmt.Errorf("expected db version %d, got %d",
				versionedDBVersion, version)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
