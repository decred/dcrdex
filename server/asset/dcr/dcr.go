// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math"
	"strings"
	"sync"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/wire"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the DCR backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

func init() {
	asset.Register(assetName, &Driver{})
}

var zeroHash chainhash.Hash

type Error = dex.Error

const (
	assetName                = "dcr"
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

// Backend is an asset backend for Decred. It has methods for fetching UTXO
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. Backend implements asset.Backend, so provides
// exported methods for DEX-related blockchain info.
type Backend struct {
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
	log dex.Logger
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)

// NewBackend is the exported constructor by which the DEX will import the
// Backend. The provided context.Context should be canceled when the DEX
// application exits. If configPath is an empty string, the backend will
// attempt to read the settings directly from the dcrd config file in its
// default system location.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (*Backend, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// package variable.
	cfg, err := loadConfig(configPath, network)
	if err != nil {
		return nil, err
	}
	dcr := unconnectedDCR(logger)
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

// InitTxSize is an asset.Backend method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (btc *Backend) InitTxSize() uint32 {
	return dexdcr.InitTxSize
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the dcr package. Part of the asset.Backend interface.
func (dcr *Backend) BlockChannel(size int) chan uint32 {
	c := make(chan uint32, size)
	dcr.signalMtx.Lock()
	defer dcr.signalMtx.Unlock()
	dcr.blockChans = append(dcr.blockChans, c)
	return c
}

// Coin is part of the asset.Backend interface, so returns the asset.Coin type.
// Only spendable utxos with known types of pubkey script will be successfully
// retrieved. A spendable utxo is one that can be spent in the next block. Every
// regular-tree output from a non-coinbase transaction is spendable immediately.
// Coinbase and stake tree outputs are only spendable after CoinbaseMaturity
// confirmations. Pubkey scripts can be P2PKH or P2SH in either regular- or
// stake-tree flavor. P2PKH supports two alternative signatures, Schnorr and
// Edwards. Multi-sig P2SH redeem scripts are supported as well.
func (dcr *Backend) Coin(coinID []byte, redeemScript []byte) (asset.Coin, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	return dcr.utxo(txHash, vout, redeemScript)
}

// ValidateCoinID attempts to decode the coinID.
func (dcr *Backend) ValidateCoinID(coinID []byte) error {
	_, _, err := decodeCoinID(coinID)
	return err
}

// ValidateContract ensures that the swap contract is constructed properly, and
// contains valid sender and receiver addresses.
func (dcr *Backend) ValidateContract(contract []byte) error {
	_, _, _, _, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	return err
}

// CheckAddress checks that the given address is parseable.
func (dcr *Backend) CheckAddress(addr string) bool {
	_, err := dcrutil.DecodeAddress(addr, chainParams)
	return err == nil
}

// UnspentCoinDetails gets the recipient address, value, and confirmations of
// unspent coins. For DCR, this corresponds to a UTXO. If the utxo does not
// exist or has a pubkey script of the wrong type, an error will be returned.
func (dcr *Backend) UnspentCoinDetails(coinID []byte) (addr string, val uint64, confs int64, err error) {
	txHash, vout, errCoin := decodeCoinID(coinID)
	if errCoin != nil {
		err = fmt.Errorf("error decoding coin ID %x: %v", coinID, errCoin)
		return
	}
	return dcr.UTXODetails(txHash.String(), vout)
}

// UTXODetails gets the recipient address, value, and confs of an unspent
// P2PKH transaction output. If the utxo does not exist or has a pubkey script
// of the wrong type, an error will be returned.
func (dcr *Backend) UTXODetails(txid string, vout uint32) (string, uint64, int64, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return "", 0, -1, fmt.Errorf("error decoding tx ID %s: %v", txid, err)
	}
	txOut, pkScript, err := dcr.getUnspentTxOut(txHash, vout)
	if err != nil {
		return "", 0, -1, err
	}
	scriptType := dexdcr.ParseScriptType(dexdcr.CurrentScriptVersion, pkScript, nil)
	if scriptType == dexdcr.ScriptUnsupported {
		return "", 0, -1, dex.UnsupportedScriptError
	}
	if !scriptType.IsP2PKH() {
		return "", 0, -1, dex.UnsupportedScriptError
	}

	scriptAddrs, err := dexdcr.ExtractScriptAddrs(pkScript, chainParams)
	if err != nil {
		return "", 0, -1, fmt.Errorf("error parsing utxo script addresses")
	}
	if scriptAddrs.NumPK != 0 {
		return "", 0, -1, fmt.Errorf("pubkey addresses not supported for P2PKHDetails")
	}
	if scriptAddrs.NumPKH != 1 {
		return "", 0, -1, fmt.Errorf("multi-sig not supported for P2PKHDetails")
	}
	return scriptAddrs.PkHashes[0].String(), toAtoms(txOut.Value), txOut.Confirmations, nil
}

// Get the Tx. Transaction info is not cached, so every call will result in a
// GetRawTransactionVerbose RPC call.
func (dcr *Backend) transaction(txHash *chainhash.Hash, verboseTx *chainjson.TxRawResult) (*Tx, error) {
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
	var isCoinbase bool
	for _, input := range verboseTx.Vin {
		isCoinbase = input.Coinbase != ""
		sumIn += toAtoms(input.AmountIn)
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
		sumOut += toAtoms(output.Value)
		outputs = append(outputs, txOut{
			value:    toAtoms(output.Value),
			pkScript: pkScript,
		})
	}
	feeRate := (sumIn - sumOut) / uint64(len(verboseTx.Hex)/2)
	if isCoinbase {
		feeRate = 0
	}
	return newTransaction(txHash, blockHash, lastLookup, verboseTx.BlockHeight, isStake, inputs, outputs, feeRate), nil
}

// Shutdown down the rpcclient.Client.
func (dcr *Backend) shutdown() {
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// unconnectedDCR returns a Backend without a node. The node should be set
// before use.
func unconnectedDCR(logger dex.Logger) *Backend {
	return &Backend{
		blockChans: make([]chan uint32, 0),
		blockCache: newBlockCache(logger),
		anyQ:       make(chan interface{}, 128), // way bigger than needed.
		log:        logger,
	}
}

// Run processes the queue and monitors the application context. The
// dcrd-registered handlers should perform any necessary type conversion and
// then deposit the payload into the anyQ channel.
func (dcr *Backend) Run(ctx context.Context) {
	defer dcr.shutdown()
out:
	for {
		select {
		case rawMsg := <-dcr.anyQ:
			switch msg := rawMsg.(type) {
			case *chainhash.Hash:
				// This is a new block notification.
				blockHash := msg
				dcr.log.Debugf("Run: Processing new block %s", blockHash)
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
					case c <- block.height:
					default:
						dcr.log.Errorf("tried sending block update on blocking channel")
					}
				}
				dcr.signalMtx.RUnlock()
			default:
				dcr.log.Warn("unknown message type in Run: %T", rawMsg)
			}
		case <-ctx.Done():
			break out
		}
	}
}

// A callback to be registered with dcrd. It is critical that no RPC calls are
// made from this method. Doing so will likely result in a deadlock, as per
// https://github.com/decred/dcrd/blob/952bd7bba34c8aeab86f63f9c9f69fc74ff1a7e1/rpcclient/notify.go#L78
func (dcr *Backend) onBlockConnected(serializedHeader []byte, _ [][]byte) {
	blockHeader := new(wire.BlockHeader)
	err := blockHeader.FromBytes(serializedHeader)
	if err != nil {
		dcr.log.Errorf("error decoding serialized header: %v", err)
		return
	}
	h := blockHeader.BlockHash()
	// TODO: Instead of a buffered channel, anyQ, make a queue with a slice so
	// the buffer size has no role in correctness i.e. the ability of this
	// function to work when previous blocks require RPC calls to complete their
	// processing. That is, if the channel buffer is full there is likely to be
	// deadlock waiting for other RPCs.
	dcr.anyQ <- &h
}

// validateTxOut validates an outpoint (txHash:out) by retrieving associated
// output's pkScript, and if the provided redeemScript is not empty, verifying
// that the pkScript is a P2SH with a script hash to which the redeem script
// hashes. This also screens out multi-sig scripts.
func (dcr *Backend) validateTxOut(txHash *chainhash.Hash, vout uint32, redeemScript []byte) error {
	_, pkScript, err := dcr.getUnspentTxOut(txHash, vout)
	if err != nil {
		return err
	}

	scriptType := dexdcr.ParseScriptType(dexdcr.CurrentScriptVersion, pkScript, redeemScript)
	if scriptType == dexdcr.ScriptUnsupported {
		return dex.UnsupportedScriptError
	}

	switch {
	case scriptType.IsP2SH(): // regular or stake (vsp vote) p2sh
		if len(redeemScript) == 0 {
			return fmt.Errorf("no redeem script provided for P2SH pkScript")
		}
		scriptHash, err := dexdcr.ExtractScriptHashByType(scriptType, pkScript)
		if err != nil {
			return fmt.Errorf("failed to extract script hash for P2SH pkScript: %v", err)
		}
		// Check the script hash against the hash of the redeem script.
		if !bytes.Equal(dcrutil.Hash160(redeemScript), scriptHash) {
			return fmt.Errorf("redeem script does not match script hash from P2SH pkScript")
		}
	case len(redeemScript) > 0:
		return fmt.Errorf("redeem script provided for non P2SH pubkey script")
	}

	return nil
}

// Get the UTXO, populating the block data along the way.
func (dcr *Backend) utxo(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := dcr.getTxOutInfo(txHash, vout)
	if err != nil {
		return nil, err
	}

	inputNfo, err := dexdcr.InputInfo(pkScript, redeemScript, chainParams)
	if err != nil {
		return nil, err
	}
	scriptType := inputNfo.ScriptType

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.IsP2SH() {
		scriptHash, err := dexdcr.ExtractScriptHashByType(scriptType, pkScript)
		if err != nil {
			return nil, fmt.Errorf("utxo error: %v", err)
		}
		if !bytes.Equal(dcrutil.Hash160(redeemScript), scriptHash) {
			return nil, fmt.Errorf("script hash check failed for utxo %s,%d", txHash, vout)
		}
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
		blockHeight = blk.height
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
	if scriptType.IsStake() || txOut.Coinbase {
		maturity = int64(chainParams.CoinbaseMaturity)
	}
	if txOut.Confirmations < maturity {
		return nil, immatureTransactionError
	}

	tx, err := dcr.transaction(txHash, verboseTx)
	if err != nil {
		return nil, fmt.Errorf("error fetching verbose transaction data: %v", err)
	}

	return &UTXO{
		dcr:          dcr,
		tx:           tx,
		height:       blockHeight,
		blockHash:    blockHash,
		vout:         vout,
		maturity:     int32(maturity),
		scriptType:   scriptType,
		pkScript:     pkScript,
		redeemScript: redeemScript,
		numSigs:      inputNfo.ScriptAddrs.NRequired,
		// The total size associated with the wire.TxIn.
		spendSize:  inputNfo.SigScriptSize + dexdcr.TxInOverhead,
		value:      toAtoms(txOut.Value),
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
func (dcr *Backend) getUnspentTxOut(txHash *chainhash.Hash, vout uint32) (*chainjson.GetTxOutResult, []byte, error) {
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, nil, fmt.Errorf("GetTxOut error for output %s:%d: %v", txHash, vout, err) // TODO: make RPC error type for client message sanitization
	}
	if txOut == nil {
		return nil, nil, fmt.Errorf("UTXO - no unspent txout found for %s:%d", txHash, vout)
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode pubkey script from '%s' for output %s:%d", txOut.ScriptPubKey.Hex, txHash, vout)
	}
	return txOut, pkScript, nil
}

// Get information for an unspent transaction output, plus the verbose
// transaction.
func (dcr *Backend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*chainjson.GetTxOutResult, *chainjson.TxRawResult, []byte, error) {
	txOut, pkScript, err := dcr.getUnspentTxOut(txHash, vout)
	if err != nil {
		return nil, nil, nil, err
	}
	verboseTx, err := dcr.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}
	return txOut, verboseTx, pkScript, nil
}

// Get the block information, checking the cache first. Same as
// getDcrBlock, but takes a string argument.
func (dcr *Backend) getBlockInfo(blockid string) (*dcrBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(blockid)
	if err != nil {
		return nil, fmt.Errorf("unable to decode block hash from %s", blockid)
	}
	return dcr.getDcrBlock(blockHash)
}

// Get the block information, checking the cache first.
func (dcr *Backend) getDcrBlock(blockHash *chainhash.Hash) (*dcrBlock, error) {
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
func (dcr *Backend) getMainchainDcrBlock(height uint32) (*dcrBlock, error) {
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

// decodeCoinID decodes the coin ID into a tx hash and a vout.
func decodeCoinID(coinID []byte) (*chainhash.Hash, uint32, error) {
	if len(coinID) != 36 {
		return nil, 0, fmt.Errorf("coin ID wrong length. expected 36, got %d", len(coinID))
	}
	var txHash chainhash.Hash
	copy(txHash[:], coinID[:32])
	return &txHash, binary.BigEndian.Uint32(coinID[32:]), nil
}

// toCoinID converts the outpoint to a coin ID.
func toCoinID(txHash *chainhash.Hash, vout uint32) []byte {
	hashLen := len(txHash)
	b := make([]byte, hashLen+4)
	copy(b[:hashLen], txHash[:])
	binary.BigEndian.PutUint32(b[hashLen:], vout)
	return b
}

// Convert the DCR value to atoms.
func toAtoms(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}
