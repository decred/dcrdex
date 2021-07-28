// +build harness
//
// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/gamma/node/geth.ipc
//
// These tests are expected to be run in descending as some depend on the tests
// before. They cannot be run in parallel.
//
// NOTE: These test reuse a light node that lives in the dextest folders.
// However, when recreating the test database for every test, the nonce used
// for imported accounts is sometimes, randomly, off, which causes transactions
// to not be mined and effectively makes the node unuseable (at least before
// restarting). It also seems to have caused getting balance of an account to
// fail, and sometime the redeem and refund functions to also fail. This could
// be a problem in the future if a user restores from seed. Punting on this
// particular problem for now.
//
// TODO: Running these tests many times eventually results in all transactions
// returning "unexpeted error for test ok: exceeds block gas limit". Find out
// why that is.

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
	testDir          = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests")
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
	// Create dir if none yet exists. This persists for the life of the
	// testing harness.
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		err := os.Mkdir(testDir, 0755)
		if err != nil {
			fmt.Printf("error creating temp dir: %v\n", err)
			os.Exit(1)
		}
	}
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
		"appdir":         testDir,
		"nodelistenaddr": "localhost:30355",
	}
	wallet, err := NewWallet(&asset.WalletConfig{Settings: settings}, tLogger, dex.Simnet)
	if err != nil {
		fmt.Printf("error starting node: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Node created at: %v\n", testDir)
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
	// Unable to import a second time.
	accts := ethClient.accounts()
	if len(accts) > 1 {
		fmt.Println("Skipping TestImportAccounts because accounts are already imported.")
		t.Skip()
	}
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
