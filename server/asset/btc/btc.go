// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	srvdex "decred.org/dcrdex/server/dex"
	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrjson/v4" // for dcrjson.RPCError returns from rpcclient
	"github.com/decred/dcrd/rpcclient/v7"
)

const defaultNoCompetitionRate = 10

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

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexbtc.UnitInfo
}

// NewAddresser creates an asset.Addresser for deriving addresses for the given
// extended public key. The KeyIndexer will be used for discovering the current
// child index, and storing the index as new addresses are generated with the
// NextAddress method of the Addresser.
func (d *Driver) NewAddresser(xPub string, keyIndexer asset.KeyIndexer, network dex.Network) (asset.Addresser, uint32, error) {
	params, err := netParams(network)
	if err != nil {
		return nil, 0, err
	}

	return NewAddressDeriver(xPub, keyIndexer, params)
}

func init() {
	asset.Register(BipID, &Driver{})

	if blockPollIntervalStr != "" {
		blockPollInterval, _ = time.ParseDuration(blockPollIntervalStr)
		if blockPollInterval < time.Second {
			panic(fmt.Sprintf("invalid value for blockPollIntervalStr: %q", blockPollIntervalStr))
		}
	}
}

var (
	zeroHash chainhash.Hash
	// The blockPollInterval is the delay between calls to GetBestBlockHash to
	// check for new blocks. Modify at compile time via blockPollIntervalStr:
	// go build -ldflags "-X 'decred.org/dcrdex/server/asset/btc.blockPollIntervalStr=4s'"
	blockPollInterval            = time.Second
	blockPollIntervalStr         string
	conventionalConversionFactor = float64(dexbtc.UnitInfo.Conventional.ConversionFactor)
	defaultMaxFeeBlocks          = 3
)

const (
	version                  = 0
	BipID                    = 0
	assetName                = "btc"
	immatureTransactionError = dex.ErrorKind("immature output")
	BondVersion              = 0
)

func netParams(network dex.Network) (*chaincfg.Params, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Simnet:
		params = &chaincfg.RegressionNetParams
	case dex.Testnet:
		params = &chaincfg.TestNet3Params
	case dex.Mainnet:
		params = &chaincfg.MainNetParams
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
	return params, nil
}

// Backend is a dex backend for Bitcoin or a Bitcoin clone. It has methods for
// fetching UTXO information and subscribing to block updates. It maintains a
// cache of block data for quick lookups. Backend implements asset.Backend, so
// provides exported methods for DEX-related blockchain info.
type Backend struct {
	rpcCfg *dexbtc.RPCConfig
	cfg    *BackendCloneConfig
	// The asset name (e.g. btc), primarily for logging purposes.
	name string
	// segwit should be set to true for blockchains that support segregated
	// witness.
	segwit bool
	// node is used throughout for RPC calls. For testing, it can be set to a stub.
	node *RPCClient
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
	log        dex.Logger
	decodeAddr dexbtc.AddressDecoder
	// booleanGetBlockRPC corresponds to BackendCloneConfig.BooleanGetBlockRPC
	// field and is used by RPCClient, which is constructed on Connect.
	booleanGetBlockRPC bool
	blockDeserializer  func([]byte) (*wire.MsgBlock, error)
	numericGetRawRPC   bool
	// fee estimation configuration
	feeConfs          int64
	noCompetitionRate uint64

	initTxSize     uint32
	initTxSizeBase uint32

	// The feeCache prevents repeated calculations of the median fee rate
	// between block changes when estimate(smart)fee is unprimed.
	feeCache struct {
		sync.Mutex
		fee  uint64
		hash chainhash.Hash
	}
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*Backend)(nil)
var _ srvdex.Bonder = (*Backend)(nil)

// NewBackend is the exported constructor by which the DEX will import the
// backend. The configPath can be an empty string, in which case the standard
// system location of the bitcoind config file is assumed.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	params, err := netParams(network)
	if err != nil {
		return nil, err
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("bitcoin")
	}

	return NewBTCClone(&BackendCloneConfig{
		Name:        assetName,
		Segwit:      true,
		ConfigPath:  configPath,
		Logger:      logger,
		Net:         network,
		ChainParams: params,
		Ports:       dexbtc.RPCPorts,
	})
}

func newBTC(cloneCfg *BackendCloneConfig, rpcCfg *dexbtc.RPCConfig) *Backend {
	addrDecoder := btcutil.DecodeAddress
	if cloneCfg.AddressDecoder != nil {
		addrDecoder = cloneCfg.AddressDecoder
	}

	noCompetitionRate := cloneCfg.NoCompetitionFeeRate
	if noCompetitionRate == 0 {
		noCompetitionRate = defaultNoCompetitionRate
	}

	feeConfs := cloneCfg.FeeConfs
	if feeConfs == 0 {
		feeConfs = 1
	}

	initTxSize := cloneCfg.InitTxSize
	if initTxSize == 0 {
		if cloneCfg.Segwit {
			initTxSize = dexbtc.InitTxSizeSegwit
		} else {
			initTxSize = dexbtc.InitTxSize
		}
	}

	initTxSizeBase := cloneCfg.InitTxSizeBase
	if initTxSizeBase == 0 {
		if cloneCfg.Segwit {
			initTxSizeBase = dexbtc.InitTxSizeBaseSegwit
		} else {
			initTxSizeBase = dexbtc.InitTxSizeBase
		}
	}

	return &Backend{
		rpcCfg:             rpcCfg,
		cfg:                cloneCfg,
		name:               cloneCfg.Name,
		blockCache:         newBlockCache(),
		blockChans:         make(map[chan *asset.BlockUpdate]struct{}),
		chainParams:        cloneCfg.ChainParams,
		log:                cloneCfg.Logger,
		segwit:             cloneCfg.Segwit,
		decodeAddr:         addrDecoder,
		noCompetitionRate:  noCompetitionRate,
		feeConfs:           feeConfs,
		booleanGetBlockRPC: cloneCfg.BooleanGetBlockRPC,
		blockDeserializer:  cloneCfg.BlockDeserializer,
		numericGetRawRPC:   cloneCfg.NumericGetRawRPC,
		initTxSize:         initTxSize,
		initTxSizeBase:     initTxSizeBase,
	}
}

// BackendCloneConfig captures the arguments necessary to configure a BTC clone
// backend.
type BackendCloneConfig struct {
	Name           string
	Segwit         bool
	ConfigPath     string
	AddressDecoder dexbtc.AddressDecoder
	Logger         dex.Logger
	Net            dex.Network
	ChainParams    *chaincfg.Params
	Ports          dexbtc.NetPorts
	// ManualFeeScan specifies that median block fees should be calculated by
	// scanning transactions since the getblockstats rpc is not available.
	// Median block fees are used to estimate fee rates when the cache is not
	// primed.
	ManualMedianFee bool
	// NoCompetitionFeeRate specifies a fee rate to use if estimatesmartfee
	// or estimatefee aren't ready and the median fee is finding relatively
	// empty blocks.
	NoCompetitionFeeRate uint64
	// DumbFeeEstimates is for asset's whose RPC is estimatefee instead of
	// estimatesmartfee.
	DumbFeeEstimates bool
	// Argsless fee estimates are for assets who don't take an argument for
	// number of blocks to estimatefee.
	ArglessFeeEstimates bool
	// FeeConfs specifies the target number of confirmations to use for
	// estimate(smart)fee. If not set, default value is 1,
	FeeConfs int64
	// MaxFeeBlocks is the maximum number of blocks that can be evaluated for
	// median fee calculations. If > 100 txs are not seen in the last
	// MaxFeeBlocks, then the NoCompetitionRate will be returned as the median
	// fee.
	MaxFeeBlocks int
	// BooleanGetBlockRPC will pass true instead of 2 as the getblock argument.
	BooleanGetBlockRPC bool
	// BlockDeserializer can be used in place of (*wire.MsgBlock).Deserialize.
	BlockDeserializer func(blk []byte) (*wire.MsgBlock, error)
	// TxDeserializer is an optional function used to deserialize a transaction.
	// TxDeserializer is only used if ManualMedianFee is true.
	TxDeserializer func([]byte) (*wire.MsgTx, error)
	// BlockFeeTransactions is a function to fetch a set of FeeTx and a previous
	// block hash for a specific block.
	BlockFeeTransactions BlockFeeTransactions
	// NumericGetRawRPC uses a numeric boolean indicator for the
	// getrawtransaction RPC.
	NumericGetRawRPC bool
	// ShieldedIO is a function to read a transaction and calculate the shielded
	// input and output amounts. This is a temporary measure until zcashd
	// encodes valueBalanceOrchard in their getrawtransaction RPC results.
	ShieldedIO     func(tx *VerboseTxExtended) (in, out uint64, err error)
	InitTxSize     uint32
	InitTxSizeBase uint32
}

// NewBTCClone creates a BTC backend for a set of network parameters and default
// network ports. A BTC clone can use this method, possibly in conjunction with
// ReadCloneParams, to create a Backend for other assets with minimal coding.
// See ReadCloneParams and CompatibilityCheck for more info.
func NewBTCClone(cloneCfg *BackendCloneConfig) (*Backend, error) {
	// Read the configuration parameters
	cfg := new(dexbtc.RPCConfig)
	err := config.ParseInto(cloneCfg.ConfigPath, cfg)
	if err != nil {
		return nil, err
	}
	err = dexbtc.CheckRPCConfig(cfg, cloneCfg.Name, cloneCfg.Net, cloneCfg.Ports)
	if err != nil {
		return nil, err
	}
	return newBTC(cloneCfg, cfg), nil
}

func (btc *Backend) shutdown() {
	if btc.node != nil {
		btc.node.requester.Shutdown()
		btc.node.requester.WaitForShutdown()
	}
}

// Connect connects to the node RPC server. A dex.Connector.
func (btc *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         btc.rpcCfg.RPCBind,
		User:         btc.rpcCfg.RPCUser,
		Pass:         btc.rpcCfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating %q RPC client: %w", btc.name, err)
	}

	maxFeeBlocks := btc.cfg.MaxFeeBlocks
	if maxFeeBlocks == 0 {
		maxFeeBlocks = defaultMaxFeeBlocks
	}

	txDeserializer := btc.cfg.TxDeserializer
	if txDeserializer == nil {
		txDeserializer = msgTxFromBytes
	}

	blockFeeTransactions := btc.cfg.BlockFeeTransactions
	if blockFeeTransactions == nil {
		blockFeeTransactions = btcBlockFeeTransactions
	}

	btc.node = &RPCClient{
		ctx:                  ctx,
		requester:            client,
		booleanGetBlockRPC:   btc.booleanGetBlockRPC,
		maxFeeBlocks:         maxFeeBlocks,
		arglessFeeEstimates:  btc.cfg.ArglessFeeEstimates,
		blockDeserializer:    btc.blockDeserializer,
		numericGetRawRPC:     btc.numericGetRawRPC,
		deserializeTx:        txDeserializer,
		blockFeeTransactions: blockFeeTransactions,
	}

	// Prime the cache
	bestHash, err := btc.node.GetBestBlockHash()
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

	txindex, err := btc.node.checkTxIndex()
	if err != nil {
		btc.log.Warnf(`Please ensure txindex is enabled in the node config and you might need to re-index if txindex was not previously enabled for %s`, btc.name)
		btc.shutdown()
		return nil, fmt.Errorf("error checking txindex for %s: %w", btc.name, err)
	}
	if !txindex {
		btc.shutdown()
		return nil, fmt.Errorf("%s transaction index is not enabled. Please enable txindex in the node config and you might need to re-index when you enable txindex", btc.name)
	}

	if _, err = btc.estimateFee(ctx); err != nil {
		btc.log.Warnf("Backend started without fee estimation available: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.run(ctx)
	}()
	return &wg, nil
}

// Net returns the *chaincfg.Params. This is not part of the asset.Backend
// interface, and is exported as a convenience for embedding types.
func (btc *Backend) Net() *chaincfg.Params {
	return btc.chainParams
}

// Contract is part of the asset.Backend interface. An asset.Contract is an
// output that has been validated as a swap contract for the passed redeem
// script. A spendable output is one that can be spent in the next block. Every
// output from a non-coinbase transaction is spendable immediately. Coinbase
// outputs are only spendable after CoinbaseMaturity confirmations. Pubkey
// scripts can be P2PKH or P2SH. Multi-sig P2SH redeem scripts are supported.
func (btc *Backend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	output, err := btc.output(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}
	// Verify contract and set refundAddress and swapAddress.
	return btc.auditContract(output)
}

// ValidateSecret checks that the secret satisfies the contract.
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
	chainInfo, err := btc.node.GetBlockChainInfo()
	if err != nil {
		return false, fmt.Errorf("GetBlockChainInfo error: %w", err)
	}
	return !chainInfo.InitialBlockDownload && chainInfo.Headers-chainInfo.Blocks <= 1, nil
}

// Redemption is an input that redeems a swap contract.
func (btc *Backend) Redemption(redemptionID, contractID, _ []byte) (asset.Coin, error) {
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

// ParseBondTx performs basic validation of a serialized time-locked fidelity
// bond transaction given the bond's P2SH or P2WSH redeem script.
//
// The transaction must have at least two outputs: out 0 pays to a P2SH address
// (the bond), and out 1 is a nulldata output that commits to an account ID.
// There may also be a change output.
//
// Returned: The bond's coin ID (i.e. encoded UTXO) of the bond output. The bond
// output's amount and P2SH/P2WSH address. The lockTime and pubkey hash data pushes
// from the script. The account ID from the second output is also returned.
//
// Properly formed transactions:
//
//  1. The bond output (vout 0) must be a P2SH/P2WSH output.
//  2. The bond's redeem script must be of the form:
//     <lockTime[4]> OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 <pubkeyhash[20]> OP_EQUALVERIFY OP_CHECKSIG
//  3. The null data output (vout 1) must have a 58-byte data push (ver | account ID | lockTime | pubkeyHash).
//  4. The transaction must have a zero locktime and expiry.
//  5. All inputs must have the max sequence num set (finalized).
//  6. The transaction must pass the checks in the
//     blockchain.CheckTransactionSanity function.
func ParseBondTx(ver uint16, rawTx []byte, chainParams *chaincfg.Params, segwit bool) (bondCoinID []byte, amt int64, bondAddr string,
	bondPubKeyHash []byte, lockTime int64, acct account.AccountID, err error) {
	if ver != BondVersion {
		err = errors.New("only version 0 bonds supported")
		return
	}
	msgTx := wire.NewMsgTx(wire.TxVersion)
	if err = msgTx.Deserialize(bytes.NewReader(rawTx)); err != nil {
		return
	}

	if msgTx.LockTime != 0 {
		err = errors.New("transaction locktime not zero")
		return
	}
	if err = blockchain.CheckTransactionSanity(btcutil.NewTx(msgTx)); err != nil {
		return
	}

	if len(msgTx.TxOut) < 2 {
		err = fmt.Errorf("expected at least 2 outputs, found %d", len(msgTx.TxOut))
		return
	}

	for _, txIn := range msgTx.TxIn {
		if txIn.Sequence != wire.MaxTxInSequenceNum {
			err = errors.New("input has non-max sequence number")
			return
		}
	}

	// Fidelity bond (output 0)
	bondOut := msgTx.TxOut[0]
	scriptHash := dexbtc.ExtractScriptHash(bondOut.PkScript)
	if scriptHash == nil {
		err = fmt.Errorf("bad bond pkScript")
		return
	}
	switch len(scriptHash) {
	case 32:
		if !segwit {
			err = fmt.Errorf("%s backend does not support segwit bonds", chainParams.Name)
			return
		}
	case 20:
		if segwit {
			err = fmt.Errorf("%s backend requires segwit bonds", chainParams.Name)
			return
		}
	default:
		err = fmt.Errorf("unexpected script hash length %d", len(scriptHash))
		return
	}

	acctCommitOut := msgTx.TxOut[1]
	acct, lock, pkh, err := dexbtc.ExtractBondCommitDataV0(0, acctCommitOut.PkScript)
	if err != nil {
		err = fmt.Errorf("invalid bond commitment output: %w", err)
		return
	}

	// Reconstruct and check the bond redeem script.
	bondScript, err := dexbtc.MakeBondScript(ver, lock, pkh[:])
	if err != nil {
		err = fmt.Errorf("failed to build bond output redeem script: %w", err)
		return
	}

	// Check that the script hash extracted from output 0 is what is expected
	// based on the information in the account commitment.
	// P2WSH uses sha256, while P2SH uses ripemd160(sha256).
	var expectedScriptHash []byte
	if len(scriptHash) == 32 {
		hash := sha256.Sum256(bondScript)
		expectedScriptHash = hash[:]
	} else {
		expectedScriptHash = btcutil.Hash160(bondScript)
	}
	if !bytes.Equal(expectedScriptHash, scriptHash) {
		err = fmt.Errorf("script hash check failed for output 0 of %s", msgTx.TxHash())
		return
	}

	_, addrs, _, err := dexbtc.ExtractScriptData(bondOut.PkScript, chainParams)
	if err != nil {
		err = fmt.Errorf("error extracting addresses from bond output: %w", err)
		return
	}

	txid := msgTx.TxHash()
	bondCoinID = toCoinID(&txid, 0)
	amt = bondOut.Value
	bondAddr = addrs[0] // don't convert address, must match type we specified
	lockTime = int64(lock)
	bondPubKeyHash = pkh[:]

	return
}

// BondVer returns the latest supported bond version.
func (dcr *Backend) BondVer() uint16 {
	return BondVersion
}

// ParseBondTx makes the package-level ParseBondTx pure function accessible via
// a Backend instance. This performs basic validation of a serialized
// time-locked fidelity bond transaction given the bond's P2SH redeem script.
func (btc *Backend) ParseBondTx(ver uint16, rawTx []byte) (bondCoinID []byte, amt int64, bondAddr string,
	bondPubKeyHash []byte, lockTime int64, acct account.AccountID, err error) {
	return ParseBondTx(ver, rawTx, btc.chainParams, btc.segwit)
}

// BondCoin locates a bond transaction output, validates the entire transaction,
// and returns the amount, encoded lockTime and account ID, and the
// confirmations of the transaction. It is a CoinNotFoundError if the
// transaction output is spent.
func (btc *Backend) BondCoin(ctx context.Context, ver uint16, coinID []byte) (amt, lockTime, confs int64, acct account.AccountID, err error) {
	txHash, vout, errCoin := decodeCoinID(coinID)
	if errCoin != nil {
		err = fmt.Errorf("error decoding coin ID %x: %w", coinID, errCoin)
		return
	}

	verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			err = asset.CoinNotFoundError
		}
		return
	}

	if int(vout) > len(verboseTx.Vout)-1 {
		err = fmt.Errorf("invalid output index for tx with %d outputs", len(verboseTx.Vout))
		return
	}

	confs = int64(verboseTx.Confirmations)

	rawTx, err := hex.DecodeString(verboseTx.Hex) // ParseBondTx will deserialize to msgTx, so just get the bytes
	if err != nil {
		err = fmt.Errorf("failed to decode transaction %s: %w", txHash, err)
		return
	}

	txOut, err := btc.node.GetTxOut(txHash, vout, true) // check regular tree first
	if err != nil {
		if isTxNotFoundErr(err) { // should be txOut==nil, but checking anyway
			err = asset.CoinNotFoundError
			return
		}
		return
	}
	if txOut == nil { // spent == invalid bond
		err = asset.CoinNotFoundError
		return
	}

	_, amt, _, _, lockTime, acct, err = ParseBondTx(ver, rawTx, btc.chainParams, btc.segwit)
	return
}

// FeeCoin gets the recipient address, value, and confirmations of a transaction
// output encoded by the given coinID. A non-nil error is returned if the
// output's pubkey script is not a P2WPKH requiring a single ECDSA-secp256k1
// signature.
func (btc *Backend) FeeCoin(coinID []byte) (addr string, val uint64, confs int64, err error) {
	txHash, vout, errCoin := decodeCoinID(coinID)
	if errCoin != nil {
		err = fmt.Errorf("error decoding coin ID %x: %w", coinID, errCoin)
		return
	}

	var txOut *txOutData
	txOut, confs, err = btc.outputSummary(txHash, vout)
	if err != nil {
		return
	}

	// AddressDeriver gives out p2wpkh addresses.
	if len(txOut.addresses) != 1 || !txOut.scriptType.IsP2WPKH() {
		return "", 0, -1, dex.UnsupportedScriptError
	}
	addr = txOut.addresses[0]
	val = txOut.value
	return
}

// txOutData is transaction output data, including recipient addresses, value,
// script type, and number of required signatures.
type txOutData struct {
	value        uint64
	addresses    []string
	sigsRequired int
	scriptType   dexbtc.BTCScriptType
}

// outputSummary gets transaction output data, including recipient addresses,
// value, script type, and number of required signatures, plus the current
// confirmations of a transaction output. If the output does not exist, an error
// will be returned. Non-standard scripts are not an error.
func (btc *Backend) outputSummary(txHash *chainhash.Hash, vout uint32) (txOut *txOutData, confs int64, err error) {
	var verboseTx *VerboseTxExtended
	verboseTx, err = btc.node.GetRawTransactionVerbose(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			err = asset.CoinNotFoundError
		}
		return
	}

	if int(vout) > len(verboseTx.Vout)-1 {
		err = asset.CoinNotFoundError // should be something fatal?
		return
	}

	out := verboseTx.Vout[vout]

	script, err := hex.DecodeString(out.ScriptPubKey.Hex)
	if err != nil {
		return nil, -1, dex.UnsupportedScriptError
	}
	scriptType, addrs, numRequired, err := dexbtc.ExtractScriptData(script, btc.chainParams)
	if err != nil {
		return nil, -1, dex.UnsupportedScriptError
	}

	txOut = &txOutData{
		value:        toSat(out.Value),
		addresses:    addrs,       // out.ScriptPubKey.Addresses
		sigsRequired: numRequired, // out.ScriptPubKey.ReqSigs
		scriptType:   scriptType,  // integer representation of the string in out.ScriptPubKey.Type
	}

	confs = int64(verboseTx.Confirmations)
	return
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
	return btc.initTxSize
}

// InitTxSizeBase is InitTxSize not including an input.
func (btc *Backend) InitTxSizeBase() uint32 {
	return btc.initTxSizeBase
}

// FeeRate returns the current optimal fee rate in sat / byte.
func (btc *Backend) FeeRate(ctx context.Context) (uint64, error) {
	return btc.estimateFee(ctx)
}

// Info provides some general information about the backend.
func (*Backend) Info() *asset.BackendInfo {
	return &asset.BackendInfo{}
}

// ValidateFeeRate checks that the transaction fees used to initiate the
// contract are sufficient.
func (*Backend) ValidateFeeRate(contract *asset.Contract, reqFeeRate uint64) bool {
	return contract.FeeRate() >= reqFeeRate
}

// CheckSwapAddress checks that the given address is parseable, and suitable as
// a redeem address in a swap contract script.
func (btc *Backend) CheckSwapAddress(addr string) bool {
	btcAddr, err := btc.decodeAddr(addr, btc.chainParams)
	if err != nil {
		btc.log.Errorf("CheckSwapAddress for %s failed: %v", addr, err)
		return false
	}
	if btc.segwit {
		if _, ok := btcAddr.(*btcutil.AddressWitnessPubKeyHash); !ok {
			btc.log.Errorf("CheckSwapAddress for %s failed: not a witness-pubkey-hash address (%T)",
				btcAddr.String(), btcAddr)
			return false
		}
	} else {
		if _, ok := btcAddr.(*btcutil.AddressPubKeyHash); !ok {
			btc.log.Errorf("CheckSwapAddress for %s failed: not a pubkey-hash address (%T)",
				btcAddr.String(), btcAddr)
			return false
		}
	}
	return true
}

// TxData is the raw transaction bytes. SPV clients rebroadcast the transaction
// bytes to get around not having a mempool to check.
func (btc *Backend) TxData(coinID []byte) ([]byte, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	txB, err := btc.node.GetRawTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("GetRawTransaction for txid %s: %w", txHash, err)
	}
	return txB, nil
}

// blockInfo returns block information for the verbose transaction data. The
// current tip hash is also returned as a convenience.
func (btc *Backend) blockInfo(verboseTx *VerboseTxExtended) (blockHeight uint32, blockHash chainhash.Hash, tipHash *chainhash.Hash, err error) {
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
func (btc *Backend) transaction(txHash *chainhash.Hash, verboseTx *VerboseTxExtended) (*Tx, error) {
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

	// sumIn, sumOut := verboseTx.ShieldedIO()
	var sumIn, sumOut uint64
	if btc.cfg.ShieldedIO != nil {
		var err error
		sumIn, sumOut, err = btc.cfg.ShieldedIO(verboseTx)
		if err != nil {
			return nil, fmt.Errorf("ShieldedIO error: %w", err)
		}
	}

	var isCoinbase bool
	for vin, input := range verboseTx.Vin {
		isCoinbase = input.Coinbase != ""
		var valIn uint64
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
	rawTx, err := hex.DecodeString(verboseTx.Hex)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx hex: %w", err)
	}

	var feeRate uint64
	if btc.segwit {
		if verboseTx.Vsize > 0 {
			feeRate = (sumIn - sumOut) / uint64(verboseTx.Vsize)
		}
	} else if verboseTx.Size > 0 {
		// For non-segwit transactions, Size = Vsize anyway, so use Size to
		// cover assets that won't set Vsize in their RPC response.
		feeRate = (sumIn - sumOut) / uint64(verboseTx.Size)

	}
	return newTransaction(btc, txHash, blockHash, lastLookup, blockHeight, isCoinbase, inputs, outputs, feeRate, rawTx), nil
}

// Get information for an unspent transaction output and it's transaction.
func (btc *Backend) getTxOutInfo(txHash *chainhash.Hash, vout uint32) (*btcjson.GetTxOutResult, *VerboseTxExtended, []byte, error) {
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		if isTxNotFoundErr(err) { // should be txOut==nil, but checking anyway
			return nil, nil, nil, asset.CoinNotFoundError
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
		if isTxNotFoundErr(err) {
			return nil, nil, nil, asset.CoinNotFoundError // shouldn't happen if gettxout found it
		}
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
func (btc *Backend) auditContract(contract *Output) (*asset.Contract, error) {
	tx := contract.tx
	if len(tx.outs) <= int(contract.vout) {
		return nil, fmt.Errorf("invalid index %d for transaction %s", contract.vout, tx.hash)
	}
	output := tx.outs[int(contract.vout)]

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	scriptType := dexbtc.ParseScriptType(output.pkScript, contract.redeemScript)
	if scriptType == dexbtc.ScriptUnsupported {
		return nil, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, contract.vout)
	}
	var scriptHash, hashed []byte
	if scriptType.IsP2SH() || scriptType.IsP2WSH() {
		scriptHash = dexbtc.ExtractScriptHash(output.pkScript)
		if scriptType.IsSegwit() {
			if !btc.segwit {
				return nil, fmt.Errorf("segwit contract, but %s is not configured for segwit", btc.name)
			}
			shash := sha256.Sum256(contract.redeemScript)
			hashed = shash[:]
		} else {
			if btc.segwit {
				return nil, fmt.Errorf("non-segwit contract, but %s is configured for segwit", btc.name)
			}
			hashed = btcutil.Hash160(contract.redeemScript)
		}
	}
	if scriptHash == nil {
		return nil, fmt.Errorf("specified output %s:%d is not P2SH or P2WSH", tx.hash, contract.vout)
	}
	if !bytes.Equal(hashed, scriptHash) {
		return nil, fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, contract.vout)
	}
	_, receiver, lockTime, secretHash, err := dexbtc.ExtractSwapDetails(contract.redeemScript, contract.btc.segwit, contract.btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing swap contract for %s:%d: %w", tx.hash, contract.vout, err)
	}
	return &asset.Contract{
		Coin:         contract,
		SwapAddress:  receiver.String(),
		ContractData: contract.redeemScript,
		SecretHash:   secretHash,
		LockTime:     time.Unix(int64(lockTime), 0),
		TxData:       contract.tx.raw,
	}, nil
}

// run is responsible for best block polling and checking the application
// context to trigger a clean shutdown.
func (btc *Backend) run(ctx context.Context) {
	defer btc.shutdown()

	btc.log.Infof("Starting %v block polling with interval of %v",
		strings.ToUpper(btc.name), blockPollInterval)
	blockPoll := time.NewTicker(blockPollInterval)
	defer blockPoll.Stop()
	addBlock := func(block *GetBlockVerboseResult, reorg bool) {
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

// estimateFee attempts to get a reasonable tx fee rates (units: atomic/(v)byte)
// to use for the asset by checking estimate(smart)fee. That call can fail or
// otherwise be useless on an otherwise perfectly functioning node. In that
// case, an estimate is calculated from the median fees of the previous
// block(s).
func (btc *Backend) estimateFee(ctx context.Context) (satsPerB uint64, err error) {
	if btc.cfg.DumbFeeEstimates {
		satsPerB, err = btc.node.EstimateFee(btc.feeConfs)
	} else {
		satsPerB, err = btc.node.EstimateSmartFee(btc.feeConfs, &btcjson.EstimateModeConservative)
	}
	if err == nil && satsPerB > 0 {
		return satsPerB, nil
	} else if err != nil && !errors.Is(err, errNoFeeRate) {
		btc.log.Debugf("Estimate fee failure: %v", err)
	}
	btc.log.Debugf("No fee estimate from node. Computing median fee rate from blocks...")

	tip := btc.blockCache.tipHash()

	btc.feeCache.Lock()
	defer btc.feeCache.Unlock()

	// If the current block hasn't changed, no need to recalc.
	if btc.feeCache.hash == tip {
		return btc.feeCache.fee, nil
	}

	// Need to revert to the median fee calculation.
	if btc.cfg.ManualMedianFee {
		satsPerB, err = btc.node.medianFeesTheHardWay(ctx)
	} else {
		satsPerB, err = btc.node.medianFeeRate()
	}
	if err != nil {
		if errors.Is(err, errNoCompetition) {
			btc.log.Debugf("Blocks are too empty to calculate median fees. "+
				"Using no-competition rate (%d).", btc.noCompetitionRate)
			btc.feeCache.fee = btc.noCompetitionRate
			btc.feeCache.hash = tip
			return btc.noCompetitionRate, nil
		}
		return 0, err
	}
	if satsPerB < btc.noCompetitionRate {
		btc.log.Debugf("Calculated median fees %d are lower than the no-competition rate %d. Using the latter.",
			satsPerB, btc.noCompetitionRate)
		satsPerB = btc.noCompetitionRate
	}
	btc.feeCache.fee = satsPerB
	btc.feeCache.hash = tip
	return satsPerB, nil
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
	return uint64(math.Round(v * conventionalConversionFactor))
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	// We are using dcrd's client with Bitcoin Core, so errors will be of type
	// dcrjson.RPCError, but numeric codes should come from btcjson.
	const errRPCNoTxInfo = int(btcjson.ErrRPCNoTxInfo)
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && int(rpcErr.Code) == errRPCNoTxInfo
}

// isMethodNotFoundErr will return true if the error indicates that the RPC
// method was not found by the RPC server. The error must be dcrjson.RPCError
// with a numeric code equal to btcjson.ErrRPCMethodNotFound.Code or a message
// containing "method not found".
func isMethodNotFoundErr(err error) bool {
	var errRPCMethodNotFound = int(btcjson.ErrRPCMethodNotFound.Code)
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) &&
		(int(rpcErr.Code) == errRPCMethodNotFound ||
			strings.Contains(strings.ToLower(rpcErr.Message), "method not found"))
}

// serializeMsgTx serializes the wire.MsgTx.
func serializeMsgTx(msgTx *wire.MsgTx) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, msgTx.SerializeSize()))
	err := msgTx.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// msgTxFromBytes creates a wire.MsgTx by deserializing the transaction.
func msgTxFromBytes(txB []byte) (*wire.MsgTx, error) {
	msgTx := new(wire.MsgTx)
	if err := msgTx.Deserialize(bytes.NewReader(txB)); err != nil {
		return nil, err
	}
	return msgTx, nil
}
