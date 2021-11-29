//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	srveth "decred.org/dcrdex/server/asset/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)

	testAddressA = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	testAddressB = common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	testAddressC = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
)

type initTx struct {
	hash        common.Hash
	opts        *bind.TransactOpts
	initiations []dexeth.ETHSwapInitiation
}

type testNode struct {
	connectErr          error
	bestHdr             *types.Header
	bestHdrErr          error
	bestBlkHash         common.Hash
	bestBlkHashErr      error
	blk                 *types.Block
	blkErr              error
	blkNum              uint64
	blkNumErr           error
	syncProg            *ethereum.SyncProgress
	syncProgErr         error
	peerInfo            []*p2p.PeerInfo
	peersErr            error
	bal                 *big.Int
	balErr              error
	signDataErr         error
	privKeyForSigning   *ecdsa.PrivateKey
	initErr             error
	nonce               uint64
	lastInitiation      initTx
	pendingTxs          []*types.Transaction
	pendingTxsErr       error
	suggestedGasTipCap  *big.Int
	suggestGasTipCapErr error
	swapMap             map[[32]byte]dexeth.ETHSwapSwap
	swapErr             error
}

func (n *testNode) connect(ctx context.Context, node *node.Node, addr *common.Address) error {
	return n.connectErr
}
func (n *testNode) shutdown() {}
func (n *testNode) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.bestHdr, n.bestHdrErr
}
func (n *testNode) bestBlockHash(ctx context.Context) (common.Hash, error) {
	return n.bestBlkHash, n.bestBlkHashErr
}
func (n *testNode) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return n.blk, n.blkErr
}
func (n *testNode) accounts() []*accounts.Account {
	return nil
}
func (n *testNode) balance(ctx context.Context, acct *common.Address) (*big.Int, error) {
	balCopy := new(big.Int)
	balCopy.Set(n.bal)
	return balCopy, n.balErr
}
func (n *testNode) sendTransaction(ctx context.Context, tx map[string]string) (common.Hash, error) {
	return common.Hash{}, nil
}
func (n *testNode) unlock(ctx context.Context, pw string, acct *accounts.Account) error {
	return nil
}
func (n *testNode) lock(ctx context.Context, acct *accounts.Account) error {
	return nil
}
func (n *testNode) listWallets(ctx context.Context) ([]rawWallet, error) {
	return nil, nil
}
func (n *testNode) addPeer(ctx context.Context, peer string) error {
	return nil
}
func (n *testNode) nodeInfo(ctx context.Context) (*p2p.NodeInfo, error) {
	return nil, nil
}
func (n *testNode) blockNumber(ctx context.Context) (uint64, error) {
	return n.blkNum, n.blkNumErr
}
func (n *testNode) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return n.syncProg, n.syncProgErr
}
func (n *testNode) pendingTransactions(ctx context.Context) ([]*types.Transaction, error) {
	if n.pendingTxsErr != nil {
		return nil, n.pendingTxsErr
	}
	return n.pendingTxs, nil
}

// initiate is not concurrent safe
func (n *testNode) initiate(opts *bind.TransactOpts, netID int64, initiations []dexeth.ETHSwapInitiation) (*types.Transaction, error) {
	if n.initErr != nil {
		return nil, n.initErr
	}
	baseTx := &types.DynamicFeeTx{
		Nonce:     n.nonce,
		GasFeeCap: opts.GasFeeCap,
		GasTipCap: opts.GasTipCap,
		Gas:       opts.GasLimit,
		Value:     opts.Value,
		Data:      []byte{},
	}
	tx := types.NewTx(baseTx)
	n.nonce++
	n.lastInitiation = initTx{
		initiations: initiations,
		hash:        tx.Hash(),
		opts:        opts,
	}
	return tx, nil
}
func (n *testNode) redeem(opts *bind.TransactOpts, netID int64, redemptions []dexeth.ETHSwapRedemption) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) refund(opts *bind.TransactOpts, netID int64, secretHash [32]byte) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) swap(ctx context.Context, from *accounts.Account, secretHash [32]byte) (*dexeth.ETHSwapSwap, error) {
	if n.swapErr != nil {
		return nil, n.swapErr
	}
	swap, ok := n.swapMap[secretHash]
	if !ok {
		return nil, errors.New("swap not in map")
	}
	return &swap, nil
}
func (n *testNode) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, nil
}
func (n *testNode) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	return n.peerInfo, n.peersErr
}
func (n *testNode) suggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if n.suggestGasTipCapErr != nil {
		return nil, n.suggestGasTipCapErr
	}
	return n.suggestedGasTipCap, nil
}
func (n *testNode) signData(addr common.Address, data []byte) ([]byte, error) {
	if n.signDataErr != nil {
		return nil, n.signDataErr
	}

	if n.privKeyForSigning == nil {
		return nil, nil
	}

	return crypto.Sign(crypto.Keccak256(data), n.privKeyForSigning)
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		network dex.Network
		wantErr bool
	}{{
		name:    "ok",
		network: dex.Simnet,
	}, {
		name:    "mainnet not allowed",
		network: dex.Mainnet,
		wantErr: true,
	}}

	for _, test := range tests {
		_, err := loadConfig(nil, test.network)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
	}
}

func TestCheckForNewBlocks(t *testing.T) {
	header0 := &types.Header{Number: big.NewInt(0)}
	block0 := types.NewBlockWithHeader(header0)
	header1 := &types.Header{Number: big.NewInt(1)}
	block1 := types.NewBlockWithHeader(header1)
	tests := []struct {
		name                  string
		hashErr, blockErr     error
		bestHash              common.Hash
		wantErr, hasTipChange bool
	}{{
		name:         "ok",
		bestHash:     block1.Hash(),
		hasTipChange: true,
	}, {
		name:     "ok same hash",
		bestHash: block0.Hash(),
	}, {
		name:         "best hash error",
		hasTipChange: true,
		hashErr:      errors.New(""),
		wantErr:      true,
	}, {
		name:         "block error",
		bestHash:     block1.Hash(),
		hasTipChange: true,
		blockErr:     errors.New(""),
		wantErr:      true,
	}}

	for _, test := range tests {
		var err error
		blocker := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		tipChange := func(tipErr error) {
			err = tipErr
			close(blocker)
		}
		node := &testNode{}
		node.bestBlkHash = test.bestHash
		node.blk = block1
		node.bestBlkHashErr = test.hashErr
		node.blkErr = test.blockErr
		eth := &ExchangeWallet{
			node:       node,
			tipChange:  tipChange,
			ctx:        ctx,
			currentTip: block0,
			log:        tLogger,
		}
		eth.checkForNewBlocks()

		if test.hasTipChange {
			<-blocker
		}
		cancel()
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}

	}
}

func TestSyncStatus(t *testing.T) {
	fourthSyncProg := &ethereum.SyncProgress{
		CurrentBlock: 25,
		HighestBlock: 100,
	}
	tests := []struct {
		name                    string
		syncProg                *ethereum.SyncProgress
		subSecs                 uint64
		bestHdrErr, syncProgErr error
		wantErr, wantSynced     bool
		wantRatio               float32
	}{{
		name:       "ok synced",
		wantRatio:  1,
		wantSynced: true,
	}, {
		name:      "ok syncing",
		syncProg:  fourthSyncProg,
		wantRatio: 0.25,
	}, {
		name:    "ok header too old",
		subSecs: srveth.MaxBlockInterval,
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}, {
		name:        "sync progress error",
		syncProgErr: errors.New(""),
		wantErr:     true,
	}}

	for _, test := range tests {
		nowInSecs := uint64(time.Now().Unix() / 1000)
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{
			syncProg:    test.syncProg,
			syncProgErr: test.syncProgErr,
			bestHdr:     &types.Header{Time: nowInSecs - test.subSecs},
			bestHdrErr:  test.bestHdrErr,
		}
		eth := &ExchangeWallet{
			node: node,
			ctx:  ctx,
			log:  tLogger,
		}
		synced, ratio, err := eth.SyncStatus()
		cancel()
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if synced != test.wantSynced {
			t.Fatalf("want synced %v got %v for test %q", test.wantSynced, synced, test.name)
		}
		if ratio != test.wantRatio {
			t.Fatalf("want ratio %v got %v for test %q", test.wantRatio, ratio, test.name)
		}
	}
}

func createSecretsAndHashes(numSecrets int) (secrets [][32]byte, secretHashes [][32]byte) {
	secrets = make([][32]byte, 0, numSecrets)
	secretHashes = make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}
	return secrets, secretHashes
}

func TestBalance(t *testing.T) {
	// TODO: replace this with common functions once refactor PR is merged
	gweiToWei := func(v uint64) *big.Int {
		bigGweiFactor := big.NewInt(srveth.GweiFactor)
		return big.NewInt(0).Mul(big.NewInt(int64(v)), bigGweiFactor)
	}

	newTx := func(gasFeeCapGwei uint64, gas uint64, valueGwei uint64, data []byte) *types.Transaction {
		simnetChainID := big.NewInt(42)
		return types.NewTx(&types.DynamicFeeTx{
			ChainID:   simnetChainID,
			GasFeeCap: gweiToWei(gasFeeCapGwei),
			To:        &testAddressA,
			Value:     gweiToWei(valueGwei),
			Data:      data,
			Gas:       gas,
		})
	}

	newInitiateTx := func(gasFeeCapGwei uint64, gas uint64, valueGwei uint64, initiations []dexeth.ETHSwapInitiation) *types.Transaction {
		data, err := dexeth.PackInitiateData(initiations)
		if err != nil {
			t.Fatalf("failed to pack initiate data: %v", err)
		}
		return newTx(gasFeeCapGwei, gas, valueGwei, data)
	}

	newRedeemTx := func(gasFeeCapGwei uint64, gas uint64, valueGwei uint64, redemptions []dexeth.ETHSwapRedemption) *types.Transaction {
		data, err := dexeth.PackRedeemData(redemptions)
		if err != nil {
			t.Fatalf("failed to pack initiate data: %v", err)
		}
		return newTx(gasFeeCapGwei, gas, valueGwei, data)
	}

	newRefundTx := func(gasFeeCapGwei uint64, gas uint64, valueGwei uint64, secretHash [32]byte) *types.Transaction {
		data, err := dexeth.PackRefundData(secretHash)
		if err != nil {
			t.Fatalf("failed to pack initiate data: %v", err)
		}
		return newTx(gasFeeCapGwei, gas, valueGwei, data)
	}

	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(srveth.GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	overMaxWei.Add(overMaxWei, gweiFactorBig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	node.swapMap = make(map[[32]byte]dexeth.ETHSwapSwap)
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: new(accounts.Account),
	}

	addSwapToSwapMap := func(secretHash [32]byte, value *big.Int) {
		swap := dexeth.ETHSwapSwap{
			InitBlockNumber:      big.NewInt(1),
			RefundBlockTimestamp: big.NewInt(1),
			Initiator:            testAddressA,
			Participant:          testAddressB,
			Value:                value,
			State:                uint8(srveth.SSInitiated),
		}
		node.swapMap[secretHash] = swap
	}

	secrets, secretHashes := createSecretsAndHashes(2)
	addSwapToSwapMap(secretHashes[0], gweiToWei(1e8))
	addSwapToSwapMap(secretHashes[1], gweiToWei(2e8))

	tests := []struct {
		name          string
		bal           *big.Int
		pendingTxs    []*types.Transaction
		pendingTxsErr error
		balErr        error
		swapErr       error
		wantBal       uint64
		wantImmature  uint64
		wantErr       bool
	}{{
		name:         "ok zero",
		bal:          big.NewInt(0),
		pendingTxs:   []*types.Transaction{},
		wantBal:      0,
		wantImmature: 0,
	}, {
		name:         "ok rounded down",
		bal:          big.NewInt(srveth.GweiFactor - 1),
		pendingTxs:   []*types.Transaction{},
		wantBal:      0,
		wantImmature: 0,
	}, {
		name:         "ok one",
		bal:          big.NewInt(srveth.GweiFactor),
		pendingTxs:   []*types.Transaction{},
		wantBal:      1,
		wantImmature: 0,
	}, {
		name: "ok pending initiation",
		bal:  gweiToWei(4e8),
		pendingTxs: []*types.Transaction{
			newInitiateTx(300, 200000, 2e8, []dexeth.ETHSwapInitiation{
				{RefundTimestamp: big.NewInt(1),
					SecretHash:  [32]byte{},
					Participant: testAddressC,
					Value:       gweiToWei(1e8)},
				{RefundTimestamp: big.NewInt(1),
					SecretHash:  [32]byte{},
					Participant: testAddressC,
					Value:       gweiToWei(1e8)}})},
		wantBal:      1.4e8,
		wantImmature: 0,
	}, {
		name: "ok pending redeem",
		bal:  gweiToWei(4e8),
		pendingTxs: []*types.Transaction{
			newRedeemTx(300, 200000, 0, []dexeth.ETHSwapRedemption{
				{Secret: secrets[0],
					SecretHash: secretHashes[0]},
				{Secret: secrets[1],
					SecretHash: secretHashes[1]}})},
		wantBal:      3.4e8,
		wantImmature: 3e8,
	}, {
		name: "ok pending withdrawal and refund",
		bal:  gweiToWei(4e8),
		pendingTxs: []*types.Transaction{
			newRefundTx(300, 200000, 0, secretHashes[0]),
			newTx(300, 21000, 1e8, []byte{}),
		},
		wantBal:      233700000,
		wantImmature: 1e8,
	}, {
		name: "pending redeem with swap error",
		bal:  gweiToWei(4e8),
		pendingTxs: []*types.Transaction{
			newRedeemTx(300, 200000, 0, []dexeth.ETHSwapRedemption{
				{Secret: secrets[0],
					SecretHash: secretHashes[0]},
				{Secret: secrets[1],
					SecretHash: secretHashes[1]}})},
		swapErr: errors.New("swap error"),
		wantErr: true,
	}, {
		name:         "ok max int",
		bal:          maxWei,
		pendingTxs:   []*types.Transaction{},
		wantBal:      maxInt,
		wantImmature: 0,
	}, {
		name:       "over max int",
		bal:        overMaxWei,
		pendingTxs: []*types.Transaction{},
		wantErr:    true,
	}, {
		name:    "node balance error",
		bal:     big.NewInt(0),
		balErr:  errors.New(""),
		wantErr: true,
	}, {
		name:          "node pending transactions error",
		bal:           big.NewInt(0),
		pendingTxsErr: errors.New(""),
		wantErr:       true,
	}}

	for _, test := range tests {
		node.bal = test.bal
		node.balErr = test.balErr
		node.pendingTxs = test.pendingTxs
		node.pendingTxsErr = test.pendingTxsErr
		node.swapErr = test.swapErr

		bal, err := eth.Balance()
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if bal.Available != test.wantBal {
			t.Fatalf("want available balance %v got %v for test %q", test.wantBal, bal.Available, test.name)
		}
		if bal.Immature != test.wantImmature {
			t.Fatalf("want immature balance %v got %v for test %q", test.wantImmature, bal.Immature, test.name)
		}
	}
}

// badCoin fulfills the asset.Coin interface, but the ID does not match the ID expected in the
// ETH wallet code.
type badCoin uint64

func (*badCoin) ID() dex.Bytes {
	return []byte{123}
}
func (*badCoin) String() string {
	return "abc"
}
func (b *badCoin) Value() uint64 {
	return uint64(*b)
}

func TestFundOrderReturnCoinsFundingCoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node := &testNode{}
	walletBalanceGwei := uint64(srveth.GweiFactor)
	node.bal = big.NewInt(int64(walletBalanceGwei) * srveth.GweiFactor)
	address := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	eth := &ExchangeWallet{
		node:        node,
		ctx:         ctx,
		log:         tLogger,
		acct:        &account,
		lockedFunds: make(map[string]uint64),
	}

	checkBalance := func(wallet *ExchangeWallet, expectedAvailable, expectedLocked uint64, testName string) {
		balance, err := wallet.Balance()
		if err != nil {
			t.Fatalf("%v: unexpected error %v", testName, err)
		}
		if balance.Available != expectedAvailable {
			t.Fatalf("%v: expected %v funds to be available but got %v", testName, expectedAvailable, balance.Available)
		}
		if balance.Locked != expectedLocked {
			t.Fatalf("%v: expected %v funds to be locked but got %v", testName, expectedLocked, balance.Locked)
		}
	}

	type fundOrderTest struct {
		testName    string
		wantErr     bool
		coinValue   uint64
		coinAddress string
	}
	checkFundOrderResult := func(coins asset.Coins, redeemScripts []dex.Bytes, err error, test fundOrderTest) {
		if test.wantErr && err == nil {
			t.Fatalf("%v: expected error but didn't get", test.testName)
		}
		if test.wantErr {
			return
		}
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", test.testName, err)
		}
		if len(coins) != 1 {
			t.Fatalf("%v: expected 1 coins but got %v", test.testName, len(coins))
		}
		if len(redeemScripts) != 1 {
			t.Fatalf("%v: expected 1 redeem script but got %v", test.testName, len(redeemScripts))
		}
		_, err = eth.decodeAmountCoinID(coins[0].ID())
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", test.testName, err)
		}
		if coins[0].Value() != test.coinValue {
			t.Fatalf("%v: expected %v but got %v", test.testName, test.coinValue, coins[0].Value())
		}
	}

	order := asset.Order{
		Value:        500000000,
		MaxSwapCount: 2,
		DEXConfig: &dex.Asset{
			ID:         60,
			Symbol:     "ETH",
			Version:    0,
			MaxFeeRate: 150,
			SwapSize:   180000,
		},
	}

	// Test fund order with less than available funds
	coins1, redeemScripts1, err := eth.FundOrder(&order)
	expectedOrderFees := order.DEXConfig.SwapSize * order.DEXConfig.MaxFeeRate * order.MaxSwapCount
	expctedRefundFees := srveth.RefundGas * order.DEXConfig.MaxFeeRate
	expectedFees := expectedOrderFees + expctedRefundFees
	expectedCoinValue := order.Value + expectedFees
	checkFundOrderResult(coins1, redeemScripts1, err, fundOrderTest{
		testName:    "more than enough",
		coinValue:   expectedCoinValue,
		coinAddress: address,
	})
	checkBalance(eth, walletBalanceGwei-expectedCoinValue, expectedCoinValue, "more than enough")

	// Test fund order with 1 more than available funds
	order.Value = walletBalanceGwei - expectedCoinValue - expectedFees + 1
	coins, redeemScripts, err := eth.FundOrder(&order)
	checkFundOrderResult(coins, redeemScripts, err, fundOrderTest{
		testName: "not enough",
		wantErr:  true,
	})
	checkBalance(eth, walletBalanceGwei-expectedCoinValue, expectedCoinValue, "not enough")

	// Test fund order with funds equal to available
	order.Value = order.Value - 1
	coins2, redeemScripts2, err := eth.FundOrder(&order)
	checkFundOrderResult(coins2, redeemScripts2, err, fundOrderTest{
		testName:    "just enough",
		coinValue:   order.Value + expectedFees,
		coinAddress: address,
	})
	checkBalance(eth, 0, walletBalanceGwei, "just enough")

	// Test returning too much funds fails
	err = eth.ReturnCoins([]asset.Coin{coins1[0], coins1[0]})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth, 0, walletBalanceGwei, "after redeem too much")

	// Test returning coin with invalid ID
	var badCoin badCoin
	err = eth.ReturnCoins([]asset.Coin{&badCoin})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}

	// Test returning correct coins returns all funds
	err = eth.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error")
	}
	checkBalance(eth, walletBalanceGwei, 0, "returned correct amount")

	node.balErr = errors.New("")
	_, _, err = eth.FundOrder(&order)
	if err == nil {
		t.Fatalf("balance error should cause error but did not")
	}
	node.balErr = nil

	eth2 := &ExchangeWallet{
		node:        node,
		ctx:         ctx,
		log:         tLogger,
		acct:        &account,
		lockedFunds: make(map[string]uint64),
	}

	// Test reloading coins from first order
	coins, err = eth2.FundingCoins([]dex.Bytes{coins1[0].ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coins) != 1 {
		t.Fatalf("expected 1 coins but got %v", len(coins))
	}
	if coins[0].Value() != coins1[0].Value() {
		t.Fatalf("funding coin value %v != expected %v", coins[0].Value(), coins[1].Value())
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "funding1")

	// Test reloading more coins than are available in balance
	_, err = eth2.FundingCoins([]dex.Bytes{coins1[0].ID()})
	if err == nil {
		t.Fatalf("expected error but didn't get one")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 1")

	// Double the available balance
	node.bal.Mul(node.bal, big.NewInt(2))

	// Test funding two coins with the same id
	_, err = eth2.FundingCoins([]dex.Bytes{coins2[0].ID(), coins2[0].ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei*2-coins1[0].Value(), coins1[0].Value(), "after funding error 2")

	// Return to original available balance
	node.bal.Div(node.bal, big.NewInt(2))

	// Test funding coins with bad coin ID
	_, err = eth2.FundingCoins([]dex.Bytes{badCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 3")

	// Test funding coins with coin from different address
	var differentAddress [20]byte
	decodedHex, _ := hex.DecodeString("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	copy(differentAddress[:], decodedHex)
	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))
	differentAddressCoin := coin{
		id: (&srveth.AmountCoinID{
			Address: differentAddress,
			Amount:  100000,
			Nonce:   nonce,
		}).Encode(),
	}
	_, err = eth2.FundingCoins([]dex.Bytes{differentAddressCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 4")

	// Test funding coins with balance error
	node.balErr = errors.New("")
	_, err = eth2.FundingCoins([]dex.Bytes{badCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	node.balErr = nil
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 5")

	// Reloading coins from second order
	coins, err = eth2.FundingCoins([]dex.Bytes{coins2[0].ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coins) != 1 {
		t.Fatalf("expected 1 coins but got %v", len(coins))
	}
	if coins[0].Value() != coins2[0].Value() {
		t.Fatalf("funding coin value %v != expected %v", coins[0].Value(), coins[1].Value())
	}
	checkBalance(eth2, 0, walletBalanceGwei, "funding2")

	// return coin with incorrect address
	err = eth2.ReturnCoins([]asset.Coin{&differentAddressCoin})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}

	// return all coins
	err = eth2.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error")
	}
	checkBalance(eth2, walletBalanceGwei, 0, "return coins after funding")

	// Test funding coins with two coins at the same time
	_, err = eth2.FundingCoins([]dex.Bytes{coins1[0].ID(), coins2[0].ID()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth2, 0, walletBalanceGwei, "funding3")
}

func TestPreSwap(t *testing.T) {
	ethToGwei := func(eth uint64) uint64 {
		return eth * srveth.GweiFactor
	}

	ethToWei := func(eth int64) *big.Int {
		return big.NewInt(0).Mul(big.NewInt(eth*srveth.GweiFactor), big.NewInt(srveth.GweiFactor))
	}

	tests := []struct {
		name          string
		bal           *big.Int
		balErr        error
		lotSize       uint64
		maxFeeRate    uint64
		feeSuggestion uint64
		lots          uint64

		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
		wantLocked    uint64
	}{
		{
			name:          "no balance",
			bal:           big.NewInt(0),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantErr: true,
		},
		{
			name:          "not enough for fees",
			bal:           ethToWei(10),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantErr: true,
		},
		{
			name:          "one lot enough for fees",
			bal:           ethToWei(11),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * srveth.InitGas,
			wantBestCase:  90 * srveth.InitGas,
			wantWorstCase: 90 * srveth.InitGas,
			wantLocked:    ethToGwei(10) + (100 * srveth.InitGas),
		},
		{
			name:          "more lots than max lots",
			bal:           ethToWei(11),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          2,

			wantErr: true,
		},
		{
			name:          "less than max lots",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          4,

			wantLots:      4,
			wantValue:     ethToGwei(40),
			wantMaxFees:   4 * 100 * srveth.InitGas,
			wantBestCase:  90 * srveth.InitGas,
			wantWorstCase: 4 * 90 * srveth.InitGas,
			wantLocked:    ethToGwei(40) + (4 * 100 * srveth.InitGas),
		},
		{
			name:          "balanceError",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			balErr:        errors.New(""),
			lots:          1,

			wantErr: true,
		},
	}

	dexAsset := dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     srveth.InitGas,
		SwapSizeBase: 0,
		SwapConf:     1,
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		preSwapForm := asset.PreSwapForm{
			LotSize:       test.lotSize,
			Lots:          test.lots,
			AssetConfig:   &dexAsset,
			FeeSuggestion: test.feeSuggestion,
		}
		node := &testNode{}
		node.bal = test.bal
		node.balErr = test.balErr
		eth := &ExchangeWallet{
			node: node,
			ctx:  ctx,
			log:  tLogger,
			acct: new(accounts.Account),
		}
		dexAsset.MaxFeeRate = test.maxFeeRate
		preSwap, err := eth.PreSwap(&preSwapForm)
		cancel()

		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if preSwap.Estimate.Lots != test.wantLots {
			t.Fatalf("want lots %v got %v for test %q", test.wantLots, preSwap.Estimate.Lots, test.name)
		}
		if preSwap.Estimate.Value != test.wantValue {
			t.Fatalf("want value %v got %v for test %q", test.wantValue, preSwap.Estimate.Value, test.name)
		}
		if preSwap.Estimate.MaxFees != test.wantMaxFees {
			t.Fatalf("want maxFees %v got %v for test %q", test.wantMaxFees, preSwap.Estimate.MaxFees, test.name)
		}
		if preSwap.Estimate.RealisticBestCase != test.wantBestCase {
			t.Fatalf("want best case %v got %v for test %q", test.wantBestCase, preSwap.Estimate.RealisticBestCase, test.name)
		}
		if preSwap.Estimate.RealisticWorstCase != test.wantWorstCase {
			t.Fatalf("want worst case %v got %v for test %q", test.wantWorstCase, preSwap.Estimate.RealisticWorstCase, test.name)
		}
		if preSwap.Estimate.Locked != test.wantLocked {
			t.Fatalf("want locked %v got %v for test %q", test.wantLocked, preSwap.Estimate.Locked, test.name)
		}
	}
}

func TestSwap(t *testing.T) {
	ethToGwei := func(eth uint64) uint64 {
		return eth * srveth.GweiFactor
	}
	ethToWei := func(eth int64) *big.Int {
		return big.NewInt(0).Mul(big.NewInt(eth*srveth.GweiFactor), big.NewInt(srveth.GweiFactor))
	}

	node := &testNode{}
	node.suggestedGasTipCap = big.NewInt(srveth.GweiFactor)
	address := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	receivingAddress := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eth := &ExchangeWallet{
		node:        node,
		ctx:         ctx,
		log:         tLogger,
		acct:        &account,
		lockedFunds: make(map[string]uint64),
	}

	coinIDsForAmounts := func(coinAmounts []uint64) []dex.Bytes {
		coinIDs := make([]dex.Bytes, 0, len(coinAmounts))
		for _, amt := range coinAmounts {
			amountCoinID := srveth.CreateAmountCoinID(eth.acct.Address, amt)
			coinIDs = append(coinIDs, amountCoinID.Encode())
		}
		return coinIDs
	}

	coinIDsToCoins := func(coinIDs []dex.Bytes) []asset.Coin {
		coins := make([]asset.Coin, 0, len(coinIDs))
		for _, id := range coinIDs {
			coin, _ := eth.decodeAmountCoinID(id)
			coins = append(coins, coin)
		}
		return coins
	}

	refreshWalletAndFundCoins := func(ethBalance uint64, coinAmounts []uint64) asset.Coins {
		node.bal = ethToWei(int64(ethBalance))
		eth.lockedFunds = make(map[string]uint64)
		coins, err := eth.FundingCoins(coinIDsForAmounts(coinAmounts))
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		return coins
	}

	gasNeededForSwaps := func(numSwaps int) uint64 {
		return srveth.InitGas * uint64(numSwaps)
	}

	testSwap := func(testName string, swaps asset.Swaps, expectError bool) {
		originalBalance, err := eth.Balance()
		if err != nil {
			t.Fatalf("%v: error getting balance: %v", testName, err)
		}

		receipts, changeCoin, feeSpent, err := eth.Swap(&swaps)
		if expectError {
			if err == nil {
				t.Fatalf("%v: expected error but did not get", testName)
			}
			return
		}
		if err != nil {
			t.Fatalf("%v: unexpected error doing Swap: %v", testName, err)
		}

		if len(receipts) != len(swaps.Contracts) {
			t.Fatalf("%v: num receipts %d != num contracts %d",
				testName, len(receipts), len(swaps.Contracts))
		}

		var totalCoinValue uint64
		for i, contract := range swaps.Contracts {
			// Check that receipts match the contract inputs
			receipt := receipts[i]
			expiration := int64(contract.LockTime)
			if receipt.Expiration().Unix() != expiration {
				t.Fatalf("%v: expected expiration %v != expiration %v",
					testName, time.Unix(expiration, 0), receipts[0].Expiration())
			}
			if receipt.Coin().Value() != contract.Value {
				t.Fatalf("%v: receipt coin value: %v != expected: %v",
					testName, receipt.Coin().Value(), contract.Value)
			}
			if len(receipt.Contract()) != srveth.SecretHashSize {
				t.Fatalf("%v: expected length of contract to be %v but got %v",
					testName, srveth.SecretHashSize, len(receipt.Contract()))
			}
			if !bytes.Equal(receipt.Contract(), contract.SecretHash[:]) {
				t.Fatalf("%v, contract: %x != secret hash in input: %x",
					testName, receipt.Contract(), contract.SecretHash)
			}

			if !bytes.Equal(node.lastInitiation.hash.Bytes(), receipt.Coin().ID()) {
				t.Fatalf("%v: tx hash: %x != coin id: %x",
					testName, node.lastInitiation.hash, receipt.Coin().ID())
			}

			// Check that initiations match the contract inputs
			initiation := node.lastInitiation.initiations[i]
			if !bytes.Equal(initiation.Participant.Bytes(), common.HexToAddress(contract.Address).Bytes()) {
				t.Fatalf("%v, address in contract: %v != participant address used to init swap: %v",
					testName, common.HexToAddress(contract.Address), initiation.Participant)
			}
			if !bytes.Equal(initiation.SecretHash[:], contract.SecretHash) {
				t.Fatalf("%v: secretHash in contract: %x != secret hash used to init swap: %x",
					testName, initiation.SecretHash, contract.SecretHash)
			}
			if initiation.RefundTimestamp.Uint64() != contract.LockTime {
				t.Fatalf("%v: lock time in contract %v != refundTimestamp used to init swap: %v",
					testName, contract.LockTime, initiation.RefundTimestamp)
			}
			contractValueWei := srveth.ToWei(contract.Value)
			if initiation.Value.Cmp(contractValueWei) != 0 {
				t.Fatalf("%v: value in contract %v != value used to init swap: %v",
					testName, contractValueWei, initiation.Value)
			}

			totalCoinValue += receipt.Coin().Value()
		}

		// Make sure transaction options are properly set
		txValue := node.lastInitiation.opts.Value
		totalCoinValueWei := srveth.ToWei(totalCoinValue)
		if txValue.Cmp(totalCoinValueWei) != 0 {
			t.Fatalf("%v: expected tx value to be %v, but got %v", testName, totalCoinValue, txValue)
		}
		lastGasTipCap := node.lastInitiation.opts.GasTipCap
		minGasTipCapWei := srveth.ToWei(MinGasTipCap)
		if node.suggestedGasTipCap.Cmp(minGasTipCapWei) <= 0 &&
			lastGasTipCap.Cmp(minGasTipCapWei) != 0 {
			t.Fatalf("%v: tip cap expected to be %v but got %v",
				testName, minGasTipCapWei, lastGasTipCap)
		}
		if node.suggestedGasTipCap.Cmp(minGasTipCapWei) > 0 &&
			lastGasTipCap.Cmp(node.suggestedGasTipCap) != 0 {
			t.Fatalf("%v: tip cap expected to be %v but got %v",
				testName, node.suggestedGasTipCap, lastGasTipCap)
		}
		lastFeeCap := node.lastInitiation.opts.GasFeeCap
		if srveth.ToWei(swaps.FeeRate).Cmp(lastFeeCap) != 0 {
			t.Fatalf("%v: fee cap expected to be %v but got %v",
				testName, swaps.FeeRate, lastFeeCap)
		}

		var totalInputValue uint64
		for _, coin := range swaps.Inputs {
			totalInputValue += coin.Value()
		}

		// Check that the coins used in swaps are no longer locked
		postSwapBalance, err := eth.Balance()
		if err != nil {
			t.Fatalf("%v: error getting balance: %v", testName, err)
		}
		var expectedLocked uint64
		if swaps.LockChange {
			expectedLocked = originalBalance.Locked -
				totalCoinValue -
				gasNeededForSwaps(len(swaps.Contracts))*swaps.FeeRate
		} else {
			expectedLocked = originalBalance.Locked - totalInputValue
		}
		if expectedLocked != postSwapBalance.Locked {
			t.Fatalf("%v: funds locked after swap expected: %v != actual: %v",
				testName, expectedLocked, postSwapBalance.Locked)
		}

		// Check that change coin is correctly returned
		expectedChangeValue := totalInputValue -
			totalCoinValue -
			gasNeededForSwaps(len(swaps.Contracts))*swaps.FeeRate
		if expectedChangeValue == 0 && changeCoin != nil {
			t.Fatalf("%v: change coin should be nil if change is 0", testName)
		} else if expectedChangeValue > 0 && changeCoin == nil {
			t.Fatalf("%v: change coin should not be nil if there is expected change", testName)
		} else if changeCoin != nil && changeCoin.Value() != expectedChangeValue {
			t.Fatalf("%v: expected change value %v != change coin value: %v",
				testName, expectedChangeValue, changeCoin.Value())
		}

		expectedFees := gasNeededForSwaps(len(swaps.Contracts)) * swaps.FeeRate
		if feeSpent != expectedFees {
			t.Fatalf("%v: expected fees: %v != actual fees %v", testName, expectedFees, feeSpent)
		}
	}

	secret := encode.RandomBytes(32)
	secretHash := sha256.Sum256(secret)
	secret2 := encode.RandomBytes(32)
	secretHash2 := sha256.Sum256(secret2)
	expiration := time.Now().Add(time.Hour * 8).Unix()

	// Ensure error with invalid secret hash
	contracts := []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: encode.RandomBytes(31),
			LockTime:   uint64(expiration),
		},
	}
	inputs := refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps := asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("incorrect length secret hash", swaps, true)

	// Ensure error with invalid receiving address
	contracts = []*asset.Contract{
		{
			Address:    hex.EncodeToString(encode.RandomBytes(21)),
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
	}
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("invalid receiving address", swaps, true)

	// Ensure error when initializing swap errors
	node.initErr = errors.New("")
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
	}
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("error initialize but no send", swaps, true)
	node.initErr = nil

	// Ensure error initializing two contracts with same secret hash
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
	}
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(3)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("two contracts same hash error", swaps, true)

	// Tests one contract without locking change
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
	}
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("one contract, don't lock change", swaps, false)

	// Test one contract with locking change
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: true,
	}
	testSwap("one contract, lock change", swaps, false)

	// Test two contracts
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   uint64(expiration),
		},
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash2[:],
			LockTime:   uint64(expiration),
		},
	}
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(3)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("two contracts", swaps, false)

	// Test error when funding coins are not enough to cover swaps
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(1)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("funding coins not enough balance", swaps, true)

	// Ensure error when inputs were not locked by wallet
	_ = refreshWalletAndFundCoins(5, []uint64{ethToGwei(3)})
	inputs = coinIDsToCoins(coinIDsForAmounts([]uint64{ethToGwei(3)}))
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("funding coins that were not locked", swaps, true)

	// Ensure when funds are exactly the same as required works properly
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * srveth.InitGas)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("exact change", swaps, false)

	// Ensure if suggested tip cap is higher than minimum, the suggested
	// value is used
	node.suggestedGasTipCap = big.NewInt((MinGasTipCap + 1) * srveth.GweiFactor)
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * srveth.InitGas)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("suggested tip cap higher than min", swaps, false)
	node.suggestedGasTipCap = big.NewInt(srveth.GweiFactor)

	// Ensure there is an error if suggest tip cap fails
	node.suggestGasTipCapErr = errors.New("")
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * srveth.InitGas)})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("suggested tip cap error", swaps, true)
	node.suggestGasTipCapErr = nil
}

func TestPreRedeem(t *testing.T) {
	node := &testNode{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: new(accounts.Account),
	}
	preRedeem, err := eth.PreRedeem(&asset.PreRedeemForm{
		LotSize:       123456,
		Lots:          5,
		FeeSuggestion: 100,
	})
	if err != nil {
		t.Fatalf("unexpected PreRedeem error: %v", err)
	}

	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("best case > worst case")
	}
}

func TestMaxOrder(t *testing.T) {
	ethToGwei := func(eth uint64) uint64 {
		return eth * srveth.GweiFactor
	}

	ethToWei := func(eth int64) *big.Int {
		return big.NewInt(0).Mul(big.NewInt(eth*srveth.GweiFactor), big.NewInt(srveth.GweiFactor))
	}

	tests := []struct {
		name          string
		bal           *big.Int
		balErr        error
		lotSize       uint64
		maxFeeRate    uint64
		feeSuggestion uint64
		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
		wantLocked    uint64
	}{
		{
			name:          "no balance",
			bal:           big.NewInt(0),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "not enough for fees",
			bal:           ethToWei(10),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "one lot enough for fees",
			bal:           ethToWei(11),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * srveth.InitGas,
			wantBestCase:  90 * srveth.InitGas,
			wantWorstCase: 90 * srveth.InitGas,
			wantLocked:    ethToGwei(10) + (100 * srveth.InitGas),
		},
		{
			name:          "multiple lots",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * srveth.InitGas,
			wantBestCase:  90 * srveth.InitGas,
			wantWorstCase: 5 * 90 * srveth.InitGas,
			wantLocked:    ethToGwei(50) + (5 * 100 * srveth.InitGas),
		},
		{
			name:          "balanceError",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			balErr:        errors.New(""),
			wantErr:       true,
		},
	}

	dexAsset := dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     srveth.InitGas,
		SwapSizeBase: 0,
		SwapConf:     1,
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{}
		node.bal = test.bal
		node.balErr = test.balErr
		eth := &ExchangeWallet{
			node: node,
			ctx:  ctx,
			log:  tLogger,
			acct: new(accounts.Account),
		}
		dexAsset.MaxFeeRate = test.maxFeeRate
		maxOrder, err := eth.MaxOrder(test.lotSize, test.feeSuggestion, &dexAsset)
		cancel()

		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if maxOrder.Lots != test.wantLots {
			t.Fatalf("want lots %v got %v for test %q", test.wantLots, maxOrder.Lots, test.name)
		}
		if maxOrder.Value != test.wantValue {
			t.Fatalf("want value %v got %v for test %q", test.wantValue, maxOrder.Value, test.name)
		}
		if maxOrder.MaxFees != test.wantMaxFees {
			t.Fatalf("want maxFees %v got %v for test %q", test.wantMaxFees, maxOrder.MaxFees, test.name)
		}
		if maxOrder.RealisticBestCase != test.wantBestCase {
			t.Fatalf("want best case %v got %v for test %q", test.wantBestCase, maxOrder.RealisticBestCase, test.name)
		}
		if maxOrder.RealisticWorstCase != test.wantWorstCase {
			t.Fatalf("want worst case %v got %v for test %q", test.wantWorstCase, maxOrder.RealisticWorstCase, test.name)
		}
		if maxOrder.Locked != test.wantLocked {
			t.Fatalf("want locked %v got %v for test %q", test.wantLocked, maxOrder.Locked, test.name)
		}
	}
}

func TestOwnsAddress(t *testing.T) {
	address := "0b84C791b79Ee37De042AD2ffF1A253c3ce9bc27" // no "0x" prefix
	if !common.IsHexAddress(address) {
		t.Fatalf("bad test address")
	}

	var otherAddress common.Address
	rand.Read(otherAddress[:])

	eth := &ExchangeWallet{
		acct: &accounts.Account{
			Address: common.HexToAddress(address),
		},
	}

	tests := []struct {
		name     string
		address  string
		wantOwns bool
		wantErr  bool
	}{
		{
			name:     "same (exact)",
			address:  address,
			wantOwns: true,
			wantErr:  false,
		},
		{
			name:     "same (lower)",
			address:  strings.ToLower(address),
			wantOwns: true,
			wantErr:  false,
		},
		{
			name:     "same (upper)",
			address:  strings.ToUpper(address),
			wantOwns: true,
			wantErr:  false,
		},
		{
			name:     "same (0x prefix)",
			address:  "0x" + address,
			wantOwns: true,
			wantErr:  false,
		},
		{
			name:     "different (valid canonical)",
			address:  otherAddress.String(),
			wantOwns: false,
			wantErr:  false,
		},
		{
			name:     "different (valid hex)",
			address:  otherAddress.Hex(),
			wantOwns: false,
			wantErr:  false,
		},
		{
			name:     "error (bad hex char)",
			address:  strings.Replace(address, "b", "r", 1),
			wantOwns: false,
			wantErr:  true,
		},
		{
			name:     "error (bad length)",
			address:  "ababababababab",
			wantOwns: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owns, err := eth.OwnsAddress(tt.address)
			if (err == nil) && tt.wantErr {
				t.Error("expected error")
			}
			if (err != nil) && !tt.wantErr {
				t.Errorf("unexpected error: %v", err)
			}
			if owns != tt.wantOwns {
				t.Errorf("got %v, want %v", owns, tt.wantOwns)
			}
		})
	}
}

func TestSignMessage(t *testing.T) {
	node := &testNode{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	privKey, _ := crypto.HexToECDSA("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")
	node.privKeyForSigning = privKey
	address := "2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: &account,
	}

	msg := []byte("msg")

	// Error due to coin with unparsable ID
	var badCoin badCoin
	_, _, err := eth.SignMessage(&badCoin, msg)
	if err == nil {
		t.Fatalf("expected error for signing message with bad coin")
	}

	// Error due to coin from with account than wallet
	differentAddress := common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	nonce := [8]byte{}
	coinDifferentAddress := coin{
		id: (&srveth.AmountCoinID{
			Address: differentAddress,
			Amount:  100,
			Nonce:   nonce,
		}).Encode(),
	}
	_, _, err = eth.SignMessage(&coinDifferentAddress, msg)
	if err == nil {
		t.Fatalf("expected error for signing message with different address than wallet")
	}

	coin := coin{
		id: (&srveth.AmountCoinID{
			Address: account.Address,
			Amount:  100,
			Nonce:   nonce,
		}).Encode(),
	}

	// SignData error
	node.signDataErr = errors.New("")
	_, _, err = eth.SignMessage(&coin, msg)
	if err == nil {
		t.Fatalf("expected error due to error in rpcclient signData")
	}
	node.signDataErr = nil

	// Test no error
	pubKeys, sigs, err := eth.SignMessage(&coin, msg)
	if err != nil {
		t.Fatalf("unexpected error signing message: %v", err)
	}
	if len(pubKeys) != 1 {
		t.Fatalf("expected 1 pubKey but got %v", len(pubKeys))
	}
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signature but got %v", len(sigs))
	}
	if !secp256k1.VerifySignature(pubKeys[0], crypto.Keccak256(msg), sigs[0][:len(sigs[0])-1]) {
		t.Fatalf("failed to verify signature")
	}
}
