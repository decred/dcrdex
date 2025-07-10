// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lexi

import (
	"context"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger"
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
	log      dex.Logger
	idSeq    *badger.Sequence
	wg       sync.WaitGroup
	updateWG sync.WaitGroup
}

// Config is the configuration settings for the Lexi DB.
type Config struct {
	Path string
	Log  dex.Logger
}

// New constructs a new Lexi DB.
func New(cfg *Config) (*DB, error) {
	opts := badger.DefaultOptions(cfg.Path).WithLogger(&badgerLoggerWrapper{cfg.Log.SubLogger("BADG")})
	bdb, err := badger.Open(opts)
	if err == badger.ErrTruncateNeeded {
		// Probably a Windows thing.
		// https://github.com/dgraph-io/badger/issues/744
		cfg.Log.Warnf("Error opening badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		cfg.Log.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		bdb, err = badger.Open(opts)
	}
	if err != nil {
		return nil, err
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

const versionKey = "__version__"

func (db *DB) getDBVersion() (version uint32, err error) {
	err = db.View(func(txn *badger.Txn) error {
		prefix, err := db.prefixForName(versionKey)
		if err != nil {
			return err
		}
		item, err := txn.Get(prefix[:])
		if errors.Is(err, badger.ErrKeyNotFound) {
			// Version not found, so we'll assume it's 0
			return nil
		}
		return item.Value(func(b []byte) error {
			version = binary.BigEndian.Uint32(b)
			return nil
		})
	})

	return
}

func (db *DB) setDBVersion(version uint32) error {
	return db.Update(func(txn *badger.Txn) error {
		prefix, err := db.prefixForName(versionKey)
		if err != nil {
			return err
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b[:], version)
		return txn.Set(prefix[:], b)
	})
}

// Upgrade updates the schema of the database. It should be called right
// after Connect, before any transactions are done. Each upgrade should
// contain a call to one of the functions that change the schema of the
// database, including DeleteIndex and ReIndex.
//
// As the schema evolves, additional upgrades should be added to the list
// that is passed to Upgrade. When this function is called with a certain
// amount of upgrades, each of them are applied in order and the version of
// the database is incremented to the amount of upgrades that have been
// applied. When the function is called again, the upgrades that have already
// been applied are skipped.
func (db *DB) Upgrade(upgrades []func() error) error {
	version, err := db.getDBVersion()
	if err != nil {
		return err
	}

	if version > uint32(len(upgrades)) {
		return fmt.Errorf("upgrade list is too short. expected at least %d upgrades, got %d",
			version, len(upgrades))
	}

	for i, upgrade := range upgrades {
		if i < int(version) {
			continue
		}
		if err := upgrade(); err != nil {
			return err
		}
		if err := db.setDBVersion(uint32(i + 1)); err != nil {
			return err
		}
	}

	return nil
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
		if err = db.DB.Update(f); err == nil || !errors.Is(err, badger.ErrConflict) {
			return err
		}
		sleepTime *= 2
		time.Sleep(sleepTime)
	}

	return err
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
type KV interface{}

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
