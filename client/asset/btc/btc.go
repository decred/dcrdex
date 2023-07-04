// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
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
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/rpcclient/v8"
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

	minNetworkVersion  = 210000
	minProtocolVersion = 70015
	// version which descriptor wallets have been introduced.
	minDescriptorVersion = 220000

	// splitTxBaggage is the total number of additional bytes associated with
	// using a split transaction to fund a swap.
	splitTxBaggage = dexbtc.MinimumTxOverhead + dexbtc.RedeemP2PKHInputSize + 2*dexbtc.P2PKHOutputSize
	// splitTxBaggageSegwit it the analogue of splitTxBaggage for segwit.
	// We include the 2 bytes for marker and flag.
	splitTxBaggageSegwit = dexbtc.MinimumTxOverhead + 2*dexbtc.P2WPKHOutputSize +
		dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + dexbtc.SegwitMarkerAndFlagWeight + 3) / 4)

	walletTypeLegacy   = ""
	walletTypeRPC      = "bitcoindRPC"
	walletTypeSPV      = "SPV"
	walletTypeElectrum = "electrumRPC"

	swapFeeBumpKey      = "swapfeebump"
	splitKey            = "swapsplit"
	multiSplitKey       = "multisplit"
	multiSplitBufferKey = "multisplitbuffer"
	redeemFeeBumpFee    = "redeemfeebump"
	// externalApiUrl is the URL of the external API in case of fallback.
	externalApiUrl = "https://mempool.space/api/"
	// testnetExternalApiUrl is the URL of the testnet external API in case of
	// fallback.
	testnetExternalApiUrl = "https://mempool.space/testnet/api/"

	// requiredRedeemConfirms is the amount of confirms a redeem transaction
	// needs before the trade is considered confirmed. The redeem is
	// monitored until this number of confirms is reached.
	requiredRedeemConfirms = 1
)

const (
	minTimeBeforeAcceleration uint64 = 3600 // 1 hour
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

	ElectrumConfigOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Electrum's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Electrum's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Electrum's 'rpcport' (if not set with rpcbind)",
		},
		{
			Key:          "rpcbind", // match RPCConfig struct field tags
			DisplayName:  "JSON-RPC Address",
			Description:  "Electrum's 'rpchost' <addr> or <addr>:<port>",
			DefaultValue: "127.0.0.1",
		},
		{
			Key:          "walletname", // match RPCConfig struct field tags
			DisplayName:  "Wallet File",
			Description:  "Full path to the wallet file (empty is default_wallet)",
			DefaultValue: "", // empty string, not a nil interface
		},
	}

	// 02 Jun 21 21:12 CDT
	defaultWalletBirthdayUnix = 1622668320
	defaultWalletBirthday     = time.Unix(int64(defaultWalletBirthdayUnix), 0)

	rpcWalletDefinition = &asset.WalletDefinition{
		Type:              walletTypeRPC,
		Tab:               "External",
		Description:       "Connect to bitcoind",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"),
		ConfigOpts:        append(RPCConfigOpts("Bitcoin", "8332"), CommonConfigOpts("BTC", true)...),
	}
	spvWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeSPV,
		Tab:         "Native",
		Description: "Use the built-in SPV wallet",
		ConfigOpts:  append(SPVConfigOpts("BTC"), CommonConfigOpts("BTC", true)...),
		Seeded:      true,
	}

	electrumWalletDefinition = &asset.WalletDefinition{
		Type:        walletTypeElectrum,
		Tab:         "Electrum (external)",
		Description: "Use an external Electrum Wallet",
		// json: DefaultConfigPath: filepath.Join(btcutil.AppDataDir("electrum", false), "config"), // e.g. ~/.electrum/config
		ConfigOpts: append(append(ElectrumConfigOpts, CommonConfigOpts("BTC", false)...), apiFallbackOpt(false)),
	}

	// WalletInfo defines some general information about a Bitcoin wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Bitcoin",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexbtc.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			spvWalletDefinition,
			rpcWalletDefinition,
			electrumWalletDefinition,
		},
		LegacyWalletIndex: 1,
	}
)

func apiFallbackOpt(defaultV bool) *asset.ConfigOption {
	return &asset.ConfigOption{
		Key:         "apifeefallback",
		DisplayName: "External fee rate estimates",
		Description: "Allow fee rate estimation from a block explorer API. " +
			"This is useful as a fallback for SPV wallets and RPC wallets " +
			"that have recently been started.",
		IsBoolean:    true,
		DefaultValue: defaultV,
	}
}

// CommonConfigOpts are the common options that the Wallets recognize.
func CommonConfigOpts(symbol string /* upper-case */, withApiFallback bool) []*asset.ConfigOption {
	opts := []*asset.ConfigOption{
		{
			Key:         "fallbackfee",
			DisplayName: "Fallback fee rate",
			Description: fmt.Sprintf("The fee rate to use for sending or withdrawing funds and fee payment when"+
				" estimatesmartfee is not available. Units: %s/kB", symbol),
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: fmt.Sprintf("This is the highest network fee rate you are willing to "+
				"pay on swap transactions. If feeratelimit is lower than a market's "+
				"maxfeerate, you will not be able to trade on that market with this "+
				"wallet.  Units: %s/kB", symbol),
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
				"mining fees are paid.",
			IsBoolean:    true,
			DefaultValue: false,
		},
	}

	if withApiFallback {
		opts = append(opts, apiFallbackOpt(true))
	}
	return opts
}

// SPVConfigOpts are the options common to built-in SPV wallets.
func SPVConfigOpts(symbol string) []*asset.ConfigOption {
	return []*asset.ConfigOption{{
		Key:         "walletbirthday",
		DisplayName: "Wallet Birthday",
		Description: fmt.Sprintf("This is the date the wallet starts scanning the blockchain "+
			"for transactions related to this wallet. If reconfiguring an existing "+
			"wallet, this may start a rescan if the new birthday is older. This "+
			"option is disabled if there are currently active %s trades.", symbol),
		DefaultValue: defaultWalletBirthdayUnix,
		MaxValue:     "now",
		// This MinValue must be removed if we start supporting importing private keys
		MinValue:          defaultWalletBirthdayUnix,
		IsDate:            true,
		DisableWhenActive: true,
		IsBirthdayConfig:  true,
	}}
}

// RPCConfigOpts are the settings that are used to connect to and external RPC
// wallet.
func RPCConfigOpts(name, rpcPort string) []*asset.ConfigOption {
	return []*asset.ConfigOption{
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "The wallet name",
		},
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: fmt.Sprintf("%s's 'rpcuser' setting", name),
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: fmt.Sprintf("%s's 'rpcpassword' setting", name),
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
			DefaultValue: rpcPort,
		},
	}
}

// TxInSigner is a transaction input signer. In addition to the standard Bitcoin
// arguments, TxInSigner receives all values and pubkey scripts for previous
// outpoints spent in this transaction.
type TxInSigner func(tx *wire.MsgTx, idx int, subScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, vals []int64, prevScripts [][]byte) ([]byte, error)

// BTCCloneCFG holds clone specific parameters.
type BTCCloneCFG struct {
	WalletCFG         *asset.WalletConfig
	MinNetworkVersion uint64
	WalletInfo        *asset.WalletInfo
	Symbol            string
	Logger            dex.Logger
	Network           dex.Network
	ChainParams       *chaincfg.Params
	// Ports is the default wallet RPC tcp ports used when undefined in
	// WalletConfig.
	Ports               dexbtc.NetPorts
	DefaultFallbackFee  uint64 // sats/byte
	DefaultFeeRateLimit uint64 // sats/byte
	// LegacyBalance is for clones that don't yet support the 'getbalances' RPC
	// call.
	LegacyBalance bool
	// ZECStyleBalance is for clones that don't support getbalances or
	// walletinfo, and don't take an account name argument.
	ZECStyleBalance bool
	// If segwit is false, legacy addresses and contracts will be used. This
	// setting must match the configuration of the server's asset backend.
	Segwit bool
	// LegacyRawFeeLimit can be true if the RPC only supports the boolean
	// allowHighFees argument to the sendrawtransaction RPC.
	LegacyRawFeeLimit bool
	// InitTxSize is the size of a swap initiation transaction with a single
	// input i.e. chained swaps.
	InitTxSize uint32
	// InitTxSizeBase is the size of a swap initiation transaction with no
	// inputs. This is used to accurately determine the size of the first swap
	// in a chain when considered with the actual inputs.
	InitTxSizeBase uint32
	// PrivKeyFunc is an optional function to get a private key for an address
	// from the wallet. If not given the usual dumpprivkey RPC will be used.
	PrivKeyFunc func(addr string) (*btcec.PrivateKey, error)
	// AddrFunc is an optional function to produce new addresses. If AddrFunc
	// is provided, the regular getnewaddress and getrawchangeaddress methods
	// will not be used, and AddrFunc will be used instead.
	AddrFunc func() (btcutil.Address, error)
	// AddressDecoder is an optional argument that can decode an address string
	// into btcutil.Address. If AddressDecoder is not supplied,
	// btcutil.DecodeAddress will be used.
	AddressDecoder dexbtc.AddressDecoder // string => btcutil.Address
	// AddressStringer is an optional argument that can encode a btcutil.Address
	// into an address string. If AddressStringer is not supplied, the
	// (btcutil.Address).String method will be used.
	AddressStringer dexbtc.AddressStringer // btcutil.Address => string, may be an override or just the String method
	// BlockDeserializer can be used in place of (*wire.MsgBlock).Deserialize.
	BlockDeserializer func([]byte) (*wire.MsgBlock, error)
	// ArglessChangeAddrRPC can be true if the getrawchangeaddress takes no
	// address-type argument.
	ArglessChangeAddrRPC bool
	// NonSegwitSigner can be true if the transaction signature hash data is not
	// the standard for non-segwit Bitcoin. If nil, txscript.
	NonSegwitSigner TxInSigner
	// ConnectFunc, if provided, is called by the RPC client at the end of the
	// (*rpcClient).connect method. Errors returned by ConnectFunc will preclude
	// the starting of goroutines associated with block and peer monitoring.
	ConnectFunc func() error
	// FeeEstimator provides a way to get fees given an RawRequest-enabled
	// client and a confirmation target.
	FeeEstimator func(context.Context, RawRequester, uint64) (uint64, error)
	// ExternalFeeEstimator should be supplied if the clone provides the
	// apifeefallback ConfigOpt. TODO: confTarget uint64
	ExternalFeeEstimator func(context.Context, dex.Network) (uint64, error)
	// OmitAddressType causes the address type (bech32, legacy) to be omitted
	// from calls to getnewaddress.
	OmitAddressType bool
	// LegacySignTxRPC causes the RPC client to use the signrawtransaction
	// endpoint instead of the signrawtransactionwithwallet endpoint.
	LegacySignTxRPC bool
	// BooleanGetBlockRPC causes the RPC client to use a boolean second argument
	// for the getblock endpoint, instead of Bitcoin's numeric.
	BooleanGetBlockRPC bool
	// NumericGetRawRPC uses a numeric boolean indicator for the
	// getrawtransaction RPC.
	NumericGetRawRPC bool
	// LegacyValidateAddressRPC uses the validateaddress endpoint instead of
	// getaddressinfo in order to discover ownership of an address.
	LegacyValidateAddressRPC bool
	// SingularWallet signals that the node software supports only one wallet,
	// so the RPC endpoint does not have a /wallet/{walletname} path.
	SingularWallet bool
	// UnlockSpends manually unlocks outputs as they are spent. Most assets will
	// unlock wallet outputs automatically as they are spent.
	UnlockSpends bool
	// ConstantDustLimit is used if an asset enforces a dust limit (minimum
	// output value) that doesn't depend on the serialized size of the output.
	// If ConstantDustLimit is zero, dexbtc.IsDust is used.
	ConstantDustLimit uint64
	// TxDeserializer is an optional function used to deserialize a transaction.
	TxDeserializer func([]byte) (*wire.MsgTx, error)
	// TxSerializer is an optional function used to serialize a transaction.
	TxSerializer func(*wire.MsgTx) ([]byte, error)
	// TxHasher is a function that generates a tx hash from a MsgTx.
	TxHasher func(*wire.MsgTx) *chainhash.Hash
	// TxSizeCalculator is an optional function that will be used to calculate
	// the size of a transaction.
	TxSizeCalculator func(*wire.MsgTx) uint64
	// TxVersion is an optional function that returns a version to use for
	// new transactions.
	TxVersion func() int32
	// ManualMedianTime causes the median time to be calculated manually.
	ManualMedianTime bool
	// OmitRPCOptionsArg is for clones that don't take an options argument.
	OmitRPCOptionsArg bool
	// AssetID is the asset ID of the clone.
	AssetID uint32
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
	recipient  btcutil.Address // caution: use stringAddr, not the Stringer
	contract   []byte
	secretHash []byte
	expiration time.Time
}

// Expiration returns the expiration time of the contract, which is the earliest
// time that a refund can be issued for an un-redeemed contract.
func (ci *auditInfo) Expiration() time.Time {
	return ci.expiration
}

// Coin returns the output as an asset.Coin.
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

// RPCConfig adds a wallet name to the basic configuration.
type RPCConfig struct {
	dexbtc.RPCConfig `ini:",extends"`
	WalletName       string `ini:"walletname"`
}

// RPCWalletConfig is a combination of RPCConfig and WalletConfig. Used for a
// wallet based on a bitcoind-like RPC API.
type RPCWalletConfig struct {
	RPCConfig    `ini:",extends"`
	WalletConfig `ini:",extends"`
}

// WalletConfig are wallet-level configuration settings.
type WalletConfig struct {
	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	FeeRateLimit     float64 `ini:"feeratelimit"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
	ActivelyUsed     bool    `ini:"special_activelyUsed"` // injected by core
	Birthday         uint64  `ini:"walletbirthday"`       // SPV
	ApiFeeFallback   bool    `ini:"apifeefallback"`
}

// AdjustedBirthday converts WalletConfig.Birthday to a time.Time, and adjusts
// it so that defaultWalletBirthday <= WalletConfig.Birthday <= now.
func (cfg *WalletConfig) AdjustedBirthday() time.Time {
	bday := time.Unix(int64(cfg.Birthday), 0)
	now := time.Now()
	if defaultWalletBirthday.After(bday) {
		return defaultWalletBirthday
	} else if bday.After(now) {
		return now
	} else {
		return bday
	}
}

func readBaseWalletConfig(walletCfg *WalletConfig) (*baseWalletConfig, error) {
	cfg := &baseWalletConfig{}
	// if values not specified, use defaults. As they are validated as BTC/KB,
	// we need to convert first.
	if walletCfg.FallbackFeeRate == 0 {
		walletCfg.FallbackFeeRate = float64(defaultFee) * 1000 / 1e8
	}
	if walletCfg.FeeRateLimit == 0 {
		walletCfg.FeeRateLimit = float64(defaultFeeRateLimit) * 1000 / 1e8
	}
	if walletCfg.RedeemConfTarget == 0 {
		walletCfg.RedeemConfTarget = defaultRedeemConfTarget
	}
	// If set in the user config, the fallback fee will be in conventional units
	// per kB, e.g. BTC/kB. Translate that to sats/byte.
	cfg.fallbackFeeRate = toSatoshi(walletCfg.FallbackFeeRate / 1000)
	if cfg.fallbackFeeRate == 0 {
		return nil, fmt.Errorf("fallback fee rate limit is smaller than the minimum 1000 sats/byte: %v",
			walletCfg.FallbackFeeRate)
	}
	// If set in the user config, the fee rate limit will be in units of BTC/KB.
	// Convert to sats/byte & error if value is smaller than smallest unit.
	cfg.feeRateLimit = toSatoshi(walletCfg.FeeRateLimit / 1000)
	if cfg.feeRateLimit == 0 {
		return nil, fmt.Errorf("fee rate limit is smaller than the minimum 1000 sats/byte: %v",
			walletCfg.FeeRateLimit)
	}

	cfg.redeemConfTarget = walletCfg.RedeemConfTarget
	cfg.useSplitTx = walletCfg.UseSplitTx
	cfg.apiFeeFallback = walletCfg.ApiFeeFallback

	return cfg, nil
}

// readRPCWalletConfig parses the settings map into a *RPCWalletConfig.
func readRPCWalletConfig(settings map[string]string, symbol string, net dex.Network, ports dexbtc.NetPorts) (cfg *RPCWalletConfig, err error) {
	cfg = new(RPCWalletConfig)
	err = config.Unmapify(settings, cfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing rpc wallet config: %w", err)
	}
	err = dexbtc.CheckRPCConfig(&cfg.RPCConfig.RPCConfig, symbol, net, ports)
	return
}

// parseRPCWalletConfig parses a *RPCWalletConfig from the settings map and
// creates the unconnected *rpcclient.Client.
func parseRPCWalletConfig(settings map[string]string, symbol string, net dex.Network,
	ports dexbtc.NetPorts, singularWallet bool) (*RPCWalletConfig, *rpcclient.Client, error) {
	cfg, err := readRPCWalletConfig(settings, symbol, net, ports)
	if err != nil {
		return nil, nil, err
	}

	cl, err := newRPCConnection(cfg, singularWallet)
	if err != nil {
		return nil, nil, err
	}

	return cfg, cl, nil
}

// newRPCConnection creates a new RPC client.
func newRPCConnection(cfg *RPCWalletConfig, singularWallet bool) (*rpcclient.Client, error) {
	endpoint := cfg.RPCBind
	if !singularWallet {
		endpoint += "/wallet/" + cfg.WalletName
	}

	return rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         endpoint,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
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
	dir := filepath.Join(dataDir, chainParams.Name)
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	loader := wallet.NewLoader(chainParams, dir, true, dbTimeout, 250)
	return loader.WalletExists()
}

// createConfig combines the configuration settings used for wallet creation.
type createConfig struct {
	WalletConfig `ini:",extends"`
	RecoveryCfg  `ini:",extends"`
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

	cfg := new(createConfig)
	err = config.Unmapify(params.Settings, cfg)
	if err != nil {
		return err
	}

	_, err = readBaseWalletConfig(&cfg.WalletConfig)
	if err != nil {
		return err
	}

	dir := filepath.Join(params.DataDir, chainParams.Name)
	return createSPVWallet(params.Pass, params.Seed, cfg.AdjustedBirthday(), dir,
		params.Logger, cfg.NumExternalAddresses, cfg.NumInternalAddresses, chainParams)
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
		return "<invalid>", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// swapOptions captures the available Swap options. Tagged to be used with
// config.Unmapify to decode e.g. asset.Order.Options.
type swapOptions struct {
	Split   *bool    `ini:"swapsplit"`
	FeeBump *float64 `ini:"swapfeebump"`
}

func (s *swapOptions) feeBump() (float64, error) {
	bump := 1.0
	if s.FeeBump != nil {
		bump = *s.FeeBump
		if bump > 2.0 {
			return 0, fmt.Errorf("fee bump %f is higher than the 2.0 limit", bump)
		}
		if bump < 1.0 {
			return 0, fmt.Errorf("fee bump %f is lower than 1", bump)
		}
	}
	return bump, nil
}

// fundMultiOptions are the possible order options when calling FundMultiOrder.
type fundMultiOptions struct {
	// Split, if true, and multi-order cannot be funded with the existing UTXOs
	// in the wallet without going over the maxLock limit, a split transaction
	// will be created with one output per order.
	//
	// Use the multiSplitKey const defined above in the options map to set this option.
	Split *bool
	// SplitBuffer, if set, will instruct the wallet to add a buffer onto each
	// output of the multi-order split transaction (if the split is needed).
	// SplitBuffer is defined as a percentage of the output. If a .1 BTC output
	// is required for an order and SplitBuffer is set to 5, a .105 BTC output
	// will be created.
	//
	// The motivation for this is to assist market makers in having to do the
	// least amount of splits as possible. It is useful when BTC is the quote
	// asset on a market, and the price is increasing. During a market maker's
	// operation, it will frequently have to cancel and replace orders as the
	// rate moves. If BTC is the quote asset on a market, and the rate has
	// lightly increased, the market maker will need to lock slightly more of
	// the quote asset for the same amount of lots of the base asset. If there
	// is no split buffer, this may necessitate a new split transaction.
	//
	// Use the multiSplitBufferKey const defined above in the options map to set this.
	SplitBuffer *uint64
}

func decodeFundMultiOptions(options map[string]string) (*fundMultiOptions, error) {
	opts := new(fundMultiOptions)
	if options == nil {
		return opts, nil
	}

	if split, ok := options[multiSplitKey]; ok {
		b, err := strconv.ParseBool(split)
		if err != nil {
			return nil, fmt.Errorf("error parsing split option: %w", err)
		}
		opts.Split = &b
	}

	if splitBuffer, ok := options[multiSplitBufferKey]; ok {
		b, err := strconv.ParseUint(splitBuffer, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing split buffer option: %w", err)
		}
		opts.SplitBuffer = &b
	}

	return opts, nil
}

// redeemOptions are order options that apply to redemptions.
type redeemOptions struct {
	FeeBump *float64 `ini:"redeemfeebump"`
}

func init() {
	asset.Register(BipID, &Driver{})
}

// baseWalletConfig is the validated, unit-converted, user-configurable wallet
// settings.
type baseWalletConfig struct {
	fallbackFeeRate  uint64 // atoms/byte
	feeRateLimit     uint64 // atoms/byte
	redeemConfTarget uint64
	useSplitTx       bool
	apiFeeFallback   bool
}

// baseWallet is a wallet backend for Bitcoin. The backend is how the DEX
// client app communicates with the BTC blockchain and wallet. baseWallet
// satisfies the dex.Wallet interface.
type baseWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	cfgV              atomic.Value // *baseWalletConfig
	node              Wallet
	walletInfo        *asset.WalletInfo
	cloneParams       *BTCCloneCFG
	chainParams       *chaincfg.Params
	log               dex.Logger
	symbol            string
	tipChange         func(error)
	lastPeerCount     uint32
	peersChange       func(uint32, error)
	minNetworkVersion uint64
	dustLimit         uint64
	initTxSize        uint64
	initTxSizeBase    uint64
	useLegacyBalance  bool
	zecStyleBalance   bool
	segwit            bool
	signNonSegwit     TxInSigner
	localFeeRate      func(context.Context, RawRequester, uint64) (uint64, error)
	externalFeeRate   func(context.Context, dex.Network) (uint64, error)
	decodeAddr        dexbtc.AddressDecoder
	deserializeTx     func([]byte) (*wire.MsgTx, error)
	serializeTx       func(*wire.MsgTx) ([]byte, error)
	calcTxSize        func(*wire.MsgTx) uint64
	hashTx            func(*wire.MsgTx) *chainhash.Hash
	stringAddr        dexbtc.AddressStringer
	txVersion         func() int32
	Network           dex.Network
	ctx               context.Context // the asset subsystem starts with Connect(ctx)

	// TODO: remove currentTip and the mutex, and make it local to the
	// watchBlocks->reportNewTip call stack. The tests are reliant on current
	// internals, so this will take a little work.
	tipMtx     sync.RWMutex
	currentTip *block

	// Coins returned by Fund are cached for quick reference.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*utxo

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[outPoint]*findRedemptionReq

	reservesMtx sync.RWMutex // frequent reads for balance, infrequent updates
	// bondReservesEnforced is used to reserve unspent amounts for upcoming bond
	// transactions, including projected transaction fees, and does not include
	// amounts that are currently locked in unspent bonds, which are in
	// bondReservesUsed. When bonds are created, bondReservesEnforced is
	// decremented and bondReservesUsed are incremented; when bonds are
	// refunded, the reverse. bondReservesEnforced may become negative during
	// the unbonding process.
	bondReservesEnforced int64  // set by ReserveBondFunds, modified by bondSpent and bondLocked
	bondReservesUsed     uint64 // set by RegisterUnspent, modified by bondSpent and bondLocked
	// When bondReservesEnforced is non-zero, bondReservesNominal is the
	// cumulative of all ReserveBondFunds and RegisterUnspent input amounts,
	// with no fee padding. It includes the future and live (currently unspent)
	// bond amounts. This amount only changes via ReserveBondFunds, and it is
	// used to recognize when all reserves have been released.
	bondReservesNominal int64 // only set by ReserveBondFunds

	recycledAddrMtx sync.Mutex
	// recycledAddrs are returned, unused redemption addresses. We track these
	// to avoid issues with the gap policy.
	recycledAddrs map[string]struct{}
	recyclePath   string
}

func (w *baseWallet) fallbackFeeRate() uint64 {
	return w.cfgV.Load().(*baseWalletConfig).fallbackFeeRate
}

func (w *baseWallet) feeRateLimit() uint64 {
	return w.cfgV.Load().(*baseWalletConfig).feeRateLimit
}

func (w *baseWallet) redeemConfTarget() uint64 {
	return w.cfgV.Load().(*baseWalletConfig).redeemConfTarget
}

func (w *baseWallet) useSplitTx() bool {
	return w.cfgV.Load().(*baseWalletConfig).useSplitTx
}

func (w *baseWallet) apiFeeFallback() bool {
	return w.cfgV.Load().(*baseWalletConfig).apiFeeFallback
}

type intermediaryWallet struct {
	*baseWallet
	txFeeEstimator txFeeEstimator
	tipRedeemer    tipRedemptionWallet
}

// ExchangeWalletSPV embeds a ExchangeWallet, but also provides the Rescan
// method to implement asset.Rescanner.
type ExchangeWalletSPV struct {
	*intermediaryWallet
	*authAddOn

	spvNode *spvWallet
}

// ExchangeWalletFullNode implements Wallet and adds the FeeRate method.
type ExchangeWalletFullNode struct {
	*intermediaryWallet
	*authAddOn
}

type ExchangeWalletNoAuth struct {
	*intermediaryWallet
}

// ExchangeWalletAccelerator implements the Accelerator interface on an
// ExchangeWalletFullNode.
type ExchangeWalletAccelerator struct {
	*ExchangeWalletFullNode
}

// Check that wallets satisfy their supported interfaces.
var _ asset.Wallet = (*intermediaryWallet)(nil)
var _ asset.Accelerator = (*ExchangeWalletAccelerator)(nil)
var _ asset.Accelerator = (*ExchangeWalletSPV)(nil)
var _ asset.Withdrawer = (*baseWallet)(nil)
var _ asset.FeeRater = (*baseWallet)(nil)
var _ asset.Rescanner = (*ExchangeWalletSPV)(nil)
var _ asset.LogFiler = (*ExchangeWalletSPV)(nil)
var _ asset.Recoverer = (*ExchangeWalletSPV)(nil)
var _ asset.PeerManager = (*ExchangeWalletSPV)(nil)
var _ asset.TxFeeEstimator = (*intermediaryWallet)(nil)
var _ asset.Bonder = (*baseWallet)(nil)
var _ asset.Authenticator = (*ExchangeWalletSPV)(nil)
var _ asset.Authenticator = (*ExchangeWalletFullNode)(nil)
var _ asset.Authenticator = (*ExchangeWalletAccelerator)(nil)
var _ asset.MultiOrderFunder = (*baseWallet)(nil)
var _ asset.AddressReturner = (*baseWallet)(nil)

// RecoveryCfg is the information that is transferred from the old wallet
// to the new one when the wallet is recovered.
type RecoveryCfg struct {
	NumExternalAddresses uint32 `ini:"numexternaladdr"`
	NumInternalAddresses uint32 `ini:"numinternaladdr"`
}

// GetRecoveryCfg returns information that will help the wallet get
// back to its previous state after it is recreated. Part of the
// Recoverer interface.
func (btc *ExchangeWalletSPV) GetRecoveryCfg() (map[string]string, error) {
	internal, external, err := btc.spvNode.numDerivedAddresses()
	if err != nil {
		return nil, err
	}

	reCfg := &RecoveryCfg{
		NumInternalAddresses: internal,
		NumExternalAddresses: external,
	}
	cfg, err := config.Mapify(reCfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Destroy will delete all the wallet files so the wallet can be recreated.
// Part of the Recoverer interface.
func (btc *ExchangeWalletSPV) Move(backupDir string) error {
	err := btc.spvNode.moveWalletData(backupDir)
	if err != nil {
		return fmt.Errorf("unable to move wallet data: %w", err)
	}

	return nil
}

// Rescan satisfies the asset.Rescanner interface, and issues a rescan wallet
// command if the backend is an SPV wallet.
func (btc *ExchangeWalletSPV) Rescan(_ context.Context) error {
	atomic.StoreInt64(&btc.tipAtConnect, 0) // for progress
	// Caller should start calling SyncStatus on a ticker.
	return btc.spvNode.wallet.RescanAsync()
}

// Peers returns a list of peers that the wallet is connected to.
func (btc *ExchangeWalletSPV) Peers() ([]*asset.WalletPeer, error) {
	return btc.spvNode.peers()
}

// AddPeer connects the wallet to a new peer. The peer's address will be
// persisted and connected to each time the wallet is started up.
func (btc *ExchangeWalletSPV) AddPeer(addr string) error {
	return btc.spvNode.addPeer(addr)
}

// RemovePeer will remove a peer that was added by AddPeer. This peer may
// still be connected to by the wallet if it discovers it on it's own.
func (btc *ExchangeWalletSPV) RemovePeer(addr string) error {
	return btc.spvNode.removePeer(addr)
}

var _ asset.FeeRater = (*ExchangeWalletFullNode)(nil)
var _ asset.FeeRater = (*ExchangeWalletNoAuth)(nil)

// FeeRate satisfies asset.FeeRater.
func (btc *baseWallet) FeeRate() uint64 {
	rate, err := btc.feeRate(1)
	if err != nil {
		btc.log.Tracef("Failed to get fee rate: %v", err)
		return 0
	}
	return rate
}

// LogFilePath returns the path to the neutrino log file.
func (btc *ExchangeWalletSPV) LogFilePath() string {
	return btc.spvNode.logFilePath()
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
		// FeeEstimator must default to rpcFeeRate if not set, but set a
		// specific external estimator:
		ExternalFeeEstimator: externalFeeEstimator,
		AssetID:              BipID,
	}

	switch cfg.Type {
	case walletTypeSPV:
		return OpenSPVWallet(cloneCFG, openSPVWallet)
	case walletTypeRPC, walletTypeLegacy:
		rpcWallet, err := BTCCloneWallet(cloneCFG)
		if err != nil {
			return nil, err
		}
		return &ExchangeWalletAccelerator{rpcWallet}, nil
	case walletTypeElectrum:
		cloneCFG.Ports = dexbtc.NetPorts{} // no default ports
		return ElectrumWallet(cloneCFG)
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
	}
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *BTCCloneCFG) (*ExchangeWalletFullNode, error) {
	iw, err := btcCloneWallet(cfg)
	if err != nil {
		return nil, err
	}
	return &ExchangeWalletFullNode{iw, &authAddOn{iw.node}}, nil
}

// BTCCloneWalletNoAuth is like BTCCloneWallet but the wallet created does not
// implement asset.Authenticator.
func BTCCloneWalletNoAuth(cfg *BTCCloneCFG) (*ExchangeWalletNoAuth, error) {
	iw, err := btcCloneWallet(cfg)
	if err != nil {
		return nil, err
	}
	return &ExchangeWalletNoAuth{iw}, nil
}

// btcCloneWallet creates a wallet backend for a set of network parameters and
// default network ports.
func btcCloneWallet(cfg *BTCCloneCFG) (*intermediaryWallet, error) {
	clientCfg, client, err := parseRPCWalletConfig(cfg.WalletCFG.Settings, cfg.Symbol, cfg.Network, cfg.Ports, cfg.SingularWallet)
	if err != nil {
		return nil, err
	}

	iw, err := newRPCWallet(client, cfg, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating %s exchange wallet: %v", cfg.Symbol,
			err)
	}

	return iw, nil
}

// newRPCWallet creates the ExchangeWallet and starts the block monitor.
func newRPCWallet(requester RawRequester, cfg *BTCCloneCFG, parsedCfg *RPCWalletConfig) (*intermediaryWallet, error) {
	btc, err := newUnconnectedWallet(cfg, &parsedCfg.WalletConfig)
	if err != nil {
		return nil, err
	}

	blockDeserializer := cfg.BlockDeserializer
	if blockDeserializer == nil {
		blockDeserializer = deserializeBlock
	}

	core := &rpcCore{
		rpcConfig:                &parsedCfg.RPCConfig,
		cloneParams:              cfg,
		segwit:                   cfg.Segwit,
		decodeAddr:               btc.decodeAddr,
		stringAddr:               btc.stringAddr,
		deserializeBlock:         blockDeserializer,
		legacyRawSends:           cfg.LegacyRawFeeLimit,
		minNetworkVersion:        cfg.MinNetworkVersion,
		log:                      cfg.Logger.SubLogger("RPC"),
		chainParams:              cfg.ChainParams,
		omitAddressType:          cfg.OmitAddressType,
		legacySignTx:             cfg.LegacySignTxRPC,
		booleanGetBlock:          cfg.BooleanGetBlockRPC,
		unlockSpends:             cfg.UnlockSpends,
		deserializeTx:            btc.deserializeTx,
		serializeTx:              btc.serializeTx,
		hashTx:                   btc.hashTx,
		numericGetRawTxRPC:       cfg.NumericGetRawRPC,
		legacyValidateAddressRPC: cfg.LegacyValidateAddressRPC,
		manualMedianTime:         cfg.ManualMedianTime,
		omitRPCOptionsArg:        cfg.OmitRPCOptionsArg,
		addrFunc:                 cfg.AddrFunc,
		connectFunc:              cfg.ConnectFunc,
		privKeyFunc:              cfg.PrivKeyFunc,
	}
	core.requesterV.Store(requester)
	node := newRPCClient(core)
	btc.node = node
	return &intermediaryWallet{
		baseWallet:     btc,
		txFeeEstimator: node,
		tipRedeemer:    node,
	}, nil
}

func decodeAddress(addr string, params *chaincfg.Params) (btcutil.Address, error) {
	a, err := btcutil.DecodeAddress(addr, params)
	if err != nil {
		return nil, err
	}
	if !a.IsForNet(params) {
		return nil, errors.New("wrong network")
	}
	return a, nil
}

func newUnconnectedWallet(cfg *BTCCloneCFG, walletCfg *WalletConfig) (*baseWallet, error) {
	// Make sure we can use the specified wallet directory.
	walletDir := filepath.Join(cfg.WalletCFG.DataDir, cfg.ChainParams.Name)
	if err := os.MkdirAll(walletDir, 0744); err != nil {
		return nil, fmt.Errorf("error creating wallet directory: %w", err)
	}

	baseCfg, err := readBaseWalletConfig(walletCfg)
	if err != nil {
		return nil, err
	}

	addrDecoder := decodeAddress
	if cfg.AddressDecoder != nil {
		addrDecoder = cfg.AddressDecoder
	}

	nonSegwitSigner := rawTxInSig
	if cfg.NonSegwitSigner != nil {
		nonSegwitSigner = cfg.NonSegwitSigner
	}

	initTxSize := cfg.InitTxSize
	if initTxSize == 0 {
		if cfg.Segwit {
			initTxSize = dexbtc.InitTxSizeSegwit
		} else {
			initTxSize = dexbtc.InitTxSize
		}
	}

	initTxSizeBase := cfg.InitTxSizeBase
	if initTxSizeBase == 0 {
		if cfg.Segwit {
			initTxSizeBase = dexbtc.InitTxSizeBaseSegwit
		} else {
			initTxSizeBase = dexbtc.InitTxSizeBase
		}
	}

	txDeserializer := cfg.TxDeserializer
	if txDeserializer == nil {
		txDeserializer = msgTxFromBytes
	}

	txSerializer := cfg.TxSerializer
	if txSerializer == nil {
		txSerializer = serializeMsgTx
	}

	txSizeCalculator := cfg.TxSizeCalculator
	if txSizeCalculator == nil {
		txSizeCalculator = dexbtc.MsgTxVBytes
	}

	txHasher := cfg.TxHasher
	if txHasher == nil {
		txHasher = hashTx
	}

	addrStringer := cfg.AddressStringer
	if addrStringer == nil {
		addrStringer = stringifyAddress
	}

	txVersion := cfg.TxVersion
	if txVersion == nil {
		txVersion = func() int32 { return wire.TxVersion }
	}

	w := &baseWallet{
		symbol:              cfg.Symbol,
		chainParams:         cfg.ChainParams,
		cloneParams:         cfg,
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		peersChange:         cfg.WalletCFG.PeersChange,
		fundingCoins:        make(map[outPoint]*utxo),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		dustLimit:           cfg.ConstantDustLimit,
		useLegacyBalance:    cfg.LegacyBalance,
		zecStyleBalance:     cfg.ZECStyleBalance,
		segwit:              cfg.Segwit,
		initTxSize:          uint64(initTxSize),
		initTxSizeBase:      uint64(initTxSizeBase),
		signNonSegwit:       nonSegwitSigner,
		localFeeRate:        cfg.FeeEstimator,
		externalFeeRate:     cfg.ExternalFeeEstimator,
		decodeAddr:          addrDecoder,
		stringAddr:          addrStringer,
		walletInfo:          cfg.WalletInfo,
		deserializeTx:       txDeserializer,
		serializeTx:         txSerializer,
		hashTx:              txHasher,
		calcTxSize:          txSizeCalculator,
		txVersion:           txVersion,
		Network:             cfg.Network,
		recyclePath:         filepath.Join(walletDir, "recycled-addrs.txt"),
	}
	w.cfgV.Store(baseCfg)

	// Try to load any cached unused redemption addresses.
	b, err := os.ReadFile(w.recyclePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("error looking for recycled address file: %w", err)
	}
	addrs := strings.Split(string(b), "\n")
	w.recycledAddrs = make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		w.recycledAddrs[addr] = struct{}{}
	}

	// Default to the BTC RPC estimator (see LTC). Consumers can use
	// noLocalFeeRate or a similar dummy function to power feeRate() requests
	// with only an external fee rate source available. Otherwise, all method
	// calls must provide a rate or accept the configured fallback.
	if w.localFeeRate == nil {
		w.localFeeRate = rpcFeeRate
	}

	return w, nil
}

// noLocalFeeRate is a dummy function for BTCCloneCFG.FeeEstimator for a wallet
// instance that cannot support a local fee rate estimate but has an external
// fee rate source.
func noLocalFeeRate(ctx context.Context, rr RawRequester, u uint64) (uint64, error) {
	return 0, errors.New("no local fee rate estimate possible")
}

// OpenSPVWallet opens the previously created native SPV wallet.
func OpenSPVWallet(cfg *BTCCloneCFG, walletConstructor BTCWalletConstructor) (*ExchangeWalletSPV, error) {
	walletCfg := new(WalletConfig)
	err := config.Unmapify(cfg.WalletCFG.Settings, walletCfg)
	if err != nil {
		return nil, err
	}

	// SPV wallets without a FeeEstimator will default to any enabled external
	// fee estimator.
	if cfg.FeeEstimator == nil {
		cfg.FeeEstimator = noLocalFeeRate
	}

	btc, err := newUnconnectedWallet(cfg, walletCfg)
	if err != nil {
		return nil, err
	}

	spvw := &spvWallet{
		chainParams: cfg.ChainParams,
		cfg:         walletCfg,
		acctNum:     defaultAcctNum,
		acctName:    defaultAcctName,
		dir:         filepath.Join(cfg.WalletCFG.DataDir, cfg.ChainParams.Name),
		txBlocks:    make(map[chainhash.Hash]*hashEntry),
		checkpoints: make(map[outPoint]*scanCheckpoint),
		log:         cfg.Logger.SubLogger("SPV"),
		tipChan:     make(chan *block, 8),
		decodeAddr:  btc.decodeAddr,
	}

	spvw.wallet = walletConstructor(spvw.dir, spvw.cfg, spvw.chainParams, spvw.log)
	btc.node = spvw

	return &ExchangeWalletSPV{
		intermediaryWallet: &intermediaryWallet{
			baseWallet:     btc,
			txFeeEstimator: spvw,
			tipRedeemer:    spvw,
		},
		authAddOn: &authAddOn{spvw},
		spvNode:   spvw,
	}, nil
}

// Info returns basic information about the wallet and asset.
func (btc *baseWallet) Info() *asset.WalletInfo {
	return btc.walletInfo
}

// connect is shared between Wallet implementations that may have different
// monitoring goroutines or other configuration set after connect. For example
// an asset.Wallet implementation that embeds baseWallet may override Connect to
// perform monitoring differently, but still use this connect method to start up
// the btc.Wallet (the btc.node field).
func (btc *baseWallet) connect(ctx context.Context) (*sync.WaitGroup, error) {
	btc.ctx = ctx
	var wg sync.WaitGroup
	if err := btc.node.connect(ctx, &wg); err != nil {
		return nil, err
	}
	// Initialize the best block.
	bestBlockHdr, err := btc.node.getBestBlockHeader()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for %s: %w", btc.symbol, err)
	}
	bestBlockHash, err := chainhash.NewHashFromStr(bestBlockHdr.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid best block hash from %s node: %v", btc.symbol, err)
	}
	// Check for method unknown error for feeRate method.
	_, err = btc.feeRate(1)
	if isMethodNotFoundErr(err) {
		return nil, fmt.Errorf("fee estimation method not found. Are you configured for the correct RPC?")
	}

	bestBlock := &block{bestBlockHdr.Height, *bestBlockHash}
	btc.log.Infof("Connected wallet with current best block %v (%d)", bestBlock.hash, bestBlock.height)
	btc.tipMtx.Lock()
	btc.currentTip = bestBlock
	btc.tipMtx.Unlock()
	atomic.StoreInt64(&btc.tipAtConnect, btc.currentTip.height)

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		btc.writeRecycledAddrsToFile()
	}()

	return &wg, nil
}

// Connect connects the wallet to the btc.Wallet backend and starts monitoring
// blocks and peers. Satisfies the dex.Connector interface.
func (btc *intermediaryWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg, err := btc.connect(ctx)
	if err != nil {
		return nil, err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.watchBlocks(ctx)
		btc.cancelRedemptionSearches()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.monitorPeers(ctx)
	}()
	return wg, nil
}

func (btc *baseWallet) cancelRedemptionSearches() {
	// Close all open channels for contract redemption searches
	// to prevent leakages and ensure goroutines that are started
	// to wait on these channels end gracefully.
	btc.findRedemptionMtx.Lock()
	for contractOutpoint, req := range btc.findRedemptionQueue {
		req.fail("shutting down")
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()
}

// Reconfigure attempts to reconfigure the wallet.
func (btc *baseWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, currentAddress string) (restart bool, err error) {
	// See what the node says.
	restart, err = btc.node.reconfigure(cfg, currentAddress)
	if err != nil {
		return false, err
	}

	parsedCfg := new(RPCWalletConfig)
	if err = config.Unmapify(cfg.Settings, parsedCfg); err != nil {
		return false, err
	}
	walletCfg := &parsedCfg.WalletConfig

	// Make sure the configuration parameters are valid. If restart is required,
	// this validates the configuration parameters, preventing an ugly surprise
	// when the caller attempts to open the wallet. If no restart is required,
	// we'll swap out the configuration parameters right away.
	newCfg, err := readBaseWalletConfig(walletCfg)
	if err != nil {
		return false, err
	}
	btc.cfgV.Store(newCfg) // probably won't matter if restart/reinit required

	return restart, nil
}

// IsDust checks if the tx output's value is dust. If the dustLimit is set, it
// is compared against that, otherwise the formula in dexbtc.IsDust is used.
func (btc *baseWallet) IsDust(txOut *wire.TxOut, minRelayTxFee uint64) bool {
	if btc.dustLimit > 0 {
		return txOut.Value < int64(btc.dustLimit)
	}
	return dexbtc.IsDust(txOut, minRelayTxFee)
}

// getBlockchainInfoResult models the data returned from the getblockchaininfo
// command.
type getBlockchainInfoResult struct {
	Chain         string `json:"chain"`
	Blocks        int64  `json:"blocks"`
	Headers       int64  `json:"headers"`
	BestBlockHash string `json:"bestblockhash"`
	// InitialBlockDownload will be true if the node is still in the initial
	// block download mode.
	InitialBlockDownload *bool `json:"initialblockdownload"`
	// InitialBlockDownloadComplete will be true if this node has completed its
	// initial block download and is expected to be synced to the network.
	// Zcash uses this terminology instead of initialblockdownload.
	InitialBlockDownloadComplete *bool `json:"initial_block_download_complete"`
}

func (r *getBlockchainInfoResult) syncing() bool {
	if r.InitialBlockDownloadComplete != nil && *r.InitialBlockDownloadComplete {
		return false
	}
	if r.InitialBlockDownload != nil && *r.InitialBlockDownload {
		return true
	}
	return r.Headers-r.Blocks > 1
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

// OwnsDepositAddress indicates if the provided address can be used
// to deposit funds into the wallet.
func (btc *baseWallet) OwnsDepositAddress(address string) (bool, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams) // maybe move into the ownsAddress impls
	if err != nil {
		return false, err
	}
	return btc.node.ownsAddress(addr)
}

func (btc *baseWallet) balance() (*asset.Balance, error) {
	if btc.useLegacyBalance || btc.zecStyleBalance {
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
		Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
	}, nil
}

// Balance should return the total available funds in the wallet.
func (btc *baseWallet) Balance() (*asset.Balance, error) {
	bal, err := btc.balance()
	if err != nil {
		return nil, err
	}

	reserves := btc.reserves()
	if reserves > bal.Available {
		btc.log.Warnf("Available balance is below configured reserves: %f < %f",
			toBTC(bal.Available), toBTC(reserves))
		bal.ReservesDeficit = reserves - bal.Available
		reserves = bal.Available
	}

	bal.BondReserves = reserves
	bal.Available -= reserves
	bal.Locked += reserves

	return bal, nil
}

func bondsFeeBuffer(segwit bool, highFeeRate uint64) uint64 {
	const inputCount uint64 = 8 // plan for lots of inputs
	var largeBondTxSize uint64
	if segwit {
		largeBondTxSize = dexbtc.MinimumTxOverhead + dexbtc.P2WSHOutputSize + 1 + dexbtc.BondPushDataSize +
			dexbtc.P2WPKHOutputSize + inputCount*dexbtc.RedeemP2WPKHInputSize
	} else {
		largeBondTxSize = dexbtc.MinimumTxOverhead + dexbtc.P2SHOutputSize + 1 + dexbtc.BondPushDataSize +
			dexbtc.P2PKHOutputSize + inputCount*dexbtc.RedeemP2PKHInputSize
	}

	// Normally we can plan on just 2 parallel "tracks" (single bond overlap
	// when bonds are expired and waiting to refund) but that may increase
	// temporarily if target tier is adjusted up.
	const parallelTracks uint64 = 4
	return parallelTracks * largeBondTxSize * highFeeRate
}

// legacyBalance is used for clones that are < node version 0.18 and so don't
// have 'getbalances'.
func (btc *baseWallet) legacyBalance() (*asset.Balance, error) {
	cl, ok := btc.node.(*rpcClient)
	if !ok {
		return nil, fmt.Errorf("legacyBalance unimplemented for spv clients")
	}

	locked, err := btc.lockedSats()
	if err != nil {
		return nil, fmt.Errorf("(legacy) lockedSats error: %w", err)
	}

	if btc.zecStyleBalance {
		var bal uint64
		// args: "(dummy)" minconf includeWatchonly inZat
		if err := cl.call(methodGetBalance, anylist{"", 0, false, true}, &bal); err != nil {
			return nil, err
		}
		return &asset.Balance{
			Available: bal - locked,
			Locked:    locked,
			Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
		}, nil
	}

	walletInfo, err := cl.GetWalletInfo()
	if err != nil {
		return nil, fmt.Errorf("(legacy) GetWalletInfo error: %w", err)
	}

	return &asset.Balance{
		Available: toSatoshi(walletInfo.Balance+walletInfo.UnconfirmedBalance) - locked,
		Immature:  toSatoshi(walletInfo.ImmatureBalance),
		Locked:    locked,
		Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
	}, nil
}

// feeRate returns the current optimal fee rate in sat / byte using the
// estimatesmartfee RPC or an external API if configured and enabled.
func (btc *baseWallet) feeRate(confTarget uint64) (uint64, error) {
	// Local estimate first. localFeeRate might be a dummy function for spv
	// wallets.
	feeRate, err := btc.localFeeRate(btc.ctx, btc.node, confTarget) // e.g. rpcFeeRate
	if err == nil {
		return feeRate, nil
	}

	if !btc.apiFeeFallback() {
		return 0, err
	}
	if btc.externalFeeRate == nil {
		return 0, fmt.Errorf("external fee rate fetcher not configured")
	}

	// External estimate fallback. Error if it exceeds our limit, and the caller
	// may use btc.fallbackFeeRate(), as in targetFeeRateWithFallback.
	feeRate, err = btc.externalFeeRate(btc.ctx, btc.Network) // e.g. externalFeeEstimator
	if err != nil {
		btc.log.Errorf("Failed to get fee rate from external API: %v", err)
		return 0, err
	}
	if feeRate <= 0 || feeRate > btc.feeRateLimit() { // but fetcher shouldn't return <= 0 without error
		return 0, fmt.Errorf("external fee rate %v exceeds configured limit", feeRate)
	}
	btc.log.Debugf("Retrieved fee rate from external API: %v", feeRate)
	return feeRate, nil
}

func rpcFeeRate(ctx context.Context, rr RawRequester, confTarget uint64) (uint64, error) {
	feeResult, err := estimateSmartFee(ctx, rr, confTarget, &btcjson.EstimateModeConservative)
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
	if satPerKB <= 0 {
		return 0, errors.New("zero or negative fee rate")
	}
	return uint64(dex.IntDivUp(int64(satPerKB), 1000)), nil
}

// externalFeeEstimator gets the fee rate from the external API and returns it
// in sats/vByte.
func externalFeeEstimator(ctx context.Context, net dex.Network) (uint64, error) {
	var url string
	if net == dex.Testnet {
		url = testnetExternalApiUrl
	} else {
		url = externalApiUrl
	}
	url = url + "v1/fees/recommended"
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	httpResponse, err := http.DefaultClient.Do(r)
	if err != nil {
		return 0, err
	}
	var resp map[string]uint64
	reader := io.LimitReader(httpResponse.Body, 1<<20)
	err = json.NewDecoder(reader).Decode(&resp)
	if err != nil {
		return 0, err
	}
	httpResponse.Body.Close()

	// we use fastestFee, docs to mempool api https://mempool.space/docs/api
	feeInSat, ok := resp["fastestFee"]
	if !ok {
		return 0, errors.New("no fee rate found")
	}
	return feeInSat, nil
}

type amount uint64

func (a amount) String() string {
	return strconv.FormatFloat(btcutil.Amount(a).ToBTC(), 'f', -1, 64) // dec, but no trailing zeros
}

// targetFeeRateWithFallback attempts to get a fresh fee rate for the target
// number of confirmations, but falls back to the suggestion or fallbackFeeRate
// via feeRateWithFallback.
func (btc *baseWallet) targetFeeRateWithFallback(confTarget, feeSuggestion uint64) uint64 {
	feeRate, err := btc.feeRate(confTarget)
	if err == nil && feeRate > 0 {
		btc.log.Tracef("Obtained estimate for %d-conf fee rate, %d", confTarget, feeRate)
		return feeRate
	}
	btc.log.Tracef("no %d-conf feeRate available: %v", confTarget, err)
	return btc.feeRateWithFallback(feeSuggestion)
}

// feeRateWithFallback filters the suggested fee rate by ensuring it is within
// limits. If not, the configured fallbackFeeRate is returned and a warning
// logged.
func (btc *baseWallet) feeRateWithFallback(feeSuggestion uint64) uint64 {
	if feeSuggestion > 0 && feeSuggestion < btc.feeRateLimit() {
		btc.log.Tracef("feeRateWithFallback using caller's suggestion for fee rate, %d.",
			feeSuggestion)
		return feeSuggestion
	}
	btc.log.Warnf("Unable to get optimal fee rate, using fallback of %d", btc.fallbackFeeRate)
	return btc.fallbackFeeRate()
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
// lotSize must not be zero and will cause a panic if so.
func (btc *baseWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	_, maxEst, err := btc.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.MaxFeeRate)
	return maxEst, err
}

// maxOrder gets the estimate for MaxOrder, and also returns the
// []*compositeUTXO to be used for further order estimation without additional
// calls to listunspent.
func (btc *baseWallet) maxOrder(lotSize, feeSuggestion, maxFeeRate uint64) (utxos []*compositeUTXO, est *asset.SwapEstimate, err error) {
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
	basicFee := btc.initTxSize * maxFeeRate
	lots := avail / (lotSize + basicFee)
	for lots > 0 {
		est, _, _, err := btc.estimateSwap(lots, lotSize, feeSuggestion, maxFeeRate,
			utxos, true, 1.0)
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

// sizeUnit returns the short form of the unit used to measure size, either
// vB if segwit, else B.
func (btc *baseWallet) sizeUnit() string {
	if btc.segwit {
		return "vB"
	}
	return "B"
}

// PreSwap get order estimates and order options based on the available funds
// and user-selected options.
func (btc *baseWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	// Start with the maxOrder at the default configuration. This gets us the
	// utxo set, the network fee rate, and the wallet's maximum order size. The
	// utxo set can then be used repeatedly in estimateSwap at virtually zero
	// cost since there are no more RPC calls.
	utxos, maxEst, err := btc.maxOrder(req.LotSize, req.FeeSuggestion, req.MaxFeeRate)
	if err != nil {
		return nil, err
	}
	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	// Load the user's selected order-time options.
	customCfg := new(swapOptions)
	err = config.Unmapify(req.SelectedOptions, customCfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing selected swap options: %w", err)
	}

	// Parse the configured split transaction.
	split := btc.useSplitTx()
	if customCfg.Split != nil {
		split = *customCfg.Split
	}

	// Parse the configured fee bump.
	bump, err := customCfg.feeBump()
	if err != nil {
		return nil, err
	}

	// Get the estimate using the current configuration.
	est, _, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion,
		req.MaxFeeRate, utxos, split, bump)
	if err != nil {
		btc.log.Warnf("estimateSwap failure: %v", err)
	}

	// Always offer the split option, even for non-standing orders since
	// immediately spendable change many be desirable regardless.
	opts := []*asset.OrderOption{btc.splitOption(req, utxos, bump)}

	// Figure out what our maximum available fee bump is, within our 2x hard
	// limit.
	var maxBump float64
	var maxBumpEst *asset.SwapEstimate
	for maxBump = 2.0; maxBump > 1.01; maxBump -= 0.1 {
		if est == nil {
			break
		}
		tryEst, splitUsed, _, err := btc.estimateSwap(req.Lots, req.LotSize,
			req.FeeSuggestion, req.MaxFeeRate, utxos, split, maxBump)
		// If the split used wasn't the configured value, this option is not
		// available.
		if err == nil && split == splitUsed {
			maxBumpEst = tryEst
			break
		}
	}

	if maxBumpEst != nil {
		noBumpEst, _, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion,
			req.MaxFeeRate, utxos, split, 1.0)
		if err != nil {
			// shouldn't be possible, since we already succeeded with a higher bump.
			return nil, fmt.Errorf("error getting no-bump estimate: %w", err)
		}

		bumpLabel := "2X"
		if maxBump < 2.0 {
			bumpLabel = strconv.FormatFloat(maxBump, 'f', 1, 64) + "X"
		}

		extraFees := maxBumpEst.RealisticWorstCase - noBumpEst.RealisticWorstCase
		desc := fmt.Sprintf("Add a fee multiplier up to %.1fx (up to ~%s %s more) for faster settlement when %s network traffic is high.",
			maxBump, prettyBTC(extraFees), btc.symbol, btc.walletInfo.Name)

		opts = append(opts, &asset.OrderOption{
			ConfigOption: asset.ConfigOption{
				Key:          swapFeeBumpKey,
				DisplayName:  "Faster Swaps",
				Description:  desc,
				DefaultValue: 1.0,
			},
			XYRange: &asset.XYRange{
				Start: asset.XYRangePoint{
					Label: "1X",
					X:     1.0,
					Y:     float64(req.FeeSuggestion),
				},
				End: asset.XYRangePoint{
					Label: bumpLabel,
					X:     maxBump,
					Y:     float64(req.FeeSuggestion) * maxBump,
				},
				XUnit: "X",
				YUnit: btc.walletInfo.UnitInfo.AtomicUnit + "/" + btc.sizeUnit(),
			},
		})
	}

	return &asset.PreSwap{
		Estimate: est, // may be nil so we can present options, which in turn affect estimate feasibility
		Options:  opts,
	}, nil
}

// SingleLotSwapFees returns the fees for a swap transaction for a single lot.
func (btc *baseWallet) SingleLotSwapFees(_ uint32, feeSuggestion uint64, options map[string]string) (fees uint64, err error) {
	// Load the user's selected order-time options.
	customCfg := new(swapOptions)
	err = config.Unmapify(options, customCfg)
	if err != nil {
		return 0, fmt.Errorf("error parsing selected swap options: %w", err)
	}

	// Parse the configured split transaction.
	split := btc.useSplitTx()
	if customCfg.Split != nil {
		split = *customCfg.Split
	}

	feeBump, err := customCfg.feeBump()
	if err != nil {
		return 0, err
	}

	bumpedNetRate := feeSuggestion
	if feeBump > 1 {
		bumpedNetRate = uint64(math.Round(float64(bumpedNetRate) * feeBump))
	}

	// TODO: The following is not correct for all BTC clones. e.g. Zcash has
	// a different MinimumTxOverhead (29).

	const numInputs = 12 // plan for lots of inputs to get a safe estimate

	var txSize uint64
	if btc.segwit {
		txSize = dexbtc.MinimumTxOverhead + (numInputs * dexbtc.RedeemP2WPKHInputSize) + dexbtc.P2WSHOutputSize + dexbtc.P2WPKHOutputSize
	} else {
		txSize = dexbtc.MinimumTxOverhead + (numInputs * dexbtc.RedeemP2PKHInputSize) + dexbtc.P2SHOutputSize + dexbtc.P2PKHOutputSize
	}

	var splitTxSize uint64
	if split {
		if btc.segwit {
			splitTxSize = dexbtc.MinimumTxOverhead + dexbtc.RedeemP2WPKHInputSize + dexbtc.P2WPKHOutputSize
		} else {
			splitTxSize = dexbtc.MinimumTxOverhead + dexbtc.RedeemP2PKHInputSize + dexbtc.P2PKHOutputSize
		}
	}

	totalTxSize := txSize + splitTxSize

	return totalTxSize * bumpedNetRate, nil
}

// splitOption constructs an *asset.OrderOption with customized text based on the
// difference in fees between the configured and test split condition.
func (btc *baseWallet) splitOption(req *asset.PreSwapForm, utxos []*compositeUTXO, bump float64) *asset.OrderOption {
	opt := &asset.OrderOption{
		ConfigOption: asset.ConfigOption{
			Key:           splitKey,
			DisplayName:   "Pre-size Funds",
			IsBoolean:     true,
			DefaultValue:  btc.useSplitTx(), // not nil interface
			ShowByDefault: true,
		},
		Boolean: &asset.BooleanConfig{},
	}

	noSplitEst, _, noSplitLocked, err := btc.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, false, bump)
	if err != nil {
		btc.log.Errorf("estimateSwap (no split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate without a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}
	splitEst, splitUsed, splitLocked, err := btc.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, true, bump)
	if err != nil {
		btc.log.Errorf("estimateSwap (with split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate with a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}
	symbol := strings.ToUpper(btc.symbol)

	if !splitUsed || splitLocked >= noSplitLocked { // locked check should be redundant
		opt.Boolean.Reason = fmt.Sprintf("avoids no %s overlock for this order (ignored)", symbol)
		opt.Description = fmt.Sprintf("A split transaction for this order avoids no %s overlock, "+
			"but adds additional fees.", symbol)
		opt.DefaultValue = false
		return opt // not enabled by default, but explain why
	}

	overlock := noSplitLocked - splitLocked
	pctChange := (float64(splitEst.RealisticWorstCase)/float64(noSplitEst.RealisticWorstCase) - 1) * 100
	if pctChange > 1 {
		opt.Boolean.Reason = fmt.Sprintf("+%d%% fees, avoids %s %s overlock", int(math.Round(pctChange)), prettyBTC(overlock), symbol)
	} else {
		opt.Boolean.Reason = fmt.Sprintf("+%.1f%% fees, avoids %s %s overlock", pctChange, prettyBTC(overlock), symbol)
	}

	xtraFees := splitEst.RealisticWorstCase - noSplitEst.RealisticWorstCase
	opt.Description = fmt.Sprintf("Using a split transaction to prevent temporary overlock of %s %s, but for additional fees of %s %s",
		prettyBTC(overlock), symbol, prettyBTC(xtraFees), symbol)

	return opt
}

// estimateSwap prepares an *asset.SwapEstimate.
func (btc *baseWallet) estimateSwap(lots, lotSize, feeSuggestion, maxFeeRate uint64, utxos []*compositeUTXO,
	trySplit bool, feeBump float64) (*asset.SwapEstimate, bool /*split used*/, uint64 /*amt locked*/, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.amount
	}
	reserves := btc.reserves()

	// If there is a fee bump, the networkFeeRate can be higher than the
	// MaxFeeRate
	bumpedMaxRate := maxFeeRate
	bumpedNetRate := feeSuggestion
	if feeBump > 1 {
		bumpedMaxRate = uint64(math.Ceil(float64(bumpedMaxRate) * feeBump))
		bumpedNetRate = uint64(math.Ceil(float64(bumpedNetRate) * feeBump))
	}

	val := lots * lotSize
	// The orderEnough func does not account for a split transaction at the start,
	// so it is possible that funding for trySplit would actually choose more
	// UTXOs. Actual order funding accounts for this. For this estimate, we will
	// just not use a split tx if the split-adjusted required funds exceeds the
	// total value of the UTXO selected with this enough closure.
	sum, _, inputsSize, _, _, _, _, err := tryFund(utxos,
		orderEnough(val, lots, bumpedMaxRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, trySplit))
	if err != nil {
		return nil, false, 0, fmt.Errorf("error funding swap value %s: %w", amount(val), err)
	}

	digestInputs := func(inputsSize uint64) (reqFunds, maxFees, estHighFees, estLowFees uint64) {
		reqFunds = calc.RequiredOrderFundsAlt(val, inputsSize, lots,
			btc.initTxSizeBase, btc.initTxSize, bumpedMaxRate) // same as in enough func
		maxFees = reqFunds - val

		estHighFunds := calc.RequiredOrderFundsAlt(val, inputsSize, lots,
			btc.initTxSizeBase, btc.initTxSize, bumpedNetRate)
		estHighFees = estHighFunds - val

		estLowFunds := calc.RequiredOrderFundsAlt(val, inputsSize, 1,
			btc.initTxSizeBase, btc.initTxSize, bumpedNetRate) // best means single multi-lot match, even better than batch
		estLowFees = estLowFunds - val
		return
	}

	reqFunds, maxFees, estHighFees, estLowFees := digestInputs(inputsSize)

	// Math for split transactions is a little different.
	if trySplit {
		_, splitMaxFees := btc.splitBaggageFees(bumpedMaxRate, false)
		_, splitFees := btc.splitBaggageFees(bumpedNetRate, false)
		reqTotal := reqFunds + splitMaxFees // ~ rather than actually fund()ing again
		if reqTotal <= sum && sum-reqTotal >= reserves {
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees + splitMaxFees,
				RealisticBestCase:  estLowFees + splitFees,
				RealisticWorstCase: estHighFees + splitFees,
			}, true, reqFunds, nil // requires reqTotal, but locks reqFunds in the split output
		}
	}

	if sum > avail-reserves {
		if trySplit {
			return nil, false, 0, errors.New("balance too low to both fund order and maintain bond reserves")
		}
		kept := leastOverFund(reserveEnough(reserves), utxos)
		utxos := utxoSetDiff(utxos, kept)
		sum, _, inputsSize, _, _, _, _, err = tryFund(utxos, orderEnough(val, lots, bumpedMaxRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, false))
		if err != nil {
			return nil, false, 0, fmt.Errorf("error funding swap value %s: %w", amount(val), err)
		}
		_, maxFees, estHighFees, estLowFees = digestInputs(inputsSize)
	}

	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              val,
		MaxFees:            maxFees,
		RealisticBestCase:  estLowFees,
		RealisticWorstCase: estHighFees,
	}, false, sum, nil
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (btc *baseWallet) preRedeem(numLots, feeSuggestion uint64, options map[string]string) (*asset.PreRedeem, error) {
	feeRate := feeSuggestion
	if feeRate == 0 {
		feeRate = btc.targetFeeRateWithFallback(btc.redeemConfTarget(), 0)
	}
	// Best is one transaction with req.Lots inputs and 1 output.
	var best uint64 = dexbtc.MinimumTxOverhead
	// Worst is req.Lots transactions, each with one input and one output.
	var worst uint64 = dexbtc.MinimumTxOverhead * numLots
	var inputSize, outputSize uint64
	if btc.segwit {
		// Add the marker and flag weight here.
		inputSize = dexbtc.TxInOverhead + (dexbtc.RedeemSwapSigScriptSize+2+3)/4
		outputSize = dexbtc.P2WPKHOutputSize

	} else {
		inputSize = dexbtc.TxInOverhead + dexbtc.RedeemSwapSigScriptSize
		outputSize = dexbtc.P2PKHOutputSize
	}
	best += inputSize*numLots + outputSize
	worst += (inputSize + outputSize) * numLots

	// Read the order options.
	customCfg := new(redeemOptions)
	err := config.Unmapify(options, customCfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing selected options: %w", err)
	}

	// Parse the configured fee bump.
	currentBump := 1.0
	if customCfg.FeeBump != nil {
		bump := *customCfg.FeeBump
		if bump < 1.0 || bump > 2.0 {
			return nil, fmt.Errorf("invalid fee bump: %f", bump)
		}
		currentBump = bump
	}

	opts := []*asset.OrderOption{{
		ConfigOption: asset.ConfigOption{
			Key:          redeemFeeBumpFee,
			DisplayName:  "Change Redemption Fees",
			Description:  "Bump the redemption transaction fees up to 2x for faster confirmation of your redemption transaction.",
			DefaultValue: 1.0,
		},
		XYRange: &asset.XYRange{
			Start: asset.XYRangePoint{
				Label: "1X",
				X:     1.0,
				Y:     float64(feeRate),
			},
			End: asset.XYRangePoint{
				Label: "2X",
				X:     2.0,
				Y:     float64(feeRate * 2),
			},
			YUnit: btc.walletInfo.UnitInfo.AtomicUnit + "/" + btc.sizeUnit(),
			XUnit: "X",
		},
	}}

	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticWorstCase: uint64(math.Round(float64(worst*feeRate) * currentBump)),
			RealisticBestCase:  uint64(math.Round(float64(best*feeRate) * currentBump)),
		},
		Options: opts,
	}, nil
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (btc *baseWallet) PreRedeem(form *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return btc.preRedeem(form.Lots, form.FeeSuggestion, form.SelectedOptions)
}

// SingleLotRedeemFees returns the fees for a redeem transaction for a single lot.
func (btc *baseWallet) SingleLotRedeemFees(_ uint32, feeSuggestion uint64, options map[string]string) (uint64, error) {
	preRedeem, err := btc.preRedeem(1, feeSuggestion, options)
	if err != nil {
		return 0, err
	}
	return preRedeem.Estimate.RealisticWorstCase, nil
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// The returned []dex.Bytes contains the redeem scripts for the selected coins.
// Equal number of coins and redeemed scripts must be returned. A nil or empty
// dex.Bytes should be appended to the redeem scripts collection for coins with
// no redeem script.
func (btc *baseWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	ordValStr := amount(ord.Value).String()
	btc.log.Debugf("Attempting to fund order for %s %s, maxFeeRate = %d, max swaps = %d",
		ordValStr, btc.symbol, ord.MaxFeeRate, ord.MaxSwapCount)

	if ord.Value == 0 {
		return nil, nil, 0, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, 0, fmt.Errorf("cannot fund a zero-lot order")
	}
	if ord.FeeSuggestion > ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf("fee suggestion %d > max fee rate %d", ord.FeeSuggestion, ord.MaxFeeRate)
	}
	// Check wallets fee rate limit against server's max fee rate
	if btc.feeRateLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configued fee rate limit %v",
			dex.BipIDSymbol(BipID), ord.MaxFeeRate, btc.feeRateLimit())
	}

	customCfg := new(swapOptions)
	err := config.Unmapify(ord.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing swap options: %w", err)
	}

	bumpedMaxRate, err := calcBumpedRate(ord.MaxFeeRate, customCfg.FeeBump)
	if err != nil {
		btc.log.Errorf("calcBumpRate error: %v", err)
	}

	// If a split is not requested, but is forced, create an extra output from
	// the split tx to help avoid a forced split in subsequent orders.
	var extraSplitOutput uint64
	useSplit := btc.useSplitTx()
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	reserves := btc.reserves()
	minConfs := uint32(0)
	coins, fundingCoins, spents, redeemScripts, inputsSize, sum, err := btc.fund(reserves, minConfs, true,
		orderEnough(ord.Value, ord.MaxSwapCount, bumpedMaxRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, useSplit))
	if err != nil {
		if !useSplit && reserves > 0 {
			// Force a split if funding failure may be due to reserves.
			btc.log.Infof("Retrying order funding with a forced split transaction to help respect reserves.")
			useSplit = true
			coins, fundingCoins, spents, redeemScripts, inputsSize, sum, err = btc.fund(reserves, minConfs, true,
				orderEnough(ord.Value, ord.MaxSwapCount, bumpedMaxRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, useSplit))
			extraSplitOutput = reserves + btc.BondsFeeBuffer(ord.FeeSuggestion)
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error funding swap value of %s: %w", amount(ord.Value), err)
		}
	}

	if useSplit {
		// We apply the bumped fee rate to the split transaction when the
		// PreSwap is created, so we use that bumped rate here too.
		// But first, check that it's within bounds.
		splitFeeRate := ord.FeeSuggestion
		if splitFeeRate == 0 {
			// TODO
			// 1.0: Error when no suggestion.
			// return nil, nil, fmt.Errorf("cannot do a split transaction without a fee rate suggestion from the server")
			splitFeeRate = btc.targetFeeRateWithFallback(btc.redeemConfTarget(), 0)
			// We PreOrder checked this as <= MaxFeeRate, so use that as an
			// upper limit.
			if splitFeeRate > ord.MaxFeeRate {
				splitFeeRate = ord.MaxFeeRate
			}
		}
		splitFeeRate, err = calcBumpedRate(splitFeeRate, customCfg.FeeBump)
		if err != nil {
			btc.log.Errorf("calcBumpRate error: %v", err)
		}

		splitCoins, split, splitFees, err := btc.split(ord.Value, ord.MaxSwapCount, spents,
			inputsSize, fundingCoins, splitFeeRate, bumpedMaxRate, extraSplitOutput)
		if err != nil {
			if err := btc.ReturnCoins(coins); err != nil {
				btc.log.Errorf("Error returning coins: %v", err)
			}
			return nil, nil, 0, err
		} else if split {
			fmt.Printf("original coins: %s, split coins %s\n", coins, splitCoins)
			return splitCoins, []dex.Bytes{nil}, splitFees, nil // no redeem script required for split tx output
		}
		return coins, redeemScripts, 0, nil // splitCoins == coins
	}

	btc.log.Infof("Funding %s %s order with coins %v worth %s",
		ordValStr, btc.symbol, coins, amount(sum))

	return coins, redeemScripts, 0, nil
}

func (btc *baseWallet) fundInternalWithUTXOs(utxos []*compositeUTXO, avail uint64, keep uint64, lockUnspents bool,
	enough func(size, sum uint64) (bool, uint64)) (
	coins asset.Coins, fundingCoins map[outPoint]*utxo, spents []*output, redeemScripts []dex.Bytes, size, sum uint64, err error) {

	if keep > 0 {
		kept := leastOverFund(reserveEnough(keep), utxos)
		btc.log.Debugf("Setting aside %v BTC in %d UTXOs to respect the %v BTC reserved amount",
			toBTC(sumUTXOs(kept)), len(kept), toBTC(keep))
		utxosPruned := utxoSetDiff(utxos, kept)
		sum, _, size, coins, fundingCoins, redeemScripts, spents, err = tryFund(utxosPruned, enough)
		if err != nil {
			btc.log.Debugf("Unable to fund order with UTXOs set aside (%v), trying again with full UTXO set.", err)
		}
	}
	if len(spents) == 0 { // either keep is zero or it failed with utxosPruned
		// Without utxos set aside for keep, we have to consider any spendable
		// change (extra) that the enough func grants us.
		var extra uint64
		sum, extra, size, coins, fundingCoins, redeemScripts, spents, err = tryFund(utxos, enough)
		if err != nil {
			return nil, nil, nil, nil, 0, 0, err
		}
		if avail-sum+extra < keep {
			return nil, nil, nil, nil, 0, 0, asset.ErrInsufficientBalance
		}
		// else we got lucky with the legacy funding approach and there was
		// either available unspent or the enough func granted spendable change.
		if keep > 0 && extra > 0 {
			btc.log.Debugf("Funding succeeded with %v BTC in spendable change.", toBTC(extra))
		}
	}

	if lockUnspents {
		err = btc.node.lockUnspent(false, spents)
		if err != nil {
			return nil, nil, nil, nil, 0, 0, fmt.Errorf("LockUnspent error: %w", err)
		}
		for pt, utxo := range fundingCoins {
			btc.fundingCoins[pt] = utxo
		}
	}

	return coins, fundingCoins, spents, redeemScripts, size, sum, err
}

func (btc *baseWallet) fundInternal(keep uint64, minConfs uint32, lockUnspents bool,
	enough func(size, sum uint64) (bool, uint64)) (
	coins asset.Coins, fundingCoins map[outPoint]*utxo, spents []*output, redeemScripts []dex.Bytes, size, sum uint64, err error) {
	utxos, _, avail, err := btc.spendableUTXOs(minConfs)
	if err != nil {
		return nil, nil, nil, nil, 0, 0, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	return btc.fundInternalWithUTXOs(utxos, avail, keep, lockUnspents, enough)
}

func (btc *baseWallet) fund(keep uint64, minConfs uint32, lockUnspents bool,
	enough func(size, sum uint64) (bool, uint64)) (
	coins asset.Coins, fundingCoins map[outPoint]*utxo, spents []*output, redeemScripts []dex.Bytes, size, sum uint64, err error) {

	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()

	return btc.fundInternal(keep, minConfs, lockUnspents, enough)
}

// orderWithLeastOverFund returns the index of the order from a slice of orders
// that requires the least over-funding without using more than maxLock. It
// also returns the UTXOs that were used to fund the order. If none can be
// funded without using more than maxLock, -1 is returned.
func (btc *baseWallet) orderWithLeastOverFund(maxLock, feeRate uint64, orders []*asset.MultiOrderValue, utxos []*compositeUTXO) (orderIndex int, leastOverFundingUTXOs []*compositeUTXO) {
	minOverFund := uint64(math.MaxUint64)
	orderIndex = -1
	for i, value := range orders {
		enough := orderEnough(value.Value, value.MaxSwapCount, feeRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, false)
		var fundingUTXOs []*compositeUTXO
		if maxLock > 0 {
			fundingUTXOs = leastOverFundWithLimit(enough, maxLock, utxos)
		} else {
			fundingUTXOs = leastOverFund(enough, utxos)
		}
		if len(fundingUTXOs) == 0 {
			continue
		}
		sum := sumUTXOs(fundingUTXOs)
		overFund := sum - value.Value
		if overFund < minOverFund {
			minOverFund = overFund
			orderIndex = i
			leastOverFundingUTXOs = fundingUTXOs
		}
	}
	return
}

// fundsRequiredForMultiOrders returns an slice of the required funds for each
// of a slice of orders and the total required funds.
func (btc *baseWallet) fundsRequiredForMultiOrders(orders []*asset.MultiOrderValue, feeRate, splitBuffer uint64) ([]uint64, uint64) {
	var swapInputSize uint64
	if btc.segwit {
		swapInputSize = dexbtc.RedeemP2WPKHInputTotalSize
	} else {
		swapInputSize = dexbtc.RedeemP2PKHInputSize
	}

	requiredForOrders := make([]uint64, len(orders))
	var totalRequired uint64

	for i, value := range orders {
		req := calc.RequiredOrderFundsAlt(value.Value, swapInputSize, value.MaxSwapCount, btc.initTxSizeBase, btc.initTxSize, feeRate)
		req = req * (100 + splitBuffer) / 100
		requiredForOrders[i] = req
		totalRequired += req
	}

	return requiredForOrders, totalRequired
}

// fundMultiBestEffors makes a best effort to fund every order. If it is not
// possible, it returns coins for the orders that could be funded. The coins
// that fund each order are returned in the same order as the values that were
// passed in. If a split is allowed and all orders cannot be funded, nil slices
// are returned.
func (btc *baseWallet) fundMultiBestEffort(keep, maxLock uint64, values []*asset.MultiOrderValue,
	maxFeeRate uint64, splitAllowed bool) ([]asset.Coins, [][]dex.Bytes, map[outPoint]*utxo, []*output, error) {
	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	fundAllOrders := func() [][]*compositeUTXO {
		indexToFundingCoins := make(map[int][]*compositeUTXO, len(values))
		remainingUTXOs := utxos
		remainingOrders := values
		remainingIndexes := make([]int, len(values))
		for i := range remainingIndexes {
			remainingIndexes[i] = i
		}
		var totalFunded uint64
		for range values {
			orderIndex, fundingUTXOs := btc.orderWithLeastOverFund(maxLock-totalFunded, maxFeeRate, remainingOrders, remainingUTXOs)
			if orderIndex == -1 {
				return nil
			}
			totalFunded += sumUTXOs(fundingUTXOs)
			if totalFunded > avail-keep {
				return nil
			}
			newRemainingOrders := make([]*asset.MultiOrderValue, 0, len(remainingOrders)-1)
			newRemainingIndexes := make([]int, 0, len(remainingOrders)-1)
			for j := range remainingOrders {
				if j != orderIndex {
					newRemainingOrders = append(newRemainingOrders, remainingOrders[j])
					newRemainingIndexes = append(newRemainingIndexes, remainingIndexes[j])
				}
			}
			indexToFundingCoins[remainingIndexes[orderIndex]] = fundingUTXOs
			remainingOrders = newRemainingOrders
			remainingIndexes = newRemainingIndexes
			remainingUTXOs = utxoSetDiff(remainingUTXOs, fundingUTXOs)
		}
		allFundingUTXOs := make([][]*compositeUTXO, len(values))
		for i := range values {
			allFundingUTXOs[i] = indexToFundingCoins[i]
		}
		return allFundingUTXOs
	}

	fundInOrder := func(orderedValues []*asset.MultiOrderValue) [][]*compositeUTXO {
		allFundingUTXOs := make([][]*compositeUTXO, 0, len(orderedValues))
		remainingUTXOs := utxos
		var totalFunded uint64
		for _, value := range orderedValues {
			enough := orderEnough(value.Value, value.MaxSwapCount, maxFeeRate, btc.initTxSizeBase, btc.initTxSize, btc.segwit, false)

			var fundingUTXOs []*compositeUTXO
			if maxLock > 0 {
				if maxLock < totalFunded {
					// Should never happen unless there is a bug in leastOverFundWithLimit
					btc.log.Errorf("maxLock < totalFunded. %d < %d", maxLock, totalFunded)
					return allFundingUTXOs
				}
				fundingUTXOs = leastOverFundWithLimit(enough, maxLock-totalFunded, remainingUTXOs)
			} else {
				fundingUTXOs = leastOverFund(enough, remainingUTXOs)
			}
			if len(fundingUTXOs) == 0 {
				return allFundingUTXOs
			}
			totalFunded += sumUTXOs(fundingUTXOs)
			if totalFunded > avail-keep {
				return allFundingUTXOs
			}
			allFundingUTXOs = append(allFundingUTXOs, fundingUTXOs)
			remainingUTXOs = utxoSetDiff(remainingUTXOs, fundingUTXOs)
		}
		return allFundingUTXOs
	}

	returnValues := func(allFundingUTXOs [][]*compositeUTXO) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingCoins map[outPoint]*utxo, spents []*output, err error) {
		coins = make([]asset.Coins, len(allFundingUTXOs))
		fundingCoins = make(map[outPoint]*utxo)
		spents = make([]*output, 0, len(allFundingUTXOs))
		redeemScripts = make([][]dex.Bytes, len(allFundingUTXOs))
		for i, fundingUTXOs := range allFundingUTXOs {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, output := range fundingUTXOs {
				coins[i][j] = newOutput(output.txHash, output.vout, output.amount)
				fundingCoins[outPoint{txHash: *output.txHash, vout: output.vout}] = &utxo{
					txHash:  output.txHash,
					vout:    output.vout,
					amount:  output.amount,
					address: output.address,
				}
				spents = append(spents, newOutput(output.txHash, output.vout, output.amount))
				redeemScripts[i][j] = output.redeemScript
			}
		}
		return
	}

	// Attempt to fund all orders by selecting the order that requires the least
	// over funding, removing the funding utxos from the set of available utxos,
	// and continuing until all orders are funded.
	allFundingUTXOs := fundAllOrders()
	if allFundingUTXOs != nil {
		return returnValues(allFundingUTXOs)
	}

	// Return nil if a split is allowed. There is no need to fund in priority
	// order if a split will be done regardless.
	if splitAllowed {
		return returnValues([][]*compositeUTXO{})
	}

	// If could not fully fund, fund as much as possible in the priority
	// order.
	allFundingUTXOs = fundInOrder(values)
	return returnValues(allFundingUTXOs)
}

// fundMultiSplitTx uses the utxos provided and attempts to fund a multi-split
// transaction to fund each of the orders. If successful, it returns the
// funding coins and outputs.
func (btc *baseWallet) fundMultiSplitTx(orders []*asset.MultiOrderValue, utxos []*compositeUTXO,
	splitTxFeeRate, maxFeeRate, splitBuffer, keep, maxLock uint64) (bool, asset.Coins, []*output) {
	_, totalOutputRequired := btc.fundsRequiredForMultiOrders(orders, maxFeeRate, splitBuffer)

	var splitTxSizeWithoutInputs uint64 = dexbtc.MinimumTxOverhead
	numOutputs := len(orders)
	if keep > 0 {
		numOutputs++
	}
	if btc.segwit {
		splitTxSizeWithoutInputs += uint64(dexbtc.P2WPKHOutputSize * numOutputs)
	} else {
		splitTxSizeWithoutInputs += uint64(dexbtc.P2PKHOutputSize * numOutputs)
	}
	enough := func(inputSize, sum uint64) (bool, uint64) {
		splitTxFee := (splitTxSizeWithoutInputs + inputSize) * splitTxFeeRate
		req := totalOutputRequired + splitTxFee
		return sum >= req, sum - req
	}

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.amount
	}

	fundSplitCoins, _, spents, _, inputsSize, _, err := btc.fundInternalWithUTXOs(utxos, avail, keep, false, enough)
	if err != nil {
		return false, nil, nil
	}

	if maxLock > 0 {
		totalSize := inputsSize + splitTxSizeWithoutInputs
		if totalOutputRequired+(totalSize*splitTxFeeRate) > maxLock {
			return false, nil, nil
		}
	}

	return true, fundSplitCoins, spents
}

// submitMultiSplitTx creates a multi-split transaction using fundingCoins with
// one output for each order, and submits it to the network.
func (btc *baseWallet) submitMultiSplitTx(fundingCoins asset.Coins, spents []*output, orders []*asset.MultiOrderValue,
	maxFeeRate, splitTxFeeRate, splitBuffer uint64) ([]asset.Coins, uint64, error) {
	baseTx, totalIn, _, err := btc.fundedTx(fundingCoins)
	if err != nil {
		return nil, 0, err
	}

	btc.node.lockUnspent(false, spents)
	var success bool
	defer func() {
		if !success {
			btc.node.lockUnspent(true, spents)
		}
	}()

	requiredForOrders, totalRequired := btc.fundsRequiredForMultiOrders(orders, maxFeeRate, splitBuffer)

	outputAddresses := make([]btcutil.Address, len(orders))
	for i, req := range requiredForOrders {
		outputAddr, err := btc.node.changeAddress()
		if err != nil {
			return nil, 0, err
		}
		outputAddresses[i] = outputAddr
		script, err := txscript.PayToAddrScript(outputAddr)
		if err != nil {
			return nil, 0, err
		}
		baseTx.AddTxOut(wire.NewTxOut(int64(req), script))
	}

	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, 0, err
	}
	tx, err := btc.sendWithReturn(baseTx, changeAddr, totalIn, totalRequired, splitTxFeeRate)
	if err != nil {
		return nil, 0, err
	}

	txHash := tx.TxHash()
	coins := make([]asset.Coins, len(orders))
	ops := make([]*output, len(orders))
	for i := range coins {
		coins[i] = asset.Coins{newOutput(&txHash, uint32(i), uint64(tx.TxOut[i].Value))}
		ops[i] = newOutput(&txHash, uint32(i), uint64(tx.TxOut[i].Value))
		btc.fundingCoins[ops[i].pt] = &utxo{
			txHash:  &txHash,
			vout:    uint32(i),
			amount:  uint64(tx.TxOut[i].Value),
			address: outputAddresses[i].String(),
		}
	}
	btc.node.lockUnspent(false, ops)

	var totalOut uint64
	for _, txOut := range tx.TxOut {
		totalOut += uint64(txOut.Value)
	}

	success = true
	return coins, totalIn - totalOut, nil
}

// fundMultiWithSplit creates a split transaction to fund multiple orders. It
// attempts to fund as many of the orders as possible without a split transaction,
// and only creates a split transaction for the remaining orders. This is only
// called after it has been determined that all of the orders cannot be funded
// without a split transaction.
func (btc *baseWallet) fundMultiWithSplit(keep, maxLock uint64, values []*asset.MultiOrderValue,
	splitTxFeeRate, maxFeeRate, splitBuffer uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	utxos, _, avail, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	canFund, splitCoins, splitSpents := btc.fundMultiSplitTx(values, utxos, splitTxFeeRate, maxFeeRate, splitBuffer, keep, maxLock)
	if !canFund {
		return nil, nil, 0, fmt.Errorf("cannot fund all with split")
	}

	remainingUTXOs := utxos
	remainingOrders := values

	// The return values must be in the same order as the values that were
	// passed in, so we keep track of the original indexes here.
	indexToFundingCoins := make(map[int][]*compositeUTXO, len(values))
	remainingIndexes := make([]int, len(values))
	for i := range remainingIndexes {
		remainingIndexes[i] = i
	}

	var totalFunded uint64

	// Find each of the orders that can be funded without being included
	// in the split transaction.
	for range values {
		// First find the order the can be funded with the least overlock.
		// If there is no order that can be funded without going over the
		// maxLock limit, or not leaving enough for bond reserves, then all
		// of the remaining orders must be funded with the split transaction.
		orderIndex, fundingUTXOs := btc.orderWithLeastOverFund(maxLock-totalFunded, maxFeeRate, remainingOrders, remainingUTXOs)
		if orderIndex == -1 {
			break
		}
		totalFunded += sumUTXOs(fundingUTXOs)
		if totalFunded > avail-keep {
			break
		}

		newRemainingOrders := make([]*asset.MultiOrderValue, 0, len(remainingOrders)-1)
		newRemainingIndexes := make([]int, 0, len(remainingOrders)-1)
		for j := range remainingOrders {
			if j != orderIndex {
				newRemainingOrders = append(newRemainingOrders, remainingOrders[j])
				newRemainingIndexes = append(newRemainingIndexes, remainingIndexes[j])
			}
		}
		remainingUTXOs = utxoSetDiff(remainingUTXOs, fundingUTXOs)

		// Then we make sure that a split transaction can be created for
		// any remaining orders without using the utxos returned by
		// orderWithLeastOverFund.
		if len(newRemainingOrders) > 0 {
			canFund, newSplitCoins, newSpents := btc.fundMultiSplitTx(newRemainingOrders, remainingUTXOs,
				splitTxFeeRate, maxFeeRate, splitBuffer, keep, maxLock-totalFunded)
			if !canFund {
				break
			}
			splitCoins = newSplitCoins
			splitSpents = newSpents
		}

		indexToFundingCoins[remainingIndexes[orderIndex]] = fundingUTXOs
		remainingOrders = newRemainingOrders
		remainingIndexes = newRemainingIndexes
	}

	var splitOutputCoins []asset.Coins
	var splitFees uint64

	// This should always be true, otherwise this function would not have been
	// called.
	if len(remainingOrders) > 0 {
		splitOutputCoins, splitFees, err = btc.submitMultiSplitTx(splitCoins,
			splitSpents, remainingOrders, maxFeeRate, splitTxFeeRate, splitBuffer)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating split transaction: %w", err)
		}
	}

	coins := make([]asset.Coins, len(values))
	redeemScripts := make([][]dex.Bytes, len(values))
	spents := make([]*output, 0, len(values))

	var splitIndex int

	for i := range values {
		if fundingUTXOs, ok := indexToFundingCoins[i]; ok {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, unspent := range fundingUTXOs {
				output := newOutput(unspent.txHash, unspent.vout, unspent.amount)
				btc.fundingCoins[output.pt] = &utxo{
					txHash:  unspent.txHash,
					vout:    unspent.vout,
					amount:  unspent.amount,
					address: unspent.address,
				}
				coins[i][j] = output
				spents = append(spents, output)
				redeemScripts[i][j] = unspent.redeemScript
			}
		} else {
			coins[i] = splitOutputCoins[splitIndex]
			redeemScripts[i] = []dex.Bytes{nil}
			splitIndex++
		}
	}

	btc.node.lockUnspent(false, spents)

	return coins, redeemScripts, splitFees, nil
}

// fundMulti first attempts to fund each of the orders with with the available
// UTXOs. If a split is not allowed, it will fund the orders that it was able
// to fund. If splitting is allowed, a split transaction will be created to fund
// all of the orders.
func (btc *baseWallet) fundMulti(maxLock uint64, values []*asset.MultiOrderValue, splitTxFeeRate, maxFeeRate uint64, allowSplit bool, splitBuffer uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()

	reserves := btc.reserves()

	coins, redeemScripts, fundingCoins, spents, err := btc.fundMultiBestEffort(reserves, maxLock, values, maxFeeRate, allowSplit)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(coins) == len(values) || !allowSplit {
		for pt, fc := range fundingCoins {
			btc.fundingCoins[pt] = fc
		}
		btc.node.lockUnspent(false, spents)
		return coins, redeemScripts, 0, nil
	}

	return btc.fundMultiWithSplit(reserves, maxLock, values, splitTxFeeRate, maxFeeRate, splitBuffer)
}

func tryFund(utxos []*compositeUTXO,
	enough func(uint64, uint64) (bool, uint64)) (
	sum, extra, size uint64, coins asset.Coins, fundingCoins map[outPoint]*utxo, redeemScripts []dex.Bytes, spents []*output, err error) {

	fundingCoins = make(map[outPoint]*utxo)

	isEnoughWith := func(unspent *compositeUTXO) bool {
		ok, _ := enough(size+uint64(unspent.input.VBytes()), sum+unspent.amount)
		return ok
	}

	addUTXO := func(unspent *compositeUTXO) {
		v := unspent.amount
		op := newOutput(unspent.txHash, unspent.vout, v)
		coins = append(coins, op)
		redeemScripts = append(redeemScripts, unspent.redeemScript)
		spents = append(spents, op)
		size += uint64(unspent.input.VBytes())
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

			// Check if the largest output is too small.
			lastUTXO := okUTXOs[len(okUTXOs)-1]
			if !isEnoughWith(lastUTXO) {
				addUTXO(lastUTXO)
				okUTXOs = okUTXOs[0 : len(okUTXOs)-1]
				continue
			}

			// We only need one then. Find it.
			idx := sort.Search(len(okUTXOs), func(i int) bool {
				return isEnoughWith(okUTXOs[i])
			})
			// No need to check idx == len(okUTXOs). We already verified that the last
			// utxo passes above.
			addUTXO(okUTXOs[idx])
			_, extra = enough(size, sum)
			return true
		}
	}

	// First try with confs>0, falling back to allowing 0-conf outputs.
	if !tryUTXOs(1) {
		if !tryUTXOs(0) {
			return 0, 0, 0, nil, nil, nil, nil, fmt.Errorf("not enough to cover requested funds. "+
				"%s available in %d UTXOs", amount(sum), len(coins))
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
//   - overhead from 1 transaction
//   - 1 extra signed p2wpkh-spending input. The split tx has the fundingCoins as
//     inputs now, but we'll add the input that spends the sized coin that will go
//     into the first swap if the split tx does not add excess baggage
//   - 2 additional p2wpkh outputs for the split tx sized output and change
//
// If the fees associated with this extra baggage are more than the excess
// amount that would be locked if a split transaction were not used, then the
// split transaction is pointless. This might be common, for instance, if an
// order is canceled partially filled, and then the remainder resubmitted. We
// would already have an output of just the right size, and that would be
// recognized here.
func (btc *baseWallet) split(value uint64, lots uint64, outputs []*output, inputsSize uint64,
	fundingCoins map[outPoint]*utxo, suggestedFeeRate, bumpedMaxRate, extraOutput uint64) (asset.Coins, bool, uint64, error) {

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
	swapInputSize, baggage := btc.splitBaggageFees(bumpedMaxRate, extraOutput > 0)

	var coinSum uint64
	coins := make(asset.Coins, 0, len(outputs))
	for _, op := range outputs {
		coins = append(coins, op)
		coinSum += op.value
	}

	valueStr := amount(value).String()

	excess := coinSum - calc.RequiredOrderFundsAlt(value, inputsSize, lots, btc.initTxSizeBase, btc.initTxSize, bumpedMaxRate)
	if baggage > excess {
		btc.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. "+
			"%s > %s", amount(baggage), amount(excess))
		btc.log.Infof("Funding %s %s order with coins %v worth %s",
			valueStr, btc.symbol, coins, amount(coinSum))
		return coins, false, 0, nil // err==nil records and locks the provided fundingCoins in defer
	}

	// Use an internal address for the sized output.
	addr, err := btc.node.changeAddress()
	if err != nil {
		return nil, false, 0, fmt.Errorf("error creating split transaction address: %w", err)
	}
	addrStr, err := btc.stringAddr(addr, btc.chainParams)
	if err != nil {
		return nil, false, 0, fmt.Errorf("failed to stringify the change address: %w", err)
	}

	reqFunds := calc.RequiredOrderFundsAlt(value, swapInputSize, lots, btc.initTxSizeBase, btc.initTxSize, bumpedMaxRate)

	baseTx, _, _, err := btc.fundedTx(coins)
	splitScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, false, 0, fmt.Errorf("error creating split tx script: %w", err)
	}
	baseTx.AddTxOut(wire.NewTxOut(int64(reqFunds), splitScript))

	if extraOutput > 0 {
		addr, err := btc.node.changeAddress()
		if err != nil {
			return nil, false, 0, fmt.Errorf("error creating split transaction address: %w", err)
		}
		splitScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			return nil, false, 0, fmt.Errorf("error creating split tx script: %w", err)
		}
		baseTx.AddTxOut(wire.NewTxOut(int64(extraOutput), splitScript))
	}

	// Grab a change address.
	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, false, 0, fmt.Errorf("error creating change address: %w", err)
	}

	// Sign, add change, and send the transaction.
	msgTx, err := btc.sendWithReturn(baseTx, changeAddr, coinSum, reqFunds+extraOutput, suggestedFeeRate)
	if err != nil {
		return nil, false, 0, fmt.Errorf("error sending tx: %w", err)
	}

	txHash := btc.hashTx(msgTx)
	op := newOutput(txHash, 0, reqFunds)

	totalOut := reqFunds
	for i := 1; i < len(msgTx.TxOut); i++ {
		totalOut += uint64(msgTx.TxOut[i].Value)
	}

	// Need to save one funding coin (in the deferred function).
	fundingCoins = map[outPoint]*utxo{op.pt: {
		txHash:  op.txHash(),
		vout:    op.vout(),
		address: addrStr,
		amount:  reqFunds,
	}}

	// Unlock spent coins
	returnErr := btc.ReturnCoins(coins)
	if returnErr != nil {
		btc.log.Errorf("error unlocking spent coins: %v", returnErr)
	}

	btc.log.Infof("Funding %s %s order with split output coin %v from original coins %v",
		valueStr, btc.symbol, op, coins)
	btc.log.Infof("Sent split transaction %s to accommodate swap of size %s %s + fees = %s",
		op.txHash(), valueStr, btc.symbol, amount(reqFunds))

	// Assign to coins so the deferred function will lock the output.
	outputs = []*output{op}
	return asset.Coins{op}, true, coinSum - totalOut, nil
}

// splitBaggageFees is the fees associated with adding a split transaction.
func (btc *baseWallet) splitBaggageFees(maxFeeRate uint64, extraOutput bool) (swapInputSize, baggage uint64) {
	if btc.segwit {
		baggage = maxFeeRate * splitTxBaggageSegwit
		if extraOutput {
			baggage += maxFeeRate * dexbtc.P2WPKHOutputSize
		}
		swapInputSize = dexbtc.RedeemP2WPKHInputTotalSize
		return
	}
	baggage = maxFeeRate * splitTxBaggage
	if extraOutput {
		baggage += maxFeeRate * dexbtc.P2PKHOutputSize
	}
	swapInputSize = dexbtc.RedeemP2PKHInputSize
	return
}

// ReturnCoins unlocks coins. This would be used in the case of a canceled or
// partially filled order. Part of the asset.Wallet interface.
func (btc *baseWallet) ReturnCoins(unspents asset.Coins) error {
	if unspents == nil { // not just empty to make this harder to do accidentally
		btc.log.Debugf("Returning all coins.")
		btc.fundingMtx.Lock()
		defer btc.fundingMtx.Unlock()
		if err := btc.node.lockUnspent(true, nil); err != nil {
			return err
		}
		btc.fundingCoins = make(map[outPoint]*utxo)
		return nil
	}
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
	}
	if err := btc.node.lockUnspent(true, ops); err != nil {
		return err // could it have unlocked some of them? we may want to loop instead if that's the case
	}
	for _, op := range ops {
		delete(btc.fundingCoins, op.pt)
	}
	return nil
}

// rawWalletTx gets the raw bytes of a transaction and the number of
// confirmations. This is a wrapper for checkWalletTx (if node is a
// walletTxChecker), with a fallback to getWalletTransaction.
func (btc *baseWallet) rawWalletTx(hash *chainhash.Hash) ([]byte, uint32, error) {
	if fast, ok := btc.node.(walletTxChecker); ok {
		txRaw, confs, err := fast.checkWalletTx(hash.String())
		if err == nil {
			return txRaw, confs, nil
		}
		btc.log.Warnf("checkWalletTx: %v", err)
		// fallback to getWalletTransaction
	}

	tx, err := btc.node.getWalletTransaction(hash)
	if err != nil {
		return nil, 0, err
	}
	return tx.Hex, uint32(tx.Confirmations), nil
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

	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid from rpc server %s: %w", rpcOP.TxID, err)
		}
		pt := newOutPoint(txHash, rpcOP.Vout)
		if !notFound[pt] {
			continue // unrelated to the order
		}

		txRaw, _, err := btc.rawWalletTx(txHash)
		if err != nil {
			return nil, err
		}
		msgTx, err := btc.deserializeTx(txRaw)
		if err != nil {
			btc.log.Warnf("Invalid transaction %v (%x): %v", txHash, txRaw, err)
			continue
		}
		if rpcOP.Vout >= uint32(len(msgTx.TxOut)) {
			btc.log.Warnf("Invalid vout %d for %v", rpcOP.Vout, txHash)
			continue
		}
		txOut := msgTx.TxOut[rpcOP.Vout]
		if txOut.Value <= 0 {
			btc.log.Warnf("Invalid value %v for %v", txOut.Value, pt)
			continue // try the listunspent output
		}
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, btc.chainParams)
		if err != nil {
			btc.log.Warnf("Invalid pkScript for %v: %v", pt, err)
			continue
		}
		if len(addrs) != 1 {
			btc.log.Warnf("pkScript for %v contains %d addresses instead of one", pt, len(addrs))
			continue
		}
		addrStr, err := btc.stringAddr(addrs[0], btc.chainParams)
		if err != nil {
			btc.log.Errorf("Failed to stringify address %v (default encoding): %v", addrs[0], err)
			addrStr = addrs[0].String() // may or may not be able to retrieve the private keys by address!
		}
		utxo := &utxo{
			txHash:  txHash,
			vout:    rpcOP.Vout,
			address: addrStr, // for retrieving private key by address string
			amount:  uint64(txOut.Value),
		}
		coin := newOutput(txHash, rpcOP.Vout, uint64(txOut.Value))
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

// authAddOn implements the asset.Authenticator.
type authAddOn struct {
	w Wallet
}

// Unlock unlocks the underlying wallet. The pw supplied should be the same as
// the password for the underlying bitcoind wallet which will also be unlocked.
// It implements asset.authenticator.
func (a *authAddOn) Unlock(pw []byte) error {
	return a.w.walletUnlock(pw)
}

// Lock locks the underlying bitcoind wallet. It implements asset.authenticator.
func (a *authAddOn) Lock() error {
	return a.w.walletLock()
}

// Locked will be true if the wallet is currently locked. It implements
// asset.authenticator.
func (a *authAddOn) Locked() bool {
	return a.w.locked()
}

func (btc *baseWallet) addInputsToTx(tx *wire.MsgTx, coins asset.Coins) (uint64, []outPoint, error) {
	var totalIn uint64
	// Add the funding utxos.
	pts := make([]outPoint, 0, len(coins))
	for _, coin := range coins {
		op, err := btc.convertCoin(coin)
		if err != nil {
			return 0, nil, fmt.Errorf("error converting coin: %w", err)
		}
		if op.value == 0 {
			return 0, nil, fmt.Errorf("zero-valued output detected for %s:%d", op.txHash(), op.vout())
		}
		totalIn += op.value
		txIn := wire.NewTxIn(op.wireOutPoint(), []byte{}, nil)
		tx.AddTxIn(txIn)
		pts = append(pts, op.pt)
	}
	return totalIn, pts, nil
}

// fundedTx creates and returns a new MsgTx with the provided coins as inputs.
func (btc *baseWallet) fundedTx(coins asset.Coins) (*wire.MsgTx, uint64, []outPoint, error) {
	baseTx := wire.NewMsgTx(btc.txVersion())
	totalIn, pts, err := btc.addInputsToTx(baseTx, coins)
	if err != nil {
		return nil, 0, nil, err
	}
	return baseTx, totalIn, pts, nil
}

// lookupWalletTxOutput looks up the value of a transaction output that is
// spandable by this wallet, and creates an output.
func (btc *baseWallet) lookupWalletTxOutput(txHash *chainhash.Hash, vout uint32) (*output, error) {
	getTxResult, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		return nil, err
	}

	tx, err := btc.deserializeTx(getTxResult.Hex)
	if err != nil {
		return nil, err
	}
	if len(tx.TxOut) <= int(vout) {
		return nil, fmt.Errorf("txId %s only has %d outputs. tried to access index %d",
			txHash, len(tx.TxOut), vout)
	}

	value := tx.TxOut[vout].Value
	return newOutput(txHash, vout, uint64(value)), nil
}

// getTransactions retrieves the transactions that created coins. The
// returned slice will be in the same order as the argument.
func (btc *baseWallet) getTransactions(coins []dex.Bytes) ([]*GetTransactionResult, error) {
	txs := make([]*GetTransactionResult, 0, len(coins))

	for _, coinID := range coins {
		txHash, _, err := decodeCoinID(coinID)
		if err != nil {
			return nil, err
		}
		getTxRes, err := btc.node.getWalletTransaction(txHash)
		if err != nil {
			return nil, err
		}
		txs = append(txs, getTxRes)
	}

	return txs, nil
}

func (btc *baseWallet) getTxFee(tx *wire.MsgTx) (uint64, error) {
	var in, out uint64

	for _, txOut := range tx.TxOut {
		out += uint64(txOut.Value)
	}

	for _, txIn := range tx.TxIn {
		prevTx, err := btc.node.getWalletTransaction(&txIn.PreviousOutPoint.Hash)
		if err != nil {
			return 0, err
		}
		prevMsgTx, err := btc.deserializeTx(prevTx.Hex)
		if err != nil {
			return 0, err
		}
		if len(prevMsgTx.TxOut) <= int(txIn.PreviousOutPoint.Index) {
			return 0, fmt.Errorf("tx %x references index %d output of %x, but it only has %d outputs",
				tx.TxHash(), txIn.PreviousOutPoint.Index, prevMsgTx.TxHash(), len(prevMsgTx.TxOut))
		}
		in += uint64(prevMsgTx.TxOut[int(txIn.PreviousOutPoint.Index)].Value)
	}

	if in < out {
		return 0, fmt.Errorf("tx %x has value of inputs %d < value of outputs %d",
			tx.TxHash(), in, out)
	}

	return in - out, nil
}

// sizeAndFeesOfConfirmedTxs returns the total size in vBytes and the total
// fees spent by the unconfirmed transactions in txs.
func (btc *baseWallet) sizeAndFeesOfUnconfirmedTxs(txs []*GetTransactionResult) (size uint64, fees uint64, err error) {
	for _, tx := range txs {
		if tx.Confirmations > 0 {
			continue
		}

		msgTx, err := btc.deserializeTx(tx.Hex)
		if err != nil {
			return 0, 0, err
		}

		fee, err := btc.getTxFee(msgTx)
		if err != nil {
			return 0, 0, err
		}

		fees += fee
		size += btc.calcTxSize(msgTx)
	}

	return size, fees, nil
}

// additionalFeesRequired calculates the additional satoshis that need to be
// sent to miners in order to increase the average fee rate of unconfirmed
// transactions to newFeeRate. An error is returned if no additional fees
// are required.
func (btc *baseWallet) additionalFeesRequired(txs []*GetTransactionResult, newFeeRate uint64) (uint64, error) {
	size, fees, err := btc.sizeAndFeesOfUnconfirmedTxs(txs)
	if err != nil {
		return 0, err
	}

	if fees >= size*newFeeRate {
		return 0, fmt.Errorf("extra fees are not needed. %d would be needed "+
			"for a fee rate of %d, but %d was already paid",
			size*newFeeRate, newFeeRate, fees)
	}

	return size*newFeeRate - fees, nil
}

// changeCanBeAccelerated returns nil if the change can be accelerated,
// otherwise it returns an error containing the reason why it cannot.
func (btc *baseWallet) changeCanBeAccelerated(change *output, remainingSwaps bool) error {
	lockedUtxos, err := btc.node.listLockUnspent()
	if err != nil {
		return err
	}

	changeTxHash := change.pt.txHash.String()
	for _, utxo := range lockedUtxos {
		if utxo.TxID == changeTxHash && utxo.Vout == change.pt.vout {
			if !remainingSwaps {
				return errors.New("change locked by another order")
			}
			// change is locked by this order
			return nil
		}
	}

	utxos, err := btc.node.listUnspent()
	if err != nil {
		return err
	}
	for _, utxo := range utxos {
		if utxo.TxID == changeTxHash && utxo.Vout == change.pt.vout {
			return nil
		}
	}

	return errors.New("change already spent")
}

// signedAccelerationTx returns a signed transaction that sends funds to a
// change address controlled by this wallet. This new transaction will have
// a fee high enough to make the average fee of the unmined previousTxs to
// the newFeeRate. orderChange latest change in the order, and must be spent
// by this new transaction in order to accelerate the order.
// requiredForRemainingSwaps is the amount of funds that are still required
// to complete the order, so the change of the acceleration transaction must
// contain at least that amount.
func (btc *baseWallet) signedAccelerationTx(previousTxs []*GetTransactionResult, orderChange *output, requiredForRemainingSwaps, newFeeRate uint64) (*wire.MsgTx, *output, uint64, error) {
	makeError := func(err error) (*wire.MsgTx, *output, uint64, error) {
		return nil, nil, 0, err
	}

	err := btc.changeCanBeAccelerated(orderChange, requiredForRemainingSwaps > 0)
	if err != nil {
		return makeError(err)
	}

	additionalFeesRequired, err := btc.additionalFeesRequired(previousTxs, newFeeRate)
	if err != nil {
		return makeError(err)
	}

	// Figure out how much funds we need to increase the fee to the requested
	// amount.
	txSize := uint64(dexbtc.MinimumTxOverhead)
	// Add the size of using the order change as an input
	if btc.segwit {
		txSize += dexbtc.RedeemP2WPKHInputTotalSize
	} else {
		txSize += dexbtc.RedeemP2PKHInputSize
	}
	// We need an output if funds are still required for additional swaps in
	// the order.
	if requiredForRemainingSwaps > 0 {
		if btc.segwit {
			txSize += dexbtc.P2WPKHOutputSize
		} else {
			txSize += dexbtc.P2PKHOutputSize
		}
	}
	fundsRequired := additionalFeesRequired + requiredForRemainingSwaps + txSize*newFeeRate

	var additionalInputs asset.Coins
	if fundsRequired > orderChange.value {
		// If change not enough, need to use other UTXOs.
		enough := func(inputSize, inputsVal uint64) (bool, uint64) {
			txSize := dexbtc.MinimumTxOverhead + inputSize

			// add the order change as an input
			if btc.segwit {
				txSize += dexbtc.RedeemP2WPKHInputTotalSize
			} else {
				txSize += dexbtc.RedeemP2PKHInputSize
			}

			if requiredForRemainingSwaps > 0 {
				if btc.segwit {
					txSize += dexbtc.P2WPKHOutputSize
				} else {
					txSize += dexbtc.P2PKHOutputSize
				}
			}

			totalFees := additionalFeesRequired + txSize*newFeeRate
			totalReq := requiredForRemainingSwaps + totalFees
			totalVal := inputsVal + orderChange.value
			return totalReq <= totalVal, totalVal - totalReq
		}
		minConfs := uint32(1)
		additionalInputs, _, _, _, _, _, err = btc.fundInternal(btc.reserves(), minConfs, false, enough)
		if err != nil {
			return makeError(fmt.Errorf("failed to fund acceleration tx: %w", err))
		}
	}

	baseTx, totalIn, _, err := btc.fundedTx(append(additionalInputs, orderChange))
	if err != nil {
		return makeError(err)
	}

	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return makeError(fmt.Errorf("error creating change address: %w", err))
	}

	tx, output, txFee, err := btc.signTxAndAddChange(baseTx, changeAddr, totalIn, additionalFeesRequired, newFeeRate)
	if err != nil {
		return makeError(err)
	}

	return tx, output, txFee + additionalFeesRequired, nil
}

// FeesForRemainingSwaps returns the fees for a certain number of swaps at a given
// feeRate. This is only accurate if each swap has a single input. Accurate
// estimates should use PreSwap or FundOrder.
func (btc *intermediaryWallet) FeesForRemainingSwaps(n, feeRate uint64) uint64 {
	return btc.initTxSize * n * feeRate
}

// AccelerateOrder uses the Child-Pays-For-Parent technique to accelerate a
// chain of swap transactions and previous accelerations. It broadcasts a new
// transaction with a fee high enough so that the average fee of all the
// unconfirmed transactions in the chain and the new transaction will have
// an average fee rate of newFeeRate. The changeCoin argument is the latest
// change in the order. It must be the input in the acceleration transaction
// in order for the order to be accelerated. requiredForRemainingSwaps is the
// amount of funds required to complete the rest of the swaps in the order.
// The change output of the acceleration transaction will have at least
// this amount.
//
// The returned change coin may be nil, and should be checked before use.
func (btc *ExchangeWalletAccelerator) AccelerateOrder(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (asset.Coin, string, error) {
	return accelerateOrder(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
}

// AccelerateOrder uses the Child-Pays-For-Parent technique to accelerate a
// chain of swap transactions and previous accelerations. It broadcasts a new
// transaction with a fee high enough so that the average fee of all the
// unconfirmed transactions in the chain and the new transaction will have
// an average fee rate of newFeeRate. The changeCoin argument is the latest
// change in the order. It must be the input in the acceleration transaction
// in order for the order to be accelerated. requiredForRemainingSwaps is the
// amount of funds required to complete the rest of the swaps in the order.
// The change output of the acceleration transaction will have at least
// this amount.
//
// The returned change coin may be nil, and should be checked before use.
func (btc *ExchangeWalletSPV) AccelerateOrder(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (asset.Coin, string, error) {
	return accelerateOrder(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
}

func accelerateOrder(btc *baseWallet, swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (asset.Coin, string, error) {
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()

	changeTxHash, changeVout, err := decodeCoinID(changeCoin)
	if err != nil {
		return nil, "", err
	}
	changeOutput, err := btc.lookupWalletTxOutput(changeTxHash, changeVout)
	if err != nil {
		return nil, "", err
	}
	previousTxs, err := btc.getTransactions(append(swapCoins, accelerationCoins...))
	if err != nil {
		return nil, "", err
	}
	signedTx, newChange, _, err :=
		btc.signedAccelerationTx(previousTxs, changeOutput, requiredForRemainingSwaps, newFeeRate)
	if err != nil {
		return nil, "", err
	}

	_, err = btc.broadcastTx(signedTx)
	if err != nil {
		return nil, "", err
	}

	// Delete the old change from the cache
	delete(btc.fundingCoins, newOutPoint(changeTxHash, changeVout))

	if newChange == nil {
		return nil, btc.hashTx(signedTx).String(), nil
	}

	// Add the new change to the cache if needed. We check if
	// required for remaining swaps > 0 because this ensures if the
	// previous change was locked, this one will also be locked. If
	// requiredForRemainingSwaps = 0, but the change was locked,
	// changeCanBeAccelerated would have returned an error since this means
	// that the change was locked by another order.
	if requiredForRemainingSwaps > 0 {
		err = btc.node.lockUnspent(false, []*output{newChange})
		if err != nil {
			// The transaction is already broadcasted, so don't fail now.
			btc.log.Errorf("failed to lock change output: %v", err)
		}

		// Log it as a fundingCoin, since it is expected that this will be
		// chained into further matches.
		btc.fundingCoins[newChange.pt] = &utxo{
			txHash:  newChange.txHash(),
			vout:    newChange.vout(),
			address: newChange.String(),
			amount:  newChange.value,
		}
	}

	// return nil error since tx is already broadcast, and core needs to update
	// the change coin
	return newChange, btc.hashTx(signedTx).String(), nil
}

// AccelerationEstimate takes the same parameters as AccelerateOrder, but
// instead of broadcasting the acceleration transaction, it just returns
// the amount of funds that will need to be spent in order to increase the
// average fee rate to the desired amount.
func (btc *ExchangeWalletAccelerator) AccelerationEstimate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (uint64, error) {
	return accelerationEstimate(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
}

// AccelerationEstimate takes the same parameters as AccelerateOrder, but
// instead of broadcasting the acceleration transaction, it just returns
// the amount of funds that will need to be spent in order to increase the
// average fee rate to the desired amount.
func (btc *ExchangeWalletSPV) AccelerationEstimate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (uint64, error) {
	return accelerationEstimate(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
}

func accelerationEstimate(btc *baseWallet, swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (uint64, error) {
	btc.fundingMtx.RLock()
	defer btc.fundingMtx.RUnlock()

	previousTxs, err := btc.getTransactions(append(swapCoins, accelerationCoins...))
	if err != nil {
		return 0, fmt.Errorf("failed to get transactions: %w", err)
	}

	changeTxHash, changeVout, err := decodeCoinID(changeCoin)
	if err != nil {
		return 0, err
	}
	changeOutput, err := btc.lookupWalletTxOutput(changeTxHash, changeVout)
	if err != nil {
		return 0, err
	}

	_, _, fee, err := btc.signedAccelerationTx(previousTxs, changeOutput, requiredForRemainingSwaps, newFeeRate)
	if err != nil {
		return 0, err
	}

	return fee, nil
}

// tooEarlyToAccelerate returns an asset.EarlyAcceleration if
// minTimeBeforeAcceleration has not passed since either the earliest
// unconfirmed swap transaction, or the latest acceleration transaction.
func tooEarlyToAccelerate(swapTxs []*GetTransactionResult, accelerationTxs []*GetTransactionResult) (*asset.EarlyAcceleration, error) {
	accelerationTxLookup := make(map[string]bool, len(accelerationTxs))
	for _, accelerationCoin := range accelerationTxs {
		accelerationTxLookup[accelerationCoin.TxID] = true
	}

	var latestAcceleration, earliestUnconfirmed uint64 = 0, math.MaxUint64
	for _, tx := range swapTxs {
		if tx.Confirmations > 0 {
			continue
		}
		if tx.Time < earliestUnconfirmed {
			earliestUnconfirmed = tx.Time
		}
	}
	for _, tx := range accelerationTxs {
		if tx.Confirmations > 0 {
			continue
		}
		if tx.Time > latestAcceleration {
			latestAcceleration = tx.Time
		}
	}

	var actionTime uint64
	var wasAccelerated bool
	if latestAcceleration == 0 && earliestUnconfirmed == math.MaxUint64 {
		return nil, fmt.Errorf("no need to accelerate because all tx are confirmed")
	} else if earliestUnconfirmed > latestAcceleration && earliestUnconfirmed < math.MaxUint64 {
		actionTime = earliestUnconfirmed
	} else {
		actionTime = latestAcceleration
		wasAccelerated = true
	}

	currentTime := uint64(time.Now().Unix())
	if actionTime+minTimeBeforeAcceleration > currentTime {
		return &asset.EarlyAcceleration{
			TimePast:       currentTime - actionTime,
			WasAccelerated: wasAccelerated,
		}, nil
	}

	return nil, nil
}

// PreAccelerate returns the current average fee rate of the unmined swap
// initiation and acceleration transactions, and also returns a suggested
// range that the fee rate should be increased to in order to expedite mining.
// The feeSuggestion argument is the current prevailing network rate. It is
// used to help determine the suggestedRange, which is a range meant to give
// the user a good amount of flexibility in determining the post acceleration
// effective fee rate, but still not allowing them to pick something
// outrageously high.
func (btc *ExchangeWalletAccelerator) PreAccelerate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *asset.XYRange, *asset.EarlyAcceleration, error) {
	return preAccelerate(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, feeSuggestion)
}

// PreAccelerate returns the current average fee rate of the unmined swap
// initiation and acceleration transactions, and also returns a suggested
// range that the fee rate should be increased to in order to expedite mining.
// The feeSuggestion argument is the current prevailing network rate. It is
// used to help determine the suggestedRange, which is a range meant to give
// the user a good amount of flexibility in determining the post acceleration
// effective fee rate, but still not allowing them to pick something
// outrageously high.
func (btc *ExchangeWalletSPV) PreAccelerate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *asset.XYRange, *asset.EarlyAcceleration, error) {
	return preAccelerate(btc.baseWallet, swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, feeSuggestion)
}

// maxAccelerationRate returns the max rate to which an order can be
// accelerated, if the max rate is less than rateNeeded. If the max rate is
// greater than rateNeeded, rateNeeded is returned.
func (btc *baseWallet) maxAccelerationRate(changeVal, feesAlreadyPaid, orderTxVBytes, requiredForRemainingSwaps, rateNeeded uint64) (uint64, error) {
	var txSize, witnessSize, additionalUtxosVal uint64

	// First, add all the elements that will definitely be part of the
	// acceleration transaction, without any additional inputs.
	txSize += dexbtc.MinimumTxOverhead
	if btc.segwit {
		txSize += dexbtc.RedeemP2WPKHInputSize
		witnessSize += dexbtc.RedeemP2WPKHInputWitnessWeight
	} else {
		txSize += dexbtc.RedeemP2PKHInputSize
	}
	if requiredForRemainingSwaps > 0 {
		if btc.segwit {
			txSize += dexbtc.P2WPKHOutputSize
		} else {
			txSize += dexbtc.P2PKHOutputSize
		}
	}

	calcFeeRate := func() uint64 {
		accelerationTxVBytes := txSize + (witnessSize+3)/4
		totalValue := changeVal + feesAlreadyPaid + additionalUtxosVal
		if totalValue < requiredForRemainingSwaps {
			return 0
		}
		totalValue -= requiredForRemainingSwaps
		totalSize := accelerationTxVBytes + orderTxVBytes
		return totalValue / totalSize
	}

	if calcFeeRate() >= rateNeeded {
		return rateNeeded, nil
	}

	// If necessary, use as many additional utxos as needed
	btc.fundingMtx.RLock()
	utxos, _, _, err := btc.spendableUTXOs(1)
	btc.fundingMtx.RUnlock()
	if err != nil {
		return 0, err
	}

	for _, utxo := range utxos {
		if utxo.input.NonStandardScript {
			continue
		}
		txSize += dexbtc.TxInOverhead +
			uint64(wire.VarIntSerializeSize(uint64(utxo.input.SigScriptSize))) +
			uint64(utxo.input.SigScriptSize)
		witnessSize += uint64(utxo.input.WitnessSize)
		additionalUtxosVal += utxo.amount
		if calcFeeRate() >= rateNeeded {
			return rateNeeded, nil
		}
	}

	return calcFeeRate(), nil
}

func preAccelerate(btc *baseWallet, swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *asset.XYRange, *asset.EarlyAcceleration, error) {
	makeError := func(err error) (uint64, *asset.XYRange, *asset.EarlyAcceleration, error) {
		return 0, &asset.XYRange{}, nil, err
	}

	changeTxHash, changeVout, err := decodeCoinID(changeCoin)
	if err != nil {
		return makeError(err)
	}
	changeOutput, err := btc.lookupWalletTxOutput(changeTxHash, changeVout)
	if err != nil {
		return makeError(err)
	}

	err = btc.changeCanBeAccelerated(changeOutput, requiredForRemainingSwaps > 0)
	if err != nil {
		return makeError(err)
	}

	txs, err := btc.getTransactions(append(swapCoins, accelerationCoins...))
	if err != nil {
		return makeError(fmt.Errorf("failed to get transactions: %w", err))
	}

	existingTxSize, feesAlreadyPaid, err := btc.sizeAndFeesOfUnconfirmedTxs(txs)
	if err != nil {
		return makeError(err)
	}
	// Is it safe to assume that transactions will all have some fee?
	if feesAlreadyPaid == 0 {
		return makeError(fmt.Errorf("all transactions are already confirmed, no need to accelerate"))
	}

	earlyAcceleration, err := tooEarlyToAccelerate(txs[:len(swapCoins)], txs[len(swapCoins):])
	if err != nil {
		return makeError(err)
	}

	// The suggested range will be the min and max of the slider that is
	// displayed on the UI. The minimum of the range is 1 higher than the
	// current effective range of the swap transactions. The max of the range
	// will be the maximum of 5x the current effective rate, or 5x the current
	// prevailing network rate. This is a completely arbitrary choice, but in
	// this way the user will definitely be able to accelerate at least 5x the
	// original rate, and even if the prevailing network rate is much higher
	// than the current effective rate, they will still have a comformtable
	// buffer above the prevailing network rate.
	const scalingFactor = 5
	currentEffectiveRate := feesAlreadyPaid / existingTxSize
	maxSuggestion := currentEffectiveRate * scalingFactor
	if feeSuggestion > currentEffectiveRate {
		maxSuggestion = feeSuggestion * scalingFactor
	}

	// We must make sure that the wallet can fund an acceleration at least
	// the max suggestion, and if not, lower the max suggestion to the max
	// rate that the wallet can fund.
	maxRate, err := btc.maxAccelerationRate(changeOutput.value, feesAlreadyPaid, existingTxSize, requiredForRemainingSwaps, maxSuggestion)
	if err != nil {
		return makeError(err)
	}
	if maxRate <= currentEffectiveRate {
		return makeError(fmt.Errorf("cannot accelerate, max rate %v <= current rate %v", maxRate, currentEffectiveRate))
	}
	if maxRate < maxSuggestion {
		maxSuggestion = maxRate
	}

	suggestedRange := asset.XYRange{
		Start: asset.XYRangePoint{
			Label: "Min",
			X:     float64(currentEffectiveRate+1) / float64(currentEffectiveRate),
			Y:     float64(currentEffectiveRate + 1),
		},
		End: asset.XYRangePoint{
			Label: "Max",
			X:     float64(maxSuggestion) / float64(currentEffectiveRate),
			Y:     float64(maxSuggestion),
		},
		XUnit: "X",
		YUnit: btc.walletInfo.UnitInfo.AtomicUnit + "/" + btc.sizeUnit(),
	}

	return currentEffectiveRate, &suggestedRange, earlyAcceleration, nil
}

// Swap sends the swaps in a single transaction and prepares the receipts. The
// Receipts returned can be used to refund a failed transaction. The Input coins
// are NOT manually unlocked because they're auto-unlocked when the transaction
// is broadcasted.
func (btc *baseWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	if swaps.FeeRate == 0 {
		return nil, nil, 0, fmt.Errorf("cannot send swap with with zero fee rate")
	}

	contracts := make([][]byte, 0, len(swaps.Contracts))
	var totalOut uint64
	// Start with an empty MsgTx.
	baseTx, totalIn, pts, err := btc.fundedTx(swaps.Inputs)
	if err != nil {
		return nil, nil, 0, err
	}

	customCfg := new(swapOptions)
	err = config.Unmapify(swaps.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing swap options: %w", err)
	}

	refundAddrs := make([]btcutil.Address, 0, len(swaps.Contracts))

	// Add the contract outputs.
	// TODO: Make P2WSH contract and P2WPKH change outputs instead of
	// legacy/non-segwit swap contracts pkScripts.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address belonging to the key that may be used to
		// sign and refund a swap past its encoded refund locktime.
		revokeAddrStr, err := btc.recyclableAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating revocation address: %w", err)
		}
		revokeAddr, err := btc.decodeAddr(revokeAddrStr, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("refund address decode error: %v", err)
		}
		refundAddrs = append(refundAddrs, revokeAddr)

		contractAddr, err := btc.decodeAddr(contract.Address, btc.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("contract address decode error: %v", err)
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

	feeRate, err := calcBumpedRate(swaps.FeeRate, customCfg.FeeBump)
	if err != nil {
		btc.log.Errorf("ignoring invalid fee bump factor, %s: %v", float64PtrStr(customCfg.FeeBump), err)
	}

	// Sign, add change, but don't send the transaction yet until
	// the individual swap refund txs are prepared and signed.
	msgTx, change, fees, err := btc.signTxAndAddChange(baseTx, changeAddr, totalIn, totalOut, feeRate)
	if err != nil {
		return nil, nil, 0, err
	}
	txHash := btc.hashTx(msgTx)

	// Prepare the receipts.
	receipts := make([]asset.Receipt, 0, swapCount)
	for i, contract := range swaps.Contracts {
		output := newOutput(txHash, uint32(i), contract.Value)
		refundAddr := refundAddrs[i]
		signedRefundTx, err := btc.refundTx(output.txHash(), output.vout(), contracts[i],
			contract.Value, refundAddr, swaps.FeeRate)
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
	_, err = btc.broadcastTx(msgTx)
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

		addrStr, err := btc.stringAddr(changeAddr, btc.chainParams)
		if err != nil {
			btc.log.Errorf("Failed to stringify address %v (default encoding): %v", changeAddr, err)
			addrStr = changeAddr.String() // may or may not be able to retrieve the private keys for the next swap!
		}

		// Log it as a fundingCoin, since it is expected that this will be
		// chained into further matches.
		btc.fundingCoins[change.pt] = &utxo{
			txHash:  change.txHash(),
			vout:    change.vout(),
			address: addrStr,
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
	msgTx := wire.NewMsgTx(btc.txVersion())
	var totalIn uint64
	contracts := make([][]byte, 0, len(form.Redemptions))
	prevScripts := make([][]byte, 0, len(form.Redemptions))
	addresses := make([]btcutil.Address, 0, len(form.Redemptions))
	values := make([]int64, 0, len(form.Redemptions))
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
		pkScript, err := btc.scriptHashScript(contract)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error constructs p2sh script: %v", err)
		}
		prevScripts = append(prevScripts, pkScript)
		addresses = append(addresses, receiver)
		contracts = append(contracts, contract)
		txIn := wire.NewTxIn(cinfo.output.wireOutPoint(), nil, nil)
		msgTx.AddTxIn(txIn)
		values = append(values, int64(cinfo.output.value))
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := btc.calcTxSize(msgTx)
	if btc.segwit {
		// Add the marker and flag weight here.
		witnessVBytes := (dexbtc.RedeemSwapSigScriptSize*uint64(len(form.Redemptions)) + 2 + 3) / 4
		size += witnessVBytes + dexbtc.P2WPKHOutputSize
	} else {
		size += dexbtc.RedeemSwapSigScriptSize*uint64(len(form.Redemptions)) + dexbtc.P2PKHOutputSize
	}

	customCfg := new(redeemOptions)
	err := config.Unmapify(form.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing selected swap options: %w", err)
	}

	rawFeeRate := btc.targetFeeRateWithFallback(btc.redeemConfTarget(), form.FeeSuggestion)
	feeRate, err := calcBumpedRate(rawFeeRate, customCfg.FeeBump)
	if err != nil {
		btc.log.Errorf("calcBumpRate error: %v", err)
	}
	fee := feeRate * size
	if fee > totalIn {
		// Double check that the fee bump isn't the issue.
		feeRate = rawFeeRate
		fee = feeRate * size
		if fee > totalIn {
			return nil, nil, 0, fmt.Errorf("redeem tx not worth the fees")
		}
		btc.log.Warnf("Ignoring fee bump (%s) resulting in fees > redemption", float64PtrStr(customCfg.FeeBump))
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
	if btc.IsDust(txOut, feeRate) {
		return nil, nil, 0, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)

	if btc.segwit {
		// NewTxSigHashes uses the PrevOutFetcher only for detecting a taproot
		// output, so we can provide a dummy that always returns a wire.TxOut
		// with a nil pkScript that so IsPayToTaproot returns false.
		sigHashes := txscript.NewTxSigHashes(msgTx, new(txscript.CannedPrevOutputFetcher))
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
			redeemSig, redeemPubKey, err := btc.createSig(msgTx, i, contract, addresses[i], values, prevScripts)
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
	txHash, err := btc.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
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
		output:     newOutput(txHash, vout, ai.Coin.Value()), // *output
		recipient:  recip,                                    // btcutil.Address
		contract:   ai.Contract,                              // []byte
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
	defer privKey.Zero()
	pk := privKey.PubKey()
	hash := chainhash.HashB(msg) // legacy servers will not accept this signature!
	sig := ecdsa.Sign(privKey, hash)
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
		tx, err = btc.deserializeTx(txData)
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

	addrStr, err := btc.stringAddr(receiver, btc.chainParams)
	if err != nil {
		btc.log.Errorf("Failed to stringify receiver address %v (default): %v", receiver, err)
		addrStr = receiver.String() // potentially misleading AuditInfo.Recipient
	}

	return &asset.AuditInfo{
		Coin:       newOutput(txHash, vout, uint64(txOut.Value)),
		Recipient:  addrStr,
		Contract:   contract,
		SecretHash: secretHash,
		Expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// LockTimeExpired returns true if the specified locktime has expired, making it
// possible to redeem the locked coins.
func (btc *baseWallet) LockTimeExpired(_ context.Context, lockTime time.Time) (bool, error) {
	medianTime, err := btc.node.medianTime() // TODO: pass ctx
	if err != nil {
		return false, fmt.Errorf("error getting median time: %w", err)
	}
	return medianTime.After(lockTime), nil
}

// ContractLockTimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (btc *baseWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	expired, err := btc.LockTimeExpired(ctx, contractExpiry)
	if err != nil {
		return false, time.Time{}, err
	}
	return expired, contractExpiry, nil
}

// FindRedemption watches for the input that spends the specified contract
// coin, and returns the spending input and the contract's secret key when it
// finds a spender.
//
// This method blocks until the redemption is found, an error occurs or the
// provided context is canceled.
func (btc *intermediaryWallet) FindRedemption(ctx context.Context, coinID, _ dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot decode contract coin id: %w", err)
	}

	outPt := newOutPoint(txHash, vout)

	tx, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		return nil, nil, fmt.Errorf("error finding wallet transaction: %v", err)
	}

	txOut, err := btc.txOutFromTxBytes(tx.Hex, vout)
	if err != nil {
		return nil, nil, err
	}
	pkScript := txOut.PkScript

	var blockHash *chainhash.Hash
	if tx.BlockHash != "" {
		blockHash, err = chainhash.NewHashFromStr(tx.BlockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("error decoding block hash from string %q: %w",
				tx.BlockHash, err)
		}
	}

	var blockHeight int32
	if blockHash != nil {
		btc.log.Infof("FindRedemption - Checking block %v for swap %v", blockHash, outPt)
		blockHeight, err = btc.checkRedemptionBlockDetails(outPt, blockHash, pkScript)
		if err != nil {
			return nil, nil, fmt.Errorf("checkRedemptionBlockDetails: op %v / block %q: %w",
				outPt, tx.BlockHash, err)
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
		return nil, nil, fmt.Errorf("queueFindRedemptionRequest error for redemption %s: %w", outPt, err)
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
func (btc *intermediaryWallet) tryRedemptionRequests(ctx context.Context, startBlock *chainhash.Hash, reqs []*findRedemptionReq) {
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
	//  1) A new block is mined during our search. In this case, we won't
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
		h, err := btc.tipRedeemer.getBlockHeight(startBlock)
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

		btc.log.Debugf("tryRedemptionRequests - Checking block %v for redemptions...", iHash)
		discovered := btc.tipRedeemer.searchBlockForRedemptions(ctx, validReqs, *iHash)
		for outPt, res := range discovered {
			req, found := undiscovered[outPt]
			if !found {
				btc.log.Critical("Request not found in undiscovered map. This shouldn't be possible.")
				continue
			}
			redeemTxID, redeemTxInput, _ := decodeCoinID(res.redemptionCoinID)
			btc.log.Debugf("Found redemption %s:%d", redeemTxID, redeemTxInput)
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
	for outPt, res := range btc.tipRedeemer.findRedemptionsInMempool(searchCtx, mempoolReqs) {
		req, ok := mempoolReqs[outPt]
		if !ok {
			btc.log.Errorf("findRedemptionsInMempool discovered outpoint not found")
			continue
		}
		req.success(res)
	}
	if err := searchCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			btc.log.Errorf("mempool search exceeded %s time limit", searchDur)
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
func (btc *baseWallet) Refund(coinID, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	pkScript, err := btc.scriptHashScript(contract)
	if err != nil {
		return nil, fmt.Errorf("error parsing pubkey script: %w", err)
	}

	if feeRate == 0 {
		feeRate = btc.targetFeeRateWithFallback(2, 0)
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
	msgTx, err := btc.refundTx(txHash, vout, contract, uint64(utxo.Value), nil, feeRate)
	if err != nil {
		return nil, fmt.Errorf("error creating refund tx: %w", err)
	}

	refundHash, err := btc.broadcastTx(msgTx)
	if err != nil {
		return nil, fmt.Errorf("broadcastTx: %w", err)
	}
	return toCoinID(refundHash, 0), nil
}

// refundTx creates and signs a contract`s refund transaction. If refundAddr is
// not supplied, one will be requested from the wallet.
func (btc *baseWallet) refundTx(txHash *chainhash.Hash, vout uint32, contract dex.Bytes, val uint64, refundAddr btcutil.Address, feeRate uint64) (*wire.MsgTx, error) {
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// Create the transaction that spends the contract.
	msgTx := wire.NewMsgTx(btc.txVersion())
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	// Enable the OP_CHECKLOCKTIMEVERIFY opcode to be used.
	//
	// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.

	size := btc.calcTxSize(msgTx)

	if btc.segwit {
		// Add the marker and flag weight too.
		witnessVBytes := uint64((dexbtc.RefundSigScriptSize + 2 + 3) / 4)
		size += witnessVBytes + dexbtc.P2WPKHOutputSize
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
	if btc.IsDust(txOut, feeRate) {
		return nil, fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)

	if btc.segwit {
		sigHashes := txscript.NewTxSigHashes(msgTx, new(txscript.CannedPrevOutputFetcher))
		refundSig, refundPubKey, err := btc.createWitnessSig(msgTx, 0, contract, sender, int64(val), sigHashes)
		if err != nil {
			return nil, fmt.Errorf("createWitnessSig: %w", err)
		}
		txIn.Witness = dexbtc.RefundP2WSHContract(contract, refundSig, refundPubKey)

	} else {
		prevScript, err := btc.scriptHashScript(contract)
		if err != nil {
			return nil, fmt.Errorf("error constructing p2sh script: %w", err)
		}

		refundSig, refundPubKey, err := btc.createSig(msgTx, 0, contract, sender, []int64{int64(val)}, [][]byte{prevScript})
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

// DepositAddress returns an address for depositing funds into the
// exchange wallet.
func (btc *baseWallet) DepositAddress() (string, error) {
	addr, err := btc.node.externalAddress()
	if err != nil {
		return "", err
	}
	addrStr, err := btc.stringAddr(addr, btc.chainParams)
	if err != nil {
		return "", err
	}
	if btc.node.locked() {
		return addrStr, nil
	}

	// If the wallet is unlocked, be extra cautious and ensure the wallet gave
	// us an address for which we can retrieve the private keys, regardless of
	// what ownsAddress would say.
	priv, err := btc.node.privKeyForAddress(addrStr)
	if err != nil {
		return "", fmt.Errorf("private key unavailable for address %v: %w", addrStr, err)
	}
	priv.Zero()
	return addrStr, nil
}

// RedemptionAddress gets an address for use in redeeming the counterparty's
// swap. This would be included in their swap initialization.
func (btc *baseWallet) RedemptionAddress() (string, error) {
	return btc.recyclableAddress()
}

// A recyclable address is a redemption or refund address that may be recycled
// if unused. If already recycled addresses are available, one will be returned.
func (btc *baseWallet) recyclableAddress() (string, error) {
	var recycledAddr string
	btc.recycledAddrMtx.Lock()
	for addr := range btc.recycledAddrs {
		if owns, err := btc.OwnsDepositAddress(addr); owns {
			delete(btc.recycledAddrs, addr)
			recycledAddr = addr
			break
		} else if err != nil {
			btc.log.Errorf("Error checking ownership of recycled address %q: %v", addr, err)
			// Don't delete the address in case it's just a network error for
			// an rpc wallet or something.
		} else { // we don't own it
			delete(btc.recycledAddrs, addr)
		}
	}
	btc.recycledAddrMtx.Unlock()
	if recycledAddr != "" {
		return recycledAddr, nil
	}

	return btc.DepositAddress()
}

// ReturnRefundContracts should be called with the Receipt.Contract() data for
// any swaps that will not be refunded.
func (btc *baseWallet) ReturnRefundContracts(contracts [][]byte) {
	addrs := make([]string, 0, len(contracts))
	for _, c := range contracts {
		sender, _, _, _, err := dexbtc.ExtractSwapDetails(c, btc.segwit, btc.chainParams)
		if err != nil {
			btc.log.Errorf("Error extracting refund address from contract '%x': %v", c, err)
			continue
		}
		addr, err := btc.stringAddr(sender, btc.chainParams)
		if err != nil {
			btc.log.Errorf("Error stringifying address %q: %v", addr, err)
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) > 0 {
		btc.returnAddresses(addrs)
	}
}

// ReturnRedemptionAddress accepts a Wallet.RedemptionAddress() if the address
// will not be used.
func (btc *baseWallet) ReturnRedemptionAddress(addr string) {
	btc.returnAddresses([]string{addr})
}

func (btc *baseWallet) returnAddresses(addrs []string) {
	btc.recycledAddrMtx.Lock()
	defer btc.recycledAddrMtx.Unlock()
	for _, addr := range addrs {
		if _, exists := btc.recycledAddrs[addr]; exists {
			btc.log.Errorf("Returned address %q was already indexed", addr)
			continue
		}
		btc.recycledAddrs[addr] = struct{}{}
	}
}

// NewAddress returns a new address from the wallet. This satisfies the
// NewAddresser interface.
func (btc *baseWallet) NewAddress() (string, error) {
	return btc.DepositAddress()
}

// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
// pay the registration fee using the provided feeRate.
func (btc *baseWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	const inputCount = 5 // buffer so this estimate is higher than actual reg tx fee.
	if feeRate == 0 || feeRate > btc.feeRateLimit() {
		feeRate = btc.fallbackFeeRate()
	}
	return (dexbtc.MinimumTxOverhead + 2*dexbtc.P2PKHOutputSize + inputCount*dexbtc.RedeemP2PKHInputSize) * feeRate
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of sats/byte.
// Withdraw satisfies asset.Withdrawer.
func (btc *baseWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(feeRate), true)
	if err != nil {
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// Send sends the exact value to the specified address. This is different from
// Withdraw, which subtracts the tx fees from the amount sent. feeRate is in
// units of sats/byte.
func (btc *baseWallet) Send(address string, value, feeRate uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, btc.feeRateWithFallback(feeRate), false)
	if err != nil {
		return nil, err
	}
	return newOutput(txHash, vout, sent), nil
}

// SendTransaction broadcasts a valid fully-signed transaction.
func (btc *baseWallet) SendTransaction(rawTx []byte) ([]byte, error) {
	msgTx, err := btc.deserializeTx(rawTx)
	if err != nil {
		return nil, err
	}
	txHash, err := btc.node.sendRawTransaction(msgTx)
	if err != nil {
		return nil, err
	}
	return toCoinID(txHash, 0), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (btc *baseWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of sats/byte.
func (btc *baseWallet) send(address string, val uint64, feeRate uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("invalid address: %s", address)
	}
	pay2script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("PayToAddrScript error: %w", err)
	}

	baseSize := dexbtc.MinimumTxOverhead
	if btc.segwit {
		baseSize += dexbtc.P2WPKHOutputSize * 2
	} else {
		baseSize += dexbtc.P2PKHOutputSize * 2
	}

	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()

	enough := sendEnough(val, feeRate, subtract, uint64(baseSize), btc.segwit, true)
	minConfs := uint32(0)
	coins, _, _, _, inputsSize, _, err := btc.fundInternal(btc.reserves(), minConfs, false, enough)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error funding transaction: %w", err)
	}

	fundedTx, totalIn, _, err := btc.fundedTx(coins)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error adding inputs to transaction: %w", err)
	}

	fees := feeRate * (inputsSize + uint64(baseSize))
	var toSend uint64
	if subtract {
		toSend = val - fees
	} else {
		toSend = val
	}
	fundedTx.AddTxOut(wire.NewTxOut(int64(toSend), pay2script))

	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error creating change address: %w", err)
	}

	msgTx, err := btc.sendWithReturn(fundedTx, changeAddr, totalIn, toSend, feeRate)
	if err != nil {
		return nil, 0, 0, err
	}

	txHash := btc.hashTx(msgTx)
	return txHash, 0, toSend, nil
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
	_, confs, err = btc.rawWalletTx(txHash)
	return
}

func (btc *baseWallet) checkPeers() {
	numPeers, err := btc.node.peerCount()
	if err != nil {
		prevPeer := atomic.SwapUint32(&btc.lastPeerCount, 0)
		if prevPeer != 0 {
			btc.log.Errorf("Failed to get peer count: %v", err)
			btc.peersChange(0, err)
		}
		return
	}
	prevPeer := atomic.SwapUint32(&btc.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		btc.peersChange(numPeers, nil)
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
func (btc *intermediaryWallet) watchBlocks(ctx context.Context) {
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
			newTipHdr, err := btc.node.getBestBlockHeader()
			if err != nil {
				go btc.tipChange(fmt.Errorf("failed to get best block header from %s node: %v", btc.symbol, err))
				continue
			}
			newTipHash, err := chainhash.NewHashFromStr(newTipHdr.Hash)
			if err != nil {
				go btc.tipChange(fmt.Errorf("invalid best block hash from %s node: %v", btc.symbol, err))
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

			newTip := &block{newTipHdr.Height, *newTipHash}

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
					btc.log.Errorf("Error retrieving sync status before queuing polled block: %v", err)
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

		// Ensure context cancellation takes priority before the next iteration.
		if ctx.Err() != nil {
			return
		}
	}
}

// prepareRedemptionRequestsForBlockCheck prepares a copy of the
// findRedemptionQueue, checking for missing block data along the way.
func (btc *intermediaryWallet) prepareRedemptionRequestsForBlockCheck() []*findRedemptionReq {
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
func (btc *intermediaryWallet) reportNewTip(ctx context.Context, newTip *block) {
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
	prevTipHeader, isMainchain, err := btc.tipRedeemer.getBlockHeader(&prevTip.hash)
	switch {
	case err != nil:
		// Redemption search cannot continue reliably without knowing if there
		// was a reorg, cancel all find redemption requests in queue.
		notifyFatalFindRedemptionError("getBlockHeader error for prev tip hash %s: %w",
			prevTip.hash, err)
		return

	case !isMainchain:
		// The previous tip is no longer part of the mainchain. Crawl blocks
		// backwards until finding a mainchain block. Start with the block
		// that is the immediate ancestor to the previous tip.
		ancestorBlockHash, err := chainhash.NewHashFromStr(prevTipHeader.PreviousBlockHash)
		if err != nil {
			notifyFatalFindRedemptionError("hash decode error for block %s: %w", prevTipHeader.PreviousBlockHash, err)
			return
		}
		for {
			aBlock, isMainchain, err := btc.tipRedeemer.getBlockHeader(ancestorBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("getBlockHeader error for block %s: %w", ancestorBlockHash, err)
				return
			}
			if isMainchain {
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
func (btc *intermediaryWallet) trySetRedemptionRequestBlock(req *findRedemptionReq) {
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
func (btc *intermediaryWallet) checkRedemptionBlockDetails(outPt outPoint, blockHash *chainhash.Hash, pkScript []byte) (int32, error) {
	blockHeight, err := btc.tipRedeemer.getBlockHeight(blockHash)
	if err != nil {
		return 0, fmt.Errorf("GetBlockHeight for redemption block %s error: %w", blockHash, err)
	}
	blk, err := btc.tipRedeemer.getBlock(*blockHash)
	if err != nil {
		return 0, fmt.Errorf("error retrieving redemption block %s: %w", blockHash, err)
	}

	var tx *wire.MsgTx
out:
	for _, iTx := range blk.Transactions {
		if *btc.hashTx(iTx) == outPt.txHash {
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

	_, err = btc.broadcastTx(signedTx)
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
	vSize := btc.calcTxSize(msgTx)
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
	changeAdded := !btc.IsDust(changeOutput, feeRate)
	if changeAdded {
		// Add the change output.
		vSize0 := btc.calcTxSize(baseTx)
		baseTx.AddTxOut(changeOutput)
		changeSize := btc.calcTxSize(baseTx) - vSize0       // may be dexbtc.P2WPKHOutputSize
		addrStr, _ := btc.stringAddr(addr, btc.chainParams) // just for logging
		btc.log.Debugf("Change output size = %d, addr = %s", changeSize, addrStr)

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
			vSize = btc.calcTxSize(msgTx) // recompute the size with new tx signature
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
			if btc.IsDust(changeOutput, feeRate) {
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
	} else {
		btc.log.Debugf("Foregoing change worth up to %v in tx %v because it is dust",
			changeOutput.Value, btc.hashTx(msgTx))
	}

	txHash := btc.hashTx(msgTx)

	fee := totalIn - totalOut
	actualFeeRate := fee / vSize
	btc.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, txHash, feeRate, actualFeeRate, fee, vSize, changeAdded)

	var change *output
	if changeAdded {
		change = newOutput(txHash, uint32(changeIdx), uint64(changeOutput.Value))
	}

	return msgTx, change, fee, nil
}

func (btc *baseWallet) broadcastTx(signedTx *wire.MsgTx) (*chainhash.Hash, error) {
	txHash, err := btc.node.sendRawTransaction(signedTx)
	if err != nil {
		return nil, fmt.Errorf("sendrawtx error: %v, raw tx: %x", err, btc.wireBytes(signedTx))
	}
	checkHash := btc.hashTx(signedTx)
	if *txHash != *checkHash {
		return nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %x", checkHash, *txHash, btc.wireBytes(signedTx))
	}
	return txHash, nil
}

// txOutFromTxBytes parses the specified *wire.TxOut from the serialized
// transaction.
func (btc *baseWallet) txOutFromTxBytes(txB []byte, vout uint32) (*wire.TxOut, error) {
	msgTx, err := btc.deserializeTx(txB)
	if err != nil {
		return nil, fmt.Errorf("error decoding transaction bytes: %v", err)
	}

	if len(msgTx.TxOut) <= int(vout) {
		return nil, fmt.Errorf("no vout %d in tx %s", vout, btc.hashTx(msgTx))
	}
	return msgTx.TxOut[vout], nil
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (btc *baseWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address, vals []int64, pkScripts [][]byte) (sig, pubkey []byte, err error) {
	addrStr, err := btc.stringAddr(addr, btc.chainParams)
	if err != nil {
		return nil, nil, err
	}

	privKey, err := btc.node.privKeyForAddress(addrStr)
	if err != nil {
		return nil, nil, err
	}
	defer privKey.Zero()

	sig, err = btc.signNonSegwit(tx, idx, pkScript, txscript.SigHashAll, privKey, vals, pkScripts)
	if err != nil {
		return nil, nil, err
	}

	return sig, privKey.PubKey().SerializeCompressed(), nil
}

// createWitnessSig creates and returns a signature for the witness of a segwit
// input and the pubkey associated with the address.
func (btc *baseWallet) createWitnessSig(tx *wire.MsgTx, idx int, pkScript []byte,
	addr btcutil.Address, val int64, sigHashes *txscript.TxSigHashes) (sig, pubkey []byte, err error) {
	addrStr, err := btc.stringAddr(addr, btc.chainParams)
	if err != nil {
		return nil, nil, err
	}
	privKey, err := btc.node.privKeyForAddress(addrStr)
	if err != nil {
		return nil, nil, err
	}
	defer privKey.Zero()
	sig, err = txscript.RawTxInWitnessSignature(tx, sigHashes, idx, val,
		pkScript, txscript.SigHashAll, privKey)

	if err != nil {
		return nil, nil, err
	}
	return sig, privKey.PubKey().SerializeCompressed(), nil
}

// ValidateAddress checks that the provided address is valid.
func (btc *baseWallet) ValidateAddress(address string) bool {
	_, err := btc.decodeAddr(address, btc.chainParams)
	return err == nil
}

// dummyP2PKHScript only has to be a valid 25-byte pay-to-pubkey-hash pkScript
// for EstimateSendTxFee when an empty or invalid address is provided.
var dummyP2PKHScript = []byte{0x76, 0xa9, 0x14, 0xe4, 0x28, 0x61, 0xa,
	0xfc, 0xd0, 0x4e, 0x21, 0x94, 0xf7, 0xe2, 0xcc, 0xf8,
	0x58, 0x7a, 0xc9, 0xe7, 0x2c, 0x79, 0x7b, 0x88, 0xac,
}

// EstimateSendTxFee returns a tx fee estimate for sending or withdrawing the
// provided amount using the provided feeRate.
func (btc *intermediaryWallet) EstimateSendTxFee(address string, sendAmount, feeRate uint64, subtract bool) (fee uint64, isValidAddress bool, err error) {
	if sendAmount == 0 {
		return 0, false, fmt.Errorf("cannot check fee: send amount = 0")
	}

	var pkScript []byte
	if addr, err := btc.decodeAddr(address, btc.chainParams); err == nil {
		pkScript, err = txscript.PayToAddrScript(addr)
		if err != nil {
			return 0, false, fmt.Errorf("error generating pubkey script: %w", err)
		}
		isValidAddress = true
	} else {
		// use a dummy 25-byte p2pkh script
		pkScript = dummyP2PKHScript
	}

	wireOP := wire.NewTxOut(int64(sendAmount), pkScript)
	if dexbtc.IsDust(wireOP, feeRate) {
		return 0, false, errors.New("output value is dust")
	}

	tx := wire.NewMsgTx(btc.txVersion())
	tx.AddTxOut(wireOP)
	fee, err = btc.txFeeEstimator.estimateSendTxFee(tx, btc.feeRateWithFallback(feeRate), subtract)
	if err != nil {
		return 0, false, err
	}
	return fee, isValidAddress, nil
}

func (btc *baseWallet) reserves() uint64 {
	btc.reservesMtx.RLock()
	defer btc.reservesMtx.RUnlock()
	if r := btc.bondReservesEnforced; r > 0 {
		return uint64(r)
	}
	if btc.bondReservesNominal == 0 { // disabled
		return 0
	}
	// When enforced is negative, we're unbonding. If nominal is still positive,
	// we're partially unbonding and we need to report the remaining reserves
	// after excess is unbonded, offsetting the negative enforced amount. This
	// is the relatively small fee buffer.
	if int64(btc.bondReservesUsed) == btc.bondReservesNominal {
		return uint64(-btc.bondReservesEnforced)
	}

	return 0
}

// bondLocked reduces reserves, increases bonded (used) amount.
func (btc *baseWallet) bondLocked(amt uint64) (reserved int64, unspent uint64) {
	btc.reservesMtx.Lock()
	defer btc.reservesMtx.Unlock()
	e0 := btc.bondReservesEnforced
	btc.bondReservesEnforced -= int64(amt)
	btc.bondReservesUsed += amt
	btc.log.Tracef("bondLocked (%v): enforced %v ==> %v (with bonded = %v / nominal = %v)",
		toBTC(amt), toBTC(e0), toBTC(btc.bondReservesEnforced),
		toBTC(btc.bondReservesUsed), toBTC(btc.bondReservesNominal))
	return btc.bondReservesEnforced, btc.bondReservesUsed
}

// bondSpent increases enforce reserves, decreases bonded amount. When the
// tracked unspent amount is reduced to zero, this clears the enforced amount
// (just the remaining fee buffer).
func (btc *baseWallet) bondSpent(amt uint64) (reserved int64, unspent uint64) {
	btc.reservesMtx.Lock()
	defer btc.reservesMtx.Unlock()

	if amt <= btc.bondReservesUsed {
		btc.bondReservesUsed -= amt
	} else {
		btc.log.Errorf("bondSpent: live bonds accounting error, spending bond worth %v with %v known live (zeroing!)",
			amt, btc.bondReservesUsed)
		btc.bondReservesUsed = 0
	}

	if btc.bondReservesNominal == 0 { // disabled
		return btc.bondReservesEnforced, btc.bondReservesUsed // return 0, ...
	}

	e0 := btc.bondReservesEnforced
	btc.bondReservesEnforced += int64(amt)

	btc.log.Tracef("bondSpent (%v): enforced %v ==> %v (with bonded = %v / nominal = %v)",
		toBTC(amt), toBTC(e0), toBTC(btc.bondReservesEnforced),
		toBTC(btc.bondReservesUsed), toBTC(btc.bondReservesNominal))
	return btc.bondReservesEnforced, btc.bondReservesUsed
}

// RegisterUnspent should be called once for every configured DEX with existing
// unspent bond amounts, prior to login, which is when reserves for future bonds
// are then added given the actual account tier, target tier, and this combined
// existing bonds amount. This must be used before ReserveBondFunds, which
// begins reserves enforcement provided a future amount that may be required
// before the existing bonds are refunded. No reserves enforcement is enabled
// until ReserveBondFunds is called, even with a future value of 0. A wallet
// that is not enforcing reserves, but which has unspent bonds should use this
// method to facilitate switching to the wallet for bonds in future.
func (btc *baseWallet) RegisterUnspent(inBonds uint64) {
	btc.reservesMtx.Lock()
	defer btc.reservesMtx.Unlock()
	btc.log.Tracef("RegisterUnspent(%v) changing unspent in bonds: %v => %v",
		toBTC(inBonds), toBTC(btc.bondReservesUsed), toBTC(btc.bondReservesUsed+inBonds))
	btc.bondReservesUsed += inBonds
	// This method should be called before ReserveBondFunds, prior to login on
	// application initialization (if there are existing for this asset bonds).
	// The nominal counter is not modified until ReserveBondFunds is called.
	if btc.bondReservesNominal != 0 {
		btc.log.Warnf("BUG: RegisterUnspent called with existing nominal reserves of %v BTC",
			toBTC(btc.bondReservesNominal))
	}
}

// ReserveBondFunds increases the bond reserves to accommodate a certain nominal
// amount of future bonds, or reduces the amount if a negative value is
// provided. If indicated, updating the reserves will require sufficient
// available balance, otherwise reserves will be adjusted regardless and the
// funds are pre-reserved. This returns false if the available balance was
// insufficient iff the caller requested it be respected, otherwise it always
// returns true (success). The fee buffer is used only when enabling the
// reserves (when starting from zero) to compute the fee buffer. It may also be
// zero, in which case the wallet will attempt to obtain it's own estimate.
//
// The reserves enabled with this method are enforced when funding transactions
// (e.g. regular withdraws/sends or funding orders), and deducted from available
// balance. Amounts may be reserved beyond the available balance, but only the
// amount that is offset by the available balance is reflected in the locked
// balance category. Like funds locked in swap contracts, the caller must
// supplement balance reporting with known bond amounts. However, via
// RegisterUnspent, the wallet is made aware of pre-existing unspent bond
// amounts (cumulative) that will eventually be spent with RefundBond.
//
// If this wallet is enforcing reserves (this method has been called, even with
// a future value of zero), when new bonds are created the nominal bond amount
// is deducted from the enforced reserves; when bonds are spent with RefundBond,
// the nominal bond amount is added back into the enforced reserves. That is,
// when there are no active bonds, the locked balance category will reflect the
// entire amount requested with ReserveBondFunds (plus a fee buffer, see below),
// and when bonds are created with MakeBondTx, the locked amount decreases since
// that portion of the reserves are now held in inaccessible UTXOs, the amounts
// of which the caller tracks independently. When spent with RefundBond, that
// same *nominal* bond value is added back to the enforced reserves amount.
//
// The amounts requested for bond reserves should be the nominal amounts of the
// bonds, but the reserved amount reflected in the locked balance category will
// include a considerable buffer for transaction fees. Therefore when the full
// amount of the reserves are presently locked in unspent bonds, the locked
// balance will include this fee buffer while the wallet is enforcing reserves.
//
// Until this method is called, reserves enforcement is disabled, and any
// unspent bonds registered with RegisterUnspent do not go into the enforced
// reserves when spent. In this way, all Bonder wallets remain aware of the
// total nominal value of unspent bonds even if the wallet is not presently
// being used to maintain a target bonding amount that necessitates reserves
// enforcement.
//
// A negative value may be provided to reduce allocated reserves. When the
// amount is reduced by the same amount it was previously increased by both
// ReserveBondFunds and RegisterUnspent, reserves enforcement including fee
// padding is disabled. Consider the following example: on startup, .2 BTC of
// existing unspent bonds are registered via RegisterUnspent, then on login and
// auth with the relevant DEX host, .4 BTC of future bond reserves are requested
// with ReserveBondFunds to maintain a configured target tier given the current
// tier and amounts of the existing unspent bonds. To disable reserves, the
// client would call ReserveBondFunds with -.6 BTC, which the wallet's internal
// accounting recognizes as complete removal of the reserves.
func (btc *baseWallet) ReserveBondFunds(future int64, feeBuffer uint64, respectBalance bool) bool {
	btc.reservesMtx.Lock()
	defer btc.reservesMtx.Unlock()

	defer func(enforced0, used0, nominal0 int64) {
		btc.log.Tracef("ReserveBondFunds(%v, %v): enforced = %v / bonded = %v / nominal = %v "+
			" ==>  enforced = %v / bonded = %v / nominal = %v",
			toBTC(future), respectBalance,
			toBTC(enforced0), toBTC(used0), toBTC(nominal0),
			toBTC(btc.bondReservesEnforced), toBTC(btc.bondReservesUsed), toBTC(uint64(btc.bondReservesNominal)))
	}(btc.bondReservesEnforced, int64(btc.bondReservesUsed), btc.bondReservesNominal)

	enforcedDelta := future

	// For the reserves initialization, add the fee buffer.
	if btc.bondReservesNominal == 0 { // enabling, add a fee buffer
		if feeBuffer == 0 {
			feeRate := 2 * btc.targetFeeRateWithFallback(1, 0)
			feeBuffer = bondsFeeBuffer(btc.segwit, feeRate)
		}
		enforcedDelta += int64(feeBuffer)
	}

	// How much of that is covered by the available balance, when increasing
	// reserves.
	if respectBalance && future > 0 {
		bal, err := btc.balance()
		if err != nil {
			btc.log.Errorf("Failed to retrieve balance: %v")
			return false
		}
		if int64(bal.Available) < btc.bondReservesEnforced+enforcedDelta {
			return false
		}
	}

	if btc.bondReservesNominal == 0 { // enabling, add any previously-registered unspent
		btc.log.Debugf("Re-enabling reserves with %v in existing unspent bonds (added to nominal).", toBTC(btc.bondReservesUsed))
		btc.bondReservesNominal += int64(btc.bondReservesUsed)
	}
	btc.bondReservesNominal += future
	btc.bondReservesEnforced += enforcedDelta

	// When disabling/zeroing reserves, wipe the fee buffer too. If there are
	// unspent bonds, this will be done in bondSpent when the last one is spent.
	if btc.bondReservesNominal <= 0 { // nominal should not go negative though
		btc.log.Infof("Nominal reserves depleted -- clearing enforced reserves!")
		btc.bondReservesEnforced = 0
		btc.bondReservesNominal = 0
	}

	return true
}

// MakeBondTx creates a time-locked fidelity bond transaction. The V0
// transaction has two required outputs:
//
// Output 0 is a the time-locked bond output of type P2SH with the provided
// value. The redeem script looks similar to the refund path of an atomic swap
// script, but with a pubkey hash:
//
//	<locktime> OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 <pubkeyhash[20]> OP_EQUALVERIFY OP_CHECKSIG
//
// The pubkey referenced by the script is provided by the caller.
//
// Output 1 is a DEX Account commitment. This is an OP_RETURN output that
// references the provided account ID.
//
//	OP_RETURN <2-byte version> <32-byte account ID> <4-byte locktime> <20-byte pubkey hash>
//
// Having the account ID in the raw allows the txn alone to identify the account
// without the bond output's redeem script.
//
// Output 2 is change, if any.
//
// The bond output's redeem script, which is needed to spend the bond output, is
// returned as the Data field of the Bond. The bond output pays to a pubkeyhash
// script for a wallet address. Bond.RedeemTx is a backup transaction that
// spends the bond output after lockTime passes, paying to an address for the
// current underlying wallet; the bond private key should normally be used to
// author a new transaction paying to a new address instead.
func (btc *baseWallet) MakeBondTx(ver uint16, amt, feeRate uint64, lockTime time.Time, bondKey *secp256k1.PrivateKey, acctID []byte) (*asset.Bond, func(), error) {
	if ver != 0 {
		return nil, nil, errors.New("only version 0 bonds supported")
	}
	if until := time.Until(lockTime); until >= 365*12*time.Hour /* ~6 months */ {
		return nil, nil, fmt.Errorf("that lock time is nuts: %v", lockTime)
	} else if until < 0 {
		return nil, nil, fmt.Errorf("that lock time is already passed: %v", lockTime)
	}

	pk := bondKey.PubKey().SerializeCompressed()
	pkh := btcutil.Hash160(pk)

	feeRate = btc.feeRateWithFallback(feeRate)
	baseTx := wire.NewMsgTx(btc.txVersion())

	// TL output.
	lockTimeSec := lockTime.Unix()
	if lockTimeSec >= dexbtc.MaxCLTVScriptNum || lockTimeSec <= 0 {
		return nil, nil, fmt.Errorf("invalid lock time %v", lockTime)
	}
	bondScript, err := dexbtc.MakeBondScript(ver, uint32(lockTimeSec), pkh)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build bond output redeem script: %w", err)
	}
	pkScript, err := btc.scriptHashScript(bondScript)
	if err != nil {
		return nil, nil, fmt.Errorf("error constructing p2sh script: %v", err)
	}
	txOut := wire.NewTxOut(int64(amt), pkScript)
	if dexbtc.IsDust(txOut, feeRate) {
		return nil, nil, fmt.Errorf("bond output value of %d is dust", amt)
	}
	baseTx.AddTxOut(txOut)

	// Acct ID commitment and bond details output, v0. The integers are encoded
	// with big-endian byte order and a fixed number of bytes, unlike in Script,
	// for natural visual inspection of the version and lock time.
	pushData := make([]byte, 2+len(acctID)+4+20)
	var offset int
	binary.BigEndian.PutUint16(pushData[offset:], ver)
	offset += 2
	copy(pushData[offset:], acctID[:])
	offset += len(acctID)
	binary.BigEndian.PutUint32(pushData[offset:], uint32(lockTimeSec))
	offset += 4
	copy(pushData[offset:], pkh)
	commitPkScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_RETURN).
		AddData(pushData).
		Script()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build acct commit output script: %w", err)
	}
	acctOut := wire.NewTxOut(0, commitPkScript) // value zero
	baseTx.AddTxOut(acctOut)

	baseSize := uint32(baseTx.SerializeSize())
	if btc.segwit {
		baseSize += dexbtc.P2WPKHOutputSize
	} else {
		baseSize += dexbtc.P2PKHOutputSize
	}

	coins, _, _, _, _, _, err := btc.fund(0, 0, true, sendEnough(amt, feeRate, true, uint64(baseSize), btc.segwit, true))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fund bond tx: %w", err)
	}

	// Reduce the reserves counter now that utxos are explicitly allocated. When
	// the bond is refunded and we pay back into our wallet, we will increase
	// the reserves counter.
	newReserves, unspent := btc.bondLocked(amt) // nominal, not spent amount
	btc.log.Debugf("New bond reserves (new post) = %f BTC with %f in unspent bonds",
		toBTC(newReserves), toBTC(unspent)) // decrement and report new

	abandon := func() { // if caller does not broadcast, or we fail in this method
		newReserves, unspent = btc.bondSpent(amt)
		btc.log.Debugf("New bond reserves (abandoned post) = %f BTC with %f in unspent bonds",
			toBTC(newReserves), toBTC(unspent)) // increment/restore and report new
		err := btc.ReturnCoins(coins)
		if err != nil {
			btc.log.Errorf("error returning coins for unused bond tx: %v", coins)
		}
	}

	var success bool
	defer func() {
		if !success {
			abandon()
		}
	}()

	totalIn, _, err := btc.addInputsToTx(baseTx, coins)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add inputs to bond tx: %w", err)
	}

	changeAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %w", err)
	}
	signedTx, _, _, err := btc.signTxAndAddChange(baseTx, changeAddr, totalIn, amt, feeRate)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign bond tx: %w", err)
	}

	txid := btc.hashTx(signedTx)

	signedTxBytes, err := btc.serializeTx(signedTx)
	if err != nil {
		return nil, nil, err
	}
	unsignedTxBytes, err := btc.serializeTx(baseTx)
	if err != nil {
		return nil, nil, err
	}

	// Prep the redeem / refund tx.
	redeemMsgTx, err := btc.makeBondRefundTxV0(txid, 0, amt, bondScript, bondKey, feeRate)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create bond redemption tx: %w", err)
	}
	redeemTx, err := btc.serializeTx(redeemMsgTx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize bond redemption tx: %w", err)
	}

	bond := &asset.Bond{
		Version:    ver,
		AssetID:    btc.cloneParams.AssetID,
		Amount:     amt,
		CoinID:     toCoinID(txid, 0),
		Data:       bondScript,
		SignedTx:   signedTxBytes,
		UnsignedTx: unsignedTxBytes,
		RedeemTx:   redeemTx,
	}
	success = true

	return bond, abandon, nil
}

func (btc *baseWallet) makeBondRefundTxV0(txid *chainhash.Hash, vout uint32, amt uint64,
	script []byte, priv *secp256k1.PrivateKey, feeRate uint64) (*wire.MsgTx, error) {
	lockTime, pkhPush, err := dexbtc.ExtractBondDetailsV0(0, script)
	if err != nil {
		return nil, err
	}

	pk := priv.PubKey().SerializeCompressed()
	pkh := btcutil.Hash160(pk)
	if !bytes.Equal(pkh, pkhPush) {
		return nil, fmt.Errorf("incorrect private key to spend the bond output")
	}

	msgTx := wire.NewMsgTx(btc.txVersion())
	// Transaction LockTime must be <= spend time, and >= the CLTV lockTime, so
	// we use exactly the CLTV's value. This limits the CLTV value to 32-bits.
	msgTx.LockTime = lockTime
	bondPrevOut := wire.NewOutPoint(txid, vout)
	txIn := wire.NewTxIn(bondPrevOut, []byte{}, nil)
	txIn.Sequence = wire.MaxTxInSequenceNum - 1 // not finalized, do not disable cltv
	msgTx.AddTxIn(txIn)

	// Calculate fees and add the refund output.
	size := btc.calcTxSize(msgTx)
	if btc.segwit {
		witnessVBytes := (dexbtc.RedeemBondSigScriptSize + 2 + 3) / 4
		size += uint64(witnessVBytes) + dexbtc.P2WPKHOutputSize
	} else {
		size += dexbtc.RedeemBondSigScriptSize + dexbtc.P2PKHOutputSize
	}
	fee := feeRate * size
	if fee > amt {
		return nil, fmt.Errorf("irredeemable bond at fee rate %d atoms/byte", feeRate)
	}

	// Add the refund output.
	redeemAddr, err := btc.node.changeAddress()
	if err != nil {
		return nil, fmt.Errorf("error creating change address: %w", err)
	}
	redeemPkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating pubkey script: %w", err)
	}
	redeemTxOut := wire.NewTxOut(int64(amt-fee), redeemPkScript)
	if dexbtc.IsDust(redeemTxOut, feeRate) { // hard to imagine
		return nil, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(redeemTxOut)

	if btc.segwit {
		sigHashes := txscript.NewTxSigHashes(msgTx, new(txscript.CannedPrevOutputFetcher))
		sig, err := txscript.RawTxInWitnessSignature(msgTx, sigHashes, 0, int64(amt),
			script, txscript.SigHashAll, priv)
		if err != nil {
			return nil, err
		}
		txIn.Witness = dexbtc.RefundBondScriptSegwit(script, sig, pk)
	} else {
		prevPkScript, err := btc.scriptHashScript(script) // P2SH: OP_HASH160 <script hash> OP_EQUAL
		if err != nil {
			return nil, fmt.Errorf("error constructing p2sh script: %w", err)
		}
		sig, err := btc.signNonSegwit(msgTx, 0, script, txscript.SigHashAll, priv, []int64{int64(amt)}, [][]byte{prevPkScript})
		if err != nil {
			return nil, err
		}
		txIn.SignatureScript, err = dexbtc.RefundBondScript(script, sig, pk)
		if err != nil {
			return nil, fmt.Errorf("RefundBondScript: %w", err)
		}
	}

	return msgTx, nil
}

// RefundBond refunds a bond output to a new wallet address given the redeem
// script and private key. After broadcasting, the output paying to the wallet
// is returned.
func (btc *baseWallet) RefundBond(ctx context.Context, ver uint16, coinID, script []byte, amt uint64, privKey *secp256k1.PrivateKey) (asset.Coin, error) {
	if ver != 0 {
		return nil, errors.New("only version 0 bonds supported")
	}
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	feeRate := btc.targetFeeRateWithFallback(2, 0)

	msgTx, err := btc.makeBondRefundTxV0(txHash, vout, amt, script, privKey, feeRate)
	if err != nil {
		return nil, err
	}

	newReserves, unspent := btc.bondSpent(amt)
	btc.log.Debugf("New bond reserves (new refund of %f BTC) = %f BTC with %f in unspent bonds",
		toBTC(amt), toBTC(newReserves), toBTC(unspent))

	_, err = btc.node.sendRawTransaction(msgTx)
	if err != nil {
		newReserves, unspent = btc.bondLocked(amt) // assume it didn't really send :/
		btc.log.Debugf("New bond reserves (failed refund broadcast) = %f BTC with %f in unspent bonds",
			toBTC(newReserves), toBTC(unspent)) // increment/restore and report new
		return nil, fmt.Errorf("error sending refund bond transaction: %w", err)
	}

	return newOutput(txHash, 0, uint64(msgTx.TxOut[0].Value)), nil
}

// BondsFeeBuffer suggests how much extra may be required for the transaction
// fees part of required bond reserves when bond rotation is enabled. The
// provided fee rate may be zero, in which case the wallet will use it's own
// estimate or fallback value.
func (btc *baseWallet) BondsFeeBuffer(feeRate uint64) uint64 {
	if feeRate == 0 {
		feeRate = btc.targetFeeRateWithFallback(1, 0)
	}
	feeRate *= 2 // double the current fee rate estimate so this fee buffer does not get stale too quickly
	return bondsFeeBuffer(btc.segwit, feeRate)
}

// FundMultiOrder funds multiple orders in one shot. MaxLock is the maximum
// amount that the wallet can lock for these orders. If maxLock == 0, then
// there is no limit. An error is returned if the wallet does not have enough
// available balance to fund each of the orders, however, if splitting is
// not enabled and all of the orders cannot be funded due to mismatches in
// UTXO sizes, the orders that can be funded are funded. It will fail on the
// first order that cannot be funded. The returned values will always be in
// the same order as the Values in the parameter. If the length of the returned
// orders is shorter than what was passed in, it means that the orders at the
// end of the list were unable to be funded.
func (btc *baseWallet) FundMultiOrder(mo *asset.MultiOrder, maxLock uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	btc.log.Debugf("Attempting to fund a multi-order for %s, maxFeeRate = %d", btc.symbol, mo.MaxFeeRate)

	var totalRequiredForOrders uint64
	var swapInputSize uint64
	if btc.segwit {
		swapInputSize = dexbtc.RedeemP2WPKHInputTotalSize
	} else {
		swapInputSize = dexbtc.RedeemP2PKHInputSize
	}
	for _, value := range mo.Values {
		if value.Value == 0 {
			return nil, nil, 0, fmt.Errorf("cannot fund value = 0")
		}
		if value.MaxSwapCount == 0 {
			return nil, nil, 0, fmt.Errorf("cannot fund zero-lot order")
		}
		req := calc.RequiredOrderFundsAlt(value.Value, swapInputSize, value.MaxSwapCount,
			btc.initTxSizeBase, btc.initTxSize, mo.MaxFeeRate)
		totalRequiredForOrders += req
	}

	if maxLock < totalRequiredForOrders && maxLock != 0 {
		return nil, nil, 0, fmt.Errorf("maxLock < totalRequiredForOrders (%d < %d)", maxLock, totalRequiredForOrders)
	}

	if mo.FeeSuggestion > mo.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf("fee suggestion %d > max fee rate %d", mo.FeeSuggestion, mo.MaxFeeRate)
	}
	// Check wallets fee rate limit against server's max fee rate
	if btc.feeRateLimit() < mo.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configued fee rate limit %v",
			dex.BipIDSymbol(BipID), mo.MaxFeeRate, btc.feeRateLimit())
	}

	bal, err := btc.Balance()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting wallet balance: %w", err)
	}
	if bal.Available < totalRequiredForOrders {
		return nil, nil, 0, fmt.Errorf("insufficient funds. %d < %d",
			bal.Available, totalRequiredForOrders)
	}

	customCfg, err := decodeFundMultiOptions(mo.Options)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error decoding options: %w", err)
	}

	var useSplit bool
	var splitBuffer uint64
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}
	if useSplit && customCfg.SplitBuffer != nil {
		splitBuffer = *customCfg.SplitBuffer
	}

	return btc.fundMulti(maxLock, mo.Values, mo.FeeSuggestion, mo.MaxFeeRate, useSplit, splitBuffer)
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

// spendableUTXOs filters the RPC utxos for those that are spendable with
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
	var relock []*output
	var i int
	for _, utxo := range utxos {
		// Guard against inconsistencies between the wallet's view of
		// spendable unlocked UTXOs and ExchangeWallet's. e.g. User manually
		// unlocked something or even restarted the wallet software.
		pt := newOutPoint(utxo.txHash, utxo.vout)
		if btc.fundingCoins[pt] != nil {
			btc.log.Warnf("Known order-funding coin %s returned by listunspent!", pt)
			delete(utxoMap, pt)
			relock = append(relock, &output{pt, utxo.amount})
		} else { // in-place filter maintaining order
			utxos[i] = utxo
			i++
		}
	}
	if len(relock) > 0 {
		if err = btc.node.lockUnspent(false, relock); err != nil {
			btc.log.Errorf("Failed to re-lock funding coins with wallet: %v", err)
		}
	}
	utxos = utxos[:i]
	return utxos, utxoMap, sum, nil
}

func convertUnspent(confs uint32, unspents []*ListUnspentResult, chainParams *chaincfg.Params) ([]*compositeUTXO, map[outPoint]*compositeUTXO, uint64, error) {
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	var sum uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	utxoMap := make(map[outPoint]*compositeUTXO, len(unspents))
	for _, txout := range unspents {
		if txout.Confirmations >= confs && txout.Safe() && txout.Spendable {
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %w", err)
			}

			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, chainParams)
			if err != nil {
				if errors.Is(err, dex.UnsupportedScriptError) {
					continue
				}
				return nil, nil, 0, fmt.Errorf("error reading asset info: %w", err)
			}
			if nfo.ScriptType == dexbtc.ScriptUnsupported || nfo.NonStandardScript {
				// InputInfo sets NonStandardScript for P2SH with non-standard
				// redeem scripts. Don't return these since they cannot fund
				// arbitrary txns.
				continue
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
		txOut, err := btc.txOutFromTxBytes(tx.Hex, rpcOP.Vout)
		if err != nil {
			return 0, err
		}
		sum += uint64(txOut.Value)
	}
	return sum, nil
}

// wireBytes dumps the serialized transaction bytes.
func (btc *baseWallet) wireBytes(tx *wire.MsgTx) []byte {
	b, err := btc.serializeTx(tx)
	// wireBytes is just used for logging, and a serialization error is
	// extremely unlikely, so just log the error and return the nil bytes.
	if err != nil {
		btc.log.Errorf("error serializing %s transaction: %v", btc.symbol, err)
		return nil
	}
	return b
}

// GetBestBlockHeight is exported for use by clone wallets. Not part of the
// asset.Wallet interface.
func (btc *baseWallet) GetBestBlockHeight() (int32, error) {
	return btc.node.getBestBlockHeight()
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
	PreviousBlockHash string `json:"previousblockhash"`
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

// CallRPC is a method for making RPC calls directly on an underlying RPC
// client. CallRPC is not part of the wallet interface. Its intended use is for
// clone wallets to implement custom functionality.
func (btc *baseWallet) CallRPC(method string, args []interface{}, thing interface{}) error {
	rpcCl, is := btc.node.(*rpcClient)
	if !is {
		return errors.New("wallet is not RPC")
	}
	return rpcCl.call(method, args, thing)
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

// toBTC returns a float representation in conventional units for the sats.
func toBTC[V uint64 | int64](v V) float64 {
	return btcutil.Amount(v).ToBTC()
}

// rawTxInSig signs the transaction in input using the standard bitcoin
// signature hash and ECDSA algorithm.
func rawTxInSig(tx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, _ []int64, _ [][]byte) ([]byte, error) {

	return txscript.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, key)
}

// findRedemptionsInTx searches the MsgTx for the redemptions for the specified
// swaps.
func findRedemptionsInTx(ctx context.Context, segwit bool, reqs map[outPoint]*findRedemptionReq, msgTx *wire.MsgTx,
	chainParams *chaincfg.Params) (discovered map[outPoint]*findRedemptionResult) {

	return findRedemptionsInTxWithHasher(ctx, segwit, reqs, msgTx, chainParams, hashTx)
}

func findRedemptionsInTxWithHasher(ctx context.Context, segwit bool, reqs map[outPoint]*findRedemptionReq, msgTx *wire.MsgTx,
	chainParams *chaincfg.Params, hashTx func(*wire.MsgTx) *chainhash.Hash) (discovered map[outPoint]*findRedemptionResult) {

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
				txHash := hashTx(msgTx)
				secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript, req.contractHash[:], segwit, chainParams)
				if err != nil {
					req.fail("no secret extracted from redemption input %s:%d for swap output %s: %v",
						txHash, vin, outPt, err)
					continue
				}
				discovered[outPt] = &findRedemptionResult{
					redemptionCoinID: toCoinID(txHash, uint32(vin)),
					secret:           secret,
				}
			}
		}
	}
	return
}

// prettyBTC prints a value as a float with up to 8 digits of precision, but
// with trailing zeros and decimal points removed.
func prettyBTC(v uint64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(float64(v)/1e8, 'f', 8, 64), "0"), ".")
}

// calcBumpedRate calculated a bump on the baseRate. If bump is nil, the
// baseRate is returned directly. In the case of an error (nil or out-of-range),
// the baseRate is returned unchanged.
func calcBumpedRate(baseRate uint64, bump *float64) (uint64, error) {
	if bump == nil {
		return baseRate, nil
	}
	userBump := *bump
	if userBump > 2.0 {
		return baseRate, fmt.Errorf("fee bump %f is higher than the 2.0 limit", userBump)
	}
	if userBump < 1.0 {
		return baseRate, fmt.Errorf("fee bump %f is lower than 1", userBump)
	}
	return uint64(math.Round(float64(baseRate) * userBump)), nil
}

func float64PtrStr(v *float64) string {
	if v == nil {
		return "nil"
	}
	return strconv.FormatFloat(*v, 'f', 8, 64)
}

func hashTx(tx *wire.MsgTx) *chainhash.Hash {
	h := tx.TxHash()
	return &h
}

func stringifyAddress(addr btcutil.Address, _ *chaincfg.Params) (string, error) {
	return addr.String(), nil
}

func deserializeBlock(b []byte) (*wire.MsgBlock, error) {
	msgBlock := &wire.MsgBlock{}
	return msgBlock, msgBlock.Deserialize(bytes.NewReader(b))
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

// deserializeMsgTx creates a wire.MsgTx by deserializing data from the Reader.
func deserializeMsgTx(r io.Reader) (*wire.MsgTx, error) {
	msgTx := new(wire.MsgTx)
	err := msgTx.Deserialize(r)
	if err != nil {
		return nil, err
	}
	return msgTx, nil
}

// msgTxFromBytes creates a wire.MsgTx by deserializing the transaction.
func msgTxFromBytes(txB []byte) (*wire.MsgTx, error) {
	return deserializeMsgTx(bytes.NewReader(txB))
}

// ConfirmRedemption returns how many confirmations a redemption has. Normally
// this is very straightforward. However, with fluxuating fees, there's the
// possibility that the tx is never mined and eventually purged from the
// mempool. In that case we use the provided fee suggestion to create and send
// a new redeem transaction, returning the new transactions hash.
func (btc *baseWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, feeSuggestion uint64) (*asset.ConfirmRedemptionStatus, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	_, confs, err := btc.rawWalletTx(txHash)
	// redemption transaction found, return its confirms.
	//
	// TODO: Investigate the case where this redeem has been sitting in the
	// mempool for a long amount of time, possibly requiring some action by
	// us to get it unstuck.
	if err == nil {
		return &asset.ConfirmRedemptionStatus{
			Confs:  uint64(confs),
			Req:    requiredRedeemConfirms,
			CoinID: coinID,
		}, nil
	}

	if !errors.Is(err, WalletTransactionNotFound) {
		return nil, fmt.Errorf("problem searching for redemption transaction %s: %w", txHash, err)
	}

	// Redemption transaction is missing from the point of view of our node!
	// Unlikely, but possible it was redeemed by another transaction. Check
	// if the contract is still an unspent output.

	pkScript, err := btc.scriptHashScript(redemption.Spends.Contract)
	if err != nil {
		return nil, fmt.Errorf("error creating contract script: %w", err)
	}

	swapHash, vout, err := decodeCoinID(redemption.Spends.Coin.ID())
	if err != nil {
		return nil, err
	}

	utxo, _, err := btc.node.getTxOut(swapHash, vout, pkScript, time.Now().Add(-ContractSearchLimit))
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract %s with swap hash %v vout %d: %w", redemption.Spends.Coin.ID(), swapHash, vout, err)
	}
	if utxo == nil {
		// TODO: Spent, but by who. Find the spending tx.
		btc.log.Warnf("Contract coin %v with swap hash %v vout %d spent by someone but not sure who.", redemption.Spends.Coin.ID(), swapHash, vout)
		// Incorrect, but we will be in a loop of erroring if we don't
		// return something.
		return &asset.ConfirmRedemptionStatus{
			Confs:  requiredRedeemConfirms,
			Req:    requiredRedeemConfirms,
			CoinID: coinID,
		}, nil
	}

	// The contract has not yet been redeemed, but it seems the redeeming
	// tx has disappeared. Assume the fee was too low at the time and it
	// was eventually purged from the mempool. Attempt to redeem again with
	// a currently reasonable fee.

	form := &asset.RedeemForm{
		Redemptions:   []*asset.Redemption{redemption},
		FeeSuggestion: feeSuggestion,
	}
	_, coin, _, err := btc.Redeem(form)
	if err != nil {
		return nil, fmt.Errorf("unable to re-redeem %s with swap hash %v vout %d: %w", redemption.Spends.Coin.ID(), swapHash, vout, err)
	}
	return &asset.ConfirmRedemptionStatus{
		Confs:  0,
		Req:    requiredRedeemConfirms,
		CoinID: coin.ID(),
	}, nil
}

// writeRecycledAddrsToFile writes the recycled address cache to file.
func (btc *baseWallet) writeRecycledAddrsToFile() {
	btc.recycledAddrMtx.Lock()
	addrs := make([]string, 0, len(btc.recycledAddrs))
	for addr := range btc.recycledAddrs {
		addrs = append(addrs, addr)
	}
	btc.recycledAddrMtx.Unlock()
	contents := []byte(strings.Join(addrs, "\n"))
	if err := os.WriteFile(btc.recyclePath, contents, 0600); err != nil {
		btc.log.Errorf("Error writing recycled address file: %v", err)
	}
}
