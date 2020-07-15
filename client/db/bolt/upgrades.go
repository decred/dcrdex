// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

type upgradefunc func(tx *bbolt.Tx) error

// Each database upgrade function should be keyed by the database
// version it upgrades.
var upgrades = [...]upgradefunc{
	//v1Upgrade, // v0 => v1 adds a version key.
}

// DBVersion is the latest version of the database that is understood. Databases
// with recorded versions higher than this will fail to open (meaning any
// upgrades prevent reverting to older software).
const DBVersion = uint32(len(upgrades))

func fetchDBVersion(tx *bbolt.Tx) (uint32, error) {
	bucket := tx.Bucket(appBucket)
	if bucket == nil {
		return 0, fmt.Errorf("app bucket not found")
	}

	versionB := bucket.Get(versionKey)
	if versionB == nil {
		return 0, fmt.Errorf("database version not found")
	}

	return intCoder.Uint32(versionB), nil
}

func setDBVersion(tx *bbolt.Tx, newVersion uint32) error {
	bucket := tx.Bucket(appBucket)
	if bucket == nil {
		return fmt.Errorf("app bucket not found")
	}

	return bucket.Put(versionKey, encode.Uint32Bytes(newVersion))
}

// upgradeDB checks whether any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func upgradeDB(db *bbolt.DB) error {
	var version uint32
	err := db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(appBucket)
		if bucket == nil {
			return fmt.Errorf("appBucket not found")
		}

		versionB := bucket.Get(versionKey)
		if versionB == nil {
			// A nil version indicates a version 0 database.
			return nil
		}
		version = intCoder.Uint32(versionB)
		return nil
	})
	if err != nil {
		return err
	}

	if version > DBVersion {
		return fmt.Errorf("unknown database version %d, "+
			"client recognizes up to %d", version, DBVersion)
	}

	if version == DBVersion {
		// No upgrades necessary.
		return nil
	}

	log.Infof("Upgrading database from version %d to %d", version, DBVersion)

	return db.Update(func(tx *bbolt.Tx) error {
		// Execute all necessary upgrades in order.
		for _, upgrade := range upgrades[version:] {
			err := upgrade(tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func v1Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 0
	const newVersion = 1

	dbVersion, err := fetchDBVersion(dbtx)
	if err == nil {
		return fmt.Errorf("unexpected database version found")
	}

	if dbVersion != oldVersion {
		return fmt.Errorf("versionedDBUpgrade inappropriately called")
	}

	bkt := dbtx.Bucket(appBucket)
	if bkt == nil {
		return fmt.Errorf("appBucket not found")
	}

	// Persist the database version.
	return setDBVersion(dbtx, newVersion)
}
