package eth

import (
	"encoding/hex"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/dgraph-io/badger"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestTxDB(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	txHistoryStore, err := newBadgerTxDB(tempDir, tLogger)
	if err != nil {
		t.Fatalf("error creating tx history store: %v", err)
	}

	txs, err := txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("expected 0 txs but got %d", len(txs))
	}

	wt1 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Send,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      100,
			Fees:        300,
			BlockNumber: 123,
			AdditionalData: map[string]string{
				"Nonce": "1",
			},
			TokenID:   &simnetTokenID,
			Confirmed: true,
		},
	}

	wt2 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Swap,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      200,
			Fees:        100,
			BlockNumber: 124,
			AdditionalData: map[string]string{
				"Nonce": "2",
			},
		},
	}

	wt3 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Redeem,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      200,
			Fees:        200,
			BlockNumber: 125,
			AdditionalData: map[string]string{
				"Nonce": "3",
			},
		},
	}

	wt4 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Redeem,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      200,
			Fees:        300,
			BlockNumber: 125,
			AdditionalData: map[string]string{
				"Nonce": "3",
			},
		},
	}

	err = txHistoryStore.storeTx(1, wt1)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs := []*asset.WalletTransaction{wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	err = txHistoryStore.storeTx(2, wt2)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	err = txHistoryStore.storeTx(3, wt3)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(2, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt3.WalletTransaction, wt2.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txs, err = txHistoryStore.getTxs(0, &wt2.ID, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txs, err = txHistoryStore.getTxs(0, &wt2.ID, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt3.WalletTransaction, wt2.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	// Update nonce with different tx
	err = txHistoryStore.storeTx(3, wt4)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(txs) != 3 {
		t.Fatalf("expected 3 txs but got %d", len(txs))
	}
	expectedTxs = []*asset.WalletTransaction{wt4.WalletTransaction, wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	// Update same tx with new fee
	wt4.Fees = 300
	err = txHistoryStore.storeTx(3, wt4)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt4.WalletTransaction, wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txHistoryStore.close()

	txHistoryStore, err = newBadgerTxDB(tempDir, dex.StdOutLogger("TXDB", dex.LevelTrace))
	if err != nil {
		t.Fatalf("error creating tx history store: %v", err)
	}
	defer txHistoryStore.close()

	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt4.WalletTransaction, wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	unconfirmedTxs, err := txHistoryStore.getPendingTxs()
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedUnconfirmedTxs := map[uint64]*extendedWalletTx{
		3: wt4,
		2: wt2,
	}
	if !reflect.DeepEqual(expectedUnconfirmedTxs, unconfirmedTxs) {
		t.Fatalf("expected txs %+v but got %+v", expectedUnconfirmedTxs, unconfirmedTxs)
	}

	err = txHistoryStore.removeTx(wt2.ID)
	if err != nil {
		t.Fatalf("error removing tx: %v", err)
	}

	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt4.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txs, err = txHistoryStore.getTxs(0, nil, false, &simnetTokenID)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txHashes := make([]common.Hash, 3)
	for i := range txHashes {
		txHashes[i] = common.BytesToHash(encode.RandomBytes(32))
	}
	monitoredTx1 := &monitoredTx{
		tx:             types.NewTx(&types.LegacyTx{Data: []byte{1}}),
		replacementTx:  &txHashes[1],
		blockSubmitted: 1,
	}
	monitoredTx2 := &monitoredTx{
		tx:             types.NewTx(&types.LegacyTx{Data: []byte{2}}),
		replacementTx:  &txHashes[2],
		replacedTx:     &txHashes[0],
		blockSubmitted: 2,
	}
	monitoredTx3 := &monitoredTx{
		tx:             types.NewTx(&types.LegacyTx{Data: []byte{3}}),
		replacedTx:     &txHashes[1],
		blockSubmitted: 3,
	}

	txHistoryStore.storeMonitoredTx(txHashes[0], monitoredTx1)
	txHistoryStore.storeMonitoredTx(txHashes[1], monitoredTx2)
	txHistoryStore.storeMonitoredTx(txHashes[2], monitoredTx3)
	monitoredTxs, err := txHistoryStore.getMonitoredTxs()
	if err != nil {
		t.Fatalf("error retrieving monitored txs: %v", err)
	}

	expectedMonitoredTxs := map[common.Hash]*monitoredTx{
		txHashes[0]: monitoredTx1,
		txHashes[1]: monitoredTx2,
		txHashes[2]: monitoredTx3,
	}

	if len(monitoredTxs) != len(expectedMonitoredTxs) {
		t.Fatalf("expected %d monitored txs but got %d", len(expectedMonitoredTxs), len(monitoredTxs))
	}

	monitoredTxsEqual := func(a, b *monitoredTx) bool {
		if a.tx.Hash() != b.tx.Hash() {
			return false
		}
		if a.replacementTx != nil && b.replacementTx != nil && *a.replacementTx != *b.replacementTx {
			return false
		}
		if a.replacedTx != nil && b.replacedTx != nil && *a.replacedTx != *b.replacedTx {
			return false
		}
		if a.blockSubmitted != b.blockSubmitted {
			return false
		}
		return true
	}

	for txHash, monitoredTx := range monitoredTxs {
		expectedMonitoredTx := expectedMonitoredTxs[txHash]
		if !monitoredTxsEqual(monitoredTx, expectedMonitoredTxs[txHash]) {
			t.Fatalf("expected monitored tx %+v but got %+v", expectedMonitoredTx, monitoredTx)
		}
	}

	err = txHistoryStore.removeMonitoredTxs([]common.Hash{txHashes[0]})
	if err != nil {
		t.Fatalf("error removing monitored tx: %v", err)
	}

	monitoredTxs, err = txHistoryStore.getMonitoredTxs()
	if err != nil {
		t.Fatalf("error retrieving monitored txs: %v", err)
	}

	expectedMonitoredTxs = map[common.Hash]*monitoredTx{
		txHashes[1]: monitoredTx2,
		txHashes[2]: monitoredTx3,
	}

	if len(monitoredTxs) != len(expectedMonitoredTxs) {
		t.Fatalf("expected %d monitored txs but got %d", len(expectedMonitoredTxs), len(monitoredTxs))
	}

	for txHash, monitoredTx := range monitoredTxs {
		expectedMonitoredTx := expectedMonitoredTxs[txHash]
		if !monitoredTxsEqual(monitoredTx, expectedMonitoredTxs[txHash]) {
			t.Fatalf("expected monitored tx %+v but got %+v", expectedMonitoredTx, monitoredTx)
		}
	}

	err = txHistoryStore.removeMonitoredTxs([]common.Hash{txHashes[1], txHashes[2]})
	if err != nil {
		t.Fatalf("error removing monitored tx: %v", err)
	}

	monitoredTxs, err = txHistoryStore.getMonitoredTxs()
	if err != nil {
		t.Fatalf("error retrieving monitored txs: %v", err)
	}

	if len(monitoredTxs) != 0 {
		t.Fatalf("expected 0 monitored txs but got %d", len(monitoredTxs))
	}
}

func TestTxDBUpgrade(t *testing.T) {
	dir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	opts := badger.DefaultOptions(dir).WithLogger(&badgerLoggerWrapper{tLogger})
	db, err := badger.Open(opts)
	if err == badger.ErrTruncateNeeded {
		// Probably a Windows thing.
		// https://github.com/dgraph-io/badger/issues/744
		tLogger.Warnf("newTxHistoryStore badger db: %v", err)
		// Try again with value log truncation enabled.
		opts.Truncate = true
		tLogger.Warnf("Attempting to reopen badger DB with the Truncate option set...")
		db, err = badger.Open(opts)
	}
	if err != nil {
		t.Fatalf("error opening badger db: %v", err)
	}

	txHashes := make([]common.Hash, 3)
	for i := range txHashes {
		txHashes[i] = common.BytesToHash(encode.RandomBytes(32))
	}

	monitoredTxs := map[common.Hash]*monitoredTx{
		txHashes[0]: {
			tx:             types.NewTx(&types.LegacyTx{Data: []byte{1}}),
			replacementTx:  &txHashes[1],
			blockSubmitted: 1,
		},
		txHashes[1]: {
			tx:             types.NewTx(&types.LegacyTx{Data: []byte{2}}),
			replacementTx:  &txHashes[2],
			replacedTx:     &txHashes[0],
			blockSubmitted: 2,
		},
		txHashes[2]: {
			tx:             types.NewTx(&types.LegacyTx{Data: []byte{3}}),
			replacedTx:     &txHashes[1],
			blockSubmitted: 3,
		},
	}

	err = db.Update(func(txn *badger.Txn) error {
		for txHash, monitoredTx := range monitoredTxs {
			monitoredTxB, err := monitoredTx.MarshalBinary()
			if err != nil {
				return err
			}

			th := txHash
			err = txn.Set(th[:], monitoredTxB)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("error storing monitored txs: %v", err)
	}

	err = db.Close()
	if err != nil {
		t.Fatalf("error closing badger db: %v", err)
	}

	txHistoryStore, err := newBadgerTxDB(dir, tLogger)
	if err != nil {
		t.Fatalf("error creating tx history store: %v", err)
	}

	retrievedMonitoredTxs, err := txHistoryStore.getMonitoredTxs()
	if err != nil {
		t.Fatalf("error retrieving monitored txs: %v", err)
	}

	if len(retrievedMonitoredTxs) != len(monitoredTxs) {
		t.Fatalf("expected %d monitored txs but got %d", len(monitoredTxs), len(retrievedMonitoredTxs))
	}

	monitoredTxsEqual := func(a, b *monitoredTx) bool {
		if a.tx.Hash() != b.tx.Hash() {
			return false
		}
		if a.replacementTx != nil && b.replacementTx != nil && *a.replacementTx != *b.replacementTx {
			return false
		}
		if a.replacedTx != nil && b.replacedTx != nil && *a.replacedTx != *b.replacedTx {
			return false
		}
		if a.blockSubmitted != b.blockSubmitted {
			return false
		}
		return true
	}

	for txHash, monitoredTx := range retrievedMonitoredTxs {
		expectedMonitoredTx := monitoredTxs[txHash]
		if !monitoredTxsEqual(monitoredTx, expectedMonitoredTx) {
			t.Fatalf("expected monitored tx %+v but got %+v", expectedMonitoredTx, monitoredTx)
		}
	}
}
