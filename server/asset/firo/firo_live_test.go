//go:build firolive

package firo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/user"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/decred/dcrd/rpcclient/v8"
)

func TestScanTestnetBlocks(t *testing.T) {
	// Testnet switched to ProgPOW at 37_310
	testScanBlocks(t, dex.Testnet, 35_000, 40_000, "18888")
}

func TestScanMainnetBlocks(t *testing.T) {
	// Mainnet switched to ProgPOW at 419_269
	testScanBlocks(t, dex.Mainnet, 415_000, 425_000, "8888")
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deserializeBlockAtHeight := func(blockHeight int64) {
		blockHash, err := cl.GetBlockHash(ctx, blockHeight)
		if err != nil {
			t.Fatalf("Error getting block hash for ")
		}

		hashStr, _ := json.Marshal(blockHash.String())

		b, err := cl.RawRequest(ctx, "getblock", []json.RawMessage{hashStr, []byte("false")})
		if err != nil {
			t.Fatalf("RawRequest error: %v", err)
		}
		var blockB dex.Bytes
		if err := json.Unmarshal(b, &blockB); err != nil {
			t.Fatalf("Error unmarshalling hash string: %v", err)
		}
		firoBlock, err := deserializeFiroBlock(blockB, net)
		if err != nil {
			t.Fatalf("Deserialize error for block %s at height %d: %v", blockHash, blockHeight, err)
		}
		if firoBlock.HashRootMTP != [16]byte{} {
			// None found on testnet or mainnet. I think the MTP proof stuff
			// was cleaned out in an upgrade or something.
			fmt.Printf("##### Block %d has MTP proofs \n", blockHeight)
		}
	}

	for i := startHeight; i <= endHeight; i++ {
		deserializeBlockAtHeight(i)
	}
}
