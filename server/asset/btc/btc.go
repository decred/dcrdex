// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/decred/dcrdex/server/asset"
)

var (
	zeroHash chainhash.Hash
	// The blockPollInterval is the delay between calls to GetBestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second * 5
)

const (
	btcToSatoshi             = 1e8
	immatureTransactionError = asset.Error("immature output")
)

// btcNode represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient.Client, and all methods are matches for Client
// methods. For testing, it can be satisfied by a stub.
type btcNode interface {
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)
	GetBlockVerbose(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash() (*chainhash.Hash, error)
}

// BTCBackend is an asset backend for Bitcoin. It has methods for fetching UTXO
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. BTCBackend implements asset.DEXAsset, so provides
// exported methods for DEX-related blockchain info.
type BTCBackend struct {
	// An application context provided as part of the constructor. The BTCBackend
	// will perform some cleanup when the context is cancelled.
	ctx context.Context
	// If an rpcclient.Client is used for the node, keeping a reference at client
	// will result the (Client).Shutdown() being called on context cancellation.
	client *rpcclient.Client
	// node is used throughout for RPC calls, and in typical use will be the same
	// as client. For testing, it can be set to a stub.
	node btcNode
	// The block cache stores just enough info about the blocks to shortcut future
	// calls to GetBlockVerbose.
	blockCache *blockCache
	// The backend provides block notification channels through it BlockChannel
	// method. signalMtx locks the blockChans array.
	signalMtx   sync.RWMutex
	blockChans  []chan uint32
	chainParams *chaincfg.Params
	// A logger will be provided by the dex for this backend. All logging should
	// use the provided logger.
	log asset.Logger
}

// Check that BTCBackend satisfies the DEXAsset interface.
var _ asset.DEXAsset = (*BTCBackend)(nil)

// NewBackend is the exported constructor by which the DEX will import the
// backend. The provided context.Context should be cancelled when the DEX
// application exits. The configPath can be an empty string, in which case the
// standard system location of the bitcoind config file is assumed.
func NewBackend(ctx context.Context, configPath string, logger asset.Logger, network asset.Network) (asset.DEXAsset, error) {
	var params *chaincfg.Params
	switch network {
	case asset.Mainnet:
		params = &chaincfg.MainNetParams
	case asset.Testnet:
		params = &chaincfg.TestNet3Params
	case asset.Regtest:
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	if configPath == "" {
		configPath = SystemConfigPath("bitcoin")
	}

	return NewBTCClone(ctx, configPath, logger, network, params, btcPorts)
}

// NewBTCClone creates a BTC backend for a set of network parameters and default
// network ports. A BTC clone can use this method, possibly in conjunction with
// ReadCloneParams, to create a BTCBackend for other assets with minimal coding.
// See ReadCloneParams and CompatibilityCheck for more info.
func NewBTCClone(ctx context.Context, configPath string, logger asset.Logger,
	network asset.Network, params *chaincfg.Params, ports NetPorts) (*BTCBackend, error) {

	// Read the configuration parameters
	cfg, err := LoadConfig(configPath, network, ports)
	if err != nil {
		return nil, err
	}

	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         cfg.RPCBind,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating BTC RPC client: %v", err)
	}

	btc := newBTC(ctx, params, logger, client)
	// Setting the client field will enable shutdown
	btc.client = client

	// Prime the cache
	bestHash, err := btc.client.GetBestBlockHash()
	if err != nil {
		return nil, fmt.Errorf("error getting best block from rpc: %v", err)
	}
	if bestHash != nil {
		_, err := btc.getBtcBlock(bestHash)
		if err != nil {
			return nil, fmt.Errorf("error priming the cache: %v", err)
		}
	}

	return btc, nil
}

// This entire module may be usable by a BTC clone if certain conditions are
// met. Many BTC clones have btcd forks with their own go config files. The
// BTCBackend only uses a handful of configuration settings, so if all of those
// settings are present in the clone parameter files, the ReadCloneParams can
// likely translate them into btcd-flavor.
//
// Another option for clones that don't have a compatible golang config file is
// to write the file on the fly. ReadCloneParams will take any interface and
// look for the fields with appropriate names. This list is an attempt to
// capture those, but may not be comprehensive. Run live tests and compatibility
// tests from testing.go as well.
//
// PubKeyHashAddrID: Net ID byte for a pubkey-hash address
// ScriptHashAddrID: Net ID byte for a script-hash address
// CoinbaseMaturity: The number of confirmations before a transaction spending
//                   a coinbase input can be spent.
// WitnessPubKeyHashAddrID: Net ID byte for a witness-pubkey-hash address
// WitnessScriptHashAddrID: Net ID byte for a witness-script-hash address
// Bech32HRPSegwit: Human-readable part for Bech32 encoded segwit addresses,
//                  as defined in BIP 173.
func ReadCloneParams(cloneParams interface{}) (*chaincfg.Params, error) {
	p := new(chaincfg.Params)
	cloneJson, err := json.Marshal(cloneParams)
	if err != nil {
		return nil, fmt.Errorf("error marshaling network params: %v", err)
	}
	err = json.Unmarshal(cloneJson, &p)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling nework params: %v", err)
	}
	return p, nil
}

// UTXO is part of the asset.UTXO interface. See the unexported BTCBackend.utxo
// method for the full implementation.
func (btc *BTCBackend) UTXO(txid string, vout uint32, redeemScript []byte) (asset.UTXO, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	return btc.utxo(txHash, vout, redeemScript)
}

// Transaction is part of the asset.DEXTx interface. See the unexported
// BTCBackend.transaction method for the full implementation.
func (btc *BTCBackend) Transaction(txid string) (asset.DEXTx, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	return btc.transaction(txHash)
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the btc package. Part of the asset.DEXAsset interface.
func (btc *BTCBackend) BlockChannel(size int) chan uint32 {
	c := make(chan uint32, size)
	btc.signalMtx.Lock()
	defer btc.signalMtx.Unlock()
	btc.blockChans = append(btc.blockChans, c)
	return c
}

// InitTxSize is an asset.DEXAsset method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (btc *BTCBackend) InitTxSize() uint32 {
	return initTxSize
}

// Create a *BTCBackend and start the block monitor loop.
func newBTC(ctx context.Context, chainParams *chaincfg.Params, logger asset.Logger, node btcNode) *BTCBackend {
	btc := &BTCBackend{
		ctx:         ctx,
		blockCache:  newBlockCache(),
		blockChans:  make([]chan uint32, 0),
		chainParams: chainParams,
		log:         logger,
		node:        node,
	}
	go btc.loop()
	return btc
}

// Get the UTXO data and perform some checks for script support.
func (btc *BTCBackend) utxo(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := btc.getTxOutInfo(txHash, vout)
	if err != nil {
		return nil, err
	}
	scriptType := parseScriptType(pkScript, redeemScript)
	if scriptType == scriptUnsupported {
		return nil, asset.UnsupportedScriptError
	}

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.isP2SH() {
		if scriptType.isSegwit() {
			scriptHash := extractWitnessScriptHash(pkScript)
			shash := sha256.Sum256(redeemScript)
			if !bytes.Equal(shash[:], scriptHash) {
				return nil, fmt.Errorf("script hash check failed for utxo %s,%d", txHash, vout)
			}
		} else {
			scriptHash := extractScriptHash(pkScript)
			if !bytes.Equal(btcutil.Hash160(redeemScript), scriptHash) {
				return nil, fmt.Errorf("script hash check failed for utxo %s,%d", txHash, vout)
			}
		}
	}

	// Get information about the signatures and pubkeys needed to spend the utxo.
	evalScript := pkScript
	if scriptType.isP2SH() {
		evalScript = redeemScript
	}
	scriptAddrs, err := extractScriptAddrs(evalScript, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing utxo script addresses")
	}

	// Start with standard P2PKH.
	sigScriptSize := P2PKHSigScriptSize
	// If it's a P2SH, the size must be calculated based on other factors.
	if scriptType.isP2SH() {
		// Start with the signatures.
		sigScriptSize = (derSigLength + 1) * scriptAddrs.nRequired // 73 max for sig, 1 for push code
		// If there are pubkey-hash addresses, they'll need pubkeys.
		if scriptAddrs.numPKH > 0 {
			sigScriptSize += scriptAddrs.nRequired * (pubkeyLength + 1)
		}
		// Then add the length of the script and another push opcode byte.
		sigScriptSize += len(redeemScript) + 1
	}

	// Get block information.
	var blockHeight uint32
	var blockHash chainhash.Hash
	var lastLookup *chainhash.Hash
	if txOut.Confirmations > 0 {
		blk, err := btc.getBlockInfo(verboseTx.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("error retreiving block for hash %s", verboseTx.BlockHash)
		}
		blockHeight = uint32(blk.height)
		blockHash = blk.hash
	} else {
		// Set the lastLookup to the current tip.
		tipHash := btc.blockCache.tipHash()
		if tipHash != zeroHash {
			lastLookup = &tipHash
		}
	}

	// Coinbase transactions must mature before spending.
	var maturity int64
	if txOut.Coinbase {
		maturity = int64(btc.chainParams.CoinbaseMaturity)
	}
	if txOut.Confirmations < maturity {
		return nil, immatureTransactionError
	}

	return &UTXO{
		btc:          btc,
		height:       blockHeight,
		blockHash:    blockHash,
		txHash:       *txHash,
		vout:         vout,
		maturity:     int32(maturity),
		scriptType:   scriptType,
		pkScript:     pkScript,
		redeemScript: redeemScript,
		numSigs:      scriptAddrs.nRequired,
		spendSize:    uint32(sigScriptSize) + txInOverhead,
		lastLookup:   lastLookup,
	}, nil
}

// Get the Tx. Transaction info is not cached, so every call will result in a
// GetRawTransactionVerbose RPC call.
func (btc *BTCBackend) transaction(txHash *chainhash.Hash) (*Tx, error) {
	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}

	// If it's not a mempool transaction, get and cache the block data.
	var blockHash *chainhash.Hash
	var lastLookup *chainhash.Hash
	var blockHeight int64
	if verboseTx.BlockHash == "" {
		tipHash := btc.blockCache.tipHash()
		if tipHash != zeroHash {
			lastLookup = &tipHash
		}
	} else {
		blockHash, err = chainhash.NewHashFromStr(verboseTx.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("error decoding block hash %s for tx %s: %v", verboseTx.BlockHash, txHash, err)
		}
		// Make sure the block info is cached.
		blk, err := btc.getBtcBlock(blockHash)
		if err != nil {
			return nil, fmt.Errorf("error caching the block data for transaction %s", txHash)
		}
		blockHeight = int64(blk.height)
	}

	// Parse inputs and outputs, storing only what's needed.
	inputs := make([]txIn, 0, len(verboseTx.Vin))
	for _, input := range verboseTx.Vin {
		if input.Txid == "" {
			inputs = append(inputs, txIn{vout: input.Vout})
			continue
		}
		hash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, fmt.Errorf("error decoding previous tx hash %sfor tx %s: %v", input.Txid, txHash, err)
		}
		inputs = append(inputs, txIn{prevTx: *hash, vout: input.Vout})
	}

	outputs := make([]txOut, 0, len(verboseTx.Vout))
	for vout, output := range verboseTx.Vout {
		pkScript, err := hex.DecodeString(output.ScriptPubKey.Hex)
		if err != nil {
			return nil, fmt.Errorf("error decoding pubkey script from %s for transaction %d:%d: %v",
				output.ScriptPubKey.Hex, txHash, vout, err)
		}
		outputs = append(outputs, txOut{
			value:    uint64(output.Value * btcToSatoshi),
			pkScript: pkScript,
		})
	}
	return newTransaction(btc, txHash, blockHash, lastLookup, blockHeight, inputs, outputs), nil
}

// Get information for an unspent transaction output and it's transaction.
func (btc *BTCBackend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*btcjson.GetTxOutResult, *btcjson.TxRawResult, []byte, error) {
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetTxOut error for output %s:%d: %v", txHash, vout, err)
	}
	if txOut == nil {
		return nil, nil, nil, fmt.Errorf("UTXO - no unspent txout found for %s:%d", txHash, vout)
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode pubkey from '%s' for output %s:%d", txOut.ScriptPubKey.Hex, txHash, vout)
	}
	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}
	return txOut, verboseTx, pkScript, nil
}

// Get the block information, checking the cache first. Same as
// getBtcBlock, but takes a string argument.
func (btc *BTCBackend) getBlockInfo(blockid string) (*cachedBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(blockid)
	if err != nil {
		return nil, fmt.Errorf("unable to decode block hash from %s", blockid)
	}
	return btc.getBtcBlock(blockHash)
}

// Get the block information, checking the cache first.
func (btc *BTCBackend) getBtcBlock(blockHash *chainhash.Hash) (*cachedBlock, error) {
	cachedBlk, found := btc.blockCache.block(blockHash)
	if found {
		return cachedBlk, nil
	}
	blockVerbose, err := btc.node.GetBlockVerbose(blockHash)
	if err != nil {
		return nil, fmt.Errorf("error retrieving block %s: %v", blockHash, err)
	}
	return btc.blockCache.add(blockVerbose)
}

// loop should be run as a goroutine. This loop is responsible for best block
// polling and checking the application context to trigger a clean shutdown.
func (btc *BTCBackend) loop() {
	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()
	addBlock := func(block *btcjson.GetBlockVerboseResult) {
		_, err := btc.blockCache.add(block)
		if err != nil {
			btc.log.Errorf("error adding new best block to cache: %v", err)
			return
		}
		btc.signalMtx.RLock()
		for _, c := range btc.blockChans {
			select {
			case c <- uint32(block.Height):
			default:
				btc.log.Errorf("tried sending block update on blocking channel")
			}
		}
		btc.signalMtx.RUnlock()
	}
out:
	for {
		select {
		case <-blockPoll.C:
			tip := btc.blockCache.tip()
			bestHash, err := btc.node.GetBestBlockHash()
			if err != nil {
				btc.log.Errorf("error retrieving best block: %v", err)
				continue
			}
			if *bestHash == tip.hash {
				continue
			}
			block, err := btc.node.GetBlockVerbose(bestHash)
			if err != nil {
				btc.log.Errorf("error retrieving block %s: %v", bestHash, err)
				continue
			}
			// If this doesn't build on the best known block, look for a reorg.
			prevHash, err := chainhash.NewHashFromStr(block.PreviousHash)
			if err != nil {
				btc.log.Errorf("error parsing previous hash %s: %v", block.PreviousHash, err)
				continue
			}
			// If it builds on the best block or the cache is empty, it's good to add.
			if *prevHash == tip.hash || tip.height == 0 {
				addBlock(block)
				continue
			}
			// It must be a reorg. Crawl blocks backwards until finding a mainchain
			// block, flagging blocks from the cache as orphans along the way.
			iHash := &tip.hash
			for {
				if *iHash == zeroHash {
					break
				}
				iBlock, err := btc.node.GetBlockVerbose(iHash)
				if err != nil {
					btc.log.Errorf("error retreiving block %s: %v", iHash, err)
					break
				}
				if iBlock.Confirmations > -1 {
					// This is a mainchain block, nothing to do.
					break
				}
				// It's an orphan. reorg it.
				btc.blockCache.reorg(iBlock.Height)
				if iBlock.Height == 0 {
					break
				}
				iHash, err = chainhash.NewHashFromStr(iBlock.PreviousHash)
				if err != nil {
					btc.log.Errorf("error decoding previous hash %s: %v", iBlock.PreviousHash, err)
					break
				}
			}
			// Now add the new block.
			addBlock(block)
		case <-btc.ctx.Done():
			btc.shutdown()
			break out
		}
	}
}

// Shutdown down the rpcclient.Client.
func (btc *BTCBackend) shutdown() {
	if btc.client != nil {
		btc.client.Shutdown()
		btc.client.WaitForShutdown()
	}
}
