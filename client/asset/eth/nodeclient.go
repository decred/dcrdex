// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
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
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/ethereum/go-ethereum/consensus/misc"
)

const contractVersionNewest = ^uint32(0)

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
	net     dex.Network
	log     dex.Logger
	creds   *accountCredentials
	p2pSrv  *p2p.Server
	ec      *ethclient.Client
	node    *node.Node
	leth    *les.LightEthereum
	chainID *big.Int
}

func newNodeClient(dir string, chainID int64, net dex.Network, log dex.Logger) (*nodeClient, error) {
	node, err := prepareNode(&nodeConfig{
		net:    net,
		appDir: dir,
		logger: log.SubLogger(asset.InternalNodeLoggerName),
	})
	if err != nil {
		return nil, err
	}

	creds, err := nodeCredentials(node)
	if err != nil {
		return nil, err
	}

	return &nodeClient{
		chainID: big.NewInt(chainID),
		node:    node,
		creds:   creds,
		net:     net,
		log:     log,
	}, nil
}

func (n *nodeClient) address() common.Address {
	return n.creds.addr
}

func (n *nodeClient) chainConfig() *params.ChainConfig {
	return n.leth.ApiBackend.ChainConfig()
}

// connect connects to a node. It then wraps ethclient's client and
// bundles commands in a form we can easily use.
func (n *nodeClient) connect(ctx context.Context) (err error) {
	n.leth, err = startNode(n.chainID.Int64(), n.node, n.net)
	if err != nil {
		return err
	}

	client, err := n.node.Attach()
	if err != nil {
		return fmt.Errorf("unable to dial rpc: %v", err)
	}
	n.ec = ethclient.NewClient(client)
	n.p2pSrv = n.node.Server()
	if n.p2pSrv == nil {
		return fmt.Errorf("no *p2p.Server")
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

func (n *nodeClient) peerCount() uint32 {
	return uint32(n.p2pSrv.PeerCount())
}

// bestHeader gets the best header at the time of calling.
func (n *nodeClient) bestHeader(ctx context.Context) (*types.Header, error) {
	return n.leth.ApiBackend.HeaderByNumber(ctx, rpc.LatestBlockNumber)
}

func (n *nodeClient) headerByHash(ctx context.Context, txHash common.Hash) (*types.Header, error) {
	return n.leth.BlockChain().GetHeaderByHash(txHash), nil
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

// transactionReceipt retrieves the transaction's receipt.
func (n *nodeClient) transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error) {
	tx, blockHash, _, index, err := n.leth.ApiBackend.GetTransaction(ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, nil, asset.CoinNotFoundError
		}
		return nil, nil, err
	}
	if tx == nil {
		return nil, nil, fmt.Errorf("%w: transaction %v not found", asset.CoinNotFoundError, txHash)
	}
	receipts, err := n.leth.ApiBackend.GetReceipts(ctx, blockHash)
	if err != nil {
		return nil, nil, err
	}
	if len(receipts) <= int(index) {
		return nil, nil, fmt.Errorf("no receipt at index %d in block %s for tx %s", index, blockHash, txHash)
	}
	receipt := receipts[index]
	if receipt == nil {
		return nil, nil, fmt.Errorf("nil receipt at index %d in block %s for tx %s", index, blockHash, txHash)
	}
	return receipt, tx, nil
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

// getConfirmedNonce returns the nonce of the account in the state of a
// certain block.
func (n *nodeClient) getConfirmedNonce(ctx context.Context) (uint64, error) {
	state, _, err := n.leth.ApiBackend.StateAndHeaderByNumber(ctx, rpc.PendingBlockNumber)
	if err != nil {
		return 0, err
	}
	return state.GetNonce(n.address()), nil
}

// sendTransaction sends a tx. The nonce should be set in txOpts.
func (n *nodeClient) sendTransaction(ctx context.Context, txOpts *bind.TransactOpts,
	to common.Address, data []byte) (*types.Transaction, error) {

	tx, err := n.creds.ks.SignTx(*n.creds.acct, types.NewTx(&types.DynamicFeeTx{
		To:        &to,
		ChainID:   n.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
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

// syncProgress return the current sync progress and the best block's header
// time in seconds. Returns no error and nil when not syncing.
func (n *nodeClient) syncProgress(ctx context.Context) (*ethereum.SyncProgress, uint64, error) {
	hdr, err := n.bestHeader(ctx)
	if err != nil {
		return nil, 0, err
	}
	p := n.leth.ApiBackend.SyncProgress()
	return &p, hdr.Time, nil
}

// signData uses the private key of the address to sign a piece of data.
// The wallet must be unlocked to use this function.
func (n *nodeClient) signData(data []byte) (sig, pubKey []byte, err error) {
	return signData(n.creds, data)
}

func (n *nodeClient) addSignerToOpts(txOpts *bind.TransactOpts) {
	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return n.creds.wallet.SignTx(*n.creds.acct, tx, n.chainID)
	}
}

// currentFees gets the baseFee and tipCap for the next block.
func (n *nodeClient) currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error) {
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
// added automatically. Sets the passed nonce if supplied. If nonce is nil the
// next nonce will be fetched and the passed argument altered.
//
// NOTE: The nonce included in the txOpts must be sent before txOpts is used
// again. The caller should ensure that txOpts -> send sequence is synchronized.
func (n *nodeClient) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, nonce *big.Int) (*bind.TransactOpts, error) {
	baseFee, gasTipCap, err := n.currentFees(ctx)
	if err != nil {
		return nil, err
	}

	if maxFeeRate == nil {
		maxFeeRate = new(big.Int).Mul(baseFee, big.NewInt(2))
	}

	// If nonce is not nil, this indicates that we are trying to re-send an
	// old transaction with higher fee in order to ensure it is mined.
	if nonce == nil {
		n, err := n.leth.ApiBackend.GetPoolNonce(ctx, n.creds.addr)
		if err != nil {
			return nil, fmt.Errorf("error getting nonce: %v", err)
		}
		nonce = new(big.Int).SetUint64(n)
	}
	txOpts := newTxOpts(ctx, n.creds.addr, val, maxGas, maxFeeRate, gasTipCap)
	txOpts.Nonce = nonce
	n.addSignerToOpts(txOpts)

	return txOpts, nil
}

func (n *nodeClient) contractBackend() bind.ContractBackend {
	return n.ec
}

// getTransaction looks up a transaction and returns the transaction and the
// height of the block it was included in. -1 is returned for the height of
// unmined transactions.
func (n *nodeClient) getTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, int64, error) {
	// We'll check the local tx pool first, since from what I can tell, a light
	// client always requests tx data from the network for anything else.
	if tx := n.leth.ApiBackend.GetPoolTransaction(txHash); tx != nil {
		return tx, -1, nil
	}
	tx, _, blockHeight, _, err := n.leth.ApiBackend.GetTransaction(ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, 0, asset.CoinNotFoundError
		}
		return nil, 0, err
	}
	if tx != nil {
		return tx, int64(blockHeight), nil
	}
	// TODO: There may be a race between when the tx is removed from our local
	// tx pool, and when our peers are ready to supply the info. I saw a
	// CoinNotFoundError in TestAccount/testSendTransaction, but haven't
	// reproduced.
	n.log.Warnf("getTransaction: cannot find %v", txHash)
	return nil, 0, asset.CoinNotFoundError
}

// transactionConfirmations gets the number of confirmations for the specified
// transaction.
func (n *nodeClient) transactionConfirmations(ctx context.Context, txHash common.Hash) (uint32, error) {
	_, blockHeight, err := n.getTransaction(ctx, txHash)
	if err != nil || blockHeight < 0 {
		return 0, err
	}

	hdr, err := n.bestHeader(ctx)
	if err != nil {
		return 0, err
	}

	if hdr.Number.Int64() < blockHeight { // avoid potential overflow
		return 0, nil
	}

	return uint32(hdr.Number.Int64() - blockHeight + 1), nil
}

// sendSignedTransaction injects a signed transaction into the pending pool for execution.
func (n *nodeClient) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	return n.leth.ApiBackend.SendTx(ctx, tx)
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

func gases(contractVer uint32, versionedGases map[uint32]*dexeth.Gases) *dexeth.Gases {
	if contractVer != contractVersionNewest {
		return versionedGases[contractVer]
	}
	var bestVer uint32
	var bestGases *dexeth.Gases
	for ver, gases := range versionedGases {
		if ver >= bestVer {
			bestGases = gases
			bestVer = ver
		}
	}
	return bestGases
}
