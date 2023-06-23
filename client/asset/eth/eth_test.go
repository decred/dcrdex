//go:build !harness && !rpclive

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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/kvdb"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
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

	ethGases   = dexeth.VersionedGases[0]
	tokenGases = dexeth.Tokens[simnetTokenID].NetTokens[dex.Simnet].SwapContracts[0].Gas

	tETH = &dex.Asset{
		// Version meaning?
		ID:           60,
		Symbol:       "ETH",
		MaxFeeRate:   100,
		SwapSize:     ethGases.Swap,
		SwapSizeBase: ethGases.Swap,
		RedeemSize:   ethGases.Redeem,
		SwapConf:     1,
	}

	tBTC = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		Version:      0, // match btc.version
		SwapSize:     300,
		SwapSizeBase: 300,
		MaxFeeRate:   10,
		SwapConf:     1,
	}

	tToken = &dex.Asset{
		ID:           simnetTokenID,
		Symbol:       "dextt.eth",
		Version:      0,
		SwapSize:     tokenGases.Swap,
		SwapSizeBase: tokenGases.Swap,
		RedeemSize:   tokenGases.Redeem,
		MaxFeeRate:   20,
		SwapConf:     1,
	}

	// simBackend = backends.NewSimulatedBackend(core.GenesisAlloc{
	// 	testAddressA: core.GenesisAccount{Balance: dexeth.GweiToWei(5e10)},
	// }, 1e9)
)

type tGetTxRes struct {
	tx     *types.Transaction
	height int64
}

type testNode struct {
	acct            *accounts.Account
	addr            common.Address
	connectErr      error
	bestHdr         *types.Header
	bestHdrErr      error
	syncProg        ethereum.SyncProgress
	syncProgT       uint64
	syncProgErr     error
	bal             *big.Int
	balErr          error
	signDataErr     error
	privKey         *ecdsa.PrivateKey
	swapVers        map[uint32]struct{} // For SwapConfirmations -> swap. TODO for other contractor methods
	swapMap         map[[32]byte]*dexeth.SwapState
	refundable      bool
	baseFee         *big.Int
	tip             *big.Int
	netFeeStateErr  error
	confNonce       uint64
	confNonceErr    error
	getTxRes        *types.Transaction
	getTxResMap     map[common.Hash]*tGetTxRes
	getTxHeight     int64
	getTxErr        error
	receipt         *types.Receipt
	receiptTx       *types.Transaction
	receiptErr      error
	hdrByHash       *types.Header
	txReceipt       *types.Receipt
	lastSignedTx    *types.Transaction
	sendTxTx        *types.Transaction
	sendTxErr       error
	simBackend      bind.ContractBackend
	maxFeeRate      *big.Int
	tContractor     *tContractor
	tokenContractor *tTokenContractor
	contractor      contractor
	tokenParent     *assetWallet // only set for tokens
	txConfirmations map[common.Hash]uint32
	txConfsErr      map[common.Hash]error
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

func (n *testNode) getConfirmedNonce(context.Context) (uint64, error) {
	return n.confNonce, n.confNonceErr
}

func (n *testNode) getTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, int64, error) {
	if n.getTxErr != nil {
		return nil, 0, n.getTxErr
	}

	if n.getTxResMap != nil {
		if tx, ok := n.getTxResMap[hash]; ok {
			return tx.tx, tx.height, nil
		} else {
			return nil, 0, asset.CoinNotFoundError
		}
	}

	return n.getTxRes, n.getTxHeight, n.getTxErr
}

func (n *testNode) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, nonce *big.Int) (*bind.TransactOpts, error) {
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
func (n *testNode) syncProgress(context.Context) (prog *ethereum.SyncProgress, bestBlockUNIXTime uint64, err error) {
	return &n.syncProg, n.syncProgT, n.syncProgErr
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
	n.lastSignedTx = tx
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

func (n *testNode) transactionConfirmations(_ context.Context, txHash common.Hash) (uint32, error) {
	if n.txConfirmations == nil {
		return 0, nil
	}
	return n.txConfirmations[txHash], n.txConfsErr[txHash]
}

func (n *testNode) headerByHash(_ context.Context, txHash common.Hash) (*types.Header, error) {
	return n.hdrByHash, nil
}

func (n *testNode) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error) {
	return n.receipt, n.receiptTx, n.receiptErr
}

type tMempoolNode struct {
	*testNode
	pendingTxs []*types.Transaction
}

func (n *tMempoolNode) pendingTransactions() ([]*types.Transaction, error) {
	return n.pendingTxs, nil
}

type tContractor struct {
	gasEstimates      *dexeth.Gases
	swapMap           map[[32]byte]*dexeth.SwapState
	swapErr           error
	initTx            *types.Transaction
	initErr           error
	redeemTx          *types.Transaction
	redeemErr         error
	refundTx          *types.Transaction
	refundErr         error
	initGasErr        error
	redeemGasErr      error
	refundGasErr      error
	redeemGasOverride *uint64
	valueIn           map[common.Hash]uint64
	valueOut          map[common.Hash]uint64
	valueErr          error
	refundable        bool
	refundableErr     error
	lastRedeemOpts    *bind.TransactOpts
	lastRedeems       []*asset.Redemption
	lastRefund        struct {
		// tx          *types.Transaction
		secretHash [32]byte
		maxFeeRate *big.Int
		// contractVer uint32
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
	c.lastRedeemOpts = txOpts
	c.lastRedeems = redeems
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
	if c.redeemGasOverride != nil {
		return *c.redeemGasOverride, nil
	}
	return c.gasEstimates.RedeemN(len(secrets)), c.redeemGasErr
}

func (c *tContractor) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	return c.gasEstimates.Refund, c.refundGasErr
}

func (c *tContractor) value(_ context.Context, tx *types.Transaction) (incoming, outgoing uint64, err error) {
	return c.valueIn[tx.Hash()], c.valueOut[tx.Hash()], c.valueErr
}

func (c *tContractor) isRefundable(secretHash [32]byte) (bool, error) {
	return c.refundable, c.refundableErr
}

func (c *tContractor) voidUnusedNonce() {}

type tTokenContractor struct {
	*tContractor
	tokenAddr           common.Address
	bal                 *big.Int
	balErr              error
	allow               *big.Int
	allowErr            error
	approveTx           *types.Transaction
	approved            bool
	approveErr          error
	approveEstimate     uint64
	approveEstimateErr  error
	transferTx          *types.Transaction
	transferErr         error
	transferEstimate    uint64
	transferEstimateErr error
}

var _ tokenContractor = (*tTokenContractor)(nil)

func (c *tTokenContractor) tokenAddress() common.Address {
	return c.tokenAddr
}

func (c *tTokenContractor) balance(context.Context) (*big.Int, error) {
	return c.bal, c.balErr
}

func (c *tTokenContractor) allowance(context.Context) (*big.Int, error) {
	return c.allow, c.allowErr
}

func (c *tTokenContractor) approve(*bind.TransactOpts, *big.Int) (*types.Transaction, error) {
	if c.approveErr == nil {
		c.approved = true
	}
	return c.approveTx, c.approveErr
}

func (c *tTokenContractor) estimateApproveGas(context.Context, *big.Int) (uint64, error) {
	return c.approveEstimate, c.approveEstimateErr
}

func (c *tTokenContractor) transfer(*bind.TransactOpts, common.Address, *big.Int) (*types.Transaction, error) {
	return c.transferTx, c.transferErr
}

func (c *tTokenContractor) estimateTransferGas(context.Context, *big.Int) (uint64, error) {
	return c.transferEstimate, c.transferEstimateErr
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
		w := &ETHWallet{
			assetWallet: &assetWallet{
				baseWallet: &baseWallet{
					node:       node,
					addr:       node.address(),
					ctx:        ctx,
					log:        tLogger,
					currentTip: header0,
				},
				log:       tLogger.SubLogger("ETH"),
				tipChange: tipChange,
				assetID:   BipID,
			},
		}
		w.wallets = map[uint32]*assetWallet{BipID: w.assetWallet}
		w.assetWallet.connected.Store(true)
		w.checkForNewBlocks(ctx, tipChange)

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
	tests := []struct {
		name                string
		syncProg            ethereum.SyncProgress
		syncProgErr         error
		subSecs             uint64
		wantErr, wantSynced bool
		wantRatio           float32
	}{{
		name: "ok synced",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 25,
		},
		wantRatio:  1,
		wantSynced: true,
	}, {
		name: "ok syncing",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 100,
		},
		wantRatio: 0.25,
	}, {
		name: "ok header too old",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 0,
		},
		subSecs: dexeth.MaxBlockInterval + 1,
	}, {
		name: "sync progress error",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 0,
		},
		syncProgErr: errors.New(""),
		wantErr:     true,
	}}

	for _, test := range tests {
		nowInSecs := uint64(time.Now().Unix())
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{
			syncProg:    test.syncProg,
			syncProgT:   nowInSecs - test.subSecs,
			syncProgErr: test.syncProgErr,
		}
		eth := &baseWallet{
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

func newTestNode(assetID uint32) *tMempoolNode {
	privKey, _ := crypto.HexToECDSA("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")
	addr := common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
	acct := &accounts.Account{
		Address: addr,
	}

	tc := &tContractor{
		gasEstimates: ethGases,
		swapMap:      make(map[[32]byte]*dexeth.SwapState),
		valueIn:      make(map[common.Hash]uint64),
		valueOut:     make(map[common.Hash]uint64),
	}

	var c contractor = tc

	ttc := &tTokenContractor{
		tContractor: tc,
		allow:       new(big.Int),
	}
	if assetID != BipID {
		ttc.tContractor.gasEstimates = &tokenGases
		c = ttc
	}

	return &tMempoolNode{
		testNode: &testNode{
			acct:            acct,
			addr:            acct.Address,
			maxFeeRate:      dexeth.GweiToWei(100),
			baseFee:         dexeth.GweiToWei(100),
			tip:             dexeth.GweiToWei(2),
			privKey:         privKey,
			contractor:      c,
			tContractor:     tc,
			tokenContractor: ttc,
		},
	}
}

func tassetWallet(assetID uint32) (asset.Wallet, *assetWallet, *tMempoolNode, context.CancelFunc) {
	node := newTestNode(assetID)
	ctx, cancel := context.WithCancel(context.Background())
	var c contractor = node.tContractor
	if assetID != BipID {
		c = node.tokenContractor
	}

	versionedGases := make(map[uint32]*dexeth.Gases)
	if assetID == BipID { // just make a copy
		for ver, g := range dexeth.VersionedGases {
			versionedGases[ver] = g
		}
	} else {
		netToken := dexeth.Tokens[assetID].NetTokens[dex.Simnet]
		for ver, c := range netToken.SwapContracts {
			versionedGases[ver] = &c.Gas
		}
	}

	aw := &assetWallet{
		baseWallet: &baseWallet{
			baseChainID:   BipID,
			chainID:       dexeth.ChainIDs[dex.Simnet],
			tokens:        dexeth.Tokens,
			addr:          node.addr,
			net:           dex.Simnet,
			node:          node,
			ctx:           ctx,
			log:           tLogger,
			gasFeeLimitV:  defaultGasFeeLimit,
			monitoredTxs:  make(map[common.Hash]*monitoredTx),
			monitoredTxDB: kvdb.NewMemoryDB(),
			pendingTxs:    make(map[common.Hash]*pendingTx),
		},
		versionedGases:     versionedGases,
		log:                tLogger.SubLogger(strings.ToUpper(dex.BipIDSymbol(assetID))),
		assetID:            assetID,
		contractors:        map[uint32]contractor{0: c},
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
		maxSwapsInTx:       40,
		maxRedeemsInTx:     60,
		pendingTxCheckBal:  new(big.Int),
		pendingApprovals:   make(map[uint32]*pendingApproval),
		approvalCache:      make(map[uint32]bool),
	}
	aw.wallets = map[uint32]*assetWallet{
		BipID: aw,
	}

	var w asset.Wallet
	if assetID == BipID {
		w = &ETHWallet{assetWallet: aw}
		aw.wallets = map[uint32]*assetWallet{
			BipID: aw,
		}
	} else {
		node.tokenParent = &assetWallet{
			baseWallet:       aw.baseWallet,
			log:              tLogger.SubLogger("ETH"),
			contractors:      map[uint32]contractor{0: node.tContractor},
			assetID:          BipID,
			atomize:          dexeth.WeiToGwei,
			pendingApprovals: make(map[uint32]*pendingApproval),
			approvalCache:    make(map[uint32]bool),
		}
		w = &TokenWallet{
			assetWallet: aw,
			cfg:         &tokenWalletConfig{},
			parent:      node.tokenParent,
			token:       dexeth.Tokens[simnetTokenID],
			netToken:    dexeth.Tokens[simnetTokenID].NetTokens[dex.Simnet],
		}
		aw.wallets = map[uint32]*assetWallet{
			simnetTokenID: aw,
			BipID:         node.tokenParent,
		}
	}

	return w, aw, node, cancel
}

func TestBalanceWithMempool(t *testing.T) {
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
		token        bool
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
		name:    "ok one token",
		bal:     newBalance(1, 0, 0),
		wantBal: 1,
		token:   true,
	}, {
		name:       "ok pending out",
		bal:        newBalance(4e8, 0, 1.4e8),
		wantBal:    2.6e8,
		wantLocked: 0,
	}, {
		name:       "ok pending out token",
		bal:        newBalance(4e8, 0, 1.4e8),
		wantBal:    2.6e8,
		wantLocked: 0,
		token:      true,
	}, {
		name:         "ok pending in",
		bal:          newBalance(1e8, 3e8, 0),
		wantBal:      1e8,
		wantImmature: 3e8,
	}, {
		name:         "ok pending in token",
		bal:          newBalance(1e8, 3e8, 0),
		wantBal:      1e8,
		wantImmature: 3e8,
		token:        true,
	}, {
		name:         "ok pending out and in",
		bal:          newBalance(4e8, 2e8, 1e8),
		wantBal:      3e8,
		wantLocked:   0,
		wantImmature: 2e8,
	}, {
		name:         "ok pending out and in token",
		bal:          newBalance(4e8, 2e8, 1e8),
		wantBal:      3e8,
		wantLocked:   0,
		wantImmature: 2e8,
		token:        true,
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
		var assetID uint32 = BipID
		if test.token {
			assetID = simnetTokenID
		}

		_, eth, node, shutdown := tassetWallet(assetID)
		defer shutdown()

		if test.token {
			node.tokenContractor.bal = test.bal.Current
			node.tokenContractor.balErr = test.balErr
		} else {
			node.bal = test.bal.Current
			node.balErr = test.balErr
		}

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
			node.tContractor.valueIn[tx.Hash()] = dexeth.WeiToGwei(test.bal.PendingIn)
		}
		if test.bal.PendingOut.Cmp(new(big.Int)) > 0 {
			if test.token {
				tx := newTx(nil)
				node.pendingTxs = append(node.pendingTxs, tx)
				node.tContractor.valueOut[tx.Hash()] = dexeth.WeiToGwei(test.bal.PendingOut)
			} else {
				node.pendingTxs = append(node.pendingTxs, newTx(test.bal.PendingOut))
			}
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

func TestBalanceNoMempool(t *testing.T) {

	const tipHeight = 50
	const lastCheck = tipHeight - 1

	type tPendingTx struct {
		*pendingTx
		confs uint32
	}

	newPendingTx := func(assetID uint32, out, in, maxFees uint64, confs uint32) *tPendingTx {
		return &tPendingTx{
			pendingTx: &pendingTx{
				assetID:   assetID,
				out:       out,
				in:        in,
				maxFees:   maxFees,
				stamp:     time.Now(),
				lastCheck: lastCheck,
			},
			confs: confs,
		}
	}

	tests := []struct {
		name          string
		assetID       uint32
		pendingTxs    []*tPendingTx
		expPendingIn  uint64
		expPendingOut uint64
		expCountAfter int
	}{
		{
			name:    "single eth tx",
			assetID: BipID,
			pendingTxs: []*tPendingTx{
				newPendingTx(BipID, 1, 0, 2, 0),
			},
			expPendingOut: 3,
			expCountAfter: 1,
		},
		{
			name:    "single tx expired",
			assetID: BipID,
			pendingTxs: []*tPendingTx{
				newPendingTx(BipID, 1, 0, 1, 1),
			},
		},
		{
			name:    "eth with token fees",
			assetID: BipID,
			pendingTxs: []*tPendingTx{
				newPendingTx(simnetTokenID, 4, 0, 5, 0),
			},
			expPendingOut: 5,
			expCountAfter: 1,
		},
		{
			name:    "token with 1 tx and other ignored assets",
			assetID: simnetTokenID,
			pendingTxs: []*tPendingTx{
				newPendingTx(simnetTokenID, 4, 0, 5, 0),
				newPendingTx(simnetTokenID+1, 8, 0, 9, 0),
			},
			expPendingOut: 4,
			expCountAfter: 2,
		},
		{
			name:    "token with 1 tx incoming",
			assetID: simnetTokenID,
			pendingTxs: []*tPendingTx{
				newPendingTx(simnetTokenID, 0, 15, 5, 0),
			},
			expPendingIn:  15,
			expCountAfter: 1,
		},
		{
			name:    "eth mixed txs",
			assetID: BipID,
			pendingTxs: []*tPendingTx{
				newPendingTx(BipID, 1, 0, 2, 0),         // 3 eth out
				newPendingTx(simnetTokenID, 3, 0, 4, 1), // confirmed
				newPendingTx(simnetTokenID, 5, 0, 6, 0), // 6 eth out
				newPendingTx(BipID, 0, 7, 1, 0),         // 1 eth out, 7 eth in
			},
			expPendingOut: 10,
			expPendingIn:  7,
			expCountAfter: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, eth, tNode, shutdown := tassetWallet(tt.assetID)
			defer shutdown()
			eth.node = tNode.testNode // no mempool
			tNode.txConfirmations = make(map[common.Hash]uint32)
			tNode.txConfsErr = make(map[common.Hash]error)
			tNode.bal = unlimitedAllowance
			tNode.tokenContractor.bal = unlimitedAllowance

			eth.tipMtx.Lock()
			eth.currentTip = &types.Header{Number: new(big.Int).SetUint64(tipHeight)}
			eth.tipMtx.Unlock()

			for _, pt := range tt.pendingTxs {
				txHash := common.BytesToHash(encode.RandomBytes(32))
				eth.pendingTxs[txHash] = pt.pendingTx
				if pt.confs == 0 {
					tNode.txConfsErr[txHash] = asset.CoinNotFoundError
				} else {
					tNode.txConfirmations[txHash] = pt.confs
				}
			}

			bal, err := eth.balanceWithTxPool()
			if err != nil {
				t.Fatalf("balanceWithTxPool error: %v", err)
			}

			if in := dexeth.WeiToGwei(bal.PendingIn); in != tt.expPendingIn {
				t.Fatalf("wrong PendingIn. wanted %d, got %d", tt.expPendingIn, in)
			}

			if out := dexeth.WeiToGwei(bal.PendingOut); out != tt.expPendingOut {
				t.Fatalf("wrong PendingOut. wanted %d, got %d", tt.expPendingOut, out)
			}

			if len(eth.pendingTxs) != tt.expCountAfter {
				t.Fatalf("wrong pending tx count after balance check. expected %d, got %d", tt.expCountAfter, len(eth.pendingTxs))
			}
		})
	}
}

func TestFeeRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	eth := &baseWallet{
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
	t.Run("eth", func(t *testing.T) { testRefund(t, BipID) })
	t.Run("token", func(t *testing.T) { testRefund(t, simnetTokenID) })
}

func testRefund(t *testing.T, assetID uint32) {
	_, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	const feeSuggestion = 100
	const gweiBal = 1e9
	const ogRefundReserves = 1e8

	v1Contractor := &tContractor{
		swapMap:      make(map[[32]byte]*dexeth.SwapState, 1),
		gasEstimates: ethGases,
		redeemTx:     types.NewTx(&types.DynamicFeeTx{}),
	}
	var v1c contractor = v1Contractor

	gasesV1 := &dexeth.Gases{Refund: 1e5}
	if assetID == BipID {
		eth.versionedGases[1] = gasesV1
	} else {
		eth.versionedGases[1] = &dexeth.Tokens[simnetTokenID].NetTokens[dex.Simnet].SwapContracts[0].Gas
		v1c = &tTokenContractor{tContractor: v1Contractor}
	}

	eth.contractors[1] = v1c

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	v0Contract := dexeth.EncodeContractData(0, secretHash)
	ss := &dexeth.SwapState{Value: dexeth.GweiToWei(1)}
	v0Contractor := node.tContractor

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
		c := v0Contractor
		if test.useV1Gases {
			contract = dexeth.EncodeContractData(1, secretHash)
			c = v1Contractor
		} else if test.badContract {
			contract = []byte{}
		}

		c.refundable = test.isRefundable
		c.refundableErr = test.isRefundableErr
		c.refundErr = test.refundErr
		node.bal = dexeth.GweiToWei(gweiBal)
		c.swapErr = test.swapErr
		ss.State = test.swapStep
		eth.lockedFunds.refundReserves = ogRefundReserves

		var txHash common.Hash
		if test.isRefundable {
			tx := types.NewTx(&types.DynamicFeeTx{})
			txHash = tx.Hash()
			c.refundTx = tx
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

			if secretHash != c.lastRefund.secretHash {
				t.Fatalf(`%v: secret hash in contract %x != used to call refund %x`,
					test.name, secretHash, c.lastRefund.secretHash)
			}

			if dexeth.GweiToWei(feeSuggestion).Cmp(c.lastRefund.maxFeeRate) != 0 {
				t.Fatalf(`%v: fee suggestion %v != used to call refund %v`,
					test.name, dexeth.GweiToWei(feeSuggestion), c.lastRefund.maxFeeRate)
			}
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
	t.Run("eth", func(t *testing.T) { testFundOrderReturnCoinsFundingCoins(t, BipID) })
	t.Run("token", func(t *testing.T) { testFundOrderReturnCoinsFundingCoins(t, simnetTokenID) })
}

func testFundOrderReturnCoinsFundingCoins(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()
	walletBalanceGwei := uint64(dexeth.GweiFactor)
	fromAsset := tETH
	if assetID == BipID {
		node.bal = dexeth.GweiToWei(walletBalanceGwei)
	} else {
		fromAsset = tToken
		node.tokenContractor.bal = dexeth.GweiToWei(walletBalanceGwei)
		node.tokenContractor.allow = unlimitedAllowance
		node.tokenParent.node.(*tMempoolNode).bal = dexeth.GweiToWei(walletBalanceGwei)
	}

	checkBalance := func(wallet *assetWallet, expectedAvailable, expectedLocked uint64, testName string) {
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

		if assetID == BipID {
			_, err = decodeFundingCoin(rc.RecoveryID())
		} else {
			_, err = decodeTokenFundingCoin(rc.RecoveryID())
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.testName, err)
		}
		if coins[0].Value() != test.coinValue {
			t.Fatalf("%s: wrong value. expected %v but got %v", test.testName, test.coinValue, coins[0].Value())
		}
	}

	order := asset.Order{
		Version:       fromAsset.Version,
		Value:         walletBalanceGwei / 2,
		MaxSwapCount:  2,
		MaxFeeRate:    fromAsset.MaxFeeRate,
		RedeemVersion: tBTC.Version, // not important if not a token
		RedeemAssetID: tBTC.ID,
	}

	// Test fund order with less than available funds
	coins1, redeemScripts1, _, err := w.FundOrder(&order)
	// NOTE: the following should NOT use dex.Asset.SwapSize, instead w.gases(ver) and the swap count and fee rate
	expectedOrderFees := fromAsset.SwapSize * order.MaxFeeRate * order.MaxSwapCount
	expectedFees := expectedOrderFees
	expectedCoinValue := order.Value
	if assetID == BipID {
		expectedCoinValue += expectedOrderFees
	}

	checkFundOrderResult(coins1, redeemScripts1, err, fundOrderTest{
		testName:    "more than enough",
		coinValue:   expectedCoinValue,
		coinAddress: node.addr.String(),
	})
	checkBalance(eth, walletBalanceGwei-expectedCoinValue, expectedCoinValue, "more than enough")

	// Test fund order with 1 more than available funds
	order.Value = walletBalanceGwei - expectedCoinValue + 1
	if assetID == BipID {
		order.Value -= expectedOrderFees
	}
	coins, redeemScripts, _, err := w.FundOrder(&order)
	checkFundOrderResult(coins, redeemScripts, err, fundOrderTest{
		testName: "not enough",
		wantErr:  true,
	})
	checkBalance(eth, walletBalanceGwei-expectedCoinValue, expectedCoinValue, "not enough")

	// Test fund order with funds equal to available
	order.Value = order.Value - 1
	expVal := order.Value
	if assetID == BipID {
		expVal += expectedFees
	}
	coins2, redeemScripts2, _, err := w.FundOrder(&order)
	checkFundOrderResult(coins2, redeemScripts2, err, fundOrderTest{
		testName:    "just enough",
		coinValue:   expVal,
		coinAddress: node.addr.String(),
	})
	checkBalance(eth, 0, walletBalanceGwei, "just enough")

	// Test returning funds > locked returns locked funds to 0
	err = w.ReturnCoins([]asset.Coin{coins1[0], coins1[0]})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, walletBalanceGwei, 0, "after return too much")

	// Fund order with funds equal to available
	order.Value = walletBalanceGwei
	if assetID == BipID {
		order.Value -= expectedOrderFees
	}
	_, _, _, err = w.FundOrder(&order)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, 0, walletBalanceGwei, "just enough 2")

	// Test returning coin with invalid ID
	var badCoin badCoin
	err = w.ReturnCoins([]asset.Coin{&badCoin})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}

	// Test returning correct coins returns all funds
	err = w.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth, walletBalanceGwei, 0, "returned correct amount")

	node.balErr = errors.New("")
	_, _, _, err = w.FundOrder(&order)
	if err == nil {
		t.Fatalf("balance error should cause error but did not")
	}
	node.balErr = nil

	// Test that funding without allowance causes error
	if assetID != BipID {
		eth.approvalCache = make(map[uint32]bool)
		node.tokenContractor.allow = big.NewInt(0)
		_, _, _, err = w.FundOrder(&order)
		if err == nil {
			t.Fatalf("no allowance should cause error but did not")
		}
		node.tokenContractor.allow = unlimitedAllowance
	}

	// Test eth wallet gas fee limit > server MaxFeeRate causes error
	tmpGasFeeLimit := eth.gasFeeLimit()
	eth.gasFeeLimitV = order.MaxFeeRate - 1
	_, _, _, err = w.FundOrder(&order)
	if err == nil {
		t.Fatalf("eth wallet gas fee limit > server MaxFeeRate should cause error")
	}
	eth.gasFeeLimitV = tmpGasFeeLimit

	w2, eth2, _, shutdown2 := tassetWallet(assetID)
	defer shutdown2()
	eth2.node = node
	eth2.contractors[0] = node.tokenContractor
	node.tokenContractor.bal = dexeth.GweiToWei(walletBalanceGwei)

	// Test reloading coins from first order
	coinVal := coins1[0].Value()
	coins, err = w2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0])})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(coins) != 1 {
		t.Fatalf("expected 1 coins but got %v", len(coins))
	}
	if coins[0].Value() != coinVal {
		t.Fatalf("funding coin value %v != expected %v", coins[0].Value(), coins[1].Value())
	}
	checkBalance(eth2, walletBalanceGwei-coinVal, coinVal, "funding1")

	// Test reloading more coins than are available in balance
	rid := parseRecoveryID(coins1[0])
	if assetID != BipID {
		rid = createTokenFundingCoin(node.addr, coinVal+1, 1).RecoveryID()
	}
	_, err = w2.FundingCoins([]dex.Bytes{rid})
	if err == nil {
		t.Fatalf("expected error but didn't get one")
	}
	checkBalance(eth2, walletBalanceGwei-coinVal, coinVal, "after funding error 1")

	// Test funding coins with bad coin ID

	_, err = w2.FundingCoins([]dex.Bytes{append(parseRecoveryID(coins1[0]), 0x0a)})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coinVal, coinVal, "after funding error 3")

	// Test funding coins with coin from different address
	var differentAddress [20]byte
	decodedHex, _ := hex.DecodeString("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	copy(differentAddress[:], decodedHex)
	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))

	differentKindaCoin := (&coin{
		id: randomHash(), // e.g. tx hash
	})
	_, err = w2.FundingCoins([]dex.Bytes{differentKindaCoin.ID()})
	if err == nil {
		t.Fatalf("expected error for unknown coin id format, but did not get")
	}

	differentAddressCoin := createFundingCoin(differentAddress, 100000)
	_, err = w2.FundingCoins([]dex.Bytes{differentAddressCoin.ID()})
	if err == nil {
		t.Fatalf("expected error for wrong address, but did not get")
	}
	checkBalance(eth2, walletBalanceGwei-coinVal, coinVal, "after funding error 4")

	// Test funding coins with balance error
	node.balErr = errors.New("")
	_, err = w2.FundingCoins([]dex.Bytes{badCoin.ID()})
	if err == nil {
		t.Fatalf("expected error but did not get")
	}
	node.balErr = nil
	checkBalance(eth2, walletBalanceGwei-coinVal, coinVal, "after funding error 5")

	// Reloading coins from second order
	coins, err = w2.FundingCoins([]dex.Bytes{parseRecoveryID(coins2[0])})
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
	err = w2.ReturnCoins([]asset.Coin{differentKindaCoin})
	if err == nil {
		t.Fatalf("expected error for unknown coin ID format, but did not get")
	}

	err = w2.ReturnCoins([]asset.Coin{differentAddressCoin})
	if err == nil {
		t.Fatalf("expected error for wrong address, but did not get")
	}

	// return all coins
	err = w2.ReturnCoins([]asset.Coin{coins1[0], coins2[0]})
	if err != nil {
		t.Fatalf("unexpected error")
	}
	checkBalance(eth2, walletBalanceGwei, 0, "return coins after funding")

	// Test funding coins with two coins at the same time
	_, err = w2.FundingCoins([]dex.Bytes{parseRecoveryID(coins1[0]), parseRecoveryID(coins2[0])})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checkBalance(eth2, 0, walletBalanceGwei, "funding3")
}

func TestFundMultiOrder(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testFundMultiOrder(t, BipID) })
	t.Run("token", func(t *testing.T) { testFundMultiOrder(t, simnetTokenID) })
}

func testFundMultiOrder(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)

	defer shutdown()

	fromAsset := tETH
	swapGas := dexeth.VersionedGases[fromAsset.Version].Swap
	if assetID != BipID {
		fromAsset = tToken
		node.tokenContractor.allow = unlimitedAllowance
		swapGas = dexeth.Tokens[simnetTokenID].NetTokens[dex.Simnet].
			SwapContracts[fromAsset.Version].Gas.Swap
	}

	type test struct {
		name       string
		multiOrder *asset.MultiOrder
		maxLock    uint64
		bal        uint64
		tokenBal   uint64
		parentBal  uint64

		ethOnly   bool
		tokenOnly bool

		expectErr bool
	}

	tests := []test{
		{
			name:      "ok",
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
		},
		{
			name:      "maxLock just enough, eth",
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			maxLock: uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
		},
		{
			name:      "maxLock not enough, eth",
			ethOnly:   true,
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			maxLock:   uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate - 1,
			expectErr: true,
		},
		{
			name:      "maxLock just enough, token",
			tokenOnly: true,
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			maxLock: uint64(dexeth.GweiFactor),
		},
		{
			name:      "maxLock not enough, eth",
			tokenOnly: true,
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			maxLock: uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate,
		},

		{
			name:      "insufficient balance",
			bal:       uint64(dexeth.GweiFactor) + swapGas*4*fromAsset.MaxFeeRate - 1,
			tokenBal:  uint64(dexeth.GweiFactor) - 1,
			parentBal: uint64(dexeth.GweiFactor),
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			expectErr: true,
		},
		{
			name:      "parent balance ok",
			tokenOnly: true,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: swapGas * 4 * fromAsset.MaxFeeRate,
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
		},
		{
			name:      "insufficient parent balance",
			tokenOnly: true,
			tokenBal:  uint64(dexeth.GweiFactor),
			parentBal: swapGas*4*fromAsset.MaxFeeRate - 1,
			multiOrder: &asset.MultiOrder{
				Version:    fromAsset.Version,
				MaxFeeRate: fromAsset.MaxFeeRate,
				Values: []*asset.MultiOrderValue{
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
					{
						Value:        uint64(dexeth.GweiFactor) / 2,
						MaxSwapCount: 2,
					},
				},
			},
			expectErr: true,
		},
	}

	for _, test := range tests {
		if assetID == BipID {
			if test.tokenOnly {
				continue
			}
			node.bal = dexeth.GweiToWei(test.bal)
		} else {
			if test.ethOnly {
				continue
			}
			node.tokenContractor.bal = dexeth.GweiToWei(test.tokenBal)
			node.tokenParent.node.(*tMempoolNode).bal = dexeth.GweiToWei(test.parentBal)
		}
		eth.lockedFunds.initiateReserves = 0
		eth.baseWallet.wallets[BipID].lockedFunds.initiateReserves = 0

		allCoins, redeemScripts, _, err := w.FundMultiOrder(test.multiOrder, test.maxLock)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get one", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if len(allCoins) != len(test.multiOrder.Values) {
			t.Fatalf("%s: expected %d coins but got %d", test.name, len(test.multiOrder.Values), len(allCoins))
		}
		if len(redeemScripts) != len(test.multiOrder.Values) {
			t.Fatalf("%s: expected %d redeem scripts but got %d", test.name, len(test.multiOrder.Values), len(redeemScripts))
		}
		for i, coins := range allCoins {
			if len(coins) != 1 {
				t.Fatalf("%s: expected 1 coin but got %d", test.name, len(coins))
			}
			expectedValue := test.multiOrder.Values[i].Value
			if assetID == BipID {
				expectedValue += swapGas * test.multiOrder.Values[i].MaxSwapCount * fromAsset.MaxFeeRate
			}
			if coins[0].Value() != expectedValue {
				t.Fatalf("%s: expected coin %d value %d but got %d", test.name, i, expectedValue, coins[0].Value())
			}
		}
	}
}

func TestPreSwap(t *testing.T) {
	const baseFee, tip = 42, 2
	const feeSuggestion = 90 // ignored by eth's PreSwap
	const lotSize = 10e9
	oneFee := ethGases.Swap * tETH.MaxFeeRate
	refund := ethGases.Refund * tETH.MaxFeeRate
	oneLock := lotSize + oneFee + refund

	oneFeeToken := tokenGases.Swap*tToken.MaxFeeRate + tokenGases.Refund*tToken.MaxFeeRate

	type testData struct {
		name      string
		bal       uint64
		balErr    error
		lots      uint64
		token     bool
		parentBal uint64

		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
	}

	tests := []testData{
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
			name:      "not enough for fees - token",
			bal:       lotSize,
			parentBal: oneFeeToken - 1,
			lots:      1,
			token:     true,

			wantErr: true,
		},
		{
			name: "one lot enough for fees",
			bal:  oneLock,
			lots: 1,

			wantLots:      1,
			wantValue:     lotSize,
			wantMaxFees:   tETH.MaxFeeRate * ethGases.Swap,
			wantBestCase:  (baseFee + tip) * ethGases.Swap,
			wantWorstCase: (baseFee + tip) * ethGases.Swap,
		},
		{
			name:      "one lot enough for fees - token",
			bal:       lotSize,
			lots:      1,
			parentBal: oneFeeToken,
			token:     true,

			wantLots:      1,
			wantValue:     lotSize,
			wantMaxFees:   tToken.MaxFeeRate * tokenGases.Swap,
			wantBestCase:  (baseFee + tip) * tokenGases.Swap,
			wantWorstCase: (baseFee + tip) * tokenGases.Swap,
		},
		{
			name: "more lots than max lots",
			bal:  oneLock*2 - 1,
			lots: 2,

			wantErr: true,
		},
		{
			name:      "more lots than max lots - token",
			bal:       lotSize*2 - 1,
			lots:      2,
			token:     true,
			parentBal: oneFeeToken * 2,

			wantErr: true,
		},
		{
			name: "fewer than max lots",
			bal:  10 * oneLock,
			lots: 4,

			wantLots:      4,
			wantValue:     4 * lotSize,
			wantMaxFees:   4 * tETH.MaxFeeRate * ethGases.Swap,
			wantBestCase:  (baseFee + tip) * ethGases.Swap,
			wantWorstCase: 4 * (baseFee + tip) * ethGases.Swap,
		},
		{
			name:      "fewer than max lots - token",
			bal:       10 * lotSize,
			lots:      4,
			token:     true,
			parentBal: oneFeeToken * 4,

			wantLots:      4,
			wantValue:     4 * lotSize,
			wantMaxFees:   4 * tToken.MaxFeeRate * tokenGases.Swap,
			wantBestCase:  (baseFee + tip) * tokenGases.Swap,
			wantWorstCase: 4 * (baseFee + tip) * tokenGases.Swap,
		},
		{
			name:   "balanceError",
			bal:    5 * lotSize,
			balErr: errors.New(""),
			lots:   1,

			wantErr: true,
		},
		{
			name:   "balanceError - token",
			bal:    5 * lotSize,
			balErr: errors.New(""),
			lots:   1,
			token:  true,

			wantErr: true,
		},
	}

	runTest := func(t *testing.T, test testData) {
		var assetID uint32 = BipID
		assetCfg := tETH
		if test.token {
			assetID = simnetTokenID
			assetCfg = tToken
		}

		w, _, node, shutdown := tassetWallet(assetID)
		defer shutdown()
		node.baseFee, node.tip = dexeth.GweiToWei(baseFee), dexeth.GweiToWei(tip)

		if test.token {
			node.tContractor.gasEstimates = &tokenGases
			node.tokenContractor.bal = dexeth.GweiToWei(test.bal)
			node.bal = dexeth.GweiToWei(test.parentBal)
		} else {
			node.bal = dexeth.GweiToWei(test.bal)
		}

		node.balErr = test.balErr

		preSwap, err := w.PreSwap(&asset.PreSwapForm{
			Version:       assetCfg.Version,
			LotSize:       lotSize,
			Lots:          test.lots,
			MaxFeeRate:    assetCfg.MaxFeeRate,
			FeeSuggestion: feeSuggestion, // ignored
			RedeemVersion: tBTC.Version,
			RedeemAssetID: tBTC.ID,
		})

		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error")
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		est := preSwap.Estimate

		if est.Lots != test.wantLots {
			t.Fatalf("want lots %v got %v", test.wantLots, est.Lots)
		}
		if est.Value != test.wantValue {
			t.Fatalf("want value %v got %v", test.wantValue, est.Value)
		}
		if est.MaxFees != test.wantMaxFees {
			t.Fatalf("want maxFees %v got %v", test.wantMaxFees, est.MaxFees)
		}
		if est.RealisticBestCase != test.wantBestCase {
			t.Fatalf("want best case %v got %v", test.wantBestCase, est.RealisticBestCase)
		}
		if est.RealisticWorstCase != test.wantWorstCase {
			t.Fatalf("want worst case %v got %v", test.wantWorstCase, est.RealisticWorstCase)
		}
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runTest(t, test)
		})
	}
}

func TestSwap(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testSwap(t, BipID) })
	t.Run("token", func(t *testing.T) { testSwap(t, simnetTokenID) })
}

func testSwap(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	receivingAddress := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	node.tContractor.initTx = types.NewTx(&types.DynamicFeeTx{})

	coinIDsForAmounts := func(coinAmounts []uint64, n uint64) []dex.Bytes {
		coinIDs := make([]dex.Bytes, 0, len(coinAmounts))
		for _, amt := range coinAmounts {
			if assetID == BipID {
				coinIDs = append(coinIDs, createFundingCoin(eth.addr, amt).RecoveryID())
			} else {
				fees := n * tokenGases.Swap * tToken.MaxFeeRate
				coinIDs = append(coinIDs, createTokenFundingCoin(eth.addr, amt, fees).RecoveryID())
			}
		}
		return coinIDs
	}

	refreshWalletAndFundCoins := func(bal uint64, coinAmounts []uint64, n uint64) asset.Coins {
		if assetID == BipID {
			node.bal = ethToWei(bal)
		} else {
			node.tokenContractor.bal = ethToWei(bal)
			node.bal = ethToWei(10)
		}

		eth.lockedFunds.initiateReserves = 0
		eth.lockedFunds.redemptionReserves = 0
		eth.lockedFunds.refundReserves = 0
		coins, err := w.FundingCoins(coinIDsForAmounts(coinAmounts, n))
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		return coins
	}

	gasNeededForSwaps := func(numSwaps int) uint64 {
		if assetID == BipID {
			return ethGases.Swap * uint64(numSwaps)
		} else {
			return tokenGases.Swap * uint64(numSwaps)
		}

	}

	testSwap := func(testName string, swaps asset.Swaps, expectError bool) {
		t.Helper()
		originalBalance, err := eth.Balance()
		if err != nil {
			t.Fatalf("%v: error getting balance: %v", testName, err)
		}

		receipts, changeCoin, feeSpent, err := w.Swap(&swaps)
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
			if uint64(receipt.Expiration().Unix()) != contract.LockTime {
				t.Fatalf("%v: expected expiration %v != expiration %v",
					testName, time.Unix(int64(contract.LockTime), 0), receipts[0].Expiration())
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
			if swaps.Version != ver {
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
			expectedLocked = originalBalance.Locked - totalCoinValue
			if assetID == BipID {
				expectedLocked -= gasNeededForSwaps(len(swaps.Contracts)) * swaps.FeeRate
			}
		} else {
			expectedLocked = originalBalance.Locked - totalInputValue
		}
		if expectedLocked != postSwapBalance.Locked {
			t.Fatalf("%v: funds locked after swap expected: %v != actual: %v",
				testName, expectedLocked, postSwapBalance.Locked)
		}

		// Check that change coin is correctly returned
		expectedChangeValue := totalInputValue - totalCoinValue
		if assetID == BipID {
			expectedChangeValue -= gasNeededForSwaps(len(swaps.Contracts)) * swaps.FeeRate
		}
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
	expiration := uint64(time.Now().Add(time.Hour * 8).Unix())

	// Ensure error when initializing swap errors
	node.tContractor.initErr = errors.New("")
	contracts := []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   expiration,
		},
	}
	inputs := refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)}, 1)
	assetCfg := tETH
	if assetID != BipID {
		assetCfg = tToken
	}
	swaps := asset.Swaps{
		Version:    assetCfg.Version,
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
		LockChange: false,
	}
	testSwap("error initialize but no send", swaps, true)
	node.tContractor.initErr = nil

	// Tests one contract without locking change
	contracts = []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   expiration,
		},
	}

	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)}, 1)
	swaps = asset.Swaps{
		Version:    assetCfg.Version,
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
		LockChange: false,
	}
	testSwap("one contract, don't lock change", swaps, false)

	// Test one contract with locking change
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)}, 1)
	swaps = asset.Swaps{
		Version:    assetCfg.Version,
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
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
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(3)}, 2)
	swaps = asset.Swaps{
		Version:    assetCfg.Version,
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
		LockChange: false,
	}
	testSwap("two contracts", swaps, false)

	// Test error when funding coins are not enough to cover swaps
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(1)}, 2)
	swaps = asset.Swaps{
		Version:    assetCfg.Version,
		Inputs:     inputs,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
		LockChange: false,
	}
	testSwap("funding coins not enough balance", swaps, true)

	// Ensure when funds are exactly the same as required works properly
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * dexeth.InitGas(1, 0))}, 2)
	swaps = asset.Swaps{
		Inputs:     inputs,
		Version:    assetCfg.Version,
		Contracts:  contracts,
		FeeRate:    assetCfg.MaxFeeRate,
		LockChange: false,
	}
	testSwap("exact change", swaps, false)
}

func TestPreRedeem(t *testing.T) {
	w, _, _, shutdown := tassetWallet(BipID)
	defer shutdown()

	form := &asset.PreRedeemForm{
		Version:       tETH.Version,
		Lots:          5,
		FeeSuggestion: 100,
	}

	preRedeem, err := w.PreRedeem(form)
	if err != nil {
		t.Fatalf("unexpected PreRedeem error: %v", err)
	}

	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("best case > worst case")
	}

	// Token
	w, _, _, shutdown2 := tassetWallet(simnetTokenID)
	defer shutdown2()

	form.Version = tToken.Version

	preRedeem, err = w.PreRedeem(form)
	if err != nil {
		t.Fatalf("unexpected token PreRedeem error: %v", err)
	}

	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("token best case > worst case")
	}
}

func TestRedeem(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testRedeem(t, BipID) })
	t.Run("token", func(t *testing.T) { testRedeem(t, simnetTokenID) })
}

func testRedeem(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	// Test with a non-zero contract version to ensure it makes it into the receipt
	contractVer := uint32(1)

	eth.versionedGases[1] = ethGases
	if assetID != BipID {
		eth.versionedGases[1] = &tokenGases
	}

	tokenContracts := eth.tokens[simnetTokenID].NetTokens[dex.Simnet].SwapContracts
	tokenContracts[1] = tokenContracts[0]
	defer delete(tokenContracts, 1)

	contractorV1 := &tContractor{
		swapMap:      make(map[[32]byte]*dexeth.SwapState, 1),
		gasEstimates: ethGases,
		redeemTx:     types.NewTx(&types.DynamicFeeTx{Data: []byte{1, 2, 3}}),
	}
	var c contractor = contractorV1
	if assetID != BipID {
		c = &tTokenContractor{
			tContractor: contractorV1,
		}
	}
	eth.contractors[1] = c

	addSwapToSwapMap := func(secretHash [32]byte, value uint64, step dexeth.SwapStep) {
		swap := dexeth.SwapState{
			BlockHeight: 1,
			LockTime:    time.Now(),
			Initiator:   testAddressB,
			Participant: testAddressA,
			Value:       dexeth.GweiToWei(value),
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

	addSwapToSwapMap(secretHashes[0], 1e9, dexeth.SSInitiated) // states will be reset by tests though
	addSwapToSwapMap(secretHashes[1], 1e9, dexeth.SSInitiated)

	/* COMMENTED while estimateRedeemGas is on the $#!t list
	var redeemGas uint64
	if assetID == BipID {
		redeemGas = ethGases.Redeem
	} else {
		redeemGas = tokenGases.Redeem
	}

	var higherGasEstimate uint64 = redeemGas * 2 * 12 / 10       // 120% of estimate
	var doubleGasEstimate uint64 = (redeemGas * 2 * 2) * 10 / 11 // 200% of estimate after 10% increase
	// var moreThanDoubleGasEstimate uint64 = (redeemGas * 2 * 21 / 10) * 10 / 11 // > 200% of estimate after 10% increase
	// additionalFundsNeeded calculates the amount of available funds that we be
	// needed to use a higher gas estimate than the original, and double the base
	// fee if it is higher than the server's max fee rate.
	additionalFundsNeeded := func(feeSuggestion, baseFee, gasEstimate, numRedeems uint64) uint64 {
		originalReserves := feeSuggestion * redeemGas * numRedeems

		var gasFeeCap, gasLimit uint64
		if gasEstimate > redeemGas {
			gasLimit = gasEstimate * 11 / 10
		} else {
			gasLimit = redeemGas * numRedeems
		}

		if baseFee > feeSuggestion {
			gasFeeCap = 2 * baseFee
		} else {
			gasFeeCap = feeSuggestion
		}

		amountRequired := gasFeeCap * gasLimit

		return amountRequired - originalReserves
	}
	*/

	var bestBlock int64 = 123
	node.bestHdr = &types.Header{
		Number: big.NewInt(bestBlock),
	}

	swappableSwapMap := map[[32]byte]dexeth.SwapStep{
		secretHashes[0]: dexeth.SSInitiated,
		secretHashes[1]: dexeth.SSInitiated,
	}

	tests := []struct {
		name              string
		form              asset.RedeemForm
		redeemErr         error
		swapMap           map[[32]byte]dexeth.SwapStep
		swapErr           error
		ethBal            *big.Int
		baseFee           *big.Int
		redeemGasOverride *uint64
		expectedGasFeeCap *big.Int
		expectError       bool
	}{
		{
			name:              "ok",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(10e9),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		/* COMMENTED while estimateRedeemGas is on the $#!t list
		{
			name:              "higher gas estimate than reserved",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 50, higherGasEstimate, 2)),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			redeemGasOverride: &higherGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:              "gas estimate double reserved",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(10e9),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			redeemGasOverride: &doubleGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:              "gas estimate more than double reserved",
			expectError:       true,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 50, moreThanDoubleGasEstimate, 2)),
			baseFee:           dexeth.GweiToWei(100),
			redeemGasOverride: &moreThanDoubleGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:              "higher gas estimate than reserved, balance too low",
			expectError:       true,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 50, higherGasEstimate, 2) - 1),
			baseFee:           dexeth.GweiToWei(100),
			redeemGasOverride: &higherGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:              "base fee > fee suggestion",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 200, higherGasEstimate, 2)),
			baseFee:           dexeth.GweiToWei(150),
			expectedGasFeeCap: dexeth.GweiToWei(300),
			redeemGasOverride: &higherGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:              "base fee > fee suggestion, not enough for 2x base fee",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 149, higherGasEstimate, 2)),
			baseFee:           dexeth.GweiToWei(150),
			expectedGasFeeCap: dexeth.GweiToWei(298),
			redeemGasOverride: &higherGasEstimate,
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:], // redundant for all current assets, unused with eth
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		*/
		{
			name:        "not redeemable",
			expectError: true,
			swapMap: map[[32]byte]dexeth.SwapStep{
				secretHashes[0]: dexeth.SSNone,
				secretHashes[1]: dexeth.SSRedeemed,
			},
			ethBal:  dexeth.GweiToWei(10e9),
			baseFee: dexeth.GweiToWei(100),

			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:        "isRedeemable error",
			expectError: true,
			ethBal:      dexeth.GweiToWei(10e9),
			baseFee:     dexeth.GweiToWei(100),
			swapErr:     errors.New("swap() error"),
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[1]),
							SecretHash: secretHashes[1][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[1][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:        "redeem error",
			redeemErr:   errors.New(""),
			swapMap:     swappableSwapMap,
			expectError: true,
			ethBal:      dexeth.GweiToWei(10e9),
			baseFee:     dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[0]),
							SecretHash: secretHashes[0][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[0][:],
					},
				},
				FeeSuggestion: 200,
			},
		},
		{
			name:        "swap not found in contract",
			swapMap:     swappableSwapMap,
			expectError: true,
			ethBal:      dexeth.GweiToWei(10e9),
			baseFee:     dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions: []*asset.Redemption{
					{
						Spends: &asset.AuditInfo{
							Contract:   dexeth.EncodeContractData(contractVer, secretHashes[2]),
							SecretHash: secretHashes[2][:],
							Coin: &coin{
								id: randomHash(),
							},
						},
						Secret: secrets[2][:],
					},
				},
				FeeSuggestion: 100,
			},
		},
		{
			name:        "empty redemptions slice error",
			ethBal:      dexeth.GweiToWei(10e9),
			baseFee:     dexeth.GweiToWei(100),
			swapMap:     swappableSwapMap,
			expectError: true,
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{},
				FeeSuggestion: 100,
			},
		},
	}

	for _, test := range tests {
		contractorV1.redeemErr = test.redeemErr
		contractorV1.swapErr = test.swapErr
		contractorV1.redeemGasOverride = test.redeemGasOverride
		for secretHash, step := range test.swapMap {
			contractorV1.swapMap[secretHash].State = step
		}

		eth.monitoredTxsMtx.Lock()
		eth.monitoredTxs = make(map[common.Hash]*monitoredTx)
		eth.monitoredTxsMtx.Unlock()

		node.bal = test.ethBal
		node.baseFee = test.baseFee

		txs, out, fees, err := w.Redeem(&test.form)
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
		if assetID != BipID {
			expectedGas = tokenGases.Redeem + (uint64(len(test.form.Redemptions))-1)*tokenGases.RedeemAdd
		}
		expectedFees := expectedGas * test.form.FeeSuggestion
		if fees != expectedFees {
			t.Fatalf("%v: expected fees %d, but got %d", test.name, expectedFees, fees)
		}

		// Check that value of output coin is as axpected
		var totalSwapValue uint64
		for _, redemption := range test.form.Redemptions {
			_, secretHash, err := dexeth.DecodeContractData(redemption.Spends.Contract)
			if err != nil {
				t.Fatalf("DecodeContractData: %v", err)
			}
			// secretHash should equal redemption.Spends.SecretHash, but it's
			// not part of the Redeem code, just the test input consistency.
			swap := contractorV1.swapMap[secretHash]
			totalSwapValue += dexeth.WeiToGwei(swap.Value)
		}
		if out.Value() != totalSwapValue {
			t.Fatalf("expected coin value to be %d but got %d",
				totalSwapValue, out.Value())
		}

		// Check that gas limit in the transaction is as expected
		var expectedGasLimit uint64
		// if test.redeemGasOverride == nil {
		if assetID == BipID {
			expectedGasLimit = ethGases.Redeem * uint64(len(test.form.Redemptions))
		} else {
			expectedGasLimit = tokenGases.Redeem * uint64(len(test.form.Redemptions))
		}
		// } else {
		// 	expectedGasLimit = *test.redeemGasOverride * 11 / 10
		// }
		if contractorV1.lastRedeemOpts.GasLimit != expectedGasLimit {
			t.Fatalf("%s: expected gas limit %d, but got %d", test.name, expectedGasLimit, contractorV1.lastRedeemOpts.GasLimit)
		}

		// Check that the gas fee cap in the transaction is as expected
		if contractorV1.lastRedeemOpts.GasFeeCap.Cmp(test.expectedGasFeeCap) != 0 {
			t.Fatalf("%s: expected gas fee cap %v, but got %v", test.name, test.expectedGasFeeCap, contractorV1.lastRedeemOpts.GasFeeCap)
		}

		// Check that tx was stored in the monitored transactions
		txHash := contractorV1.redeemTx.Hash()
		eth.monitoredTxsMtx.RLock()
		monitoredTx, stored := eth.monitoredTxs[txHash]
		if !stored {
			t.Fatalf("%s: tx was not stored in monitored transactions", test.name)
		}
		if monitoredTx.blockSubmitted != uint64(bestBlock) {
			t.Fatalf("%s: expected block submitted to be %d, but got %d", test.name, bestBlock, monitoredTx.blockSubmitted)
		}
		eth.monitoredTxsMtx.RUnlock()
	}
}

func TestMaxOrder(t *testing.T) {
	const baseFee, tip = 42, 2

	type testData struct {
		name          string
		bal           uint64
		balErr        error
		lotSize       uint64
		maxFeeRate    uint64
		feeSuggestion uint64
		token         bool
		parentBal     uint64
		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
		wantLocked    uint64
	}
	tests := []testData{
		{
			name:          "no balance",
			bal:           0,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "no balance - token",
			bal:           0,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			token:         true,
			parentBal:     100,
		},
		{
			name:          "not enough for fees",
			bal:           10,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "not enough for fees - token",
			bal:           10,
			token:         true,
			parentBal:     0,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
		},
		{
			name:          "one lot enough for fees",
			bal:           11,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * ethGases.Swap,
			wantBestCase:  (baseFee + tip) * ethGases.Swap,
			wantWorstCase: (baseFee + tip) * ethGases.Swap,
			wantLocked:    ethToGwei(10) + (100 * ethGases.Swap),
		},
		{
			name:          "one lot enough for fees - token",
			bal:           11,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			token:         true,
			parentBal:     1,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * tokenGases.Swap,
			wantBestCase:  (baseFee + tip) * tokenGases.Swap,
			wantWorstCase: (baseFee + tip) * tokenGases.Swap,
			wantLocked:    ethToGwei(10) + (100 * tokenGases.Swap),
		},
		{
			name:          "multiple lots",
			bal:           51,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * ethGases.Swap,
			wantBestCase:  (baseFee + tip) * ethGases.Swap,
			wantWorstCase: 5 * (baseFee + tip) * ethGases.Swap,
			wantLocked:    ethToGwei(50) + (5 * 100 * ethGases.Swap),
		},
		{
			name:          "multiple lots - token",
			bal:           51,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			token:         true,
			parentBal:     1,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * tokenGases.Swap,
			wantBestCase:  (baseFee + tip) * tokenGases.Swap,
			wantWorstCase: 5 * (baseFee + tip) * tokenGases.Swap,
			wantLocked:    ethToGwei(50) + (5 * 100 * tokenGases.Swap),
		},
		{
			name:          "balanceError",
			bal:           51,
			lotSize:       10,
			feeSuggestion: 90,
			maxFeeRate:    100,
			balErr:        errors.New(""),
			wantErr:       true,
		},
	}

	runTest := func(t *testing.T, test testData) {
		var assetID uint32 = BipID
		assetCfg := tETH
		if test.token {
			assetID = simnetTokenID
			assetCfg = tToken
		}

		w, _, node, shutdown := tassetWallet(assetID)
		defer shutdown()
		node.baseFee, node.tip = dexeth.GweiToWei(baseFee), dexeth.GweiToWei(tip)

		if test.token {
			node.tContractor.gasEstimates = &tokenGases
			node.tokenContractor.bal = dexeth.GweiToWei(ethToGwei(test.bal))
			node.bal = dexeth.GweiToWei(ethToGwei(test.parentBal))
		} else {
			node.bal = dexeth.GweiToWei(ethToGwei(test.bal))
		}

		node.balErr = test.balErr

		maxOrder, err := w.MaxOrder(&asset.MaxOrderForm{
			LotSize:       ethToGwei(test.lotSize),
			FeeSuggestion: test.feeSuggestion, // ignored
			AssetVersion:  assetCfg.Version,
			MaxFeeRate:    test.maxFeeRate,
			RedeemVersion: tBTC.Version,
			RedeemAssetID: tBTC.ID,
		})
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error")
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if maxOrder.Lots != test.wantLots {
			t.Fatalf("want lots %v got %v", test.wantLots, maxOrder.Lots)
		}
		if maxOrder.Value != test.wantValue {
			t.Fatalf("want value %v got %v", test.wantValue, maxOrder.Value)
		}
		if maxOrder.MaxFees != test.wantMaxFees {
			t.Fatalf("want maxFees %v got %v", test.wantMaxFees, maxOrder.MaxFees)
		}
		if maxOrder.RealisticBestCase != test.wantBestCase {
			t.Fatalf("want best case %v got %v", test.wantBestCase, maxOrder.RealisticBestCase)
		}
		if maxOrder.RealisticWorstCase != test.wantWorstCase {
			t.Fatalf("want worst case %v got %v", test.wantWorstCase, maxOrder.RealisticWorstCase)
		}
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runTest(t, test)
		})
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
		bigVal := new(big.Int).Set(init.Value)
		abiInitiations = append(abiInitiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(init.LockTime.Unix()),
			SecretHash:      init.SecretHash,
			Participant:     init.Participant,
			Value:           new(big.Int).Mul(bigVal, big.NewInt(dexeth.GweiFactor)),
		})
	}
	return (*dexeth.ABIs[0]).Pack("initiate", abiInitiations)
}

func packRedeemDataV0(redemptions []*dexeth.Redemption) ([]byte, error) {
	abiRedemptions := make([]swapv0.ETHSwapRedemption, 0, len(redemptions))
	for _, redeem := range redemptions {
		abiRedemptions = append(abiRedemptions, swapv0.ETHSwapRedemption{
			Secret:     redeem.Secret,
			SecretHash: redeem.SecretHash,
		})
	}
	return (*dexeth.ABIs[0]).Pack("redeem", abiRedemptions)
}

func TestAuditContract(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testAuditContract(t, BipID) })
	t.Run("token", func(t *testing.T) { testAuditContract(t, simnetTokenID) })
}

func testAuditContract(t *testing.T, assetID uint32) {
	_, eth, _, shutdown := tassetWallet(assetID)
	defer shutdown()

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
					Value:       dexeth.GweiToWei(1),
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       dexeth.GweiToWei(1),
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
					Value:       dexeth.GweiToWei(1),
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
					Value:       dexeth.GweiToWei(1),
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       dexeth.GweiToWei(1),
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
					Value:       dexeth.GweiToWei(1),
				},
				{
					LockTime:    laterThanNow,
					SecretHash:  secretHashes[1],
					Participant: testAddressB,
					Value:       dexeth.GweiToWei(1),
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
			copy(txHash[:], encode.RandomBytes(20))
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

	eth := &baseWallet{
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
			owns, err := eth.OwnsDepositAddress(tt.address)
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

	node := newTestNode(BipID)
	eth := &assetWallet{
		baseWallet: &baseWallet{
			node: node,
			addr: node.address(),
			ctx:  ctx,
			log:  tLogger,
		},
		assetID: BipID,
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
	_, eth, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	state := &dexeth.SwapState{}
	hdr := &types.Header{}

	node.tContractor.swapMap[secretHash] = state

	state.BlockHeight = 5
	state.State = dexeth.SSInitiated
	hdr.Number = big.NewInt(6)
	node.bestHdr = hdr

	ver := uint32(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checkResult := func(expErr bool, expConfs uint32, expSpent bool) {
		confs, spent, err := eth.SwapConfirmations(ctx, nil, dexeth.EncodeContractData(ver, secretHash), time.Time{})
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
	node.tContractor.swapErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.tContractor.swapErr = nil

	// header error
	node.bestHdrErr = fmt.Errorf("test error")
	checkResult(true, 0, false)
	node.bestHdrErr = nil

	// ErrSwapNotInitiated
	state.State = dexeth.SSNone
	_, _, err := eth.SwapConfirmations(ctx, nil, dexeth.EncodeContractData(0, secretHash), time.Time{})
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

	settings := map[string]string{providersKey: "a.ipc"}
	err := CreateEVMWallet(dexeth.ChainIDs[dex.Testnet], &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     encode.RandomBytes(32),
		Pass:     encode.RandomBytes(32),
		Settings: settings,
		DataDir:  tmpDir,
		Net:      dex.Testnet,
		Logger:   logger,
	}, &testnetCompatibilityData, true)
	if err != nil {
		t.Fatalf("CreateWallet error: %v", err)
	}

	// Make sure default gas fee limit is used when nothing is set
	cfg := &asset.WalletConfig{
		Type:     walletTypeRPC,
		Settings: settings,
		DataDir:  tmpDir,
	}
	wallet, err := drv.Open(cfg, logger, dex.Testnet)
	if err != nil {
		t.Fatalf("driver open error: %v", err)
	}
	eth, ok := wallet.(*ETHWallet)
	if !ok {
		t.Fatalf("failed to cast wallet as assetWallet")
	}
	if eth.gasFeeLimit() != defaultGasFeeLimit {
		t.Fatalf("expected gasFeeLimit to be default, but got %v", eth.gasFeeLimit())
	}

	// Make sure gas fee limit is properly parsed from settings
	cfg.Settings["gasfeelimit"] = "150"
	wallet, err = drv.Open(cfg, logger, dex.Testnet)
	if err != nil {
		t.Fatalf("driver open error: %v", err)
	}
	eth, ok = wallet.(*ETHWallet)
	if !ok {
		t.Fatalf("failed to cast wallet as assetWallet")
	}
	if eth.gasFeeLimit() != 150 {
		t.Fatalf("expected gasFeeLimit to be 150, but got %v", eth.gasFeeLimit())
	}
}

func TestDriverExists(t *testing.T) {
	drv := &Driver{}
	tmpDir := t.TempDir()

	settings := map[string]string{providersKey: "a.ipc"}

	// no wallet
	exists, err := drv.Exists(walletTypeRPC, tmpDir, settings, dex.Simnet)
	if err != nil {
		t.Fatalf("Exists error for no geth wallet: %v", err)
	}
	if exists {
		t.Fatalf("Uninitiated wallet exists")
	}

	// Create the wallet.
	err = CreateEVMWallet(dexeth.ChainIDs[dex.Simnet], &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     encode.RandomBytes(32),
		Pass:     encode.RandomBytes(32),
		Settings: settings,
		DataDir:  tmpDir,
		Net:      dex.Simnet,
		Logger:   tLogger,
	}, &testnetCompatibilityData, true)
	if err != nil {
		t.Fatalf("CreateEVMWallet error: %v", err)
	}

	// exists
	exists, err = drv.Exists(walletTypeRPC, tmpDir, settings, dex.Simnet)
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

	// Token funding coin
	fc := createTokenFundingCoin(address, 1000, 1)
	coinID, err = drv.DecodeCoinID(fc.RecoveryID())
	if err != nil {
		t.Fatalf("error decoding token coin id: %v", err)
	}
	if coinID != fc.String() {
		t.Fatalf("expected coin id to be %s but got %s", fc.String(), coinID)
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
	_, eth, node, shutdown := tassetWallet(BipID)
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

	node.tContractor.swapMap[secretHash] = state
	node.bestHdr = header

	contract := make([]byte, 36)
	copy(contract[4:], secretHash[:])

	ensureResult := func(tag string, expErr, expExpired bool) {
		t.Helper()
		expired, _, err := eth.ContractLockTimeExpired(context.Background(), contract)
		switch {
		case err != nil:
			if !expErr {
				t.Fatalf("%s: ContractLockTimeExpired error existing expired swap: %v", tag, err)
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
	delete(node.tContractor.swapMap, secretHash)
	ensureResult("missing swap", true, false)
	node.tContractor.swapMap[secretHash] = state

	// lock time not expired
	state.LockTime = time.Now().Add(time.Minute)
	ensureResult("lock time not expired", false, false)

	// wrong contract version
	contract[3] = 1
	ensureResult("wrong contract version", true, false)
	contract[3] = 0

	// bad contract
	contract = append(contract, 0) // nolint:makezero
	ensureResult("bad contract", true, false)
}

func TestFindRedemption(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testFindRedemption(t, BipID) })
	t.Run("token", func(t *testing.T) { testFindRedemption(t, simnetTokenID) })
}

func testFindRedemption(t *testing.T, assetID uint32) {
	_, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	contract := dexeth.EncodeContractData(0, secretHash)
	state := &dexeth.SwapState{
		Secret: secret,
		State:  dexeth.SSInitiated,
	}

	node.tContractor.swapMap[secretHash] = state

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
	node.tContractor.swapErr = errors.New("test error")
	runTest("swap error", true, 0)
	node.tContractor.swapErr = nil

	// swap error after queuing
	runWithUpdate("swap error after queuing", true, dexeth.SSInitiated, func() {
		node.tContractor.swapErr = errors.New("test error")
	})
	node.tContractor.swapErr = nil

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
	t.Run("eth", func(t *testing.T) { testRefundReserves(t, BipID) })
	t.Run("token", func(t *testing.T) { testRefundReserves(t, simnetTokenID) })
}

func testRefundReserves(t *testing.T, assetID uint32) {
	wi, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	w := wi.(asset.AccountLocker)

	node.bal = dexeth.GweiToWei(1e9)
	node.refundable = true
	node.swapVers = map[uint32]struct{}{0: {}}

	var secretHash [32]byte
	node.swapMap = map[[32]byte]*dexeth.SwapState{secretHash: {}}

	feeWallet := eth
	gasesV0 := dexeth.VersionedGases[0]
	gasesV1 := &dexeth.Gases{Refund: 1e6}
	assetV0 := *tETH

	assetV1 := *tETH
	if assetID == BipID {
		eth.versionedGases[1] = gasesV1
	} else {
		feeWallet = node.tokenParent
		assetV0 = *tToken
		assetV1 = *tToken
		tokenContracts := eth.tokens[simnetTokenID].NetTokens[dex.Simnet].SwapContracts
		tc := *tokenContracts[0]
		tc.Gas = *gasesV1
		tokenContracts[1] = &tc
		defer delete(tokenContracts, 1)
		gasesV0 = &tokenGases
		eth.versionedGases[0] = gasesV0
		eth.versionedGases[1] = gasesV1
		node.tokenContractor.bal = dexeth.GweiToWei(1e9)
	}

	assetV0.MaxFeeRate = 45
	assetV1.Version = 1
	assetV1.MaxFeeRate = 50

	// Lock for 3 refunds with contract version 0
	v0Val, err := w.ReserveNRefunds(3, assetV0.Version, assetV0.MaxFeeRate)
	if err != nil {
		t.Fatalf("ReserveNRefunds error: %v", err)
	}
	lockPerV0 := gasesV0.Refund * assetV0.MaxFeeRate

	expLock := 3 * lockPerV0
	if feeWallet.lockedFunds.refundReserves != expLock {
		t.Fatalf("wrong v0 locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.refundReserves)
	}
	if v0Val != expLock {
		t.Fatalf("expected locked %d, got %d", expLock, v0Val)
	}

	// Lock for 2 refunds with contract version 1
	v1Val, err := w.ReserveNRefunds(2, assetV1.Version, assetV1.MaxFeeRate)
	if err != nil {
		t.Fatalf("ReserveNRefunds error: %v", err)
	}
	lockPerV1 := gasesV1.Refund * assetV1.MaxFeeRate
	v1Lock := 2 * lockPerV1
	expLock += v1Lock
	if feeWallet.lockedFunds.refundReserves != expLock {
		t.Fatalf("wrong v1 locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.refundReserves)
	}
	if v1Val != v1Lock {
		t.Fatalf("expected locked %d, got %d", v1Lock, v1Val)
	}

	w.UnlockRefundReserves(9e5)
	expLock -= 9e5
	if feeWallet.lockedFunds.refundReserves != expLock {
		t.Fatalf("incorrect amount locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.refundReserves)
	}

	// Reserve more than available should return an error
	err = w.ReReserveRefund(1e9 + 1)
	if err == nil {
		t.Fatalf("expected an error but did not get")
	}

	err = w.ReReserveRefund(5e6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expLock += 5e6
	if feeWallet.lockedFunds.refundReserves != expLock {
		t.Fatalf("incorrect amount locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.refundReserves)
	}
}

func TestRedemptionReserves(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testRedemptionReserves(t, BipID) })
	t.Run("token", func(t *testing.T) { testRedemptionReserves(t, simnetTokenID) })
}

func testRedemptionReserves(t *testing.T, assetID uint32) {
	wi, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	w := wi.(asset.AccountLocker)

	node.bal = dexeth.GweiToWei(1e9)
	// node.tContractor.swapMap =  map[[32]byte]*dexeth.SwapState{
	// 	secretHashes[0]: {
	// 		State: dexeth.SSInitiated,
	// 	},
	// },

	var secretHash [32]byte
	node.tContractor.swapMap[secretHash] = &dexeth.SwapState{}

	gasesV1 := &dexeth.Gases{Redeem: 1e6, RedeemAdd: 85e5}
	gasesV0 := dexeth.VersionedGases[0]
	assetV0 := *tETH
	assetV1 := *tETH
	feeWallet := eth
	if assetID == BipID {
		eth.versionedGases[1] = gasesV1
	} else {
		node.tokenContractor.allow = unlimitedAllowanceReplenishThreshold
		feeWallet = node.tokenParent
		assetV0 = *tToken
		assetV1 = *tToken
		gasesV0 = &tokenGases
		eth.versionedGases[0] = gasesV0
		eth.versionedGases[1] = gasesV1
	}

	assetV0.MaxFeeRate = 45
	assetV1.Version = 1
	assetV1.MaxFeeRate = 50

	v0Val, err := w.ReserveNRedemptions(3, assetV0.Version, assetV0.MaxFeeRate)
	if err != nil {
		t.Fatalf("reservation error: %v", err)
	}

	lockPerV0 := gasesV0.Redeem * assetV0.MaxFeeRate
	expLock := 3 * lockPerV0

	if feeWallet.lockedFunds.redemptionReserves != expLock {
		t.Fatalf("wrong v0 locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.redemptionReserves)
	}

	if v0Val != expLock {
		t.Fatalf("expected value %d, got %d", lockPerV0, v0Val)
	}

	v1Val, err := w.ReserveNRedemptions(2, assetV1.Version, assetV1.MaxFeeRate)
	if err != nil {
		t.Fatalf("reservation error: %v", err)
	}

	lockPerV1 := gasesV1.Redeem * assetV1.MaxFeeRate
	v1Lock := 2 * lockPerV1
	if v1Val != v1Lock {
		t.Fatal("wrong reserved val", v1Val, v1Lock)
	}

	expLock += v1Lock
	if feeWallet.lockedFunds.redemptionReserves != expLock {
		t.Fatalf("wrong v1 locked. wanted %d, got %d", expLock, feeWallet.lockedFunds.redemptionReserves)
	}

	// Run some token tests
	if assetID == BipID {
		return
	}
}

func ethToGwei(v uint64) uint64 {
	return v * dexeth.GweiFactor
}

func ethToWei(v uint64) *big.Int {
	bigV := new(big.Int).SetUint64(ethToGwei(v))
	return new(big.Int).Mul(bigV, big.NewInt(dexeth.GweiFactor))
}

func TestReconfigure(t *testing.T) {
	w, eth, _, shutdown := tassetWallet(BipID)
	defer shutdown()

	reconfigurer, is := w.(asset.LiveReconfigurer)
	if !is {
		t.Fatal("wallet is not a reconfigurer")
	}

	ethCfg := &WalletConfig{
		GasFeeLimit: 123,
	}

	settings, err := config.Mapify(ethCfg)
	if err != nil {
		t.Fatal("failed to mapify")
	}

	walletCfg := &asset.WalletConfig{
		Type:     walletTypeRPC,
		Settings: settings,
	}

	restart, err := reconfigurer.Reconfigure(context.Background(), walletCfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restart {
		t.Fatalf("unexpected restart")
	}

	if eth.baseWallet.gasFeeLimit() != ethCfg.GasFeeLimit {
		t.Fatal("gas fee limit was not updated properly")
	}
}

func TestSend(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testSend(t, BipID) })
	t.Run("token", func(t *testing.T) { testSend(t, simnetTokenID) })
}

func testSend(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	tx := tTx(0, 0, 0, &testAddressA, nil)
	txHash := tx.Hash()

	node.sendTxTx = tx
	node.tokenContractor.transferTx = tx

	maxFeeRate, _ := eth.recommendedMaxFeeRate(eth.ctx)
	ethFees := dexeth.WeiToGwei(maxFeeRate) * defaultSendGasLimit
	tokenFees := dexeth.WeiToGwei(maxFeeRate) * tokenGases.Transfer

	const val = 10e9
	const testAddr = "dd93b447f7eBCA361805eBe056259853F3912E04"
	tests := []struct {
		name              string
		sendAdj, feeAdj   uint64
		balErr, sendTxErr error
		addr              string
		wantErr           bool
	}{{
		name: "ok",
		addr: testAddr,
	}, {
		name:    "balance error",
		balErr:  errors.New(""),
		wantErr: true,
		addr:    testAddr,
	}, {
		name:    "not enough",
		sendAdj: 1,
		wantErr: true,
		addr:    testAddr,
	}, {
		name:    "low fees",
		feeAdj:  1,
		wantErr: true,
		addr:    testAddr,
	}, {
		name:      "sendToAddr error",
		sendTxErr: errors.New(""),
		wantErr:   true,
		addr:      testAddr,
	}, {
		name:      "Invalid address",
		sendTxErr: errors.New("invalid hex address error"),
		wantErr:   true,
		addr:      "",
	}}

	for _, test := range tests {

		node.balErr = test.balErr
		node.sendTxErr = test.sendTxErr
		node.tokenContractor.transferErr = test.sendTxErr

		if assetID == BipID {
			node.bal = dexeth.GweiToWei(val + ethFees - test.sendAdj - test.feeAdj)
		} else {
			node.tokenContractor.bal = dexeth.GweiToWei(val - test.sendAdj)
			node.bal = dexeth.GweiToWei(tokenFees - test.feeAdj)
		}
		coin, err := w.Send(test.addr, val, 0)
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

func TestConfirmRedemption(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testConfirmRedemption(t, BipID) })
	t.Run("token", func(t *testing.T) { testConfirmRedemption(t, simnetTokenID) })
}

func testConfirmRedemption(t *testing.T, assetID uint32) {
	wi, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	txHashes := make([]common.Hash, 5)
	secrets := make([]common.Hash, 5)
	secretHashes := make([][32]byte, 5)
	for i := 0; i < 5; i++ {
		copy(txHashes[i][:], encode.RandomBytes(32))
		copy(secrets[i][:], encode.RandomBytes(32))
		secretHashes[i] = sha256.Sum256(secrets[i][:])
	}

	type txData struct {
		nonce         uint64
		gasFeeCapGwei uint64
		height        int64
		data          dex.Bytes
	}

	toEthTx := func(nonce, gasFeeCapGwei uint64, data dex.Bytes) *types.Transaction {
		return types.NewTx(&types.DynamicFeeTx{
			Nonce:     nonce,
			GasFeeCap: dexeth.GweiToWei(gasFeeCapGwei),
			Data:      data,
		})
	}

	toEthTxHash := func(nonce, gasFeeCapGwei uint64, data dex.Bytes) *common.Hash {
		txHash := toEthTx(nonce, gasFeeCapGwei, data).Hash()
		return &txHash
	}

	toEthTxCoinID := func(nonce, gasFeeCapGwei uint64, data dex.Bytes) dex.Bytes {
		txHash := toEthTx(nonce, gasFeeCapGwei, data).Hash()
		return txHash[:]
	}

	redeem0 := []*dexeth.Redemption{
		{
			Secret:     secrets[0],
			SecretHash: secretHashes[0],
		},
	}
	redeem0Data, err := packRedeemDataV0(redeem0)
	if err != nil {
		panic("failed to pack redeem data")
	}

	redeem0and1 := []*dexeth.Redemption{
		{
			Secret:     secrets[0],
			SecretHash: secretHashes[0],
		},
		{
			Secret:     secrets[1],
			SecretHash: secretHashes[1],
		},
	}
	redeem0and1Data, err := packRedeemDataV0(redeem0and1)
	if err != nil {
		panic("failed to pack redeem data")
	}

	assetRedemption := func(secretHash, secret common.Hash) *asset.Redemption {
		return &asset.Redemption{
			Spends: &asset.AuditInfo{
				Contract: dexeth.EncodeContractData(0, secretHash),
			},
			Secret: secret[:],
		}
	}

	tests := []struct {
		name string

		redemption *asset.Redemption
		coinID     dex.Bytes

		expectedResult                 *asset.ConfirmRedemptionStatus
		expectErr                      bool
		expectSwapRefundedErr          bool
		expectedResubmittedRedemptions []*asset.Redemption
		expectSentSignedTransaction    *types.Transaction
		expectedMonitoredTxs           map[common.Hash]*monitoredTx

		getTxResMap  map[common.Hash]*txData
		swapMap      map[[32]byte]*dexeth.SwapState
		monitoredTxs map[common.Hash]*monitoredTx

		redeemTx  *types.Transaction
		redeemErr error

		confNonce    uint64
		confNonceErr error

		baseFee   *big.Int
		getFeeErr error

		bestBlock  int64
		bestHdrErr error

		receipt    *types.Receipt
		receiptErr error
	}{
		{
			name:       "in monitored txs, found by geth, not yet confirmed",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        10,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			bestBlock: 11, // tx.height + txConfsNeededToConfirm - 2
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  txConfsNeededToConfirm - 1,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(3, 200, redeem0Data),
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:       "in monitored txs, found by geth, confirmed",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        10,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSRedeemed,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{},
			bestBlock:            12, // tx.height + txConfsNeededToConfirm
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  txConfsNeededToConfirm,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(3, 200, redeem0Data),
			},
			receipt: &types.Receipt{
				Status: types.ReceiptStatusSuccessful,
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:       "in monitored txs, found by geth, receipt failed",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        10,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
					replacementTx:  toEthTxHash(4, 123, redeem0Data),
				},
				(*toEthTxHash(4, 123, redeem0Data)): {
					tx:             toEthTx(4, 123, redeem0Data),
					blockSubmitted: 19,
				},
			},
			// redeemableMap: map[common.Hash]bool{
			// 	secretHashes[0]: true,
			// },
			bestBlock: 19,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0Data),
			},
			redeemTx: toEthTx(4, 123, redeem0Data),
			receipt: &types.Receipt{
				Status: types.ReceiptStatusFailed,
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:       "in monitored txs, found by geth, refunded",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        -1,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSRefunded,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			bestBlock:             19,
			expectErr:             true,
			expectSwapRefundedErr: true,
			receipt: &types.Receipt{
				Status: types.ReceiptStatusSuccessful,
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:        "not in monitored txs, not found by geth",
			coinID:      toEthTxCoinID(3, 200, redeem0Data),
			redemption:  assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(4, 123, redeem0Data)): {
					tx:             toEthTx(4, 123, redeem0Data),
					blockSubmitted: 13,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0Data),
			},
			redeemTx: toEthTx(4, 123, redeem0Data),
			baseFee:  dexeth.GweiToWei(100),
		},
		{
			name:       "not in monitored txs, found by geth, < 3 confirmations",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        10,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{},
			bestBlock:    11,

			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  2,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(3, 200, redeem0Data),
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:        "in monitored txs, not found by geth, 10 blocks since submitted",
			coinID:      toEthTxCoinID(3, 200, redeem0and1Data),
			redemption:  assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
				secretHashes[1]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0and1Data)): {
					tx:             toEthTx(3, 200, redeem0and1Data),
					blockSubmitted: 3,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0and1Data)): {
					tx:             toEthTx(3, 200, redeem0and1Data),
					blockSubmitted: 3,
					replacementTx:  toEthTxHash(4, 123, redeem0and1Data),
				},
				(*toEthTxHash(4, 123, redeem0and1Data)): {
					tx:             toEthTx(4, 123, redeem0and1Data),
					blockSubmitted: 13,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0and1Data),
			},
			expectedResubmittedRedemptions: []*asset.Redemption{
				assetRedemption(secretHashes[0], secrets[0]),
				assetRedemption(secretHashes[1], secrets[1]),
			},
			redeemTx: toEthTx(4, 123, redeem0and1Data),
			baseFee:  big.NewInt(100),
		},
		{
			name:        "in monitored txs, not found by geth, other swap in tx already complete",
			coinID:      toEthTxCoinID(3, 200, redeem0and1Data),
			redemption:  assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
				secretHashes[1]: {
					State: dexeth.SSRedeemed,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0and1Data)): {
					tx:             toEthTx(3, 200, redeem0and1Data),
					blockSubmitted: 3,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0and1Data)): {
					tx:             toEthTx(3, 200, redeem0and1Data),
					blockSubmitted: 3,
					replacementTx:  toEthTxHash(4, 123, redeem0Data),
				},
				(*toEthTxHash(4, 123, redeem0Data)): {
					tx:             toEthTx(4, 123, redeem0Data),
					blockSubmitted: 13,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0Data),
			},
			expectedResubmittedRedemptions: []*asset.Redemption{
				assetRedemption(secretHashes[0], secrets[0]),
			},
			redeemTx: toEthTx(4, 123, redeem0Data),
			baseFee:  big.NewInt(100),
		},
		{
			name:       "replaced, but call with old coin ID",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(5, 200, redeem0Data)): {
					nonce:         5,
					gasFeeCapGwei: 200,
					height:        21,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
					replacementTx:  toEthTxHash(5, 200, redeem0Data),
				},
				(*toEthTxHash(5, 200, redeem0Data)): {
					tx:             toEthTx(5, 200, redeem0Data),
					blockSubmitted: 19,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
					replacementTx:  toEthTxHash(5, 200, redeem0Data),
				},
				(*toEthTxHash(5, 200, redeem0Data)): {
					tx:             toEthTx(5, 200, redeem0Data),
					blockSubmitted: 19,
				},
			},
			bestBlock: 22,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  2,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(5, 200, redeem0Data),
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:       "found by geth, redeemed by another unknown transaction",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        -1,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSRedeemed,
				},
			},
			bestBlock: 3, // txConfsNeededToConfirm + tx.height + 1
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  txConfsNeededToConfirm,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(3, 200, redeem0Data),
			},
			baseFee: dexeth.GweiToWei(100),
		},
		{
			name:       "found by geth, nonce replaced",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        -1,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
					replacementTx:  toEthTxHash(4, 123, redeem0Data),
				},
				(*toEthTxHash(4, 123, redeem0Data)): {
					tx:             toEthTx(4, 123, redeem0Data),
					blockSubmitted: 13,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0Data),
			},
			expectedResubmittedRedemptions: []*asset.Redemption{
				assetRedemption(secretHashes[0], secrets[0]),
			},
			confNonce: 4,
			redeemTx:  toEthTx(4, 123, redeem0Data),
			baseFee:   dexeth.GweiToWei(100),
		},
		{
			name:       "found by geth, fee too low",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        -1,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
					replacementTx:  toEthTxHash(4, 123, redeem0Data),
				},
				(*toEthTxHash(4, 123, redeem0Data)): {
					tx:             toEthTx(4, 123, redeem0Data),
					blockSubmitted: 13,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(4, 123, redeem0Data),
			},
			expectedResubmittedRedemptions: []*asset.Redemption{
				assetRedemption(secretHashes[0], secrets[0]),
			},
			confNonce: 3,
			redeemTx:  toEthTx(4, 123, redeem0Data),
			baseFee:   dexeth.GweiToWei(300),
		},
		{
			name:       "found by geth, expect resubmission",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        -1,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
				},
			},
			expectedMonitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 3,
				},
			},
			bestBlock: 13,
			expectedResult: &asset.ConfirmRedemptionStatus{
				Confs:  0,
				Req:    txConfsNeededToConfirm,
				CoinID: toEthTxCoinID(3, 200, redeem0Data),
			},
			expectSentSignedTransaction: toEthTx(3, 200, redeem0Data),
			confNonce:                   3,
			baseFee:                     dexeth.GweiToWei(100),
		},
		{
			name:       "best hdr error",
			coinID:     toEthTxCoinID(3, 200, redeem0Data),
			redemption: assetRedemption(secretHashes[0], secrets[0]),
			getTxResMap: map[common.Hash]*txData{
				(*toEthTxHash(3, 200, redeem0Data)): {
					nonce:         3,
					gasFeeCapGwei: 200,
					height:        10,
					data:          redeem0Data,
				},
			},
			swapMap: map[[32]byte]*dexeth.SwapState{
				secretHashes[0]: {
					State: dexeth.SSInitiated,
				},
			},
			monitoredTxs: map[common.Hash]*monitoredTx{
				(*toEthTxHash(3, 200, redeem0Data)): {
					tx:             toEthTx(3, 200, redeem0Data),
					blockSubmitted: 9,
				},
			},
			bestBlock:  13,
			bestHdrErr: errors.New(""),
			expectErr:  true,
			baseFee:    dexeth.GweiToWei(100),
		},
	}

	for _, test := range tests {
		fmt.Printf("###### %s ###### \n", test.name)
		node.getTxResMap = make(map[common.Hash]*tGetTxRes)
		for hash, txData := range test.getTxResMap {
			node.getTxResMap[hash] = &tGetTxRes{
				tx:     toEthTx(txData.nonce, txData.gasFeeCapGwei, txData.data),
				height: txData.height,
			}
		}
		for _, s := range test.swapMap {
			s.Value = big.NewInt(1)
		}

		node.tContractor.swapMap = test.swapMap
		node.tContractor.redeemTx = test.redeemTx
		node.tContractor.lastRedeems = nil
		node.tokenContractor.bal = big.NewInt(1e9)
		node.bal = big.NewInt(1e9)

		node.lastSignedTx = nil

		node.baseFee = test.baseFee
		node.netFeeStateErr = test.getFeeErr
		node.confNonce = test.confNonce
		node.confNonceErr = test.confNonceErr
		node.bestHdr = &types.Header{Number: big.NewInt(test.bestBlock)}
		node.bestHdrErr = test.bestHdrErr
		node.receipt = test.receipt
		node.receiptErr = test.receiptErr

		eth.monitoredTxDB = kvdb.NewMemoryDB()
		eth.monitoredTxs = test.monitoredTxs
		for h, tx := range test.monitoredTxs {
			if err := eth.monitoredTxDB.Store(h[:], tx); err != nil {
				t.Fatalf("%s: error storing monitored tx: %v", test.name, err)
			}
		}

		result, err := wi.ConfirmRedemption(test.coinID, test.redemption, 0)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: expected but did not get", test.name)
			}
			if test.expectSwapRefundedErr && !errors.Is(asset.ErrSwapRefunded, err) {
				t.Fatalf("%s: expected swap refunded error but got %v", test.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error %v", test.name, err)
		}

		// Check that the correct swaps were resubmitted
		if test.expectedResubmittedRedemptions != nil {
			if len(test.expectedResubmittedRedemptions) != len(node.tContractor.lastRedeems) {
				t.Fatalf("%s expected %d redeems but got %d",
					test.name,
					len(test.expectedResubmittedRedemptions),
					len(node.tContractor.lastRedeems))
			}

			// The redemptions might be out of order, so we create this map.
			lastRedeems := make(map[string]*asset.Redemption)
			for _, redeem := range node.tContractor.lastRedeems {
				lastRedeems[fmt.Sprintf("%x", redeem.Spends.Contract)] = redeem
			}
			for i := range test.expectedResubmittedRedemptions {
				expected := test.expectedResubmittedRedemptions[i]
				actual, found := lastRedeems[fmt.Sprintf("%x", expected.Spends.Contract)]
				if !found {
					t.Fatalf("%s: expected contract not found among redemptions", test.name)
				}
				if !bytes.Equal(expected.Spends.Contract, actual.Spends.Contract) {
					t.Fatalf("%s: redeemed contract not as expected", test.name)
				}
				if !bytes.Equal(expected.Secret, actual.Secret) {
					t.Fatalf("%s: redeemed secret not as expected", test.name)
				}
			}
		}

		// If the transaction should be resubmitted unmodified, check that this
		// happened properly
		if test.expectSentSignedTransaction != nil {
			if test.expectSentSignedTransaction.Hash() != node.lastSignedTx.Hash() {
				t.Fatalf("%s expected sent signed tx %s != actual %s",
					test.name, test.expectSentSignedTransaction.Hash(), node.lastSignedTx.Hash())
			}
		}

		// Check that the monitoredTxs were updated properly
		monitoredTxsMatch := func(a, b *monitoredTx) bool {
			if a.blockSubmitted != b.blockSubmitted {
				return false
			} else if a.tx.Hash() != b.tx.Hash() {
				return false
			} else if a.tx.Hash() != b.tx.Hash() {
				return false
			} else if (a.replacementTx == nil) != (b.replacementTx == nil) {
				return false
			} else if a.replacementTx != nil && *a.replacementTx != *b.replacementTx {
				return false
			}
			return true
		}
		storedTxs, err := loadMonitoredTxs(eth.monitoredTxDB)
		if err != nil {
			t.Fatalf("%s: failed to load stored txs", test.name)
		}

		// We do not check the length of the in memory map because that will be cleared later
		if len(storedTxs) != len(test.expectedMonitoredTxs) {
			t.Fatalf("expected %d monitored txs to be stored but got %d", len(test.expectedMonitoredTxs), len(storedTxs))
		}

		for hash, expected := range test.expectedMonitoredTxs {
			actual, found := eth.monitoredTxs[hash]
			if !found {
				t.Fatalf("%s: expected monitored tx not found among monitored txs", test.name)
			}
			if !monitoredTxsMatch(expected, actual) {
				t.Fatalf("%s: expected monitored tx %+v != actual %+v", test.name, expected, actual)
			}

			stored, found := storedTxs[hash]
			if !found {
				t.Fatalf("%s: expected monitored tx not found among stored txs", test.name)
			}
			if !monitoredTxsMatch(expected, stored) {
				t.Fatalf("%s: expected monitored tx %+v != stored %+v", test.name, expected, stored)
			}
		}

		// Check that the resulting status is as expected
		if !bytes.Equal(test.expectedResult.CoinID, result.CoinID) ||
			test.expectedResult.Confs != result.Confs ||
			test.expectedResult.Req != result.Req {
			t.Fatalf("%s: expected result %+v != result %+v", test.name, test.expectedResult, result)
		}
	}
}

func TestMarshalMonitoredTx(t *testing.T) {
	var replacementTxHash common.Hash
	copy(replacementTxHash[:], encode.RandomBytes(32))

	original := &monitoredTx{
		tx:             tTx(100, 200, 300, &testAddressA, []byte{}),
		blockSubmitted: 123,
		replacementTx:  &replacementTxHash,
	}

	originalB, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("error marshaling monitored tx: %v", err)
	}

	var unmarshaledMonitoredTx monitoredTx
	err = unmarshaledMonitoredTx.UnmarshalBinary(originalB)
	if err != nil {
		t.Fatalf("error unmarshalling monitored tx: %v", err)
	}

	if original.tx.Hash() != unmarshaledMonitoredTx.tx.Hash() ||
		original.blockSubmitted != unmarshaledMonitoredTx.blockSubmitted ||
		*original.replacementTx != *unmarshaledMonitoredTx.replacementTx {
		t.Fatalf("incorrectly unmarshalled")
	}

	originalNoReplacement := &monitoredTx{
		tx:             tTx(100, 200, 300, &testAddressA, []byte{}),
		blockSubmitted: 123,
	}

	noReplacementB, err := originalNoReplacement.MarshalBinary()
	if err != nil {
		t.Fatalf("error marshaling monitored tx: %v", err)
	}

	var unmarshalledNoReplacement monitoredTx
	err = unmarshalledNoReplacement.UnmarshalBinary(noReplacementB)
	if err != nil {
		t.Fatalf("error unmarshalling monitored tx: %v", err)
	}

	if originalNoReplacement.tx.Hash() != unmarshalledNoReplacement.tx.Hash() ||
		originalNoReplacement.blockSubmitted != unmarshalledNoReplacement.blockSubmitted ||
		originalNoReplacement.replacementTx != unmarshalledNoReplacement.replacementTx {
		t.Fatalf("incorrectly unmarshalled")
	}
}

// Ensures that a small rise in the base fee between estimation
// and sending will not cause a failure.
func TestEstimateVsActualSendFees(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testEstimateVsActualSendFees(t, BipID) })
	t.Run("token", func(t *testing.T) { testEstimateVsActualSendFees(t, simnetTokenID) })
}

func testEstimateVsActualSendFees(t *testing.T, assetID uint32) {
	w, _, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	tx := tTx(0, 0, 0, &testAddressA, nil)
	node.sendTxTx = tx
	node.tokenContractor.transferTx = tx

	const testAddr = "dd93b447f7eBCA361805eBe056259853F3912E04"

	txFeeEstimator := w.(asset.TxFeeEstimator)
	fee, _, err := txFeeEstimator.EstimateSendTxFee("", 0, 0, false)
	if err != nil {
		t.Fatalf("error estimating fee: %v", err)
	}

	// Increase the base fee by 10%.
	node.baseFee = node.baseFee.Mul(node.baseFee, big.NewInt(11))
	node.baseFee = node.baseFee.Div(node.baseFee, big.NewInt(10))

	if assetID == BipID {
		node.bal = dexeth.GweiToWei(11e9)
		canSend := new(big.Int).Sub(node.bal, dexeth.GweiToWei(fee))
		canSendGwei, err := dexeth.WeiToGweiUint64(canSend)
		if err != nil {
			t.Fatalf("error converting canSend to gwei: %v", err)
		}
		_, err = w.Send(testAddr, canSendGwei, 0)
		if err != nil {
			t.Fatalf("error sending: %v", err)
		}
	} else {
		tokenVal := uint64(10e9)
		node.tokenContractor.bal = dexeth.GweiToWei(tokenVal)
		node.bal = dexeth.GweiToWei(fee)
		_, err = w.Send(testAddr, tokenVal, 0)
		if err != nil {
			t.Fatalf("error sending: %v", err)
		}
	}
}

func TestEstimateSendTxFee(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testEstimateSendTxFee(t, BipID) })
	t.Run("token", func(t *testing.T) { testEstimateSendTxFee(t, simnetTokenID) })
}

func testEstimateSendTxFee(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	maxFeeRate, _ := eth.recommendedMaxFeeRate(eth.ctx)
	ethFees := dexeth.WeiToGwei(maxFeeRate) * defaultSendGasLimit
	tokenFees := dexeth.WeiToGwei(maxFeeRate) * tokenGases.Transfer

	ethFees = ethFees * 12 / 10
	tokenFees = tokenFees * 12 / 10

	const testAddr = "dd93b447f7eBCA361805eBe056259853F3912E04"

	const val = 10e9
	tests := []struct {
		name, addr      string
		sendAdj, feeAdj uint64
		balErr          error
		withdraw        bool
		wantErr         bool
	}{{
		name: "ok",
		addr: testAddr,
	}, {
		name: "ok: empty address",
		addr: "",
	}, {
		name:    "not enough",
		sendAdj: 1,
		wantErr: true,
		addr:    testAddr,
	}, {
		name:    "low fees",
		feeAdj:  1,
		wantErr: true,
		addr:    testAddr,
	}, {
		name:     "subtract",
		feeAdj:   1,
		withdraw: true,
		wantErr:  true,
		addr:     testAddr,
	}, {
		name:    "balance error",
		balErr:  errors.New(""),
		wantErr: true,
		addr:    testAddr,
	}}

	for _, test := range tests {
		node.balErr = test.balErr
		if assetID == BipID {
			node.bal = dexeth.GweiToWei(val + ethFees - test.sendAdj - test.feeAdj)
		} else {
			node.tokenContractor.bal = dexeth.GweiToWei(val - test.sendAdj)
			node.bal = dexeth.GweiToWei(tokenFees - test.feeAdj)
		}
		txFeeEstimator := w.(asset.TxFeeEstimator)
		estimate, _, err := txFeeEstimator.EstimateSendTxFee(test.addr, val, 0, false)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if assetID == BipID {
			if estimate != ethFees {
				t.Fatalf("%s: expected fees to be %v, got %v", test.name, ethFees, estimate)
			}
		} else {
			if estimate != tokenFees {
				t.Fatalf("%s: expected fees to be %v, got %v", test.name, tokenFees, estimate)
			}
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
	}
}

// This test will fail if new versions of the eth or the test token
// contract (that require more gas) are added.
func TestMaxSwapRedeemLots(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testMaxSwapRedeemLots(t, BipID) })
	t.Run("token", func(t *testing.T) { testMaxSwapRedeemLots(t, simnetTokenID) })
}

func testMaxSwapRedeemLots(t *testing.T, assetID uint32) {
	drv := &Driver{}
	logger := dex.StdOutLogger("ETHTEST", dex.LevelOff)
	tmpDir := t.TempDir()

	settings := map[string]string{providersKey: "a.ipc"}
	err := CreateEVMWallet(dexeth.ChainIDs[dex.Testnet], &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     encode.RandomBytes(32),
		Pass:     encode.RandomBytes(32),
		Settings: settings,
		DataDir:  tmpDir,
		Net:      dex.Testnet,
		Logger:   logger,
	}, &testnetCompatibilityData, true)
	if err != nil {
		t.Fatalf("CreateEVMWallet error: %v", err)
	}

	wallet, err := drv.Open(&asset.WalletConfig{
		Type:     walletTypeRPC,
		Settings: settings,
		DataDir:  tmpDir,
	}, logger, dex.Testnet)
	if err != nil {
		t.Fatalf("driver open error: %v", err)
	}

	if assetID != BipID {
		eth, _ := wallet.(*ETHWallet)
		eth.net = dex.Simnet
		wallet, err = eth.OpenTokenWallet(&asset.TokenConfig{
			AssetID: assetID,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	info := wallet.Info()
	if assetID == BipID {
		if info.MaxSwapsInTx != 28 {
			t.Fatalf("expected 28 for max swaps but got %d", info.MaxSwapsInTx)
		}
		if info.MaxRedeemsInTx != 63 {
			t.Fatalf("expected 63 for max redemptions but got %d", info.MaxRedeemsInTx)
		}
	} else {
		if info.MaxSwapsInTx != 28 {
			t.Fatalf("expected 28 for max swaps but got %d", info.MaxSwapsInTx)
		}
		if info.MaxRedeemsInTx != 71 {
			t.Fatalf("expected 71 for max redemptions but got %d", info.MaxRedeemsInTx)
		}
	}
}

func TestSwapOrRedemptionFeesPaid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	bw := &baseWallet{node: node}
	coinID, secretHA, secretHB := encode.RandomBytes(32), encode.RandomBytes(32), encode.RandomBytes(32)
	contractDataFn := func(ver uint32, secretH []byte) []byte {
		s := [32]byte{}
		copy(s[:], secretH)
		return dexeth.EncodeContractData(ver, s)
	}
	rcpt := &types.Receipt{
		GasUsed: 100,
	}
	hdr := &types.Header{
		BaseFee: dexeth.GweiToWei(2),
		Number:  big.NewInt(1),
	}
	hdrConfirms := &types.Header{Number: big.NewInt(11)}
	initFn := func(secretHs [][]byte) []byte {
		inits := make([]*dexeth.Initiation, 0, len(secretHs))
		for i := range secretHs {
			s := [32]byte{}
			copy(s[:], secretHs[i])
			init := &dexeth.Initiation{
				SecretHash: s,
				Value:      big.NewInt(0),
			}
			inits = append(inits, init)
		}
		data, err := packInitiateDataV0(inits)
		if err != nil {
			t.Fatalf("problem packing inits: %v", err)
		}
		return data
	}
	redeemFn := func(secretHs [][]byte) []byte {
		redeems := make([]*dexeth.Redemption, 0, len(secretHs))
		for i := range secretHs {
			s := [32]byte{}
			copy(s[:], secretHs[i])
			redeem := &dexeth.Redemption{
				SecretHash: s,
			}
			redeems = append(redeems, redeem)
		}
		data, err := packRedeemDataV0(redeems)
		if err != nil {
			t.Fatalf("problem packing redeems: %v", err)
		}
		return data
	}
	abFn := func() [][]byte {
		return [][]byte{secretHA, secretHB}
	}
	sortedFn := func() [][]byte {
		ab := abFn()
		sort.Slice(ab, func(i, j int) bool { return bytes.Compare(ab[i], ab[j]) < 0 })
		return ab
	}
	tests := []struct {
		name                   string
		coinID, contractData   []byte
		isInit, wantErr        bool
		receipt                *types.Receipt
		receiptTx              *types.Transaction
		receiptErr, bestHdrErr error
		hdrByHash, bestHdr     *types.Header
		wantSecrets            [][]byte
		wantFee                uint64
	}{{
		name:         "ok init",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		isInit:       true,
		receipt:      rcpt,
		receiptTx:    tTx(200, 2, 0, nil, initFn(abFn())),
		hdrByHash:    hdr,
		bestHdr:      hdrConfirms,
		wantSecrets:  sortedFn(),
		wantFee:      400,
	}, {
		name:         "ok redeem",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHB),
		receipt:      rcpt,
		receiptTx:    tTx(200, 3, 0, nil, redeemFn(abFn())),
		hdrByHash:    hdr,
		bestHdr:      hdrConfirms,
		wantSecrets:  sortedFn(),
		wantFee:      500,
	}, {
		name:         "bad contract data",
		coinID:       coinID,
		contractData: nil,
		wantErr:      true,
	}, {
		name:         "receipt error",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		receiptErr:   errors.New(""),
		wantErr:      true,
	}, {
		name:         "nil header",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		receipt:      rcpt,
		receiptTx:    tTx(200, 2, 0, nil, initFn(abFn())),
		bestHdr:      hdrConfirms,
		wantErr:      true,
	}, {
		name:         "best header error",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		receipt:      rcpt,
		hdrByHash:    hdr,
		bestHdrErr:   errors.New(""),
		wantErr:      true,
	}, {
		name:         "not enough confirms",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		receipt:      rcpt,
		hdrByHash:    hdr,
		bestHdr:      hdr,
		wantErr:      true,
	}, {
		name:         "bad init data",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		isInit:       true,
		receipt:      rcpt,
		receiptTx:    tTx(200, 2, 0, nil, nil),
		hdrByHash:    hdr,
		bestHdr:      hdrConfirms,
		wantErr:      true,
	}, {
		name:         "bad redeem data",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHA),
		receipt:      rcpt,
		receiptTx:    tTx(200, 2, 0, nil, nil),
		hdrByHash:    hdr,
		bestHdr:      hdrConfirms,
		wantErr:      true,
	}, {
		name:         "secret hash not found",
		coinID:       coinID,
		contractData: contractDataFn(0, secretHB),
		isInit:       true,
		receipt:      rcpt,
		receiptTx:    tTx(200, 2, 0, nil, initFn([][]byte{secretHA})),
		hdrByHash:    hdr,
		bestHdr:      hdrConfirms,
		wantErr:      true,
	}}
	for _, test := range tests {
		node.receipt = test.receipt
		node.receiptTx = test.receiptTx
		node.receiptErr = test.receiptErr
		node.hdrByHash = test.hdrByHash
		node.bestHdr = test.bestHdr
		node.bestHdrErr = test.bestHdrErr
		fee, secretHs, err := bw.swapOrRedemptionFeesPaid(ctx, test.coinID, test.contractData, test.isInit)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", test.name, err)
		}
		if test.wantFee != fee {
			t.Fatalf("%q: wanted fee %d but got %d", test.name, test.wantFee, fee)
		}
		if len(test.wantSecrets) != len(secretHs) {
			t.Fatalf("%q: wanted %d secrets but got %d", test.name, len(test.wantSecrets), len(secretHs))
		}
		for i := range test.wantSecrets {
			sGot := secretHs[i]
			sWant := test.wantSecrets[i]
			if !bytes.Equal(sGot, sWant) {
				t.Fatalf("%q: wanted secret %x but got %x at position %d", test.name, sWant, sGot, i)
			}
		}
	}
}

func TestReceiptCache(t *testing.T) {
	m := &multiRPCClient{}
	c := make(map[common.Hash]*receiptRecord)
	m.receipts.cache = c

	r := &receiptRecord{
		r: &types.Receipt{
			Type: 50,
		},
		lastAccess: time.Now(),
	}

	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))
	c[txHash] = r

	if r := m.cachedReceipt(txHash); r == nil {
		t.Fatalf("cached receipt not returned")
	}

	r.lastAccess = time.Now().Add(-(unconfirmedReceiptExpiration + 1))
	if r := m.cachedReceipt(txHash); r != nil {
		t.Fatalf("expired receipt returned")
	}

	// The receipt still hasn't been pruned.
	if len(c) != 1 {
		t.Fatalf("receipt was pruned?")
	}

	// An if it was confirmed, it would be returned.
	r.confirmed = true
	if r := m.cachedReceipt(txHash); r == nil {
		t.Fatalf("confirmed receipt not returned")
	}

	r.lastAccess = time.Now().Add(-(receiptCacheExpiration + 1))
	m.receipts.lastClean = time.Time{}
	m.cachedReceipt(common.Hash{})
	// The receipt still hasn't been pruned.
	if len(c) != 0 {
		t.Fatalf("receipt wasn't pruned")
	}

}

func TestUnusedNonce(t *testing.T) {
	mRPC := new(multiRPCClient)
	tests := []struct {
		name  string
		nonce uint64
		want  bool
		wait  bool
	}{{
		name:  "ok initiation",
		nonce: 0,
		want:  true,
	}, {
		name:  "ok larger",
		nonce: 1,
		want:  true,
	}, {
		name:  "same nonce",
		nonce: 1,
		// Uncomment for full tests.
		// }, {
		// 	name:  "ok after expiration",
		// 	nonce: 1,
		// 	wait:  true,
		// 	want:  true,
	}}
	for _, test := range tests {
		if test.wait {
			time.Sleep(time.Minute + time.Second)
		}
		got := mRPC.registerNonce(test.nonce)
		if test.want != got {
			t.Fatalf("%q: wanted %v got %v", test.name, test.want, got)
		}
	}
}

func TestFreshProviderList(t *testing.T) {

	tests := []struct {
		times    []int64
		expOrder []int
	}{
		{
			times:    []int64{1, 2, 3},
			expOrder: []int{0, 1, 2},
		},
		{
			times:    []int64{3, 2, 1},
			expOrder: []int{2, 1, 0},
		},
		{
			times:    []int64{1, 5, 4, 2, 3},
			expOrder: []int{0, 3, 4, 2, 1},
		},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test#%d", i), func(t *testing.T) {
			node := &multiRPCClient{providers: make([]*provider, len(tt.times))}
			for i, stamp := range tt.times {
				p := &provider{}
				p.tip.headerStamp = time.Unix(stamp, 0)
				p.tip.failCount = i // hi-jacking field for initial sort order
				node.providers[i] = p
			}
			providers := node.freshnessSortedProviders()
			for i, p := range providers {
				if p.tip.failCount != tt.expOrder[i] {
					t.Fatalf("%d'th provider in sorted list is unexpected", i)
				}
			}
		})
	}
}

func TestDomain(t *testing.T) {
	tests := []struct {
		addr       string
		wantDomain string
		wantErr    bool
	}{
		{
			addr:       "http://www.place.io/v3/234234wfsefe",
			wantDomain: "place.io",
		},
		{
			addr:       "wss://www.place.nz/stuff?=token",
			wantDomain: "place.nz",
		},
		{
			addr:       "http://en.us.lotssubdomains.place.io/v3/234234wfsefe",
			wantDomain: "place.io",
		},
		{
			addr:       "https://www.place.co.uk:443/blog/article/search?docid=720&hl=en#dayone",
			wantDomain: "place.co.uk:443",
		},
		{
			addr:       "wmba://www.place.com",
			wantDomain: "place.com",
		},
		{
			addr:       "ws://127.0.0.1:3000",
			wantDomain: "127.0.0.1:3000",
		},
		{
			addr:       "ws://localhost:3000",
			wantDomain: "localhost:3000",
		},
		{
			addr:       "http://123.123.123.123:3000",
			wantDomain: "123.123.123.123:3000",
		},
		{
			addr:       "https://123.123.123.123",
			wantDomain: "123.123.123.123",
		},
		{
			addr:       "ws://[abab:fde3:0:0:0:0:0:1]:8080",
			wantDomain: "[abab:fde3:0:0:0:0:0:1]:8080",
		},
		{
			addr:       "ws://[::1]:8000",
			wantDomain: "[::1]:8000",
		},
		{
			addr:       "[::1]:8000",
			wantDomain: "[::1]:8000",
		},
		{
			addr:       "[::1]",
			wantDomain: "[::1]",
		},
		{
			addr:       "127.0.0.1",
			wantDomain: "127.0.0.1",
		},
		{
			addr:       "/home/john/.geth/geth.ipc",
			wantDomain: "/home/john/.geth/geth.ipc",
		},
		{
			addr:       "/home/john/.geth/geth",
			wantDomain: "/home/john/.geth/geth",
		},
		{
			addr:    "https://\n:1234",
			wantErr: true,
		},
		{
			addr:    ":asdf",
			wantErr: true,
		},
		{
			wantErr: true,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("test#%d", i), func(t *testing.T) {
			d, err := domain(test.addr)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantDomain != d {
				t.Fatalf("wanted domain %s but got %s", test.wantDomain, d)
			}
		})
	}
}

func parseRecoveryID(c asset.Coin) []byte {
	return c.(asset.RecoveryCoin).RecoveryID()
}

func randomHash() common.Hash {
	return common.BytesToHash(encode.RandomBytes(20))
}
