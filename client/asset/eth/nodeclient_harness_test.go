//go:build harness

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

package eth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

const (
	alphaNode         = "enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304"
	alphaAddr         = "18d65fb8d60c1199bb1ad381be47aa692b482605"
	pw                = "bee75192465cef9f8ab1198093dfed594e93ae810ccbbf2b3b12e1771cc6cb19"
	maxFeeRate uint64 = 200 // gwei per gas

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
	testnetWalletDir            string
	testnetParticipantWalletDir string

	alphaNodeDir               = filepath.Join(homeDir, "dextest", "eth", "alpha", "node")
	alphaWSEndpoint            = "ws://127.0.0.1:38557"
	ctx                        context.Context
	tLogger                    = dex.StdOutLogger("ETHTEST", dex.LevelWarn)
	simnetWalletSeed           = "0812f5244004217452059e2fd11603a511b5d0870ead753df76c966ce3c71531"
	simnetAddr                 common.Address
	simnetAcct                 *accounts.Account
	ethClient                  ethFetcher
	participantWalletSeed      = "a897afbdcba037c8c735cc63080558a30d72851eb5a3d05684400ec4123a2d00"
	participantAddr            common.Address
	participantAcct            *accounts.Account
	participantEthClient       ethFetcher
	simnetContractor           contractor
	participantContractor      contractor
	simnetTokenContractor      tokenContractor
	participantTokenContractor tokenContractor
	ethGases                   *dexeth.Gases
	tokenGases                 *dexeth.Gases
	testnetSecPerBlock         = 15 * time.Second
	// secPerBlock is one for simnet, because it takes one second to mine a
	// block currently. Is set in code to testnetSecPerBlock if running on
	// testnet.
	secPerBlock = time.Second
	// If you are testing on testnet, you must specify the rpcNode. You can also
	// specify it in the testnet-credentials.json file.
	rpcProviders []string

	// isTestnet can be set to true to perform tests on the sepolia testnet.
	// May need some setup including sending testnet coins to the addresses
	// and a lengthy sync. Wallet addresses are the same as simnet. Tests may
	// need to be run with a high --timeout=2h for the initial sync.
	//
	// Only for non-token tests, so run with --run=TestGroupName.
	//
	// TODO: Make this also work for token tests.
	isTestnet bool

	// testnetWalletSeed and testnetParticipantWalletSeed are required for
	// use on testnet and can be any 256 bit hex. If the wallets created by
	// these seeds do not have enough funds to test, addresses that need
	// funds will be printed.
	testnetWalletSeed            string
	testnetParticipantWalletSeed string
	usdcID, _                    = dex.BipSymbolID("usdc.eth")
	masterToken                  *dexeth.Token

	v1          bool
	contractVer uint32
)

func newContract(stamp uint64, secretHash [32]byte, val uint64) *asset.Contract {
	return &asset.Contract{
		LockTime:   stamp,
		SecretHash: secretHash[:],
		Address:    participantAddr.String(),
		Value:      val,
	}
}

func acLocator(c *asset.Contract) []byte {
	return makeLocator(bytesToArray(c.SecretHash), c.Value, c.LockTime)
}

func makeLocator(secretHash [32]byte, valg, lockTime uint64) []byte {
	if contractVer == 1 {
		return (&dexeth.SwapVector{
			From:       ethClient.address(),
			To:         participantEthClient.address(),
			Value:      dexeth.GweiToWei(valg),
			SecretHash: secretHash,
			LockTime:   lockTime,
		}).Locator()
	}
	return secretHash[:]
}

func newRedeem(secret, secretHash [32]byte, valg, lockTime uint64) *asset.Redemption {
	return &asset.Redemption{
		Spends: &asset.AuditInfo{
			SecretHash: secretHash[:],
			Recipient:  participantEthClient.address().String(),
			Expiration: time.Unix(int64(lockTime), 0),
			Coin: &coin{
				// id:    txHash,
				value: valg,
			},
			Contract: dexeth.EncodeContractData(contractVer, makeLocator(secretHash, valg, lockTime)),
		},
		Secret: secret[:],
	}
}

// waitForReceipt waits for a tx. This is useful on testnet when a tx may be "missing"
// due to reorg. Wait for a few blocks to find the main chain and hopefully our tx.
func waitForReceipt(nc ethFetcher, tx *types.Transaction) (*types.Receipt, error) {
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
			// spew.Dump(receipt)
			return receipt, nil
		case <-timesUp:
			spew.Dump(tx)
			return nil, errors.New("wait for receipt timed out, txn might be missing due to reorg, " +
				"check the preceding tx for a bad nonce or low gas cap")
		}
	}
}

func waitForMined() error {
	hdr, err := ethClient.bestHeader(ctx)
	if err != nil {
		return err
	}
	const targetConfs = 3
	currentHeight := hdr.Number
	barrierHeight := new(big.Int).Add(currentHeight, big.NewInt(targetConfs))
	fmt.Println("Waiting for RPC blocks")
	for {
		select {
		case <-time.After(time.Second):
			hdr, err = ethClient.bestHeader(ctx)
			if err != nil {
				return err
			}
			if hdr.Number.Cmp(barrierHeight) > 0 {
				return nil
			}
			if hdr.Number.Cmp(currentHeight) > 0 {
				currentHeight = hdr.Number
				fmt.Println("Block mined!!!", new(big.Int).Sub(barrierHeight, currentHeight).Uint64()+1, "to go")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func prepareRPCClient(name, dataDir string, providers []string, net dex.Network) (*multiRPCClient, *accounts.Account, error) {
	cfg, err := ChainConfig(net)
	if err != nil {
		return nil, nil, err
	}

	c, err := newMultiRPCClient(dataDir, providers, tLogger.SubLogger(name), cfg, 3, net)
	if err != nil {
		return nil, nil, fmt.Errorf("(%s) prepareRPCClient error: %v", name, err)
	}
	if err := c.connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("(%s) connect error: %v", name, err)
	}
	return c, c.creds.acct, nil
}

func rpcEndpoints(net dex.Network) []string {
	if net == dex.Testnet {
		return rpcProviders
	}
	return []string{alphaWSEndpoint}
}

func prepareTestRPCClients(initiatorDir, participantDir string, net dex.Network) (err error) {
	endpoints := rpcEndpoints(net)

	ethClient, simnetAcct, err = prepareRPCClient("initiator", initiatorDir, endpoints, net)
	if err != nil {
		return err
	}
	fmt.Println("initiator address is", ethClient.address())

	participantEthClient, participantAcct, err = prepareRPCClient("participant", participantDir, endpoints, net)
	if err != nil {
		ethClient.shutdown()
		return err
	}
	fmt.Println("participant address is", participantEthClient.address())
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

	tokenGases = &dexeth.Tokens[usdcID].NetTokens[dex.Simnet].SwapContracts[contractVer].Gas

	// ETH swap contract.
	masterToken = dexeth.Tokens[usdcID]
	token := masterToken.NetTokens[dex.Simnet]
	fmt.Printf("ETH swap contract address is %v\n", dexeth.ContractAddresses[contractVer][dex.Simnet])
	fmt.Printf("Token swap contract addr is %v\n", token.SwapContracts[contractVer].Address)
	fmt.Printf("Test token contract addr is %v\n", token.Address)

	contractAddr := dexeth.ContractAddresses[contractVer][dex.Simnet]

	providers := rpcEndpoints(dex.Simnet)

	err = setupWallet(simnetWalletDir, simnetWalletSeed, "localhost:30355", providers, dex.Simnet)
	if err != nil {
		return 1, err
	}

	err = setupWallet(participantWalletDir, participantWalletSeed, "localhost:30356", providers, dex.Simnet)
	if err != nil {
		return 1, err
	}

	if err = prepareTestRPCClients(simnetWalletDir, participantWalletDir, dex.Simnet); err != nil {
		return 1, err
	}
	defer ethClient.shutdown()
	defer participantEthClient.shutdown()

	if err := syncClient(ethClient); err != nil {
		return 1, fmt.Errorf("error initializing initiator client: %v", err)
	}
	if err := syncClient(participantEthClient); err != nil {
		return 1, fmt.Errorf("error initializing participant client: %v", err)
	}

	simnetAddr = simnetAcct.Address
	participantAddr = participantAcct.Address

	contractAddr, exists := dexeth.ContractAddresses[contractVer][dex.Simnet]
	if !exists || contractAddr == (common.Address{}) {
		return 1, fmt.Errorf("no contract address for contract version %d", contractVer)
	}

	prepareSimnetContractors()

	if err := ethClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}
	if err := participantEthClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}

	// Fund the wallets.
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
		{"./sendUSDC", simnetAddr.String(), "10"},
		{"./sendUSDC", participantAddr.String(), "10"},
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
	masterToken = dexeth.Tokens[usdcID]
	tokenGases = &masterToken.NetTokens[dex.Testnet].SwapContracts[contractVer].Gas
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
	contractAddr := dexeth.ContractAddresses[contractVer][dex.Testnet]
	fmt.Printf("Swap contract address is %v\n", contractAddr)

	rpc := rpcEndpoints(dex.Testnet)

	err = setupWallet(testnetWalletDir, testnetWalletSeed, "localhost:30355", rpc, dex.Testnet)
	if err != nil {
		return 1, err
	}
	err = setupWallet(testnetParticipantWalletDir, testnetParticipantWalletSeed, "localhost:30356", rpc, dex.Testnet)
	if err != nil {
		return 1, err
	}
	if err = prepareTestRPCClients(testnetWalletDir, testnetParticipantWalletDir, dex.Testnet); err != nil {
		return 1, err
	}
	defer ethClient.shutdown()
	defer participantEthClient.shutdown()

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

	simnetAddr = simnetAcct.Address
	participantAddr = participantAcct.Address

	contractAddr, exists := dexeth.ContractAddresses[contractVer][dex.Testnet]
	if !exists || contractAddr == (common.Address{}) {
		return 1, fmt.Errorf("no contract address for version %d", contractVer)
	}

	ctor := newV0Contractor
	if contractVer == 1 {
		ctor = newV1Contractor
	}

	if simnetContractor, err = ctor(dex.Testnet, contractAddr, simnetAddr, ethClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("newV0Contractor error: %w", err)
	}
	if participantContractor, err = ctor(dex.Testnet, contractAddr, participantAddr, participantEthClient.contractBackend()); err != nil {
		return 1, fmt.Errorf("participant newV0Contractor error: %w", err)
	}

	if err := ethClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}
	if err := participantEthClient.unlock(pw); err != nil {
		return 1, fmt.Errorf("error unlocking initiator client: %w", err)
	}

	if v1 {
		scv1 := simnetContractor.(*contractorV1)
		if simnetTokenContractor, err = scv1.tokenContractor(dexeth.Tokens[usdcID]); err != nil {
			return 1, fmt.Errorf("v1 tokenContractor error: %w", err)
		}
		pcv1 := participantContractor.(*contractorV1)
		if participantTokenContractor, err = pcv1.tokenContractor(dexeth.Tokens[usdcID]); err != nil {
			return 1, fmt.Errorf("participant v1 tokenContractor error: %w", err)
		}
	} else {
		if simnetTokenContractor, err = newV0TokenContractor(dex.Testnet, dexeth.Tokens[usdcID], simnetAddr, ethClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("newV0TokenContractor error: %w", err)
		}

		// I don't know why this is needed for the participant client but not
		// the initiator. Without this, we'll get a bind.ErrNoCode from
		// (*BoundContract).Call while calling (*ERC20Swap).TokenAddress.
		time.Sleep(time.Second)

		if participantTokenContractor, err = newV0TokenContractor(dex.Testnet, dexeth.Tokens[usdcID], participantAddr, participantEthClient.contractBackend()); err != nil {
			return 1, fmt.Errorf("participant newV0TokenContractor error: %w", err)
		}
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

func prepareSimnetContractors() (err error) {
	contractAddr := dexeth.ContractAddresses[contractVer][dex.Simnet]

	ctor := newV0Contractor
	if contractVer == 1 {
		ctor = newV1Contractor
	}

	if simnetContractor, err = ctor(dex.Simnet, contractAddr, simnetAddr, ethClient.contractBackend()); err != nil {
		return fmt.Errorf("new contractor error: %w", err)
	}
	if participantContractor, err = ctor(dex.Simnet, contractAddr, participantAddr, participantEthClient.contractBackend()); err != nil {
		return fmt.Errorf("participant new contractor error: %w", err)
	}

	token := dexeth.Tokens[usdcID]

	if v1 {
		scv1 := simnetContractor.(*contractorV1)
		if simnetTokenContractor, err = scv1.tokenContractor(token); err != nil {
			return fmt.Errorf("v1 tokenContractor error: %w", err)
		}
		pcv1 := participantContractor.(*contractorV1)
		if participantTokenContractor, err = pcv1.tokenContractor(token); err != nil {
			return fmt.Errorf("participant v1 tokenContractor error: %w", err)
		}
	} else {
		if simnetTokenContractor, err = newV0TokenContractor(dex.Simnet, token, simnetAddr, ethClient.contractBackend()); err != nil {
			return fmt.Errorf("newV0TokenContractor error: %w", err)
		}

		// I don't know why this is needed for the participant client but not
		// the initiator. Without this, we'll get a bind.ErrNoCode from
		// (*BoundContract).Call while calling (*ERC20Swap).TokenAddress.
		time.Sleep(time.Second)

		if participantTokenContractor, err = newV0TokenContractor(dex.Simnet, token, participantAddr, participantEthClient.contractBackend()); err != nil {
			return fmt.Errorf("participant newV0TokenContractor error: %w", err)
		}
	}

	return
}

func useTestnet() error {
	isTestnet = true
	b, err := os.ReadFile(filepath.Join(homeDir, "dextest", "credentials.json"))
	if err != nil {
		return fmt.Errorf("error reading credentials file: %v", err)
	}
	var creds providersFile
	if err = json.Unmarshal(b, &creds); err != nil {
		return fmt.Errorf("error decoding credential: %w", err)
	}
	if len(creds.Seed) == 0 {
		return errors.New("no seed found in credentials file")
	}
	seed2 := sha256.Sum256(creds.Seed)
	testnetWalletSeed = hex.EncodeToString(creds.Seed)
	testnetParticipantWalletSeed = hex.EncodeToString(seed2[:])
	rpcProviders = creds.Providers["eth"][dex.Testnet.String()]
	return nil
}

func TestMain(m *testing.M) {
	rand.Seed(time.Now().UnixNano())
	dexeth.MaybeReadSimnetAddrs()

	flag.BoolVar(&isTestnet, "testnet", false, "use testnet")
	flag.BoolVar(&v1, "v1", true, "Use Version 1 contract")
	flag.Parse()

	if v1 {
		contractVer = 1
	}

	ethGases = dexeth.VersionedGases[contractVer]

	if isTestnet {
		tmpDir, err := os.MkdirTemp("", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating temporary directory: %v", err)
			os.Exit(1)
		}
		testnetWalletDir = filepath.Join(tmpDir, "initiator")
		defer os.RemoveAll(testnetWalletDir)
		testnetParticipantWalletDir = filepath.Join(tmpDir, "participant")
		defer os.RemoveAll(testnetParticipantWalletDir)
		if err := useTestnet(); err != nil {
			fmt.Fprintf(os.Stderr, "error loading testnet: %v", err)
			os.Exit(1)
		}
	}

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

func setupWallet(walletDir, seed, listenAddress string, providers []string, net dex.Network) error {
	walletType := walletTypeRPC
	settings := map[string]string{
		providersKey: strings.Join(providers, " "),
	}
	seedB, _ := hex.DecodeString(seed)
	createWalletParams := asset.CreateWalletParams{
		Type:     walletType,
		Seed:     seedB,
		Pass:     []byte(pw),
		Settings: settings,
		DataDir:  walletDir,
		Net:      net,
		Logger:   tLogger,
	}
	compat, err := NetworkCompatibilityData(net)
	if err != nil {
		return err
	}
	return CreateEVMWallet(dexeth.ChainIDs[net], &createWalletParams, &compat, true)
}

func prepareTokenClients(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatalf("initiator unlock error; %v", err)
	}
	txOpts, err := ethClient.txOpts(ctx, 0, tokenGases.Approve, nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}
	var tx1, tx2 *types.Transaction
	if tx1, err = simnetTokenContractor.approve(txOpts, unlimitedAllowance); err != nil {
		t.Fatalf("initiator approveToken error: %v", err)
	}
	err = participantEthClient.unlock(pw)
	if err != nil {
		t.Fatalf("participant unlock error; %v", err)
	}

	txOpts, err = participantEthClient.txOpts(ctx, 0, tokenGases.Approve, nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}
	if tx2, err = participantTokenContractor.approve(txOpts, unlimitedAllowance); err != nil {
		t.Fatalf("participant approveToken error: %v", err)
	}

	if err := waitForMined(); err != nil {
		t.Fatalf("unexpected error while waiting to mine approval block: %v", err)
	}

	_, err = waitForReceipt(ethClient, tx1)
	if err != nil {
		t.Fatal(err)
	}
	// spew.Dump(receipt1)

	_, err = waitForReceipt(participantEthClient, tx2)
	if err != nil {
		t.Fatal(err)
	}
	// spew.Dump(receipt2)
}

func syncClient(cl ethFetcher) error {
	giveUpAt := 60
	if isTestnet {
		giveUpAt = 10000
	}
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		prog, tipTime, err := cl.syncProgress(ctx)
		if err != nil {
			return err
		}
		if isTestnet {
			timeDiff := time.Now().Unix() - int64(tipTime)
			if timeDiff < dexeth.MaxBlockInterval {
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
	if !t.Run("testAddressesHaveFunds", testAddressesHaveFundsFn(100_000 /* gwei */)) {
		t.Fatal("not enough funds")
	}
	t.Run("testBestHeader", testBestHeader)
	t.Run("testPendingTransactions", testPendingTransactions)
	t.Run("testHeaderByHash", testHeaderByHash)
	t.Run("testTransactionReceipt", testTransactionReceipt)
}

func TestPeering(t *testing.T) {
	t.Run("testSyncProgress", testSyncProgress)
}

func TestAccount(t *testing.T) {
	if !t.Run("testAddressesHaveFunds", testAddressesHaveFundsFn(10_000_000 /* gwei */)) {
		t.Fatal("not enough funds")
	}
	t.Run("testAddressBalance", testAddressBalance)
	t.Run("testSendTransaction", testSendTransaction)
	t.Run("testSendSignedTransaction", testSendSignedTransaction)
	t.Run("testSignMessage", testSignMessage)
}

// TestContract tests methods that interact with the contract.
func TestContract(t *testing.T) {
	if !t.Run("testAddressesHaveFunds", testAddressesHaveFundsFn(100_000_000 /* gwei */)) {
		t.Fatal("not enough funds")
	}
	// t.Run("testSwap", func(t *testing.T) { testSwap(t, BipID) }) // TODO: Replace with testStatusAndVector?
	t.Run("testInitiate", func(t *testing.T) { testInitiate(t, BipID) })
	t.Run("testRedeem", func(t *testing.T) { testRedeem(t, BipID) })
	t.Run("testRefund", func(t *testing.T) { testRefund(t, BipID) })
}

func TestGas(t *testing.T) {
	// t.Run("testInitiateGas", func(t *testing.T) { testInitiateGas(t, BipID) })
	t.Run("testRedeemGas", func(t *testing.T) { testRedeemGas(t, BipID) })
	t.Run("testRefundGas", func(t *testing.T) { testRefundGas(t, BipID) })
}

func TestTokenContract(t *testing.T) {
	// t.Run("testTokenSwap", func(t *testing.T) { testSwap(t, usdcID) }) // TODO: Replace with testTokenStatusAndVector?
	t.Run("testInitiateToken", func(t *testing.T) { testInitiate(t, usdcID) })
	t.Run("testRedeemToken", func(t *testing.T) { testRedeem(t, usdcID) })
	t.Run("testRefundToken", func(t *testing.T) { testRefund(t, usdcID) })
}

func TestTokenGas(t *testing.T) {
	t.Run("testTransferGas", testTransferGas)
	t.Run("testApproveGas", testApproveGas)
	// t.Run("testInitiateTokenGas", func(t *testing.T) { testInitiateGas(t, usdcID) })
	t.Run("testRedeemTokenGas", func(t *testing.T) { testRedeemGas(t, usdcID) })
	t.Run("testRefundTokenGas", func(t *testing.T) { testRefundGas(t, usdcID) })
}

func TestTokenAccess(t *testing.T) {
	t.Run("testTokenBalance", testTokenBalance)
	t.Run("testApproveAllowance", testApproveAllowance)
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
		t.Fatalf("error getting initiator balance: %v", err)
	}
	if bal == nil {
		t.Fatalf("empty balance")
	}
	fmt.Printf("Initiator balance: %.9f ETH \n", float64(dexeth.WeiToGwei(bal))/dexeth.GweiFactor)
	bal, err = participantEthClient.addressBalance(ctx, participantAddr)
	if err != nil {
		t.Fatalf("error getting participant balance: %v", err)
	}
	fmt.Printf("Participant balance: %.9f ETH \n", float64(dexeth.WeiToGwei(bal))/dexeth.GweiFactor)
}

func testTokenBalance(t *testing.T) {
	bal, err := simnetTokenContractor.balance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bal == nil {
		t.Fatalf("empty balance")
	}

	fmt.Println("### Balance:", simnetAddr, stringifyTokenBalance(t, bal))
}

func stringifyTokenBalance(t *testing.T, evmBal *big.Int) string {
	t.Helper()
	atomicBal := masterToken.EVMToAtomic(evmBal)
	ui, err := asset.UnitInfo(usdcID)
	if err != nil {
		t.Fatalf("cannot get unit info: %v", err)
	}
	prec := math.Round(math.Log10(float64(ui.Conventional.ConversionFactor)))
	return strconv.FormatFloat(float64(atomicBal)/float64(ui.Conventional.ConversionFactor), 'f', int(prec), 64)
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

	txOpts, err := ethClient.txOpts(ctx, 1, defaultSendGasLimit, nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}

	tx, err := ethClient.sendTransaction(ctx, txOpts, participantAddr, nil)
	if err != nil {
		t.Fatal(err)
	}

	txHash = tx.Hash()

	confs, err := ethClient.transactionConfirmations(ctx, txHash)
	// CoinNotFoundError OK for RPC wallet until mined.
	if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("transactionConfirmations error: %v", err)
	}
	if confs != 0 {
		t.Fatalf("%d confs reported for unmined transaction", confs)
	}

	spew.Dump(tx)
	if err := waitForMined(); err != nil {
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

func testHeaderByHash(t *testing.T) {
	// Checking a random hash should result in no header.
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	_, err := ethClient.headerByHash(ctx, txHash)
	if err == nil {
		t.Fatal("expected header not found error")
	}

	bestHdr, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}

	txHash = bestHdr.Hash()

	hdr, err := ethClient.headerByHash(ctx, txHash)
	if err != nil {
		t.Fatal(err)
	}
	hdrHash := hdr.Hash()
	if !bytes.Equal(hdrHash[:], txHash[:]) {
		t.Fatal("hashes not equal")
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
	c := ethClient.(*multiRPCClient)
	var nonce uint64
	var chainID *big.Int
	var ks *keystore.KeyStore
	n, _, err := ethClient.nonce(ctx)
	if err != nil {
		t.Fatalf("error getting nonce: %v", err)
	}
	nonce = n.Uint64()
	ks = c.creds.ks
	chainID = c.chainID

	tx := types.NewTx(&types.DynamicFeeTx{
		To:        &simnetAddr,
		ChainID:   chainID,
		Nonce:     nonce,
		Gas:       21000,
		GasFeeCap: dexeth.GweiToWei(maxFeeRate),
		GasTipCap: dexeth.GweiToWei(2),
		Value:     dexeth.GweiToWei(1),
		Data:      []byte{},
	})
	tx, err = ks.SignTx(*simnetAcct, tx, chainID)
	if err != nil {
		t.Fatal(err)
	}

	err = ethClient.sendSignedTransaction(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}

	txHash = tx.Hash()

	confs, err := ethClient.transactionConfirmations(ctx, txHash)
	// CoinNotFoundError OK for RPC wallet until mined.
	if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("transactionConfirmations error: %v", err)
	}
	if confs != 0 {
		t.Fatalf("%d confs reported for unmined transaction", confs)
	}

	spew.Dump(tx)
	if err := waitForMined(); err != nil {
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
	txOpts, err := ethClient.txOpts(ctx, 1, defaultSendGasLimit, nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}
	tx, err := ethClient.sendTransaction(ctx, txOpts, simnetAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForMined(); err != nil {
		t.Fatal(err)
	}
	receipt, err := waitForReceipt(ethClient, tx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(receipt)
}

func testPendingTransactions(t *testing.T) {
	mf, is := ethClient.(txPoolFetcher)
	if !is {
		return
	}
	txs, err := mf.pendingTransactions()
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty.
	spew.Dump(txs)
}

func testSyncProgress(t *testing.T) {
	p, _, err := ethClient.syncProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(p)
}

func testInitiateGas(t *testing.T, assetID uint32) {
	gases := ethGases
	c := simnetContractor
	if assetID != BipID {
		c = simnetTokenContractor
		prepareTokenClients(t)
		gases = tokenGases
	}

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
		if actualGas > expectedGas || actualGas < expectedGas/2 {
			t.Fatalf("Expected incremental gas for %d initiations to be close to %d but got %d",
				i, expectedGas, actualGas)
		}

		fmt.Printf("Gas used for batch initiating %v swaps: %v. %v more than previous \n", i, gas, gas-previousGas)
		previousGas = gas
	}
}

// feesAtBlk calculates the gas fee at blkNum. This adds the base fee at blkNum
// to a minimum gas tip cap.
func feesAtBlk(ctx context.Context, n ethFetcher, blkNum int64) (fees *big.Int, err error) {
	hdr, err := n.(*multiRPCClient).HeaderByNumber(ctx, big.NewInt(blkNum))
	if err != nil {
		return nil, err
	}

	minGasTipCapWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
	tip := new(big.Int).Set(minGasTipCapWei)

	return tip.Add(tip, hdr.BaseFee), nil
}

// initiateOverflow is just like *contractorV0.initiate but sets the first swap
// value to a max uint256 minus one emv unit.
func initiateOverflow(c *contractorV0, txOpts *bind.TransactOpts, contracts []*asset.Contract) (*types.Transaction, error) {
	inits := make([]swapv0.ETHSwapInitiation, 0, len(contracts))
	secrets := make(map[[32]byte]bool, len(contracts))

	for i, contract := range contracts {
		if len(contract.SecretHash) != dexeth.SecretHashSize {
			return nil, fmt.Errorf("wrong secret hash length. wanted %d, got %d", dexeth.SecretHashSize, len(contract.SecretHash))
		}

		var secretHash [32]byte
		copy(secretHash[:], contract.SecretHash)

		if secrets[secretHash] {
			return nil, fmt.Errorf("secret hash %s is a duplicate", contract.SecretHash)
		}
		secrets[secretHash] = true

		if !common.IsHexAddress(contract.Address) {
			return nil, fmt.Errorf("%q is not an address", contract.Address)
		}

		val := c.evmify(contract.Value)
		if i == 0 {
			val = big.NewInt(2)
			val.Exp(val, big.NewInt(256), nil)
			val.Sub(val, c.evmify(1))
		}
		inits = append(inits, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(int64(contract.LockTime)),
			SecretHash:      secretHash,
			Participant:     common.HexToAddress(contract.Address),
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
	evmify := dexeth.GweiToWei
	if !isETH {
		sc = simnetTokenContractor
		balance = func() (*big.Int, error) {
			return simnetTokenContractor.balance(ctx)
		}
		gases = tokenGases
		switch contractVer {
		case 0:
			evmify = sc.(*tokenContractorV0).evmify
		case 1:
			evmify = sc.(*tokenContractorV1).evmify
		}
	}

	// Create a slice of random secret hashes that can be used in the tests and
	// make sure none of them have been used yet.
	numSecretHashes := 10
	secretHashes := make([][32]byte, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		secretHashes[i] = bytesToArray(encode.RandomBytes(32))
	}

	now := uint64(time.Now().Unix())

	tests := []struct {
		name              string
		swaps             []*asset.Contract
		success, overflow bool
		swapErr           bool
	}{
		{
			name:    "1 swap ok",
			success: true,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[0], 2),
			},
		},
		{
			name:    "1 duplicate swap",
			success: false,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[0], 2),
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
		{
			// Preventing this used to need explicit checks before solidity 0.8, but now the
			// compiler checks for integer overflows by default.
			name:     "value addition overflows",
			success:  false,
			overflow: true,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[5], 0), // Will be set to max uint256 - 1 evm unit
				newContract(now, secretHashes[6], 3),
			},
		},
		{
			name:    "swap with 0 value",
			success: false,
			swaps: []*asset.Contract{
				newContract(now, secretHashes[7], 0),
				newContract(now, secretHashes[8], 1),
			},
		},
	}

	for _, test := range tests {
		var originalParentBal *big.Int

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("balance error for asset %d, test %s: %v", assetID, test.name, err)
		}

		if !isETH {
			originalParentBal, err = ethClient.addressBalance(ctx, ethClient.address())
			if err != nil {
				t.Fatalf("balance error for eth, test %s: %v", test.name, err)
			}
		}

		var totalVal uint64
		originalStates := make(map[string]dexeth.SwapStep)
		for _, tSwap := range test.swaps {
			status, _, err := sc.statusAndVector(ctx, acLocator(tSwap))
			if err != nil {
				t.Fatalf("%s: swap error: %v", test.name, err)
			}
			originalStates[tSwap.SecretHash.String()] = status.Step
			totalVal += tSwap.Value
		}

		optsVal := totalVal
		if test.overflow {
			optsVal = 2
		} else if !isETH {
			optsVal = 0
		}

		expGas := gases.SwapN(len(test.swaps))
		txOpts, err := ethClient.txOpts(ctx, optsVal, expGas, dexeth.GweiToWei(maxFeeRate), nil, nil)
		if err != nil {
			t.Fatalf("%s: txOpts error: %v", test.name, err)
		}
		var tx *types.Transaction
		if test.overflow && !v1 { // We're limited by uint64 in v1
			switch c := sc.(type) {
			case *contractorV0:
				tx, err = initiateOverflow(c, txOpts, test.swaps)
			case *tokenContractorV0:
				tx, err = initiateOverflow(c.contractorV0, txOpts, test.swaps)
			}
		} else {
			tx, err = sc.initiate(txOpts, test.swaps)
		}
		if err != nil {
			if test.swapErr {
				continue
			}
			t.Fatalf("%s: initiate error: %v", test.name, err)
		}

		if err := waitForMined(); err != nil {
			t.Fatalf("%s: post-initiate mining error: %v", test.name, err)
		}

		// It appears the receipt is only accessible after the tx is mined.
		receipt, err := waitForReceipt(ethClient, tx)
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
			wantBal.Sub(wantBal, evmify(totalVal))
		}
		bal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		if isETH {
			wantBal = new(big.Int).Sub(wantBal, txFee)
		} else {
			parentBal, err := ethClient.addressBalance(ctx, ethClient.address())
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.Cmp(big.NewInt(0)) != 0 {
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %d",
					test.name, wantParentBal, parentBal, diff)
			}
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.Cmp(big.NewInt(0)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d units, diff = %d units",
				test.name, wantBal, bal, diff)
		}

		for _, tSwap := range test.swaps {
			status, _, err := sc.statusAndVector(ctx, acLocator(tSwap))
			if err != nil {
				t.Fatalf("%s: swap error post-init: %v", test.name, err)
			}

			if test.success && status.Step != dexeth.SSInitiated {
				t.Fatalf("%s: wrong success swap state: want %s got %s", test.name, dexeth.SSInitiated, status.Step)
			}

			originalState := originalStates[hex.EncodeToString(tSwap.SecretHash[:])]
			if !test.success && status.Step != originalState {
				t.Fatalf("%s: wrong error swap state: want %s got %s", test.name, originalState, status.Step)
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

	swaps := make([]*asset.Contract, 0, numSwaps)
	locators := make([][]byte, 0, numSwaps)
	for i := 0; i < numSwaps; i++ {
		c := newContract(now, secretHashes[i], 1)
		swaps = append(swaps, c)
		locators = append(locators, acLocator(c))
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

	txOpts, err := ethClient.txOpts(ctx, optsVal, gases.SwapN(len(swaps)), dexeth.GweiToWei(maxFeeRate), nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}
	tx, err := c.initiate(txOpts, swaps)
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}
	receipt, err := waitForReceipt(ethClient, tx)
	if err != nil {
		t.Fatalf("failed retrieving initiate receipt: %v", err)
	}
	spew.Dump(receipt)

	err = checkTxStatus(receipt, txOpts.GasLimit)
	if err != nil {
		t.Fatalf("failed init transaction status: %v", err)
	}

	// Make sure swaps were properly initiated
	for i := range swaps {
		status, _, err := c.statusAndVector(ctx, locators[i])
		if err != nil {
			t.Fatal("unable to get swap state")
		}
		if status.Step != dexeth.SSInitiated {
			t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, status.Step)
		}
	}

	// Test gas usage of redeem function
	var previous uint64
	for i := 0; i < numSwaps; i++ {
		gas, err := pc.estimateRedeemGas(ctx, secrets[:i+1], locators[:i+1])
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
		if actualGas > expectedGas || actualGas < (expectedGas/2) { // Use GetGasEstimates to better precision estimates.
			t.Fatalf("Expected incremental gas for %d redemptions to be close to %d but got %d",
				i+1, expectedGas, actualGas)
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
	evmify := dexeth.GweiToWei
	if !isETH {
		gases = tokenGases
		c, pc = simnetTokenContractor, participantTokenContractor
		tc := c.(*tokenContractorV0)
		evmify = tc.evmify
	}

	const val = 1

	tests := []struct {
		name               string
		sleepNBlocks       int
		redeemerClient     ethFetcher
		redeemer           *accounts.Account
		redeemerContractor contractor
		swaps              []*asset.Contract
		redemptions        []*asset.Redemption
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
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[0], val)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[0], secretHashes[0], val, lockTime)},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "ok two before locktime",
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps: []*asset.Contract{
				newContract(lockTime, secretHashes[1], val),
				newContract(lockTime, secretHashes[2], val),
			},
			redemptions: []*asset.Redemption{
				newRedeem(secrets[1], secretHashes[1], val, lockTime),
				newRedeem(secrets[2], secretHashes[2], val, lockTime),
			},
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
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[3], val)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[3], secretHashes[3], val, lockTime)},
			finalStates:        []dexeth.SwapStep{dexeth.SSRedeemed},
			addAmt:             true,
		},
		{
			name:               "bad redeemer",
			sleepNBlocks:       8,
			redeemerClient:     ethClient,
			redeemer:           simnetAcct,
			redeemerContractor: c,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[4], val)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[4], secretHashes[4], val, lockTime)},
			finalStates:        []dexeth.SwapStep{dexeth.SSInitiated},
			addAmt:             false,
		},
		{
			name:               "bad secret",
			expectRedeemErr:    true,
			sleepNBlocks:       8,
			redeemerClient:     participantEthClient,
			redeemer:           participantAcct,
			redeemerContractor: pc,
			swaps:              []*asset.Contract{newContract(lockTime, secretHashes[5], val)},
			redemptions:        []*asset.Redemption{newRedeem(secrets[6], secretHashes[5], val, lockTime)},
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
			swaps: []*asset.Contract{
				newContract(lockTime, secretHashes[7], val),
				newContract(lockTime, secretHashes[8], val),
			},
			redemptions: []*asset.Redemption{
				newRedeem(secrets[7], secretHashes[7], val, lockTime),
				newRedeem(secrets[7], secretHashes[7], val, lockTime),
			},
			finalStates: []dexeth.SwapStep{
				dexeth.SSInitiated,
				dexeth.SSInitiated,
			},
			addAmt: false,
		},
	}

	for _, test := range tests {
		var optsVal uint64
		locators := make([][]byte, 0, len(test.swaps))
		for _, contract := range test.swaps {
			locator := acLocator(contract)
			locators = append(locators, locator)
			status, _, err := c.statusAndVector(ctx, locator)
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			if status.Step != dexeth.SSNone {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, status.Step)
			}
			if isETH {
				optsVal += contract.Value
			}
		}

		balance := func() (*big.Int, error) {
			return test.redeemerClient.addressBalance(ctx, test.redeemerClient.address())
		}
		if !isETH {
			balance = func() (*big.Int, error) {
				return test.redeemerContractor.(tokenContractor).balance(ctx)
			}
		}

		txOpts, err := test.redeemerClient.txOpts(ctx, optsVal, gases.SwapN(len(test.swaps)), dexeth.GweiToWei(maxFeeRate), nil, nil)
		if err != nil {
			t.Fatalf("%s: txOpts error: %v", test.name, err)
		}
		tx, err := c.initiate(txOpts, test.swaps)
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		// This waitForMined will always take test.sleepNBlocks to complete.
		if err := waitForMined(); err != nil {
			t.Fatalf("%s: post-init mining error: %v", test.name, err)
		}

		receipt, err := waitForReceipt(test.redeemerClient, tx)
		if err != nil {
			t.Fatalf("%s: failed to get init receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		err = checkTxStatus(receipt, txOpts.GasLimit)
		if err != nil {
			t.Fatalf("%s: failed init transaction status: %v", test.name, err)
		}
		fmt.Printf("Gas used for %d inits: %d \n", len(test.swaps), receipt.GasUsed)

		for i := range test.swaps {
			status, _, err := test.redeemerContractor.statusAndVector(ctx, locators[i])
			if err != nil {
				t.Fatal("unable to get swap state")
			}
			if status.Step != dexeth.SSInitiated {
				t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSInitiated, status.Step)
			}
		}

		var originalParentBal *big.Int
		if !isETH {
			originalParentBal, err = test.redeemerClient.addressBalance(ctx, test.redeemerClient.address())
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
		}

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		expGas := gases.RedeemN(len(test.redemptions))
		txOpts, err = test.redeemerClient.txOpts(ctx, 0, expGas, dexeth.GweiToWei(maxFeeRate), nil, nil)
		if err != nil {
			t.Fatalf("%s: txOpts error: %v", test.name, err)
		}
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

		if err := waitForMined(); err != nil {
			t.Fatalf("%s: post-redeem mining error: %v", test.name, err)
		}

		receipt, err = waitForReceipt(test.redeemerClient, tx)
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
			wantBal.Add(wantBal, evmify(uint64(len(test.redemptions))))
		}

		if isETH {
			wantBal.Sub(wantBal, txFee)
		} else {
			parentBal, err := test.redeemerClient.addressBalance(ctx, test.redeemerClient.address())
			if err != nil {
				t.Fatalf("%s: post-redeem eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.Cmp(big.NewInt(0)) != 0 {
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %d",
					test.name, wantParentBal, parentBal, diff)
			}
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.Cmp(big.NewInt(0)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, wantBal, bal, diff)
		}

		for i := range test.redemptions {
			status, _, err := c.statusAndVector(ctx, locators[i])
			if err != nil {
				t.Fatalf("unexpected error for test %v: %v", test.name, err)
			}
			if status.Step != test.finalStates[i] {
				t.Fatalf("unexpected swap state for test %v [%d]: want %s got %s",
					test.name, i, test.finalStates[i], status.Step)
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

	txOpts, err := ethClient.txOpts(ctx, optsVal, gases.SwapN(1), nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}
	ac := newContract(lockTime, secretHash, 1)
	locator := acLocator(ac)
	_, err = c.initiate(txOpts, []*asset.Contract{ac})
	if err != nil {
		t.Fatalf("Unable to initiate swap: %v ", err)
	}
	if err := waitForMined(); err != nil {
		t.Fatalf("unexpected error while waiting to mine: %v", err)
	}

	status, _, err := c.statusAndVector(ctx, locator)
	if err != nil {
		t.Fatal("unable to get swap state")
	}
	if status.Step != dexeth.SSInitiated {
		t.Fatalf("unexpected swap state: want %s got %s", dexeth.SSInitiated, status.Step)
	}

	gas, err := c.estimateRefundGas(ctx, locator)
	if err != nil {
		t.Fatalf("Error estimating gas for refund function: %v", err)
	}
	if isETH {
		expGas := gases.Refund
		if gas > expGas || gas < expGas/2 {
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
	evmify := dexeth.GweiToWei
	if !isETH {
		gases = tokenGases
		c, pc = simnetTokenContractor, participantTokenContractor
		tc := c.(*tokenContractorV0)
		evmify = tc.evmify
	}
	tests := []struct {
		name                         string
		refunder                     *accounts.Account
		refunderClient               ethFetcher
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
		ac := newContract(inLocktime, secretHash, amt)
		locator := acLocator(ac)

		status, _, err := test.refunderContractor.statusAndVector(ctx, locator)
		if err != nil {
			t.Fatalf("%s: unable to get swap state pre-init", test.name)
		}
		if status.Step != dexeth.SSNone {
			t.Fatalf("unexpected swap state for test %v: want %s got %s", test.name, dexeth.SSNone, status.Step)
		}

		txOpts, err := ethClient.txOpts(ctx, optsVal, gases.SwapN(1), nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: txOpts error: %v", test.name, err)
		}
		_, err = c.initiate(txOpts, []*asset.Contract{ac})
		if err != nil {
			t.Fatalf("%s: initiate error: %v ", test.name, err)
		}

		if test.redeem {
			if err := waitForMined(); err != nil {
				t.Fatalf("%s: pre-redeem mining error: %v", test.name, err)
			}

			txOpts, err = participantEthClient.txOpts(ctx, 0, gases.RedeemN(1), nil, nil, nil)
			if err != nil {
				t.Fatalf("%s: txOpts error: %v", test.name, err)
			}
			_, err := pc.redeem(txOpts, []*asset.Redemption{newRedeem(secret, secretHash, amt, inLocktime)})
			if err != nil {
				t.Fatalf("%s: redeem error: %v", test.name, err)
			}
		}

		// This waitForMined will always take test.sleep to complete.
		if err := waitForMined(); err != nil {
			t.Fatalf("unexpected post-init mining error for test %v: %v", test.name, err)
		}

		var originalParentBal *big.Int
		if !isETH {
			originalParentBal, err = test.refunderClient.addressBalance(ctx, test.refunderClient.address())
			if err != nil {
				t.Fatalf("%s: eth balance error: %v", test.name, err)
			}
		}

		originalBal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		isRefundable, err := test.refunderContractor.isRefundable(locator)
		if err != nil {
			t.Fatalf("%s: isRefundable error %v", test.name, err)
		}
		if isRefundable != test.isRefundable {
			t.Fatalf("%s: expected isRefundable=%v, but got %v",
				test.name, test.isRefundable, isRefundable)
		}

		txOpts, err = test.refunderClient.txOpts(ctx, 0, gases.Refund, dexeth.GweiToWei(maxFeeRate), nil, nil)
		if err != nil {
			t.Fatalf("%s: txOpts error: %v", test.name, err)
		}
		tx, err := test.refunderContractor.refund(txOpts, locator)
		if err != nil {
			t.Fatalf("%s: refund error: %v", test.name, err)
		}
		spew.Dump(tx)

		in, _, err := test.refunderContractor.value(ctx, tx)
		if err != nil {
			t.Fatalf("%s: error finding in value: %v", test.name, err)
		}

		if test.addAmt && in != amt {
			t.Fatalf("%s: unexpected pending in balance %d", test.name, in)
		}

		if err := waitForMined(); err != nil {
			t.Fatalf("%s: post-refund mining error: %v", test.name, err)
		}

		receipt, err := waitForReceipt(test.refunderClient, tx)
		if err != nil {
			t.Fatalf("%s: failed to get refund receipt: %v", test.name, err)
		}
		spew.Dump(receipt)

		err = checkTxStatus(receipt, txOpts.GasLimit)
		// test.addAmt being true indicates the refund should succeed.
		if err != nil && test.addAmt {
			t.Fatalf("%s: failed refund transaction status: %v", test.name, err)
		}
		fmt.Printf("Gas used for refund, success = %t: %d\n", test.addAmt, receipt.GasUsed)

		// Balance should increase or decrease by a certain amount
		// depending on whether refund completed successfully on-chain.
		// If unsuccessful the fee is subtracted. If successful, amt is
		// added.
		gasPrice, err := feesAtBlk(ctx, ethClient, receipt.BlockNumber.Int64())
		if err != nil {
			t.Fatalf("%s: feesAtBlk error: %v", test.name, err)
		}
		fmt.Printf("Gas price for refund: %d\n", gasPrice)
		bigGasUsed := new(big.Int).SetUint64(receipt.GasUsed)
		txFee := new(big.Int).Mul(bigGasUsed, gasPrice)

		wantBal := new(big.Int).Set(originalBal)
		if test.addAmt {
			wantBal.Add(wantBal, evmify(amt))
		}

		if isETH {
			wantBal.Sub(wantBal, txFee)
		} else {
			parentBal, err := test.refunderClient.addressBalance(ctx, test.refunderClient.address())
			if err != nil {
				t.Fatalf("%s: post-refund eth balance error: %v", test.name, err)
			}
			wantParentBal := new(big.Int).Sub(originalParentBal, txFee)
			diff := new(big.Int).Sub(wantParentBal, parentBal)
			if diff.Cmp(big.NewInt(0)) != 0 {
				t.Fatalf("%s: unexpected parent chain balance change: want %d got %d, diff = %d",
					test.name, wantParentBal, parentBal, diff)
			}
		}

		bal, err := balance()
		if err != nil {
			t.Fatalf("%s: balance error: %v", test.name, err)
		}

		diff := new(big.Int).Sub(wantBal, bal)
		if diff.Cmp(big.NewInt(0)) != 0 {
			t.Fatalf("%s: unexpected balance change: want %d got %d, diff = %d",
				test.name, wantBal, bal, diff)
		}

		status, _, err = test.refunderContractor.statusAndVector(ctx, locator)
		if err != nil {
			t.Fatalf("%s: post-refund swap error: %v", test.name, err)
		}
		if status.Step != test.finalState {
			t.Fatalf("%s: wrong swap state: want %s got %s", test.name, test.finalState, status.Step)
		}
	}
}

func testApproveAllowance(t *testing.T) {
	err := ethClient.unlock(pw)
	if err != nil {
		t.Fatal(err)
	}

	txOpts, err := ethClient.txOpts(ctx, 0, tokenGases.Approve, nil, nil, nil)
	if err != nil {
		t.Fatalf("txOpts error: %v", err)
	}

	if _, err = simnetTokenContractor.approve(txOpts, unlimitedAllowance); err != nil {
		t.Fatalf("initiator approveToken error: %v", err)
	}

	if err := waitForMined(); err != nil {
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
	gas, err := simnetTokenContractor.estimateTransferGas(ctx, simnetTokenContractor.(*tokenContractorV0).contractorV0.evmify(1))
	if err != nil {
		t.Fatalf("estimateTransferGas error: %v", err)
	}
	fmt.Printf("=========== gas for transfer: %d ==============\n", gas)
}

func testApproveGas(t *testing.T) {
	gas, err := simnetTokenContractor.estimateApproveGas(ctx, simnetTokenContractor.(*tokenContractorV0).contractorV0.evmify(1))
	if err != nil {
		t.Fatalf("")
	}
	fmt.Printf("=========== gas for approve: %d ==============\n", gas)
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

	walletDir := filepath.Join(dcrutil.AppDataDir("bisonw", false), "simnet", "assetdb", dex.BipIDSymbol(60))
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	runSimnetMiner(ctx, "eth", tLogger)
	prepareTokenClients(t)
	tLogger.SetLevel(dex.LevelInfo)
	if err := getGasEstimates(ctx, ethClient, participantEthClient, simnetTokenContractor, participantTokenContractor, 5, contractVer, tokenGases, dexeth.GweiToWei, tLogger); err != nil {
		t.Fatalf("getGasEstimates error: %v", err)
	}
}

func TestConfirmedNonce(t *testing.T) {
	_, err := ethClient.getConfirmedNonce(ctx)
	if err != nil {
		t.Fatalf("getConfirmedNonce error: %v", err)
	}
}

func bytesToArray(b []byte) (a [32]byte) {
	copy(a[:], b)
	return
}

func bigUint(v uint64) *big.Int {
	return new(big.Int).SetUint64(v)
}
