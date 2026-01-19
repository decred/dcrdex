// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"context"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	v1badger "github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/v4"
)

// ErrKeyNotFound is an alias for badger.ErrKeyNotFound so that the caller
// doesn't have to import badger to use the semantics. Either error will satisfy
// errors.Is the same.
var ErrKeyNotFound = badger.ErrKeyNotFound

func convertError(err error) error {
	switch {
	case errors.Is(err, badger.ErrKeyNotFound):
		return ErrKeyNotFound
	}
	return err
}

// DB is the Lexi DB. The Lexi DB wraps a badger key-value database and provides
// the ability to add indexed data.
type DB struct {
	*badger.DB
	log        dex.Logger
	idSeq      *badger.Sequence
	wg         sync.WaitGroup
	updateWG   sync.WaitGroup
	upgradeTxn *badger.Txn
}

// Config is the configuration settings for the Lexi DB.
type Config struct {
	Path string
	Log  dex.Logger
}

// BadgerV1Update attempts to update old badger databases from v1 to v4.
func BadgerV1Update(path string, v4Path string, logger dex.Logger, opts badger.Options) (*badger.DB, error) {
	// Copy the old db directory as is.
	logger.Warnf("Detected incompatible database version at %s. "+
		"Backing up old database and attempting to update.", path)
	// Open the old db using v1 badger.
	v1opts := v1badger.DefaultOptions(path).WithLogger(&badgerLoggerWrapper{logger})
	oldDB, err := v1badger.Open(v1opts)
	if err != nil {
		return nil, err
	}
	defer oldDB.Close()
	// Back up values to a temporary file.
	tempPath := path + ".temp"
	tempF, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempPath)
	if _, err := oldDB.Backup(tempF, 0); err != nil {
		tempF.Close()
		return nil, err
	}
	tempF.Close()
	tempF, err = os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	defer tempF.Close()
	// Open a new database at the v4 path and attempt to load. Delete if that fails.
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	maxPendingWrites := 16
	if err := db.Load(tempF, maxPendingWrites); err != nil {
		db.Close()
		defer os.Remove(v4Path)
		return nil, err
	}
	return db, nil
}

const version4Suffix = "_v4"

// NeedsV1toV4Update returns whether the database needs an upgrade from v1 to
// v4. Works by searching for a directory with the v4 suffix.
func NeedsV1toV4Update(path string) (string, bool, error) {
	path = strings.TrimRight(path, "/")
	lenDiff := len(path) - len(version4Suffix)
	var v4Path string
	// May be already pointing to a v4 directory.
	if lenDiff >= 0 && path[lenDiff:] == version4Suffix {
		v4Path = path
		path = path[:len(version4Suffix)]
	} else {
		v4Path = fmt.Sprintf("%s%s", path, version4Suffix)
	}
	_, err := os.Stat(v4Path)
	if err == nil {
		// v4 directory exists where expected
		return v4Path, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		// v1 directory found, update it
		return v4Path, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	// Neither directory exists, new db.
	return v4Path, false, nil
}

// New constructs a new Lexi DB. Starting at badger v4, badger versions are
// appended to the directory path, i.e. "path_v4".
func New(cfg *Config) (*DB, error) {
	v4Path, needs, err := NeedsV1toV4Update(cfg.Path)
	if err != nil {
		return nil, err
	}
	logger := cfg.Log.SubLogger("BADG")
	opts := badger.DefaultOptions(v4Path).WithLogger(&badgerLoggerWrapper{logger})
	var bdb *badger.DB
	if needs {
		bdb, err = BadgerV1Update(cfg.Path, v4Path, logger, opts)
		if err != nil {
			logger.Warnf("Unable to update old db, creating new one: %v", err)
			bdb, err = badger.Open(opts)
			if err != nil {
				return nil, err
			}
		}
	} else {
		bdb, err = badger.Open(opts)
		if err != nil {
			return nil, err
		}
	}
	idSeq, err := bdb.GetSequence(prefixedKey(primarySequencePrefix, []byte{0x00}), 1000)
	if err != nil {
		return nil, fmt.Errorf("error getting constructing primary sequence: %w", err)
	}

	return &DB{
		DB:    bdb,
		log:   cfg.Log,
		idSeq: idSeq,
	}, nil
}

// Connect starts the DB, and creates goroutines to perform shutdown when the
// context is canceled.
func (db *DB) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	db.wg.Add(1)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer func() {
			ticker.Stop()
			db.updateWG.Wait()
			if err := db.idSeq.Release(); err != nil {
				db.log.Errorf("Error releasing sequence: %v", err)
			}
			db.Close()
			db.wg.Done()
		}()
		for {
			select {
			case <-ticker.C:
				err := db.RunValueLogGC(0.5)
				if err != nil && !errors.Is(err, badger.ErrNoRewrite) {
					db.log.Errorf("garbage collection error: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return &db.wg, nil
}

// GetDBVersion retrieves the current database version.
// If the version was never set, 0 is returned.
func (db *DB) GetDBVersion() (version uint32, err error) {
	err = db.View(func(txn *badger.Txn) error {
		versionItem, err := txn.Get(versionPrefix[:])
		if err != nil {
			if err == badger.ErrKeyNotFound {
				version = 0
				return nil
			}
			return err
		}
		return versionItem.Value(func(b []byte) error {
			version = binary.BigEndian.Uint32(b)
			return nil
		})
	})
	if err != nil {
		return 0, err
	}

	return
}

// SetDBVersion sets the current database version in the DB.
func (db *DB) SetDBVersion(version uint32) error {
	return db.Update(func(txn *badger.Txn) error {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b[:], version)
		return txn.Set(versionPrefix[:], b)
	})
}

// Update: badger can return an ErrConflict if a read and write happen
// concurrently. This bugs the hell out of me, because I though that if a
// database was ACID-compliant, this was impossible, but I guess not. Either
// way, the solution is to try again.
func (db *DB) Update(f func(txn *badger.Txn) error) (err error) {
	db.updateWG.Add(1)
	defer db.updateWG.Done()

	const maxRetries = 10
	sleepTime := 5 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		if db.upgradeTxn == nil {
			err = db.DB.Update(f)
		} else {
			err = f(db.upgradeTxn)
		}
		if err == nil || !errors.Is(err, badger.ErrConflict) {
			return err
		}

		sleepTime *= 2
		time.Sleep(sleepTime)
	}

	return err
}

// Upgrade provides a way to perform a series of db updates under a single
// transaction. All calls to Upgrade must happen synchronously when the
// database is initialized and not being used by any other goroutines.
func (db *DB) Upgrade(upgrade func() error) error {
	return db.DB.Update(func(txn *badger.Txn) error {
		db.upgradeTxn = txn
		defer func() {
			db.upgradeTxn = nil
		}()
		return upgrade()
	})
}

func (db *DB) prefixForNameImpl(name string, existing bool) (prefix keyPrefix, _ error) {
	nameKey := prefixedKey(nameToPrefixPrefix, []byte(name))
	return prefix, db.Update(func(txn *badger.Txn) error {
		it, err := txn.Get(nameKey)
		if err == nil {
			return it.Value(func(b []byte) error {
				prefix = bytesToPrefix(b)
				return nil
			})
		}
		if errors.Is(err, badger.ErrKeyNotFound) && existing {
			return badger.ErrKeyNotFound
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("error getting name: %w", err)
		}
		lastPrefix := lastKeyForPrefix(txn, prefixToNamePrefix)
		if len(lastPrefix) == 0 {
			prefix = firstAvailablePrefix
		} else {
			prefix = incrementPrefix(bytesToPrefix(lastPrefix))
		}
		if err := txn.Set(prefixedKey(nameToPrefixPrefix, []byte(name)), prefix[:]); err != nil {
			return fmt.Errorf("error setting prefix for table name: %w", err)
		}
		if err := txn.Set(prefixedKey(prefixToNamePrefix, prefix[:]), []byte(name)); err != nil {
			return fmt.Errorf("error setting table name for prefix: %w", err)
		}
		return nil
	})
}

// existingPrefixForName is like prefixForName, but it does not create a
// new prefix if the name is not found. Instead, it returns
// badger.ErrKeyNotFound.
func (db *DB) existingPrefixForName(name string) (prefix keyPrefix, _ error) {
	return db.prefixForNameImpl(name, true)
}

// prefixForName returns a unique prefix for the provided name and logs the
// relationship in the DB. Repeated calls to prefixForName with the same name
// will return the same prefix, including through restarts.
func (db *DB) prefixForName(name string) (prefix keyPrefix, _ error) {
	return db.prefixForNameImpl(name, false)
}

func (db *DB) nextID() (dbID DBID, _ error) {
	i, err := db.idSeq.Next()
	if err != nil {
		return dbID, err
	}
	binary.BigEndian.PutUint64(dbID[:], i)
	return
}

// KeyID returns the DBID for the key. This is the same DBID that will be used
// internally for the key when datum is inserted into a Table with Set. This
// method is provided as a tool to keep database index entries short.
func (db *DB) KeyID(kB []byte) (dbID DBID, err error) {
	err = db.View(func(txn *badger.Txn) error {
		dbID, err = db.keyID(txn, kB, true)
		return err
	})
	return
}

func (db *DB) keyID(txn *badger.Txn, kB []byte, readOnly bool) (dbID DBID, err error) {
	item, err := txn.Get(prefixedKey(keyToIDPrefix, kB))
	if err == nil {
		err = item.Value(func(v []byte) error {
			copy(dbID[:], v)
			return nil
		})
		return
	}
	if !readOnly && errors.Is(err, ErrKeyNotFound) {
		if dbID, err = db.nextID(); err != nil {
			return
		}
		if err = txn.Set(prefixedKey(keyToIDPrefix, kB), dbID[:]); err != nil {
			err = fmt.Errorf("error mapping key to ID: %w", err)
		} else if err = txn.Set(prefixedKey(idToKeyPrefix, dbID[:]), kB); err != nil {
			err = fmt.Errorf("error mapping ID to key: %w", err)
		}
	}
	return
}

// deleteDBID deletes the id-to-key mapping and the key-to-id mapping for the
// DBID.
func (db *DB) deleteDBID(txn *badger.Txn, dbID DBID) error {
	idK := prefixedKey(idToKeyPrefix, dbID[:])
	item, err := txn.Get(idK)
	if err != nil {
		return convertError(err)
	}
	if err := item.Value(func(kB []byte) error {
		if err := txn.Delete(prefixedKey(keyToIDPrefix, kB)); err != nil {
			return fmt.Errorf("error deleting key to ID mapping: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := txn.Delete(idK); err != nil {
		return fmt.Errorf("error deleting ID to key mapping: %w", err)
	}
	return nil
}

// KV is any one of a number of common types whose binary encoding is
// straight-forward.
type KV any

func parseKV(i KV) (b []byte, err error) {
	switch it := i.(type) {
	case []byte:
		b = it
	case uint32:
		b = make([]byte, 4)
		binary.BigEndian.PutUint32(b[:], it)
	case time.Time:
		b = make([]byte, 8)
		binary.BigEndian.PutUint64(b[:], uint64(it.UnixMilli()))
	case encoding.BinaryMarshaler:
		b, err = it.MarshalBinary()
	case nil:
	default:
		err = fmt.Errorf("unknown IndexBucket type %T", it)
	}
	return
}
