// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
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

type ExtendedWalletTx struct {
	*asset.WalletTransaction
	// Create bond transactions are added to the store before
	// they are submitted.
	Submitted bool `json:"submitted"`
}

// "b" and "c" must be the first two prefixes.
// getPendingTxs relies on this.
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
func txKey(txid string) []byte {
	key := make([]byte, len(txPrefix)+len([]byte(txid)))
	copy(key, txPrefix)
	copy(key[len(txPrefix):], []byte(txid))
	return key
}

type BadgerTxDB struct {
	*badger.DB
	log dex.Logger
}

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

func NewBadgerTxDB(filePath string, log dex.Logger) (*BadgerTxDB, error) {
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

	return &BadgerTxDB{
		DB:  db,
		log: log}, nil
}

func (db *BadgerTxDB) findFreeBlockKey(txn *badger.Txn, blockNumber uint64) ([]byte, error) {
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

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	return bytes.Equal(b[:len(prefix)], prefix)
}

// StoreTx stores a transaction in the database.
func (db *BadgerTxDB) StoreTx(tx *ExtendedWalletTx) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(tx.ID)
		txKeyItem, getErr := txn.Get(txKey)
		if getErr != nil && !errors.Is(getErr, badger.ErrKeyNotFound) {
			return getErr
		}

		key, err := db.findFreeBlockKey(txn, tx.BlockNumber)
		if err != nil {
			return err
		}

		if getErr == nil { // already stored
			currBlockKey, err := txKeyItem.ValueCopy(nil)
			if err != nil {
				return err
			}
			err = txn.Delete(currBlockKey)
			if err != nil {
				return err
			}
			// Only update the key if it is a pending tx that has been confirmed,
			// or if the block number has changed indicating a reorg.
			if hasPrefix(currBlockKey, pendingPrefix) && tx.BlockNumber == 0 {
				key = currBlockKey
			} else if hasPrefix(currBlockKey, blockPrefix) {
				blockHeight, _ := parseBlockKey(currBlockKey)
				if blockHeight == tx.BlockNumber {
					key = currBlockKey
				}
			}
		}

		txB, err := json.Marshal(tx)
		if err != nil {
			return err
		}

		err = txn.Set(txKey, key)
		if err != nil {
			return err
		}

		return txn.Set(key, txB)
	})
}

// MarkTxAsSubmitted should be called when a previously stored transaction
// that had not yet been sent to the network is sent to the network.
// asset.CoinNotFoundError is returned if the transaction is not in the
// database.
func (db *BadgerTxDB) MarkTxAsSubmitted(txID string) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(txID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil {
			return asset.CoinNotFoundError
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

		var wt ExtendedWalletTx
		if err := json.Unmarshal(wtB, &wt); err != nil {
			return err
		}

		wt.Submitted = true
		submittedWt, err := json.Marshal(wt)
		if err != nil {
			return err
		}

		return txn.Set(blockKey, submittedWt)
	})
}

// GetTxs retrieves n transactions from the database. refID optionally
// takes a transaction ID, and returns that transaction and the at most
// (n - 1) transactions that were made either before or after it, depending
// on the value of past. If refID is nil, the most recent n transactions
// are returned, and the value of past is ignored. If the transaction with
// ID refID is not in the database, asset.CoinNotFoundError is returned.
// Unsubmitted transactions are not returned.
func (db *BadgerTxDB) GetTxs(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
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

		canIterate := func() bool {
			validPrefix := it.ValidForPrefix(blockPrefix) || it.ValidForPrefix(pendingPrefix)
			withinLimit := n <= 0 || len(txs) < n
			return validPrefix && withinLimit
		}
		for it.Seek(startKey); canIterate(); it.Next() {
			item := it.Item()
			wtB, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			var wt ExtendedWalletTx
			if err := json.Unmarshal(wtB, &wt); err != nil {
				return err
			}
			if !wt.Submitted {
				continue
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

// GetTx retrieves a transaction by its ID. If the transaction is not in
// the database, asset.CoinNotFoundError is returned.
func (db *BadgerTxDB) GetTx(txID string) (*asset.WalletTransaction, error) {
	txs, err := db.GetTxs(1, &txID, false)
	if err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		// This should never happen.
		return nil, fmt.Errorf("no results returned from getTxs")
	}
	return txs[0], nil
}

// GetPendingTxs returns all transactions that have not yet been confirmed.
func (db *BadgerTxDB) GetPendingTxs() ([]*ExtendedWalletTx, error) {
	var txs []*ExtendedWalletTx
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
			var wt ExtendedWalletTx
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

// RemoveTx removes a transaction from the database. If the transaction is
// not in the database, asset.CoinNotFoundError is returned.
func (db *BadgerTxDB) RemoveTx(txID string) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(txID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil {
			return asset.CoinNotFoundError
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

// SetLastReceiveTxQuery stores the last time the wallet was queried for
// receive transactions. This is required to know how far back to query
// for incoming transactions that were received while the wallet is
// offline.
func (db *BadgerTxDB) SetLastReceiveTxQuery(block uint64) error {
	return db.Update(func(txn *badger.Txn) error {
		// use binary big endian
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, block)
		return txn.Set(lastQueryKey, b)
	})
}

const ErrNeverQueried = dex.ErrorKind("never queried")

// GetLastReceiveTxQuery retrieves the last time the wallet was queried for
// receive transactions.
func (db *BadgerTxDB) GetLastReceiveTxQuery() (uint64, error) {
	var block uint64
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(lastQueryKey)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrNeverQueried
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

// Run runs the garbage collector in a loop.
func (db *BadgerTxDB) Run(ctx context.Context) {
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
			err := db.Close()
			if err != nil {
				db.log.Errorf("error closing BadgerTxDB: %v", err)
			}
			return
		}
	}
}

// Close closes the database.
func (db *BadgerTxDB) Close() error {
	return db.DB.Close()
}
