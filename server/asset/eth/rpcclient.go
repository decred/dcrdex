// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"context"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var (
	_ ethFetcher = (*rpcclient)(nil)

	bigZero = new(big.Int)
)

type ContextCaller interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

type rpcclient struct {
	net dex.Network
	ipc string
	// ec wraps a *rpc.Client with some useful calls.
	ec *ethclient.Client
	// c is a direct client for raw calls.
	caller ContextCaller
	// swapContract is the current ETH swapContract.
	swapContract swapContract

	// tokens are tokeners for loaded tokens. tokens is not protected by a
	// mutex, as it is expected that the caller will connect and place calls to
	// loadToken sequentially in the same thread during initialization.
	tokens map[uint32]*tokener
}

func newRPCClient(net dex.Network, ipc string) *rpcclient {
	return &rpcclient{
		net:    net,
		tokens: make(map[uint32]*tokener),
		ipc:    ipc,
	}
}

// connect connects to an ipc socket. It then wraps ethclient's client and
// bundles commands in a form we can easil use.
func (c *rpcclient) connect(ctx context.Context) error {
	client, err := rpc.DialIPC(ctx, c.ipc)
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	c.ec = ethclient.NewClient(client)

	netAddrs, found := dexeth.ContractAddresses[ethContractVersion]
	if !found {
		return fmt.Errorf("no contract address for eth version %d", ethContractVersion)
	}
	contractAddr, found := netAddrs[c.net]
	if !found {
		return fmt.Errorf("no contract address for eth version %d on %s", ethContractVersion, c.net)
	}

	es, err := swapv0.NewETHSwap(contractAddr, c.ec)
	if err != nil {
		return fmt.Errorf("unable to find swap contract: %v", err)
	}
	c.swapContract = &swapSourceV0{es}
	c.caller = client
	return nil
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	if c.ec != nil {
		c.ec.Close()
	}
}

func (c *rpcclient) loadToken(ctx context.Context, assetID uint32) error {
	tkn, err := newTokener(ctx, assetID, c.net, c.ec)
	if err != nil {
		return fmt.Errorf("error constructing ERC20Swap: %w", err)
	}

	c.tokens[assetID] = tkn
	return nil
}

func (c *rpcclient) withTokener(assetID uint32, f func(*tokener) error) error {
	tkn, found := c.tokens[assetID]
	if !found {
		return fmt.Errorf("no swap source for asset %d", assetID)
	}
	return f(tkn)
}

// bestHeader gets the best header at the time of calling.
func (c *rpcclient) bestHeader(ctx context.Context) (*types.Header, error) {
	bn, err := c.ec.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	return c.ec.HeaderByNumber(ctx, big.NewInt(int64(bn)))
}

// headerByHeight gets the best header at height.
func (c *rpcclient) headerByHeight(ctx context.Context, height uint64) (*types.Header, error) {
	return c.ec.HeaderByNumber(ctx, big.NewInt(int64(height)))
}

// suggestGasTipCap retrieves the currently suggested priority fee to allow a
// timely execution of a transaction.
func (c *rpcclient) suggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return c.ec.SuggestGasTipCap(ctx)
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return c.ec.SyncProgress(ctx)
}

// blockNumber gets the chain length at the time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (uint64, error) {
	return c.ec.BlockNumber(ctx)
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, assetID uint32, secretHash [32]byte) (state *dexeth.SwapState, err error) {
	if assetID == BipID {
		return c.swapContract.Swap(ctx, secretHash)
	}
	return state, c.withTokener(assetID, func(tkn *tokener) error {
		state, err = tkn.Swap(ctx, secretHash)
		return err
	})
}

// transaction gets the transaction that hashes to hash from the chain or
// mempool. Errors if tx does not exist.
func (c *rpcclient) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return c.ec.TransactionByHash(ctx, hash)
}

// accountBalance gets the account balance, including the effects of known
// unmined transactions.
func (c *rpcclient) accountBalance(ctx context.Context, assetID uint32, addr common.Address) (*big.Int, error) {
	tip, err := c.blockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("blockNumber error: %v", err)
	}

	// We need to subtract and pending outgoing value, but ignore any pending
	// incoming value since that can't be spent until mined. So we can't using
	// PendingBalanceAt or BalanceAt by themselves.
	// We'll iterate tx pool transactions and subtract any value and fees being
	// sent from this account. The rpc.Client doesn't expose the
	// txpool_contentFrom => (*TxPool).ContentFrom RPC method, for whatever
	// reason, so we'll have to use CallContext and copy the mimic the
	// internal RPCTransaction type.
	var txs map[string]map[string]*RPCTransaction
	err = c.caller.CallContext(ctx, &txs, "txpool_contentFrom", addr)
	if err != nil {
		return nil, fmt.Errorf("contentFrom error: %w", err)
	}

	if assetID == BipID {
		ethBalance, err := c.ec.BalanceAt(ctx, addr, big.NewInt(int64(tip)))
		if err != nil {
			return nil, err
		}
		outgoingEth := new(big.Int)
		for _, group := range txs { // 2 groups, pending and queued
			for _, tx := range group {
				outgoingEth.Add(outgoingEth, tx.Value.ToInt())
				gas := new(big.Int).SetUint64(uint64(tx.Gas))
				if tx.GasPrice != nil && tx.GasPrice.ToInt().Cmp(bigZero) > 0 {
					outgoingEth.Add(outgoingEth, new(big.Int).Mul(gas, tx.GasPrice.ToInt()))
				} else if tx.GasFeeCap != nil {
					outgoingEth.Add(outgoingEth, new(big.Int).Mul(gas, tx.GasFeeCap.ToInt()))
				} else {
					return nil, fmt.Errorf("cannot find fees for tx %s", tx.Hash)
				}
			}
		}
		return ethBalance.Sub(ethBalance, outgoingEth), nil
	}

	// For tokens, we'll do something similar, but with checks for pending txs
	// that transfer tokens or pay to the swap contract.
	bal := new(big.Int)
	return bal, c.withTokener(assetID, func(tkn *tokener) error {
		bal, err = tkn.balanceOf(ctx, addr)
		if err != nil {
			return err
		}
		for _, group := range txs {
			for _, rpcTx := range group {
				to := *rpcTx.To
				if to == tkn.tokenAddr {
					if sent := tkn.transferred(rpcTx.Input); sent != nil {
						bal.Sub(bal, sent)
					}
				}
				if to == tkn.contractAddr {
					if swapped := tkn.swapped(rpcTx.Input); swapped != nil {
						bal.Sub(bal, swapped)
					}
				}
			}
		}
		return nil
	})
}

type RPCTransaction struct {
	Value     *hexutil.Big    `json:"value"`
	Gas       hexutil.Uint64  `json:"gas"`
	GasPrice  *hexutil.Big    `json:"gasPrice"`
	GasFeeCap *hexutil.Big    `json:"maxFeePerGas,omitempty"`
	Hash      common.Hash     `json:"hash"`
	To        *common.Address `json:"to"`
	Input     hexutil.Bytes   `json:"input"`
	// BlockHash        *common.Hash      `json:"blockHash"`
	// BlockNumber      *hexutil.Big      `json:"blockNumber"`
	// From             common.Address    `json:"from"`
	// GasTipCap        *hexutil.Big      `json:"maxPriorityFeePerGas,omitempty"`
	// Nonce            hexutil.Uint64    `json:"nonce"`
	// TransactionIndex *hexutil.Uint64   `json:"transactionIndex"`
	// Type             hexutil.Uint64    `json:"type"`
	// Accesses         *types.AccessList `json:"accessList,omitempty"`
	// ChainID          *hexutil.Big      `json:"chainId,omitempty"`
	// V                *hexutil.Big      `json:"v"`
	// R                *hexutil.Big      `json:"r"`
	// S                *hexutil.Big      `json:"s"`
}
