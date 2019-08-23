// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/rpcclient/v4"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdex/server/asset"
)

var zeroHash chainhash.Hash

const (
	dcrToAtoms       = 1e8
	SwapContractSize = 97
	// P2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	P2PKHSigScriptSize = 1 + 73 + 1 + 33
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

// dcrBackend is an asset backend for Decred. It has utilities for fetching UTXO
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. dcrBackend implements asset.DEXAsset, so provides
// exported methods for DEX-related blockchain info.
type dcrBackend struct {
	// An application context provided as part of the DCRConfig. The dcrBackend
	// will perform some cleanup when the context is cancelled.
	ctx context.Context
	// If an rpcclient.Client is used for the node, keeping a reference at client
	// will result the (Client).Shutdown() being called on context cancellation.
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
}

// Check that dcrBackend satisfies the DEXAsset interface.
var _ asset.DEXAsset = (*dcrBackend)(nil)

func NewDCR(cfg *DCRConfig) (*dcrBackend, error) {
	// tidyConfig will set fields if defaults are used and set the chainParams
	// package variable.
	err := tidyConfig(cfg)
	if err != nil {
		return nil, err
	}
	dcr := unconnectedDCR(cfg)
	notifications := &rpcclient.NotificationHandlers{
		OnBlockConnected: dcr.onBlockConnected,
	}
	// When the exported constructor is used, the node will be an
	// rpcclient.Client.
	dcr.client, err = connectNodeRPC(cfg.DcrdServ, cfg.DcrdUser, cfg.DcrdPass,
		cfg.DcrdCert, notifications)
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
func (dcr *dcrBackend) UTXO(txid string, vout uint32) (asset.UTXO, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	return dcr.utxo(txHash, vout)
}

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

	// If it's not a mempool transaction, get and cache the block data.
	var blockHash *chainhash.Hash
	if verboseTx.BlockHash != "" {
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

	// Parse inputs and outputs, grabbing only what's needed.
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
			value:    uint64(output.Value * dcrToAtoms),
			pkScript: pkScript,
		})
	}
	return newTransaction(dcr, txHash, blockHash, verboseTx.BlockHeight, inputs, outputs), nil
}

// Shutdown down the rpcclient.Client.
func (dcr *dcrBackend) shutdown() {
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// unconnectedDCR returns a dcrBAckend without a node. The node should be set
// before use.
func unconnectedDCR(cfg *DCRConfig) *dcrBackend {
	dcr := &dcrBackend{
		ctx:        cfg.Context,
		blockChans: make([]chan uint32, 0),
		blockCache: newBlockCache(),
		anyQ:       make(chan interface{}, 128), // way bigger than needed.
	}
	go dcr.superQueue()
	return dcr
}

// superQueue should be run as a goroutine. The dcrd-registered handlers should
// perform any necessary type conversion and then deposit the payload into the
// anyQ channel.
func (dcr *dcrBackend) superQueue() {
out:
	for {
		select {
		case rawMsg := <-dcr.anyQ:
			switch msg := rawMsg.(type) {
			case *chainhash.Hash:
				// This is a new block notification.
				blockHash := msg
				log.Debugf("superQueue: Processing new block %s", blockHash)
				blockVerbose, err := dcr.node.GetBlockVerbose(blockHash, false)
				if err != nil {
					log.Errorf("onBlockConnected error retrieving block %s: %v", blockHash, err)
					return
				}
				// Check if this forces a reorg.
				currentTip := int64(dcr.blockCache.tipHeight())
				if blockVerbose.Height <= currentTip {
					dcr.blockCache.reorg(blockVerbose.Height)
				}
				block, err := dcr.blockCache.add(blockVerbose)
				if err != nil {
					log.Errorf("error adding block to cache")
				}
				dcr.signalMtx.RLock()
				for _, c := range dcr.blockChans {
					select {
					case c <- uint32(block.height):
					default:
						log.Errorf("tried sending block update on blocking channel")
					}
				}
				dcr.signalMtx.RUnlock()
			default:
				log.Warn("unknown message type in superQueue: %T", rawMsg)
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
		log.Errorf("error decoding serialized header: %v", err)
		return
	}
	dcr.anyQ <- blockHeader.BlockHash()
}

// Get the UTXO, populating the block data along the way. Only spendable UTXOs
// with know types of pubkey script will be returned.
func (dcr *dcrBackend) utxo(txHash *chainhash.Hash, vout uint32) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := dcr.getTxOutInfo(txHash, vout)
	if err != nil {
		return nil, err
	}

	blockHeight := uint32(verboseTx.BlockHeight)
	var blockHash chainhash.Hash
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
	}
	var maturity int64
	if txOut.Coinbase {
		maturity = int64(chainParams.CoinbaseMaturity)
	}

	return &UTXO{
		dcr:       dcr,
		height:    blockHeight,
		blockHash: blockHash,
		txHash:    *txHash,
		vout:      vout,
		maturity:  int32(maturity),
		pkScript:  pkScript,
	}, nil
}

// Get information for an unspent transaction output. Returns an error if the
// output has an unsupported pubkey script type.
func (dcr *dcrBackend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*chainjson.GetTxOutResult, *chainjson.TxRawResult, []byte, error) {
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetTxOut error for txid %s: %v", txHash, err)
	}
	if txOut == nil {
		return txOut, nil, nil, fmt.Errorf("UTXO - no unspent txout found for %s:%d", txHash, vout)
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return txOut, nil, nil, fmt.Errorf("pk script decode error for txid %s: %v", txHash, err)
	}
	if !isAcceptableScript(pkScript) {
		return txOut, nil, pkScript, asset.UnsupportedScriptError
	}

	verboseTx, err := dcr.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return txOut, nil, pkScript, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
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

// VerifySignatures checks that the message's signature was created with the
// private key for the provided public key. This is an asset.DEXAsset method.
func (dcr *dcrBackend) VerifySignature(msg, pkBytes, sigBytes []byte) bool {
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		log.Errorf("error decoding PublicKey from bytes: %v", err)
		return false
	}
	signature, err := secp256k1.ParseDERSignature(sigBytes)
	if err != nil {
		log.Errorf("error decoding Signature from bytes: %v", err)
		return false
	}
	return signature.Verify(msg, pubKey)
}

// connectNodeRPC attempts to create a new websocket connection to a dcrd node
// with the given credentials and optional notification handlers.
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

// Check if the pubkey script is of a supported type.
func isAcceptableScript(script []byte) bool {
	// Only P2PKH for now.
	return isPubKeyHashScript(script)
}

// isPubKeyHashScript returns whether or not the passed script is a standard
// pay-to-pubkey-hash script.
func isPubKeyHashScript(script []byte) bool {
	return extractPubKeyHash(script) != nil
}

// extractPubKeyHash extracts the pubkey hash from the passed script if it is a
// /standard pay-to-pubkey-hash script.  It will return nil otherwise.
func extractPubKeyHash(script []byte) []byte {
	// A pay-to-pubkey-hash script is of the form:
	//  OP_DUP OP_HASH160 <20-byte hash> OP_EQUALVERIFY OP_CHECKSIG
	if len(script) == 25 &&
		script[0] == txscript.OP_DUP &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUALVERIFY &&
		script[24] == txscript.OP_CHECKSIG {

		return script[3:23]
	}

	return nil
}

// Extract the sender and receiver addresses from a swap contract. If the
// provided script is not a swap contract, an error will be returned.
func extractSwapAddresses(pkScript []byte) (string, string, error) {
	// A swaap redemption sigScript is <pubkey> <secret> and satisfies the
	// following swap contract.
	//
	// OP_IF
	//  OP_SIZE hashSize OP_EQUALVERIFY OP_SHA256 OP_DATA_32 secretHash OP_EQUALVERIFY OP_DUP OP_HASH160 OP_DATA20 pkHashReceiver
	//     1   +   2    +      1       +    1    +   1      +   32     +      1       +   1  +   1      +    1    +    20
	// OP_ELSE
	//  OP_DATA4 locktime OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 OP_DATA_20 pkHashSender
	//     1    +    4   +           1          +   1   +  1   +    1     +   1      +    20
	// OP_ENDIF
	// OP_EQUALVERIFY
	// OP_CHECKSIG
	//
	// 5 bytes if-else-endif-equalverify-checksig
	// 1 + 2 + 1 + 1 + 1 + 32 + 1 + 1 + 1 + 1 + 20 = 62 bytes for redeem block
	// 1 + 4 + 1 + 1 + 1 + 1 + 1 + 20 = 30 bytes for refund block
	// 5 + 62 + 30 = 97 bytes
	if len(pkScript) != SwapContractSize {
		return "", "", fmt.Errorf("incorrect swap contract length")
	}
	if pkScript[0] == txscript.OP_IF &&
		pkScript[1] == txscript.OP_SIZE &&
		// secret key hash size (2 bytes)
		pkScript[4] == txscript.OP_EQUALVERIFY &&
		pkScript[5] == txscript.OP_SHA256 &&
		pkScript[6] == txscript.OP_DATA_32 &&
		// secretHash (32 bytes)
		pkScript[39] == txscript.OP_EQUALVERIFY &&
		pkScript[40] == txscript.OP_DUP &&
		pkScript[41] == txscript.OP_HASH160 &&
		pkScript[42] == txscript.OP_DATA_20 &&
		// receiver's pkh (20 bytes)
		pkScript[63] == txscript.OP_ELSE &&
		pkScript[64] == txscript.OP_DATA_4 &&
		// time (4 bytes)
		pkScript[69] == txscript.OP_CHECKLOCKTIMEVERIFY &&
		pkScript[70] == txscript.OP_DROP &&
		pkScript[71] == txscript.OP_DUP &&
		pkScript[72] == txscript.OP_HASH160 &&
		pkScript[73] == txscript.OP_DATA_20 &&
		// sender's pkh (20 bytes)
		pkScript[94] == txscript.OP_ENDIF &&
		pkScript[95] == txscript.OP_EQUALVERIFY &&
		pkScript[96] == txscript.OP_CHECKSIG {

		receiverAddr, err := dcrutil.NewAddressPubKeyHash(pkScript[43:63], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		senderAddr, err := dcrutil.NewAddressPubKeyHash(pkScript[74:94], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from sender's pubkey hash")
		}

		return senderAddr.String(), receiverAddr.String(), nil
	}
	return "", "", fmt.Errorf("invalid swap contract")
}
