// +build !harness
//
// These tests will not be run if the harness build tag is set.

package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
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
}

func (n *testNode) connect(ctx context.Context, node *node.Node, addr common.Address) error {
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
func (n *testNode) balance(ctx context.Context, acct *accounts.Account) (*big.Int, error) {
	return n.bal, n.balErr
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
func (n *testNode) initiate(opts *bind.TransactOpts, netID int64, refundTimestamp int64, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
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
