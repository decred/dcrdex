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

const (
	txConfsNeededToConfirm = 3
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)

	testAddressA = common.HexToAddress("dd93b447f7eBCA361805eBe056259853F3912E04")
	testAddressB = common.HexToAddress("8d83B207674bfd53B418a6E47DA148F5bFeCc652")
	testAddressC = common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")

	ethGasesV0   = dexeth.VersionedGases[0]
	tokenGasesV0 = dexeth.Tokens[usdcEthID].NetTokens[dex.Simnet].SwapContracts[0].Gas
	ethGasesV1   = dexeth.VersionedGases[1]
	tokenGasesV1 = dexeth.Tokens[usdcEthID].NetTokens[dex.Simnet].SwapContracts[1].Gas

	tETHV0 = &dex.Asset{
		Version:    0,
		ID:         60,
		Symbol:     "ETH",
		MaxFeeRate: 100,
		SwapConf:   1,
	}

	tETHV1 = &dex.Asset{
		Version:    1,
		ID:         60,
		Symbol:     "ETH",
		MaxFeeRate: 100,
		SwapConf:   1,
	}

	tBTC = &dex.Asset{
		ID:         0,
		Symbol:     "btc",
		Version:    0, // match btc.version
		MaxFeeRate: 10,
		SwapConf:   1,
	}

	tTokenV0 = &dex.Asset{
		ID:         usdcEthID,
		Symbol:     "usdc.eth",
		Version:    0,
		MaxFeeRate: 20,
		SwapConf:   1,
	}

	tTokenV1 = &dex.Asset{
		ID:         usdcEthID,
		Symbol:     "dextt.eth",
		Version:    1,
		MaxFeeRate: 20,
		SwapConf:   1,
	}

	signer = types.LatestSigner(params.AllEthashProtocolChanges)

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
	receipts        map[common.Hash]*types.Receipt
	receiptTxs      map[common.Hash]*types.Transaction
	receiptErrs     map[common.Hash]error
	hdrByHash       *types.Header
	lastSignedTx    *types.Transaction
	sentTxs         int
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

func (n *testNode) newTransaction(nonce uint64, value *big.Int) *types.Transaction {
	to := common.BytesToAddress(encode.RandomBytes(20))
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		Nonce:   nonce,
		Value:   value,
		Gas:     50_000,
		To:      &to,
		ChainID: n.chainConfig().ChainID,
	}), signer, n.privKey)
	if err != nil {
		panic("tx signing error")
	}
	return tx
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

func (n *testNode) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, tipRate, nonce *big.Int) (*bind.TransactOpts, error) {
	if maxFeeRate == nil {
		maxFeeRate = n.maxFeeRate
	}
	txOpts := newTxOpts(ctx, n.addr, val, maxGas, maxFeeRate, dexeth.GweiToWei(2))
	txOpts.Nonce = big.NewInt(1)
	return txOpts, nil
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
func (n *testNode) sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte, filts ...acceptabilityFilter) (*types.Transaction, error) {
	n.sentTxs++
	return n.sendTxTx, n.sendTxErr
}
func (n *testNode) sendSignedTransaction(ctx context.Context, tx *types.Transaction, filts ...acceptabilityFilter) error {
	n.lastSignedTx = tx
	return nil
}

func tTx(gasFeeCap, gasTipCap, value uint64, to *common.Address, data []byte, gasLimit uint64) *types.Transaction {
	return types.NewTx(&types.DynamicFeeTx{
		GasFeeCap: dexeth.GweiToWei(gasFeeCap),
		GasTipCap: dexeth.GweiToWei(gasTipCap),
		To:        to,
		Value:     dexeth.GweiToWei(value),
		Data:      data,
		Gas:       gasLimit,
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

func (n *testNode) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if n.receiptErr != nil {
		return nil, n.receiptErr
	}
	if n.receipt != nil {
		return n.receipt, nil
	}

	return n.receipts[txHash], n.receiptErrs[txHash]
}

func (n *testNode) transactionAndReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error) {
	if n.receiptErr != nil {
		return nil, nil, n.receiptErr
	}
	if n.receipt != nil {
		return n.receipt, n.receiptTx, nil
	}

	return n.receipts[txHash], n.receiptTxs[txHash], n.receiptErrs[txHash]
}

func (n *testNode) nonce(ctx context.Context) (*big.Int, *big.Int, error) {
	return big.NewInt(0), big.NewInt(1), nil
}

func (n *testNode) setBalanceError(w *assetWallet, err error) {
	n.balErr = err
	n.tokenContractor.balErr = err
	w.balances.m = nil
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
	redeemable        bool
	redeemableErr     error
	redeemableMap     map[string]bool
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

func (c *tContractor) status(ctx context.Context, locator []byte) (*dexeth.SwapStatus, error) {
	if c.swapErr != nil {
		return nil, c.swapErr
	}
	vector, err := c.vector(ctx, locator)
	if err != nil {
		return nil, err
	}
	swap, ok := c.swapMap[vector.SecretHash]
	if !ok {
		return nil, errors.New("swap not in map")
	}
	s := &dexeth.SwapStatus{
		Step:        swap.State,
		Secret:      swap.Secret,
		BlockHeight: swap.BlockHeight,
	}
	return s, nil
}

func (c *tContractor) vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error) {
	if c.swapErr != nil {
		return nil, c.swapErr
	}
	if len(locator) == dexeth.LocatorV1Length {
		return dexeth.ParseV1Locator(locator)
	}
	var secretHash [32]byte
	copy(secretHash[:], locator)
	swap, ok := c.swapMap[secretHash]
	if !ok {
		return nil, errors.New("swap not in map")
	}
	v := &dexeth.SwapVector{
		From:       swap.Initiator,
		To:         swap.Participant,
		Value:      swap.Value,
		SecretHash: secretHash,
		LockTime:   uint64(swap.LockTime.Unix()),
	}
	return v, nil
}

func (c *tContractor) statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error) {
	if c.swapErr != nil {
		return nil, nil, c.swapErr
	}
	vector, err := c.vector(ctx, locator)
	if err != nil {
		return nil, nil, err
	}
	swap, ok := c.swapMap[vector.SecretHash]
	if !ok {
		return nil, nil, errors.New("swap not in map")
	}
	v := &dexeth.SwapVector{
		From:       swap.Initiator,
		To:         swap.Participant,
		Value:      swap.Value,
		SecretHash: vector.SecretHash,
		LockTime:   uint64(swap.LockTime.Unix()),
	}
	s := &dexeth.SwapStatus{
		Step:        swap.State,
		Secret:      swap.Secret,
		BlockHeight: swap.BlockHeight,
	}
	return s, v, nil
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

func (c *tContractor) refund(opts *bind.TransactOpts, locator []byte) (*types.Transaction, error) {
	vector, err := c.vector(context.Background(), locator)
	if err != nil {
		return nil, err
	}
	c.lastRefund.secretHash = vector.SecretHash
	c.lastRefund.maxFeeRate = opts.GasFeeCap
	return c.refundTx, c.refundErr
}

func (c *tContractor) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	return c.gasEstimates.SwapN(n), c.initGasErr
}

func (c *tContractor) estimateRedeemGas(ctx context.Context, secrets [][32]byte, locators [][]byte) (uint64, error) {
	if c.redeemGasOverride != nil {
		return *c.redeemGasOverride, nil
	}
	return c.gasEstimates.RedeemN(len(secrets)), c.redeemGasErr
}

func (c *tContractor) estimateRefundGas(ctx context.Context, locator []byte) (uint64, error) {
	return c.gasEstimates.Refund, c.refundGasErr
}

func (c *tContractor) isRedeemable(locator []byte, secret [32]byte) (bool, error) {
	if c.redeemableErr != nil {
		return false, c.redeemableErr
	}

	vector, err := c.vector(context.Background(), locator)
	if err != nil {
		return false, err
	}

	if c.swapMap != nil && c.swapMap[vector.SecretHash] == nil {
		return false, fmt.Errorf("test error: no swap in swap map")
	}

	if c.redeemableMap != nil {
		return c.redeemableMap[string(locator)], nil
	}

	return c.redeemable, c.redeemableErr
}

func (c *tContractor) value(_ context.Context, tx *types.Transaction) (incoming, outgoing uint64, err error) {
	incoming, outgoing = c.valueIn[tx.Hash()], c.valueOut[tx.Hash()]
	if incoming > 0 {
		delete(c.valueIn, tx.Hash())
	}
	if outgoing > 0 {
		delete(c.valueOut, tx.Hash())
	}
	return incoming, outgoing, c.valueErr
}

func (c *tContractor) isRefundable(locator []byte) (bool, error) {
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

func (c *tTokenContractor) parseTransfer(*types.Receipt) (uint64, error) {
	return 0, nil
}

func (c *tTokenContractor) estimateTransferGas(context.Context, *big.Int) (uint64, error) {
	return c.transferEstimate, c.transferEstimateErr
}

type tTxDB struct {
	storeTxCalled  bool
	storedTx       *extendedWalletTx
	storeTxErr     error
	removeTxCalled bool
	removeTxErr    error
	txToGet        *extendedWalletTx
	getTxErr       error
	pendingBridges []*extendedWalletTx
}

var _ txDB = (*tTxDB)(nil)

func (db *tTxDB) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return &sync.WaitGroup{}, nil
}
func (db *tTxDB) storeTx(wt *extendedWalletTx) error {
	db.storeTxCalled = true
	db.storedTx = wt
	return db.storeTxErr
}
func (db *tTxDB) removeTx(_ /* id */ string) error {
	db.removeTxCalled = true
	return db.removeTxErr
}
func (db *tTxDB) getTxs(n int, refID *common.Hash, past bool, tokenID *uint32) ([]*asset.WalletTransaction, error) {
	panic("getTxs not implemented")
}

// getTx gets a single transaction. It is not an error if the tx is not known.
// In that case, a nil tx is returned.
func (db *tTxDB) getTx(txHash common.Hash) (tx *extendedWalletTx, _ error) {
	return db.txToGet, db.getTxErr
}
func (db *tTxDB) getPendingTxs() ([]*extendedWalletTx, error) {
	panic("getPendingTxs not implemented")
}
func (db *tTxDB) close() error {
	return nil
}
func (db *tTxDB) getBridges(n int, refID *common.Hash, past bool) ([]*asset.WalletTransaction, error) {
	panic("getBridges not implemented")
}
func (db *tTxDB) getPendingBridges() ([]*extendedWalletTx, error) {
	return db.pendingBridges, nil
}
func (db *tTxDB) getBridgeCompletion(initiationTxID string) (*extendedWalletTx, error) {
	return db.txToGet, db.getTxErr
}

// func TestCheckUnconfirmedTxs(t *testing.T) {
// 	const tipHeight = 50
// 	const baseFeeGwei = 100
// 	const gasTipCapGwei = 2

// 	type tExtendedWalletTx struct {
// 		wt           *extendedWalletTx
// 		confs        uint32
// 		gasUsed      uint64
// 		txReceiptErr error
// 	}

// 	newExtendedWalletTx := func(assetID uint32, nonce int64, maxFees uint64, currBlockNumber uint64, txReceiptConfs uint32,
// 		txReceiptGasUsed uint64, txReceiptErr error, timeStamp int64, savedToDB bool) *tExtendedWalletTx {
// 		var tokenID *uint32
// 		if assetID != BipID {
// 			tokenID = &assetID
// 		}

// 		return &tExtendedWalletTx{
// 			wt: &extendedWalletTx{
// 				WalletTransaction: &asset.WalletTransaction{
// 					BlockNumber: currBlockNumber,
// 					TokenID:     tokenID,
// 					Fees:        maxFees,
// 				},
// 				SubmissionTime: uint64(timeStamp),
// 				nonce:          big.NewInt(nonce),
// 				savedToDB:      savedToDB,
// 			},
// 			confs:        txReceiptConfs,
// 			gasUsed:      txReceiptGasUsed,
// 			txReceiptErr: txReceiptErr,
// 		}
// 	}

// 	gasFee := func(gasUsed uint64) uint64 {
// 		return gasUsed * (baseFeeGwei + gasTipCapGwei)
// 	}

// 	now := time.Now().Unix()

// 	tests := []struct {
// 		name           string
// 		assetID        uint32
// 		unconfirmedTxs []*tExtendedWalletTx
// 		confirmedNonce uint64

// 		expTxsAfter       []*extendedWalletTx
// 		expStoreTxCalled  bool
// 		expRemoveTxCalled bool
// 		storeTxErr        error
// 		removeTxErr       error
// 	}{
// 		{
// 			name:    "coin not found",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 0, 0, asset.CoinNotFoundError, now, true),
// 			},
// 			expTxsAfter: []*extendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 0, 0, asset.CoinNotFoundError, now, true).wt,
// 			},
// 		},
// 		{
// 			name:    "still in mempool",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 0, 0, nil, now, true),
// 			},
// 			expTxsAfter: []*extendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 0, 0, nil, now, true).wt,
// 			},
// 		},
// 		{
// 			name:    "1 confirmation",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 1, 6e5, nil, now, true),
// 			},
// 			expTxsAfter: []*extendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, gasFee(6e5), tipHeight, 0, 0, nil, now, true).wt,
// 			},
// 			expStoreTxCalled: true,
// 		},
// 		{
// 			name:    "3 confirmations",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 1, gasFee(6e5), tipHeight, 3, 6e5, nil, now, true),
// 			},
// 			expTxsAfter:      []*extendedWalletTx{},
// 			expStoreTxCalled: true,
// 		},
// 		{
// 			name:    "3 confirmations, leave in unconfirmed txs if txDB.storeTx fails",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, gasFee(6e5), tipHeight-2, 3, 6e5, nil, now, true),
// 			},
// 			expTxsAfter: []*extendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, gasFee(6e5), tipHeight-2, 3, 6e5, nil, now, true).wt,
// 			},
// 			expStoreTxCalled: true,
// 			storeTxErr:       errors.New("test error"),
// 		},
// 		{
// 			name:    "was confirmed but now not found",
// 			assetID: BipID,
// 			unconfirmedTxs: []*tExtendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, tipHeight-1, 0, 0, asset.CoinNotFoundError, now, true),
// 			},
// 			expTxsAfter: []*extendedWalletTx{
// 				newExtendedWalletTx(BipID, 5, 1e7, 0, 0, 0, asset.CoinNotFoundError, now, true).wt,
// 			},
// 			expStoreTxCalled: true,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			_, eth, node, shutdown := tassetWallet(tt.assetID)
// 			defer shutdown()

// 			node.tokenContractor.bal = unlimitedAllowance
// 			node.receipts = make(map[common.Hash]*types.Receipt)
// 			node.receiptTxs = make(map[common.Hash]*types.Transaction)
// 			node.receiptErrs = make(map[common.Hash]error)
// 			node.hdrByHash = &types.Header{
// 				BaseFee: dexeth.GweiToWei(baseFeeGwei),
// 			}
// 			node.confNonce = tt.confirmedNonce
// 			eth.connected.Store(true)
// 			eth.tipMtx.Lock()
// 			eth.currentTip = &types.Header{Number: new(big.Int).SetUint64(tipHeight)}
// 			eth.tipMtx.Unlock()

// 			txDB := &tTxDB{
// 				storeTxErr:  tt.storeTxErr,
// 				removeTxErr: tt.removeTxErr,
// 			}
// 			eth.txDB = txDB

// 			for _, pt := range tt.unconfirmedTxs {
// 				txHash := common.BytesToHash(encode.RandomBytes(32))
// 				pt.wt.ID = txHash.String()
// 				pt.wt.txHash = txHash
// 				eth.pendingTxs = append(eth.pendingTxs, pt.wt)
// 				var blockNumber *big.Int
// 				if pt.confs > 0 {
// 					blockNumber = big.NewInt(int64(tipHeight - pt.confs + 1))
// 				}
// 				node.receipts[txHash] = &types.Receipt{BlockNumber: blockNumber, GasUsed: pt.gasUsed}

// 				node.receiptTxs[txHash] = types.NewTx(&types.DynamicFeeTx{
// 					GasTipCap: dexeth.GweiToWei(gasTipCapGwei),
// 					GasFeeCap: dexeth.GweiToWei(2 * baseFeeGwei),
// 				})
// 				node.receiptErrs[txHash] = pt.txReceiptErr
// 			}

// 			eth.checkPendingTxs()

// 			if len(eth.pendingTxs) != len(tt.expTxsAfter) {
// 				t.Fatalf("expected %d unconfirmed txs, got %d", len(tt.expTxsAfter), len(eth.pendingTxs))
// 			}
// 			for i, expTx := range tt.expTxsAfter {
// 				tx := eth.pendingTxs[i]
// 				if expTx.nonce.Cmp(tx.nonce) != 0 {
// 					t.Fatalf("expected tx index %d to have nonce %d, not %d", i, expTx.nonce, tx.nonce)
// 				}
// 			}

// 			if txDB.storeTxCalled != tt.expStoreTxCalled {
// 				t.Fatalf("expected storeTx called %v, got %v", tt.expStoreTxCalled, txDB.storeTxCalled)
// 			}
// 			if txDB.removeTxCalled != tt.expRemoveTxCalled {
// 				t.Fatalf("expected removeTx called %v, got %v", tt.expRemoveTxCalled, txDB.removeTxCalled)
// 			}
// 		})
// 	}
// }

func TestCheckPendingTxs(t *testing.T) {
	_, eth, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	const tip = 12552
	const finalized = tip - txConfsNeededToConfirm + 1
	now := uint64(time.Now().Unix())
	finalizedStamp := now - txConfsNeededToConfirm*10
	rebroadcastable := now - 300
	mature := now - 600 // able to send actions
	agedOut := now - uint64(txAgeOut.Seconds()) - 1

	val := dexeth.GweiToWei(1)
	extendedTx := func(nonce, blockNum, blockStamp, submissionStamp uint64) *extendedWalletTx {
		pendingTx := eth.extendedTx(&genTxResult{
			tx:     node.newTransaction(nonce, val),
			txType: asset.Send,
			amt:    1,
		})
		pendingTx.BlockNumber = blockNum
		pendingTx.Confirmed = blockNum > 0 && blockNum <= finalized
		pendingTx.Timestamp = blockStamp
		pendingTx.SubmissionTime = submissionStamp
		pendingTx.lastBroadcast = time.Unix(int64(submissionStamp), 0)
		pendingTx.lastFeeCheck = time.Unix(int64(submissionStamp), 0)
		return pendingTx
	}

	const initiationTxID = "initiationTx"
	updateToBridgeCompletion := func(tx *extendedWalletTx) *extendedWalletTx {
		tx.Type = asset.CompleteBridge
		tx.BridgeCounterpartTx = &asset.BridgeCounterpartTx{
			ID:      initiationTxID,
			AssetID: BipID,
		}
		return tx
	}

	newReceipt := func(confs uint64) *types.Receipt {
		r := &types.Receipt{
			EffectiveGasPrice: big.NewInt(1),
		}
		if confs > 0 {
			r.BlockNumber = big.NewInt(int64(tip - confs + 1))
		}
		return r
	}

	emitChan := make(chan asset.WalletNotification, 128)
	eth.emit = asset.NewWalletEmitter(emitChan, BipID, eth.log)

	getAction := func(t *testing.T) string {
		for {
			select {
			case ni := <-emitChan:
				if n, ok := ni.(*asset.ActionRequiredNote); ok {
					return n.ActionID
				}
			default:
				t.Fatalf("no ActionRequiredNote found")
			}
		}
	}

	getBridgeCompleteNote := func(t *testing.T) *asset.BridgeCompletedNote {
		for {
			select {
			case ni := <-emitChan:
				if n, ok := ni.(*asset.BridgeCompletedNote); ok {
					return n
				}
			default:
				return nil
			}
		}
	}

	for _, tt := range []*struct {
		name        string
		dbErr       error
		pendingTxs  []*extendedWalletTx
		receipts    []*types.Receipt
		receiptErrs []error
		txs         []bool
		noncesAfter []uint64
		actionID    string
		bridgeDone  string
		recast      bool
	}{
		{
			name: "first of two is confirmed",
			pendingTxs: []*extendedWalletTx{
				extendedTx(0, finalized, finalizedStamp, finalizedStamp),
				extendedTx(1, finalized+1, finalizedStamp, finalizedStamp), // won't even be checked.
			},
			noncesAfter: []uint64{1},
		},
		{
			name: "second one is confirmed first",
			pendingTxs: []*extendedWalletTx{
				extendedTx(2, 0, 0, rebroadcastable),
				extendedTx(3, finalized, finalizedStamp, finalizedStamp),
			},
			noncesAfter: []uint64{2, 3},
			receipts:    []*types.Receipt{nil, nil},
			txs:         []bool{false, true},
			receiptErrs: []error{asset.CoinNotFoundError, nil},
			actionID:    actionTypeLostNonce,
		},
		{
			name: "confirm one with receipt",
			pendingTxs: []*extendedWalletTx{
				extendedTx(4, 0, 0, finalizedStamp),
			},
			receipts: []*types.Receipt{newReceipt(txConfsNeededToConfirm)},
		},
		{
			name: "bridge completion not yet confirmed",
			pendingTxs: []*extendedWalletTx{
				updateToBridgeCompletion(extendedTx(4, 0, 0, finalizedStamp)),
			},
			noncesAfter: []uint64{4},
			receipts:    []*types.Receipt{newReceipt(txConfsNeededToConfirm - 1)},
		},
		{
			name: "confirm bridge completion",
			pendingTxs: []*extendedWalletTx{
				updateToBridgeCompletion(extendedTx(4, 0, 0, finalizedStamp)),
			},
			receipts:   []*types.Receipt{newReceipt(txConfsNeededToConfirm)},
			bridgeDone: initiationTxID,
		},
		{
			name: "old and unindexed",
			pendingTxs: []*extendedWalletTx{
				extendedTx(5, 0, 0, agedOut),
			},
			noncesAfter: []uint64{5},
			receipts:    []*types.Receipt{nil},
			receiptErrs: []error{asset.CoinNotFoundError},
			txs:         []bool{false},
			actionID:    actionTypeLostNonce,
		},
		{
			name: "mature and indexed, low fees",
			pendingTxs: []*extendedWalletTx{
				extendedTx(6, 0, 0, mature),
			},
			noncesAfter: []uint64{6},
			receipts:    []*types.Receipt{newReceipt(0)},
			actionID:    actionTypeTooCheap,
		}, {
			name: "missing nonces",
			pendingTxs: []*extendedWalletTx{
				extendedTx(8, finalized, finalizedStamp, finalizedStamp),
				extendedTx(11, finalized+1, finalizedStamp, finalizedStamp),
			},
			noncesAfter: []uint64{11},
			actionID:    actionTypeMissingNonces,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eth.confirmedNonceAt = tt.pendingTxs[0].Nonce
			eth.pendingNonceAt = new(big.Int).Add(tt.pendingTxs[len(tt.pendingTxs)-1].Nonce, big.NewInt(1))

			node.lastSignedTx = nil
			eth.currentTip = &types.Header{Number: new(big.Int).SetUint64(tip)}
			eth.pendingTxs = tt.pendingTxs
			for i, r := range tt.receipts {
				pendingTx := tt.pendingTxs[i]
				if tt.receiptErrs != nil {
					node.receiptErrs[pendingTx.txHash] = tt.receiptErrs[i]
				}
				if r == nil {
					continue
				}
				node.receipts[pendingTx.txHash] = r
				if len(tt.txs) < i+1 || tt.txs[i] {
					node.receiptTxs[pendingTx.txHash], _ = pendingTx.tx()
				}
				if pendingTx.Timestamp == 0 && r.BlockNumber != nil && r.BlockNumber.Uint64() != 0 {
					node.hdrByHash = &types.Header{
						Number: r.BlockNumber,
						Time:   now,
					}
				}
			}
			eth.checkPendingTxs()
			if len(eth.pendingTxs) != len(tt.noncesAfter) {
				t.Fatalf("wrong number of pending txs. expected %d got %d", len(tt.noncesAfter), len(eth.pendingTxs))
			}
			for i, pendingTx := range eth.pendingTxs {
				if pendingTx.Nonce.Uint64() != tt.noncesAfter[i] {
					t.Fatalf("Expected nonce %d at index %d, but got nonce %s", tt.noncesAfter[i], i, pendingTx.Nonce)
				}
			}
			if tt.actionID != "" {
				if actionID := getAction(t); actionID != tt.actionID {
					t.Fatalf("expected action %s, got %s", tt.actionID, actionID)
				}
			}
			if tt.recast != (node.lastSignedTx != nil) {
				t.Fatalf("wrong recast result recast = %t, lastSignedTx = %t", tt.recast, node.lastSignedTx != nil)
			}
			if tt.bridgeDone != "" {
				if bridgeNote := getBridgeCompleteNote(t); bridgeNote == nil {
					t.Fatalf("expected bridge completion, got none")
				} else if bridgeNote.InitiationTxID != tt.bridgeDone {
					t.Fatalf("expected bridge completion for %s, got %s", tt.bridgeDone, bridgeNote.InitiationTxID)
				}
			} else {
				if bridgeNote := getBridgeCompleteNote(t); bridgeNote != nil {
					t.Fatalf("expected no bridge completion, got %s", bridgeNote.InitiationTxID)
				}
			}
		})
	}
}

func TestTakeAction(t *testing.T) {
	_, eth, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	aGwei := dexeth.GweiToWei(1)
	pendingTx := eth.extendedTx(&genTxResult{
		tx:     node.newTransaction(0, aGwei),
		txType: asset.Send,
		amt:    1,
	})
	eth.pendingTxs = []*extendedWalletTx{pendingTx}

	feeCap := new(big.Int).Mul(aGwei, big.NewInt(5))
	tipCap := new(big.Int).Mul(aGwei, big.NewInt(2))
	replacementTx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		Nonce:     1,
		GasTipCap: tipCap,
		GasFeeCap: feeCap,
		Gas:       50_000,
		ChainID:   node.chainConfig().ChainID,
	}), signer, node.privKey)
	node.sendTxTx = replacementTx

	tooCheapAction := []byte(fmt.Sprintf(`{"txID":"%s","bump":true}`, pendingTx.ID))
	if err := eth.TakeAction(actionTypeTooCheap, tooCheapAction); err != nil {
		t.Fatalf("TakeAction error: %v", err)
	}

	newPendingTx := eth.pendingTxs[0]
	if pendingTx.txHash == newPendingTx.txHash {
		t.Fatal("tx wasn't replaced")
	}
	tx, _ := newPendingTx.tx()
	if tx.GasFeeCap().Cmp(feeCap) != 0 {
		t.Fatalf("wrong fee cap. wanted %s, got %s", feeCap, tx.GasFeeCap())
	}
	if tx.GasTipCap().Cmp(tipCap) != 0 {
		t.Fatalf("wrong tip cap. wanted %s, got %s", tipCap, tx.GasTipCap())
	}
	if !newPendingTx.savedToDB {
		t.Fatal("didn't save to DB")
	}

	pendingTx = eth.extendedTx(&genTxResult{
		tx:     node.newTransaction(1, aGwei),
		txType: asset.Send,
		amt:    1,
	})
	eth.pendingTxs = []*extendedWalletTx{pendingTx}
	pendingTx.SubmissionTime = 0
	// Neglecting to bump should reset submission time.
	tooCheapAction = []byte(fmt.Sprintf(`{"txID":"%s","bump":false}`, pendingTx.ID))
	if err := eth.TakeAction(actionTypeTooCheap, tooCheapAction); err != nil {
		t.Fatalf("TakeAction bump=false error: %v", err)
	}
	tx, _ = pendingTx.tx()
	if tx.GasTipCap().Uint64() != 0 {
		t.Fatal("The fee was bumped. The fee shouldn't have been bumped.")
	}
	if pendingTx.actionIgnored.IsZero() {
		t.Fatalf("The ignore time wasn't reset")
	}
	if len(eth.pendingTxs) != 1 {
		t.Fatalf("Tx was removed")
	}

	// Nonce-replaced tx
	eth.pendingTxs = []*extendedWalletTx{pendingTx}
	lostNonceAction := []byte(fmt.Sprintf(`{"txID":"%s","abandon":true}`, pendingTx.ID))
	if err := eth.TakeAction(actionTypeLostNonce, lostNonceAction); err != nil {
		t.Fatalf("TakeAction replacment=false, abandon=true error: %v", err)
	}
	if len(eth.pendingTxs) != 0 {
		t.Fatalf("Tx wasn't abandoned")
	}
	eth.pendingTxs = []*extendedWalletTx{pendingTx}
	node.getTxRes = replacementTx
	lostNonceAction = []byte(fmt.Sprintf(`{"txID":"%s","abandon":false,"replacementID":"%s"}`, pendingTx.ID, replacementTx.Hash()))
	if err := eth.TakeAction(actionTypeLostNonce, lostNonceAction); err != nil {
		t.Fatalf("TakeAction replacment=true, error: %v", err)
	}
	newPendingTx = eth.pendingTxs[0]
	if newPendingTx.txHash != replacementTx.Hash() {
		t.Fatalf("replacement tx wasn't accepted")
	}
	// wrong nonce is an error though
	pendingTx = eth.extendedTx(&genTxResult{
		tx:     node.newTransaction(5050, aGwei),
		txType: asset.Send,
		amt:    1,
	})
	eth.pendingTxs = []*extendedWalletTx{pendingTx}
	lostNonceAction = []byte(fmt.Sprintf(`{"txID":"%s","abandon":false,"replacementID":"%s"}`, pendingTx.ID, replacementTx.Hash()))
	if err := eth.TakeAction(actionTypeLostNonce, lostNonceAction); err == nil {
		t.Fatalf("no error for wrong nonce")
	}

	// Missing nonces
	tx5 := eth.extendedTx(&genTxResult{
		tx:     node.newTransaction(5, aGwei),
		txType: asset.Send,
		amt:    1,
	})
	eth.pendingTxs = []*extendedWalletTx{tx5}
	eth.confirmedNonceAt = big.NewInt(2)
	eth.pendingNonceAt = big.NewInt(6)
	nonceRecoveryAction := []byte(`{"recover":true}`)
	node.sentTxs = 0
	if err := eth.TakeAction(actionTypeMissingNonces, nonceRecoveryAction); err != nil {
		t.Fatalf("error for nonce recover: %v", err)
	}
	if node.sentTxs != 3 {
		t.Fatalf("expected 2 new txs. saw %d", node.sentTxs)
	}

}

func TestCheckForNewBlocks(t *testing.T) {
	header0 := &types.Header{Number: new(big.Int)}
	header1 := &types.Header{Number: big.NewInt(1)}
	tests := []struct {
		name              string
		hashErr, blockErr error
		bestHeader        *types.Header
		hasTipChange      bool
	}{{
		name:         "ok",
		bestHeader:   header1,
		hasTipChange: true,
	}, {
		name:       "ok same hash",
		bestHeader: header0,
	}}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{}
		notes := make(chan asset.WalletNotification, 1)
		emit := asset.NewWalletEmitter(notes, BipID, tLogger)

		node.bestHdr = test.bestHeader
		node.bestHdrErr = test.blockErr
		w := &ETHWallet{
			assetWallet: &assetWallet{
				baseWallet: &baseWallet{
					node:             node,
					addr:             node.address(),
					ctx:              ctx,
					log:              tLogger,
					currentTip:       header0,
					confirmedNonceAt: new(big.Int),
					pendingNonceAt:   new(big.Int),
					txDB:             &tTxDB{},
					finalizeConfs:    txConfsNeededToConfirm,
				},
				versionedContracts: map[uint32]common.Address{
					0: {},
				},
				log:     tLogger.SubLogger("ETH"),
				emit:    emit,
				assetID: BipID,
			},
		}
		w.wallets = map[uint32]*assetWallet{BipID: w.assetWallet}
		w.assetWallet.connected.Store(true)
		w.checkForNewBlocks(ctx)

		if test.hasTipChange {
		out:
			for {
				select {
				case ni := <-notes:
					switch n := ni.(type) {
					case *asset.TipChangeNote:
						if n.Tip != test.bestHeader.Number.Uint64() {
							t.Fatalf("expected tip change but didn't get it")
						}
						break out
					}
				case <-time.After(time.Second * 5):
					t.Fatal("timed out waiting for tip change")
				}
			}
		} else {
			if w.currentTip.Number.Cmp(test.bestHeader.Number) != 0 {
				t.Fatalf("tip was changed. wasn't supposed to be changed")
			}
		}
		cancel()

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
		name: "ok header too old",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 25,
		},
		subSecs:    dexeth.MaxBlockInterval + 1,
		wantRatio:  0.999,
		wantSynced: false,
	}, {
		name: "sync progress error",
		syncProg: ethereum.SyncProgress{
			CurrentBlock: 25,
			HighestBlock: 0,
		},
		syncProgErr: errors.New("test error"),
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
			node:          node,
			addr:          node.address(),
			ctx:           ctx,
			log:           tLogger,
			finalizeConfs: txConfsNeededToConfirm,
		}
		ss, err := eth.SyncStatus()
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
		if ss.Synced != test.wantSynced {
			t.Fatalf("want synced %v got %v for test %q", test.wantSynced, ss.Synced, test.name)
		}
		if ss.BlockProgress() != test.wantRatio {
			t.Fatalf("want ratio %v got %v for test %q", test.wantRatio, ss.BlockProgress(), test.name)
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
		gasEstimates: ethGasesV0,
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
		ttc.tContractor.gasEstimates = &tokenGasesV0
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
			txConfirmations: make(map[common.Hash]uint32),
			txConfsErr:      make(map[common.Hash]error),
			receipts:        make(map[common.Hash]*types.Receipt),
			receiptErrs:     make(map[common.Hash]error),
			receiptTxs:      make(map[common.Hash]*types.Transaction),
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

	emitChan := make(chan asset.WalletNotification, 128)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-emitChan:
			}
		}
	}()

	aw := &assetWallet{
		baseWallet: &baseWallet{
			baseChainID:      BipID,
			chainID:          dexeth.ChainIDs[dex.Simnet],
			tokens:           dexeth.Tokens,
			addr:             node.addr,
			net:              dex.Simnet,
			node:             node,
			ctx:              ctx,
			log:              tLogger,
			gasFeeLimitV:     defaultGasFeeLimit,
			pendingNonceAt:   new(big.Int),
			confirmedNonceAt: new(big.Int),
			pendingTxs:       make([]*extendedWalletTx, 0),
			txDB:             &tTxDB{},
			currentTip:       &types.Header{Number: new(big.Int), GasLimit: 30_000_000},
			finalizeConfs:    txConfsNeededToConfirm,
			maxTxFeeGwei:     dexeth.GweiFactor, // 1 ETH
		},
		versionedGases:     versionedGases,
		log:                tLogger.SubLogger(strings.ToUpper(dex.BipIDSymbol(assetID))),
		assetID:            assetID,
		contractorV0:       c,
		contractorV1:       c,
		findRedemptionReqs: make(map[string]*findRedemptionRequest),
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
		pendingTxCheckBal:  new(big.Int),
		pendingApprovals:   make(map[common.Address]*pendingApproval),
		approvalCache:      make(map[common.Address]bool),
		// move up after review
		wi:   WalletInfo,
		emit: asset.NewWalletEmitter(emitChan, BipID, tLogger),
		versionedContracts: map[uint32]common.Address{
			0: {},
		},
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
			contractorV0:     node.tContractor,
			contractorV1:     node.tContractor,
			assetID:          BipID,
			atomize:          dexeth.WeiToGwei,
			pendingApprovals: make(map[common.Address]*pendingApproval),
			approvalCache:    make(map[common.Address]bool),
			emit:             asset.NewWalletEmitter(emitChan, BipID, tLogger),
		}
		w = &TokenWallet{
			assetWallet: aw,
			cfg:         &tokenWalletConfig{},
			parent:      node.tokenParent,
			token:       dexeth.Tokens[usdcEthID],
			netToken:    dexeth.Tokens[usdcEthID].NetTokens[dex.Simnet],
		}
		aw.wallets = map[uint32]*assetWallet{
			usdcEthID: aw,
			BipID:     node.tokenParent,
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
		balErr:  errors.New("test error"),
		wantErr: true,
	}}

	for _, test := range tests {
		var assetID uint32 = BipID
		if test.token {
			assetID = usdcEthID
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
			return node.newTransaction(nonce, value)
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
			t.Fatalf("%s: want available balance %v got %v for test %q", test.name, test.wantBal, bal.Available, test.name)
		}
		if bal.Immature != test.wantImmature {
			t.Fatalf("%s: want immature balance %v got %v for test %q", test.name, test.wantImmature, bal.Immature, test.name)
		}
		if bal.Locked != test.wantLocked {
			t.Fatalf("%s: want locked balance %v got %v for test %q", test.name, test.wantLocked, bal.Locked, test.name)
		}
	}
}

func TestBalanceNoMempool(t *testing.T) {
	var nonceCounter int64

	newExtendedWalletTx := func(assetID uint32, amt, maxFees, blockNum uint64, txType asset.TransactionType) *extendedWalletTx {
		var tokenID *uint32
		if assetID != BipID {
			tokenID = &assetID
		}
		txHash := common.BytesToHash(encode.RandomBytes(32))

		et := &extendedWalletTx{
			WalletTransaction: &asset.WalletTransaction{
				ID:          txHash.String(),
				Type:        txType,
				Amount:      amt,
				BlockNumber: blockNum,
				TokenID:     tokenID,
				Fees:        maxFees,
			},
			Nonce:  big.NewInt(nonceCounter),
			txHash: txHash,
		}
		nonceCounter++

		return et
	}

	tests := []struct {
		name           string
		assetID        uint32
		unconfirmedTxs []*extendedWalletTx
		expPendingIn   uint64
		expPendingOut  uint64
	}{
		{
			name:    "single eth tx",
			assetID: BipID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(BipID, 1, 2, 0, asset.Send),
			},
			expPendingOut: 3,
		},
		{
			name:    "eth with token fees",
			assetID: BipID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(usdcEthID, 4, 5, 0, asset.Send),
			},
			expPendingOut: 5,
		},
		{
			name:    "token with 1 tx and other ignored assets",
			assetID: usdcEthID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(usdcEthID, 4, 5, 0, asset.Send),
				newExtendedWalletTx(usdcEthID+1, 8, 9, 0, asset.Send),
			},
			expPendingOut: 4,
		},
		{
			name:    "token with 1 tx incoming",
			assetID: usdcEthID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(usdcEthID, 15, 5, 0, asset.Redeem),
			},
			expPendingIn: 15,
		},
		{
			name:    "eth mixed txs",
			assetID: BipID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(BipID, 1, 2, 0, asset.Swap),     // 3 eth out
				newExtendedWalletTx(usdcEthID, 3, 4, 1, asset.Send), // confirmed
				newExtendedWalletTx(usdcEthID, 5, 6, 0, asset.Swap), // 6 eth out
				newExtendedWalletTx(BipID, 7, 1, 0, asset.Refund),   // 1 eth out, 7 eth in
			},
			expPendingOut: 10,
			expPendingIn:  7,
		},
		{
			name:    "already confirmed, but still waiting for txConfsNeededToConfirm",
			assetID: usdcEthID,
			unconfirmedTxs: []*extendedWalletTx{
				newExtendedWalletTx(usdcEthID, 15, 5, 1, asset.Redeem),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, eth, node, shutdown := tassetWallet(tt.assetID)
			defer shutdown()
			eth.node = node.testNode // no mempool
			node.bal = unlimitedAllowance
			node.tokenContractor.bal = unlimitedAllowance
			eth.connected.Store(true)

			eth.pendingTxs = tt.unconfirmedTxs

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
		})
	}
}

func TestFeeRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := &testNode{}
	eth := &baseWallet{
		node:          node,
		ctx:           ctx,
		log:           tLogger,
		finalizeConfs: txConfsNeededToConfirm,
		currentTip:    &types.Header{Number: big.NewInt(100)},
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
			netFeeStateErr: errors.New("test error"),
		},
		{
			name:    "overflow error",
			baseFee: big.NewInt(maxInt),
			tip:     big.NewInt(1),
		},
	}

	for _, test := range tests {
		eth.currentFees.blockNum = 0
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
	t.Run("token", func(t *testing.T) { testRefund(t, usdcEthID) })
}

func testRefund(t *testing.T, assetID uint32) {
	_, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	const feeSuggestion = 100
	const gweiBal = 1e9
	const ogRefundReserves = 1e8

	// v1Contractor := &tContractor{
	// 	swapMap:      make(map[[32]byte]*dexeth.SwapState, 1),
	// 	gasEstimates: ethGasesV0,
	// 	redeemTx:     types.NewTx(&types.DynamicFeeTx{}),
	// }
	// var v1c contractor = v1Contractor

	// gasesV1 := &dexeth.Gases{Refund: 1e5}
	// if assetID == BipID {
	// 	dexeth.VersionedGases[1] = gasesV1
	// 	defer delete(dexeth.VersionedGases, 1)
	// } else {
	// 	tokenContracts := dexeth.Tokens[usdcEthID].NetTokens[dex.Simnet].SwapContracts
	// 	tc := *tokenContracts[0]
	// 	tc.Gas = *gasesV1
	// 	tokenContracts[1] = &tc
	// 	defer delete(tokenContracts, 1)
	// 	v1c = &tTokenContractor{tContractor: v1Contractor}
	// }

	// eth.contractors[1] = v1c

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	v0Contract := dexeth.EncodeContractData(0, secretHash[:])
	v1Vector := dexeth.SwapVector{
		From:       testAddressA,
		To:         testAddressB,
		Value:      dexeth.GweiToWei(1),
		SecretHash: secretHash,
		LockTime:   uint64(time.Now().Unix()),
	}
	v1Contract := dexeth.EncodeContractData(1, v1Vector.Locator())

	ss := &dexeth.SwapState{
		Value: dexeth.GweiToWei(1),
	}

	node.tContractor.swapMap[secretHash] = ss

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
		v1              bool
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
			v1:           true,
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
			swapErr:  errors.New("test error"),
			wantErr:  true,
		},
		{
			name:            "is refundable error",
			isRefundable:    true,
			isRefundableErr: errors.New("test error"),
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
			refundErr:    errors.New("test error"),
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
		c := node.tContractor
		contract := v0Contract
		if test.v1 {
			contract = v1Contract
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
func (*badCoin) TxID() string {
	return "abc"
}
func (b *badCoin) Value() uint64 {
	return uint64(*b)
}

func TestFundOrderReturnCoinsFundingCoins(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testFundOrderReturnCoinsFundingCoins(t, BipID) })
	t.Run("token", func(t *testing.T) { testFundOrderReturnCoinsFundingCoins(t, usdcEthID) })
}

func testFundOrderReturnCoinsFundingCoins(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()
	walletBalanceGwei := uint64(dexeth.GweiFactor)
	fromAsset := tETHV0
	if assetID == BipID {
		node.bal = dexeth.GweiToWei(walletBalanceGwei)
	} else {
		fromAsset = tTokenV0
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
		AssetVersion:  fromAsset.Version,
		Value:         walletBalanceGwei / 2,
		MaxSwapCount:  2,
		MaxFeeRate:    fromAsset.MaxFeeRate,
		RedeemVersion: tBTC.Version, // not important if not a token
		RedeemAssetID: tBTC.ID,
	}

	// Test fund order with less than available funds
	coins1, redeemScripts1, _, err := w.FundOrder(&order)
	expectedOrderFees := eth.gases(fromAsset.Version).Swap * order.MaxFeeRate * order.MaxSwapCount
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

	node.setBalanceError(eth, errors.New("test error"))
	_, _, _, err = w.FundOrder(&order)
	if err == nil {
		t.Fatalf("balance error should cause error but did not")
	}
	node.setBalanceError(eth, nil)

	// Test that funding without allowance causes error
	if assetID != BipID {
		eth.approvalCache = make(map[common.Address]bool)
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
	eth2.contractorV0 = node.tokenContractor
	eth2.contractorV1 = node.tokenContractor
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
	node.balErr = errors.New("test error")
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
	t.Run("token", func(t *testing.T) { testFundMultiOrder(t, usdcEthID) })
}

func testFundMultiOrder(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)

	defer shutdown()

	fromAsset := tETHV0
	swapGas := dexeth.VersionedGases[fromAsset.Version].Swap
	if assetID != BipID {
		fromAsset = tTokenV0
		node.tokenContractor.allow = unlimitedAllowance
		swapGas = dexeth.Tokens[usdcEthID].NetTokens[dex.Simnet].
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
				AssetVersion: fromAsset.Version,
				MaxFeeRate:   fromAsset.MaxFeeRate,
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
		node.setBalanceError(eth, nil)
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
	const currentFees = 44
	const lotSize = 10e9
	oneFee := ethGasesV0.Swap * tETHV0.MaxFeeRate
	refund := ethGasesV0.Refund * tETHV0.MaxFeeRate
	oneLock := lotSize + oneFee + refund
	oneFeeToken := tokenGasesV0.Swap*tTokenV0.MaxFeeRate + tokenGasesV0.Refund*tTokenV0.MaxFeeRate

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
			wantMaxFees:   tETHV0.MaxFeeRate * ethGasesV0.Swap,
			wantBestCase:  currentFees * ethGasesV0.Swap,
			wantWorstCase: currentFees * ethGasesV0.Swap,
		},
		{
			name:      "one lot enough for fees - token",
			bal:       lotSize,
			lots:      1,
			parentBal: oneFeeToken,
			token:     true,

			wantLots:      1,
			wantValue:     lotSize,
			wantMaxFees:   tTokenV0.MaxFeeRate * tokenGasesV0.Swap,
			wantBestCase:  currentFees * tokenGasesV0.Swap,
			wantWorstCase: currentFees * tokenGasesV0.Swap,
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
			wantMaxFees:   4 * tETHV0.MaxFeeRate * ethGasesV0.Swap,
			wantBestCase:  currentFees * ethGasesV0.Swap,
			wantWorstCase: 4 * currentFees * ethGasesV0.Swap,
		},
		{
			name:      "fewer than max lots - token",
			bal:       10 * lotSize,
			lots:      4,
			token:     true,
			parentBal: oneFeeToken * 4,

			wantLots:      4,
			wantValue:     4 * lotSize,
			wantMaxFees:   4 * tTokenV0.MaxFeeRate * tokenGasesV0.Swap,
			wantBestCase:  currentFees * tokenGasesV0.Swap,
			wantWorstCase: 4 * currentFees * tokenGasesV0.Swap,
		},
		{
			name:   "balanceError",
			bal:    5 * lotSize,
			balErr: errors.New("test error"),
			lots:   1,

			wantErr: true,
		},
		{
			name:   "balanceError - token",
			bal:    5 * lotSize,
			balErr: errors.New("test error"),
			lots:   1,
			token:  true,

			wantErr: true,
		},
	}

	runTest := func(t *testing.T, test testData) {

		var assetID uint32 = BipID
		assetCfg := tETHV0
		if test.token {
			assetID = usdcEthID
			assetCfg = tTokenV0
		}

		w, _, node, shutdown := tassetWallet(assetID)
		defer shutdown()
		node.baseFee, node.tip = dexeth.GweiToWei(baseFee), dexeth.GweiToWei(tip)

		if test.token {
			node.tContractor.gasEstimates = &tokenGasesV0
			node.tokenContractor.bal = dexeth.GweiToWei(test.bal)
			node.bal = dexeth.GweiToWei(test.parentBal)
		} else {
			node.bal = dexeth.GweiToWei(test.bal)
		}

		node.balErr = test.balErr

		preSwap, err := w.PreSwap(&asset.PreSwapForm{
			AssetVersion:  assetCfg.Version,
			LotSize:       lotSize,
			Lots:          test.lots,
			MaxFeeRate:    assetCfg.MaxFeeRate,
			FeeSuggestion: currentFees, // ignored
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
			t.Fatalf("%q: %v", test.name, err)
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
	t.Run("token", func(t *testing.T) { testSwap(t, usdcEthID) })
}

func testSwap(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	assetCfg := tETHV0
	gases := ethGasesV0
	if assetID != BipID {
		assetCfg = tTokenV0
		gases = &tokenGasesV0
	}

	receivingAddress := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	node.tContractor.initTx = types.NewTx(&types.DynamicFeeTx{})

	coinIDsForAmounts := func(coinAmounts []uint64, n uint64) []dex.Bytes {
		coinIDs := make([]dex.Bytes, 0, len(coinAmounts))
		for _, amt := range coinAmounts {
			if assetID == BipID {
				coinIDs = append(coinIDs, createFundingCoin(eth.addr, amt).RecoveryID())
			} else {
				// Not gonna version the fees here unless it matters.
				fees := n * gases.Swap * assetCfg.MaxFeeRate
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
		return gases.Swap * uint64(numSwaps)
	}

	testSwap := func(testName string, swaps asset.Swaps, expectError bool) {
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
			contractVer, locator, err := dexeth.DecodeContractData(contractData)
			if err != nil {
				t.Fatalf("failed to decode contract data: %v", err)
			}
			if contractVersion(swaps.AssetVersion) != contractVer {
				t.Fatal("wrong contract version")
			}
			chkLocator := acToLocator(contractVer, contract, dexeth.GweiToWei(contract.Value), node.addr)
			if !bytes.Equal(locator, chkLocator) {
				t.Fatalf("%v, contract: %x != locator in input: %x",
					testName, receipt.Contract(), locator)
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
	node.tContractor.initErr = errors.New("test error")
	contracts := []*asset.Contract{
		{
			Address:    receivingAddress,
			Value:      ethToGwei(1),
			SecretHash: secretHash[:],
			LockTime:   expiration,
		},
	}
	inputs := refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)}, 1)
	swaps := asset.Swaps{
		AssetVersion: assetCfg.Version,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
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
		AssetVersion: assetCfg.Version,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
	}
	testSwap("one contract, don't lock change", swaps, false)

	// Test one contract with locking change
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2)}, 1)
	swaps = asset.Swaps{
		AssetVersion: assetCfg.Version,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   true,
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
		AssetVersion: assetCfg.Version,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
	}
	testSwap("two contracts", swaps, false)

	// Test error when funding coins are not enough to cover swaps
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(1)}, 2)
	swaps = asset.Swaps{
		AssetVersion: assetCfg.Version,
		Inputs:       inputs,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
	}
	testSwap("funding coins not enough balance", swaps, true)

	// Ensure when funds are exactly the same as required works properly
	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * dexeth.InitGas(1, 0))}, 2)
	swaps = asset.Swaps{
		Inputs:       inputs,
		AssetVersion: assetCfg.Version,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
	}
	testSwap("exact change", swaps, false)

	// Version 1
	assetCfg = tETHV1
	gases = ethGasesV1
	if assetID != BipID {
		assetCfg = tTokenV1
		gases = &tokenGasesV1
	}
	node.tContractor.gasEstimates = gases

	inputs = refreshWalletAndFundCoins(5, []uint64{ethToGwei(2) + (2 * 200 * dexeth.InitGas(1, 1))}, 2)
	swaps = asset.Swaps{
		Inputs:       inputs,
		AssetVersion: assetCfg.Version,
		Contracts:    contracts,
		FeeRate:      assetCfg.MaxFeeRate,
		LockChange:   false,
	}
	testSwap("v1", swaps, false)
}

func TestPreRedeem(t *testing.T) {
	w, _, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	form := &asset.PreRedeemForm{
		AssetVersion:  tETHV0.Version,
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
	w, _, _, shutdown2 := tassetWallet(usdcEthID)
	defer shutdown2()

	form.AssetVersion = tTokenV0.Version
	node.tokenContractor.allow = unlimitedAllowanceReplenishThreshold

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
	t.Run("token", func(t *testing.T) { testRedeem(t, usdcEthID) })
}

func testRedeem(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	// // Test with a non-zero contract version to ensure it makes it into the receipt
	// contractVer := uint32(1)

	// eth.versionedGases[1] = ethGases
	// if assetID != BipID {
	// 	eth.versionedGases[1] = &tokenGases
	// }

	// tokenContracts := eth.tokens[usdcEthID].NetTokens[dex.Simnet].SwapContracts
	// tokenContracts[1] = tokenContracts[0]
	// defer delete(tokenContracts, 1)

	// contractorV1 := &tContractor{
	// 	swapMap:      make(map[[32]byte]*dexeth.SwapState, 1),
	// 	gasEstimates: ethGases,
	// 	redeemTx:     types.NewTx(&types.DynamicFeeTx{Data: []byte{1, 2, 3}}),
	// }
	// var c contractor = contractorV1
	// if assetID != BipID {
	// 	c = &tTokenContractor{
	// 		tContractor: contractorV1,
	// 	}
	// }
	var contractor *tContractor
	if assetID == BipID {
		contractor = eth.contractorV0.(*tContractor)
	} else {
		contractor = eth.contractorV1.(*tTokenContractor).tContractor
	}
	contractor.redeemTx = types.NewTx(&types.DynamicFeeTx{Data: []byte{1, 2, 3}})
	now := time.Now()
	const value = 1e9

	addSwapToSwapMap := func(secretHash [32]byte, step dexeth.SwapStep) {
		swap := dexeth.SwapState{
			BlockHeight: 1,
			LockTime:    now,
			Initiator:   testAddressB,
			Participant: testAddressA,
			Value:       dexeth.GweiToWei(value),
			State:       step,
		}
		contractor.swapMap[secretHash] = &swap
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

	addSwapToSwapMap(secretHashes[0], dexeth.SSInitiated)
	addSwapToSwapMap(secretHashes[1], dexeth.SSInitiated)

	/* COMMENTED while estimateRedeemGas is on the $#!t list
	var redeemGasesV0, redeemGasesV1 *dexeth.Gases
	if assetID == BipID {
		redeemGasesV0 = ethGasesV0
		redeemGasesV1 = ethGasesV1
	} else {
		redeemGasesV0 = &tokenGasesV0
		redeemGasesV1 = &tokenGasesV1
	}
	redeemGas := redeemGasesV0.Redeem
	var higherGasEstimate uint64 = redeemGas * 2 * 12 / 10                     // 120% of estimate
	var doubleGasEstimate uint64 = (redeemGas * 2 * 2) * 10 / 11               // 200% of estimate after 10% increase
	var moreThanDoubleGasEstimate uint64 = (redeemGas * 2 * 21 / 10) * 10 / 11 // > 200% of estimate after 10% increase
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
	newRedeem := func(idx int) *asset.Redemption {
		return &asset.Redemption{
			Spends: &asset.AuditInfo{
				Contract:   dexeth.EncodeContractData(0, secretHashes[idx][:]),
				SecretHash: secretHashes[idx][:], // redundant for all current assets, unused with eth
				Coin: &coin{
					id:    randomHash(),
					value: value,
				},
			},
			Secret: secrets[idx][:],
		}
	}

	// newRedeemV1 := func(idx int) *asset.Redemption {
	// 	locator := (&dexeth.SwapVector{
	// 		From:       testAddressA,
	// 		To:         testAddressB,
	// 		Value:      value,
	// 		SecretHash: secretHashes[idx],
	// 		LockTime:   uint64(now.Unix()),
	// 	}).Locator()
	// 	return &asset.Redemption{
	// 		Spends: &asset.AuditInfo{
	// 			Contract:   dexeth.EncodeContractData(1, locator),
	// 			SecretHash: secretHashes[idx][:], // redundant for all current assets, unused with eth
	// 			Coin: &coin{
	// 				id:    randomHash(),
	// 				value: value,
	// 			},
	// 		},
	// 		Secret: secrets[idx][:],
	// 	}
	// }

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
		v1                bool
	}{
		{
			name:              "ok",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(10e9),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
				FeeSuggestion: 100,
			},
		},
		/* COMMENTED while estimateRedeemGas is on the $#!t list
		{
			name:              "ok-v1",
			expectError:       false,
			isRedeemable:      true,
			ethBal:            dexeth.GweiToWei(10e9),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{newRedeemV1(0), newRedeemV1(1)},
				FeeSuggestion: 100,
			},
			v1: true,
		},
		{
			name:              "higher gas estimate than reserved",
			expectError:       false,
			swapMap:           swappableSwapMap,
			ethBal:            dexeth.GweiToWei(additionalFundsNeeded(100, 50, higherGasEstimate, 2)),
			baseFee:           dexeth.GweiToWei(100),
			expectedGasFeeCap: dexeth.GweiToWei(100),
			redeemGasOverride: &higherGasEstimate,
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
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
				Redemptions:   []*asset.Redemption{newRedeem(0), newRedeem(1)},
				FeeSuggestion: 100,
			},
		},
		{
			name:        "redeem error",
			redeemErr:   errors.New("test error"),
			swapMap:     swappableSwapMap,
			expectError: true,
			ethBal:      dexeth.GweiToWei(10e9),
			baseFee:     dexeth.GweiToWei(100),
			form: asset.RedeemForm{
				Redemptions:   []*asset.Redemption{newRedeem(0)},
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
				Redemptions:   []*asset.Redemption{newRedeem(2)},
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
		contractor.redeemErr = test.redeemErr
		contractor.swapErr = test.swapErr
		contractor.redeemGasOverride = test.redeemGasOverride
		for secretHash, step := range test.swapMap {
			contractor.swapMap[secretHash].State = step
		}
		node.bal = test.ethBal
		node.baseFee = test.baseFee

		var contractVer uint32
		if test.v1 {
			contractVer = 1
		}

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
		rg := gases(contractVer, eth.versionedGases)
		expectedGas := rg.Redeem + (uint64(len(test.form.Redemptions))-1)*rg.RedeemAdd
		expectedFees := expectedGas * test.form.FeeSuggestion
		if fees != expectedFees {
			t.Fatalf("%v: expected fees %d, but got %d", test.name, expectedFees, fees)
		}

		// Check that value of output coin is as axpected
		var totalSwapValue uint64
		for _, redemption := range test.form.Redemptions {
			_, locator, err := dexeth.DecodeContractData(redemption.Spends.Contract)
			if err != nil {
				t.Fatalf("DecodeLocator: %v", err)
			}
			var secretHash [32]byte
			if test.v1 {
				v, _ := dexeth.ParseV1Locator(locator)
				secretHash = v.SecretHash
			} else {
				copy(secretHash[:], locator)
			}
			// secretHash should equal redemption.Spends.SecretHash, but it's
			// not part of the Redeem code, just the test input consistency.
			swap := contractor.swapMap[secretHash]
			totalSwapValue += dexeth.WeiToGwei(swap.Value)
		}
		if out.Value() != totalSwapValue {
			t.Fatalf("%s: expected coin value to be %d but got %d",
				test.name, totalSwapValue, out.Value())
		}

		// Check that gas limit in the transaction is as expected
		var expectedGasLimit uint64
		if test.redeemGasOverride == nil {
			if assetID == BipID {
				expectedGasLimit = rg.Redeem * uint64(len(test.form.Redemptions))
			} else {
				expectedGasLimit = rg.Redeem * uint64(len(test.form.Redemptions))
			}
		} else {
			expectedGasLimit = rg.Redeem * uint64(len(test.form.Redemptions))
		}
		if contractor.lastRedeemOpts.GasLimit != expectedGasLimit {
			t.Fatalf("%s: expected gas limit %d, but got %d", test.name, expectedGasLimit, contractor.lastRedeemOpts.GasLimit)
		}

		// Check that the gas fee cap in the transaction is as expected
		if contractor.lastRedeemOpts.GasFeeCap.Cmp(test.expectedGasFeeCap) != 0 {
			t.Fatalf("%s: expected gas fee cap %v, but got %v", test.name, test.expectedGasFeeCap, contractor.lastRedeemOpts.GasFeeCap)
		}
	}
}

func TestMaxOrder(t *testing.T) {
	const baseFee, tip = 42, 2
	const currentFee = baseFee + tip

	type testData struct {
		name          string
		bal           uint64
		balErr        error
		lotSize       uint64
		maxFeeRate    uint64
		token         bool
		parentBal     uint64
		wantErr       bool
		wantLots      uint64
		wantValue     uint64
		wantMaxFees   uint64
		wantWorstCase uint64
		wantBestCase  uint64
		wantLocked    uint64
		v1            bool
	}
	tests := []testData{
		{
			name:       "no balance",
			bal:        0,
			lotSize:    10,
			maxFeeRate: 100,
		},
		{
			name:       "no balance - token",
			bal:        0,
			lotSize:    10,
			maxFeeRate: 100,
			token:      true,
			parentBal:  100,
		},
		{
			name:       "not enough for fees",
			bal:        10,
			lotSize:    10,
			maxFeeRate: 100,
		},
		{
			name:       "not enough for fees - token",
			bal:        10,
			token:      true,
			parentBal:  0,
			lotSize:    10,
			maxFeeRate: 100,
		},
		{
			name:          "one lot enough for fees",
			bal:           11,
			lotSize:       10,
			maxFeeRate:    100,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * ethGasesV0.Swap,
			wantBestCase:  currentFee * ethGasesV0.Swap,
			wantWorstCase: currentFee * ethGasesV0.Swap,
			wantLocked:    ethToGwei(10) + (100 * ethGasesV0.Swap),
		},
		{
			name:          "one lot enough for fees - v1",
			bal:           11,
			lotSize:       10,
			maxFeeRate:    100,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * ethGasesV1.Swap,
			wantBestCase:  currentFee * ethGasesV1.Swap,
			wantWorstCase: currentFee * ethGasesV1.Swap,
			wantLocked:    ethToGwei(10) + (100 * ethGasesV0.Swap),
			v1:            true,
		},
		{
			name:          "one lot enough for fees - token",
			bal:           11,
			lotSize:       10,
			maxFeeRate:    100,
			token:         true,
			parentBal:     1,
			wantLots:      1,
			wantValue:     ethToGwei(10),
			wantMaxFees:   100 * tokenGasesV0.Swap,
			wantBestCase:  currentFee * tokenGasesV0.Swap,
			wantWorstCase: currentFee * tokenGasesV0.Swap,
			wantLocked:    ethToGwei(10) + (100 * tokenGasesV0.Swap),
		},
		{
			name:          "multiple lots",
			bal:           51,
			lotSize:       10,
			maxFeeRate:    100,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * ethGasesV0.Swap,
			wantBestCase:  currentFee * ethGasesV0.Swap,
			wantWorstCase: 5 * currentFee * ethGasesV0.Swap,
			wantLocked:    ethToGwei(50) + (5 * 100 * ethGasesV0.Swap),
		},
		{
			name:          "multiple lots - token",
			bal:           51,
			lotSize:       10,
			maxFeeRate:    100,
			token:         true,
			parentBal:     1,
			wantLots:      5,
			wantValue:     ethToGwei(50),
			wantMaxFees:   5 * 100 * tokenGasesV0.Swap,
			wantBestCase:  currentFee * tokenGasesV0.Swap,
			wantWorstCase: 5 * currentFee * tokenGasesV0.Swap,
			wantLocked:    ethToGwei(50) + (5 * 100 * tokenGasesV0.Swap),
		},
		{
			name:    "balanceError",
			bal:     51,
			lotSize: 10,
			// feeSuggestion: 90,
			maxFeeRate: 100,
			balErr:     errors.New("test error"),
			wantErr:    true,
		},
	}

	runTest := func(t *testing.T, test testData) {
		var assetID uint32 = BipID
		gases := ethGasesV0
		if test.token {
			assetID = usdcEthID
			gases = &tokenGasesV0
			if test.v1 {
				gases = &tokenGasesV1
			}
		} else if test.v1 {
			gases = ethGasesV1
		}

		w, _, node, shutdown := tassetWallet(assetID)
		defer shutdown()
		node.baseFee, node.tip = dexeth.GweiToWei(baseFee), dexeth.GweiToWei(tip)

		if test.token {
			node.tContractor.gasEstimates = &tokenGasesV0
			// dexAsset = tTokenV0
			node.tokenContractor.bal = dexeth.GweiToWei(ethToGwei(test.bal))
			node.bal = dexeth.GweiToWei(ethToGwei(test.parentBal))
		} else {
			node.bal = dexeth.GweiToWei(ethToGwei(test.bal))
		}

		node.balErr = test.balErr
		node.tContractor.gasEstimates = gases

		var serverVer uint32
		if test.v1 {
			serverVer = 1
		}

		maxOrder, err := w.MaxOrder(&asset.MaxOrderForm{
			LotSize:       ethToGwei(test.lotSize),
			AssetVersion:  serverVer,
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
	t.Run("token", func(t *testing.T) { testAuditContract(t, usdcEthID) })
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
			contract: dexeth.EncodeContractData(0, secretHashes[1][:]),
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
			contract: dexeth.EncodeContractData(0, secretHashes[0][:]),
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
			contract: dexeth.EncodeContractData(0, secretHashes[2][:]),
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
			contract:  dexeth.EncodeContractData(0, secretHashes[2][:]),
			badTxData: true,
			wantErr:   true,
		},
		{
			name:     "cannot unmarshal tx binary",
			contract: dexeth.EncodeContractData(0, secretHashes[1][:]),
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

		tx := tTx(2, 300, uint64(len(test.initiations)), &testAddressC, txData, 21000)
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
		addr:          common.HexToAddress(address),
		finalizeConfs: txConfsNeededToConfirm,
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
			node:          node,
			addr:          node.address(),
			ctx:           ctx,
			log:           tLogger,
			finalizeConfs: txConfsNeededToConfirm,
		},
		assetID: BipID,
	}

	msg := []byte("msg")

	// SignData error
	node.signDataErr = errors.New("test error")
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
	state := &dexeth.SwapState{
		Value: dexeth.GweiToWei(1),
	}
	hdr := &types.Header{
		GasLimit: 30_000_000,
	}

	node.tContractor.swapMap[secretHash] = state

	state.BlockHeight = 5
	state.State = dexeth.SSInitiated
	hdr.Number = big.NewInt(6)
	eth.currentTip = hdr

	ver := uint32(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	checkResult := func(expErr bool, expConfs uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := eth.SwapConfirmations(ctx, nil, dexeth.EncodeContractData(ver, secretHash[:]), time.Time{})
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

	// ErrSwapNotInitiated
	state.State = dexeth.SSNone
	_, _, err := eth.SwapConfirmations(ctx, nil, dexeth.EncodeContractData(0, secretHash[:]), time.Time{})
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
	eth, ok := wallet.(*ETHBridgeWallet)
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
	eth, ok = wallet.(*ETHBridgeWallet)
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
		Value:    dexeth.GweiToWei(1),
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
	t.Run("token", func(t *testing.T) { testFindRedemption(t, usdcEthID) })
}

func testFindRedemption(t *testing.T, assetID uint32) {
	_, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	var secret [32]byte
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])

	contract := dexeth.EncodeContractData(0, secretHash[:])
	state := &dexeth.SwapState{
		Secret: secret,
		State:  dexeth.SSInitiated,
		Value:  dexeth.GweiToWei(1),
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
					pending := eth.findRedemptionReqs[string(secretHash[:])] != nil
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
	eth.findRedemptionReqs[string(secretHash[:])] = &findRedemptionRequest{}
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
	t.Run("token", func(t *testing.T) { testRefundReserves(t, usdcEthID) })
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
	gasesV0 := eth.versionedGases[0]
	gasesV1 := eth.versionedGases[1]
	assetV0 := *tETHV0
	assetV1 := *tETHV0
	if assetID != BipID {
		feeWallet = node.tokenParent
		assetV0 = *tTokenV0
		assetV1 = *tTokenV0
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
	t.Run("token", func(t *testing.T) { testRedemptionReserves(t, usdcEthID) })
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

	gasesV0 := eth.versionedGases[0]
	gasesV1 := eth.versionedGases[1]
	eth.versionedGases[1] = gasesV1
	assetV0 := *tETHV0
	assetV1 := *tETHV0
	feeWallet := eth
	if assetID != BipID {
		node.tokenContractor.allow = unlimitedAllowanceReplenishThreshold
		feeWallet = node.tokenParent
		assetV0 = *tTokenV0
		assetV1 = *tTokenV0
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
	t.Run("token", func(t *testing.T) { testSend(t, usdcEthID) })
}

func testSend(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	tx := tTx(0, 0, 0, &testAddressA, nil, 21000)
	txHash := tx.Hash()

	node.sendTxTx = tx
	node.tokenContractor.transferTx = tx

	maxFeeRate, _, _ := eth.recommendedMaxFeeRate(eth.ctx)
	ethFees := dexeth.WeiToGwei(maxFeeRate) * defaultSendGasLimit
	tokenFees := dexeth.WeiToGwei(maxFeeRate) * tokenGasesV1.Transfer

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
		balErr:  errors.New("test error"),
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
		sendTxErr: errors.New("test error"),
		wantErr:   true,
		addr:      testAddr,
	}, {
		name:      "Invalid address",
		sendTxErr: errors.New("invalid hex address error"),
		wantErr:   true,
		addr:      "",
	}}

	for _, test := range tests {
		node.setBalanceError(eth, test.balErr)
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
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if !bytes.Equal(txHash[:], coin.ID()) {
			t.Fatal("coin is not the tx hash")
		}
	}
}

func TestConfirmTransaction(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testConfirmTransaction(t, BipID) })
	t.Run("token", func(t *testing.T) { testConfirmTransaction(t, usdcEthID) })
}

func testConfirmTransaction(t *testing.T, assetID uint32) {
	wi, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	db := eth.txDB.(*tTxDB)

	const tip = 12
	const confBlock = tip - txConfsNeededToConfirm + 1

	var secret, secretHash [32]byte
	copy(secret[:], encode.RandomBytes(32))
	copy(secretHash[:], encode.RandomBytes(32))
	var txHash common.Hash
	copy(txHash[:], encode.RandomBytes(32))

	confirmTx := asset.NewRedeemConfTx(&asset.AuditInfo{
		Contract: dexeth.EncodeContractData(0, secretHash[:]),
	}, secret[:])

	pendingTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:          txHash.String(),
			BlockNumber: confBlock + 1,
		},
		txHash: txHash,
	}
	dbTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:          txHash.String(),
			BlockNumber: confBlock,
		},
	}

	type test struct {
		name                      string
		expectedConfs             uint64
		expectErr                 bool
		expectSwapRefundedErr     bool
		expectRedemptionFailedErr bool
		pendingTx                 *extendedWalletTx
		dbTx                      *extendedWalletTx
		dbErr                     error
		step                      dexeth.SwapStep
		receipt                   *types.Receipt
		receiptErr                error
		confirmTx                 *asset.ConfirmTx
	}

	tests := []*test{
		{
			name: "found on-chain. not yet confirmed",
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusSuccessful,
				BlockNumber: big.NewInt(confBlock + 1),
			},
			confirmTx:     confirmTx,
			expectedConfs: txConfsNeededToConfirm - 1,
		},
		{
			name:          "found on-chain. confirmed",
			step:          dexeth.SSRedeemed,
			expectedConfs: txConfsNeededToConfirm,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusSuccessful,
				BlockNumber: big.NewInt(confBlock),
			},
			confirmTx: confirmTx,
		},
		{
			name:          "found in pending txs",
			step:          dexeth.SSRedeemed,
			pendingTx:     pendingTx,
			confirmTx:     confirmTx,
			expectedConfs: txConfsNeededToConfirm - 1,
		},
		{
			name:          "found in db",
			step:          dexeth.SSRedeemed,
			dbTx:          dbTx,
			expectedConfs: txConfsNeededToConfirm,
			confirmTx:     confirmTx,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusSuccessful,
				BlockNumber: big.NewInt(confBlock),
			},
		},
		{
			name:          "db error not propagated. unconfirmed",
			step:          dexeth.SSRedeemed,
			dbErr:         errors.New("test error"),
			expectedConfs: txConfsNeededToConfirm - 1,
			confirmTx:     confirmTx,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusSuccessful,
				BlockNumber: big.NewInt(confBlock + 1),
			},
		},
		{
			name:                      "found on-chain. tx failed",
			step:                      dexeth.SSInitiated,
			expectErr:                 true,
			expectRedemptionFailedErr: true,
			confirmTx:                 confirmTx,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusFailed,
				BlockNumber: big.NewInt(confBlock),
			},
		},
		{
			name: "found on-chain. redeemed by another unknown transaction",
			step: dexeth.SSRedeemed,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusFailed,
				BlockNumber: big.NewInt(confBlock),
			},
			confirmTx:     confirmTx,
			expectedConfs: txConfsNeededToConfirm,
		},
		{
			name: "refund found on-chain. refunded by another unknown transaction",
			step: dexeth.SSRefunded,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusFailed,
				BlockNumber: big.NewInt(confBlock),
			},
			confirmTx:     asset.NewRefundConfTx(txHash[:], dexeth.EncodeContractData(0, secretHash[:]), secret[:]),
			expectedConfs: txConfsNeededToConfirm,
		},
		{
			name: "redeem refunded by another unknown transaction",
			step: dexeth.SSRefunded,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusFailed,
				BlockNumber: big.NewInt(confBlock),
			},
			confirmTx: confirmTx,
			expectErr: true,
		},
		{
			name: "refund redeemed by another unknown transaction",
			step: dexeth.SSRedeemed,
			receipt: &types.Receipt{
				Status:      types.ReceiptStatusFailed,
				BlockNumber: big.NewInt(confBlock),
			},
			confirmTx: asset.NewRefundConfTx(txHash[:], dexeth.EncodeContractData(0, secretHash[:]), secret[:]),
			expectErr: true,
		},
	}

	runTest := func(test *test) {
		fmt.Printf("###### %s ###### \n", test.name)

		node.tContractor.swapMap = map[[32]byte]*dexeth.SwapState{
			secretHash: {State: test.step, Value: big.NewInt(1)},
		}
		node.tContractor.lastRedeems = nil
		node.tokenContractor.bal = big.NewInt(1e9)
		node.bal = big.NewInt(1e9)

		eth.pendingTxs = []*extendedWalletTx{}
		if test.pendingTx != nil {
			eth.pendingTxs = append(eth.pendingTxs, test.pendingTx)
		}

		db.txToGet = test.dbTx
		db.getTxErr = test.dbErr

		node.lastSignedTx = nil
		eth.currentTip = &types.Header{Number: big.NewInt(tip)}
		node.receipt = test.receipt
		node.receiptErr = test.receiptErr

		result, err := wi.ConfirmTransaction(txHash[:], test.confirmTx, 0)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			if test.expectRedemptionFailedErr && !errors.Is(err, asset.ErrTxRejected) {
				t.Fatalf("%s: expected rejected tx error. got %v", test.name, err)
			}
			if test.expectSwapRefundedErr && !errors.Is(asset.ErrSwapRefunded, err) {
				t.Fatalf("%s: expected swap refunded error but got %v", test.name, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		// Check that the resulting status is as expected
		if test.expectedConfs != result.Confs ||
			txConfsNeededToConfirm != result.Req {
			t.Fatalf("%s: expected confs %d != result %d", test.name, test.expectedConfs, result.Confs)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

// Ensures that a small rise in the base fee between estimation
// and sending will not cause a failure.
func TestEstimateVsActualSendFees(t *testing.T) {
	t.Run("eth", func(t *testing.T) { testEstimateVsActualSendFees(t, BipID) })
	t.Run("token", func(t *testing.T) { testEstimateVsActualSendFees(t, usdcEthID) })
}

func testEstimateVsActualSendFees(t *testing.T, assetID uint32) {
	w, _, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	tx := tTx(0, 0, 0, &testAddressA, nil, 21000)
	node.sendTxTx = tx
	node.tokenContractor.transferTx = tx

	const testAddr = "dd93b447f7eBCA361805eBe056259853F3912E04"

	txFeeEstimator := w.(asset.TxFeeEstimator)
	fee, _, err := txFeeEstimator.EstimateSendTxFee("", 0, 0, false, false)
	if err != nil {
		t.Fatalf("error estimating fee: %v", err)
	}

	// Increase the base fee by 10%.
	node.baseFee = node.baseFee.Mul(node.baseFee, big.NewInt(11))
	node.baseFee = node.baseFee.Div(node.baseFee, big.NewInt(10))

	if assetID == BipID {
		node.bal = dexeth.GweiToWei(11e9)
		canSend := new(big.Int).Sub(node.bal, dexeth.GweiToWei(fee))
		canSendGwei, err := dexeth.WeiToGweiSafe(canSend)
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
	t.Run("token", func(t *testing.T) { testEstimateSendTxFee(t, usdcEthID) })
}

func testEstimateSendTxFee(t *testing.T, assetID uint32) {
	w, eth, node, shutdown := tassetWallet(assetID)
	defer shutdown()

	maxFeeRate, _, _ := eth.recommendedMaxFeeRate(eth.ctx)
	ethFees := dexeth.WeiToGwei(maxFeeRate) * defaultSendGasLimit
	tokenFees := dexeth.WeiToGwei(maxFeeRate) * tokenGasesV1.Transfer

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
		balErr:  errors.New("test error"),
		wantErr: true,
		addr:    testAddr,
	}}

	for _, test := range tests {
		node.setBalanceError(eth, test.balErr)
		if assetID == BipID {
			node.bal = dexeth.GweiToWei(val + ethFees - test.sendAdj - test.feeAdj)
		} else {
			node.tokenContractor.bal = dexeth.GweiToWei(val - test.sendAdj)
			node.bal = dexeth.GweiToWei(tokenFees - test.feeAdj)
		}
		txFeeEstimator := w.(asset.TxFeeEstimator)
		estimate, _, err := txFeeEstimator.EstimateSendTxFee(test.addr, val, 0, false, false)
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

func TestSwapOrRedemptionFeesPaid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, bw, node, shutdown := tassetWallet(BipID)
	defer shutdown()

	secretHA, secretHB := encode.RandomBytes(32), encode.RandomBytes(32)
	contractDataFn := func(ver uint32, secretH []byte) []byte {
		s := [32]byte{}
		copy(s[:], secretH)
		return dexeth.EncodeContractData(ver, s[:])
	}
	const tip = 100
	const feeRate = 2 // gwei / gas
	const gasUsed = 100
	const fees = feeRate * gasUsed

	bw.currentTip = &types.Header{
		BaseFee: dexeth.GweiToWei(2),
		Number:  big.NewInt(tip),
	}

	confirmedReceipt := &types.Receipt{
		GasUsed:           gasUsed,
		EffectiveGasPrice: dexeth.GweiToWei(feeRate),
		BlockNumber:       big.NewInt(tip - txConfsNeededToConfirm + 1),
	}

	unconfirmedReceipt := &types.Receipt{
		BlockNumber: big.NewInt(tip - txConfsNeededToConfirm + 2),
	}

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
	initTx := tTx(200, 2, 0, nil, initFn(abFn()), 200)
	redeemTx := tTx(200, 3, 0, nil, redeemFn(abFn()), 200)
	tests := []struct {
		name            string
		contractData    []byte
		isInit, wantErr bool
		receipt         *types.Receipt
		receiptTx       *types.Transaction
		receiptErr      error
		wantSecrets     [][]byte
		pendingTx       *types.Transaction
		pendingTxBlock  uint64
	}{{
		name:         "ok init",
		contractData: contractDataFn(0, secretHA),
		isInit:       true,
		receipt:      confirmedReceipt,
		receiptTx:    initTx,
		wantSecrets:  sortedFn(),
	}, {
		name:         "ok redeem",
		contractData: contractDataFn(0, secretHB),
		receipt:      confirmedReceipt,
		receiptTx:    redeemTx,
		wantSecrets:  sortedFn(),
	}, {
		name:           "ok init from pending txs",
		contractData:   contractDataFn(0, secretHA),
		isInit:         true,
		pendingTx:      initTx,
		pendingTxBlock: confirmedReceipt.BlockNumber.Uint64(),
		wantSecrets:    sortedFn(),
	}, {
		name:           "ok redeem from pending txs",
		contractData:   contractDataFn(0, secretHB),
		pendingTx:      redeemTx,
		pendingTxBlock: confirmedReceipt.BlockNumber.Uint64(),
		wantSecrets:    sortedFn(),
	}, {
		name:         "bad contract data",
		contractData: nil,
		wantErr:      true,
	}, {
		name:         "receipt error",
		contractData: contractDataFn(0, secretHA),
		receiptErr:   errors.New("test error"),
		wantErr:      true,
	}, {
		name:         "not enough confirms",
		contractData: contractDataFn(0, secretHA),
		receipt:      unconfirmedReceipt,
		wantErr:      true,
	}, {
		name:           "not enough confs, pending tx",
		contractData:   contractDataFn(0, secretHA),
		isInit:         true,
		pendingTx:      initTx,
		pendingTxBlock: confirmedReceipt.BlockNumber.Uint64() + 1,
		wantSecrets:    sortedFn(),
		wantErr:        true,
	}, {
		name:         "bad init data",
		contractData: contractDataFn(0, secretHA),
		isInit:       true,
		receipt:      confirmedReceipt,
		receiptTx:    tTx(200, 2, 0, nil, nil, 200),
		wantErr:      true,
	}, {
		name:         "bad redeem data",
		contractData: contractDataFn(0, secretHA),
		receipt:      confirmedReceipt,
		receiptTx:    tTx(200, 2, 0, nil, nil, 200),
		wantErr:      true,
	}, {
		name:         "secret hash not found",
		contractData: contractDataFn(0, secretHB),
		isInit:       true,
		receipt:      confirmedReceipt,
		receiptTx:    tTx(200, 2, 0, nil, initFn([][]byte{secretHA}), 200),
		wantErr:      true,
	}}
	for _, test := range tests {
		var txHash common.Hash
		if test.pendingTx != nil {
			wt := bw.extendedTx(&genTxResult{
				tx:     test.pendingTx,
				txType: asset.Unknown,
				amt:    1,
			})
			wt.BlockNumber = test.pendingTxBlock
			wt.Fees = fees
			bw.pendingTxs = []*extendedWalletTx{wt}
			txHash = test.pendingTx.Hash()
		}
		node.receiptTx = test.receiptTx
		node.receipt = test.receipt
		node.receiptErr = test.receiptErr
		feesPaid, secretHs, err := bw.swapOrRedemptionFeesPaid(ctx, txHash[:], test.contractData, test.isInit)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", test.name, err)
		}
		if feesPaid != fees {
			t.Fatalf("%q: wanted fee %d but got %d", test.name, fees, feesPaid)
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
	m := &multiRPCClient{
		finalizeConfs: 3,
	}
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
			node := &multiRPCClient{
				finalizeConfs: 3,
				providers:     make([]*provider, len(tt.times)),
			}
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

type mockBridge struct {
	getCompletionDataFunc   func(ctx context.Context, txID string) ([]byte, error)
	getCompletionDataCalled chan struct{}
	completeBridgeCalled    bool
}

var _ bridge = (*mockBridge)(nil)

func (m *mockBridge) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	if m.getCompletionDataCalled != nil {
		m.getCompletionDataCalled <- struct{}{}
	}
	return m.getCompletionDataFunc(ctx, bridgeTxID)
}
func (m *mockBridge) bridgeContractAddr() common.Address { panic("not implemented") }
func (m *mockBridge) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	panic("not implemented")
}
func (m *mockBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	panic("not implemented")
}
func (m *mockBridge) requiresBridgeContractApproval() bool { panic("not implemented") }
func (m *mockBridge) initiateBridge(txOpts *bind.TransactOpts, destAssetID uint32, amount *big.Int) (*types.Transaction, bool, error) {
	panic("not implemented")
}
func (m *mockBridge) completeBridge(txOpts *bind.TransactOpts, completionData []byte) (*types.Transaction, error) {
	m.completeBridgeCalled = true
	return types.NewTransaction(0, common.Address{}, big.NewInt(0), 0, nil, []byte{}), nil
}
func (m *mockBridge) initiateBridgeGas() uint64 { return 0 }
func (m *mockBridge) completeBridgeGas() uint64 { return 0 }

func TestBridgeManager(t *testing.T) {
	setupWithPendingBridges := func(t *testing.T, pendingBridges []*extendedWalletTx) (*bridgeManager, *mockBridge, chan asset.WalletNotification, *tTxDB, dex.Logger) {
		log := dex.StdOutLogger("TEST", dex.LevelDebug)
		notificationChan := make(chan asset.WalletNotification, 10)
		emitter := asset.NewWalletEmitter(notificationChan, BipID, log)

		// Create a mock bridge with default "not ready" behavior
		mb := &mockBridge{
			getCompletionDataFunc: func(ctx context.Context, txID string) ([]byte, error) {
				return nil, errors.New("not ready")
			},
			getCompletionDataCalled: make(chan struct{}, 10),
		}

		db := &tTxDB{}
		db.pendingBridges = pendingBridges
		bm, err := newBridgeManager(context.Background(), BipID, BipID, mb, emitter, db, 100*time.Millisecond, log)
		if err != nil {
			t.Fatalf("error creating bridge manager: %v", err)
		}

		return bm, mb, notificationChan, db, log
	}

	setup := func(t *testing.T) (*bridgeManager, *mockBridge, chan asset.WalletNotification, *tTxDB, dex.Logger) {
		bm, mb, notificationChan, db, log := setupWithPendingBridges(t, nil)
		return bm, mb, notificationChan, db, log
	}

	// Test: Adding and monitoring a pending bridge
	t.Run("AddAndMonitor", func(t *testing.T) {
		bm, mb, _, _, _ := setup(t)

		// Add a pending bridge
		burnTxID := "tx1"
		destAssetID := uint32(123)
		bm.addPendingBridge(burnTxID, destAssetID, 1)

		// Wait for getMintInfo to be called at least twice
		for i := 0; i < 2; i++ {
			select {
			case <-mb.getCompletionDataCalled:
				// Continue
			case <-time.After(1 * time.Second):
				t.Fatalf("timeout waiting for getCompletionData call %d", i+1)
			}
		}
	})

	// Test: Successful mint info retrieval and notification
	t.Run("SuccessfulMintInfo", func(t *testing.T) {
		bm, mb, notificationChan, _, _ := setup(t)

		// Configure getMintInfo to succeed
		completionData := []byte("completionData")
		mb.getCompletionDataFunc = func(ctx context.Context, txID string) ([]byte, error) {
			return completionData, nil
		}

		// Add a pending bridge
		burnTxID := "tx2"
		destAssetID := uint32(60001)
		bm.addPendingBridge(burnTxID, destAssetID, 1)

		// Wait for and verify the notification
		select {
		case note := <-notificationChan:
			bridgeNote, ok := note.(*asset.BridgeReadyToCompleteNote)
			if !ok {
				t.Fatalf("expected *BridgeReadyToCompleteNote, got %T", note)
			}
			if bridgeNote.DestAssetID != destAssetID {
				t.Errorf("expected destAssetID %d, got %d", destAssetID, bridgeNote.DestAssetID)
			}
			if string(bridgeNote.Data) != string(completionData) {
				t.Errorf("expected completionData %q, got %q", completionData, bridgeNote.Data)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for BridgeReadyToComplete notification")
		}
	})

	t.Run("MarkAsComplete", func(t *testing.T) {
		bm, mb, _, db, _ := setup(t)

		// Add a pending bridge
		burnTxID := "tx3"
		destAssetID := uint32(789)
		bm.addPendingBridge(burnTxID, destAssetID, 1)

		db.txToGet = &extendedWalletTx{
			WalletTransaction: &asset.WalletTransaction{
				ID: burnTxID,
				BridgeCounterpartTx: &asset.BridgeCounterpartTx{
					AssetID: destAssetID,
				},
			},
		}

		// Wait for at least one getMintInfo call
		select {
		case <-mb.getCompletionDataCalled:
			// Proceed
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for initial getCompletionData call")
		}

		// Remove the bridge
		bm.markBridgeComplete(burnTxID, "mintTxID", 1)

		// Wait and ensure no more calls occur
		time.Sleep(300 * time.Millisecond) // Longer than 2 monitor intervals
		select {
		case <-mb.getCompletionDataCalled:
			t.Error("getCompletionData was called after removing all pending bridges")
		default:
			// No additional calls, as expected
		}

		if db.storedTx.BridgeCounterpartTx.ID != "mintTxID" {
			t.Errorf("expected BridgeCounterpartTx.ID to be mintTxID, got %s", db.storedTx.BridgeCounterpartTx.ID)
		}
	})

	t.Run("LoadPendingBridges", func(t *testing.T) {
		destAssetID := uint32(123)
		burnTxID := "tx4"
		bm, _, _, _, _ := setupWithPendingBridges(t, []*extendedWalletTx{
			{
				WalletTransaction: &asset.WalletTransaction{
					Type: asset.InitiateBridge,
					ID:   burnTxID,
					BridgeCounterpartTx: &asset.BridgeCounterpartTx{
						AssetID: destAssetID,
					},
				},
			},
		})

		if len(bm.pendingBridges) != 1 {
			t.Fatalf("expected 1 pending bridge, got %d", len(bm.pendingBridges))
		}
		if bm.pendingBridges[burnTxID].destAssetID != destAssetID {
			t.Fatalf("expected pending bridge ID to be %s, and dest asset ID to be %d", burnTxID, destAssetID)
		}
	})
}

func TestCompleteBridge(t *testing.T) {
	initiationTx := &asset.BridgeCounterpartTx{
		ID:      "initiation-tx-id",
		AssetID: 123,
	}
	timestamp := uint64(time.Now().Unix())
	confirmedTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:                  "complete-tx-id",
			Amount:              1e9,
			Confirmed:           true,
			Type:                asset.CompleteBridge,
			BridgeCounterpartTx: initiationTx,
			Timestamp:           timestamp,
		},
		Nonce:     new(big.Int),
		savedToDB: true,
	}
	pendingTx := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			ID:                  "complete-tx-id",
			Amount:              1e9,
			Confirmed:           false,
			Type:                asset.CompleteBridge,
			BridgeCounterpartTx: initiationTx,
			Timestamp:           timestamp,
		},
		Nonce:     new(big.Int),
		savedToDB: true,
	}

	tests := []struct {
		name           string
		dbTx           *extendedWalletTx
		pendingTx      *extendedWalletTx
		dbErr          error
		expectComplete bool
		expectNote     bool
		expectErr      bool
	}{
		{
			name:           "new completion",
			expectComplete: true,
		},
		{
			name:      "pending completion",
			pendingTx: pendingTx,
		},
		{
			name:       "confirmed completion",
			dbTx:       confirmedTx,
			expectNote: true,
		},
		{
			name:      "db error",
			dbErr:     errors.New("db error"),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _, _, shutdown := tassetWallet(BipID)
			defer shutdown()

			bridgeWallet := &ETHBridgeWallet{ETHWallet: w.(*ETHWallet)}
			bridge := &mockBridge{}
			bridgeWallet.manager = &bridgeManager{
				bridge: bridge,
			}

			emitChan := make(chan asset.WalletNotification, 1)
			bridgeWallet.emit = asset.NewWalletEmitter(emitChan, BipID, bridgeWallet.log)

			txDB := &tTxDB{
				txToGet:  tt.dbTx,
				getTxErr: tt.dbErr,
			}
			bridgeWallet.txDB = txDB

			if tt.pendingTx != nil {
				bridgeWallet.pendingTxs = []*extendedWalletTx{tt.pendingTx}
			} else {
				bridgeWallet.pendingTxs = []*extendedWalletTx{}
			}

			err := bridgeWallet.CompleteBridge(context.Background(), initiationTx, 1e9, []byte("completionData"))

			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectComplete != bridge.completeBridgeCalled {
				t.Fatalf("completeBridge called = %v, want %v", bridge.completeBridgeCalled, tt.expectComplete)
			}

			var note *asset.BridgeCompletedNote
		noteLoop:
			for {
				select {
				case n := <-emitChan:
					if n, ok := n.(*asset.BridgeCompletedNote); ok {
						note = n
					}
				default:
					break noteLoop
				}
			}

			if tt.expectNote != (note != nil) {
				t.Fatalf("expected note = %v, got %v", tt.expectNote, (note != nil))
			}

			if note != nil {
				if note.CompletionTime != timestamp {
					t.Fatalf("expected CompletionTime = %d, got %d", timestamp, note.CompletionTime)
				}
				if note.InitiationTxID != initiationTx.ID {
					t.Fatalf("expected InitiationTxID = %s, got %s", initiationTx.ID, note.InitiationTxID)
				}
				if note.CompletionTxID != confirmedTx.ID {
					t.Fatalf("expected CompletionTxID = %s, got %s", confirmedTx.ID, note.CompletionTxID)
				}
				if note.SourceAssetID != initiationTx.AssetID {
					t.Fatalf("expected SourceAssetID = %d, got %d", initiationTx.AssetID, note.SourceAssetID)
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
