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
// to not be mined and effectively makes the node unusable (at least before
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
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexerc20 "decred.org/dcrdex/dex/networks/erc20"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	ethv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"decred.org/dcrdex/internal/eth/reentryattack"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	alphaNode = "enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304"
	alphaAddr = "18d65fb8d60c1199bb1ad381be47aa692b482605"
	pw        = "bee75192465cef9f8ab1198093dfed594e93ae810ccbbf2b3b12e1771cc6cb19"
)

var (
	homeDir                   = os.Getenv("HOME")
	ethSwapContractAddrFile   = filepath.Join(homeDir, "dextest", "eth", "eth_swap_contract_address.txt")
	tokenSwapContractAddrFile = filepath.Join(homeDir, "dextest", "eth", "erc20_swap_contract_address.txt")
	testTokenContractAddrFile = filepath.Join(homeDir, "dextest", "eth", "test_token_contract_address.txt")
	simnetWalletDir           = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "simnet")
	participantWalletDir      = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "participant")
	alphaNodeDir              = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	ctx                       context.Context
	tLogger                   = dex.StdOutLogger("ETHTEST", dex.LevelCritical)
	simnetWalletSeed          = "5c52e2ef5f5298ec41107e4e9573df4488577fb3504959cbc26c88437205dd2c0812f5244004217452059e2fd11603a511b5d0870ead753df76c966ce3c71531"
	// simnetPrivKey             = "8b19650a41e740f3b7ebb7ad112a91f63df9f005320489147cdf6f8d8f585500"
	simnetAddr            = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	simnetAcct            = &accounts.Account{Address: simnetAddr}
	ethClient             *nodeClient
	participantWalletSeed = "b99fb787fc5886eb539830d103c0017eff5241ace28ee137d40f135fd02212b1a897afbdcba037c8c735cc63080558a30d72851eb5a3d05684400ec4123a2d00"
	// participantPrivKey        = "4fc4f43c00bc6550314b8561878edbfc776884e006ad51a2fe2c054f85cfbd12"
	participantAddr       = common.HexToAddress("1D4F2ee206474B136Af4868B887C7b166693c194")
	participantAcct       = &accounts.Account{Address: participantAddr}
	participantEthClient  *nodeClient
	ethSwapContractAddr   common.Address
	tokenSwapContractAddr common.Address
	testTokenContractAddr common.Address
	simnetID              int64  = 42
	maxFeeRate            uint64 = 1000 // gwei per gas
)

func newContract(stamp uint64, secretHash [32]byte, val uint64) *asset.Contract {
	return &asset.Contract{
		LockTime:   stamp,
		SecretHash: secretHash[:],
		Address:    participantAddr.String(),
		Value:      val,
	}
}

func newRedeem(secret, secretHash [32]byte) *asset.Redemption {
	return &asset.Redemption{
		Spends: &asset.AuditInfo{
			SecretHash: secretHash[:],
		},
		Secret: secret[:],
	}
}

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
			txs, err := ethClient.pendingTransactions()
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

func getContractAddrFromFile(fileName string) (common.Address, error) {
	addrBytes, err := os.ReadFile(fileName)
	if err != nil {
		return common.Address{}, fmt.Errorf("error reading contract address: %v", err)
	}
	addrLen := len(addrBytes)
	if addrLen == 0 {
		return common.Address{}, fmt.Errorf("no contract address found at %v", fileName)
	}
	addrStr := string(addrBytes[:addrLen-1])
	address := common.HexToAddress(addrStr)
	return address, nil
}

func TestMain(m *testing.M) {
	// Run in function so that defers happen before os.Exit is called.
	run := func() (int, error) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		// Create dir if none yet exists. This persists for the life of the
		// testing harness.
		err := os.MkdirAll(simnetWalletDir, 0755)
		if err != nil {
			return 1, fmt.Errorf("error creating simnet wallet dir dir: %v", err)
		}
		err = os.MkdirAll(participantWalletDir, 0755)
		if err != nil {
			return 1, fmt.Errorf("error creating participant wallet dir: %v", err)
		}
		ethSwapContractAddr, err = getContractAddrFromFile(ethSwapContractAddrFile)
		if err != nil {
			return 1, err
		}
		dexeth.ContractAddresses[0][dex.Simnet] = ethSwapContractAddr
		fmt.Printf("ETH swap contract address is %v\n", ethSwapContractAddr)
		tokenSwapContractAddr, err = getContractAddrFromFile(tokenSwapContractAddrFile)
		if err != nil {
			return 1, err
		}
		dexerc20.ContractAddresses[0][dex.Simnet] = tokenSwapContractAddr
		fmt.Printf("Token swap contract addr is %v\n", tokenSwapContractAddr)
		testTokenContractAddr, err = getContractAddrFromFile(testTokenContractAddrFile)
		if err != nil {
			return 1, err
		}
		fmt.Printf("Test token contract addr is %v\n", testTokenContractAddr)
		err = setupWallet(simnetWalletDir, simnetWalletSeed, "localhost:30355")
		if err != nil {
			return 1, err
		}
		err = setupWallet(participantWalletDir, participantWalletSeed, "localhost:30356")
		if err != nil {
			return 1, err
		}
		ethClient, err = newNodeClient(getWalletDir(simnetWalletDir, dex.Simnet), dex.Simnet, tLogger.SubLogger("initiator"))
		if err != nil {
			return 1, fmt.Errorf("newNodeClient initiator error: %v", err)
		}
		if err := ethClient.connect(ctx); err != nil {
			return 1, fmt.Errorf("connect error: %v\n", err)
		}
		defer ethClient.shutdown()

		participantEthClient, err = newNodeClient(getWalletDir(participantWalletDir, dex.Simnet), dex.Simnet, tLogger.SubLogger("participant"))
		if err != nil {
			return 1, fmt.Errorf("newNodeClient participant error: %v", err)
		}
		if err := participantEthClient.connect(ctx); err != nil {
			return 1, fmt.Errorf("connect error: %v\n", err)
		}
		defer participantEthClient.shutdown()

		if err := syncClient(ethClient); err != nil {
			return 1, fmt.Errorf("error initializing initiator client: %v", err)
		}
		if err := syncClient(participantEthClient); err != nil {
			return 1, fmt.Errorf("error initializing participant client: %v", err)
		}

		accts, err := exportAccountsFromNode(ethClient.node)
		if err != nil {
			return 1, err
		}
		if len(accts) != 1 {
			return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
		}
		accts, err = exportAccountsFromNode(participantEthClient.node)
		if err != nil {
			return 1, err
		}
		if len(accts) != 1 {
			return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
		}
		return m.Run(), nil
	}
	exitCode, err := run()
	if err != nil {
		fmt.Println(err)
	}
	os.Exit(exitCode)
}

func setupWallet(walletDir, seed, listenAddress string) error {
	settings := map[string]string{
		"nodelistenaddr": listenAddress,
	}
	seedB, _ := hex.DecodeString(seed)
	createWalletParams := asset.CreateWalletParams{
		Type:     walletTypeGeth,
		Seed:     seedB,
		Pass:     []byte(pw),
		Settings: settings,
		DataDir:  walletDir,
		Net:      dex.Simnet,
	}
	return CreateWallet(&createWalletParams)
}

func syncClient(cl *nodeClient) error {
	for i := 0; ; i++ {
		prog := cl.syncProgress()
		if prog.CurrentBlock >= prog.HighestBlock {
			return nil
		}
		if i == 60 {
			return fmt.Errorf("block count has not synced one minute")
		}
		time.Sleep(time.Second)
	}
}

func TestBasicRetrieval(t *testing.T) {
	t.Run("testBestBlockHash", testBestBlockHash)
	t.Run("testBestHeader", testBestHeader)
	t.Run("testBlock", testBlock)
	t.Run("testPendingTransactions", testPendingTransactions)
}

func TestPeering(t *testing.T) {
	t.Run("testAddPeer", testAddPeer)
	t.Run("testSyncProgress", testSyncProgress)
	t.Run("testGetCodeAt", testGetCodeAt)
}

func TestAccount(t *testing.T) {
	t.Run("testBalance", testBalance)
	t.Run("testUnlock", testUnlock)
	t.Run("testLock", testLock)
	t.Run("testSendTransaction", testSendTransaction)
	t.Run("testSendSignedTransaction", testSendSignedTransaction)
	t.Run("testTransactionReceipt", testTransactionReceipt)
	t.Run("testSignMessage", testSignMessage)
}

// TestContract tests methods that interact with the contract.
func TestContract(t *testing.T) {
	t.Run("testSwap", testSwap)
	t.Run("testInitiate", testInitiate)
	t.Run("testRedeem", testRedeem)
	t.Run("testRefund", testRefund)
}

func TestTokenContract(t *testing.T) {
	t.Run("testTokenSwap", testTokenSwap)
	t.Run("testInitiateToken", testInitiateToken)
	t.Run("testRedeemToken", testRedeemToken)
	t.Run("testRefundToken", testRefundToken)
}

func TestTokenAccess(t *testing.T) {
	t.Run("testTokenBalance", testTokenBalance)
	t.Run("testApproveAllowance", testApproveAllowance)
}

func testAddPeer(t *testing.T) {
	if err := ethClient.addPeer(alphaNode); err != nil {
		t.Fatal(err)
	}
}

func testBestBlockHash(t *testing.T) {
	bbh, err := ethClient.bestBlockHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bbh)
}

func testBestHeader(t *testing.T) {
	bh, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bh)
}

func testBlock(t *testing.T) {
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

func testBalance(t *testing.T) {
	bal, err := ethClient.balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal == nil {
		t.Fatalf("empty balance")
	}
	spew.Dump(bal)
}

func testUnlock(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
}

func testLock(t *testing.T) {
	err := ethClient.lock()
	if err != nil {
		t.Fatal(err)
	}
}

func testSendTransaction(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	// Checking confirmations for a random hash should result in not found error.
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	_, err = ethClient.transactionConfirmations(ctx, txHash)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("no CoinNotFoundError")
	}

	txOpts, _ := ethClient.txOpts(ctx, 1, defaultSendGasLimit, nil)

	tx, err := ethClient.sendTransaction(ctx, txOpts, participantAddr, nil)
	if err != nil {
		t.Fatal(err)
	}

	txHash = tx.Hash()

	confs, err := ethClient.transactionConfirmations(ctx, txHash)
	if err != nil {
		t.Fatalf("transactionConfirmations error: %v", err)
	}
	if confs != 0 {
		t.Fatalf("%d confs reported for unmined transaction", confs)
	}

	fees := new(big.Int).Mul(txOpts.GasFeeCap, new(big.Int).SetUint64(txOpts.GasLimit))

	bal, _ := ethClient.balance(ctx)

	if bal.PendingOut.Cmp(new(big.Int).Add(dexeth.GweiToWei(1), fees)) != 0 {
		t.Fatalf("pending out not showing")
	}

	spew.Dump(tx)
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}

	confs, err = ethClient.transactionConfirmations(ctx, txHash)
	if err != nil {
		t.Fatalf("transactionConfirmations error after mining: %v", err)
	}
	if confs == 0 {
		t.Fatalf("zero confs after mining")
	}
}

func testSendSignedTransaction(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	// Checking confirmations for a random hash should result in not found error.
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	_, err = ethClient.transactionConfirmations(ctx, txHash)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("no CoinNotFoundError")
	}

	ethClient.nonceSendMtx.Lock()
	defer ethClient.nonceSendMtx.Unlock()
	nonce, err := ethClient.leth.ApiBackend.GetPoolNonce(ctx, ethClient.creds.addr)
	if err != nil {
		t.Fatalf("error getting nonce: %v", err)
	}
	tx := types.NewTx(&types.DynamicFeeTx{
		To:        &simnetAddr,
		ChainID:   ethClient.chainID,
		Nonce:     nonce,
		Gas:       21000,
		GasFeeCap: dexeth.GweiToWei(300),
		GasTipCap: dexeth.GweiToWei(2),
		Value:     dexeth.GweiToWei(1),
		Data:      []byte{},
	})
	tx, err = ethClient.signTransaction(tx)

	err = ethClient.sendSignedTransaction(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}

	txHash = tx.Hash()

	confs, err := ethClient.transactionConfirmations(ctx, txHash)
	if err != nil {
		t.Fatalf("transactionConfirmations error: %v", err)
	}
	if confs != 0 {
		t.Fatalf("%d confs reported for unmined transaction", confs)
	}

	bal, _ := ethClient.balance(ctx)

	fee := uint64(21000 * 300) // gwei
	if bal.PendingOut.Cmp(dexeth.GweiToWei(1+fee)) != 0 {
		t.Fatalf("pending out not showing")
	}

	spew.Dump(tx)
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}

	confs, err = ethClient.transactionConfirmations(ctx, txHash)
	if err != nil {
		t.Fatalf("transactionConfirmations error after mining: %v", err)
	}
	if confs == 0 {
		t.Fatalf("zero confs after mining")
	}
}

func testTransactionReceipt(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	txOpts, _ := ethClient.txOpts(ctx, 1, dexeth.InitGas(1, 0), nil)

	tx, err := ethClient.sendTransaction(ctx, txOpts, simnetAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt)
}

func testPendingTransactions(t *testing.T) {
	txs, err := ethClient.pendingTransactions()
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(txs)
}

func testSwap(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	swap, err := ethClient.swap(ctx, secretHash, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(swap)
}

func testSyncProgress(t *testing.T) {
	spew.Dump(ethClient.syncProgress())
}

func TestInitiateGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	var previousGas uint64
	maxSwaps := 50
	for i := 1; i <= maxSwaps; i++ {
		gas, err := ethClient.estimateInitGas(ctx, i, 0)
		if err != nil {
			t.Fatalf("unexpected error from estimateInitGas(%d): %v", i, err)
		}

		gases := dexeth.VersionedGases[0]

		var expectedGas uint64
		var actualGas uint64
		if i == 1 {
			expectedGas = gases.Swap
			actualGas = gas
		} else {
			expectedGas = gases.SwapAdd
			actualGas = gas - previousGas
		}
		if actualGas > expectedGas || actualGas < expectedGas/100*95 {
			t.Fatalf("Expected incremental gas for %d initiations to be close to %d but got %d",
				i, expectedGas, actualGas)
		}

		fmt.Printf("Gas used for batch initiating %v swaps: %v. %v more than previous \n", i, gas, gas-previousGas)
		previousGas = gas
	}
}

// feesAtBlk calculates the gas fee at blkNum. This adds the base fee at blkNum
// to a minimum gas tip cap.
func feesAtBlk(ctx context.Context, n *nodeClient, blkNum int64) (fees *big.Int, err error) {
	hdr, err := n.leth.ApiBackend.HeaderByNumber(ctx, rpc.BlockNumber(blkNum))
	if err != nil {
		return nil, err
	}
	tip := new(big.Int).Set(minGasTipCap)

	return tip.Add(tip, hdr.BaseFee), nil
}

func testInitiate(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	// Create a slice of random secret hashes that can be used in the tests and
	// make sure none of them have been used yet.
	numSecretHashes := 10
	secretHashes := make([][32]byte, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		copy(secretHashes[i][:], encode.RandomBytes(32))
		swap, err := ethClient.swap(ctx, secretHashes[i], 0)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := dexeth.SwapStep(swap.State)
		if state != dexeth.SSNone {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSNone, state)
		}
	}

	now := uint64(time.Now().Unix())

	tests := []struct {
		name    string
		swaps   []*asset.Contract
		success bool
		swapErr bool
	}{
		{
			name:    "1 swap ok",
			success: true,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[0], 2),
			},
		},
		{
			name:    "1 swap with existing hash",
			success: false,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[0], 1),
			},
		},
		{
			name:    "2 swaps ok",
			success: true,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[1], 1),
				newContract(now, secretHashes[2], 1),
			},
		},
		{
			name:    "2 swaps repeated hash",
			success: false,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[3], 1),
				newContract(now, secretHashes[3], 1),
			},
			swapErr: true,
		},
		{
			name:    "1 swap nil refundtimestamp",
			success: false,
			swaps: []*asset.Contract{
				newContract(0, secretHashes[4], 1),
			},
		},
		// {
		// 	// Preventing this used to need explicit checks before solidity 0.8, but now the
		// 	// compiler checks for integer overflows by default.
		// 	name:    "integer overflow attack",
		// 	success: false,
		// 	txValue: 999,
		// 	swaps: []*asset.Contract{
		// 		newContract(now, secretHashes[5], maxInt),
		// 		newContract(now, secretHashes[6], 1000),
		// 	},
		// },
		{
			name:    "swap with 0 value",
			success: false,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[5], 0),
				newContract(now, secretHashes[6], 1000),
			},
		},
	}

	for _, test := range tests {
		originalBal, err := ethClient.balance(ctx)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		originalStates := make(map[string]dexeth.SwapStep)
		for _, testSwap := range test.swaps {
			swap, err := ethClient.swap(ctx, bytesToArray(testSwap.SecretHash), 0)
			if err != nil {
				t.Fatalf("%s: swap error: %v", test.name, err)
			}
			originalStates[testSwap.SecretHash.String()] = dexeth.SwapStep(swap.State)
		}

		tx, err := ethClient.initiate(ctx, test.swaps, maxFeeRate, 0)
		if err != nil {
			if test.swapErr {
				continue
			}
			t.Fatalf("%s: initiate error: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-initiate mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump("receipt", receipt)

		// Balance should be reduced by a certain amount depending on
		// whether initiate completed successfully on-chain. If
		// unsuccessful the fee is subtracted. If successful, amt is
		// also subtracted.
		bal, err := ethClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)
		wantBal := new(big.Int).Sub(originalBal.Current, txFee)
		if test.success {
			for _, swap := range test.swaps {
				wantBal.Sub(wantBal, dexeth.GweiToWei(swap.Value))
			}
		}

		diff := new(big.Int).Abs(new(big.Int).Sub(wantBal, bal.Current))
		if diff.Cmp(new(big.Int)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, wantBal, bal.Current, diff)
		}

		for _, testSwap := range test.swaps {
			swap, err := ethClient.swap(ctx, bytesToArray(testSwap.SecretHash), 0)
			if err != nil {
				t.Fatalf("%s: swap error post-init: %v", test.name, err)
			}

			state := dexeth.SwapStep(swap.State)
			if test.success && state != dexeth.SSInitiated {
				t.Fatalf("%s: wrong success swap state: want %s got %s", test.name, dexeth.SSInitiated, state)
			}

			originalState := originalStates[hex.EncodeToString(testSwap.SecretHash[:])]
			if !test.success && state != originalState {
				t.Fatalf("%s: wrong error swap state: want %s got %s", test.name, originalState, state)
			}
		}
	}
}

func TestRedeemGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	// Create secrets and secret hashes
	numSecrets := 7
	secrets := make([][32]byte, 0, numSecrets)
	secretHashes := make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	// Initiate swaps
	now := uint64(time.Now().Unix())

	swaps := make([]*asset.Contract, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		swaps = append(swaps, newContract(now, secretHashes[i], 1))
	}
	_, err = ethClient.initiate(ctx, swaps, maxFeeRate, 0)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	// Make sure swaps were properly initiated
	for i := range swaps {
		swap, err := ethClient.swap(ctx, bytesToArray(swaps[i].SecretHash), 0)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		if swap.State != dexeth.SSInitiated {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, swap.State)
		}
	}

	// Test gas usage of redeem function
	var previous uint64
	for i := 0; i < numSecrets; i++ {
		gas, err := participantEthClient.estimateRedeemGas(ctx, secrets[:i+1], 0)
		if err != nil {
			t.Fatalf("Error estimating gas for redeem function: %v", err)
		}

		gases := dexeth.VersionedGases[0]

		var expectedGas uint64
		var actualGas uint64
		if i == 0 {
			expectedGas = gases.Redeem
			actualGas = gas
		} else {
			expectedGas = gases.RedeemAdd
			actualGas = gas - previous
		}
		if actualGas > expectedGas || actualGas < (expectedGas/100*95) {
			t.Fatalf("Expected incremental gas for %d redemptions to be close to %d but got %d",
				i, expectedGas, actualGas)
		}
		fmt.Printf("\n\nGas used to redeem %d swaps: %d -- %d more than previous \n\n", i+1, gas, gas-previous)
		previous = gas
	}
}

func testRedeem(t *testing.T) {
	lockTime := uint64(time.Now().Add(time.Second * 12).Unix())
	numSecrets := 10
	secrets := make([][32]byte, 0, numSecrets)
	secretHashes := make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
	err = participantEthClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		sleep           time.Duration
		redeemerClient  *nodeClient
		redeemer        *accounts.Account
		swaps           []*asset.Contract
		redemptions     []*asset.Redemption
		isRedeemable    []bool
		finalStates     []dexeth.SwapStep
		addAmt          bool
		expectRedeemErr bool
	}{
		{
			name:           "ok before locktime",
			sleep:          time.Second * 8,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps:          []*asset.Contract{newContract(lockTime, secretHashes[0], 1)},
			redemptions:    []*asset.Redemption{newRedeem(secrets[0], secretHashes[0])},
			isRedeemable:   []bool{true},
			finalStates:    []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:         true,
		},
		{
			name:           "ok two before locktime",
			sleep:          time.Second * 8,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps: []*asset.Contract{
				newContract(lockTime, secretHashes[1], 1),
				newContract(lockTime, secretHashes[2], 1),
			},
			redemptions: []*asset.Redemption{
				newRedeem(secrets[1], secretHashes[1]),
				newRedeem(secrets[2], secretHashes[2]),
			},
			isRedeemable: []bool{true, true},
			finalStates: []dexeth.SwapStep{
				dexeth.SSRedeemed, dexeth.SSRedeemed,
			},
			addAmt: true,
		},
		{
			name:           "ok after locktime",
			sleep:          time.Second * 16,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps:          []*asset.Contract{newContract(lockTime, secretHashes[3], 1)},
			redemptions:    []*asset.Redemption{newRedeem(secrets[3], secretHashes[3])},
			isRedeemable:   []bool{true},
			finalStates:    []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:         true,
		},
		{
			name:           "bad redeemer",
			sleep:          time.Second * 8,
			redeemerClient: ethClient,
			redeemer:       simnetAcct,
			swaps:          []*asset.Contract{newContract(lockTime, secretHashes[4], 1)},
			redemptions:    []*asset.Redemption{newRedeem(secrets[4], secretHashes[4])},
			isRedeemable:   []bool{false},
			finalStates:    []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:         false,
		},
		{
			name:           "bad secret",
			sleep:          time.Second * 8,
			redeemerClient: ethClient,
			redeemer:       simnetAcct,
			swaps:          []*asset.Contract{newContract(lockTime, secretHashes[5], 1)},
			redemptions:    []*asset.Redemption{newRedeem(secrets[6], secretHashes[5])},
			isRedeemable:   []bool{false},
			finalStates:    []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:         false,
		},
		{
			name:            "duplicate secret hashes",
			expectRedeemErr: true,
			sleep:           time.Second * 8,
			redeemerClient:  participantEthClient,
			redeemer:        participantAcct,
			swaps: []*asset.Contract{
				newContract(lockTime, secretHashes[7], 1),
				newContract(lockTime, secretHashes[8], 1),
			},
			redemptions: []*asset.Redemption{
				newRedeem(secrets[7], secretHashes[7]),
				newRedeem(secrets[7], secretHashes[7]),
			},
			isRedeemable: []bool{true, true},
			finalStates: []dexeth.SwapStep{
				dexeth.SSInitiated,
				dexeth.SSInitiated,
			},
			addAmt: false,
		},
	}

	for _, test := range tests {
		for i := range test.swaps {
			swap, err := ethClient.swap(ctx, bytesToArray(test.swaps[i].SecretHash), 0)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			state := dexeth.SwapStep(swap.State)
			if state != dexeth.SSNone {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, state)
			}
		}

		_, err = ethClient.initiate(ctx, test.swaps, maxFeeRate, 0)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("%s: post-init mining error: %v", test.name, err)
		}

		for i := range test.swaps {
			swap, err := ethClient.swap(ctx, bytesToArray(test.swaps[i].SecretHash), 0)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			state := dexeth.SwapStep(swap.State)
			if state != dexeth.SSInitiated {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSInitiated, state)
			}
		}

		originalBal, err := test.redeemerClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}
		for i, redemption := range test.redemptions {
			expected := test.isRedeemable[i]
			isRedeemable, err := test.redeemerClient.isRedeemable(bytesToArray(redemption.Spends.SecretHash), bytesToArray(redemption.Secret), 0)
			if err != nil {
				t.Fatalf(`test "%v": error calling isRedeemable: %v`, test.name, err)
			}
			if isRedeemable != expected {
				t.Fatalf(`test "%v": expected isRedeemable to be %v, but got %v`, test.name, expected, isRedeemable)
			}
		}

		tx, err := test.redeemerClient.redeem(ctx, test.redemptions, maxFeeRate, 0)
		if test.expectRedeemErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: redeem error: %v", test.name, err)
		}
		spew.Dump(tx)

		bal, err := test.redeemerClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: redeemer pending in balance error: %v", test.name, err)
		}

		if test.addAmt && dexeth.WeiToGwei(bal.PendingIn) != uint64(len(test.swaps)) {
			t.Fatalf("%s: unexpected pending in balance %s", test.name, bal.PendingIn)
		}

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-redeem mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := test.redeemerClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err = test.redeemerClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: redeemer balance error: %v", test.name, err)
		}

		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)
		wantBal := new(big.Int).Sub(originalBal.Current, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, dexeth.GweiToWei(uint64(len(test.redemptions))))
		}

		diff := new(big.Int).Abs(new(big.Int).Sub(wantBal, bal.Current))
		if diff.Cmp(new(big.Int)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, wantBal, bal.Current, diff)
		}

		for i, redemption := range test.redemptions {
			swap, err := ethClient.swap(ctx, bytesToArray(redemption.Spends.SecretHash), 0)
			if err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
			state := dexeth.SwapStep(swap.State)
			if state != test.finalStates[i] {
				t.Fatalf("unexpected swap state for test %v [%d]: want %s got %s",
					test.name, i, test.finalStates[i], state)
			}
		}
	}
}

func TestRefundGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	lockTime := uint64(time.Now().Unix())
	_, err = ethClient.initiate(ctx, []*asset.Contract{newContract(lockTime, secretHash, 1)}, maxFeeRate, 0)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	swap, err := ethClient.swap(ctx, secretHash, 0)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	state := dexeth.SwapStep(swap.State)
	if state != dexeth.SSInitiated {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, state)
	}

	gas, err := ethClient.estimateRefundGas(ctx, secretHash, 0)
	if err != nil {
		t.Fatalf("Error estimating gas for refund function: %v", err)
	}
	expGas := dexeth.RefundGas(0)
	if gas > expGas || gas < expGas/100*95 {
		t.Fatalf("expected refund gas to be near %d, but got %d",
			expGas, gas)
	}
	fmt.Printf("Gas used for refund: %v \n", gas)
}

func testRefund(t *testing.T) {
	const amt = 1e9
	locktime := time.Second * 12
	tests := []struct {
		name                                 string
		sleep                                time.Duration
		refunder                             *accounts.Account
		refunderClient                       *nodeClient
		finalState                           dexeth.SwapStep
		addAmt, addFee, redeem, isRefundable bool
	}{{
		name:           "ok",
		sleep:          time.Second * 16,
		isRefundable:   true,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		addAmt:         true,
		addFee:         true,
		finalState:     dexeth.SSRefunded,
	}, {
		name:           "before locktime",
		sleep:          time.Second * 8,
		isRefundable:   false,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		addFee:         true,
		finalState:     dexeth.SSInitiated,

		// NOTE: Refunding to an account other than the sender takes more
		// gas. At present redeem gas must be set to around 46000 although
		// it will only use about 43100. Set in dex/networks/eth/params.go
		// to test.
		// }, {
		// 	name:           "ok non initiator refunder",
		// 	sleep:          time.Second * 16,
		// 	isRefundable:   true,
		// 	refunderClient: participantEthClient,
		// 	refunder:       participantAcct,
		// 	addAmt:         true,
		// 	finalState:     dexeth.SSRefunded,
	}, {
		name:           "already redeemed",
		sleep:          time.Second * 16,
		isRefundable:   false,
		refunderClient: ethClient,
		refunder:       simnetAcct,
		addFee:         true,
		redeem:         true,
		finalState:     dexeth.SSRedeemed,
	}}

	for _, test := range tests {
		err := ethClient.unlock(pw)
		if err != nil {
			t.Fatal(err)
		}
		err = participantEthClient.unlock(pw)
		if err != nil {
			t.Fatal(err)
		}
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.swap(ctx, secretHash, 0)
		if err != nil {
			t.Fatalf("%s: unable to get swap state pre-init", test.name)
		}
		state := dexeth.SwapStep(swap.State)
		if state != dexeth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, state)
		}

		inLocktime := uint64(time.Now().Add(locktime).Unix())

		_, err = ethClient.initiate(ctx, []*asset.Contract{newContract(inLocktime, secretHash, amt)}, maxFeeRate, 0)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, time.Second*8, false); err != nil {
				t.Fatalf("%s: pre-redeem mining error: %v", test.name, err)
			}
			_, err := participantEthClient.redeem(ctx, []*asset.Redemption{newRedeem(secret, secretHash)}, maxFeeRate, 0)
			if err != nil {
				t.Fatalf("%s: redeem error: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpected post-init mining error for test %v: %v", test.name, err)
		}

		originalBal, err := ethClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		isRefundable, err := test.refunderClient.isRefundable(secretHash, 0)
		if err != nil {
			t.Fatalf("%s: isRefundable error %v", test.name, err)
		}
		if isRefundable != test.isRefundable {
			t.Fatalf("%s: expected isRefundable=%v, but got %v",
				test.name, test.isRefundable, isRefundable)
		}

		tx, err := test.refunderClient.refund(ctx, secretHash, maxFeeRate, 0)
		if err != nil {
			t.Fatalf("%s: refund error: %v", test.name, err)
		}
		spew.Dump(tx)

		bal, err := ethClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		if test.addFee && dexeth.WeiToGwei(bal.PendingIn) != amt {
			t.Fatalf("%s: unexpected pending in balance %s", test.name, bal.PendingIn)
		}

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-refund mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err = ethClient.balance(ctx)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}
		swap, err = ethClient.swap(ctx, secretHash, 0)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		txFee := big.NewInt(0)
		if test.addFee {
			bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
			txFee = new(big.Int).Mul(bigGasUsed, gasPrice)
		}
		wantBal := new(big.Int).Sub(originalBal.Current, txFee)
		if test.addAmt {
			wantBal.Add(wantBal, dexeth.GweiToWei(amt))
		}

		diff := new(big.Int).Abs(new(big.Int).Sub(wantBal, bal.Current))
		if diff.Cmp(new(big.Int)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, wantBal, bal.Current, diff)
		}

		swap, err = ethClient.swap(ctx, secretHash, 0)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		state = dexeth.SwapStep(swap.State)
		if state != test.finalState {
			t.Fatalf("%s: wrong swap state: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func testTokenBalance(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	bal, err := ethClient.tokenBalance(ctx, testTokenContractAddr)
	if err != nil {
		t.Fatal(err)
	}

	if bal == nil {
		t.Fatalf("empty balance")
	}
	spew.Dump(bal)
}

func testApproveAllowance(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	expectedAllowance := big.NewInt(1000)

	_, err = ethClient.approveToken(ctx, testTokenContractAddr, expectedAllowance, maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatalf("post approve mining error: %v", err)
	}

	allowance, err := ethClient.tokenAllowance(ctx, testTokenContractAddr)
	if err != nil {
		t.Fatal(err)
	}

	if expectedAllowance.Cmp(allowance) != 0 {
		t.Fatalf("expected allowance %v != actual %v", expectedAllowance, allowance)
	}
}

func TestInitiateTokenGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(1000), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatalf("post approve mining error: %v", err)
	}

	var previousGas uint64
	maxSwaps := 50
	for i := 1; i <= maxSwaps; i++ {
		gas, err := ethClient.estimateInitTokenGas(ctx, testTokenContractAddr, i)
		if err != nil {
			t.Fatalf("unexpected error from estimateInitGas(%d): %v", i, err)
		}

		fmt.Printf("Gas used for batch initiating %v swaps: %v. %v more than previous \n", i, gas, gas-previousGas)
		previousGas = gas
	}
}

func newTokenInitiation(stamp int64, secretHash [32]byte, val int64) ethv0.ETHSwapInitiation {
	return ethv0.ETHSwapInitiation{
		RefundTimestamp: big.NewInt(stamp),
		SecretHash:      secretHash,
		Participant:     participantAddr,
		Value:           big.NewInt(val),
	}
}

func newTokenRedeem(secret, secretHash [32]byte) ethv0.ETHSwapRedemption {
	return ethv0.ETHSwapRedemption{
		SecretHash: secretHash,
		Secret:     secret,
	}
}

func testTokenSwap(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	swap, err := ethClient.tokenSwap(ctx, secretHash)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(swap)
}

func TestGetTokenAddress(t *testing.T) {
	addr, err := ethClient.getTokenAddress(ctx)
	if err != nil {
		t.Fatalf("Error getting token address: %v", err)
	}

	fmt.Printf("FAHK U ~~ 0x%x\n", addr)
}

func testInitiateToken(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	// Create a slice of random secret hashes that can be used in the tests and
	// make sure none of them have been used yet.
	numSecretHashes := 10
	secretHashes := make([][32]byte, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		copy(secretHashes[i][:], encode.RandomBytes(32))
		swap, err := ethClient.tokenSwap(ctx, secretHashes[i])
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		state := dexeth.SwapStep(swap.State)
		if state != dexeth.SSNone {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSNone, state)
		}
	}

	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(1000), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatalf("post-approve mining error: %v", err)
	}

	now := time.Now().Unix()

	tests := []struct {
		name    string
		swaps   []ethv0.ETHSwapInitiation
		success bool
	}{
		{
			name:    "1 swap ok",
			success: true,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(now, secretHashes[0], 2),
			},
		},
		{
			name:    "1 swap with existing hash",
			success: false,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(now, secretHashes[0], 1),
			},
		},
		{
			name:    "2 swaps ok",
			success: true,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(now, secretHashes[1], 1),
				newTokenInitiation(now, secretHashes[2], 1),
			},
		},
		{
			name:    "1 swap nil refundtimestamp",
			success: false,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(0, secretHashes[4], 1),
			},
		},
		{
			name:    "swap with 0 value",
			success: false,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(now, secretHashes[5], 0),
				newTokenInitiation(now, secretHashes[6], 1000),
			},
		},
	}
	for _, test := range tests {
		originalBal, err := ethClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		originalStates := make(map[string]dexeth.SwapStep)
		for _, testSwap := range test.swaps {
			swap, err := ethClient.tokenSwap(ctx, testSwap.SecretHash)
			if err != nil {
				t.Fatalf("%s: swap error: %v", test.name, err)
			}
			originalStates[hex.EncodeToString(testSwap.SecretHash[:])] = dexeth.SwapStep(swap.State)
		}

		totalValue := new(big.Int)
		for _, swap := range test.swaps {
			totalValue.Add(totalValue, swap.Value)
		}

		tx, err := ethClient.initiateToken(ctx, test.swaps, testTokenContractAddr, maxFeeRate)
		if err != nil {
			t.Fatalf("%s: initiate error: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-initiate mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := ethClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump("receipt", receipt)

		wantBal := new(big.Int)
		wantBal.Set(originalBal)
		if test.success {
			wantBal.Sub(wantBal, totalValue)
		}

		afterBal, err := ethClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		if afterBal.Cmp(wantBal) != 0 {
			t.Fatalf("%s: expected balance %v but got %v", test.name, wantBal, afterBal)
		}

		for _, testSwap := range test.swaps {
			swap, err := ethClient.tokenSwap(ctx, testSwap.SecretHash)
			if err != nil {
				t.Fatalf("%s: swap error post-init: %v", test.name, err)
			}

			state := dexeth.SwapStep(swap.State)
			if test.success && state != dexeth.SSInitiated {
				t.Fatalf("%s: wrong success swap state: want %s got %s", test.name, dexeth.SSInitiated, state)
			}

			originalState := originalStates[hex.EncodeToString(testSwap.SecretHash[:])]
			if !test.success && state != originalState {
				t.Fatalf("%s: wrong error swap state: want %s got %s", test.name, originalState, state)
			}
		}
	}
}

func TestRedeemTokenGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(10000), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	// Create secrets and secret hashes
	numSecrets := 5
	secrets := make([][32]byte, 0, numSecrets)
	secretHashes := make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	// Initiate swaps
	now := time.Now().Unix()

	swaps := make([]ethv0.ETHSwapInitiation, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		swaps = append(swaps, newTokenInitiation(now, secretHashes[i], 1))
	}
	_, err = ethClient.initiateToken(ctx, swaps, testTokenContractAddr, maxFeeRate)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	// Make sure swaps were properly initiated
	for i := range swaps {
		swap, err := ethClient.tokenSwap(ctx, swaps[i].SecretHash)
		if err != nil {
			t.Fatal("unable to get swap state")
		}

		state := dexeth.SwapStep(swap.State)
		if state != dexeth.SSInitiated {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, state)
		}
	}

	// Test gas usage of redeem function
	var previous uint64
	for i := 0; i < numSecrets; i++ {
		gas, err := participantEthClient.estimateRedeemTokenGas(ctx, secrets[:i+1])
		if err != nil {
			t.Fatalf("Error estimating gas for redeem function: %v", err)
		}

		fmt.Printf("\n\nGas used to redeem %d swaps: %d -- %d more than previous \n\n", i+1, gas, gas-previous)
		previous = gas
	}
}

func testRedeemToken(t *testing.T) {
	lockTime := time.Now().Add(time.Second * 12).Unix()
	numSecrets := 10
	secrets := make([][32]byte, 0, numSecrets)
	secretHashes := make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
	err = participantEthClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(1e6), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		sleep           time.Duration
		redeemerClient  *nodeClient
		redeemer        *accounts.Account
		swaps           []ethv0.ETHSwapInitiation
		redemptions     []ethv0.ETHSwapRedemption
		isRedeemable    []bool
		finalStates     []dexeth.SwapStep
		addAmt          bool
		expectRedeemErr bool
	}{
		{
			name:           "ok before locktime",
			sleep:          time.Second * 8,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps:          []ethv0.ETHSwapInitiation{newTokenInitiation(lockTime, secretHashes[0], 1)},
			redemptions:    []ethv0.ETHSwapRedemption{newTokenRedeem(secrets[0], secretHashes[0])},
			isRedeemable:   []bool{true},
			finalStates:    []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:         true,
		},
		{
			name:           "ok two before locktime",
			sleep:          time.Second * 8,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(lockTime, secretHashes[1], 1),
				newTokenInitiation(lockTime, secretHashes[2], 1),
			},
			redemptions: []ethv0.ETHSwapRedemption{
				newTokenRedeem(secrets[1], secretHashes[1]),
				newTokenRedeem(secrets[2], secretHashes[2]),
			},
			isRedeemable: []bool{true, true},
			finalStates: []dexeth.SwapStep{
				dexeth.SSRedeemed, dexeth.SSRedeemed,
			},
			addAmt: true,
		},
		{
			name:           "ok after locktime",
			sleep:          time.Second * 16,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps:          []ethv0.ETHSwapInitiation{newTokenInitiation(lockTime, secretHashes[3], 1)},
			redemptions:    []ethv0.ETHSwapRedemption{newTokenRedeem(secrets[3], secretHashes[3])},
			isRedeemable:   []bool{true},
			finalStates:    []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:         true,
		},
		{
			name:           "bad redeemer",
			sleep:          time.Second * 8,
			redeemerClient: ethClient,
			redeemer:       simnetAcct,
			swaps:          []ethv0.ETHSwapInitiation{newTokenInitiation(lockTime, secretHashes[4], 1)},
			redemptions:    []ethv0.ETHSwapRedemption{newTokenRedeem(secrets[4], secretHashes[4])},
			isRedeemable:   []bool{false},
			finalStates:    []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:         false,
		},
		{
			name:           "bad secret",
			sleep:          time.Second * 8,
			redeemerClient: ethClient,
			redeemer:       simnetAcct,
			swaps:          []ethv0.ETHSwapInitiation{newTokenInitiation(lockTime, secretHashes[5], 1)},
			redemptions:    []ethv0.ETHSwapRedemption{newTokenRedeem(secrets[6], secretHashes[5])},
			isRedeemable:   []bool{false},
			finalStates:    []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:         false,
		},
		{
			name:           "duplicate secret hashes",
			sleep:          time.Second * 8,
			redeemerClient: participantEthClient,
			redeemer:       participantAcct,
			swaps: []ethv0.ETHSwapInitiation{
				newTokenInitiation(lockTime, secretHashes[7], 1),
				newTokenInitiation(lockTime, secretHashes[8], 1),
			},
			redemptions: []ethv0.ETHSwapRedemption{
				newTokenRedeem(secrets[7], secretHashes[7]),
				newTokenRedeem(secrets[7], secretHashes[7]),
			},
			isRedeemable: []bool{true, true},
			finalStates: []dexeth.SwapStep{
				dexeth.SSInitiated,
				dexeth.SSInitiated,
			},
			addAmt: false,
		},
	}

	for _, test := range tests {
		for i := range test.swaps {
			swap, err := ethClient.tokenSwap(ctx, test.swaps[i].SecretHash)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			state := dexeth.SwapStep(swap.State)
			if state != dexeth.SSNone {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, state)
			}
		}

		_, err = ethClient.initiateToken(ctx, test.swaps, testTokenContractAddr, maxFeeRate)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("%s: post-init mining error: %v", test.name, err)
		}

		for i := range test.swaps {
			swap, err := ethClient.tokenSwap(ctx, test.swaps[i].SecretHash)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			state := dexeth.SwapStep(swap.State)
			if state != dexeth.SSInitiated {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSInitiated, state)
			}
		}

		originalBal, err := test.redeemerClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		for i, redemption := range test.redemptions {
			expected := test.isRedeemable[i]
			isRedeemable, err := test.redeemerClient.tokenIsRedeemable(ctx, redemption.SecretHash, redemption.Secret)
			if err != nil {
				t.Fatalf(`test "%v": error calling isRedeemable: %v`, test.name, err)
			}
			if isRedeemable != expected {
				t.Fatalf(`test "%v": expected isRedeemable to be %v, but got %v`, test.name, expected, isRedeemable)
			}
		}

		tx, err := test.redeemerClient.redeemToken(ctx, test.redemptions, maxFeeRate)
		if err != nil {
			t.Fatalf("%s: redeem error: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-redeem mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := test.redeemerClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump(receipt)

		wantBal := new(big.Int)
		wantBal.Set(originalBal)
		if test.addAmt {
			wantBal.Add(wantBal, big.NewInt(int64(len(test.redemptions))))
		}

		afterBal, err := test.redeemerClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

		if afterBal.Cmp(wantBal) != 0 {
			t.Fatalf("%s: expected balance %v but got %v", test.name, wantBal, afterBal)
		}

		for i, redemption := range test.redemptions {
			swap, err := ethClient.tokenSwap(ctx, redemption.SecretHash)
			if err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
			state := dexeth.SwapStep(swap.State)
			if state != test.finalStates[i] {
				t.Fatalf("unexpected swap state for test %v [%d]: want %s got %s",
					test.name, i, test.finalStates[i], state)
			}
		}
	}
}
func testRefundToken(t *testing.T) {
	const amt = 1e5
	locktime := time.Second * 12
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
	err = participantEthClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(1e6), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                 string
		sleep                time.Duration
		refunderClient       *nodeClient
		finalState           dexeth.SwapStep
		redeem, isRefundable bool
	}{{
		name:           "ok",
		sleep:          time.Second * 16,
		refunderClient: ethClient,
		isRefundable:   true,
		finalState:     dexeth.SSRefunded,
	}, {
		name:           "before locktime",
		sleep:          time.Second * 8,
		refunderClient: ethClient,
		finalState:     dexeth.SSInitiated,
	}, {
		name:           "wrong refunder",
		sleep:          time.Second * 16,
		refunderClient: participantEthClient,
		finalState:     dexeth.SSInitiated,
	}, {
		name:           "already redeemed",
		sleep:          time.Second * 16,
		refunderClient: ethClient,
		redeem:         true,
		finalState:     dexeth.SSRedeemed,
	}}

	for _, test := range tests {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := ethClient.tokenSwap(ctx, secretHash)
		if err != nil {
			t.Fatalf("%s: unable to get swap state pre-init", test.name)
		}
		state := dexeth.SwapStep(swap.State)
		if state != dexeth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, state)
		}

		inLocktime := time.Now().Add(locktime).Unix()
		_, err = ethClient.initiateToken(ctx, []ethv0.ETHSwapInitiation{newTokenInitiation(inLocktime, secretHash, amt)}, testTokenContractAddr, maxFeeRate)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, time.Second*8, false); err != nil {
				t.Fatalf("%s: pre-redeem mining error: %v", test.name, err)
			}
			_, err := participantEthClient.redeemToken(ctx, []ethv0.ETHSwapRedemption{newTokenRedeem(secret, secretHash)}, maxFeeRate)
			if err != nil {
				t.Fatalf("%s: redeem error: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("unexpected post-init mining error for test %v: %v", test.name, err)
		}

		isRefundable, err := test.refunderClient.tokenIsRefundable(ctx, secretHash)
		if err != nil {
			t.Fatalf("%s: isRefundable error: %v", test.name, err)
		}
		if isRefundable != test.isRefundable {
			t.Fatalf("%s: expected isRefundable = %v", test.name, test.isRefundable)
		}

		originalBal, err := test.refunderClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		tx, err := test.refunderClient.refundToken(ctx, secretHash, maxFeeRate)
		if err != nil {
			t.Fatalf("%s: refund error: %v", test.name, err)
		}
		spew.Dump(tx)

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-refund mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := test.refunderClient.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			t.Fatalf("%s: receipt error: %v", test.name, err)
		}
		spew.Dump(receipt)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		bal, err := test.refunderClient.tokenBalance(ctx, testTokenContractAddr)
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}
		wantBal := new(big.Int)
		wantBal.Set(originalBal)
		if test.isRefundable {
			wantBal.Add(wantBal, big.NewInt(amt))
		}

		if bal.Cmp(wantBal) != 0 {
			t.Fatalf("%s: expected balance %v but got %v", test.name, wantBal, bal)
		}

		swap, err = test.refunderClient.tokenSwap(ctx, secretHash)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		state = dexeth.SwapStep(swap.State)
		if state != test.finalState {
			t.Fatalf("%s: wrong swap state: want %s got %s", test.name, test.finalState, state)
		}
	}
}

func TestRefundTokenGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	_, err = ethClient.approveToken(ctx, testTokenContractAddr, big.NewInt(1e6), maxFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	lockTime := time.Now().Unix()
	_, err = ethClient.initiateToken(ctx, []ethv0.ETHSwapInitiation{newTokenInitiation(lockTime, secretHash, 1)}, testTokenContractAddr, maxFeeRate)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	swap, err := ethClient.tokenSwap(ctx, secretHash)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	state := dexeth.SwapStep(swap.State)
	if state != dexeth.SSInitiated {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, state)
	}

	gas, err := ethClient.estimateRefundTokenGas(ctx, secretHash)
	if err != nil {
		t.Fatalf("Error estimating gas for refund function: %v", err)
	}

	fmt.Printf("Gas used for refund: %v \n", gas)
}

func TestReplayAttack(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	txOpts, err := ethClient.txOpts(ctx, 1, defaultSendGasLimit*5, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}

	err = ethClient.addSignerToOpts(txOpts)
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

	originalContractBal, err := ethClient.addressBalance(ctx, ethSwapContractAddr)
	if err != nil {
		t.Fatal(err)
	}

	var secretHash [32]byte
	// Make four swaps that should be locked and refundable and one that is
	// soon refundable.
	for i := 0; i < 5; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash = sha256.Sum256(secret[:])

		if i != 4 {
			inLocktime := uint64(time.Now().Add(time.Hour).Unix())
			_, err = ethClient.initiate(ctx, []*asset.Contract{newContract(inLocktime, secretHash, 1)}, maxFeeRate, 0)
			if err != nil {
				t.Fatalf("unable to initiate swap: %v ", err)
			}

			if err := waitForMined(t, time.Second*10, false); err != nil {
				t.Fatal(err)
			}
			continue
		}

		intermediateContractVal, _ := ethClient.addressBalance(ctx, ethSwapContractAddr)
		t.Logf("intermediate contract value %d", dexeth.WeiToGwei(intermediateContractVal))

		inLocktime := time.Now().Add(-1 * time.Second).Unix()
		// Set some variables in the contract used for the exploit. This
		// will fail (silently) due to require(msg.origin == msg.sender)
		// in the real contract.
		_, err := reentryContract.SetUsUpTheBomb(txOpts, ethSwapContractAddr, secretHash, big.NewInt(inLocktime), participantAddr)
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

	originalAcctBal, err := ethClient.balance(ctx)
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

	gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
	if err != nil {
		t.Fatalf("feesAtBlk error: %v", err)
	}
	bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
	txFee := new(big.Int).Mul(bigGasUsed, gasPrice)
	wantBal := new(big.Int).Sub(originalAcctBal.Current, txFee)

	acctBal, err := ethClient.balance(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// If the exploit worked, the test will fail here, with 4 ether we
	// shouldn't be drained from the contract.
	delta := new(big.Int).Sub(originalAcctBal.Current, acctBal.Current)
	wantDelta := new(big.Int).Sub(originalAcctBal.Current, wantBal)
	diff := new(big.Int).Abs(new(big.Int).Sub(wantDelta, delta))
	if dexeth.WeiToGwei(diff) > receipt.GasUsed { // See TestContract notes.
		delta := new(big.Int).Sub(originalAcctBal.Current, acctBal.Current)
		wantDelta := new(big.Int).Sub(originalAcctBal.Current, wantBal)
		diff := new(big.Int).Sub(wantDelta, delta)
		t.Logf("unexpected balance change of account. original = %d, final = %d, expected %d",
			dexeth.WeiToGwei(originalAcctBal.Current), dexeth.WeiToGwei(acctBal.Current), dexeth.WeiToGwei(wantBal))
		t.Fatalf("actual change = %d, expected change = %d, a difference of %d",
			dexeth.WeiToGwei(delta), dexeth.WeiToGwei(wantDelta), dexeth.WeiToGwei(diff))
	}

	// The exploit failed and status should be SSNone because initiation also
	// failed.
	swap, err := ethClient.swap(ctx, secretHash, 0)
	if err != nil {
		t.Fatal(err)
	}
	state := dexeth.SwapStep(swap.State)
	if state != dexeth.SSNone {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSNone, state)
	}

	// The contract should hold four more ether because initiation of one
	// swap failed.
	contractBal, err := ethClient.addressBalance(ctx, ethSwapContractAddr)
	if err != nil {
		t.Fatal(err)
	}

	wantBal = new(big.Int).Add(originalContractBal, dexeth.GweiToWei(4))
	balDiff := new(big.Int).Abs(new(big.Int).Sub(contractBal, wantBal))
	if dexeth.WeiToGwei(balDiff) > receipt.GasUsed {
		wantDiff := new(big.Int).Sub(originalContractBal, wantBal)
		actualDiff := new(big.Int).Sub(originalContractBal, contractBal)
		t.Logf("balance before = %d, expected balance after = %d, actual balance after = %d",
			dexeth.WeiToGwei(originalContractBal), dexeth.WeiToGwei(wantBal), dexeth.WeiToGwei(contractBal))
		t.Fatalf("wanted diff = %d, actual diff = %d, a difference of %d",
			dexeth.WeiToGwei(wantDiff), dexeth.WeiToGwei(actualDiff), dexeth.WeiToGwei(balDiff))
	}
}

func testGetCodeAt(t *testing.T) {
	byteCode, err := ethClient.getCodeAt(ctx, ethSwapContractAddr)
	if err != nil {
		t.Fatalf("Failed to get bytecode: %v", err)
	}
	c, err := hex.DecodeString(swapv0.ETHSwapRuntimeBin)
	if err != nil {
		t.Fatalf("Error decoding")
	}
	if !bytes.Equal(byteCode, c) {
		t.Fatal("Contract on chain does not match one in code")
	}
}

func testSignMessage(t *testing.T) {
	msg := []byte("test message")
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatalf("error unlocking account: %v", err)
	}
	sig, pubKey, err := ethClient.signData(msg)
	if err != nil {
		t.Fatalf("error signing text: %v", err)
	}
	x, y := elliptic.Unmarshal(secp256k1.S256(), pubKey)
	recoveredAddress := crypto.PubkeyToAddress(ecdsa.PublicKey{
		Curve: secp256k1.S256(),
		X:     x,
		Y:     y,
	})
	if !bytes.Equal(recoveredAddress.Bytes(), simnetAcct.Address.Bytes()) {
		t.Fatalf("recovered address: %v != simnet account address: %v", recoveredAddress, simnetAcct.Address)
	}
	if !crypto.VerifySignature(pubKey, crypto.Keccak256(msg), sig) {
		t.Fatalf("failed to verify signature")
	}
}

func bytesToArray(b []byte) (a [32]byte) {
	copy(a[:], b)
	return
}

func bigUint(v uint64) *big.Int {
	return new(big.Int).SetUint64(v)
}

// exportAccountsFromNode returns all the accounts for which a the ethereum wallet
// has stored a private key.
func exportAccountsFromNode(node *node.Node) ([]accounts.Account, error) {
	ks, err := exportKeyStoreFromNode(node)
	if err != nil {
		return nil, err
	}
	return ks.Accounts(), nil
}
