// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

//go:build ltclive

package ltc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"decred.org/dcrdex/dex/config"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/decred/dcrd/chaincfg/chainhash" // just for the array type, not its methods
	"github.com/decred/dcrd/rpcclient/v7"
)

// TestOnlineDeserializeBlock attempts to deserialize every testnet4 LTC block
// from 1000 blocks prior to mweb to the chain tip.
func TestOnlineDeserializeBlock(t *testing.T) {
	cfg := struct {
		RPCUser string `ini:"rpcuser"`
		RPCPass string `ini:"rpcpassword"`
	}{}
	usr, _ := user.Current()
	if err := config.ParseInto(filepath.Join(usr.HomeDir, ".litecoin", "litecoin.conf"), &cfg); err != nil {
		t.Fatalf("config.ParseInto error: %v", err)
	}
	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         "127.0.0.1:19332", // testnet4, mainnet is 9332
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
	if err != nil {
		t.Fatalf("error creating RPC client: %v", err)
	}

	msg, err := client.RawRequest(context.Background(), "getblockchaininfo", nil)
	if err != nil {
		t.Fatalf("getblockchaininfo: %v", err)
	}
	gbci := &struct {
		Chain string `json:"chain"`
	}{}
	err = json.Unmarshal(msg, &gbci)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	isTestNet := gbci.Chain == "test"
	t.Log("Network:", gbci.Chain)

	makeParams := func(args ...interface{}) []json.RawMessage {
		params := make([]json.RawMessage, 0, len(args))
		for i := range args {
			p, err := json.Marshal(args[i])
			if err != nil {
				t.Fatal(err)
			}
			params = append(params, p)
		}
		return params
	}

	getBlkStr := func(hash *chainhash.Hash) string {
		params := makeParams(hash.String(), 0) // not verbose
		msg, err := client.RawRequest(context.Background(), "getblock", params)
		if err != nil {
			t.Fatalf("RawRequest: %v", err)
		}
		var blkStr string
		err = json.Unmarshal(msg, &blkStr)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		return blkStr
	}

	getBlkVerbose := func(hash *chainhash.Hash) *btcjson.GetBlockVerboseResult {
		params := makeParams(hash.String(), 1) // verbose
		msg, err := client.RawRequest(context.Background(), "getblock", params)
		if err != nil {
			t.Fatalf("RawRequest: %v", err)
		}
		var res btcjson.GetBlockVerboseResult
		err = json.Unmarshal(msg, &res)
		if err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		return &res
	}

	// start 1000 blocks prior to mweb testnet blocks
	// 2215586 for mainnet, 2214584 for testnet4
	var startBlk, numBlocks int64 = 2214584, 2000
	if isTestNet {
		numBlocks = 12000 // testnet blocks are empty and fast
	}
	for iBlk := startBlk; iBlk <= startBlk+numBlocks; iBlk++ {
		hash, err := client.GetBlockHash(context.Background(), iBlk)
		if err != nil {
			if strings.Contains(err.Error(), "height out of range") {
				break
			}
			t.Fatal(err) // hint: 401 means unauthorized (check user/pass above)
		}
		blkStr := getBlkStr(hash)
		blk, err := DeserializeBlock(hex.NewDecoder(strings.NewReader(blkStr)))
		if err != nil {
			t.Fatalf("Unmarshal (%d): %v", iBlk, err)
		}
		if iBlk%500 == 0 {
			fmt.Println(iBlk)
		}
		gbv := getBlkVerbose(hash)
		if len(blk.Transactions) != len(gbv.Tx) {
			t.Fatalf("block %v has %d but decoded %d txns", hash, len(gbv.Tx), len(blk.Transactions))
		}
		for i, tx := range blk.Transactions {
			txid := tx.TxHash().String()
			if txid != gbv.Tx[i] {
				t.Errorf("got txid %v, wanted %v", txid, gbv.Tx[i])
			}
		}
	}
}
