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
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
)

func init() {
	asset.Register(assetName, &Driver{})
}

const (
	version   = 0
	assetName = "eth"
	// The blockPollInterval is the delay between calls to bestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second
	// TODO: Fill in with an addresses. Also consider upgrades where one
	// contract will be good for current active swaps, but a new one is
	// required for new swaps.
	mainnetContractAddr = ""
	testnetContractAddr = ""
	simnetContractAddr  = ""
)

var (
	zeroHash                       = common.Hash{}
	notImplementedErr              = errors.New("not implemented")
	_                 asset.Driver = (*Driver)(nil)
)

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

// DecodeCoinID creates a human-readable representation of a coin ID for Ethereum.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txHash, err := decodeCoinID(coinID)
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
	bestBlockHash(ctx context.Context) (common.Hash, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	block(ctx context.Context, hash common.Hash) (*types.Block, error)
	connect(ctx context.Context, ipc string, contractAddr *common.Address) error
	shutdown()
	suggestGasPrice(ctx context.Context) (*big.Int, error)
	syncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	blockNumber(ctx context.Context) (uint64, error)
	peers(ctx context.Context) ([]*p2p.PeerInfo, error)
	swap(ctx context.Context, secretHash [32]byte) (*dexeth.ETHSwapSwap, error)
	transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error)
	accountBalance(ctx context.Context, addr common.Address) (*big.Int, error)
}

// Backend is an asset backend for Ethereum. It has methods for fetching output
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. Backend implements asset.Backend, so provides
// exported methods for DEX-related blockchain info.
type Backend struct {
	// A connection-scoped Context is used to cancel active RPCs on
	// connection shutdown.
	ver        uint32
	rpcCtx     context.Context
	cancelRPCs context.CancelFunc
	cfg        *config
	node       ethFetcher
	// The backend provides block notification channels through the BlockChannel
	// method. signalMtx locks the blockChans array.
	signalMtx  sync.RWMutex
	blockChans map[chan *asset.BlockUpdate]struct{}
	// The block cache stores just enough info about the blocks to prevent future
	// calls to Block.
	blockCache *blockCache
	// A logger will be provided by the DEX. All logging should use the provided
	// logger.
	log dex.Logger
	// contractAddr is the address of the swap contract used for swaps.
	//
	// TODO: Allow supporting multiple addresses/contracts. Needed in the
	// case of updating where two contracts may be valid for some time,
	// possibly disallowing initialization for the deprecated one only.
	contractAddr common.Address
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)

// unconnectedETH returns a Backend without a node. The node should be set
// before use.
func unconnectedETH(logger dex.Logger, cfg *config) *Backend {
	ctx, cancel := context.WithCancel(context.Background())
	// TODO: At some point multiple contracts will need to be used, at
	// least for transitory periods when updating the contract, and
	// possibly a random contract setup, and so this section will need to
	// change to support multiple contracts.
	var contractAddr common.Address
	switch cfg.network {
	case dex.Simnet:
		contractAddr = common.HexToAddress(simnetContractAddr)
	case dex.Testnet:
		contractAddr = common.HexToAddress(testnetContractAddr)
	case dex.Mainnet:
		contractAddr = common.HexToAddress(mainnetContractAddr)
	}
	return &Backend{
		rpcCtx:       ctx,
		cancelRPCs:   cancel,
		cfg:          cfg,
		blockCache:   newBlockCache(logger),
		log:          logger,
		blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
		contractAddr: contractAddr,
	}
}

// NewBackend is the exported constructor by which the DEX will import the
// Backend.
func NewBackend(ipc string, logger dex.Logger, network dex.Network) (*Backend, error) {
	cfg, err := load(ipc, network)
	if err != nil {
		return nil, err
	}
	return unconnectedETH(logger, cfg), nil
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

	// Prime the cache with the best block.
	bestHash, err := eth.node.bestBlockHash(ctx)
	if err != nil {
		eth.shutdown()
		return nil, fmt.Errorf("error getting best block hash from geth: %w", err)
	}
	block, err := eth.node.block(ctx, bestHash)
	if err != nil {
		eth.shutdown()
		return nil, fmt.Errorf("error getting best block from geth: %w", err)
	}
	_, err = eth.blockCache.add(block)
	if err != nil {
		eth.log.Errorf("error adding new best block to cache: %v", err)
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
	txHash, err := decodeCoinID(coinID)
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

// InitTxSize is not size for eth. In ethereum the size of a non-standard
// transaction does not say anything about the processing power the ethereum
// virtual machine will use in order to process it, and therefor gas needed
// cannot be ascertained from it. Multiplying the required gas by the gas price
// will give us the actual fee needed, so returning gas here.
func (eth *Backend) InitTxSize() uint32 {
	return uint32(dexeth.InitGas(1, eth.ver))
}

// InitTxSizeBase is used in fee.go in a fee calculation. Currently we are
// unable to batch eth contract calls like UTXO coins so all contracts will
// need to be per transaction. Setting this to zero produces the expected
// result in fee calculations.
func (eth *Backend) InitTxSizeBase() uint32 {
	return 0
}

// FeeRate returns the current optimal fee rate in gwei / gas.
func (eth *Backend) FeeRate(ctx context.Context) (uint64, error) {
	bigGP, err := eth.node.suggestGasPrice(ctx)
	if err != nil {
		return 0, err
	}
	return dexeth.ToGwei(bigGP)
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the eth package. Part of the asset.Backend interface.
func (eth *Backend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	c := make(chan *asset.BlockUpdate, size)
	eth.signalMtx.Lock()
	defer eth.signalMtx.Unlock()
	eth.blockChans[c] = struct{}{}
	return c
}

// Contract is part of the asset.Backend interface.
func (eth *Backend) Contract(coinID, _ []byte) (*asset.Contract, error) {
	sc, err := eth.newSwapCoin(coinID, sctInit)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}
	// Confirmations performs some extra swap status checks if the the tx
	// is mined.
	_, err = sc.Confirmations(eth.rpcCtx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}
	return &asset.Contract{
		Coin:         sc,
		SwapAddress:  sc.counterParty.String(),
		RedeemScript: sc.secretHash[:],
		LockTime:     encode.UnixTimeMilli(sc.locktime),
	}, nil
}

// ValidateSecret checks that the secret satisfies the secret hash.
func (eth *Backend) ValidateSecret(secret, secretHash []byte) bool {
	sh := sha256.Sum256(secret)
	return bytes.Equal(sh[:], secretHash)
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
func (eth *Backend) Redemption(redeemCoinID, contractCoinID []byte) (asset.Coin, error) {
	cnr, err := eth.newSwapCoin(redeemCoinID, sctRedeem)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}
	// Ensure that the redeem is for the same coin hash and contract as the
	// contract coin.
	if err = cnr.validateRedeem(contractCoinID); err != nil {
		return nil, fmt.Errorf("unable to validate redeem: %v", err)
	}
	// Confirmations performs some extra swap status checks if the the tx
	// is mined.
	_, err = cnr.Confirmations(eth.rpcCtx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}
	return cnr, nil
}

// ValidateCoinID attempts to decode the coinID.
func (eth *Backend) ValidateCoinID(coinID []byte) (string, error) {
	txHash, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return txHash.String(), nil
}

// ValidateContract ensures that the secret hash is the correct length.
func (eth *Backend) ValidateContract(secretHash []byte) error {
	if len(secretHash) != dexeth.SecretHashSize {
		return fmt.Errorf("secret hash is wrong size: want %d but got %d", dexeth.SecretHashSize, len(secretHash))
	}
	return nil
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
	return dexeth.ToGwei(bigBal)
}

// run processes the queue and monitors the application context. The supplied
// running channel will be closed upon setting the context which is used by the
// rpcclient.
func (eth *Backend) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	// Shut down the RPC client on ctx.Done().
	go func() {
		<-ctx.Done()
		eth.cancelRPCs()
		eth.shutdown()
		wg.Done()
	}()

	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()
	addBlock := func(block *types.Block, reorg bool) {
		_, err := eth.blockCache.add(block)
		if err != nil {
			eth.log.Errorf("error adding new best block to cache: %v", err)
		}
		eth.signalMtx.Lock()
		eth.log.Tracef("Notifying %d eth asset consumers of new block at height %d",
			len(eth.blockChans), block.NumberU64())
		for c := range eth.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err:   nil,
				Reorg: reorg,
			}:
			default:
				// Commented to try sends on future blocks.
				// close(c)
				// delete(eth.blockChans, c)
				//
				// TODO: Allow the receiver (e.g. Swapper.Run) to inform done
				// status so the channels can be retired cleanly rather than
				// trying them forever.
			}
		}
		eth.signalMtx.Unlock()
	}

	sendErr := func(err error) {
		eth.log.Error(err)
		eth.signalMtx.Lock()
		for c := range eth.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err: err,
			}:
			default:
				eth.log.Errorf("failed to send sending block update on blocking channel")
				// close(c)
				// delete(eth.blockChans, c)
			}
		}
		eth.signalMtx.Unlock()
	}

	sendErrFmt := func(s string, a ...interface{}) {
		sendErr(fmt.Errorf(s, a...))
	}

out:
	for {
		select {
		case <-blockPoll.C:
			tip := eth.blockCache.tip()
			bestHash, err := eth.node.bestBlockHash(ctx)
			if err != nil {
				sendErr(asset.NewConnectionError("error retrieving best block: %w", err))
				continue
			}
			if bestHash == tip.hash {
				continue
			}

			block, err := eth.node.block(ctx, bestHash)
			if err != nil {
				sendErrFmt("error retrieving block %x: %w", bestHash, err)
				continue
			}
			// If this doesn't build on the best known block, look for a reorg.
			prevHash := block.ParentHash()
			// If it builds on the best block or the cache is empty, it's good to add.
			if prevHash == tip.hash || tip.height == 0 {
				eth.log.Debugf("New block %x (%d)", bestHash, block.NumberU64())
				addBlock(block, false)
				continue
			}

			// It is either a reorg, or the previous block is not the cached
			// best block. Crawl blocks backwards until finding a mainchain
			// block, flagging blocks from the cache as orphans along the way.
			//
			// TODO: Fix this. The exact ethereum behavior here is yet unknown.
			iHash := tip.hash
			reorgHeight := uint64(0)
			for {
				if iHash == zeroHash {
					break
				}
				iBlock, err := eth.node.block(ctx, iHash)
				if err != nil {
					sendErrFmt("error retrieving block %s: %w", iHash, err)
					break
				}
				// TODO: It is yet unknown how to tell if we are
				// on the main chain. Blocks do not contain confirmation
				// information.
				// if iBlock.Confirmations > -1 {
				// 	// This is a mainchain block, nothing to do.
				// 	break
				// }
				if iBlock.NumberU64() == 0 {
					break
				}
				reorgHeight = iBlock.NumberU64()
				iHash = iBlock.ParentHash()
			}

			var reorg bool
			if reorgHeight > 0 {
				reorg = true
				eth.log.Infof("Tip change from %s (%d) to %s (%d) detected (reorg or just fast blocks).",
					tip.hash, tip.height, bestHash, block.NumberU64())
				eth.blockCache.reorg(reorgHeight)
			}

			// Now add the new block.
			addBlock(block, reorg)

		case <-ctx.Done():
			break out
		}
	}
	// Wait for the RPC client to shut down.
	wg.Wait()
}

// decodeCoinID decodes the coin ID into a common.Hash. For eth, there are no
// funding coin IDs, just an account address. Care should be taken not to use
// decodeCoinID or (Driver).DecodeCoinID for account addresses.
func decodeCoinID(coinID []byte) (common.Hash, error) {
	if len(coinID) != common.HashLength {
		return common.Hash{}, fmt.Errorf("wrong coin ID length. wanted %d, got %d", common.HashLength, len(coinID))
	}
	var h common.Hash
	h.SetBytes(coinID)
	return h, nil
}
