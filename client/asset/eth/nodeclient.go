// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/les"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/ethereum/go-ethereum/consensus/misc"
)

var (
	// https://github.com/ethereum/go-ethereum/blob/16341e05636fd088aa04a27fca6dc5cda5dbab8f/eth/backend.go#L110-L113
	// ultimately results in a minimum fee rate by the filter applied at
	// https://github.com/ethereum/go-ethereum/blob/4ebeca19d739a243dc0549bcaf014946cde95c4f/core/tx_pool.go#L626
	minGasPrice = ethconfig.Defaults.Miner.GasPrice

	// Check that nodeClient satisfies the ethFetcher interface.
	_ ethFetcher = (*nodeClient)(nil)
)

// nodeClient satisfies the ethFetcher interface. Do not use until Connect is
// called.
type nodeClient struct {
	net          dex.Network
	log          dex.Logger
	creds        *accountCredentials
	p2pSrv       *p2p.Server
	ec           *ethclient.Client
	node         *node.Node
	leth         *les.LightEthereum
	chainID      *big.Int
	contractors  map[uint32]contractor
	nonceSendMtx sync.Mutex
}

func newNodeClient(dir string, net dex.Network, log dex.Logger) (*nodeClient, error) {
	node, err := prepareNode(&nodeConfig{
		net:    net,
		appDir: dir,
		logger: log.SubLogger("NODE"),
	})
	if err != nil {
		return nil, err
	}

	leth, err := startNode(node, net)
	if err != nil {
		return nil, err
	}

	creds, err := nodeCredentials(node)
	if err != nil {
		return nil, err
	}

	return &nodeClient{
		chainID:     big.NewInt(chainIDs[net]),
		node:        node,
		leth:        leth,
		creds:       creds,
		net:         net,
		log:         log,
		contractors: make(map[uint32]contractor),
	}, nil
}

func (n *nodeClient) address() common.Address {
	return n.creds.addr
}

// connect connects to a node. It then wraps ethclient's client and
// bundles commands in a form we can easily use.
func (n *nodeClient) connect(ctx context.Context) error {
	client, err := n.node.Attach()
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	n.ec = ethclient.NewClient(client)
	n.p2pSrv = n.node.Server()
	if n.p2pSrv == nil {
		return fmt.Errorf("no *p2p.Server")
	}

	for ver, constructor := range contractorConstructors {
		if n.contractors[ver], err = constructor(n.net, n.creds.addr, n.ec); err != nil {
			return fmt.Errorf("error constructor version %d contractor: %v", ver, err)
		}
	}

	return nil
}

// shutdown shuts down the client.
func (n *nodeClient) shutdown() {
	if n.ec != nil {
		// this will also close the underlying rpc.Client.
		n.ec.Close()
	}
	if n.node != nil {
		n.node.Close()
		n.node.Wait()
	}
}

// withcontractor runs the provided function with the versioned contractor.
func (n *nodeClient) withcontractor(ver uint32, f func(contractor) error) error {
	contractor, found := n.contractors[ver]
	if !found {
		return fmt.Errorf("no version %d contractor", ver)
	}
	return f(contractor)
}

// bestBlockHash gets the best block's hash at the time of calling.
func (n *nodeClient) bestBlockHash(ctx context.Context) (common.Hash, error) {
	header, err := n.bestHeader(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Hash(), nil
}

// bestHeader gets the best header at the time of calling.
func (n *nodeClient) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.leth.ApiBackend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
}

// block gets the block identified by hash.
func (n *nodeClient) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return n.leth.ApiBackend.BlockByHash(ctx, hash)
}

// accounts returns all accounts from the internal node.
func (n *nodeClient) accounts() []*accounts.Account {
	var accts []*accounts.Account
	for _, wallet := range n.node.AccountManager().Wallets() {
		for _, acct := range wallet.Accounts() {
			accts = append(accts, &acct)
		}
	}
	return accts
}

func (n *nodeClient) stateAt(ctx context.Context, bn rpc.BlockNumber) (*state.StateDB, error) {
	state, _, err := n.leth.ApiBackend.StateAndHeaderByNumberOrHash(ctx, rpc.BlockNumberOrHashWithNumber(bn))
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.New("no state")
	}
	return state, nil
}

func (n *nodeClient) balanceAt(ctx context.Context, addr common.Address, bn rpc.BlockNumber) (*big.Int, error) {
	state, err := n.stateAt(ctx, bn)
	if err != nil {
		return nil, err
	}
	return state.GetBalance(addr), nil
}

func (n *nodeClient) addressBalance(ctx context.Context, addr common.Address) (*big.Int, error) {
	return n.balanceAt(ctx, addr, rpc.LatestBlockNumber)
}

// balance gets the current and pending balances.
func (n *nodeClient) balance(ctx context.Context) (*Balance, error) {
	bal, err := n.balanceAt(ctx, n.creds.addr, rpc.LatestBlockNumber)
	if err != nil {
		return nil, fmt.Errorf("pending balance error: %w", err)
	}

	pendingTxs, err := n.pendingTransactions()
	if err != nil {
		return nil, fmt.Errorf("error getting pending txs: %w", err)
	}

	outgoing := new(big.Int)
	incoming := new(big.Int)
	zero := new(big.Int)

	addFees := func(tx *types.Transaction) {
		gas := new(big.Int).SetUint64(tx.Gas())
		if gasPrice := tx.GasPrice(); gasPrice != nil && gasPrice.Cmp(zero) > 0 {
			outgoing.Add(outgoing, new(big.Int).Mul(gas, gasPrice))
		} else if gasFeeCap := tx.GasFeeCap(); gasFeeCap != nil {
			outgoing.Add(outgoing, new(big.Int).Mul(gas, gasFeeCap))
		} else {
			n.log.Errorf("unable to calculate fees for tx %s", tx.Hash())
		}
	}

	ethSigner := types.LatestSigner(n.leth.ApiBackend.ChainConfig()) // "latest" good for pending

	for _, tx := range pendingTxs {
		from, _ := ethSigner.Sender(tx) // zero Address on error
		if from != n.creds.addr {
			continue
		}
		addFees(tx)
		v := tx.Value()
		if v.Cmp(zero) == 0 {
			// If zero value, attempt to find redemptions or refunds that pay
			// to us.
			for ver, c := range n.contractors {
				in, err := c.incomingValue(ctx, tx)
				if err != nil {
					n.log.Errorf("version %d contractor incomingValue error: %v", ver, err)
					continue
				}
				if in > 0 {
					incoming.Add(incoming, dexeth.GweiToWei(in))
				}
			}
		} else {
			// If non-zero outgoing value, this is a swap or a send of some
			// type.
			outgoing.Add(outgoing, v)
		}
	}

	return &Balance{
		Current:    bal,
		PendingOut: outgoing,
		PendingIn:  incoming,
	}, nil
}

// unlock the account indefinitely.
func (n *nodeClient) unlock(pw string) error {
	return n.creds.ks.TimedUnlock(*n.creds.acct, pw, 0)
}

// lock the account indefinitely.
func (n *nodeClient) lock() error {
	return n.creds.ks.Lock(n.creds.addr)
}

// locked returns true if the wallet is currently locked.
func (n *nodeClient) locked() bool {
	status, _ := n.creds.wallet.Status()
	return status != "Unlocked"
}

// sendToAddr sends funds to the address.
func (n *nodeClient) sendToAddr(ctx context.Context, addr common.Address, amt uint64) (*types.Transaction, error) {
	txOpts, err := n.txOpts(ctx, amt, defaultSendGasLimit, nil)
	if err != nil {
		return nil, err
	}
	return n.sendTransaction(ctx, txOpts, addr, nil)
}

// transactionReceipt retrieves the transaction's receipt.
func (n *nodeClient) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	_, blockHash, _, index, err := n.leth.ApiBackend.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, nil
	}
	receipts, err := n.leth.ApiBackend.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	if len(receipts) <= int(index) {
		return nil, fmt.Errorf("no receipt at index %d in block %s for tx %s", index, blockHash, txHash)
	}
	receipt := receipts[index]
	if receipt == nil {
		return nil, fmt.Errorf("nil receipt at index %d in block %s for tx %s", index, blockHash, txHash)
	}
	return receipt, nil
}

// pendingTransactions returns pending transactions.
func (n *nodeClient) pendingTransactions() ([]*types.Transaction, error) {
	return n.leth.ApiBackend.GetPoolTransactions()
}

// addPeer adds a peer.
func (n *nodeClient) addPeer(peerURL string) error {
	peer, err := enode.Parse(enode.ValidSchemes, peerURL)
	if err != nil {
		return err
	}
	n.p2pSrv.AddPeer(peer)
	return nil
}

// nodeInfo retrieves useful information about a node.
// Not used in production. TODO: remove?
func (n *nodeClient) nodeInfo() *p2p.NodeInfo {
	return n.p2pSrv.NodeInfo()
}

// listWallets list all of the wallet's wallets? and accounts along with details
// such as locked status.
// Not used in production. TODO: remove?
func (n *nodeClient) listWallets() []accounts.Wallet {
	return n.creds.ks.Wallets()
}

// sendTransaction sends a tx.
func (n *nodeClient) sendTransaction(ctx context.Context, txOpts *bind.TransactOpts,
	to common.Address, data []byte) (*types.Transaction, error) {

	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()

	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}

	tx, err := n.creds.ks.SignTx(*n.creds.acct, types.NewTx(&types.DynamicFeeTx{
		To:        &to,
		ChainID:   n.chainID,
		Nonce:     nonce,
		Gas:       txOpts.GasLimit,
		GasFeeCap: txOpts.GasFeeCap,
		GasTipCap: txOpts.GasTipCap,
		Value:     txOpts.Value,
		Data:      data,
	}), n.chainID)

	if err != nil {
		return nil, fmt.Errorf("signing error: %v", err)
	}

	return tx, n.leth.ApiBackend.SendTx(ctx, tx)
}

// syncProgress return the current sync progress. Returns no error and nil when not syncing.
func (n *nodeClient) syncProgress() ethereum.SyncProgress {
	return n.leth.ApiBackend.SyncProgress()
}

// peers returns connected peers.
// Not used in production. TODO: remove?
func (n *nodeClient) peers() []*p2p.Peer {
	return n.p2pSrv.Peers()
}

// swap gets a swap keyed by secretHash in the contract.
func (n *nodeClient) swap(ctx context.Context, secretHash [32]byte, contractVer uint32) (swap *dexeth.SwapState, err error) {
	return swap, n.withcontractor(contractVer, func(c contractor) error {
		swap, err = c.swap(ctx, secretHash)
		return err
	})
}

// signData uses the private key of the address to sign a piece of data.
// The address must have been imported and unlocked to use this function.
func (n *nodeClient) signData(addr common.Address, data []byte) ([]byte, error) {
	// The mime type argument to SignData is not used in the keystore wallet in geth.
	// It treats any data like plain text.
	return n.creds.wallet.SignData(*n.creds.acct, accounts.MimetypeTextPlain, data)
}

func (n *nodeClient) addSignerToOpts(txOpts *bind.TransactOpts) error {
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return n.creds.wallet.SignTx(accounts.Account{Address: addr}, tx, n.chainID)
	}
	return nil
}

// initiate initiates multiple swaps in the same transaction.
func (n *nodeClient) initiate(ctx context.Context, contracts []*asset.Contract, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	gas := dexeth.InitGas(len(contracts), 0)
	var val uint64
	for _, c := range contracts {
		val += c.Value
	}
	txOpts, _ := n.txOpts(ctx, val, gas, dexeth.GweiToWei(maxFeeRate))
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}
	return tx, n.withcontractor(contractVer, func(c contractor) error {
		tx, err = c.initiate(txOpts, contracts)
		return err
	})
}

// estimateestimateInitGasGas checks the amount of gas that is used for the
// initialization.
func (n *nodeClient) estimateInitGas(ctx context.Context, numSwaps int, contractVer uint32) (gas uint64, err error) {
	return gas, n.withcontractor(contractVer, func(c contractor) error {
		gas, err = c.estimateInitGas(ctx, numSwaps)
		return err
	})
}

// estimateRedeemGas checks the amount of gas that is used for the redemption.
func (n *nodeClient) estimateRedeemGas(ctx context.Context, secrets [][32]byte, contractVer uint32) (gas uint64, err error) {
	return gas, n.withcontractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRedeemGas(ctx, secrets)
		return err
	})
}

// estimateRefundGas checks the amount of gas that is used for a refund.
func (n *nodeClient) estimateRefundGas(ctx context.Context, secretHash [32]byte, contractVer uint32) (gas uint64, err error) {
	return gas, n.withcontractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRefundGas(ctx, secretHash)
		return err
	})
}

// redeem redeems a swap contract. The redeemer will be the account at txOpts.From.
// Any on-chain failure, such as this secret not matching the hash, will not cause
// this to error.
func (n *nodeClient) redeem(txOpts *bind.TransactOpts, redemptions []*asset.Redemption, contractVer uint32) (tx *types.Transaction, err error) {
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, err
	}
	return tx, n.withcontractor(contractVer, func(c contractor) error {
		tx, err = c.redeem(txOpts, redemptions)
		return err
	})
}

// refund refunds a swap contract. The refunder will be the account at txOpts.From.
// Any on-chain failure, such as the locktime not being past, will not cause
// this to error.
func (n *nodeClient) refund(txOpts *bind.TransactOpts, secretHash [32]byte, contractVer uint32) (tx *types.Transaction, err error) {
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, err
	}
	return tx, n.withcontractor(contractVer, func(c contractor) error {
		tx, err = c.refund(txOpts, secretHash)
		return err
	})
}

// netFeeState gets the baseFee and tipCap for the next block.
func (n *nodeClient) netFeeState(ctx context.Context) (baseFees, tipCap *big.Int, err error) {
	hdr, err := n.bestHeader(ctx)
	if err != nil {
		return nil, nil, err
	}

	base := misc.CalcBaseFee(n.leth.ApiBackend.ChainConfig(), hdr)

	if base.Cmp(minGasPrice) < 0 {
		base.Set(minGasPrice)
	}

	tip, err := n.leth.ApiBackend.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, err
	}

	if tip.Cmp(minGasTipCap) < 0 {
		tip = new(big.Int).Set(minGasTipCap)
	}

	return base, tip, nil
}

// getCodeAt retrieves the runtime bytecode at a certain address.
func (n *nodeClient) getCodeAt(ctx context.Context, contractAddr common.Address) ([]byte, error) {
	state, err := n.stateAt(ctx, rpc.PendingBlockNumber)
	if err != nil {
		return nil, err
	}
	code := state.GetCode(contractAddr)
	return code, state.Error()
}

// txOpts generates a set of TransactOpts for the account. If maxFeeRate is
// zero, it will be calculated as double the current baseFee. The tip will be
// added automatically.
func (n *nodeClient) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate *big.Int) (*bind.TransactOpts, error) {
	baseFee, gasTipCap, err := n.netFeeState(ctx)
	if maxFeeRate == nil {
		maxFeeRate = new(big.Int).Mul(baseFee, big.NewInt(2))
	}
	if err != nil {
		return nil, err
	}
	return newTxOpts(ctx, n.creds.addr, val, maxGas, maxFeeRate, gasTipCap), nil
}

// isRedeemable checks if the swap identified by secretHash is redeemable using secret.
func (n *nodeClient) isRedeemable(secretHash [32]byte, secret [32]byte, contractVer uint32) (redeemable bool, err error) {
	return redeemable, n.withcontractor(contractVer, func(c contractor) error {
		redeemable, err = c.isRedeemable(secretHash, secret)
		return err
	})
}

// transactionConfirmations gets the number of confirmations for the specified
// transaction.
func (n *nodeClient) transactionConfirmations(ctx context.Context, txHash common.Hash) (uint32, error) {
	// We'll check the local tx pool first, since from what I can tell, a light
	// client always requests tx data from the network for anything else.
	if tx := n.leth.ApiBackend.GetPoolTransaction(txHash); tx != nil {
		return 0, nil
	}
	hdr, err := n.bestHeader(ctx)
	if err != nil {
		return 0, err
	}
	tx, _, blockHeight, _, err := n.leth.ApiBackend.GetTransaction(ctx, txHash)
	if err != nil {
		return 0, err
	}
	if tx != nil {
		return uint32(hdr.Number.Uint64() - blockHeight + 1), nil
	}
	// TODO: There may be a race between when the tx is removed from our local
	// tx pool, and when our peers are ready to supply the info. I saw a
	// CoinNotFoundError in TestAccount/testSendTransaction, but haven't
	// reproduced.
	return 0, asset.CoinNotFoundError
}

// newTxOpts is a constructor for a TransactOpts.
func newTxOpts(ctx context.Context, from common.Address, val, maxGas uint64, maxFeeRate, gasTipCap *big.Int) *bind.TransactOpts {
	if gasTipCap.Cmp(maxFeeRate) > 0 {
		gasTipCap = maxFeeRate
	}
	return &bind.TransactOpts{
		Context:   ctx,
		From:      from,
		Value:     dexeth.GweiToWei(val),
		GasFeeCap: maxFeeRate,
		GasTipCap: gasTipCap,
		GasLimit:  maxGas,
	}
}
