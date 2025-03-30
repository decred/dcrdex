//go:build !harness && !rpclive

package eth

import (
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

func TestMigrateLegacyTxDB(t *testing.T) {
	tempDir := t.TempDir()
	tLogger := dex.StdOutLogger("TXDB", dex.LevelTrace)

	legacyDBPath := filepath.Join(tempDir, "legacy")

	// Create and populate legacy db
	legacyDB, err := newBadgerTxDB(legacyDBPath, tLogger)
	if err != nil {
		t.Fatalf("error creating legacy db: %v", err)
	}

	// Create some test transactions
	_, eth, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	newTx := func(nonce uint64) *extendedWalletTx {
		return eth.extendedTx(&genTxResult{
			tx:     node.newTransaction(nonce, big.NewInt(1)),
			txType: asset.Send,
			amt:    1,
		})
	}

	wt1 := newTx(1)
	wt1.Confirmed = true
	wt2 := newTx(2)
	wt3 := newTx(3)
	wt3.TokenID = &usdcEthID

	// Store in legacy db
	txs := []*extendedWalletTx{wt1, wt2, wt3}
	for _, tx := range txs {
		err := legacyDB.storeTx(tx)
		if err != nil {
			t.Fatalf("error storing tx in legacy db: %v", err)
		}
	}

	legacyDB.Close()

	// Create new lexi db
	lexiDB, err := NewTxDB(tempDir+"/lexi", tLogger, BipID)
	if err != nil {
		t.Fatalf("error creating lexi db: %v", err)
	}

	// Migrate
	err = migrateLegacyTxDB(legacyDBPath, lexiDB, tLogger)
	if err != nil {
		t.Fatalf("error migrating legacy db: %v", err)
	}

	// Check all transactions were migrated
	migratedTxsResp, err := lexiDB.getTxs(nil, &asset.TxHistoryRequest{N: 0, Past: true})
	if err != nil {
		t.Fatalf("error getting migrated txs: %v", err)
	}

	if len(migratedTxsResp.Txs) != len(txs) {
		t.Fatalf("expected %d txs but got %d", len(txs), len(migratedTxsResp.Txs))
	}

	// Check each tx was migrated correctly
	for i, origTx := range txs {
		var found bool
		for _, tx := range migratedTxsResp.Txs {
			if reflect.DeepEqual(origTx.WalletTransaction, tx) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tx %s #%d not found in migrated txs", origTx.ID, i)
		}
	}

	// Check that the legacy db was deleted
	_, err = os.Stat(legacyDBPath)
	if !os.IsNotExist(err) {
		t.Fatalf("legacy db was not deleted: %v", err)
	}
}
