// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcutil"
)

const methodGetBlockchainInfo = "getblockchaininfo"

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the BTC backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Bitcoin.
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

const (
	assetName                = "btc"
	immatureTransactionError = dex.ErrorKind("immature output")
)

// btcNode represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient.Client, and all methods are matches for Client
// methods. For testing, it can be satisfied by a stub.
type btcNode interface {
	EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)
	GetBlockVerbose(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash() (*chainhash.Hash, error)
	RawRequest(method string, params []json.RawMessage) (json.RawMessage, error)
}

// Backend is a dex backend for Bitcoin or a Bitcoin clone. It has methods for
// fetching UTXO information and subscribing to block updates. It maintains a
// cache of block data for quick lookups. Backend implements asset.Backend, so
// provides exported methods for DEX-related blockchain info.
type Backend struct {
	cfg *dexbtc.Config
	// The asset name (e.g. btc), primarily for logging purposes.
	name string
	// segwit should be set to true for blockchains that support segregated
	// witness.
	segwit bool
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
	blockChans  map[chan *asset.BlockUpdate]struct{}
	chainParams *chaincfg.Params
	// A logger will be provided by the dex for this backend. All logging should
	// use the provided logger.
	log dex.Logger
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)

// NewBackend is the exported constructor by which the DEX will import the
// backend. The configPath can be an empty string, in which case the standard
// system location of the bitcoind config file is assumed.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = &chaincfg.MainNetParams
	case dex.Testnet:
		params = &chaincfg.TestNet3Params
	case dex.Regtest:
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("bitcoin")
	}

	return NewBTCClone(assetName, true, configPath, logger, network, params, dexbtc.RPCPorts)
}

func newBTC(name string, segwit bool, chainParams *chaincfg.Params, logger dex.Logger, cfg *dexbtc.Config) *Backend {
	return &Backend{
		cfg:         cfg,
		name:        name,
		blockCache:  newBlockCache(),
		blockChans:  make(map[chan *asset.BlockUpdate]struct{}),
		chainParams: chainParams,
		log:         logger,
		segwit:      segwit,
	}
}

// NewBTCClone creates a BTC backend for a set of network parameters and default
// network ports. A BTC clone can use this method, possibly in conjunction with
// ReadCloneParams, to create a Backend for other assets with minimal coding.
// See ReadCloneParams and CompatibilityCheck for more info.
func NewBTCClone(name string, segwit bool, configPath string, logger dex.Logger, network dex.Network,
	params *chaincfg.Params, ports dexbtc.NetPorts) (*Backend, error) {

	// Read the configuration parameters
	cfg, err := dexbtc.LoadConfigFromPath(configPath, name, network, ports)
	if err != nil {
		return nil, err
	}

	return newBTC(name, segwit, params, logger, cfg), nil
}

func (btc *Backend) shutdown() {
	if btc.client != nil {
		btc.client.Shutdown()
		btc.client.WaitForShutdown()
	}
}

// Connect connects to the node RPC server. A dex.Connector.
func (btc *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         btc.cfg.RPCBind,
		User:         btc.cfg.RPCUser,
		Pass:         btc.cfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating %q RPC client: %w", btc.name, err)
	}

	// Setting the client field will enable shutdown
	btc.client = client
	btc.node = client

	// Prime the cache
	bestHash, err := btc.client.GetBestBlockHash()
	if err != nil {
		btc.shutdown()
		return nil, fmt.Errorf("error getting best block from rpc: %w", err)
	}
	if bestHash != nil {
		if _, err = btc.getBtcBlock(bestHash); err != nil {
			btc.shutdown()
			return nil, fmt.Errorf("error priming the cache: %w", err)
		}
	}

	if _, err = btc.FeeRate(); err != nil {
		btc.log.Warnf("%s backend started without fee estimation available: %v", btc.name, err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.run(ctx)
	}()
	return &wg, nil
}

// Contract is part of the asset.Backend interface. An asset.Contract is an
// output that has been validated as a swap contract for the passed redeem
// script. A spendable output is one that can be spent in the next block. Every
// output from a non-coinbase transaction is spendable immediately. Coinbase
// outputs are only spendable after CoinbaseMaturity confirmations. Pubkey
// scripts can be P2PKH or P2SH. Multi-sig P2SH redeem scripts are supported.
func (btc *Backend) Contract(coinID []byte, redeemScript []byte) (asset.Contract, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	output, err := btc.output(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}
	contract := &Contract{Output: output}
	// Verify contract and set refundAddress and swapAddress.
	err = btc.auditContract(contract)
	if err != nil {
		return nil, err
	}
	return contract, nil
}

// VerifySecret checks that the secret satisfies the contract.
func (btc *Backend) ValidateSecret(secret, contract []byte) bool {
	_, _, _, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		btc.log.Errorf("ValidateSecret->ExtractSwapDetails error: %v\n", err)
		return false
	}
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Synced is true if the blockchain is ready for action.
func (btc *Backend) Synced() (bool, error) {
	chainInfo, err := btc.getBlockchainInfo()
	if err != nil {
		return false, fmt.Errorf("GetBlockChainInfo error: %w", err)
	}
	return !chainInfo.InitialBlockDownload && chainInfo.Headers-chainInfo.Blocks <= 1, nil
}

// Redemption is an input that redeems a swap contract.
func (btc *Backend) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
	txHash, vin, err := decodeCoinID(redemptionID)
	if err != nil {
		return nil, fmt.Errorf("error decoding redemption coin ID %x: %w", txHash, err)
	}
	input, err := btc.input(txHash, vin)
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
func (btc *Backend) FundingCoin(_ context.Context, coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}

	utxo, err := btc.utxo(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}

	if utxo.nonStandardScript {
		return nil, fmt.Errorf("non-standard script")
	}
	return utxo, nil
}

// ValidateCoinID attempts to decode the coinID.
func (btc *Backend) ValidateCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

// ValidateContract ensures that the swap contract is constructed properly, and
// contains valid sender and receiver addresses.
func (btc *Backend) ValidateContract(contract []byte) error {
	_, _, _, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	return err
}

// VerifyUnspentCoin attempts to verify a coin ID by decoding the coin ID and
// retrieving the corresponding UTXO. If the coin is not found or no longer
// unspent, an asset.CoinNotFoundError is returned.
func (btc *Backend) VerifyUnspentCoin(_ context.Context, coinID []byte) error {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return fmt.Errorf("GetTxOut (%s:%d): %w", txHash.String(), vout, err)
	}
	if txOut == nil {
		return asset.CoinNotFoundError
	}
	return nil
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the btc package. Part of the asset.Backend interface.
func (btc *Backend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	c := make(chan *asset.BlockUpdate, size)
	btc.signalMtx.Lock()
	defer btc.signalMtx.Unlock()
	btc.blockChans[c] = struct{}{}
	return c
}

// InitTxSize is an asset.Backend method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (btc *Backend) InitTxSize() uint32 {
	if btc.segwit {
		return dexbtc.InitTxSizeSegwit
	}
	return dexbtc.InitTxSize
}

// InitTxSizeBase is InitTxSize not including an input.
func (btc *Backend) InitTxSizeBase() uint32 {
	if btc.segwit {
		return dexbtc.InitTxSizeBaseSegwit
	}
	return dexbtc.InitTxSizeBase
}

// FeeRate returns the current optimal fee rate in sat / byte.
func (btc *Backend) FeeRate() (uint64, error) {
	feeResult, err := btc.node.EstimateSmartFee(1, &btcjson.EstimateModeConservative)
	if err != nil {
		return 0, err
	}
	if len(feeResult.Errors) > 0 {
		return 0, fmt.Errorf(strings.Join(feeResult.Errors, "; "))
	}
	if feeResult.FeeRate == nil {
		return 0, fmt.Errorf("no fee rate available")
	}
	satPerKB, err := btcutil.NewAmount(*feeResult.FeeRate)
	if err != nil {
		return 0, err
	}
	satPerB := uint64(math.Round(float64(satPerKB) / 1000))
	if satPerB == 0 {
		satPerB = 1
	}
	return satPerB, nil
}

// CheckAddress checks that the given address is parseable.
func (btc *Backend) CheckAddress(addr string) bool {
	_, err := btcutil.DecodeAddress(addr, btc.chainParams)
	return err == nil
}

// blockInfo returns block information for the verbose transaction data. The
// current tip hash is also returned as a convenience.
func (btc *Backend) blockInfo(verboseTx *btcjson.TxRawResult) (blockHeight uint32, blockHash chainhash.Hash, tipHash *chainhash.Hash, err error) {
	h := btc.blockCache.tipHash()
	if h != zeroHash {
		tipHash = &h
	}
	if verboseTx.Confirmations > 0 {
		var blk *cachedBlock
		blk, err = btc.getBlockInfo(verboseTx.BlockHash)
		if err != nil {
			return
		}
		blockHeight = blk.height
		blockHash = blk.hash
	}
	return
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// call is used internally to  marshal parmeters and send requests to  the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (btc *Backend) call(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := btc.node.RawRequest(method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

// getBlockchainInfoResult models the data returned from the getblockchaininfo
// command.
type getBlockchainInfoResult struct {
	Blocks               int64  `json:"blocks"`
	Headers              int64  `json:"headers"`
	BestBlockHash        string `json:"bestblockhash"`
	InitialBlockDownload bool   `json:"initialblockdownload"`
}

// getBlockchainInfo sends the getblockchaininfo request and returns the result.
func (btc *Backend) getBlockchainInfo() (*getBlockchainInfoResult, error) {
	chainInfo := new(getBlockchainInfoResult)
	err := btc.call(methodGetBlockchainInfo, nil, chainInfo)
	if err != nil {
		return nil, err
	}
	return chainInfo, nil
}

// Get the UTXO data and perform some checks for script support.
func (btc *Backend) utxo(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := btc.getTxOutInfo(txHash, vout)
	if err != nil {
		return nil, err
	}

	inputNfo, err := dexbtc.InputInfo(pkScript, redeemScript, btc.chainParams)
	if err != nil {
		return nil, err
	}
	scriptType := inputNfo.ScriptType

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		scriptHash := dexbtc.ExtractScriptHash(pkScript)
		if scriptType.IsSegwit() {
			shash := sha256.Sum256(redeemScript)
			if !bytes.Equal(shash[:], scriptHash) {
				return nil, fmt.Errorf("(utxo:segwit) script hash check failed for utxo %s,%d", txHash, vout)
			}
		} else {
			if !bytes.Equal(btcutil.Hash160(redeemScript), scriptHash) {
				return nil, fmt.Errorf("(utxo:non-segwit) script hash check failed for utxo %s,%d", txHash, vout)
			}
		}
	}

	// Get block information.
	blockHeight, blockHash, lastLookup, err := btc.blockInfo(verboseTx)
	if err != nil {
		return nil, err
	}

	// Coinbase transactions must mature before spending.
	var maturity int64
	if txOut.Coinbase {
		maturity = int64(btc.chainParams.CoinbaseMaturity)
	}
	if txOut.Confirmations < maturity {
		return nil, immatureTransactionError
	}

	tx, err := btc.transaction(txHash, verboseTx)
	if err != nil {
		return nil, fmt.Errorf("error fetching verbose transaction data: %w", err)
	}

	out := &Output{
		TXIO: TXIO{
			btc:        btc,
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
		spendSize:         inputNfo.VBytes(),
		value:             toSat(txOut.Value),
	}
	return &UTXO{out}, nil
}

// newTXIO creates a TXIO for a transaction, spent or unspent.
func (btc *Backend) newTXIO(txHash *chainhash.Hash) (*TXIO, int64, error) {
	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, asset.CoinNotFoundError
		}
		return nil, 0, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txHash, err)
	}
	tx, err := btc.transaction(txHash, verboseTx)
	if err != nil {
		return nil, 0, fmt.Errorf("error fetching verbose transaction data: %w", err)
	}
	blockHeight, blockHash, lastLookup, err := btc.blockInfo(verboseTx)
	if err != nil {
		return nil, 0, err
	}
	var maturity int32
	if tx.isCoinbase {
		maturity = int32(btc.chainParams.CoinbaseMaturity)
	}
	return &TXIO{
		btc:        btc,
		tx:         tx,
		height:     blockHeight,
		blockHash:  blockHash,
		maturity:   maturity,
		lastLookup: lastLookup,
	}, int64(verboseTx.Confirmations), nil
}

// input gets the transaction input.
func (btc *Backend) input(txHash *chainhash.Hash, vin uint32) (*Input, error) {
	txio, _, err := btc.newTXIO(txHash)
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
func (btc *Backend) output(txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*Output, error) {
	txio, confs, err := btc.newTXIO(txHash)
	if err != nil {
		return nil, err
	}
	if int(vout) >= len(txio.tx.outs) {
		return nil, fmt.Errorf("tx %v has %d outputs (no vout %d)", txHash, len(txio.tx.outs), vout)
	}

	txOut := txio.tx.outs[vout]
	pkScript := txOut.pkScript
	inputNfo, err := dexbtc.InputInfo(pkScript, redeemScript, btc.chainParams)
	if err != nil {
		return nil, err
	}
	scriptType := inputNfo.ScriptType

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		scriptHash := dexbtc.ExtractScriptHash(pkScript)
		if scriptType.IsSegwit() {
			shash := sha256.Sum256(redeemScript)
			if !bytes.Equal(shash[:], scriptHash) {
				return nil, fmt.Errorf("(output:segwit) script hash check failed for utxo %s,%d", txHash, vout)
			}
		} else {
			if !bytes.Equal(btcutil.Hash160(redeemScript), scriptHash) {
				return nil, fmt.Errorf("(output:non-segwit) script hash check failed for utxo %s,%d", txHash, vout)
			}
		}
	}

	scrAddrs := inputNfo.ScriptAddrs
	addresses := make([]string, scrAddrs.NumPK+scrAddrs.NumPKH)
	for i, addr := range append(scrAddrs.PkHashes, scrAddrs.PubKeys...) {
		addresses[i] = addr.String() // unconverted
	}

	// Coinbase transactions must mature before spending.
	if confs < int64(txio.maturity) {
		return nil, immatureTransactionError
	}

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
		spendSize: inputNfo.VBytes(),
	}, nil
}

// Get the value of the previous outpoint.
func (btc *Backend) prevOutputValue(txid string, vout int) (uint64, error) {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		return 0, fmt.Errorf("error decoding tx hash %s: %w", txid, err)
	}
	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return 0, err
	}
	if vout > len(verboseTx.Vout)-1 {
		return 0, fmt.Errorf("prevOutput: vout index out of range")
	}
	output := verboseTx.Vout[vout]
	return toSat(output.Value), nil
}

// Get the Tx. Transaction info is not cached, so every call will result in a
// GetRawTransactionVerbose RPC call.
func (btc *Backend) transaction(txHash *chainhash.Hash, verboseTx *btcjson.TxRawResult) (*Tx, error) {
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
		var err error
		blockHash, err = chainhash.NewHashFromStr(verboseTx.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("error decoding block hash %s for tx %s: %w", verboseTx.BlockHash, txHash, err)
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
	var sumIn, sumOut, valIn uint64
	var isCoinbase bool
	for vin, input := range verboseTx.Vin {
		isCoinbase = input.Coinbase != ""
		if isCoinbase {
			valIn = toSat(verboseTx.Vout[0].Value)
		} else {
			var err error
			valIn, err = btc.prevOutputValue(input.Txid, int(input.Vout))
			if err != nil {
				return nil, fmt.Errorf("error fetching previous output value for %s:%d: %w", txHash, vin, err)
			}
		}
		sumIn += valIn
		if input.Txid == "" {
			inputs = append(inputs, txIn{
				vout:  input.Vout,
				value: valIn,
			})
			continue
		}
		hash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, fmt.Errorf("error decoding previous tx hash %s for tx %s: %w", input.Txid, txHash, err)
		}
		inputs = append(inputs, txIn{
			prevTx: *hash,
			vout:   input.Vout,
			value:  valIn,
		})
	}

	outputs := make([]txOut, 0, len(verboseTx.Vout))
	for vout, output := range verboseTx.Vout {
		pkScript, err := hex.DecodeString(output.ScriptPubKey.Hex)
		if err != nil {
			return nil, fmt.Errorf("error decoding pubkey script from %s for transaction %d:%d: %w",
				output.ScriptPubKey.Hex, txHash, vout, err)
		}
		vOut := toSat(output.Value)
		sumOut += vOut
		outputs = append(outputs, txOut{
			value:    vOut,
			pkScript: pkScript,
		})
	}
	var feeRate uint64
	if verboseTx.Vsize > 0 {
		feeRate = (sumIn - sumOut) / uint64(verboseTx.Vsize)
	}
	return newTransaction(btc, txHash, blockHash, lastLookup, blockHeight, isCoinbase, inputs, outputs, feeRate), nil
}

// Get information for an unspent transaction output and it's transaction.
func (btc *Backend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*btcjson.GetTxOutResult, *btcjson.TxRawResult, []byte, error) {
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		var rpcErr *btcjson.RPCError
		if errors.As(err, &rpcErr) {
			if rpcErr.Code == btcjson.ErrRPCInvalidAddressOrKey {
				return nil, nil, nil, asset.CoinNotFoundError
			}
		}
		return nil, nil, nil, fmt.Errorf("GetTxOut error for output %s:%d: %w", txHash, vout, err)
	}
	if txOut == nil {
		return nil, nil, nil, asset.CoinNotFoundError
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode pubkey from '%s' for output %s:%d", txOut.ScriptPubKey.Hex, txHash, vout)
	}
	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txHash, err)
	}
	return txOut, verboseTx, pkScript, nil
}

// Get the block information, checking the cache first. Same as
// getBtcBlock, but takes a string argument.
func (btc *Backend) getBlockInfo(blockid string) (*cachedBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(blockid)
	if err != nil {
		return nil, fmt.Errorf("unable to decode block hash from %s", blockid)
	}
	return btc.getBtcBlock(blockHash)
}

// Get the block information, checking the cache first.
func (btc *Backend) getBtcBlock(blockHash *chainhash.Hash) (*cachedBlock, error) {
	cachedBlk, found := btc.blockCache.block(blockHash)
	if found {
		return cachedBlk, nil
	}
	blockVerbose, err := btc.node.GetBlockVerbose(blockHash)
	if err != nil {
		return nil, fmt.Errorf("error retrieving block %s: %w", blockHash, err)
	}
	return btc.blockCache.add(blockVerbose)
}

// auditContract checks that output is a swap contract and extracts the
// receiving address and contract value on success.
func (btc *Backend) auditContract(contract *Contract) error {
	tx := contract.tx
	if len(tx.outs) <= int(contract.vout) {
		return fmt.Errorf("invalid index %d for transaction %s", contract.vout, tx.hash)
	}
	output := tx.outs[int(contract.vout)]

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	scriptType := dexbtc.ParseScriptType(output.pkScript, contract.redeemScript)
	if scriptType == dexbtc.ScriptUnsupported {
		return fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, contract.vout)
	}
	var scriptHash, hashed []byte
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		scriptHash = dexbtc.ExtractScriptHash(output.pkScript)
		if scriptType.IsSegwit() {
			if !btc.segwit {
				return fmt.Errorf("segwit contract, but %s is not configured for segwit", btc.name)
			}
			shash := sha256.Sum256(contract.redeemScript)
			hashed = shash[:]
		} else {
			if btc.segwit {
				return fmt.Errorf("non-segwit contract, but %s is configured for segwit", btc.name)
			}
			hashed = btcutil.Hash160(contract.redeemScript)
		}
	}
	if scriptHash == nil {
		return fmt.Errorf("specified output %s:%d is not P2SH or P2WSH", tx.hash, contract.vout)
	}
	if !bytes.Equal(hashed, scriptHash) {
		return fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, contract.vout)
	}
	refund, receiver, lockTime, _, err := dexbtc.ExtractSwapDetails(contract.redeemScript, contract.btc.segwit, contract.btc.chainParams)
	if err != nil {
		return fmt.Errorf("error parsing swap contract for %s:%d: %w", tx.hash, contract.vout, err)
	}
	contract.refundAddress = refund.String()
	contract.swapAddress = receiver.String()
	contract.lockTime = time.Unix(int64(lockTime), 0)
	return nil
}

// run is responsible for best block polling and checking the application
// context to trigger a clean shutdown.
func (btc *Backend) run(ctx context.Context) {
	defer btc.shutdown()

	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()
	addBlock := func(block *btcjson.GetBlockVerboseResult, reorg bool) {
		_, err := btc.blockCache.add(block)
		if err != nil {
			btc.log.Errorf("error adding new best block to cache: %v", err)
		}
		btc.signalMtx.RLock()
		btc.log.Tracef("Notifying %d %s asset consumers of new block at height %d",
			len(btc.blockChans), btc.name, block.Height)
		for c := range btc.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err:   nil,
				Reorg: reorg,
			}:
			default:
				btc.log.Errorf("failed to send block update on blocking channel")
				// Commented to try sends on future blocks.
				// close(c)
				// delete(btc.blockChans, c)
				//
				// TODO: Allow the receiver (e.g. Swapper.Run) to inform done
				// status so the channels can be retired cleanly rather than
				// trying them forever.
			}
		}
		btc.signalMtx.RUnlock()
	}

	sendErr := func(err error) {
		btc.log.Error(err)
		for c := range btc.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Err: err,
			}:
			default:
				btc.log.Errorf("failed to send sending block update on blocking channel")
				// close(c)
				// delete(btc.blockChans, c)
			}
		}
	}

	sendErrFmt := func(s string, a ...interface{}) {
		sendErr(fmt.Errorf(s, a...))
	}

out:
	for {
		select {
		case <-blockPoll.C:
			tip := btc.blockCache.tip()
			bestHash, err := btc.node.GetBestBlockHash()
			if err != nil {
				sendErr(asset.NewConnectionError("error retrieving best block: %v", err))
				continue
			}
			if *bestHash == tip.hash {
				continue
			}
			best := bestHash.String()
			block, err := btc.node.GetBlockVerbose(bestHash)
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
				btc.log.Debugf("New block %s (%d)", bestHash, block.Height)
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
				iBlock, err := btc.node.GetBlockVerbose(iHash)
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
				btc.log.Infof("Reorg from %s (%d) to %s (%d) detected.",
					tip.hash, tip.height, bestHash, block.Height)
				btc.blockCache.reorg(reorgHeight)
			}
			// Now add the new block.
			addBlock(block, reorg)
		case <-ctx.Done():
			break out
		}
	}
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

// Convert the BTC value to satoshis.
func toSat(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *btcjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == btcjson.ErrRPCInvalidAddressOrKey
}
