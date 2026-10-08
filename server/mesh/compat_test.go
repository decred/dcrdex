// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import "testing"

func TestNewCompatSnapshotDeterministic(t *testing.T) {
	cfgA := CompatConfig{
		Network:            "testnet",
		APIVersion:         4,
		BroadcastTimeoutMS: 720000,
		TxWaitExpirationMS: 120000,
		CancelThreshold:    0.95,
		FreeCancels:        false,
		PenaltyThreshold:   20,
		Assets: []CompatAsset{
			{ID: 42, Symbol: "DCR", MaxFeeRate: 10, SwapConf: 2, BondAmt: 1},
			{ID: 0, Symbol: "btc", MaxFeeRate: 20, SwapConf: 3, RegFee: 5, RegConfs: 6},
		},
		Markets: []CompatMarket{
			{Name: "btc_dcr", Base: 0, Quote: 42, LotSize: 2, ParcelSize: 3, RateStep: 4, EpochDuration: 5, MarketBuyBuffer: 1.25, MaxUserCancelsPerEpoch: 7},
			{Name: "dcr_btc", Base: 42, Quote: 0, LotSize: 20, ParcelSize: 30, RateStep: 40, EpochDuration: 50, MarketBuyBuffer: 1.15, MaxUserCancelsPerEpoch: 70},
		},
	}
	cfgB := CompatConfig{
		Network:            "TESTNET",
		APIVersion:         4,
		BroadcastTimeoutMS: 720000,
		TxWaitExpirationMS: 120000,
		CancelThreshold:    0.95,
		FreeCancels:        false,
		PenaltyThreshold:   20,
		Assets: []CompatAsset{
			{ID: 0, Symbol: "BTC", MaxFeeRate: 20, SwapConf: 3, RegFee: 5, RegConfs: 6},
			{ID: 42, Symbol: "dcr", MaxFeeRate: 10, SwapConf: 2, BondAmt: 1},
		},
		Markets: []CompatMarket{
			{Name: "DCR_BTC", Base: 42, Quote: 0, LotSize: 20, ParcelSize: 30, RateStep: 40, EpochDuration: 50, MarketBuyBuffer: 1.15, MaxUserCancelsPerEpoch: 70},
			{Name: "BTC_DCR", Base: 0, Quote: 42, LotSize: 2, ParcelSize: 3, RateStep: 4, EpochDuration: 5, MarketBuyBuffer: 1.25, MaxUserCancelsPerEpoch: 7},
		},
	}

	snapA, err := NewCompatSnapshot(cfgA)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(cfgA) error: %v", err)
	}
	snapB, err := NewCompatSnapshot(cfgB)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(cfgB) error: %v", err)
	}

	if snapA.Hash != snapB.Hash {
		t.Fatalf("hash mismatch for equivalent configs: %x != %x", snapA.Hash, snapB.Hash)
	}
}

func TestNewCompatSnapshotDetectsRelevantChange(t *testing.T) {
	cfg := CompatConfig{
		Network:            "testnet",
		APIVersion:         4,
		EventSchemaVersion: 2,
		BroadcastTimeoutMS: 720000,
		TxWaitExpirationMS: 120000,
		CancelThreshold:    0.95,
		PenaltyThreshold:   20,
		Assets: []CompatAsset{
			{ID: 42, Symbol: "dcr", MaxFeeRate: 10, SwapConf: 2},
		},
		Markets: []CompatMarket{
			{Name: "dcr_btc", Base: 42, Quote: 0, LotSize: 2, ParcelSize: 3, RateStep: 4, EpochDuration: 5},
		},
	}

	snapA, err := NewCompatSnapshot(cfg)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(cfg) error: %v", err)
	}

	cfg.Markets[0].RateStep++
	snapB, err := NewCompatSnapshot(cfg)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(modified cfg) error: %v", err)
	}

	if snapA.Hash == snapB.Hash {
		t.Fatalf("hash did not change after relevant config change")
	}
}

func TestNewCompatSnapshotDetectsEventSchemaChange(t *testing.T) {
	cfg := CompatConfig{
		Network:            "testnet",
		APIVersion:         4,
		EventSchemaVersion: 2,
	}

	snapA, err := NewCompatSnapshot(cfg)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(cfg) error: %v", err)
	}

	cfg.EventSchemaVersion++
	snapB, err := NewCompatSnapshot(cfg)
	if err != nil {
		t.Fatalf("NewCompatSnapshot(modified cfg) error: %v", err)
	}

	if snapA.Hash == snapB.Hash {
		t.Fatalf("hash did not change after event schema version change")
	}
}
