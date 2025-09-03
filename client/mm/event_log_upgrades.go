package mm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

func getVersionTx(tx *bbolt.Tx) (version uint32, err error) {
	botRuns := tx.Bucket(botRunsBucket)
	versionB := botRuns.Get(versionKey)
	if versionB != nil {
		version = encode.BytesToUint32(versionB)
	}
	return version, nil
}

func ensureVersion(tx *bbolt.Tx, ver uint32) error {
	dbVersion, err := getVersionTx(tx)
	if err != nil {
		return fmt.Errorf("error fetching database version: %w", err)
	}
	if dbVersion != ver {
		return fmt.Errorf("wrong version for upgrade. expected %d, got %d", ver, dbVersion)
	}
	return nil
}

func setDBVersion(tx *bbolt.Tx, newVersion uint32) error {
	botRuns := tx.Bucket(botRunsBucket)
	return botRuns.Put(versionKey, encode.Uint32Bytes(newVersion))
}

func (db *boltEventLogDB) getVersion() (version uint32, err error) {
	return version, db.View(func(tx *bbolt.Tx) error {
		version, err = getVersionTx(tx)
		return err
	})
}

type upgradefunc func(tx *bbolt.Tx) error

// v1Upgrade removes all run buckets. This was done very early in the development of
// the event log db.
func v1Upgrade(tx *bbolt.Tx) error {
	botRuns := tx.Bucket(botRunsBucket)
	return botRuns.ForEachBucket(func(k []byte) error {
		err := botRuns.DeleteBucket(k)
		if err != nil {
			return err
		}
		return nil
	})
}

// v2Upgrade is done in order to support CEX asset ID fields being added to
// BotConfig, DepositEvent, and WithdrawalEvent. All of these fields are
// set to have the same value as the DEX asset IDs.
func v2Upgrade(tx *bbolt.Tx) error {
	botRuns := tx.Bucket(botRunsBucket)
	if botRuns == nil {
		return fmt.Errorf("botRuns bucket not found")
	}

	return botRuns.ForEachBucket(func(k []byte) error {
		if bytes.Equal(k, versionKey) {
			return nil // Skip version key
		}

		runBucket := botRuns.Bucket(k)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", k)
		}

		// Parse the run key to get market info
		_, mkt, err := parseRunKey(k)
		if err != nil {
			return fmt.Errorf("error parsing run key: %w", err)
		}

		if err := updateConfigsForV2(runBucket, mkt); err != nil {
			return fmt.Errorf("error updating configs for run: %w", err)
		}

		if err := updateEventsForV2(runBucket, mkt); err != nil {
			return fmt.Errorf("error updating events for run: %w", err)
		}

		return nil
	})
}

// updateConfigsForV2 updates all configs in a run bucket to have the cex asset IDs
// match the DEX asset IDs if a CEX was being used.
func updateConfigsForV2(runBucket *bbolt.Bucket, mkt *MarketWithHost) error {
	cfgsBucket := runBucket.Bucket(cfgsBucket)
	if cfgsBucket == nil {
		return nil
	}

	cursor := cfgsBucket.Cursor()
	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		cfg, err := decodeBotConfig(v)
		if err != nil {
			return fmt.Errorf("error decoding config: %w", err)
		}

		// Update config if cexName is not empty
		if cfg.CEXName != "" {
			cfg.CEXBaseID = mkt.BaseID
			cfg.CEXQuoteID = mkt.QuoteID
		}

		// Re-marshal and store the updated config
		cfgB, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("error marshaling updated config: %w", err)
		}
		if err := cfgsBucket.Put(k, versionedBytes(0).AddData(cfgB)); err != nil {
			return fmt.Errorf("error storing updated config: %w", err)
		}
	}

	return nil
}

// updateEventsForV2 updates all the deposit and withdrawal events to have the
// cex asset ID match the dex asset ID.
func updateEventsForV2(runBucket *bbolt.Bucket, mkt *MarketWithHost) error {
	eventsBucket := runBucket.Bucket(eventsBucket)
	if eventsBucket == nil {
		return nil
	}

	cursor := eventsBucket.Cursor()
	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		event, err := decodeMarketMakingEvent(v)
		if err != nil {
			return fmt.Errorf("error decoding event: %w", err)
		}

		if event.DepositEvent != nil {
			event.DepositEvent.CEXAssetID = event.DepositEvent.DexAssetID
		}

		if event.WithdrawalEvent != nil {
			event.WithdrawalEvent.CEXAssetID = event.WithdrawalEvent.DEXAssetID
		}

		eventB, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("error marshaling updated event: %w", err)
		}

		if err := eventsBucket.Put(k, versionedBytes(0).AddData(eventB)); err != nil {
			return fmt.Errorf("error storing updated event: %w", err)
		}
	}

	return nil
}

var upgrades = [...]upgradefunc{
	v1Upgrade,
	v2Upgrade,
}

const dbVersion = uint32(len(upgrades))

func ovrFlag(overwrite bool) int {
	if overwrite {
		return os.O_TRUNC
	}
	return os.O_EXCL
}

// backup writes a direct copy of the DB into the specified destination file.
// This function should be called from BackupTo to validate the destination file
// and create any needed parent folders.
func (db *boltEventLogDB) backup(dst string, overwrite bool) error {
	// Just copy. This is like tx.CopyFile but with the overwrite flag set
	// as specified.
	f, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|ovrFlag(overwrite), 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	err = db.View(func(tx *bbolt.Tx) error {
		_, err = tx.WriteTo(f)
		return err
	})
	if err != nil {
		return err
	}
	return f.Sync()
}

func doUpgrade(tx *bbolt.Tx, upgrade upgradefunc, newVersion uint32) error {
	if err := ensureVersion(tx, newVersion-1); err != nil {
		return err
	}
	err := upgrade(tx)
	if err != nil {
		return fmt.Errorf("error upgrading DB: %v", err)
	}
	err = setDBVersion(tx, newVersion)
	if err != nil {
		return fmt.Errorf("error setting DB version: %v", err)
	}
	return nil
}

func (db *boltEventLogDB) upgradeDB() error {
	version, err := db.getVersion()
	if err != nil {
		return err
	}
	if version == dbVersion {
		return nil
	}
	if version > dbVersion {
		return fmt.Errorf("database version %d is greater than expected %d", version, dbVersion)
	}

	db.log.Infof("Upgrading database from version %d to %d", version, dbVersion)

	// Backup the current version's DB file before processing the upgrades to
	// DBVersion. Note that any intermediate versions are not stored.
	backupPath := fmt.Sprintf("%s.v%d.bak", db.Path(), version) // e.g. eventlog.db.v1.bak
	if err = db.backup(backupPath, true); err != nil {
		return fmt.Errorf("failed to backup DB prior to upgrade: %w", err)
	}

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
