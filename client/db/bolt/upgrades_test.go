// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

type upgradeValidator struct {
	upgrade upgradefunc
	verify  func(*testing.T, *bbolt.DB)
}

var validators = []upgradeValidator{
	{v1Upgrade, verifyV1Upgrade},
	{v2Upgrade, verifyV2Upgrade},
}

// The indices of the archives here in outdatedDBs should match the first
// eligible validator for the database.
var outdatedDBs = []string{
	"v0.db.gz", "v1.db.gz",
}

func TestUpgrades(t *testing.T) {
	d, err := ioutil.TempDir("", "dcrdex_test_upgrades")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)

	for i, db := range outdatedDBs {
		archive, err := os.Open(filepath.Join("testdata", db))
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Close()
		r, err := gzip.NewReader(archive)
		if err != nil {
			t.Fatal(err)
		}
		dbPath := filepath.Join(d, strings.TrimSuffix(db, ".gz"))
		dbFile, err := os.Create(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(dbFile, r)
		dbFile.Close()
		if err != nil {
			t.Fatal(err)
		}
		db, err := bbolt.Open(dbPath, 0600,
			&bbolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		// Do every validator from the db index up.
		for j := i; j < len(validators); j++ {
			validator := validators[j]
			err = db.Update(func(dbtx *bbolt.Tx) error {
				return validator.upgrade(dbtx)
			})
			if err != nil {
				t.Fatalf("Upgrade failed: %v", err)
			}
			validator.verify(t, db)
		}
	}
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

func verifyV2Upgrade(t *testing.T, db *bbolt.DB) {
	maxFeeB := uint64Bytes(^uint64(0))

	err := db.View(func(dbtx *bbolt.Tx) error {
		master := dbtx.Bucket(ordersBucket)
		if master == nil {
			return fmt.Errorf("orders bucket not found")
		}
		return master.ForEach(func(oid, _ []byte) error {
			oBkt := master.Bucket(oid)
			if oBkt == nil {
				return fmt.Errorf("order %x bucket is not a bucket", oid)
			}
			if !bytes.Equal(oBkt.Get(maxFeeRateKey), maxFeeB) {
				return fmt.Errorf("max fee not upgraded")
			}
			return nil
		})
	})
	if err != nil {
		t.Error(err)
	}
}
