//go:build harness && lgpl

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
	dexeth "decred.org/dcrdex/dex/networks/eth"
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
	participantAddr            = common.HexToAddress("1D4F2ee206474B136Af4868B887C7b166693c194")
	participantAcct            = &accounts.Account{Address: participantAddr}
	participantEthClient       *nodeClient
	ethSwapContractAddr        common.Address
	tokenSwapContractAddr      common.Address
	testTokenContractAddr      common.Address
	simnetID                   int64  = 42
	maxFeeRate                 uint64 = 200 // gwei per gas
	simnetContractor           contractor
	participantContractor      contractor
	simnetTokenContractor      tokenContractor
	participantTokenContractor tokenContractor
	ethGases                   = dexeth.VersionedGases[0]
	tokenGases                 = &dexeth.Tokens[testTokenID].NetTokens[dex.Simnet].SwapContracts[0].Gas
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
			txsa, err := ethClient.pendingTransactions()
			if err != nil {
				return fmt.Errorf("initiator pendingTransactions error: %v", err)
			}
			txsb, err := participantEthClient.pendingTransactions()
			if err != nil {
				return fmt.Errorf("participant pendingTransactions error: %v", err)
			}
			if len(txsa)+len(txsb) == 0 {
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

		// ETH swap contract.
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
		fmt.Printf("Token swap contract addr is %v\n", tokenSwapContractAddr)
		testTokenContractAddr, err = getContractAddrFromFile(testTokenContractAddrFile)
		if err != nil {
			return 1, err
		}
		fmt.Printf("Test token contract addr is %v\n", testTokenContractAddr)
		token := dexeth.Tokens[testTokenID].NetTokens[dex.Simnet]
		token.Address = testTokenContractAddr
		token.SwapContracts[0].Address = tokenSwapContractAddr

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

		fmt.Println("initiator address is", ethClient.address())

		participantEthClient, err = newNodeClient(getWalletDir(participantWalletDir, dex.Simnet), dex.Simnet, tLogger.SubLogger("participant"))
		if err != nil {
			return 1, fmt.Errorf("newNodeClient participant error: %v", err)
		}
		if err := participantEthClient.connect(ctx); err != nil {
			return 1, fmt.Errorf("connect error: %v\n", err)
		}
		defer participantEthClient.shutdown()

		fmt.Println("participant address is", participantEthClient.address())

		if err := syncClient(ethClient); err != nil {
			return 1, fmt.Errorf("error initializing initiator client: %v", err)
		}
		if err := syncClient(participantEthClient); err != nil {
			return 1, fmt.Errorf("error initializing participant client: %v", err)
		}

		if simnetContractor, err = newV0Contractor(dex.Simnet, simnetAddr, ethClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("newV0Contractor error: %w", err)
		}
		if participantContractor, err = newV0Contractor(dex.Simnet, participantAddr, participantEthClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("participant newV0Contractor error: %w", err)
		}

		if simnetTokenContractor, err = newV0TokenContractor(dex.Simnet, testTokenID, simnetAddr, ethClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("newV0TokenContractor error: %w", err)
		}

		if participantTokenContractor, err = newV0TokenContractor(dex.Simnet, testTokenID, participantAddr, participantEthClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("participant newV0TokenContractor error: %w", err)
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

		if err := ethClient.unlock(pw); err != nil {
			return 1, fmt.Errorf("error unlocking initiator client: %w", err)
		}
		if err := participantEthClient.unlock(pw); err != nil {
			return 1, fmt.Errorf("error unlocking initiator client: %w", err)
		}

		code := m.Run()

		if code != 0 {
			return code, nil
		}

		if err := ethClient.lock(); err != nil {
			return 1, fmt.Errorf("error locking initiator client: %w", err)
		}
		if err := participantEthClient.lock(); err != nil {
			return 1, fmt.Errorf("error locking initiator client: %w", err)
		}

		return code, nil

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

func prepareTokenClients(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatalf("initiator unlock error; %v", err)
	}
	txOpts, _ := ethClient.txOpts(ctx, 0, tokenGases.Approve, nil)
	var tx1, tx2 *types.Transaction
	if tx1, err = simnetTokenContractor.approve(txOpts, unlimitedAllowance); err != nil {
		t.Fatalf("initiator approveToken error: %v", err)
	}
	err = participantEthClient.unlock(pw)
	if err != nil {
		t.Fatalf("participant unlock error; %v", err)
	}

	txOpts, _ = participantEthClient.txOpts(ctx, 0, tokenGases.Approve, nil)
	if tx2, err = participantTokenContractor.approve(txOpts, unlimitedAllowance); err != nil {
		t.Fatalf("participant approveToken error: %v", err)
	}

	time.Sleep(1) // waitForMined only looks at alpha's tx pool. Give txs time to propagate.
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine approval block: %v", err)
	}

	receipt1, err := ethClient.transactionReceipt(ctx, tx1.Hash())
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt1)

	receipt2, err := participantEthClient.transactionReceipt(ctx, tx2.Hash())
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt2)
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
	t.Run("testBestHeader", testBestHeader)
	t.Run("testPendingTransactions", testPendingTransactions)
}

func TestPeering(t *testing.T) {
	t.Run("testAddPeer", testAddPeer)
	t.Run("testSyncProgress", testSyncProgress)
	t.Run("testGetCodeAt", testGetCodeAt)
}

func TestAccount(t *testing.T) {
	t.Run("testAddressBalance", testAddressBalance)
	t.Run("testSendTransaction", testSendTransaction)
	t.Run("testSendSignedTransaction", testSendSignedTransaction)
	t.Run("testTransactionReceipt", testTransactionReceipt)
	t.Run("testSignMessage", testSignMessage)
}

// TestContract tests methods that interact with the contract.
func TestContract(t *testing.T) {
	t.Run("testSwap", func(t *testing.T) { testSwap(t, BipID) })
	t.Run("testInitiate", func(t *testing.T) { testInitiate(t, BipID) })
	t.Run("testRedeem", func(t *testing.T) { testRedeem(t, BipID) })
	t.Run("testRefund", func(t *testing.T) { testRefund(t, BipID) })
}

func TestGas(t *testing.T) {
	t.Run("testInitiateGas", func(t *testing.T) { testInitiateGas(t, BipID) })
	t.Run("testRedeemGas", func(t *testing.T) { testRedeemGas(t, BipID) })
	t.Run("testRefundGas", func(t *testing.T) { testRefundGas(t, BipID) })
}

func TestTokenContract(t *testing.T) {
	t.Run("testTokenSwap", func(t *testing.T) { testSwap(t, testTokenID) })
	t.Run("testInitiateToken", func(t *testing.T) { testInitiate(t, testTokenID) })
	t.Run("testRedeemToken", func(t *testing.T) { testRedeem(t, testTokenID) })
	t.Run("testRefundToken", func(t *testing.T) { testRefund(t, testTokenID) })
	t.Run("testGetTokenAddress", testGetTokenAddress)

}

func TestTokenGas(t *testing.T) {
	t.Run("testTransferGas", testTransferGas)
	t.Run("testApproveGas", testApproveGas)
	t.Run("testInitiateTokenGas", func(t *testing.T) { testInitiateGas(t, testTokenID) })
	t.Run("testRedeemTokenGas", func(t *testing.T) { testRedeemGas(t, testTokenID) })
	t.Run("testRefundTokenGas", func(t *testing.T) { testRefundGas(t, testTokenID) })
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

func testBestHeader(t *testing.T) {
	bh, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(bh)
}

func testAddressBalance(t *testing.T) {
	bal, err := ethClient.addressBalance(ctx, simnetAddr)
	if err != nil {
		t.Fatal(err)
	}
	if bal == nil {
		t.Fatalf("empty balance")
	}
	spew.Dump(bal)
}

func testTokenBalance(t *testing.T) {
	bal, err := simnetTokenContractor.balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal == nil {
		t.Fatalf("empty balance")
	}
	spew.Dump(bal)
}

func testSendTransaction(t *testing.T) {
	// Checking confirmations for a random hash should result in not found error.
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	_, err := ethClient.transactionConfirmations(ctx, txHash)
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
	// Checking confirmations for a random hash should result in not found error.
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	_, err := ethClient.transactionConfirmations(ctx, txHash)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("no CoinNotFoundError")
	}

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
	tx, err = ethClient.creds.ks.SignTx(*simnetAcct, tx, ethClient.chainID)

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

func testSwap(t *testing.T, assetID uint32) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	swap, err := simnetContractor.swap(ctx, secretHash)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(swap)
}

func testSyncProgress(t *testing.T) {
	spew.Dump(ethClient.syncProgress())
}

func testInitiateGas(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	c := simnetContractor

	if assetID != BipID {
		c = simnetTokenContractor
	}

	gases := gases(assetID, 0, dex.Simnet)

	var previousGas uint64
	maxSwaps := 50
	for i := 1; i <= maxSwaps; i++ {
		gas, err := c.estimateInitGas(ctx, i)
		if err != nil {
			t.Fatalf("unexpected error from estimateInitGas(%d): %v", i, err)
		}

		var expectedGas uint64
		var actualGas uint64
		if i == 1 {
			expectedGas = gases.Swap
			actualGas = gas
		} else {
			expectedGas = gases.SwapAdd
			actualGas = gas - previousGas
		}
		if actualGas > expectedGas || actualGas < expectedGas*75/100 {
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

	minGasTipCapWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
	tip := new(big.Int).Set(minGasTipCapWei)

	return tip.Add(tip, hdr.BaseFee), nil
}

func testInitiate(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	isETH := assetID == BipID

	c := simnetContractor
	balance := func() (*big.Int, error) {
		return ethClient.addressBalance(ctx, simnetAddr)
	}
	gases := ethGases
	if !isETH {
		c = simnetTokenContractor
		balance = func() (*big.Int, error) {
			return simnetTokenContractor.balance(ctx)
		}
		gases = tokenGases
	}

	// Create a slice of random secret hashes that can be used in the tests and
	// make sure none of them have been used yet.
	numSecretHashes := 10
	secretHashes := make([][32]byte, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		copy(secretHashes[i][:], encode.RandomBytes(32))
		swap, err := c.swap(ctx, secretHashes[i])
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
		var originalParentBal *big.Int

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("balance error for asset %d, test %s: %v", assetID, test.name, err)
		}

		var totalVal uint64
		originalStates := make(map[string]dexeth.SwapStep)
		for _, tSwap := range test.swaps {
			swap, err := c.swap(ctx, bytesToArray(tSwap.SecretHash))
			if err != nil {
				t.Fatalf("%s: swap error: %v", test.name, err)
			}
			originalStates[tSwap.SecretHash.String()] = dexeth.SwapStep(swap.State)
			totalVal += tSwap.Value
		}

		optsVal := totalVal
		if !isETH {
			optsVal = 0
			originalParentBal, err = ethClient.addressBalance(ctx, simnetAddr)
			if err != nil {
				t.Fatalf("balance error for eth, test %s: %v", test.name, err)
			}
		}

		expGas := gases.SwapN(len(test.swaps))
		txOpts, _ := ethClient.txOpts(ctx, optsVal, expGas, dexeth.GweiToWei(maxFeeRate))
		tx, err := c.initiate(txOpts, test.swaps)
		if err != nil {
			if test.swapErr {
				continue
			}
			t.Fatalf("%s: initiate error: %v", test.name, err)
		}

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-initiate mining error: %v", test.name, err)
		}

		receipt, err := ethClient.checkTxStatus(ctx, tx, txOpts)
		if err != nil && test.success {
			spew.Dump(tx)
			spew.Dump(receipt)
			t.Fatalf("%s: failed transaction status: %v", test.name, err)
		} else if receipt.GasUsed > expGas {
			t.Fatalf("%s: gas used %d exceeds expected max %d", test.name, receipt.GasUsed, expGas)
		}

		fmt.Printf("Gas used for %d initiations, success = %t: %d (expected max %d) \n",
			len(test.swaps), test.success, receipt.GasUsed, expGas)

		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)

		wantBal := new(big.Int).Set(originalBal)
		if test.success {
			wantBal.Sub(wantBal, dexeth.GweiToWei(totalVal))
		}
		bal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		if isETH {
			wantBal = new(big.Int).Sub(wantBal, txFee)
		} else {
			parentBal, err := ethClient.addressBalance(ctx, simnetAddr)
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.CmpAbs(dexeth.GweiToWei(1)) >= 0 { // Ugh. Need to get to != 0 again.
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %.9f",
					test.name, dexeth.WeiToGwei(wantParentBal), dexeth.WeiToGwei(parentBal), float64(diff.Int64())/dexeth.GweiFactor)
			}
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.CmpAbs(new(big.Int)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d gwei, diff = %.9f gwei",
				test.name, dexeth.WeiToGwei(wantBal), dexeth.WeiToGwei(bal), float64(diff.Int64())/dexeth.GweiFactor)
		}

		for _, tSwap := range test.swaps {
			swap, err := c.swap(ctx, bytesToArray(tSwap.SecretHash))
			if err != nil {
				t.Fatalf("%s: swap error post-init: %v", test.name, err)
			}

			state := dexeth.SwapStep(swap.State)
			if test.success && state != dexeth.SSInitiated {
				t.Fatalf("%s: wrong success swap state: want %s got %s", test.name, dexeth.SSInitiated, state)
			}

			originalState := originalStates[hex.EncodeToString(tSwap.SecretHash[:])]
			if !test.success && state != originalState {
				t.Fatalf("%s: wrong error swap state: want %s got %s", test.name, originalState, state)
			}
		}
	}
}

func testRedeemGas(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	// Create secrets and secret hashes
	const numSwaps = 9
	secrets := make([][32]byte, 0, numSwaps)
	secretHashes := make([][32]byte, 0, numSwaps)
	for i := 0; i < numSwaps; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	// Initiate swaps
	now := uint64(time.Now().Unix())

	swaps := make([]*asset.Contract, 0, numSwaps)
	for i := 0; i < numSwaps; i++ {
		swaps = append(swaps, newContract(now, secretHashes[i], 1))
	}

	gases := ethGases
	c := simnetContractor
	pc := participantContractor
	optsVal := uint64(numSwaps)
	if assetID != BipID {
		optsVal = 0
		gases = tokenGases
		c = simnetTokenContractor
		pc = participantTokenContractor
	}

	txOpts, _ := ethClient.txOpts(ctx, optsVal, gases.SwapN(len(swaps)), dexeth.GweiToWei(maxFeeRate))
	tx, err := c.initiate(txOpts, swaps)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}
	_, err = ethClient.checkTxStatus(ctx, tx, txOpts)
	if err != nil {
		t.Fatalf("Init transaction failed: %v", err)
	}

	// Make sure swaps were properly initiated
	for i := range swaps {
		swap, err := c.swap(ctx, bytesToArray(swaps[i].SecretHash))
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		if swap.State != dexeth.SSInitiated {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, swap.State)
		}
	}

	// Test gas usage of redeem function
	var previous uint64
	for i := 0; i < numSwaps; i++ {
		gas, err := pc.estimateRedeemGas(ctx, secrets[:i+1])
		if err != nil {
			t.Fatalf("Error estimating gas for redeem function: %v", err)
		}

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

func testRedeem(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	lockTime := uint64(time.Now().Unix())
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

	isETH := assetID == BipID
	gases := ethGases
	c, pc := simnetContractor, participantContractor
	if !isETH {
		gases = tokenGases
		c, pc = simnetTokenContractor, participantTokenContractor
	}

	tests := []struct {
		name               string
		sleep              time.Duration
		redeemerClient     *nodeClient
		redeemer           *accounts.Account
		redeemerContractor contractor
		swaps              []*asset.Contract
		redemptions        []*asset.Redemption
		isRedeemable       []bool
		finalStates        []dexeth.SwapStep
		addAmt             bool
		expectRedeemErr    bool
	}{
		{
			name:               "ok before locktime",
			sleep:              time.Second * 8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[0], 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[0], secretHashes[0])},
			isRedeemable:       []bool{true},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "ok two before locktime",
			sleep:              time.Second * 8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
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
			name:               "ok after locktime",
			sleep:              time.Second * 16,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[3], 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[3], secretHashes[3])},
			isRedeemable:       []bool{true},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "bad redeemer",
			sleep:              time.Second * 8,
			redeemerClient:     ethClient,
			redeemer:           simnetAcct,
			redeemerContractor: c,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[4], 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[4], secretHashes[4])},
			isRedeemable:       []bool{false},
			finalStates:        []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:             false,
		},
		{
			name:               "bad secret",
			sleep:              time.Second * 8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[5], 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[6], secretHashes[5])},
			isRedeemable:       []bool{false},
			finalStates:        []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:             false,
		},
		{
			name:               "duplicate secret hashes",
			expectRedeemErr:    true,
			sleep:              time.Second * 8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
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
		var optsVal uint64
		for i, contract := range test.swaps {
			swap, err := c.swap(ctx, bytesToArray(test.swaps[i].SecretHash))
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			state := dexeth.SwapStep(swap.State)
			if state != dexeth.SSNone {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, state)
			}
			if isETH {
				optsVal += contract.Value
			}
		}

		balance := func() (*big.Int, error) {
			return test.redeemerClient.addressBalance(ctx, test.redeemer.Address)
		}
		if !isETH {
			balance = func() (*big.Int, error) {
				return test.redeemerContractor.(tokenContractor).balance(ctx)
			}
		}

		txOpts, _ := test.redeemerClient.txOpts(ctx, optsVal, gases.SwapN(len(test.swaps)), dexeth.GweiToWei(maxFeeRate))
		tx, err := test.redeemerContractor.initiate(txOpts, test.swaps)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, test.sleep, true); err != nil {
			t.Fatalf("%s: post-init mining error: %v", test.name, err)
		}

		receipt, err := test.redeemerClient.checkTxStatus(ctx, tx, txOpts)
		if err != nil {
			t.Fatalf("%s: failed init transaction status: %v", test.name, err)
		}
		spew.Dump(receipt)
		fmt.Printf("Gas used for %d inits: %d \n", len(test.swaps), receipt.GasUsed)

		for i := range test.swaps {
			swap, err := test.redeemerContractor.swap(ctx, bytesToArray(test.swaps[i].SecretHash))
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			if swap.State != dexeth.SSInitiated {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSInitiated, swap.State)
			}

			expected := test.isRedeemable[i]
			redemption := test.redemptions[i]

			isRedeemable, err := test.redeemerContractor.isRedeemable(bytesToArray(redemption.Spends.SecretHash), bytesToArray(redemption.Secret))
			if err != nil {
				t.Fatalf(`test "%v": error calling isRedeemable: %v`, test.name, err)
			} else if isRedeemable != expected {
				t.Fatalf(`test "%v": expected isRedeemable to be %v, but got %v. swap = %+v`, test.name, expected, isRedeemable, swap)
			}
		}

		var originalParentBal *big.Int
		if !isETH {
			originalParentBal, err = test.redeemerClient.addressBalance(ctx, test.redeemer.Address)
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
		}

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		expGas := gases.RedeemN(len(test.redemptions))
		txOpts, _ = test.redeemerClient.txOpts(ctx, 0, expGas, dexeth.GweiToWei(maxFeeRate))
		tx, err = test.redeemerContractor.redeem(txOpts, test.redemptions)
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

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-redeem mining error: %v", test.name, err)
		}

		expSuccess := !test.expectRedeemErr && test.addAmt
		receipt, err = test.redeemerClient.checkTxStatus(ctx, tx, txOpts)
		if err != nil && expSuccess {
			t.Fatalf("%s: failed redeem transaction status: %v", test.name, err)
		} else if receipt.GasUsed > expGas {
			t.Fatalf("%s: gas used %d is > expected max %d", test.name, receipt.GasUsed, expGas)
		}
		spew.Dump(receipt)
		fmt.Printf("Gas used for %d redeems, success = %t: %d \n", len(test.swaps), expSuccess, receipt.GasUsed)

		bal, err := balance()
		if err != nil {
			t.Fatalf("%s: redeemer balance error: %v", test.name, err)
		}

		// Check transaction parsing while we're here.
		if in, _, err := test.redeemerContractor.value(ctx, tx); err != nil {
			t.Fatalf("error parsing value from redemption: %v", err)
		} else if in != uint64(len(test.swaps)) {
			t.Fatalf("%s: unexpected pending in balance %d", test.name, in)
		}

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)
		wantBal := new(big.Int).Set(originalBal)

		if test.addAmt {
			wantBal.Add(wantBal, dexeth.GweiToWei(uint64(len(test.redemptions))))
		}

		if isETH {
			wantBal.Sub(wantBal, txFee)
		} else {
			parentBal, err := test.redeemerClient.addressBalance(ctx, test.redeemer.Address)
			if err != nil {
				t.Fatalf("%s: post-redeem eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.CmpAbs(dexeth.GweiToWei(1)) >= 0 {
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %d",
					test.name, dexeth.WeiToGwei(wantParentBal), dexeth.WeiToGwei(parentBal), dexeth.WeiToGwei(diff))
			}
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.CmpAbs(new(big.Int)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %.9f",
				test.name, dexeth.WeiToGwei(wantBal), dexeth.WeiToGwei(bal), float64(diff.Int64())/dexeth.GweiFactor)
		}

		for i, redemption := range test.redemptions {
			swap, err := c.swap(ctx, bytesToArray(redemption.Spends.SecretHash))
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

func testRefundGas(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	isETH := assetID == BipID

	c := simnetContractor
	gases := ethGases
	var optsVal uint64 = 1
	if !isETH {
		c = simnetTokenContractor
		gases = tokenGases
		optsVal = 0
	}

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	lockTime := uint64(time.Now().Unix())

	txOpts, _ := ethClient.txOpts(ctx, optsVal, gases.SwapN(1), nil)
	_, err := c.initiate(txOpts, []*asset.Contract{newContract(lockTime, secretHash, 1)})
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, time.Second*8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	state := dexeth.SwapStep(swap.State)
	if state != dexeth.SSInitiated {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, state)
	}

	gas, err := c.estimateRefundGas(ctx, secretHash)
	if err != nil {
		t.Fatalf("Error estimating gas for refund function: %v", err)
	}
	if isETH {
		expGas := gases.Refund
		if gas > expGas || gas < expGas*95/100 {
			t.Fatalf("expected refund gas to be near %d, but got %d",
				expGas, gas)
		}
	}
	fmt.Printf("Gas used for refund: %v \n", gas)
}

func testRefund(t *testing.T, assetID uint32) {
	prepareTokenClients(t)

	const amt = 1e9

	isETH := assetID == BipID

	gases := ethGases
	c, pc := simnetContractor, participantContractor
	if !isETH {
		gases = tokenGases
		c, pc = simnetTokenContractor, participantTokenContractor
	}

	tests := []struct {
		name                         string
		refunder                     *accounts.Account
		refunderClient               *nodeClient
		refunderContractor           contractor
		finalState                   dexeth.SwapStep
		addAmt, redeem, isRefundable bool
		addTime                      time.Duration
	}{{
		name:               "ok",
		isRefundable:       true,
		refunderClient:     ethClient,
		refunder:           simnetAcct,
		refunderContractor: c,
		addAmt:             true,
		finalState:         dexeth.SSRefunded,
	}, {
		name:               "before locktime",
		isRefundable:       false,
		refunderClient:     ethClient,
		refunder:           simnetAcct,
		refunderContractor: c,
		finalState:         dexeth.SSInitiated,
		addTime:            time.Hour,

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
		name:               "already redeemed",
		isRefundable:       false,
		refunderClient:     ethClient,
		refunder:           simnetAcct,
		refunderContractor: c,
		redeem:             true,
		finalState:         dexeth.SSRedeemed,
	}}

	for _, test := range tests {
		balance := func() (*big.Int, error) {
			return test.refunderClient.addressBalance(ctx, test.refunder.Address)
		}

		var optsVal uint64 = amt
		if !isETH {
			optsVal = 0
			balance = func() (*big.Int, error) {
				return test.refunderContractor.(tokenContractor).balance(ctx)
			}
		}

		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])

		swap, err := test.refunderContractor.swap(ctx, secretHash)
		if err != nil {
			t.Fatalf("%s: unable to get swap state pre-init", test.name)
		}
		if swap.State != dexeth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, swap.State)
		}

		inLocktime := uint64(time.Now().Add(test.addTime).Unix())

		txOpts, _ := ethClient.txOpts(ctx, optsVal, gases.SwapN(1), nil)
		_, err = c.initiate(txOpts, []*asset.Contract{newContract(inLocktime, secretHash, amt)})
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, time.Second*8, false); err != nil {
				t.Fatalf("%s: pre-redeem mining error: %v", test.name, err)
			}

			txOpts, _ = participantEthClient.txOpts(ctx, 0, gases.RedeemN(1), nil)
			_, err := pc.redeem(txOpts, []*asset.Redemption{newRedeem(secret, secretHash)})
			if err != nil {
				t.Fatalf("%s: redeem error: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, time.Second*8, false); err != nil {
			t.Fatalf("unexpected post-init mining error for test %v: %v", test.name, err)
		}

		var originalParentBal *big.Int
		if !isETH {
			originalParentBal, err = test.refunderClient.addressBalance(ctx, test.refunder.Address)
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
		}

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		isRefundable, err := test.refunderContractor.isRefundable(secretHash)
		if err != nil {
			t.Fatalf("%s: isRefundable error %v", test.name, err)
		}
		if isRefundable != test.isRefundable {
			t.Fatalf("%s: expected isRefundable=%v, but got %v",
				test.name, test.isRefundable, isRefundable)
		}

		txOpts, _ = test.refunderClient.txOpts(ctx, 0, gases.Refund, nil)
		tx, err := test.refunderContractor.refund(txOpts, secretHash)
		if err != nil {
			t.Fatalf("%s: refund error: %v", test.name, err)
		}
		spew.Dump(tx)

		in, _, err := test.refunderContractor.value(ctx, tx)

		if test.addAmt && in != amt {
			t.Fatalf("%s: unexpected pending in balance %d", test.name, in)
		}

		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("%s: post-refund mining error: %v", test.name, err)
		}

		receipt, err := test.refunderClient.checkTxStatus(ctx, tx, txOpts)
		if err != nil && test.addAmt {
			t.Fatalf("%s: failed redeem transaction status: %v", test.name, err)
		}
		spew.Dump(receipt)
		fmt.Printf("Gas used for refund, success = %t: %d \n", test.addAmt, receipt.GasUsed)

		// Balance should increase or decrease by a certain amount
		// depending on whether redeem completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)

		wantBal := new(big.Int).Set(originalBal)
		if test.addAmt {
			wantBal.Add(wantBal, dexeth.GweiToWei(amt))
		}

		if isETH {
			wantBal.Sub(wantBal, txFee)
		} else {
			parentBal, err := test.refunderClient.addressBalance(ctx, test.refunder.Address)
			if err != nil {
				t.Fatalf("%s: post-redeem eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.CmpAbs(dexeth.GweiToWei(1)) >= 0 {
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %d",
					test.name, dexeth.WeiToGwei(wantParentBal), dexeth.WeiToGwei(parentBal), dexeth.WeiToGwei(diff))
			}
		}

		bal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.CmpAbs(dexeth.GweiToWei(1)) >= 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, dexeth.WeiToGwei(wantBal), dexeth.WeiToGwei(bal), dexeth.WeiToGwei(diff))
		}

		swap, err = test.refunderContractor.swap(ctx, secretHash)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		if swap.State != test.finalState {
			t.Fatalf("%s: wrong swap state: want %s got %s", test.name, test.finalState, swap.State)
		}
	}
}

func testApproveAllowance(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForMined(t, time.Second*10, false); err != nil {
		t.Fatalf("post approve mining error: %v", err)
	}

	allowance, err := simnetTokenContractor.allowance(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if allowance.Cmp(new(big.Int)) == 0 {
		t.Fatalf("expected allowance > 0")
	}
}

func testGetTokenAddress(t *testing.T) {
	addr, err := simnetTokenContractor.tokenAddress(ctx)
	if err != nil {
		t.Fatalf("Error getting token address: %v", err)
	}
	if addr != testTokenContractAddr {
		t.Fatalf("Wrong contract retrieved from contract. wanted %s, got %s", testTokenContractAddr, addr)
	}
	fmt.Println("Token Address: ", addr)
}

func testTransferGas(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatalf("unlock error: %v", err)
	}
	gas, err := simnetTokenContractor.estimateTransferGas(ctx, dexeth.GweiToWei(1e8))
	if err != nil {
		t.Fatalf("estimateTransferGas error: %v", err)
	}
	fmt.Printf("=========== gas for transfer: %d ==============\n", gas)
}

func testApproveGas(t *testing.T) {
	gas, err := simnetTokenContractor.estimateApproveGas(ctx, dexeth.GweiToWei(1e8))
	if err != nil {
		t.Fatalf("")
	}
	fmt.Printf("=========== gas for approve: %d ==============\n", gas)
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

			txOpts, _ := ethClient.txOpts(ctx, 1, ethGases.SwapN(1), nil)
			_, err = simnetContractor.initiate(txOpts, []*asset.Contract{newContract(inLocktime, secretHash, 1)})
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
		txOpts, _ := ethClient.txOpts(ctx, 1, defaultSendGasLimit*5, nil)
		_, err := reentryContract.SetUsUpTheBomb(txOpts, ethSwapContractAddr, secretHash, big.NewInt(inLocktime), participantAddr)
		if err != nil {
			t.Fatalf("unable to set up the bomb: %v", err)
		}
		if err = waitForMined(t, time.Second*10, false); err != nil {
			t.Fatal(err)
		}
	}

	txOpts, _ = ethClient.txOpts(ctx, 1, defaultSendGasLimit*5, nil)
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

	originalAcctBal, err := ethClient.addressBalance(ctx, simnetAddr)
	if err != nil {
		t.Fatal(err)
	}

	// Send the siphoned funds to us.
	txOpts, _ = ethClient.txOpts(ctx, 1, defaultSendGasLimit*5, nil)
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
	wantBal := new(big.Int).Sub(originalAcctBal, txFee)

	acctBal, err := ethClient.addressBalance(ctx, simnetAddr)
	if err != nil {
		t.Fatal(err)
	}

	// If the exploit worked, the test will fail here, with 4 ether we
	// shouldn't be drained from the contract.
	delta := new(big.Int).Sub(originalAcctBal, acctBal)
	wantDelta := new(big.Int).Sub(originalAcctBal, wantBal)
	diff := new(big.Int).Abs(new(big.Int).Sub(wantDelta, delta))
	if dexeth.WeiToGwei(diff) > receipt.GasUsed { // See TestContract notes.
		delta := new(big.Int).Sub(originalAcctBal, acctBal)
		wantDelta := new(big.Int).Sub(originalAcctBal, wantBal)
		diff := new(big.Int).Sub(wantDelta, delta)
		t.Logf("unexpected balance change of account. original = %d, final = %d, expected %d",
			dexeth.WeiToGwei(originalAcctBal), dexeth.WeiToGwei(acctBal), dexeth.WeiToGwei(wantBal))
		t.Fatalf("actual change = %d, expected change = %d, a difference of %d",
			dexeth.WeiToGwei(delta), dexeth.WeiToGwei(wantDelta), dexeth.WeiToGwei(diff))
	}

	// The exploit failed and status should be SSNone because initiation also
	// failed.
	swap, err := simnetContractor.swap(ctx, secretHash)
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

func TestTokenGasEstimates(t *testing.T) {
	prepareTokenClients(t)
	if err := GetGasEstimates(ctx, ethClient, simnetTokenContractor, 5, tokenGases, func() {
		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("mining error: %v", err)
		}
	}); err != nil {
		t.Fatalf("getGasEstimates error: %v", err)
	}
}

func TestTokenGasEstimatesParticipant(t *testing.T) {
	prepareTokenClients(t)
	if err := GetGasEstimates(ctx, participantEthClient, participantTokenContractor, 5, tokenGases, func() {
		if err := waitForMined(t, time.Second*10, false); err != nil {
			t.Fatalf("mining error: %v", err)
		}
	}); err != nil {
		t.Fatalf("getGasEstimates error: %v", err)
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
