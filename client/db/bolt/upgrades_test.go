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

	"go.etcd.io/bbolt"
)

var dbUpgradeTests = [...]struct {
	name     string
	upgrade  upgradefunc
	verify   func(*testing.T, *bbolt.DB)
	filename string // in testdata directory
}{
	{"upgradeFromV0", v1Upgrade, verifyV1Upgrade, "v0.db.gz"},
}

func TestUpgrades(t *testing.T) {
	d, err := ioutil.TempDir("", "dcrdex_test_upgrades")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("group", func(t *testing.T) {
		for _, tc := range dbUpgradeTests {
			tc := tc // capture range variable
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				testFile, err := os.Open(filepath.Join("testdata", tc.filename))
				if err != nil {
					t.Fatal(err)
				}
				defer testFile.Close()
				r, err := gzip.NewReader(testFile)
				if err != nil {
					t.Fatal(err)
				}
				dbPath := filepath.Join(d, tc.name+".db")
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
				err = db.Update(func(dbtx *bbolt.Tx) error {
					return tc.upgrade(dbtx)
				})
				if err != nil {
					t.Fatalf("Upgrade failed: %v", err)
				}
				tc.verify(t, db)
			})
		}
	})

	os.RemoveAll(d)
}

func verifyV1Upgrade(t *testing.T, db *bbolt.DB) {
	expectedVersion := uint32(1)
	err := db.View(func(dbtx *bbolt.Tx) error {
		bkt := dbtx.Bucket(appBucket)
		if bkt == nil {
			return fmt.Errorf("appBucket not found")
		}
		versionB := bkt.Get(versionKey)
		if versionB == nil {
			return fmt.Errorf("expected a non-nil version value")
		}
		version := intCoder.Uint32(versionB)
		if version != expectedVersion {
			return fmt.Errorf("expected db version %d, got %d",
				expectedVersion, version)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
