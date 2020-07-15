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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	assetName = "btc"
	// Use RawRequest to get the verbose block header for a blockhash.
	methodGetBlockHeader = "getblockheader"
	// Use RawRequest to get the verbose block with verbose txs, as the btcd
	// rpcclient.Client's GetBlockVerboseTx appears to be busted.
	methodGetBlockVerboseTx = "getblock"
	methodGetNetworkInfo    = "getnetworkinfo"
	BipID                   = 0
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee         = 100
	minNetworkVersion  = 190000
	minProtocolVersion = 70015
)

var (
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker    = time.Second
	fallbackFeeKey = "fallbackfee"
	configOpts     = []*asset.ConfigOption{
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Bitcoin's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Bitcoin's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcbind",
			DisplayName: "JSON-RPC Address",
			Description: "<addr> or <addr>:<port> (default 'localhost')",
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Port for RPC connections (if not set in Address)",
		},
		{
			Key:          fallbackFeeKey,
			DisplayName:  "Fallback fee rate",
			Description:  "Bitcoin's 'fallbackfee' rate. Units: Sats/kB",
			DefaultValue: defaultFee,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "Pre-split funding inputs to prevent locking funds into an order for which a change output may not be immediately available. Only used for standing-type orders.",
			IsBoolean:   true,
		},
	}
	// walletInfo defines some general information about a Bitcoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Bitcoin",
		Units:             "Satoshis",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"),
		ConfigOpts:        configOpts,
	}
)

// rpcClient is a wallet RPC client. In production, rpcClient is satisfied by
// rpcclient.Client. A stub can be used for testing.
type rpcClient interface {
	EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error)
	SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash() (*chainhash.Hash, error)
	GetRawMempool() ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)
	RawRequest(method string, params []json.RawMessage) (json.RawMessage, error)
}

// BTCCloneCFG holds clone specific parameters.
type BTCCloneCFG struct {
	WalletCFG         *asset.WalletConfig
	MinNetworkVersion uint64
	WalletInfo        *asset.WalletInfo
	Symbol            string
	Logger            dex.Logger
	Network           dex.Network
	ChainParams       *chaincfg.Params
	Ports             dexbtc.NetPorts
}

// outpointID creates a unique string for a transaction output.
func outpointID(txid string, vout uint32) string {
	return txid + ":" + strconv.Itoa(int(vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	txHash chainhash.Hash
	vout   uint32
	value  uint64
	redeem dex.Bytes
	node   rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64, redeem dex.Bytes) *output {
	return &output{
		txHash: *txHash,
		vout:   vout,
		value:  value,
		redeem: redeem,
		node:   node,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (op *output) Value() uint64 {
	return op.value
}

// Confirmations is the number of confirmations on the output's block.
// Confirmations always pulls the block information fresh from the block chain,
// and will return an error if the output has been spent. Part of the
// asset.Coin interface.
func (op *output) Confirmations() (uint32, error) {
	txOut, err := op.node.GetTxOut(&op.txHash, op.vout, true)
	if err != nil {
		return 0, fmt.Errorf("error finding coin: %v", err)
	}
	if txOut == nil {
		return 0, asset.CoinNotFoundError
	}
	return uint32(txOut.Confirmations), nil
}

// ID is the output's coin ID. Part of the asset.Coin interface. For BTC, the
// coin ID is 36 bytes = 32 bytes tx hash + 4 bytes big-endian vout.
func (op *output) ID() dex.Bytes {
	return toCoinID(&op.txHash, op.vout)
}

// String is a string representation of the coin.
func (op *output) String() string {
	return fmt.Sprintf("%s:%v", op.txHash, op.vout)
}

// Redeem is any known redeem script required to spend this output. Part of the
// asset.Coin interface.
func (op *output) Redeem() dex.Bytes {
	return op.redeem
}

// auditInfo is information about a swap contract on that blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
// auditInfo satisfies the asset.AuditInfo interface.
type auditInfo struct {
	output     *output
	recipient  btcutil.Address
	secretHash []byte
	expiration time.Time
}

// Recipient returns a base58 string for the contract's receiving address. Part
// of the asset.AuditInfo interface.
func (ci *auditInfo) Recipient() string {
	return ci.recipient.String()
}

// Expiration returns the expiration time of the contract, which is the earliest
// time that a refund can be issued for an un-redeemed contract. Part of the
// asset.AuditInfo interface.
func (ci *auditInfo) Expiration() time.Time {
	return ci.expiration
}

// Coin returns the output as an asset.Coin. Part of the asset.AuditInfo
// interface.
func (ci *auditInfo) Coin() asset.Coin {
	return ci.output
}

// SecretHash is the contract's secret hash.
func (ci *auditInfo) SecretHash() dex.Bytes {
	return ci.secretHash
}

// swapReceipt is information about a swap contract that was broadcast by this
// wallet. Satisfies the asset.Receipt interface.
type swapReceipt struct {
	output     *output
	expiration time.Time
}

// Expiration is the time that the contract will expire, allowing the user to
// issue a refund transaction. Part of the asset.Receipt interface.
func (r *swapReceipt) Expiration() time.Time {
	return r.expiration
}

// Coin is the output information as an asset.Coin. Part of the asset.Receipt
// interface.
func (r *swapReceipt) Coin() asset.Coin {
	return r.output
}

// String provides a human-readable representation of the contract's Coin.
func (r *swapReceipt) String() string {
	return r.output.String()
}

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the BTC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
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

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

// ExchangeWallet is a wallet backend for Bitcoin. The backend is how the DEX
// client app communicates with the BTC blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	client            *rpcclient.Client
	node              rpcClient
	wallet            *walletClient
	walletInfo        *asset.WalletInfo
	chainParams       *chaincfg.Params
	log               dex.Logger
	symbol            string
	hasConnected      uint32
	tipChange         func(error)
	minNetworkVersion uint64
	fallbackFeeRate   uint64
	useSplitTx        bool

	// In the future, the client may wish to specify minimum confirmations for
	// utxos to fund orders, and allowing change outputs from DEX-related swap
	// transaction may be part of this client policy.
	changeMtx   sync.RWMutex
	tradeChange map[string]time.Time // TODO: delete fully confirmed change

	// Coins returned by Fund are cached for quick reference and for cleanup on
	// shutdown.
	fundingMtx   sync.RWMutex
	fundingCoins map[string]*compositeUTXO
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the bitcoind config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
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
	cloneCFG := &BTCCloneCFG{
		WalletCFG:         cfg,
		MinNetworkVersion: minNetworkVersion,
		WalletInfo:        WalletInfo,
		Symbol:            "btc",
		Logger:            logger,
		Network:           network,
		ChainParams:       params,
		Ports:             dexbtc.RPCPorts,
	}

	if cfg.Settings[fallbackFeeKey] == "" {
		cfg.Settings[fallbackFeeKey] = strconv.FormatUint(defaultFee, 10)
	}

	return BTCCloneWallet(cloneCFG)
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *BTCCloneCFG) (*ExchangeWallet, error) {

	// Read the configuration parameters
	btcCfg, err := dexbtc.LoadConfigFromSettings(cfg.WalletCFG.Settings, cfg.Symbol, cfg.Network, cfg.Ports)
	if err != nil {
		return nil, err
	}

	endpoint := btcCfg.RPCBind + "/wallet/" + cfg.WalletCFG.Settings["walletname"]
	cfg.Logger.Infof("Setting up new %s wallet at %s.", cfg.Symbol, endpoint)

	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         endpoint,
		User:         btcCfg.RPCUser,
		Pass:         btcCfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating BTC RPC client: %v", err)
	}

	btc := newWallet(cfg, btcCfg, client)
	btc.client = client

	return btc, nil
}

// newWallet creates the ExchangeWallet and starts the block monitor.
func newWallet(cfg *BTCCloneCFG, btcCfg *dexbtc.Config, node rpcClient) *ExchangeWallet {
	return &ExchangeWallet{
		node:              node,
		wallet:            newWalletClient(node, cfg.ChainParams),
		symbol:            cfg.Symbol,
		chainParams:       cfg.ChainParams,
		log:               cfg.Logger,
		tradeChange:       make(map[string]time.Time),
		tipChange:         cfg.WalletCFG.TipChange,
		fundingCoins:      make(map[string]*compositeUTXO),
		minNetworkVersion: cfg.MinNetworkVersion,
		fallbackFeeRate:   btcCfg.FallbackFeeRate,
		useSplitTx:        btcCfg.UseSplitTx,
		walletInfo:        cfg.WalletInfo,
	}
}

var _ asset.Wallet = (*ExchangeWallet)(nil)

// Info returns basic information about the wallet and asset.
func (btc *ExchangeWallet) Info() *asset.WalletInfo {
	return btc.walletInfo
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (btc *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Check the version. Do it here, so we can also diagnose a bad connection.
	netVer, codeVer, err := btc.getVersion()
	if err != nil {
		return nil, fmt.Errorf("error getting version: %v", err)
	}
	if netVer < btc.minNetworkVersion {
		return nil, fmt.Errorf("reported node version %d is less than minimum %d", netVer, minNetworkVersion)
	}
	if codeVer < minProtocolVersion {
		return nil, fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion)
	}
	// If this is the first time connecting, clear the locked coins. This should
	// have been done at shutdown, but shutdown may not have been clean.
	if atomic.SwapUint32(&btc.hasConnected, 1) == 0 {
		err := btc.wallet.LockUnspent(true, nil)
		if err != nil {
			return nil, err
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.run(ctx)
		err := btc.wallet.LockUnspent(true, nil)
		if err != nil {
			btc.log.Errorf("failed to unlock %s outputs on shutdown: %v", btc.symbol, err)
		}
	}()
	return &wg, nil
}

// Balance returns the total available funds in the wallet. Part of the
// asset.Wallet interface.
func (btc *ExchangeWallet) Balance() (*asset.Balance, error) {
	balances, err := btc.wallet.Balances()
	if err != nil {
		return nil, err
	}
	locked, err := btc.lockedSats()
	if err != nil {
		return nil, err
	}

	return &asset.Balance{
		Available: toSatoshi(balances.Mine.Trusted),
		Immature:  toSatoshi(balances.Mine.Immature + balances.Mine.Untrusted),
		Locked:    locked,
	}, err
}

// FeeRate returns the current optimal fee rate in sat / byte.
func (btc *ExchangeWallet) FeeRate() (uint64, error) {
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
	satPerKB, err := btcutil.NewAmount(*feeResult.FeeRate) // satPerKB is 0 when err != nil
	if err != nil {
		return 0, err
	}
	// Add 1 extra sat/byte, which is both extra conservative and prevents a
	// zero value if the sat/KB is less than 1000.
	return 1 + uint64(satPerKB)/1000, nil
}

// feeRateWithFallback attempts to get the optimal fee rate in sat / byte via
// FeeRate. If that fails, it will return the configured fallback fee rate.
func (btc *ExchangeWallet) feeRateWithFallback() uint64 {
	feeRate, err := btc.FeeRate()
	if err != nil {
		feeRate = btc.fallbackFeeRate
		btc.log.Warnf("Unable to get optimal fee rate, using fallback of %d: %v",
			btc.fallbackFeeRate, err)
	}
	return feeRate
}

// FundOrder selects utxos (as asset.Coin) for use in an order. Any Coins
// returned will be locked. Part of the asset.Wallet interface.
func (btc *ExchangeWallet) FundOrder(value uint64, nfo *dex.Asset) (asset.Coins, error) {
	if value == 0 {
		return nil, fmt.Errorf("cannot fund value = 0")
	}
	// Now that we allow funding with 0 conf UTXOs, some more logic could be
	// used out of caution, including preference for >0 confs.
	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, fmt.Errorf("error parsing unspent outputs: %v", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("insufficient funds. %.8f available", btcutil.Amount(avail).ToBTC())
	}
	var sum uint64
	var size uint32
	var coins asset.Coins
	var spents []*output
	fundingCoins := make(map[string]*compositeUTXO)

	// TODO: For the chained swaps, make sure that contract outputs are P2WSH,
	// and that change outputs that fund further swaps are P2WPKH.

	isEnoughWith := func(unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFunds(value, uint64(size+unspent.input.VBytes()), nfo)
		return sum+unspent.amount >= reqFunds
	}

	addUTXO := func(unspent *compositeUTXO) {
		v := unspent.amount
		op := newOutput(btc.node, unspent.txHash, unspent.vout, v, unspent.redeemScript)
		coins = append(coins, op)
		spents = append(spents, op)
		size += unspent.input.VBytes()
		fundingCoins[op.String()] = unspent
		sum += v
	}

out:
	for {
		// If there are none left, we don't have enough.
		if len(utxos) == 0 {
			return nil, fmt.Errorf("not enough to cover requested funds + fees = %d",
				calc.RequiredOrderFunds(value, uint64(size), nfo))
		}
		// On each loop, find the smallest UTXO that is enough for the value. If
		// no UTXO is large enough, add the largest and continue.
		var txout *compositeUTXO
		for _, txout = range utxos {
			if isEnoughWith(txout) {
				addUTXO(txout)
				break out
			}
		}
		// Append the last output, which is the largest.
		addUTXO(txout)
		// Pop the utxo from the unspents
		utxos = utxos[:len(utxos)-1]
	}

	err = btc.wallet.LockUnspent(false, spents)
	if err != nil {
		return nil, err
	}

	btc.fundingMtx.Lock()
	for opID, utxo := range fundingCoins {
		btc.fundingCoins[opID] = utxo
	}
	btc.fundingMtx.Unlock()

	btc.log.Debugf("funding %d %s order with coins %v", value, btc.walletInfo.Units, coins)

	return coins, nil
}

// ReturnCoins unlocks coins. This would be used in the case of a
// canceled or partially filled order. Part of the asset.Wallet interface.
func (btc *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}
	ops := make([]*output, 0, len(unspents))
	btc.log.Debugf("returning coins %s", unspents)
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for _, unspent := range unspents {
		op, err := btc.convertCoin(unspent)
		if err != nil {
			return fmt.Errorf("error converting coin: %v", err)
		}
		ops = append(ops, op)
		delete(btc.fundingCoins, outpointID(op.txHash.String(), op.vout))
	}
	return btc.wallet.LockUnspent(true, ops)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (btc *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[string]struct{})
	btc.fundingMtx.RLock()
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			btc.fundingMtx.RUnlock()
			return nil, err
		}
		opID := outpointID(txHash.String(), vout)
		fundingCoin, found := btc.fundingCoins[opID]
		if found {
			coins = append(coins, newOutput(btc.node, txHash, vout, fundingCoin.amount, fundingCoin.redeemScript))
			continue
		}
		notFound[opID] = struct{}{}
	}
	btc.fundingMtx.RUnlock()
	if len(notFound) == 0 {
		return coins, nil
	}
	_, utxoMap, _, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, err
	}
	lockers := make([]*output, 0, len(ids))
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		opID := outpointID(txHash.String(), vout)
		utxo, found := utxoMap[opID]
		if !found {
			return nil, fmt.Errorf("funding coin %s not found", opID)
		}
		btc.fundingCoins[opID] = utxo
		coin := newOutput(btc.node, utxo.txHash, utxo.vout, utxo.amount, utxo.redeemScript)
		coins = append(coins, coin)
		lockers = append(lockers, coin)
		delete(notFound, opID)
		if len(notFound) == 0 {
			break
		}
	}
	if len(notFound) != 0 {
		ids := make([]string, 0, len(notFound))
		for opID := range notFound {
			ids = append(ids, opID)
		}
		return nil, fmt.Errorf("coins not found: %s", strings.Join(ids, ", "))
	}
	err = btc.wallet.LockUnspent(false, lockers)
	if err != nil {
		return nil, err
	}

	btc.log.Debugf("locking coins %v", coins)

	return coins, nil
}

// Unlock unlocks the ExchangeWallet. The pw supplied should be the same as the
// password for the underlying bitcoind wallet which will also be unlocked.
func (btc *ExchangeWallet) Unlock(pw string, dur time.Duration) error {
	return btc.wallet.Unlock(pw, dur)
}

// Lock locks the ExchangeWallet and the underlying bitcoind wallet.
func (btc *ExchangeWallet) Lock() error {
	return btc.wallet.Lock()
}

// Swap sends the swaps in a single transaction. The Receipts returned can be
// used to refund a failed transaction.
func (btc *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, error) {
	var contracts [][]byte
	var totalOut, totalIn uint64
	// Start with an empty MsgTx.
	baseTx := wire.NewMsgTx(wire.TxVersion)

	// Add the funding utxos.
	spents := make([]*output, 0, len(swaps.Inputs))
	for _, coin := range swaps.Inputs {
		output, err := btc.convertCoin(coin)
		if err != nil {
			return nil, nil, fmt.Errorf("error converting coin: %v", err)
		}
		if output.value == 0 {
			return nil, nil, fmt.Errorf("zero-valued output detected for %s:%d", output.txHash, output.vout)
		}
		totalIn += output.value
		prevOut := wire.NewOutPoint(&output.txHash, output.vout)
		txIn := wire.NewTxIn(prevOut, []byte{}, nil)
		baseTx.AddTxIn(txIn)
		spents = append(spents, output)
	}

	// Add the contract outputs.
	// TODO: Make P2WSH contract and P2WPKH change outputs instead of
	// legacy/non-segwit swap contracts pkScripts.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address that will receive the refund if the contract is
		// abandoned.
		revokeAddr, err := btc.wallet.AddressPKH()
		if err != nil {
			return nil, nil, fmt.Errorf("error creating revocation address: %v", err)
		}
		// Create the contract, a P2SH redeem script.
		pkScript, err := dexbtc.MakeContract(contract.Address, revokeAddr.String(), contract.SecretHash, int64(contract.LockTime), btc.chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create pubkey script for address %s: %v", contract.Address, err)
		}
		contracts = append(contracts, pkScript)
		// Make the P2SH address and pubkey script.
		scriptAddr, err := btcutil.NewAddressScriptHash(pkScript, btc.chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error encoding script address: %v", err)
		}
		p2shScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating P2SH script: %v", err)
		}
		// Add the transaction output.
		txOut := wire.NewTxOut(int64(contract.Value), p2shScript)
		baseTx.AddTxOut(txOut)
	}
	if totalIn < totalOut {
		return nil, nil, fmt.Errorf("unfunded contract. %d < %d", totalIn, totalOut)
	}

	// Ensure we have enough outputs before broadcasting.
	swapCount := len(swaps.Contracts)
	if len(baseTx.TxOut) < swapCount {
		return nil, nil, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}

	// Grab a change address.
	changeAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %v", err)
	}

	// Sign, add change, and send the transaction.
	msgTx, change, changeScript, err := btc.sendWithReturn(baseTx, changeAddr, totalIn, totalOut, swaps.FeeRate)
	if err != nil {
		return nil, nil, err
	}

	// Prepare the receipts.
	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			output:     newOutput(btc.node, &txHash, uint32(i), contract.Value, contracts[i]),
			expiration: time.Unix(int64(contract.LockTime), 0).UTC(),
		})
	}

	// If change is nil, return a nil asset.Coin.
	var changeCoin asset.Coin
	if change != nil {
		changeCoin = change
	}

	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	if swaps.LockChange {
		// Lock the change output
		btc.log.Debugf("locking change coin %s", change)
		err = btc.wallet.LockUnspent(false, []*output{change})
		if err != nil {
			// The swap transaction is already broadcasted, so don't fail now.
			btc.log.Errorf("failed to lock change output: %v", err)
		}

		// Log it as a fundingCoin, since it is expected that this will be
		// chained into further matches.
		nfo, err := dexbtc.InputInfo(changeScript, nil, btc.chainParams)
		if err != nil {
			// This would be virtually impossible at this point.
			return nil, nil, fmt.Errorf("error getting spend info: %v", err)
		}

		btc.fundingCoins[outpointID(change.txHash.String(), change.vout)] = &compositeUTXO{
			txHash:       &change.txHash,
			vout:         change.vout,
			address:      changeAddr.String(),
			redeemScript: nil,
			amount:       change.value,
			input:        nfo,
		}
	}

	// Delete the UTXOs from the cache.
	for _, spent := range spents {
		delete(btc.fundingCoins, outpointID(spent.txHash.String(), spent.vout))
	}

	return receipts, changeCoin, nil
}

// Redeem sends the redemption transaction, completing the atomic swap.
func (btc *ExchangeWallet) Redeem(redemptions []*asset.Redemption) ([]dex.Bytes, asset.Coin, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx(wire.TxVersion)
	var totalIn uint64
	var contracts [][]byte
	var addresses []btcutil.Address
	for _, r := range redemptions {
		cinfo, ok := r.Spends.(*auditInfo)
		if !ok {
			return nil, nil, fmt.Errorf("Redemption contract info of wrong type")
		}
		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		_, receiver, _, secretHash, err := dexbtc.ExtractSwapDetails(cinfo.output.redeem, btc.chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error extracting swap addresses: %v", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, fmt.Errorf("secret hash mismatch")
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, cinfo.output.redeem)
		prevOut := wire.NewOutPoint(&cinfo.output.txHash, cinfo.output.vout)
		txIn := wire.NewTxIn(prevOut, []byte{}, nil)
		// Enable locktime
		// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
		txIn.Sequence = wire.MaxTxInSequenceNum - 1
		msgTx.AddTxIn(txIn)
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := msgTx.SerializeSize() + dexbtc.RedeemSwapSigScriptSize*len(redemptions) + dexbtc.P2WPKHOutputSize
	feeRate := btc.feeRateWithFallback()
	fee := feeRate * uint64(size)
	if fee > totalIn {
		return nil, nil, fmt.Errorf("redeem tx not worth the fees")
	}
	// Send the funds back to the exchange wallet.
	redeemAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change script: %v", err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, feeRate) {
		return nil, nil, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign the inputs.
	for i, r := range redemptions {
		contract := contracts[i]
		redeemSig, redeemPubKey, err := btc.createSig(msgTx, i, contract, addresses[i])
		if err != nil {
			return nil, nil, err
		}
		redeemSigScript, err := dexbtc.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return nil, nil, err
		}
		msgTx.TxIn[i].SignatureScript = redeemSigScript
	}
	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, err
	}
	if *txHash != checkHash {
		return nil, nil, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	// Log the change output.
	btc.addChange(txHash.String(), 0)
	coinIDs := make([]dex.Bytes, 0, len(redemptions))
	for i := range redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}
	return coinIDs, newOutput(btc.node, txHash, 0, uint64(txOut.Value), nil), nil
}

// SignMessage signs the message with the private key associated with the
// specified unspent coin. A slice of pubkeys required to spend the coin and a
// signature for each pubkey are returned.
func (btc *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	op, err := btc.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %v", err)
	}
	btc.fundingMtx.RLock()
	utxo := btc.fundingCoins[outpointID(op.txHash.String(), op.vout)]
	btc.fundingMtx.RUnlock()
	if utxo == nil {
		return nil, nil, fmt.Errorf("no utxo found for %s", op)
	}
	privKey, err := btc.wallet.PrivKeyForAddress(utxo.address)
	if err != nil {
		return nil, nil, err
	}
	pk := privKey.PubKey()
	sig, err := privKey.Sign(msg)
	if err != nil {
		return nil, nil, err
	}
	pubkeys = append(pubkeys, pk.SerializeCompressed())
	sigs = append(sigs, sig.Serialize())
	return
}

// AuditContract retrieves information about a swap contract on the blockchain.
// AuditContract would be used to audit the counter-party's contract during a
// swap.
func (btc *ExchangeWallet) AuditContract(coinID dex.Bytes, contract dex.Bytes) (asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Get the contracts P2SH address from the tx output's pubkey script.
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %s:%d : %v", txHash, vout, err)
	}
	if txOut == nil {
		return nil, asset.CoinNotFoundError
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, fmt.Errorf("error decoding pubkey script from hex '%s': %v",
			txOut.ScriptPubKey.Hex, err)
	}
	// Check for standard P2SH.
	scriptClass, addrs, numReq, err := txscript.ExtractPkScriptAddrs(pkScript, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting script addresses from '%x': %v", pkScript, err)
	}
	if scriptClass != txscript.ScriptHashTy {
		return nil, fmt.Errorf("unexpected script class %d", scriptClass)
	}
	// These last two checks are probably overkill.
	if numReq != 1 {
		return nil, fmt.Errorf("unexpected number of signatures expected for P2SH script: %d", numReq)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}
	// Compare the contract hash to the P2SH address.
	contractHash := btcutil.Hash160(contract)
	addr := addrs[0]
	if !bytes.Equal(contractHash, addr.ScriptAddress()) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addr.ScriptAddress())
	}
	return &auditInfo{
		output:     newOutput(btc.node, txHash, vout, toSatoshi(txOut.Value), contract),
		recipient:  receiver,
		secretHash: secretHash,
		expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (btc *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.chainParams)
	if err != nil {
		return false, fmt.Errorf("error extracting contract locktime: %v", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	bestBlockHash, err := btc.node.GetBestBlockHash()
	if err != nil {
		return false, fmt.Errorf("get best block hash error: %v", err)
	}
	bestBlockHeader, err := btc.getBlockHeader(bestBlockHash.String())
	if err != nil {
		return false, fmt.Errorf("get best block header error: %v", err)
	}
	bestBlockMedianTime := time.Unix(bestBlockHeader.MedianTime, 0).UTC()
	return bestBlockMedianTime.After(contractExpiry), nil
}

// FindRedemption attempts to find the input that spends the specified output,
// and returns the secret key if it does. The inputs are the txid and vout of
// the contract output that was redeemed. This method only works with contracts
// sent from the wallet.
func (btc *ExchangeWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	txid := txHash.String()
	// Get the wallet transaction.
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("error finding transaction %s in wallet: %v", txHash, err)
	}
	if tx.BlockIndex == 0 {
		// This is a mempool transaction, GetRawTransactionVerbose works on mempool
		// transactions, so grab the contract from there and prepare a hash.
		rawTx, err := btc.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			return nil, fmt.Errorf("error getting contract details from mempool")
		}
		if int(vout) > len(rawTx.Vout)-1 {
			return nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
		}
		contractHash, err := dexbtc.ExtractContractHash(rawTx.Vout[vout].ScriptPubKey.Hex)
		if err != nil {
			return nil, err
		}
		// Only need to look at other mempool transactions. For each txid, get the
		// verbose transaction and check each input's previous outpoint.
		txs, err := btc.node.GetRawMempool()
		if err != nil {
			return nil, fmt.Errorf("error retrieve mempool transactions")
		}
		for _, txHash := range txs {
			tx, err := btc.node.GetRawTransactionVerbose(txHash)
			if err != nil {
				return nil, fmt.Errorf("error encountered retreving mempool transaction %s: %v", txHash, err)
			}
			for i := range tx.Vin {
				if tx.Vin[i].Txid == txid && tx.Vin[i].Vout == vout {
					// Found it. Extract the key.
					vin := tx.Vin[i]
					sigScript, err := hex.DecodeString(vin.ScriptSig.Hex)
					if err != nil {
						return nil, fmt.Errorf("error decoding scriptSig '%s': %v", vin.ScriptSig.Hex, err)
					}
					key, err := dexbtc.FindKeyPush(sigScript, contractHash, btc.chainParams)
					if err != nil {
						return nil, fmt.Errorf("found spending input at %s:%d, but error extracting key from '%s': %v",
							txHash, i, vin.ScriptSig.Hex, err)
					}
					return key, nil
				}
			}
		}
		return nil, fmt.Errorf("no key found")
	}
	// Since it's not a mempool transaction, start scanning blocks at the height
	// of the swap.
	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("error decoding block hash: %v", err)
	}
	contractBlock := *blockHash
	var contractHash []byte
	blockHeight := tx.BlockIndex
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("redemption search canceled")
		default:
		}
		block, err := btc.getVerboseBlockTxs(blockHash.String())
		if err != nil {
			return nil, fmt.Errorf("error fetching verbose block '%s': %v", blockHash, err)
		}
		// If this is the swap contract's block, grab the contract hash.
		if *blockHash == contractBlock {
			for i := range block.Tx {
				if block.Tx[i].Txid == txid {
					vouts := block.Tx[i].Vout
					if len(vouts) < int(vout)+1 {
						return nil, fmt.Errorf("vout %d not found in tx %s", vout, txid)
					}
					contractHash, err = dexbtc.ExtractContractHash(vouts[vout].ScriptPubKey.Hex)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if len(contractHash) == 0 {
			return nil, fmt.Errorf("no secret hash found at %s:%d", txid, vout)
		}
		// Look at the previous outpoint of each input of each transaction in the
		// block.
		for i := range block.Tx {
			for j := range block.Tx[i].Vin {
				if block.Tx[i].Vin[j].Txid == txid && block.Tx[i].Vin[j].Vout == vout {
					// Found it. Extract the key.
					vin := block.Tx[i].Vin[j]
					sigScript, err := hex.DecodeString(vin.ScriptSig.Hex)
					if err != nil {
						return nil, fmt.Errorf("error decoding scriptSig '%s': %v", vin.ScriptSig.Hex, err)
					}
					key, err := dexbtc.FindKeyPush(sigScript, contractHash, btc.chainParams)
					if err != nil {
						return nil, fmt.Errorf("error extracting key from '%s': %v", vin.ScriptSig.Hex, err)
					}
					return key, nil
				}
			}
		}
		// Not found in this block. Get the next block
		blockHeight++
		blockHash, err = btc.node.GetBlockHash(blockHeight)
		if err != nil {
			return nil, fmt.Errorf("spending transaction not found")
		}
	}
	// No viable way to get here.
}

// Refund revokes a contract. This can only be used after the time lock has
// expired.
// NOTE: The contract cannot be retrieved from the unspent coin info as the
// wallet does not store it, even though it was known when the init transaction
// was created. The client should store this information for persistence across
// sessions.
func (btc *ExchangeWallet) Refund(coinID, contract dex.Bytes) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Grab the unspent output to make sure it's good and to get the value.
	utxo, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}
	val := toSatoshi(utxo.Value)
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}

	// Create the transaction that spends the contract.
	feeRate := btc.feeRateWithFallback()
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.
	size := msgTx.SerializeSize() + dexbtc.RefundSigScriptSize + dexbtc.P2WPKHOutputSize
	fee := feeRate * uint64(size) // TODO: use btc.FeeRate in caller and fallback to nfo.MaxFeeRate
	if fee > val {
		return nil, fmt.Errorf("refund tx not worth the fees")
	}
	refundAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating change script: %v", err)
	}
	txOut := wire.NewTxOut(int64(val-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, feeRate) {
		return nil, fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := btc.createSig(msgTx, 0, contract, sender)
	if err != nil {
		return nil, err
	}
	redeemSigScript, err := dexbtc.RefundP2SHContract(contract, refundSig, refundPubKey)
	if err != nil {
		return nil, err
	}
	txIn.SignatureScript = redeemSigScript
	// Send it.
	checkHash := msgTx.TxHash()
	refundHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, err
	}
	if *refundHash != checkHash {
		return nil, fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return toCoinID(refundHash, 0), nil
}

// Address returns a new external address from the wallet.
func (btc *ExchangeWallet) Address() (string, error) {
	addr, err := btc.wallet.AddressPKH()
	return addr.String(), err
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (btc *ExchangeWallet) PayFee(address string, regFee uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, regFee, btc.feeRateWithFallback(), false)
	if err != nil {
		btc.log.Errorf("PayFee error address = '%s', fee = %d: %v", address, regFee, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent, nil), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) Withdraw(address string, value uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(), true)
	if err != nil {
		btc.log.Errorf("Withdraw error address = '%s', fee = %d: %v", address, value, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent, nil), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (btc *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) send(address string, val uint64, feeRate uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	txHash, err := btc.wallet.SendToAddress(address, val, feeRate, subtract)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("SendToAddress error: %v", err)
	}
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, 0, asset.CoinNotFoundError
		}
		return nil, 0, 0, fmt.Errorf("failed to fetch transaction after send: %v", err)
	}
	for _, details := range tx.Details {
		if details.Address == address {
			return txHash, details.Vout, toSatoshi(details.Amount), nil
		}
	}
	return nil, 0, 0, fmt.Errorf("failed to locate transaction vout")
}

// Confirmations gets the number of confirmations for the specified coin ID.
// The coin must be known to the wallet, but need not be unspent.
func (btc *ExchangeWallet) Confirmations(id dex.Bytes) (uint32, error) {
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		if isTxNotFoundErr(err) {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// run pings for new blocks and runs the tipChange callback function when the
// block changes.
func (btc *ExchangeWallet) run(ctx context.Context) {
	h, err := btc.node.GetBestBlockHash()
	if err != nil {
		btc.tipChange(fmt.Errorf("error initializing best block for %s: %v", btc.symbol, err))
	}
	tipHash := *h
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h, err := btc.node.GetBestBlockHash()
			if err != nil {
				btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
				continue
			}
			if *h != tipHash {
				tipHash = *h
				btc.tipChange(nil)
			}
		case <-ctx.Done():
			return
		}
	}
}

// convertCoin converts the asset.Coin to an output.
func (btc *ExchangeWallet) convertCoin(coin asset.Coin) (*output, error) {
	op, _ := coin.(*output)
	if op != nil {
		return op, nil
	}
	txHash, vout, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, coin.Value(), coin.Redeem()), nil
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change.
func (btc *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx, addr btcutil.Address,
	totalIn, totalOut, feeRate uint64) (*wire.MsgTx, *output, []byte, error) {
	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	sigCycles := 1
	msgTx, err := btc.wallet.SignTx(baseTx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("signing error: %v", err)
	}
	size := msgTx.SerializeSize()
	minFee := feeRate * uint64(size)
	remaining := totalIn - totalOut
	if minFee > remaining {
		return nil, nil, nil, fmt.Errorf("not enough funds to cover minimum fee rate. %d < %d",
			totalIn, minFee+totalOut)
	}

	// Create a change output.
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating change script: %v", err)
	}
	changeIdx := len(baseTx.TxOut)
	changeOutput := wire.NewTxOut(int64(remaining-minFee), changeScript)

	// If the change is not dust, recompute the signed txn size and iterate on
	// the fees vs. change amount.
	changeAdded := !dexbtc.IsDust(changeOutput, feeRate)
	if changeAdded {
		// Add the change output.
		size0 := baseTx.SerializeSize()
		baseTx.AddTxOut(changeOutput)
		changeSize := baseTx.SerializeSize() - size0 // may be dexbtc.P2WPKHOutputSize
		btc.log.Debugf("Change output size = %d, addr = %s", changeSize, addr.String())

		size += changeSize
		fee := feeRate * uint64(size)
		changeOutput.Value = int64(remaining - fee)

		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]bool{}
		for {
			// Sign the transaction with the change output and compute new size.
			sigCycles++
			msgTx, err = btc.wallet.SignTx(baseTx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("signing error: %v", err)
			}
			size = msgTx.SerializeSize() // recompute the size with new tx signature
			reqFee := feeRate * uint64(size)
			if reqFee > remaining {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d",
					totalIn, totalOut, reqFee, fee)
				return nil, nil, nil, fmt.Errorf("change error")
			}
			if fee == reqFee || (fee > reqFee && tried[reqFee]) {
				// If a lower fee appears available, but it's already been attempted and
				// had a longer serialized size, the current fee is likely as good as
				// it gets.
				break
			}

			// We must have some room for improvement.
			tried[fee] = true
			fee = reqFee
			changeOutput.Value = int64(remaining - fee)
			if dexbtc.IsDust(changeOutput, feeRate) {
				// Another condition that should be impossible, but check anyway in case
				// the maximum fee was underestimated causing the first check to be
				// missed.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d",
					totalIn, totalOut, reqFee, fee)
				return nil, nil, nil, fmt.Errorf("dust error")
			}
			continue
		}

		totalOut += uint64(changeOutput.Value)
	}

	fee := totalIn - totalOut
	actualFeeRate := fee / uint64(size)
	checkHash := msgTx.TxHash()
	btc.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, checkHash, feeRate, actualFeeRate, fee, size, changeAdded)

	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, nil, err
	}
	if *txHash != checkHash {
		return nil, nil, nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", checkHash, *txHash)
	}

	var change *output
	if changeAdded {
		btc.addChange(txHash.String(), uint32(changeIdx))
		change = newOutput(btc.node, txHash, uint32(changeIdx), uint64(changeOutput.Value), nil)
	} else {
		changeScript = nil
	}
	return msgTx, change, changeScript, nil
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (btc *ExchangeWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address) (sig, pubkey []byte, err error) {
	privKey, err := btc.wallet.PrivKeyForAddress(addr.String())
	if err != nil {
		return nil, nil, err
	}
	sig, err = txscript.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, privKey)
	if err != nil {
		return nil, nil, err
	}
	return sig, privKey.PubKey().SerializeCompressed(), nil
}

// Combines the RPC type with the spending input information.
type compositeUTXO struct {
	txHash       *chainhash.Hash
	vout         uint32
	address      string
	redeemScript []byte
	amount       uint64
	input        *dexbtc.SpendInfo
}

// spendableUTXOs filters the RPC utxos for those that are spendable with with
// regards to the DEX's configuration, and considered safe to spend according to
// confirmations and coin source. The UTXOs will be sorted by ascending value.
func (btc *ExchangeWallet) spendableUTXOs(confs uint32) ([]*compositeUTXO, map[string]*compositeUTXO, uint64, error) {
	unspents, err := btc.wallet.ListUnspent()
	if err != nil {
		return nil, nil, 0, err
	}
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	var sum uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	utxoMap := make(map[string]*compositeUTXO, len(unspents))
	for _, txout := range unspents {
		if txout.Confirmations >= confs && txout.Safe {
			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, btc.chainParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error reading asset info: %v", err)
			}
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %v", err)
			}
			utxo := &compositeUTXO{
				txHash:       txHash,
				vout:         txout.Vout,
				address:      txout.Address,
				redeemScript: txout.RedeemScript,
				amount:       toSatoshi(txout.Amount),
				input:        nfo,
			}
			utxos = append(utxos, utxo)
			utxoMap[outpointID(txout.TxID, txout.Vout)] = utxo
			sum += toSatoshi(txout.Amount)
		}
	}
	return utxos, utxoMap, sum, nil
}

// lockedSats is the total value of locked outputs, as locked with LockUnspent.
func (btc *ExchangeWallet) lockedSats() (uint64, error) {
	lockedOutpoints, err := btc.wallet.ListLockUnspent()
	if err != nil {
		return 0, err
	}
	var sum uint64
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for _, outPoint := range lockedOutpoints {
		opID := outpointID(outPoint.TxID, outPoint.Vout)
		utxo, found := btc.fundingCoins[opID]
		if found {
			sum += utxo.amount
			continue
		}
		txHash, err := chainhash.NewHashFromStr(outPoint.TxID)
		if err != nil {
			return 0, err
		}
		txOut, err := btc.node.GetTxOut(txHash, outPoint.Vout, true)
		if err != nil {
			return 0, err
		}
		if txOut == nil {
			// Must be spent now?
			btc.log.Debugf("ignoring output from listlockunspent that wasn't found with gettxout. %s", opID)
			continue
		}
		sum += toSatoshi(txOut.Value)
	}
	return sum, nil
}

// addChange adds the output to the list of DEX-trade change outputs. These
// outputs are tracked because the client may wish to exempt change outputs from
// funding coin confirmation requirements.
func (btc *ExchangeWallet) addChange(txid string, vout uint32) {
	btc.changeMtx.Lock()
	defer btc.changeMtx.Unlock()
	btc.tradeChange[outpointID(txid, vout)] = time.Now()
}

// isDEXChange checks whether the specified output is change from a DEX trade.
func (btc *ExchangeWallet) isDEXChange(txid string, vout uint32) bool {
	btc.changeMtx.RLock()
	_, found := btc.tradeChange[outpointID(txid, vout)]
	btc.changeMtx.RUnlock()
	return found
}

// Convert the BTC value to satoshi.
func toSatoshi(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// blockHeader is a partial btcjson.GetBlockHeaderVerboseResult with mediantime
// included.
type blockHeader struct {
	Hash          string `json:"hash"`
	Confirmations int64  `json:"confirmations"`
	Height        int32  `json:"height"`
	Time          int64  `json:"time"`
	MedianTime    int64  `json:"mediantime"`
}

// getBlockHeader gets the block header for the specified block hash.
func (btc *ExchangeWallet) getBlockHeader(blockHash string) (*blockHeader, error) {
	blkHeader := new(blockHeader)
	err := btc.wallet.call(methodGetBlockHeader, anylist{blockHash, true}, blkHeader)
	if err != nil {
		return nil, err
	}
	return blkHeader, nil
}

// verboseBlockTxs is a partial btcjson.GetBlockVerboseResult with
// key "rawtx" -> "tx".
type verboseBlockTxs struct {
	Hash   string                `json:"hash"`
	Height uint64                `json:"height"`
	Tx     []btcjson.TxRawResult `json:"tx"`
}

// getVerboseBlockTxs gets a list of TxRawResult for a block. The
// rpcclient.Client's GetBlockVerboseTx appears to be broken with the current
// version of bitcoind. Though it's not a wallet method, it uses the wallet's
// RPC call method for convenience.
func (btc *ExchangeWallet) getVerboseBlockTxs(blockID string) (*verboseBlockTxs, error) {
	blk := new(verboseBlockTxs)
	// verbosity = 2 -> verbose transactions
	err := btc.wallet.call(methodGetBlockVerboseTx, anylist{blockID, 2}, blk)
	if err != nil {
		return nil, err
	}
	return blk, nil
}

// getVersion gets the current BTC network and protocol versions.
func (btc *ExchangeWallet) getVersion() (uint64, uint64, error) {
	r := &struct {
		Version         uint64 `json:"version"`
		ProtocolVersion uint64 `json:"protocolversion"`
	}{}
	err := btc.wallet.call(methodGetNetworkInfo, nil, r)
	if err != nil {
		return 0, 0, err
	}
	return r.Version, r.ProtocolVersion, nil
}

// toCoinID converts the tx hash and vout to a coin ID, as a []byte.
func toCoinID(txHash *chainhash.Hash, vout uint32) []byte {
	coinID := make([]byte, chainhash.HashSize+4)
	copy(coinID[:chainhash.HashSize], txHash[:])
	binary.BigEndian.PutUint32(coinID[chainhash.HashSize:], vout)
	return coinID
}

// decodeCoinID decodes the coin ID into a tx hash and a vout.
func decodeCoinID(coinID dex.Bytes) (*chainhash.Hash, uint32, error) {
	if len(coinID) != 36 {
		return nil, 0, fmt.Errorf("coin ID wrong length. expected 36, got %d", len(coinID))
	}
	var txHash chainhash.Hash
	copy(txHash[:], coinID[:32])
	return &txHash, binary.BigEndian.Uint32(coinID[32:]), nil
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *btcjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == btcjson.ErrRPCInvalidAddressOrKey
}
