package eth

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/common"
)

var (
	// noncePrefix is the prefix for the key used to map a nonce to an
	// extendedWalletTx.
	noncePrefix = []byte("nonce-")
	// txHashPrefix is the prefix for the key used to map a transaction hash
	// to a nonce key.
	txHashPrefix = []byte("txHash-")
	// dbVersionKey is the key used to store the database version.
	dbVersionKey = []byte("dbVersion")
)

func nonceKey(nonce uint64) []byte {
	key := make([]byte, len(noncePrefix)+8)
	copy(key, noncePrefix)
	binary.BigEndian.PutUint64(key[len(noncePrefix):], nonce)
	return key
}

func txKey(txHash common.Hash) []byte {
	key := make([]byte, len(txHashPrefix)+20)
	copy(key, txHashPrefix)
	copy(key[len(txHashPrefix):], txHash[:])
	return key
}

// badgerDB returns ErrConflict when a read happening in a update (read/write)
// transaction is stale. This function retries updates multiple times in
// case of conflicts.
func (db *badgerTxDB) Update(f func(txn *badger.Txn) error) (err error) {
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

var maxNonceKey = nonceKey(math.MaxUint64)

// initialDBVersion only contained mappings from txHash -> monitoredTx.
// const initialDBVersion = 0

// prefixDBVersion contains two mappings each marked with a prefix:
//
//	nonceKey -> extendedWalletTx (noncePrefix)
//	txHash -> nonceKey (txHashPrefix)
// const prefixDBVersion = 1

// txMappingVersion reverses the semantics so that all txs are accessible
// by txHash.
//
// nonceKey -> best-known txHash
// txHash -> extendedWalletTx, which contains a nonce
const txMappingVersion = 2

const txDBVersion = txMappingVersion

// badgerTxDB was used prior to the introduction of the lexi db. It is
// only used to migrate the legacy db to the new lexi db.
type badgerTxDB struct {
	*badger.DB
	filePath string
	log      dex.Logger
	updateWG sync.WaitGroup
}

// newBadgerTxDB creates a legacy badgerTxDB.
func newBadgerTxDB(filePath string, log dex.Logger) (*badgerTxDB, error) {
	// If memory use is a concern, could try
	//   .WithValueLogLoadingMode(options.FileIO) // default options.MemoryMap
	//   .WithMaxTableSize(sz int64); // bytes, default 6MB
	//   .WithValueLogFileSize(sz int64), bytes, default 1 GB, must be 1MB <= sz <= 1GB
	opts := badger.DefaultOptions(filePath).WithLogger(&badgerLoggerWrapper{log})
	var err error
	bdb, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	db := &badgerTxDB{
		DB:       bdb,
		filePath: filePath,
		log:      log,
	}

	return db, nil
}

func (db *badgerTxDB) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if err := db.updateVersion(); err != nil {
		return nil, fmt.Errorf("failed to update db: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer db.Close()
		defer db.updateWG.Wait()
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
	}()
	return &wg, nil
}

// txForNonce gets the registered for the given nonce.
func txForNonce(txn *badger.Txn, nonce uint64) (tx *extendedWalletTx, err error) {
	nk := nonceKey(nonce)
	txHashi, err := txn.Get(nk)
	if err != nil {
		return nil, err
	}
	return tx, txHashi.Value(func(txHashB []byte) error {
		var txHash common.Hash
		copy(txHash[:], txHashB)
		txi, err := txn.Get(txKey(txHash))
		if err != nil {
			return err
		}
		return txi.Value(func(wtB []byte) error {
			tx, err = unmarshalTx(wtB)
			return err
		})
	})
}

// txForHash get the extendedWalletTx at the given tx hash and checks for any
// unsaved nonce replacement.
func txForHash(txn *badger.Txn, txHash common.Hash) (wt *extendedWalletTx, err error) {
	txi, err := txn.Get(txKey(txHash))
	if err != nil {
		return nil, err
	}
	return wt, txi.Value(func(wtB []byte) error {
		wt, err = unmarshalTx(wtB)
		if err != nil || wt.Confirmed || wt.NonceReplacement != "" {
			return err
		}
		nonceTx, err := txForNonce(txn, wt.Nonce.Uint64())
		if err != nil {
			return err
		}
		if nonceTx.txHash != wt.txHash && nonceTx.Confirmed {
			wt.NonceReplacement = wt.txHash.String()
		}
		return nil
	})
}

// updateVersion updates the DB to the latest version. In version 0,
// only a mapping from txHash to monitoredTx was stored, with no
// prefixes.
func (db *badgerTxDB) updateVersion() error {
	// Check if the database version is stored. If not, the db
	// is version 0.
	var version int
	err := db.View(func(txn *badger.Txn) error {
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
		db.log.Errorf("error retrieving database version: %v", err)
	}

	if version < txMappingVersion {
		if err := db.DB.DropAll(); err != nil {
			return fmt.Errorf("error deleting DB entries for version upgrade: %w", err)
		}
		versionB := make([]byte, 8)
		binary.BigEndian.PutUint64(versionB, txMappingVersion)
		if err = db.Update(func(txn *badger.Txn) error {
			return txn.Set(dbVersionKey, versionB)
		}); err != nil {
			return err
		}
		db.log.Infof("Upgraded DB to version %d by deleting everything and starting from scratch.", txMappingVersion)
	} else if version > txDBVersion {
		return fmt.Errorf("database version %d is not supported", version)
	}

	return nil
}

// unmarshalTx attempts to decode the binary tx and sets some unexported fields.
func unmarshalTx(wtB []byte) (wt *extendedWalletTx, err error) {
	if err = json.Unmarshal(wtB, &wt); err != nil {
		return nil, err
	}
	wt.txHash = common.HexToHash(wt.ID)
	wt.lastBroadcast = time.Unix(int64(wt.SubmissionTime), 0)
	wt.savedToDB = true
	return
}

// storeTx stores a mapping from nonce to extendedWalletTx and a mapping from
// transaction hash to nonce so transactions can be looked up by hash. If a
// nonce already exists, the extendedWalletTx is overwritten.
func (db *badgerTxDB) storeTx(wt *extendedWalletTx) error {
	wtB, err := json.Marshal(wt)
	if err != nil {
		return err
	}
	nonce := wt.Nonce.Uint64()

	return db.Update(func(txn *badger.Txn) error {
		// If there is not a confirmed tx at this tx's nonce, map the nonce
		// to this tx.
		nonceTx, err := txForNonce(txn, nonce)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return fmt.Errorf("error reading nonce tx: %w", err)
		}
		// If we don't have a tx stored at the nonce or the tx stored at the
		// nonce is not confirmed, put this one there instead, unless this one
		// has been marked as nonce-replaced.
		if (nonceTx == nil || !nonceTx.Confirmed) && wt.NonceReplacement == "" {
			if err := txn.Set(nonceKey(nonce), wt.txHash[:]); err != nil {
				return fmt.Errorf("error mapping nonce to tx hash: %w", err)
			}
		}
		// Store the tx at its hash.
		return txn.Set(txKey(wt.txHash), wtB)
	})
}

// getAllEntries returns all entries in the database. This is used
// in order to copy the entries from the legacy db into the new lexi db.
func (db *badgerTxDB) getAllEntries() ([]*extendedWalletTx, error) {
	txs := make([]*extendedWalletTx, 0, 16)

	return txs, db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true
		opts.Prefix = noncePrefix
		startNonceKey := maxNonceKey

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(startNonceKey); it.Valid(); it.Next() {
			txHashi := it.Item()
			if err := txHashi.Value(func(txHashB []byte) error {
				var txHash common.Hash
				copy(txHash[:], txHashB)
				wt, err := txForHash(txn, txHash)
				if err != nil {
					return err
				}
				txs = append(txs, wt)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger. It also lowers the log level of Infof to Debugf
// and Debugf to Tracef.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Debugf -> dex.Logger.Tracef
func (log *badgerLoggerWrapper) Debugf(s string, a ...any) {
	log.Tracef(s, a...)
}

func (log *badgerLoggerWrapper) Debug(a ...any) {
	log.Trace(a...)
}

// Infof -> dex.Logger.Debugf
func (log *badgerLoggerWrapper) Infof(s string, a ...any) {
	log.Debugf(s, a...)
}

func (log *badgerLoggerWrapper) Info(a ...any) {
	log.Debug(a...)
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...any) {
	log.Warnf(s, a...)
}

func (log *badgerLoggerWrapper) Warning(a ...any) {
	log.Warn(a...)
}
