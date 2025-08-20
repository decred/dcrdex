//go:build !harness && !rpclive

package eth

import (
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
)

func TestTxDB(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	// Grab these for the tx generation utilities
	_, eth, node, shutdown := tassetWallet(BipID)
	shutdown()

	txHistoryStore, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}

	txs, err := txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("expected 0 txs but got %d", len(txs))
	}

	newTx := func(nonce uint64) *extendedWalletTx {
		return eth.extendedTx(&genTxResult{
			tx:     node.newTransaction(nonce, big.NewInt(1)),
			txType: asset.Send,
			amt:    1,
		})
	}

	wt1 := newTx(1)
	wt1.Confirmed = true
	wt1.TokenID = &usdcEthID
	wt2 := newTx(2)
	wt3 := newTx(3)
	wt4 := newTx(4)

	err = txHistoryStore.storeTx(wt1)
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

	err = txHistoryStore.storeTx(wt2)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %s but got %s", spew.Sdump(expectedTxs), spew.Sdump(txs))
	}

	err = txHistoryStore.storeTx(wt3)
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

	txs, err = txHistoryStore.getTxs(0, &wt2.txHash, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt2.WalletTransaction, wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txs, err = txHistoryStore.getTxs(0, &wt2.txHash, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt2.WalletTransaction, wt3.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	allTxs := []*asset.WalletTransaction{wt4.WalletTransaction, wt3.WalletTransaction, wt2.WalletTransaction, wt1.WalletTransaction}

	// Update same tx with new fee
	wt4.Fees = 300
	err = txHistoryStore.storeTx(wt4)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}
	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if !reflect.DeepEqual(allTxs, txs) {
		t.Fatalf("expected txs %s but got %s", spew.Sdump(allTxs), spew.Sdump(txs))
	}
	txHistoryStore.Close()

	txHistoryStore, err = NewTxDB(tempDir, dex.StdOutLogger("TXDB", dex.LevelTrace), BipID)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}
	defer txHistoryStore.Close()

	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if !reflect.DeepEqual(allTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	unconfirmedTxs, err := txHistoryStore.getPendingTxs()
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedUnconfirmedTxs := []*extendedWalletTx{wt4, wt3, wt2}
	compareTxs := func(txs0, txs1 []*extendedWalletTx) bool {
		if len(txs0) != len(txs1) {
			return false
		}
		for i, tx0 := range txs0 {
			tx1 := txs1[i]
			n0, n1 := tx0.Nonce, tx1.Nonce
			tx0.Nonce, tx1.Nonce = nil, nil
			eq := reflect.DeepEqual(tx0.WalletTransaction, tx1.WalletTransaction)
			tx0.Nonce, tx1.Nonce = n0, n1
			if !eq {
				return false
			}
		}
		return true
	}
	if !compareTxs(expectedUnconfirmedTxs, unconfirmedTxs) {
		t.Fatalf("expected txs:\n%s\n\nbut got:\n%s", spew.Sdump(expectedUnconfirmedTxs), spew.Sdump(unconfirmedTxs))
	}

	txs, err = txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if !reflect.DeepEqual(allTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}

	txs, err = txHistoryStore.getTxs(0, nil, false, &usdcEthID)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	expectedTxs = []*asset.WalletTransaction{wt1.WalletTransaction}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}
}

func TestTxDB_userOp(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	// Grab these for the tx generation utilities
	_, eth, node, shutdown := tassetWallet(BipID)
	shutdown()

	txHistoryStore, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}

	newTx := func(nonce uint64, blockNumber uint64, isUserOp bool) *extendedWalletTx {
		tx := eth.extendedTx(&genTxResult{
			tx:     node.newTransaction(nonce, big.NewInt(1)),
			txType: asset.Send,
			amt:    1,
		})
		tx.BlockNumber = blockNumber
		tx.IsUserOp = isUserOp
		tx.ID = fmt.Sprintf("%d", rand.Intn(1000000))
		if blockNumber > 0 {
			tx.Confirmed = true
		}
		return tx
	}

	wt1 := newTx(1, 100, false)
	wt2 := newTx(2, 101, false)
	wt2UserOp := newTx(1, 101, true)
	wt3 := newTx(3, 102, false)
	wt4 := newTx(4, 103, false)

	for _, wt := range []*extendedWalletTx{wt1, wt2, wt2UserOp, wt3, wt4} {
		err := txHistoryStore.storeTx(wt)
		if err != nil {
			t.Fatalf("error storing tx: %v", err)
		}
	}

	expectedTxs := []*asset.WalletTransaction{wt4.WalletTransaction, wt3.WalletTransaction, wt2UserOp.WalletTransaction, wt2.WalletTransaction, wt1.WalletTransaction}
	txs, err := txHistoryStore.getTxs(0, nil, true, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if !reflect.DeepEqual(expectedTxs, txs) {
		t.Fatalf("expected txs %+v but got %+v", expectedTxs, txs)
	}
}

func TestTxDBReplaceNonce(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	_, eth, node, shutdown := tassetWallet(BipID)
	shutdown()

	txHistoryStore, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}

	newTx := func(nonce uint64) *extendedWalletTx {
		return eth.extendedTx(&genTxResult{
			tx:     node.newTransaction(nonce, big.NewInt(1)),
			txType: asset.Send,
			amt:    1,
		})
	}

	wt1 := newTx(1)
	wt2 := newTx(1)

	err = txHistoryStore.storeTx(wt1)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}

	err = txHistoryStore.storeTx(wt2)
	if err != nil {
		t.Fatalf("error storing tx: %v", err)
	}

	tx, err := txHistoryStore.getTx(wt1.txHash)
	if err != nil {
		t.Fatalf("error retrieving tx: %v", err)
	}
	if tx != nil {
		t.Fatalf("expected nil tx but got %+v", tx)
	}

	txs, err := txHistoryStore.getTxs(0, nil, false, nil)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx but got %d", len(txs))
	}
	if txs[0].ID != wt2.ID {
		t.Fatalf("expected tx %s but got %s", wt2.ID, txs[0].ID)
	}
}

func TestTxDB_getUnknownTx(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	txHistoryStore, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}

	tx, err := txHistoryStore.getTx(common.Hash{0x01})
	if err != nil {
		t.Fatalf("error retrieving tx: %v", err)
	}
	if tx != nil {
		t.Fatalf("expected nil tx but got %+v", tx)
	}
}

func TestTxDB_GetBridges(t *testing.T) {
	tempDir := t.TempDir()
	db, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error creating txDB: %v", err)
	}
	defer db.Close()

	now := time.Now()
	txs := []*extendedWalletTx{
		// Completed bridge, older submission time
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x1111111111111111111111111111111111111111111111111111111111111111",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					IDs:      []string{"0x1111111111111111111111111111111111111111111111111111111111111112"},
					Complete: true,
				},
			},
			Nonce:          big.NewInt(1),
			SubmissionTime: uint64(now.Add(-24 * time.Hour).Unix()),
		},
		// Completed bridge, newer submission time
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x2222222222222222222222222222222222222222222222222222222222222222",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					IDs:      []string{"0x2222222222222222222222222222222222222222222222222222222222222223"},
					Complete: true,
				},
			},
			Nonce:          big.NewInt(2),
			SubmissionTime: uint64(now.Add(-20 * time.Hour).Unix()),
		},
		// Pending bridge, older
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x3333333333333333333333333333333333333333333333333333333333333333",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					Complete: false,
				},
			},
			Nonce:          big.NewInt(3),
			SubmissionTime: uint64(now.Add(-18 * time.Hour).Unix()),
		},
		// Pending bridge, newer
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x4444444444444444444444444444444444444444444444444444444444444444",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					Complete: false,
				},
			},
			Nonce:          big.NewInt(4),
			SubmissionTime: uint64(now.Add(-10 * time.Hour).Unix()),
		},
		// Non-bridge transaction
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x5555555555555555555555555555555555555555555555555555555555555555",
				Type: asset.Send,
			},
			Nonce:          big.NewInt(5),
			SubmissionTime: uint64(now.Add(-5 * time.Hour).Unix()),
		},
	}

	// Store all transactions
	for _, tx := range txs {
		if err := db.storeTx(tx); err != nil {
			t.Fatalf("error storing tx: %v", err)
		}
	}

	t.Run("getBridges all", func(t *testing.T) {
		result, err := db.getBridges(0, nil, false)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 4 {
			t.Fatalf("expected 4 bridge initiations, got %d", len(result))
		}
		// Expected order: pending bridges first (sorted by submission time, newest first),
		// then completed bridges (sorted by submission time, newest first)
		// So: txs[3] (pending, newer), txs[2] (pending, older), txs[1] (completed, newer), txs[0] (completed, older)
		if result[0].ID != txs[3].ID || result[1].ID != txs[2].ID ||
			result[2].ID != txs[1].ID || result[3].ID != txs[0].ID {
			t.Fatalf("getBridges returned incorrect order of transactions")
		}
	})

	t.Run("getBridges limited", func(t *testing.T) {
		result, err := db.getBridges(2, nil, false)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 bridge initiations, got %d", len(result))
		}

		// Expected order: first two pending bridges (newest first)
		if result[0].ID != txs[3].ID || result[1].ID != txs[2].ID {
			t.Fatalf("getBridges returned incorrect transactions")
		}
	})

	t.Run("getBridges with refID and past=false", func(t *testing.T) {
		refID := common.HexToHash(txs[0].ID)
		result, err := db.getBridges(2, &refID, false)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 bridge initiations, got %d", len(result))
		}

		// Forward iteration from txs[0]: should get txs[0], then txs[1] (next in chronological order)
		if result[0].ID != txs[0].ID || result[1].ID != txs[1].ID {
			t.Fatalf("getBridges returned incorrect transactions")
		}
	})

	t.Run("getBridges with refID and past=true", func(t *testing.T) {
		refID := common.HexToHash(txs[0].ID)
		result, err := db.getBridges(2, &refID, true)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 bridge transaction, got %d", len(result))
		}

		// Only txs[0] should be returned since it's the oldest completed bridge
		if result[0].ID != txs[0].ID {
			t.Fatalf("getBridges returned incorrect transactions")
		}
	})

	t.Run("getBridges with refID and past=true from newer bridge", func(t *testing.T) {
		// Use txs[1] as reference to test past=true with older transactions available
		refID := common.HexToHash(txs[1].ID)
		result, err := db.getBridges(2, &refID, true)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 bridge transactions, got %d", len(result))
		}

		// Should return txs[1] (reference) then txs[0] (older) in reverse chronological order
		if result[0].ID != txs[1].ID || result[1].ID != txs[0].ID {
			t.Fatalf("getBridges returned incorrect transactions")
		}
	})

	t.Run("getBridges with invalid refID", func(t *testing.T) {
		invalidID := common.HexToHash("0xdeadbeef")
		_, err := db.getBridges(2, &invalidID, false)
		if err == nil || err != asset.CoinNotFoundError {
			t.Fatalf("expected CoinNotFoundError, got %v", err)
		}
	})

	t.Run("getBridges with non-bridge refID", func(t *testing.T) {
		nonBridgeID := common.HexToHash(txs[4].ID)
		_, err := db.getBridges(2, &nonBridgeID, false)
		if err == nil {
			t.Fatalf("expected error for non-bridge refID, got nil")
		}
	})

	t.Run("getPendingBridges", func(t *testing.T) {
		result, err := db.getPendingBridges()
		if err != nil {
			t.Fatalf("getPendingBridges failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 pending bridge transactions, got %d", len(result))
		}
		// Should be in reverse chronological order (newest first)
		if result[0].ID != txs[3].ID || result[1].ID != txs[2].ID {
			t.Fatalf("getPendingBridges returned incorrect order")
		}
	})
}

func TestTxDB_GetBridgeCompletions(t *testing.T) {
	tempDir := t.TempDir()
	db, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error creating txDB: %v", err)
	}
	defer db.Close()

	burnTxID := "0x1111111111111111111111111111111111111111111111111111111111111111"

	// Create first mint transaction that references the burn transaction
	mintTx1 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:   "0x2222222222222222222222222222222222222222222222222222222222222222",
			Type: asset.CompleteBridge,
			BridgeCounterpartTx: &asset.BridgeCounterpartTx{
				IDs: []string{burnTxID},
			},
		},
		Nonce:          big.NewInt(2),
		SubmissionTime: uint64(time.Now().Add(-20 * time.Hour).Unix()),
	}

	// Create second mint transaction that references the same burn ID
	mintTx2 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:   "0x3333333333333333333333333333333333333333333333333333333333333333",
			Type: asset.CompleteBridge,
			BridgeCounterpartTx: &asset.BridgeCounterpartTx{
				IDs: []string{burnTxID},
			},
		},
		Nonce:          big.NewInt(3),
		SubmissionTime: uint64(time.Now().Add(-18 * time.Hour).Unix()),
	}

	// Store first completion and verify single result
	if err := db.storeTx(mintTx1); err != nil {
		t.Fatalf("error storing first mint tx: %v", err)
	}

	result, err := db.getBridgeCompletions(burnTxID)
	if err != nil {
		t.Fatalf("getBridgeCompletions failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 completion transaction, got %d", len(result))
	}
	if result[0].ID != mintTx1.ID {
		t.Fatalf("expected first mint tx %s, got %s", mintTx1.ID, result[0].ID)
	}

	// Store second completion and verify both results in correct order
	if err := db.storeTx(mintTx2); err != nil {
		t.Fatalf("error storing second mint tx: %v", err)
	}

	result, err = db.getBridgeCompletions(burnTxID)
	if err != nil {
		t.Fatalf("getBridgeCompletions failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 completion transactions, got %d", len(result))
	}
	// Should be in order they were stored
	if result[0].ID != mintTx1.ID {
		t.Fatalf("expected first result to be %s, got %s", mintTx1.ID, result[0].ID)
	}
	if result[1].ID != mintTx2.ID {
		t.Fatalf("expected second result to be %s, got %s", mintTx2.ID, result[1].ID)
	}

	// Store duplicate and verify no change
	if err := db.storeTx(mintTx1); err != nil {
		t.Fatalf("error storing duplicate mint tx: %v", err)
	}

	result, err = db.getBridgeCompletions(burnTxID)
	if err != nil {
		t.Fatalf("getBridgeCompletions failed after duplicate: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 completion transactions after duplicate, got %d", len(result))
	}
	// Should still be in same order
	if result[0].ID != mintTx1.ID {
		t.Fatalf("expected first result to be %s after duplicate, got %s", mintTx1.ID, result[0].ID)
	}
	if result[1].ID != mintTx2.ID {
		t.Fatalf("expected second result to be %s after duplicate, got %s", mintTx2.ID, result[1].ID)
	}
}
