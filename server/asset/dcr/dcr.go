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
	"io/ioutil"
	"math"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/rpcclient/v7"
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

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

func init() {
	asset.Register(assetName, &Driver{})
}

var (
	zeroHash chainhash.Hash
	// The blockPollInterval is the delay between calls to GetBestBlockHash to
	// check for new blocks.
	blockPollInterval = time.Second

	requiredNodeVersion = dex.Semver{Major: 7, Minor: 0, Patch: 0}
)

const (
	version                  = 0
	assetName                = "dcr"
	immatureTransactionError = dex.ErrorKind("immature output")
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
}

// The rpcclient package functions will return a rpcclient.ErrRequestCanceled
// error if the context is canceled. Translate these to asset.ErrRequestTimeout.
func translateRPCCancelErr(err error) error {
	if errors.Is(err, rpcclient.ErrRequestCanceled) {
		err = asset.ErrRequestTimeout
	}
	return err
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
	if !dex.SemverCompatible(requiredNodeVersion, nodeSemver) {
		dcr.shutdown()
		return nil, fmt.Errorf("dcrd has an incompatible JSON-RPC version: got %s, expected %s",
			nodeSemver, requiredNodeVersion)
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

	if _, err = dcr.FeeRate(); err != nil {
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
func (dcr *Backend) FeeRate() (uint64, error) {
	// estimatesmartfee 1 returns extremely high rates on DCR.
	estimateFeeResult, err := dcr.node.EstimateSmartFee(dcr.ctx, 2, chainjson.EstimateSmartFeeConservative)
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
	chainInfo, err := dcr.node.GetBlockChainInfo(dcr.ctx)
	if err != nil {
		return false, fmt.Errorf("GetBlockChainInfo error: %w", translateRPCCancelErr(err))
	}
	return !chainInfo.InitialBlockDownload && chainInfo.Headers-chainInfo.Blocks <= 1, nil
}

// Redemption is an input that redeems a swap contract.
func (dcr *Backend) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
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
		return nil, err
	}
	if utxo.nonStandardScript {
		return nil, fmt.Errorf("non-standard script")
	}
	return utxo, nil
}

// ValidateXPub validates the base-58 encoded extended key, and ensures that it
// is an extended public, not private, key.
func (dcr *Backend) ValidateXPub(xpub string) error {
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

// CheckAddress checks that the given address is parseable.
func (dcr *Backend) CheckAddress(addr string) bool {
	_, err := dcrutil.DecodeAddress(addr, chainParams)
	if err != nil {
		dcr.log.Errorf("DecodeAddress error for %s: %v", addr, err)
	}

	return err == nil
}

// TxData is the raw transaction bytes. SPV clients rebroadcast the transaction
// bytes to get around not having a mempool to check.
func (dcr *Backend) TxData(coinID []byte) ([]byte, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	dcrutilTx, err := dcr.node.GetRawTransaction(dcr.ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txHash, err)
	}
	return dcrutilTx.MsgTx().Bytes()
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

	var txOut *TxOutData
	txOut, confs, err = dcr.OutputSummary(txHash, vout)
	if err != nil {
		return
	}

	if len(txOut.Addresses) != 1 || txOut.SigsRequired != 1 ||
		txOut.ScriptType != dexdcr.ScriptP2PKH /* no schorr or edwards */ ||
		txOut.ScriptType&dexdcr.ScriptStake != 0 {
		return "", 0, -1, dex.UnsupportedScriptError
	}
	addr = txOut.Addresses[0]
	val = txOut.Value
	return
}

// TxOutData is transaction output data, including recipient addresses, value,
// script type, and number of required signatures.
type TxOutData struct {
	Value        uint64
	Addresses    []string
	SigsRequired int
	ScriptType   dexdcr.DCRScriptType
}

// OutputSummary gets transaction output data, including recipient addresses,
// value, script type, and number of required signatures, plus the current
// confirmations of a transaction output. If the output does not exist, an error
// will be returned. Non-standard scripts are not an error.
func (dcr *Backend) OutputSummary(txHash *chainhash.Hash, vout uint32) (txOut *TxOutData, confs int64, err error) {
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
		err = asset.CoinNotFoundError
		return
	}

	out := verboseTx.Vout[vout]

	scriptHex, err := hex.DecodeString(out.ScriptPubKey.Hex)
	if err != nil {
		return nil, -1, dex.UnsupportedScriptError
	}
	scriptType, addrs, numRequired, err := dexdcr.ExtractScriptData(scriptHex, chainParams)
	if err != nil {
		return nil, -1, dex.UnsupportedScriptError
	}

	txOut = &TxOutData{
		Value:        toAtoms(out.Value),
		Addresses:    addrs,
		SigsRequired: numRequired,
		ScriptType:   scriptType,
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
			return nil, fmt.Errorf("utxo error: %w", err)
		}
		if !bytes.Equal(dcrutil.Hash160(redeemScript), scriptHash) {
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
		return nil, 0, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txHash, translateRPCCancelErr(err))
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
			return nil, fmt.Errorf("output error: %w", err)
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
		return nil, nil, nil, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txHash, translateRPCCancelErr(err))
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

func determineTxTree(msgTx *wire.MsgTx) int8 {
	// stake.DetermineTxType will produce correct results if we pass true for
	// isTreasuryEnabled regardless of whether the treasury vote has activated
	// or not.
	// The only possiblity for wrong results is passing isTreasuryEnabled=false
	// _after_ the treasury vote activates - some stake tree votes may identify
	// as regular tree transactions.
	// Could try with isTreasuryEnabled false, then true and if neither comes up
	// as a stake transaction, then we infer regular, but that isn't necessary
	// as explained above.
	isTreasuryEnabled := true
	if stake.DetermineTxType(msgTx, isTreasuryEnabled) != stake.TxTypeRegular {
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

	dcrdCerts, err := ioutil.ReadFile(cert)
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
	return uint64(math.Round(v * 1e8))
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCNoTxInfo
}
