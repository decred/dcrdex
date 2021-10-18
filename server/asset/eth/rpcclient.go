// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"fmt"
	"math/big"

	swap "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var _ ethFetcher = (*rpcclient)(nil)

type rpcclient struct {
	// ec wraps a *rpc.Client with some useful calls.
	ec *ethclient.Client
	// c is a direct client for raw calls.
	c *rpc.Client
	// es is a wrapper for contract calls.
	es *swap.ETHSwap
}

// connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easil use.
func (c *rpcclient) connect(ctx context.Context, IPC string, contractAddr *common.Address) error {
	client, err := rpc.DialIPC(ctx, IPC)
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	c.ec = ethclient.NewClient(client)
	c.es, err = swap.NewETHSwap(*contractAddr, c.ec)
	if err != nil {
		return fmt.Errorf("unable to find swap contract: %v", err)
	}
	c.c = client
	return nil
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	if c.ec != nil {
		c.ec.Close()
	}
}

// bestBlockHash gets the best blocks hash at the time of calling. Due to the
// speed of Ethereum blocks, this changes often.
func (c *rpcclient) bestBlockHash(ctx context.Context) (common.Hash, error) {
	header, err := c.bestHeader(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}

// bestHeader gets the best header at the time of calling.
func (c *rpcclient) bestHeader(ctx context.Context) (*types.Header, error) {
	bn, err := c.ec.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	return c.ec.HeaderByNumber(ctx, big.NewInt(int64(bn)))
}

// block gets the block identified by hash.
func (c *rpcclient) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return c.ec.BlockByHash(ctx, hash)
}

// suggestGasPrice retrieves the currently suggested gas price to allow a timely
// execution of a transaction.
func (c *rpcclient) suggestGasPrice(ctx context.Context) (sgp *big.Int, err error) {
	return c.ec.SuggestGasPrice(ctx)
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return c.ec.SyncProgress(ctx)
}

// blockNumber gets the chain length at the time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (uint64, error) {
	return c.ec.BlockNumber(ctx)
}

// peers returns connected peers.
func (c *rpcclient) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	var peers []*p2p.PeerInfo
	return peers, c.c.CallContext(ctx, &peers, "admin_peers")
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, secretHash [32]byte) (*swap.ETHSwapSwap, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		Context: ctx,
	}
	swap, err := c.es.Swap(callOpts, secretHash)
	if err != nil {
		return nil, err
	}
	return &swap, nil
}

// transaction gets the transaction that hashes to hash from the chain or
// mempool. Errors if tx does not exist.
func (c *rpcclient) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return c.ec.TransactionByHash(ctx, hash)
}

// balance gets the current balance held by an address.
func (c *rpcclient) balance(ctx context.Context, addr *common.Address) (*big.Int, error) {
	// The last argument is block number. nil will use the latest know
	// block.
	return c.ec.BalanceAt(ctx, *addr, nil)
}

// pendingBalance gets the current pending balance held by an address.
func (c *rpcclient) pendingBalance(ctx context.Context, addr *common.Address) (*big.Int, error) {
	return c.ec.PendingBalanceAt(ctx, *addr)
}
