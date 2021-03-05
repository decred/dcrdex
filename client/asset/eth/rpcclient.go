// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"

	swap "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// Check that rpcclient satisfies the ethFetcher interface.
var _ ethFetcher = (*rpcclient)(nil)

// rpcclient satisfies the ethFetcher interface. Do not use until Connect is
// called.
type rpcclient struct {
	// c is a direct client for raw calls.
	c *rpc.Client
	// ec wraps the client with some useful calls.
	ec *ethclient.Client
	n  *node.Node
	es *swap.ETHSwap
}

// connect connects to a node. It then wraps ethclient's client and
// bundles commands in a form we can easily use.
func (c *rpcclient) connect(ctx context.Context, node *node.Node, contractAddr common.Address) error {
	client, err := node.Attach()
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	c.c = client
	c.ec = ethclient.NewClient(client)
	c.n = node
	c.es, err = swap.NewETHSwap(contractAddr, c.ec)
	if err != nil {
		return fmt.Errorf("unable to find swap contract: %v", err)
	}
	return nil
}

// shutdown shuts down the client.
func (c *rpcclient) shutdown() {
	if c.ec != nil {
		// this will also close c.c
		c.ec.Close()
	}
}

// bestBlockHash gets the best block's hash at the time of calling.
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

// accounts returns all accounts from the internal node.
func (c *rpcclient) accounts() []*accounts.Account {
	var accts []*accounts.Account
	for _, wallet := range c.n.AccountManager().Wallets() {
		for _, acct := range wallet.Accounts() {
			accts = append(accts, &acct)
		}
	}
	return accts
}

// balance gets the current balance of an account.
func (c *rpcclient) balance(ctx context.Context, acct *accounts.Account) (*big.Int, error) {
	return c.ec.BalanceAt(ctx, acct.Address, nil)
}

// unlock uses a raw request to unlock an account indefinitely.
func (c *rpcclient) unlock(ctx context.Context, pw string, acct *accounts.Account) error {
	// Passing 0 as the last argument unlocks with not lock time.
	return c.c.CallContext(ctx, nil, "personal_unlockAccount", acct.Address.String(), pw, 0)
}

// lock uses a raw request to unlock an account indefinitely.
func (c *rpcclient) lock(ctx context.Context, acct *accounts.Account) error {
	return c.c.CallContext(ctx, nil, "personal_lockAccount", acct.Address.String())
}

// transactionReceipt uses a raw request to retrieve a transaction's receipt.
func (c *rpcclient) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	res := new(types.Receipt)
	if err := c.c.CallContext(ctx, res, "eth_getTransactionReceipt", txHash.String()); err != nil {
		return nil, err
	}
	return res, nil
}

// pendingTransactions returns pending transactions.
func (c *rpcclient) pendingTransactions(ctx context.Context) ([]*types.Transaction, error) {
	var ptxs []*types.Transaction
	err := c.c.CallContext(ctx, &ptxs, "eth_pendingTransactions")
	if err != nil {
		return nil, err
	}
	return ptxs, nil
}

// addPeer adds a peer.
func (c *rpcclient) addPeer(ctx context.Context, peer string) error {
	return c.c.CallContext(ctx, nil, "admin_addPeer", peer)
}

// blockNumber gets the block number at time of calling.
func (c *rpcclient) blockNumber(ctx context.Context) (uint64, error) {
	bn, err := c.ec.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	return bn, nil
}

// nodeInfo retrieves useful information about a node.
func (c *rpcclient) nodeInfo(ctx context.Context) (*p2p.NodeInfo, error) {
	info := new(p2p.NodeInfo)
	if err := c.c.CallContext(ctx, info, "admin_nodeInfo"); err != nil {
		return nil, err
	}
	return info, nil
}

// listWallets list all of the wallet's wallets? and accounts along with details
// such as locked status.
func (c *rpcclient) listWallets(ctx context.Context) ([]rawWallet, error) {
	var res []rawWallet
	if err := c.c.CallContext(ctx, &res, "personal_listWallets"); err != nil {
		return nil, err
	}
	return res, nil
}

// sendTransaction uses a raw request to send tx.
func (c *rpcclient) sendTransaction(ctx context.Context, tx map[string]string) (common.Hash, error) {
	res := common.Hash{}
	err := c.c.CallContext(ctx, &res, "eth_sendTransaction", tx)
	if err != nil {
		return common.Hash{}, err
	}
	return res, nil
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (c *rpcclient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, error) {
	return c.ec.SyncProgress(ctx)
}

// importAccount imports an account into the ethereum wallet by private key
// that can be unlocked with password.
func (c *rpcclient) importAccount(pw string, privKeyB []byte) (*accounts.Account, error) {
	privKey, err := crypto.ToECDSA(privKeyB)
	if err != nil {
		return new(accounts.Account), fmt.Errorf("error parsing private key: %v", err)
	}
	ks := c.n.AccountManager().Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
	acct, err := ks.ImportECDSA(privKey, pw)
	if err != nil {
		return nil, err
	}
	return &acct, nil
}

// peers returns connected peers.
func (c *rpcclient) peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	var peers []*p2p.PeerInfo
	err := c.c.CallContext(ctx, &peers, "admin_peers")
	if err != nil {
		return nil, err
	}
	return peers, nil
}

// swap gets a swap keyed by secretHash in the contract.
func (c *rpcclient) swap(ctx context.Context, from *accounts.Account, secretHash [32]byte) (*swap.ETHSwapSwap, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    from.Address,
		Context: ctx,
	}
	swap, err := c.es.Swap(callOpts, secretHash)
	if err != nil {
		return nil, err
	}
	return &swap, nil
}

// wallet returns a wallet that owns acct from an ethereum wallet.
func (c *rpcclient) wallet(acct accounts.Account) (accounts.Wallet, error) {
	wallet, err := c.n.AccountManager().Find(acct)
	if err != nil {
		return nil, fmt.Errorf("error finding wallet for account %s: %v \n", acct.Address, err)
	}
	return wallet, nil
}

// initiate creates a swap contract. The initiator will be the account at
// txOpts.From. Any on-chain failure, such as this secret hash already existing
// in the swaps map, will not cause this to error.
func (c *rpcclient) initiate(txOpts *bind.TransactOpts, netID int64, refundTimestamp int64, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	wallet, err := c.wallet(accounts.Account{Address: txOpts.From})
	if err != nil {
		return nil, err
	}
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return wallet.SignTx(accounts.Account{Address: addr}, tx, big.NewInt(netID))
	}
	return c.es.Initiate(txOpts, big.NewInt(refundTimestamp), secretHash, participant)
}

// redeem redeems a swap contract. The redeemer will be the account at txOpts.From.
// Any on-chain failure, such as this secret not matching the hash, will not cause
// this to error.
func (c *rpcclient) redeem(txOpts *bind.TransactOpts, netID int64, secret, secretHash [32]byte) (*types.Transaction, error) {
	wallet, err := c.wallet(accounts.Account{Address: txOpts.From})
	if err != nil {
		return nil, err
	}
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return wallet.SignTx(accounts.Account{Address: addr}, tx, big.NewInt(netID))
	}
	return c.es.Redeem(txOpts, secret, secretHash)
}

// refund refunds a swap contract. The refunder will be the account at txOpts.From.
// Any on-chain failure, such as the locktime not being past, will not cause
// this to error.
func (c *rpcclient) refund(txOpts *bind.TransactOpts, netID int64, secretHash [32]byte) (*types.Transaction, error) {
	wallet, err := c.wallet(accounts.Account{Address: txOpts.From})
	if err != nil {
		return nil, err
	}
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return wallet.SignTx(accounts.Account{Address: addr}, tx, big.NewInt(netID))
	}
	return c.es.Refund(txOpts, secretHash)
}
