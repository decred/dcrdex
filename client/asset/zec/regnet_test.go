//go:build harness

package zec

// Regnet tests expect the ZEC test harness to be running.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
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

func TestMultiSplit(t *testing.T) {
	log := dex.StdOutLogger("T", dex.LevelTrace)
	c := make(chan asset.WalletNotification, 16)
	tmpDir := t.TempDir()
	walletCfg := &asset.WalletConfig{
		Type: walletTypeRPC,
		Settings: map[string]string{
			"txsplit":     "true",
			"rpcuser":     "user",
			"rpcpassword": "pass",
			"regtest":     "1",
			"rpcport":     "33770",
		},
		Emit: asset.NewWalletEmitter(c, BipID, log),
		PeersChange: func(u uint32, err error) {
			log.Info("peers changed", u, err)
		},
		DataDir: tmpDir,
	}
	wi, err := NewWallet(walletCfg, log, dex.Simnet)
	if err != nil {
		t.Fatalf("Error making new wallet: %v", err)
	}
	w := wi.(*zecWallet)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			select {
			case n := <-c:
				log.Infof("wallet note emitted: %+v", n)
			case <-ctx.Done():
				return
			}
		}
	}()

	cm := dex.NewConnectionMaster(w)
	if err := cm.ConnectOnce(ctx); err != nil {
		t.Fatalf("Error connecting wallet: %v", err)
	}

	// Unlock all transparent outputs.
	if ops, err := listLockUnspent(w, log); err != nil {
		t.Fatalf("Error listing unspent outputs: %v", err)
	} else if len(ops) > 0 {
		coins := make([]*btc.Output, len(ops))
		for i, op := range ops {
			txHash, _ := chainhash.NewHashFromStr(op.TxID)
			coins[i] = btc.NewOutput(txHash, op.Vout, 0)
		}
		if err := lockUnspent(w, true, coins); err != nil {
			t.Fatalf("Error unlocking coins")
		}
		log.Info("Unlocked %d transparent outputs", len(ops))
	}

	bals, err := w.balances()
	if err != nil {
		t.Fatalf("Error getting wallet balance: %v", err)
	}

	var v0, v1 uint64 = 1e8, 2e8
	orderReq0, orderReq1 := dexzec.RequiredOrderFunds(v0, 1, dexbtc.RedeemP2PKHInputSize, 1), dexzec.RequiredOrderFunds(v1, 1, dexbtc.RedeemP2PKHInputSize, 2)

	tAddr := func() string {
		addr, err := transparentAddressString(w)
		if err != nil {
			t.Fatalf("Error getting transparent address: %v", err)
		}
		return addr
	}

	// Send everything to a transparent address.
	unspents, err := listUnspent(w)
	if err != nil {
		t.Fatalf("listUnspent error: %v", err)
	}
	fees := dexzec.TxFeesZIP317(1+(dexbtc.RedeemP2PKHInputSize*uint64(len(unspents))), 1+(3*dexbtc.P2PKHOutputSize), 0, 0, 0, uint64(bals.orchard.noteCount))
	netBal := bals.available() - fees
	changeVal := netBal - orderReq0 - orderReq1

	recips := []*zSendManyRecipient{
		{Address: tAddr(), Amount: btcutil.Amount(orderReq0).ToBTC()},
		{Address: tAddr(), Amount: btcutil.Amount(orderReq1).ToBTC()},
		{Address: tAddr(), Amount: btcutil.Amount(changeVal).ToBTC()},
	}

	txHash, err := w.sendManyShielded(recips)
	if err != nil {
		t.Fatalf("sendManyShielded error: %v", err)
	}

	log.Infof("z_sendmany successful. txid = %s", txHash)

	// Could be orchard notes. Mature them.
	if err := mineAlpha(ctx); err != nil {
		t.Fatalf("Error mining a block: %v", err)
	}

	// All funds should be transparent now.
	multiFund := &asset.MultiOrder{
		AssetVersion: version,
		Values: []*asset.MultiOrderValue{
			{Value: v0, MaxSwapCount: 1},
			{Value: v1, MaxSwapCount: 2},
		},
		Options: map[string]string{"multisplit": "true"},
	}

	checkFundMulti := func(expSplit bool) {
		t.Helper()
		coinSets, _, fundingFees, err := w.FundMultiOrder(multiFund, 0)
		if err != nil {
			t.Fatalf("FundMultiOrder error: %v", err)
		}

		if len(coinSets) != 2 || len(coinSets[0]) != 1 || len(coinSets[1]) != 1 {
			t.Fatalf("Expected 2 coin sets of len 1 each, got %+v", coinSets)
		}

		coin0, coin1 := coinSets[0][0], coinSets[1][0]

		if err := w.cm.ReturnCoins(asset.Coins{coin0, coin1}); err != nil {
			t.Fatalf("ReturnCoins error: %v", err)
		}

		if coin0.Value() != orderReq0 {
			t.Fatalf("coin 0 had insufficient value: %d < %d", coin0.Value(), orderReq0)
		}

		if coin1.Value() < orderReq1 {
			t.Fatalf("coin 1 had insufficient value: %d < %d", coin1.Value(), orderReq1)
		}

		// Should be no split tx.
		split := fundingFees > 0
		if split != expSplit {
			t.Fatalf("Expected split %t, got %t", expSplit, split)
		}

		log.Infof("Coin 0: %s", coin0)
		log.Infof("Coin 1: %s", coin1)
		log.Infof("Funding fees: %d", fundingFees)
	}

	checkFundMulti(false) // no split

	// Could be orchard notes. Mature them.
	if err := mineAlpha(ctx); err != nil {
		t.Fatalf("Error mining a block: %v", err)
	}

	// Send everything to a single transparent address to test for a
	// fully-transparent split tx.
	splitFees := dexzec.TxFeesZIP317(1+(3*dexbtc.RedeemP2PKHInputSize), 1+dexbtc.P2PKHOutputSize, 0, 0, 0, 0)
	netBal -= splitFees
	txHash, err = w.sendOneShielded(ctx, tAddr(), netBal, NoPrivacy)
	if err != nil {
		t.Fatalf("sendOneShielded(transparent) error: %v", err)
	}
	log.Infof("Sent all to transparent with tx %s", txHash)

	// Could be orchard notes. Mature them.
	if err := mineAlpha(ctx); err != nil {
		t.Fatalf("Error mining a block: %v", err)
	}

	checkFundMulti(true) // fully-transparent split

	// Could be orchard notes. Mature them.
	if err := mineAlpha(ctx); err != nil {
		t.Fatalf("Error mining a block: %v", err)
	}

	// Send everything to a shielded address.
	addrRes, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		t.Fatalf("zGetAddressForAccount error: %v", err)
	}
	receivers, err := zGetUnifiedReceivers(w, addrRes.Address)
	if err != nil {
		t.Fatalf("zGetUnifiedReceivers error: %v", err)
	}
	orchardAddr := receivers.Orchard

	bals, err = w.balances()
	if err != nil {
		t.Fatalf("Error getting wallet balance: %v", err)
	}
	unspents, err = listUnspent(w)
	if err != nil {
		t.Fatalf("listUnspent error: %v", err)
	}

	splitFees = dexzec.TxFeesZIP317(1+(dexbtc.RedeemP2PKHInputSize*uint64(len(unspents))), 1, 0, 0, 0, uint64(bals.orchard.noteCount))
	netBal = bals.available() - splitFees

	txHash, err = w.sendOneShielded(ctx, orchardAddr, netBal, NoPrivacy)
	if err != nil {
		t.Fatalf("sendManyShielded error: %v", err)
	}
	log.Infof("sendOneShielded(shielded) successful. txid = %s", txHash)

	// Could be orchard notes. Mature them.
	if err := mineAlpha(ctx); err != nil {
		t.Fatalf("Error mining a block: %v", err)
	}

	checkFundMulti(true) // shielded split

	cancel()
	cm.Wait()
}

func mineAlpha(ctx context.Context) error {
	// Wait for txs to propagate
	select {
	case <-time.After(time.Second * 5):
	case <-ctx.Done():
		return ctx.Err()
	}
	// Mine
	if err := exec.Command("tmux", "send-keys", "-t", "zec-harness:4", "./mine-alpha 1", "C-m").Run(); err != nil {
		return err
	}
	// Wait for blocks to propagate
	select {
	case <-time.After(time.Second * 5):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
