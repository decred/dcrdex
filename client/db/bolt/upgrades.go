// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"fmt"
	"path/filepath"

	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
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
	// v2 => v3 adds a tx data field to the match proof.
	v3Upgrade,
	// v3 => v4 splits orders into active and archived.
	v4Upgrade,
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

	// Backup the current version's DB file before processing the upgrades to
	// DBVersion. Note that any intermediate versions are not stored.
	currentFile := filepath.Base(db.Path())
	backupPath := fmt.Sprintf("%s.v%d.bak", currentFile, version) // e.g. dexc.db.v1.bak
	if err = db.backup(backupPath); err != nil {
		return fmt.Errorf("failed to backup DB prior to upgrade: %w", err)
	}

	// Each upgrade its own tx, otherwise bolt eats too much RAM.
	for i, upgrade := range upgrades[version:] {
		newVersion := version + uint32(i) + 1
		db.log.Debugf("Upgrading to version %d...", newVersion)
		err = db.Update(func(tx *bbolt.Tx) error {
			return doUpgrade(tx, upgrade, newVersion)
		})
		if err != nil {
			return err
		}
	}
	return nil
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

	if err := ensureVersion(dbtx, oldVersion); err != nil {
		return err
	}

	bkt := dbtx.Bucket(appBucket)
	if bkt == nil {
		return fmt.Errorf("appBucket not found")
	}
	skipCancels := true // cancel matches don't get revoked, only cancel orders
	return reloadMatchProofs(dbtx, skipCancels)
}

// v2Upgrade adds a MaxFeeRate field to the OrderMetaData. The upgrade sets the
// MaxFeeRate field for all historical trade orders to the max uint64. This
// avoids any chance of rejecting a pre-existing active match.
func v2Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 1
	ordersBucket := []byte("orders")

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
		// Cancel orders should be stored with a zero maxFeeRate, as done in
		// (*Core).tryCancelTrade. Besides, the maxFeeRate should not be applied
		// to cancel matches, as done in (*dexConnection).parseMatches.
		oTypeB := oBkt.Get(typeKey)
		if len(oTypeB) != 1 {
			return fmt.Errorf("order %x type invalid: %x", oid, oTypeB)
		}
		if order.OrderType(oTypeB[0]) == order.CancelOrderType {
			// Don't bother setting maxFeeRate for cancel orders.
			// decodeOrderBucket will default to zero for cancels.
			return nil
		}
		return oBkt.Put(maxFeeRateKey, maxFeeB)
	})
}

func v3Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 2

	if err := ensureVersion(dbtx, oldVersion); err != nil {
		return err
	}

	// Upgrade the match proof. We just have to retrieve and re-store the
	// buckets. The decoder will recognize the the old version and add the new
	// field.
	skipCancels := true // cancel matches have no tx data
	return reloadMatchProofs(dbtx, skipCancels)
}

func ensureVersion(tx *bbolt.Tx, ver uint32) error {
	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return fmt.Errorf("error fetching database version: %w", err)
	}

	if dbVersion != ver {
		return fmt.Errorf("wrong version for upgrade. expected %d, got %d", ver, dbVersion)
	}
	return nil
}

// Note that reloadMatchProofs will rewrite the MatchProof with the current
// match proof encoding version. Thus, multiple upgrades in a row calling
// reloadMatchProofs may be no-ops. Matches with cancel orders may be skipped.
func reloadMatchProofs(tx *bbolt.Tx, skipCancels bool) error {
	matches := tx.Bucket(matchesBucket)
	return matches.ForEach(func(k, _ []byte) error {
		mBkt := matches.Bucket(k)
		if mBkt == nil {
			return fmt.Errorf("match %x bucket is not a bucket", k)
		}
		proofB := mBkt.Get(proofKey)
		if len(proofB) == 0 {
			return fmt.Errorf("empty match proof")
		}
		proof, ver, err := dexdb.DecodeMatchProof(proofB)
		if err != nil {
			return fmt.Errorf("error decoding proof: %w", err)
		}
		// No need to rewrite this if it was loaded from the current version.
		if ver == dexdb.MatchProofVer {
			return nil
		}
		// No Script, and MatchComplete status means this is a cancel match.
		if skipCancels && len(proof.Script) == 0 {
			statusB := mBkt.Get(statusKey)
			if len(statusB) != 1 {
				return fmt.Errorf("no match status")
			}
			if order.MatchStatus(statusB[0]) == order.MatchComplete {
				return nil
			}
		}

		err = mBkt.Put(proofKey, proof.Encode())
		if err != nil {
			return fmt.Errorf("error re-storing match proof: %w", err)
		}
		return nil
	})
}

// v4Upgrade moves active orders from what will become the archivedOrdersBucket
// to a new ordersBucket. This is done in order to make searching active orders
// faster, as they do not need to be pulled out of all orders any longer. This
// upgrade moves active orders as opposed to inactive orders under the
// assumption that there are less active orders to move, and so a smaller
// database transaction occurs.
func v4Upgrade(dbtx *bbolt.Tx) error {
	const oldVersion = 3

	if err := ensureVersion(dbtx, oldVersion); err != nil {
		return err
	}

	// Move any inactive orders to the new archivedOrdersBucket.
	return moveActiveOrders(dbtx)
}

// moveActiveOrders searches the v1 ordersBucket for orders that are inactive,
// adds those to the v2 ordersBucket, and deletes them from the v1 ordersBucket,
// which becomes the archived orders bucket.
func moveActiveOrders(tx *bbolt.Tx) error {
	oldOrdersBucket := []byte("orders")
	newActiveOrdersBucket := []byte("activeOrders")
	// NOTE: newActiveOrdersBucket created in NewDB, but TestUpgrades skips that.
	_, err := tx.CreateBucketIfNotExists(newActiveOrdersBucket)
	if err != nil {
		return err
	}

	archivedOrdersBkt := tx.Bucket(oldOrdersBucket)
	activeOrdersBkt := tx.Bucket(newActiveOrdersBucket)
	return archivedOrdersBkt.ForEach(func(k, _ []byte) error {
		archivedOBkt := archivedOrdersBkt.Bucket(k)
		if archivedOBkt == nil {
			return fmt.Errorf("order %x bucket is not a bucket", k)
		}
		status := order.OrderStatus(intCoder.Uint16(archivedOBkt.Get(statusKey)))
		if status == order.OrderStatusUnknown {
			fmt.Printf("Encountered order with unknown status: %x\n", k)
			return nil
		}
		if !status.IsActive() {
			return nil
		}
		activeOBkt, err := activeOrdersBkt.CreateBucket(k)
		if err != nil {
			return err
		}
		// Assume the order bucket contains only values, no sub-buckets
		if err := archivedOBkt.ForEach(func(k, v []byte) error {
			return activeOBkt.Put(k, v)
		}); err != nil {
			return err
		}
		return archivedOrdersBkt.DeleteBucket(k)
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
