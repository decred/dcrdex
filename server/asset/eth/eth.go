// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func init() {
	asset.Register(assetName, &Driver{})
}

const (
	assetName  = "eth"
	coinIDSize = 52
	// The blockPollInterval is the delay between calls to bestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second
)

var (
	zeroHash          = common.Hash{}
	notImplementedErr = errors.New("not implemented")
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Ethereum.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	return coinIDToString(coinID)
}

// ethFetcher represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient. For testing, it can be satisfied by a stub.
type ethFetcher interface {
	shutdown()
	connect(ctx context.Context, IPC string) error
	bestBlockHash(ctx context.Context) (common.Hash, error)
	block(ctx context.Context, hash common.Hash) (*types.Block, error)
}

// Backend is an asset backend for Ethereum. It has methods for fetching output
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. Backend implements asset.Backend, so provides
// exported methods for DEX-related blockchain info.
type Backend struct {
	cfg  *config
	node ethFetcher
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
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)

// unconnectedETH returns a Backend without a node. The node should be set
// before use.
func unconnectedETH(logger dex.Logger, cfg *config) *Backend {
	return &Backend{
		cfg:        cfg,
		blockCache: newBlockCache(logger),
		log:        logger,
		blockChans: make(map[chan *asset.BlockUpdate]struct{}),
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

// Connect connects to the node RPC server. A dex.Connector.
func (eth *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	c := rpcclient{}
	if err := c.connect(ctx, eth.cfg.IPC); err != nil {
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
	return nil, notImplementedErr
}

// InitTxSize is an asset.Backend method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (eth *Backend) InitTxSize() uint32 {
	return 0
}

// InitTxSizeBase is InitTxSize not including an input.
func (eth *Backend) InitTxSizeBase() uint32 {
	return 0
}

// FeeRate returns the current optimal fee rate in atoms / byte.
func (eth *Backend) FeeRate() (uint64, error) {
	return 0, notImplementedErr
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
func (eth *Backend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) {
	return nil, notImplementedErr
}

// ValidateSecret checks that the secret satisfies the contract.
func (eth *Backend) ValidateSecret(secret, contract []byte) bool {
	return false
}

// Synced is true if the blockchain is ready for action.
func (eth *Backend) Synced() (bool, error) {
	return false, notImplementedErr
}

// Redemption is an input that redeems a swap contract.
func (eth *Backend) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
	return nil, notImplementedErr
}

// FundingCoin is an unspent output.
func (eth *Backend) FundingCoin(ctx context.Context, coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	return nil, notImplementedErr
}

// ValidateCoinID attempts to decode the coinID.
func (eth *Backend) ValidateCoinID(coinID []byte) (string, error) {
	return coinIDToString(coinID)
}

// ValidateContract ensures that the swap contract is constructed properly, and
// contains valid sender and receiver addresses.
func (eth *Backend) ValidateContract(contract []byte) error {
	return notImplementedErr
}

// CheckAddress checks that the given address is parseable.
func (eth *Backend) CheckAddress(addr string) bool {
	return common.IsHexAddress(addr)
}

// VerifyUnspentCoin attempts to verify a coin ID is unspent.
func (eth *Backend) VerifyUnspentCoin(ctx context.Context, coinID []byte) error {
	return notImplementedErr
}

// run processes the queue and monitors the application context.
func (eth *Backend) run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	// Shut down the RPC client on ctx.Done().
	go func() {
		<-ctx.Done()
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

// decodeCoinID decodes the coin ID into a contract address and secret hash.
func decodeCoinID(coinID []byte) (common.Address, []byte, error) {
	if len(coinID) != coinIDSize {
		return common.Address{}, nil, fmt.Errorf("coin ID wrong length. expected %d, got %d",
			coinIDSize, len(coinID))
	}
	secretHash := make([]byte, 32)
	copy(secretHash, coinID[20:])
	return common.BytesToAddress(coinID[:20]), secretHash, nil
}

func coinIDToString(coinID []byte) (string, error) {
	addr, secretHash, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x", addr, secretHash), err
}

// toCoinID converts the address and secret hash to a coin ID.
func toCoinID(addr *common.Address, secretHash []byte) []byte {
	b := make([]byte, coinIDSize)
	copy(b[:], addr[:])
	copy(b[20:], secretHash[:])
	return b
}
