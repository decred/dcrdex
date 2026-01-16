// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package kvdb

import (
	"context"
	"encoding"
	"errors"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger/v4"
)

type KeyValueDB interface {
	Store(k []byte, v encoding.BinaryMarshaler) error
	Close() error
	ForEach(f func(k, v []byte) error) error
	Run(context.Context)
	Delete(k []byte) error
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

func (d *kvDB) ForEach(f func(k, v []byte) error) error {
	return d.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				return f(k, v)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
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

func (m memoryDB) Close() error {
	return nil
}

func (m memoryDB) ForEach(f func(k, v []byte) error) error {
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
