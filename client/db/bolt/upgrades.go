// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"fmt"

	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

type upgradefunc func(tx *bbolt.Tx) error

// Each database upgrade function should be keyed by the database
// version it upgrades.
var upgrades = [...]upgradefunc{
	// v0 => v1 adds a version key. Upgrades the MatchProof struct to
	// differentiate between server revokes and self revokes.
	v1Upgrade,
	// v1 => v2 adds a MaxFeeRate field to the OrderMetaData, used for match
	// validation.
	v2Upgrade,
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
func (db *BoltDB) upgradeDB() error {
	var version uint32
	version, err := db.getVersion()
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

	db.log.Infof("Upgrading database from version %d to %d", version, DBVersion)

	return db.Update(func(tx *bbolt.Tx) error {
		// Execute all necessary upgrades in order.
		for i, upgrade := range upgrades[version:] {
			err := doUpgrade(tx, upgrade, version+uint32(i)+1)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Get the currently stored DB version.
func (db *BoltDB) getVersion() (version uint32, err error) {
	return version, db.View(func(tx *bbolt.Tx) error {
		version, err = getVersionTx(tx)
		return err
	})
}

// Get the uint32 stored in the appBucket's versionKey entry.
func getVersionTx(tx *bbolt.Tx) (uint32, error) {
	bucket := tx.Bucket(appBucket)
	if bucket == nil {
		return 0, fmt.Errorf("appBucket not found")
	}
	versionB := bucket.Get(versionKey)
	if versionB == nil {
		// A nil version indicates a version 0 database.
		return 0, nil
	}
	return intCoder.Uint32(versionB), nil
}

func v1Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 0

	dbVersion, err := fetchDBVersion(dbtx)
	if err != nil {
		return fmt.Errorf("error fetching database version: %w", err)
	}

	if dbVersion != oldVersion {
		return fmt.Errorf("v1Upgrade inappropriately called")
	}

	bkt := dbtx.Bucket(appBucket)
	if bkt == nil {
		return fmt.Errorf("appBucket not found")
	}

	// Upgrade the match proof. We just have to retrieve and re-store the
	// buckets. The decoder will recognize the the old version and add the new
	// field.
	matches := dbtx.Bucket(matchesBucket)
	return matches.ForEach(func(k, _ []byte) error {
		mBkt := matches.Bucket(k)
		if mBkt == nil {
			return fmt.Errorf("match %x bucket is not a bucket", k)
		}
		proofB := getCopy(mBkt, proofKey)
		if len(proofB) == 0 {
			return fmt.Errorf("empty match proof")
		}
		proof, err := dexdb.DecodeMatchProof(proofB)
		if err != nil {
			return fmt.Errorf("error decoding proof: %w", err)
		}
		err = mBkt.Put(proofKey, proof.Encode())
		if err != nil {
			return fmt.Errorf("error re-storing match proof: %w", err)
		}
		return nil
	})
}

// v2Upgrade adds a MaxFeeRate field to the OrderMetaData. The upgrade sets the
// MaxFeeRate field for all historical orders to the max uint64. This avoids any
// chance of rejecting a pre-existing active match.
func v2Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 1

	dbVersion, err := fetchDBVersion(dbtx)
	if err != nil {
		return fmt.Errorf("error fetching database version: %w", err)
	}

	if dbVersion != oldVersion {
		return fmt.Errorf("v2Upgrade inappropriately called")
	}

	// For each order, set a maxfeerate of max uint64.
	maxFeeB := uint64Bytes(^uint64(0))

	master := dbtx.Bucket(ordersBucket)
	if master == nil {
		return fmt.Errorf("failed to open orders bucket")
	}

	return master.ForEach(func(oid, _ []byte) error {
		oBkt := master.Bucket(oid)
		if oBkt == nil {
			return fmt.Errorf("order %x bucket is not a bucket", oid)
		}
		return oBkt.Put(maxFeeRateKey, maxFeeB)
	})
}

func doUpgrade(tx *bbolt.Tx, upgrade upgradefunc, newVersion uint32) error {
	err := upgrade(tx)
	if err != nil {
		return fmt.Errorf("error upgrading DB: %v", err)
	}
	// Persist the database version.
	err = setDBVersion(tx, newVersion)
	if err != nil {
		return fmt.Errorf("error setting DB version: %v", err)
	}
	return nil
}
