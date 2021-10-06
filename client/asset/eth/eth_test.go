//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	swap "decred.org/dcrdex/dex/networks/eth"
	dexeth "decred.org/dcrdex/server/asset/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
)

type testNode struct {
	connectErr     error
	bestHdr        *types.Header
	bestHdrErr     error
	bestBlkHash    common.Hash
	bestBlkHashErr error
	blk            *types.Block
	blkErr         error
	blkNum         uint64
	blkNumErr      error
	syncProg       *ethereum.SyncProgress
	syncProgErr    error
	peerInfo       []*p2p.PeerInfo
	peersErr       error
	bal            *big.Int
	balErr         error
	initGas        uint64
	initGasErr     error
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
func (n *testNode) syncStatus(ctx context.Context) (bool, float32, error) {
	return false, 0, nil
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
func (n *testNode) importAccount(pw string, privKeyB []byte) (*accounts.Account, error) {
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
	return nil, nil
}
func (n *testNode) initiate(opts *bind.TransactOpts, netID int64, refundTimestamp int64, secretHash [32]byte, participant *common.Address) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) redeem(opts *bind.TransactOpts, netID int64, secret, secretHash [32]byte) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) refund(opts *bind.TransactOpts, netID int64, secretHash [32]byte) (*types.Transaction, error) {
	return nil, nil
}
func (n *testNode) swap(ctx context.Context, from *accounts.Account, secretHash [32]byte) (*swap.ETHSwapSwap, error) {
	return nil, nil
}
func (n *testNode) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, nil
}
func (n *testNode) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	return n.peerInfo, n.peersErr
}
func (n *testNode) estimateGas(ctx context.Context, callMsg ethereum.CallMsg) (uint64, error) {
	return n.initGas, n.initGasErr
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
		subSecs: dexeth.MaxBlockInterval,
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

func TestBalance(t *testing.T) {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(dexeth.GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	overMaxWei.Add(overMaxWei, gweiFactorBig)
	tests := []struct {
		name    string
		bal     *big.Int
		balErr  error
		wantBal uint64
		wantErr bool
	}{{
		name:    "ok zero",
		bal:     big.NewInt(0),
		wantBal: 0,
	}, {
		name:    "ok rounded down",
		bal:     big.NewInt(dexeth.GweiFactor - 1),
		wantBal: 0,
	}, {
		name:    "ok one",
		bal:     big.NewInt(dexeth.GweiFactor),
		wantBal: 1,
	}, {
		name:    "ok max int",
		bal:     maxWei,
		wantBal: maxInt,
	}, {
		name:    "over max int",
		bal:     overMaxWei,
		wantErr: true,
	}, {
		name:    "node balance error",
		bal:     big.NewInt(0),
		balErr:  errors.New(""),
		wantErr: true,
	}}

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
		bal, err := eth.Balance()
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
		if bal.Available != test.wantBal {
			t.Fatalf("want available balance %v got %v for test %q", test.wantBal, bal.Available, test.name)
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
	walletBalanceGwei := uint64(dexeth.GweiFactor)
	node.bal = big.NewInt(int64(walletBalanceGwei) * dexeth.GweiFactor)
	address := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	account := accounts.Account{
		Address: common.HexToAddress(address),
	}
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: &account,
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
		coin, err := decodeCoinID(coins[0].ID())
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", test.testName, err)
		}
		if coin.id.Address.String() != test.coinAddress {
			t.Fatalf("%v: coin address expected to be %v, but got %v", test.testName, test.coinAddress, coin.id.Address.String())
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
	expectedCoinValue := order.Value + expectedOrderFees
	checkFundOrderResult(coins1, redeemScripts1, err, fundOrderTest{
		testName:    "more than enough",
		coinValue:   expectedCoinValue,
		coinAddress: address,
	})
	checkBalance(eth, walletBalanceGwei-expectedCoinValue, expectedCoinValue, "more than enough")

	// Test fund order with 1 more than available funds
	order.Value = walletBalanceGwei - expectedCoinValue - expectedOrderFees + 1
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
		coinValue:   order.Value + expectedOrderFees,
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
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: &account,
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

	// Test funding coins with bad coin ID
	_, err = eth2.FundingCoins([]dex.Bytes{badCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 2")

	// Test funding coins with coin from different address
	var differentAddress [20]byte
	decodedHex, _ := hex.DecodeString("345853e21b1d475582E71cC269124eD5e2dD3422")
	copy(differentAddress[:], decodedHex)
	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))
	differentAddressCoin := coin{
		id: dexeth.AmountCoinID{
			Address: differentAddress,
			Amount:  100000,
			Nonce:   nonce,
		},
	}
	_, err = eth2.FundingCoins([]dex.Bytes{differentAddressCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 3")

	// Test funding coins with balance error
	node.balErr = errors.New("")
	_, err = eth2.FundingCoins([]dex.Bytes{badCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	node.balErr = nil
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 4")

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
func TestGetInitGas(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	node := &testNode{}
	node.initGas = 1800
	node.initGasErr = errors.New("")
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
		acct: new(accounts.Account),
	}

	_, err := eth.getInitGas()
	if err == nil {
		t.Fatalf("expected error but did not get one")
	}

	node.initGasErr = nil
	gas, err := eth.getInitGas()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 1800 {
		t.Fatalf("expected gas to be 1800 but got %d", gas)
	}

	node.initGas = 500
	gas, err = eth.getInitGas()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 1800 {
		t.Fatalf("expected gas to be 1800 but got %d", gas)
	}

	node.initGasErr = errors.New("")
	gas, err = eth.getInitGas()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gas != 1800 {
		t.Fatalf("expected gas to be 1800 but got %d", gas)
	}

	cancel()
}

func TestPreSwap(t *testing.T) {
	ethToGwei := func(eth uint64) uint64 {
		return eth * dexeth.GweiFactor
	}

	ethToWei := func(eth int64) *big.Int {
		return big.NewInt(0).Mul(big.NewInt(eth*dexeth.GweiFactor), big.NewInt(dexeth.GweiFactor))
	}

	estimatedInitGas := uint64(180000)
	hardcodedInitGas := uint64(170000)
	tests := []struct {
		name          string
		bal           *big.Int
		balErr        error
		initGasErr    error
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
			wantMaxFees:   100 * estimatedInitGas,
			wantBestCase:  90 * estimatedInitGas,
			wantWorstCase: 90 * estimatedInitGas,
			wantLocked:    ethToGwei(10) + (100 * estimatedInitGas),
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
			wantMaxFees:   4 * 100 * estimatedInitGas,
			wantBestCase:  90 * estimatedInitGas,
			wantWorstCase: 4 * 90 * estimatedInitGas,
			wantLocked:    ethToGwei(40) + (4 * 100 * estimatedInitGas),
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
		{
			name:          "initGasError",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			initGasErr:    errors.New(""),
			lots:          5,

			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * hardcodedInitGas,
			wantBestCase:  90 * hardcodedInitGas,
			wantWorstCase: 5 * 90 * hardcodedInitGas,
			wantLocked:    ethToGwei(50) + (5 * 100 * hardcodedInitGas),
		},
	}

	dexAsset := dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     hardcodedInitGas,
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
		node.initGasErr = test.initGasErr
		node.initGas = estimatedInitGas
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
		return eth * dexeth.GweiFactor
	}

	ethToWei := func(eth int64) *big.Int {
		return big.NewInt(0).Mul(big.NewInt(eth*dexeth.GweiFactor), big.NewInt(dexeth.GweiFactor))
	}

	estimatedInitGas := uint64(180000)
	hardcodedInitGas := uint64(170000)
	tests := []struct {
		name          string
		bal           *big.Int
		balErr        error
		initGasErr    error
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
			wantMaxFees:   100 * estimatedInitGas,
			wantBestCase:  90 * estimatedInitGas,
			wantWorstCase: 90 * estimatedInitGas,
			wantLocked:    ethToGwei(10) + (100 * estimatedInitGas),
		},
		{
			name:          "multiple lots",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * estimatedInitGas,
			wantBestCase:  90 * estimatedInitGas,
			wantWorstCase: 5 * 90 * estimatedInitGas,
			wantLocked:    ethToGwei(50) + (5 * 100 * estimatedInitGas),
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
		{
			name:          "initGasError",
			bal:           ethToWei(51),
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			initGasErr:    errors.New(""),
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * hardcodedInitGas,
			wantBestCase:  90 * hardcodedInitGas,
			wantWorstCase: 5 * 90 * hardcodedInitGas,
			wantLocked:    ethToGwei(50) + (5 * 100 * hardcodedInitGas),
		},
	}

	dexAsset := dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     hardcodedInitGas,
		SwapSizeBase: 0,
		SwapConf:     1,
	}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{}
		node.bal = test.bal
		node.balErr = test.balErr
		node.initGasErr = test.initGasErr
		node.initGas = estimatedInitGas
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
