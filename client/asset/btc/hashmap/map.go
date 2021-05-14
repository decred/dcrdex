// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package hashmap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/dgraph-io/badger"
)

// Map is a badger-backed mapping of chainhash.Hash -> chainhash.Hash.
type Map struct {
	*badger.DB
	log dex.Logger
}

// New is a constructor for a *Map.
func New(rootPath, dbName string, logger dex.Logger) (*Map, error) {
	dbPath := filepath.Join(rootPath, dbName)
	_, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dbPath, 0755); err != nil {
			return nil, fmt.Errorf("error creating database file at %s: %w", dbPath, err)
		}
	}

	badgerLogger := &badgerLoggerWrapper{logger.SubLogger("BADGER")}
	badgerLogger.SetLevel(dex.LevelWarn)

	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	db, err := badger.Open(badger.DefaultOptions(dbPath).WithLogger(badgerLogger))
	if err != nil {
		return nil, err
	}

	return &Map{
		DB:  db,
		log: logger,
	}, nil
}

// Run starts the garbage collection loop.
func (db *Map) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
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
}

// Close the database.
func (db *Map) Close() error {
	return db.DB.Close()
}

// Set sets the value at the key. Any previously set value is silently replaced.
func (db *Map) Set(k, v chainhash.Hash) {
	err := db.Update(func(tx *badger.Txn) error {
		return tx.Set(k[:], v[:])
	})
	if err != nil {
		db.log.Errorf("insertion error: %v", err)
	}
}

// Get gets the value at the key, or nil if no value is set.
func (db *Map) Get(k chainhash.Hash) *chainhash.Hash {
	var value []byte
	key := k[:]
	err := db.View(func(tx *badger.Txn) error {
		item, err := tx.Get(key)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}
		return item.Value(func(v []byte) error {
			value = make([]byte, len(v))
			copy(value, v)
			return nil
		})
	})
	if err != nil {
		db.log.Errorf("retrieval error: %v", err)
		return nil
	}
	if value == nil {
		return nil
	}
	var h chainhash.Hash
	copy(h[:], value)
	return &h
}

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger.
type badgerLoggerWrapper struct {
	dex.Logger
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...interface{}) {
	log.Warnf(s, a...)
}
