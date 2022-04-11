//go:build !harness && lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)

	testAddressA = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	testAddressB = common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	testAddressC = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")

	ethGases = dexeth.VersionedGases[0]

	tETH = &dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     ethGases.Swap,
		SwapSizeBase: ethGases.Swap,
		RedeemSize:   ethGases.Redeem,
		SwapConf:     1,
	}
)

type testNode struct {
	acct           *accounts.Account
	addr           common.Address
	connectErr     error
	bestHdr        *types.Header
	bestHdrErr     error
	syncProg       ethereum.SyncProgress
	bal            *big.Int
	balErr         error
	signDataErr    error
	privKey        *ecdsa.PrivateKey
	swapVers       map[uint32]struct{} // For SwapConfirmations -> swap. TODO for other contractor methods
	swapMap        map[[32]byte]*dexeth.SwapState
	refundable     bool
	baseFee        *big.Int
	tip            *big.Int
	netFeeStateErr error

	sendTxTx   *types.Transaction
	sendTxErr  error
	simBackend bind.ContractBackend
	maxFeeRate *big.Int
	pendingTxs []*types.Transaction
	contractor *tContractor
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

func (n *testNode) contractBackend() bind.ContractBackend {
	return n.simBackend
}

func (n *testNode) chainConfig() *params.ChainConfig {
	return params.AllEthashProtocolChanges
}

func (n *testNode) pendingTransactions() ([]*types.Transaction, error) {
	return n.pendingTxs, nil
}

func (n *testNode) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate *big.Int) (*bind.TransactOpts, error) {
	if maxFeeRate == nil {
		maxFeeRate = n.maxFeeRate
	}
	return newTxOpts(ctx, n.addr, val, maxGas, maxFeeRate, dexeth.GweiToWei(2)), nil
}

func (n *testNode) currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error) {
	return n.baseFee, n.tip, n.netFeeStateErr
}

func (n *testNode) shutdown() {}
func (n *testNode) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.bestHdr, n.bestHdrErr
}
func (n *testNode) addressBalance(ctx context.Context, addr common.Address) (*big.Int, error) {
	return n.bal, n.balErr
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
func (n *testNode) peerCount() uint32 {
	return 1
}

func (n *testNode) signData(data []byte) (sig, pubKey []byte, err error) {
	if n.signDataErr != nil {
		return nil, nil, n.signDataErr
	}

	if n.privKey == nil {
		return nil, nil, nil
	}

	sig, err = crypto.Sign(crypto.Keccak256(data), n.privKey)
	if err != nil {
		return nil, nil, err
	}

	return sig, crypto.FromECDSAPub(&n.privKey.PublicKey), nil
}
func (n *testNode) sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error) {
	return n.sendTxTx, n.sendTxErr
}
func (n *testNode) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	return nil
}

func tTx(gasFeeCap, gasTipCap, value uint64, to *common.Address, data []byte) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{
		GasFeeCap: dexeth.GweiToWei(gasFeeCap),
		GasTipCap: dexeth.GweiToWei(gasTipCap),
		To:        to,
		Value:     dexeth.GweiToWei(value),
		Data:      data,
	})
}

func (n *testNode) transactionConfirmations(context.Context, common.Hash) (uint32, error) {
	return 0, nil
}

type tContractor struct {
	gasEstimates  *dexeth.Gases
	swapMap       map[[32]byte]*dexeth.SwapState
	swapErr       error
	initTx        *types.Transaction
	initErr       error
	redeemTx      *types.Transaction
	redeemErr     error
	refundTx      *types.Transaction
	refundErr     error
	initGasErr    error
	redeemGasErr  error
	refundGasErr  error
	redeemable    bool
	redeemableErr error
	valueIn       map[common.Hash]uint64
	// valueOut      uint64
	valueErr      error
	refundable    bool
	refundableErr error
	lastRefund    struct {
		// tx          *types.Transaction
		secretHash [32]byte
		maxFeeRate *big.Int
		// contractVer uint32
	}
}

func tNewContractor(g *dexeth.Gases) *tContractor {
	return &tContractor{
		gasEstimates: g,
		swapMap:      make(map[[32]byte]*dexeth.SwapState),
		valueIn:      make(map[common.Hash]uint64),
	}
}

func (c *tContractor) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	if c.swapErr != nil {
		return nil, c.swapErr
	}
	swap, ok := c.swapMap[secretHash]
	if !ok {
		return nil, errors.New("swap not in map")
	}
	return swap, nil
}

func (c *tContractor) initiate(*bind.TransactOpts, []*asset.Contract) (*types.Transaction, error) {
	return c.initTx, c.initErr
}

func (c *tContractor) redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error) {
	return c.redeemTx, c.redeemErr
}

func (c *tContractor) refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	c.lastRefund.secretHash = secretHash
	c.lastRefund.maxFeeRate = opts.GasFeeCap
	return c.refundTx, c.refundErr
}

func (c *tContractor) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	return c.gasEstimates.SwapN(n), c.initGasErr
}

func (c *tContractor) estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error) {
	return c.gasEstimates.RedeemN(len(secrets)), c.redeemGasErr
}

func (c *tContractor) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	return c.gasEstimates.Refund, c.refundGasErr
}

func (c *tContractor) isRedeemable(secretHash, secret [32]byte) (bool, error) {
	return c.redeemable, c.redeemableErr
}

func (c *tContractor) incomingValue(_ context.Context, tx *types.Transaction) (incoming uint64, err error) {
	return c.valueIn[tx.Hash()], c.valueErr
}

func (c *tContractor) isRefundable(secretHash [32]byte) (bool, error) {
	return c.refundable, c.refundableErr
}

func TestCheckForNewBlocks(t *testing.T) {
	header0 := &types.Header{Number: new(big.Int)}
	header1 := &types.Header{Number: big.NewInt(1)}
	tests := []struct {
		name                  string
		hashErr, blockErr     error
		bestHeader            *types.Header
		wantErr, hasTipChange bool
	}{{
		name:         "ok",
		bestHeader:   header1,
		hasTipChange: true,
	}, {
		name:       "ok same hash",
		bestHeader: header0,
	}, {
		name:         "block error",
		bestHeader:   header1,
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

		node.bestHdr = test.bestHeader
		node.bestHdrErr = test.blockErr
		w := &ExchangeWallet{
			node:       node,
			addr:       node.address(),
			ctx:        ctx,
			log:        tLogger,
			currentTip: header0,
			tipChange:  tipChange,
		}
		w.checkForNewBlocks()

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
		subSecs: dexeth.MaxBlockInterval + 1,
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}}

	for _, test := range tests {
		nowInSecs := uint64(time.Now().Unix())
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

func newTestNode() *testNode {
	privKey, _ := crypto.HexToECDSA("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")
	addr := common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
	acct := &accounts.Account{
		Address: addr,
	}
	return &testNode{
		acct:       acct,
		addr:       acct.Address,
		maxFeeRate: dexeth.GweiToWei(100),
		baseFee:    dexeth.GweiToWei(100),
		tip:        dexeth.GweiToWei(2),
		privKey:    privKey,
		contractor: tNewContractor(ethGases),
	}
}

func tAssetWallet() (*ExchangeWallet, *testNode, context.CancelFunc) {
	node := newTestNode()
	ctx, cancel := context.WithCancel(context.Background())
	w := &ExchangeWallet{
		addr:               node.addr,
		node:               node,
		ctx:                ctx,
		log:                tLogger,
		gasFeeLimit:        defaultGasFeeLimit,
		contractors:        map[uint32]contractor{0: node.contractor},
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
	}

	return w, node, cancel
}

func TestBalance(t *testing.T) {
	tinyBal := newBalance(0, 0, 0)
	tinyBal.Current = big.NewInt(dexeth.GweiFactor - 1)

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
		wantLocked: 0,
	}, {
		name:         "ok pending in",
		bal:          newBalance(1e8, 3e8, 0),
		wantBal:      1e8,
		wantImmature: 3e8,
	}, {
		name:         "ok pending out and in",
		bal:          newBalance(4e8, 2e8, 1e8),
		wantBal:      3e8,
		wantLocked:   0,
		wantImmature: 2e8,
	}, {
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
		bal:     newBalance(0, 0, 0),
		balErr:  errors.New(""),
		wantErr: true,
	}}

	for _, test := range tests {
		eth, node, shutdown := tAssetWallet()
		defer shutdown()

		node.bal = test.bal.Current
		node.balErr = test.balErr

		var nonce uint64
		newTx := func(value *big.Int) *types.Transaction {
			nonce++
			signer := types.LatestSignerForChainID(node.chainConfig().ChainID)
			tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
				Nonce: nonce,
				Value: value,
			}), signer, node.privKey)
			if err != nil {
				t.Fatalf("SignTx error: %v", err)
			}
			return tx
		}

		if test.bal.PendingIn.Cmp(new(big.Int)) > 0 {
			tx := newTx(new(big.Int))
			node.pendingTxs = append(node.pendingTxs, tx)
			node.contractor.valueIn[tx.Hash()] = dexeth.WeiToGwei(test.bal.PendingIn)
		}
		if test.bal.PendingOut.Cmp(new(big.Int)) > 0 {
			node.pendingTxs = append(node.pendingTxs, newTx(test.bal.PendingOut))
		}

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

func TestFeeRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	eth := &ExchangeWallet{
		node: node,
		ctx:  ctx,
		log:  tLogger,
	}

	maxInt := ^int64(0)
	tests := []struct {
		name           string
		baseFee        *big.Int
		tip            *big.Int
		netFeeStateErr error
		wantFeeRate    uint64
	}{
		{
			name:        "ok",
			baseFee:     big.NewInt(100e9),
			tip:         big.NewInt(2e9),
			wantFeeRate: 202,
		},
		{
			name:           "net fee state error",
			netFeeStateErr: errors.New(""),
		},
		{
			name:    "overflow error",
			baseFee: big.NewInt(maxInt),
			tip:     big.NewInt(1),
		},
	}

	for _, test := range tests {
		node.baseFee = test.baseFee
		node.tip = test.tip
		node.netFeeStateErr = test.netFeeStateErr
		feeRate := eth.FeeRate()
		if feeRate != test.wantFeeRate {
			t.Fatalf("%v: expected fee rate %d but got %d", test.name, test.wantFeeRate, feeRate)
		}
	}
}

func TestRefund(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	const feeSuggestion = 100
	const gweiBal = 1e9
	const ogRefundReserves = 1e8

	gasesV1 := &dexeth.Gases{Refund: 1e5}
	dexeth.VersionedGases[1] = gasesV1
	v1Contractor := tNewContractor(gasesV1)

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	v0Contract := dexeth.EncodeContractData(0, secretHash)
	ss := new(dexeth.SwapState)
	v0Contractor := node.contractor
	v0Contractor.swapMap[secretHash] = ss
	v1Contractor.swapMap[secretHash] = ss

	tests := []struct {
		name            string
		badContract     bool
		isRefundable    bool
		isRefundableErr error
		refundErr       error
		wantLocked      uint64
		wantErr         bool
		wantZeroHash    bool
		swapStep        dexeth.SwapStep
		swapErr         error
		useV1Gases      bool
	}{
		{
			name:         "ok",
			swapStep:     dexeth.SSInitiated,
			isRefundable: true,
			wantLocked:   ogRefundReserves - feeSuggestion*dexeth.RefundGas(0),
		},
		{
			name:         "ok v1",
			swapStep:     dexeth.SSInitiated,
			isRefundable: true,
			wantLocked:   ogRefundReserves - feeSuggestion*dexeth.RefundGas(1),
			useV1Gases:   true,
		},
		{
			name:         "ok refunded",
			swapStep:     dexeth.SSRefunded,
			isRefundable: true,
			wantLocked:   ogRefundReserves - feeSuggestion*dexeth.RefundGas(0),
			wantZeroHash: true,
		},
		{
			name:     "swap error",
			swapStep: dexeth.SSInitiated,
			swapErr:  errors.New(""),
			wantErr:  true,
		},
		{
			name:            "is refundable error",
			isRefundable:    true,
			isRefundableErr: errors.New(""),
			wantErr:         true,
		},
		{
			name:         "is refundable false",
			isRefundable: false,
			wantErr:      true,
		},
		{
			name:         "refund error",
			isRefundable: true,
			refundErr:    errors.New(""),
			wantErr:      true,
		},
		{
			name:         "cannot decode contract",
			badContract:  true,
			isRefundable: true,
			wantErr:      true,
		},
	}

	for _, test := range tests {
		contract := v0Contract
		node.contractor = v0Contractor
		if test.useV1Gases {
			contract = dexeth.EncodeContractData(1, secretHash)
			node.contractor = v1Contractor
			eth.contractors[1] = node.contractor
			defer delete(eth.contractors, 1)
		} else if test.badContract {
			contract = []byte{}
		}

		node.contractor.refundable = test.isRefundable
		node.contractor.refundableErr = test.isRefundableErr
		node.contractor.refundErr = test.refundErr
		node.bal = dexeth.GweiToWei(gweiBal)
		node.contractor.swapErr = test.swapErr
		ss.State = test.swapStep
		eth.lockedFunds.refundReserves = ogRefundReserves

		var txHash common.Hash
		if test.isRefundable {
			tx := types.NewTx(&types.DynamicFeeTx{})
			txHash = tx.Hash()
			node.contractor.refundTx = tx
		}

		refundID, err := eth.Refund(nil, contract, feeSuggestion)

		if test.wantErr {
			if err == nil {
				t.Fatalf(`%v: expected error but did not get: %v`, test.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf(`%v: unexpected error: %v`, test.name, err)
		}

		if test.wantZeroHash {
			// No on chain refund expected if status was already refunded.
			zeroHash := common.Hash{}
			if !bytes.Equal(refundID, zeroHash[:]) {
				t.Fatalf(`%v: expected refund tx hash: %x = returned id: %s`, test.name, zeroHash, refundID)
			}
		} else {
			if !bytes.Equal(refundID, txHash[:]) {
				t.Fatalf(`%v: expected refund tx hash: %x = returned id: %s`, test.name, txHash, refundID)
			}

			if secretHash != node.contractor.lastRefund.secretHash {
				t.Fatalf(`%v: secret hash in contract %x != used to call refund %x`,
					test.name, secretHash, node.contractor.lastRefund.secretHash)
			}

			if dexeth.GweiToWei(feeSuggestion).Cmp(node.contractor.lastRefund.maxFeeRate) != 0 {
				t.Fatalf(`%v: fee suggestion %v != used to call refund %v`,
					test.name, dexeth.GweiToWei(feeSuggestion), node.contractor.lastRefund.maxFeeRate)
			}
		}

		if test.wantLocked > 0 && eth.lockedFunds.refundReserves != test.wantLocked {
			t.Fatalf("%v: refund reserves %v != expected %v",
				test.name, eth.lockedFunds.refundReserves, test.wantLocked)
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
	eth, node, shutdown := tAssetWallet()
	defer shutdown()
	walletBalanceGwei := uint64(dexeth.GweiFactor)
	node.bal = dexeth.GweiToWei(walletBalanceGwei)

	checkBalance := func(wallet *ExchangeWallet, expectedAvailable, expectedLocked uint64, testName string) {
		t.Helper()
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
			t.Fatalf("%s: unexpected error: %v", test.testName, err)
		}
		if len(coins) != 1 {
			t.Fatalf("%s: expected 1 coins but got %v", test.testName, len(coins))
		}
		if len(redeemScripts) != 1 {
			t.Fatalf("%s: expected 1 redeem script but got %v", test.testName, len(redeemScripts))
		}
		rc, is := coins[0].(asset.RecoveryCoin)
		if !is {
			t.Fatalf("%s: funding coin is not a RecoveryCoin", test.testName)
		}
		_, err = eth.decodeFundingCoinID(rc.RecoveryID())
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.testName, err)
		}
		if coins[0].Value() != test.coinValue {
			t.Fatalf("%s: wrong value. expected %v but got %v", test.testName, test.coinValue, coins[0].Value())
		}
	}

	order := asset.Order{
		Value:        500000000,
		MaxSwapCount: 2,
		DEXConfig:    tETH,
	}

	// Test fund order with less than available funds
	coins1, redeemScripts1, err := eth.FundOrder(&order)
	expectedOrderFees := order.DEXConfig.SwapSize * order.DEXConfig.MaxFeeRate * order.MaxSwapCount
	expectedFees := expectedOrderFees
	expectedCoinValue := order.Value + expectedOrderFees

	checkFundOrderResult(coins1, redeemScripts1, err, fundOrderTest{
		testName:    "more than enough",
		coinValue:   expectedCoinValue,
		coinAddress: node.addr.String(),
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
		coinValue:   order.Value + expectedFees,
		coinAddress: node.addr.String(),
	})
	checkBalance(eth, 0, walletBalanceGwei, "just enough")

	// Test returning funds > locked returns locked funds to 0
	err = eth.ReturnCoins([]asset.Coin{coins1[0], coins1[0]})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, walletBalanceGwei, 0, "after return too much")

	// Fund order with funds equal to available
	order.Value = walletBalanceGwei - expectedOrderFees
	_, _, err = eth.FundOrder(&order)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, 0, walletBalanceGwei, "just enough 2")

	// Test returning coin with invalid ID
	var badCoin badCoin
	err = eth.ReturnCoins([]asset.Coin{&badCoin})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}

	// Test returning correct coins returns all funds
	err = eth.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, walletBalanceGwei, 0, "returned correct amount")

	node.balErr = errors.New("")
	_, _, err = eth.FundOrder(&order)
	if err == nil {
		t.Fatalf("balance error should cause error but did not")
	}
	node.balErr = nil

	// Test eth wallet gas fee limit > server MaxFeeRate causes error
	tmpGasFeeLimit := eth.gasFeeLimit
	eth.gasFeeLimit = order.DEXConfig.MaxFeeRate - 1
	_, _, err = eth.FundOrder(&order)
	if err == nil {
		t.Fatalf("eth wallet gas fee limit > server MaxFeeRate should cause error")
	}
	eth.gasFeeLimit = tmpGasFeeLimit

	eth2, _, shutdown2 := tAssetWallet()
	defer shutdown2()
	eth2.node = node

	// Test reloading coins from first order
	coins, err = eth2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0])})
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
	_, err = eth2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0])})
	if err == nil {
		t.Fatalf("expected error but didn't get one")
	}
	checkBalance(eth2, walletBalanceGwei-coins1[0].Value(), coins1[0].Value(), "after funding error 1")

	// Test funding coins with bad coin ID
	_, err = eth2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0])})
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

	differentKindaCoin := (&coin{
		id: encode.RandomBytes(20), // e.g. tx hash
	})
	_, err = eth2.FundingCoins([]dex.Bytes{differentKindaCoin.ID()})
	if err == nil {
		t.Fatalf("expected error for unknown coin id format, but did not get")
	}

	differentAddressCoin := createFundingCoin(differentAddress, 100000)
	_, err = eth2.FundingCoins([]dex.Bytes{differentAddressCoin.ID()})
	if err == nil {
		t.Fatalf("expected error for wrong address, but did not get")
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
	coins, err = eth2.FundingCoins([]dex.Bytes{parseRecoveryID(coins2[0])})
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

	// return coin with wrong kind and incorrect address
	err = eth2.ReturnCoins([]asset.Coin{differentKindaCoin})
	if err == nil {
		t.Fatalf("expected error for unknown coin ID format, but did not get")
	}

	err = eth2.ReturnCoins([]asset.Coin{differentAddressCoin})
	if err == nil {
		t.Fatalf("expected error for wrong address, but did not get")
	}

	// return all coins
	err = eth2.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error")
	}
	checkBalance(eth2, walletBalanceGwei, 0, "return coins after funding")

	// Test funding coins with two coins at the same time
	_, err = eth2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0]), parseRecoveryID(coins2[0])})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth2, 0, walletBalanceGwei, "funding3")
}

func TestPreSwap(t *testing.T) {
	const feeSuggestion = 90
	const lotSize = 10e9
	oneFee := ethGases.Swap * tETH.MaxFeeRate
	oneLock := lotSize + oneFee

	tests := []struct {
		name   string
		bal    uint64
		balErr error
		lots   uint64

		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
	}{
		{
			name: "no balance",
			bal:  0,
			lots: 1,

			wantErr: true,
		},
		{
			name: "not enough for fees",
			bal:  lotSize,
			lots: 1,

			wantErr: true,
		},
		{
			name: "one lot enough for fees",
			bal:  oneLock,
			lots: 1,

			wantLots:      1,
			wantValue:     lotSize,
			wantMaxFees:   tETH.MaxFeeRate * ethGases.Swap,
			wantBestCase:  feeSuggestion * ethGases.Swap,
			wantWorstCase: feeSuggestion * ethGases.Swap,
		},
		{
			name: "more lots than max lots",
			bal:  oneLock*2 - 1,
			lots: 2,

			wantErr: true,
		},
		{
			name: "fewer than max lots",
			bal:  10 * oneLock,
			lots: 4,

			wantLots:      4,
			wantValue:     4 * lotSize,
			wantMaxFees:   4 * tETH.MaxFeeRate * ethGases.Swap,
			wantBestCase:  feeSuggestion * ethGases.SwapN(4),
			wantWorstCase: 4 * feeSuggestion * ethGases.Swap,
		},
		{
			name:   "balanceError",
			bal:    5 * lotSize,
			balErr: errors.New(""),
			lots:   1,

			wantErr: true,
		},
	}

	for _, test := range tests {
		eth, node, shutdown := tAssetWallet()
		defer shutdown()
		node.bal = dexeth.GweiToWei(test.bal)
		node.balErr = test.balErr

		preSwap, err := eth.PreSwap(&asset.PreSwapForm{
			LotSize:       lotSize,
			Lots:          test.lots,
			AssetConfig:   tETH,
			FeeSuggestion: feeSuggestion,
		})

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
	}
}

func TestSwap(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()
	receivingAddress := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	node.contractor.initTx = types.NewTx(&types.DynamicFeeTx{})

	coinIDsForAmounts := func(coinAmounts []uint64) []dex.Bytes {
		coinIDs := make([]dex.Bytes, 0, len(coinAmounts))
		for _, amt := range coinAmounts {
			fundingCoinID := createFundingCoin(eth.addr, amt)
			coinIDs = append(coinIDs, fundingCoinID.RecoveryID())
		}
		return coinIDs
	}

	refreshWalletAndFundCoins := func(ethBalance uint64, coinAmounts []uint64) asset.Coins {
		node.bal = ethToWei(ethBalance)
		eth.lockedFunds.initiateReserves = 0
		eth.lockedFunds.redemptionReserves = 0
		eth.lockedFunds.refundReserves = 0

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
		t.Helper()
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
			ver, secretHash, err := dexeth.DecodeContractData(contractData)
			if err != nil {
				t.Fatalf("failed to decode contract data: %v", err)
			}
			if swaps.AssetVersion != ver {
				t.Fatal("wrong contract version")
			}
			if !bytes.Equal(contract.SecretHash, secretHash[:]) {
				t.Fatalf("%v, contract: %x != secret hash in input: %x",
					testName, receipt.Contract(), secretHash)
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
	node.contractor.initErr = errors.New("")
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
	node.contractor.initErr = nil

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
	node := newTestNode()
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

func TestRedeem(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	// Test with a non-zero contract version to ensure it makes it into the receipt
	contractVer := uint32(1)
	dexeth.VersionedGases[1] = ethGases // for dexeth.RedeemGas(..., 1)
	defer delete(dexeth.VersionedGases, 1)

	node.bal = dexeth.GweiToWei(10e9)

	contractorV1 := &tContractor{
		swapMap:      make(map[[32]byte]*dexeth.SwapState, 1),
		gasEstimates: ethGases,
		redeemTx:     types.NewTx(&types.DynamicFeeTx{}),
	}
	eth.contractors[1] = contractorV1

	addSwapToSwapMap := func(secretHash [32]byte, value uint64, step dexeth.SwapStep) {
		swap := dexeth.SwapState{
			BlockHeight: 1,
			LockTime:    time.Now(),
			Initiator:   testAddressB,
			Participant: testAddressA,
			Value:       value,
			State:       step,
		}
		contractorV1.swapMap[secretHash] = &swap
	}

	numSecrets := 3
	secrets := make([][32]byte, 0, numSecrets)
	secretHashes := make([][32]byte, 0, numSecrets)
	for i := 0; i < numSecrets; i++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		secretHash := sha256.Sum256(secret[:])
		secrets = append(secrets, secret)
		secretHashes = append(secretHashes, secretHash)
	}

	addSwapToSwapMap(secretHashes[0], 1e9, dexeth.SSInitiated)
	addSwapToSwapMap(secretHashes[1], 1e9, dexeth.SSInitiated)

	tests := []struct {
		name            string
		form            asset.RedeemForm
		redeemErr       error
		isRedeemable    bool
		isRedeemableErr error
		expectError     bool
	}{
		{
			name:         "ok",
			expectError:  false,
			isRedeemable: true,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:         "not redeemable",
			expectError:  true,
			isRedeemable: false,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:            "isRedeemable error",
			expectError:     true,
			isRedeemable:    true,
			isRedeemableErr: errors.New(""),
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:         "redeem error",
			redeemErr:    errors.New(""),
			isRedeemable: true,
			expectError:  true,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[0][:],
					},
				},
				FeeSuggestion: 200,
			},
		},
		{
			name:         "swap not found in contract",
			isRedeemable: true,
			expectError:  true,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[2]),
							SecretHash: secretHashes[2][:],
							Coin: &coin{
								id: encode.RandomBytes(32),
							},
						},
						Secret: secrets[2][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:         "empty redemptions slice error",
			isRedeemable: true,
			expectError:  true,
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{},
				FeeSuggestion: 100,
			},
		},
	}

	for _, test := range tests {
		contractorV1.redeemErr = test.redeemErr
		contractorV1.redeemable = test.isRedeemable
		contractorV1.redeemableErr = test.isRedeemableErr

		txs, out, fees, err := eth.Redeem(&test.form)
		if test.expectError {
			if err == nil {
				t.Fatalf("%v: expected error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%v: unexpected Redeem error: %v", test.name, err)
		}

		if len(txs) != len(test.form.Redemptions) {
			t.Fatalf("%v: expected %d txn but got %d",
				test.name, len(test.form.Redemptions), len(txs))
		}

		// Check fees returned from Redeem are as expected
		expectedGas := dexeth.RedeemGas(len(test.form.Redemptions), 0)
		expectedFees := expectedGas * test.form.FeeSuggestion
		if fees != expectedFees {
			t.Fatalf("%v: expected fees %d, but got %d", test.name, expectedFees, fees)
		}

		var totalSwapValue uint64
		for _, redemption := range test.form.Redemptions {
			_, secretHash, err := dexeth.DecodeContractData(redemption.Spends.Contract)
			if err != nil {
				t.Fatalf("DecodeContractData: %v", err)
			}
			// secretHash should equal redemption.Spends.SecretHash, but it's
			// not part of the Redeem code, just the test input consistency.
			swap := contractorV1.swapMap[secretHash]
			totalSwapValue += swap.Value
		}

		if out.Value() != totalSwapValue {
			t.Fatalf("expected coin value to be %d but got %d",
				totalSwapValue, out.Value())
		}
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
			wantMaxFees:   100 * gases.Swap,
			wantBestCase:  90 * gases.Swap,
			wantWorstCase: 90 * gases.Swap,
			wantLocked:    ethToGwei(10) + (100 * gases.Swap),
		},
		{
			name:          "multiple lots",
			bal:           51,
			lotSize:       ethToGwei(10),
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * gases.Swap,
			wantBestCase:  90 * gases.SwapN(5),
			wantWorstCase: 5 * 90 * gases.Swap,
			wantLocked:    ethToGwei(50) + (5 * 100 * gases.Swap),
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

	dexAsset := &dex.Asset{
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     gases.Swap,
		SwapSizeBase: gases.Swap,
		SwapConf:     1,
	}

	for _, test := range tests {
		eth, node, shutdown := tAssetWallet()
		defer shutdown()

		node.bal = dexeth.GweiToWei(test.bal * 1e9)
		node.balErr = test.balErr

		dexAsset.MaxFeeRate = test.maxFeeRate

		maxOrder, err := eth.MaxOrder(test.lotSize, test.feeSuggestion, dexAsset)
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
	}
}

func overMaxWei() *big.Int {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(dexeth.GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	return overMaxWei.Add(overMaxWei, gweiFactorBig)
}

func packInitiateDataV0(initiations []*dexeth.Initiation) ([]byte, error) {
	abiInitiations := make([]swapv0.ETHSwapInitiation, 0, len(initiations))
	for _, init := range initiations {
		bigVal := new(big.Int).SetUint64(init.Value)
		abiInitiations = append(abiInitiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(init.LockTime.Unix()),
			SecretHash:      init.SecretHash,
			Participant:     init.Participant,
			Value:           new(big.Int).Mul(bigVal, big.NewInt(dexeth.GweiFactor)),
		})
	}
	return (*dexeth.ABIs[0]).Pack("initiate", abiInitiations)
}

func TestAuditContract(t *testing.T) {
	node := &testNode{}
	eth := &ExchangeWallet{
		node: node,
		log:  tLogger,
	}

	numSecretHashes := 3
	secretHashes := make([][32]byte, 0, numSecretHashes)
	for i := 0; i < numSecretHashes; i++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		secretHashes = append(secretHashes, secretHash)
	}

	now := time.Now()
	laterThanNow := now.Add(time.Hour)

	tests := []struct {
		name           string
		contract       dex.Bytes
		initiations    []*dexeth.Initiation
		differentHash  bool
		badTxData      bool
		badTxBinary    bool
		wantErr        bool
		wantRecipient  string
		wantExpiration time.Time
	}{
		{
			name:     "ok",
			contract: dexeth.EncodeContractData(0, secretHashes[1]),
			initiations: []*dexeth.Initiation{
				{
					LockTime:    now,
					SecretHash:  secretHashes[0],
					Participant: testAddressA,
					Value:       1,
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       1,
				},
			},
			wantRecipient:  testAddressB.Hex(),
			wantExpiration: laterThanNow,
		},
		{
			name:     "coin id different than tx hash",
			contract: dexeth.EncodeContractData(0, secretHashes[0]),
			initiations: []*dexeth.Initiation{
				{
					LockTime:    now,
					SecretHash:  secretHashes[0],
					Participant: testAddressA,
					Value:       1,
				},
			},
			differentHash: true,
			wantErr:       true,
		},
		{
			name:     "contract is invalid versioned bytes",
			contract: []byte{},
			wantErr:  true,
		},
		{
			name:     "contract not part of transaction",
			contract: dexeth.EncodeContractData(0, secretHashes[2]),
			initiations: []*dexeth.Initiation{
				{
					LockTime:    now,
					SecretHash:  secretHashes[0],
					Participant: testAddressA,
					Value:       1,
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       1,
				},
			},
			wantErr: true,
		},
		{
			name:      "cannot parse tx data",
			contract:  dexeth.EncodeContractData(0, secretHashes[2]),
			badTxData: true,
			wantErr:   true,
		},
		{
			name:     "cannot unmarshal tx binary",
			contract: dexeth.EncodeContractData(0, secretHashes[1]),
			initiations: []*dexeth.Initiation{
				{
					LockTime:    now,
					SecretHash:  secretHashes[0],
					Participant: testAddressA,
					Value:       1,
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       1,
				},
			},
			badTxBinary: true,
			wantErr:     true,
		},
	}

	for _, test := range tests {
		txData, err := packInitiateDataV0(test.initiations)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if test.badTxData {
			txData = []byte{0}
		}

		tx := tTx(2, 300, uint64(len(test.initiations)), &testAddressC, txData)
		txBinary, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf(`"%v": failed to marshal binary: %v`, test.name, err)
		}
		if test.badTxBinary {
			txBinary = []byte{0}
		}

		txHash := tx.Hash()
		if test.differentHash {
			copy(txHash[:], encode.RandomBytes(32))
		}

		auditInfo, err := eth.AuditContract(txHash[:], test.contract, txBinary, true)
		if test.wantErr {
			if err == nil {
				t.Fatalf(`"%v": expected error but did not get`, test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf(`"%v": unexpected error: %v`, test.name, err)
		}

		if test.wantRecipient != auditInfo.Recipient {
			t.Fatalf(`"%v": expected recipient %v != actual %v`, test.name, test.wantRecipient, auditInfo.Recipient)
		}
		if test.wantExpiration.Unix() != auditInfo.Expiration.Unix() {
			t.Fatalf(`"%v": expected expiration %v != actual %v`, test.name, test.wantExpiration, auditInfo.Expiration)
		}
		if !bytes.Equal(txHash[:], auditInfo.Coin.ID()) {
			t.Fatalf(`"%v": tx hash %x != coin id %x`, test.name, txHash, auditInfo.Coin.ID())
		}
		if !bytes.Equal(test.contract, auditInfo.Contract) {
			t.Fatalf(`"%v": expected contract %x != actual %x`, test.name, test.contract, auditInfo.Contract)
		}

		_, expectedSecretHash, err := dexeth.DecodeContractData(test.contract)
		if err != nil {
			t.Fatalf(`"%v": failed to decode versioned bytes: %v`, test.name, err)
		}
		if !bytes.Equal(expectedSecretHash[:], auditInfo.SecretHash) {
			t.Fatalf(`"%v": expected secret hash %x != actual %x`, test.name, expectedSecretHash, auditInfo.SecretHash)
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
	node := newTestNode()
	eth := &ExchangeWallet{
		node: node,
		addr: node.address(),
		ctx:  ctx,
		log:  tLogger,
	}

	msg := []byte("msg")

	// SignData error
	node.signDataErr = errors.New("")
	_, _, err := eth.SignMessage(nil, msg)
	if err == nil {
		t.Fatalf("expected error due to error in rpcclient signData")
	}
	node.signDataErr = nil

	// Test no error
	pubKeys, sigs, err := eth.SignMessage(nil, msg)
	if err != nil {
		t.Fatalf("unexpected error signing message: %v", err)
	}
	if len(pubKeys) != 1 {
		t.Fatalf("expected 1 pubKey but got %v", len(pubKeys))
	}
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signature but got %v", len(sigs))
	}
	if !crypto.VerifySignature(pubKeys[0], crypto.Keccak256(msg), sigs[0][:len(sigs[0])-1]) {
		t.Fatalf("failed to verify signature")
	}
}

func TestSwapConfirmation(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	state := &dexeth.SwapState{}
	hdr := &types.Header{}

	node.contractor.swapMap[secretHash] = state

	state.BlockHeight = 5
	state.State = dexeth.SSInitiated
	hdr.Number = big.NewInt(6)
	node.bestHdr = hdr

	ver := uint32(0)

	checkResult := func(expErr bool, expConfs uint32, expSpent bool) {
		confs, spent, err := eth.SwapConfirmations(nil, nil, dexeth.EncodeContractData(ver, secretHash), time.Time{})
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
	node.contractor.swapErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.contractor.swapErr = nil

	// header error
	node.bestHdrErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.bestHdrErr = nil

	// ErrSwapNotInitiated
	state.State = dexeth.SSNone
	_, _, err := eth.SwapConfirmations(nil, nil, dexeth.EncodeContractData(0, secretHash), time.Time{})
	if !errors.Is(err, asset.ErrSwapNotInitiated) {
		t.Fatalf("expected ErrSwapNotInitiated, got %v", err)
	}

	// 1 conf, spent
	state.BlockHeight = 6
	state.State = dexeth.SSRedeemed
	checkResult(false, 1, true)
}

func TestDriverOpen(t *testing.T) {
	drv := &Driver{}
	logger := dex.StdOutLogger("ETHTEST", dex.LevelOff)
	tmpDir := t.TempDir()

	err := CreateWallet(&asset.CreateWalletParams{
		Type:     walletTypeGeth,
		Seed:     encode.RandomBytes(32),
		Pass:     encode.RandomBytes(32),
		Settings: make(map[string]string),
		DataDir:  tmpDir,
		Net:      dex.Testnet,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("CreateWallet error: %v", err)
	}

	// Make sure default gas fee limit is used when nothing is set
	cfg := &asset.WalletConfig{
		Type:     walletTypeGeth,
		Settings: make(map[string]string),
		DataDir:  tmpDir,
	}
	wallet, err := drv.Open(cfg, logger, dex.Testnet)
	if err != nil {
		t.Fatalf("driver open error: %v", err)
	}
	eth, ok := wallet.(*ExchangeWallet)
	if !ok {
		t.Fatalf("failed to cast wallet as ExchangeWallet")
	}
	if eth.gasFeeLimit != defaultGasFeeLimit {
		t.Fatalf("expected gasFeeLimit to be default, but got %v", eth.gasFeeLimit)
	}
	eth.shutdown()

	// Make sure gas fee limit is properly parsed from settings
	cfg.Settings["gasfeelimit"] = "150"
	wallet, err = drv.Open(cfg, logger, dex.Testnet)
	if err != nil {
		t.Fatalf("driver open error: %v", err)
	}
	eth, ok = wallet.(*ExchangeWallet)
	if !ok {
		t.Fatalf("failed to cast wallet as ExchangeWallet")
	}
	if eth.gasFeeLimit != 150 {
		t.Fatalf("expected gasFeeLimit to be 150, but got %v", eth.gasFeeLimit)
	}
}

func TestDriverExists(t *testing.T) {
	drv := &Driver{}
	tmpDir := t.TempDir()

	settings := map[string]string{}

	// no wallet
	exists, err := drv.Exists(walletTypeGeth, tmpDir, settings, dex.Simnet)
	if err != nil {
		t.Fatalf("Exists error for no geth wallet: %v", err)
	}
	if exists {
		t.Fatalf("Uninitiated wallet exists")
	}

	// Create the wallet.
	err = CreateWallet(&asset.CreateWalletParams{
		Type:     walletTypeGeth,
		Seed:     encode.RandomBytes(32),
		Pass:     encode.RandomBytes(32),
		Settings: settings,
		DataDir:  tmpDir,
		Net:      dex.Simnet,
		Logger:   tLogger,
	})
	if err != nil {
		t.Fatalf("CreateWallet error: %v", err)
	}

	// exists
	exists, err = drv.Exists(walletTypeGeth, tmpDir, settings, dex.Simnet)
	if err != nil {
		t.Fatalf("Exists error for existent geth wallet: %v", err)
	}
	if !exists {
		t.Fatalf("Initiated wallet doesn't exist")
	}

	// Wrong wallet type
	if _, err := drv.Exists("not-geth", tmpDir, settings, dex.Simnet); err == nil {
		t.Fatalf("no error for unknown wallet type")
	}
}

func TestDriverDecodeCoinID(t *testing.T) {
	drv := &Driver{}
	addressStr := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	address := common.HexToAddress(addressStr)

	// Test tx hash
	txHash := encode.RandomBytes(common.HashLength)
	coinID, err := drv.DecodeCoinID(txHash)
	if err != nil {
		t.Fatalf("error decoding coin id: %v", err)
	}
	var hash common.Hash
	hash.SetBytes(txHash)
	if coinID != hash.String() {
		t.Fatalf("expected coin id to be %s but got %s", hash.String(), coinID)
	}

	// Test funding coin id
	fundingCoin := createFundingCoin(address, 1000)
	coinID, err = drv.DecodeCoinID(fundingCoin.RecoveryID())
	if err != nil {
		t.Fatalf("error decoding coin id: %v", err)
	}
	if coinID != fundingCoin.String() {
		t.Fatalf("expected coin id to be %s but got %s", fundingCoin.String(), coinID)
	}

	// Test byte encoded address string
	coinID, err = drv.DecodeCoinID([]byte(addressStr))
	if err != nil {
		t.Fatalf("error decoding coin id: %v", err)
	}
	if coinID != addressStr {
		t.Fatalf("expected coin id to be %s but got %s", addressStr, coinID)
	}

	// Test invalid coin id
	_, err = drv.DecodeCoinID(encode.RandomBytes(20))
	if err == nil {
		t.Fatal("expected error but did not get")
	}
}

func TestLocktimeExpired(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))

	state := &dexeth.SwapState{
		LockTime: time.Now(),
		State:    dexeth.SSInitiated,
	}

	header := &types.Header{
		Time: uint64(time.Now().Add(time.Second).Unix()),
	}

	node.contractor.swapMap[secretHash] = state
	node.bestHdr = header

	contract := make([]byte, 36)
	copy(contract[4:], secretHash[:])

	ensureResult := func(tag string, expErr, expExpired bool) {
		t.Helper()
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

	// swap not initiated
	saveState := state.State
	state.State = dexeth.SSNone
	ensureResult("swap not initiated", true, false)
	state.State = saveState

	// missing swap
	delete(node.contractor.swapMap, secretHash)
	ensureResult("missing swap", true, false)
	node.contractor.swapMap[secretHash] = state

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

func TestFindRedemption(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	contract := dexeth.EncodeContractData(0, secretHash)
	state := &dexeth.SwapState{
		Secret: secret,
		State:  dexeth.SSInitiated,
	}

	node.contractor.swapMap[secretHash] = state

	baseCtx := context.Background()

	runTest := func(tag string, wantErr bool, initStep dexeth.SwapStep) {
		// The queue should always be empty.
		eth.findRedemptionMtx.RLock()
		reqsPending := len(eth.findRedemptionReqs) > 0
		eth.findRedemptionMtx.RUnlock()
		if reqsPending {
			t.Fatalf("%s: requests pending at beginning of test", tag)
		}

		state.State = initStep
		var err error
		ctx, cancel := context.WithTimeout(baseCtx, time.Second)
		defer cancel()
		_, secretB, err := eth.FindRedemption(ctx, nil, contract)
		if err != nil {
			if wantErr {
				return
			}
			t.Fatalf("%s: %v", tag, err)
		} else if wantErr {
			t.Fatalf("%s: didn't see expected error", tag)
		}
		if !bytes.Equal(secretB, secret[:]) {
			t.Fatalf("%s: wrong secret. %x != %x", tag, []byte(secretB), secret)
		}
	}

	runWithUpdate := func(tag string, wantErr bool, initStep dexeth.SwapStep, updateFunc func()) {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := time.After(time.Second)
			for {
				select {
				case <-time.After(time.Millisecond):
					eth.findRedemptionMtx.RLock()
					pending := eth.findRedemptionReqs[secretHash] != nil
					eth.findRedemptionMtx.RUnlock()
					if !pending {
						continue
					}
					updateFunc()
					eth.checkFindRedemptions()
				case <-timeout:
					return
				}
			}
		}()
		runTest(tag, wantErr, initStep)
		wg.Wait()
	}

	// Already redeemed.
	runTest("already redeemed", false, dexeth.SSRedeemed)

	// Redeemed after queuing
	runWithUpdate("redeemed after queuing", false, dexeth.SSInitiated, func() {
		state.State = dexeth.SSRedeemed
	})

	// Doesn't exist
	runTest("already refunded", true, dexeth.SSNone)

	// Unknown swap state
	runTest("already refunded", true, dexeth.SwapStep(^uint8(0)))

	// Already refunded
	runTest("already refunded", true, dexeth.SSRefunded)

	// Refunded after queuing
	runWithUpdate("refunded after queuing", true, dexeth.SSInitiated, func() {
		state.State = dexeth.SSRefunded
	})

	// swap error
	node.contractor.swapErr = errors.New("test error")
	runTest("swap error", true, 0)
	node.contractor.swapErr = nil

	// swap error after queuing
	runWithUpdate("swap error after queuing", true, dexeth.SSInitiated, func() {
		node.contractor.swapErr = errors.New("test error")
	})
	node.contractor.swapErr = nil

	// cancelled context error
	var cancel context.CancelFunc
	baseCtx, cancel = context.WithCancel(context.Background())
	cancel()
	runTest("context cancellation", true, dexeth.SSInitiated)
	baseCtx = context.Background()

	// bad contract
	goodContract := contract
	contract = append(contract, 0)
	runTest("bad contract", true, dexeth.SSInitiated)
	contract = goodContract

	// dupe
	eth.findRedemptionMtx.Lock()
	eth.findRedemptionReqs[secretHash] = &findRedemptionRequest{}
	eth.findRedemptionMtx.Unlock()
	res := make(chan error, 1)
	go func() {
		_, _, err := eth.FindRedemption(baseCtx, nil, contract)
		res <- err
	}()

	select {
	case err := <-res:
		if err == nil {
			t.Fatalf("no error for dupe")
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out on dupe test")
	}
}

func TestRefundReserves(t *testing.T) {
	node := newTestNode()
	node.bal = dexeth.GweiToWei(1e9)
	node.refundable = true
	node.swapVers = map[uint32]struct{}{0: {}}

	var secretHash [32]byte
	node.swapMap = map[[32]byte]*dexeth.SwapState{secretHash: {}}

	eth := &ExchangeWallet{
		node: node,
	}

	var maxFeeRateV0 uint64 = 45
	gasesV0 := dexeth.VersionedGases[0]

	gasesV1 := &dexeth.Gases{Refund: 1e6}
	dexeth.VersionedGases[1] = gasesV1
	var maxFeeRateV1 uint64 = 50

	// Lock for 3 refunds with contract version 0
	v0Val, err := eth.ReserveNRefunds(3, maxFeeRateV0, 0)
	if err != nil {
		t.Fatalf("ReserveNRefunds error: %v", err)
	}
	lockPerV0 := gasesV0.Refund * maxFeeRateV0
	expLock := 3 * lockPerV0
	if eth.lockedFunds.refundReserves != expLock {
		t.Fatalf("wrong v0 locked. wanted %d, got %d", expLock, eth.lockedFunds.refundReserves)
	}
	if v0Val != expLock {
		t.Fatalf("expected locked %d, got %d", expLock, v0Val)
	}

	// Lock for 2 refunds with contract version 1
	v1Val, err := eth.ReserveNRefunds(2, maxFeeRateV1, 1)
	if err != nil {
		t.Fatalf("ReserveNRefunds error: %v", err)
	}
	lockPerV1 := gasesV1.Refund * maxFeeRateV1
	v1Lock := 2 * lockPerV1
	expLock += v1Lock
	if eth.lockedFunds.refundReserves != expLock {
		t.Fatalf("wrong v1 locked. wanted %d, got %d", expLock, eth.lockedFunds.refundReserves)
	}
	if v1Val != v1Lock {
		t.Fatalf("expected locked %d, got %d", v1Lock, v1Val)
	}

	eth.UnlockRefundReserves(9e5)
	expLock -= 9e5
	if eth.lockedFunds.refundReserves != expLock {
		t.Fatalf("incorrect amount locked. wanted %d, got %d", expLock, eth.lockedFunds.refundReserves)
	}

	// Reserve more than available should return an error
	err = eth.ReReserveRefund(1e9)
	if err == nil {
		t.Fatalf("expected an error but did not get")
	}

	err = eth.ReReserveRefund(5e6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expLock += 5e6
	if eth.lockedFunds.refundReserves != expLock {
		t.Fatalf("incorrect amount locked. wanted %d, got %d", expLock, eth.lockedFunds.refundReserves)
	}
}

func TestRedemptionReserves(t *testing.T) {
	eth, node, shutdown := tAssetWallet()
	defer shutdown()

	node.bal = dexeth.GweiToWei(1e9)
	node.contractor.redeemable = true

	var secretHash [32]byte
	node.contractor.swapMap[secretHash] = &dexeth.SwapState{}
	spentCoin := &coin{id: encode.RandomBytes(32)}

	var maxFeeRateV0 uint64 = 45
	gasesV0 := dexeth.VersionedGases[0]

	gasesV1 := &dexeth.Gases{Redeem: 1e6, RedeemAdd: 85e5}
	dexeth.VersionedGases[1] = gasesV1
	var maxFeeRateV1 uint64 = 50

	v0Val, err := eth.ReserveNRedemptions(3, maxFeeRateV0, 0)
	if err != nil {
		t.Fatalf("reservation error: %v", err)
	}

	lockPerV0 := gasesV0.Redeem * maxFeeRateV0
	expLock := 3 * lockPerV0
	if eth.lockedFunds.redemptionReserves != expLock {
		t.Fatalf("wrong v0 locked. wanted %d, got %d", expLock, eth.lockedFunds.redemptionReserves)
	}

	if v0Val != expLock {
		t.Fatalf("expected value %d, got %d", lockPerV0, v0Val)
	}

	v1Val, err := eth.ReserveNRedemptions(2, maxFeeRateV1, 1)
	if err != nil {
		t.Fatalf("reservation error: %v", err)
	}

	lockPerV1 := gasesV1.Redeem * maxFeeRateV1
	v1Lock := 2 * lockPerV1
	if v1Val != v1Lock {
		t.Fatalf("")
	}

	expLock += v1Lock
	if eth.lockedFunds.redemptionReserves != expLock {
		t.Fatalf("wrong v1 locked. wanted %d, got %d", expLock, eth.lockedFunds.redemptionReserves)
	}

	// Redeem two v0.
	node.contractor.redeemTx = types.NewTx(&types.DynamicFeeTx{})
	if _, _, _, err = eth.Redeem(&asset.RedeemForm{
		Redemptions: []*asset.Redemption{{
			Spends: &asset.AuditInfo{
				Coin:     spentCoin,
				Contract: dexeth.EncodeContractData(0, secretHash),
			},
			UnlockedReserves: lockPerV0,
		}, {
			Spends: &asset.AuditInfo{
				Coin:     spentCoin,
				Contract: dexeth.EncodeContractData(0, secretHash),
			},
			UnlockedReserves: lockPerV0,
		}},
	}); err != nil {
		t.Fatalf("Redeem error: %v", err)
	}

	expLock -= lockPerV0 * 2
	if eth.lockedFunds.redemptionReserves != expLock {
		t.Fatalf("wrong unreserved. wanted %d, got %d", expLock, eth.lockedFunds.redemptionReserves)
	}
}

func ethToGwei(v uint64) uint64 {
	return v * dexeth.GweiFactor
}

func ethToWei(v uint64) *big.Int {
	bigV := new(big.Int).SetUint64(ethToGwei(v))
	return new(big.Int).Mul(bigV, big.NewInt(dexeth.GweiFactor))
}

func TestPayFee(t *testing.T) {
	tx := tTx(0, 0, 0, &testAddressA, nil)
	txHash := tx.Hash()
	maxFee := uint64(defaultSendGasLimit * defaultGasFeeLimit)
	tests := []struct {
		name              string
		regFee            uint64
		bal               uint64
		balErr, sendTxErr error
		wantErr           bool
	}{{
		name:   "ok",
		regFee: 10e9,
		bal:    10e9 + maxFee,
	}, {
		name:    "balance error",
		balErr:  errors.New(""),
		wantErr: true,
	}, {
		name:    "not enough",
		regFee:  10e9,
		bal:     10e9,
		wantErr: true,
	}, {
		name:      "sendToAddr error",
		regFee:    5e9,
		bal:       10e9,
		sendTxErr: errors.New(""),
		wantErr:   true,
	}}

	for _, test := range tests {
		node := newTestNode()

		node.bal = dexeth.GweiToWei(test.bal)
		node.balErr = test.balErr
		node.sendTxTx = tx
		node.sendTxErr = test.sendTxErr
		eth := &ExchangeWallet{
			node:        node,
			gasFeeLimit: defaultGasFeeLimit,
		}
		coin, err := eth.PayFee("", test.regFee, 0)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if !bytes.Equal(txHash[:], coin.ID()) {
			t.Fatal("coin is not the tx hash")
		}
	}
}

func TestWithdraw(t *testing.T) {
	tx := tTx(0, 0, 0, &testAddressA, nil)
	txHash := tx.Hash()
	maxFee := uint64(defaultSendGasLimit * defaultGasFeeLimit)
	tests := []struct {
		name              string
		value             uint64
		bal               uint64
		balErr, sendTxErr error
		wantErr           bool
	}{{
		name:  "ok",
		value: 10e9,
		bal:   10e9,
	}, {
		name:    "balance error",
		balErr:  errors.New(""),
		wantErr: true,
	}, {
		name:    "not enough",
		value:   10e9 + 1,
		bal:     10e9,
		wantErr: true,
	}, {
		name:    "cannot cover fee",
		bal:     maxFee - 1,
		wantErr: true,
	}, {
		name:      "sendToAddr error",
		value:     5e9,
		bal:       10e9,
		sendTxErr: errors.New(""),
		wantErr:   true,
	}}

	for _, test := range tests {
		node := newTestNode()
		node.bal = dexeth.GweiToWei(test.bal)
		node.balErr = test.balErr
		node.sendTxTx = tx
		node.sendTxErr = test.sendTxErr
		eth := &ExchangeWallet{
			node:        node,
			gasFeeLimit: defaultGasFeeLimit,
		}
		coin, err := eth.Withdraw("", test.value, 0)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if !bytes.Equal(txHash[:], coin.ID()) {
			t.Fatal("coin is not the tx hash")
		}
	}
}

func parseRecoveryID(c asset.Coin) []byte {
	return c.(*fundingCoin).RecoveryID()
}
