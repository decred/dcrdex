// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var _ ethFetcher = (*rpcclient)(nil)

type rpcclient struct {
	ec *ethclient.Client
}

// connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easil use.
func (c *rpcclient) connect(ctx context.Context, IPC string) error {
	client, err := rpc.DialIPC(ctx, IPC)
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	ec := ethclient.NewClient(client)
	c.ec = ec
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
	header, err := c.ec.HeaderByNumber(ctx, big.NewInt(int64(bn)))
	if err != nil {
		return nil, err
	}
	return header, nil
}

// block gets the block identified by hash.
func (c *rpcclient) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	block, err := c.ec.BlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return block, nil
}
