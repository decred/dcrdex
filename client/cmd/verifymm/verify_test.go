// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func signSnap(privKey *secp256k1.PrivateKey, snap *msgjson.MMEpochSnapshot) {
	hash := sha256.Sum256(snap.Serialize())
	snap.Sig = ecdsa.Sign(privKey, hash[:]).Serialize()
}

func TestVerify(t *testing.T) {
	privKey, _ := secp256k1.GeneratePrivateKey()
	pubKeyHex := hex.EncodeToString(privKey.PubKey().SerializeCompressed())

	acctID := make([]byte, 32)
	for i := range acctID {
		acctID[i] = byte(i)
	}

	// Create snapshots with known values.
	snap1 := &msgjson.MMEpochSnapshot{
		MarketID:  "dcr_btc",
		Base:      42,
		Quote:     0,
		EpochIdx:  100,
		EpochDur:  60000,
		AccountID: acctID,
		BuyOrders: []msgjson.SnapOrder{
			{Rate: 1e8, Qty: 5e8},
		},
		SellOrders: []msgjson.SnapOrder{
			{Rate: 2e8, Qty: 5e8},
		},
		BestBuy:  1e8,
		BestSell: 2e8,
	}
	snap2 := &msgjson.MMEpochSnapshot{
		MarketID:  "dcr_btc",
		Base:      42,
		Quote:     0,
		EpochIdx:  200,
		EpochDur:  60000,
		AccountID: acctID,
		BuyOrders: []msgjson.SnapOrder{
			{Rate: 1e8, Qty: 3e8},
		},
		SellOrders: []msgjson.SnapOrder{
			{Rate: 2e8, Qty: 3e8},
		},
		BestBuy:  1e8,
		BestSell: 2e8,
	}
	signSnap(privKey, snap1)
	signSnap(privKey, snap2)

	snapsData, err := json.Marshal([]*msgjson.MMEpochSnapshot{snap1, snap2})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid snapshots", func(t *testing.T) {
		report, err := verify(snapsData, pubKeyHex, 0, 0, 1e8, 0)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.TotalEpochs != 2 {
			t.Fatalf("expected 2 total epochs, got %d", report.TotalEpochs)
		}
		if report.ValidSigs != 2 {
			t.Fatalf("expected 2 valid sigs, got %d", report.ValidSigs)
		}
		if report.InvalidSigs != 0 {
			t.Fatalf("expected 0 invalid sigs, got %d", report.InvalidSigs)
		}
		if report.CoveredEpochs != 2 {
			t.Fatalf("expected 2 covered epochs, got %d", report.CoveredEpochs)
		}
		if report.CoveragePct != 100 {
			t.Fatalf("expected 100%% coverage, got %.2f%%", report.CoveragePct)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		badSnap := &msgjson.MMEpochSnapshot{
			MarketID:  "dcr_btc",
			Base:      42,
			Quote:     0,
			EpochIdx:  300,
			EpochDur:  60000,
			AccountID: acctID,
			BuyOrders: []msgjson.SnapOrder{
				{Rate: 1e8, Qty: 5e8},
			},
			SellOrders: []msgjson.SnapOrder{
				{Rate: 2e8, Qty: 5e8},
			},
			BestBuy:  1e8,
			BestSell: 2e8,
		}
		// Sign with a different key.
		otherKey, _ := secp256k1.GeneratePrivateKey()
		signSnap(otherKey, badSnap)

		data, _ := json.Marshal([]*msgjson.MMEpochSnapshot{badSnap})
		report, err := verify(data, pubKeyHex, 0, 0, 0, 0)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.InvalidSigs != 1 {
			t.Fatalf("expected 1 invalid sig, got %d", report.InvalidSigs)
		}
		if report.ValidSigs != 0 {
			t.Fatalf("expected 0 valid sigs, got %d", report.ValidSigs)
		}
	})

	t.Run("spread assessment", func(t *testing.T) {
		// BestBuy=1e8, BestSell=2e8, mid=1.5e8
		// spread = (2e8-1e8)/1.5e8 * 100 = 66.67%
		report, err := verify(snapsData, pubKeyHex, 0, 0, 0, 70)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.WithinSpread != 2 {
			t.Fatalf("expected 2 within spread (maxSpreadPct=70), got %d", report.WithinSpread)
		}

		// Now with a tight spread requirement.
		report, err = verify(snapsData, pubKeyHex, 0, 0, 0, 1)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.WithinSpread != 0 {
			t.Fatalf("expected 0 within spread (maxSpreadPct=1), got %d", report.WithinSpread)
		}
	})

	t.Run("epoch range filtering", func(t *testing.T) {
		report, err := verify(snapsData, pubKeyHex, 150, 250, 0, 0)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		// Only snap2 (epoch 200) should be included.
		if report.TotalEpochs != 1 {
			t.Fatalf("expected 1 epoch in range [150,250], got %d", report.TotalEpochs)
		}
		if report.Epochs[0].EpochIdx != 200 {
			t.Fatalf("expected epoch 200, got %d", report.Epochs[0].EpochIdx)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		emptyData, _ := json.Marshal([]*msgjson.MMEpochSnapshot{})
		report, err := verify(emptyData, pubKeyHex, 0, 0, 0, 0)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.TotalEpochs != 0 {
			t.Fatalf("expected 0 total epochs, got %d", report.TotalEpochs)
		}
	})

	t.Run("qty threshold", func(t *testing.T) {
		// snap1 has 5e8 on each side, snap2 has 3e8 on each side.
		// With minQty=4e8, only snap1 should be covered.
		report, err := verify(snapsData, pubKeyHex, 0, 0, 4e8, 0)
		if err != nil {
			t.Fatalf("verify error: %v", err)
		}
		if report.CoveredEpochs != 1 {
			t.Fatalf("expected 1 covered epoch with minQty=4e8, got %d", report.CoveredEpochs)
		}
		if report.CoveragePct != 50 {
			t.Fatalf("expected 50%% coverage, got %.2f%%", report.CoveragePct)
		}
	})
}
