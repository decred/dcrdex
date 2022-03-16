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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/decred/dcrd/dcrjson/v4" // for dcrjson.RPCError returns from rpcclient
	"github.com/decred/dcrd/rpcclient/v7"
)

const (
	version = 0

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

	walletTypeLegacy = ""
	walletTypeRPC    = "bitcoindRPC"
	walletTypeSPV    = "SPV"
)

var (
	// ContractSearchLimit is how far back in time AuditContract in SPV mode
	// will search for a contract if no txData is provided. This should be a
	// positive duration.
	ContractSearchLimit = 48 * time.Hour

	// blockTicker is the delay between calls to check for new blocks.
	blockTicker                  = time.Second
	peerCountTicker              = 5 * time.Second
	walletBlockAllowance         = time.Second * 10
	conventionalConversionFactor = float64(dexbtc.UnitInfo.Conventional.ConversionFactor)
	rpcOpts                      = []*asset.ConfigOption{
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
	}

	commonOpts = []*asset.ConfigOption{
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
			IsBoolean:    true,
			DefaultValue: false,
		},
	}
	rpcWalletDefinition = &asset.WalletDefinition{
		Type:              walletTypeRPC,
		Tab:               "External",
		Description:       "Connect to bitcoind",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"),
		ConfigOpts:        append(rpcOpts, commonOpts...),
	}
	spvWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeSPV,
		Tab:         "Native",
		Description: "Use the built-in SPV wallet",
		ConfigOpts:  commonOpts,
		Seeded:      true,
	}

	// WalletInfo defines some general information about a Bitcoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:     "Bitcoin",
		Version:  version,
		UnitInfo: dexbtc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			spvWalletDefinition,
			rpcWalletDefinition,
		},
		LegacyWalletIndex: 1,
	}
	walletBirthday time.Time
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

// RPCWalletConfig is a combination of RPCConfig and WalletConfig. Used for a
// wallet based on a bitcoind-like RPC API.
type RPCWalletConfig struct {
	dexbtc.RPCConfig
	WalletConfig
}

// WalletConfig are wallet-level configuration settings.
type WalletConfig struct {
	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	FeeRateLimit     float64 `ini:"feeratelimit"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
	WalletName       string  `ini:"walletname"` // RPC
	Peer             string  `ini:"peer"`       // SPV
}

// readRPCWalletConfig parses the settings map into a *RPCWalletConfig.
func readRPCWalletConfig(settings map[string]string, symbol string, net dex.Network, ports dexbtc.NetPorts) (cfg *RPCWalletConfig, err error) {
	cfg = new(RPCWalletConfig)
	err = config.Unmapify(settings, &cfg.WalletConfig)
	if err != nil {
		return nil, fmt.Errorf("error parsing wallet config: %w", err)
	}
	err = config.Unmapify(settings, &cfg.RPCConfig)
	if err != nil {
		return nil, fmt.Errorf("error parsing rpc config: %w", err)
	}
	err = dexbtc.CheckRPCConfig(&cfg.RPCConfig, symbol, net, ports)
	return
}

// parseRPCWalletConfig parses a *RPCWalletConfig from the settings map and
// creates the unconnected *rpcclient.Client.
func parseRPCWalletConfig(settings map[string]string, symbol string, net dex.Network, ports dexbtc.NetPorts) (*RPCWalletConfig, *rpcclient.Client, error) {
	cfg, err := readRPCWalletConfig(settings, symbol, net, ports)
	if err != nil {
		return nil, nil, err
	}
	endpoint := cfg.RPCBind + "/wallet/" + cfg.WalletName
	cl, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         endpoint,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	return cfg, cl, nil
}

// Driver implements asset.Driver.
type Driver struct{}

// Check that Driver implements Driver and Creator.
var _ asset.Driver = (*Driver)(nil)
var _ asset.Creator = (*Driver)(nil)

// Exists checks the existence of the wallet. Part of the Creator interface, so
// only used for wallets with WalletDefinition.Seeded = true.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeSPV {
		return false, fmt.Errorf("no Bitcoin wallet of type %q available", walletType)
	}

	chainParams, err := parseChainParams(net)
	if err != nil {
		return false, err
	}
	netDir := filepath.Join(dataDir, chainParams.Name)
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	loader := wallet.NewLoader(chainParams, netDir, true, 60*time.Second, 250)
	return loader.WalletExists()
}

// Create creates a new SPV wallet.
func (d *Driver) Create(params *asset.CreateWalletParams) error {
	if params.Type != walletTypeSPV {
		return fmt.Errorf("SPV is the only seeded wallet type. required = %q, requested = %q", walletTypeSPV, params.Type)
	}
	if len(params.Seed) == 0 {
		return errors.New("wallet seed cannot be empty")
	}
	if len(params.DataDir) == 0 {
		return errors.New("must specify wallet data directory")
	}
	chainParams, err := parseChainParams(params.Net)
	if err != nil {
		return fmt.Errorf("error parsing chain: %w", err)
	}

	return createSPVWallet(params.Pass, params.Seed, params.DataDir, params.Logger, chainParams)
}

// Open opens or connects to the BTC exchange wallet. Start the wallet with its
// Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
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
	var err error
	walletBirthday, err = time.Parse(time.RFC822, "02 Jun 21 21:12 CDT")
	if err != nil {
		panic("error parsing wallet birthday: " + err.Error())
	}

	asset.Register(BipID, &Driver{})
}

// baseWallet is a wallet backend for Bitcoin. The backend is how the DEX
// client app communicates with the BTC blockchain and wallet. baseWallet
// satisfies the dex.Wallet interface.
type baseWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect      int64
	node              Wallet
	walletInfo        *asset.WalletInfo
	chainParams       *chaincfg.Params
	log               dex.Logger
	symbol            string
	tipChange         func(error)
	lastPeerCount     uint32
	peersChange       func(uint32)
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

// ExchangeWalletSPV embeds a ExchangeWallet, but also provides the Rescan
// method to implement asset.Rescanner.
type ExchangeWalletSPV struct {
	*baseWallet
}

// ExchangeWalletFullNode implements Wallet and adds the FeeRate method.
type ExchangeWalletFullNode struct {
	*baseWallet
}

// Check that wallets satisfy their supported interfaces.

var _ asset.Wallet = (*baseWallet)(nil)
var _ asset.Rescanner = (*ExchangeWalletSPV)(nil)
var _ asset.FeeRater = (*ExchangeWalletFullNode)(nil)
var _ asset.LogFiler = (*ExchangeWalletSPV)(nil)

// Rescan satisfies the asset.Rescanner interface, and issues a rescan wallet
// command if the backend is an SPV wallet.
func (btc *ExchangeWalletSPV) Rescan(_ context.Context) error {
	// This will panic if not an spvWallet, which would indicate that
	// openSPVWallet was not used to construct this instance.
	w := btc.node.(*spvWallet)
	atomic.StoreInt64(&btc.tipAtConnect, 0) // for progress
	// Caller should start calling SyncStatus on a ticker.
	return w.rescanWalletAsync()
}

// FeeRate satisfies asset.FeeRater.
func (btc *ExchangeWalletFullNode) FeeRate() uint64 {
	rate, err := btc.feeRate(nil, 1)
	if err != nil {
		btc.log.Errorf("Failed to get fee rate: %v", err)
		return 0
	}
	return rate
}

// LogFilePath returns the path to the neutrino log file.
func (btc *ExchangeWalletSPV) LogFilePath() string {
	w := btc.node.(*spvWallet)
	return w.logFilePath()
}

type block struct {
	height int64
	hash   chainhash.Hash
}

// findRedemptionReq represents a request to find a contract's redemption,
// which is added to the findRedemptionQueue with the contract outpoint as
// key.
type findRedemptionReq struct {
	outPt        outPoint
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

func parseChainParams(net dex.Network) (*chaincfg.Params, error) {
	switch net {
	case dex.Mainnet:
		return &chaincfg.MainNetParams, nil
	case dex.Testnet:
		return &chaincfg.TestNet3Params, nil
	case dex.Regtest:
		return &chaincfg.RegressionNetParams, nil
	}
	return nil, fmt.Errorf("unknown network ID %v", net)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	params, err := parseChainParams(net)
	if err != nil {
		return nil, err
	}

	cloneCFG := &BTCCloneCFG{
		WalletCFG:           cfg,
		MinNetworkVersion:   minNetworkVersion,
		WalletInfo:          WalletInfo,
		Symbol:              "btc",
		Logger:              logger,
		Network:             net,
		ChainParams:         params,
		Ports:               dexbtc.RPCPorts,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              true,
	}

	switch cfg.Type {
	case walletTypeSPV:
		return openSPVWallet(cloneCFG)
	case walletTypeRPC, walletTypeLegacy:
		return BTCCloneWallet(cloneCFG)
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
	}
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *BTCCloneCFG) (*ExchangeWalletFullNode, error) {
	clientCfg, client, err := parseRPCWalletConfig(cfg.WalletCFG.Settings, cfg.Symbol, cfg.Network, cfg.Ports)
	if err != nil {
		return nil, err
	}

	btc, err := newRPCWallet(client, cfg, &clientCfg.WalletConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating %s ExchangeWallet: %v", cfg.Symbol,
			err)
	}

	return btc, nil
}

func newRPCWalletConnection(cfg *RPCWalletConfig) (*rpcclient.Client, error) {
	endpoint := cfg.RPCBind + "/wallet/" + cfg.WalletName
	return rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         endpoint,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
}

// newRPCWallet creates the ExchangeWallet and starts the block monitor.
func newRPCWallet(requester RawRequesterWithContext, cfg *BTCCloneCFG, walletConfig *WalletConfig) (*ExchangeWalletFullNode, error) {
	btc, err := newUnconnectedWallet(cfg, walletConfig)
	if err != nil {
		return nil, err
	}
	btc.node = newRPCClient(requester, cfg.Segwit, btc.decodeAddr, cfg.ArglessChangeAddrRPC,
		cfg.LegacyRawFeeLimit, cfg.MinNetworkVersion, cfg.Logger.SubLogger("RPC"), cfg.ChainParams)
	return &ExchangeWalletFullNode{btc}, nil
}

func newUnconnectedWallet(cfg *BTCCloneCFG, walletCfg *WalletConfig) (*baseWallet, error) {
	// If set in the user config, the fallback fee will be in conventional units
	// per kB, e.g. BTC/kB. Translate that to sats/byte.
	fallbackFeesPerByte := toSatoshi(walletCfg.FallbackFeeRate / 1000)
	if fallbackFeesPerByte == 0 {
		fallbackFeesPerByte = cfg.DefaultFallbackFee
	}
	cfg.Logger.Tracef("Fallback fees set at %d %s/vbyte",
		fallbackFeesPerByte, cfg.WalletInfo.UnitInfo.AtomicUnit)

	// If set in the user config, the fee rate limit will be in units of BTC/KB.
	// Convert to sats/byte & error if value is smaller than smallest unit.
	feesLimitPerByte := uint64(defaultFeeRateLimit)
	if walletCfg.FeeRateLimit > 0 {
		feesLimitPerByte = toSatoshi(walletCfg.FeeRateLimit / 1000)
		if feesLimitPerByte == 0 {
			return nil, fmt.Errorf("Fee rate limit is smaller than smallest unit: %v",
				walletCfg.FeeRateLimit)
		}
	}
	cfg.Logger.Tracef("Fees rate limit set at %d sats/byte", feesLimitPerByte)

	redeemConfTarget := walletCfg.RedeemConfTarget
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

	w := &baseWallet{
		symbol:              cfg.Symbol,
		chainParams:         cfg.ChainParams,
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		peersChange:         cfg.WalletCFG.PeersChange,
		fundingCoins:        make(map[outPoint]*utxo),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		fallbackFeeRate:     fallbackFeesPerByte,
		feeRateLimit:        feesLimitPerByte,
		redeemConfTarget:    redeemConfTarget,
		useSplitTx:          walletCfg.UseSplitTx,
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

// openSPVWallet opens the previously created native SPV wallet.
func openSPVWallet(cfg *BTCCloneCFG) (*ExchangeWalletSPV, error) {
	walletCfg := new(WalletConfig)
	err := config.Unmapify(cfg.WalletCFG.Settings, walletCfg)
	if err != nil {
		return nil, err
	}

	btc, err := newUnconnectedWallet(cfg, walletCfg)
	if err != nil {
		return nil, err
	}

	var peers []string
	if walletCfg.Peer != "" {
		peers = append(peers, walletCfg.Peer)
	}

	btc.node = loadSPVWallet(cfg.WalletCFG.DataDir, cfg.Logger.SubLogger("SPV"), peers, cfg.ChainParams)

	return &ExchangeWalletSPV{
		baseWallet: btc,
	}, nil
}

// Info returns basic information about the wallet and asset.
func (btc *baseWallet) Info() *asset.WalletInfo {
	return btc.walletInfo
}

// Net returns the ExchangeWallet's *chaincfg.Params. This is not part of the
// asset.Wallet interface, but is provided as a convenience for embedding types.
func (btc *baseWallet) Net() *chaincfg.Params {
	return btc.chainParams
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (btc *baseWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	if err := btc.node.connect(ctx, &wg); err != nil {
		return nil, err
	}
	// Initialize the best block.
	bestBlockHash, err := btc.node.getBestBlockHash()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for %s: %w", btc.symbol, err)
	}
	// Check for method unknown error for feeRate method.
	_, err = btc.estimateFee(btc.node, 1)
	if isMethodNotFoundErr(err) {
		return nil, fmt.Errorf("fee estimation method not found. Are you configured for the correct RPC?")
	}

	bestBlock, err := btc.blockFromHash(bestBlockHash)
	if err != nil {
		return nil, fmt.Errorf("error parsing best block for %s: %w", btc.symbol, err)
	}
	btc.log.Infof("Connected wallet with current best block %v (%d)", bestBlock.hash, bestBlock.height)
	btc.tipMtx.Lock()
	btc.currentTip = bestBlock
	btc.tipMtx.Unlock()
	atomic.StoreInt64(&btc.tipAtConnect, btc.currentTip.height)
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.watchBlocks(ctx)
		btc.shutdown()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.monitorPeers(ctx)
	}()
	return &wg, nil
}

func (btc *baseWallet) shutdown() {
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
func (btc *baseWallet) SyncStatus() (bool, float32, error) {
	ss, err := btc.node.syncStatus()
	if err != nil {
		return false, 0, err
	}
	if ss.Target == 0 { // do not say progress = 1
		return false, 0, nil
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

	// It looks like we are ready based on syncStatus, but that may just be
	// comparing wallet height to known chain height. Now check peers.
	numPeers, err := btc.node.peerCount()
	if err != nil {
		return false, 0, err
	}
	return numPeers > 0, 1, nil
}

// OwnsAddress indicates if an address belongs to the wallet.
func (btc *baseWallet) OwnsAddress(address string) (bool, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return false, err
	}
	return btc.node.ownsAddress(addr)
}

// Balance returns the total available funds in the wallet. Part of the
// asset.Wallet interface.
func (btc *baseWallet) Balance() (*asset.Balance, error) {
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
func (btc *baseWallet) legacyBalance() (*asset.Balance, error) {
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
func (btc *baseWallet) feeRate(_ RawRequester, confTarget uint64) (uint64, error) {
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

// targetFeeRateWithFallback attempts to get a fresh fee rate for the target
// number of confirmations, but falls back to the suggestion or fallbackFeeRate
// via feeRateWithFallback.
func (btc *baseWallet) targetFeeRateWithFallback(confTarget, feeSuggestion uint64) uint64 {
	feeRate, err := btc.estimateFee(btc.node, confTarget)
	if err == nil {
		btc.log.Tracef("Obtained local estimate for %d-conf fee rate, %d", confTarget, feeRate)
		return feeRate
	}
	btc.log.Tracef("no %d-conf feeRate available: %v", confTarget, err)
	return btc.feeRateWithFallback(feeSuggestion)
}

// feeRateWithFallback filters the suggested fee rate by ensuring it is within
// limits. If not, the configured fallbackFeeRate is returned and a warning
// logged.
func (btc *baseWallet) feeRateWithFallback(feeSuggestion uint64) uint64 {
	if feeSuggestion > 0 && feeSuggestion < btc.feeRateLimit {
		btc.log.Tracef("feeRateWithFallback using caller's suggestion for fee rate, %d. Local estimate unavailable",
			feeSuggestion)
		return feeSuggestion
	}
	btc.log.Warnf("Unable to get optimal fee rate, using fallback of %d", btc.fallbackFeeRate)
	return btc.fallbackFeeRate
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
// lotSize must not be zero and will cause a panic if so.
func (btc *baseWallet) MaxOrder(lotSize, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	_, maxEst, err := btc.maxOrder(lotSize, feeSuggestion, nfo)
	return maxEst, err
}

// maxOrder gets the estimate for MaxOrder, and also returns the
// []*compositeUTXO to be used for further order estimation without additional
// calls to listunspent.
func (btc *baseWallet) maxOrder(lotSize, feeSuggestion uint64, nfo *dex.Asset) (utxos []*compositeUTXO, est *asset.SwapEstimate, err error) {
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
func (btc *baseWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
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
func (btc *baseWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, utxos []*compositeUTXO,
	nfo *dex.Asset, trySplit bool) (*asset.SwapEstimate, bool /*split used*/, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.amount
	}

	val := lots * lotSize

	enough := func(inputsSize, inputsVal uint64) bool {
		reqFunds := calc.RequiredOrderFunds(val, inputsSize, lots, nfo)
		return inputsVal >= reqFunds
	}

	sum, inputsSize, _, _, _, _, err := fund(utxos, enough)
	if err != nil {
		return nil, false, fmt.Errorf("error funding swap value %s: %w", amount(val), err)
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
func (btc *baseWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	feeRate := btc.targetFeeRateWithFallback(btc.redeemConfTarget, req.FeeSuggestion)
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
func (btc *baseWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
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

	enough := func(inputsSize, inputsVal uint64) bool {
		reqFunds := calc.RequiredOrderFunds(ord.Value, inputsSize, ord.MaxSwapCount, ord.DEXConfig)
		return inputsVal >= reqFunds
	}

	sum, size, coins, fundingCoins, redeemScripts, spents, err := fund(utxos, enough)
	if err != nil {
		return nil, nil, fmt.Errorf("error funding swap value of %s: %w", amount(ord.Value), err)
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

func fund(utxos []*compositeUTXO, enough func(uint64, uint64) bool) (
	sum uint64, size uint32, coins asset.Coins, fundingCoins map[outPoint]*utxo, redeemScripts []dex.Bytes, spents []*output, err error) {

	fundingCoins = make(map[outPoint]*utxo)

	isEnoughWith := func(unspent *compositeUTXO) bool {
		return enough(uint64(size+unspent.input.VBytes()), sum+unspent.amount)
		// reqFunds := calc.RequiredOrderFunds(val, uint64(size+unspent.input.VBytes()), lots, nfo)
		// return sum+unspent.amount >= reqFunds
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
		coins, spents, redeemScripts = nil, nil, nil
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
			return 0, 0, nil, nil, nil, nil, fmt.Errorf("not enough to cover requested funds. "+
				"%s BTC available in %d UTXOs", amount(sum), len(coins))
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
func (btc *baseWallet) split(value uint64, lots uint64, outputs []*output,
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
		suggestedFeeRate = btc.targetFeeRateWithFallback(1, 0)
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
func (btc *baseWallet) splitBaggageFees(maxFeeRate uint64) (swapInputSize, baggage uint64) {
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
func (btc *baseWallet) ReturnCoins(unspents asset.Coins) error {
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
func (btc *baseWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
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

		tx, err := btc.node.getWalletTransaction(txHash)
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
func (btc *baseWallet) Unlock(pw []byte) error {
	return btc.node.walletUnlock(pw)
}

// Lock locks the ExchangeWallet and the underlying bitcoind wallet.
func (btc *baseWallet) Lock() error {
	return btc.node.walletLock()
}

// Locked will be true if the wallet is currently locked.
func (btc *baseWallet) Locked() bool {
	return btc.node.locked()
}

// fundedTx creates and returns a new MsgTx with the provided coins as inputs.
func (btc *baseWallet) fundedTx(coins asset.Coins) (*wire.MsgTx, uint64, []outPoint, error) {
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
func (btc *baseWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
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
		signedRefundTx, err := btc.refundTx(output.txHash(), output.vout(), contracts[i], contract.Value, refundAddrs[i], swaps.FeeRate)
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
func (btc *baseWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
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

	feeRate := btc.targetFeeRateWithFallback(btc.redeemConfTarget, form.FeeSuggestion)
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
func (btc *baseWallet) convertAuditInfo(ai *asset.AuditInfo) (*auditInfo, error) {
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
func (btc *baseWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
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
	hash := chainhash.HashB(msg) // legacy servers will not accept this signature!
	sig, err := privKey.Sign(hash)
	if err != nil {
		return nil, nil, err
	}
	pubkeys = append(pubkeys, pk.SerializeCompressed())
	sigs = append(sigs, sig.Serialize()) // DER format serialization
	return
}

// AuditContract retrieves information about a swap contract from the provided
// txData. The extracted information would be used to audit the counter-party's
// contract during a swap. The txData may be empty to attempt retrieval of the
// transaction output from the network, but it is only ensured to succeed for a
// full node or, if the tx is confirmed, an SPV wallet. Normally the server
// should communicate this txData, and the caller can decide to require it. The
// ability to work with an empty txData is a convenience for recovery tools and
// testing, and it may change in the future if a GetTxData method is added for
// this purpose.
func (btc *baseWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// If no tx data is provided, attempt to get the required data (the txOut)
	// from the wallet. If this is a full node wallet, a simple gettxout RPC is
	// sufficient with no pkScript or "since" time. If this is an SPV wallet,
	// only a confirmed counterparty contract can be located, and only one
	// within ContractSearchLimit. As such, this mode of operation is not
	// intended for normal server-coordinated operation.
	var tx *wire.MsgTx
	var txOut *wire.TxOut
	if len(txData) == 0 {
		// Fall back to gettxout, but we won't have the tx to rebroadcast.
		pkScript, _ := btc.scriptHashScript(contract) // pkScript and since time are unused if full node
		txOut, _, err = btc.node.getTxOut(txHash, vout, pkScript, time.Now().Add(-ContractSearchLimit))
		if err != nil || txOut == nil {
			return nil, fmt.Errorf("error finding unspent contract: %s:%d : %w", txHash, vout, err)
		}
	} else {
		tx, err = msgTxFromBytes(txData)
		if err != nil {
			return nil, fmt.Errorf("coin not found, and error encountered decoding tx data: %v", err)
		}
		if len(tx.TxOut) <= int(vout) {
			return nil, fmt.Errorf("specified output %d not found in decoded tx %s", vout, txHash)
		}
		txOut = tx.TxOut[vout]
	}

	// Check for standard P2SH. NOTE: btc.scriptHashScript(contract) should
	// equal txOut.PkScript. All we really get from the TxOut is the *value*.
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

	// Broadcast the transaction, but do not block because this is not required
	// and does not affect the audit result.
	if rebroadcast && tx != nil {
		go func() {
			if hashSent, err := btc.node.sendRawTransaction(tx); err != nil {
				btc.log.Debugf("Rebroadcasting counterparty contract %v (THIS MAY BE NORMAL): %v", txHash, err)
			} else if !hashSent.IsEqual(txHash) {
				btc.log.Errorf("Counterparty contract %v was rebroadcast as %v!", txHash, hashSent)
			}
		}()
	}

	return &asset.AuditInfo{
		Coin:       newOutput(txHash, vout, uint64(txOut.Value)),
		Recipient:  receiver.String(),
		Contract:   contract,
		SecretHash: secretHash,
		Expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (btc *baseWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	bestBlockHash, err := btc.node.getBestBlockHash()
	if err != nil {
		return false, time.Time{}, fmt.Errorf("get best block hash error: %w", err)
	}
	bestBlockHeader, err := btc.node.getBlockHeader(bestBlockHash)
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
func (btc *baseWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	exitError := func(s string, a ...interface{}) (dex.Bytes, dex.Bytes, error) {
		return nil, nil, fmt.Errorf(s, a...)
	}

	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return exitError("cannot decode contract coin id: %w", err)
	}

	outPt := newOutPoint(txHash, vout)

	tx, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		return nil, nil, fmt.Errorf("error finding wallet transaction: %v", err)
	}

	txOut, err := txOutFromTxBytes(tx.Hex, vout)
	if err != nil {
		return nil, nil, err
	}
	pkScript := txOut.PkScript

	var blockHash *chainhash.Hash
	if tx.BlockHash != "" {
		blockHash, err = chainhash.NewHashFromStr(tx.BlockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("error decoding block hash from string %q: %v", tx.BlockHash, err)
		}
	}

	var blockHeight int32
	if blockHash != nil {
		blockHeight, err = btc.checkRedemptionBlockDetails(outPt, blockHash, pkScript)
		if err != nil {
			return exitError("GetBlockHeight for redemption %s error: %v", outPt, err)
		}
	}

	req := &findRedemptionReq{
		outPt:        outPt,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   make(chan *findRedemptionResult, 1),
		pkScript:     pkScript,
		contractHash: dexbtc.ExtractScriptHash(pkScript),
	}

	if err := btc.queueFindRedemptionRequest(req); err != nil {
		return exitError("queueFindRedemptionRequest error for redemption %s: %v", outPt, err)
	}

	go btc.tryRedemptionRequests(ctx, nil, []*findRedemptionReq{req})

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

// queueFindRedemptionRequest adds the *findRedemptionReq to the queue, erroring
// if there is already a request queued for this outpoint.
func (btc *baseWallet) queueFindRedemptionRequest(req *findRedemptionReq) error {
	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()
	if _, exists := btc.findRedemptionQueue[req.outPt]; exists {
		return fmt.Errorf("duplicate find redemption request for %s", req.outPt)
	}
	btc.findRedemptionQueue[req.outPt] = req
	return nil
}

// tryRedemptionRequests searches all mainchain blocks with height >= startBlock
// for redemptions.
func (btc *baseWallet) tryRedemptionRequests(ctx context.Context, startBlock *chainhash.Hash, reqs []*findRedemptionReq) {
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

	epicFail := func(s string, a ...interface{}) {
		errMsg := fmt.Sprintf(s, a...)
		for _, req := range reqs {
			req.fail(errMsg)
		}
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
		epicFail("tryRedemptionRequests getBestBlockHeight error: %v", err)
		return
	}

	// If a startBlock is provided at a higher height, use that as the starting
	// point.
	var iHash *chainhash.Hash
	var iHeight int32
	if startBlock != nil {
		h, err := btc.node.getBlockHeight(startBlock)
		if err != nil {
			epicFail("tryRedemptionRequests startBlock getBlockHeight error: %v", err)
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
			iHeight++
			continue
		}

		discovered := btc.node.searchBlockForRedemptions(ctx, validReqs, *iHash)
		for outPt, res := range discovered {
			req, found := undiscovered[outPt]
			if !found {
				btc.log.Critical("Request not found in undiscovered map. This shouldn't be possible.")
				continue
			}
			req.success(res)
			delete(undiscovered, outPt)
		}

		if len(undiscovered) == 0 {
			break
		}

		iHeight++
		if iHeight <= tipHeight {
			if iHash, err = btc.node.getBlockHash(int64(iHeight)); err != nil {
				// This might be due to a reorg. Don't abandon yet, since
				// tryRedemptionRequests will be tried again by the block
				// monitor loop.
				btc.log.Warn("error getting block hash for height %d: %v", iHeight, err)
				return
			}
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
	searchDur := time.Minute * 5
	searchCtx, cancel := context.WithTimeout(ctx, searchDur)
	defer cancel()
	for outPt, res := range btc.node.findRedemptionsInMempool(searchCtx, mempoolReqs) {
		req, ok := mempoolReqs[outPt]
		if !ok {
			btc.log.Errorf("findRedemptionsInMempool discovered outpoint not found")
			continue
		}
		req.success(res)
	}
	if err := searchCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			btc.log.Error("mempool search exceeded %s time limit", searchDur)
		} else {
			btc.log.Error("mempool search was cancelled")
		}
	}
}

// Refund revokes a contract. This can only be used after the time lock has
// expired. This MUST return an asset.CoinNotFoundError error if the coin is
// spent.
// NOTE: The contract cannot be retrieved from the unspent coin info as the
// wallet does not store it, even though it was known when the init transaction
// was created. The client should store this information for persistence across
// sessions.
func (btc *baseWallet) Refund(coinID, contract dex.Bytes, feeSuggestion uint64) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	pkScript, err := btc.scriptHashScript(contract)
	if err != nil {
		return nil, fmt.Errorf("error parsing pubkey script: %w", err)
	}

	// TODO: I'd recommend not passing a pkScript without a limited startTime
	// to prevent potentially long searches. In this case though, the output
	// will be found in the wallet and won't need to be searched for, only
	// the spender search will be conducted using the pkScript starting from
	// the block containing the original tx. The script can be gotten from
	// the wallet tx though and used for the spender search, while not passing
	// a script here to ensure no attempt is made to find the output without
	// a limited startTime.
	utxo, _, err := btc.node.getTxOut(txHash, vout, pkScript, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %w", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError // spent
	}
	msgTx, err := btc.refundTx(txHash, vout, contract, uint64(utxo.Value), nil, feeSuggestion)
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

// refundTx creates and signs a contract`s refund transaction. If refundAddr is
// not supplied, one will be requested from the wallet.
func (btc *baseWallet) refundTx(txHash *chainhash.Hash, vout uint32, contract dex.Bytes, val uint64, refundAddr btcutil.Address, feeSuggestion uint64) (*wire.MsgTx, error) {
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// Create the transaction that spends the contract.
	feeRate := btc.targetFeeRateWithFallback(2, feeSuggestion) // meh level urgency
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	// Enable the OP_CHECKLOCKTIMEVERIFY opcode to be used.
	//
	// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
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

// Address returns an external address from the wallet.
func (btc *baseWallet) Address() (string, error) {
	addr, err := btc.externalAddress()
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// NewAddress returns a new address from the wallet. This satisfies the
// NewAddresser interface.
func (btc *baseWallet) NewAddress() (string, error) {
	return btc.Address()
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (btc *baseWallet) PayFee(address string, regFee, feeRate uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, regFee, btc.feeRateWithFallback(feeRate), false)
	if err != nil {
		btc.log.Errorf("PayFee error - address = '%s', fee = %s: %v", address, amount(regFee), err)
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
// pay the registration fee using the provided feeRate.
func (btc *baseWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	const inputCount = 5 // buffer so this estimate is higher than what PayFee uses
	if feeRate == 0 || feeRate > btc.feeRateLimit {
		feeRate = btc.fallbackFeeRate
	}
	return (dexbtc.MinimumTxOverhead + 2*dexbtc.P2PKHOutputSize + inputCount*dexbtc.RedeemP2PKHInputSize) * feeRate
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
func (btc *baseWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(feeRate), true)
	if err != nil {
		btc.log.Errorf("Withdraw error - address = '%s', amount = %s: %v", address, amount(value), err)
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (btc *baseWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of atoms/byte.
func (btc *baseWallet) send(address string, val uint64, feeRate uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("address decode error: %w", err)
	}
	pay2script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("PayToAddrScript error: %w", err)
	}
	txHash, err := btc.node.sendToAddress(address, val, feeRate, subtract)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("SendToAddress error: %w", err)
	}
	txRes, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, 0, 0, asset.CoinNotFoundError
		}
		return nil, 0, 0, fmt.Errorf("failed to fetch transaction after send: %w", err)
	}

	tx, err := msgTxFromBytes(txRes.Hex)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error decoding transaction: %w", err)
	}
	for vout, txOut := range tx.TxOut {
		if bytes.Equal(txOut.PkScript, pay2script) {
			return txHash, uint32(vout), uint64(txOut.Value), nil
		}
	}
	return nil, 0, 0, fmt.Errorf("failed to locate transaction vout")
}

// SwapConfirmations gets the number of confirmations for the specified swap
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (btc *baseWallet) SwapConfirmations(_ context.Context, id dex.Bytes, contract dex.Bytes, startTime time.Time) (uint32, bool, error) {
	txHash, vout, err := decodeCoinID(id)
	if err != nil {
		return 0, false, err
	}
	pkScript, err := btc.scriptHashScript(contract)
	if err != nil {
		return 0, false, err
	}
	return btc.node.swapConfirmations(txHash, vout, pkScript, startTime)
}

// RegFeeConfirmations gets the number of confirmations for the specified output
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (btc *baseWallet) RegFeeConfirmations(_ context.Context, id dex.Bytes) (confs uint32, err error) {
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

func (btc *baseWallet) checkPeers() {
	numPeers, err := btc.node.peerCount()
	if err != nil {
		prevPeer := atomic.SwapUint32(&btc.lastPeerCount, 0)
		if prevPeer != 0 {
			btc.log.Errorf("Failed to get peer count: %v", err)
			btc.peersChange(0)
		}
		return
	}
	prevPeer := atomic.SwapUint32(&btc.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		btc.peersChange(numPeers)
	}
}

func (btc *baseWallet) monitorPeers(ctx context.Context) {
	ticker := time.NewTicker(peerCountTicker)
	defer ticker.Stop()
	for {
		btc.checkPeers()

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// watchBlocks pings for new blocks and runs the tipChange callback function
// when the block changes.
func (btc *baseWallet) watchBlocks(ctx context.Context) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()

	var walletBlock <-chan *block
	if notifier, isNotifier := btc.node.(tipNotifier); isNotifier {
		walletBlock = notifier.tipFeed()
	}

	// A polledBlock is a block found during polling, but whose broadcast has
	// been queued in anticipation of a wallet notification.
	type polledBlock struct {
		*block
		queue *time.Timer
	}

	// queuedBlock is the currently queued, polling-discovered block that will
	// be broadcast after a timeout if the wallet doesn't send the matching
	// notification.
	var queuedBlock *polledBlock

	for {
		select {

		// Poll for the block. If the wallet offers tip reports, delay reporting
		// the tip to give the wallet a moment to request and scan block data.
		case <-ticker.C:
			newTipHash, err := btc.node.getBestBlockHash()
			if err != nil {
				go btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
				continue
			}

			if queuedBlock != nil && *newTipHash == queuedBlock.block.hash {
				continue
			}

			btc.tipMtx.RLock()
			sameTip := btc.currentTip.hash == *newTipHash
			btc.tipMtx.RUnlock()
			if sameTip {
				continue
			}

			newTip, err := btc.blockFromHash(newTipHash)
			if err != nil {
				go btc.tipChange(fmt.Errorf("error setting new tip: %w", err))
				continue
			}

			// If the wallet is not offering tip reports, send this one right
			// away.
			if walletBlock == nil {
				btc.reportNewTip(ctx, newTip)
			} else {
				// Queue it for reporting, but don't send it right away. Give the
				// wallet a chance to provide their block update. SPV wallet may
				// need more time after storing the block header to fetch and
				// scan filters and issue the FilteredBlockConnected report.
				if queuedBlock != nil {
					queuedBlock.queue.Stop()
				}
				blockAllowance := walletBlockAllowance
				syncStatus, err := btc.node.syncStatus()
				if err != nil {
					btc.log.Errorf("Error retreiving sync status before queuing polled block: %v", err)
				} else if syncStatus.Syncing {
					blockAllowance *= 10
				}
				queuedBlock = &polledBlock{
					block: newTip,
					queue: time.AfterFunc(blockAllowance, func() {
						btc.log.Warnf("Reporting a block found in polling that the wallet apparently "+
							"never reported: %d %s. If you see this message repeatedly, it may indicate "+
							"an issue with the wallet.", newTip.height, newTip.hash)
						btc.reportNewTip(ctx, newTip)
					}),
				}
			}

		// Tip reports from the wallet are always sent, and we'll clear any
		// queued polled block that would appear to be superceded by this one.
		case walletTip := <-walletBlock:
			if queuedBlock != nil && walletTip.height >= queuedBlock.height {
				if !queuedBlock.queue.Stop() && walletTip.hash == queuedBlock.hash {
					continue
				}
				queuedBlock = nil
			}
			btc.reportNewTip(ctx, walletTip)

		case <-ctx.Done():
			return
		}
	}
}

// prepareRedemptionRequestsForBlockCheck prepares a copy of the
// findRedemptionQueue, checking for missing block data along the way.
func (btc *baseWallet) prepareRedemptionRequestsForBlockCheck() []*findRedemptionReq {
	// Search for contract redemption in new blocks if there
	// are contracts pending redemption.
	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()
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

// reportNewTip sets the currentTip. The tipChange callback function is invoked
// and a goroutine is started to check if any contracts in the
// findRedemptionQueue are redeemed in the new blocks.
func (btc *baseWallet) reportNewTip(ctx context.Context, newTip *block) {
	btc.tipMtx.Lock()
	defer btc.tipMtx.Unlock()

	prevTip := btc.currentTip
	btc.currentTip = newTip
	btc.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTip.height, newTip.hash)
	go btc.tipChange(nil)

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

	var startPoint *block
	// Check if the previous tip is still part of the mainchain (prevTip confs >= 0).
	// Redemption search would typically resume from prevTipHeight + 1 unless the
	// previous tip was re-orged out of the mainchain, in which case redemption
	// search will resume from the mainchain ancestor of the previous tip.
	prevTipHeader, err := btc.node.getBlockHeader(&prevTip.hash)
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
		ancestorBlockHash, err := chainhash.NewHashFromStr(prevTipHeader.PreviousBlockHash)
		if err != nil {
			notifyFatalFindRedemptionError("hash decode error for block %s: %w", prevTipHeader.PreviousBlockHash, err)
			return
		}
		for {
			aBlock, err := btc.node.getBlockHeader(ancestorBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("getBlockHeader error for block %s: %w", ancestorBlockHash, err)
				return
			}
			if aBlock.Confirmations > -1 {
				// Found the mainchain ancestor of previous tip.
				startPoint = &block{height: aBlock.Height, hash: *ancestorBlockHash}
				btc.log.Debugf("reorg detected from height %d to %d", aBlock.Height, newTip.height)
				break
			}
			if aBlock.Height == 0 {
				// Crawled back to genesis block without finding a mainchain ancestor
				// for the previous tip. Should never happen!
				notifyFatalFindRedemptionError("no mainchain ancestor for orphaned block %s", prevTipHeader.Hash)
				return
			}
			ancestorBlockHash, err = chainhash.NewHashFromStr(aBlock.PreviousBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("hash decode error for block %s: %w", prevTipHeader.PreviousBlockHash, err)
				return
			}
		}

	case newTip.height-prevTipHeader.Height > 1:
		// 2 or more blocks mined since last tip, start at prevTip height + 1.
		afterPrivTip := prevTipHeader.Height + 1
		hashAfterPrevTip, err := btc.node.getBlockHash(afterPrivTip)
		if err != nil {
			notifyFatalFindRedemptionError("getBlockHash error for height %d: %w", afterPrivTip, err)
			return
		}
		startPoint = &block{hash: *hashAfterPrevTip, height: afterPrivTip}

	default:
		// Just 1 new block since last tip report, search the lone block.
		startPoint = newTip
	}

	if len(reqs) > 0 {
		go btc.tryRedemptionRequests(ctx, &startPoint.hash, reqs)
	}
}

// trySetRedemptionRequestBlock should be called with findRedemptionMtx Lock'ed.
func (btc *baseWallet) trySetRedemptionRequestBlock(req *findRedemptionReq) {
	tx, err := btc.node.getWalletTransaction(&req.outPt.txHash)
	if err != nil {
		btc.log.Errorf("getWalletTransaction error for FindRedemption transaction: %v", err)
		return
	}

	if tx.BlockHash == "" {
		return
	}
	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		btc.log.Errorf("error decoding block hash %q: %v", tx.BlockHash, err)
		return
	}

	blockHeight, err := btc.checkRedemptionBlockDetails(req.outPt, blockHash, req.pkScript)
	if err != nil {
		btc.log.Error(err)
		return
	}
	// Don't update the findRedemptionReq, since the findRedemptionMtx only
	// protects the map.
	req = &findRedemptionReq{
		outPt:        req.outPt,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   req.resultChan,
		pkScript:     req.pkScript,
		contractHash: req.contractHash,
	}
	btc.findRedemptionQueue[req.outPt] = req
}

// checkRedemptionBlockDetails retrieves the block at blockStr and checks that
// the provided pkScript matches the specified outpoint. The transaction's
// block height is returned.
func (btc *baseWallet) checkRedemptionBlockDetails(outPt outPoint, blockHash *chainhash.Hash, pkScript []byte) (int32, error) {
	blockHeight, err := btc.node.getBlockHeight(blockHash)
	if err != nil {
		return 0, fmt.Errorf("GetBlockHeight for redemption block %s error: %w", blockHash, err)
	}
	blk, err := btc.node.getBlock(*blockHash)
	if err != nil {
		return 0, fmt.Errorf("error retrieving redemption block for %s: %w", blockHash, err)
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
		return 0, fmt.Errorf("transaction %s not found in block %s", outPt.txHash, blockHash)
	}
	if uint32(len(tx.TxOut)) < outPt.vout+1 {
		return 0, fmt.Errorf("no output %d in redemption transaction %s found in block %s", outPt.vout, outPt.txHash, blockHash)
	}
	if !bytes.Equal(tx.TxOut[outPt.vout].PkScript, pkScript) {
		return 0, fmt.Errorf("pubkey script mismatch for redemption at %s", outPt)
	}

	return blockHeight, nil
}

func (btc *baseWallet) blockFromHash(hash *chainhash.Hash) (*block, error) {
	blk, err := btc.node.getBlockHeader(hash)
	if err != nil {
		return nil, fmt.Errorf("getBlockHeader error for hash %s: %w", hash, err)
	}
	return &block{hash: *hash, height: blk.Height}, nil
}

// convertCoin converts the asset.Coin to an output.
func (btc *baseWallet) convertCoin(coin asset.Coin) (*output, error) {
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
func (btc *baseWallet) sendWithReturn(baseTx *wire.MsgTx, addr btcutil.Address,
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
func (btc *baseWallet) signTxAndAddChange(baseTx *wire.MsgTx, addr btcutil.Address,
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

func (btc *baseWallet) broadcastTx(signedTx *wire.MsgTx) error {
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
func (btc *baseWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address, val uint64) (sig, pubkey []byte, err error) {
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
func (btc *baseWallet) createWitnessSig(tx *wire.MsgTx, idx int, pkScript []byte,
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
// spendableUTXOs should only be called with the fundingMtx RLock'ed.
func (btc *baseWallet) spendableUTXOs(confs uint32) ([]*compositeUTXO, map[outPoint]*compositeUTXO, uint64, error) {
	unspents, err := btc.node.listUnspent()
	if err != nil {
		return nil, nil, 0, err
	}
	utxos, utxoMap, sum, err := convertUnspent(confs, unspents, btc.chainParams)
	if err != nil {
		return nil, nil, 0, err
	}
	for _, utxo := range utxos {
		// Guard against inconsistencies between the wallet's view of
		// spendable unlocked UTXOs and ExchangeWallet's. e.g. User manually
		// unlocked something or even restarted the wallet software.
		pt := newOutPoint(utxo.txHash, utxo.vout)
		if btc.fundingCoins[pt] != nil {
			btc.log.Warnf("Known order-funding coin %s returned by listunspent!", pt)
			// TODO: Consider relocking the coin in the wallet.
			//continue
		}
	}
	return utxos, utxoMap, sum, nil
}

func convertUnspent(confs uint32, unspents []*ListUnspentResult, chainParams *chaincfg.Params) ([]*compositeUTXO, map[outPoint]*compositeUTXO, uint64, error) {
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

			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, chainParams)
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
			utxoMap[newOutPoint(txHash, txout.Vout)] = utxo
			sum += toSatoshi(txout.Amount)
		}
	}
	return utxos, utxoMap, sum, nil
}

// lockedSats is the total value of locked outputs, as locked with LockUnspent.
func (btc *baseWallet) lockedSats() (uint64, error) {
	lockedOutpoints, err := btc.node.listLockUnspent()
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
		tx, err := btc.node.getWalletTransaction(txHash)
		if err != nil {
			return 0, err
		}
		txOut, err := txOutFromTxBytes(tx.Hex, rpcOP.Vout)
		if err != nil {
			return 0, err
		}
		sum += uint64(txOut.Value)
	}
	return sum, nil
}

// wireBytes dumps the serialized transaction bytes.
func (btc *baseWallet) wireBytes(tx *wire.MsgTx) []byte {
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
	return uint64(math.Round(v * conventionalConversionFactor))
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
func (btc *baseWallet) externalAddress() (btcutil.Address, error) {
	if btc.segwit {
		return btc.node.addressWPKH()
	}
	return btc.node.addressPKH()
}

// hashContract hashes the contract for use in a p2sh or p2wsh pubkey script.
// The hash function used depends on whether the wallet is configured for
// segwit. Non-segwit uses Hash160, segwit uses SHA256.
func (btc *baseWallet) hashContract(contract []byte) []byte {
	return hashContract(btc.segwit, contract)
}

func hashContract(segwit bool, contract []byte) []byte {
	if segwit {
		h := sha256.Sum256(contract) // BIP141
		return h[:]
	}
	return btcutil.Hash160(contract) // BIP16
}

// scriptHashAddress returns a new p2sh or p2wsh address, depending on whether
// the wallet is configured for segwit.
func (btc *baseWallet) scriptHashAddress(contract []byte) (btcutil.Address, error) {
	return scriptHashAddress(btc.segwit, contract, btc.chainParams)
}

func (btc *baseWallet) scriptHashScript(contract []byte) ([]byte, error) {
	addr, err := btc.scriptHashAddress(contract)
	if err != nil {
		return nil, err
	}
	return txscript.PayToAddrScript(addr)
}

func scriptHashAddress(segwit bool, contract []byte, chainParams *chaincfg.Params) (btcutil.Address, error) {
	if segwit {
		return btcutil.NewAddressWitnessScriptHash(hashContract(segwit, contract), chainParams)
	}
	return btcutil.NewAddressScriptHash(contract, chainParams)
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

// findRedemptionsInTx searches the MsgTx for the redemptions for the specified
// swaps.
func findRedemptionsInTx(ctx context.Context, segwit bool, reqs map[outPoint]*findRedemptionReq, msgTx *wire.MsgTx,
	chainParams *chaincfg.Params) (discovered map[outPoint]*findRedemptionResult) {

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	for vin, txIn := range msgTx.TxIn {
		if ctx.Err() != nil {
			return discovered
		}
		poHash, poVout := txIn.PreviousOutPoint.Hash, txIn.PreviousOutPoint.Index
		for outPt, req := range reqs {
			if discovered[outPt] != nil {
				continue
			}
			if outPt.txHash == poHash && outPt.vout == poVout {
				// Match!
				txHash := msgTx.TxHash()
				secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript, req.contractHash[:], segwit, chainParams)
				if err != nil {
					req.fail("no secret extracted from redemption input %s:%d for swap output %s: %v",
						txHash, vin, outPt, err)
					continue
				}
				discovered[outPt] = &findRedemptionResult{
					redemptionCoinID: toCoinID(&txHash, uint32(vin)),
					secret:           secret,
				}
			}
		}
	}
	return
}
