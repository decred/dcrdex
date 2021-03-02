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

// Check that client satisfies the ethFetcher interface.
var _ ethFetcher = (*client)(nil)

type client struct {
	ec *ethclient.Client
}

// Connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easil use.
func (c *client) Connect(ctx context.Context, IPC string) error {
	client, err := rpc.DialIPC(ctx, IPC)
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	ec := ethclient.NewClient(client)
	c.ec = ec
	return nil
}

// Shutdown shuts down the client.
func (c *client) Shutdown() {
	if c.ec != nil {
		c.ec.Close()
	}
}

// BestBlockHash gets the best blocks hash at the time of calling. Due to the
// speed of Ethereum blocks, this changes often.
func (c *client) BestBlockHash(ctx context.Context) (common.Hash, error) {
	header, err := c.bestHeader(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}

func (c *client) bestHeader(ctx context.Context) (*types.Header, error) {
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

// Block gets the block identified by hash.
func (c *client) Block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	block, err := c.ec.BlockByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return block, nil
}
