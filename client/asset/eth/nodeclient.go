// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexerc20 "decred.org/dcrdex/dex/networks/erc20"
	v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	ethv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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

	tokenSwapContract     *v0.ERC20Swap
	tokenSwapContractAddr *common.Address
}

func newNodeClient(dir string, net dex.Network, log dex.Logger) (*nodeClient, error) {
	node, err := prepareNode(&nodeConfig{
		net:    net,
		appDir: dir,
		logger: log.SubLogger(asset.InternalNodeLoggerName),
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

	tokenSwapContractAddr := dexerc20.ContractAddresses[0][n.net]
	n.tokenSwapContractAddr = &tokenSwapContractAddr
	n.tokenSwapContract, err = v0.NewERC20Swap(*n.tokenSwapContractAddr, n.ec)
	if err != nil {
		return err
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

func (n *nodeClient) peerCount() uint32 {
	return uint32(n.p2pSrv.PeerCount())
}

// bestHeader gets the best header at the time of calling.
func (n *nodeClient) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.leth.ApiBackend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
}

// block gets the block identified by hash.
func (n *nodeClient) block(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return n.leth.ApiBackend.BlockByHash(ctx, hash)
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
		// For legacy transactions, GasFeeCap returns gas price
		if gasFeeCap := tx.GasFeeCap(); gasFeeCap != nil {
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
	tx, blockHash, _, index, err := n.leth.ApiBackend.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, fmt.Errorf("transaction %v not found", txHash)
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

// swap gets a swap keyed by secretHash in the contract.
func (n *nodeClient) swap(ctx context.Context, secretHash [32]byte, contractVer uint32) (swap *dexeth.SwapState, err error) {
	return swap, n.withcontractor(contractVer, func(c contractor) error {
		swap, err = c.swap(ctx, secretHash)
		return err
	})
}

// signData uses the private key of the address to sign a piece of data.
// The wallet must be unlocked to use this function.
func (n *nodeClient) signData(data []byte) (sig, pubKey []byte, err error) {
	sig, err = n.creds.ks.SignHash(*n.creds.acct, crypto.Keccak256(data))
	if err != nil {
		return nil, nil, err
	}
	if len(sig) != 65 {
		return nil, nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	pubKey, err = recoverPubkey(crypto.Keccak256(data), sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %w", err)
	}

	// Lop off the "recovery id", since we already recovered the pub key and
	// it's not used for validation.
	sig = sig[:64]

	return
}

func (n *nodeClient) addSignerToOpts(txOpts *bind.TransactOpts) error {
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return n.creds.wallet.SignTx(*n.creds.acct, tx, n.chainID)
	}
	return nil
}

// signTransaction signs a transaction.
func (n *nodeClient) signTransaction(tx *types.Transaction) (*types.Transaction, error) {
	return n.creds.ks.SignTx(*n.creds.acct, tx, n.chainID)
}

// initiate initiates multiple swaps in the same transaction.
func (n *nodeClient) initiate(ctx context.Context, contracts []*asset.Contract, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	gas := dexeth.InitGas(len(contracts), contractVer)
	var val uint64
	for _, c := range contracts {
		val += c.Value
	}
	txOpts, _ := n.txOpts(ctx, val, gas, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}
	return tx, n.withcontractor(contractVer, func(c contractor) error {
		tx, err = c.initiate(txOpts, contracts)
		return err
	})
}

// estimateInitGas checks the amount of gas that is used for the
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

// redeem redeems a swap contract. Any on-chain failure, such as this secret not
// matching the hash, will not cause this to error.
func (n *nodeClient) redeem(ctx context.Context, redemptions []*asset.Redemption, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	gas := dexeth.RedeemGas(len(redemptions), contractVer)
	txOpts, _ := n.txOpts(ctx, 0, gas, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}
	return tx, n.withcontractor(contractVer, func(c contractor) error {
		tx, err = c.redeem(txOpts, redemptions)
		return err
	})
}

// refund refunds a swap contract using the account controlled by the wallet.
// Any on-chain failure, such as the locktime not being past, will not cause
// this to error.
func (n *nodeClient) refund(ctx context.Context, secretHash [32]byte, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	gas := dexeth.RefundGas(contractVer)
	txOpts, _ := n.txOpts(ctx, 0, gas, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
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

	minGasTipCapWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
	if tip.Cmp(minGasTipCapWei) < 0 {
		tip = new(big.Int).Set(minGasTipCapWei)
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
//
// NOTE: The v0 contract will reuse account nonces if the tx is not yet
// confirmed. To avoid that be sure to get the next nonce from the light eth
// node pool api while holding the nodeClient nonceSendMtx and set manually.
// Hold the mutex until the tx has been sent or errors on sending.
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

// isRefundable checks if the swap identified by secretHash is refundable.
func (n *nodeClient) isRefundable(secretHash [32]byte, contractVer uint32) (refundable bool, err error) {
	return refundable, n.withcontractor(contractVer, func(c contractor) error {
		refundable, err = c.isRefundable(secretHash)
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
	n.log.Warnf("transactionConfirmations: cannot find %v", txHash)
	return 0, asset.CoinNotFoundError
}

// sendSignedTransaction injects a signed transaction into the pending pool for execution.
func (n *nodeClient) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	return n.leth.ApiBackend.SendTx(ctx, tx)
}

// tokenSwap gets a swap keyed by secretHash in the token swap contract.
func (n *nodeClient) tokenSwap(ctx context.Context, secretHash [32]byte) (ethv0.ETHSwapSwap, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}
	return n.tokenSwapContract.Swap(callOpts, secretHash)
}

// tokenBalance checks the token balance of the account handled by the wallet.
func (n *nodeClient) tokenBalance(ctx context.Context, tokenAddr common.Address) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}

	tokenContract, err := dexerc20.NewIERC20(tokenAddr, n.ec)
	if err != nil {
		return nil, err
	}
	return tokenContract.BalanceOf(callOpts, n.address())
}

// tokenAllowance checks the amount of tokens that the swap contract is approved
// to spend on behalf of the account handled by the wallet.
func (n *nodeClient) tokenAllowance(ctx context.Context, tokenAddr common.Address) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}

	tokenContract, err := dexerc20.NewIERC20(tokenAddr, n.ec)
	if err != nil {
		return nil, err
	}
	return tokenContract.Allowance(callOpts, n.address(), *n.tokenSwapContractAddr)
}

// approveToken approves the token swap contract to spend tokens on behalf of
// account handled by the wallet.
func (n *nodeClient) approveToken(ctx context.Context, tokenAddr common.Address, amount *big.Int, maxFeeRate uint64) (tx *types.Transaction, err error) {
	txOpts, _ := n.txOpts(ctx, 0, 3e5, dexeth.GweiToWei(maxFeeRate))
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	tokenContract, err := dexerc20.NewIERC20(tokenAddr, n.ec)
	if err != nil {
		return nil, err
	}
	return tokenContract.Approve(txOpts, *n.tokenSwapContractAddr, amount)
}

// initiateToken initiates multiple token swaps in the same transaction.
func (n *nodeClient) initiateToken(ctx context.Context, initiations []ethv0.ETHSwapInitiation, token common.Address, maxFeeRate uint64) (tx *types.Transaction, err error) {
	// TODO: reject if there is duplicate secret hash
	// TODO: use estimated gas
	txOpts, _ := n.txOpts(ctx, 0, 1e6, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return n.tokenSwapContract.Initiate(txOpts, initiations)
}

// redeemToken redeems multiple token swaps. All redemptions must involve the
// same ERC20 token.
func (n *nodeClient) redeemToken(ctx context.Context, redemptions []ethv0.ETHSwapRedemption, maxFeeRate uint64) (tx *types.Transaction, err error) {
	// TODO: reject if there is duplicate secret hash
	// TODO: use estimated gas
	txOpts, _ := n.txOpts(ctx, 0, 300000, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return n.tokenSwapContract.Redeem(txOpts, redemptions)
}

// tokenIsRedeemable checks if a token swap identified by secretHash is currently
// redeemable using secret.
func (n *nodeClient) tokenIsRedeemable(ctx context.Context, secretHash, secret [32]byte) (bool, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}
	return n.tokenSwapContract.IsRedeemable(callOpts, secretHash, secret)
}

// refundToken refunds a token swap.
func (n *nodeClient) refundToken(ctx context.Context, secretHash [32]byte, maxFeeRate uint64) (tx *types.Transaction, err error) {
	// TODO: use estimated gas
	txOpts, _ := n.txOpts(ctx, 0, 5e5, dexeth.GweiToWei(maxFeeRate))
	n.nonceSendMtx.Lock()
	defer n.nonceSendMtx.Unlock()
	nonce, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)
	if err := n.addSignerToOpts(txOpts); err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return n.tokenSwapContract.Refund(txOpts, secretHash)
}

// tokenIsRefundable checks if a token swap identified by secretHash is
// refundable.
func (n *nodeClient) tokenIsRefundable(ctx context.Context, secretHash [32]byte) (bool, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}
	return n.tokenSwapContract.IsRefundable(callOpts, secretHash)
}

func (n *nodeClient) getTokenAddress(ctx context.Context) (common.Address, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    n.address(),
		Context: ctx,
	}

	return n.tokenSwapContract.TokenAddress(callOpts)
}

// estimateInitTokenGas checks the amount of gas that is used to redeem
// token swaps.
func (n *nodeClient) estimateInitTokenGas(ctx context.Context, tokenAddr common.Address, num int) (uint64, error) {
	initiations := make([]ethv0.ETHSwapInitiation, 0, num)
	for j := 0; j < num; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, ethv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(1),
			SecretHash:      secretHash,
			Participant:     n.address(),
			Value:           big.NewInt(1),
		})
	}

	parsedABI, err := abi.JSON(strings.NewReader(v0.ERC20SwapMetaData.ABI))
	if err != nil {
		return 0, err
	}

	data, err := parsedABI.Pack("initiate", initiations)
	if err != nil {
		return 0, nil
	}

	return n.ec.EstimateGas(ctx, ethereum.CallMsg{
		From:  n.address(),
		To:    n.tokenSwapContractAddr,
		Value: big.NewInt(0),
		Gas:   0,
		Data:  data,
	})
}

// estimateRedeemTokenGas checks the amount of gas that is used to redeem
// token swaps.
func (n *nodeClient) estimateRedeemTokenGas(ctx context.Context, secrets [][32]byte) (uint64, error) {
	redemptions := make([]ethv0.ETHSwapRedemption, 0, len(secrets))
	for _, secret := range secrets {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		redemptions = append(redemptions, ethv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: sha256.Sum256(secret[:]),
		})
	}

	parsedABI, err := abi.JSON(strings.NewReader(ethv0.ETHSwapMetaData.ABI))
	if err != nil {
		return 0, err
	}

	data, err := parsedABI.Pack("redeem", redemptions)
	if err != nil {
		return 0, nil
	}

	return n.ec.EstimateGas(ctx, ethereum.CallMsg{
		From:  n.address(),
		To:    n.tokenSwapContractAddr,
		Value: big.NewInt(0),
		Gas:   0,
		Data:  data,
	})
}

// estimateRefundTokenGas checks the amount of gas that is used to refund
// a token swap.
func (n *nodeClient) estimateRefundTokenGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	parsedABI, err := abi.JSON(strings.NewReader(ethv0.ETHSwapMetaData.ABI))
	if err != nil {
		return 0, err
	}

	data, err := parsedABI.Pack("refund", secretHash)
	if err != nil {
		return 0, nil
	}

	return n.ec.EstimateGas(ctx, ethereum.CallMsg{
		From:  n.address(),
		To:    n.tokenSwapContractAddr,
		Value: big.NewInt(0),
		Gas:   0,
		Data:  data,
	})
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
