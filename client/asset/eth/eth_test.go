// +build !harness
//
// These tests will not be run if the harness build tag is set.

package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
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
}

func (n *testNode) connect(ctx context.Context, node *node.Node, addr common.Address) error {
	return n.connectErr
}
func (n *testNode) shutdown() {}
func (n *testNode) bestBlockHash(ctx context.Context) (common.Hash, error) {
	return n.bestBlkHash, n.bestBlkHashErr
}
func (n *testNode) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return n.blk, n.blkErr
}
func (n *testNode) accounts() []*accounts.Account {
	return nil
}
func (n *testNode) balance(ctx context.Context, acct *accounts.Account) (*big.Int, error) {
	return nil, nil
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
func (n *testNode) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, nil
}
func (n *testNode) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	return n.peerInfo, n.peersErr
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
		name                  string
		syncProg              *ethereum.SyncProgress
		peerInfo              []*p2p.PeerInfo
		peersErr, syncProgErr error
		wantErr, wantSynced   bool
		wantRatio             float32
	}{{
		name:       "ok synced",
		peerInfo:   make([]*p2p.PeerInfo, 2),
		wantRatio:  1,
		wantSynced: true,
	}, {
		name:       "ok syncing",
		peerInfo:   make([]*p2p.PeerInfo, 2),
		syncProg:   fourthSyncProg,
		wantRatio:  0.25,
		wantSynced: false,
	}, {
		name:     "ok not enough peers",
		peerInfo: make([]*p2p.PeerInfo, 1),
	}, {
		name:     "peers error",
		peersErr: errors.New(""),
		wantErr:  true,
	}, {
		name:        "sync progress error",
		peerInfo:    make([]*p2p.PeerInfo, 2),
		syncProgErr: errors.New(""),
		wantErr:     true,
	}}

	for _, test := range tests {
		ctx, cancel := context.WithCancel(context.Background())
		node := &testNode{
			syncProg:    test.syncProg,
			syncProgErr: test.syncProgErr,
			peerInfo:    test.peerInfo,
			peersErr:    test.peersErr,
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
