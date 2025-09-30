//go:build firolive

package firo

import (
	"encoding/json"
	"os/user"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/decred/dcrd/rpcclient/v8"
)

// TestScanMainnetBlocks tests the firo MTP mining algo change to progpow at block 419_269
// We only support firo on dex after progpow as dex firo code was released late 2023.
func TestScanMainnetBlocks(t *testing.T) {
	testScanBlocks(t, dex.Mainnet, 415_000, 425_000, "8888")
}

// TestScanMainnetRecentBlocks tests more recent blocks after hard fork at 958_655
// https://github.com/firoorg/firo/releases/tag/v0.14.14.0
func TestScanMainnetRecentBlocks(t *testing.T) {
	testScanBlocks(t, dex.Mainnet, 900_000, 1_173_927, "8888") // 1173927 2025-09-26
}

func testScanBlocks(t *testing.T, net dex.Network, startHeight, endHeight int64, port string) {
	u, _ := user.Current()
	configPath := filepath.Join(u.HomeDir, ".firo", "firo.conf")
	var cfg dexbtc.RPCConfig

	if err := config.ParseInto(configPath, &cfg); err != nil {
		t.Fatalf("ParseInto error: %v", err)
	}
	dexbtc.StandardizeRPCConf(&cfg, port)
	cl, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         cfg.RPCBind,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
	if err != nil {
		t.Fatalf("rpcclient.New error: %v", err)
	}

	deserializeBlockAtHeight := func(blockHeight int64) {
		blockHash, err := cl.GetBlockHash(blockHeight)
		if err != nil {
			t.Fatalf("Error getting block hash for ")
		}

		hashStr, _ := json.Marshal(blockHash.String())

		b, err := cl.RawRequest("getblock", []json.RawMessage{hashStr, []byte("false")})
		if err != nil {
			t.Fatalf("RawRequest error: %v", err)
		}
		var blockB dex.Bytes
		if err := json.Unmarshal(b, &blockB); err != nil {
			t.Fatalf("Error unmarshalling hash string: %v", err)
		}
		var params *chaincfg.Params
		switch net {
		case dex.Mainnet:
			params = dexfiro.MainNetParams
		case dex.Testnet:
			params = dexfiro.TestNetParams
		case dex.Regtest:
			params = dexfiro.RegressionNetParams
		default:
			t.Fatalf("unknown network ID %v", net)
		}

		_, err = deserializeBlock(params, blockB)
		if err != nil {
			t.Fatalf("Deserialize error for block %s at height %d: %v", blockHash, blockHeight, err)
		}
	}

	for i := startHeight; i <= endHeight; i++ {
		deserializeBlockAtHeight(i)
	}
}
