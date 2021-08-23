// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/decred/dcrd/dcrjson/v4" // for dcrjson.RPCError returns from rpcclient
	"github.com/decred/dcrd/rpcclient/v7"
)

const (
	version = 0

	// Use RawRequest to get the verbose block header for a blockhash.
	methodGetBlockHeader = "getblockheader"
	// Use RawRequest to get the verbose block with verbose txs, as the btcd
	// rpcclient.Client's GetBlockVerboseTx appears to be busted.
	methodGetNetworkInfo    = "getnetworkinfo"
	methodGetBlockchainInfo = "getblockchaininfo"
	methodSendRawTx         = "sendrawtransaction"
	// BipID is the BIP-0044 asset ID.
	BipID = 0

	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee = 100
	// defaultFeeRateLimit is the default value for the feeratelimit.
	defaultFeeRateLimit = 1400
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
			Key:         "fallbackfee",
			DisplayName: "Fallback fee rate",
			Description: "The fee rate to use for fee payment and withdrawals when" +
				" estimatesmartfee is not available. Units: BTC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "redeemconftarget",
			DisplayName: "Redeem confirmation target",
			Description: "The target number of blocks for the redeem transaction " +
				"to be mined. Used to set the transaction's fee rate. " +
				"(default: 2 blocks)",
			DefaultValue: defaultRedeemConfTarget,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to " +
				"fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order " +
				"until the first swap contract is broadcast during match settlement, " +
				"or the order is canceled. This an extra transaction for which network " +
				"mining fees are paid. Used only for standing-type orders, e.g. limit " +
				"orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Bitcoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Bitcoin",
		Units:             "Satoshis",
		Version:           version,
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"),
		ConfigOpts:        configOpts,
	}
)

// TxInSigner is a transaction input signer.
type TxInSigner func(tx *wire.MsgTx, idx int, subScript []byte, hashType txscript.SigHashType, key *btcec.PrivateKey, val uint64) ([]byte, error)

// BTCCloneCFG holds clone specific parameters.
type BTCCloneCFG struct {
	WalletCFG           *asset.WalletConfig
	MinNetworkVersion   uint64
	WalletInfo          *asset.WalletInfo
	Symbol              string
	Logger              dex.Logger
	Network             dex.Network
	ChainParams         *chaincfg.Params
	Ports               dexbtc.NetPorts
	DefaultFallbackFee  uint64 // sats/byte
	DefaultFeeRateLimit uint64 // sats/byte
	// LegacyBalance is for clones that don't yet support the 'getbalances' RPC
	// call.
	LegacyBalance bool
	// If segwit is false, legacy addresses and contracts will be used. This
	// setting must match the configuration of the server's asset backend.
	Segwit bool
	// LegacyRawFeeLimit can be true if the RPC only supports the boolean
	// allowHighFees argument to the sendrawtransaction RPC.
	LegacyRawFeeLimit bool
	// AddressDecoder is an optional argument that can decode an address string
	// into btcutil.Address. If AddressDecoder is not supplied,
	// btcutil.DecodeAddress will be used.
	AddressDecoder dexbtc.AddressDecoder
	// ArglessChangeAddrRPC can be true if the getrawchangeaddress takes no
	// address-type argument.
	ArglessChangeAddrRPC bool
	// NonSegwitSigner can be true if the transaction signature hash data is not
	// the standard for non-segwit Bitcoin. If nil, txscript.
	NonSegwitSigner TxInSigner
	// FeeEstimator provides a way to get fees given an RawRequest-enabled
	// client and a confirmation target.
	FeeEstimator func(RawRequester, uint64) (uint64, error)
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
func (pt outPoint) String() string {
	return pt.txHash.String() + ":" + strconv.Itoa(int(pt.vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	pt    outPoint
	value uint64
}

// newOutput is the constructor for an output.
func newOutput(txHash *chainhash.Hash, vout uint32, value uint64) *output {
	return &output{
		pt:    newOutPoint(txHash, vout),
		value: value,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (op *output) Value() uint64 {
	return op.value
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

// auditInfo is information about a swap contract on that blockchain.
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
	output       *output
	contract     []byte
	signedRefund []byte
	expiration   time.Time
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

// SignedRefund is a signed refund script that can be used to return
// funds to the user in the case a contract expires.
func (r *swapReceipt) SignedRefund() dex.Bytes {
	return r.signedRefund
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
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect      int64
	node              Wallet
	walletInfo        *asset.WalletInfo
	chainParams       *chaincfg.Params
	log               dex.Logger
	symbol            string
	tipChange         func(error)
	minNetworkVersion uint64
	fallbackFeeRate   uint64
	feeRateLimit      uint64
	redeemConfTarget  uint64
	useSplitTx        bool
	useLegacyBalance  bool
	segwit            bool
	legacyRawFeeLimit bool
	signNonSegwit     TxInSigner
	estimateFee       func(RawRequester, uint64) (uint64, error)
	decodeAddr        dexbtc.AddressDecoder

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
	outPt        outPoint
	contract     []byte
	blockHash    *chainhash.Hash
	blockHeight  int32
	resultChan   chan *findRedemptionResult
	pkScript     []byte
	contractHash []byte
}

func (req *findRedemptionReq) fail(s string, a ...interface{}) {
	req.success(&findRedemptionResult{err: fmt.Errorf(s, a...)})

}

func (req *findRedemptionReq) success(res *findRedemptionResult) {
	select {
	case req.resultChan <- res:
	default:
		// In-case two separate threads find a result.
	}
}

// findRedemptionResult models the result of a find redemption attempt.
type findRedemptionResult struct {
	redemptionCoinID dex.Bytes
	secret           dex.Bytes
	err              error
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
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "btc",
		Logger:              logger,
		Network:             network,
		ChainParams:         params,
		Ports:               dexbtc.RPCPorts,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              true,
	}

	return BTCCloneWallet(cloneCFG)
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *BTCCloneCFG) (*ExchangeWallet, error) {
	// Read the configuration parameters
	btcCfg, err := dexbtc.LoadConfigFromSettings(cfg.WalletCFG.Settings,
		cfg.Symbol, cfg.Network, cfg.Ports)
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
		return nil, err
	}
	btc, err := newWallet(client, cfg, btcCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating %s ExchangeWallet: %v", cfg.Symbol,
			err)
	}

	return btc, nil
}

// newWallet creates the ExchangeWallet and starts the block monitor.
func newWallet(requester RawRequesterWithContext, cfg *BTCCloneCFG, btcCfg *dexbtc.Config) (*ExchangeWallet, error) {
	// If set in the user config, the fallback fee will be in conventional units
	// per kB, e.g. BTC/kB. Translate that to sats/byte.
	fallbackFeesPerByte := toSatoshi(btcCfg.FallbackFeeRate / 1000)
	if fallbackFeesPerByte == 0 {
		fallbackFeesPerByte = cfg.DefaultFallbackFee
	}
	cfg.Logger.Tracef("Fallback fees set at %d %s/vbyte",
		fallbackFeesPerByte, cfg.WalletInfo.Units)

	// If set in the user config, the fee rate limit will be in units of BTC/KB.
	// Convert to sats/byte & error if value is smaller than smallest unit.
	feesLimitPerByte := uint64(defaultFeeRateLimit)
	if btcCfg.FeeRateLimit > 0 {
		feesLimitPerByte = toSatoshi(btcCfg.FeeRateLimit / 1000)
		if feesLimitPerByte == 0 {
			return nil, fmt.Errorf("Fee rate limit is smaller than smallest unit: %v",
				btcCfg.FeeRateLimit)
		}
	}
	cfg.Logger.Tracef("Fees rate limit set at %d sats/byte", feesLimitPerByte)

	redeemConfTarget := btcCfg.RedeemConfTarget
	if redeemConfTarget == 0 {
		redeemConfTarget = defaultRedeemConfTarget
	}
	cfg.Logger.Tracef("Redeem conf target set to %d blocks", redeemConfTarget)

	addrDecoder := btcutil.DecodeAddress
	if cfg.AddressDecoder != nil {
		addrDecoder = cfg.AddressDecoder
	}

	nonSegwitSigner := rawTxInSig
	if cfg.NonSegwitSigner != nil {
		nonSegwitSigner = cfg.NonSegwitSigner
	}

	cl := newRPCClient(requester, cfg.Segwit, addrDecoder, cfg.ArglessChangeAddrRPC,
		cfg.LegacyRawFeeLimit, cfg.MinNetworkVersion, cfg.Logger.SubLogger("RPC"), cfg.ChainParams)

	w := &ExchangeWallet{
		node:                cl,
		symbol:              cfg.Symbol,
		chainParams:         cfg.ChainParams,
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		fundingCoins:        make(map[outPoint]*utxo),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		fallbackFeeRate:     fallbackFeesPerByte,
		feeRateLimit:        feesLimitPerByte,
		redeemConfTarget:    redeemConfTarget,
		useSplitTx:          btcCfg.UseSplitTx,
		useLegacyBalance:    cfg.LegacyBalance,
		segwit:              cfg.Segwit,
		legacyRawFeeLimit:   cfg.LegacyRawFeeLimit,
		signNonSegwit:       nonSegwitSigner,
		estimateFee:         cfg.FeeEstimator,
		decodeAddr:          addrDecoder,
		walletInfo:          cfg.WalletInfo,
	}

	if w.estimateFee == nil {
		w.estimateFee = w.feeRate
	}

	return w, nil
}

var _ asset.Wallet = (*ExchangeWallet)(nil)

// Info returns basic information about the wallet and asset.
func (btc *ExchangeWallet) Info() *asset.WalletInfo {
	return btc.walletInfo
}

// Net returns the ExchangeWallet's *chaincfg.Params. This is not part of the
// asset.Wallet interface, but is provided as a convenience for embedding types.
func (btc *ExchangeWallet) Net() *chaincfg.Params {
	return btc.chainParams
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (btc *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if err := btc.node.connect(ctx); err != nil {
		return nil, err
	}
	// Initialize the best block.
	h, err := btc.node.getBestBlockHash()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for %s: %w", btc.symbol, err)
	}
	// Check for method unknown error for feeRate method.
	_, err = btc.estimateFee(btc.node, 1)
	if isMethodNotFoundErr(err) {
		return nil, fmt.Errorf("fee estimation method not found. Are you configured for the correct RPC?")
	}

	btc.tipMtx.Lock()
	btc.currentTip, err = btc.blockFromHash(h.String())
	btc.tipMtx.Unlock()
	if err != nil {
		return nil, fmt.Errorf("error parsing best block for %s: %w", btc.symbol, err)
	}
	atomic.StoreInt64(&btc.tipAtConnect, btc.currentTip.height)
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
		req.fail("shutdown")
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()
}

// getBlockchainInfoResult models the data returned from the getblockchaininfo
// command.
type getBlockchainInfoResult struct {
	Blocks               int64  `json:"blocks"`
	Headers              int64  `json:"headers"`
	BestBlockHash        string `json:"bestblockhash"`
	InitialBlockDownload bool   `json:"initialblockdownload"`
}

// SyncStatus is information about the blockchain sync status.
func (btc *ExchangeWallet) SyncStatus() (bool, float32, error) {
	ss, err := btc.node.syncStatus()
	if err != nil {
		return false, 0, err
	}
	if ss.Syncing {
		ogTip := atomic.LoadInt64(&btc.tipAtConnect)
		totalToSync := ss.Target - int32(ogTip)
		var progress float32 = 1
		if totalToSync > 0 {
			progress = 1 - (float32(ss.Target-ss.Height) / float32(totalToSync))
		}
		return false, progress, nil
	}
	return true, 1, nil
}

// OwnsAddress indicates if an address belongs to the wallet.
func (btc *ExchangeWallet) OwnsAddress(address string) (bool, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return false, err
	}
	return btc.node.ownsAddress(addr)
}

// Balance returns the total available funds in the wallet. Part of the
// asset.Wallet interface.
func (btc *ExchangeWallet) Balance() (*asset.Balance, error) {
	if btc.useLegacyBalance {
		return btc.legacyBalance()
	}
	balances, err := btc.node.balances()
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
	cl, ok := btc.node.(*rpcClient)
	if !ok {
		return nil, fmt.Errorf("legacyBalance unimplemented for spv clients")
	}

	walletInfo, err := cl.GetWalletInfo()
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

// feeRate returns the current optimal fee rate in sat / byte using the
// estimatesmartfee RPC.
func (btc *ExchangeWallet) feeRate(_ RawRequester, confTarget uint64) (uint64, error) {
	feeResult, err := btc.node.estimateSmartFee(int64(confTarget), &btcjson.EstimateModeConservative)
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

type amount uint64

func (a amount) String() string {
	return strconv.FormatFloat(btcutil.Amount(a).ToBTC(), 'f', -1, 64) // dec, but no trailing zeros
}

// feeRateWithFallback attempts to get the optimal fee rate in sat / byte via
// FeeRate. If that fails, it will return the configured fallback fee rate.
func (btc *ExchangeWallet) feeRateWithFallback(confTarget, feeSuggestion uint64) uint64 {
	feeRate, err := btc.estimateFee(btc.node, confTarget)
	if err == nil {
		btc.log.Tracef("Obtained local estimate for %d-conf fee rate, %d", confTarget, feeRate)
		return feeRate
	}
	if feeSuggestion > 0 && feeSuggestion < btc.fallbackFeeRate && feeSuggestion < btc.feeRateLimit {
		btc.log.Tracef("feeRateWithFallback using caller's suggestion for %d-conf fee rate, %d. Local estimate unavailable (%q)",
			confTarget, feeSuggestion, err)
		return feeSuggestion
	}
	btc.log.Warnf("Unable to get optimal fee rate, using fallback of %d: %v",
		btc.fallbackFeeRate, err)
	return btc.fallbackFeeRate
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
// lotSize must not be zero and will cause a panic if so.
func (btc *ExchangeWallet) MaxOrder(lotSize, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	_, maxEst, err := btc.maxOrder(lotSize, feeSuggestion, nfo)
	return maxEst, err
}

// maxOrder gets the estimate for MaxOrder, and also returns the
// []*compositeUTXO to be used for further order estimation without additional
// calls to listunspent.
func (btc *ExchangeWallet) maxOrder(lotSize, feeSuggestion uint64, nfo *dex.Asset) (utxos []*compositeUTXO, est *asset.SwapEstimate, err error) {
	if lotSize == 0 {
		return nil, nil, errors.New("cannot divide by lotSize zero")
	}

	btc.fundingMtx.RLock()
	utxos, _, avail, err := btc.spendableUTXOs(0)
	btc.fundingMtx.RUnlock()
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}
	// Start by attempting max lots with a basic fee.
	basicFee := nfo.SwapSize * nfo.MaxFeeRate
	lots := avail / (lotSize + basicFee)
	for lots > 0 {
		est, _, err := btc.estimateSwap(lots, lotSize, feeSuggestion, utxos, nfo, btc.useSplitTx)
		// The only failure mode of estimateSwap -> btc.fund is when there is
		// not enough funds, so if an error is encountered, count down the lots
		// and repeat until we have enough.
		if err != nil {
			lots--
			continue
		}
		return utxos, est, nil
	}
	return utxos, &asset.SwapEstimate{}, nil
}

// PreSwap get order estimates based on the available funds and the wallet
// configuration.
func (btc *ExchangeWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	// Start with the maxOrder at the default configuration. This gets us the
	// utxo set, the network fee rate, and the wallet's maximum order size.
	// The utxo set can then be used repeatedly in estimateSwap at virtually
	// zero cost since there are no more RPC calls.
	// The utxo set is only used once right now, but when order-time options are
	// implemented, the utxos will be used to calculate option availability and
	// fees.
	utxos, maxEst, err := btc.maxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig)
	if err != nil {
		return nil, err
	}
	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	// Get the estimate for the requested number of lots.
	est, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, btc.useSplitTx)
	if err != nil {
		return nil, fmt.Errorf("estimation failed: %v", err)
	}

	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (btc *ExchangeWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, utxos []*compositeUTXO,
	nfo *dex.Asset, trySplit bool) (*asset.SwapEstimate, bool /*split used*/, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.amount
	}

	val := lots * lotSize

	sum, inputsSize, _, _, _, _, err := btc.fund(val, lots, utxos, nfo)
	if err != nil {
		return nil, false, err
	}

	reqFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, nfo.MaxFeeRate)
	maxFees := reqFunds - val

	estHighFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, feeSuggestion)
	estHighFees := estHighFunds - val

	estLowFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), 1, nfo.SwapSizeBase, nfo.SwapSize, feeSuggestion)
	if btc.segwit {
		estLowFunds += dexbtc.P2WSHOutputSize * (lots - 1) * feeSuggestion
	} else {
		estLowFunds += dexbtc.P2SHOutputSize * (lots - 1) * feeSuggestion
	}

	estLowFees := estLowFunds - val

	// Math for split transactions is a little different.
	if trySplit {
		_, extraMaxFees := btc.splitBaggageFees(nfo.MaxFeeRate)
		_, splitFees := btc.splitBaggageFees(feeSuggestion)

		if avail >= reqFunds+extraMaxFees {
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees + extraMaxFees,
				RealisticBestCase:  estLowFees + splitFees,
				RealisticWorstCase: estHighFees + splitFees,
				Locked:             val + maxFees + extraMaxFees,
			}, true, nil
		}
	}

	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              val,
		MaxFees:            maxFees,
		RealisticBestCase:  estLowFees,
		RealisticWorstCase: estHighFees,
		Locked:             sum,
	}, false, nil
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (btc *ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	feeRate := btc.feeRateWithFallback(btc.redeemConfTarget, req.FeeSuggestion)
	// Best is one transaction with req.Lots inputs and 1 output.
	var best uint64 = dexbtc.MinimumTxOverhead
	// Worst is req.Lots transactions, each with one input and one output.
	var worst uint64 = dexbtc.MinimumTxOverhead * req.Lots
	var inputSize, outputSize uint64
	if btc.segwit {
		// Add the marker and flag weight here.
		inputSize = dexbtc.TxInOverhead + (dexbtc.RedeemSwapSigScriptSize+2+3)/4
		outputSize = dexbtc.P2WPKHOutputSize

	} else {
		inputSize = dexbtc.TxInOverhead + dexbtc.RedeemSwapSigScriptSize
		outputSize = dexbtc.P2PKHOutputSize
	}
	best += inputSize*req.Lots + outputSize
	worst += (inputSize + outputSize) * req.Lots

	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticWorstCase: worst * feeRate,
			RealisticBestCase:  best * feeRate,
		},
	}, nil
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// The returned []dex.Bytes contains the redeem scripts for the selected coins.
// Equal number of coins and redeemed scripts must be returned. A nil or empty
// dex.Bytes should be appended to the redeem scripts collection for coins with
// no redeem script.
func (btc *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	ordValStr := amount(ord.Value).String()
	btc.log.Debugf("Attempting to fund order for %s %s, maxFeeRate = %d, max swaps = %d",
		ordValStr, btc.symbol, ord.DEXConfig.MaxFeeRate, ord.MaxSwapCount)

	if ord.Value == 0 {
		return nil, nil, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, fmt.Errorf("cannot fund a zero-lot order")
	}
	// Check wallets fee rate limit against server's max fee rate
	if btc.feeRateLimit < ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configued fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			btc.feeRateLimit)
	}

	btc.fundingMtx.Lock()         // before getting spendable utxos from wallet
	defer btc.fundingMtx.Unlock() // after we update the map and lock in the wallet

	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}
	if avail < ord.Value {
		return nil, nil, fmt.Errorf("insufficient funds. %s requested, %s available",
			ordValStr, amount(avail))
	}

	sum, size, coins, fundingCoins, redeemScripts, spents, err := btc.fund(ord.Value, ord.MaxSwapCount, utxos, ord.DEXConfig)
	if err != nil {
		return nil, nil, err
	}

	if btc.useSplitTx && !ord.Immediate {
		splitCoins, split, err := btc.split(ord.Value, ord.MaxSwapCount, spents,
			uint64(size), fundingCoins, ord.FeeSuggestion, ord.DEXConfig)
		if err != nil {
			return nil, nil, err
		} else if split {
			return splitCoins, []dex.Bytes{nil}, nil // no redeem script required for split tx output
		}
		return splitCoins, redeemScripts, nil // splitCoins == coins
	}

	btc.log.Infof("Funding %s %s order with coins %v worth %s",
		ordValStr, btc.symbol, coins, amount(sum))

	err = btc.node.lockUnspent(false, spents)
	if err != nil {
		return nil, nil, fmt.Errorf("LockUnspent error: %w", err)
	}

	for pt, utxo := range fundingCoins {
		btc.fundingCoins[pt] = utxo
	}

	return coins, redeemScripts, nil
}

func (btc *ExchangeWallet) fund(val, lots uint64, utxos []*compositeUTXO, nfo *dex.Asset) (
	sum uint64, size uint32, coins asset.Coins, fundingCoins map[outPoint]*utxo, redeemScripts []dex.Bytes, spents []*output, err error) {

	fundingCoins = make(map[outPoint]*utxo)

	isEnoughWith := func(unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFunds(val, uint64(size+unspent.input.VBytes()), lots, nfo)
		return sum+unspent.amount >= reqFunds
	}

	addUTXO := func(unspent *compositeUTXO) {
		v := unspent.amount
		op := newOutput(unspent.txHash, unspent.vout, v)
		coins = append(coins, op)
		redeemScripts = append(redeemScripts, unspent.redeemScript)
		spents = append(spents, op)
		size += unspent.input.VBytes()
		fundingCoins[op.pt] = unspent.utxo
		sum += v
	}

	tryUTXOs := func(minconf uint32) bool {
		sum, size = 0, 0
		coins, spents = nil, nil
		fundingCoins = make(map[outPoint]*utxo)

		okUTXOs := make([]*compositeUTXO, 0, len(utxos)) // over-allocate
		for _, cu := range utxos {
			if cu.confs >= minconf {
				okUTXOs = append(okUTXOs, cu)
			}
		}

		for {
			// If there are none left, we don't have enough.
			if len(okUTXOs) == 0 {
				return false
			}
			// On each loop, find the smallest UTXO that is enough.
			for _, txout := range okUTXOs {
				if isEnoughWith(txout) {
					addUTXO(txout)
					return true
				}
			}
			// No single UTXO was large enough. Add the largest (the last
			// output) and continue.
			addUTXO(okUTXOs[len(okUTXOs)-1])
			// Pop the utxo.
			okUTXOs = okUTXOs[:len(okUTXOs)-1]
		}
	}

	// First try with confs>0, falling back to allowing 0-conf outputs.
	if !tryUTXOs(1) {
		if !tryUTXOs(0) {
			return 0, 0, nil, nil, nil, nil, fmt.Errorf("not enough to cover requested funds (%s %s + tx fees). %s available",
				amount(val), btc.symbol, amount(sum))
		}
	}

	return
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
//   into the first swap if the split tx does not add excess baggage
// - 2 additional p2wpkh outputs for the split tx sized output and change
//
// If the fees associated with this extra baggage are more than the excess
// amount that would be locked if a split transaction were not used, then the
// split transaction is pointless. This might be common, for instance, if an
// order is canceled partially filled, and then the remainder resubmitted. We
// would already have an output of just the right size, and that would be
// recognized here.
func (btc *ExchangeWallet) split(value uint64, lots uint64, outputs []*output,
	inputsSize uint64, fundingCoins map[outPoint]*utxo, suggestedFeeRate uint64, nfo *dex.Asset) (asset.Coins, bool, error) {

	var err error
	defer func() {
		if err != nil {
			return
		}
		for pt, fCoin := range fundingCoins {
			btc.fundingCoins[pt] = fCoin
		}
		err = btc.node.lockUnspent(false, outputs)
		if err != nil {
			btc.log.Errorf("error locking unspent outputs: %v", err)
		}
	}()

	// Calculate the extra fees associated with the additional inputs, outputs,
	// and transaction overhead, and compare to the excess that would be locked.
	swapInputSize, baggage := btc.splitBaggageFees(nfo.MaxFeeRate)

	var coinSum uint64
	coins := make(asset.Coins, 0, len(outputs))
	for _, op := range outputs {
		coins = append(coins, op)
		coinSum += op.value
	}

	valueStr := amount(value).String()

	excess := coinSum - calc.RequiredOrderFundsAlt(value, inputsSize, lots, nfo.SwapSizeBase, nfo.SwapSize, nfo.MaxFeeRate)
	if baggage > excess {
		btc.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. "+
			"%s > %s", amount(baggage), amount(excess))
		btc.log.Infof("Funding %s %s order with coins %v worth %s",
			valueStr, btc.symbol, coins, amount(coinSum))
		return coins, false, nil // err==nil records and locks the provided fundingCoins in defer
	}

	// Use an internal address for the sized output.
	addr, err := btc.node.changeAddress()
	if err != nil {
		return nil, false, fmt.Errorf("error creating split transaction address: %w", err)
	}

	reqFunds := calc.RequiredOrderFundsAlt(value, swapInputSize, lots, nfo.SwapSizeBase, nfo.SwapSize, nfo.MaxFeeRate)

	baseTx, _, _, err := btc.fundedTx(coins)
	splitScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, false, fmt.Errorf("error creating split tx script: %w", err)
	}
	baseTx.AddTxOut(wire.NewTxOut(int64(reqFunds), splitScript))

	// Grab a change address.
	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, false, fmt.Errorf("error creating change address: %w", err)
	}

	if suggestedFeeRate > nfo.MaxFeeRate {
		return nil, false, fmt.Errorf("suggested fee is > the max fee rate")
	}
	if suggestedFeeRate > btc.feeRateLimit {
		return nil, false, fmt.Errorf("suggested fee is > our internal limit")
	}
	if suggestedFeeRate == 0 {
		suggestedFeeRate = btc.feeRateWithFallback(1, 0)
		// TODO
		// 1.0: Error when no suggestion.
		// return nil, false, fmt.Errorf("cannot do a split transaction without a fee rate suggestion from the server")
	}

	// Sign, add change, and send the transaction.
	msgTx, err := btc.sendWithReturn(baseTx, changeAddr, coinSum, reqFunds, suggestedFeeRate)
	if err != nil {
		return nil, false, err
	}
	txHash := msgTx.TxHash()

	op := newOutput(&txHash, 0, reqFunds)

	// Need to save one funding coin (in the deferred function).
	fundingCoins = map[outPoint]*utxo{op.pt: {
		txHash:  op.txHash(),
		vout:    op.vout(),
		address: addr.String(),
		amount:  reqFunds,
	}}

	btc.log.Infof("Funding %s %s order with split output coin %v from original coins %v",
		valueStr, btc.symbol, op, coins)
	btc.log.Infof("Sent split transaction %s to accommodate swap of size %s %s + fees = %s",
		op.txHash(), valueStr, btc.symbol, amount(reqFunds))

	// Assign to coins so the deferred function will lock the output.
	outputs = []*output{op}
	return asset.Coins{op}, true, nil
}

// splitBaggageFees is the fees associated with adding a split transaction.
func (btc *ExchangeWallet) splitBaggageFees(maxFeeRate uint64) (swapInputSize, baggage uint64) {
	if btc.segwit {
		baggage = maxFeeRate * splitTxBaggageSegwit
		swapInputSize = dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4)
		return
	}
	baggage = maxFeeRate * splitTxBaggage
	swapInputSize = dexbtc.RedeemP2PKHInputSize
	return
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
			return fmt.Errorf("error converting coin: %w", err)
		}
		ops = append(ops, op)
		delete(btc.fundingCoins, op.pt)
	}
	return btc.node.lockUnspent(true, ops)
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
			coins = append(coins, newOutput(txHash, vout, fundingCoin.amount))
			continue
		}
		notFound[pt] = true
	}
	if len(notFound) == 0 {
		return coins, nil
	}

	// Check locked outputs for not found coins.
	lockedOutpoints, err := btc.node.listLockUnspent()
	if err != nil {
		return nil, err
	}
outer:
	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid from rpc server %s: %w", rpcOP.TxID, err)
		}
		pt := newOutPoint(txHash, rpcOP.Vout)
		if !notFound[pt] {
			continue
		}

		tx, err := btc.node.getTransaction(txHash)
		if err != nil {
			return nil, err
		}
		for _, item := range tx.Details {
			if item.Vout != rpcOP.Vout {
				continue
			}
			if item.Amount <= 0 {
				return nil, fmt.Errorf("unexpected debit at %s:%v", txHash, rpcOP.Vout)
			}

			utxo := &utxo{
				txHash:  txHash,
				vout:    rpcOP.Vout,
				address: item.Address,
				amount:  toSatoshi(item.Amount),
			}
			coin := newOutput(txHash, rpcOP.Vout, toSatoshi(item.Amount))
			coins = append(coins, coin)
			btc.fundingCoins[pt] = utxo
			delete(notFound, pt)
			if len(notFound) == 0 {
				return coins, nil
			}

			continue outer
		}
		return nil, fmt.Errorf("funding coin %s:%v not found", txHash, rpcOP.Vout)
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
		coin := newOutput(utxo.txHash, utxo.vout, utxo.amount)
		coins = append(coins, coin)
		coinsToLock = append(coinsToLock, coin)
		delete(notFound, pt)
	}
	btc.log.Debugf("Locking funding coins that were unlocked %v", coinsToLock)
	err = btc.node.lockUnspent(false, coinsToLock)
	if err != nil {
		return nil, err
	}

	return coins, nil
}

// Unlock unlocks the ExchangeWallet. The pw supplied should be the same as the
// password for the underlying bitcoind wallet which will also be unlocked.
func (btc *ExchangeWallet) Unlock(pw string) error {
	return btc.node.walletUnlock(pw)
}

// Lock locks the ExchangeWallet and the underlying bitcoind wallet.
func (btc *ExchangeWallet) Lock() error {
	return btc.node.walletLock()
}

// Locked will be true if the wallet is currently locked.
func (btc *ExchangeWallet) Locked() bool {
	return btc.node.locked()
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
			return nil, 0, nil, fmt.Errorf("error converting coin: %w", err)
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

	refundAddrs := make([]btcutil.Address, 0, len(swaps.Contracts))

	// Add the contract outputs.
	// TODO: Make P2WSH contract and P2WPKH change outputs instead of
	// legacy/non-segwit swap contracts pkScripts.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address belonging to the key that will be
		// used to sign and refund a swap past its encoded refund locktime.
		revokeAddr, err := btc.externalAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating revocation address: %w", err)
		}
		refundAddrs = append(refundAddrs, revokeAddr)

		contractAddr, err := btc.decodeAddr(contract.Address, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("address decode error: %v", err)
		}

		// Create the contract, a P2SH redeem script.
		contractScript, err := dexbtc.MakeContract(contractAddr, revokeAddr,
			contract.SecretHash, int64(contract.LockTime), btc.segwit, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("unable to create pubkey script for address %s: %w", contract.Address, err)
		}
		contracts = append(contracts, contractScript)

		// Make the P2SH address and pubkey script.
		scriptAddr, err := btc.scriptHashAddress(contractScript)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error encoding script address: %w", err)
		}

		pkScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating pubkey script: %w", err)
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
	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change address: %w", err)
	}

	// Sign, add change, but don't send the transaction yet until
	// the individual swap refund txs are prepared and signed.
	msgTx, change, fees, err := btc.signTxAndAddChange(baseTx, changeAddr, totalIn, totalOut, swaps.FeeRate)
	if err != nil {
		return nil, nil, 0, err
	}

	// Prepare the receipts.
	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		output := newOutput(&txHash, uint32(i), contract.Value)
		signedRefundTx, err := btc.refundTx(output.txHash(), output.vout(), contracts[i], contract.Value, refundAddrs[i], time.Now().Add(-time.Hour))
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating refund tx: %w", err)
		}
		refundBuff := new(bytes.Buffer)
		err = signedRefundTx.Serialize(refundBuff)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error serializing refund tx: %w", err)
		}
		receipts = append(receipts, &swapReceipt{
			output:       output,
			contract:     contracts[i],
			expiration:   time.Unix(int64(contract.LockTime), 0).UTC(),
			signedRefund: refundBuff.Bytes(),
		})
	}

	// Refund txs prepared and signed. Can now broadcast the swap(s).
	err = btc.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
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
		err = btc.node.lockUnspent(false, []*output{change})
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
func (btc *ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx(wire.TxVersion)
	var totalIn uint64
	var contracts [][]byte
	var addresses []btcutil.Address
	var values []uint64
	for _, r := range form.Redemptions {
		if r.Spends == nil {
			return nil, nil, 0, fmt.Errorf("no audit info")
		}

		cinfo, err := btc.convertAuditInfo(r.Spends)
		if err != nil {
			return nil, nil, 0, err
		}

		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		contract := cinfo.contract
		_, receiver, _, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error extracting swap addresses: %w", err)
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
		witnessVBytes := (dexbtc.RedeemSwapSigScriptSize*uint64(len(form.Redemptions)) + 2 + 3) / 4
		size += witnessVBytes + dexbtc.P2WPKHOutputSize
	} else {
		size += dexbtc.RedeemSwapSigScriptSize*uint64(len(form.Redemptions)) + dexbtc.P2PKHOutputSize
	}

	feeRate := btc.feeRateWithFallback(btc.redeemConfTarget, form.FeeSuggestion)
	fee := feeRate * size
	if fee > totalIn {
		return nil, nil, 0, fmt.Errorf("redeem tx not worth the fees")
	}

	// Send the funds back to the exchange wallet.
	redeemAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting new address from the wallet: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change script: %w", err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, feeRate) {
		return nil, nil, 0, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)

	if btc.segwit {
		sigHashes := txscript.NewTxSigHashes(msgTx)
		for i, r := range form.Redemptions {
			contract := contracts[i]
			redeemSig, redeemPubKey, err := btc.createWitnessSig(msgTx, i, contract, addresses[i], values[i], sigHashes)
			if err != nil {
				return nil, nil, 0, err
			}
			msgTx.TxIn[i].Witness = dexbtc.RedeemP2WSHContract(contract, redeemSig, redeemPubKey, r.Secret)
		}
	} else {
		for i, r := range form.Redemptions {
			contract := contracts[i]
			redeemSig, redeemPubKey, err := btc.createSig(msgTx, i, contract, addresses[i], values[i])
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
	txHash, err := btc.node.sendRawTransaction(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}
	if *txHash != checkHash {
		return nil, nil, 0, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	// Log the change output.
	coinIDs := make([]dex.Bytes, 0, len(form.Redemptions))
	for i := range form.Redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}
	return coinIDs, newOutput(txHash, 0, uint64(txOut.Value)), fee, nil
}

// convertAuditInfo converts from the common *asset.AuditInfo type to our
// internal *auditInfo type.
func (btc *ExchangeWallet) convertAuditInfo(ai *asset.AuditInfo) (*auditInfo, error) {
	if ai.Coin == nil {
		return nil, fmt.Errorf("no coin")
	}

	txHash, vout, err := decodeCoinID(ai.Coin.ID())
	if err != nil {
		return nil, err
	}

	recip, err := btc.decodeAddr(ai.Recipient, btc.chainParams)
	if err != nil {
		return nil, err
	}

	return &auditInfo{
		output:     newOutput(txHash, vout, ai.Coin.Value()), //     *output
		recipient:  recip,                                    //  btcutil.Address
		contract:   ai.Contract,                              //   []byte
		secretHash: ai.SecretHash,                            // []byte
		expiration: ai.Expiration,                            // time.Time
	}, nil
}

// SignMessage signs the message with the private key associated with the
// specified unspent coin. A slice of pubkeys required to spend the coin and a
// signature for each pubkey are returned.
func (btc *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	op, err := btc.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %w", err)
	}
	btc.fundingMtx.RLock()
	utxo := btc.fundingCoins[op.pt]
	btc.fundingMtx.RUnlock()
	if utxo == nil {
		return nil, nil, fmt.Errorf("no utxo found for %s", op)
	}
	privKey, err := btc.node.privKeyForAddress(utxo.address)
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
func (btc *ExchangeWallet) AuditContract(coinID, contract, txData dex.Bytes, since time.Time) (*asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}
	// Get the contracts P2SH address from the tx output's pubkey script.
	txOut, _, err := btc.node.getTxOut(txHash, vout, contract, since)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %s:%d : %w", txHash, vout, err)
	}
	if txOut == nil {
		return nil, asset.CoinNotFoundError
	}

	// Check for standard P2SH.
	scriptClass, addrs, numReq, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting script addresses from '%x': %w", txOut.PkScript, err)
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
	return &asset.AuditInfo{
		Coin:       newOutput(txHash, vout, uint64(txOut.Value)),
		Recipient:  receiver.String(),
		Contract:   contract,
		SecretHash: secretHash,
		Expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// RefundAddress extracts and returns the refund address from a contract.
func (btc *ExchangeWallet) RefundAddress(contract dex.Bytes) (string, error) {
	sender, _, _, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return "", fmt.Errorf("error extracting refund address: %w", err)
	}
	return sender.String(), nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (btc *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	bestBlockHash, err := btc.node.getBestBlockHash()
	if err != nil {
		return false, time.Time{}, fmt.Errorf("get best block hash error: %w", err)
	}
	bestBlockHeader, err := btc.node.getBlockHeader(bestBlockHash.String())
	if err != nil {
		return false, time.Time{}, fmt.Errorf("get best block header error: %w", err)
	}
	bestBlockMedianTime := time.Unix(bestBlockHeader.MedianTime, 0).UTC()
	return bestBlockMedianTime.After(contractExpiry), contractExpiry, nil
}

// FindRedemption watches for the input that spends the specified contract
// coin, and returns the spending input and the contract's secret key when it
// finds a spender.
//
// This method blocks until the redemption is found, an error occurs or the
// provided context is canceled.
func (btc *ExchangeWallet) FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	exitError := func(s string, a ...interface{}) (dex.Bytes, dex.Bytes, error) {
		return nil, nil, fmt.Errorf(s, a...)
	}

	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return exitError("cannot decode contract coin id: %w", err)
	}

	outPt := newOutPoint(txHash, vout)

	tx, err := btc.node.getTransaction(txHash)
	if err != nil {
		return exitError("getTransaction error for FindRedemption transaction: %v", err)
	}

	contractHash := btc.hashContract(contract)

	contractAddr, err := btc.scriptHashAddress(contract)
	if err != nil {
		return exitError("scriptHashAddress error: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(contractAddr)
	if err != nil {
		return exitError("PayToAddrScript error: %v", err)
	}

	var blockHash *chainhash.Hash
	var blockHeight int32
	if tx.BlockHash != "" {
		blockHash, blockHeight, err = btc.checkRedemptionBlockDetails(outPt, tx.BlockHash, pkScript)
		if err != nil {
			return exitError("GetBlockHeight for redemption %s error: %v", outPt, err)
		}
	}

	req := &findRedemptionReq{
		outPt:        outPt,
		contract:     contract,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   make(chan *findRedemptionResult, 1),
		pkScript:     pkScript,
		contractHash: contractHash,
	}

	btc.findRedemptionMtx.Lock()
	oldRedemption := btc.findRedemptionQueue[outPt]
	btc.findRedemptionQueue[outPt] = req
	btc.findRedemptionMtx.Unlock()

	if oldRedemption != nil {
		oldRedemption.fail("Duplicate FindRedemption request received. Aborting the old request.")
	}

	go btc.tryRedemptionRequests(nil, []*findRedemptionReq{req})

	var result *findRedemptionResult
	select {
	case result = <-req.resultChan:
		if result == nil {
			err = fmt.Errorf("unexpected nil result for redemption search for %s", outPt)
		}
	case <-ctx.Done():
		err = fmt.Errorf("context cancelled during search for redemption for %s", outPt)
	}

	// If this contract is still in the findRedemptionQueue, remove from the queue
	// to prevent further redemption search attempts for this contract.
	btc.findRedemptionMtx.Lock()
	delete(btc.findRedemptionQueue, outPt)
	btc.findRedemptionMtx.Unlock()

	// result would be nil if ctx is canceled or the result channel is closed
	// without data, which would happen if the redemption search is aborted when
	// this ExchangeWallet is shut down.
	if result != nil {
		return result.redemptionCoinID, result.secret, result.err
	}
	return nil, nil, err
}

// tryRedemptionRequests searches all mainchain blocks with height >= startBlock
// for redemptions.
func (btc *ExchangeWallet) tryRedemptionRequests(startBlock *chainhash.Hash, reqs []*findRedemptionReq) {
	undiscovered := make(map[outPoint]*findRedemptionReq, len(reqs))
	mempoolReqs := make(map[outPoint]*findRedemptionReq)
	for _, req := range reqs {
		// If there is no block hash yet, this request hasn't been mined, and a
		// spending tx cannot have been mined. Only check mempool.
		if req.blockHash == nil {
			mempoolReqs[req.outPt] = req
			continue
		}
		undiscovered[req.outPt] = req
	}

	// Only search up to the current tip. This does leave two unhandled
	// scenarios worth mentioning.
	//  1) A new block is mined during our search. In this case, we won't see
	//     see the new block, but tryRedemptionRequests should be called again
	//     by the block monitoring loop.
	//  2) A reorg happens, and this tip becomes orphaned. In this case, the
	//     worst that can happen is that a shorter chain will replace a longer
	//     one (extremely rare). Even in that case, we'll just log the error and
	//     exit the block loop.
	tipHeight, err := btc.node.getBestBlockHeight()
	if err != nil {
		btc.log.Errorf("tryRedemptionRequests getBestBlockHeight error: %v", err)
		return
	}

	// If a startBlock is provided at a higher height, use that as the starting
	// point.
	var iHash *chainhash.Hash
	var iHeight int32
	if startBlock != nil {
		h, err := btc.node.getBlockHeight(startBlock)
		if err != nil {
			btc.log.Errorf("tryRedemptionRequests getBlockHeight error: %v", err)
			return
		}
		iHeight = h
		iHash = startBlock
	} else {
		iHeight = math.MaxInt32
		for _, req := range undiscovered {
			if req.blockHash != nil && req.blockHeight < iHeight {
				iHeight = req.blockHeight
				iHash = req.blockHash
			}
		}
	}

	// Helper function to check that the request hasn't been located in another
	// thread and removed from queue already.
	reqStillQueued := func(outPt outPoint) bool {
		_, found := btc.findRedemptionQueue[outPt]
		return found
	}

	for iHeight <= tipHeight {
		validReqs := make(map[outPoint]*findRedemptionReq, len(undiscovered))
		btc.findRedemptionMtx.RLock()
		for outPt, req := range undiscovered {
			if iHeight >= req.blockHeight && reqStillQueued(req.outPt) {
				validReqs[outPt] = req
			}
		}
		btc.findRedemptionMtx.RUnlock()

		if len(validReqs) == 0 {
			continue
		}

		discovered := btc.node.searchBlockForRedemptions(validReqs, *iHash)
		for outPt, res := range discovered {
			req, found := undiscovered[outPt]
			if !found {
				btc.log.Errorf("Request not found in undiscovered map. This shouldn't be possible.")
				continue
			}
			req.success(res)
			delete(undiscovered, outPt)
		}

		if len(undiscovered) == 0 {
			return
		}

		iHeight += 1
		if iHash, err = btc.node.getBlockHash(int64(iHeight)); err != nil {
			if iHeight < tipHeight {
				btc.log.Errorf("error getting block hash for height %d: %v", iHeight, err)
			}
			return
		}
	}

	// Check mempool for any remaining undiscovered requests.
	for outPt, req := range undiscovered {
		mempoolReqs[outPt] = req
	}

	if len(mempoolReqs) == 0 {
		return
	}

	// Do we really want to do this? Mempool could be huge.
	btc.node.findRedemptionsInMempool(mempoolReqs)
}

// Refund revokes a contract. This can only be used after the time lock has
// expired.
// NOTE: The contract cannot be retrieved from the unspent coin info as the
// wallet does not store it, even though it was known when the init transaction
// was created. The client should store this information for persistence across
// sessions.
func (btc *ExchangeWallet) Refund(coinID, contract dex.Bytes, startTime time.Time) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	utxo, _, err := btc.node.getTxOut(txHash, vout, contract, startTime)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %w", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}
	msgTx, err := btc.refundTx(txHash, vout, contract, uint64(utxo.Value), nil, startTime)
	if err != nil {
		return nil, fmt.Errorf("error creating refund tx: %w", err)
	}

	checkHash := msgTx.TxHash()
	refundHash, err := btc.node.sendRawTransaction(msgTx)
	if err != nil {
		return nil, fmt.Errorf("sendRawTransaction: %w", err)
	}
	if *refundHash != checkHash {
		return nil, fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return toCoinID(refundHash, 0), nil
}

// refundTx crates and signs a contract`s refund transaction. If refundAddr is
// not supplied, one will be requested from the wallet. If val is not supplied
// it will be retrieved with gettxout.
func (btc *ExchangeWallet) refundTx(txHash *chainhash.Hash, vout uint32, contract dex.Bytes, val uint64, refundAddr btcutil.Address, startTime time.Time) (*wire.MsgTx, error) {
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// Create the transaction that spends the contract.
	feeRate := btc.feeRateWithFallback(2, 0) // meh level urgency
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
	if refundAddr == nil {
		refundAddr, err = btc.node.changeAddress()
		if err != nil {
			return nil, fmt.Errorf("error getting new address from the wallet: %w", err)
		}
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating change script: %w", err)
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
			return nil, fmt.Errorf("createWitnessSig: %w", err)
		}
		txIn.Witness = dexbtc.RefundP2WSHContract(contract, refundSig, refundPubKey)

	} else {
		refundSig, refundPubKey, err := btc.createSig(msgTx, 0, contract, sender, val)
		if err != nil {
			return nil, fmt.Errorf("createSig: %w", err)
		}
		txIn.SignatureScript, err = dexbtc.RefundP2SHContract(contract, refundSig, refundPubKey)
		if err != nil {
			return nil, fmt.Errorf("RefundP2SHContract: %w", err)
		}
	}
	return msgTx, nil
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
	txHash, vout, sent, err := btc.send(address, regFee, btc.feeRateWithFallback(1, 0), false)
	if err != nil {
		btc.log.Errorf("PayFee error - address = '%s', fee = %s: %v", address, amount(regFee), err)
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) Withdraw(address string, value uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(2, 0), true)
	if err != nil {
		btc.log.Errorf("Withdraw error - address = '%s', amount = %s: %v", address, amount(value), err)
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (btc *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) send(address string, val uint64, feeRate uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	txHash, err := btc.node.sendToAddress(address, val, feeRate, subtract)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("SendToAddress error: %w", err)
	}
	tx, err := btc.node.getTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, 0, asset.CoinNotFoundError
		}
		return nil, 0, 0, fmt.Errorf("failed to fetch transaction after send: %w", err)
	}
	for _, details := range tx.Details {
		if details.Address == address {
			return txHash, details.Vout, toSatoshi(details.Amount), nil
		}
	}
	return nil, 0, 0, fmt.Errorf("failed to locate transaction vout")
}

// SwapConfirmations gets the number of confirmations for the specified swap
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (btc *ExchangeWallet) SwapConfirmations(_ context.Context, id dex.Bytes, contract dex.Bytes, startTime time.Time) (uint32, error) {
	txHash, vout, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	return btc.node.swapConfirmations(txHash, vout, contract, startTime)
}

// RegFeeConfirmations gets the number of confirmations for the specified output
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (btc *ExchangeWallet) RegFeeConfirmations(_ context.Context, id dex.Bytes) (confs uint32, err error) {
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := btc.node.getTransaction(txHash)
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

// prepareRedemptionRequestsForBlockCheck prepares a copy of the
// findRedemptionQueue, checking for missing block data along the way.
func (btc *ExchangeWallet) prepareRedemptionRequestsForBlockCheck() []*findRedemptionReq {
	// Search for contract redemption in new blocks if there
	// are contracts pending redemption.
	btc.findRedemptionMtx.RLock()
	defer btc.findRedemptionMtx.RUnlock()
	reqs := make([]*findRedemptionReq, 0, len(btc.findRedemptionQueue))
	for _, req := range btc.findRedemptionQueue {
		// If the request doesn't have a block hash yet, check if we can get one
		// now.
		if req.blockHash == nil {
			btc.trySetRedemptionRequestBlock(req)
		}
		reqs = append(reqs, req)
	}
	return reqs
}

// checkForNewBlocks checks for new blocks. When a tip change is detected, the
// tipChange callback function is invoked and a goroutine is started to check
// if any contracts in the findRedemptionQueue are redeemed in the new blocks.
func (btc *ExchangeWallet) checkForNewBlocks() {
	reqs := btc.prepareRedemptionRequestsForBlockCheck()
	// Redemption search would be compromised if the starting point cannot
	// be determined, as searching just the new tip might result in blocks
	// being omitted from the search operation. If that happens, cancel all
	// find redemption requests in queue.
	notifyFatalFindRedemptionError := func(s string, a ...interface{}) {
		for _, req := range reqs {
			req.fail("tipChange handler - "+s, a...)
		}
	}

	newTipHash, err := btc.node.getBestBlockHash()
	if err != nil {
		go btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
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
		go btc.tipChange(fmt.Errorf("error setting new tip: %w", err))
		return
	}

	prevTip := btc.currentTip
	btc.currentTip = newTip
	btc.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTip.height, newTip.hash)
	go btc.tipChange(nil)

	var startPoint *block
	// Check if the previous tip is still part of the mainchain (prevTip confs >= 0).
	// Redemption search would typically resume from prevTipHeight + 1 unless the
	// previous tip was re-orged out of the mainchain, in which case redemption
	// search will resume from the mainchain ancestor of the previous tip.
	prevTipHeader, err := btc.node.getBlockHeader(prevTip.hash)
	switch {
	case err != nil:
		// Redemption search cannot continue reliably without knowing if there
		// was a reorg, cancel all find redemption requests in queue.
		notifyFatalFindRedemptionError("getBlockHeader error for prev tip hash %s: %w",
			prevTip.hash, err)
		return

	case prevTipHeader.Confirmations < 0:
		// The previous tip is no longer part of the mainchain. Crawl blocks
		// backwards until finding a mainchain block. Start with the block
		// that is the immediate ancestor to the previous tip.
		ancestorBlockHash := prevTipHeader.PreviousBlockHash
		for {
			aBlock, err := btc.node.getBlockHeader(ancestorBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("getBlockHeader error for block %s: %w", ancestorBlockHash, err)
				return
			}
			if aBlock.Confirmations > -1 {
				// Found the mainchain ancestor of previous tip.
				startPoint = &block{height: aBlock.Height, hash: aBlock.Hash}
				btc.log.Debugf("reorg detected from height %d to %d", aBlock.Height, newTip.height)
				break
			}
			if aBlock.Height == 0 {
				// Crawled back to genesis block without finding a mainchain ancestor
				// for the previous tip. Should never happen!
				notifyFatalFindRedemptionError("no mainchain ancestor for orphaned block %s", prevTipHeader.Hash)
				return
			}
			ancestorBlockHash = aBlock.PreviousBlockHash
		}

	case newTip.height-prevTipHeader.Height > 1:
		// 2 or more blocks mined since last tip, start at prevTip height + 1.
		afterPrivTip := prevTipHeader.Height + 1
		hashAfterPrevTip, err := btc.node.getBlockHash(afterPrivTip)
		if err != nil {
			notifyFatalFindRedemptionError("getBlockHash error for height %d: %w", afterPrivTip, err)
			return
		}
		startPoint = &block{hash: hashAfterPrevTip.String(), height: afterPrivTip}

	default:
		// Just 1 new block since last tip report, search the lone block.
		startPoint = newTip
	}

	if len(reqs) > 0 {
		startHash, _ := chainhash.NewHashFromStr(startPoint.hash)
		go btc.tryRedemptionRequests(startHash, reqs)
	}
}

// trySetRedemptionRequestBlock should be called with findRedemptionMtx Lock'ed.
func (btc *ExchangeWallet) trySetRedemptionRequestBlock(req *findRedemptionReq) {
	tx, err := btc.node.getTransaction(&req.outPt.txHash)
	if err != nil {
		btc.log.Errorf("getTransaction error for FindRedemption transaction: %v", err)
		return
	}

	if tx.BlockHash == "" {
		return
	}
	blockHash, blockHeight, err := btc.checkRedemptionBlockDetails(req.outPt, tx.BlockHash, req.pkScript)
	if err != nil {
		btc.log.Error(err)
		return
	}
	// Don't update the findRedemptionReq, since the findRedemptionMtx only
	// protects the map.
	req = &findRedemptionReq{
		outPt:        req.outPt,
		contract:     req.contract,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   req.resultChan,
		pkScript:     req.pkScript,
		contractHash: req.contractHash,
	}
	btc.findRedemptionQueue[req.outPt] = req
}

// checkRedemptionBlockDetails looks retrieves the block at hashStr and checks
// that the provided pkScript matches the specified outpoint
func (btc *ExchangeWallet) checkRedemptionBlockDetails(outPt outPoint, blockStr string, pkScript []byte) (*chainhash.Hash, int32, error) {
	blockHash, err := chainhash.NewHashFromStr(blockStr)
	if err != nil {
		return nil, 0, fmt.Errorf("NewHashFromStr error: %w", err)
	}
	blockHeight, err := btc.node.getBlockHeight(blockHash)
	if err != nil {
		return nil, 0, fmt.Errorf("GetBlockHeight for redemption block %s error: %w", blockStr, err)
	}
	blk, err := btc.node.getBlock(*blockHash)
	if err != nil {
		return nil, 0, fmt.Errorf("error retrieving redemption block for %s: %w", blockStr, err)
	}

	var tx *wire.MsgTx
out:
	for _, iTx := range blk.Transactions {
		if iTx.TxHash() == outPt.txHash {
			tx = iTx
			break out
		}
	}
	if tx == nil {
		return nil, 0, fmt.Errorf("transaction %s not found in block %s", outPt.txHash, blockStr)
	}
	if uint32(len(tx.TxOut)) < outPt.vout+1 {
		return nil, 0, fmt.Errorf("no output %d in redemption transaction %s found in block %s", outPt.vout, outPt.txHash, blockStr)
	}
	if !bytes.Equal(tx.TxOut[outPt.vout].PkScript, pkScript) {
		return nil, 0, fmt.Errorf("pubkey script mismatch for redemption at %s", outPt)
	}

	return blockHash, blockHeight, nil
}

func (btc *ExchangeWallet) blockFromHash(hash string) (*block, error) {
	blk, err := btc.node.getBlockHeader(hash)
	if err != nil {
		return nil, fmt.Errorf("getBlockHeader error for hash %s: %w", hash, err)
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
	return newOutput(txHash, vout, coin.Value()), nil
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change.
func (btc *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx, addr btcutil.Address,
	totalIn, totalOut, feeRate uint64) (*wire.MsgTx, error) {

	signedTx, _, _, err := btc.signTxAndAddChange(baseTx, addr, totalIn, totalOut, feeRate)
	if err != nil {
		return nil, err
	}

	err = btc.broadcastTx(signedTx)
	return signedTx, err
}

// signTxAndAddChange signs the passed tx and adds a change output if the change
// wouldn't be dust. Returns but does NOT broadcast the signed tx.
func (btc *ExchangeWallet) signTxAndAddChange(baseTx *wire.MsgTx, addr btcutil.Address,
	totalIn, totalOut, feeRate uint64) (*wire.MsgTx, *output, uint64, error) {

	makeErr := func(s string, a ...interface{}) (*wire.MsgTx, *output, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	sigCycles := 1
	msgTx, err := btc.node.signTx(baseTx)
	if err != nil {
		return makeErr("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
	}
	vSize := dexbtc.MsgTxVBytes(msgTx)
	minFee := feeRate * vSize
	remaining := totalIn - totalOut
	if minFee > remaining {
		return makeErr("not enough funds to cover minimum fee rate. %.8f < %.8f, raw tx: %x",
			toBTC(totalIn), toBTC(minFee+totalOut), btc.wireBytes(baseTx))
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
			msgTx, err = btc.node.signTx(baseTx)
			if err != nil {
				return makeErr("signing error: %v, raw tx: %x", err, btc.wireBytes(baseTx))
			}
			vSize = dexbtc.MsgTxVBytes(msgTx) // recompute the size with new tx signature
			reqFee := feeRate * vSize
			if reqFee > remaining {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				btc.log.Errorf("reached the impossible place. in = %.8f, out = %.8f, reqFee = %.8f, lastFee = %.8f, raw tx = %x, vSize = %d, feeRate = %d",
					toBTC(totalIn), toBTC(totalOut), toBTC(reqFee), toBTC(fee), btc.wireBytes(msgTx), vSize, feeRate)
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
				btc.log.Errorf("reached the impossible place. in = %.8f, out = %.8f, reqFee = %.8f, lastFee = %.8f, raw tx = %x",
					toBTC(totalIn), toBTC(totalOut), toBTC(reqFee), toBTC(fee), btc.wireBytes(msgTx))
				return makeErr("dust error")
			}
			continue
		}

		totalOut += uint64(changeOutput.Value)
	}

	fee := totalIn - totalOut
	actualFeeRate := fee / vSize
	txHash := msgTx.TxHash()
	btc.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, txHash, feeRate, actualFeeRate, fee, vSize, changeAdded)

	var change *output
	if changeAdded {
		change = newOutput(&txHash, uint32(changeIdx), uint64(changeOutput.Value))
	}

	return msgTx, change, fee, nil
}

func (btc *ExchangeWallet) broadcastTx(signedTx *wire.MsgTx) error {
	txHash, err := btc.node.sendRawTransaction(signedTx)
	if err != nil {
		return fmt.Errorf("sendrawtx error: %v, raw tx: %x", err, btc.wireBytes(signedTx))
	}
	checkHash := signedTx.TxHash()
	if *txHash != checkHash {
		return fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %x", checkHash, *txHash, btc.wireBytes(signedTx))
	}
	return nil
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (btc *ExchangeWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address, val uint64) (sig, pubkey []byte, err error) {
	privKey, err := btc.node.privKeyForAddress(addr.String())
	if err != nil {
		return nil, nil, err
	}
	sig, err = btc.signNonSegwit(tx, idx, pkScript, txscript.SigHashAll, privKey, val)
	if err != nil {
		return nil, nil, err
	}
	return sig, privKey.PubKey().SerializeCompressed(), nil
}

// createWitnessSig creates and returns a signature for the witness of a segwit
// input and the pubkey associated with the address.
func (btc *ExchangeWallet) createWitnessSig(tx *wire.MsgTx, idx int, pkScript []byte,
	addr btcutil.Address, val uint64, sigHashes *txscript.TxSigHashes) (sig, pubkey []byte, err error) {

	privKey, err := btc.node.privKeyForAddress(addr.String())
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
	confs        uint32
	redeemScript []byte
	input        *dexbtc.SpendInfo
}

// spendableUTXOs filters the RPC utxos for those that are spendable with with
// regards to the DEX's configuration, and considered safe to spend according to
// confirmations and coin source. The UTXOs will be sorted by ascending value.
func (btc *ExchangeWallet) spendableUTXOs(confs uint32) ([]*compositeUTXO, map[outPoint]*compositeUTXO, uint64, error) {
	unspents, err := btc.node.listUnspent()
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
				return nil, nil, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %w", err)
			}

			// Guard against inconsistencies between the wallet's view of
			// spendable unlocked UTXOs and ExchangeWallet's. e.g. User manually
			// unlocked something or even restarted the wallet software.
			pt := newOutPoint(txHash, txout.Vout)
			if btc.fundingCoins[pt] != nil {
				btc.log.Warnf("Known order-funding coin %s returned by listunspent!", pt)
				// TODO: Consider relocking the coin in the wallet.
				//continue
			}

			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, btc.chainParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error reading asset info: %w", err)
			}

			utxo := &compositeUTXO{
				utxo: &utxo{
					txHash:  txHash,
					vout:    txout.Vout,
					address: txout.Address,
					amount:  toSatoshi(txout.Amount),
				},
				confs:        txout.Confirmations,
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
	lockedOutpoints, err := btc.node.listLockUnspent()
	if err != nil {
		return 0, err
	}
	var sum uint64
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
outer:
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
		tx, err := btc.node.getTransaction(txHash)
		if err != nil {
			return 0, err
		}
		for _, item := range tx.Details {
			if item.Vout == rpcOP.Vout {
				if item.Amount <= 0 {
					return 0, fmt.Errorf("unexpected debit at %s:%v", txHash, rpcOP.Vout)
				}
				sum += toSatoshi(item.Amount)
				continue outer
			}
		}
		return 0, fmt.Errorf("output %s:%v not found", txHash, rpcOP.Vout)
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
	Hash              string `json:"hash"`
	Confirmations     int64  `json:"confirmations"`
	Height            int64  `json:"height"`
	Time              int64  `json:"time"`
	MedianTime        int64  `json:"mediantime"`
	PreviousBlockHash string `json:"previousblockhash"`
}

// verboseBlockTxs is a partial btcjson.GetBlockVerboseResult with
// key "rawtx" -> "tx".
type verboseBlockTxs struct {
	Hash     string                `json:"hash"`
	Height   uint64                `json:"height"`
	NextHash string                `json:"nextblockhash"`
	Tx       []btcjson.TxRawResult `json:"tx"`
}

// externalAddress will return a new address for public use.
func (btc *ExchangeWallet) externalAddress() (btcutil.Address, error) {
	if btc.segwit {
		return btc.node.addressWPKH()
	}
	return btc.node.addressPKH()
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
// transaction is not known. The error must be dcrjson.RPCError with a numeric
// code equal to btcjson.ErrRPCNoTxInfo.
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

// toBTC returns a float representation in conventional units for the sats.
func toBTC(v uint64) float64 {
	return btcutil.Amount(v).ToBTC()
}

// rawTxInSig signs the transaction in input using the standard bitcoin
// signature hash and ECDSA algorithm.
func rawTxInSig(tx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType, key *btcec.PrivateKey, _ uint64) ([]byte, error) {
	return txscript.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, key)
}
