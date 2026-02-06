// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/lexi"
	"github.com/dgraph-io/badger/v4"
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
var secNoncePrefix = []byte("sn")
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
	filePath string
	log      dex.Logger
	seq      *badger.Sequence
	running  atomic.Bool
	wg       sync.WaitGroup
	ctx      context.Context
}

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger. It also lowers the log level of Infof to Debugf.
// Debugf is discarded as badger's debug logs are too noisy even for trace.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Debugf is discarded - badger's debug logs are too verbose.
func (log *badgerLoggerWrapper) Debugf(s string, a ...any) {}

// Infof -> dex.Logger.Debugf
func (log *badgerLoggerWrapper) Infof(s string, a ...any) {
	log.Debugf(s, a...)
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...any) {
	log.Warnf(s, a...)
}

func NewBadgerTxDB(filePath string, log dex.Logger) *BadgerTxDB {
	return &BadgerTxDB{
		filePath: filePath,
		log:      log,
	}
}

func (db *BadgerTxDB) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	v4Path, needs, err := lexi.NeedsV1toV4Update(db.filePath)
	if err != nil {
		return nil, err
	}
	opts := badger.DefaultOptions(v4Path).WithLogger(&badgerLoggerWrapper{db.log})
	if needs {
		db.DB, err = lexi.BadgerV1Update(db.filePath, v4Path, db.log, opts)
		if err != nil {
			db.log.Warnf("Unable to update old db, creating new one: %v", err)
			db.DB, err = badger.Open(opts)
			if err != nil {
				return nil, err
			}
		}
	} else {
		db.DB, err = badger.Open(opts)
		if err != nil {
			return nil, err
		}
	}
	db.ctx = ctx
	db.seq, err = db.GetSequence([]byte("seq"), 10)
	if err != nil {
		return nil, err
	}

	db.running.Store(true)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

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
				db.running.Store(false)
				db.wg.Wait()
				if err := db.seq.Release(); err != nil {
					db.log.Errorf("error releasing sequence: %v", err)
				}
				db.Close()
				return
			}
		}
	}()

	return &wg, nil
}

// badgerDB returns ErrConflict when a read happening in a update (read/write)
// transaction is stale. This function retries updates multiple times in
// case of conflicts.
func (db *BadgerTxDB) handleConflictWithBackoff(update func() error) (err error) {
	maxRetries := 10
	sleepTime := 5 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		sleepTime *= 2
		err = update()
		if err != badger.ErrConflict {
			return err
		}
		time.Sleep(sleepTime)
	}

	return err
}

func (db *BadgerTxDB) newBlockKey(blockNumber uint64) ([]byte, error) {
	seq, err := db.seq.Next()
	if err != nil {
		return nil, err
	}
	if blockNumber == 0 {
		return pendingKey(seq), nil
	}
	return blockKey(blockNumber, seq), nil
}

func hasPrefix(b, prefix []byte) bool {
	if len(b) < len(prefix) {
		return false
	}
	return bytes.Equal(b[:len(prefix)], prefix)
}

func (db *BadgerTxDB) storeTx(tx *ExtendedWalletTx) error {
	return db.Update(func(txn *badger.Txn) error {
		txKey := txKey(tx.ID)
		txKeyItem, err := txn.Get(txKey)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		var key []byte
		if err == nil { // already stored
			currBlockKey, err := txKeyItem.ValueCopy(nil)
			if err != nil {
				return err
			}
			err = txn.Delete(currBlockKey)
			if err != nil {
				return err
			}
			// Keep the same key unless a pending tx that has been confirmed,
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

		if key == nil {
			key, err = db.newBlockKey(tx.BlockNumber)
			if err != nil {
				return err
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

// StoreTx stores a transaction in the database.
func (db *BadgerTxDB) StoreTx(tx *ExtendedWalletTx) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	return db.handleConflictWithBackoff(func() error { return db.storeTx(tx) })
}

func (db *BadgerTxDB) deleteSecNonce(pubNonce []byte) error {
	return db.Update(func(txn *badger.Txn) error {
		secNonceKey := make([]byte, len(secNoncePrefix)+len(pubNonce))
		copy(secNonceKey, secNoncePrefix)
		copy(secNonceKey[len(secNoncePrefix):], pubNonce)
		return txn.Delete(secNonceKey)
	})
}

// DeleteSecNonce deletes the secret nonce for a given public nonce.
// This is used to clean up the database after a private swap is complete.
func (db *BadgerTxDB) DeleteSecNonce(pubNonce []byte) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	return db.handleConflictWithBackoff(func() error { return db.deleteSecNonce(pubNonce) })
}

func (db *BadgerTxDB) storeSecNonce(pubNonce, secNonce []byte) error {
	return db.Update(func(txn *badger.Txn) error {
		secNonceKey := make([]byte, len(secNoncePrefix)+len(pubNonce))
		copy(secNonceKey, secNoncePrefix)
		copy(secNonceKey[len(secNoncePrefix):], pubNonce)
		return txn.Set(secNonceKey, secNonce)
	})
}

const (
	gcmNonceSize = 12
)

func encryptAES(key []byte, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %v", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func decryptAES(key []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	if len(ciphertext) < gcmNonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:gcmNonceSize], ciphertext[gcmNonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %v", err)
	}

	return plaintext, nil
}

// StoreSecNonce stores the secret nonce for a given public nonce. encKey
// is used to encrypt the secret nonce before it is stored in the database.
func (db *BadgerTxDB) StoreSecNonce(pubNonce, secNonce, encKey []byte) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	encSecNonce, err := encryptAES(encKey, secNonce)
	if err != nil {
		return fmt.Errorf("error encrypting secret nonce: %w", err)
	}

	return db.handleConflictWithBackoff(func() error { return db.storeSecNonce(pubNonce, encSecNonce) })
}

func (db *BadgerTxDB) getSecNonce(pubNonce []byte) ([]byte, error) {
	var secNonce []byte
	err := db.View(func(txn *badger.Txn) error {
		secNonceKey := make([]byte, len(secNoncePrefix)+len(pubNonce))
		copy(secNonceKey, secNoncePrefix)
		copy(secNonceKey[len(secNoncePrefix):], pubNonce)
		item, err := txn.Get(secNonceKey)
		if err != nil {
			return err
		}
		secNonce, err = item.ValueCopy(nil)
		return err
	})
	return secNonce, err
}

// GetSecNonce retrieves the secret nonce for a given public nonce. The
// same encryption key must be used as the one used to encrypt the secret
// nonce when it was stored.
func (db *BadgerTxDB) GetSecNonce(pubNonce, encKey []byte) ([]byte, error) {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return nil, fmt.Errorf("database is not running")
	}

	encSecNonce, err := db.getSecNonce(pubNonce)
	if err != nil {
		return nil, err
	}

	return decryptAES(encKey, encSecNonce)
}

func (db *BadgerTxDB) markTxAsSubmitted(txID string) error {
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

// MarkTxAsSubmitted should be called when a previously stored transaction
// that had not yet been sent to the network is sent to the network.
// asset.CoinNotFoundError is returned if the transaction is not in the
// database.
func (db *BadgerTxDB) MarkTxAsSubmitted(txID string) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	return db.handleConflictWithBackoff(func() error { return db.markTxAsSubmitted(txID) })
}

// GetTxs retrieves n transactions from the database. refID optionally
// takes a transaction ID, and returns that transaction and the at most
// (n - 1) transactions that were made either before or after it, depending
// on the value of past. If refID is nil, the most recent n transactions
// are returned, and the value of past is ignored. If the transaction with
// ID refID is not in the database, asset.CoinNotFoundError is returned.
// Unsubmitted transactions are not returned.
func (db *BadgerTxDB) GetTxs(req *asset.TxHistoryRequest) (*asset.TxHistoryResponse, error) {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return nil, fmt.Errorf("database is not running")
	}

	var txs []*asset.WalletTransaction
	var moreAvailable bool
	ignoreTypes := req.IngoreTypesLookup()
	if err := db.View(func(txn *badger.Txn) error {
		var startKey []byte
		if req.RefID != nil {
			txKey := txKey(*req.RefID)
			txKeyItem, err := txn.Get(txKey)
			if err != nil {
				return asset.CoinNotFoundError
			}

			startKey, err = txKeyItem.ValueCopy(nil)
			if err != nil {
				return err
			}
		}
		n := req.N
		past := req.Past
		if startKey == nil {
			past = true
			startKey = maxPendingKey
		}

		opts := badger.DefaultIteratorOptions
		opts.Reverse = past
		it := txn.NewIterator(opts)
		defer it.Close()

		canIterate := func() bool {
			if !(it.ValidForPrefix(blockPrefix) || it.ValidForPrefix(pendingPrefix)) {
				return false
			}
			return n <= 0 || len(txs) <= n
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
			if ignoreTypes[wt.Type] {
				continue
			}
			if !wt.Submitted {
				continue
			}
			if n > 0 && len(txs) == n {
				moreAvailable = true
				break
			}
			if past {
				txs = append(txs, wt.WalletTransaction)
			} else {
				txs = append([]*asset.WalletTransaction{wt.WalletTransaction}, txs...)
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}
	return &asset.TxHistoryResponse{
		Txs:           txs,
		MoreAvailable: moreAvailable,
	}, nil
}

// GetTx retrieves a transaction by its ID. If the transaction is not in
// the database, asset.CoinNotFoundError is returned.
func (db *BadgerTxDB) GetTx(txID string) (*asset.WalletTransaction, error) {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return nil, fmt.Errorf("database is not running")
	}
	r, err := db.GetTxs(&asset.TxHistoryRequest{
		N:     1,
		RefID: &txID,
	})
	if err != nil {
		return nil, err
	}
	if len(r.Txs) == 0 {
		// This should never happen.
		return nil, fmt.Errorf("no results returned from getTxs")
	}
	return r.Txs[0], nil
}

// GetPendingTxs returns all transactions that have not yet been confirmed.
func (db *BadgerTxDB) GetPendingTxs() ([]*ExtendedWalletTx, error) {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return nil, fmt.Errorf("database is not running")
	}

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

func (db *BadgerTxDB) removeTx(txID string) error {
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

// RemoveTx removes a transaction from the database. If the transaction is
// not in the database, asset.CoinNotFoundError is returned.
func (db *BadgerTxDB) RemoveTx(txID string) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	return db.handleConflictWithBackoff(func() error { return db.removeTx(txID) })
}

func (db *BadgerTxDB) setLastReceiveTxQuery(block uint64) error {
	return db.Update(func(txn *badger.Txn) error {
		// use binary big endian
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, block)
		return txn.Set(lastQueryKey, b)
	})
}

// SetLastReceiveTxQuery stores the last time the wallet was queried for
// receive transactions. This is required to know how far back to query
// for incoming transactions that were received while the wallet is
// offline.
func (db *BadgerTxDB) SetLastReceiveTxQuery(block uint64) error {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return fmt.Errorf("database is not running")
	}

	return db.handleConflictWithBackoff(func() error { return db.setLastReceiveTxQuery(block) })
}

const ErrNeverQueried = dex.ErrorKind("never queried")

// GetLastReceiveTxQuery retrieves the last time the wallet was queried for
// receive transactions.
func (db *BadgerTxDB) GetLastReceiveTxQuery() (uint64, error) {
	db.wg.Add(1)
	defer db.wg.Done()
	if !db.running.Load() {
		return 0, fmt.Errorf("database is not running")
	}

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
