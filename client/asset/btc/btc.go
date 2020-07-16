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
	// BipID is the BIP-0044 asset ID.
	BipID = 0
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee         = 100
	minNetworkVersion  = 190000
	minProtocolVersion = 70015
	// splitTxBaggage is the total number of additional bytes associated with
	// using a split transaction to fund a swap.
	splitTxBaggage = dexbtc.MimimumTxOverhead + dexbtc.RedeemP2WPKHInputSize + 2*dexbtc.P2PKHOutputSize
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
			Key:          fallbackFeeKey,
			DisplayName:  "Fallback fee rate",
			Description:  "Bitcoin's 'fallbackfee' rate. Units: BTC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
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
	pt     outPoint
	value  uint64
	redeem dex.Bytes
	node   rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64, redeem dex.Bytes) *output {
	return &output{
		pt:     newOutPoint(txHash, vout),
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

// Redeem is any known redeem script required to spend this output. Part of the
// asset.Coin interface.
func (op *output) Redeem() dex.Bytes {
	return op.redeem
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
	tipMtx            sync.RWMutex
	currentTipHash    string
	tipChange         func(error)
	minNetworkVersion uint64
	fallbackFeeRate   uint64
	useSplitTx        bool
	useLegacyBalance  bool

	// Coins returned by Fund are cached for quick reference and for cleanup on
	// shutdown.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*compositeUTXO

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[string]findRedemptionReq
}

type block struct {
	height int64
	hash   string
}

type findRedemptionReq struct {
	contractTxHash *chainhash.Hash
	contractTxOut  uint32
	contractHash   []byte
	contractBlock  *block // for mined contracts, nil for contracts stil in mempool
	resultChan     chan asset.FindRedemptionResult
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
	cfg.Logger.Tracef("fallback fees set at %d %s/byte", fallbackFeesPerByte, cfg.WalletInfo.Units)

	return &ExchangeWallet{
		node:                node,
		wallet:              newWalletClient(node, cfg.ChainParams),
		symbol:              cfg.Symbol,
		chainParams:         cfg.ChainParams,
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		fundingCoins:        make(map[outPoint]*compositeUTXO),
		findRedemptionQueue: make(map[string]findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		fallbackFeeRate:     fallbackFeesPerByte,
		useSplitTx:          btcCfg.UseSplitTx,
		useLegacyBalance:    cfg.LegacyBalance,
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.processFindRedemptionRequests(ctx)
	}()
	return &wg, nil
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
func (btc *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, error) {
	btc.log.Debugf("Attempting to fund order for %d based units of %s", ord.Value, btc.symbol)

	if ord.Value == 0 {
		return nil, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, fmt.Errorf("cannot fund a zero-lot order")
	}

	btc.fundingMtx.Lock()         // before getting spendable utxos from wallet
	defer btc.fundingMtx.Unlock() // after we update the map and lock in the wallet
	// Now that we allow funding with 0 conf UTXOs, some more logic could be
	// used out of caution, including preference for >0 confs.
	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, fmt.Errorf("error parsing unspent outputs: %v", err)
	}
	if avail < ord.Value {
		return nil, fmt.Errorf("insufficient funds. %.8f requested, %.8f available",
			btcutil.Amount(ord.Value).ToBTC(), btcutil.Amount(avail).ToBTC())
	}
	var sum uint64
	var size uint32
	var coins asset.Coins
	var spents []*output
	fundingCoins := make(map[outPoint]*compositeUTXO)

	// TODO: For the chained swaps, make sure that contract outputs are P2WSH,
	// and that change outputs that fund further swaps are P2WPKH.

	isEnoughWith := func(unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFunds(ord.Value, uint64(size+unspent.input.VBytes()), ord.MaxSwapCount, ord.DEXConfig)
		return sum+unspent.amount >= reqFunds
	}

	addUTXO := func(unspent *compositeUTXO) {
		v := unspent.amount
		op := newOutput(btc.node, unspent.txHash, unspent.vout, v, unspent.redeemScript)
		coins = append(coins, op)
		spents = append(spents, op)
		size += unspent.input.VBytes()
		fundingCoins[op.pt] = unspent
		sum += v
	}

out:
	for {
		// If there are none left, we don't have enough.
		reqFunds := calc.RequiredOrderFunds(ord.Value, uint64(size), ord.MaxSwapCount, ord.DEXConfig)
		fees := reqFunds - ord.Value
		if len(utxos) == 0 {
			return nil, fmt.Errorf("not enough to cover requested funds (%d) + fees (%d) = %d",
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

	btc.log.Debugf("funding %d %s order with coins %v worth %d", ord.Value, btc.walletInfo.Units, coins, sum)

	if btc.useSplitTx && !ord.Immediate {
		return btc.split(ord.Value, ord.MaxSwapCount, spents, uint64(size), fundingCoins, ord.DEXConfig)
	}

	err = btc.wallet.LockUnspent(false, spents)
	if err != nil {
		return nil, err
	}

	for pt, utxo := range fundingCoins {
		btc.fundingCoins[pt] = utxo
	}

	return coins, nil
}

// split will send a split transaction and return the sized output. If the
// split transaction is determined to be un-economical, it will not be sent,
// there is no error, and the input coins will be returned unmodified, but an
// info message will be logged.
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
func (btc *ExchangeWallet) split(value uint64, lots uint64, outputs []*output, inputsSize uint64, fundingCoins map[outPoint]*compositeUTXO, nfo *dex.Asset) (asset.Coins, error) {
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
	baggageFees := nfo.MaxFeeRate * splitTxBaggage

	var coinSum uint64
	coins := make(asset.Coins, 0, len(outputs))
	for _, op := range outputs {
		coins = append(coins, op)
		coinSum += op.value
	}

	excess := coinSum - calc.RequiredOrderFunds(value, inputsSize, lots, nfo)
	if baggageFees > excess {
		btc.log.Infof("skipping split transaction because cost is greater than potential over-lock. %d > %d", baggageFees, excess)
		return coins, nil
	}

	// Use an internal address for the sized output.
	addr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, fmt.Errorf("error creating split transaction address: %v", err)
	}

	reqFunds := calc.RequiredOrderFunds(value, dexbtc.RedeemP2WPKHInputSize, lots, nfo)

	baseTx, _, _, err := btc.fundedTx(coins)
	splitScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("error creating split tx script: %v", err)
	}
	baseTx.AddTxOut(wire.NewTxOut(int64(reqFunds), splitScript))

	// Grab a change address.
	changeAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, fmt.Errorf("error creating change address: %v", err)
	}

	// Sign, add change, and send the transaction.
	msgTx, _, _, err := btc.sendWithReturn(baseTx, changeAddr, coinSum, reqFunds, btc.feeRateWithFallback())
	if err != nil {
		return nil, err
	}
	txHash := msgTx.TxHash()

	op := newOutput(btc.node, &txHash, 0, reqFunds, nil)

	// Need to save one funding coin (in the deferred function).
	spendInfo, err := dexbtc.InputInfo(msgTx.TxOut[0].PkScript, nil, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error reading asset info: %v", err)
	}
	fundingCoins = map[outPoint]*compositeUTXO{op.pt: {
		txHash:       op.txHash(),
		vout:         op.vout(),
		address:      addr.String(),
		redeemScript: nil,
		amount:       reqFunds,
		input:        spendInfo,
	}}

	btc.log.Infof("sent split transaction %s to accommodate swap of size %d + fees = %d", op.txHash(), value, reqFunds)

	// Assign to coins so the deferred function will lock the output.
	outputs = []*output{op}
	return asset.Coins{op}, nil
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
	notFound := make(map[outPoint]struct{})
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
			coins = append(coins, newOutput(btc.node, txHash, vout, fundingCoin.amount, fundingCoin.redeemScript))
			continue
		}
		notFound[pt] = struct{}{}
	}

	if len(notFound) == 0 {
		return coins, nil
	}
	_, utxoMap, _, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, err
	}

	lockers := make([]*output, 0, len(ids))

	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		pt := newOutPoint(txHash, vout)
		utxo, found := utxoMap[pt]
		if !found {
			return nil, fmt.Errorf("funding coin %s not found", pt.String())
		}
		btc.fundingCoins[pt] = utxo
		coin := newOutput(btc.node, utxo.txHash, utxo.vout, utxo.amount, utxo.redeemScript)
		coins = append(coins, coin)
		lockers = append(lockers, coin)
		delete(notFound, pt)
		if len(notFound) == 0 {
			break
		}
	}
	if len(notFound) != 0 {
		ids := make([]string, 0, len(notFound))
		for pt := range notFound {
			ids = append(ids, pt.String())
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

// Swap sends the swap contracts and prepares the receipts.
func (btc *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, error) {
	contracts := make([][]byte, 0, len(swaps.Contracts))
	var totalOut uint64
	// Start with an empty MsgTx.
	baseTx, totalIn, pts, err := btc.fundedTx(swaps.Inputs)
	if err != nil {
		return nil, nil, err
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

		btc.fundingCoins[change.pt] = &compositeUTXO{
			txHash:       change.txHash(),
			vout:         change.vout(),
			address:      changeAddr.String(),
			redeemScript: nil,
			amount:       change.value,
			input:        nfo,
		}
	}

	// Delete the UTXOs from the cache.
	for _, pt := range pts {
		delete(btc.fundingCoins, pt)
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
		txIn := wire.NewTxIn(cinfo.output.wireOutPoint(), []byte{}, nil)
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
func (btc *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.chainParams)
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

// FindRedemption attempts to find the inputs that spend the specified coins,
// and return the secret key for each contract when it does.
// Every input of every block starting at the earliest mined contract block is
// scanned in a goroutine until the spending inputs are found or an error occurs.
// The result channel is used to notify the caller when a secret key is found for
// a contract or if an error occurs during the search.
//
// NOTE: FindRedemption is necessary to deal with the case of a maker
// redeeming but not forwarding their redemption information. The DEX does not
// monitor for this case. While it will result in the counter-party being
// penalized, the input still needs to be found so the swap can be completed.
// This could potentially be an expensive operation if performed long after
// the swap is broadcast. Realistically, though, the taker should start
// looking for the maker's redemption beginning at swapconf confirmations
// regardless of whether the server sends the 'redemption' message or not.
func (btc *ExchangeWallet) FindRedemption(coinIDs []dex.Bytes, resultChan chan asset.FindRedemptionResult) {
	handleError := func(coinID dex.Bytes, err error) {
		resultChan <- asset.FindRedemptionResult{
			ContractCoinID: coinID,
			Err:            err,
		}
	}

	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()
	for _, coinID := range coinIDs {
		txHash, vout, err := decodeCoinID(coinID)
		if err != nil {
			handleError(coinID, fmt.Errorf("cannot decode contract coin id: %v", err))
			continue
		}
		rawTx, err := btc.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			handleError(coinID, fmt.Errorf("error getting contract details: %v", err))
			continue
		}
		if int(vout) > len(rawTx.Vout)-1 {
			handleError(coinID, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash))
			continue
		}
		contractHash, err := dexbtc.ExtractContractHash(rawTx.Vout[vout].ScriptPubKey.Hex)
		if err != nil {
			handleError(coinID, err)
			continue
		}
		req := findRedemptionReq{
			contractTxHash: txHash,
			contractTxOut:  vout,
			contractHash:   contractHash,
			resultChan:     resultChan,
		}
		if rawTx.BlockHash != "" {
			blk, err := btc.getBlockHeader(rawTx.BlockHash)
			if err != nil {
				handleError(coinID, fmt.Errorf("getBlockHeader error for hash %q: %v", rawTx.BlockHash, err))
				continue
			}
			req.contractBlock = &block{hash: rawTx.BlockHash, height: blk.Height}
		}
		btc.findRedemptionQueue[coinID.String()] = req
	}
}

// processFindRedemptionRequests attempts to find spending info for contracts
// by scanning every input of every block starting from the block in which the
// contract was mined (and mempool txs for unmined contracts).
// If the spending input for any contract is not found after searching the best
// block, this method waits for new blocks to search to ensure that redemption
// info is found for all contracts, parsing and returning the secret from the
// spending input's sig.
func (btc *ExchangeWallet) processFindRedemptionRequests(ctx context.Context) {
	processingQueue := make(map[string]findRedemptionReq)

	// errorAll is used if/when this contract monitoring goroutine
	// hits an unexpected error and cannot continue to reliably
	// track contract redemption. The error encountered is sent to
	// all callers via the registered channel for find redemption
	// results and the requests removed from the processing queue.
	// If a client wishes to repeat find redemption attempts for any
	// of the deleted requests, client may re-call btc.FindRedemption.
	errorAll := func(err error) {
		for _, req := range processingQueue {
			req.resultChan <- asset.FindRedemptionResult{
				ContractCoinID: toCoinID(req.contractTxHash, req.contractTxOut),
				Err:            err,
			}
		}
		processingQueue = make(map[string]findRedemptionReq)
	}

	// findRedeemsInTx checks if any input of the provided tx spends any
	// contract from the processing queue. If any such spending is found,
	// the spent contract is deleted from the processing queue, the spending
	// input's sig is parsed to retreive the secret and the secret is sent
	// to the caller via the channel for registered find redemption results.
	// If an error is encountered while trying to extract the secret, the
	// error is communicated to the requester via the registered result chan.
	findRedeemsInTx := func(tx *btcjson.TxRawResult) {
		for i, input := range tx.Vin {
			for k, req := range processingQueue {
				if input.Txid != req.contractTxHash.String() || input.Vout != req.contractTxOut {
					// input doesn't spend this contract, check next contract.
					continue
				}
				// Found contract spender, delete from map and parse out secret.
				delete(processingQueue, k)
				var sigScript, secret []byte
				redeemTxHash, err := chainhash.NewHashFromStr(tx.Txid)
				if err == nil {
					sigScript, err = hex.DecodeString(input.ScriptSig.Hex)
				}
				if err == nil {
					secret, err = dexbtc.FindKeyPush(sigScript, req.contractHash, btc.chainParams)
				}
				if err != nil {
					req.resultChan <- asset.FindRedemptionResult{
						ContractCoinID: toCoinID(req.contractTxHash, req.contractTxOut),
						Err:            fmt.Errorf("error extracting key from %s:%d, %v", tx.Txid, i, err),
					}
				} else {
					req.resultChan <- asset.FindRedemptionResult{
						ContractCoinID:   toCoinID(req.contractTxHash, req.contractTxOut),
						RedemptionCoinID: toCoinID(redeemTxHash, uint32(i)),
						Secret:           secret,
						Err:              err,
					}
				}
				break // skip checking other contracts for this input and check next input
			}
		}
	}

	var lastSearchedBlockHeight int64 = -1
	var nextBlockToSearch *block

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Copy new requests over to processing queue and determine
		// lowest block hash to scan in this iteration.
		var newUnminedContracts bool
		btc.findRedemptionMtx.Lock()
		for k, req := range btc.findRedemptionQueue {
			// Repeated requests for contracts already in the processing
			// queue should not affect the nextBlockToSearch. We do not
			// want to revert to the block in which the contract was mined
			// as we would have searched that block upwards in previous
			// iterations.
			_, previouslyProcessed := processingQueue[k]
			processingQueue[k] = req
			if previouslyProcessed || req.contractBlock == nil {
				continue
			}
			if nextBlockToSearch == nil || req.contractBlock.height < nextBlockToSearch.height {
				nextBlockToSearch = req.contractBlock
			}
		}
		btc.findRedemptionQueue = make(map[string]findRedemptionReq)
		btc.findRedemptionMtx.Unlock()

		if len(processingQueue) == 0 {
			continue
		}

		if nextBlockToSearch == nil {
			// Must have exhausted all mined blocks in last loop run
			// or the processing queue has no mined contracts.
			// Either way, check if there's a new block that can be
			// searched.

			btc.tipMtx.RLock()
			currentTipHash := btc.currentTipHash
			btc.tipMtx.RUnlock()
			if currentTipHash == "" {
				continue
			}
			if lastSearchedBlockHeight == -1 {
				// No previous scan, start scanning at this current tip.
				nextBlockToSearch = &block{hash: currentTipHash} // don't really need the height to scan
			} else {
				// Check if this new block is more than 1 block ahead of the
				// last scanned block.
				latestBlock, err := btc.getBlockHeader(currentTipHash)
				if err != nil {
					errorAll(fmt.Errorf("unexpected getBlockHeader error for hash %q: %v", currentTipHash, err))
					continue
				}
				if latestBlock.Height == lastSearchedBlockHeight+1 {
					nextBlockToSearch = &block{hash: currentTipHash, height: latestBlock.Height}
				} else if latestBlock.Height > lastSearchedBlockHeight+1 {
					nextBlockHeight := lastSearchedBlockHeight + 1
					nextBlockHash, err := btc.node.GetBlockHash(nextBlockHeight)
					if err != nil {
						errorAll(fmt.Errorf("unexpected GetBlockHash error for height %d: %v", nextBlockHeight, err))
						continue
					}
					nextBlockToSearch = &block{hash: nextBlockHash.String(), height: nextBlockHeight}
				}
			}
		}

		if nextBlockToSearch != nil {
			blk, err := btc.getVerboseBlockTxs(nextBlockToSearch.hash)
			if err != nil {
				errorAll(fmt.Errorf("error fetching verbose block '%s': %v", nextBlockToSearch.hash, err))
				continue
			}
			for _, tx := range blk.Tx {
				findRedeemsInTx(&tx)
			}
			lastSearchedBlockHeight = int64(blk.Height)
			nextBlockHeight := lastSearchedBlockHeight + 1
			nextBlockHash, err := btc.node.GetBlockHash(nextBlockHeight)
			if err == nil {
				nextBlockToSearch = &block{hash: nextBlockHash.String(), height: nextBlockHeight}
			} else {
				nextBlockToSearch = nil
			}
		}

		// Newly reported mempool contracts may only have been spent
		// by another mempool tx, so check mempool for potential spends.
		if newUnminedContracts {
			mempoolTxs, err := btc.node.GetRawMempool()
			if err != nil {
				errorAll(fmt.Errorf("error retrieving mempool transactions"))
				continue
			}
			for _, txHash := range mempoolTxs {
				tx, err := btc.node.GetRawTransactionVerbose(txHash)
				if err != nil {
					errorAll(fmt.Errorf("error encountered retreving mempool transaction %s: %v", txHash, err))
					continue
				}
				findRedeemsInTx(tx)
			}
		}
	}
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
	btc.tipMtx.Lock()
	btc.currentTipHash = h.String()
	btc.tipMtx.Unlock()

	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			newTip, err := btc.node.GetBestBlockHash()
			if err != nil {
				btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
				continue
			}
			if newTip.String() != btc.currentTipHash {
				btc.tipMtx.Lock()
				btc.currentTipHash = newTip.String()
				btc.tipMtx.Unlock()
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
		return nil, nil, nil, fmt.Errorf("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
	}
	size := msgTx.SerializeSize()
	minFee := feeRate * uint64(size)
	remaining := totalIn - totalOut
	if minFee > remaining {
		return nil, nil, nil, fmt.Errorf("not enough funds to cover minimum fee rate. %d < %d, raw tx: %x",
			totalIn, minFee+totalOut, btc.wireBytes(baseTx))
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
				return nil, nil, nil, fmt.Errorf("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
			}
			size = msgTx.SerializeSize() // recompute the size with new tx signature
			reqFee := feeRate * uint64(size)
			if reqFee > remaining {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x",
					totalIn, totalOut, reqFee, fee, btc.wireBytes(msgTx))
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
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x",
					totalIn, totalOut, reqFee, fee, btc.wireBytes(msgTx))
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
		return nil, nil, nil, fmt.Errorf("sendrawtx error: %v, raw tx: %x", err, btc.wireBytes(msgTx))
	}
	if *txHash != checkHash {
		return nil, nil, nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %x", checkHash, *txHash, btc.wireBytes(msgTx))
	}

	var change *output
	if changeAdded {
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
				txHash:       txHash,
				vout:         txout.Vout,
				address:      txout.Address,
				redeemScript: txout.RedeemScript,
				amount:       toSatoshi(txout.Amount),
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
