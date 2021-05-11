// +build harness
//
// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/gamma/node/geth.ipc
//
// These tests are expected to be run in descending as some depend on the tests before. They cannot
// be run in parallel.
//
// NOTE: Occationally tests will fail with "timed out". Please try again...
//
// TODO: Running these tests many times eventually results in all transactions
// returning "unexpeted error for test ok: exceeds block gas limit". Find out
// why that is.

package eth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

const (
	pw        = "abc"
	alphaAddr = "enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304"
)

var (
	gasPrice        = big.NewInt(82e9)
	homeDir         = os.Getenv("HOME")
	genesisFile     = filepath.Join(homeDir, "dextest", "eth", "genesis.json")
	alphaNodeDir    = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	ethClient       = new(rpcclient)
	ctx             context.Context
	tLogger         = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
	simnetAddr      = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
	simnetAcct      = &accounts.Account{Address: simnetAddr}
	participantAddr = common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	participantAcct = &accounts.Account{Address: participantAddr}
	simnetID        = int64(42)
	newTXOpts       = func(ctx context.Context, from common.Address, value *big.Int) *bind.TransactOpts {
		return &bind.TransactOpts{
			GasPrice: gasPrice,
			GasLimit: 1e6,
			Context:  ctx,
			From:     from,
			Value:    value,
		}
	}
)

func waitForMined(t *testing.T, timeLimit time.Duration, waitTimeLimit bool) error {
	t.Helper()
	err := exec.Command("geth", "--datadir="+alphaNodeDir, "attach", "--exec", "miner.start()").Run()
	if err != nil {
		return err
	}
	defer func() {
		_ = exec.Command("geth", "--datadir="+alphaNodeDir, "attach", "--exec", "miner.stop()").Run()
	}()
	timesUp := time.After(timeLimit)
out:
	for {
		select {
		case <-timesUp:
			return errors.New("timed out")
		case <-time.After(time.Second):
			txs, err := ethClient.pendingTransactions(ctx)
			if err != nil {
				return err
			}
			if len(txs) == 0 {
				break out
			}
		}
	}
	if waitTimeLimit {
		<-timesUp
	}
	return nil
}

func TestMain(m *testing.M) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer func() {
		cancel()
		ethClient.shutdown()
	}()
	tmpDir, err := ioutil.TempDir("", "dextest")
	if err != nil {
		fmt.Printf("error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)
	genBytes, err := ioutil.ReadFile(genesisFile)
	if err != nil {
		fmt.Printf("error reading genesis file: %v\n", err)
		os.Exit(1)
	}
	genLen := len(genBytes)
	if genLen == 0 {
		fmt.Printf("no genesis found at %v\n", genesisFile)
		os.Exit(1)
	}
	genBytes = genBytes[:genLen-1]
	fmt.Printf("Genesis is:\n%v\n", string(genBytes))
	SetSimnetGenesis(string(genBytes))
	settings := map[string]string{
		"appdir":         tmpDir,
		"nodelistenaddr": "localhost:30355",
	}
	wallet, err := NewWallet(&asset.WalletConfig{Settings: settings}, tLogger, dex.Simnet)
	if err != nil {
		fmt.Printf("error starting node: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Node created at: %v\n", tmpDir)
	defer func() {
		wallet.internalNode.Close()
		wallet.internalNode.Wait()
	}()
	if err := ethClient.connect(ctx, wallet.internalNode, common.Address{}); err != nil {
		fmt.Printf("connect error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestNodeInfo(t *testing.T) {
	ni, err := ethClient.nodeInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(ni)
}

func TestAddPeer(t *testing.T) {
	if err := ethClient.addPeer(ctx, alphaAddr); err != nil {
		t.Fatal(err)
	}
}

func TestBlockNumber(t *testing.T) {
	var bn uint64
	for i := 0; ; i++ {
		var err error
		bn, err = ethClient.blockNumber(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if bn != 0 {
			break
		}
		if i == 60 {
			t.Fatal("block count has not synced one minute")
		}
		time.Sleep(time.Second)
	}
	spew.Dump(bn)
}

func TestBestBlockHash(t *testing.T) {
	bbh, err := ethClient.bestBlockHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bbh)
}

func TestBestHeader(t *testing.T) {
	bh, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bh)
}

func TestBlock(t *testing.T) {
	bh, err := ethClient.bestBlockHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ethClient.block(ctx, bh)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(b)
}

func TestImportAccounts(t *testing.T) {
	// The address of this will be 2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27.
	privB, err := hex.DecodeString("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")
	if err != nil {
		t.Fatal(err)
	}
	acct, err := ethClient.importAccount(pw, privB)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(acct)
	// The address of this will be 345853e21b1d475582E71cC269124eD5e2dD3422.
	privB, err = hex.DecodeString("0695b9347a4dc096ae5c6f1935380ceba550c70b112f1323c211bade4d11651a")
	if err != nil {
		t.Fatal(err)
	}
	acct, err = ethClient.importAccount(pw, privB)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(acct)
}

func TestAccounts(t *testing.T) {
	accts := ethClient.accounts()
	spew.Dump(accts)
}

func TestBalance(t *testing.T) {
	bal, err := ethClient.balance(ctx, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bal)
}

func TestUnlock(t *testing.T) {
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLock(t *testing.T) {
	err := ethClient.lock(ctx, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
}

func TestListWallets(t *testing.T) {
	wallets, err := ethClient.listWallets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(wallets)
}

func TestSendTransaction(t *testing.T) {
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
	tx := map[string]string{
		"from":     fmt.Sprintf("0x%x", simnetAddr),
		"to":       fmt.Sprintf("0x%x", simnetAddr),
		"value":    fmt.Sprintf("0x%x", big.NewInt(1)),
		"gasPrice": fmt.Sprintf("0x%x", gasPrice),
	}
	txHash, err := ethClient.sendTransaction(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(txHash)
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal("timeout")
	}
}

func TestTransactionReceipt(t *testing.T) {
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
	tx := map[string]string{
		"from":     fmt.Sprintf("0x%x", simnetAddr),
		"to":       fmt.Sprintf("0x%x", simnetAddr),
		"value":    fmt.Sprintf("0x%x", big.NewInt(1)),
		"gasPrice": fmt.Sprintf("0x%x", gasPrice),
	}
	txHash, err := ethClient.sendTransaction(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal("timeout")
	}
	receipt, err := ethClient.transactionReceipt(ctx, txHash)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt)
}

func TestPendingTransactions(t *testing.T) {
	txs, err := ethClient.pendingTransactions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(txs)
}

func TestSyncProgress(t *testing.T) {
	progress, err := ethClient.syncProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(progress)
}
