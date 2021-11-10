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
	swap "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
)

var (
	_            ethFetcher = (*testNode)(nil)
	tLogger                 = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
	initCalldata            = mustParseHex("ae0521470000000000000000000000000000000000" +
		"000000000000000000000061481114ebdc4c31b88d0c8f4d644591a8e00" +
		"e92b607f920ad8050deb7c7469767d9c561000000000000000000000000" +
		"345853e21b1d475582e71cc269124ed5e2dd3422")
	redeemCalldata = mustParseHex("b31597ad87eac09638c0c38b4e735b79f053" +
		"cb869167ee770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d64459" +
		"1a8e00e92b607f920ad8050deb7c7469767d9c561")
	initParticipantAddr = common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	initLocktime        = int64(1632112916)
	secretHashSlice     = mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561")
	secretSlice         = mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a")
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
	bestBlkHash    common.Hash
	bestBlkHashErr error
	blk            *types.Block
	blkErr         error
	bestHdr        *types.Header
	bestHdrErr     error
	blkNum         uint64
	blkNumErr      error
	syncProg       *ethereum.SyncProgress
	syncProgErr    error
	sugGasPrice    *big.Int
	sugGasPriceErr error
	peerInfo       []*p2p.PeerInfo
	peersErr       error
	swp            *swap.ETHSwapSwap
	swpErr         error
	tx             *types.Transaction
	txIsMempool    bool
	txErr          error
}

func (n *testNode) connect(ctx context.Context, ipc string, contractAddr *common.Address) error {
	return n.connectErr
}

func (n *testNode) shutdown() {}

func (n *testNode) bestBlockHash(ctx context.Context) (common.Hash, error) {
	return n.bestBlkHash, n.bestBlkHashErr
}

func (n *testNode) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.bestHdr, n.bestHdrErr
}

func (n *testNode) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return n.blk, n.blkErr
}

func (n *testNode) blockNumber(ctx context.Context) (uint64, error) {
	return n.blkNum, n.blkNumErr
}

func (n *testNode) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return n.syncProg, n.syncProgErr
}

func (n *testNode) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	return n.peerInfo, n.peersErr
}

func (n *testNode) suggestGasPrice(ctx context.Context) (*big.Int, error) {
	return n.sugGasPrice, n.sugGasPriceErr
}

func (n *testNode) swap(ctx context.Context, secretHash [32]byte) (*swap.ETHSwapSwap, error) {
	return n.swp, n.swpErr
}

func (n *testNode) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return n.tx, n.txIsMempool, n.txErr
}

func tSwap(bn int64, locktime, value *big.Int, state SwapState, participantAddr *common.Address) *swap.ETHSwapSwap {
	return &swap.ETHSwapSwap{
		InitBlockNumber:      big.NewInt(bn),
		RefundBlockTimestamp: locktime,
		Participant:          *participantAddr,
		State:                uint8(state),
		Value:                value,
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

func TestCoinIDs(t *testing.T) {
	// Decode and encode TxCoinID
	var txID [32]byte
	copy(txID[:], encode.RandomBytes(32))
	originalTxCoin := TxCoinID{
		TxID: txID,
	}
	encodedTxCoin := originalTxCoin.Encode()
	decodedCoin, err := DecodeCoinID(encodedTxCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding tx coin: %v", err)
	}
	decodedTxCoin, ok := decodedCoin.(*TxCoinID)
	if !ok {
		t.Fatalf("expected coin to be a TxCoin")
	}
	if !bytes.Equal(originalTxCoin.TxID[:], decodedTxCoin.TxID[:]) {
		t.Fatalf("expected txIds to be equal before and after decoding")
	}

	// Decode tx coin id with incorrect length
	txCoinID := make([]byte, 33)
	binary.BigEndian.PutUint16(txCoinID[:2], uint16(CIDTxID))
	copy(txCoinID[2:], encode.RandomBytes(30))
	if _, err := DecodeCoinID(txCoinID); err == nil {
		t.Fatalf("expected error decoding tx coin ID with incorrect length")
	}

	// Decode and encode SwapCoinID
	var contractAddress [20]byte
	var secretHash [32]byte
	copy(contractAddress[:], encode.RandomBytes(20))
	copy(secretHash[:], encode.RandomBytes(32))
	originalSwapCoin := SwapCoinID{
		ContractAddress: contractAddress,
		SecretHash:      secretHash,
	}
	encodedSwapCoin := originalSwapCoin.Encode()
	decodedCoin, err = DecodeCoinID(encodedSwapCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding swap coin: %v", err)
	}
	decodedSwapCoin, ok := decodedCoin.(*SwapCoinID)
	if !ok {
		t.Fatalf("expected coin to be a SwapCoinID")
	}
	if !bytes.Equal(originalSwapCoin.ContractAddress[:], decodedSwapCoin.ContractAddress[:]) {
		t.Fatalf("expected contract address to be equal before and after decoding")
	}
	if !bytes.Equal(originalSwapCoin.SecretHash[:], decodedSwapCoin.SecretHash[:]) {
		t.Fatalf("expected secret hash to be equal before and after decoding")
	}

	// Decode swap coin id with incorrect length
	swapCoinID := make([]byte, 53)
	binary.BigEndian.PutUint16(swapCoinID[:2], uint16(CIDSwap))
	copy(swapCoinID[2:], encode.RandomBytes(50))
	if _, err := DecodeCoinID(swapCoinID); err == nil {
		t.Fatalf("expected error decoding swap coin ID with incorrect length")
	}

	// Decode and encode AmountCoinID
	var address [20]byte
	var nonce [8]byte
	copy(address[:], encode.RandomBytes(20))
	copy(nonce[:], encode.RandomBytes(8))
	originalAmountCoin := AmountCoinID{
		Address: address,
		Amount:  100,
		Nonce:   nonce,
	}
	encodedAmountCoin := originalAmountCoin.Encode()
	decodedCoin, err = DecodeCoinID(encodedAmountCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding swap coin: %v", err)
	}
	decodedAmountCoin, ok := decodedCoin.(*AmountCoinID)
	if !ok {
		t.Fatalf("expected coin to be a AmounCoinID")
	}
	if !bytes.Equal(originalAmountCoin.Address[:], decodedAmountCoin.Address[:]) {
		t.Fatalf("expected address to be equal before and after decoding")
	}
	if !bytes.Equal(originalAmountCoin.Nonce[:], decodedAmountCoin.Nonce[:]) {
		t.Fatalf("expected nonce to be equal before and after decoding")
	}
	if originalAmountCoin.Amount != decodedAmountCoin.Amount {
		t.Fatalf("expected amount to be equal before and after decoding")
	}

	// Decode amount coin id with incorrect length
	amountCoinId := make([]byte, 37)
	binary.BigEndian.PutUint16(amountCoinId[:2], uint16(CIDAmount))
	copy(amountCoinId[2:], encode.RandomBytes(35))
	if _, err := DecodeCoinID(amountCoinId); err == nil {
		t.Fatalf("expected error decoding amount coin ID with incorrect length")
	}

	// Decode coin id with non existant flag
	nonExistantCoinID := make([]byte, 37)
	binary.BigEndian.PutUint16(nonExistantCoinID[:2], uint16(5))
	copy(nonExistantCoinID, encode.RandomBytes(35))
	if _, err := DecodeCoinID(nonExistantCoinID); err == nil {
		t.Fatalf("expected error decoding coin id with non existant flag")
	}
}

func TestRun(t *testing.T) {
	// TODO: Test all paths.
	ctx, cancel := context.WithCancel(context.Background())
	header1 := &types.Header{Number: big.NewInt(1)}
	block1 := types.NewBlockWithHeader(header1)
	blockHash1 := block1.Hash()
	node := &testNode{
		bestBlkHash: blockHash1,
		blk:         block1,
	}

	backend := unconnectedETH(tLogger, new(config))
	ch := backend.BlockChannel(1)
	backend.node = node
	go func() {
		<-ch
		cancel()
	}()
	backend.run(ctx)
	backend.blockCache.mtx.Lock()
	best := backend.blockCache.best
	backend.blockCache.mtx.Unlock()
	if best.hash != blockHash1 {
		t.Fatalf("want header hash %x but got %x", blockHash1, best.hash)
	}
	cancel()
}

func TestFeeRate(t *testing.T) {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(GweiFactor)
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
		gas:     big.NewInt(0),
		wantFee: 0,
	}, {
		name:    "ok rounded down",
		gas:     big.NewInt(GweiFactor - 1),
		wantFee: 0,
	}, {
		name:    "ok one",
		gas:     big.NewInt(GweiFactor),
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
		gas:     big.NewInt(0),
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
		subSecs: MaxBlockInterval,
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
	eth := new(Backend)
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
	got := calc.RequiredOrderFunds(swapVal, initSize, numSwaps, nfo)
	if got != want {
		t.Fatalf("want %v got %v for fees", want, got)
	}
}

func tTx(gasPrice, value *big.Int, to *common.Address, data []byte) *types.Transaction {
	return types.NewTx(&types.LegacyTx{
		GasPrice: gasPrice,
		To:       to,
		Value:    value,
		Data:     data,
	})
}

func TestContract(t *testing.T) {
	receiverAddr, contractAddr := new(common.Address), new(common.Address)
	copy(receiverAddr[:], encode.RandomBytes(20))
	copy(contractAddr[:], encode.RandomBytes(20))
	var txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	tc := TxCoinID{
		TxID: txHash,
	}
	txCoinIDBytes := tc.Encode()
	sc := SwapCoinID{}
	swapCoinIDBytes := sc.Encode()
	locktime := big.NewInt(initLocktime)
	tests := []struct {
		name           string
		coinID         []byte
		tx             *types.Transaction
		swap           *dexeth.ETHSwapSwap
		swapErr, txErr error
		wantErr        bool
	}{{
		name:   "ok",
		tx:     tTx(gasPrice, value, contractAddr, initCalldata),
		swap:   tSwap(97, locktime, value, SSInitiated, &initParticipantAddr),
		coinID: txCoinIDBytes,
	}, {
		name:    "new coiner error, wrong tx type",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		swap:    tSwap(97, locktime, value, SSInitiated, &initParticipantAddr),
		coinID:  swapCoinIDBytes,
		wantErr: true,
	}, {
		name:    "confirmations error, swap error",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		swapErr: errors.New(""),
		wantErr: true,
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
		contract, err := eth.Contract(test.coinID, nil)
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
			contract.LockTime.Unix() != initLocktime/1000 {
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
	secretHash, txHash := [32]byte{}, make([]byte, 32)
	copy(secretHash[:], secretHashSlice)
	copy(txHash[:], encode.RandomBytes(32))
	gasPrice := big.NewInt(3e10)
	bigO := big.NewInt(0)
	ccID := &SwapCoinID{
		SecretHash:      secretHash,
		ContractAddress: *contractAddr,
	}
	tests := []struct {
		name               string
		coinID, contractID []byte
		swp                *swap.ETHSwapSwap
		tx                 *types.Transaction
		txIsMempool        bool
		swpErr, txErr      error
		wantErr            bool
	}{{
		name:       "ok",
		tx:         tTx(gasPrice, bigO, contractAddr, redeemCalldata),
		contractID: ccID.Encode(),
		coinID:     new(TxCoinID).Encode(),
		swp:        tSwap(0, bigO, bigO, SSRedeemed, receiverAddr),
	}, {
		name:       "new coiner error, wrong tx type",
		tx:         tTx(gasPrice, bigO, contractAddr, redeemCalldata),
		contractID: ccID.Encode(),
		coinID:     new(SwapCoinID).Encode(),
		wantErr:    true,
	}, {
		name:       "confirmations error, swap wrong state",
		tx:         tTx(gasPrice, bigO, contractAddr, redeemCalldata),
		contractID: ccID.Encode(),
		swp:        tSwap(0, bigO, bigO, SSRefunded, receiverAddr),
		coinID:     new(TxCoinID).Encode(),
		wantErr:    true,
	}, {
		name:       "validate redeem error",
		tx:         tTx(gasPrice, bigO, contractAddr, redeemCalldata),
		contractID: new(SwapCoinID).Encode(),
		coinID:     new(TxCoinID).Encode(),
		swp:        tSwap(0, bigO, bigO, SSRedeemed, receiverAddr),
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
		_, err := eth.Redemption(test.coinID, test.contractID)
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
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	addr := randomAddress()
	data := encode.RandomBytes(5)
	tx := tTx(gasPrice, value, addr, data)
	goodCoinID := (&TxCoinID{TxID: tx.Hash()}).Encode()
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
	coinID = (&SwapCoinID{}).Encode()
	_, err = eth.TxData(coinID)
	if err == nil {
		t.Fatalf("no error for wrong coin type")
	}

	// No transaction
	_, err = eth.TxData(coinID)
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
