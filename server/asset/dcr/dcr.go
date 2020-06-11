// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math"
	"strings"
	"sync"
	"time"

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

// DecodeCoinID creates a human-readable representation of a coin ID for Decred.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

func init() {
	asset.Register(assetName, &Driver{})
}

var (
	zeroHash chainhash.Hash
	// The blockPollInterval is the delay between calls to GetBestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second
)

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
	GetBestBlockHash() (*chainhash.Hash, error)
}

// Backend is an asset backend for Decred. It has methods for fetching output
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
	blockChans map[chan *asset.BlockUpdate]struct{}
	// The block cache stores just enough info about the blocks to prevent future
	// calls to GetBlockVerbose.
	blockCache *blockCache
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
	// When the exported constructor is used, the node will be an
	// rpcclient.Client.
	dcr.client, err = connectNodeRPC(cfg.RPCListen, cfg.RPCUser, cfg.RPCPass,
		cfg.RPCCert)
	if err != nil {
		return nil, err
	}

	// Ensure the network of the connected node is correct for the expected
	// dex.Network.
	net, err := dcr.client.GetCurrentNet()
	if err != nil {
		return nil, fmt.Errorf("getcurrentnet failure: %v", err)
	}
	var wantCurrencyNet wire.CurrencyNet
	switch network {
	case dex.Testnet:
		wantCurrencyNet = wire.TestNet3
	case dex.Mainnet:
		wantCurrencyNet = wire.MainNet
	case dex.Regtest: // dex.Simnet
		wantCurrencyNet = wire.SimNet
	}
	if net != wantCurrencyNet {
		return nil, fmt.Errorf("wrong net %v", net.String())
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
func (dcr *Backend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	c := make(chan *asset.BlockUpdate, size)
	dcr.signalMtx.Lock()
	defer dcr.signalMtx.Unlock()
	dcr.blockChans[c] = struct{}{}
	return c
}

// Contract is part of the asset.Backend interface. An asset.Contract is an
// output that has been validated as a swap contract for the passed redeem
// script. A spendable output is one that can be spent in the next block. Every
// regular-tree output from a non-coinbase transaction is spendable immediately.
// Coinbase and stake tree outputs are only spendable after CoinbaseMaturity
// confirmations. Pubkey scripts can be P2PKH or P2SH in either regular- or
// stake-tree flavor. P2PKH supports two alternative signatures, Schnorr and
// Edwards. Multi-sig P2SH redeem scripts are supported as well.
func (dcr *Backend) Contract(coinID []byte, redeemScript []byte) (asset.Contract, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	output, err := dcr.output(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}
	contract := &Contract{Output: output}
	// Verify contract and set refundAddress and swapAddress.
	err = contract.auditContract()
	if err != nil {
		return nil, err
	}
	return contract, nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (dcr *Backend) ValidateSecret(secret, contract []byte) bool {
	_, _, _, secretHash, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	if err != nil {
		dcr.log.Errorf("ValidateSecret->ExtractSwapDetails error: %v\n", err)
		return false
	}
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Redemption is an input that redeems a swap contract.
func (dcr *Backend) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
	txHash, vin, err := decodeCoinID(redemptionID)
	if err != nil {
		return nil, fmt.Errorf("error decoding redemption coin ID %x: %v", txHash, err)
	}
	input, err := dcr.input(txHash, vin)
	if err != nil {
		return nil, err
	}
	spends, err := input.spendsCoin(contractID)
	if err != nil {
		return nil, err
	}
	if !spends {
		return nil, fmt.Errorf("%x does not spend %x", redemptionID, contractID)
	}
	return input, nil
}

// FundingCoin is an unspent output.
func (dcr *Backend) FundingCoin(coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	utxo, err := dcr.utxo(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}
	if utxo.nonStandardScript {
		return nil, fmt.Errorf("non-standard script")
	}
	return utxo, nil
}

// ValidateCoinID attempts to decode the coinID.
func (dcr *Backend) ValidateCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
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

// VerifyUnspentCoin attempts to verify a coin ID by decoding the coin ID and
// retrieving the corresponding UTXO. If the coin is not found or no longer
// unspent, an asset.CoinNotFoundError is returned.
func (dcr *Backend) VerifyUnspentCoin(coinID []byte) error {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return fmt.Errorf("GetTxOut (%s:%d): %v", txHash.String(), vout, err)
	}
	if txOut == nil {
		return asset.CoinNotFoundError
	}
	return nil
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

	scriptAddrs, nonStandard, err := dexdcr.ExtractScriptAddrs(pkScript, chainParams)
	if err != nil {
		return "", 0, -1, fmt.Errorf("error parsing utxo script addresses")
	}
	if nonStandard {
		// This should be covered by the NumPKH check, but this is a more
		// informative error message.
		return "", 0, -1, fmt.Errorf("non-standard script")
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
		value := toAtoms(input.AmountIn)
		sumIn += value
		hash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, fmt.Errorf("error decoding previous tx hash %s for tx %s: %v", input.Txid, txHash, err)
		}
		inputs = append(inputs, txIn{prevTx: *hash, vout: input.Vout, value: value})
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
	return newTransaction(txHash, blockHash, lastLookup, verboseTx.BlockHeight, isStake, isCoinbase, inputs, outputs, feeRate), nil
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
		blockCache: newBlockCache(logger),
		log:        logger,
		blockChans: make(map[chan *asset.BlockUpdate]struct{}),
	}
}

// Run processes the queue and monitors the application context. The
// dcrd-registered handlers should perform any necessary type conversion and
// then deposit the payload into the anyQ channel.
func (dcr *Backend) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	// Shut down the RPC client on ctx.Done().
	go func() {
		<-ctx.Done()
		dcr.shutdown()
		wg.Done()
	}()
	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()
	addBlock := func(block *chainjson.GetBlockVerboseResult, reorg bool) {
		_, err := dcr.blockCache.add(block)
		if err != nil {
			dcr.log.Errorf("error adding new best block to cache: %v", err)
		}
		dcr.signalMtx.Lock()
		dcr.log.Debugf("Notifying %d dcr asset consumers of new block at height %d",
			len(dcr.blockChans), block.Height)
		for c := range dcr.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err:   nil,
				Reorg: reorg,
			}:
			default:
				// Commented to try sends on future blocks.
				// close(c)
				// delete(dcr.blockChans, c)
				//
				// TODO: Allow the receiver (e.g. Swapper.Run) to inform done
				// status so the channels can be retired cleanly rather than
				// trying them forever.
			}
		}
		dcr.signalMtx.Unlock()
	}

	sendErr := func(err error) {
		dcr.log.Error(err)
		dcr.signalMtx.Lock()
		for c := range dcr.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err: err,
			}:
			default:
				dcr.log.Errorf("failed to send sending block update on blocking channel")
				// close(c)
				// delete(dcr.blockChans, c)
			}
		}
		dcr.signalMtx.Unlock()
	}

	sendErrFmt := func(s string, a ...interface{}) {
		sendErr(fmt.Errorf(s, a...))
	}

out:
	for {
		select {

		case <-blockPoll.C:
			tip := dcr.blockCache.tip()
			bestHash, err := dcr.node.GetBestBlockHash()
			if err != nil {
				sendErr(asset.NewConnectionError("error retrieving best block: %v", err))
				continue
			}
			if *bestHash == tip.hash {
				continue
			}

			best := bestHash.String()
			block, err := dcr.node.GetBlockVerbose(bestHash, false)
			if err != nil {
				sendErrFmt("error retrieving block %s: %v", best, err)
				continue
			}
			// If this doesn't build on the best known block, look for a reorg.
			prevHash, err := chainhash.NewHashFromStr(block.PreviousHash)
			if err != nil {
				sendErrFmt("error parsing previous hash %s: %v", block.PreviousHash, err)
				continue
			}
			// If it builds on the best block or the cache is empty, it's good to add.
			if *prevHash == tip.hash || tip.height == 0 {
				dcr.log.Debugf("Run: Processing new block %s", bestHash)
				addBlock(block, false)
				continue
			}
			// It is either a reorg, or the previous block is not the cached
			// best block. Crawl blocks backwards until finding a mainchain
			// block, flagging blocks from the cache as orphans along the way.
			iHash := &tip.hash
			reorgHeight := int64(0)
			for {
				if *iHash == zeroHash {
					break
				}
				iBlock, err := dcr.node.GetBlockVerbose(iHash, false)
				if err != nil {
					sendErrFmt("error retrieving block %s: %v", iHash, err)
					break
				}
				if iBlock.Confirmations > -1 {
					// This is a mainchain block, nothing to do.
					break
				}
				if iBlock.Height == 0 {
					break
				}
				reorgHeight = iBlock.Height
				iHash, err = chainhash.NewHashFromStr(iBlock.PreviousHash)
				if err != nil {
					sendErrFmt("error decoding previous hash %s for block %s: %v",
						iBlock.PreviousHash, iHash.String(), err)
					// Some blocks on the side chain may not be flagged as
					// orphaned, but still proceed, flagging the ones we have
					// identified and adding the new best block to the cache and
					// setting it to the best block in the cache.
					break
				}
			}
			var reorg bool
			if reorgHeight > 0 {
				reorg = true
				dcr.log.Infof("Reorg from %s (%d) to %s (%d) detected.",
					tip.hash, tip.height, bestHash, block.Height)
				dcr.blockCache.reorg(reorgHeight)
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

// blockInfo returns block information for the verbose transaction data. The
// current tip hash is also returned as a convenience.
func (dcr *Backend) blockInfo(verboseTx *chainjson.TxRawResult) (blockHeight uint32, blockHash chainhash.Hash, tipHash *chainhash.Hash, err error) {
	blockHeight = uint32(verboseTx.BlockHeight)
	tip := dcr.blockCache.tipHash()
	if tip != zeroHash {
		tipHash = &tip
	}
	// Assumed to be valid while in mempool, so skip the validity check.
	if verboseTx.Confirmations > 0 {
		if blockHeight == 0 {
			err = fmt.Errorf("zero block height for output with "+
				"non-zero confirmation count (%s has %d confirmations)", verboseTx.Txid, verboseTx.Confirmations)
			return
		}
		var blk *dcrBlock
		blk, err = dcr.getBlockInfo(verboseTx.BlockHash)
		if err != nil {
			return
		}
		blockHeight = blk.height
		blockHash = blk.hash
	}
	return
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

	blockHeight, blockHash, lastLookup, err := dcr.blockInfo(verboseTx)
	if err != nil {
		return nil, err
	}

	// Coinbase, vote, and revocation transactions must mature before spending.
	var maturity int64
	if scriptType.IsStake() || txOut.Coinbase {
		// TODO: this is specific to the output with stake transactions. Must
		// check the output type.
		maturity = int64(chainParams.CoinbaseMaturity)
	}
	if txOut.Confirmations < maturity {
		return nil, immatureTransactionError
	}

	tx, err := dcr.transaction(txHash, verboseTx)
	if err != nil {
		return nil, fmt.Errorf("error fetching verbose transaction data: %v", err)
	}

	out := &Output{
		TXIO: TXIO{
			dcr:        dcr,
			tx:         tx,
			height:     blockHeight,
			blockHash:  blockHash,
			maturity:   int32(maturity),
			lastLookup: lastLookup,
		},
		vout:              vout,
		scriptType:        scriptType,
		nonStandardScript: inputNfo.NonStandardScript,
		pkScript:          pkScript,
		redeemScript:      redeemScript,
		numSigs:           inputNfo.ScriptAddrs.NRequired,
		// The total size associated with the wire.TxIn.
		spendSize: inputNfo.SigScriptSize + dexdcr.TxInOverhead,
		value:     toAtoms(txOut.Value),
	}
	return &UTXO{out}, nil
}

// newTXIO creates a TXIO for any transaction, spent or unspent. The caller must
// set the maturity field.
func (dcr *Backend) newTXIO(txHash *chainhash.Hash) (*TXIO, int64, error) {
	verboseTx, err := dcr.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, asset.CoinNotFoundError
		}
		return nil, 0, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txHash, err)
	}
	tx, err := dcr.transaction(txHash, verboseTx)
	if err != nil {
		return nil, 0, fmt.Errorf("error fetching verbose transaction data: %v", err)
	}
	blockHeight, blockHash, lastLookup, err := dcr.blockInfo(verboseTx)
	if err != nil {
		return nil, 0, err
	}
	return &TXIO{
		dcr:       dcr,
		tx:        tx,
		height:    blockHeight,
		blockHash: blockHash,
		// maturity TODO: move this into an output specific type.
		lastLookup: lastLookup,
	}, verboseTx.Confirmations, nil
}

// input gets the transaction input.
func (dcr *Backend) input(txHash *chainhash.Hash, vin uint32) (*Input, error) {
	txio, _, err := dcr.newTXIO(txHash)
	if err != nil {
		return nil, err
	}
	if int(vin) >= len(txio.tx.ins) {
		return nil, fmt.Errorf("tx %v has %d outputs (no vin %d)", txHash, len(txio.tx.ins), vin)
	}
	return &Input{
		TXIO: *txio,
		vin:  vin,
	}, nil
}

// output gets the transaction output.
func (dcr *Backend) output(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*Output, error) {
	txio, confs, err := dcr.newTXIO(txHash)
	if err != nil {
		return nil, err
	}
	if int(vout) >= len(txio.tx.outs) {
		return nil, fmt.Errorf("tx %v has %d outputs (no vout %d)", txHash, len(txio.tx.outs), vout)
	}

	txOut := txio.tx.outs[vout]
	pkScript := txOut.pkScript
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
			return nil, fmt.Errorf("output error: %v", err)
		}
		if !bytes.Equal(dcrutil.Hash160(redeemScript), scriptHash) {
			return nil, fmt.Errorf("script hash check failed for output %s:%d", txHash, vout)
		}
	}

	scrAddrs := inputNfo.ScriptAddrs
	addresses := make([]string, scrAddrs.NumPK+scrAddrs.NumPKH)
	for i, addr := range append(scrAddrs.PkHashes, scrAddrs.PubKeys...) {
		addresses[i] = addr.String() // unconverted
	}

	// Coinbase, vote, and revocation transactions must mature before spending.
	var maturity int64
	if scriptType.IsStake() || txio.tx.isCoinbase {
		maturity = int64(chainParams.CoinbaseMaturity)
	}
	if confs < maturity {
		return nil, immatureTransactionError
	}
	txio.maturity = int32(maturity)

	return &Output{
		TXIO:              *txio,
		vout:              vout,
		value:             txOut.value,
		addresses:         addresses,
		scriptType:        scriptType,
		nonStandardScript: inputNfo.NonStandardScript,
		pkScript:          pkScript,
		redeemScript:      redeemScript,
		numSigs:           scrAddrs.NRequired,
		// The total size associated with the wire.TxIn.
		spendSize: inputNfo.SigScriptSize + dexdcr.TxInOverhead,
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
		return nil, nil, asset.CoinNotFoundError
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
func connectNodeRPC(host, user, pass, cert string) (*rpcclient.Client, error) {

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

	dcrdClient, err := rpcclient.New(config, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to start dcrd RPC client: %v", err)
	}

	return dcrdClient, nil
}

// decodeCoinID decodes the coin ID into a tx hash and a vin/vout index.
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

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	// TODO: Could probably do this right with errors.As if we enforce an RPC
	// version when connecting.
	return strings.HasPrefix(err.Error(), "-5:")
}
