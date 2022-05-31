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
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
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
		dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + dexbtc.SegwitMarkerAndFlagWeight + 3) / 4)

	walletTypeLegacy = ""
	walletTypeRPC    = "bitcoindRPC"
	walletTypeSPV    = "SPV"

	swapFeeBumpKey   = "swapfeebump"
	splitKey         = "swapsplit"
	redeemFeeBumpFee = "redeemfeebump"
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

	// 02 Jun 21 21:12 CDT
	defaultWalletBirthdayUnix = 1622668320
	defaultWalletBirthday     = time.Unix(int64(defaultWalletBirthdayUnix), 0)

	spvOpts = []*asset.ConfigOption{{
		Key:         "walletbirthday",
		DisplayName: "Wallet Birthday",
		Description: "This is the date the wallet starts scanning the blockchain " +
			"for transactions related to this wallet. If reconfiguring an existing " +
			"wallet, this may start a rescan if the new birthday is older. This " +
			"option is disabled if there are currently active BTC trades.",
		DefaultValue: defaultWalletBirthdayUnix,
		MaxValue:     "now",
		// This MinValue must be removed if we start supporting importing private keys
		MinValue:          defaultWalletBirthdayUnix,
		IsDate:            true,
		DisableWhenActive: true,
		IsBirthdayConfig:  true,
	}}

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
		ConfigOpts:  append(spvOpts, commonOpts...),
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
)

// TxInSigner is a transaction input signer. In addition to the standard Bitcoin
// arguments, TxInSigner receives all values and pubkey scripts for previous
// outpoints spent in this transaction.
type TxInSigner func(tx *wire.MsgTx, idx int, subScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, vals []int64, prevScripts [][]byte) ([]byte, error)

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
	// ZECStyleBalance is for clones that don't support getbalances or
	// walletinfo, and don't take an account name argument.
	ZECStyleBalance bool
	// If segwit is false, legacy addresses and contracts will be used. This
	// setting must match the configuration of the server's asset backend.
	Segwit bool
	// LegacyRawFeeLimit can be true if the RPC only supports the boolean
	// allowHighFees argument to the sendrawtransaction RPC.
	LegacyRawFeeLimit bool
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
	// FeeEstimator provides a way to get fees given an RawRequest-enabled
	// client and a confirmation target.
	FeeEstimator func(RawRequester, uint64) (uint64, error)
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
	// getwalletinfo in order to discover ownership of an address.
	LegacyValidateAddressRPC bool
	// SingularWallet signals that the node software supports only one wallet,
	// so the RPC endpoint does not have a /wallet/{walletname} path.
	SingularWallet bool
	// UnlockSpends manually unlocks outputs as they are spent. Most asses will
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
	ActivelyUsed     bool    `ini:"special:activelyUsed"` //injected by core
	WalletName       string  `ini:"walletname"`           // RPC
	Birthday         uint64  `ini:"walletbirthday"`       // SPV
}

// adjustedBirthday converts WalletConfig.Birthday to a time.Time, and adjusts
// it so that defaultWalletBirthday <= WalletConfig.Bithday <= now.
func (cfg *WalletConfig) adjustedBirthday() time.Time {
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
func parseRPCWalletConfig(settings map[string]string, symbol string, net dex.Network,
	ports dexbtc.NetPorts, singularWallet bool) (*RPCWalletConfig, *rpcclient.Client, error) {

	cfg, err := readRPCWalletConfig(settings, symbol, net, ports)
	if err != nil {
		return nil, nil, err
	}
	endpoint := cfg.RPCBind
	if !singularWallet {
		endpoint += "/wallet/" + cfg.WalletName
	}

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

	walletCfg := new(WalletConfig)
	err = config.Unmapify(params.Settings, walletCfg)
	if err != nil {
		return err
	}

	recoveryCfg := new(RecoveryCfg)
	err = config.Unmapify(params.Settings, recoveryCfg)
	if err != nil {
		return err
	}

	return createSPVWallet(params.Pass, params.Seed, walletCfg.adjustedBirthday(), params.DataDir,
		params.Logger, recoveryCfg.NumExternalAddresses, recoveryCfg.NumInternalAddresses, chainParams)
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

// redeemOptions are order options that apply to redemptions.
type redeemOptions struct {
	FeeBump *float64 `ini:"redeemfeebump"`
}

func init() {
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
	dustLimit         uint64
	redeemConfTarget  uint64
	useSplitTx        bool
	useLegacyBalance  bool
	zecStyleBalance   bool
	segwit            bool
	signNonSegwit     TxInSigner
	estimateFee       func(RawRequester, uint64) (uint64, error) // TODO: resolve the awkwardness of an RPC-oriented func in a generic framework
	decodeAddr        dexbtc.AddressDecoder
	deserializeTx     func([]byte) (*wire.MsgTx, error)
	serializeTx       func(*wire.MsgTx) ([]byte, error)
	calcTxSize        func(*wire.MsgTx) uint64
	hashTx            func(*wire.MsgTx) *chainhash.Hash
	stringAddr        dexbtc.AddressStringer
	txVersion         func() int32

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

// ExchangeWalletAccelerator implements the Accelerator interface on an
// ExchangeWalletFullNode.
type ExchangeWalletAccelerator struct {
	*ExchangeWalletFullNode
}

// Check that wallets satisfy their supported interfaces.
var _ asset.Wallet = (*baseWallet)(nil)
var _ asset.Accelerator = (*ExchangeWalletAccelerator)(nil)
var _ asset.Accelerator = (*ExchangeWalletSPV)(nil)
var _ asset.Rescanner = (*ExchangeWalletSPV)(nil)
var _ asset.FeeRater = (*ExchangeWalletFullNode)(nil)
var _ asset.LogFiler = (*ExchangeWalletSPV)(nil)
var _ asset.Recoverer = (*ExchangeWalletSPV)(nil)

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
	w := btc.node.(*spvWallet)

	internal, external, err := w.numDerivedAddresses()
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
	w := btc.node.(*spvWallet)
	err := w.moveWalletData(backupDir)
	if err != nil {
		return fmt.Errorf("unable to move wallet data: %w", err)
	}

	return nil
}

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
	rate, err := btc.estimateFee(btc.node, 1)
	if err != nil {
		btc.log.Tracef("Failed to get fee rate: %v", err)
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
		rpcWallet, err := BTCCloneWallet(cloneCFG)
		if err != nil {
			return nil, err
		}
		return &ExchangeWalletAccelerator{rpcWallet}, nil
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
	}
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *BTCCloneCFG) (*ExchangeWalletFullNode, error) {
	clientCfg, client, err := parseRPCWalletConfig(cfg.WalletCFG.Settings, cfg.Symbol, cfg.Network, cfg.Ports, cfg.SingularWallet)
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

// newRPCWallet creates the ExchangeWallet and starts the block monitor.
func newRPCWallet(requester RawRequesterWithContext, cfg *BTCCloneCFG, walletConfig *WalletConfig) (*ExchangeWalletFullNode, error) {
	btc, err := newUnconnectedWallet(cfg, walletConfig)
	if err != nil {
		return nil, err
	}

	blockDeserializer := cfg.BlockDeserializer
	if blockDeserializer == nil {
		blockDeserializer = deserializeBlock
	}

	btc.node = newRPCClient(&rpcCore{
		requester:                requester,
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
	})
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
	feesLimitPerByte := cfg.DefaultFeeRateLimit
	if feesLimitPerByte == 0 {
		feesLimitPerByte = defaultFeeRateLimit
	}
	if walletCfg.FeeRateLimit > 0 {
		feesLimitPerByte = toSatoshi(walletCfg.FeeRateLimit / 1000)
		if feesLimitPerByte == 0 {
			return nil, fmt.Errorf("fee rate limit is smaller than smallest unit: %v",
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
		log:                 cfg.Logger,
		tipChange:           cfg.WalletCFG.TipChange,
		peersChange:         cfg.WalletCFG.PeersChange,
		fundingCoins:        make(map[outPoint]*utxo),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		minNetworkVersion:   cfg.MinNetworkVersion,
		fallbackFeeRate:     fallbackFeesPerByte,
		feeRateLimit:        feesLimitPerByte,
		dustLimit:           cfg.ConstantDustLimit,
		redeemConfTarget:    redeemConfTarget,
		useSplitTx:          walletCfg.UseSplitTx,
		useLegacyBalance:    cfg.LegacyBalance,
		zecStyleBalance:     cfg.ZECStyleBalance,
		segwit:              cfg.Segwit,
		signNonSegwit:       nonSegwitSigner,
		estimateFee:         cfg.FeeEstimator,
		decodeAddr:          addrDecoder,
		stringAddr:          addrStringer,
		walletInfo:          cfg.WalletInfo,
		deserializeTx:       txDeserializer,
		serializeTx:         txSerializer,
		hashTx:              txHasher,
		calcTxSize:          txSizeCalculator,
		txVersion:           txVersion,
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

	allowAutomaticRescan := !walletCfg.ActivelyUsed
	btc.node = loadSPVWallet(cfg.WalletCFG.DataDir, cfg.Logger.SubLogger("SPV"), cfg.ChainParams, walletCfg.adjustedBirthday(), allowAutomaticRescan)

	return &ExchangeWalletSPV{baseWallet: btc}, nil
}

// Info returns basic information about the wallet and asset.
func (btc *baseWallet) Info() *asset.WalletInfo {
	return btc.walletInfo
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
	Blocks        int64  `json:"blocks"`
	Headers       int64  `json:"headers"`
	BestBlockHash string `json:"bestblockhash"`
	// InitialBlockDownload will be true if the node is still in the initial
	// block download mode.
	InitialBlockDownload *bool `json:"initialblockdownload"`
	// InitialBlockDownloadComplete will be true if this node has completed its
	// initial block download and is expected to be synced to the network.
	// ZCash uses this terminology instead of initialblockdownload.
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
	addr, err := btc.decodeAddr(address, btc.chainParams)
	if err != nil {
		return false, err
	}
	return btc.node.ownsAddress(addr)
}

// Balance returns the total available funds in the wallet. Part of the
// asset.Wallet interface.
func (btc *baseWallet) Balance() (*asset.Balance, error) {
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
	}, nil
}

// legacyBalance is used for clones that are < node version 0.18 and so don't
// have 'getbalances'.
func (btc *baseWallet) legacyBalance() (*asset.Balance, error) {
	cl, ok := btc.node.(*rpcClient)
	if !ok {
		return nil, fmt.Errorf("legacyBalance unimplemented for spv clients")
	}

	if btc.zecStyleBalance {
		var bal uint64
		// args: "(dummy)" minconf includeWatchonly inZat
		if err := cl.call(methodGetBalance, anylist{"", 0, false, true}, &bal); err != nil {
			return nil, err
		}
		return &asset.Balance{Available: bal}, nil
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
	if err == nil && feeRate > 0 {
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
func (btc *baseWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	_, maxEst, err := btc.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.AssetConfig)
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
		est, _, _, err := btc.estimateSwap(lots, lotSize, feeSuggestion, utxos, nfo, btc.useSplitTx, 1.0)
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
	// cost since there are no more RPC calls. The utxo set is only used once
	// right now, but when order-time options are implemented, the utxos will be
	// used to calculate option availability and fees.
	utxos, maxEst, err := btc.maxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig)
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
	split := btc.useSplitTx
	if customCfg.Split != nil {
		split = *customCfg.Split
	}

	// Parse the configured fee bump.
	var bump float64 = 1.0
	if customCfg.FeeBump != nil {
		bump = *customCfg.FeeBump
		if bump > 2.0 {
			return nil, fmt.Errorf("fee bump %f is higher than the 2.0 limit", bump)
		}
		if bump < 1.0 {
			return nil, fmt.Errorf("fee bump %f is lower than 1", bump)
		}
	}

	// Get the estimate using the current configuration.
	est, splitUsed, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, bump)
	if err != nil {
		return nil, fmt.Errorf("estimation failed: %v", err)
	}

	var opts []*asset.OrderOption

	// If the used split isn't the requested split, the other split option was
	// unavailable, so there is no option to offer.
	if !req.Immediate && splitUsed == split {
		if splitOpt := btc.splitOption(req, utxos, bump); splitOpt != nil {
			opts = append(opts, splitOpt)
		}
	}

	// Figure out what our maximum available fee bump is, within our 2x hard
	// limit.
	var maxBump float64
	var maxBumpEst *asset.SwapEstimate
	for maxBump = 2.0; maxBump > 1.01; maxBump -= 0.1 {
		tryEst, splitUsed, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, maxBump)
		// If the split used wasn't the configured value, this option is not
		// available.
		if err == nil && split == splitUsed {
			maxBumpEst = tryEst
			break
		}
	}

	if maxBumpEst != nil {
		noBumpEst, _, _, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, 1.0)
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
		Estimate: est,
		Options:  opts,
	}, nil
}

// splitOption constructs an *asset.OrderOption with customized text based on the
// difference in fees between the configured and test split condition.
func (btc *baseWallet) splitOption(req *asset.PreSwapForm, utxos []*compositeUTXO, bump float64) *asset.OrderOption {
	noSplitEst, _, noSplitLocked, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, false, bump)
	if err != nil {
		btc.log.Errorf("estimateSwap (no split) error: %v", err)
		return nil
	}
	splitEst, splitUsed, splitLocked, err := btc.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, true, bump)
	if err != nil {
		btc.log.Errorf("estimateSwap (with split) error: %v", err)
		return nil
	}
	if !splitUsed {
		// unable to do the split. no option.
		btc.log.Debugf("split option unavailable")
		return nil
	}
	symbol := strings.ToUpper(btc.symbol)

	xtraFees := splitEst.RealisticWorstCase - noSplitEst.RealisticWorstCase
	pctChange := (float64(splitEst.RealisticWorstCase)/float64(noSplitEst.RealisticWorstCase) - 1) * 100
	overlock := noSplitLocked - splitLocked

	var reason string
	if pctChange > 1 {
		reason = fmt.Sprintf("+%d%% fees, -%s %s overlock", int(math.Round(pctChange)), prettyBTC(overlock), symbol)
	} else {
		reason = fmt.Sprintf("+%.1f%% fees, -%s %s overlock", pctChange, prettyBTC(overlock), symbol)
	}

	desc := fmt.Sprintf("Using a split transaction to prevent temporary overlock of %s %s, but for additional fees of %s %s",
		prettyBTC(overlock), symbol, prettyBTC(xtraFees), symbol)

	return &asset.OrderOption{
		ConfigOption: asset.ConfigOption{
			Key:          splitKey,
			DisplayName:  "Pre-size Funds",
			Description:  desc,
			DefaultValue: btc.useSplitTx,
			IsBoolean:    true,
		},
		Boolean: &asset.BooleanConfig{
			Reason: reason,
		},
	}
}

// estimateSwap prepares an *asset.SwapEstimate.
func (btc *baseWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, utxos []*compositeUTXO,
	nfo *dex.Asset, trySplit bool, feeBump float64) (*asset.SwapEstimate, bool /*split used*/, uint64 /*amt locked*/, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.amount
	}

	// If there is a fee bump, the networkFeeRate can be higher than the
	// MaxFeeRate
	bumpedMaxRate := nfo.MaxFeeRate
	bumpedNetRate := feeSuggestion
	if feeBump > 1 {
		bumpedMaxRate = uint64(math.Round(float64(bumpedMaxRate) * feeBump))
		bumpedNetRate = uint64(math.Round(float64(bumpedNetRate) * feeBump))
	}

	val := lots * lotSize

	enough := func(inputsSize, inputsVal uint64) bool {
		reqFunds := calc.RequiredOrderFundsAlt(val, inputsSize, lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)
		return inputsVal >= reqFunds
	}

	sum, inputsSize, _, _, _, _, err := fund(utxos, enough)
	if err != nil {
		return nil, false, 0, fmt.Errorf("error funding swap value %s: %w", amount(val), err)
	}

	reqFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)
	maxFees := reqFunds - val

	estHighFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedNetRate)
	estHighFees := estHighFunds - val

	estLowFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), 1, nfo.SwapSizeBase, nfo.SwapSize, bumpedNetRate)
	if btc.segwit {
		estLowFunds += dexbtc.P2WSHOutputSize * (lots - 1) * bumpedNetRate
	} else {
		estLowFunds += dexbtc.P2SHOutputSize * (lots - 1) * bumpedNetRate
	}

	estLowFees := estLowFunds - val

	// Math for split transactions is a little different.
	if trySplit {
		_, extraMaxFees := btc.splitBaggageFees(bumpedMaxRate)
		_, splitFees := btc.splitBaggageFees(bumpedNetRate)
		locked := val + maxFees + extraMaxFees

		if avail >= reqFunds+extraMaxFees {
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees + extraMaxFees,
				RealisticBestCase:  estLowFees + splitFees,
				RealisticWorstCase: estHighFees + splitFees,
			}, true, locked, nil
		}
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

	// Read the order options.
	customCfg := new(redeemOptions)
	err := config.Unmapify(req.SelectedOptions, customCfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing selected options: %w", err)
	}

	// Parse the configured fee bump.
	var currentBump float64 = 1.0
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
	if ord.FeeSuggestion > ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf("fee suggestion %d > max fee rate %d", ord.FeeSuggestion, ord.DEXConfig.MaxFeeRate)
	}
	if ord.FeeSuggestion > btc.feeRateLimit {
		return nil, nil, fmt.Errorf("suggested fee > configured limit. %d > %d", ord.FeeSuggestion, btc.feeRateLimit)
	}
	// Check wallets fee rate limit against server's max fee rate
	if btc.feeRateLimit < ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configued fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			btc.feeRateLimit)
	}

	customCfg := new(swapOptions)
	err := config.Unmapify(ord.Options, customCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing swap options: %w", err)
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

	bumpedMaxRate, err := calcBumpedRate(ord.DEXConfig.MaxFeeRate, customCfg.FeeBump)
	if err != nil {
		btc.log.Errorf("calcBumpRate error: %v", err)
	}

	enough := func(inputsSize, inputsVal uint64) bool {
		reqFunds := calc.RequiredOrderFundsAlt(ord.Value, inputsSize, ord.MaxSwapCount, ord.DEXConfig.SwapSizeBase, ord.DEXConfig.SwapSize, bumpedMaxRate)
		return inputsVal >= reqFunds
	}

	sum, size, coins, fundingCoins, redeemScripts, spents, err := fund(utxos, enough)
	if err != nil {
		return nil, nil, fmt.Errorf("error funding swap value of %s: %w", amount(ord.Value), err)
	}

	useSplit := btc.useSplitTx
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	if useSplit && !ord.Immediate {
		// We apply the bumped fee rate to the split transaction when the
		// PreSwap is created, so we use that bumped rate here too.
		// But first, check that it's within bounds.
		splitFeeRate := ord.FeeSuggestion
		if splitFeeRate == 0 {
			// TODO
			// 1.0: Error when no suggestion.
			// return nil, nil, fmt.Errorf("cannot do a split transaction without a fee rate suggestion from the server")
			splitFeeRate = btc.targetFeeRateWithFallback(btc.redeemConfTarget, 0)
			// We PreOrder checked this as <= MaxFeeRate, so use that as an
			// upper limit.
			if splitFeeRate > ord.DEXConfig.MaxFeeRate {
				splitFeeRate = ord.DEXConfig.MaxFeeRate
			}
		}
		splitFeeRate, err = calcBumpedRate(splitFeeRate, customCfg.FeeBump)
		if err != nil {
			btc.log.Errorf("calcBumpRate error: %v", err)
		}

		splitCoins, split, err := btc.split(ord.Value, ord.MaxSwapCount, spents,
			uint64(size), fundingCoins, splitFeeRate, bumpedMaxRate, ord.DEXConfig)
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
	inputsSize uint64, fundingCoins map[outPoint]*utxo, suggestedFeeRate, bumpedMaxRate uint64, nfo *dex.Asset) (asset.Coins, bool, error) {

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
	swapInputSize, baggage := btc.splitBaggageFees(bumpedMaxRate)

	var coinSum uint64
	coins := make(asset.Coins, 0, len(outputs))
	for _, op := range outputs {
		coins = append(coins, op)
		coinSum += op.value
	}

	valueStr := amount(value).String()

	excess := coinSum - calc.RequiredOrderFundsAlt(value, inputsSize, lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)
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
	addrStr, err := btc.stringAddr(addr, btc.chainParams)
	if err != nil {
		return nil, false, fmt.Errorf("failed to stringify the change address: %w", err)
	}

	reqFunds := calc.RequiredOrderFundsAlt(value, swapInputSize, lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)

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

	// Sign, add change, and send the transaction.
	msgTx, err := btc.sendWithReturn(baseTx, changeAddr, coinSum, reqFunds, suggestedFeeRate)
	if err != nil {
		return nil, false, err
	}

	txHash := btc.hashTx(msgTx)
	op := newOutput(txHash, 0, reqFunds)

	// Need to save one funding coin (in the deferred function).
	fundingCoins = map[outPoint]*utxo{op.pt: {
		txHash:  op.txHash(),
		vout:    op.vout(),
		address: addrStr,
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
		swapInputSize = dexbtc.RedeemP2WPKHInputTotalSize
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
	baseTx := wire.NewMsgTx(btc.txVersion())
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

// lookupWalletTxOutput looks up the value of a transaction output that is
// spandable by this wallet, and creates an output.
func (btc *baseWallet) lookupWalletTxOutput(txHash *chainhash.Hash, vout uint32) (*output, error) {
	getTxResult, err := btc.node.getWalletTransaction(txHash)
	if err != nil {
		return nil, err
	}

	tx, err := msgTxFromBytes(getTxResult.Hex)
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
		prevMsgTx, err := msgTxFromBytes(prevTx.Hex)
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

		msgTx, err := msgTxFromBytes(tx.Hex)
		if err != nil {
			return 0, 0, err
		}

		fee, err := btc.getTxFee(msgTx)
		if err != nil {
			return 0, 0, err
		}

		fees += fee
		size += dexbtc.MsgTxVBytes(msgTx)
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
		utxos, _, _, err := btc.spendableUTXOs(1)
		if err != nil {
			return makeError(err)
		}

		_, _, additionalInputs, _, _, _, err = fund(utxos, func(inputSize, inputsVal uint64) bool {
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
			return totalFees+requiredForRemainingSwaps <= inputsVal+orderChange.value
		})
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
	err = btc.broadcastTx(signedTx)
	if err != nil {
		return nil, "", err
	}

	// Delete the old change from the cache
	delete(btc.fundingCoins, newOutPoint(changeTxHash, changeVout))

	if newChange == nil {
		return nil, signedTx.TxHash().String(), nil
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
	return newChange, signedTx.TxHash().String(), nil
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

	rawFeeRate := btc.targetFeeRateWithFallback(btc.redeemConfTarget, form.FeeSuggestion)
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
	checkHash := btc.hashTx(msgTx)
	txHash, err := btc.node.sendRawTransaction(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}
	if *txHash != *checkHash {
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
func (btc *baseWallet) FindRedemption(ctx context.Context, coinID, _ dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
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

	txOut, err := btc.txOutFromTxBytes(tx.Hex, vout)
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

	checkHash := btc.hashTx(msgTx)
	refundHash, err := btc.node.sendRawTransaction(msgTx)
	if err != nil {
		return nil, fmt.Errorf("sendRawTransaction: %w", err)
	}
	if *refundHash != *checkHash {
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
	addr, err := btc.externalAddress()
	if err != nil {
		return "", err
	}
	return btc.stringAddr(addr, btc.chainParams)
}

// NewAddress returns a new address from the wallet. This satisfies the
// NewAddresser interface.
func (btc *baseWallet) NewAddress() (string, error) {
	return btc.DepositAddress()
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

	tx, err := btc.deserializeTx(txRes.Hex)
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
		changeSize := dexbtc.MsgTxVBytes(baseTx) - vSize0   // may be dexbtc.P2WPKHOutputSize
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
			changeOutput.Value, msgTx.TxHash())
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

func (btc *baseWallet) broadcastTx(signedTx *wire.MsgTx) error {
	txHash, err := btc.node.sendRawTransaction(signedTx)
	if err != nil {
		return fmt.Errorf("sendrawtx error: %v, raw tx: %x", err, btc.wireBytes(signedTx))
	}
	checkHash := btc.hashTx(signedTx)
	if *txHash != *checkHash {
		return fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %x", checkHash, *txHash, btc.wireBytes(signedTx))
	}
	return nil
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
	sig, err = txscript.RawTxInWitnessSignature(tx, sigHashes, idx, val,
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
	MedianTime        int64  `json:"mediantime"`
	PreviousBlockHash string `json:"previousblockhash"`
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
