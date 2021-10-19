//go:build harness && lgpl
// +build harness,lgpl

// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/gamma/node/geth.ipc
//
// These tests are expected to be run in descending order as some depend on the
// tests before. They cannot be run in parallel.
//
// NOTE: These test reuse a light node that lives in the dextest folders.
// However, when recreating the test database for every test, the nonce used
// for imported accounts is sometimes, randomly, off, which causes transactions
// to not be mined and effectively makes the node unuseable (at least before
// restarting). It also seems to have caused getting balance of an account to
// fail, and sometimes the redeem and refund functions to also fail. This could
// be a problem in the future if a user restores from seed. Punting on this
// particular problem for now.
//
// TODO: Running these tests many times eventually results in all transactions
// returning "unexpected error for test ok: exceeds block gas limit". Find out
// why that is.

package eth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/internal/eth/reentryattack"
	srveth "decred.org/dcrdex/server/asset/eth"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

const (
	alphaNode = "enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304"
	alphaAddr = "18d65fb8d60c1199bb1ad381be47aa692b482605"
	pw        = "bee75192465cef9f8ab1198093dfed594e93ae810ccbbf2b3b12e1771cc6cb19"
)

var (
	gasPrice              = big.NewInt(82e9)
	homeDir               = os.Getenv("HOME")
	contractAddrFile      = filepath.Join(homeDir, "dextest", "eth", "contract_addr.txt")
	simnetWalletDir       = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "simnet")
	participantWalletDir  = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "participant")
	alphaNodeDir          = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	ctx                   context.Context
	tLogger               = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
	simnetWalletSeed      = "5c52e2ef5f5298ec41107e4e9573df4488577fb3504959cbc26c88437205dd2c0812f5244004217452059e2fd11603a511b5d0870ead753df76c966ce3c71531"
	simnetPrivKey         = "8b19650a41e740f3b7ebb7ad112a91f63df9f005320489147cdf6f8d8f585500"
	simnetAddr            = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	simnetAcct            = &accounts.Account{Address: simnetAddr}
	ethClient             = new(rpcclient)
	participantWalletSeed = "b99fb787fc5886eb539830d103c0017eff5241ace28ee137d40f135fd02212b1a897afbdcba037c8c735cc63080558a30d72851eb5a3d05684400ec4123a2d00"
	participantPrivKey    = "88daaaa2cb839a08d90dbe910b4603184bd669ae3316e7a7bae581493b927e61"
	participantAddr       = common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	participantAcct       = &accounts.Account{Address: participantAddr}
	participantEthClient  = new(rpcclient)
	contractAddr          common.Address
	simnetID              = int64(42)
	newTxOpts             = func(ctx context.Context, from *common.Address, value *big.Int) *bind.TransactOpts {
		return &bind.TransactOpts{
			GasPrice: gasPrice,
			GasLimit: 1e6,
			Context:  ctx,
			From:     *from,
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
	// Run in function so that defers happen before os.Exit is called.
	run := func() (int, error) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		defer func() {
			cancel()
			ethClient.shutdown()
		}()
		// Create dir if none yet exists. This persists for the life of the
		// testing harness.
		err := os.MkdirAll(simnetWalletDir, 0755)
		if err != nil {
			return 1, fmt.Errorf("error creating simnet wallet dir dir: %v\n", err)
		}
		err = os.MkdirAll(participantWalletDir, 0755)
		if err != nil {
			return 1, fmt.Errorf("error creating participant wallet dir: %v\n", err)
		}
		addrBytes, err := os.ReadFile(contractAddrFile)
		if err != nil {
			return 1, fmt.Errorf("error reading contract address: %v\n", err)
		}
		addrLen := len(addrBytes)
		if addrLen == 0 {
			return 1, fmt.Errorf("no contract address found at %v\n", contractAddrFile)
		}
		addrStr := string(addrBytes[:addrLen-1])
		contractAddr = common.HexToAddress(addrStr)
		fmt.Printf("Contract address is %v\n", addrStr)
		simnetWallet, err := setupWallet(simnetWalletDir, simnetWalletSeed, "localhost:30355")
		if err != nil {
			return 1, err
		}
		defer func() {
			simnetWallet.internalNode.Close()
			simnetWallet.internalNode.Wait()
		}()
		participantWallet, err := setupWallet(participantWalletDir, participantWalletSeed, "localhost:30356")
		defer func() {
			participantWallet.internalNode.Close()
			participantWallet.internalNode.Wait()
		}()
		addr := common.HexToAddress(addrStr)
		if err := ethClient.connect(ctx, simnetWallet.internalNode, &addr); err != nil {
			return 1, fmt.Errorf("connect error: %v\n", err)
		}
		if err := participantEthClient.connect(ctx, participantWallet.internalNode, &addr); err != nil {
			return 1, fmt.Errorf("connect error: %v\n", err)
		}
		return m.Run(), nil
	}
	exitCode, err := run()
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(exitCode)
}

func setupWallet(walletDir, seed, listenAddress string) (*ExchangeWallet, error) {
	settings := map[string]string{
		"appdir":         walletDir,
		"nodelistenaddr": listenAddress,
	}
	seedB, _ := hex.DecodeString(seed)
	walletConfig := asset.WalletConfig{
		Settings: settings,
		DataDir:  walletDir,
	}
	walletPass, _ := hex.DecodeString(pw)
	createWalletParams := asset.CreateWalletParams{
		Seed:     seedB,
		Pass:     walletPass,
		Settings: settings,
		DataDir:  walletDir,
		Net:      dex.Simnet,
	}
	err := CreateWallet(&createWalletParams)
	if err != nil {
		return nil, fmt.Errorf("error creating node: %v\n", err)
	}
	wallet, err := NewWallet(&walletConfig, tLogger, dex.Simnet)
	if err != nil {
		return nil, fmt.Errorf("error starting node: %v\n", err)
	}
	return wallet, nil
}

func TestNodeInfo(t *testing.T) {
	ni, err := ethClient.nodeInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(ni)
}

func TestAddPeer(t *testing.T) {
	if err := ethClient.addPeer(ctx, alphaNode); err != nil {
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

func TestAccounts(t *testing.T) {
	accts := ethClient.accounts()
	spew.Dump(accts)
}

func TestBalance(t *testing.T) {
	bal, err := ethClient.balance(ctx, &simnetAddr)
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
		t.Fatal(err)
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
		t.Fatal(err)
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

func TestInitiateGas(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	parsedAbi, err := abi.JSON(strings.NewReader(dexeth.ETHSwapABI))
	if err != nil {
		t.Fatalf("unexpected error parsing abi: %v", err)
	}
	data, err := parsedAbi.Pack("initiate", big.NewInt(1), secretHash, &participantAddr)
	if err != nil {
		t.Fatalf("unexpected error packing abi: %v", err)
	}
	msg := ethereum.CallMsg{
		From:  participantAddr,
		To:    &contractAddr,
		Value: big.NewInt(1),
		Gas:   0,
		Data:  data,
	}
	gas, err := participantEthClient.estimateGas(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error from estimateGas: %v", err)
	}
	if gas > srveth.InitGas {
		t.Fatalf("actual gas %v is greater than srveth.InitGas %v", gas, srveth.InitGas)
	}
	if gas+10000 < srveth.InitGas {
		t.Fatalf("actual gas %v is much less than srveth.InitGas %v", gas, srveth.InitGas)
	}
	fmt.Printf("Gas used for initiate: %v \n", gas)
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
	txOpts := newTxOpts(ctx, &simnetAddr, amt)
	swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	spew.Dump(swap)
	state := srveth.SwapState(swap.State)
	if state != srveth.SSNone {
		t.Fatalf("unexpected swap state: want %s got %s", srveth.SSNone, state)
	}

	tests := []struct {
		name       string
		subAmt     bool
		finalState srveth.SwapState
	}{{
		name:       "ok",
		finalState: srveth.SSInitiated,
		subAmt:     true,
	}, {
		// If the hash already exists, the contract should subtract only
		// the tx fee from the account.
		name:       "secret hash already exists",
		finalState: srveth.SSInitiated,
	}}

	for _, test := range tests {
		originalBal, err := ethClient.balance(ctx, &simnetAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		tx, err := ethClient.initiate(txOpts, simnetID, now, secretHash, &participantAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should be reduced by a certain amount depending on
		// whether initiate completed successfully on-chain. If
		// unsuccessful the fee is subtracted. If successful, amt is
		// also subtracted.
		bal, err := ethClient.balance(ctx, &simnetAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.subAmt {
			wantBal.Sub(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpected balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		state := srveth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func TestRedeemGas(t *testing.T) {
	now := time.Now().Unix()
	amt := big.NewInt(1e18)
	txOpts := newTxOpts(ctx, &simnetAddr, amt)
	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])
	_, err := ethClient.initiate(txOpts, simnetID, now, secretHash, &participantAddr)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}
	parsedAbi, err := abi.JSON(strings.NewReader(dexeth.ETHSwapABI))
	if err != nil {
		t.Fatalf("unexpected error parsing abi: %v", err)
	}

	data, err := parsedAbi.Pack("redeem", secret, secretHash)
	if err != nil {
		t.Fatalf("unexpected error packing abi: %v", err)
	}
	msg := ethereum.CallMsg{
		From: participantAddr,
		To:   &contractAddr,
		Gas:  0,
		Data: data,
	}
	gas, err := participantEthClient.estimateGas(ctx, msg)
	if err != nil {
		t.Fatalf("Error estimating gas for redeem function: %v", err)
	}
	if gas > RedeemGas {
		t.Fatalf("actual gas %v is greater than RedeemGas %v", gas, RedeemGas)
	}
	if gas+3000 < RedeemGas {
		t.Fatalf("actual gas %v is much less than RedeemGas %v", gas, RedeemGas)
	}
	fmt.Printf("Gas used for redeem: %v \n", gas)
}

func TestRedeem(t *testing.T) {
	amt := big.NewInt(1e18)
	locktime := time.Second * 12
	tests := []struct {
		name              string
		sleep             time.Duration
		redeemerClient    *rpcclient
		redeemer          *accounts.Account
		finalState        srveth.SwapState
		addAmt, badSecret bool
	}{{
		name:           "ok before locktime",
		sleep:          time.Second * 8,
		redeemerClient: participantEthClient,
		redeemer:       participantAcct,
		finalState:     srveth.SSRedeemed,
		addAmt:         true,
	}, {
		name:           "ok after locktime",
		sleep:          time.Second * 16,
		redeemerClient: participantEthClient,
		redeemer:       participantAcct,
		finalState:     srveth.SSRedeemed,
		addAmt:         true,
	}, {
		name:           "bad secret",
		sleep:          time.Second * 8,
		redeemerClient: participantEthClient,
		redeemer:       participantAcct,
		finalState:     srveth.SSInitiated,
		badSecret:      true,
	}, {
		name:           "wrong redeemer",
		sleep:          time.Second * 8,
		finalState:     srveth.SSInitiated,
		redeemerClient: ethClient,
		redeemer:       simnetAcct,
	}}

	for _, test := range tests {
		err := ethClient.unlock(ctx, pw, simnetAcct)
		if err != nil {
			t.Fatal(err)
		}
		err = participantEthClient.unlock(ctx, pw, participantAcct)
		if err != nil {
			t.Fatal(err)
		}
		txOpts := newTxOpts(ctx, &simnetAddr, amt)
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := srveth.SwapState(swap.State)
		if state != srveth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, srveth.SSNone, state)
		}

		// Create a secret that doesn't has to secretHash.
		if test.badSecret {
			copy(secret[:], encode.RandomBytes(32))
		}

		inLocktime := time.Now().Add(locktime).Unix()
		_, err = ethClient.initiate(txOpts, simnetID, inLocktime, secretHash, &participantAddr)
		if err != nil {
			t.Fatalf("unable to initiate swap for test %v: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		originalBal, err := test.redeemerClient.balance(ctx, &test.redeemer.Address)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		txOpts = newTxOpts(ctx, &test.redeemer.Address, nil)
		tx, err := test.redeemerClient.redeem(txOpts, simnetID, secret, secretHash)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := test.redeemerClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err := test.redeemerClient.balance(ctx, &test.redeemer.Address)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpected balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		state = srveth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, test.finalState, state)
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
		refunderClient *rpcclient
		finalState     srveth.SwapState
		addAmt, redeem bool
	}{{
		name:           "ok",
		sleep:          time.Second * 16,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		addAmt:         true,
		finalState:     srveth.SSRefunded,
	}, {
		name:           "before locktime",
		sleep:          time.Second * 8,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		finalState:     srveth.SSInitiated,
	}, {
		name:           "wrong refunder",
		sleep:          time.Second * 16,
		refunderClient: participantEthClient,
		refunder:       participantAcct,
		finalState:     srveth.SSInitiated,
	}, {
		name:           "already redeemed",
		sleep:          time.Second * 16,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		redeem:         true,
		finalState:     srveth.SSRedeemed,
	}}

	for _, test := range tests {
		err := ethClient.unlock(ctx, pw, simnetAcct)
		if err != nil {
			t.Fatal(err)
		}
		err = participantEthClient.unlock(ctx, pw, participantAcct)
		if err != nil {
			t.Fatal(err)
		}
		txOpts := newTxOpts(ctx, &simnetAddr, amt)
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := srveth.SwapState(swap.State)
		if state != srveth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, srveth.SSNone, state)
		}

		inLocktime := time.Now().Add(locktime).Unix()
		_, err = ethClient.initiate(txOpts, simnetID, inLocktime, secretHash, &participantAddr)
		if err != nil {
			t.Fatalf("unable to initiate swap for test %v: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, time.Second*8, false); err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
			txOpts = newTxOpts(ctx, &participantAddr, nil)
			_, err := participantEthClient.redeem(txOpts, simnetID, secret, secretHash)
			if err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		originalBal, err := test.refunderClient.balance(ctx, &test.refunder.Address)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		txOpts = newTxOpts(ctx, &test.refunder.Address, nil)
		tx, err := test.refunderClient.refund(txOpts, simnetID, secretHash)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		// It appears the receipt is only accessable after the tx is mined.
		receipt, err := test.refunderClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err := test.refunderClient.balance(ctx, &test.refunder.Address)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
		wantBal := big.NewInt(0).Sub(originalBal, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, amt)
		}
		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("unexpected balance change for test %v: want %v got %v", test.name, wantBal, bal)
		}

		swap, err = test.refunderClient.swap(ctx, simnetAcct, secretHash)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		state = srveth.SwapState(swap.State)
		if state != test.finalState {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func TestReplayAttack(t *testing.T) {
	amt := big.NewInt(1e18)
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatal(err)
	}

	txOpts := newTxOpts(ctx, &simnetAddr, nil)
	err = ethClient.addSignerToOpts(txOpts, simnetID)
	if err != nil {
		t.Fatal(err)
	}

	// Deploy the reentry attack contract.
	_, _, reentryContract, err := reentryattack.DeployReentryAttack(txOpts, ethClient.ec)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}

	originalContractBal, err := ethClient.balance(ctx, &contractAddr)
	if err != nil {
		t.Fatal(err)
	}

	txOpts.Value = amt
	var secretHash [32]byte
	// Make four swaps that should be locked and refundable and one that is
	// soon refundable.
	for i := 0; i < 5; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash = sha256.Sum256(secret[:])

		if i != 4 {
			inLocktime := time.Now().Add(time.Hour).Unix()
			_, err = ethClient.initiate(txOpts, simnetID, inLocktime, secretHash, &participantAddr)
			if err != nil {
				t.Fatalf("unable to initiate swap: %v ", err)
			}

			if err := waitForMined(t, time.Second*10, false); err != nil {
				t.Fatal(err)
			}
			continue
		}

		inLocktime := time.Now().Add(-1 * time.Second).Unix()
		// Set some variables in the contract used for the exploit. This
		// will fail (silently) due to require(msg.origin == msg.sender)
		// in the real contract.
		_, err := reentryContract.SetUsUpTheBomb(txOpts, contractAddr, secretHash, big.NewInt(inLocktime), participantAddr)
		if err != nil {
			t.Fatalf("unable to set up the bomb: %v", err)
		}
		if err = waitForMined(t, time.Second*10, false); err != nil {
			t.Fatal(err)
		}
	}

	txOpts.Value = nil
	// Siphon funds into the contract.
	tx, err := reentryContract.AllYourBase(txOpts)
	if err != nil {
		t.Fatalf("unable to get all your base: %v", err)
	}
	spew.Dump(tx)
	if err = waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Fatalf("unable to get receipt: %v", err)
	}
	spew.Dump(receipt)

	if err = waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}

	originalAcctBal, err := ethClient.balance(ctx, &simnetAddr)
	if err != nil {
		t.Fatal(err)
	}

	// Send the siphoned funds to us.
	tx, err = reentryContract.AreBelongToUs(txOpts)
	if err != nil {
		t.Fatalf("unable to are belong to us: %v", err)
	}
	if err = waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err = ethClient.transactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Fatal(err)
	}
	txFee := big.NewInt(0).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
	wantBal := big.NewInt(0).Sub(originalAcctBal, txFee)

	bal, err := ethClient.balance(ctx, &simnetAddr)
	if err != nil {
		t.Fatal(err)
	}

	// If the exploit worked, the test will fail here, with 4 ether we
	// shouldn't be able to touch drained from the contract.
	if bal.Cmp(wantBal) != 0 {
		diff := big.NewInt(0).Sub(bal, wantBal)
		t.Fatalf("unexpected balance change of account: want %v got %v "+
			"or a difference of %v", wantBal, bal, diff)
	}

	// The exploit failed and status should be SSNone because initiation also
	// failed.
	swap, err := ethClient.swap(ctx, simnetAcct, secretHash)
	if err != nil {
		t.Fatal(err)
	}
	state := srveth.SwapState(swap.State)
	if state != srveth.SSNone {
		t.Fatalf("unexpected swap state: want %s got %s", srveth.SSNone, state)
	}

	// The contract should hold four more ether because initiation of one
	// swap failed.
	bal, err = ethClient.balance(ctx, &contractAddr)
	if err != nil {
		t.Fatal(err)
	}
	expectDiff := big.NewInt(0).Mul(big.NewInt(int64(4)), amt)
	wantBal = big.NewInt(0).Add(originalContractBal, expectDiff)
	if bal.Cmp(wantBal) != 0 {
		t.Fatalf("unexpected balance change of contract: want %v got %v", wantBal, bal)
	}
}

func TestGetCodeAt(t *testing.T) {
	byteCode, err := ethClient.getCodeAt(ctx, &contractAddr)
	if err != nil {
		t.Fatalf("Failed to get bytecode: %v", err)
	}
	c, err := hex.DecodeString(dexeth.ETHSwapRuntimeBin)
	if err != nil {
		t.Fatalf("Error decoding")
	}
	if !bytes.Equal(byteCode, c) {
		t.Fatal("Contract on chain does not match one in code")
	}
}

func TestSignMessage(t *testing.T) {
	msg := []byte("test message")
	err := ethClient.unlock(ctx, pw, simnetAcct)
	if err != nil {
		t.Fatalf("error unlocking account: %v", err)
	}
	signature, err := ethClient.signData(simnetAddr, msg)
	if err != nil {
		t.Fatalf("error signing text: %v", err)
	}
	pubKey, err := secp256k1.RecoverPubkey(crypto.Keccak256(msg), signature)
	x, y := elliptic.Unmarshal(secp256k1.S256(), pubKey)
	recoveredAddress := crypto.PubkeyToAddress(ecdsa.PublicKey{
		Curve: secp256k1.S256(),
		X:     x,
		Y:     y,
	})
	if !bytes.Equal(recoveredAddress.Bytes(), simnetAcct.Address.Bytes()) {
		t.Fatalf("recovered address: %v != simnet account address: %v", recoveredAddress, simnetAcct.Address)
	}
	if !secp256k1.VerifySignature(pubKey, crypto.Keccak256(msg), signature[:len(signature)-1]) {
		t.Fatalf("failed to verify signature")
	}
}
