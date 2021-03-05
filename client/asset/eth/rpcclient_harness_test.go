// +build harness
//
// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/gamma/node/geth.ipc
//
// These tests are expected to be run in descending as some depend on the tests before. They cannot
// be run in parallel.
//
// TODO: Occasionally tests will fail with "timed out" because transactions are
// not being mined. If one tx is not mined, following txs from the same account
// with a higher nonce also cannot be mined, so it causes all tests after to
// fail as well. Find out why the first tx fails to be broadcast.
//
// TODO: Running these tests many times eventually results in all transactions
// returning "unexpeted error for test ok: exceeds block gas limit". Find out
// why that is.
//
// Other random failures that could use investigation:
//
// === RUN   TestBalance
// INFO [07-22|19:22:59.273] Imported new block headers               count=152 elapsed=13.003ms number=344 hash=62f3d2..092dbc age=1h3m49s
// INFO [07-22|19:22:59.309] Imported new block headers               count=2   elapsed="195.066µs" number=346 hash=d8fdec..bb55c2 age=1h3m47s
// WARN [07-22|19:23:02.157] Served eth_getBalance                    reqid=12 t=3.000772099s err="getDeleteStateObject (2b84c791b79ee37de042ad2fff1a253c3ce9bc27) error: no suitable peers available"
//     rpcclient_harness_test.go:252: getDeleteStateObject (2b84c791b79ee37de042ad2fff1a253c3ce9bc27) error: no suitable peers available
// --- FAIL: TestBalance (3.00s)
//
// ----------------------------------------------------------------------------------------------------
//
// NOTE: May happen on ok before or after locktime.
//
// === RUN   TestRedeem
// INFO [07-22|20:36:36.677] Looking for peers                        peercount=1 tried=3 static=1
// INFO [07-22|20:36:37.003] Imported new block headers               count=1   elapsed="99.306µs"  number=967 hash=02e925..b84642
// INFO [07-22|20:36:37.467] Submitted transaction                    hash=0x51c241ff265de32e1513f6aeba491dcf5ce1c8bc1297a240ab11c5e39363f62f from=0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27 nonce=113 recipient=0xc26C58195deBc5E2e9864DF74ab75FEdd69330d0 value=1,000,000,000,000,000,000
// INFO [07-22|20:36:38.004] Imported new block headers               count=1   elapsed="82.174µs"  number=968 hash=3df9b8..006253
// INFO [07-22|20:36:39.003] Imported new block headers               count=1   elapsed="100.478µs" number=969 hash=9c074c..ea162b
// INFO [07-22|20:36:40.003] Imported new block headers               count=1   elapsed="94.357µs"  number=970 hash=e3a897..79029d
// INFO [07-22|20:36:41.005] Imported new block headers               count=1   elapsed="91.873µs"  number=971 hash=3ca379..a8ad4e
// INFO [07-22|20:36:42.003] Imported new block headers               count=1   elapsed="90.079µs"  number=972 hash=7850ec..7915bf
// INFO [07-22|20:36:43.005] Imported new block headers               count=1   elapsed="87.304µs"  number=973 hash=2e100f..5d6c32
// INFO [07-22|20:36:44.003] Imported new block headers               count=1   elapsed="113.994µs" number=974 hash=d375e7..1d3c4f
// INFO [07-22|20:36:45.005] Imported new block headers               count=1   elapsed="85.13µs"   number=975 hash=a32fa3..1b2c7f
// INFO [07-22|20:36:45.583] Submitted transaction                    hash=0xcfb227a021311cad7026bf0f04536ddbc9b417e391e023b7ac50ae7fa4fc6138 from=0x345853e21b1d475582E71cC269124eD5e2dD3422 nonce=30  recipient=0xc26C58195deBc5E2e9864DF74ab75FEdd69330d0 value=0
// (*types.Transaction)(0xc00054b320)({
//  inner: (*types.LegacyTx)(0xc00054b260)({
//   Nonce: (uint64) 30,
//   GasPrice: (*big.Int)(0xc009bc50a0)(82000000000),
//   Gas: (uint64) 1000000,
//   To: (*common.Address)(0xc0004b6780)((len=20 cap=20) 0xc26C58195deBc5E2e9864DF74ab75FEdd69330d0),
//   Value: (*big.Int)(0xc009bc5020)(0),
//   Data: ([]uint8) (len=68 cap=68) {
//    00000000  b3 15 97 ad 19 b0 4b d7  86 92 ce 4b 63 4f 37 08  |......K....KcO7.|
//    00000010  e5 3e c2 64 c1 f3 44 b0  2d bf 19 e9 ab 70 57 a2  |.>.d..D.-....pW.|
//    00000020  e7 ff 71 b6 55 53 a4 af  17 eb 6e 8a 44 98 6d 97  |..q.US....n.D.m.|
//    00000030  d5 8a e3 35 08 0f f0 8b  31 8f 78 b7 a3 24 09 47  |...5....1.x..$.G|
//    00000040  ac a6 73 05                                       |..s.|
//   },
//   V: (*big.Int)(0xc009bc4fa0)(120),
//   R: (*big.Int)(0xc009bc4e20)(43250630171892020267491356199781316832083307995525815821957676393523217865367),
//   S: (*big.Int)(0xc009bc4ea0)(9700400057119714330402052450040760603012018937638749971416620527387734954049)
//  }),
//  time: (time.Time) 2021-07-22 20:36:45.583228788 +0900 JST m=+18.925814505,
//  hash: (atomic.Value) {
//   v: (interface {}) <nil>
//  },
//  size: (atomic.Value) {
//   v: (interface {}) <nil>
//  },
//  from: (atomic.Value) {
//   v: (interface {}) <nil>
//  }
// })
// INFO [07-22|20:36:46.004] Imported new block headers               count=1   elapsed="95.89µs"   number=976 hash=09893b..40897a
// (*types.Receipt)(0xc0001b0000)({
//  Type: (uint8) 0,
//  PostState: ([]uint8) <nil>,
//  Status: (uint64) 1,
//  CumulativeGasUsed: (uint64) 61793,
//  Bloom: (types.Bloom) (len=256 cap=256) {
//   00000000  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000010  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000020  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000030  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000040  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000050  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000060  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000070  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000080  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   00000090  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000a0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000b0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000c0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000d0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000e0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//   000000f0  00 00 00 00 00 00 00 00  00 00 00 00 00 00 00 00  |................|
//  },
//  Logs: ([]*types.Log) {
//  },
//  TxHash: (common.Hash) (len=32 cap=32) 0xcfb227a021311cad7026bf0f04536ddbc9b417e391e023b7ac50ae7fa4fc6138,
//  ContractAddress: (common.Address) (len=20 cap=20) 0x0000000000000000000000000000000000000000,
//  GasUsed: (uint64) 61793,
//  BlockHash: (common.Hash) (len=32 cap=32) 0x09893bf05954ed3c1218a240851123faa4a4b6a1ce441b7b8087be812540897a,
//  BlockNumber: (*big.Int)(0xc009c7dda0)(976),
//  TransactionIndex: (uint) 0
// })
//     rpcclient_harness_test.go:567: unexpeted balance change for test ok before locktime: want 11018877295774000000000 got 11018875573773997899999
// --- FAIL: TestRedeem (10.28s)

package eth

import (
	"context"
	"crypto/sha256"
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
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/server/asset/eth"
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
	gasPrice         = big.NewInt(82e9)
	homeDir          = os.Getenv("HOME")
	contractAddrFile = filepath.Join(homeDir, "dextest", "eth", "contract_addr.txt")
	alphaNodeDir     = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	ethClient        = new(rpcclient)
	ctx              context.Context
	tLogger          = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
	simnetAddr       = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
	simnetAcct       = &accounts.Account{Address: simnetAddr}
	participantAddr  = common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	participantAcct  = &accounts.Account{Address: participantAddr}
	simnetID         = int64(42)
	newTXOpts        = func(ctx context.Context, from common.Address, value *big.Int) *bind.TransactOpts {
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
	addrBytes, err := ioutil.ReadFile(contractAddrFile)
	if err != nil {
		fmt.Printf("error reading contract address: %v\n", err)
		os.Exit(1)
	}
	addrLen := len(addrBytes)
	if addrLen == 0 {
		fmt.Printf("no contract address found at %v\n", contractAddrFile)
		os.Exit(1)
	}
	addrBytes = addrBytes[:addrLen-1]
	fmt.Printf("Contract address is %v\n", string(addrBytes))
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
	if err := ethClient.connect(ctx, wallet.internalNode, common.HexToAddress(string(addrBytes))); err != nil {
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

func TestSwap(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(swap)
}

func TestSyncProgress(t *testing.T) {
	progress, err := ethClient.syncProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(progress)
}

func TestPeers(t *testing.T) {
	peers, err := ethClient.peers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(peers)
}

func TestInitiate(t *testing.T) {
	now := time.Now().Unix()
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}
	amt := big.NewInt(1e18)
	txOpts := newTXOpts(ctx, simnetAddr, amt)
	swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	spew.Dump(swap)
	state := eth.SwapState(swap.State)
	if state != eth.None {
		t.Fatalf("unexpeted swap state: want %s got %s", eth.None, state)
	}

	tests := []struct {
		name       string
		subAmt     bool
		finalState eth.SwapState
	}{{
		name:       "ok",
		finalState: eth.Initiated,
		subAmt:     true,
	}, {
		// If the hash already exists, the contract should subtract only
		// the tx fee from the account.
		name:       "secret hash already exists",
		finalState: eth.Initiated,
	}}

	for _, test := range tests {
		originalBal, err := ethClient.balance(ctx, simnetAcct)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		tx, err := ethClient.initiate(txOpts, simnetID, now, secretHash, participantAddr)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should be reduced by a certain amount depending on
		// whether initiate completed successfully on-chain. If
		// unsuccessful the fee is subtracted. If successful, amt is
		// also subtracted.
		bal, err := ethClient.balance(ctx, simnetAcct)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.subAmt {
			wantBal.Sub(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpeted balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		state := eth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpeted swap state for test %v: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func TestRedeem(t *testing.T) {
	amt := big.NewInt(1e18)
	locktime := time.Second * 12
	tests := []struct {
		name              string
		sleep             time.Duration
		redeemer          *accounts.Account
		finalState        eth.SwapState
		addAmt, badSecret bool
	}{{
		name:       "ok before locktime",
		sleep:      time.Second * 8,
		redeemer:   participantAcct,
		finalState: eth.Redeemed,
		addAmt:     true,
	}, {
		name:       "ok after locktime",
		sleep:      time.Second * 16,
		redeemer:   participantAcct,
		finalState: eth.Redeemed,
		addAmt:     true,
	}, {
		name:       "bad secret",
		sleep:      time.Second * 8,
		redeemer:   participantAcct,
		finalState: eth.Initiated,
		badSecret:  true,
	}, {
		name:       "wrong redeemer",
		sleep:      time.Second * 8,
		finalState: eth.Initiated,
		redeemer:   simnetAcct,
	}}

	for _, test := range tests {
		err := ethClient.unlock(ctx, pw, simnetAcct)
		if err != nil {
			t.Fatal(err)
		}
		err = ethClient.unlock(ctx, pw, participantAcct)
		if err != nil {
			t.Fatal(err)
		}
		txOpts := newTXOpts(ctx, simnetAddr, amt)
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := eth.SwapState(swap.State)
		if state != eth.None {
			t.Fatalf("unexpeted swap state for test %v: want %s got %s", test.name, eth.None, state)
		}

		// Create a secret that doesn't has to secredHash.
		if test.badSecret {
			copy(secret[:], encode.RandomBytes(32))
		}

		inLocktime := time.Now().Add(locktime).Unix()
		_, err = ethClient.initiate(txOpts, simnetID, inLocktime, secretHash, participantAcct.Address)
		if err != nil {
			t.Fatalf("unable to initiate swap for test %v: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		originalBal, err := ethClient.balance(ctx, test.redeemer)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		txOpts = newTXOpts(ctx, test.redeemer.Address, nil)
		tx, err := ethClient.redeem(txOpts, simnetID, secret, secretHash)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err := ethClient.balance(ctx, test.redeemer)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpeted balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		state = eth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpeted swap state for test %v: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func TestRefund(t *testing.T) {
	amt := big.NewInt(1e18)
	locktime := time.Second * 12
	tests := []struct {
		name           string
		sleep          time.Duration
		refunder       *accounts.Account
		finalState     eth.SwapState
		addAmt, redeem bool
	}{{
		name:       "ok",
		sleep:      time.Second * 16,
		refunder:   simnetAcct,
		addAmt:     true,
		finalState: eth.Refunded,
	}, {
		name:       "before locktime",
		sleep:      time.Second * 8,
		refunder:   simnetAcct,
		finalState: eth.Initiated,
	}, {
		name:       "wrong refunder",
		sleep:      time.Second * 16,
		refunder:   participantAcct,
		finalState: eth.Initiated,
	}, {
		name:       "already redeemed",
		sleep:      time.Second * 16,
		refunder:   simnetAcct,
		redeem:     true,
		finalState: eth.Redeemed,
	}}

	for _, test := range tests {
		err := ethClient.unlock(ctx, pw, simnetAcct)
		if err != nil {
			t.Fatal(err)
		}
		err = ethClient.unlock(ctx, pw, participantAcct)
		if err != nil {
			t.Fatal(err)
		}
		txOpts := newTXOpts(ctx, simnetAddr, amt)
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := eth.SwapState(swap.State)
		if state != eth.None {
			t.Fatalf("unexpeted swap state for test %v: want %s got %s", test.name, eth.None, state)
		}

		inLocktime := time.Now().Add(locktime).Unix()
		_, err = ethClient.initiate(txOpts, simnetID, inLocktime, secretHash, participantAcct.Address)
		if err != nil {
			t.Fatalf("unable to initiate swap for test %v: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, time.Second*8, false); err != nil {
				t.Fatalf("unexpeted error for test %v: %v", test.name, err)
			}
			txOpts = newTXOpts(ctx, participantAddr, nil)
			_, err := ethClient.redeem(txOpts, simnetID, secret, secretHash)
			if err != nil {
				t.Fatalf("unexpeted error for test %v: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		originalBal, err := ethClient.balance(ctx, test.refunder)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		txOpts = newTXOpts(ctx, test.refunder.Address, nil)
		tx, err := ethClient.refund(txOpts, simnetID, secretHash)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err := ethClient.balance(ctx, test.refunder)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpeted balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpeted error for test %v: %v", test.name, err)
		}
		state = eth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpeted swap state for test %v: want %s got %s", test.name, test.finalState, state)
		}
	}
}
