// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

// Driver implements asset.Driver.
type Driver struct{}

var _ asset.Driver = (*Driver)(nil)

// Setup creates the DCR backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	// With a websocket RPC client with auto-reconnect, setup a logging
	// subsystem for the rpcclient.
	logger = logger.SubLogger("RPC")
	if logger.Level() == dex.LevelTrace {
		logger.SetLevel(dex.LevelDebug)
	}
	rpcclient.UseLogger(logger)
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

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexdcr.UnitInfo
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// NewAddresser creates an asset.Addresser for deriving addresses for the given
// extended public key. The KeyIndexer will be used for discovering the current
// child index, and storing the index as new addresses are generated with the
// NextAddress method of the Addresser. The Backend must have been created with
// NewBackend (or Setup) to initialize the chain parameters.
func (d *Driver) NewAddresser(xPub string, keyIndexer asset.KeyIndexer, network dex.Network) (asset.Addresser, uint32, error) {
	var params NetParams
	switch network {
	case dex.Simnet:
		params = chaincfg.SimNetParams()
	case dex.Testnet:
		params = chaincfg.TestNet3Params()
	case dex.Mainnet:
		params = chaincfg.MainNetParams()
	default:
		return nil, 0, fmt.Errorf("unknown network ID: %d", uint8(network))
	}

	return NewAddressDeriver(xPub, keyIndexer, params)
}

func init() {
	asset.Register(BipID, &Driver{})
}

var (
	zeroHash chainhash.Hash
	// The blockPollInterval is the delay between calls to GetBestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second

	compatibleNodeRPCVersions = []dex.Semver{
		{Major: 8, Minor: 0, Patch: 0}, // 1.8-pre, just dropped unused ticket RPCs
		{Major: 7, Minor: 0, Patch: 0}, // 1.7 release, new gettxout args
	}

	conventionalConversionFactor = float64(dexdcr.UnitInfo.Conventional.ConversionFactor)
)

const (
	version                  = 0
	BipID                    = 42
	assetName                = "dcr"
	immatureTransactionError = dex.ErrorKind("immature output")
	BondVersion              = 0
)

// dcrNode represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient.Client, and all methods are matches for Client
// methods. For testing, it can be satisfied by a stub.
type dcrNode interface {
	EstimateSmartFee(ctx context.Context, confirmations int64, mode chainjson.EstimateSmartFeeMode) (*chainjson.EstimateSmartFeeResult, error)
	GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error)
	GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash(ctx context.Context) (*chainhash.Hash, error)
	GetBlockChainInfo(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error)
	GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error)
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
}

// The rpcclient package functions will return a rpcclient.ErrRequestCanceled
// error if the context is canceled. Translate these to asset.ErrRequestTimeout.
func translateRPCCancelErr(err error) error {
	if errors.Is(err, rpcclient.ErrRequestCanceled) {
		err = asset.ErrRequestTimeout
	}
	return err
}

// ParseBondTx performs basic validation of a serialized time-locked fidelity
// bond transaction given the bond's P2SH redeem script.
//
// The transaction must have at least two outputs: out 0 pays to a P2SH address
// (the bond), and out 1 is a nulldata output that commits to an account ID.
// There may also be a change output.
//
// Returned: The bond's coin ID (i.e. encoded UTXO) of the bond output. The bond
// output's amount and P2SH address. The lockTime and pubkey hash data pushes
// from the script. The account ID from the second output is also returned.
//
// Properly formed transactions:
//
//  1. The bond output (vout 0) must be a P2SH output.
//  2. The bond's redeem script must be of the form:
//     <lockTime[4]> OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 <pubkeyhash[20]> OP_EQUALVERIFY OP_CHECKSIG
//  3. The null data output (vout 1) must have a 58-byte data push (ver | account ID | lockTime | pubkeyHash).
//  4. The transaction must have a zero locktime and expiry.
//  5. All inputs must have the max sequence num set (finalized).
//  6. The transaction must pass the checks in the
//     blockchain.CheckTransactionSanity function.
//
// For DCR, and possibly all assets, the bond script is reconstructed from the
// null data output, and it is verified that the bond output pays to this
// script.
func ParseBondTx(ver uint16, rawTx []byte) (bondCoinID []byte, amt int64, bondAddr string,
	bondPubKeyHash []byte, lockTime int64, acct account.AccountID, err error) {
	if ver != BondVersion {
		err = errors.New("only version 0 bonds supported")
		return
	}
	// While the dcr package uses a package-level chainParams variable, ensure
	// that a backend has been instantiated first. Alternatively, we can add a
	// dex.Network argument to this function, or make it a Backend method.
	if chainParams == nil {
		err = errors.New("dcr asset package config not yet loaded")
		return
	}
	msgTx := wire.NewMsgTx()
	if err = msgTx.Deserialize(bytes.NewReader(rawTx)); err != nil {
		return
	}

	if msgTx.LockTime != 0 {
		err = errors.New("transaction locktime not zero")
		return
	}
	if msgTx.Expiry != wire.NoExpiryValue {
		err = errors.New("transaction has an expiration")
		return
	}

	if err = standalone.CheckTransactionSanity(msgTx, uint64(chainParams.MaxTxSize)); err != nil {
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
	class, addrs := stdscript.ExtractAddrs(bondOut.Version, bondOut.PkScript, chainParams)
	if class != stdscript.STScriptHash || len(addrs) != 1 { // addrs check is redundant for p2sh
		err = fmt.Errorf("bad bond pkScript (class = %v)", class)
		return
	}
	scriptHash := txscript.ExtractScriptHash(bondOut.PkScript)

	// Bond account commitment (output 1)
	acctCommitOut := msgTx.TxOut[1]
	acct, lock, pkh, err := dexdcr.ExtractBondCommitDataV0(acctCommitOut.Version, acctCommitOut.PkScript)
	if err != nil {
		err = fmt.Errorf("invalid bond commitment output: %w", err)
		return
	}

	// Reconstruct and check the bond redeem script.
	bondScript, err := dexdcr.MakeBondScript(ver, lock, pkh[:])
	if err != nil {
		err = fmt.Errorf("failed to build bond output redeem script: %w", err)
		return
	}
	if !bytes.Equal(dcrutil.Hash160(bondScript), scriptHash) {
		err = fmt.Errorf("script hash check failed for output 0 of %s", msgTx.TxHash())
		return
	}
	// lock, pkh, _ := dexdcr.ExtractBondDetailsV0(bondOut.Version, bondScript)

	txid := msgTx.TxHash()
	bondCoinID = toCoinID(&txid, 0)
	amt = bondOut.Value
	bondAddr = addrs[0].String() // don't convert address, must match type we specified
	lockTime = int64(lock)
	bondPubKeyHash = pkh[:]

	return
}

// Backend is an asset backend for Decred. It has methods for fetching output
// information and subscribing to block updates. It maintains a cache of block
// data for quick lookups. Backend implements asset.Backend, so provides
// exported methods for DEX-related blockchain info.
type Backend struct {
	ctx context.Context
	cfg *config
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

// unconnectedDCR returns a Backend without a node. The node should be set
// before use.
func unconnectedDCR(logger dex.Logger, cfg *config) *Backend {
	return &Backend{
		cfg:        cfg,
		blockCache: newBlockCache(logger),
		log:        logger,
		blockChans: make(map[chan *asset.BlockUpdate]struct{}),
	}
}

// NewBackend is the exported constructor by which the DEX will import the
// Backend. If configPath is an empty string, the backend will attempt to read
// the settings directly from the dcrd config file in its default system
// location.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (*Backend, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// package variable.
	cfg, err := loadConfig(configPath, network)
	if err != nil {
		return nil, err
	}
	return unconnectedDCR(logger, cfg), nil
}

func (dcr *Backend) shutdown() {
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// Connect connects to the node RPC server. A dex.Connector.
func (dcr *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	client, err := connectNodeRPC(dcr.cfg.RPCListen, dcr.cfg.RPCUser, dcr.cfg.RPCPass, dcr.cfg.RPCCert)
	if err != nil {
		return nil, err
	}
	dcr.client = client

	// Ensure the network of the connected node is correct for the expected
	// dex.Network.
	net, err := dcr.client.GetCurrentNet(ctx)
	if err != nil {
		dcr.shutdown()
		return nil, fmt.Errorf("getcurrentnet failure: %w", err)
	}
	var wantCurrencyNet wire.CurrencyNet
	switch dcr.cfg.Network {
	case dex.Testnet:
		wantCurrencyNet = wire.TestNet3
	case dex.Mainnet:
		wantCurrencyNet = wire.MainNet
	case dex.Regtest: // dex.Simnet
		wantCurrencyNet = wire.SimNet
	}
	if net != wantCurrencyNet {
		dcr.shutdown()
		return nil, fmt.Errorf("wrong net %v", net.String())
	}

	// Check the required API versions.
	versions, err := dcr.client.Version(ctx)
	if err != nil {
		dcr.shutdown()
		return nil, fmt.Errorf("DCR node version fetch error: %w", err)
	}

	ver, exists := versions["dcrdjsonrpcapi"]
	if !exists {
		dcr.shutdown()
		return nil, fmt.Errorf("dcrd.Version response missing 'dcrdjsonrpcapi'")
	}
	nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatibleAny(compatibleNodeRPCVersions, nodeSemver) {
		dcr.shutdown()
		return nil, fmt.Errorf("dcrd has an incompatible JSON-RPC version %s, require one of %s",
			nodeSemver, compatibleNodeRPCVersions)
	}

	// Verify dcrd has tx index enabled (required for getrawtransaction).
	info, err := dcr.client.GetInfo(ctx)
	if err != nil {
		dcr.shutdown()
		return nil, fmt.Errorf("dcrd getinfo check failed: %w", err)
	}
	if !info.TxIndex {
		dcr.shutdown()
		return nil, errors.New("dcrd does not have transaction index enabled (specify --txindex)")
	}

	dcr.log.Infof("Connected to dcrd (JSON-RPC API v%s) on %v", nodeSemver, net)

	dcr.node = dcr.client
	dcr.ctx = ctx

	// Prime the cache with the best block.
	bestHash, _, err := dcr.client.GetBestBlock(ctx)
	if err != nil {
		dcr.shutdown()
		return nil, fmt.Errorf("error getting best block from dcrd: %w", err)
	}
	if bestHash != nil {
		_, err := dcr.getDcrBlock(ctx, bestHash)
		if err != nil {
			dcr.shutdown()
			return nil, fmt.Errorf("error priming the cache: %w", err)
		}
	}

	if _, err = dcr.FeeRate(ctx); err != nil {
		dcr.log.Warnf("Decred backend started without fee estimation available: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dcr.run(ctx)
	}()
	return &wg, nil
}

// InitTxSize is an asset.Backend method that must produce the max size of a
// standardized atomic swap initialization transaction.
func (dcr *Backend) InitTxSize() uint32 {
	return dexdcr.InitTxSize
}

// InitTxSizeBase is InitTxSize not including an input.
func (dcr *Backend) InitTxSizeBase() uint32 {
	return dexdcr.InitTxSizeBase
}

// FeeRate returns the current optimal fee rate in atoms / byte.
func (dcr *Backend) FeeRate(ctx context.Context) (uint64, error) {
	// estimatesmartfee 1 returns extremely high rates on DCR.
	estimateFeeResult, err := dcr.node.EstimateSmartFee(ctx, 2, chainjson.EstimateSmartFeeConservative)
	if err != nil {
		return 0, translateRPCCancelErr(err)
	}
	atomsPerKB, err := dcrutil.NewAmount(estimateFeeResult.FeeRate)
	if err != nil {
		return 0, err
	}
	atomsPerB := uint64(math.Round(float64(atomsPerKB) / 1000))
	if atomsPerB == 0 {
		atomsPerB = 1
	}
	return atomsPerB, nil
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

// SendRawTransaction broadcasts a raw transaction, returning a coin ID.
func (dcr *Backend) SendRawTransaction(rawtx []byte) (coinID []byte, err error) {
	msgTx := wire.NewMsgTx()
	if err = msgTx.Deserialize(bytes.NewReader(rawtx)); err != nil {
		return nil, err
	}

	var hash *chainhash.Hash
	hash, err = dcr.node.SendRawTransaction(dcr.ctx, msgTx, false) // or allow high fees?
	if err != nil {
		return
	}
	coinID = toCoinID(hash, 0)
	return
}

// Contract is part of the asset.Backend interface. An asset.Contract is an
// output that has been validated as a swap contract for the passed redeem
// script. A spendable output is one that can be spent in the next block. Every
// regular-tree output from a non-coinbase transaction is spendable immediately.
// Coinbase and stake tree outputs are only spendable after CoinbaseMaturity
// confirmations. Pubkey scripts can be P2PKH or P2SH in either regular- or
// stake-tree flavor. P2PKH supports two alternative signatures, Schnorr and
// Edwards. Multi-sig P2SH redeem scripts are supported as well.
func (dcr *Backend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}

	op, err := dcr.output(txHash, vout, redeemScript)
	if err != nil {
		return nil, err
	}

	return auditContract(op)
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

// Synced is true if the blockchain is ready for action.
func (dcr *Backend) Synced() (bool, error) {
	// With ws autoreconnect enabled, requests hang when backend is
	// disconnected.
	ctx, cancel := context.WithTimeout(dcr.ctx, 2*time.Second)
	defer cancel()
	chainInfo, err := dcr.node.GetBlockChainInfo(ctx)
	if err != nil {
		return false, fmt.Errorf("GetBlockChainInfo error: %w", translateRPCCancelErr(err))
	}
	return !chainInfo.InitialBlockDownload && chainInfo.Headers-chainInfo.Blocks <= 1, nil
}

// Redemption is an input that redeems a swap contract.
func (dcr *Backend) Redemption(redemptionID, contractID, _ []byte) (asset.Coin, error) {
	txHash, vin, err := decodeCoinID(redemptionID)
	if err != nil {
		return nil, fmt.Errorf("error decoding redemption coin ID %x: %w", txHash, err)
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
func (dcr *Backend) FundingCoin(ctx context.Context, coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	utxo, err := dcr.utxo(ctx, txHash, vout, redeemScript)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, err
	}
	if utxo.nonStandardScript {
		return nil, fmt.Errorf("non-standard script")
	}
	return utxo, nil
}

// ValidateXPub validates the base-58 encoded extended key, and ensures that it
// is an extended public, not private, key.
func ValidateXPub(xpub string) error {
	xp, err := hdkeychain.NewKeyFromString(xpub, chainParams)
	if err != nil {
		return err
	}
	if xp.IsPrivate() {
		xp.Zero()
		return fmt.Errorf("extended key is a private key")
	}
	return nil
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

// CheckSwapAddress checks that the given address is parseable and of the
// required type for a swap contract script (p2pkh).
func (dcr *Backend) CheckSwapAddress(addr string) bool {
	dcrAddr, err := stdaddr.DecodeAddress(addr, chainParams)
	if err != nil {
		dcr.log.Errorf("DecodeAddress error for %s: %v", addr, err)
		return false
	}
	if _, ok := dcrAddr.(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0); !ok {
		dcr.log.Errorf("CheckSwapAddress for %s failed: not a pubkey-hash-ecdsa-secp256k1 address (%T)",
			dcrAddr.String(), dcrAddr)
		return false
	}
	return true
}

// TxData is the raw transaction bytes. SPV clients rebroadcast the transaction
// bytes to get around not having a mempool to check.
func (dcr *Backend) TxData(coinID []byte) ([]byte, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	stdaddrTx, err := dcr.node.GetRawTransaction(dcr.ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("TxData: GetRawTransactionVerbose for txid %s: %w", txHash, err)
	}
	return stdaddrTx.MsgTx().Bytes()
}

// VerifyUnspentCoin attempts to verify a coin ID by decoding the coin ID and
// retrieving the corresponding UTXO. If the coin is not found or no longer
// unspent, an asset.CoinNotFoundError is returned.
func (dcr *Backend) VerifyUnspentCoin(ctx context.Context, coinID []byte) error {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	txOut, err := dcr.node.GetTxOut(ctx, txHash, vout, wire.TxTreeRegular, true) // check regular tree first
	if err == nil && txOut == nil {
		txOut, err = dcr.node.GetTxOut(ctx, txHash, vout, wire.TxTreeStake, true) // check stake tree
	}
	if err != nil {
		return fmt.Errorf("GetTxOut (%s:%d): %w", txHash.String(), vout, translateRPCCancelErr(err))
	}
	if txOut == nil {
		return asset.CoinNotFoundError
	}
	return nil
}

// BondVer returns the latest supported bond version.
func (dcr *Backend) BondVer() uint16 {
	return BondVersion
}

// ParseBondTx makes the package-level ParseBondTx pure function accessible via
// a Backend instance. This performs basic validation of a serialized
// time-locked fidelity bond transaction given the bond's P2SH redeem script.
func (*Backend) ParseBondTx(ver uint16, rawTx []byte) (bondCoinID []byte, amt int64, bondAddr string,
	bondPubKeyHash []byte, lockTime int64, acct account.AccountID, err error) {
	return ParseBondTx(ver, rawTx)
}

// BondCoin locates a bond transaction output, validates the entire transaction,
// and returns the amount, encoded lockTime and account ID, and the
// confirmations of the transaction. It is a CoinNotFoundError if the
// transaction output is spent.
func (dcr *Backend) BondCoin(ctx context.Context, ver uint16, coinID []byte) (amt, lockTime, confs int64, acct account.AccountID, err error) {
	txHash, vout, errCoin := decodeCoinID(coinID)
	if errCoin != nil {
		err = fmt.Errorf("error decoding coin ID %x: %w", coinID, errCoin)
		return
	}

	verboseTx, err := dcr.node.GetRawTransactionVerbose(dcr.ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			err = asset.CoinNotFoundError
		} else {
			err = translateRPCCancelErr(err)
		}
		return
	}

	if int(vout) > len(verboseTx.Vout)-1 {
		err = fmt.Errorf("invalid output index for tx with %d outputs", len(verboseTx.Vout))
		return
	}

	confs = verboseTx.Confirmations

	// msgTx, err := msgTxFromHex(verboseTx.Hex)
	rawTx, err := hex.DecodeString(verboseTx.Hex) // ParseBondTx will deserialize to msgTx, so just get the bytes
	if err != nil {
		err = fmt.Errorf("failed to decode transaction %s: %w", txHash, err)
		return
	}
	// rawTx, _ := msgTx.Bytes()

	// tree := determineTxTree(msgTx)
	txOut, err := dcr.node.GetTxOut(ctx, txHash, vout, wire.TxTreeRegular, true) // check regular tree first
	if err == nil && txOut == nil {
		txOut, err = dcr.node.GetTxOut(ctx, txHash, vout, wire.TxTreeStake, true) // check stake tree
	}
	if err != nil {
		err = fmt.Errorf("GetTxOut error for output %s:%d: %w",
			txHash, vout, translateRPCCancelErr(err))
		return
	}
	if txOut == nil { // spent == invalid bond
		err = asset.CoinNotFoundError
		return
	}

	_, amt, _, _, lockTime, acct, err = ParseBondTx(ver, rawTx)
	return
}

// FeeCoin gets the recipient address, value, and confirmations of a transaction
// output encoded by the given coinID. A non-nil error is returned if the
// output's pubkey script is not a non-stake P2PKH requiring a single
// ECDSA-secp256k1 signature.
func (dcr *Backend) FeeCoin(coinID []byte) (addr string, val uint64, confs int64, err error) {
	txHash, vout, errCoin := decodeCoinID(coinID)
	if errCoin != nil {
		err = fmt.Errorf("error decoding coin ID %x: %w", coinID, errCoin)
		return
	}

	var txOut *txOutData
	txOut, confs, err = dcr.outputSummary(txHash, vout)
	if err != nil {
		return
	}

	// No stake outputs, and no multisig.
	if len(txOut.addresses) != 1 || txOut.sigsRequired != 1 ||
		txOut.scriptType&dexdcr.ScriptStake != 0 {
		return "", 0, -1, dex.UnsupportedScriptError
	}

	// Needs to work for legacy fee and new bond txns.
	switch txOut.scriptType {
	case dexdcr.ScriptP2SH, dexdcr.ScriptP2PKH:
	default:
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
	scriptType   dexdcr.ScriptType
}

// outputSummary gets transaction output data, including recipient addresses,
// value, script type, and number of required signatures, plus the current
// confirmations of a transaction output. If the output does not exist, an error
// will be returned. Non-standard scripts are not an error.
func (dcr *Backend) outputSummary(txHash *chainhash.Hash, vout uint32) (txOut *txOutData, confs int64, err error) {
	var verboseTx *chainjson.TxRawResult
	verboseTx, err = dcr.node.GetRawTransactionVerbose(dcr.ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			err = asset.CoinNotFoundError
		} else {
			err = translateRPCCancelErr(err)
		}
		return
	}

	if int(vout) > len(verboseTx.Vout)-1 {
		err = fmt.Errorf("invalid output index for tx with %d outputs", len(verboseTx.Vout))
		return
	}

	out := verboseTx.Vout[vout]

	script, err := hex.DecodeString(out.ScriptPubKey.Hex)
	if err != nil {
		return nil, -1, dex.UnsupportedScriptError
	}
	scriptType, addrs, numRequired := dexdcr.ExtractScriptData(out.ScriptPubKey.Version, script, chainParams)
	txOut = &txOutData{
		value:        toAtoms(out.Value),
		addresses:    addrs,       // out.ScriptPubKey.Addresses
		sigsRequired: numRequired, // out.ScriptPubKey.ReqSigs
		scriptType:   scriptType,  // integer representation of the string in out.ScriptPubKey.Type
	}

	confs = verboseTx.Confirmations
	return
}

// Get the Tx. Transaction info is not cached, so every call will result in a
// GetRawTransactionVerbose RPC call.
func (dcr *Backend) transaction(txHash *chainhash.Hash, verboseTx *chainjson.TxRawResult) (*Tx, error) {
	// Figure out if it's a stake transaction
	msgTx, err := msgTxFromHex(verboseTx.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MsgTx from hex for transaction %s: %w", txHash, err)
	}
	isStake := determineTxTree(msgTx) == wire.TxTreeStake

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
			return nil, fmt.Errorf("error decoding block hash %s for tx %s: %w", verboseTx.BlockHash, txHash, err)
		}
		// Make sure the block info is cached.
		_, err := dcr.getDcrBlock(dcr.ctx, blockHash)
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
			return nil, fmt.Errorf("error decoding previous tx hash %s for tx %s: %w", input.Txid, txHash, err)
		}
		inputs = append(inputs, txIn{prevTx: *hash, vout: input.Vout, value: value})
	}

	outputs := make([]txOut, 0, len(verboseTx.Vout))
	for vout, output := range verboseTx.Vout {
		pkScript, err := hex.DecodeString(output.ScriptPubKey.Hex)
		if err != nil {
			return nil, fmt.Errorf("error decoding pubkey script from %s for transaction %d:%d: %w",
				output.ScriptPubKey.Hex, txHash, vout, err)
		}
		sumOut += toAtoms(output.Value)
		outputs = append(outputs, txOut{
			value:    toAtoms(output.Value),
			version:  output.ScriptPubKey.Version,
			pkScript: pkScript,
		})
	}
	rawTx, err := hex.DecodeString(verboseTx.Hex)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx hex: %w", err)
	}

	feeRate := (sumIn - sumOut) / uint64(len(verboseTx.Hex)/2)
	if isCoinbase {
		feeRate = 0
	}
	return newTransaction(txHash, blockHash, lastLookup, verboseTx.BlockHeight, isStake, isCoinbase, inputs, outputs, feeRate, rawTx), nil
}

// run processes the queue and monitors the application context.
func (dcr *Backend) run(ctx context.Context) {
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
		dcr.log.Tracef("Notifying %d dcr asset consumers of new block at height %d",
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
			bestHash, err := dcr.node.GetBestBlockHash(ctx)
			if err != nil {
				sendErr(asset.NewConnectionError("error retrieving best block: %w", translateRPCCancelErr(err)))
				continue
			}
			if *bestHash == tip.hash {
				continue
			}

			best := bestHash.String()
			block, err := dcr.node.GetBlockVerbose(ctx, bestHash, false)
			if err != nil {
				sendErrFmt("error retrieving block %s: %w", best, translateRPCCancelErr(err))
				continue
			}
			// If this doesn't build on the best known block, look for a reorg.
			prevHash, err := chainhash.NewHashFromStr(block.PreviousHash)
			if err != nil {
				sendErrFmt("error parsing previous hash %s: %w", block.PreviousHash, err)
				continue
			}
			// If it builds on the best block or the cache is empty, it's good to add.
			if *prevHash == tip.hash || tip.height == 0 {
				dcr.log.Debugf("New block %s (%d)", bestHash, block.Height)
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
				iBlock, err := dcr.node.GetBlockVerbose(ctx, iHash, false)
				if err != nil {
					sendErrFmt("error retrieving block %s: %w", iHash, translateRPCCancelErr(err))
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
					sendErrFmt("error decoding previous hash %s for block %s: %w",
						iBlock.PreviousHash, iHash.String(), translateRPCCancelErr(err))
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
				dcr.log.Infof("Tip change from %s (%d) to %s (%d) detected (reorg or just fast blocks).",
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

// blockInfo returns block information for the verbose transaction data. The
// current tip hash is also returned as a convenience.
func (dcr *Backend) blockInfo(ctx context.Context, verboseTx *chainjson.TxRawResult) (blockHeight uint32, blockHash chainhash.Hash, tipHash *chainhash.Hash, err error) {
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
		blk, err = dcr.getBlockInfo(ctx, verboseTx.BlockHash)
		if err != nil {
			return
		}
		blockHeight = blk.height
		blockHash = blk.hash
	}
	return
}

// Get the UTXO, populating the block data along the way.
func (dcr *Backend) utxo(ctx context.Context, txHash *chainhash.Hash, vout uint32, redeemScript []byte) (*UTXO, error) {
	txOut, verboseTx, pkScript, err := dcr.getTxOutInfo(ctx, txHash, vout)
	if err != nil {
		return nil, err
	}
	if len(verboseTx.Vout) <= int(vout) { // shouldn't happen if gettxout worked
		return nil, fmt.Errorf("only %d outputs, requested index %d", len(verboseTx.Vout), vout)
	}

	scriptVersion := txOut.ScriptPubKey.Version // or verboseTx.Vout[vout].Version
	inputNfo, err := dexdcr.InputInfo(scriptVersion, pkScript, redeemScript, chainParams)
	if err != nil {
		return nil, err
	}
	scriptType := inputNfo.ScriptType

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.IsP2SH() {
		scriptHash := dexdcr.ExtractScriptHash(scriptVersion, pkScript)
		if !bytes.Equal(stdaddr.Hash160(redeemScript), scriptHash) {
			return nil, fmt.Errorf("script hash check failed for utxo %s,%d", txHash, vout)
		}
	}

	blockHeight, blockHash, lastLookup, err := dcr.blockInfo(ctx, verboseTx)
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
		return nil, fmt.Errorf("error fetching verbose transaction data: %w", err)
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
		scriptVersion:     scriptVersion,
		nonStandardScript: inputNfo.NonStandardScript,
		pkScript:          pkScript,
		redeemScript:      redeemScript,
		numSigs:           inputNfo.ScriptAddrs.NRequired,
		// The total size associated with the wire.TxIn.
		spendSize: inputNfo.Size(),
		value:     toAtoms(txOut.Value),
	}
	return &UTXO{out}, nil
}

// newTXIO creates a TXIO for any transaction, spent or unspent. The caller must
// set the maturity field.
func (dcr *Backend) newTXIO(txHash *chainhash.Hash) (*TXIO, int64, error) {
	verboseTx, err := dcr.node.GetRawTransactionVerbose(dcr.ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, asset.CoinNotFoundError
		}
		return nil, 0, fmt.Errorf("newTXIO: GetRawTransactionVerbose for txid %s: %w", txHash, translateRPCCancelErr(err))
	}
	tx, err := dcr.transaction(txHash, verboseTx)
	if err != nil {
		return nil, 0, fmt.Errorf("error fetching verbose transaction data: %w", err)
	}
	blockHeight, blockHash, lastLookup, err := dcr.blockInfo(dcr.ctx, verboseTx)
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
	inputNfo, err := dexdcr.InputInfo(txOut.version, pkScript, redeemScript, chainParams)
	if err != nil {
		return nil, err
	}
	scriptType := inputNfo.ScriptType

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	if scriptType.IsP2SH() {
		scriptHash := dexdcr.ExtractScriptHash(txOut.version, pkScript)
		if !bytes.Equal(stdaddr.Hash160(redeemScript), scriptHash) {
			return nil, fmt.Errorf("script hash check failed for output %s:%d", txHash, vout)
		}
	}

	scrAddrs := inputNfo.ScriptAddrs
	addresses := make([]string, len(scrAddrs.PubKeys)+len(scrAddrs.PkHashes))
	for i, addr := range append(scrAddrs.PkHashes, scrAddrs.PubKeys...) {
		addresses[i] = addr.String()
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
		spendSize: inputNfo.Size(),
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
func (dcr *Backend) getUnspentTxOut(ctx context.Context, txHash *chainhash.Hash, vout uint32, tree int8) (*chainjson.GetTxOutResult, []byte, error) {
	txOut, err := dcr.node.GetTxOut(ctx, txHash, vout, tree, true)
	if err != nil {
		return nil, nil, fmt.Errorf("GetTxOut error for output %s:%d: %w",
			txHash, vout, translateRPCCancelErr(err))
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
func (dcr *Backend) getTxOutInfo(ctx context.Context, txHash *chainhash.Hash, vout uint32) (*chainjson.GetTxOutResult, *chainjson.TxRawResult, []byte, error) {
	verboseTx, err := dcr.node.GetRawTransactionVerbose(ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) { // since we're calling gettxout after this, check now
			return nil, nil, nil, asset.CoinNotFoundError
		}
		return nil, nil, nil, fmt.Errorf("getTxOutInfo: GetRawTransactionVerbose for txid %s: %w", txHash, translateRPCCancelErr(err))
	}
	msgTx, err := msgTxFromHex(verboseTx.Hex)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode MsgTx from hex for transaction %s: %w", txHash, err)
	}
	tree := determineTxTree(msgTx)
	txOut, pkScript, err := dcr.getUnspentTxOut(ctx, txHash, vout, tree)
	if err != nil {
		return nil, nil, nil, err
	}
	return txOut, verboseTx, pkScript, nil
}

// determineTxTree determines if the transaction is in the regular transaction
// tree (wire.TxTreeRegular) or the stake tree (wire.TxTreeStake).
func determineTxTree(msgTx *wire.MsgTx) int8 {
	if stake.DetermineTxType(msgTx) != stake.TxTypeRegular {
		return wire.TxTreeStake
	}
	return wire.TxTreeRegular
}

// Get the block information, checking the cache first. Same as
// getDcrBlock, but takes a string argument.
func (dcr *Backend) getBlockInfo(ctx context.Context, blockid string) (*dcrBlock, error) {
	blockHash, err := chainhash.NewHashFromStr(blockid)
	if err != nil {
		return nil, fmt.Errorf("unable to decode block hash from %s", blockid)
	}
	return dcr.getDcrBlock(ctx, blockHash)
}

// Get the block information, checking the cache first.
func (dcr *Backend) getDcrBlock(ctx context.Context, blockHash *chainhash.Hash) (*dcrBlock, error) {
	cachedBlock, found := dcr.blockCache.block(blockHash)
	if found {
		return cachedBlock, nil
	}
	blockVerbose, err := dcr.node.GetBlockVerbose(ctx, blockHash, false)
	if err != nil {
		return nil, fmt.Errorf("error retrieving block %s: %w", blockHash, translateRPCCancelErr(err))
	}
	return dcr.blockCache.add(blockVerbose)
}

// Get the mainchain block at the given height, checking the cache first.
func (dcr *Backend) getMainchainDcrBlock(ctx context.Context, height uint32) (*dcrBlock, error) {
	cachedBlock, found := dcr.blockCache.atHeight(height)
	if found {
		return cachedBlock, nil
	}
	hash, err := dcr.node.GetBlockHash(ctx, int64(height))
	if err != nil {
		// Likely not mined yet. Not an error.
		return nil, nil
	}
	return dcr.getDcrBlock(ctx, hash)
}

// connectNodeRPC attempts to create a new websocket connection to a dcrd node
// with the given credentials and notification handlers.
func connectNodeRPC(host, user, pass, cert string) (*rpcclient.Client, error) {

	dcrdCerts, err := os.ReadFile(cert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
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
		return nil, fmt.Errorf("Failed to start dcrd RPC client: %w", err)
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
	return uint64(math.Round(v * conventionalConversionFactor))
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCNoTxInfo
}
