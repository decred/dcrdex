// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
)

func TestTxDB(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	txHistoryStore, err := newBadgerTxDB(tempDir, tLogger)
	if err != nil {
		t.Fatalf("error creating tx history store: %v", err)
	}
	defer txHistoryStore.close()

	txs, err := txHistoryStore.getTxs(0, nil, true)
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(txs) != 0 {
		t.Fatalf("expected 0 txs but got %d", len(txs))
	}

	tx1 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Send,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        1e5,
			BlockNumber: 0,
		},
		Submitted: false,
	}

	tx2 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Receive,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        3e5,
			BlockNumber: 0,
		},
		Submitted: true,
	}

	tx3 := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Swap,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        2e5,
			BlockNumber: 0,
		},
		Submitted: true,
	}

	getTxsAndCheck := func(n int, refID *string, past bool, expected []*asset.WalletTransaction) {
		t.Helper()

		txs, err = txHistoryStore.getTxs(n, refID, past)
		if err != nil {
			t.Fatalf("failed to get txs: %v", err)
		}
		if len(txs) != len(expected) {
			t.Fatalf("expected %d txs but got %d", len(expected), len(txs))
		}
		for i, expectedTx := range expected {
			if !reflect.DeepEqual(expectedTx, txs[i]) {
				t.Fatalf("transaction %d: %+v != %+v", i, expectedTx, txs[i])
			}
		}
	}

	getPendingTxsAndCheck := func(expected []*extendedWalletTx) {
		t.Helper()

		txs, err := txHistoryStore.getPendingTxs()
		if err != nil {
			t.Fatalf("failed to get unconfirmed txs: %v", err)
		}

		if len(txs) != len(expected) {
			t.Fatalf("expected %d txs but got %d", len(expected), len(txs))
		}

		for i, expectedTx := range expected {
			if !reflect.DeepEqual(expectedTx.WalletTransaction, txs[i].WalletTransaction) {
				t.Fatalf("transaction %+v != %+v", expectedTx.WalletTransaction, txs[i].WalletTransaction)
			}
		}
	}

	err = txHistoryStore.storeTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{})
	getPendingTxsAndCheck([]*extendedWalletTx{tx1})

	err = txHistoryStore.markTxAsSubmitted(tx1.ID)
	if err != nil {
		t.Fatalf("failed to mark tx as submitted: %v", err)
	}
	tx1.Submitted = true
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx1})

	// Storing same pending tx twice should not change anything.
	err = txHistoryStore.storeTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx1})

	tx1.BlockNumber = 100
	err = txHistoryStore.storeTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx1})

	err = txHistoryStore.storeTx(tx2)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx2.WalletTransaction, tx1.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx2, tx1})

	tx2.BlockNumber = 99
	tx2.Confirmed = true
	err = txHistoryStore.storeTx(tx2)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction, tx2.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx1})

	err = txHistoryStore.storeTx(tx3)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction, tx1.WalletTransaction, tx2.WalletTransaction})
	getTxsAndCheck(2, &tx1.ID, false, []*asset.WalletTransaction{tx3.WalletTransaction, tx1.WalletTransaction})
	getTxsAndCheck(2, &tx1.ID, true, []*asset.WalletTransaction{tx1.WalletTransaction, tx2.WalletTransaction})
	getPendingTxsAndCheck([]*extendedWalletTx{tx3, tx1})

	err = txHistoryStore.removeTx(tx1.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction, tx2.WalletTransaction})

	err = txHistoryStore.removeTx(tx2.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction})

	err = txHistoryStore.removeTx(tx3.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	getTxsAndCheck(0, nil, true, []*asset.WalletTransaction{})

	_, err = txHistoryStore.getTxs(1, &tx2.ID, true)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected coin not found error but got %v", err)
	}
}

func TestSetAndGetLastQuery(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	txHistoryStore, err := newBadgerTxDB(tempDir, tLogger)
	if err != nil {
		t.Fatalf("error creating tx history store: %v", err)
	}
	defer txHistoryStore.close()

	_, err = txHistoryStore.getLastReceiveTxQuery()
	if !errors.Is(err, errNeverQueried) {
		t.Fatalf("Failed to get last query: %v", err)
	}

	block := uint64(12345)
	err = txHistoryStore.setLastReceiveTxQuery(block)
	if err != nil {
		t.Fatalf("Failed to set last query: %v", err)
	}

	lastQuery, err := txHistoryStore.getLastReceiveTxQuery()
	if err != nil {
		t.Fatalf("Failed to get last query: %v", err)
	}
	if lastQuery != block {
		t.Fatalf("Expected last query to be %d, but got %d", block, lastQuery)
	}
}
