// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"testing"
	"time"

	dexpkg "decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/meshevents"
)

func TestBuildMeshCompatSnapshot(t *testing.T) {
	cfg := &DexConf{
		DataDir:          "/tmp/a",
		Network:          dexpkg.Testnet,
		BroadcastTimeout: 12 * time.Minute,
		TxWaitExpiration: 2 * time.Minute,
		CancelThreshold:  0.95,
		FreeCancels:      true,
		PenaltyThreshold: 20,
		NodeRelayAddr:    "127.0.0.1:1000",
		Assets: []*Asset{
			{
				Symbol:     "dcr",
				Network:    "testnet",
				MaxFeeRate: 10,
				SwapConf:   2,
				RegXPub:    "xpub-1",
				BondAmt:    100,
				BondConfs:  3,
			},
		},
		Markets: []*dexpkg.MarketInfo{
			{
				Name:                   "dcr_btc",
				Base:                   42,
				Quote:                  0,
				LotSize:                1e8,
				ParcelSize:             2,
				RateStep:               1e3,
				EpochDuration:          20000,
				MarketBuyBuffer:        1.25,
				MaxUserCancelsPerEpoch: 5,
			},
		},
	}
	baseline, err := buildMeshCompatSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Config.EventSchemaVersion != meshevents.EventSchemaVersion {
		t.Fatalf("event schema version = %d, want %d",
			baseline.Config.EventSchemaVersion, meshevents.EventSchemaVersion)
	}

	t.Run("local settings do not change the hash", func(t *testing.T) {
		changed := *cfg
		changed.DataDir = "/tmp/b"
		changed.NodeRelayAddr = "127.0.0.1:2000"
		changed.MeshCfg = &MeshConfig{PeerAddr: "127.0.0.1:3000"}

		snapshot, err := buildMeshCompatSnapshot(&changed)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Hash != baseline.Hash {
			t.Fatal("hash changed after local-only config changes")
		}
	})

	t.Run("asset settings change the hash", func(t *testing.T) {
		asset := *cfg.Assets[0]
		asset.RegXPub = "xpub-2"
		changed := *cfg
		changed.Assets = []*Asset{&asset}

		snapshot, err := buildMeshCompatSnapshot(&changed)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Hash == baseline.Hash {
			t.Fatal("hash did not change after changing the registration xpub")
		}
	})
}
