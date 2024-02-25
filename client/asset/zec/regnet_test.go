//go:build harness

package zec

// Regnet tests expect the ZEC test harness to be running.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/user"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/decred/dcrd/rpcclient/v8"
)

const (
	mainnetNU5ActivationHeight        = 1687104
	testnetNU5ActivationHeight        = 1842420
	testnetSaplingActivationHeight    = 280000
	testnetOverwinterActivationHeight = 207500
)

var tZEC = &dex.Asset{
	ID:       0,
	Symbol:   "zec",
	Version:  version,
	SwapConf: 1,
}

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tZEC,
		FirstWallet: &livetest.WalletName{
			Node:     "alpha",
			Filename: "alpha.conf",
		},
		SecondWallet: &livetest.WalletName{
			Node:     "beta",
			Filename: "beta.conf",
		},
	})
}

// TestDeserializeTestnet must be run against a full RPC node.
func TestDeserializeTestnetBlocks(t *testing.T) {
	testDeserializeBlocks(t, "18232", testnetNU5ActivationHeight, testnetSaplingActivationHeight, testnetOverwinterActivationHeight)
}

func TestDeserializeMainnetBlocks(t *testing.T) {
	testDeserializeBlocks(t, "8232")
}

func testDeserializeBlocks(t *testing.T, port string, upgradeHeights ...int64) {
	cfg := struct {
		RPCUser string `ini:"rpcuser"`
		RPCPass string `ini:"rpcpassword"`
	}{}

	usr, _ := user.Current()
	if err := config.ParseInto(filepath.Join(usr.HomeDir, ".zcash", "zcash.conf"), &cfg); err != nil {
		t.Fatalf("config.Parse error: %v", err)
	}

	cl, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         "localhost:" + port,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
	if err != nil {
		t.Fatalf("error creating client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tipHash, err := cl.GetBestBlockHash(ctx)
	if err != nil {
		t.Fatalf("GetBestBlockHash error: %v", err)
	}

	mustMarshal := func(thing any) json.RawMessage {
		b, err := json.Marshal(thing)
		if err != nil {
			t.Fatalf("Failed to marshal %T thing: %v", thing, err)
		}
		return b
	}

	blockBytes := func(hashStr string) (blockB dex.Bytes) {
		raw, err := cl.RawRequest(ctx, "getblock", []json.RawMessage{mustMarshal(hashStr), mustMarshal(0)})
		if err != nil {
			t.Fatalf("Failed to fetch block hash for %s: %v", hashStr, err)
		}
		if err := json.Unmarshal(raw, &blockB); err != nil {
			t.Fatalf("Error unmarshaling block bytes for %s: %v", hashStr, err)
		}
		return
	}

	nBlocksFromHash := func(hashStr string, n int) {
		for i := 0; i < n; i++ {
			zecBlock, err := dexzec.DeserializeBlock(blockBytes(hashStr))
			if err != nil {
				t.Fatalf("Error deserializing %s: %v", hashStr, err)
			}

			for _, tx := range zecBlock.Transactions {
				switch {
				case tx.NActionsOrchard > 0:
					fmt.Printf("Orchard transaction with nActionsOrchard = %d \n", tx.NActionsOrchard)
					// case tx.NActionsOrchard > 0 && tx.NOutputsSapling > 0:
					// 	fmt.Printf("orchard + sapling shielded tx: %s:%d \n", hashStr, i)
					// 	case tx.NActionsOrchard > 0:
					// 		fmt.Printf("orchard shielded tx: %s:%d \n", hashStr, i)
					// 	case tx.NOutputsSapling > 0 || tx.NSpendsSapling > 0:
					// 		fmt.Printf("sapling shielded tx: %s:%d \n", hashStr, i)
					// 	case tx.NJoinSplit > 0:
					// 		fmt.Printf("joinsplit tx: %s:%d \n", hashStr, i)
					// 	default:
					// 		if i > 0 {
					// 			fmt.Printf("unshielded tx: %s:%d \n", hashStr, i)
					// 		}
				}
			}

			hashStr = zecBlock.Header.PrevBlock.String()
		}
	}

	// Test version 5 blocks.
	fmt.Println("Testing version 5 blocks")
	nBlocksFromHash(tipHash.String(), 1000)

	ver := 4
	for _, upgradeHeight := range upgradeHeights {
		lastVerBlock, err := cl.GetBlockHash(ctx, upgradeHeight-1)
		if err != nil {
			t.Fatalf("GetBlockHash(%d) error: %v", upgradeHeight-1, err)
		}
		fmt.Printf("Testing version %d blocks \n", ver)
		nBlocksFromHash(lastVerBlock.String(), 1000)
		ver--
	}
}
