// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger"
)

var (
	seekEnd = []byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}
)

type KeyValueDB interface {
	Get(k []byte, thing encoding.BinaryUnmarshaler) (bool, error)
	Store(k []byte, thing encoding.BinaryMarshaler) error
	Close() error
	ForEach(f func(k, v []byte) error, opts ...*IterationOption) error
	Run(context.Context)
	Delete(k []byte) error
}

type IterationOption struct {
	reverse       bool
	prefix        []byte
	maxEntries    uint64
	deleteOverMax bool
}

func WithReverse() *IterationOption {
	return &IterationOption{
		reverse: true,
	}
}

func WithPrefix(b []byte) *IterationOption {
	return &IterationOption{
		prefix: b,
	}
}

func WithMaxEntries(max uint64, deleteExcess bool) *IterationOption {
	return &IterationOption{
		maxEntries:    max,
		deleteOverMax: deleteExcess,
	}
}

type kvDB struct {
	*badger.DB
	log dex.Logger
}

func NewFileDB(filePath string, log dex.Logger) (KeyValueDB, error) {
	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	opts := badger.DefaultOptions(filePath).WithLogger(&badgerLoggerWrapper{log})
	db, err := badger.Open(opts)
	if err == badger.ErrTruncateNeeded {
		// Probably a Windows thing.
		// https://github.com/dgraph-io/badger/issues/744
		log.Warnf("NewFileDB badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		log.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		db, err = badger.Open(opts)
	}
	if err != nil {
		return nil, err
	}

	return &kvDB{db, log}, nil
}

// Run starts the garbage collection loop.
func (d *kvDB) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := d.RunValueLogGC(0.5)
			if err != nil && !errors.Is(err, badger.ErrNoRewrite) {
				d.log.Errorf("garbage collection error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Close the database.
func (d *kvDB) Close() error {
	return d.DB.Close()
}

func (d *kvDB) ForEach(f func(k, v []byte) error, iterOpts ...*IterationOption) error {
	var deletes [][]byte
	err := d.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		var maxEntries uint64 = math.MaxUint64
		var deleteExcess bool
		for _, iterOpt := range iterOpts {
			switch {
			case iterOpt.reverse:
				opts.Reverse = true
			case iterOpt.maxEntries > 0:
				maxEntries = iterOpt.maxEntries
				deleteExcess = iterOpt.deleteOverMax
			case len(iterOpt.prefix) > 0:
				opts.Prefix = iterOpt.prefix
			}
		}

		it := txn.NewIterator(opts)
		defer it.Close()

		if opts.Reverse {
			it.Seek(append(opts.Prefix, seekEnd...))
		} else {
			it.Rewind()
		}
		var n uint64
		for ; it.Valid(); it.Next() {
			n++
			item := it.Item()
			k := item.Key()
			if maxEntries > 0 && n > maxEntries {
				deletes = append(deletes, k)
				continue
			}
			err := item.Value(func(v []byte) error {
				return f(k, v)
			})
			if err != nil {
				return err
			}
			if !deleteExcess && n == maxEntries {
				break
			}
		}
		return nil
	})

	if len(deletes) > 0 {
		d.log.Tracef("deleting %d entries from db", len(deletes))
		if err := d.Update(func(txn *badger.Txn) error {
			for _, k := range deletes {
				if err := txn.Delete(k); err != nil {
					d.log.Errorf("Error deleting db entry: %v", err)
				}
			}
			return nil
		}); err != nil {
			d.log.Errorf("Error while deleting keys: %v", err)
		}
	}

	return err
}

func (d *kvDB) Delete(k []byte) error {
	return d.Update(func(tx *badger.Txn) error {
		return tx.Delete(k)
	})
}

func (d *kvDB) Store(k []byte, thing encoding.BinaryMarshaler) error {
	b, err := thing.MarshalBinary()
	if err != nil {
		return err
	}

	return d.Update(func(tx *badger.Txn) error {
		return tx.Set(k, b)
	})
}

func (d *kvDB) Get(k []byte, thing encoding.BinaryUnmarshaler) (found bool, err error) {
	return found, d.View(func(txn *badger.Txn) error {
		item, err := txn.Get(k)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return fmt.Errorf("error reading database: %w", err)
		}
		found = true
		return item.Value(func(b []byte) error {
			return thing.UnmarshalBinary(b)
		})
	})
}

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...any) {
	log.Warnf(s, a...)
}

type memoryDB map[string][]byte

func NewMemoryDB() KeyValueDB {
	return make(memoryDB)
}

func (m memoryDB) Store(k []byte, v encoding.BinaryMarshaler) error {
	b, err := v.MarshalBinary()
	if err != nil {
		return err
	}
	m[string(k)] = b
	return nil
}

func (m memoryDB) Get(k []byte, thing encoding.BinaryUnmarshaler) (found bool, err error) {
	b, found := m[string(k)]
	if !found {
		return
	}
	return true, thing.UnmarshalBinary(b)
}

func (m memoryDB) Close() error {
	return nil
}

func (m memoryDB) ForEach(f func(k, v []byte) error, iterOpts ...*IterationOption) error {
	for k, v := range m {
		if err := f([]byte(k), v); err != nil {
			return err
		}
	}
	return nil
}

func (m memoryDB) Run(context.Context) {}

func (m memoryDB) Delete(k []byte) error {
	delete(m, string(k))
	return nil
}
