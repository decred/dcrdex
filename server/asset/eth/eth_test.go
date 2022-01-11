//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const initLocktime = 1632112916

var (
	_            ethFetcher = (*testNode)(nil)
	tLogger                 = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
	initCalldata            = mustParseHex("a8793f94000000000000000000000000000" +
		"0000000000000000000000000000000000020000000000000000000000000000000000" +
		"0000000000000000000000000000002000000000000000000000000000000000000000" +
		"00000000000000000614811148b3e4acc53b664f9cf6fcac0adcd328e95d62ba1f4379" +
		"650ae3e1460a0f9d1a1000000000000000000000000345853e21b1d475582e71cc2691" +
		"24ed5e2dd342200000000000000000000000000000000000000000000000022b1c8c12" +
		"27a0000000000000000000000000000000000000000000000000000000000006148111" +
		"4ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c56100000" +
		"0000000000000000000345853e21b1d475582e71cc269124ed5e2dd342200000000000" +
		"000000000000000000000000000000000000022b1c8c1227a0000")
	/* initCallData parses to:
	[ETHSwapInitiation {
			RefundTimestamp: 1632112916
			SecretHash: 8b3e4acc53b664f9cf6fcac0adcd328e95d62ba1f4379650ae3e1460a0f9d1a1
			Value: 5e9 gwei
			Participant: 0x345853e21b1d475582e71cc269124ed5e2dd3422
		},
	ETHSwapInitiation {
			RefundTimestamp: 1632112916
			SecretHash: ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561
			Value: 5e9 gwei
			Participant: 0x345853e21b1d475582e71cc269124ed5e2dd3422
		}]
	*/
	initSecretHashA     = mustParseHex("8b3e4acc53b664f9cf6fcac0adcd328e95d62ba1f4379650ae3e1460a0f9d1a1")
	initSecretHashB     = mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561")
	initParticipantAddr = common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	redeemCalldata      = mustParseHex("f4fd17f90000000000000000000000000000000000000" +
		"000000000000000000000000020000000000000000000000000000000000000000000000000" +
		"00000000000000022c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965f" +
		"d3e7399d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d654687eac0" +
		"9638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122aebdc4c31b88d0c8f4" +
		"d644591a8e00e92b607f920ad8050deb7c7469767d9c561")
	/*
		redeemCallData parses to:
		[ETHSwapRedemption {
			SecretHash: 99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546
			Secret: 2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73
		}
		ETHSwapRedemption {
			SecretHash: ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561
			Secret: 87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a
		}]
	*/
	redeemSecretHashA = mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546")
	redeemSecretHashB = mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561")
	redeemSecretB     = mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a")
)

func mustParseHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

type testNode struct {
	connectErr     error
	bestHdr        *types.Header
	bestHdrErr     error
	hdrByHeight    *types.Header
	hdrByHeightErr error
	blkNum         uint64
	blkNumErr      error
	syncProg       *ethereum.SyncProgress
	syncProgErr    error
	sugGasPrice    *big.Int
	sugGasPriceErr error
	swp            *dexeth.SwapState
	swpErr         error
	tx             *types.Transaction
	txIsMempool    bool
	txErr          error
	acctBal        *big.Int
	acctBalErr     error
}

func (n *testNode) connect(ctx context.Context, ipc string, contractAddr *common.Address) error {
	return n.connectErr
}

func (n *testNode) shutdown() {}

func (n *testNode) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.bestHdr, n.bestHdrErr
}

func (n *testNode) headerByHeight(ctx context.Context, height uint64) (*types.Header, error) {
	return n.hdrByHeight, n.hdrByHeightErr
}

func (n *testNode) blockNumber(ctx context.Context) (uint64, error) {
	return n.blkNum, n.blkNumErr
}

func (n *testNode) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return n.syncProg, n.syncProgErr
}

func (n *testNode) suggestGasPrice(ctx context.Context) (*big.Int, error) {
	return n.sugGasPrice, n.sugGasPriceErr
}

func (n *testNode) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	return n.swp, n.swpErr
}

func (n *testNode) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return n.tx, n.txIsMempool, n.txErr
}

func (n *testNode) accountBalance(ctx context.Context, addr common.Address) (*big.Int, error) {
	return n.acctBal, n.acctBalErr
}

func tSwap(bn, locktime int64, value uint64, secret [32]byte, state dexeth.SwapStep, participantAddr *common.Address) *dexeth.SwapState {
	return &dexeth.SwapState{
		Secret:      secret,
		BlockHeight: uint64(bn),
		LockTime:    time.Unix(locktime, 0),
		Participant: *participantAddr,
		State:       state,
		Value:       value,
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name, ipc, wantIPC string
		network            dex.Network
		wantErr            bool
	}{{
		name:    "ok ipc supplied",
		ipc:     "/home/john/bleh.ipc",
		wantIPC: "/home/john/bleh.ipc",
		network: dex.Simnet,
	}, {
		name:    "ok ipc not supplied",
		ipc:     "",
		wantIPC: defaultIPC,
		network: dex.Simnet,
	}, {
		name:    "mainnet not allowed",
		ipc:     "",
		wantIPC: defaultIPC,
		network: dex.Mainnet,
		wantErr: true,
	}}

	for _, test := range tests {
		cfg, err := load(test.ipc, test.network)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if cfg.ipc != test.wantIPC {
			t.Fatalf("want ipc value of %v but got %v for test %v", test.wantIPC, cfg.ipc, test.name)
		}
	}
}

func TestDecodeCoinID(t *testing.T) {
	drv := &Driver{}
	txid := "0x1b86600b740d58ecc06eda8eba1c941c7ba3d285c78be89b56678da146ed53d1"
	txHashB := mustParseHex("1b86600b740d58ecc06eda8eba1c941c7ba3d285c78be89b56678da146ed53d1")

	type test struct {
		name    string
		input   []byte
		wantErr bool
		expRes  string
	}

	tests := []test{{
		name:   "ok",
		input:  txHashB,
		expRes: txid,
	}, {
		name:    "too short",
		input:   txHashB[:len(txHashB)/2],
		wantErr: true,
	}, {
		name:    "too long",
		input:   append(txHashB, txHashB...),
		wantErr: true,
	}}

	for _, tt := range tests {
		res, err := drv.DecodeCoinID(tt.input)
		if err != nil {
			if !tt.wantErr {
				t.Fatalf("%s: error: %v", tt.name, err)
			}
			continue
		}

		if tt.wantErr {
			t.Fatalf("%s: no error", tt.name)
		}
		if res != tt.expRes {
			t.Fatalf("%s: wrong result. wanted %s, got %s", tt.name, tt.expRes, res)
		}
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	backend, err := unconnectedETH(tLogger, &config{network: dex.Simnet})
	if err != nil {
		t.Fatalf("unconnectedETH error: %v", err)
	}
	backend.node = &testNode{
		bestHdr: &types.Header{Number: big.NewInt(1)},
	}
	ch := backend.BlockChannel(1)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-time.After(blockPollInterval * 2):
		}
	}()
	backend.run(ctx)
	// Ok if ctx was canceled above. Linters complain about calling t.Fatal
	// in the goroutine above.
	select {
	case <-ctx.Done():
		return
	default:
		t.Fatal("test timeout")
	}
}

func TestFeeRate(t *testing.T) {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(dexeth.GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	overMaxWei.Add(overMaxWei, gweiFactorBig)
	tests := []struct {
		name    string
		gas     *big.Int
		gasErr  error
		wantFee uint64
		wantErr bool
	}{{
		name:    "ok zero",
		gas:     new(big.Int),
		wantFee: 0,
	}, {
		name:    "ok rounded down",
		gas:     big.NewInt(dexeth.GweiFactor - 1),
		wantFee: 0,
	}, {
		name:    "ok one",
		gas:     big.NewInt(dexeth.GweiFactor),
		wantFee: 1,
	}, {
		name:    "ok max int",
		gas:     maxWei,
		wantFee: maxInt,
	}, {
		name:    "over max int",
		gas:     overMaxWei,
		wantErr: true,
	}, {
		name:    "node suggest gas fee error",
		gas:     new(big.Int),
		gasErr:  errors.New(""),
		wantErr: true,
	}}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{
			sugGasPrice:    test.gas,
			sugGasPriceErr: test.gasErr,
		}
		eth := &Backend{
			node:   node,
			rpcCtx: ctx,
			log:    tLogger,
		}
		fee, err := eth.FeeRate(ctx)
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
		if fee != test.wantFee {
			t.Fatalf("want fee %v got %v for test %q", test.wantFee, fee, test.name)
		}
	}
}

func TestSynced(t *testing.T) {
	tests := []struct {
		name                    string
		syncProg                *ethereum.SyncProgress
		subSecs                 uint64
		bestHdrErr, syncProgErr error
		wantErr, wantSynced     bool
	}{{
		name:       "ok synced",
		wantSynced: true,
	}, {
		name:     "ok syncing",
		syncProg: new(ethereum.SyncProgress),
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
		eth := &Backend{
			node:   node,
			rpcCtx: ctx,
			log:    tLogger,
		}
		synced, err := eth.Synced()
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
	}
}

// TestRequiredOrderFunds ensures that a fee calculation in the calc package
// will come up with the correct required funds.
func TestRequiredOrderFunds(t *testing.T) {
	eth := &Backend{
		initTxSize: uint32(dexeth.InitGas(1, 0)),
	}
	swapVal := uint64(1000000000)                // gwei
	numSwaps := uint64(17)                       // swaps
	initSizeBase := uint64(eth.InitTxSizeBase()) // 0 gas
	initSize := uint64(eth.InitTxSize())         // init value gas
	feeRate := uint64(30)                        // gwei / gas

	// We want the fee calculation to simply be the cost of the gas used
	// for each swap plus the initial value.
	want := swapVal + (numSwaps * initSize * feeRate)
	nfo := &dex.Asset{
		SwapSizeBase: initSizeBase,
		SwapSize:     initSize,
		MaxFeeRate:   feeRate,
	}

	// Second argument called inputsSize same as another initSize.
	got := calc.RequiredOrderFunds(swapVal, 0, numSwaps, nfo)
	if got != want {
		t.Fatalf("want %v got %v for fees", want, got)
	}
}

func tTx(gasPrice, value uint64, to *common.Address, data []byte) *types.Transaction {
	return types.NewTx(&types.LegacyTx{
		GasPrice: dexeth.GweiToWei(gasPrice),
		To:       to,
		Value:    dexeth.GweiToWei(value),
		Data:     data,
	})
}

func TestContract(t *testing.T) {
	receiverAddr, contractAddr := new(common.Address), new(common.Address)
	copy(receiverAddr[:], encode.RandomBytes(20))
	copy(contractAddr[:], encode.RandomBytes(20))
	var txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	const gasPrice = 30
	const swapVal = 25e8
	const txVal = 5e9
	var secret, secretHash [32]byte
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	tests := []struct {
		name           string
		coinID         []byte
		contract       []byte
		tx             *types.Transaction
		swap           *dexeth.SwapState
		swapErr, txErr error
		wantErr        bool
	}{{
		name:     "ok",
		tx:       tTx(gasPrice, txVal, contractAddr, initCalldata),
		contract: dexeth.EncodeContractData(0, secretHash),
		swap:     tSwap(97, initLocktime, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
		coinID:   txHash[:],
	}, {
		name:     "new coiner error, wrong tx type",
		tx:       tTx(gasPrice, txVal, contractAddr, initCalldata),
		contract: dexeth.EncodeContractData(0, secretHash),
		swap:     tSwap(97, initLocktime, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
		coinID:   txHash[1:],
		wantErr:  true,
	}, {
		name:     "confirmations error, swap error",
		tx:       tTx(gasPrice, txVal, contractAddr, initCalldata),
		contract: dexeth.EncodeContractData(0, secretHash),
		coinID:   txHash[:],
		swapErr:  errors.New(""),
		wantErr:  true,
	}}
	for _, test := range tests {
		node := &testNode{
			tx:     test.tx,
			txErr:  test.txErr,
			swp:    test.swap,
			swpErr: test.swapErr,
		}
		eth := &Backend{
			node:         node,
			log:          tLogger,
			contractAddr: *contractAddr,
		}
		contractData := dexeth.EncodeContractData(0, secretHash) // matches initCalldata
		contract, err := eth.Contract(test.coinID, contractData)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if contract.SwapAddress != initParticipantAddr.String() ||
			contract.LockTime.Unix() != initLocktime {
			t.Fatalf("returns do not match expected for test %q", test.name)
		}
	}
}

func TestValidateSecret(t *testing.T) {
	secret, blankHash := make([]byte, 32), make([]byte, 32)
	copy(secret[:], encode.RandomBytes(32))
	secretHash := sha256.Sum256(secret[:])
	tests := []struct {
		name       string
		secretHash []byte
		want       bool
	}{{
		name:       "ok",
		secretHash: secretHash[:],
		want:       true,
	}, {
		name:       "not the right hash",
		secretHash: blankHash,
	}}
	for _, test := range tests {
		eth := &Backend{
			log: tLogger,
		}
		got := eth.ValidateSecret(secret, test.secretHash)
		if test.want != got {
			t.Fatalf("expected %v but got %v for test %q", test.want, got, test.name)
		}
	}
}

func TestRedemption(t *testing.T) {
	receiverAddr, contractAddr := new(common.Address), new(common.Address)
	copy(receiverAddr[:], encode.RandomBytes(20))
	copy(contractAddr[:], encode.RandomBytes(20))
	var secret, secretHash, txHash [32]byte
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	copy(txHash[:], encode.RandomBytes(32))
	const gasPrice = 30
	tests := []struct {
		name               string
		coinID, contractID []byte
		swp                *dexeth.SwapState
		tx                 *types.Transaction
		txIsMempool        bool
		swpErr, txErr      error
		wantErr            bool
	}{{
		name:       "ok",
		tx:         tTx(gasPrice, 0, contractAddr, redeemCalldata),
		contractID: dexeth.EncodeContractData(0, secretHash),
		coinID:     txHash[:],
		swp:        tSwap(0, 0, 0, secret, dexeth.SSRedeemed, receiverAddr),
	}, {
		name:       "new coiner error, wrong tx type",
		tx:         tTx(gasPrice, 0, contractAddr, redeemCalldata),
		contractID: dexeth.EncodeContractData(0, secretHash),
		coinID:     txHash[1:],
		wantErr:    true,
	}, {
		name:       "confirmations error, swap wrong state",
		tx:         tTx(gasPrice, 0, contractAddr, redeemCalldata),
		contractID: dexeth.EncodeContractData(0, secretHash),
		swp:        tSwap(0, 0, 0, secret, dexeth.SSRefunded, receiverAddr),
		coinID:     txHash[:],
		wantErr:    true,
	}, {
		name:       "validate redeem error",
		tx:         tTx(gasPrice, 0, contractAddr, redeemCalldata),
		contractID: secretHash[:31],
		coinID:     txHash[:],
		swp:        tSwap(0, 0, 0, secret, dexeth.SSRedeemed, receiverAddr),
		wantErr:    true,
	}}
	for _, test := range tests {
		node := &testNode{
			tx:          test.tx,
			txIsMempool: test.txIsMempool,
			txErr:       test.txErr,
			swp:         test.swp,
			swpErr:      test.swpErr,
		}
		eth := &Backend{
			node:         node,
			log:          tLogger,
			contractAddr: *contractAddr,
		}
		_, err := eth.Redemption(test.coinID, nil, test.contractID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
	}
}

func TestTxData(t *testing.T) {
	node := &testNode{}
	eth := &Backend{
		node: node,
	}
	const gasPrice = 30
	const value = 5e9
	addr := randomAddress()
	data := encode.RandomBytes(5)
	tx := tTx(gasPrice, value, addr, data)
	goodCoinID, _ := hex.DecodeString("09c3bed75b35c6cf0549b0636c9511161b18765c019ef371e2a9f01e4b4a1487")
	node.tx = tx

	// initial success
	txData, err := eth.TxData(goodCoinID)
	if err != nil {
		t.Fatalf("TxData error: %v", err)
	}
	checkB, _ := tx.MarshalBinary()
	if !bytes.Equal(txData, checkB) {
		t.Fatalf("tx data not transmitted")
	}

	// bad coin ID
	coinID := encode.RandomBytes(2)
	_, err = eth.TxData(coinID)
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	// Wrong type of coin ID
	_, err = eth.TxData(goodCoinID[2:])
	if err == nil {
		t.Fatalf("no error for wrong coin type")
	}

	// No transaction
	node.tx = nil
	_, err = eth.TxData(goodCoinID)
	if err == nil {
		t.Fatalf("no error for missing tx")
	}

	// Success again
	node.tx = tx
	_, err = eth.TxData(goodCoinID)
	if err != nil {
		t.Fatalf("TxData error: %v", err)
	}
}

func TestValidateContract(t *testing.T) {
	tests := []struct {
		name       string
		ver        uint32
		secretHash []byte
		wantErr    bool
	}{{
		name:       "ok",
		secretHash: make([]byte, dexeth.SecretHashSize),
	}, {
		name:       "wrong size",
		secretHash: make([]byte, dexeth.SecretHashSize-1),
		wantErr:    true,
	}, {
		name:       "wrong version",
		ver:        1,
		secretHash: make([]byte, dexeth.SecretHashSize),
		wantErr:    true,
	}}
	for _, test := range tests {
		eth := new(Backend)
		swapData := make([]byte, 4+len(test.secretHash))
		binary.BigEndian.PutUint32(swapData[:4], test.ver)
		copy(swapData[4:], test.secretHash)
		err := eth.ValidateContract(swapData)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
	}
}

func TestAccountBalance(t *testing.T) {
	node := &testNode{}
	eth := &Backend{node: node}

	const gweiBal = 1e9
	bigBal := big.NewInt(gweiBal)
	node.acctBal = bigBal.Mul(bigBal, big.NewInt(dexeth.GweiFactor))

	// Initial success
	bal, err := eth.AccountBalance("")
	if err != nil {
		t.Fatalf("AccountBalance error: %v", err)
	}

	if bal != gweiBal {
		t.Fatalf("wrong balance. expected %f, got %d", gweiBal, bal)
	}

	// Only error path.
	node.acctBalErr = errors.New("test error")
	_, err = eth.AccountBalance("")
	if err == nil {
		t.Fatalf("no AccountBalance error when expected")
	}
	node.acctBalErr = nil

	// Success again
	_, err = eth.AccountBalance("")
	if err != nil {
		t.Fatalf("AccountBalance error: %v", err)
	}
}

func TestPoll(t *testing.T) {
	blkHdr := &types.Header{Number: big.NewInt(0)}
	tests := []struct {
		name                       string
		bestHdr, hdrByHeight       *types.Header
		bestHdrErr, hdrByHeightErr error
		wantErr, preventSend       bool
	}{{
		name:    "ok nothing to do",
		bestHdr: blkHdr,
	}, {
		name: "ok sequential",
		bestHdr: &types.Header{
			Number:     big.NewInt(1),
			ParentHash: blkHdr.Hash(),
		},
	}, {
		name: "ok fast blocks",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
		hdrByHeight: blkHdr,
	}, {
		name: "ok reorg",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
	}, {
		name: "ok but cannot send",
		bestHdr: &types.Header{
			Number:     big.NewInt(1),
			ParentHash: blkHdr.Hash(),
		},
		preventSend: true,
	}, {
		name:       "best header error",
		bestHdrErr: errors.New(""),
		wantErr:    true,
	}, {
		name: "header by height error",
		bestHdr: &types.Header{
			Number: big.NewInt(1),
		},
		hdrByHeightErr: errors.New(""),
		wantErr:        true,
	}}

	for _, test := range tests {
		node := &testNode{
			bestHdr:        test.bestHdr,
			bestHdrErr:     test.bestHdrErr,
			hdrByHeight:    test.hdrByHeight,
			hdrByHeightErr: test.hdrByHeightErr,
		}
		eth := Backend{
			log:        tLogger,
			blockChans: make(map[chan *asset.BlockUpdate]struct{}),
			node:       node,
			bestHash: hashN{
				hash: blkHdr.Hash(),
			},
		}
		chSize := 1
		if test.preventSend {
			chSize = 0
		}
		ch := make(chan *asset.BlockUpdate, chSize)
		eth.blockChans[ch] = struct{}{}
		bu := new(asset.BlockUpdate)
		wait := make(chan struct{})
		go func() {
			if test.preventSend {
				close(wait)
				return
			}
			select {
			case bu = <-ch:
			case <-time.After(time.Second * 2):
			}
			close(wait)
		}()
		eth.poll(nil)
		<-wait
		if test.wantErr {
			if bu.Err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if bu.Err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, bu.Err)
		}
	}
}

func TestValidateSignature(t *testing.T) {
	// "ok" values used are the same as tests in client/assets/eth.
	pkBytes := mustParseHex("04b911d1f39f7792e165767e35aa134083e2f70ac7de6945d7641a3015d09a54561b71112b8d60f63831f0e62c23c6921ec627820afedf8236155b9e9bd82b6523")
	msg := []byte("msg")
	sigBytes := mustParseHex("ffd26911d3fdaf11ac44801744f2df015a16539b6e688aff4cabc092b747466e7bc8036a03d1479a1570dd11bf042120301c34a65b237267720ef8a9e56f2eb1")
	max32Bytes := mustParseHex("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	addr := "0x2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27"
	eth := new(Backend)

	tests := []struct {
		name                   string
		wantErr                bool
		pkBytes, sigBytes, msg []byte
		addr                   string
	}{{
		name:     "ok",
		pkBytes:  pkBytes,
		msg:      msg,
		addr:     addr,
		sigBytes: sigBytes,
	}, {
		name:    "sig wrong size",
		pkBytes: pkBytes,
		msg:     msg,
		addr:    addr,
		wantErr: true,
	}, {
		name:     "pubkey doesn't match address",
		pkBytes:  pkBytes,
		msg:      msg,
		addr:     addr[:21] + "a",
		sigBytes: sigBytes,
		wantErr:  true,
	}, {
		name:     "bad pubkey",
		pkBytes:  pkBytes[1:],
		msg:      msg,
		sigBytes: sigBytes,
		addr:     addr,
		wantErr:  true,
	}, {
		name:     "r too big",
		pkBytes:  pkBytes,
		msg:      msg,
		sigBytes: append(append([]byte{}, max32Bytes...), sigBytes[32:]...),
		addr:     addr,
		wantErr:  true,
	}, {
		name:     "s too big",
		pkBytes:  pkBytes,
		msg:      msg,
		sigBytes: append(append(append([]byte{}, sigBytes[:32]...), max32Bytes...), byte(1)),
		addr:     addr,
		wantErr:  true,
	}, {
		name:     "cannot verify signature, bad msg",
		pkBytes:  pkBytes,
		sigBytes: sigBytes,
		addr:     addr,
		wantErr:  true,
	}}

	for _, test := range tests {
		err := eth.ValidateSignature(test.addr, test.pkBytes, test.msg, test.sigBytes)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
	}
}
