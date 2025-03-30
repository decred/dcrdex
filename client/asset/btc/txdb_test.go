// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
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
	tLogger := dex.StdOutLogger("TXDB", dex.LevelInfo)

	txHistoryStore := NewBadgerTxDB(tempDir, tLogger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg, err := txHistoryStore.Connect(ctx)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
	}()

	r, err := txHistoryStore.GetTxs(&asset.TxHistoryRequest{Past: true})
	if err != nil {
		t.Fatalf("error retrieving txs: %v", err)
	}
	if len(r.Txs) != 0 {
		t.Fatalf("expected 0 txs but got %d", len(r.Txs))
	}

	tx1 := &ExtendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Send,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        1e5,
			BlockNumber: 0,
		},
		Submitted: false,
	}

	tx2 := &ExtendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Receive,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        3e5,
			BlockNumber: 0,
		},
		Submitted: true,
	}

	tx3 := &ExtendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:        asset.Swap,
			ID:          hex.EncodeToString(encode.RandomBytes(32)),
			Amount:      1e8,
			Fees:        2e5,
			BlockNumber: 0,
		},
		Submitted: true,
	}

	GetTxsAndCheck := func(n int, refID *string, past bool, expected []*asset.WalletTransaction) {
		t.Helper()

		r, err = txHistoryStore.GetTxs(&asset.TxHistoryRequest{
			N:     n,
			RefID: refID,
			Past:  past,
		})
		if err != nil {
			t.Fatalf("failed to get txs: %v", err)
		}
		if len(r.Txs) != len(expected) {
			t.Fatalf("expected %d txs but got %d", len(expected), len(r.Txs))
		}
		for i, expectedTx := range expected {
			if !reflect.DeepEqual(expectedTx, r.Txs[i]) {
				t.Fatalf("transaction %d: %+v != %+v", i, expectedTx, r.Txs[i])
			}
		}
	}

	getPendingTxsAndCheck := func(expected []*ExtendedWalletTx) {
		t.Helper()

		txs, err := txHistoryStore.GetPendingTxs()
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

	err = txHistoryStore.StoreTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}

	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx1})

	err = txHistoryStore.MarkTxAsSubmitted(tx1.ID)
	if err != nil {
		t.Fatalf("failed to mark tx as submitted: %v", err)
	}
	tx1.Submitted = true
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx1})

	// Storing same pending tx twice should not change anything.
	err = txHistoryStore.StoreTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx1})

	tx1.BlockNumber = 100
	err = txHistoryStore.StoreTx(tx1)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx1})

	err = txHistoryStore.StoreTx(tx2)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx2.WalletTransaction, tx1.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx2, tx1})

	tx2.BlockNumber = 99
	tx2.Confirmed = true
	err = txHistoryStore.StoreTx(tx2)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx1.WalletTransaction, tx2.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx1})

	err = txHistoryStore.StoreTx(tx3)
	if err != nil {
		t.Fatalf("failed to store tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction, tx1.WalletTransaction, tx2.WalletTransaction})
	GetTxsAndCheck(2, &tx1.ID, false, []*asset.WalletTransaction{tx3.WalletTransaction, tx1.WalletTransaction})
	GetTxsAndCheck(2, &tx1.ID, true, []*asset.WalletTransaction{tx1.WalletTransaction, tx2.WalletTransaction})
	getPendingTxsAndCheck([]*ExtendedWalletTx{tx3, tx1})

	err = txHistoryStore.RemoveTx(tx1.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction, tx2.WalletTransaction})

	err = txHistoryStore.RemoveTx(tx2.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{tx3.WalletTransaction})

	err = txHistoryStore.RemoveTx(tx3.ID)
	if err != nil {
		t.Fatalf("failed to remove tx: %v", err)
	}
	GetTxsAndCheck(0, nil, true, []*asset.WalletTransaction{})

	_, err = txHistoryStore.GetTx(tx2.ID)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected coin not found error but got %v", err)
	}
}

func TestSetAndGetLastQuery(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	txHistoryStore := NewBadgerTxDB(tempDir, tLogger)
	wg, err := txHistoryStore.Connect(ctx)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
	}()

	_, err = txHistoryStore.GetLastReceiveTxQuery()
	if !errors.Is(err, ErrNeverQueried) {
		t.Fatalf("Failed to get last query: %v", err)
	}

	block := uint64(12345)
	err = txHistoryStore.SetLastReceiveTxQuery(block)
	if err != nil {
		t.Fatalf("Failed to set last query: %v", err)
	}

	lastQuery, err := txHistoryStore.GetLastReceiveTxQuery()
	if err != nil {
		t.Fatalf("Failed to get last query: %v", err)
	}
	if lastQuery != block {
		t.Fatalf("Expected last query to be %d, but got %d", block, lastQuery)
	}
}

func TestStoreAndGetSecNonce(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	txHistoryStore := NewBadgerTxDB(tempDir, tLogger)
	wg, err := txHistoryStore.Connect(ctx)
	if err != nil {
		t.Fatalf("error connecting to tx history store: %v", err)
	}
	defer func() {
		cancel()
		wg.Wait()
	}()

	// Generate random data for testing
	pubNonce := encode.RandomBytes(32)
	secNonce := encode.RandomBytes(32)
	encKey := encode.RandomBytes(32)

	// Store the secret nonce
	err = txHistoryStore.StoreSecNonce(pubNonce, secNonce, encKey)
	if err != nil {
		t.Fatalf("failed to store secret nonce: %v", err)
	}

	// Retrieve the secret nonce
	retrievedSecNonce, err := txHistoryStore.GetSecNonce(pubNonce, encKey)
	if err != nil {
		t.Fatalf("failed to get secret nonce: %v", err)
	}

	// Verify the retrieved nonce matches the original
	if !bytes.Equal(secNonce, retrievedSecNonce) {
		t.Fatalf("retrieved secret nonce does not match original: got %x, want %x",
			retrievedSecNonce, secNonce)
	}

	// Test with wrong encryption key - should fail
	wrongKey := encode.RandomBytes(32)
	_, err = txHistoryStore.GetSecNonce(pubNonce, wrongKey)
	if err == nil {
		t.Fatalf("expected error when using wrong encryption key, but got none")
	}

	// Test with non-existent public nonce - should fail
	nonExistentPubNonce := encode.RandomBytes(32)
	_, err = txHistoryStore.GetSecNonce(nonExistentPubNonce, encKey)
	if err == nil {
		t.Fatalf("expected error when retrieving non-existent nonce, but got none")
	}

	// Delete the secret nonce
	err = txHistoryStore.DeleteSecNonce(pubNonce)
	if err != nil {
		t.Fatalf("failed to delete secret nonce: %v", err)
	}

	// Verify the secret nonce is actually deleted by attempting to retrieve it
	_, err = txHistoryStore.GetSecNonce(pubNonce, encKey)
	if err == nil {
		t.Fatalf("expected error when retrieving deleted nonce, but got none")
	}
}
