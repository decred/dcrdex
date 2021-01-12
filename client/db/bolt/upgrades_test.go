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
	expVer  uint32
}

var validators = []upgradeValidator{
	{v1Upgrade, verifyV1Upgrade, 1},
	{v2Upgrade, verifyV2Upgrade, 2},
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

	for i, archiveName := range outdatedDBs {
		dbPath, close := unpack(t, archiveName)
		defer close()
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
				return doUpgrade(dbtx, validator.upgrade, validator.expVer)
			})
			if err != nil {
				t.Fatalf("Upgrade failed: %v", err)
			}

			var ver uint32
			err = db.View(func(tx *bbolt.Tx) error {
				ver, err = getVersionTx(tx)
				return err
			})
			if err != nil {
				t.Fatalf("Error retrieving version: %v", err)
			}

			if ver != validator.expVer {
				t.Fatalf("Wrong version. Expected %d, got %d", validator.expVer, ver)
			}

			validator.verify(t, db)
		}
	}
}

func TestUpgradeDB(t *testing.T) {
	runUpgrade := func(archiveName string) error {
		dbPath, close := unpack(t, archiveName)
		defer close()
		dbi, err := NewDB(dbPath, tLogger)
		if err != nil {
			return fmt.Errorf("database initialization error: %w", err)
		}
		db := dbi.(*BoltDB)
		err = db.upgradeDB()
		if err != nil {
			return fmt.Errorf("upgradeDB error: %v", err)
		}
		newVersion, err := db.getVersion()
		if err != nil {
			return fmt.Errorf("getVersion error: %v", err)
		}
		if newVersion != DBVersion {
			return fmt.Errorf("DB version not set. Expected %d, got %d", DBVersion, newVersion)
		}
		return nil
	}

	for v, archiveName := range outdatedDBs {
		err := runUpgrade(archiveName)
		if err != nil {
			t.Fatalf("upgrade error for version %d database: %v", v, err)
		}
	}

}

func verifyV1Upgrade(t *testing.T, db *bbolt.DB) {
	err := db.View(func(dbtx *bbolt.Tx) error {
		return checkVersion(dbtx, 1)
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV2Upgrade(t *testing.T, db *bbolt.DB) {
	maxFeeB := uint64Bytes(^uint64(0))

	err := db.View(func(dbtx *bbolt.Tx) error {
		err := checkVersion(dbtx, 2)
		if err != nil {
			return err
		}

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

func checkVersion(dbtx *bbolt.Tx, expectedVersion uint32) error {
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
}

func unpack(t *testing.T, db string) (string, func()) {
	d, err := ioutil.TempDir("", "dcrdex_test_upgrades")
	if err != nil {
		t.Fatal(err)
	}

	t.Helper()
	archive, err := os.Open(filepath.Join("testdata", db))
	if err != nil {
		t.Fatal(err)
	}

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

	if err != nil {
		t.Fatal(err)
	}
	return dbPath, func() {
		dbFile.Close()
		archive.Close()
	}
}
