//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
)

var (
	_       ethFetcher = (*testNode)(nil)
	tLogger            = dex.StdOutLogger("ETHTEST", dex.LevelTrace)
)

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
}

func (n *testNode) connect(ctx context.Context, IPC string) error {
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

func TestLoad(t *testing.T) {
	tests := []struct {
		name, IPC, wantIPC string
		network            dex.Network
		wantErr            bool
	}{{
		name:    "ok ipc supplied",
		IPC:     "/home/john/bleh.ipc",
		wantIPC: "/home/john/bleh.ipc",
		network: dex.Simnet,
	}, {
		name:    "ok ipc not supplied",
		IPC:     "",
		wantIPC: defaultIPC,
		network: dex.Simnet,
	}, {
		name:    "mainnet not allowed",
		IPC:     "",
		wantIPC: defaultIPC,
		network: dex.Mainnet,
		wantErr: true,
	}}

	for _, test := range tests {
		cfg, err := load(test.IPC, test.network)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if cfg.IPC != test.wantIPC {
			t.Fatalf("want ipc value of %v but got %v for test %v", test.wantIPC, cfg.IPC, test.name)
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
	backend := unconnectedETH(tLogger, nil)
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
