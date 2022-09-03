//go:build harness && lgpl

// This test requires that the simnet harness be running. Some tests will
// alternatively work on testnet.
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
// TODO: Running these tests many times on simnet eventually results in all
// transactions returning "unexpected error for test ok: exceeds block gas
// limit". Find out why that is.

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
	"os/signal"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"decred.org/dcrdex/dex/testing/eth/reentryattack"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	//	"encoding/binary"
	//	"github.com/decred/dcrd/dcrutil/v4"
	// 	"github.com/decred/dcrd/crypto/blake256"
)

const (
	alphaNode         = "enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304"
	alphaAddr         = "18d65fb8d60c1199bb1ad381be47aa692b482605"
	pw                = "bee75192465cef9f8ab1198093dfed594e93ae810ccbbf2b3b12e1771cc6cb19"
	maxFeeRate uint64 = 200 // gwei per gas

	// The following constants are related to testnet testing.

	// isTestnet can be set to true to perform tests on the goerli testnet.
	// May need some setup including sending testnet coins to the addresses
	// and a lengthy sync. Wallet addresses are the same as simnet. All
	// wait and lock times are multiplied by testnetSecPerBlock. Tests may
	// need to be run with a high --timeout=2h for the initial sync.
	//
	// Only for non-token tests, so run with --run=TestGroupName.
	//
	// TODO: Make this also work for token tests.
	isTestnet = false
	// testnetWalletSeed and testnetParticipantWalletSeed are required for
	// use on testnet and can be any 256 bit hex. If the wallets created by
	// these seeds do not have enough funds to test, addresses that need
	// funds will be printed.
	testnetWalletSeed            = ""
	testnetParticipantWalletSeed = ""
	// addPeer is optional and will be added if set. It should looks
	// something like enode://1565e5bc2...2c3300e9@127.0.0.1:30303
	// Be sure the full node is run with --light.serve ##
	addPeer = ""
)

var (
	homeDir                     = os.Getenv("HOME")
	harnessCtlDir               = filepath.Join(homeDir, "dextest", "eth", "harness-ctl")
	simnetWalletDir             = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "simnet")
	participantWalletDir        = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "participant")
	testnetWalletDir            = filepath.Join(homeDir, "ethtest", "testnet_contract_tests", "walletA")
	testnetParticipantWalletDir = filepath.Join(homeDir, "ethtest", "testnet_contract_tests", "walletB")
	alphaNodeDir                = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	ctx                         context.Context
	tLogger                     = dex.StdOutLogger("ETHTEST", dex.LevelCritical)
	simnetWalletSeed            = "0812f5244004217452059e2fd11603a511b5d0870ead753df76c966ce3c71531"
	simnetAddr                  common.Address
	simnetAcct                  *accounts.Account
	ethClient                   *nodeClient
	participantWalletSeed       = "a897afbdcba037c8c735cc63080558a30d72851eb5a3d05684400ec4123a2d00"
	participantAddr             common.Address
	participantAcct             *accounts.Account
	participantEthClient        *nodeClient
	ethSwapContractAddr         common.Address
	tokenSwapContractAddr       common.Address
	testTokenContractAddr       common.Address
	simnetID                    int64 = 42
	simnetContractor            contractor
	participantContractor       contractor
	simnetTokenContractor       tokenContractor
	participantTokenContractor  tokenContractor
	ethGases                    = dexeth.VersionedGases[0]
	tokenGases                  = &dexeth.Tokens[testTokenID].NetTokens[dex.Simnet].SwapContracts[0].Gas
	testnetSecPerBlock          = 15 * time.Second
	// secPerBlock is one for simnet, because it takes one second to mine a
	// block currently. Is set in code to testnetSecPerBlock if runing on
	// testnet.
	secPerBlock = time.Second
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

// waitForReceipt waits for a tx. This is useful on testnet when a tx may be "missing"
// due to reorg. Wait for a few blocks to find the main chain and hopefully our tx.
func waitForReceipt(t *testing.T, nc *nodeClient, tx *types.Transaction) (*types.Receipt, error) {
	t.Helper()
	hash := tx.Hash()
	// Waiting as much as five blocks.
	timesUp := time.After(5 * secPerBlock)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			receipt, err := nc.transactionReceipt(ctx, hash)
			if err != nil {
				if errors.Is(err, asset.CoinNotFoundError) {
					continue
				}
				return nil, err
			}
			spew.Dump(receipt)
			return receipt, nil
		case <-timesUp:
			spew.Dump(tx)
			return nil, errors.New("wait for receipt timed out, txn might be missing due to reorg, " +
				"check the preceding tx for a bad nonce or low gas cap")
		}
	}
}

// waitForMined will multiply the time limit by testnetSecPerBlock for
// testnet and mine blocks when on simnet.
func waitForMined(t *testing.T, nBlock int, waitTimeLimit bool) error {
	t.Helper()
	if !isTestnet {
		err := exec.Command("geth", "--datadir="+alphaNodeDir, "attach", "--exec", "miner.start()").Run()
		if err != nil {
			return err
		}
		defer func() {
			_ = exec.Command("geth", "--datadir="+alphaNodeDir, "attach", "--exec", "miner.stop()").Run()
		}()
	}
	timesUp := time.After(time.Duration(nBlock) * secPerBlock)
out:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
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

func runSimnet(m *testing.M) (int, error) {
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
	token := dexeth.Tokens[testTokenID].NetTokens[dex.Simnet]
	fmt.Printf("ETH swap contract address is %v\n", dexeth.ContractAddresses[0][dex.Simnet])
	fmt.Printf("Token swap contract addr is %v\n", token.SwapContracts[0].Address)
	fmt.Printf("Test token contract addr is %v\n", token.Address)

	ethSwapContractAddr = dexeth.ContractAddresses[0][dex.Simnet]

	err = setupWallet(simnetWalletDir, simnetWalletSeed, "localhost:30355", dex.Simnet)
	if err != nil {
		return 1, err
	}

	err = setupWallet(participantWalletDir, participantWalletSeed, "localhost:30356", dex.Simnet)
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

	accts, err := exportAccountsFromNode(ethClient.node)
	if err != nil {
		return 1, err
	}
	if len(accts) != 1 {
		return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
	}
	simnetAcct = &accts[0]
	simnetAddr = simnetAcct.Address
	accts, err = exportAccountsFromNode(participantEthClient.node)
	if err != nil {
		return 1, err
	}
	if len(accts) != 1 {
		return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
	}
	participantAcct = &accts[0]
	participantAddr = participantAcct.Address

	if simnetContractor, err = newV0Contractor(dex.Simnet, simnetAddr, ethClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("newV0Contractor error: %w", err)
	}
	if participantContractor, err = newV0Contractor(dex.Simnet, participantAddr, participantEthClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("participant newV0Contractor error: %w", err)
	}

	if simnetTokenContractor, err = newV0TokenContractor(dex.Simnet, testTokenID, simnetAddr, ethClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("newV0TokenContractor error: %w", err)
	}

	// I don't know why this is needed for the participant client but not
	// the initiator. Without this, we'll get a bind.ErrNoCode from
	// (*BoundContract).Call while calling (*ERC20Swap).TokenAddress.
	time.Sleep(time.Second)

	if participantTokenContractor, err = newV0TokenContractor(dex.Simnet, testTokenID, participantAddr, participantEthClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("participant newV0TokenContractor error: %w", err)
	}

	if err := ethClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}
	if err := participantEthClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}

	// Fund the wallets. Can use the simharness package once #1738 is merged.
	send := func(exe, addr, amt string) error {
		cmd := exec.CommandContext(ctx, exe, addr, amt)
		cmd.Dir = harnessCtlDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error running %q: %v", cmd, err)
		}
		fmt.Printf("result from %q: %s\n", cmd, out)
		return nil
	}
	for _, s := range []*struct {
		exe, addr, amt string
	}{
		{"./sendtoaddress", simnetAddr.String(), "10"},
		{"./sendtoaddress", participantAddr.String(), "10"},
		{"./sendTokens", simnetAddr.String(), "10"},
		{"./sendTokens", participantAddr.String(), "10"},
	} {
		if err := send(s.exe, s.addr, s.amt); err != nil {
			return 1, err
		}
	}

	cmd := exec.CommandContext(ctx, "./mine-alpha", "1")
	cmd.Dir = harnessCtlDir
	if err := cmd.Run(); err != nil {
		return 1, fmt.Errorf("error mining block after funding wallets")
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

func runTestnet(m *testing.M) (int, error) {
	if testnetWalletSeed == "" || testnetParticipantWalletSeed == "" {
		return 1, errors.New("testnet seeds not set")
	}
	// Create dir if none yet exists. This persists for the life of the
	// testing harness.
	err := os.MkdirAll(testnetWalletDir, 0755)
	if err != nil {
		return 1, fmt.Errorf("error creating testnet wallet dir dir: %v", err)
	}
	err = os.MkdirAll(testnetParticipantWalletDir, 0755)
	if err != nil {
		return 1, fmt.Errorf("error creating testnet participant wallet dir: %v", err)
	}
	secPerBlock = testnetSecPerBlock
	ethSwapContractAddr = dexeth.ContractAddresses[0][dex.Testnet]
	fmt.Printf("ETH swap contract address is %v\n", ethSwapContractAddr)
	err = setupWallet(testnetWalletDir, testnetWalletSeed, "localhost:30355", dex.Testnet)
	if err != nil {
		return 1, err
	}
	err = setupWallet(testnetParticipantWalletDir, testnetParticipantWalletSeed, "localhost:30356", dex.Testnet)
	if err != nil {
		return 1, err
	}
	ethClient, err = newNodeClient(getWalletDir(testnetWalletDir, dex.Testnet), dex.Testnet, tLogger.SubLogger("initiator"))
	if err != nil {
		return 1, fmt.Errorf("newNodeClient initiator error: %v", err)
	}
	if err := ethClient.connect(ctx); err != nil {
		return 1, fmt.Errorf("connect error: %v\n", err)
	}
	defer ethClient.shutdown()

	fmt.Println("initiator address is", ethClient.address())

	participantEthClient, err = newNodeClient(getWalletDir(testnetParticipantWalletDir, dex.Testnet), dex.Testnet, tLogger.SubLogger("participant"))
	if err != nil {
		return 1, fmt.Errorf("newNodeClient participant error: %v", err)
	}
	if err := participantEthClient.connect(ctx); err != nil {
		return 1, fmt.Errorf("connect error: %v\n", err)
	}
	defer participantEthClient.shutdown()

	fmt.Println("participant address is", participantEthClient.address())
	if addPeer != "" {
		if err := ethClient.addPeer(addPeer); err != nil {
			return 1, fmt.Errorf("unable to add peer: %v", err)
		}
		if err := participantEthClient.addPeer(addPeer); err != nil {
			return 1, fmt.Errorf("unable to add peer: %v", err)
		}
	}

	fmt.Println("Testnet nodes starting sync, this may take a while...")
	wg := sync.WaitGroup{}
	wg.Add(2)
	var initerErr, participantErr error
	go func() {
		initerErr = syncClient(ethClient)
		wg.Done()
	}()
	go func() {
		participantErr = syncClient(participantEthClient)
		wg.Done()
	}()
	wg.Wait()

	if initerErr != nil {
		return 1, fmt.Errorf("error initializing initiator client: %v", initerErr)
	}
	if participantErr != nil {
		return 1, fmt.Errorf("error initializing participant client: %v", participantErr)
	}
	fmt.Println("Testnet nodes synced!!")

	accts, err := exportAccountsFromNode(ethClient.node)
	if err != nil {
		return 1, err
	}
	if len(accts) != 1 {
		return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
	}
	simnetAcct = &accts[0]
	simnetAddr = simnetAcct.Address
	accts, err = exportAccountsFromNode(participantEthClient.node)
	if err != nil {
		return 1, err
	}
	if len(accts) != 1 {
		return 1, fmt.Errorf("expected 1 account to be exported but got %v", len(accts))
	}
	participantAcct = &accts[0]
	participantAddr = participantAcct.Address

	if simnetContractor, err = newV0Contractor(dex.Testnet, simnetAddr, ethClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("newV0Contractor error: %w", err)
	}
	if participantContractor, err = newV0Contractor(dex.Testnet, participantAddr, participantEthClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("participant newV0Contractor error: %w", err)
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

func TestMain(m *testing.M) {
	dexeth.MaybeReadSimnetAddrs()
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()
	// Run in function so that defers happen before os.Exit is called.
	run := runSimnet
	if isTestnet {
		run = runTestnet
	}
	exitCode, err := run(m)
	if err != nil {
		fmt.Println(err)
	}
	signal.Stop(c)
	cancel()
	os.Exit(exitCode)
}

func setupWallet(walletDir, seed, listenAddress string, net dex.Network) error {
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
		Net:      net,
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

	time.Sleep(1) // Give txs time to propagate.
	if err := waitForMined(t, 8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine approval block: %v", err)
	}

	receipt1, err := waitForReceipt(t, ethClient, tx1)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt1)

	receipt2, err := waitForReceipt(t, participantEthClient, tx2)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt2)
}

func syncClient(cl *nodeClient) error {
	giveUpAt := 60
	if isTestnet {
		giveUpAt = 10000
	}
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		prog := cl.syncProgress()
		if isTestnet {
			if prog.HighestBlock == 0 {
				bh, err := cl.bestHeader(ctx)
				if err != nil {
					return err
				}
				// Time in the header is in seconds.
				timeDiff := time.Now().Unix() - int64(bh.Time)
				if timeDiff < dexeth.MaxBlockInterval {
					return nil
				}
			} else if prog.CurrentBlock >= prog.HighestBlock {
				return nil
			}
		} else {
			// If client has ever synced, assume synced with
			// harness. This avoids checking the header time which
			// is probably old.
			if prog.CurrentBlock > 20 {
				return nil
			}
		}
		if i == giveUpAt {
			return fmt.Errorf("block count has not synced in %d seconds", giveUpAt)
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
	if !t.Run("testAddressesHaveFunds", testAddressesHaveFundsFn(10_000_000 /* gwei */)) {
		t.Fatal("not enough funds")
	}
	t.Run("testAddressBalance", testAddressBalance)
	t.Run("testSendTransaction", testSendTransaction)
	t.Run("testSendSignedTransaction", testSendSignedTransaction)
	t.Run("testTransactionReceipt", testTransactionReceipt)
	t.Run("testSignMessage", testSignMessage)
}

// TestContract tests methods that interact with the contract.
func TestContract(t *testing.T) {
	if !t.Run("testAddressesHaveFunds", testAddressesHaveFundsFn(100_000_000 /* gwei */)) {
		t.Fatal("not enough funds")
	}
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
	t.Run("testInitiateToken", func(t *testing.T) { testInitiate(t, testTokenID) })
	t.Run("testRedeemToken", func(t *testing.T) { testRedeem(t, testTokenID) })
	t.Run("testRefundToken", func(t *testing.T) { testRefund(t, testTokenID) })
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

// testAddressesHaveFundsFn returns a function that tests that addresses used
// in tests have enough funds to complete those tests.
func testAddressesHaveFundsFn(amt uint64) func(t *testing.T) {
	return func(t *testing.T) {
		checkAddr := func(addr common.Address) error {
			bal, err := ethClient.addressBalance(ctx, addr)
			if err != nil {
				return err
			}
			if bal == nil {
				return errors.New("empty balance")
			}
			gweiBal := dexeth.WeiToGwei(bal)
			if gweiBal < amt {
				fmt.Printf("Balance is too low to test. Send more than %v test eth to %v.\n", float64(amt-gweiBal)/1e9, addr)
				return fmt.Errorf("balance too low")
			}
			return nil
		}
		var errs error
		if err := checkAddr(simnetAddr); err != nil {
			errs = fmt.Errorf("client one: %v", err)
		}
		if err := checkAddr(participantAddr); err != nil {
			err = fmt.Errorf("client two: %v", err)
			if errs != nil {
				errs = fmt.Errorf("%v: %v", errs, err)
			} else {
				errs = err
			}
		}
		if errs != nil {
			t.Fatal(errs)
		}
	}
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
	if err := waitForMined(t, 10, false); err != nil {
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
		GasFeeCap: dexeth.GweiToWei(maxFeeRate),
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
	if err := waitForMined(t, 10, false); err != nil {
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
	if err := waitForMined(t, 10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err := waitForReceipt(t, ethClient, tx)
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

func testSyncProgress(t *testing.T) {
	spew.Dump(ethClient.syncProgress())
}

func testInitiateGas(t *testing.T, assetID uint32) {
	if assetID != BipID {
		prepareTokenClients(t)
	}

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

// initiateOverflow is just like *contractorV0.initiate but sets the first swap
// value to a max uint256 minus one emv unit.
func initiateOverflow(c *contractorV0, txOpts *bind.TransactOpts, details []*dex.SwapContractDetails) (*types.Transaction, error) {
	inits := make([]swapv0.ETHSwapInitiation, 0, len(details))
	secrets := make(map[[32]byte]bool, len(details))

	for i, deets := range details {
		if len(deets.SecretHash) != dexeth.SecretHashSize {
			return nil, fmt.Errorf("wrong secret hash length. wanted %d, got %d", dexeth.SecretHashSize, len(deets.SecretHash))
		}

		var secretHash [32]byte
		copy(secretHash[:], deets.SecretHash)

		if secrets[secretHash] {
			return nil, fmt.Errorf("secret hash %s is a duplicate", deets.SecretHash)
		}
		secrets[secretHash] = true

		if !common.IsHexAddress(deets.To) {
			return nil, fmt.Errorf("%q is not an address", deets.To)
		}

		val := c.evmify(deets.Value)
		if i == 0 {
			val = big.NewInt(2)
			val.Exp(val, big.NewInt(256), nil)
			val.Sub(val, c.evmify(1))
			fmt.Println(val)
		}
		inits = append(inits, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(int64(deets.LockTime)),
			SecretHash:      secretHash,
			Participant:     common.HexToAddress(deets.To),
			Value:           val,
		})
	}

	return c.contractV0.Initiate(txOpts, inits)
}

func testInitiate(t *testing.T, assetID uint32) {
	if assetID != BipID {
		prepareTokenClients(t)
	}

	isETH := assetID == BipID

	sc := simnetContractor
	balance := func() (*big.Int, error) {
		return ethClient.addressBalance(ctx, ethClient.address())
	}
	gases := ethGases
	if !isETH {
		sc = simnetTokenContractor
		balance = func() (*big.Int, error) {
			return simnetTokenContractor.balance(ctx)
		}
		gases = tokenGases
	}

	now := uint64(time.Now().Unix())

	// Create a slice of random secret hashes that can be used in the tests and
	// make sure none of them have been used yet.
	numSecretHashes := 10
	secretHashes := make([][32]byte, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		secretHashes[i] = bytesToArray(encode.RandomBytes(32))
	}

	newDetails := func(idx int, v uint64) *dex.SwapContractDetails {
		return &dex.SwapContractDetails{
			From:       ethClient.address().String(),
			To:         participantEthClient.address().String(),
			LockTime:   now,
			SecretHash: secretHashes[idx][:],
			Value:      v,
		}
	}

	tests := []struct {
		name              string
		swaps             []*dex.SwapContractDetails
		success, overflow bool
		swapErr           bool
	}{
		{
			name:    "1 swap ok",
			success: true,
			swaps: []*dex.SwapContractDetails{
				newDetails(0, 2),
			},
		},
		{
			name:    "1 swap with existing hash",
			success: false,
			swaps: []*dex.SwapContractDetails{
				newDetails(0, 1),
			},
		},
		{
			name:    "2 swaps ok",
			success: true,
			swaps: []*dex.SwapContractDetails{
				newDetails(1, 1),
				newDetails(2, 1),
			},
		},
		{
			name:    "2 swaps repeated hash",
			success: false,
			swaps: []*dex.SwapContractDetails{
				newDetails(3, 1),
				newDetails(3, 1),
			},
			swapErr: true,
		},
		{
			name:    "1 swap nil refundtimestamp",
			success: false,
			swaps: []*dex.SwapContractDetails{
				newDetails(4, 1),
			},
		},
		{
			// Preventing this used to need explicit checks before solidity 0.8, but now the
			// compiler checks for integer overflows by default.
			name:     "value addition overflows",
			success:  false,
			overflow: true,
			swaps: []*dex.SwapContractDetails{
				newDetails(5, 0), // Will be set to max uint256 - 1 evm unit
				newDetails(6, 3),
			},
		},
		{
			name:    "swap with 0 value",
			success: false,
			swaps: []*dex.SwapContractDetails{
				newDetails(7, 0),
				newDetails(8, 1),
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
			step, _, _, err := sc.status(ctx, tSwap)
			if err != nil {
				t.Fatalf("%s: swap error: %v", test.name, err)
			}
			originalStates[tSwap.SecretHash.String()] = step
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

		if test.overflow {
			optsVal = 2
		}

		expGas := gases.SwapN(len(test.swaps))
		txOpts, _ := ethClient.txOpts(ctx, optsVal, expGas, dexeth.GweiToWei(maxFeeRate))

		spew.Dump(txOpts)

		var tx *types.Transaction
		if test.overflow {
			switch c := sc.(type) {
			case *contractorV0:
				tx, err = initiateOverflow(c, txOpts, test.swaps)
			case *tokenContractorV0:
				tx, err = initiateOverflow(c.contractorV0, txOpts, test.swaps)
			}
		} else {
			b, _ := balance()
			fmt.Println("--beforeinit", fmtBig(b))

			tx, err = sc.initiate(txOpts, test.swaps)

			spew.Dump(tx)
		}
		if err != nil {
			if test.swapErr {
				continue
			}
			t.Fatalf("%s: initiate error: %v", test.name, err)
		}

		if err := waitForMined(t, 10, false); err != nil {
			t.Fatalf("%s: post-initiate mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := waitForReceipt(t, ethClient, tx)
		if err != nil {
			t.Fatalf("%s: failed retrieving initiate receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		err = checkTxStatus(receipt, txOpts.GasLimit)
		if err != nil && test.success {
			t.Fatalf("%s: failed init transaction status: %v", test.name, err)
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

			fmt.Println("Original balance:", fmtBig(originalBal), ", New balance:", fmtBig(bal),
				", Balance change:", fmtBig(new(big.Int).Sub(bal, originalBal)),
				", Tx fees:", fmtBig(txFee), ", Swap val:", fmtBig(dexeth.GweiToWei(totalVal)))

			cmd := exec.CommandContext(ctx, "./mine-alpha", "5")
			cmd.Dir = harnessCtlDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("error mining block after funding wallets")
			}

			bal, _ := balance()

			fmt.Println("Original balance:", fmtBig(originalBal), ", New balance:", fmtBig(bal),
				", Balance change:", fmtBig(new(big.Int).Sub(bal, originalBal)))
			t.Fatalf("%s: unexpected balance change: want %d got %d gwei, diff = %.9f gwei",
				test.name, dexeth.WeiToGwei(wantBal), dexeth.WeiToGwei(bal), float64(diff.Int64())/dexeth.GweiFactor)
		}

		for _, tSwap := range test.swaps {
			step, _, _, err := sc.status(ctx, tSwap)
			if err != nil {
				t.Fatalf("%s: swap error post-init: %v", test.name, err)
			}

			if test.success && step != dexeth.SSInitiated {
				t.Fatalf("%s: wrong success swap state: want %s got %s", test.name, dexeth.SSInitiated, step)
			}

			originalState := originalStates[hex.EncodeToString(tSwap.SecretHash[:])]
			if !test.success && step != originalState {
				t.Fatalf("%s: wrong error swap state: want %s got %s", test.name, originalState, step)
			}
		}
	}
}

func testRedeemGas(t *testing.T, assetID uint32) {
	if assetID != BipID {
		prepareTokenClients(t)
	}

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

	newDetails := func(idx int, v uint64) *dex.SwapContractDetails {
		return &dex.SwapContractDetails{
			From:       ethClient.address().String(),
			To:         participantEthClient.address().String(),
			LockTime:   now,
			SecretHash: secretHashes[idx][:],
			Value:      v,
		}
	}

	details := make([]*dex.SwapContractDetails, 0, numSwaps)
	for i := 0; i < numSwaps; i++ {
		details = append(details, newDetails(i, 1))
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

	txOpts, _ := ethClient.txOpts(ctx, optsVal, gases.SwapN(len(details)), dexeth.GweiToWei(maxFeeRate))
	tx, err := c.initiate(txOpts, details)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, 8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}
	receipt, err := waitForReceipt(t, ethClient, tx)
	if err != nil {
		t.Fatalf("failed retrieving initiate receipt: %v", err)
	}
	spew.Dump(receipt)

	err = checkTxStatus(receipt, txOpts.GasLimit)
	if err != nil {
		t.Fatalf("failed init transaction status: %v", err)
	}

	// Make sure swaps were properly initiated
	for _, deets := range details {
		step, _, _, err := c.status(ctx, deets)
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		if step != dexeth.SSInitiated {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, step)
		}
	}

	// Test gas usage of redeem function
	var previous uint64
	for i := 0; i < numSwaps; i++ {
		gas, err := pc.estimateRedeemGas(ctx, secrets[:i+1], details[:i+1])
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
	if assetID != BipID {
		prepareTokenClients(t)
	}
	lockTime := uint64(time.Now().Add(12 * secPerBlock).Unix())
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

	newDetails := func(idx int, v uint64) *dex.SwapContractDetails {
		return &dex.SwapContractDetails{
			From:       ethClient.address().String(),
			To:         participantEthClient.address().String(),
			LockTime:   lockTime,
			SecretHash: secretHashes[idx][:],
			Value:      v,
		}
	}

	tests := []struct {
		name               string
		sleepNBlocks       int
		redeemerClient     *nodeClient
		redeemer           *accounts.Account
		redeemerContractor contractor
		swaps              []*dex.SwapContractDetails
		redemptions        []*asset.Redemption
		isRedeemable       []bool
		finalStates        []dexeth.SwapStep
		addAmt             bool
		expectRedeemErr    bool
	}{
		{
			name:               "ok before locktime",
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*dex.SwapContractDetails{newDetails(0, 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[0], secretHashes[0])},
			isRedeemable:       []bool{true},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "ok two before locktime",
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps: []*dex.SwapContractDetails{
				newDetails(1, 1),
				newDetails(2, 1),
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
			sleepNBlocks:       16,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*dex.SwapContractDetails{newDetails(3, 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[3], secretHashes[3])},
			isRedeemable:       []bool{true},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "bad redeemer",
			sleepNBlocks:       8,
			redeemerClient:     ethClient,
			redeemer:           simnetAcct,
			redeemerContractor: c,
			swaps:              []*dex.SwapContractDetails{newDetails(4, 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[4], secretHashes[4])},
			isRedeemable:       []bool{false},
			finalStates:        []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:             false,
		},
		{
			name:               "bad secret",
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*dex.SwapContractDetails{newDetails(5, 1)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[6], secretHashes[5])},
			isRedeemable:       []bool{false},
			finalStates:        []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:             false,
		},
		{
			name:               "duplicate secret hashes",
			expectRedeemErr:    true,
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps: []*dex.SwapContractDetails{
				newDetails(7, 1),
				newDetails(8, 1),
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
		for _, contract := range test.swaps {
			step, _, _, err := c.status(ctx, contract)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			if step != dexeth.SSNone {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, step)
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

		// This waitForMined will always take test.sleepNBlocks to complete.
		if err := waitForMined(t, test.sleepNBlocks, true); err != nil {
			t.Fatalf("%s: post-init mining error: %v", test.name, err)
		}

		receipt, err := waitForReceipt(t, test.redeemerClient, tx)
		if err != nil {
			t.Fatalf("%s: failed to get init receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		err = checkTxStatus(receipt, txOpts.GasLimit)
		if err != nil {
			t.Fatalf("%s: failed init transaction status: %v", test.name, err)
		}
		fmt.Printf("Gas used for %d inits: %d \n", len(test.swaps), receipt.GasUsed)

		for i, contract := range test.swaps {
			step, _, _, err := test.redeemerContractor.status(ctx, contract)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			if step != dexeth.SSInitiated {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSInitiated, step)
			}

			expected := test.isRedeemable[i]
			redemption := test.redemptions[i]

			isRedeemable, err := test.redeemerContractor.isRedeemable(bytesToArray(redemption.Spends.SecretHash), contract)
			if err != nil {
				t.Fatalf(`test "%v": error calling isRedeemable: %v`, test.name, err)
			} else if isRedeemable != expected {
				t.Fatalf(`test "%v": expected isRedeemable to be %v, but got %v. step = %s`, test.name, expected, isRedeemable, step)
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

		if err := waitForMined(t, 10, false); err != nil {
			t.Fatalf("%s: post-redeem mining error: %v", test.name, err)
		}

		receipt, err = waitForReceipt(t, test.redeemerClient, tx)
		if err != nil {
			t.Fatalf("%s: failed to get redeem receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		expSuccess := !test.expectRedeemErr && test.addAmt
		err = checkTxStatus(receipt, txOpts.GasLimit)
		if err != nil && expSuccess {
			t.Fatalf("%s: failed redeem transaction status: %v", test.name, err)
		}
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

		for i := range test.redemptions {
			step, _, _, err := c.status(ctx, test.swaps[i])
			if err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
			if step != test.finalStates[i] {
				t.Fatalf("unexpected swap state for test %v [%d]: want %s got %s",
					test.name, i, test.finalStates[i], step)
			}
		}
	}
}

func testRefundGas(t *testing.T, assetID uint32) {
	if assetID != BipID {
		prepareTokenClients(t)
	}

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
	deets := &dex.SwapContractDetails{
		From:       ethClient.address().String(),
		To:         participantEthClient.address().String(),
		LockTime:   lockTime,
		SecretHash: secretHash[:],
		Value:      1,
	}
	_, err := c.initiate(txOpts, []*dex.SwapContractDetails{deets})
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(t, 8, true); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	step, _, _, err := c.status(ctx, deets)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	if step != dexeth.SSInitiated {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, step)
	}

	gas, err := c.estimateRefundGas(ctx, deets)
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
	if assetID != BipID {
		prepareTokenClients(t)
	}

	const amt = 1

	isETH := assetID == BipID

	gases := ethGases
	c, pc := simnetContractor, participantContractor
	if !isETH {
		gases = tokenGases
		c, pc = simnetTokenContractor, participantTokenContractor
	}
	sleepForNBlocks := 8
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
		inLocktime := uint64(time.Now().Add(test.addTime).Unix())
		deets := &dex.SwapContractDetails{
			From:       ethClient.address().String(),
			To:         participantEthClient.address().String(),
			LockTime:   inLocktime,
			SecretHash: secretHash[:],
			Value:      amt,
		}

		step, _, _, err := test.refunderContractor.status(ctx, deets)
		if err != nil {
			t.Fatalf("%s: unable to get swap state pre-init", test.name)
		}
		if step != dexeth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, step)
		}

		txOpts, _ := ethClient.txOpts(ctx, optsVal, gases.SwapN(1), nil)
		_, err = c.initiate(txOpts, []*dex.SwapContractDetails{deets})
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(t, sleepForNBlocks, false); err != nil {
				t.Fatalf("%s: pre-redeem mining error: %v", test.name, err)
			}

			txOpts, _ = participantEthClient.txOpts(ctx, 0, gases.RedeemN(1), nil)
			_, err := pc.redeem(txOpts, []*asset.Redemption{newRedeem(secret, secretHash)})
			if err != nil {
				t.Fatalf("%s: redeem error: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(t, sleepForNBlocks, true); err != nil {
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

		isRefundable, err := test.refunderContractor.isRefundable(deets)
		if err != nil {
			t.Fatalf("%s: isRefundable error %v", test.name, err)
		}
		if isRefundable != test.isRefundable {
			t.Fatalf("%s: expected isRefundable=%v, but got %v",
				test.name, test.isRefundable, isRefundable)
		}

		txOpts, _ = test.refunderClient.txOpts(ctx, 0, gases.Refund, nil)
		tx, err := test.refunderContractor.refund(txOpts, deets)
		if err != nil {
			t.Fatalf("%s: refund error: %v", test.name, err)
		}
		spew.Dump(tx)

		in, _, err := test.refunderContractor.value(ctx, tx)

		if test.addAmt && in != amt {
			t.Fatalf("%s: unexpected pending in balance %d", test.name, in)
		}

		if err := waitForMined(t, 10, false); err != nil {
			t.Fatalf("%s: post-refund mining error: %v", test.name, err)
		}

		receipt, err := waitForReceipt(t, test.refunderClient, tx)
		if err != nil {
			t.Fatalf("%s: failed to get refund receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		err = checkTxStatus(receipt, txOpts.GasLimit)
		// test.addAmt being true indicates the refund shoud succeed.
		if err != nil && test.addAmt {
			t.Fatalf("%s: failed refund transaction status: %v", test.name, err)
		}
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

		step, _, _, err = test.refunderContractor.status(ctx, deets)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		if step != test.finalState {
			t.Fatalf("%s: wrong swap state: want %s got %s", test.name, test.finalState, step)
		}
	}
}

func testApproveAllowance(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	if err := waitForMined(t, 10, false); err != nil {
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

	ethClient.addSignerToOpts(txOpts)

	// Deploy the reentry attack contract.
	_, _, reentryContract, err := reentryattack.DeployReentryAttack(txOpts, ethClient.ec)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(t, 10, false); err != nil {
		t.Fatal(err)
	}

	originalContractBal, err := ethClient.addressBalance(ctx, ethSwapContractAddr)
	if err != nil {
		t.Fatal(err)
	}

	var secretHash [32]byte
	var inLocktime uint64
	// Make four swaps that should be locked and refundable and one that is
	// soon refundable.
	for i := 0; i < 5; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash = sha256.Sum256(secret[:])

		if i != 4 {
			inLocktime = uint64(time.Now().Add(time.Hour).Unix())
			details := []*dex.SwapContractDetails{{
				From:       ethClient.address().String(),
				To:         participantEthClient.address().String(),
				LockTime:   inLocktime,
				SecretHash: secretHash[:],
				Value:      1,
			}}

			txOpts, _ := ethClient.txOpts(ctx, 1, ethGases.SwapN(1), nil)
			_, err = simnetContractor.initiate(txOpts, details)
			if err != nil {
				t.Fatalf("unable to initiate swap: %v ", err)
			}

			if err := waitForMined(t, 10, false); err != nil {
				t.Fatal(err)
			}
			continue
		}

		intermediateContractVal, _ := ethClient.addressBalance(ctx, ethSwapContractAddr)
		t.Logf("intermediate contract value %d", dexeth.WeiToGwei(intermediateContractVal))

		inLocktime = uint64(time.Now().Add(-1 * time.Second).Unix())
		// Set some variables in the contract used for the exploit. This
		// will fail (silently) due to require(msg.origin == msg.sender)
		// in the real contract.
		txOpts, _ := ethClient.txOpts(ctx, 1, defaultSendGasLimit*5, nil)
		_, err := reentryContract.SetUsUpTheBomb(txOpts, ethSwapContractAddr, secretHash, big.NewInt(int64(inLocktime)), participantAddr)
		if err != nil {
			t.Fatalf("unable to set up the bomb: %v", err)
		}
		if err = waitForMined(t, 10, false); err != nil {
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
	if err = waitForMined(t, 10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err := waitForReceipt(t, ethClient, tx)
	if err != nil {
		t.Fatalf("unable to get receipt: %v", err)
	}
	spew.Dump(receipt)

	if err = waitForMined(t, 10, false); err != nil {
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
	if err = waitForMined(t, 10, false); err != nil {
		t.Fatal(err)
	}
	receipt, err = waitForReceipt(t, ethClient, tx)
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
	step, _, _, err := simnetContractor.status(ctx, &dex.SwapContractDetails{
		From:       ethClient.address().String(),
		To:         participantEthClient.address().String(),
		LockTime:   inLocktime,
		SecretHash: secretHash[:],
		Value:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if step != dexeth.SSNone {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSNone, step)
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

// This test can be used to test that the resubmission of ETH redemptions after
// they have been overridden by another transaction works properly. Just replace
// the app seed and nonce. Also uncomment the lines in the function which will
// either create a replacement transaction that redeems the swap, or one that
// just sends ETH to another address. If the swap is redeemed by another tx,
// the DEX should not attempt to resubmit the redemption.
/*func TestReplaceWithRandomTransaction(t *testing.T) {
	appSeed := "36e6976207c4ae35d2942fc644fc845cba28c2d3832f93c94fd23e9857e19ad65c2752a041afbf3f1897f693d4abe3903d6374d53cb78b90002ecc6999d4b317"
	var gasFeeCap int64 = 300000000000
	var gasTipCap int64 = 300000000000
	var nonce int64 = 6

	// uncomment these lines to override the tx with another one that redeems the swap
	// addressToCall := common.HexToAddress("0x2f68e723b8989ba1c6a9f03e42f33cb7dc9d606f")
	// data, _ := hex.DecodeString("f4fd17f9000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000011ad8ca2d3762cc2d1b8e6216944d90bb5d2b0885106dcadc7ad8f24af49b295d4914b04a3523fb24740bf72f58ca69549343e3f0428fd557f4a81bb6695f467e")
	// value := 0

	// uncomment these lines to override the tx with another one that does not redeem the swap
	// addressToCall := simnetAddr
	// data := []byte{}
	// value := uint64(1)

	walletDir := filepath.Join(dcrutil.AppDataDir("dexc", false), "simnet", "assetdb", dex.BipIDSymbol(60))
	client, err := newNodeClient(getWalletDir(walletDir, dex.Simnet), dex.Simnet, tLogger.SubLogger("initiator"))
	if err != nil {
		t.Fatalf("unable to create node client %v", err)
	}
	if err := client.connect(ctx); err != nil {
		t.Fatalf("failed to connect to node: %v", err)
	}
	defer client.shutdown()

	appSeedB, err := hex.DecodeString(appSeed)
	if err != nil {
		t.Fatal(err)
	}

	appSeedAndPass := func(assetID uint32, appSeed []byte) ([]byte, []byte) {
		b := make([]byte, len(appSeed)+4)
		copy(b, appSeed)
		binary.BigEndian.PutUint32(b[len(appSeed):], assetID)

		s := blake256.Sum256(b)
		p := blake256.Sum256(s[:])
		return s[:], p[:]
	}

	_, pw := appSeedAndPass(60, appSeedB)
	err = client.unlock(string(pw))
	if err != nil {
		t.Fatal(err)
	}

	txOpts := &bind.TransactOpts{
		Context:   ctx,
		From:      client.address(),
		Value:     dexeth.GweiToWei(value),
		GasFeeCap: big.NewInt(gasFeeCap),
		GasTipCap: big.NewInt(gasTipCap),
		GasLimit:  63000,
		Nonce:     big.NewInt(nonce),
	}

	tx, err := client.sendTransaction(ctx, txOpts, addressToCall, data)
	if err != nil {
		t.Fatalf("failed to send transaction: %v", err)
	}

	fmt.Printf("replacement tx hash: %s\n", tx.Hash())
}*/

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
		if err := waitForMined(t, 10, false); err != nil {
			t.Fatalf("mining error: %v", err)
		}
	}); err != nil {
		t.Fatalf("getGasEstimates error: %v", err)
	}
}

func TestConfirmedNonce(t *testing.T) {
	_, err := ethClient.getConfirmedNonce(ctx, -1)
	if err != nil {
		t.Fatalf("getConfirmedNonce error: %v", err)
	}
}

func TestTokenGasEstimatesParticipant(t *testing.T) {
	prepareTokenClients(t)
	if err := GetGasEstimates(ctx, participantEthClient, participantTokenContractor, 5, tokenGases, func() {
		if err := waitForMined(t, 10, false); err != nil {
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

func fmtBig(v *big.Int) string {
	return fmt.Sprintf("%.9f gwei", float64(new(big.Int).Div(v, big.NewInt(dexeth.GweiFactor)).Int64()))
}
