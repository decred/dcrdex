//go:build !harness && !rpclive

package eth

import (
	"math/big"
	"reflect"
	"testing"

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
		return eth.extendedTx(node.newTransaction(nonce, big.NewInt(1)), asset.Send, 1, nil)
	}

	wt1 := newTx(1)
	wt1.Confirmed = true
	wt1.TokenID = &usdcTokenID
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

	txs, err = txHistoryStore.getTxs(0, nil, false, &usdcTokenID)
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
		return eth.extendedTx(node.newTransaction(nonce, big.NewInt(1)), asset.Send, 1, nil)
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
