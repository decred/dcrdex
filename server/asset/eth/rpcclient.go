// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

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

	bigZero                    = new(big.Int)
	headerExpirationTime       = time.Minute
	monitorConnectionsInterval = 30 * time.Second
)

type ContextCaller interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

type ethConn struct {
	*ethclient.Client
	endpoint string
	// swapContract is the current ETH swapContract.
	swapContract swapContract
	// tokens are tokeners for loaded tokens. tokens is not protected by a
	// mutex, as it is expected that the caller will connect and place calls to
	// loadToken sequentially in the same thread during initialization.
	tokens map[uint32]*tokener
	// caller is a client for raw calls not implemented by *ethclient.Client.
	caller          ContextCaller
	txPoolSupported bool
}

type rpcclient struct {
	net dex.Network
	log dex.Logger
	// endpoints should only be used during connect to know which endpoints
	// to attempt to connect. If we were unable to connect to some of the
	// endpoints, they will not be included in the clients slice.
	endpoints []string

	// the order of clients will change based on the health of the connections.
	clientsMtx sync.RWMutex
	clients    []*ethConn

	healthCheckWaiter sync.WaitGroup
}

func newRPCClient(net dex.Network, endpoints []string, log dex.Logger) *rpcclient {
	return &rpcclient{
		net:       net,
		endpoints: endpoints,
		log:       log,
	}
}

func (c *rpcclient) clientsCopy() []*ethConn {
	c.clientsMtx.RLock()
	defer c.clientsMtx.RUnlock()

	clients := make([]*ethConn, len(c.clients))
	copy(clients, c.clients)
	return clients
}

// checkIfConnectionOutdated checks if the connection is outdated.
func (c *rpcclient) checkIfConnectionOutdated(ctx context.Context, conn *ethConn) (bool, error) {
	hdr, err := conn.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("Failed to get header from %q: %v", conn.endpoint, err)
	}

	return c.headerIsOutdated(hdr), nil
}

// checkConnections checks the health of the connections and reorders them
// based on their health. It does a best header call to each connection and
// connections with non outdated headers are placed first, ones with outdated
// headers are placed in the middle, and ones that error are placed last.
func (c *rpcclient) checkConnectionsHealth(ctx context.Context) {
	clients := c.clientsCopy()

	healthyConnections := make([]*ethConn, 0, len(clients))
	outdatedConnections := make([]*ethConn, 0, len(clients))
	failingConnections := make([]*ethConn, 0, len(clients))

	for _, ec := range clients {
		outdated, err := c.checkIfConnectionOutdated(ctx, ec)
		if err != nil {
			c.log.Errorf("Error checking if connection to %q outdated: %v", ec.endpoint, err)
			failingConnections = append(failingConnections, ec)
			continue
		}

		if outdated {
			c.log.Warnf("Connection to %q is outdated. Check your system clock if you see this repeatedly.", ec.endpoint)
			outdatedConnections = append(outdatedConnections, ec)
			continue
		}

		healthyConnections = append(healthyConnections, ec)
	}

	clientsUpdatedOrder := make([]*ethConn, 0, len(clients))
	clientsUpdatedOrder = append(clientsUpdatedOrder, healthyConnections...)
	clientsUpdatedOrder = append(clientsUpdatedOrder, outdatedConnections...)
	clientsUpdatedOrder = append(clientsUpdatedOrder, failingConnections...)

	c.clientsMtx.Lock()
	defer c.clientsMtx.Unlock()
	c.clients = clientsUpdatedOrder
}

// monitorConnectionsHealth starts a goroutine that checks the health of all connections
// every 30 seconds.
func (c *rpcclient) monitorConnectionsHealth(ctx context.Context) {
	ticker := time.NewTicker(monitorConnectionsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkConnectionsHealth(ctx)
		}
	}
}

func (c *rpcclient) withClient(f func(ec *ethConn) error, haltOnNotFound ...bool) (err error) {
	clients := c.clientsCopy()

	for _, ec := range clients {
		err = f(ec)
		if err == nil {
			return nil
		}
		if len(haltOnNotFound) > 0 && haltOnNotFound[0] && (errors.Is(err, ethereum.NotFound) || strings.Contains(err.Error(), "not found")) {
			return err
		}

		c.log.Errorf("Unpropagated error from %q: %v", ec.endpoint, err)
	}

	return fmt.Errorf("all providers failed. last error: %w", err)
}

// connect will attempt to connect to all the endpoints in the endpoints slice.
// If at least one of the connections is successful and is not outdated, the
// function will return without error.
//
// Connections with an outdated block will be marked as outdated, but included
// in the clients slice. If the up-to-date providers start to fail, the outdated
// ones will be checked to see if they are still outdated.
//
// Failed connections will not be included in the clients slice.
func (c *rpcclient) connect(ctx context.Context) (err error) {
	netAddrs, found := dexeth.ContractAddresses[ethContractVersion]
	if !found {
		return fmt.Errorf("no contract address for eth version %d", ethContractVersion)
	}
	contractAddr, found := netAddrs[c.net]
	if !found {
		return fmt.Errorf("no contract address for eth version %d on %s", ethContractVersion, c.net)
	}

	var success bool

	c.clients = make([]*ethConn, 0, len(c.endpoints))
	for _, endpoint := range c.endpoints {
		client, err := rpc.DialContext(ctx, endpoint)
		if err != nil {
			c.log.Errorf("Ethereum RPC client failed to connect to %q: %v", endpoint, err)
			continue
		}

		defer func() {
			if !success {
				client.Close()
			}
		}()

		ethClient := ethclient.NewClient(client)
		ec := &ethConn{
			Client:   ethclient.NewClient(client),
			endpoint: endpoint,
			tokens:   make(map[uint32]*tokener),
		}

		reqModules := []string{"eth", "txpool"}
		if err := dexeth.CheckAPIModules(client, endpoint, c.log, reqModules); err != nil {
			c.log.Warnf("Error checking required modules at %q: %v", endpoint, err)
			c.log.Warnf("Will not account for pending transactions in balance calculations at %q", endpoint)
			ec.txPoolSupported = false
		} else {
			ec.txPoolSupported = true
		}

		hdr, err := ec.HeaderByNumber(ctx, nil)
		if err != nil {
			c.log.Errorf("Failed to get header from %q: %v", endpoint, err)
			continue
		}

		outdated := c.headerIsOutdated(hdr)
		if outdated {
			c.log.Warnf("Best header from %q is outdated.", endpoint)
		} else {
			success = true
		}

		// This only returns an error if the abi fails to parse, so if it fails
		// for one provider, it will fail for all.
		es, err := swapv0.NewETHSwap(contractAddr, ethClient)
		if err != nil {
			return fmt.Errorf("unable to initialize eth contract for %q: %v", endpoint, err)
		}

		ec.swapContract = &swapSourceV0{es}
		ec.caller = client

		// Put outdated clients at the end of the list.
		if outdated {
			c.clients = append(c.clients, ec)
		} else {
			c.clients = append([]*ethConn{ec}, c.clients...)
		}
	}

	c.log.Infof("number of connected ETH providers: %d", len(c.clients))

	if !success {
		return fmt.Errorf("no connection to an up to date ETH provider available")
	}

	go c.monitorConnectionsHealth(ctx)

	return nil
}

func (c *rpcclient) headerIsOutdated(hdr *types.Header) bool {
	return c.net != dex.Simnet && hdr.Time < uint64(time.Now().Add(-headerExpirationTime).Unix())
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	for _, ec := range c.clientsCopy() {
		ec.Close()
	}
}

func (c *rpcclient) loadToken(ctx context.Context, assetID uint32) error {
	for _, cl := range c.clientsCopy() {
		tkn, err := newTokener(ctx, assetID, c.net, cl.Client)
		if err != nil {
			return fmt.Errorf("error constructing ERC20Swap: %w", err)
		}

		cl.tokens[assetID] = tkn
	}
	return nil
}

func (c *rpcclient) withTokener(ctx context.Context, assetID uint32, f func(*tokener) error) error {
	return c.withClient(func(ec *ethConn) error {
		tkn, found := ec.tokens[assetID]
		if !found {
			return fmt.Errorf("no swap source for asset %d", assetID)
		}
		return f(tkn)
	})

}

// bestHeader gets the best header at the time of calling.
func (c *rpcclient) bestHeader(ctx context.Context) (hdr *types.Header, err error) {
	return hdr, c.withClient(func(ec *ethConn) error {
		hdr, err = ec.HeaderByNumber(ctx, nil)
		if err != nil {
			return err
		}
		return nil
	})
}

// headerByHeight gets the best header at height.
func (c *rpcclient) headerByHeight(ctx context.Context, height uint64) (hdr *types.Header, err error) {
	return hdr, c.withClient(func(ec *ethConn) error {
		hdr, err = ec.HeaderByNumber(ctx, big.NewInt(int64(height)))
		return err
	})
}

// suggestGasTipCap retrieves the currently suggested priority fee to allow a
// timely execution of a transaction.
func (c *rpcclient) suggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return tipCap, c.withClient(func(ec *ethConn) error {
		tipCap, err = ec.SuggestGasTipCap(ctx)
		return err
	})
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (prog *ethereum.SyncProgress, err error) {
	return prog, c.withClient(func(ec *ethConn) error {
		prog, err = ec.SyncProgress(ctx)
		return err
	})
}

// blockNumber gets the chain length at the time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (bn uint64, err error) {
	return bn, c.withClient(func(ec *ethConn) error {
		bn, err = ec.BlockNumber(ctx)
		return err
	})
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, assetID uint32, secretHash [32]byte) (state *dexeth.SwapState, err error) {
	if assetID == BipID {
		return state, c.withClient(func(ec *ethConn) error {
			state, err = ec.swapContract.Swap(ctx, secretHash)
			return err
		})
	}
	return state, c.withTokener(ctx, assetID, func(tkn *tokener) error {
		state, err = tkn.Swap(ctx, secretHash)
		return err
	})
}

// transaction gets the transaction that hashes to hash from the chain or
// mempool. Errors if tx does not exist.
func (c *rpcclient) transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error) {
	return tx, isMempool, c.withClient(func(ec *ethConn) error {
		tx, isMempool, err = ec.TransactionByHash(ctx, hash)
		return err
	}, true) // stop on first provider with "not found", because this should be an error if tx does not exist
}

// dumbBalance gets the account balance, ignoring the effects of unmined
// transactions.
func (c *rpcclient) dumbBalance(ctx context.Context, ec *ethConn, assetID uint32, addr common.Address) (bal *big.Int, err error) {
	if assetID == BipID {
		return ec.BalanceAt(ctx, addr, nil)
	}
	tkn := ec.tokens[assetID]
	if tkn == nil {
		return nil, fmt.Errorf("no tokener for asset ID %d", assetID)
	}
	return tkn.balanceOf(ctx, addr)
}

// smartBalance gets the account balance, including the effects of known
// unmined transactions.
func (c *rpcclient) smartBalance(ctx context.Context, ec *ethConn, assetID uint32, addr common.Address) (bal *big.Int, err error) {
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
	if err := ec.caller.CallContext(ctx, &txs, "txpool_contentFrom", addr); err != nil {
		return nil, fmt.Errorf("contentFrom error: %w", err)
	}

	if assetID == BipID {
		ethBalance, err := ec.BalanceAt(ctx, addr, big.NewInt(int64(tip)))
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
	// Can't use withTokener because we need to use the same ethConn due to
	// txPoolSupported being used to decide between {smart/dumb}Balance.
	tkn := ec.tokens[assetID]
	if tkn == nil {
		return nil, fmt.Errorf("no tokener for asset ID %d", assetID)
	}
	bal, err = tkn.balanceOf(ctx, addr)
	if err != nil {
		return nil, err
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
	return bal, nil
}

// accountBalance gets the account balance. If txPool functions are supported by the
// client, it will include the effects of unmined transactions, otherwise it will not.
func (c *rpcclient) accountBalance(ctx context.Context, assetID uint32, addr common.Address) (bal *big.Int, err error) {
	return bal, c.withClient(func(ec *ethConn) error {
		if ec.txPoolSupported {
			bal, err = c.smartBalance(ctx, ec, assetID, addr)
		} else {
			bal, err = c.dumbBalance(ctx, ec, assetID, addr)
		}
		return err
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
