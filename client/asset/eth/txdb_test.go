//go:build !harness && !rpclive

package eth

import (
	"math/big"
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

	txs := []*extendedWalletTx{
		// Completed bridge, block 100
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x1111111111111111111111111111111111111111111111111111111111111111",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					CompletionTime: 100,
				},
			},
			Nonce:          big.NewInt(1),
			SubmissionTime: uint64(time.Now().Add(-24 * time.Hour).Unix()),
		},
		// Completed bridge, block 99
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x2222222222222222222222222222222222222222222222222222222222222222",
				Type: asset.InitiateBridge,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					CompletionTime: 99,
				},
			},
			Nonce:          big.NewInt(2),
			SubmissionTime: uint64(time.Now().Add(-20 * time.Hour).Unix()),
		},
		// Pending bridge
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:                  "0x3333333333333333333333333333333333333333333333333333333333333333",
				Type:                asset.InitiateBridge,
				BridgeCounterpartTx: nil,
			},
			Nonce:          big.NewInt(3),
			SubmissionTime: uint64(time.Now().Add(-18 * time.Hour).Unix()),
		},
		// Non-bridge transaction
		{
			WalletTransaction: &asset.WalletTransaction{
				ID:   "0x4444444444444444444444444444444444444444444444444444444444444444",
				Type: asset.Send,
			},
			Nonce:          big.NewInt(5),
			SubmissionTime: uint64(time.Now().Add(-10 * time.Hour).Unix()),
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
		if len(result) != 3 {
			t.Fatalf("expected 3 bridge initiations, got %d", len(result))
		}
		if result[0].ID != txs[2].ID || result[1].ID != txs[0].ID ||
			result[2].ID != txs[1].ID {
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

		if result[0].ID != txs[2].ID || result[1].ID != txs[0].ID {
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

		if result[0].ID != txs[0].ID || result[1].ID != txs[2].ID {
			t.Fatalf("getBridges returned incorrect transactions")
		}
	})

	t.Run("getBridges with refID and past=true", func(t *testing.T) {
		refID := common.HexToHash(txs[0].ID)
		result, err := db.getBridges(2, &refID, true)
		if err != nil {
			t.Fatalf("getBridges failed: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 bridge transactions, got %d", len(result))
		}

		if result[0].ID != txs[0].ID || result[1].ID != txs[1].ID {
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
		nonBridgeID := common.HexToHash(txs[3].ID)
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
		if len(result) != 1 {
			t.Fatalf("expected 2 pending bridge transactions, got %d", len(result))
		}
		if result[0].ID != txs[2].ID {
			t.Fatalf("expected pending bridge %s but got %s", txs[2].ID, result[0].ID)
		}
	})
}

func TestTxDB_GetBridgeMint(t *testing.T) {
	tempDir := t.TempDir()
	db, err := NewTxDB(tempDir, tLogger, BipID)
	if err != nil {
		t.Fatalf("error creating txDB: %v", err)
	}
	defer db.Close()

	burnTxID := "0x1111111111111111111111111111111111111111111111111111111111111111"

	// Create a mint transaction that references the burn transaction
	mintTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:   "0x2222222222222222222222222222222222222222222222222222222222222222",
			Type: asset.CompleteBridge,
			BridgeCounterpartTx: &asset.BridgeCounterpartTx{
				ID: burnTxID,
			},
		},
		Nonce:          big.NewInt(2),
		SubmissionTime: uint64(time.Now().Add(-20 * time.Hour).Unix()),
	}

	// Create another mint transaction that references a different burn ID
	anotherMintTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:   "0x3333333333333333333333333333333333333333333333333333333333333333",
			Type: asset.CompleteBridge,
			BridgeCounterpartTx: &asset.BridgeCounterpartTx{
				ID: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
		Nonce:          big.NewInt(3),
		SubmissionTime: uint64(time.Now().Add(-18 * time.Hour).Unix()),
	}

	// Create a regular transaction (non-bridge)
	regularTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:   "0x4444444444444444444444444444444444444444444444444444444444444444",
			Type: asset.Send,
		},
		Nonce:          big.NewInt(4),
		SubmissionTime: uint64(time.Now().Add(-16 * time.Hour).Unix()),
	}

	// Store all transactions
	if err := db.storeTx(mintTx); err != nil {
		t.Fatalf("error storing mint tx: %v", err)
	}
	if err := db.storeTx(anotherMintTx); err != nil {
		t.Fatalf("error storing another mint tx: %v", err)
	}
	if err := db.storeTx(regularTx); err != nil {
		t.Fatalf("error storing regular tx: %v", err)
	}

	t.Run("get completion transaction with valid burn ID", func(t *testing.T) {
		result, err := db.getBridgeCompletion(burnTxID)
		if err != nil {
			t.Fatalf("getBridgeCompletion failed: %v", err)
		}
		if result == nil {
			t.Fatalf("expected mint transaction, got nil")
		}
		if result.ID != mintTx.ID {
			t.Fatalf("expected mint tx %s, got %s", mintTx.ID, result.ID)
		}
	})

	t.Run("get completion transaction with unknown burn ID", func(t *testing.T) {
		unknownID := "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
		result, err := db.getBridgeCompletion(unknownID)
		if err != nil {
			t.Fatalf("getBridgeCompletion should return nil, not error for unknown ID: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil result for unknown burn ID, got %+v", result)
		}
	})
}
