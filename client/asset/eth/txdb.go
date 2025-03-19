// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/lexi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// extendedWalletTx is an asset.WalletTransaction extended with additional
// fields used for tracking transactions.
type extendedWalletTx struct {
	*asset.WalletTransaction
	BlockSubmitted uint64         `json:"blockSubmitted"`
	SubmissionTime uint64         `json:"timeStamp"` // seconds
	Nonce          *big.Int       `json:"nonce"`
	Receipt        *types.Receipt `json:"receipt,omitempty"`
	RawTx          dex.Bytes      `json:"rawTx"`
	// NonceReplacement is a transaction with the same nonce that was accepted
	// by the network, meaning this tx was not applied.
	NonceReplacement string `json:"nonceReplacement,omitempty"`
	// FeeReplacement is true if the NonceReplacement is the same tx as this
	// one, just with higher fees.
	FeeReplacement bool `json:"feeReplacement,omitempty"`
	// AssumedLost will be set to true if a transaction is assumed to be lost.
	// This typically requires feedback from the user in response to an
	// ActionRequiredNote.
	AssumedLost bool `json:"assumedLost,omitempty"`

	txHash          common.Hash
	lastCheck       uint64
	savedToDB       bool
	lastBroadcast   time.Time
	lastFeeCheck    time.Time
	actionRequested bool
	actionIgnored   time.Time
	indexed         bool
}

func (wt *extendedWalletTx) MarshalBinary() ([]byte, error) {
	return json.Marshal(wt)
}

func (wt *extendedWalletTx) UnmarshalBinary(b []byte) error {
	if err := json.Unmarshal(b, &wt); err != nil {
		return err
	}
	wt.txHash = common.HexToHash(wt.ID)
	wt.lastBroadcast = time.Unix(int64(wt.SubmissionTime), 0)
	wt.savedToDB = true
	return nil
}

func (t *extendedWalletTx) age() time.Duration {
	return time.Since(time.Unix(int64(t.SubmissionTime), 0))
}

func (t *extendedWalletTx) tx() (*types.Transaction, error) {
	tx := new(types.Transaction)
	return tx, tx.UnmarshalBinary(t.RawTx)
}

type txDB interface {
	dex.Connector
	storeTx(wt *extendedWalletTx) error
	getTxs(n int, refID *common.Hash, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error)
	// getTx gets a single transaction. It is not an error if the tx is not known.
	// In that case, a nil tx is returned.
	getTx(txHash common.Hash) (*extendedWalletTx, error)
	// getPendingTxs returns any recent txs that are not confirmed, ordered
	// by nonce lowest-first.
	getPendingTxs() ([]*extendedWalletTx, error)
}

type TxDB struct {
	*lexi.DB
	txs           *lexi.Table
	allAssetIndex *lexi.Index
	assetIndex    *lexi.Index

	baseChainID uint32
}

var _ txDB = (*TxDB)(nil)

// nonceIndexEntry creates an index entry for iterating over all
// transactions.
// The entry is 8 bytes total:
// - 8 bytes: nonce
func nonceIndexEntry(wt *extendedWalletTx) []byte {
	entry := make([]byte, 8)
	binary.BigEndian.PutUint64(entry[:8], wt.Nonce.Uint64())
	return entry
}

// assetIndexEntry creates an index entry for iterating over transactions of a
// specific asset.
// The entry is 12 bytes total:
// - 4 bytes: asset ID
// - 8 bytes: nonce
func assetIndexEntry(wt *extendedWalletTx, baseChainID uint32) []byte {
	var assetID uint32 = baseChainID
	if wt.TokenID != nil {
		assetID = *wt.TokenID
	}
	assetKey := make([]byte, 4+8)
	binary.BigEndian.PutUint32(assetKey[:4], assetID)
	binary.BigEndian.PutUint64(assetKey[4:], wt.Nonce.Uint64())
	return assetKey
}

// NewTxDB creates a transaction database for storing Ethereum transactions.
func NewTxDB(path string, log dex.Logger, baseChainID uint32) (*TxDB, error) {
	ldb, err := lexi.New(&lexi.Config{
		Path: path,
		Log:  log,
	})
	if err != nil {
		return nil, err
	}

	txs, err := ldb.Table("txs")
	if err != nil {
		return nil, err
	}

	allAssetIndex, err := txs.AddUniqueIndex("allAssets", func(k, v lexi.KV) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return nonceIndexEntry(wt), nil
	})
	if err != nil {
		return nil, err
	}

	assetIndex, err := txs.AddUniqueIndex("asset", func(k, v lexi.KV) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return assetIndexEntry(wt, baseChainID), nil
	})
	if err != nil {
		return nil, err
	}

	return &TxDB{
		DB:            ldb,
		txs:           txs,
		allAssetIndex: allAssetIndex,
		assetIndex:    assetIndex,
		baseChainID:   baseChainID,
	}, nil
}

// storeTx stores a transaction in the database. If a transaction with the
// same hash or nonce already exists, it is replaced.
func (db *TxDB) storeTx(wt *extendedWalletTx) error {
	hash := common.HexToHash(wt.ID)
	err := db.txs.Set(hash[:], wt, lexi.WithReplace())
	if err != nil {
		return err
	}

	return nil
}

// getTx gets a single transaction. It is not an error if the tx is not known.
// In that case, a nil tx is returned.
func (db *TxDB) getTx(txHash common.Hash) (*extendedWalletTx, error) {
	var wt extendedWalletTx
	if err := db.txs.Get(txHash[:], &wt); err != nil {
		if errors.Is(err, lexi.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wt, nil
}

// getTxs fetches n transactions.
//
// If n=0, getTxs returns all transactions in reverse-nonce order.
//
// If no refID is provided:
// - Returns the n most recent transactions in reverse-nonce order
// - The past argument is ignored
//
// If refID is provided:
// - Returns n transactions starting with the referenced transaction
// - If past=false: Results are in increasing nonce order
// - If past=true: Results are in decreasing nonce order
// - The referenced transaction is included in results
// - Returns asset.CoinNotFoundError if refID not found
func (db *TxDB) getTxs(n int, refID *common.Hash, past bool, assetID *uint32) ([]*asset.WalletTransaction, error) {
	var opts []lexi.IterationOption
	if past || refID == nil {
		opts = append(opts, lexi.WithReverse())
	}

	if refID != nil {
		wt, err := db.getTx(*refID)
		if err != nil {
			return nil, asset.CoinNotFoundError
		}

		var entry []byte
		if assetID == nil {
			entry = nonceIndexEntry(wt)
		} else {
			var refTxAssetID uint32 = db.baseChainID
			if wt.TokenID != nil {
				refTxAssetID = *wt.TokenID
			}
			if refTxAssetID != *assetID {
				return nil, fmt.Errorf("token ID mismatch: %d != %d", refTxAssetID, *assetID)
			}
			entry = assetIndexEntry(wt, db.baseChainID)
		}

		opts = append(opts, lexi.WithSeek(entry))
	}

	txs := make([]*asset.WalletTransaction, 0, n)
	iterFunc := func(it *lexi.Iter) error {
		wt := new(extendedWalletTx)
		err := it.V(func(vB []byte) error {
			return wt.UnmarshalBinary(vB)
		})
		if err != nil {
			return err
		}

		txs = append(txs, wt.WalletTransaction)

		if n > 0 && len(txs) >= n {
			return lexi.ErrEndIteration
		}

		return nil
	}

	if assetID == nil {
		return txs, db.allAssetIndex.Iterate(nil, iterFunc, opts...)
	}

	return txs, db.assetIndex.Iterate(*assetID, iterFunc, opts...)
}

// getPendingTxs returns all unconfirmed transactions that have not been marked
// as lost.
func (db *TxDB) getPendingTxs() (txs []*extendedWalletTx, err error) {
	const numConfirmedTxsToCheck = 20
	var numConfirmedTxs int

	db.allAssetIndex.Iterate(nil, func(it *lexi.Iter) error {
		wt := new(extendedWalletTx)
		err := it.V(func(vB []byte) error {
			return wt.UnmarshalBinary(vB)
		})
		if err != nil {
			return err
		}
		if wt.AssumedLost {
			return nil
		}
		if !wt.Confirmed {
			numConfirmedTxs = 0
			txs = append(txs, wt)
		} else {
			numConfirmedTxs++
			if numConfirmedTxs >= numConfirmedTxsToCheck {
				return lexi.ErrEndIteration
			}
		}
		return nil
	}, lexi.WithReverse())
	return
}
