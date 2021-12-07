//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)

	testAddressA = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	testAddressB = common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	testAddressC = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
)

type testNode struct {
	acct              *accounts.Account
	addr              common.Address
	connectErr        error
	bestHdr           *types.Header
	bestHdrErr        error
	bestBlkHash       common.Hash
	bestBlkHashErr    error
	blk               *types.Block
	blkErr            error
	syncProg          ethereum.SyncProgress
	bal               *Balance
	balErr            error
	signDataErr       error
	privKeyForSigning *ecdsa.PrivateKey
	swapVers          map[uint32]struct{} // For SwapConfirmations -> swap. TODO for other contractor methods
	swapMap           map[[32]byte]*dexeth.SwapState
	swapErr           error
	initErr           error
	nonce             uint64
}

func newBalance(current, in, out uint64) *Balance {
	return &Balance{
		Current:    dexeth.GweiToWei(current),
		PendingIn:  dexeth.GweiToWei(in),
		PendingOut: dexeth.GweiToWei(out),
	}
}

func (n *testNode) address() common.Address {
	return n.addr
}
func (n *testNode) connect(ctx context.Context) error {
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
func (n *testNode) balance(ctx context.Context) (*Balance, error) {
	return n.bal, n.balErr
}
func (n *testNode) syncStatus(ctx context.Context) (bool, float32, error) {
	return false, 0, nil
}
func (n *testNode) unlock(pw string) error {
	return nil
}
func (n *testNode) lock() error {
	return nil
}
func (n *testNode) locked() bool {
	return false
}

func (n *testNode) syncProgress() ethereum.SyncProgress {
	return n.syncProg
}

// initiate is not concurrent safe
func (n *testNode) initiate(ctx context.Context, contracts []*asset.Contract, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	if n.initErr != nil {
		return nil, n.initErr
	}
	// baseTx := &types.DynamicFeeTx{
	// 	Nonce:     n.nonce,
	// 	GasFeeCap: maxFeeRate,
	// 	GasTipCap: MinGasTipCap,
	// 	Gas:       dexeth.InitGas(len(contracts)),
	// 	Value:     opts.Value,
	// 	Data:      []byte{},
	// }
	tx = types.NewTx(&types.DynamicFeeTx{})
	// n.nonce++
	// n.lastInitiation = initTx{
	// 	initiations: initiations,
	// 	hash:        tx.Hash(),
	// 	opts:        opts,
	// }
	n.nonce++
	return types.NewTx(&types.DynamicFeeTx{
		Nonce: n.nonce,
	}), nil
}
func (n *testNode) redeem(opts *bind.TransactOpts, redemptions []*asset.Redemption, contractVer uint32) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) refund(opts *bind.TransactOpts, secretHash [32]byte, serverVer uint32) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) swap(ctx context.Context, secretHash [32]byte, contractVer uint32) (*dexeth.SwapState, error) {
	if n.swapErr != nil {
		return nil, n.swapErr
	}
	swap, ok := n.swapMap[secretHash]
	if !ok {
		return nil, errors.New("swap not in map")
	}
	_, found := n.swapVers[contractVer]
	if !found {
		return nil, errors.New("unknown contract version")
	}
	return swap, nil
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

func (n *testNode) sendToAddr(ctx context.Context, addr common.Address, val uint64) (*types.Transaction, error) {
	return nil, nil
}

func (n *testNode) transactionConfirmations(context.Context, common.Hash) (uint32, error) {
	return 0, nil
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
	header0 := &types.Header{Number: new(big.Int)}
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
			addr:       node.address(),
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
		name                string
		syncProg            ethereum.SyncProgress
		subSecs             uint64
		bestHdrErr          error
		wantErr, wantSynced bool
		wantRatio           float32
	}{{
		name:       "ok synced",
		wantRatio:  1,
		wantSynced: true,
	}, {
		name:      "ok syncing",
		syncProg:  *fourthSyncProg,
		wantRatio: 0.25,
	}, {
		name:    "ok header too old",
		subSecs: dexeth.MaxBlockInterval,
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}}

	for _, test := range tests {
		nowInSecs := uint64(time.Now().Unix() / 1000)
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{
			syncProg:   test.syncProg,
			bestHdr:    &types.Header{Time: nowInSecs - test.subSecs},
			bestHdrErr: test.bestHdrErr,
		}
		eth := &ExchangeWallet{
			node: node,
			addr: node.address(),
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

func newTestNode(acct *accounts.Account) *testNode {
	if acct == nil {
		acct = new(accounts.Account)
	}
	return &testNode{
		acct: acct,
		addr: acct.Address,
	}
}

func TestBalance(t *testing.T) {
	// maxInt := ^uint64(0)
	// maxWei := new(big.Int).SetUint64(maxInt)
	// gweiFactorBig := big.NewInt(dexeth.GweiFactor)
	// maxWei.Mul(maxWei, gweiFactorBig)
	// overMaxWei := new(big.Int).Set(maxWei)
	// overMaxWei.Add(overMaxWei, gweiFactorBig)

	tinyBal := newBalance(0, 0, 0)
	tinyBal.Current = big.NewInt(dexeth.GweiFactor - 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	node.swapMap = make(map[[32]byte]*dexeth.SwapState)
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
	}

	tests := []struct {
		name         string
		bal          *Balance
		balErr       error
		wantErr      bool
		wantBal      uint64
		wantImmature uint64
		wantLocked   uint64
	}{{
		name:         "ok zero",
		bal:          newBalance(0, 0, 0),
		wantBal:      0,
		wantImmature: 0,
	}, {
		name:    "ok rounded down",
		bal:     tinyBal,
		wantBal: 0,
	}, {
		name:    "ok one",
		bal:     newBalance(1, 0, 0),
		wantBal: 1,
	}, {
		name:       "ok pending out",
		bal:        newBalance(4e8, 0, 1.4e8),
		wantBal:    2.6e8,
		wantLocked: 1.4e8,
	}, {
		name:         "ok pending in",
		bal:          newBalance(1e8, 3e8, 0),
		wantBal:      1e8,
		wantImmature: 3e8,
	}, {
		name:         "ok pending out and in",
		bal:          newBalance(4e8, 2e8, 1e8),
		wantBal:      3e8,
		wantLocked:   1e8,
		wantImmature: 2e8,
	}, {
		// 	name: "swap error",
		// 	swapErr: errors.New("swap error"),
		// 	wantErr: true,
		// }, {
		// 	name:         "ok max int",
		// 	bal:          maxWei,
		// 	pendingTxs:   []*types.Transaction{},
		// 	wantBal:      maxInt,
		// 	wantImmature: 0,
		// }, {
		// 	name:       "over max int",
		// 	bal:        overMaxWei,
		// 	pendingTxs: []*types.Transaction{},
		// 	wantErr:    true,
		// }, {
		name:    "node balance error",
		balErr:  errors.New(""),
		wantErr: true,
	}}

	for _, test := range tests {

		node.bal = test.bal
		node.balErr = test.balErr

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
		if bal.Locked != test.wantLocked {
			t.Fatalf("want locked balance %v got %v for test %q", test.wantLocked, bal.Locked, test.name)
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
	walletBalanceGwei := uint64(dexeth.GweiFactor)
	address := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	node := newTestNode(&account)
	node.bal = newBalance(walletBalanceGwei, 0, 0)
	eth := &ExchangeWallet{
		node:        node,
		addr:        node.address(),
		ctx:         ctx,
		log:         tLogger,
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
		t.Helper()
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
		_, err = eth.decodeFundingCoinID(coins[0].ID())
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
	expctedRefundFees := dexeth.RefundGas(0) * order.DEXConfig.MaxFeeRate
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
		addr:        node.address(),
		ctx:         ctx,
		log:         tLogger,
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
	node.bal.Current.Mul(node.bal.Current, big.NewInt(2))

	// Test funding two coins with the same id
	_, err = eth2.FundingCoins([]dex.Bytes{coins2[0].ID(), coins2[0].ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei*2-coins1[0].Value(), coins1[0].Value(), "after funding error 2")

	// Return to original available balance
	node.bal.Current.Div(node.bal.Current, big.NewInt(2))

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
		id: (&fundingCoinID{
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
	gases := dexeth.VersionedGases[0]

	tests := []struct {
		name          string
		bal           uint64
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
			bal:           0,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantErr: true,
		},
		{
			name:          "not enough for fees",
			bal:           10,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantErr: true,
		},
		{
			name:          "one lot enough for fees",
			bal:           11,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          1,

			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * gases.InitGas,
			wantBestCase:  90 * gases.InitGas,
			wantWorstCase: 90 * gases.InitGas,
			wantLocked:    ethToGwei(10) + (100 * gases.InitGas),
		},
		{
			name:          "more lots than max lots",
			bal:           11,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          2,

			wantErr: true,
		},
		{
			name:          "less than max lots",
			bal:           51,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			lots:          4,

			wantLots:      4,
			wantValue:     ethToGwei(40),
			wantMaxFees:   4 * 100 * gases.InitGas,
			wantBestCase:  90 * (gases.InitGas + 3*gases.AdditionalInitGas),
			wantWorstCase: 4 * 90 * gases.InitGas,
			wantLocked:    ethToGwei(40) + (4 * 100 * gases.InitGas),
		},
		{
			name:          "balanceError",
			bal:           51,
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
		SwapSize:     gases.InitGas,
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
		node := newTestNode(nil)
		node.bal = newBalance(test.bal*1e9, 0, 0)
		node.balErr = test.balErr
		eth := &ExchangeWallet{
			node: node,
			addr: node.address(),
			ctx:  ctx,
			log:  tLogger,
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
	node := &testNode{
		bal: newBalance(0, 0, 0),
	}
	address := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	receivingAddress := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eth := &ExchangeWallet{
		node:        node,
		addr:        common.HexToAddress(address),
		ctx:         ctx,
		log:         tLogger,
		lockedFunds: make(map[string]uint64),
	}

	coinIDsForAmounts := func(coinAmounts []uint64) []dex.Bytes {
		coinIDs := make([]dex.Bytes, 0, len(coinAmounts))
		for _, amt := range coinAmounts {
			fundingCoinID := createFundingCoinID(eth.addr, amt)
			coinIDs = append(coinIDs, fundingCoinID.Encode())
		}
		return coinIDs
	}

	coinIDsToCoins := func(coinIDs []dex.Bytes) []asset.Coin {
		coins := make([]asset.Coin, 0, len(coinIDs))
		for _, id := range coinIDs {
			coin, _ := eth.decodeFundingCoinID(id)
			coins = append(coins, coin)
		}
		return coins
	}

	refreshWalletAndFundCoins := func(ethBalance uint64, coinAmounts []uint64) asset.Coins {
		node.bal.Current = ethToWei(ethBalance)
		eth.lockedFunds = make(map[string]uint64)
		coins, err := eth.FundingCoins(coinIDsForAmounts(coinAmounts))
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		return coins
	}

	gasNeededForSwaps := func(numSwaps int) uint64 {
		return dexeth.InitGas(1, 0) * uint64(numSwaps)
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
			if encode.UnixMilliU(receipt.Expiration()) != contract.LockTime {
				t.Fatalf("%v: expected expiration %v != expiration %v",
					testName, encode.UnixTimeMilli(int64(contract.LockTime)), receipts[0].Expiration())
			}
			if receipt.Coin().Value() != contract.Value {
				t.Fatalf("%v: receipt coin value: %v != expected: %v",
					testName, receipt.Coin().Value(), contract.Value)
			}
			contractData := receipt.Contract()
			if swaps.AssetVersion != binary.BigEndian.Uint32(contractData[:4]) {
				t.Fatal("wrong contract version")
			}
			if !bytes.Equal(contractData[4:], contract.SecretHash[:]) {
				t.Fatalf("%v, contract: %x != secret hash in input: %x",
					testName, receipt.Contract(), contract.SecretHash)
			}

			totalCoinValue += receipt.Coin().Value()
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
		} else if expectedChangeValue > 0 && changeCoin == nil && swaps.LockChange {
			t.Fatalf("%v: change coin should not be nil if there is expected change and change is locked",
				testName)
		} else if !swaps.LockChange && changeCoin != nil {
			t.Fatalf("%v: change should be nil if LockChange==False", testName)
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
	expiration := encode.UnixMilliU(time.Now().Add(time.Hour * 8))

	// Ensure error when initializing swap errors
	node.initErr = errors.New("")
	contracts := []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   expiration,
		},
	}
	inputs := refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)})
	swaps := asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("error initialize but no send", swaps, true)
	node.initErr = nil

	// Tests one contract without locking change
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   expiration,
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
			LockTime:   expiration,
		},
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash2[:],
			LockTime:   expiration,
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
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * dexeth.InitGas(1, 0))})
	swaps = asset.Swaps{
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    200,
		LockChange: false,
	}
	testSwap("exact change", swaps, false)
}

func TestPreRedeem(t *testing.T) {
	node := newTestNode(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eth := &ExchangeWallet{
		node: node,
		addr: node.address(),
		ctx:  ctx,
		log:  tLogger,
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
	gases := dexeth.VersionedGases[0]

	tests := []struct {
		name          string
		bal           uint64
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
			bal:           0,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "not enough for fees",
			bal:           10,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "one lot enough for fees",
			bal:           11,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * gases.InitGas,
			wantBestCase:  90 * gases.InitGas,
			wantWorstCase: 90 * gases.InitGas,
			wantLocked:    ethToGwei(10) + (100 * gases.InitGas),
		},
		{
			name:          "multiple lots",
			bal:           51,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * gases.InitGas,
			wantBestCase:  90 * (gases.InitGas + 4*gases.AdditionalInitGas),
			wantWorstCase: 5 * 90 * gases.InitGas,
			wantLocked:    ethToGwei(50) + (5 * 100 * gases.InitGas),
		},
		{
			name:          "balanceError",
			bal:           51,
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
		SwapSize:     gases.InitGas,
		SwapSizeBase: 0,
		SwapConf:     1,
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := newTestNode(nil)
		node.bal = newBalance(test.bal*1e9, 0, 0)
		node.balErr = test.balErr
		eth := &ExchangeWallet{
			node: node,
			addr: node.address(),
			ctx:  ctx,
			log:  tLogger,
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
		addr: common.HexToAddress(address),
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	privKey, _ := crypto.HexToECDSA("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")

	address := "2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	node := newTestNode(&account)
	node.privKeyForSigning = privKey
	eth := &ExchangeWallet{
		node: node,
		addr: node.address(),
		ctx:  ctx,
		log:  tLogger,
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
		id: (&fundingCoinID{
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
		id: (&fundingCoinID{
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

func TestSwapConfirmation(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	state := &dexeth.SwapState{}
	hdr := &types.Header{}

	node := &testNode{
		bal:     newBalance(0, 0, 0),
		bestHdr: hdr,
		swapMap: map[[32]byte]*dexeth.SwapState{
			secretHash: state,
		},
		swapVers: map[uint32]struct{}{
			0: {},
		},
	}

	eth := &ExchangeWallet{
		node: node,
		addr: testAddressA,
	}

	state.BlockHeight = 5
	state.State = dexeth.SSInitiated
	hdr.Number = big.NewInt(6)

	ver := uint32(0)

	checkResult := func(expErr bool, expConfs uint32, expSpent bool) {
		confs, spent, err := eth.SwapConfirmations(nil, nil, versionedBytes(ver, secretHash[:]), time.Time{})
		if err != nil {
			if expErr {
				return
			}
			t.Fatalf("SwapConfirmations error: %v", err)
		}
		if confs != expConfs {
			t.Fatalf("expected %d confs, got %d", expConfs, confs)
		}
		if spent != expSpent {
			t.Fatalf("wrong spent. wanted %t, got %t", expSpent, spent)
		}
	}

	checkResult(false, 2, false)

	// unknown asset version
	ver = 12
	checkResult(true, 0, false)
	ver = 0

	// swap error
	node.swapErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.swapErr = nil

	// header error
	node.bestHdrErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.bestHdrErr = nil

	// CoinNotFoundError
	state.State = dexeth.SSNone
	_, _, err := eth.SwapConfirmations(nil, nil, versionedBytes(0, secretHash[:]), time.Time{})
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected CoinNotFoundError, got %v", err)
	}

	// 1 conf, spent
	state.BlockHeight = 6
	state.State = dexeth.SSRedeemed
	checkResult(false, 1, true)
}

func TestLocktimeExpired(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))

	state := &dexeth.SwapState{
		LockTime: time.Now(),
	}

	header := &types.Header{
		Time: uint64(time.Now().Add(time.Second).Unix()),
	}

	node := &testNode{
		swapMap:  map[[32]byte]*dexeth.SwapState{secretHash: state},
		swapVers: map[uint32]struct{}{0: {}},
		bestHdr:  header,
	}

	eth := &ExchangeWallet{node: node}

	contract := make([]byte, 36)
	copy(contract[4:], secretHash[:])

	ensureResult := func(tag string, expErr, expExpired bool) {
		expired, _, err := eth.LocktimeExpired(contract)
		switch {
		case err != nil:
			if !expErr {
				t.Fatalf("%s: LocktimeExpired error existing expired swap: %v", tag, err)
			}
		case expErr:
			t.Fatalf("%s: expected error, got none", tag)
		case expExpired != expired:
			t.Fatalf("%s: expired wrong. %t != %t", tag, expired, expExpired)
		}
	}

	// locktime expired
	ensureResult("locktime expired", false, true)

	// header error
	node.bestHdrErr = errors.New("test error")
	ensureResult("header error", true, false)
	node.bestHdrErr = nil

	// missing swap
	delete(node.swapMap, secretHash)
	ensureResult("missing swap", true, false)
	node.swapMap[secretHash] = state

	// lock time not expired
	state.LockTime = time.Now().Add(time.Minute)
	ensureResult("lock time not expired", false, false)

	// wrong contract version
	contract[3] = 1
	ensureResult("wrong contract version", true, false)
	contract[3] = 0

	// bad contract
	contract = append(contract, 0)
	ensureResult("bad contract", true, false)
}

func ethToGwei(v uint64) uint64 {
	return v * dexeth.GweiFactor
}

func ethToWei(v uint64) *big.Int {
	bigV := new(big.Int).SetUint64(ethToGwei(v))
	return new(big.Int).Mul(bigV, dexeth.BigGweiFactor)
}
