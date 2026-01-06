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
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/lexi"
	"github.com/dgraph-io/badger"
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

	// The following fields are used for bridge completions that require a follow-up
	// transaction. This is currently only required for withdrawing POL from Polygon
	// POS to Ethereum. Hopefully (probably) no other bridges will require this and
	// eventually Polygon will upgrade their system to not require it in the
	// future as well.

	// PreviousBridgeCompletionID will be set to the ID of the initial bridge
	// completion. It is only populated for follow-up bridge completions.
	PreviousBridgeCompletionID string `json:"previousBridgeCompletionID,omitempty"`
	// BridgeFollowUpData is the data required to submit and verify a follow-up
	// bridge completion. It is only populated for follow-up bridge completions.
	BridgeFollowUpData dex.Bytes `json:"bridgeFollowUpData,omitempty"`
	// RequiresFollowUp is true if the bridge requires a follow-up completion. It
	// is set to true for the initial bridge completion.
	RequiresFollowUp bool `json:"requiresFollowUp,omitempty"`

	txHash          common.Hash
	lastCheck       uint64
	savedToDB       bool
	lastBroadcast   time.Time
	lastFeeCheck    time.Time
	actionRequested bool
	actionIgnored   time.Time
	indexed         bool
}

const (
	dbVersion                 = 1
	txsTable                  = "txs"
	bridgeCompletionsTable    = "bridgeCompletions"
	allAssetIndexName         = "allAssets"
	assetIndexName            = "asset"
	bridgeInitiationIndexName = "bridgeinit"
	lastIncomingScanETHKey    = "lastIncomingScanETH"
)

var (
	lastIncomingScanKeyPrefix   = []byte("lastIncomingScan:")
	lastIncomingScanETHKeyBytes = []byte(lastIncomingScanETHKey)
)

const ErrNeverScanned = dex.ErrorKind("never scanned")

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
	getTxs(tokenID *uint32, req *asset.TxHistoryRequest) (*asset.TxHistoryResponse, error)
	// getTx gets a single transaction. It is not an error if the tx is not known.
	// In that case, a nil tx is returned.
	getTx(txHash common.Hash) (*extendedWalletTx, error)
	// getPendingTxs returns any recent txs that are not confirmed, ordered
	// by nonce lowest-first.
	getPendingTxs() ([]*extendedWalletTx, error)
	getBridges(n int, refID *common.Hash, past bool) ([]*asset.WalletTransaction, error)
	getPendingBridges() ([]*extendedWalletTx, error)
	getBridgeCompletions(initiationTxID string) ([]*extendedWalletTx, error)
	SetLastIncomingScanBlock(assetID *uint32, block uint64) error
	GetLastIncomingScanBlock(assetID *uint32) (uint64, error)
}

type TxDB struct {
	*lexi.DB

	txs               *lexi.Table
	bridgeCompletions *lexi.Table

	allAssetIndex         *lexi.Index
	assetIndex            *lexi.Index
	bridgeInitiationIndex *lexi.Index

	baseChainID uint32
	log         dex.Logger
}

var _ txDB = (*TxDB)(nil)

// allAssetIndexEntry is used to iterate over all transactions regardless of
// asset ID. The included data are:
//   - blockNumber: the block number of the transaction. If the transaction is pending
//     then the block number is set to ^uint64(0) in order to place pending
//     transactions at the end of the index.
//   - isUserOp: 1 if the transaction is a user op, 0 otherwise.
//   - nonce: the nonce of the transaction, so that pending transactions and
//     transactions within the same block are ordered by nonce.
func allAssetIndexEntry(wt *extendedWalletTx) []byte {
	entry := make([]byte, 8+1+8)
	blockNumber := wt.BlockNumber
	if blockNumber == 0 {
		blockNumber = ^uint64(0)
	}
	isUserOp := byte(0)
	if wt.IsUserOp {
		isUserOp = byte(1)
	}
	binary.BigEndian.PutUint64(entry[:8], blockNumber)
	entry[8] = isUserOp
	if wt.Nonce != nil {
		binary.BigEndian.PutUint64(entry[9:], wt.Nonce.Uint64())
	}
	return entry
}

// assetSpecificIndexEntry is used to iterate over all transactions for a
// specific asset ID. It is the same as allAssetIndexEntry, prepended with
// the asset ID
func assetSpecificIndexEntry(wt *extendedWalletTx, baseChainID uint32) []byte {
	var assetID uint32 = baseChainID
	if wt.TokenID != nil {
		assetID = *wt.TokenID
	}
	allAssetIndexKey := allAssetIndexEntry(wt)
	assetKey := make([]byte, 4+len(allAssetIndexKey))
	binary.BigEndian.PutUint32(assetKey[:4], assetID)
	copy(assetKey[4:], allAssetIndexKey)
	return assetKey
}

// bridgeIndexEntry generates an index entry for iterating over bridge initiation
// transactions. The index is sorted in chronological order based on the
// submission time of the bridge initiation transaction. Pending bridges appear
// at the end of the index. Pending bridges are those where the counterpart tx
// is not yet confirmed. A pending flag byte is used to separate pending from
// completed bridges while allowing both to be sorted by submission time.
func bridgeIndexEntry(wt *extendedWalletTx) []byte {
	var pendingFlag byte = 0 // 0 for completed bridges
	if wt.BridgeCounterpartTx == nil || !wt.BridgeCounterpartTx.Complete {
		pendingFlag = 1 // 1 for pending bridges
	}

	entry := make([]byte, 9) // 1 byte for pending flag + 8 bytes for submission time
	entry[0] = pendingFlag
	binary.BigEndian.PutUint64(entry[1:9], wt.SubmissionTime)
	return entry
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

	txs, err := ldb.Table(txsTable)
	if err != nil {
		return nil, err
	}

	bridgeCompletions, err := ldb.Table(bridgeCompletionsTable)
	if err != nil {
		return nil, err
	}

	allAssetIndex, err := txs.AddUniqueIndex(allAssetIndexName, func(k, v lexi.KV) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return allAssetIndexEntry(wt), nil
	})
	if err != nil {
		return nil, err
	}

	assetIndex, err := txs.AddUniqueIndex(assetIndexName, func(k, v lexi.KV) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		return assetSpecificIndexEntry(wt, baseChainID), nil
	})
	if err != nil {
		return nil, err
	}

	bridgeInitiationIndex, err := txs.AddIndex(bridgeInitiationIndexName, func(k, v lexi.KV) ([]byte, error) {
		wt, is := v.(*extendedWalletTx)
		if !is {
			return nil, fmt.Errorf("expected type *extendedWalletTx, got %T", wt)
		}
		if wt.WalletTransaction.Type != asset.InitiateBridge {
			return nil, lexi.ErrNotIndexed
		}
		return bridgeIndexEntry(wt), nil
	})
	if err != nil {
		return nil, err
	}

	db := &TxDB{
		DB:                    ldb,
		txs:                   txs,
		bridgeCompletions:     bridgeCompletions,
		allAssetIndex:         allAssetIndex,
		assetIndex:            assetIndex,
		bridgeInitiationIndex: bridgeInitiationIndex,
		baseChainID:           baseChainID,
		log:                   log,
	}

	return db, db.upgrade()
}

func (db *TxDB) v1Upgrade() error {
	err := db.ReIndex(txsTable, allAssetIndexName, func(k, v []byte) ([]byte, error) {
		wt := new(extendedWalletTx)
		if err := wt.UnmarshalBinary(v); err != nil {
			return nil, err
		}
		return allAssetIndexEntry(wt), nil
	})
	if err != nil {
		return err
	}

	return db.ReIndex(txsTable, assetIndexName, func(k, v []byte) ([]byte, error) {
		wt := new(extendedWalletTx)
		if err := wt.UnmarshalBinary(v); err != nil {
			return nil, err
		}
		return assetSpecificIndexEntry(wt, db.baseChainID), nil
	})
}

func (db *TxDB) upgrade() error {
	version, err := db.GetDBVersion()
	if err != nil {
		return err
	}
	if version == dbVersion {
		return nil
	}
	if version > dbVersion {
		return fmt.Errorf("unknown database version %d, wallet recognizes up to %d", version, dbVersion)
	}

	db.log.Infof("Upgrading database from version %d to %d", version, dbVersion)

	return db.Upgrade(func() error {
		err := db.v1Upgrade()
		if err != nil {
			return err
		}
		return db.SetDBVersion(dbVersion)
	})
}

// storeTx stores a transaction in the database. If a transaction with the
// same hash or nonce already exists, it is replaced.
func (db *TxDB) storeTx(wt *extendedWalletTx) error {
	hash := common.HexToHash(wt.ID)

	return db.Update(func(txn *badger.Txn) error {
		err := db.txs.Set(hash[:], wt, lexi.WithReplace(), lexi.WithTxn(txn))
		if err != nil {
			return err
		}

		// If this is a bridge completion, update the bridge completions table
		if wt.WalletTransaction.Type == asset.CompleteBridge {
			return db.addBridgeCompletion(wt, txn)
		}

		return nil
	})
}

// addBridgeCompletion adds a bridge completion transaction to the bridge completions table.
func (db *TxDB) addBridgeCompletion(wt *extendedWalletTx, txn *badger.Txn) error {
	if wt.WalletTransaction.BridgeCounterpartTx == nil || len(wt.WalletTransaction.BridgeCounterpartTx.IDs) < 1 {
		return fmt.Errorf("bridge completion missing initiation transaction ID")
	}

	initiationTxID := wt.WalletTransaction.BridgeCounterpartTx.IDs[0]
	initiationTxHash := common.HexToHash(initiationTxID)

	// Get the current list of bridge completions
	existingData, err := db.bridgeCompletions.GetRaw(initiationTxHash[:], lexi.WithGetTxn(txn))
	if err != nil && !errors.Is(err, lexi.ErrKeyNotFound) {
		return err
	}

	// Start with version 0
	buildy := encode.BuildyBytes{0}

	// Check if this completion is already in the list and rebuild the list
	if len(existingData) > 0 {
		_, pushes, err := encode.DecodeBlob(existingData)
		if err != nil {
			return fmt.Errorf("error decoding existing bridge completions: %w", err)
		}

		for _, push := range pushes {
			if string(push) == wt.ID {
				// Already exists, no need to add
				return nil
			}
			// Add existing completion back to the list
			buildy = buildy.AddData(push)
		}
	}

	// Add this completion to the list
	buildy = buildy.AddData([]byte(wt.ID))

	return db.bridgeCompletions.Set(initiationTxHash[:], []byte(buildy), lexi.WithReplace(), lexi.WithTxn(txn))
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
// If n <= 0, getTxs returns all transactions in reverse-nonce order.
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
func (db *TxDB) getTxs(assetIDPtr *uint32, req *asset.TxHistoryRequest) (*asset.TxHistoryResponse, error) {
	var n, past = req.N, req.Past
	var refID *common.Hash
	if req.RefID != nil {
		h := common.HexToHash(*req.RefID)
		if h == (common.Hash{}) {
			return nil, fmt.Errorf("invalid reference ID %q provided", *req.RefID)
		}
		refID = &h
	}
	var assetID interface{}
	idx := db.allAssetIndex
	if assetIDPtr != nil {
		assetID = *assetIDPtr
		idx = db.assetIndex
	}

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
		if assetIDPtr == nil {
			entry = allAssetIndexEntry(wt)
		} else {
			var refTxAssetID uint32 = db.baseChainID
			if wt.TokenID != nil {
				refTxAssetID = *wt.TokenID
			}
			if refTxAssetID != assetID {
				return nil, fmt.Errorf("token ID mismatch: %d != %d", refTxAssetID, assetID)
			}
			entry = assetSpecificIndexEntry(wt, db.baseChainID)
		}

		opts = append(opts, lexi.WithSeek(entry))
	}

	txs := make([]*asset.WalletTransaction, 0, n)
	var moreAvailable bool
	ignoreTypes := req.IngoreTypesLookup()
	iterFunc := func(it *lexi.Iter) error {
		wt := new(extendedWalletTx)
		if err := it.V(func(vB []byte) error {
			return wt.UnmarshalBinary(vB)
		}); err != nil {
			return err
		}

		if ignoreTypes[wt.Type] {
			return nil
		}
		if n > 0 && len(txs) == n {
			moreAvailable = true
			return lexi.ErrEndIteration
		}
		txs = append(txs, wt.WalletTransaction)
		return nil
	}

	if err := idx.Iterate(assetID, iterFunc, opts...); err != nil {
		return nil, err
	}
	return &asset.TxHistoryResponse{
		Txs:           txs,
		MoreAvailable: moreAvailable,
	}, nil
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

// getBridges fetches n bridge initiations.
//
// If n=0, getBridges returns all bridge initiations in chronological order based
// on bridge initiation submission time, with pending bridges at the end.
//
// If no refID is provided:
// - Returns the n most recent bridge initiations
// - The past argument is ignored
//
// If refID is provided:
// - Returns n bridge initiations starting with the referenced transaction
// - If past=false: Results are in forward chronological order
// - If past=true: Results are in reverse chronological order
// - The referenced transaction is included in results
// - Returns asset.CoinNotFoundError if refID not found
func (db *TxDB) getBridges(n int, refID *common.Hash, past bool) ([]*asset.WalletTransaction, error) {
	var opts []lexi.IterationOption
	if past || refID == nil {
		opts = append(opts, lexi.WithReverse())
	}

	if refID != nil {
		wt, err := db.getTx(*refID)
		if err != nil || wt == nil {
			return nil, asset.CoinNotFoundError
		}
		if wt.Type != asset.InitiateBridge {
			return nil, fmt.Errorf("referenced transaction is not a bridge initiation")
		}

		entry := bridgeIndexEntry(wt)
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

	return txs, db.bridgeInitiationIndex.Iterate(nil, iterFunc, opts...)
}

// getPendingBridges returns all bridge initiation transactions that have not been
// completed and are not marked as lost.
func (db *TxDB) getPendingBridges() (txs []*extendedWalletTx, err error) {
	db.bridgeInitiationIndex.Iterate(nil, func(it *lexi.Iter) error {
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

		if wt.BridgeCounterpartTx == nil || !wt.BridgeCounterpartTx.Complete {
			txs = append(txs, wt)
		}

		return nil
	}, lexi.WithReverse())
	return
}

// getBridgeCompletions retrieves all bridge completion transactions for the
// given bridge initiation transaction ID.
func (db *TxDB) getBridgeCompletions(initiationTxID string) ([]*extendedWalletTx, error) {
	txHash := common.HexToHash(initiationTxID)

	data, err := db.bridgeCompletions.GetRaw(txHash[:])
	if err != nil {
		if errors.Is(err, lexi.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if len(data) <= 1 { // version byte only or empty
		return nil, nil
	}

	_, pushes, err := encode.DecodeBlob(data)
	if err != nil {
		return nil, fmt.Errorf("error decoding bridge completions: %w", err)
	}

	var txs []*extendedWalletTx
	for _, push := range pushes {
		completionTxHash := string(push)
		wt, err := db.getTx(common.HexToHash(completionTxHash))
		if err != nil {
			return nil, err
		}
		if wt != nil {
			txs = append(txs, wt)
		}
	}

	return txs, nil
}

// buildLastIncomingScanKey builds the key for storing last incoming scan block for a specific asset.
func buildLastIncomingScanKey(assetID uint32) []byte {
	key := make([]byte, len(lastIncomingScanKeyPrefix)+4)
	copy(key, lastIncomingScanKeyPrefix)
	binary.BigEndian.PutUint32(key[len(lastIncomingScanKeyPrefix):], assetID)
	return key
}

// SetLastIncomingScanBlock stores the last block height at which incoming transactions
// were scanned for the given asset. If assetID is nil, it stores for the base chain (ETH).
func (db *TxDB) SetLastIncomingScanBlock(assetID *uint32, block uint64) error {
	var key []byte
	if assetID == nil {
		key = lastIncomingScanETHKeyBytes
	} else {
		key = buildLastIncomingScanKey(*assetID)
	}

	return db.Update(func(txn *badger.Txn) error {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, block)
		return txn.Set(key, b)
	})
}

// GetLastIncomingScanBlock retrieves the last block height at which incoming transactions
// were scanned for the given asset. If assetID is nil, it retrieves for the base chain (ETH).
// Returns ErrNeverScanned if the value was never set.
func (db *TxDB) GetLastIncomingScanBlock(assetID *uint32) (uint64, error) {
	var key []byte
	if assetID == nil {
		key = lastIncomingScanETHKeyBytes
	} else {
		key = buildLastIncomingScanKey(*assetID)
	}

	var block uint64
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrNeverScanned
		}
		if err != nil {
			return err
		}
		return item.Value(func(b []byte) error {
			block = binary.BigEndian.Uint64(b)
			return nil
		})
	})
	return block, err
}
