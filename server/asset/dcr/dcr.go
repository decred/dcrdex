// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/rpcclient/v4"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdex/server/asset"
)

var zeroHash chainhash.Hash

type Error = asset.Error

const (
	dcrToAtoms               = 1e8
	immatureTransactionError = Error("immature output")
)

// dcrNode represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient.Client, and all methods are matches for Client
// methods. For testing, it can be satisfied by a stub.
type dcrNode interface {
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
}

// dcrBackend is an asset backend for Decred. It has methods for fetching UTXO
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. dcrBackend implements asset.DEXAsset, so provides
// exported methods for DEX-related blockchain info.
type dcrBackend struct {
	// An application context provided as part of the constructor. The dcrBackend
	// will perform some cleanup when the context is cancelled.
	ctx context.Context
	// If an rpcclient.Client is used for the node, keeping a reference at client
	// will result in (Client).Shutdown() being called on context cancellation.
	client *rpcclient.Client
	// node is used throughout for RPC calls, and in typical use will be the same
	// as client. For testing, it can be set to a stub.
	node dcrNode
	// The backend provides block notification channels through it BlockChannel
	// method. signalMtx locks the blockChans array.
	signalMtx  sync.RWMutex
	blockChans []chan uint32
	// The block cache stores just enough info about the blocks to prevent future
	// calls to GetBlockVerbose.
	blockCache *blockCache
	// dcrd block and reorganization are synchronized through a general purpose
	// queue.
	anyQ chan interface{}
	// A logger will be provided by the DEX. All logging should use the provided
	// logger.
	log asset.Logger
}

// Check that dcrBackend satisfies the DEXAsset interface.
var _ asset.DEXAsset = (*dcrBackend)(nil)

// NewBackend is the exported constructor by which the DEX will import the
// dcrBackend. The provided context.Context should be cancelled when the DEX
// application exits. If configPath is an empty string, the backend will
// attempt to read the settings directly from the dcrd config file in its
// default system location.
func NewBackend(ctx context.Context, configPath string, logger asset.Logger, network asset.Network) (*dcrBackend, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// package variable.
	cfg, err := loadConfig(configPath, network)
	if err != nil {
		return nil, err
	}
	dcr := unconnectedDCR(ctx, logger)
	notifications := &rpcclient.NotificationHandlers{
		OnBlockConnected: dcr.onBlockConnected,
	}
	// When the exported constructor is used, the node will be an
	// rpcclient.Client.
	dcr.client, err = connectNodeRPC(cfg.RPCListen, cfg.RPCUser, cfg.RPCPass,
		cfg.RPCCert, notifications)
	if err != nil {
		return nil, err
	}
	err = dcr.client.NotifyBlocks()
	if err != nil {
		return nil, fmt.Errorf("error registering for block notifications")
	}
	dcr.node = dcr.client
	// Prime the cache with the best block.
	bestHash, _, err := dcr.client.GetBestBlock()
	if err != nil {
		return nil, fmt.Errorf("error getting best block from dcrd: %v", err)
	}
	if bestHash != nil {
		_, err := dcr.getDcrBlock(bestHash)
		if err != nil {
			return nil, fmt.Errorf("error priming the cache: %v", err)
		}
	}
	return dcr, nil
}

// InitTxSize is an asset.DEXAsset method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (btc *dcrBackend) InitTxSize() uint32 {
	return initTxSize
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the dcr package. Part of the asset.DEXAsset interface.
func (dcr *dcrBackend) BlockChannel(size int) chan uint32 {
	c := make(chan uint32, size)
	dcr.signalMtx.Lock()
	defer dcr.signalMtx.Unlock()
	dcr.blockChans = append(dcr.blockChans, c)
	return c
}

// UTXO is part of the asset.UTXO interface, so returns the asset.UTXO type.
// Only spendable UTXOs with known types of pubkey script will be successfully
// retrieved. A spendable UTXO is one that can be spent in the next block. Every
// regular-tree output from a non-coinbase transaction is spendable immediately.
// Coinbase and stake tree outputs are only spendable after CoinbaseMaturity
// confirmations. Pubkey scripts can be P2PKH or P2SH in either regular- or
// stake-tree flavor. P2PKH supports two alternative signatures, Schnorr and
// Edwards. Multi-sig P2SH redeem scripts are supported as well.
func (dcr *dcrBackend) UTXO(txid string, vout uint32, redeemScript []byte) (asset.UTXO, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	return dcr.utxo(txHash, vout, redeemScript)
}

// Transaction is part of the asset.DEXTx interface. The returned DEXTx has
// methods for checking spent outputs and validating swap contracts.
func (dcr *dcrBackend) Transaction(txid string) (asset.DEXTx, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	return dcr.transaction(txHash)
}

// Get the Tx. Transaction info is not cached, so every call will result in a
// GetRawTransactionVerbose RPC call.
func (dcr *dcrBackend) transaction(txHash *chainhash.Hash) (*Tx, error) {
	verboseTx, err := dcr.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}

	// Figure out if it's a stake transaction
	msgTx, err := msgTxFromHex(verboseTx.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MsgTx from hex for transaction %s: %v", txHash, err)
	}
	isStake := stake.DetermineTxType(msgTx) != stake.TxTypeRegular

	// If it's not a mempool transaction, get and cache the block data.
	var blockHash *chainhash.Hash
	var lastLookup *chainhash.Hash
	if verboseTx.BlockHash == "" {
		tipHash := dcr.blockCache.tipHash()
		if tipHash != zeroHash {
			lastLookup = &tipHash
		}
	} else {
		blockHash, err = chainhash.NewHashFromStr(verboseTx.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("error decoding block hash %s for tx %s: %v", verboseTx.BlockHash, txHash, err)
		}
		// Make sure the block info is cached.
		_, err := dcr.getDcrBlock(blockHash)
		if err != nil {
			return nil, fmt.Errorf("error caching the block data for transaction %s", txHash)
		}
	}

	var sumIn, sumOut uint64
	// Parse inputs and outputs, grabbing only what's needed.
	inputs := make([]txIn, 0, len(verboseTx.Vin))
	for _, input := range verboseTx.Vin {
		if input.Txid == "" {
			inputs = append(inputs, txIn{vout: input.Vout})
			continue
		}
		sumIn += uint64(input.AmountIn * dcrToAtoms)
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
		sumOut += uint64(output.Value * dcrToAtoms)
		outputs = append(outputs, txOut{
			value:    uint64(output.Value * dcrToAtoms),
			pkScript: pkScript,
		})
	}
	feeRate := (sumIn - sumOut) / uint64(len(verboseTx.Hex)/2)
	return newTransaction(dcr, txHash, blockHash, lastLookup, verboseTx.BlockHeight, isStake, inputs, outputs, feeRate), nil
}

// Shutdown down the rpcclient.Client.
func (dcr *dcrBackend) shutdown() {
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// unconnectedDCR returns a dcrBackend without a node. The node should be set
// before use.
func unconnectedDCR(ctx context.Context, logger asset.Logger) *dcrBackend {
	dcr := &dcrBackend{
		ctx:        ctx,
		blockChans: make([]chan uint32, 0),
		blockCache: newBlockCache(logger),
		anyQ:       make(chan interface{}, 128), // way bigger than needed.
		log:        logger,
	}
	go dcr.superQueue()
	return dcr
}

// superQueue should be run as a goroutine. The dcrd-registered handlers should
// perform any necessary type conversion and then deposit the payload into the
// anyQ channel. superQueue processes the queue and monitors the application
// context.
func (dcr *dcrBackend) superQueue() {
out:
	for {
		select {
		case rawMsg := <-dcr.anyQ:
			switch msg := rawMsg.(type) {
			case *chainhash.Hash:
				// This is a new block notification.
				blockHash := msg
				dcr.log.Debugf("superQueue: Processing new block %s", blockHash)
				blockVerbose, err := dcr.node.GetBlockVerbose(blockHash, false)
				if err != nil {
					dcr.log.Errorf("onBlockConnected error retrieving block %s: %v", blockHash, err)
					return
				}
				// Check if this forces a reorg.
				currentTip := int64(dcr.blockCache.tipHeight())
				if blockVerbose.Height <= currentTip {
					dcr.blockCache.reorg(blockVerbose)
				}
				block, err := dcr.blockCache.add(blockVerbose)
				if err != nil {
					dcr.log.Errorf("error adding block to cache")
				}
				dcr.signalMtx.RLock()
				for _, c := range dcr.blockChans {
					select {
					case c <- uint32(block.height):
					default:
						dcr.log.Errorf("tried sending block update on blocking channel")
					}
				}
				dcr.signalMtx.RUnlock()
			default:
				dcr.log.Warn("unknown message type in superQueue: %T", rawMsg)
			}
		case <-dcr.ctx.Done():
			dcr.shutdown()
			break out
		}
	}
}

// A callback to be registered with dcrd. It is critical that no RPC calls are
// made from this method. Doing so will likely result in a deadlock, as per
// https://github.com/decred/dcrd/blob/952bd7bba34c8aeab86f63f9c9f69fc74ff1a7e1/rpcclient/notify.go#L78
func (dcr *dcrBackend) onBlockConnected(serializedHeader []byte, _ [][]byte) {
	blockHeader := new(wire.BlockHeader)
	err := blockHeader.FromBytes(serializedHeader)
	if err != nil {
		dcr.log.Errorf("error decoding serialized header: %v", err)
		return
	}
	h := blockHeader.BlockHash()
	dcr.anyQ <- &h
}

// Get the UTXO, populating the block data along the way.
func (dcr *dcrBackend) utxo(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := dcr.getTxOutInfo(txHash, vout)
	if err != nil {
		return nil, err
	}
	scriptType := parseScriptType(currentScriptVersion, pkScript, redeemScript)
	if scriptType == scriptUnsupported {
		return nil, asset.UnsupportedScriptError
	}

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.isP2SH() {
		scriptHash, err := extractScriptHashByType(scriptType, pkScript)
		if err != nil {
			return nil, fmt.Errorf("utxo error: %v", err)
		}
		if !bytes.Equal(dcrutil.Hash160(redeemScript), scriptHash) {
			return nil, fmt.Errorf("script hash check failed for utxo %s,%d", txHash, vout)
		}
	}

	// Get information about the signatures and pubkeys needed to spend the utxo.
	evalScript := pkScript
	if scriptType.isP2SH() {
		evalScript = redeemScript
	}
	scriptAddrs, err := extractScriptAddrs(evalScript)
	if err != nil {
		return nil, fmt.Errorf("error parsing utxo script addresses")
	}

	// Get the size of the signature script.
	sigScriptSize := P2PKHSigScriptSize
	// If it's a P2SH, the size must be calculated based on other factors.
	if scriptType.isP2SH() {
		// Start with the signatures.
		sigScriptSize = 74 * scriptAddrs.nRequired // 73 max for sig, 1 for push code
		// If there are pubkey-hash addresses, they'll need pubkeys.
		if scriptAddrs.numPKH > 0 {
			sigScriptSize += scriptAddrs.nRequired * (pubkeyLength + 1)
		}
		// Then add the length of the script and another push opcode byte.
		sigScriptSize += len(redeemScript) + 1
	}

	blockHeight := uint32(verboseTx.BlockHeight)
	var blockHash chainhash.Hash
	var lastLookup *chainhash.Hash
	// UTXO is assumed to be valid while in mempool, so skip the validity check.
	if txOut.Confirmations > 0 {
		if blockHeight == 0 {
			return nil, fmt.Errorf("no raw transaction result found for tx output with "+
				"non-zero confirmation count (%s has %d confirmations)", txHash, txOut.Confirmations)
		}
		blk, err := dcr.getBlockInfo(verboseTx.BlockHash)
		if err != nil {
			return nil, err
		}
		blockHeight = uint32(blk.height)
		blockHash = blk.hash
	} else {
		// Set the lastLookup to the current tip.
		tipHash := dcr.blockCache.tipHash()
		if tipHash != zeroHash {
			lastLookup = &tipHash
		}
	}

	// Coinbase, vote, and revocation transactions all must mature before
	// spending.
	var maturity int64
	if scriptType.isStake() || txOut.Coinbase {
		maturity = int64(chainParams.CoinbaseMaturity)
	}
	if txOut.Confirmations < maturity {
		return nil, immatureTransactionError
	}

	return &UTXO{
		dcr:          dcr,
		height:       blockHeight,
		blockHash:    blockHash,
		txHash:       *txHash,
		vout:         vout,
		maturity:     int32(maturity),
		scriptType:   scriptType,
		pkScript:     pkScript,
		redeemScript: redeemScript,
		numSigs:      scriptAddrs.nRequired,
		// The total size associated with the wire.TxIn.
		spendSize:  uint32(sigScriptSize) + txInOverhead,
		lastLookup: lastLookup,
	}, nil
}

// MsgTxFromHex creates a wire.MsgTx by deserializing the hex transaction.
func msgTxFromHex(txhex string) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx()
	if err := msgTx.Deserialize(hex.NewDecoder(strings.NewReader(txhex))); err != nil {
		return nil, err
	}
	return msgTx, nil
}

// Get information for an unspent transaction output.
func (dcr *dcrBackend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*chainjson.GetTxOutResult, *chainjson.TxRawResult, []byte, error) {
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetTxOut error for output %s:%d: %v", txHash, vout, err)
	}
	if txOut == nil {
		return nil, nil, nil, fmt.Errorf("UTXO - no unspent txout found for %s:%d", txHash, vout)
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode pubkey script from '%s' for output %s:%d", txOut.ScriptPubKey.Hex, txHash, vout)
	}
	verboseTx, err := dcr.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}
	return txOut, verboseTx, pkScript, nil
}

// Get the block information, checking the cache first. Same as
// getDcrBlock, but takes a string argument.
func (dcr *dcrBackend) getBlockInfo(blockid string) (*dcrBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(blockid)
	if err != nil {
		return nil, fmt.Errorf("unable to decode block hash from %s", blockid)
	}
	return dcr.getDcrBlock(blockHash)
}

// Get the block information, checking the cache first.
func (dcr *dcrBackend) getDcrBlock(blockHash *chainhash.Hash) (*dcrBlock, error) {
	cachedBlock, found := dcr.blockCache.block(blockHash)
	if found {
		return cachedBlock, nil
	}
	blockVerbose, err := dcr.node.GetBlockVerbose(blockHash, false)
	if err != nil {
		return nil, fmt.Errorf("error retrieving block %s: %v", blockHash, err)
	}
	return dcr.blockCache.add(blockVerbose)
}

// Get the mainchain block at the given height, checking the cache first.
func (dcr *dcrBackend) getMainchainDcrBlock(height uint32) (*dcrBlock, error) {
	cachedBlock, found := dcr.blockCache.atHeight(height)
	if found {
		return cachedBlock, nil
	}
	hash, err := dcr.node.GetBlockHash(int64(height))
	if err != nil {
		// Likely not mined yet. Not an error.
		return nil, nil
	}
	return dcr.getDcrBlock(hash)
}

// connectNodeRPC attempts to create a new websocket connection to a dcrd node
// with the given credentials and notification handlers.
func connectNodeRPC(host, user, pass, cert string,
	notifications *rpcclient.NotificationHandlers) (*rpcclient.Client, error) {

	dcrdCerts, err := ioutil.ReadFile(cert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %v", err)
	}

	config := &rpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
	}

	dcrdClient, err := rpcclient.New(config, notifications)
	if err != nil {
		return nil, fmt.Errorf("Failed to start dcrd RPC client: %v", err)
	}

	return dcrdClient, nil
}
