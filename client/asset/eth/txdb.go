// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/dgraph-io/badger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// extendedWalletTx is an asset.WalletTransaction extended with additional
// fields used for tracking transactions.
type extendedWalletTx struct {
	mtx sync.RWMutex
	*asset.WalletTransaction
	BlockSubmitted uint64 `json:"blockSubmitted"`
	SubmissionTime uint64 `json:"timeStamp"`

	lastCheck uint64
	savedToDB bool
}

// monitoredTx is used to keep track of redemption transactions that have not
// yet been confirmed. If a transaction has to be replaced due to the fee
// being too low or another transaction being mined with the same nonce,
// the replacement transaction's ID is recorded in the replacementTx field.
// replacedTx is used to maintain a doubly linked list, which allows deletion
// of transactions that were replaced after a transaction is confirmed.
type monitoredTx struct {
	tx             *types.Transaction
	blockSubmitted uint64

	// This mutex must be held during the entire process of confirming
	// a transaction. This is to avoid confirmations of the same
	// transactions happening concurrently resulting in more than one
	// replacement for the same transaction.
	mtx           sync.Mutex
	replacementTx *common.Hash
	// replacedTx could be set when the tx is created, be immutable, and not
	// need the mutex, but since Redeem doesn't know if the transaction is a
	// replacement or a new one, this variable is set in recordReplacementTx.
	replacedTx        *common.Hash
	errorsBroadcasted uint16
}

// MarshalBinary marshals a monitoredTx into a byte array.
// It satisfies the encoding.BinaryMarshaler interface for monitoredTx.
func (m *monitoredTx) MarshalBinary() (data []byte, err error) {
	b := encode.BuildyBytes{0}
	txB, err := m.tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error marshaling tx: %v", err)
	}
	b = b.AddData(txB)

	blockB := make([]byte, 8)
	binary.BigEndian.PutUint64(blockB, m.blockSubmitted)
	b = b.AddData(blockB)

	if m.replacementTx != nil {
		replacementTxHash := m.replacementTx[:]
		b = b.AddData(replacementTxHash)
	}

	return b, nil
}

// UnmarshalBinary loads a data from a marshalled byte array into a
// monitoredTx.
func (m *monitoredTx) UnmarshalBinary(data []byte) error {
	ver, pushes, err := encode.DecodeBlob(data)
	if err != nil {
		return err
	}
	if ver != 0 {
		return fmt.Errorf("unknown version %d", ver)
	}
	if len(pushes) != 2 && len(pushes) != 3 {
		return fmt.Errorf("wrong number of pushes %d", len(pushes))
	}
	m.tx = &types.Transaction{}
	if err := m.tx.UnmarshalBinary(pushes[0]); err != nil {
		return fmt.Errorf("error reading tx: %w", err)
	}

	m.blockSubmitted = binary.BigEndian.Uint64(pushes[1])

	if len(pushes) == 3 {
		var replacementTxHash common.Hash
		copy(replacementTxHash[:], pushes[2])
		m.replacementTx = &replacementTxHash
	}

	return nil
}

var (
	// noncePrefix is the prefix for the key used to map a nonce to an
	// extendedWalletTx.
	noncePrefix = []byte("nonce-")
	// txHashPrefix is the prefix for the key used to map a transaction hash
	// to a nonce key.
	txHashPrefix = []byte("txHash-")
	// monitoredTxPrefix is the prefix for the key used to map a transaction
	// hash to a monitoredTx.
	monitoredTxPrefix = []byte("monitoredTx-")
	// dbVersionKey is the key used to store the database version.
	dbVersionKey = []byte("dbVersion")
)

func nonceKey(nonce uint64) []byte {
	key := make([]byte, len(noncePrefix)+8)
	copy(key, noncePrefix)
	binary.BigEndian.PutUint64(key[len(noncePrefix):], nonce)
	return key
}

func nonceFromKey(nk []byte) (uint64, error) {
	if !bytes.HasPrefix(nk, noncePrefix) {
		return 0, fmt.Errorf("nonce key %x does not have nonce prefix %x", nk, noncePrefix)
	}
	return binary.BigEndian.Uint64(nk[len(noncePrefix):]), nil
}

func txIDKey(txID string) []byte {
	key := make([]byte, len(txHashPrefix)+len([]byte(txID)))
	copy(key, txHashPrefix)
	copy(key[len(txHashPrefix):], []byte(txID))
	return key
}

func monitoredTxKey(txHash dex.Bytes) []byte {
	key := make([]byte, len(monitoredTxPrefix)+len(txHash))
	copy(key, monitoredTxPrefix)
	copy(key[len(monitoredTxPrefix):], txHash)
	return key
}

func monitoredTxHashFromKey(mtk []byte) (common.Hash, error) {
	if !bytes.HasPrefix(mtk, monitoredTxPrefix) {
		return common.Hash{}, fmt.Errorf("monitored tx key %x does not have monitored tx prefix %x", mtk, monitoredTxPrefix)
	}
	var txHash common.Hash
	copy(txHash[:], mtk[len(monitoredTxPrefix):])
	return txHash, nil
}

var maxNonceKey = nonceKey(math.MaxUint64)

// initialDBVersion only contained mappings from txHash -> monitoredTx.
const initialDBVersion = 0

// prefixDBVersion contains three mappings each marked with a prefix:
//
//	nonceKey -> extendedWalletTx (noncePrefix)
//	txHash -> nonceKey (txHashPrefix)
//	txHash -> monitoredTx (monitoredTxPrefix)
const prefixDBVersion = 1
const txDBVersion = prefixDBVersion

type txDB interface {
	storeTx(nonce uint64, wt *extendedWalletTx) error
	removeTx(id string) error
	getTxs(n int, refID *string, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error)
	getPendingTxs() (map[uint64]*extendedWalletTx, error)
	storeMonitoredTx(txHash common.Hash, tx *monitoredTx) error
	getMonitoredTxs() (map[common.Hash]*monitoredTx, error)
	removeMonitoredTxs([]common.Hash) error
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
		log.Warnf("error opening badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		log.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		db, err = badger.Open(opts)
	}
	if err != nil {
		return nil, err
	}

	txDB := &badgerTxDB{
		DB:  db,
		log: log,
	}

	err = txDB.updateVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to update db: %w", err)
	}

	return txDB, nil
}

// updateVersion updates the DB to the latest version. In version 0,
// only a mapping from txHash to monitoredTx was stored, with no
// prefixes.
func (s *badgerTxDB) updateVersion() error {
	// Check if the database version is stored. If not, the db
	// is version 0.
	var version int
	err := s.View(func(txn *badger.Txn) error {
		item, err := txn.Get(dbVersionKey)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil
			}
			return err
		}
		return item.Value(func(versionB []byte) error {
			version = int(binary.BigEndian.Uint64(versionB))
			return nil
		})
	})
	if err != nil {
		s.log.Errorf("error retrieving database version: %v", err)
	}

	if version == initialDBVersion {
		err = s.Update(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				key := item.Key()
				newKey := monitoredTxKey(key)
				monitoredTxB, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				err = txn.Set(newKey, monitoredTxB)
				if err != nil {
					return err
				}
				err = txn.Delete(key)
				if err != nil {
					return err
				}
			}

			versionB := make([]byte, 8)
			binary.BigEndian.PutUint64(versionB, 1)
			return txn.Set(dbVersionKey, versionB)
		})
		if err != nil {
			return err
		}
		s.log.Infof("Updated database to version %d", prefixDBVersion)
	} else if version > txDBVersion {
		return fmt.Errorf("database version %d is not supported", version)
	}

	return nil
}

func (s *badgerTxDB) close() error {
	return s.Close()
}

func (s *badgerTxDB) run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := s.RunValueLogGC(0.5)
			if err != nil && !errors.Is(err, badger.ErrNoRewrite) {
				s.log.Errorf("garbage collection error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// storeTx stores a mapping from nonce to extendedWalletTx and a mapping from
// transaction hash to nonce so transactions can be looked up by hash. If a
// nonce already exists, the extendedWalletTx is overwritten.
func (s *badgerTxDB) storeTx(nonce uint64, wt *extendedWalletTx) error {
	wtB, err := json.Marshal(wt)
	if err != nil {
		return err
	}
	nk := nonceKey(nonce)
	tk := txIDKey(wt.ID)

	return s.Update(func(txn *badger.Txn) error {
		oldWtItem, err := txn.Get(nk)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		// If there is an existing transaction with this nonce, delete the
		// mapping from tx hash to nonce.
		if err == nil {
			oldWt := new(extendedWalletTx)
			err = oldWtItem.Value(func(oldWtB []byte) error {
				err := json.Unmarshal(oldWtB, oldWt)
				if err != nil {
					s.log.Errorf("unable to unmarhsal wallet transaction: %s: %v", string(oldWtB), err)
				}
				return err
			})
			if err == nil && oldWt.ID != wt.ID {
				err = txn.Delete(txIDKey(oldWt.ID))
				if err != nil {
					s.log.Errorf("failed to delete old tx id: %s: %v", oldWt.ID, err)
				}
			}
		}

		// Store nonce key -> wallet transaction
		if err := txn.Set(nk, wtB); err != nil {
			return err
		}

		// Store tx hash -> nonce key
		return txn.Set(tk, nk)
	})
}

// removeTx removes a tx from the db.
func (s *badgerTxDB) removeTx(id string) error {
	tk := txIDKey(id)

	return s.Update(func(txn *badger.Txn) error {
		txIDEntry, err := txn.Get(tk)
		if err != nil {
			return err
		}
		err = txn.Delete(tk)
		if err != nil {
			return err
		}

		nk, err := txIDEntry.ValueCopy(nil)
		if err != nil {
			return err
		}

		return txn.Delete(nk)
	})
}

// getTxs returns the n more recent transaction if refID is nil, or the
// n transactions before/after refID depending on the value of past. The
// transactions are returned in reverse chronological order.
// If a non-nil refID is not found, asset.CoinNotFoundError is returned.
func (s *badgerTxDB) getTxs(n int, refID *string, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error) {
	var txs []*asset.WalletTransaction

	err := s.View(func(txn *badger.Txn) error {
		var startNonceKey []byte
		if refID != nil {
			// Get the nonce for the provided tx hash.
			tk := txIDKey(*refID)
			item, err := txn.Get(tk)
			if err != nil {
				return asset.CoinNotFoundError
			}
			if startNonceKey, err = item.ValueCopy(nil); err != nil {
				return err
			}
		} else {
			past = true
		}
		if startNonceKey == nil {
			startNonceKey = maxNonceKey
		}

		opts := badger.DefaultIteratorOptions
		opts.Reverse = past
		opts.Prefix = noncePrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(startNonceKey); it.Valid() && (n <= 0 || len(txs) < n); it.Next() {
			item := it.Item()
			err := item.Value(func(wtB []byte) error {
				wt := new(asset.WalletTransaction)
				err := json.Unmarshal(wtB, wt)
				if err != nil {
					s.log.Errorf("unable to unmarhsal wallet transaction: %s: %v", string(wtB), err)
					return err
				}
				if tokenID != nil && (wt.TokenID == nil || *tokenID != *wt.TokenID) {
					return nil
				}
				if past {
					txs = append(txs, wt)
				} else {
					txs = append([]*asset.WalletTransaction{wt}, txs...)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return txs, err
}

// getPendingTxs returns a map of nonce to extendedWalletTx for all
// pending transactions.
func (s *badgerTxDB) getPendingTxs() (map[uint64]*extendedWalletTx, error) {
	// We will be iterating backwards from the most recent nonce.
	// If we find numConfirmedTxsToCheck consecutive confirmed transactions,
	// we can stop iterating.
	const numConfirmedTxsToCheck = 20

	txs := make(map[uint64]*extendedWalletTx, 4)

	err := s.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = noncePrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		var numConfirmedTxs int
		for it.Seek(maxNonceKey); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(wtB []byte) error {
				wt := new(extendedWalletTx)
				err := json.Unmarshal(wtB, wt)
				if err != nil {
					s.log.Errorf("unable to unmarhsal wallet transaction: %s: %v", string(wtB), err)
					return err
				}
				if !wt.Confirmed {
					numConfirmedTxs = 0
					nonce, err := nonceFromKey(item.Key())
					if err != nil {
						return err
					}
					txs[nonce] = wt
				} else {
					numConfirmedTxs++
					if numConfirmedTxs >= numConfirmedTxsToCheck {
						return nil
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return txs, err
}

// storeMonitoredTx stores a monitoredTx in the database.
func (s *badgerTxDB) storeMonitoredTx(txHash common.Hash, tx *monitoredTx) error {
	txKey := monitoredTxKey(txHash.Bytes())
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	return s.Update(func(txn *badger.Txn) error {
		return txn.Set(txKey, txBytes)
	})
}

// getMonitoredTxs returns a map of transaction hash to monitoredTx for all
// monitored transactions.
func (s *badgerTxDB) getMonitoredTxs() (map[common.Hash]*monitoredTx, error) {
	monitoredTxs := make(map[common.Hash]*monitoredTx)

	err := s.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = monitoredTxPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(monitoredTxPrefix); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(txBytes []byte) error {
				tx := new(monitoredTx)
				err := tx.UnmarshalBinary(txBytes)
				if err != nil {
					return err
				}
				txHash, err := monitoredTxHashFromKey(item.Key())
				if err != nil {
					return err
				}
				monitoredTxs[txHash] = tx
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return monitoredTxs, err
}

// removeMonitoredTxs removes the monitored transactions with the provided
// hashes from the database.
func (s *badgerTxDB) removeMonitoredTxs(txHashes []common.Hash) error {
	return s.Update(func(txn *badger.Txn) error {
		for _, txHash := range txHashes {
			txKey := monitoredTxKey(txHash.Bytes())
			err := txn.Delete(txKey)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
