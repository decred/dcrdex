// +build !harness
//
// These tests will not be run if the harness build tag is set.

package eth

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
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

func TestDecodeCoinID(t *testing.T) {
	tests := []struct {
		name                   string
		wantFlags              uint16
		wantAddr               common.Address
		coinID, wantSecretHash []byte
		wantErr                bool
	}{{
		name: "ok",
		coinID: []byte{
			0xFF, 0x01, // 2 byte flags
			0x18, 0xd6, 0x5f, 0xb8, 0xd6, 0x0c, 0x11, 0x99, 0xbb,
			0x1a, 0xd3, 0x81, 0xbe, 0x47, 0xaa, 0x69, 0x2b, 0x48,
			0x26, 0x05, // 20 byte addr
			0x71, 0xd8, 0x10, 0xd3, 0x93, 0x33, 0x29, 0x6b, 0x51,
			0x8c, 0x84, 0x6a, 0x3e, 0x49, 0xec, 0xa5, 0x5f, 0x99,
			0x8f, 0xd7, 0x99, 0x49, 0x98, 0xbb, 0x3e, 0x50, 0x48,
			0x56, 0x7f, 0x2f, 0x07, 0x3c, // 32 byte secret hash
		},
		wantFlags: 65281,
		wantAddr: common.Address{
			0x18, 0xd6, 0x5f, 0xb8, 0xd6, 0x0c, 0x11, 0x99, 0xbb,
			0x1a, 0xd3, 0x81, 0xbe, 0x47, 0xaa, 0x69, 0x2b, 0x48,
			0x26, 0x05,
		},
		wantSecretHash: []byte{
			0x71, 0xd8, 0x10, 0xd3, 0x93, 0x33, 0x29, 0x6b, 0x51,
			0x8c, 0x84, 0x6a, 0x3e, 0x49, 0xec, 0xa5, 0x5f, 0x99,
			0x8f, 0xd7, 0x99, 0x49, 0x98, 0xbb, 0x3e, 0x50, 0x48,
			0x56, 0x7f, 0x2f, 0x07, 0x3c, // 32 byte secret hash
		},
	}, {
		name: "wrong length",
		coinID: []byte{
			0xFF, 0x01, // 2 byte flags
			0x18, 0xd6, 0x5f, 0xb8, 0xd6, 0x0c, 0x11, 0x99, 0xbb,
			0x1a, 0xd3, 0x81, 0xbe, 0x47, 0xaa, 0x69, 0x2b, 0x48,
			0x26, 0x05, // 20 byte addr
			0x71, 0xd8, 0x10, 0xd3, 0x93, 0x33, 0x29, 0x6b, 0x51,
			0x8c, 0x84, 0x6a, 0x3e, 0x49, 0xec, 0xa5, 0x5f, 0x99,
			0x8f, 0xd7, 0x99, 0x49, 0x98, 0xbb, 0x3e, 0x50, 0x48,
			0x56, 0x7f, 0x2f, 0x07, // 31 bytes
		},
		wantErr: true,
	}}

	for _, test := range tests {
		flags, addr, secretHash, err := DecodeCoinID(test.coinID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if flags != test.wantFlags {
			t.Fatalf("want flags value of %v but got %v for test %v",
				test.wantFlags, flags, test.name)
		}
		if addr != test.wantAddr {
			t.Fatalf("want addr value of %v but got %v for test %v",
				test.wantAddr, addr, test.name)
		}
		if !reflect.DeepEqual(secretHash, test.wantSecretHash) {
			t.Fatalf("want secret hash value of %v but got %v for test %v",
				test.wantSecretHash, secretHash, test.name)
		}
	}
}

func TestCoinIDToString(t *testing.T) {
	flags := "ff01"
	addr := "18d65fb8d60c1199bb1ad381be47aa692b482605"
	secretHash := "71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
	tests := []struct {
		name, wantCoinID string
		coinID           []byte
		wantErr          bool
	}{{
		name: "ok",
		coinID: []byte{
			0xFF, 0x01, // 2 byte flags
			0x18, 0xd6, 0x5f, 0xb8, 0xd6, 0x0c, 0x11, 0x99, 0xbb,
			0x1a, 0xd3, 0x81, 0xbe, 0x47, 0xaa, 0x69, 0x2b, 0x48,
			0x26, 0x05, // 20 byte addr
			0x71, 0xd8, 0x10, 0xd3, 0x93, 0x33, 0x29, 0x6b, 0x51,
			0x8c, 0x84, 0x6a, 0x3e, 0x49, 0xec, 0xa5, 0x5f, 0x99,
			0x8f, 0xd7, 0x99, 0x49, 0x98, 0xbb, 0x3e, 0x50, 0x48,
			0x56, 0x7f, 0x2f, 0x07, 0x3c, // 32 byte secret hash
		},
		wantCoinID: flags + ":" + addr + ":" + secretHash,
	}, {
		name: "wrong length",
		coinID: []byte{
			0xFF, 0x01, // 2 byte flags
			0x18, 0xd6, 0x5f, 0xb8, 0xd6, 0x0c, 0x11, 0x99, 0xbb,
			0x1a, 0xd3, 0x81, 0xbe, 0x47, 0xaa, 0x69, 0x2b, 0x48,
			0x26, 0x05, // 20 byte addr
			0x71, 0xd8, 0x10, 0xd3, 0x93, 0x33, 0x29, 0x6b, 0x51,
			0x8c, 0x84, 0x6a, 0x3e, 0x49, 0xec, 0xa5, 0x5f, 0x99,
			0x8f, 0xd7, 0x99, 0x49, 0x98, 0xbb, 0x3e, 0x50, 0x48,
			0x56, 0x7f, 0x2f, 0x07, // 31 bytes
		},
		wantErr: true,
	}}

	for _, test := range tests {
		coinID, err := CoinIDToString(test.coinID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if coinID != test.wantCoinID {
			t.Fatalf("want coinID value of %v but got %v for test %v",
				test.wantCoinID, coinID, test.name)
		}
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
		gas:     big.NewInt(gweiFactor - 1),
		wantFee: 0,
	}, {
		name:    "ok one",
		gas:     big.NewInt(gweiFactor),
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
		fee, err := eth.FeeRate()
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
