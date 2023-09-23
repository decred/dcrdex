// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger"
)

type extendedWalletTx struct {
	*asset.WalletTransaction
	Confirmed bool `json:"confirmed"`
	// Create bond transactions are added to the store before
	// they are submitted.
	Submitted bool `json:"submitted"`
}

var blockPrefix = []byte("b")
var pendingPrefix = []byte("c")
var lastQueryKey = []byte("lq")
var txPrefix = []byte("t")
var maxPendingKey = pendingKey(math.MaxUint64)

// pendingKey maps an index to an extendedWalletTransaction. The index is
// required as there may be multiple pending transactions at the same time.
func pendingKey(i uint64) []byte {
	key := make([]byte, len(pendingPrefix)+8)
	copy(key, pendingPrefix)
	binary.BigEndian.PutUint64(key[len(pendingPrefix):], i)
	return key
}

// blockKey maps a block height and an index to an extendedWalletTransaction.
// The index is required as there may be multiple transactions in the same
// block.
func blockKey(blockHeight, index uint64) []byte {
	key := make([]byte, len(blockPrefix)+16)
	copy(key, blockPrefix)
	binary.BigEndian.PutUint64(key[len(blockPrefix):], blockHeight)
	binary.BigEndian.PutUint64(key[len(blockPrefix)+8:], index)
	return key
}

func parseBlockKey(key []byte) (blockHeight, index uint64) {
	blockHeight = binary.BigEndian.Uint64(key[len(blockPrefix):])
	index = binary.BigEndian.Uint64(key[len(blockPrefix)+8:])
	return
}

// txKey maps a txid to a blockKey or pendingKey.
func txKey(txid []byte) []byte {
	key := make([]byte, len(txPrefix)+len(txid))
	copy(key, txPrefix)
	copy(key[len(txPrefix):], txid)
	return key
}

type txDB interface {
	storeTx(tx *extendedWalletTx) error
	markTxAsSubmitted(txID dex.Bytes) error
	getTxs(n int, refID *dex.Bytes, past bool) ([]*asset.WalletTransaction, error)
	getPendingTxs() ([]*extendedWalletTx, error)
	removeTx(hash dex.Bytes) error
	// setLastReceiveTxQuery stores the last time the wallet was queried for
	// receive transactions. This is required to know how far back to query
	// for incoming transactions that were received while the wallet is
	// offline.
	setLastReceiveTxQuery(block uint64) error
	getLastReceiveTxQuery() (uint64, error)
	close() error
	run(context.Context)
}

type badgerTxDB struct {
	*badger.DB
	log dex.Logger
}

var _ txDB = (*badgerTxDB)(nil)

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...interface{}) {
	log.Warnf(s, a...)
}

func newBadgerTxDB(filePath string, log dex.Logger) (*badgerTxDB, error) {
	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	opts := badger.DefaultOptions(filePath).WithLogger(&badgerLoggerWrapper{log})
	db, err := badger.Open(opts)
	if err == badger.ErrTruncateNeeded {
		// Probably a Windows thing.
		// https://github.com/dgraph-io/badger/issues/744
		log.Warnf("newTxHistoryStore badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		log.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		db, err = badger.Open(opts)
	}
	if err != nil {
		return nil, err
	}

	return &badgerTxDB{
		DB:  db,
		log: log}, nil
}

func (db *badgerTxDB) findFreeBlockKey(txn *badger.Txn, blockNumber uint64) ([]byte, error) {
	getKey := func(i uint64) []byte {
		if blockNumber == 0 {
			return pendingKey(i)
		}
		return blockKey(blockNumber, i)
	}

	for i := uint64(0); ; i++ {
		key := getKey(i)
		_, err := txn.Get(key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return key, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (db *badgerTxDB) storeTx(tx *extendedWalletTx) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(tx.ID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		var currBlockKey []byte
		needNewBlockKey := true

		if err == nil {
			needNewBlockKey = false
			currBlockKey, err = txKeyItem.ValueCopy(nil)
			if err != nil {
				return err
			}

			if currBlockKey[0] == pendingPrefix[0] {
				if tx.BlockNumber > 0 {
					needNewBlockKey = true
				}
			} else if currBlockKey[0] == blockPrefix[0] {
				blockHeight, _ := parseBlockKey(currBlockKey)
				if blockHeight != tx.BlockNumber {
					needNewBlockKey = true
				}
			} else {
				return fmt.Errorf("invalid block key %s", string(currBlockKey))
			}

			if needNewBlockKey {
				if err := txn.Delete(currBlockKey); err != nil {
					return err
				}
			}
		}

		if needNewBlockKey {
			currBlockKey, err = db.findFreeBlockKey(txn, tx.BlockNumber)
			if err != nil {
				return err
			}
			if err = txn.Set(txKey, currBlockKey); err != nil {
				return err
			}
		}

		txB, err := json.Marshal(tx)
		if err != nil {
			return err
		}
		return txn.Set(currBlockKey, txB)
	})
}

func (db *badgerTxDB) markTxAsSubmitted(txID dex.Bytes) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(txID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil {
			return err
		}

		blockKey, err := txKeyItem.ValueCopy(nil)
		if err != nil {
			return err
		}

		blockItem, err := txn.Get(blockKey)
		if err != nil {
			return err
		}

		wtB, err := blockItem.ValueCopy(nil)
		if err != nil {
			return err
		}

		var wt extendedWalletTx
		if err := json.Unmarshal(wtB, &wt); err != nil {
			return err
		}

		wt.Submitted = true
		wtB, err = json.Marshal(wt)
		if err != nil {
			return err
		}

		return txn.Set(blockKey, wtB)
	})
}

func (db *badgerTxDB) getTxs(n int, refID *dex.Bytes, past bool) ([]*asset.WalletTransaction, error) {
	var txs []*asset.WalletTransaction
	err := db.View(func(txn *badger.Txn) error {
		var startKey []byte
		if refID != nil {
			txKey := txKey(*refID)
			txKeyItem, err := txn.Get(txKey)
			if err != nil {
				return asset.CoinNotFoundError
			}

			startKey, err = txKeyItem.ValueCopy(nil)
			if err != nil {
				return err
			}
		}
		if startKey == nil {
			past = true
			startKey = maxPendingKey
		}

		opts := badger.DefaultIteratorOptions
		opts.Reverse = past
		it := txn.NewIterator(opts)
		defer it.Close()

		pendingOrBlock := func() bool {
			if it.ValidForPrefix(blockPrefix) {
				return true
			}

			if it.ValidForPrefix(pendingPrefix) {
				return true
			}

			return false
		}

		for it.Seek(startKey); pendingOrBlock() && (n <= 0 || len(txs) < n); it.Next() {
			item := it.Item()
			wtB, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			var wt extendedWalletTx
			if err := json.Unmarshal(wtB, &wt); err != nil {
				return err
			}
			if past {
				txs = append(txs, wt.WalletTransaction)
			} else {
				txs = append([]*asset.WalletTransaction{wt.WalletTransaction}, txs...)
			}
		}

		return nil
	})
	return txs, err
}

func (db *badgerTxDB) getPendingTxs() ([]*extendedWalletTx, error) {
	var txs []*extendedWalletTx
	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(maxPendingKey); it.Valid(); it.Next() {
			item := it.Item()
			wtB, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			var wt extendedWalletTx
			if err := json.Unmarshal(wtB, &wt); err != nil {
				return err
			}

			if !wt.Confirmed {
				txs = append(txs, &wt)
			}
		}

		return nil
	})

	return txs, err
}

func (db *badgerTxDB) removeTx(txID dex.Bytes) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(txID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil {
			return err
		}

		blockKey, err := txKeyItem.ValueCopy(nil)
		if err != nil {
			return err
		}

		if err := txn.Delete(txKey); err != nil {
			return err
		}

		return txn.Delete(blockKey)
	})
}

func (db *badgerTxDB) setLastReceiveTxQuery(block uint64) error {
	return db.Update(func(txn *badger.Txn) error {
		// use binary big endian
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, block)
		return txn.Set(lastQueryKey, b)
	})
}

const errNeverQueried = dex.ErrorKind("never queried")

func (db *badgerTxDB) getLastReceiveTxQuery() (uint64, error) {
	var block uint64
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(lastQueryKey)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return errNeverQueried
		}
		if err != nil {
			return err
		}
		b, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		block = binary.BigEndian.Uint64(b)
		return nil
	})
	return block, err
}

func (db *badgerTxDB) run(ctx context.Context) {
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

func (db *badgerTxDB) close() error {
	return db.DB.Close()
}
