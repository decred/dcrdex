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
	// Use RawRequest to get the verbose block header for a blockhash.
	methodGetBlockHeader = "getblockheader"
	// Use RawRequest to get the verbose block with verbose txs, as the btcd
	// rpcclient.Client's GetBlockVerboseTx appears to be busted.
	methodGetBlockVerboseTx = "getblock"
	methodGetNetworkInfo    = "getnetworkinfo"
	// BipID is the BIP-0044 asset ID.
	BipID = 0

	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee = 100
	// defaultRedeemConfTarget is the default redeem transaction confirmation
	// target in blocks used by estimatesmartfee to get the optimal fee for a
	// redeem transaction.
	defaultRedeemConfTarget = 2

	minNetworkVersion  = 190000
	minProtocolVersion = 70015

	// splitTxBaggage is the total number of additional bytes associated with
	// using a split transaction to fund a swap.
	splitTxBaggage = dexbtc.MinimumTxOverhead + dexbtc.RedeemP2PKHInputSize + 2*dexbtc.P2PKHOutputSize
	// splitTxBaggageSegwit it the analogue of splitTxBaggage for segwit.
	// We include the 2 bytes for marker and flag.
	splitTxBaggageSegwit = dexbtc.MinimumTxOverhead + 2*dexbtc.P2WPKHOutputSize +
		dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4)
)

var (
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker = time.Second
	configOpts  = []*asset.ConfigOption{
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
			Key:          "rpcbind",
			DisplayName:  "JSON-RPC Address",
			Description:  "<addr> or <addr>:<port> (default 'localhost')",
			DefaultValue: "127.0.0.1",
		},
		{
			Key:          "rpcport",
			DisplayName:  "JSON-RPC Port",
			Description:  "Port for RPC connections (if not set in rpcbind)",
			DefaultValue: "8332",
		},
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Bitcoin's 'fallbackfee' rate. Units: BTC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:          "redeemconftarget",
			DisplayName:  "Redeem confirmation target",
			Description:  "The target number of blocks for the redeem transaction to get a confirmation. Used to set the transaction's fee rate. (default: 2 blocks)",
			DefaultValue: defaultRedeemConfTarget,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Bitcoin wallet.
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
	WalletCFG          *asset.WalletConfig
	MinNetworkVersion  uint64
	WalletInfo         *asset.WalletInfo
	Symbol             string
	Logger             dex.Logger
	Network            dex.Network
	ChainParams        *chaincfg.Params
	Ports              dexbtc.NetPorts
	DefaultFallbackFee uint64 // sats/byte
	// LegacyBalance is for clones that don't yet support the 'getbalances' RPC
	// call.
	LegacyBalance bool
	// If segwit is false, legacy addresses and contracts will be used. This
	// setting must match the configuration of the server's asset backend.
	Segwit bool
}

// outPoint is the hash and output index of a transaction output.
type outPoint struct {
	txHash chainhash.Hash
	vout   uint32
}

// newOutPoint is the constructor for a new outPoint.
func newOutPoint(txHash *chainhash.Hash, vout uint32) outPoint {
	return outPoint{
		txHash: *txHash,
		vout:   vout,
	}
}

// String is a string representation of the outPoint.
func (pt *outPoint) String() string {
	return pt.txHash.String() + ":" + strconv.Itoa(int(pt.vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	pt    outPoint
	value uint64
	node  rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64) *output {
	return &output{
		pt:    newOutPoint(txHash, vout),
		value: value,
		node:  node,
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
	txOut, err := op.node.GetTxOut(op.txHash(), op.vout(), true)
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
	return toCoinID(op.txHash(), op.vout())
}

// String is a string representation of the coin.
func (op *output) String() string {
	return op.pt.String()
}

// txHash returns the pointer of the wire.OutPoint's Hash.
func (op *output) txHash() *chainhash.Hash {
	return &op.pt.txHash
}

// vout returns the wire.OutPoint's Index.
func (op *output) vout() uint32 {
	return op.pt.vout
}

// wireOutPoint creates and returns a new *wire.OutPoint for the output.
func (op *output) wireOutPoint() *wire.OutPoint {
	return wire.NewOutPoint(op.txHash(), op.vout())
}

// auditInfo is information about a swap contract on that blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
// auditInfo satisfies the asset.AuditInfo interface.
type auditInfo struct {
	output     *output
	recipient  btcutil.Address
	contract   []byte
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

// Contract is the contract script.
func (ci *auditInfo) Contract() dex.Bytes {
	return ci.contract
}

// SecretHash is the contract's secret hash.
func (ci *auditInfo) SecretHash() dex.Bytes {
	return ci.secretHash
}

// swapReceipt is information about a swap contract that was broadcast by this
// wallet. Satisfies the asset.Receipt interface.
type swapReceipt struct {
	output     *output
	contract   []byte
	expiration time.Time
}

// Expiration is the time that the contract will expire, allowing the user to
// issue a refund transaction. Part of the asset.Receipt interface.
func (r *swapReceipt) Expiration() time.Time {
	return r.expiration
}

// Contract is the contract script. Part of the asset.Receipt interface.
func (r *swapReceipt) Contract() dex.Bytes {
	return r.contract
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
	tipChange         func(error)
	minNetworkVersion uint64
	fallbackFeeRate   uint64
	redeemConfTarget  uint64
	useSplitTx        bool
	useLegacyBalance  bool
	segwit            bool

	tipMtx     sync.RWMutex
	currentTip *block

	// Coins returned by Fund are cached for quick reference.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*utxo

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[outPoint]*findRedemptionReq
}

type block struct {
	height int64
	hash   string
}

// findRedemptionReq represents a request to find a contract's redemption,
// which is added to the findRedemptionQueue with the contract outpoint as
// key.
type findRedemptionReq struct {
	contractHash []byte
	resultChan   chan *findRedemptionResult
}

// findRedemptionResult models the result of a find redemption attempt.
type findRedemptionResult struct {
	RedemptionCoinID dex.Bytes
	Secret           dex.Bytes
	Err              error
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
		WalletCFG:          cfg,
		MinNetworkVersion:  minNetworkVersion,
		WalletInfo:         WalletInfo,
		Symbol:             "btc",
		Logger:             logger,
		Network:            network,
		ChainParams:        params,
		Ports:              dexbtc.RPCPorts,
		DefaultFallbackFee: defaultFee,
		Segwit:             true,
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
	// If set in the user config, the fallback fee will be in conventional units
	// per kB, e.g. BTC/kB. Translate that to sats/B.
	fallbackFeesPerByte := toSatoshi(btcCfg.FallbackFeeRate / 1000)
	if fallbackFeesPerByte == 0 {
		fallbackFeesPerByte = cfg.DefaultFallbackFee
	}
	cfg.Logger.Tracef("Fallback fees set at %d %s/vbyte", fallbackFeesPerByte, cfg.WalletInfo.Units)

	redeemConfTarget := btcCfg.RedeemConfTarget
	if redeemConfTarget == 0 {
		redeemConfTarget = defaultRedeemConfTarget
	}
	cfg.Logger.Tracef("Redeem conf target set to %d blocks", redeemConfTarget)

	return &ExchangeWallet{
		node:                node,
		wallet:              newWalletClient(node, cfg.Segwit, cfg.ChainParams),
		symbol:              cfg.Symbol,
		chainParams:         cfg.ChainParams,
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		fundingCoins:        make(map[outPoint]*utxo),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		fallbackFeeRate:     fallbackFeesPerByte,
		redeemConfTarget:    redeemConfTarget,
		useSplitTx:          btcCfg.UseSplitTx,
		useLegacyBalance:    cfg.LegacyBalance,
		segwit:              cfg.Segwit,
		walletInfo:          cfg.WalletInfo,
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
		return nil, fmt.Errorf("reported node version %d is less than minimum %d", netVer, btc.minNetworkVersion)
	}
	if codeVer < minProtocolVersion {
		return nil, fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion)
	}
	// Initialize the best block.
	h, err := btc.node.GetBestBlockHash()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for %s: %v", btc.symbol, err)
	}
	btc.tipMtx.Lock()
	btc.currentTip, err = btc.blockFromHash(h.String())
	btc.tipMtx.Unlock()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for %s: %v", btc.symbol, err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.run(ctx)
		btc.shutdown()
	}()
	return &wg, nil
}

func (btc *ExchangeWallet) shutdown() {
	// Close all open channels for contract redemption searches
	// to prevent leakages and ensure goroutines that are started
	// to wait on these channels end gracefully.
	btc.findRedemptionMtx.Lock()
	for contractOutpoint, req := range btc.findRedemptionQueue {
		close(req.resultChan)
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()
}

// Balance returns the total available funds in the wallet. Part of the
// asset.Wallet interface.
func (btc *ExchangeWallet) Balance() (*asset.Balance, error) {
	if btc.useLegacyBalance {
		return btc.legacyBalance()
	}
	balances, err := btc.wallet.Balances()
	if err != nil {
		return nil, err
	}
	locked, err := btc.lockedSats()
	if err != nil {
		return nil, err
	}

	return &asset.Balance{
		Available: toSatoshi(balances.Mine.Trusted) - locked,
		Immature:  toSatoshi(balances.Mine.Immature + balances.Mine.Untrusted),
		Locked:    locked,
	}, nil
}

// legacyBalance is used for clones that are < node version 0.18 and so don't
// have 'getbalances'.
func (btc *ExchangeWallet) legacyBalance() (*asset.Balance, error) {
	walletInfo, err := btc.wallet.GetWalletInfo()
	if err != nil {
		return nil, fmt.Errorf("(legacy) GetWalletInfo error: %w", err)
	}

	locked, err := btc.lockedSats()
	if err != nil {
		return nil, fmt.Errorf("(legacy) lockedSats error: %w", err)
	}

	return &asset.Balance{
		Available: toSatoshi(walletInfo.Balance+walletInfo.UnconfirmedBalance) - locked,
		Immature:  toSatoshi(walletInfo.ImmatureBalance),
		Locked:    locked,
	}, nil
}

// FeeRate returns the current optimal fee rate in sat / byte.
func (btc *ExchangeWallet) feeRate(confTarget uint64) (uint64, error) {
	feeResult, err := btc.node.EstimateSmartFee(int64(confTarget), &btcjson.EstimateModeEconomical)
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
func (btc *ExchangeWallet) feeRateWithFallback(confTarget uint64) uint64 {
	feeRate, err := btc.feeRate(confTarget)
	if err != nil {
		feeRate = btc.fallbackFeeRate
		btc.log.Warnf("Unable to get optimal fee rate, using fallback of %d: %v",
			btc.fallbackFeeRate, err)
	}
	return feeRate
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// The returned []dex.Bytes contains the redeem scripts for the selected coins.
// Equal number of coins and redeemed scripts must be returned. A nil or empty
// dex.Bytes should be appended to the redeem scripts collection for coins with
// no redeem script.
func (btc *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	btc.log.Debugf("Attempting to fund order for %d %s, maxFeeRate = %d, max swaps = %d",
		ord.Value, btc.walletInfo.Units, ord.DEXConfig.MaxFeeRate, ord.MaxSwapCount)

	if ord.Value == 0 {
		return nil, nil, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, fmt.Errorf("cannot fund a zero-lot order")
	}

	btc.fundingMtx.Lock()         // before getting spendable utxos from wallet
	defer btc.fundingMtx.Unlock() // after we update the map and lock in the wallet
	// Now that we allow funding with 0 conf UTXOs, some more logic could be
	// used out of caution, including preference for >0 confs.
	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing unspent outputs: %v", err)
	}
	if avail < ord.Value {
		return nil, nil, fmt.Errorf("insufficient funds. %.8f requested, %.8f available",
			btcutil.Amount(ord.Value).ToBTC(), btcutil.Amount(avail).ToBTC())
	}
	var sum uint64
	var size uint32
	var coins asset.Coins
	var redeemScripts []dex.Bytes
	var spents []*output
	fundingCoins := make(map[outPoint]*utxo)

	// TODO: For the chained swaps, make sure that contract outputs are P2WSH,
	// and that change outputs that fund further swaps are P2WPKH.

	isEnoughWith := func(unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFunds(ord.Value, uint64(size+unspent.input.VBytes()), ord.MaxSwapCount, ord.DEXConfig)
		return sum+unspent.amount >= reqFunds
	}

	addUTXO := func(unspent *compositeUTXO) {
		v := unspent.amount
		op := newOutput(btc.node, unspent.txHash, unspent.vout, v)
		coins = append(coins, op)
		redeemScripts = append(redeemScripts, unspent.redeemScript)
		spents = append(spents, op)
		size += unspent.input.VBytes()
		fundingCoins[op.pt] = unspent.utxo
		sum += v
	}

out:
	for {
		// If there are none left, we don't have enough.
		reqFunds := calc.RequiredOrderFunds(ord.Value, uint64(size), ord.MaxSwapCount, ord.DEXConfig)
		fees := reqFunds - ord.Value
		if len(utxos) == 0 {
			return nil, nil, fmt.Errorf("not enough to cover requested funds (%d) + fees (%d) = %d",
				ord.Value, fees, reqFunds)
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

	if btc.useSplitTx && !ord.Immediate {
		splitCoins, split, err := btc.split(ord.Value, ord.MaxSwapCount, spents, uint64(size), fundingCoins, ord.DEXConfig)
		if err != nil {
			return nil, nil, err
		} else if split {
			return splitCoins, []dex.Bytes{nil}, nil // no redeem script required for split tx output
		} else {
			return splitCoins, redeemScripts, nil // splitCoins == coins
		}
	}

	btc.log.Infof("Funding %d %s order with coins %v worth %d",
		ord.Value, btc.walletInfo.Units, coins, sum)

	err = btc.wallet.LockUnspent(false, spents)
	if err != nil {
		return nil, nil, fmt.Errorf("LockUnspent error: %v", err)
	}

	for pt, utxo := range fundingCoins {
		btc.fundingCoins[pt] = utxo
	}

	return coins, redeemScripts, nil
}

// split will send a split transaction and return the sized output. If the
// split transaction is determined to be un-economical, it will not be sent,
// there is no error, and the input coins will be returned unmodified, but an
// info message will be logged. The returned bool indicates if a split tx was
// sent (true) or if the original coins were returned unmodified (false).
//
// A split transaction nets additional network bytes consisting of
// - overhead from 1 transaction
// - 1 extra signed p2wpkh-spending input. The split tx has the fundingCoins as
//   inputs now, but we'll add the input that spends the sized coin that will go
//   into the first swap
// - 2 additional p2wpkh outputs for the split tx sized output and change
//
// If the fees associated with this extra baggage are more than the excess
// amount that would be locked if a split transaction were not used, then the
// split transaction is pointless. This might be common, for instance, if an
// order is canceled partially filled, and then the remainder resubmitted. We
// would already have an output of just the right size, and that would be
// recognized here.
func (btc *ExchangeWallet) split(value uint64, lots uint64, outputs []*output, inputsSize uint64, fundingCoins map[outPoint]*utxo, nfo *dex.Asset) (asset.Coins, bool, error) {
	var err error
	defer func() {
		if err != nil {
			return
		}
		for pt, fCoin := range fundingCoins {
			btc.fundingCoins[pt] = fCoin
		}
		err = btc.wallet.LockUnspent(false, outputs)
		if err != nil {
			btc.log.Errorf("error locking unspent outputs: %v", err)
		}
	}()

	// Calculate the extra fees associated with the additional inputs, outputs,
	// and transaction overhead, and compare to the excess that would be locked.
	var swapInputSize uint64 = dexbtc.RedeemP2PKHInputSize
	baggage := nfo.MaxFeeRate * splitTxBaggage
	if btc.segwit {
		baggage = nfo.MaxFeeRate * splitTxBaggageSegwit
		swapInputSize = dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4)
	}

	var coinSum uint64
	coins := make(asset.Coins, 0, len(outputs))
	for _, op := range outputs {
		coins = append(coins, op)
		coinSum += op.value
	}

	excess := coinSum - calc.RequiredOrderFunds(value, inputsSize, lots, nfo)
	if baggage > excess {
		btc.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. "+
			"%d > %d", baggage, excess)
		btc.log.Infof("Funding %d %s order with coins %v worth %d",
			value, btc.walletInfo.Units, coins, coinSum)
		return coins, false, nil
	}

	// Use an internal address for the sized output.
	addr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, false, fmt.Errorf("error creating split transaction address: %v", err)
	}

	reqFunds := calc.RequiredOrderFunds(value, swapInputSize, lots, nfo)

	baseTx, _, _, err := btc.fundedTx(coins)
	splitScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, false, fmt.Errorf("error creating split tx script: %v", err)
	}
	baseTx.AddTxOut(wire.NewTxOut(int64(reqFunds), splitScript))

	// Grab a change address.
	changeAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, false, fmt.Errorf("error creating change address: %v", err)
	}

	feeRate := btc.feeRateWithFallback(1) // these must fund swaps, so don't under-pay (is this an issue with no fundconf requirement?)
	if feeRate > nfo.MaxFeeRate {
		feeRate = nfo.MaxFeeRate
	}

	// Sign, add change, and send the transaction.
	msgTx, _, _, err := btc.sendWithReturn(baseTx, changeAddr, coinSum, reqFunds, feeRate)
	if err != nil {
		return nil, false, err
	}
	txHash := msgTx.TxHash()

	op := newOutput(btc.node, &txHash, 0, reqFunds)

	// Need to save one funding coin (in the deferred function).
	fundingCoins = map[outPoint]*utxo{op.pt: {
		txHash:  op.txHash(),
		vout:    op.vout(),
		address: addr.String(),
		amount:  reqFunds,
	}}

	btc.log.Infof("Funding %d %s order with split output coin %v from original coins %v",
		value, btc.walletInfo.Units, op, coins)
	btc.log.Infof("Sent split transaction %s to accommodate swap of size %d + fees = %d",
		op.txHash(), value, reqFunds)

	// Assign to coins so the deferred function will lock the output.
	outputs = []*output{op}
	return asset.Coins{op}, true, nil
}

// ReturnCoins unlocks coins. This would be used in the case of a canceled or
// partially filled order. Part of the asset.Wallet interface.
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
		delete(btc.fundingCoins, op.pt)
	}
	return btc.wallet.LockUnspent(true, ops)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (btc *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[outPoint]bool)
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock() // stay locked until we update the map at the end
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		pt := newOutPoint(txHash, vout)
		fundingCoin, found := btc.fundingCoins[pt]
		if found {
			coins = append(coins, newOutput(btc.node, txHash, vout, fundingCoin.amount))
			continue
		}
		notFound[pt] = true
	}
	if len(notFound) == 0 {
		return coins, nil
	}

	// Check locked outputs for not found coins.
	lockedOutpoints, err := btc.wallet.ListLockUnspent()
	if err != nil {
		return nil, err
	}
	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid from rpc server %s: %v", rpcOP.TxID, err)
		}
		pt := newOutPoint(txHash, rpcOP.Vout)
		if !notFound[pt] {
			continue
		}
		txOut, err := btc.node.GetTxOut(txHash, rpcOP.Vout, true)
		if err != nil {
			return nil, fmt.Errorf("gettxout error for locked outpoint %v: %v", pt.String(), err)
		}
		var address string
		if len(txOut.ScriptPubKey.Addresses) > 0 {
			address = txOut.ScriptPubKey.Addresses[0]
		}
		utxo := &utxo{
			txHash:  txHash,
			vout:    rpcOP.Vout,
			address: address,
			amount:  toSatoshi(txOut.Value),
		}
		coin := newOutput(btc.node, txHash, rpcOP.Vout, toSatoshi(txOut.Value))
		coins = append(coins, coin)
		btc.fundingCoins[pt] = utxo
		delete(notFound, pt)
		if len(notFound) == 0 {
			return coins, nil
		}
	}

	// Some funding coins still not found after checking locked outputs.
	// Check wallet unspent outputs as last resort. Lock the coins if found.
	_, utxoMap, _, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, err
	}
	coinsToLock := make([]*output, 0, len(notFound))
	for pt := range notFound {
		utxo, found := utxoMap[pt]
		if !found {
			return nil, fmt.Errorf("funding coin not found: %s", pt.String())
		}
		btc.fundingCoins[pt] = utxo.utxo
		coin := newOutput(btc.node, utxo.txHash, utxo.vout, utxo.amount)
		coins = append(coins, coin)
		coinsToLock = append(coinsToLock, coin)
		delete(notFound, pt)
	}
	btc.log.Debugf("Locking funding coins that were unlocked %v", coinsToLock)
	err = btc.wallet.LockUnspent(false, coinsToLock)
	if err != nil {
		return nil, err
	}

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

// fundedTx creates and returns a new MsgTx with the provided coins as inputs.
func (btc *ExchangeWallet) fundedTx(coins asset.Coins) (*wire.MsgTx, uint64, []outPoint, error) {
	baseTx := wire.NewMsgTx(wire.TxVersion)
	var totalIn uint64
	// Add the funding utxos.
	pts := make([]outPoint, 0, len(coins))
	for _, coin := range coins {
		op, err := btc.convertCoin(coin)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("error converting coin: %v", err)
		}
		if op.value == 0 {
			return nil, 0, nil, fmt.Errorf("zero-valued output detected for %s:%d", op.txHash(), op.vout())
		}
		totalIn += op.value
		txIn := wire.NewTxIn(op.wireOutPoint(), []byte{}, nil)
		baseTx.AddTxIn(txIn)
		pts = append(pts, op.pt)
	}
	return baseTx, totalIn, pts, nil
}

// Swap sends the swaps in a single transaction and prepares the receipts. The
// Receipts returned can be used to refund a failed transaction. The Input coins
// are NOT manually unlocked because they're auto-unlocked when the transaction
// is broadcasted.
func (btc *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	contracts := make([][]byte, 0, len(swaps.Contracts))
	var totalOut uint64
	// Start with an empty MsgTx.
	baseTx, totalIn, pts, err := btc.fundedTx(swaps.Inputs)
	if err != nil {
		return nil, nil, 0, err
	}
	// Add the contract outputs.
	// TODO: Make P2WSH contract and P2WPKH change outputs instead of
	// legacy/non-segwit swap contracts pkScripts.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address that will receive the refund if the contract is
		// abandoned.
		revokeAddr, err := btc.externalAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating revocation address: %v", err)
		}
		// Create the contract, a P2SH redeem script.
		contractScript, err := dexbtc.MakeContract(contract.Address, revokeAddr.String(),
			contract.SecretHash, int64(contract.LockTime), btc.segwit, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("unable to create pubkey script for address %s: %v", contract.Address, err)
		}
		contracts = append(contracts, contractScript)

		// Make the P2SH address and pubkey script.
		scriptAddr, err := btc.scriptHashAddress(contractScript)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error encoding script address: %v", err)
		}

		pkScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating pubkey script: %v", err)
		}

		// Add the transaction output.
		txOut := wire.NewTxOut(int64(contract.Value), pkScript)
		baseTx.AddTxOut(txOut)
	}
	if totalIn < totalOut {
		return nil, nil, 0, fmt.Errorf("unfunded contract. %d < %d", totalIn, totalOut)
	}

	// Ensure we have enough outputs before broadcasting.
	swapCount := len(swaps.Contracts)
	if len(baseTx.TxOut) < swapCount {
		return nil, nil, 0, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}

	// Grab a change address.
	changeAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change address: %v", err)
	}

	// Sign, add change, and send the transaction.
	msgTx, change, fees, err := btc.sendWithReturn(baseTx, changeAddr, totalIn, totalOut, swaps.FeeRate)
	if err != nil {
		return nil, nil, 0, err
	}

	// Prepare the receipts.
	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			output:     newOutput(btc.node, &txHash, uint32(i), contract.Value),
			contract:   contracts[i],
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
		btc.fundingCoins[change.pt] = &utxo{
			txHash:  change.txHash(),
			vout:    change.vout(),
			address: changeAddr.String(),
			amount:  change.value,
		}
	}

	// Delete the UTXOs from the cache.
	for _, pt := range pts {
		delete(btc.fundingCoins, pt)
	}

	return receipts, changeCoin, fees, nil
}

// Redeem sends the redemption transaction, completing the atomic swap.
func (btc *ExchangeWallet) Redeem(redemptions []*asset.Redemption) ([]dex.Bytes, asset.Coin, uint64, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx(wire.TxVersion)
	var totalIn uint64
	var contracts [][]byte
	var addresses []btcutil.Address
	var values []uint64
	for _, r := range redemptions {
		cinfo, ok := r.Spends.(*auditInfo)
		if !ok {
			return nil, nil, 0, fmt.Errorf("Redemption contract info of wrong type")
		}
		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		contract := r.Spends.Contract()
		_, receiver, _, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error extracting swap addresses: %v", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, 0, fmt.Errorf("secret hash mismatch")
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, contract)
		txIn := wire.NewTxIn(cinfo.output.wireOutPoint(), nil, nil)
		// Enable locktime
		// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
		txIn.Sequence = wire.MaxTxInSequenceNum - 1
		msgTx.AddTxIn(txIn)
		values = append(values, cinfo.output.value)
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := dexbtc.MsgTxVBytes(msgTx)
	if btc.segwit {
		// Add the marker and flag weight here.
		witnessVBytes := (dexbtc.RedeemSwapSigScriptSize*uint64(len(redemptions)) + 2 + 3) / 4
		size += witnessVBytes + dexbtc.P2WPKHOutputSize
	} else {
		size += dexbtc.RedeemSwapSigScriptSize*uint64(len(redemptions)) + dexbtc.P2PKHOutputSize
	}

	feeRate := btc.feeRateWithFallback(btc.redeemConfTarget)
	fee := feeRate * size
	if fee > totalIn {
		return nil, nil, 0, fmt.Errorf("redeem tx not worth the fees")
	}
	// Send the funds back to the exchange wallet.
	redeemAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change script: %v", err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, feeRate) {
		return nil, nil, 0, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)

	if btc.segwit {
		sigHashes := txscript.NewTxSigHashes(msgTx)
		for i, r := range redemptions {
			contract := contracts[i]
			redeemSig, redeemPubKey, err := btc.createWitnessSig(msgTx, i, contract, addresses[i], values[i], sigHashes)
			if err != nil {
				return nil, nil, 0, err
			}
			msgTx.TxIn[i].Witness = dexbtc.RedeemP2WSHContract(contract, redeemSig, redeemPubKey, r.Secret)
		}
	} else {
		for i, r := range redemptions {
			contract := contracts[i]
			redeemSig, redeemPubKey, err := btc.createSig(msgTx, i, contract, addresses[i])
			if err != nil {
				return nil, nil, 0, err
			}
			msgTx.TxIn[i].SignatureScript, err = dexbtc.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
			if err != nil {
				return nil, nil, 0, err
			}
		}
	}

	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, 0, err
	}
	if *txHash != checkHash {
		return nil, nil, 0, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	// Log the change output.
	coinIDs := make([]dex.Bytes, 0, len(redemptions))
	for i := range redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}
	return coinIDs, newOutput(btc.node, txHash, 0, uint64(txOut.Value)), fee, nil
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
	utxo := btc.fundingCoins[op.pt]
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
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
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
	var contractHash []byte
	if btc.segwit {
		if scriptClass != txscript.WitnessV0ScriptHashTy {
			return nil, fmt.Errorf("unexpected script class. expected %s, got %s",
				txscript.WitnessV0ScriptHashTy, scriptClass)
		}
		h := sha256.Sum256(contract)
		contractHash = h[:]
	} else {
		if scriptClass != txscript.ScriptHashTy {
			return nil, fmt.Errorf("unexpected script class. expected %s, got %s",
				txscript.ScriptHashTy, scriptClass)
		}
		// Compare the contract hash to the P2SH address.
		contractHash = btcutil.Hash160(contract)
	}
	// These last two checks are probably overkill.
	if numReq != 1 {
		return nil, fmt.Errorf("unexpected number of signatures expected for P2SH script: %d", numReq)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}

	addr := addrs[0]
	if !bytes.Equal(contractHash, addr.ScriptAddress()) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addr.ScriptAddress())
	}
	return &auditInfo{
		output:     newOutput(btc.node, txHash, vout, toSatoshi(txOut.Value)),
		recipient:  receiver,
		contract:   contract,
		secretHash: secretHash,
		expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (btc *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %v", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	bestBlockHash, err := btc.node.GetBestBlockHash()
	if err != nil {
		return false, time.Time{}, fmt.Errorf("get best block hash error: %v", err)
	}
	bestBlockHeader, err := btc.getBlockHeader(bestBlockHash.String())
	if err != nil {
		return false, time.Time{}, fmt.Errorf("get best block header error: %v", err)
	}
	bestBlockMedianTime := time.Unix(bestBlockHeader.MedianTime, 0).UTC()
	return bestBlockMedianTime.After(contractExpiry), contractExpiry, nil
}

// FindRedemption watches for the input that spends the specified contract
// coin, and returns the spending input and the contract's secret key when it
// finds a spender.
// If the coin is unmined, an initial search goroutine is started to scan all
// mempool tx inputs in an attempt to find the input that spends the contract
// coin. If the contract is mined, the initial search goroutine scans every
// input of every block starting at the block in which the contract was mined
// up till the current best block, including mempool txs if redemption info is
// not found in the searched block txs.
// More search goroutines are started for every detected tip change, to handle
// cases where the contract is redeemed in a transaction mined after the current
// best block.
// When any of the search goroutines finds an input that spends this contract,
// the input and the contract's secret key are communicated to this method via
// a redemption result channel created specifically for this contract. This
// method waits on that channel before returning a response to the caller.
func (btc *ExchangeWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot decode contract coin id: %v", err)
	}

	contractOutpoint := newOutPoint(txHash, vout)
	resultChan, contractBlock, err := btc.queueFindRedemptionRequest(contractOutpoint)
	if err != nil {
		return nil, nil, err
	}

	// Run initial search for redemption. If the contract's spender is
	// not found in this initial search attempt, the contract's find
	// redemption request remains in the findRedemptionQueue to ensure
	// continued search for redemption on new or re-orged blocks.
	var wg sync.WaitGroup
	wg.Add(1)
	if contractBlock == nil {
		// Mempool contracts may only be spent by another mempool tx.
		go func() {
			defer wg.Done()
			btc.findRedemptionsInMempool([]outPoint{contractOutpoint})
		}()
	} else {
		// Begin searching for redemption for this contract from the block
		// in which this contract was mined up till the current best block.
		// Mempool txs will also be scanned if the contract's redemption is
		// not found in the block range.
		btc.tipMtx.RLock()
		bestBlock := btc.currentTip
		btc.tipMtx.RUnlock()
		go func() {
			defer wg.Done()
			btc.findRedemptionsInBlockRange(contractBlock, bestBlock, []outPoint{contractOutpoint})
		}()
	}

	var result *findRedemptionResult
	select {
	case result = <-resultChan:
	case <-ctx.Done():
	}

	// If this contract is still in the findRedemptionQueue, close the result
	// channel and remove from the queue to prevent further redemption search
	// attempts for this contract.
	btc.findRedemptionMtx.Lock()
	if req, exists := btc.findRedemptionQueue[contractOutpoint]; exists {
		close(req.resultChan)
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()

	// Don't abandon the goroutines even if context canceled.
	// findRedemptionsInTx will return when it fails to find the assigned
	// contract outpoint in the findRedemptionQueue.
	wg.Wait()

	// result would be nil if ctx is canceled or the result channel
	// is closed without data, which would happen if the redemption
	// search is aborted when this ExchangeWallet is shut down.
	if result != nil {
		return result.RedemptionCoinID, result.Secret, result.Err
	}
	return nil, nil, fmt.Errorf("aborted search for redemption of contract %s", contractOutpoint.String())
}

// queueFindRedemptionRequest extracts the contract hash and tx block (if mined)
// of the provided contract outpoint, creates a find redemption request and adds
// it to the findRedemptionQueue. Returns error if a find redemption request is
// already queued for the contract or if the contract hash or block info cannot
// be extracted.
func (btc *ExchangeWallet) queueFindRedemptionRequest(contractOutpoint outPoint) (chan *findRedemptionResult, *block, error) {
	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()

	if _, inQueue := btc.findRedemptionQueue[contractOutpoint]; inQueue {
		return nil, nil, fmt.Errorf("duplicate find redemption request for %s", contractOutpoint.String())
	}
	txHash, vout := contractOutpoint.txHash, contractOutpoint.vout
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, nil, asset.CoinNotFoundError
		}
		return nil, nil, fmt.Errorf("error finding transaction %s in wallet: %v", txHash, err)
	}
	msgTx := wire.NewMsgTx(wire.TxVersion)
	err = msgTx.Deserialize(bytes.NewBuffer(tx.Hex))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid contract tx hex %s: %v", tx.Hex.String(), err)
	}
	if int(vout) > len(msgTx.TxOut)-1 {
		return nil, nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
	}
	contractHash := dexbtc.ExtractScriptHash(msgTx.TxOut[vout].PkScript)
	if contractHash == nil {
		return nil, nil, fmt.Errorf("coin %s not a valid contract", contractOutpoint.String())
	}
	var contractBlock *block
	if tx.BlockHash != "" {
		contractBlock, err = btc.blockFromHash(tx.BlockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("getBlockHeader error for hash %s: %v", tx.BlockHash, err)
		}
	}

	resultChan := make(chan *findRedemptionResult, 1)
	btc.findRedemptionQueue[contractOutpoint] = &findRedemptionReq{
		contractHash: contractHash,
		resultChan:   resultChan,
	}
	return resultChan, contractBlock, nil
}

// findRedemptionsInMempool attempts to find spending info for the specified
// contracts by searching every input of all txs in the mempool.
// If spending info is found for any contract, the contract is purged from the
// findRedemptionQueue and the contract's secret (if successfully parsed) or any
// error that occurs during parsing is returned to the redemption finder via the
// registered result chan.
func (btc *ExchangeWallet) findRedemptionsInMempool(contractOutpoints []outPoint) {
	contractsCount := len(contractOutpoints)
	btc.log.Debugf("finding redemptions for %d contracts in mempool", contractsCount)

	var redemptionsFound int
	logAbandon := func(reason string) {
		// Do not remove the contracts from the findRedemptionQueue
		// as they could be subsequently redeemed in some mined tx(s),
		// which would be captured when a new tip is reported.
		if redemptionsFound > 0 {
			btc.log.Debugf("%d redemptions out of %d contracts found in mempool",
				redemptionsFound, contractsCount)
		}
		btc.log.Errorf("abandoning mempool redemption search for %d contracts because of %s",
			contractsCount-redemptionsFound, reason)
	}

	mempoolTxs, err := btc.node.GetRawMempool()
	if err != nil {
		logAbandon(fmt.Sprintf("error retrieving transactions: %v", err))
		return
	}

	for _, txHash := range mempoolTxs {
		tx, err := btc.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			logAbandon(fmt.Sprintf("getrawtransaction error for tx hash %v: %v", txHash, err))
			return
		}
		redemptionsFound += btc.findRedemptionsInTx("mempool", tx, contractOutpoints)
		if redemptionsFound == contractsCount {
			break
		}
	}

	btc.log.Debugf("%d redemptions out of %d contracts found in mempool",
		redemptionsFound, contractsCount)
}

// findRedemptionsInBlockRange attempts to find spending info for the specified
// contracts by searching every input of all txs in the provided block range.
// If spending info is found for any contract, the contract is purged from the
// findRedemptionQueue and the contract's secret (if successfully parsed) or any
// error that occurs during parsing is returned to the redemption finder via the
// registered result chan.
// Also checks mempool for potential redemptions if spending info is not found
// for any of these contracts in the specified block range.
func (btc *ExchangeWallet) findRedemptionsInBlockRange(startBlock, endBlock *block, contractOutpoints []outPoint) {
	contractsCount := len(contractOutpoints)
	btc.log.Debugf("finding redemptions for %d contracts in blocks %d - %d",
		contractsCount, startBlock.height, endBlock.height)

	nextBlockHash := startBlock.hash
	var lastScannedBlockHeight int64
	var redemptionsFound int

rangeBlocks:
	for nextBlockHash != "" && lastScannedBlockHeight < endBlock.height {
		blk, err := btc.getVerboseBlockTxs(nextBlockHash)
		if err != nil {
			// Redemption search for this set of contracts is compromised. Notify
			// the redemption finder(s) of this fatal error and cancel redemption
			// search for these contracts. The redemption finder(s) may re-call
			// btc.FindRedemption to restart find redemption attempts for any of
			// these contracts.
			err = fmt.Errorf("error fetching verbose block %s: %v", nextBlockHash, err)
			btc.fatalFindRedemptionsError(err, contractOutpoints)
			return
		}
		scanPoint := fmt.Sprintf("block %d", blk.Height)
		lastScannedBlockHeight = int64(blk.Height)
		for t := range blk.Tx {
			tx := &blk.Tx[t]
			redemptionsFound += btc.findRedemptionsInTx(scanPoint, tx, contractOutpoints)
			if redemptionsFound == contractsCount {
				break rangeBlocks
			}
		}
		nextBlockHash = blk.NextHash
	}

	btc.log.Debugf("%d redemptions out of %d contracts found in blocks %d - %d",
		redemptionsFound, contractsCount, startBlock.height, lastScannedBlockHeight)

	// Search for redemptions in mempool if there are yet unredeemed
	// contracts after searching this block range.
	pendingContractsCount := contractsCount - redemptionsFound
	if pendingContractsCount > 0 {
		btc.findRedemptionMtx.RLock()
		pendingContracts := make([]outPoint, 0, pendingContractsCount)
		for _, contractOutpoint := range contractOutpoints {
			if _, pending := btc.findRedemptionQueue[contractOutpoint]; pending {
				pendingContracts = append(pendingContracts, contractOutpoint)
			}
		}
		btc.findRedemptionMtx.RUnlock()
		btc.findRedemptionsInMempool(pendingContracts)
	}
}

// findRedemptionsInTx checks if any input of the passed tx spends any of the
// specified contract outpoints. If spending info is found for any contract, the
// contract's secret or any error encountered while trying to parse the secret
// is returned to the redemption finder via the registered result chan; and the
// contract is purged from the findRedemptionQueue.
// Returns the number of redemptions found.
func (btc *ExchangeWallet) findRedemptionsInTx(scanPoint string, tx *btcjson.TxRawResult, contractOutpoints []outPoint) int {
	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()

	contractsCount := len(contractOutpoints)
	var redemptionsFound int

	for inputIndex := 0; inputIndex < len(tx.Vin) && redemptionsFound < contractsCount; inputIndex++ {
		input := &tx.Vin[inputIndex]
		for _, contractOutpoint := range contractOutpoints {
			req, exists := btc.findRedemptionQueue[contractOutpoint]
			if !exists || input.Vout != contractOutpoint.vout || input.Txid != contractOutpoint.txHash.String() {
				continue // check this input against next contract
			}

			redemptionsFound++
			var secret []byte
			redeemTxHash, err := chainhash.NewHashFromStr(tx.Txid)
			var witness [][]byte
			for _, hexB := range input.Witness {
				var b []byte
				b, err = hex.DecodeString(hexB)
				if err != nil {
					break
				}
				witness = append(witness, b)
			}
			var sigScript []byte
			if input.ScriptSig != nil {
				sigScript, err = hex.DecodeString(input.ScriptSig.Hex)
			}

			txIn := wire.NewTxIn(new(wire.OutPoint), sigScript, witness)

			if err == nil {
				secret, err = dexbtc.FindKeyPush(txIn, req.contractHash, btc.segwit, btc.chainParams)
			}

			if err != nil {
				btc.log.Debugf("error parsing contract secret for %s from tx input %s:%d in %s: %v",
					contractOutpoint.String(), tx.Txid, inputIndex, scanPoint, err)
				req.resultChan <- &findRedemptionResult{
					Err: err,
				}
			} else {
				btc.log.Debugf("redemption for contract %s found in tx input %s:%d in %s",
					contractOutpoint.String(), tx.Txid, inputIndex, scanPoint)
				req.resultChan <- &findRedemptionResult{
					RedemptionCoinID: toCoinID(redeemTxHash, uint32(inputIndex)),
					Secret:           secret,
				}
			}
			close(req.resultChan)
			delete(btc.findRedemptionQueue, contractOutpoint)
			break // skip checking other contracts for this input and check next input
		}
	}

	return redemptionsFound
}

// fatalFindRedemptionsError should be called when an error occurs that prevents
// redemption search for the specified contracts from continuing reliably. The
// error will be propagated to the seeker(s) of these contracts' redemptions via
// the registered result channels and the contracts will be removed from the
// findRedemptionQueue.
func (btc *ExchangeWallet) fatalFindRedemptionsError(err error, contractOutpoints []outPoint) {
	btc.findRedemptionMtx.Lock()
	btc.log.Debugf("stopping redemption search for %d contracts in queue: %v", len(contractOutpoints), err)
	for _, contractOutpoint := range contractOutpoints {
		req, exists := btc.findRedemptionQueue[contractOutpoint]
		if !exists {
			continue
		}
		req.resultChan <- &findRedemptionResult{
			Err: err,
		}
		close(req.resultChan)
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()
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
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}

	// Create the transaction that spends the contract.
	feeRate := btc.feeRateWithFallback(2) // meh level urgency
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.

	size := dexbtc.MsgTxVBytes(msgTx)

	if btc.segwit {
		// Add the marker and flag weight too.
		witnessVBtyes := uint64((dexbtc.RefundSigScriptSize + 2 + 3) / 4)
		size += witnessVBtyes + dexbtc.P2WPKHOutputSize
	} else {
		size += dexbtc.RefundSigScriptSize + dexbtc.P2PKHOutputSize
	}

	fee := feeRate * size // TODO: use btc.FeeRate in caller and fallback to nfo.MaxFeeRate
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

	if btc.segwit {
		sigHashes := txscript.NewTxSigHashes(msgTx)
		refundSig, refundPubKey, err := btc.createWitnessSig(msgTx, 0, contract, sender, val, sigHashes)
		if err != nil {
			return nil, fmt.Errorf("createWitnessSig: %v", err)
		}
		txIn.Witness = dexbtc.RefundP2WSHContract(contract, refundSig, refundPubKey)

	} else {
		refundSig, refundPubKey, err := btc.createSig(msgTx, 0, contract, sender)
		if err != nil {
			return nil, fmt.Errorf("createSig: %v", err)
		}
		txIn.SignatureScript, err = dexbtc.RefundP2SHContract(contract, refundSig, refundPubKey)
		if err != nil {
			return nil, fmt.Errorf("RefundP2SHContract: %v", err)
		}
	}
	// Send it.
	checkHash := msgTx.TxHash()
	refundHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, fmt.Errorf("SendRawTransaction: %v", err)
	}
	if *refundHash != checkHash {
		return nil, fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return toCoinID(refundHash, 0), nil
}

// Address returns a new external address from the wallet.
func (btc *ExchangeWallet) Address() (string, error) {
	addr, err := btc.externalAddress()
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (btc *ExchangeWallet) PayFee(address string, regFee uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, regFee, btc.feeRateWithFallback(1), false)
	if err != nil {
		btc.log.Errorf("PayFee error address = '%s', fee = %d: %v", address, regFee, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) Withdraw(address string, value uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(2), true)
	if err != nil {
		btc.log.Errorf("Withdraw error address = '%s', fee = %d: %v", address, value, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent), nil
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
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			btc.checkForNewBlocks()
		case <-ctx.Done():
			return
		}
	}
}

// checkForNewBlocks checks for new blocks. When a tip change is detected, the
// tipChange callback function is invoked and a goroutine is started to check
// if any contracts in the findRedemptionQueue are redeemed in the new blocks.
func (btc *ExchangeWallet) checkForNewBlocks() {
	newTipHash, err := btc.node.GetBestBlockHash()
	if err != nil {
		btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
		return
	}

	// This method is called frequently. Don't hold write lock
	// unless tip has changed.
	btc.tipMtx.RLock()
	sameTip := btc.currentTip.hash == newTipHash.String()
	btc.tipMtx.RUnlock()
	if sameTip {
		return
	}

	btc.tipMtx.Lock()
	defer btc.tipMtx.Unlock()

	newTip, err := btc.blockFromHash(newTipHash.String())
	if err != nil {
		btc.tipChange(fmt.Errorf("error setting new tip: %v", err))
		return
	}

	prevTip := btc.currentTip
	btc.currentTip = newTip
	btc.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTip.height, newTip.hash)
	btc.tipChange(nil)

	// Search for contract redemption in new blocks if there
	// are contracts pending redemption.
	btc.findRedemptionMtx.RLock()
	pendingContractsCount := len(btc.findRedemptionQueue)
	contractOutpoints := make([]outPoint, 0, pendingContractsCount)
	for contractOutpoint := range btc.findRedemptionQueue {
		contractOutpoints = append(contractOutpoints, contractOutpoint)
	}
	btc.findRedemptionMtx.RUnlock()
	if pendingContractsCount == 0 {
		return
	}

	// Use the previous tip hash to determine the starting point for
	// the redemption search. If there was a re-org, the starting point
	// would be the common ancestor of the previous tip and the new tip.
	// Otherwise, the starting point would be the block at previous tip
	// height + 1.
	var startPoint *block
	var startPointErr error
	prevTipBlock, err := btc.getBlockHeader(prevTip.hash)
	switch {
	case err != nil:
		startPointErr = fmt.Errorf("getBlockHeader error for prev tip hash %s: %v", prevTip.hash, err)
	case prevTipBlock.Confirmations < 0:
		// There's been a re-org, common ancestor will be height
		// plus negative confirmation e.g. 155 + (-3) = 152.
		reorgHeight := prevTipBlock.Height + prevTipBlock.Confirmations
		btc.log.Debugf("reorg detected from height %d to %d", reorgHeight, newTip.height)
		reorgHash, err := btc.node.GetBlockHash(reorgHeight)
		if err != nil {
			startPointErr = fmt.Errorf("getBlockHash error for reorg height %d: %v", reorgHeight, err)
		} else {
			startPoint = &block{hash: reorgHash.String(), height: reorgHeight}
		}
	case newTip.height-prevTipBlock.Height > 1:
		// 2 or more blocks mined since last tip, start at prevTip height + 1.
		afterPrivTip := prevTipBlock.Height + 1
		hashAfterPrevTip, err := btc.node.GetBlockHash(afterPrivTip)
		if err != nil {
			startPointErr = fmt.Errorf("getBlockHash error for height %d: %v", afterPrivTip, err)
		} else {
			startPoint = &block{hash: hashAfterPrevTip.String(), height: afterPrivTip}
		}
	default:
		// Just 1 new block since last tip report, search the lone block.
		startPoint = newTip
	}

	// Redemption search would be compromised if the starting point cannot
	// be determined, as searching just the new tip might result in blocks
	// being omitted from the search operation. If that happens, cancel all
	// find redemption requests in queue.
	if startPointErr != nil {
		btc.fatalFindRedemptionsError(fmt.Errorf("new blocks handler error: %v", startPointErr), contractOutpoints)
	} else {
		go btc.findRedemptionsInBlockRange(startPoint, newTip, contractOutpoints)
	}
}

func (btc *ExchangeWallet) blockFromHash(hash string) (*block, error) {
	blk, err := btc.getBlockHeader(hash)
	if err != nil {
		return nil, fmt.Errorf("getBlockHeader error for hash %s: %v", hash, err)
	}
	return &block{hash: hash, height: blk.Height}, nil
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
	return newOutput(btc.node, txHash, vout, coin.Value()), nil
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change.
func (btc *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx, addr btcutil.Address,
	totalIn, totalOut, feeRate uint64) (*wire.MsgTx, *output, uint64, error) {
	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	makeErr := func(s string, a ...interface{}) (*wire.MsgTx, *output, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	sigCycles := 1
	msgTx, err := btc.wallet.SignTx(baseTx)
	if err != nil {
		return makeErr("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
	}
	vSize := dexbtc.MsgTxVBytes(msgTx)
	minFee := feeRate * vSize
	remaining := totalIn - totalOut
	if minFee > remaining {
		return makeErr("not enough funds to cover minimum fee rate. %d < %d, raw tx: %x",
			totalIn, minFee+totalOut, btc.wireBytes(baseTx))
	}

	// Create a change output.
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return makeErr("error creating change script: %v", err)
	}
	changeFees := dexbtc.P2PKHOutputSize * feeRate
	if btc.segwit {
		changeFees = dexbtc.P2WPKHOutputSize * feeRate
	}
	changeIdx := len(baseTx.TxOut)
	changeOutput := wire.NewTxOut(int64(remaining-minFee-changeFees), changeScript)
	if changeFees+minFee > remaining { // Prevent underflow
		changeOutput.Value = 0
	}
	// If the change is not dust, recompute the signed txn size and iterate on
	// the fees vs. change amount.
	changeAdded := !dexbtc.IsDust(changeOutput, feeRate)
	if changeAdded {
		// Add the change output.
		vSize0 := dexbtc.MsgTxVBytes(baseTx)
		baseTx.AddTxOut(changeOutput)
		changeSize := dexbtc.MsgTxVBytes(baseTx) - vSize0 // may be dexbtc.P2WPKHOutputSize
		btc.log.Debugf("Change output size = %d, addr = %s", changeSize, addr.String())

		vSize += changeSize
		fee := feeRate * vSize
		changeOutput.Value = int64(remaining - fee)
		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]bool{}
		for {
			// Sign the transaction with the change output and compute new size.
			sigCycles++
			msgTx, err = btc.wallet.SignTx(baseTx)
			if err != nil {
				return makeErr("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
			}
			vSize = dexbtc.MsgTxVBytes(msgTx) // recompute the size with new tx signature
			reqFee := feeRate * vSize
			if reqFee > remaining {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x, vSize = %d, feeRate = %d",
					totalIn, totalOut, reqFee, fee, btc.wireBytes(msgTx), vSize, feeRate)
				return makeErr("change error")
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
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x",
					totalIn, totalOut, reqFee, fee, btc.wireBytes(msgTx))
				return makeErr("dust error")
			}
			continue
		}

		totalOut += uint64(changeOutput.Value)
	}

	fee := totalIn - totalOut
	actualFeeRate := fee / vSize
	checkHash := msgTx.TxHash()
	btc.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, checkHash, feeRate, actualFeeRate, fee, vSize, changeAdded)

	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return makeErr("sendrawtx error: %v, raw tx: %x", err, btc.wireBytes(msgTx))
	}
	if *txHash != checkHash {
		return makeErr("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %x", checkHash, *txHash, btc.wireBytes(msgTx))
	}

	var change *output
	if changeAdded {
		change = newOutput(btc.node, txHash, uint32(changeIdx), uint64(changeOutput.Value))
	}
	return msgTx, change, fee, nil
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

// createWitnessSig creates and returns a signature for the witness of a segwit
// input and the pubkey associated with the address.
func (btc *ExchangeWallet) createWitnessSig(tx *wire.MsgTx, idx int, pkScript []byte,
	addr btcutil.Address, val uint64, sigHashes *txscript.TxSigHashes) (sig, pubkey []byte, err error) {

	privKey, err := btc.wallet.PrivKeyForAddress(addr.String())
	if err != nil {
		return nil, nil, err
	}
	sig, err = txscript.RawTxInWitnessSignature(tx, sigHashes, idx, int64(val),
		pkScript, txscript.SigHashAll, privKey)

	if err != nil {
		return nil, nil, err
	}
	return sig, privKey.PubKey().SerializeCompressed(), nil
}

type utxo struct {
	txHash  *chainhash.Hash
	vout    uint32
	address string
	amount  uint64
}

// Combines utxo info with the spending input information.
type compositeUTXO struct {
	*utxo
	redeemScript []byte
	input        *dexbtc.SpendInfo
}

// spendableUTXOs filters the RPC utxos for those that are spendable with with
// regards to the DEX's configuration, and considered safe to spend according to
// confirmations and coin source. The UTXOs will be sorted by ascending value.
func (btc *ExchangeWallet) spendableUTXOs(confs uint32) ([]*compositeUTXO, map[outPoint]*compositeUTXO, uint64, error) {
	unspents, err := btc.wallet.ListUnspent()
	if err != nil {
		return nil, nil, 0, err
	}
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	var sum uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	utxoMap := make(map[outPoint]*compositeUTXO, len(unspents))
	for _, txout := range unspents {
		if txout.Confirmations >= confs && txout.Safe && txout.Spendable {
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %v", err)
			}

			// Guard against inconsistencies between the wallet's view of
			// spendable unlocked UTXOs and ExchangeWallet's. e.g. User manually
			// unlocked something or even restarted the wallet software.
			pt := newOutPoint(txHash, txout.Vout)
			if btc.fundingCoins[pt] != nil {
				btc.log.Warnf("Known order-funding coin %v returned by listunspent!", pt)
				// TODO: Consider relocking the coin in the wallet.
				//continue
			}

			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, btc.chainParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error reading asset info: %v", err)
			}

			utxo := &compositeUTXO{
				utxo: &utxo{
					txHash:  txHash,
					vout:    txout.Vout,
					address: txout.Address,
					amount:  toSatoshi(txout.Amount),
				},
				redeemScript: txout.RedeemScript,
				input:        nfo,
			}
			utxos = append(utxos, utxo)
			utxoMap[pt] = utxo
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
	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return 0, err
		}
		pt := newOutPoint(txHash, rpcOP.Vout)
		utxo, found := btc.fundingCoins[pt]
		if found {
			sum += utxo.amount
			continue
		}
		txOut, err := btc.node.GetTxOut(txHash, rpcOP.Vout, true)
		if err != nil {
			return 0, err
		}
		if txOut == nil {
			// Must be spent now?
			btc.log.Debugf("ignoring output from listlockunspent that wasn't found with gettxout. %s", pt)
			continue
		}
		sum += toSatoshi(txOut.Value)
	}
	return sum, nil
}

// wireBytes dumps the serialized transaction bytes.
func (btc *ExchangeWallet) wireBytes(tx *wire.MsgTx) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, tx.SerializeSize()))
	err := tx.Serialize(buf)
	// wireBytes is just used for logging, and a serialization error is
	// extremely unlikely, so just log the error and return the nil bytes.
	if err != nil {
		btc.log.Errorf("error serializing %s transaction: %v", btc.symbol, err)
		return nil
	}
	return buf.Bytes()
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
	Height        int64  `json:"height"`
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
	Hash     string                `json:"hash"`
	Height   uint64                `json:"height"`
	NextHash string                `json:"nextblockhash"`
	Tx       []btcjson.TxRawResult `json:"tx"`
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

// externalAddress will return a new address for public use.
func (btc *ExchangeWallet) externalAddress() (btcutil.Address, error) {
	if btc.segwit {
		return btc.wallet.AddressWPKH()
	}
	return btc.wallet.AddressPKH()
}

// hashContract hashes the contract for use in a p2sh or p2wsh pubkey script.
// The hash function used depends on whether the wallet is configured for
// segwit. Non-segwit uses Hash160, segwit uses SHA256.
func (btc *ExchangeWallet) hashContract(contract []byte) []byte {
	if btc.segwit {
		h := sha256.Sum256(contract) // BIP141
		return h[:]
	}
	return btcutil.Hash160(contract) // BIP16
}

// scriptHashAddress returns a new p2sh or p2wsh address, depending on whether
// the wallet is configured for segwit.
func (btc *ExchangeWallet) scriptHashAddress(contract []byte) (btcutil.Address, error) {
	if btc.segwit {
		return btcutil.NewAddressWitnessScriptHash(btc.hashContract(contract), btc.chainParams)
	}
	return btcutil.NewAddressScriptHash(contract, btc.chainParams)

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
