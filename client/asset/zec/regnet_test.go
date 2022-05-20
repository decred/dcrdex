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
	"github.com/decred/dcrd/rpcclient/v7"
)

var (
	tLotSize uint64 = 1e6
	tZEC            = &dex.Asset{
		ID:           BipID,
		Symbol:       "zec",
		SwapSize:     dexzec.InitTxSize,
		SwapSizeBase: dexzec.InitTxSizeBase,
		MaxFeeRate:   100,
		SwapConf:     1,
	}
)

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
		Unencrypted: true,
	})
}

// TestDeserializeTestnet must be run against a full RPC node.
func TestDeserializeTestnetBlocks(t *testing.T) {
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
		Host:         "localhost:18232",
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

	lastV4Block, err := cl.GetBlockHash(ctx, testnetNU5ActivationHeight-1)
	if err != nil {
		t.Fatalf("GetBlockHash(%d) error: %v", testnetNU5ActivationHeight-1, err)
	}

	lastV3Block, err := cl.GetBlockHash(ctx, testnetSaplingActivationHeight-1)
	if err != nil {
		t.Fatalf("GetBlockHash(%d) error: %v", testnetSaplingActivationHeight-1, err)
	}

	lastV2Block, err := cl.GetBlockHash(ctx, testnetOverwinterActivationHeight-1)
	if err != nil {
		t.Fatalf("GetBlockHash(%d) error: %v", testnetOverwinterActivationHeight-1, err)
	}

	mustMarshal := func(thing interface{}) json.RawMessage {
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

			// for i, tx := range zecBlock.Transactions {
			// 	switch {
			// 	case tx.NActionsOrchard > 0 && tx.NOutputsSapling > 0:
			// 		fmt.Printf("orchard + sapling shielded tx: %s:%d \n", hashStr, i)
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
			// 	}
			// }

			hashStr = zecBlock.Header.PrevBlock.String()
		}
	}

	// Test version 5 blocks.
	fmt.Println("Testing version 5 blocks")
	nBlocksFromHash(tipHash.String(), 1000)

	// Test version 4 blocks.
	fmt.Println("Testing version 4 blocks")
	nBlocksFromHash(lastV4Block.String(), 1000)

	// Test version 3 blocks.
	fmt.Println("Testing version 3 blocks")
	nBlocksFromHash(lastV3Block.String(), 1000)

	// Test version 2 blocks.
	fmt.Println("Testing version 2 blocks")
	nBlocksFromHash(lastV2Block.String(), 1000)
}
