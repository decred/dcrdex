// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func init() {
	asset.Register(assetName, &Driver{})
}

const (
	version   = 0
	assetName = "eth"
	// The blockPollInterval is the delay between calls to bestBlockHash to
	// check for new blocks.
	blockPollInterval  = time.Second
	ethContractVersion = 0
)

var backendInfo = &asset.BackendInfo{
	SupportsDynamicTxFee: true,
}

var _ asset.Driver = (*Driver)(nil)

// Driver implements asset.Driver.
type Driver struct{}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Ethereum. This must be a transaction hash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return txHash.String(), nil
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexeth.UnitInfo
}

// ethFetcher represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient. For testing, it can be satisfied by a stub.
//
// TODO: At some point multiple contracts will need to be used, at least for
// transitory periods when updating the contract, and possibly a random
// contract setup, and so contract addresses may need to be an argument in some
// of these methods.
type ethFetcher interface {
	bestHeader(ctx context.Context) (*types.Header, error)
	blockNumber(ctx context.Context) (uint64, error)
	headerByHeight(ctx context.Context, height uint64) (*types.Header, error)
	connect(ctx context.Context, ipc string, contractAddr *common.Address) error
	shutdown()
	suggestGasTipCap(ctx context.Context) (*big.Int, error)
	syncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error)
	transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error)
	accountBalance(ctx context.Context, addr common.Address) (*big.Int, error)
}

type hashN struct {
	height uint64
	hash   common.Hash
}

// Backend is an asset backend for Ethereum. It has methods for fetching output
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. Backend implements asset.Backend, so provides
// exported methods for DEX-related blockchain info.
type Backend struct {
	// A connection-scoped Context is used to cancel active RPCs on
	// connection shutdown.
	rpcCtx     context.Context
	cancelRPCs context.CancelFunc
	cfg        *config
	node       ethFetcher

	// bestHash caches the last know best block hash and height and is used
	// to detect reorgs. Only accessed in Connect and poll which is
	// syncronous so no locking is needed presently.
	bestHash hashN

	// The backend provides block notification channels through the BlockChannel
	// method.
	blockChansMtx sync.RWMutex
	blockChans    map[chan *asset.BlockUpdate]struct{}

	// A logger will be provided by the DEX. All logging should use the provided
	// logger.
	log dex.Logger
	// contractAddr is the address of the swap contract used for swaps.
	//
	// TODO: Allow supporting multiple addresses/contracts. Needed in the
	// case of updating where two contracts may be valid for some time,
	// possibly disallowing initialization for the deprecated one only.
	contractAddr common.Address
	// initTxSize is the gas used for an initiation transaction with one swap.
	initTxSize uint32
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)

// Check that Backend satisfies the AccountBalancer interface.
var _ asset.AccountBalancer = (*Backend)(nil)

// unconnectedETH returns a Backend without a node. The node should be set
// before use.
func unconnectedETH(logger dex.Logger, cfg *config) (*Backend, error) {
	// TODO: At some point multiple contracts will need to be used, at
	// least for transitory periods when updating the contract, and
	// possibly a random contract setup, and so this section will need to
	// change to support multiple contracts.
	contractAddr, exists := dexeth.ContractAddresses[0][cfg.network]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no eth contract for version 0, net %s", cfg.network)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Backend{
		rpcCtx:       ctx,
		cancelRPCs:   cancel,
		cfg:          cfg,
		log:          logger,
		blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
		contractAddr: contractAddr,
		initTxSize:   uint32(dexeth.InitGas(1, version)),
	}, nil
}

// NewBackend is the exported constructor by which the DEX will import the
// Backend.
func NewBackend(ipc string, logger dex.Logger, network dex.Network) (*Backend, error) {
	cfg, err := load(ipc, network)
	if err != nil {
		return nil, err
	}
	return unconnectedETH(logger, cfg)
}

func (eth *Backend) shutdown() {
	eth.node.shutdown()
}

// Connect connects to the node RPC server and initializes some variables.
func (eth *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	c := rpcclient{}
	if err := c.connect(ctx, eth.cfg.ipc, &eth.contractAddr); err != nil {
		return nil, err
	}
	eth.node = &c

	// Prime the best block hash and height.
	hdr, err := c.bestHeader(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block header from geth: %w", err)
	}
	eth.bestHash = hashN{
		height: hdr.Number.Uint64(),
		hash:   hdr.Hash(),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		eth.run(ctx)
		wg.Done()
	}()
	return &wg, nil
}

// TxData fetches the raw transaction data.
func (eth *Backend) TxData(coinID []byte) ([]byte, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("coin ID decoding error: %v", err)
	}

	tx, _, err := eth.node.transaction(eth.rpcCtx, txHash)
	if err != nil {
		return nil, fmt.Errorf("error retrieving transaction: %w", err)
	}
	if tx == nil { // Possible?
		return nil, fmt.Errorf("no transaction %s", txHash)
	}

	return tx.MarshalBinary()
}

// InitTxSize is an upper limit on the gas used for an initiation.
func (eth *Backend) InitTxSize() uint32 {
	return eth.initTxSize
}

// InitTxSizeBase is the same as InitTxSize for ETH.
func (eth *Backend) InitTxSizeBase() uint32 {
	return eth.initTxSize
}

// FeeRate returns the current optimal fee rate in gwei / gas.
func (eth *Backend) FeeRate(ctx context.Context) (uint64, error) {
	hdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting best header: %w", err)
	}

	if hdr.BaseFee == nil {
		return 0, errors.New("eth block header does not contain base fee")
	}

	suggestedGasTipCap, err := eth.node.suggestGasTipCap(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting suggested gas tip cap: %w", err)
	}

	feeRate := new(big.Int).Add(
		suggestedGasTipCap,
		new(big.Int).Mul(hdr.BaseFee, big.NewInt(2)))

	feeRateGwei, err := dexeth.WeiToGweiUint64(feeRate)
	if err != nil {
		return 0, fmt.Errorf("failed to convert wei to gwei: %w", err)
	}

	return feeRateGwei, nil
}

// Info provides some general information about the backend.
func (*Backend) Info() *asset.BackendInfo {
	return backendInfo
}

// ValidateFeeRate checks that the transaction fees used to initiate the
// contract are sufficient. For most assets only the contract.FeeRate() cannot
// be less than reqFeeRate, but for Eth, the gasTipCap must also be checked.
func (eth *Backend) ValidateFeeRate(contract *asset.Contract, reqFeeRate uint64) bool {
	coin := contract.Coin
	sc, ok := coin.(*swapCoin)
	if !ok {
		eth.log.Error("eth contract coin type must be a swapCoin but got %T", sc)
		return false
	}

	if sc.gasTipCap < dexeth.MinGasTipCap {
		return false
	}

	return contract.FeeRate() >= reqFeeRate
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the eth package. Part of the asset.Backend interface.
func (eth *Backend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	c := make(chan *asset.BlockUpdate, size)
	eth.blockChansMtx.Lock()
	defer eth.blockChansMtx.Unlock()
	eth.blockChans[c] = struct{}{}
	return c
}

// ValidateContract ensures that contractData encodes both the expected contract
// version targeted and the secret hash.
func (eth *Backend) ValidateContract(contractData []byte) error {
	ver, _, err := dexeth.DecodeContractData(contractData)
	if err != nil { // ensures secretHash is proper length
		return err
	}
	if ver != version {
		return fmt.Errorf("incorrect contract version %d, wanted %d", ver, version)
	}
	return nil
}

// Contract is part of the asset.Backend interface. The contractData bytes
// encodes both the contract version targeted and the secret hash.
func (eth *Backend) Contract(coinID, contractData []byte) (*asset.Contract, error) {
	// newSwapCoin validates the contractData, extracting version, secret hash,
	// counterparty address, and locktime. The supported version is enforced.
	sc, err := eth.newSwapCoin(coinID, contractData)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}

	// Confirmations performs some extra swap status checks if the the tx is
	// mined. For init coins, this uses the contract account's state (if it is
	// mined) to verify the value, counterparty, and lock time.
	_, err = sc.Confirmations(eth.rpcCtx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}
	return &asset.Contract{
		Coin:         sc,
		SwapAddress:  sc.init.Participant.String(),
		ContractData: contractData,
		SecretHash:   sc.secretHash[:],
		TxData:       sc.serializedTx,
		LockTime:     sc.init.LockTime,
	}, nil
}

// ValidateSecret checks that the secret satisfies the secret hash.
func (eth *Backend) ValidateSecret(secret, contractData []byte) bool {
	_, secretHash, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return false
	}
	sh := sha256.Sum256(secret)
	return bytes.Equal(sh[:], secretHash[:])
}

// Synced is true if the blockchain is ready for action.
func (eth *Backend) Synced() (bool, error) {
	// node.SyncProgress will return nil both before syncing has begun and
	// after it has finished. In order to discern when syncing has begun,
	// check that the best header came in under MaxBlockInterval.
	sp, err := eth.node.syncProgress(eth.rpcCtx)
	if err != nil {
		return false, err
	}
	if sp != nil {
		return false, nil
	}
	bh, err := eth.node.bestHeader(eth.rpcCtx)
	if err != nil {
		return false, err
	}
	// Time in the header is in seconds.
	nowInSecs := time.Now().Unix() / 1000
	timeDiff := nowInSecs - int64(bh.Time)
	return timeDiff < dexeth.MaxBlockInterval, nil
}

// Redemption returns a coin that represents a contract redemption. redeemCoinID
// should be the transaction that sent a redemption, while contractCoinID is the
// swap contract this redemption redeems.
func (eth *Backend) Redemption(redeemCoinID, _, contractData []byte) (asset.Coin, error) {
	// newSwapCoin uses the contract account's state to validate the
	// contractData, extracting version, secret, and secret hash. The supported
	// version is enforced.
	rc, err := eth.newRedeemCoin(redeemCoinID, contractData)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}

	// Confirmations performs some extra swap status checks if the the tx
	// is mined. For redeem coins, this is just a swap state check.
	_, err = rc.Confirmations(eth.rpcCtx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}
	return rc, nil
}

// ValidateCoinID attempts to decode the coinID.
func (eth *Backend) ValidateCoinID(coinID []byte) (string, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return txHash.String(), nil
}

// CheckAddress checks that the given address is parseable.
func (eth *Backend) CheckAddress(addr string) bool {
	return common.IsHexAddress(addr)
}

// AccountBalance retrieves the current account balance, including the effects
// of known unmined transactions.
func (eth *Backend) AccountBalance(addrStr string) (uint64, error) {
	bigBal, err := eth.node.accountBalance(eth.rpcCtx, common.HexToAddress(addrStr))
	if err != nil {
		return 0, fmt.Errorf("accountBalance error: %w", err)
	}
	return dexeth.WeiToGweiUint64(bigBal)
}

// ValidateSignature checks that the pubkey is correct for the address and
// that the signature shows ownership of the associated private key.
func (eth *Backend) ValidateSignature(addr string, pubkey, msg, sig []byte) error {
	const sigLen = 64
	if len(sig) != sigLen {
		return fmt.Errorf("expected sig length of %d bytes but got %d", sigLen, len(sig))
	}
	ethPK, err := crypto.UnmarshalPubkey(pubkey)
	if err != nil {
		return fmt.Errorf("unable to unmarshal pubkey: %v", err)
	}
	// Ensure the pubkey matches the funding coin's account address.
	// Internally, this will take the last twenty bytes of crypto.Keccak256
	// of all but the first byte of the pubkey bytes and wrap it as a
	// common.Address.
	pubkeyAddr := crypto.PubkeyToAddress(*ethPK)
	if addr != pubkeyAddr.String() {
		return errors.New("pubkey does not correspond to address")
	}
	r := new(secp256k1.ModNScalar)
	if overflow := r.SetByteSlice(sig[0:32]); overflow {
		return errors.New("invalid signature: r >= group order")
	}
	s := new(secp256k1.ModNScalar)
	if overflow := s.SetByteSlice(sig[32:64]); overflow {
		return errors.New("invalid signature: s >= group order")
	}
	ecdsaSig := ecdsa.NewSignature(r, s)

	pk, err := secp256k1.ParsePubKey(pubkey)
	if err != nil {
		return err
	}
	// Verify the signature.
	if !ecdsaSig.Verify(crypto.Keccak256(msg), pk) {
		return errors.New("cannot verify signature")
	}
	return nil
}

// poll pulls the best hash from an eth node and compares that to a stored
// hash. If the same does nothing. If different, updates the stored hash and
// notifies listeners on block chans.
func (eth *Backend) poll(ctx context.Context) {
	best := &eth.bestHash
	send := func(reorg bool, err error) {
		if err != nil {
			eth.log.Error(err)
		}
		eth.blockChansMtx.RLock()
		for c := range eth.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Reorg: reorg,
				Err:   err,
			}:
			default:
				eth.log.Error("failed to send block update on blocking channel")
			}
		}
		eth.blockChansMtx.RUnlock()
	}
	bhdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		send(false, fmt.Errorf("error getting best block header from geth: %w", err))
		return
	}
	if bhdr.Hash() == best.hash {
		// Same hash, nothing to do.
		return
	}
	update := func(reorg bool, fastBlocks bool) {
		hash := bhdr.Hash()
		height := bhdr.Number.Uint64()
		str := fmt.Sprintf("Tip change from %s (%d) to %s (%d).",
			best.hash, best.height, hash, height)
		switch {
		case reorg:
			str += " Detected reorg."
		case fastBlocks:
			str += " Fast blocks."
		}
		eth.log.Debug(str)
		best.hash = hash
		best.height = height
	}
	if bhdr.ParentHash == best.hash {
		// Sequential hash, report a block update.
		update(false, false)
		send(false, nil)
		return
	}
	// Either a block was skipped or a reorg happened. We can only detect
	// the reorg if our last best header's hash has changed. Otherwise,
	// assume no reorg and report the new block change.
	//
	// headerByHeight will only return mainchain headers.
	hdr, err := eth.node.headerByHeight(ctx, best.height)
	if err != nil {
		send(false, fmt.Errorf("error getting block header from geth: %w", err))
		return
	}
	if hdr.Hash() == best.hash {
		// Our recorded hash is still on main chain so there is no reorg
		// that we know of. The chain has advanced more than one block.
		update(false, true)
		send(false, nil)
		return
	}
	// The block for our recorded hash was forked off and the chain had a
	// reorganization.
	update(true, false)
	send(true, nil)
}

// run processes the queue and monitors the application context.
func (eth *Backend) run(ctx context.Context) {
	done := ctx.Done()
	wait := make(chan struct{})
	// Shut down the RPC client on ctx.Done().
	go func() {
		<-done
		eth.cancelRPCs()
		eth.shutdown()
		close(wait)
	}()

	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()

out:
	for {
		select {
		case <-blockPoll.C:
			eth.poll(ctx)
		case <-done:
			break out
		}
	}
	// Wait for the RPC client to shut down.
	<-wait
}
