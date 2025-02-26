// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding"
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
	CallData       dex.Bytes      `json:"callData"`
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
	if t.IsUserOp {
		return nil, fmt.Errorf("cannot get raw tx for user op")
	}
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
}

var _ txDB = (*TxDB)(nil)

// Order by blockNumber, group userOps and regular transactions together,
// then order by nonce, then include ID to avoid collisions.
func allAssetIndexEntry(wt *extendedWalletTx) []byte {
	blockNumber := wt.BlockNumber
	if blockNumber == 0 {
		blockNumber = ^uint64(0)
	}
	isUserOpB := []byte{0}
	if wt.IsUserOp {
		isUserOpB = []byte{1}
	}
	entry := make([]byte, 8+1+8+32)
	binary.BigEndian.PutUint64(entry[:8], blockNumber)
	entry[8] = isUserOpB[0]
	var nonce uint64
	if wt.Nonce != nil {
		nonce = wt.Nonce.Uint64()
	}
	binary.BigEndian.PutUint64(entry[9:17], nonce)
	copy(entry[17:], wt.ID)
	return entry
}

func assetIndexEntry(wt *extendedWalletTx) []byte {
	var assetID uint32 = BipID
	if wt.TokenID != nil {
		assetID = *wt.TokenID
	}
	allAssetIndexKey := allAssetIndexEntry(wt)
	assetKey := make([]byte, 4+len(allAssetIndexKey))
	binary.BigEndian.PutUint32(assetKey[:4], assetID)
	copy(assetKey[4:], allAssetIndexKey)
	return assetKey
}

func NewTxDB(path string, log dex.Logger) (*TxDB, error) {
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

	allAssetIndex, err := txs.AddIndex("allAssets", func(k, v encoding.BinaryMarshaler) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return allAssetIndexEntry(wt), nil
	})
	if err != nil {
		return nil, err
	}

	assetIndex, err := txs.AddIndex("asset", func(k, v encoding.BinaryMarshaler) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return assetIndexEntry(wt), nil
	})
	if err != nil {
		return nil, err
	}

	return &TxDB{
		DB:            ldb,
		txs:           txs,
		allAssetIndex: allAssetIndex,
		assetIndex:    assetIndex,
	}, nil
}

func (db *TxDB) storeTx(wt *extendedWalletTx) error {
	hash := common.HexToHash(wt.ID)
	return db.txs.Set(lexi.B(hash[:]), wt, lexi.WithReplace())
}

func (db *TxDB) getTx(txHash common.Hash) (*extendedWalletTx, error) {
	var wt extendedWalletTx
	if err := db.txs.Get(lexi.B(txHash[:]), &wt); err != nil {
		if errors.Is(err, lexi.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &wt, nil
}

func (db *TxDB) getTxs(n int, refID *common.Hash, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error) {
	var assetID uint32 = BipID
	if tokenID != nil {
		assetID = *tokenID
	}

	var opts []lexi.IterationOption
	if past || refID == nil {
		opts = append(opts, lexi.WithReverse())
	}

	if refID != nil {
		txHash := *refID
		wt, err := db.getTx(txHash)
		if err != nil {
			return nil, fmt.Errorf("error getting reference tx %s: %w", txHash, err)
		}
		var entry []byte
		if tokenID == nil {
			entry = allAssetIndexEntry(wt)
		} else {
			if assetID != *tokenID {
				return nil, fmt.Errorf("token ID mismatch: %d != %d", assetID, *tokenID)
			}
			entry = assetIndexEntry(wt)
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

	if tokenID == nil {
		return txs, db.allAssetIndex.Iterate(nil, iterFunc, opts...)
	}

	return txs, db.assetIndex.Iterate(assetID, iterFunc, opts...)
}

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
