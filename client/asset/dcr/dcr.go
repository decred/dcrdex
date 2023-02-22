// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
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
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/wallet"
	_ "decred.org/dcrwallet/v2/wallet/drivers/bdb"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/blockchain/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

const (
	// The implementation version. This considers the dex/networks package too.
	version = 0

	// BipID is the BIP-0044 asset ID.
	BipID = 42

	// defaultFee is the default value for the fallbackfee.
	defaultFee = 20
	// defaultFeeRateLimit is the default value for the feeratelimit.
	defaultFeeRateLimit = 100
	// defaultRedeemConfTarget is the default redeem transaction confirmation
	// target in blocks used by estimatesmartfee to get the optimal fee for a
	// redeem transaction.
	defaultRedeemConfTarget = 1

	// splitTxBaggage is the total number of additional bytes associated with
	// using a split transaction to fund a swap.
	splitTxBaggage = dexdcr.MsgTxOverhead + dexdcr.P2PKHInputSize + 2*dexdcr.P2PKHOutputSize

	walletTypeDcrwRPC = "dcrwalletRPC"
	walletTypeLegacy  = "" // dcrwallet RPC prior to wallet types
	walletTypeSPV     = "SPV"

	// confCheckTimeout is the amount of time allowed to check for
	// confirmations. If SPV, this might involve pulling a full block.
	confCheckTimeout = 4 * time.Second

	// acctInternalBranch is the child number used when performing BIP0044 style
	// hierarchical deterministic key derivation for the internal branch of an
	// account.
	acctInternalBranch uint32 = 1

	// externalApiUrl is the URL of the external API in case of fallback.
	externalApiUrl = "https://explorer.dcrdata.org/insight/api"
	// testnetExternalApiUrl is the URL of the testnet external API in case of
	// fallback.
	testnetExternalApiUrl = "https://testnet.dcrdata.org/insight/api"

	// freshFeeAge is the expiry age for cached fee rates of external origin,
	// past which fetchFeeFromOracle should be used to refresh the rate.
	freshFeeAge = time.Minute
)

var (
	// ContractSearchLimit is how far back in time AuditContract in SPV mode
	// will search for a contract if no txData is provided. This should be a
	// positive duration.
	ContractSearchLimit = 48 * time.Hour

	// blockTicker is the delay between calls to check for new blocks.
	blockTicker                  = time.Second
	peerCountTicker              = 5 * time.Second
	conventionalConversionFactor = float64(dexdcr.UnitInfo.Conventional.ConversionFactor)
	walletBlockAllowance         = time.Second * 10

	walletOpts = []*asset.ConfigOption{
		{
			Key:         "fallbackfee",
			DisplayName: "Fallback fee rate",
			Description: "The fee rate to use for fee payment and withdrawals when " +
				"estimatesmartfee is not available. Units: DCR/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: DCR/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "redeemconftarget",
			DisplayName: "Redeem confirmation target",
			Description: "The target number of blocks for the redeem transaction " +
				"to get a confirmation. Used to set the transaction's fee rate." +
				" (default: 1 block)",
			DefaultValue: defaultRedeemConfTarget,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to " +
				"fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order " +
				"until the first swap contract is broadcast during match settlement, or " +
				"the order is canceled. This an extra transaction for which network " +
				"mining fees are paid.  Used only for standing-type orders, e.g. " +
				"limit orders without immediate time-in-force.",
			IsBoolean:    true,
			DefaultValue: false,
		},
		{
			Key:         "apifeefallback",
			DisplayName: "External fee rate estimates",
			Description: "Allow fee rate estimation from a block explorer API. " +
				"This is useful as a fallback for SPV wallets and RPC wallets " +
				"that have recently been started.",
			IsBoolean:    true,
			DefaultValue: false,
		},
	}

	rpcOpts = []*asset.ConfigOption{
		{
			Key:         "account",
			DisplayName: "Account Name",
			Description: "Primary dcrwallet account name for trading. If automatic mixing of trading funds is " +
				"desired, this should be the wallet's mixed account and the other accounts should be set too. " +
				"See wallet documentation for mixing wallet setup instructions.",
		},
		{
			Key:         "unmixedaccount",
			DisplayName: "Change Account Name",
			Description: "dcrwallet change account name. This and the 'Temporary Trading Account' should only be " +
				"set if mixing is enabled on the wallet. If set, deposit addresses will be from this account and will " +
				"be mixed before being available to trade.",
		},
		{
			Key:         "tradingaccount",
			DisplayName: "Temporary Trading Account",
			Description: "dcrwallet account to temporarily store split tx outputs or change from chained swaps in " +
				"multi-lot orders. This should only be set if 'Change Account Name' is set.",
		},
		{
			Key:         "username",
			DisplayName: "RPC Username",
			Description: "dcrwallet's 'username' setting for JSON-RPC",
		},
		{
			Key:         "password",
			DisplayName: "RPC Password",
			Description: "dcrwallet's 'password' setting for JSON-RPC",
			NoEcho:      true,
		},
		{
			Key:          "rpclisten",
			DisplayName:  "RPC Address",
			Description:  "dcrwallet's address (host or host:port) (default port: 9110)",
			DefaultValue: "127.0.0.1:9110",
		},
		{
			Key:          "rpccert",
			DisplayName:  "TLS Certificate",
			Description:  "Path to the dcrwallet TLS certificate file",
			DefaultValue: defaultRPCCert,
		},
	}

	spvOpts = []*asset.ConfigOption{{
		Key:         "walletbirthday",
		DisplayName: "Wallet Birthday",
		Description: "This is the date the wallet starts scanning the blockchain " +
			"for transactions related to this wallet. If reconfiguring an existing " +
			"wallet, this may start a rescan if the new birthday is older. This " +
			"option is disabled if there are currently active DCR trades.",
		DefaultValue: defaultWalletBirthdayUnix,
		MaxValue:     "now",
		// This MinValue must be removed if we start supporting importing private keys
		MinValue:          defaultWalletBirthdayUnix,
		IsDate:            true,
		DisableWhenActive: true,
		IsBirthdayConfig:  true,
	}}

	// WalletInfo defines some general information about a Decred wallet.
	WalletInfo = &asset.WalletInfo{
		Name:     "Decred",
		Version:  version,
		UnitInfo: dexdcr.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:        walletTypeSPV,
				Tab:         "Native",
				Description: "Use the built-in SPV wallet",
				ConfigOpts:  append(spvOpts, walletOpts...),
				Seeded:      true,
			},
			{
				Type:              walletTypeDcrwRPC,
				Tab:               "External",
				Description:       "Connect to dcrwallet",
				DefaultConfigPath: defaultConfigPath,
				ConfigOpts:        append(rpcOpts, walletOpts...),
			},
		},
	}
	swapFeeBumpKey   = "swapfeebump"
	splitKey         = "swapsplit"
	redeemFeeBumpFee = "redeemfeebump"
	client           http.Client
)

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

// String is a human-readable string representation of the outPoint.
func (pt outPoint) String() string {
	return pt.txHash.String() + ":" + strconv.Itoa(int(pt.vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	pt    outPoint
	tree  int8
	value uint64
}

// newOutput is the constructor for an output.
func newOutput(txHash *chainhash.Hash, vout uint32, value uint64, tree int8) *output {
	return &output{
		pt: outPoint{
			txHash: *txHash,
			vout:   vout,
		},
		value: value,
		tree:  tree,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (op *output) Value() uint64 {
	return op.value
}

// ID is the output's coin ID. Part of the asset.Coin interface. For DCR, the
// coin ID is 36 bytes = 32 bytes tx hash + 4 bytes big-endian vout.
func (op *output) ID() dex.Bytes {
	return toCoinID(op.txHash(), op.vout())
}

// String is a string representation of the coin.
func (op *output) String() string {
	return op.pt.String()
}

// txHash returns the pointer of the outPoint's txHash.
func (op *output) txHash() *chainhash.Hash {
	return &op.pt.txHash
}

// vout returns the outPoint's vout.
func (op *output) vout() uint32 {
	return op.pt.vout
}

// wireOutPoint creates and returns a new *wire.OutPoint for the output.
func (op *output) wireOutPoint() *wire.OutPoint {
	return wire.NewOutPoint(op.txHash(), op.vout(), op.tree)
}

// auditInfo is information about a swap contract on the blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
type auditInfo struct {
	output     *output
	secretHash []byte
	contract   []byte
	recipient  stdaddr.Address // unused?
	expiration time.Time
}

// Expiration is the expiration time of the contract, which is the earliest time
// that a refund can be issued for an un-redeemed contract.
func (ci *auditInfo) Expiration() time.Time {
	return ci.expiration
}

// Contract is the contract script.
func (ci *auditInfo) Contract() dex.Bytes {
	return ci.contract
}

// Coin returns the output as an asset.Coin.
func (ci *auditInfo) Coin() asset.Coin {
	return ci.output
}

// SecretHash is the contract's secret hash.
func (ci *auditInfo) SecretHash() dex.Bytes {
	return ci.secretHash
}

// convertAuditInfo converts from the common *asset.AuditInfo type to our
// internal *auditInfo type.
func convertAuditInfo(ai *asset.AuditInfo, chainParams *chaincfg.Params) (*auditInfo, error) {
	if ai.Coin == nil {
		return nil, fmt.Errorf("no coin")
	}

	op, ok := ai.Coin.(*output)
	if !ok {
		return nil, fmt.Errorf("unknown coin type %T", ai.Coin)
	}

	recip, err := stdaddr.DecodeAddress(ai.Recipient, chainParams)
	if err != nil {
		return nil, err
	}

	return &auditInfo{
		output:     op,            // *output
		recipient:  recip,         // btcutil.Address
		contract:   ai.Contract,   // []byte
		secretHash: ai.SecretHash, // []byte
		expiration: ai.Expiration, // time.Time
	}, nil
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

// Coin is the contract script. Part of the asset.Receipt interface.
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

// fundingCoin is similar to output, but also stores the address. The
// ExchangeWallet fundingCoins dict is used as a local cache of coins being
// spent.
type fundingCoin struct {
	op   *output
	addr string
}

// Driver implements asset.Driver.
type Driver struct{}

// Check that Driver implements asset.Driver.
var _ asset.Driver = (*Driver)(nil)
var _ asset.Creator = (*Driver)(nil)

// Open creates the DCR exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Decred.
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

// Exists checks the existence of the wallet. Part of the Creator interface.
func (d *Driver) Exists(walletType, dataDir string, _ map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeSPV {
		return false, fmt.Errorf("no Decred wallet of type %q available", walletType)
	}

	chainParams, err := parseChainParams(net)
	if err != nil {
		return false, err
	}

	return walletExists(filepath.Join(dataDir, chainParams.Name, "spv"))
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
		return fmt.Errorf("error parsing chain params: %w", err)
	}

	recoveryCfg := new(RecoveryCfg)
	err = config.Unmapify(params.Settings, recoveryCfg)
	if err != nil {
		return err
	}

	return createSPVWallet(params.Pass, params.Seed, params.DataDir, recoveryCfg.NumExternalAddresses,
		recoveryCfg.NumInternalAddresses, chainParams)
}

func init() {
	asset.Register(BipID, &Driver{})
}

// RecoveryCfg is the information that is transferred from the old wallet
// to the new one when the wallet is recovered.
type RecoveryCfg struct {
	NumExternalAddresses uint32 `ini:"numexternaladdr"`
	NumInternalAddresses uint32 `ini:"numinternaladdr"`
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

type feeStamped struct {
	rate  uint64
	stamp time.Time
}

// exchangeWalletConfig is the validated, unit-converted, user-configurable
// wallet settings.
type exchangeWalletConfig struct {
	primaryAcct    string
	unmixedAccount string // mixing-enabled wallets only
	// tradingAccount (mixing-enabled wallets only) stores utxos reserved for
	// executing order matches, the external branch stores split tx outputs,
	// internal branch stores chained (non-final) swap change.
	tradingAccount   string
	useSplitTx       bool
	fallbackFeeRate  uint64
	feeRateLimit     uint64
	redeemConfTarget uint64
	apiFeeFallback   bool
}

// ExchangeWallet is a wallet backend for Decred. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	cfgV atomic.Value // *exchangeWalletConfig

	ctx           context.Context // the asset subsystem starts with Connect(ctx)
	wallet        Wallet
	chainParams   *chaincfg.Params
	log           dex.Logger
	network       dex.Network
	tipChange     func(error)
	lastPeerCount uint32
	peersChange   func(uint32, error)

	oracleFeesMtx sync.Mutex
	oracleFees    map[uint64]feeStamped // conf target => fee rate
	oracleFailing bool

	tipMtx     sync.RWMutex
	currentTip *block

	// Coins returned by Fund are cached for quick reference.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*fundingCoin

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[outPoint]*findRedemptionReq

	externalTxMtx   sync.RWMutex
	externalTxCache map[chainhash.Hash]*externalTx
}

func (dcr *ExchangeWallet) config() *exchangeWalletConfig {
	return dcr.cfgV.Load().(*exchangeWalletConfig)
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)
var _ asset.FeeRater = (*ExchangeWallet)(nil)
var _ asset.Withdrawer = (*ExchangeWallet)(nil)
var _ asset.LiveReconfigurer = (*ExchangeWallet)(nil)

type block struct {
	height int64
	hash   *chainhash.Hash
}

// findRedemptionReq represents a request to find a contract's redemption,
// which is added to the findRedemptionQueue with the contract outpoint as
// key.
type findRedemptionReq struct {
	ctx                     context.Context
	contractP2SHScript      []byte
	contractOutputScriptVer uint16
	resultChan              chan *findRedemptionResult
}

func (frr *findRedemptionReq) canceled() bool {
	return frr.ctx.Err() != nil
}

// findRedemptionResult models the result of a find redemption attempt.
type findRedemptionResult struct {
	RedemptionCoinID dex.Bytes
	Secret           dex.Bytes
	Err              error
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// variable.
	walletCfg := new(walletConfig)
	chainParams, err := loadConfig(cfg.Settings, network, walletCfg)
	if err != nil {
		return nil, err
	}

	dcr, err := unconnectedWallet(cfg, walletCfg, chainParams, logger, network)
	if err != nil {
		return nil, err
	}

	switch cfg.Type {
	case walletTypeDcrwRPC, walletTypeLegacy:
		dcr.wallet, err = newRPCWallet(cfg.Settings, logger, network)
		if err != nil {
			return nil, err
		}
	case walletTypeSPV:
		dcr.wallet, err = openSPVWallet(cfg.DataDir, chainParams, logger)
		if err != nil {
			return nil, err
		}
	default:
		if makeCustomWallet, ok := customWalletConstructors[cfg.Type]; ok {
			dcr.wallet, err = makeCustomWallet(cfg.Settings, chainParams, logger)
			if err != nil {
				return nil, fmt.Errorf("custom wallet setup error: %v", err)
			}
		} else {
			return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
		}
	}

	return dcr, nil
}

func getExchangeWalletCfg(dcrCfg *walletConfig, logger dex.Logger) (*exchangeWalletConfig, error) {
	// If set in the user config, the fallback fee will be in units of DCR/kB.
	// Convert to atoms/B.
	fallbackFeesPerByte := toAtoms(dcrCfg.FallbackFeeRate / 1000)
	if fallbackFeesPerByte == 0 {
		fallbackFeesPerByte = defaultFee
	}
	logger.Tracef("Fallback fees set at %d atoms/byte", fallbackFeesPerByte)

	// If set in the user config, the fee rate limit will be in units of DCR/KB.
	// Convert to atoms/byte & error if value is smaller than smallest unit.
	feesLimitPerByte := uint64(defaultFeeRateLimit)
	if dcrCfg.FeeRateLimit > 0 {
		feesLimitPerByte = toAtoms(dcrCfg.FeeRateLimit / 1000)
		if feesLimitPerByte == 0 {
			return nil, fmt.Errorf("Fee rate limit is smaller than smallest unit: %v",
				dcrCfg.FeeRateLimit)
		}
	}
	logger.Tracef("Fees rate limit set at %d atoms/byte", feesLimitPerByte)

	redeemConfTarget := dcrCfg.RedeemConfTarget
	if redeemConfTarget == 0 {
		redeemConfTarget = defaultRedeemConfTarget
	}
	logger.Tracef("Redeem conf target set to %d blocks", redeemConfTarget)

	primaryAcct := dcrCfg.PrimaryAccount
	if primaryAcct == "" {
		primaryAcct = defaultAcctName
	}
	logger.Tracef("Primary account set to %s", primaryAcct)

	// Both UnmixedAccount and TradingAccount must be provided if primary
	// account is a mixed account. Providing one but not the other is bad
	// configuration. If set, the account names will be validated on Connect.
	if (dcrCfg.UnmixedAccount == "") != (dcrCfg.TradingAccount == "") {
		return nil, fmt.Errorf("'Change Account Name' and 'Temporary Trading Account' MUST "+
			"be set to treat %[1]q as a mixed account. If %[1]q is not a mixed account, values "+
			"should NOT be set for 'Change Account Name' and 'Temporary Trading Account'",
			dcrCfg.PrimaryAccount)
	}
	if dcrCfg.UnmixedAccount != "" {
		switch {
		case dcrCfg.PrimaryAccount == dcrCfg.UnmixedAccount:
			return nil, fmt.Errorf("Primary Account should not be the same as Change Account")
		case dcrCfg.PrimaryAccount == dcrCfg.TradingAccount:
			return nil, fmt.Errorf("Primary Account should not be the same as Temporary Trading Account")
		case dcrCfg.TradingAccount == dcrCfg.UnmixedAccount:
			return nil, fmt.Errorf("Temporary Trading Account should not be the same as Change Account")
		}
	}

	return &exchangeWalletConfig{
		primaryAcct:      primaryAcct,
		unmixedAccount:   dcrCfg.UnmixedAccount,
		tradingAccount:   dcrCfg.TradingAccount,
		fallbackFeeRate:  fallbackFeesPerByte,
		feeRateLimit:     feesLimitPerByte,
		redeemConfTarget: redeemConfTarget,
		useSplitTx:       dcrCfg.UseSplitTx,
		apiFeeFallback:   dcrCfg.ApiFeeFallback,
	}, nil
}

// unconnectedWallet returns an ExchangeWallet without a base wallet. The wallet
// should be set before use.
func unconnectedWallet(cfg *asset.WalletConfig, dcrCfg *walletConfig, chainParams *chaincfg.Params, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	walletCfg, err := getExchangeWalletCfg(dcrCfg, logger)
	if err != nil {
		return nil, err
	}

	w := &ExchangeWallet{
		log:                 logger,
		chainParams:         chainParams,
		network:             network,
		tipChange:           cfg.TipChange,
		peersChange:         cfg.PeersChange,
		fundingCoins:        make(map[outPoint]*fundingCoin),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		externalTxCache:     make(map[chainhash.Hash]*externalTx),
		oracleFees:          make(map[uint64]feeStamped),
	}

	w.cfgV.Store(walletCfg)

	return w, nil
}

// openSPVWallet opens the previously created native SPV wallet.
func openSPVWallet(dataDir string, chainParams *chaincfg.Params, log dex.Logger) (*spvWallet, error) {
	dir := filepath.Join(dataDir, chainParams.Name, "spv")
	if exists, err := walletExists(dir); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("wallet at %q doesn't exists", dir)
	}

	return &spvWallet{
		acctNum:     defaultAcct,
		acctName:    defaultAcctName,
		dir:         dir,
		chainParams: chainParams,
		log:         log.SubLogger("SPV"),
		blockCache: blockCache{
			blocks: make(map[chainhash.Hash]*cachedBlock),
		},
		tipChan: make(chan *block, 1),
	}, nil
}

// Info returns basic information about the wallet and asset.
func (dcr *ExchangeWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// var logup uint32

// func rpclog(log dex.Logger) {
// 	if atomic.CompareAndSwapUint32(&logup, 0, 1) {
// 		rpcclient.UseLogger(log)
// 	}
// }

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (dcr *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// rpclog(dcr.log)
	dcr.ctx = ctx

	err := dcr.wallet.Connect(ctx)
	if err != nil {
		return nil, err
	}

	// The wallet is connected now, so if any of the following checks
	// fails and we return with a non-nil error, we must disconnect the
	// wallet.
	// This is especially important as the wallet may be using an rpc
	// connection which was established above and if we do not disconnect,
	// subsequent reconnect attempts will be met with "websocket client
	// has already connected".
	var success bool
	defer func() {
		if !success {
			dcr.wallet.Disconnect()
		}
	}()

	// Validate accounts early on to prevent errors later.
	for _, acct := range dcr.allAccounts() {
		if acct == "" {
			continue
		}
		_, err = dcr.wallet.AccountUnlocked(ctx, acct)
		if err != nil {
			return nil, fmt.Errorf("unexpected AccountUnlocked error for %q account: %w", acct, err)
		}
	}

	// Initialize the best block.
	dcr.tipMtx.Lock()
	dcr.currentTip, err = dcr.getBestBlock(ctx)
	dcr.tipMtx.Unlock()
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for DCR: %w", err)
	}

	success = true // All good, don't disconnect the wallet when this method returns.

	// NotifyOnTipChange will return false if the wallet does not support
	// tip change notification. We'll use dcr.monitorBlocks below if so.
	monitoringBlocks := dcr.wallet.NotifyOnTipChange(ctx, dcr.handleTipChange)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !monitoringBlocks {
			dcr.monitorBlocks(ctx)
		} else {
			<-ctx.Done() // just wait for shutdown signal
		}
		dcr.shutdown()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		dcr.monitorPeers(ctx)
	}()
	return &wg, nil
}

// Reconfigure attempts to reconfigure the wallet.
func (dcr *ExchangeWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, currentAddress string) (restart bool, err error) {
	dcrCfg := new(walletConfig)
	_, err = loadConfig(cfg.Settings, dcr.network, dcrCfg)
	if err != nil {
		return false, err
	}

	var depositAccount string
	if dcrCfg.UnmixedAccount != "" {
		depositAccount = dcrCfg.UnmixedAccount
	} else {
		depositAccount = dcrCfg.PrimaryAccount
	}

	restart, err = dcr.wallet.Reconfigure(dcr.ctx, cfg, dcr.network, currentAddress, depositAccount)
	if err != nil || restart {
		return restart, err
	}

	exchangeWalletCfg, err := getExchangeWalletCfg(dcrCfg, dcr.log)
	if err != nil {
		return false, err
	}

	dcr.cfgV.Store(exchangeWalletCfg)

	return false, nil
}

// depositAccount returns the account that may be used to receive funds into
// the wallet, either by a direct deposit action or via redemption or refund.
func (dcr *ExchangeWallet) depositAccount() string {
	cfg := dcr.config()

	if cfg.unmixedAccount != "" {
		return cfg.unmixedAccount
	}
	return cfg.primaryAcct
}

// fundingAccounts returns the primary account along with any configured trading
// account which may contain spendable outputs (split tx outputs or chained swap
// change).
func (dcr *ExchangeWallet) fundingAccounts() []string {
	cfg := dcr.config()

	if cfg.unmixedAccount == "" {
		return []string{cfg.primaryAcct}
	}
	return []string{cfg.primaryAcct, cfg.tradingAccount}
}

func (dcr *ExchangeWallet) allAccounts() []string {
	cfg := dcr.config()

	if cfg.unmixedAccount == "" {
		return []string{cfg.primaryAcct}
	}
	return []string{cfg.primaryAcct, cfg.tradingAccount, cfg.unmixedAccount}
}

// OwnsDepositAddress indicates if the provided address can be used to deposit
// funds into the wallet.
func (dcr *ExchangeWallet) OwnsDepositAddress(address string) (bool, error) {
	addr, err := stdaddr.DecodeAddress(address, dcr.chainParams)
	if err != nil {
		return false, err
	}
	return dcr.wallet.AccountOwnsAddress(dcr.ctx, addr, dcr.depositAccount())
}

// Balance should return the total available funds in the wallet. Note that
// after calling Fund, the amount returned by Balance may change by more than
// the value funded. Part of the asset.Wallet interface. TODO: Since this
// includes potentially untrusted 0-conf utxos, consider prioritizing confirmed
// utxos when funding an order.
func (dcr *ExchangeWallet) Balance() (*asset.Balance, error) {
	cfg := dcr.config()

	locked, err := dcr.lockedAtoms(cfg.primaryAcct)
	if err != nil {
		return nil, err
	}
	ab, err := dcr.wallet.AccountBalance(dcr.ctx, 0, cfg.primaryAcct)
	if err != nil {
		return nil, err
	}
	bal := &asset.Balance{
		Available: toAtoms(ab.Spendable) - locked,
		Immature: toAtoms(ab.ImmatureCoinbaseRewards) +
			toAtoms(ab.ImmatureStakeGeneration),
		Locked: locked + toAtoms(ab.LockedByTickets),
	}

	if cfg.unmixedAccount == "" {
		return bal, nil
	}

	// Mixing is enabled, consider ...
	// 1) trading account spendable (-locked) as available,
	// 2) all unmixed funds as immature, and
	// 3) all locked utxos in the trading account as locked (for swapping).
	tradingAcctBal, err := dcr.wallet.AccountBalance(dcr.ctx, 0, cfg.tradingAccount)
	if err != nil {
		return nil, err
	}
	tradingAcctLocked, err := dcr.lockedAtoms(cfg.tradingAccount)
	if err != nil {
		return nil, err
	}
	unmixedAcctBal, err := dcr.wallet.AccountBalance(dcr.ctx, 0, cfg.unmixedAccount)
	if err != nil {
		return nil, err
	}

	bal.Available += toAtoms(tradingAcctBal.Spendable) - tradingAcctLocked
	bal.Immature += toAtoms(unmixedAcctBal.Total)
	bal.Locked += tradingAcctLocked
	return bal, nil
}

// FeeRate satisfies asset.FeeRater.
func (dcr *ExchangeWallet) FeeRate() uint64 {
	const confTarget = 2 // 1 historically gives crazy rates
	rate, err := dcr.feeRate(confTarget)
	if err != nil { // log and return 0
		dcr.log.Errorf("feeRate error: %v", err)
	}
	return rate
}

// feeRate returns the current optimal fee rate in atoms / byte.
func (dcr *ExchangeWallet) feeRate(confTarget uint64) (uint64, error) {
	if feeEstimator, is := dcr.wallet.(FeeRateEstimator); is && !dcr.wallet.SpvMode() {
		dcrPerKB, err := feeEstimator.EstimateSmartFeeRate(dcr.ctx, int64(confTarget), chainjson.EstimateSmartFeeConservative)
		if err == nil && dcrPerKB > 0 {
			return dcrPerKBToAtomsPerByte(dcrPerKB)
		}
		if err != nil {
			dcr.log.Warnf("Failed to get local fee rate estimate: %v", err)
		} else { // dcrPerKB == 0
			dcr.log.Warnf("Local fee estimate is zero.")
		}
	}

	cfg := dcr.config()

	// Either SPV wallet or EstimateSmartFeeRate failed.
	if !cfg.apiFeeFallback {
		return 0, fmt.Errorf("fee rate estimation unavailable and external API is disabled")
	}

	now := time.Now()

	dcr.oracleFeesMtx.Lock()
	defer dcr.oracleFeesMtx.Unlock()
	oracleFee := dcr.oracleFees[confTarget]
	if now.Sub(oracleFee.stamp) < freshFeeAge {
		return oracleFee.rate, nil
	}
	if dcr.oracleFailing {
		return 0, errors.New("fee rate oracle is in a temporary failing state")
	}

	dcr.log.Debugf("Retrieving fee rate from external fee oracle for %d target blocks", confTarget)
	dcrPerKB, err := fetchFeeFromOracle(dcr.ctx, dcr.network, confTarget)
	if err != nil {
		// Flag the oracle as failing so subsequent requests don't also try and
		// fail after the request timeout. Remove the flag after a bit.
		dcr.oracleFailing = true
		time.AfterFunc(freshFeeAge, func() {
			dcr.oracleFeesMtx.Lock()
			dcr.oracleFailing = false
			dcr.oracleFeesMtx.Unlock()
		})
		return 0, fmt.Errorf("external fee rate API failure: %v", err)
	}
	if dcrPerKB <= 0 {
		return 0, fmt.Errorf("invalid fee rate %f from fee oracle", dcrPerKB)
	}
	// Convert to atoms/B and error if it is greater than fee rate limit.
	atomsPerByte, err := dcrPerKBToAtomsPerByte(dcrPerKB)
	if err != nil {
		return 0, err
	}
	if atomsPerByte > cfg.feeRateLimit {
		return 0, fmt.Errorf("fee rate from external API greater than fee rate limit: %v > %v",
			atomsPerByte, cfg.feeRateLimit)
	}
	dcr.oracleFees[confTarget] = feeStamped{atomsPerByte, now}
	return atomsPerByte, nil
}

// dcrPerKBToAtomsPerByte converts a estimated feeRate from dcr/KB to atoms/B.
func dcrPerKBToAtomsPerByte(dcrPerkB float64) (uint64, error) {
	// The caller should check for non-positive numbers, but don't allow
	// underflow when converting to an unsigned integer.
	if dcrPerkB < 0 {
		return 0, fmt.Errorf("negative fee rate")
	}
	// dcrPerkB * 1e8 / 1e3 => atomsPerB
	atomsPerKB, err := dcrutil.NewAmount(dcrPerkB)
	if err != nil {
		return 0, err
	}
	return uint64(dex.IntDivUp(int64(atomsPerKB), 1000)), nil
}

// fetchFeeFromOracle gets the fee rate from the external API.
func fetchFeeFromOracle(ctx context.Context, net dex.Network, nb uint64) (float64, error) {
	var url string
	if net == dex.Testnet {
		url = testnetExternalApiUrl
	} else { // mainnet and simnet
		url = externalApiUrl
	}
	url += "/utils/estimatefee?nbBlocks=" + strconv.FormatUint(nb, 10)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	httpResponse, err := client.Do(r)
	if err != nil {
		return 0, err
	}
	c := make(map[uint64]float64)
	reader := io.LimitReader(httpResponse.Body, 1<<14)
	err = json.NewDecoder(reader).Decode(&c)
	httpResponse.Body.Close()
	if err != nil {
		return 0, err
	}
	dcrPerKB, ok := c[nb]
	if !ok {
		return 0, errors.New("no fee rate for requested number of blocks")
	}
	return dcrPerKB, nil
}

// targetFeeRateWithFallback attempts to get a fresh fee rate for the target
// number of confirmations, but falls back to the suggestion or fallbackFeeRate
// via feeRateWithFallback.
func (dcr *ExchangeWallet) targetFeeRateWithFallback(confTarget, feeSuggestion uint64) uint64 {
	feeRate, err := dcr.feeRate(confTarget)
	if err != nil {
		dcr.log.Errorf("Failed to get fee rate: %v", err)
	} else if feeRate != 0 {
		dcr.log.Tracef("Obtained estimate for %d-conf fee rate, %d", confTarget, feeRate)
		return feeRate
	}

	return dcr.feeRateWithFallback(feeSuggestion)
}

// feeRateWithFallback filters the suggested fee rate by ensuring it is within
// limits. If not, the configured fallbackFeeRate is returned and a warning
// logged.
func (dcr *ExchangeWallet) feeRateWithFallback(feeSuggestion uint64) uint64 {
	cfg := dcr.config()

	if feeSuggestion > 0 && feeSuggestion < cfg.feeRateLimit {
		dcr.log.Tracef("Using caller's suggestion for fee rate, %d", feeSuggestion)
		return feeSuggestion
	}
	dcr.log.Warnf("No usable fee rate suggestion. Using fallback of %d", cfg.fallbackFeeRate)
	return cfg.fallbackFeeRate
}

type amount uint64

func (a amount) String() string {
	return strconv.FormatFloat(dcrutil.Amount(a).ToCoin(), 'f', -1, 64) // dec, but no trailing zeros
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The
// provided FeeSuggestion is used directly, and should be an estimate based on
// current network conditions. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
// lotSize must not be zero and will cause a panic if so.
func (dcr *ExchangeWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	_, est, err := dcr.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.AssetConfig)
	return est, err
}

// maxOrder gets the estimate for MaxOrder, and also returns the
// []*compositeUTXO and network fee rate to be used for further order estimation
// without additional calls to listunspent.
func (dcr *ExchangeWallet) maxOrder(lotSize, feeSuggestion uint64, nfo *dex.Asset) (utxos []*compositeUTXO, est *asset.SwapEstimate, err error) {
	if lotSize == 0 {
		return nil, nil, errors.New("cannot divide by lotSize zero")
	}

	utxos, err = dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}
	var avail uint64
	for _, utxo := range utxos {
		avail += toAtoms(utxo.rpc.Amount)
	}

	// Start by attempting max lots with a basic fee.
	basicFee := nfo.SwapSize * nfo.MaxFeeRate
	lots := avail / (lotSize + basicFee)
	cfg := dcr.config()
	for lots > 0 {
		est, _, _, err := dcr.estimateSwap(lots, lotSize, feeSuggestion, utxos, nfo, cfg.useSplitTx, 1.0)
		// The only failure mode of estimateSwap -> dcr.fund is when there is
		// not enough funds, so if an error is encountered, count down the lots
		// and repeat until we have enough.
		if err != nil {
			lots--
			continue
		}
		return utxos, est, nil
	}

	return nil, &asset.SwapEstimate{}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (dcr *ExchangeWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, utxos []*compositeUTXO,
	nfo *dex.Asset, trySplit bool, feeBump float64) (*asset.SwapEstimate, bool /*split used*/, uint64 /* locked */, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += toAtoms(utxo.rpc.Amount)
	}

	// If there is a fee bump, the networkFeeRate can be higher than the
	// MaxFeeRate
	bumpedMaxRate := nfo.MaxFeeRate
	bumpedNetRate := feeSuggestion
	if feeBump > 1 {
		bumpedMaxRate = uint64(math.Ceil(float64(bumpedMaxRate) * feeBump))
		bumpedNetRate = uint64(math.Ceil(float64(bumpedNetRate) * feeBump))
	}

	val := lots * lotSize
	// The orderEnough func does not account for a split transaction at the
	// start, so it is possible that funding for trySplit would actually choose
	// more UTXOs. Actual order funding accounts for this. For this estimate, we
	// will just not use a split tx if the split-adjusted required funds exceeds
	// the total value of the UTXO selected with this enough closure.
	sum, inputsSize, _, _, _, err := dcr.tryFund(utxos, orderEnough(val, lots, bumpedMaxRate, nfo))
	if err != nil {
		return nil, false, 0, err
	}

	reqFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate) // as in tryFund's enough func
	maxFees := reqFunds - val

	estHighFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedNetRate)
	estHighFees := estHighFunds - val

	estLowFunds := calc.RequiredOrderFundsAlt(val, uint64(inputsSize), 1, nfo.SwapSizeBase, nfo.SwapSize, bumpedNetRate)
	estLowFunds += dexdcr.P2SHOutputSize * (lots - 1) * bumpedNetRate
	estLowFees := estLowFunds - val

	// Math for split transactions is a little different.
	if trySplit {
		splitMaxFees := splitTxBaggage * bumpedMaxRate
		splitFees := splitTxBaggage * bumpedNetRate
		reqTotal := reqFunds + splitMaxFees // ~ rather than actually fund()ing again
		if reqTotal <= sum {
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees + splitMaxFees,
				RealisticBestCase:  estLowFees + splitFees,
				RealisticWorstCase: estHighFees + splitFees,
			}, true, reqFunds, nil // requires reqTotal, but locks reqFunds in the split output
		}
	}

	// No split transaction.
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              val,
		MaxFees:            maxFees,
		RealisticBestCase:  estLowFees,
		RealisticWorstCase: estHighFees,
	}, false, sum, nil
}

// PreSwap get order estimates based on the available funds and the wallet
// configuration.
func (dcr *ExchangeWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	// Start with the maxOrder at the default configuration. This gets us the
	// utxo set, the network fee rate, and the wallet's maximum order size.
	// The utxo set can then be used repeatedly in estimateSwap at virtually
	// zero cost since there are no more RPC calls.
	// The utxo set is only used once right now, but when order-time options are
	// implemented, the utxos will be used to calculate option availability and
	// fees.
	utxos, maxEst, err := dcr.maxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig)
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
	cfg := dcr.config()
	split := cfg.useSplitTx
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

	// Get the estimate for the requested number of lots.
	est, splitUsed, _, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, bump)
	if err != nil {
		return nil, fmt.Errorf("estimation failed: %v", err)
	}

	var opts []*asset.OrderOption

	// If the used split isn't the requested split, the other split option was
	// unavailable, so there is no option to offer.
	if !req.Immediate && splitUsed == split {
		if splitOpt := dcr.splitOption(req, utxos, bump); splitOpt != nil {
			opts = append(opts, splitOpt)
		}
	}

	// Figure out what our maximum available fee bump is, within our 2x hard
	// limit.
	var maxBump float64
	var maxBumpEst *asset.SwapEstimate
	for maxBump = 2.0; maxBump > 1.01; maxBump -= 0.1 {
		tryEst, splitUsed, _, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, maxBump)
		// If the split used wasn't the configured value, this option is not
		// available.
		if err == nil && split == splitUsed {
			maxBumpEst = tryEst
			break
		}
	}

	if maxBumpEst != nil {
		noBumpEst, _, _, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, split, 1.0)
		if err != nil {
			// shouldn't be possible, since we already succeeded with a higher bump.
			return nil, fmt.Errorf("error getting no-bump estimate: %w", err)
		}

		bumpLabel := "2X"
		if maxBump < 2.0 {
			bumpLabel = strconv.FormatFloat(maxBump, 'f', 1, 64) + "X"
		}

		extraFees := maxBumpEst.RealisticWorstCase - noBumpEst.RealisticWorstCase
		desc := fmt.Sprintf("Add a fee multiplier up to %.1fx (up to ~%s DCR more) for faster settlement when network traffic is high.",
			maxBump, amount(extraFees))

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
				YUnit: "atoms/B",
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
func (dcr *ExchangeWallet) splitOption(req *asset.PreSwapForm, utxos []*compositeUTXO, bump float64) *asset.OrderOption {
	noSplitEst, _, noSplitLocked, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, false, bump)
	if err != nil {
		dcr.log.Errorf("estimateSwap (no split) error: %v", err)
		return nil
	}
	splitEst, splitUsed, splitLocked, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, utxos, req.AssetConfig, true, bump)
	if err != nil {
		dcr.log.Errorf("estimateSwap (with split) error: %v", err)
		return nil
	}
	if !splitUsed {
		// unable to do the split. no option.
		dcr.log.Debugf("split option unavailable")
		return nil
	}

	xtraFees := splitEst.RealisticWorstCase - noSplitEst.RealisticWorstCase
	pctChange := (float64(splitEst.RealisticWorstCase)/float64(noSplitEst.RealisticWorstCase) - 1) * 100
	overlock := noSplitLocked - splitLocked

	var reason string
	if pctChange > 1 {
		reason = fmt.Sprintf("+%d%% fees, avoids %s DCR overlock", int(math.Round(pctChange)), amount(overlock))
	} else {
		reason = fmt.Sprintf("+%.1f%% fees, avoids %s DCR overlock", pctChange, amount(overlock))
	}

	desc := fmt.Sprintf("Using a split transaction to prevent temporary overlock of %s DCR, but for additional fees of %s DCR",
		amount(overlock), amount(xtraFees))

	cfg := dcr.config()
	return &asset.OrderOption{
		ConfigOption: asset.ConfigOption{
			Key:          splitKey,
			DisplayName:  "Pre-size Funds",
			Description:  desc,
			DefaultValue: cfg.useSplitTx,
			IsBoolean:    true,
		},
		Boolean: &asset.BooleanConfig{
			Reason: reason,
		},
	}
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (dcr *ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	cfg := dcr.config()

	feeRate := req.FeeSuggestion
	if feeRate == 0 { // or just document that the caller must set it?
		feeRate = dcr.targetFeeRateWithFallback(cfg.redeemConfTarget, req.FeeSuggestion)
	}
	// Best is one transaction with req.Lots inputs and 1 output.
	var best uint64 = dexdcr.MsgTxOverhead
	// Worst is req.Lots transactions, each with one input and one output.
	var worst uint64 = dexdcr.MsgTxOverhead * req.Lots
	var inputSize uint64 = dexdcr.TxInOverhead + dexdcr.RedeemSwapSigScriptSize
	var outputSize uint64 = dexdcr.P2PKHOutputSize
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
			DisplayName:  "Faster Redemption",
			Description:  "Bump the redemption transaction fees up to 2x for faster confirmations on your redemption transaction.",
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
			YUnit: "atoms/B",
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

// orderEnough generates a function that can be used as the enough argument to
// the fund method.
func orderEnough(val, lots, feeRate uint64, nfo *dex.Asset) func(sum uint64, size uint32, unspent *compositeUTXO) bool {
	return func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFundsAlt(val, uint64(size+unspent.input.Size()), lots, nfo.SwapSizeBase, nfo.SwapSize, feeRate)
		// needed fees are reqFunds - value
		return sum+toAtoms(unspent.rpc.Amount) >= reqFunds
	}
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// The returned []dex.Bytes contains the redeem scripts for the selected coins.
// Equal number of coins and redeemed scripts must be returned. A nil or empty
// dex.Bytes should be appended to the redeem scripts collection for coins with
// no redeem script.
func (dcr *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	cfg := dcr.config()

	// Consumer checks dex asset version, so maybe this is not our job:
	// if ord.DEXConfig.Version != dcr.Info().Version {
	// 	return nil, nil, fmt.Errorf("asset version mismatch: server = %d, client = %d",
	// 		ord.DEXConfig.Version, dcr.Info().Version)
	// }
	if ord.Value == 0 {
		return nil, nil, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, fmt.Errorf("cannot fund a zero-lot order")
	}
	if ord.FeeSuggestion > ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf("fee suggestion %d > max fee rate %d", ord.FeeSuggestion, ord.DEXConfig.MaxFeeRate)
	}
	if ord.FeeSuggestion > cfg.feeRateLimit {
		return nil, nil, fmt.Errorf("suggested fee > configured limit. %d > %d", ord.FeeSuggestion, cfg.feeRateLimit)
	}
	// Check wallet's fee rate limit against server's max fee rate
	if cfg.feeRateLimit < ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			cfg.feeRateLimit)
	}

	customCfg := new(swapOptions)
	err := config.Unmapify(ord.Options, customCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("Error parsing swap options")
	}

	// Check ord.Options for a FeeBump here
	bumpedMaxRate, err := calcBumpedRate(ord.DEXConfig.MaxFeeRate, customCfg.FeeBump)
	if err != nil {
		dcr.log.Errorf("calcBumpRate error: %v", err)
	}

	coins, redeemScripts, sum, inputsSize, err := dcr.fund(orderEnough(ord.Value, ord.MaxSwapCount, bumpedMaxRate, ord.DEXConfig))
	if err != nil {
		return nil, nil, fmt.Errorf("error funding order value of %s DCR: %w",
			amount(ord.Value), err)
	}

	useSplit := cfg.useSplitTx
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	// Send a split, if preferred.
	if useSplit && !ord.Immediate {
		// We apply the bumped fee rate to the split transaction when the
		// PreSwap is created, so we use that bumped rate here too.
		// But first, check that it's within bounds.
		rawFeeRate := ord.FeeSuggestion
		if rawFeeRate == 0 {
			// TODO
			// 1.0: Error when no suggestion.
			// return nil, false, fmt.Errorf("cannot do a split transaction without a fee rate suggestion from the server")
			rawFeeRate = dcr.targetFeeRateWithFallback(cfg.redeemConfTarget, 0)
			// We PreOrder checked this as <= MaxFeeRate, so use that as an
			// upper limit.
			if rawFeeRate > ord.DEXConfig.MaxFeeRate {
				rawFeeRate = ord.DEXConfig.MaxFeeRate
			}
		}
		splitFeeRate, err := calcBumpedRate(rawFeeRate, customCfg.FeeBump)
		if err != nil {
			dcr.log.Errorf("calcBumpRate error: %v", err)
		}

		splitCoins, split, err := dcr.split(ord.Value, ord.MaxSwapCount, coins,
			inputsSize, splitFeeRate, bumpedMaxRate, ord.DEXConfig)
		if err != nil {
			if _, errRet := dcr.returnCoins(coins); errRet != nil {
				dcr.log.Warnf("Failed to unlock funding coins %v: %v", coins, errRet)
			}
			return nil, nil, err
		} else if split {
			return splitCoins, []dex.Bytes{nil}, nil // no redeem script required for split tx output
		}
		return splitCoins, redeemScripts, nil // splitCoins == coins
	}

	dcr.log.Infof("Funding %s DCR order with coins %v worth %s", amount(ord.Value), coins, amount(sum))

	return coins, redeemScripts, nil
}

// fund finds coins for the specified value. A function is provided that can
// check whether adding the provided output would be enough to satisfy the
// needed value. Preference is given to selecting coins with 1 or more confs,
// falling back to 0-conf coins where there are not enough 1+ confs coins.
func (dcr *ExchangeWallet) fund(enough func(sum uint64, size uint32, unspent *compositeUTXO) bool) (
	coins asset.Coins, redeemScripts []dex.Bytes, sum, size uint64, err error) {

	// Keep a consistent view of spendable and locked coins in the wallet and
	// the fundingCoins map to make this safe for concurrent use.
	dcr.fundingMtx.Lock()         // before listing unspents in wallet
	defer dcr.fundingMtx.Unlock() // hold until lockFundingCoins (wallet and map)

	utxos, err := dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	sum, sz, coins, spents, redeemScripts, err := dcr.tryFund(utxos, enough)
	if err != nil {
		return nil, nil, 0, 0, err
	}

	err = dcr.lockFundingCoins(spents)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	return coins, redeemScripts, sum, uint64(sz), nil
}

// spendableUTXOs generates a slice of spendable *compositeUTXO.
func (dcr *ExchangeWallet) spendableUTXOs() ([]*compositeUTXO, error) {
	cfg := dcr.config()
	unspents, err := dcr.wallet.Unspents(dcr.ctx, cfg.primaryAcct)
	if err != nil {
		return nil, err
	}
	if cfg.tradingAccount != "" {
		// Trading account may contain spendable utxos such as unspent split tx
		// outputs that are unlocked/returned. TODO: Care should probably be
		// taken to ensure only unspent split tx outputs are selected and other
		// unmixed outputs in the trading account are ignored.
		tradingAcctSpendables, err := dcr.wallet.Unspents(dcr.ctx, cfg.tradingAccount)
		if err != nil {
			return nil, err
		}
		unspents = append(unspents, tradingAcctSpendables...)
	}
	if len(unspents) == 0 {
		return nil, fmt.Errorf("insufficient funds. 0 DCR available to spend in account %q", cfg.primaryAcct)
	}

	// Parse utxos to include script size for spending input. Returned utxos
	// will be sorted in ascending order by amount (smallest first).
	utxos, err := dcr.parseUTXOs(unspents)
	if err != nil {
		return nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no funds available")
	}
	return utxos, nil
}

// tryFund attempts to use the provided []*compositeUTXO to satisfy the enough
// function with the fewest number of inputs. The selected utxos are not locked.
// If the requirement can be satisfied without 0-conf utxos, that set will be
// selected regardless of whether the 0-conf inclusive case would be cheaper.
func (dcr *ExchangeWallet) tryFund(utxos []*compositeUTXO, enough func(sum uint64, size uint32, unspent *compositeUTXO) bool) (
	sum uint64, size uint32, coins asset.Coins, spents []*fundingCoin, redeemScripts []dex.Bytes, err error) {

	addUTXO := func(unspent *compositeUTXO) error {
		txHash, err := chainhash.NewHashFromStr(unspent.rpc.TxID)
		if err != nil {
			return fmt.Errorf("error decoding txid: %w", err)
		}
		v := toAtoms(unspent.rpc.Amount)
		redeemScript, err := hex.DecodeString(unspent.rpc.RedeemScript)
		if err != nil {
			return fmt.Errorf("error decoding redeem script for %s, script = %s: %w",
				unspent.rpc.TxID, unspent.rpc.RedeemScript, err)
		}
		op := newOutput(txHash, unspent.rpc.Vout, v, unspent.rpc.Tree)
		coins = append(coins, op)
		spents = append(spents, &fundingCoin{
			op:   op,
			addr: unspent.rpc.Address,
		})
		redeemScripts = append(redeemScripts, redeemScript)
		size += unspent.input.Size()
		sum += v
		return nil
	}

	isEnoughWith := func(utxo *compositeUTXO) bool {
		return enough(sum, size, utxo)
	}

	tryUTXOs := func(minconf int64) (ok bool, err error) {
		sum, size = 0, 0 // size is only sum of inputs size, not including tx overhead or outputs
		coins, spents, redeemScripts = nil, nil, nil

		okUTXOs := make([]*compositeUTXO, 0, len(utxos)) // over-allocate
		for _, cu := range utxos {
			if cu.confs >= minconf && cu.rpc.Spendable {
				okUTXOs = append(okUTXOs, cu)
			}
		}

		for {
			// If there are none left, we don't have enough.
			if len(okUTXOs) == 0 {
				return false, nil
			}

			// Check if the largest output is too small.
			lastUTXO := okUTXOs[len(okUTXOs)-1]
			if !isEnoughWith(lastUTXO) {
				if err = addUTXO(lastUTXO); err != nil {
					return false, err
				}
				okUTXOs = okUTXOs[0 : len(okUTXOs)-1]
				continue
			}

			// We only need one then. Find it.
			idx := sort.Search(len(okUTXOs), func(i int) bool {
				return isEnoughWith(okUTXOs[i])
			})
			// No need to check idx == -1. We already verified that the last
			// utxo passes above.
			if err = addUTXO(okUTXOs[idx]); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// First try with confs>0.
	ok, err := tryUTXOs(1)
	if err != nil {
		return 0, 0, nil, nil, nil, err
	}

	// Fallback to allowing 0-conf outputs.
	if !ok {
		ok, err = tryUTXOs(0)
		if err != nil {
			return 0, 0, nil, nil, nil, err
		}
		if !ok {
			return 0, 0, nil, nil, nil, fmt.Errorf("not enough to cover requested funds. "+
				"%s DCR available in %d UTXOs", amount(sum), len(coins))
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
//   - 1 extra signed p2pkh-spending input. The split tx has the fundingCoins as
//     inputs now, but we'll add the input that spends the sized coin that will go
//     into the first swap
//   - 2 additional p2pkh outputs for the split tx sized output and change
//
// If the fees associated with this extra baggage are more than the excess
// amount that would be locked if a split transaction were not used, then the
// split transaction is pointless. This might be common, for instance, if an
// order is canceled partially filled, and then the remainder resubmitted. We
// would already have an output of just the right size, and that would be
// recognized here.
func (dcr *ExchangeWallet) split(value uint64, lots uint64, coins asset.Coins, inputsSize uint64,
	splitFeeRate, bumpedMaxRate uint64, nfo *dex.Asset) (asset.Coins, bool, error) {

	// Calculate the extra fees associated with the additional inputs, outputs,
	// and transaction overhead, and compare to the excess that would be locked.
	baggageFees := bumpedMaxRate * splitTxBaggage

	var coinSum uint64
	for _, coin := range coins {
		coinSum += coin.Value()
	}

	valStr := amount(value).String()

	excess := coinSum - calc.RequiredOrderFundsAlt(value, inputsSize, lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)

	if baggageFees > excess {
		dcr.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. %s > %s.",
			amount(baggageFees), amount(excess))
		dcr.log.Infof("Funding %s DCR order with coins %v worth %s", valStr, coins, amount(coinSum))
		return coins, false, nil
	}

	// Generate an address to receive the sized output. If mixing is enabled on
	// the wallet, generate the address from the external branch of the trading
	// account. The external branch is used so that if this split output isn't
	// spent, it won't be transferred to the unmixed account for re-mixing.
	// Instead, it'll simply be unlocked in the trading account and can thus be
	// used to fund future orders.
	cfg := dcr.config()
	addr, err := func() (stdaddr.Address, error) {
		if cfg.tradingAccount != "" {
			return dcr.wallet.ExternalAddress(dcr.ctx, cfg.tradingAccount)
		}
		return dcr.wallet.InternalAddress(dcr.ctx, cfg.primaryAcct)
	}()
	if err != nil {
		return nil, false, fmt.Errorf("error creating split transaction address: %w", err)
	}

	reqFunds := calc.RequiredOrderFundsAlt(value, dexdcr.P2PKHInputSize, lots, nfo.SwapSizeBase, nfo.SwapSize, bumpedMaxRate)

	dcr.fundingMtx.Lock()         // before generating the new output in sendCoins
	defer dcr.fundingMtx.Unlock() // after locking it (wallet and map)

	msgTx, sentVal, err := dcr.sendCoins(addr, coins, reqFunds, splitFeeRate, false)
	if err != nil {
		return nil, false, fmt.Errorf("error sending split transaction: %w", err)
	}

	if sentVal != reqFunds {
		dcr.log.Errorf("split - total sent %.8f does not match expected %.8f", toDCR(sentVal), toDCR(reqFunds))
	}

	op := newOutput(msgTx.CachedTxHash(), 0, sentVal, wire.TxTreeRegular)

	// Lock the funding coin.
	err = dcr.lockFundingCoins([]*fundingCoin{{
		op:   op,
		addr: addr.String(),
	}})
	if err != nil {
		dcr.log.Errorf("error locking funding coin from split transaction %s", op)
	}

	// Unlock the spent coins.
	_, err = dcr.returnCoins(coins)
	if err != nil {
		dcr.log.Errorf("error returning coins spent in split transaction %v", coins)
	}

	dcr.log.Infof("Funding %s DCR order with split output coin %v from original coins %v", valStr, op, coins)
	dcr.log.Infof("Sent split transaction %s to accommodate swap of size %s + fees = %s DCR",
		op.txHash(), valStr, amount(reqFunds))

	return asset.Coins{op}, true, nil
}

// lockFundingCoins locks the funding coins via RPC and stores them in the map.
// This function is not safe for concurrent use. The caller should lock
// dcr.fundingMtx.
func (dcr *ExchangeWallet) lockFundingCoins(fCoins []*fundingCoin) error {
	wireOPs := make([]*wire.OutPoint, 0, len(fCoins))
	for _, c := range fCoins {
		wireOPs = append(wireOPs, wire.NewOutPoint(c.op.txHash(), c.op.vout(), c.op.tree))
	}
	err := dcr.wallet.LockUnspent(dcr.ctx, false, wireOPs)
	if err != nil {
		return err
	}
	for _, c := range fCoins {
		dcr.fundingCoins[c.op.pt] = c
	}
	return nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a canceled
// order. Coins belonging to the tradingAcct, if configured, are transferred to
// the unmixed account with the exception of unspent split tx outputs which are
// kept in the tradingAcct and may later be used to fund future orders. If
// called with a nil slice, all coins are returned and none are moved to the
// unmixed account.
func (dcr *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	if unspents == nil { // not just empty to make this harder to do accidentally
		dcr.log.Debugf("Returning all coins.")
		dcr.fundingMtx.Lock()
		defer dcr.fundingMtx.Unlock()
		if err := dcr.wallet.LockUnspent(dcr.ctx, true, nil); err != nil {
			return err
		}
		dcr.fundingCoins = make(map[outPoint]*fundingCoin)
		return nil
	}
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}

	dcr.fundingMtx.Lock()
	returnedCoins, err := dcr.returnCoins(unspents)
	dcr.fundingMtx.Unlock()
	cfg := dcr.config()
	if err != nil || cfg.unmixedAccount == "" {
		return err
	}

	// If any of these coins belong to the trading account, transfer them to the
	// unmixed account to be re-mixed into the primary account before being
	// re-selected for funding future orders. This doesn't apply to unspent
	// split tx outputs, which should remain in the trading account and be
	// selected from there for funding future orders.
	var coinsToTransfer []asset.Coin
	for _, coin := range returnedCoins {
		if coin.addr == "" {
			txOut, err := dcr.wallet.UnspentOutput(dcr.ctx, coin.op.txHash(), coin.op.vout(), coin.op.tree)
			if err != nil {
				dcr.log.Errorf("wallet.UnspentOutput error for returned coin %s: %v", coin.op, err)
				continue
			}
			if len(txOut.Addresses) == 0 {
				dcr.log.Errorf("no address in gettxout response for returned coin %s", coin.op)
				continue
			}
			coin.addr = txOut.Addresses[0]
		}
		addrInfo, err := dcr.wallet.AddressInfo(dcr.ctx, coin.addr)
		if err != nil {
			dcr.log.Errorf("wallet.AddressInfo error for returned coin %s: %v", coin.op, err)
			continue
		}
		// Move this coin to the unmixed account if it was sent to the internal
		// branch of the trading account. This excludes unspent split tx outputs
		// which are sent to the external branch of the trading account.
		if addrInfo.Branch == acctInternalBranch && addrInfo.Account == cfg.tradingAccount {
			coinsToTransfer = append(coinsToTransfer, coin.op)
		}
	}

	if len(coinsToTransfer) > 0 {
		tx, totalSent, err := dcr.sendAll(coinsToTransfer, cfg.unmixedAccount)
		if err != nil {
			dcr.log.Errorf("unable to transfer unlocked swapped change from temp trading "+
				"account to unmixed account: %v", err)
		} else {
			dcr.log.Infof("Transferred %s from temp trading account to unmixed account in tx %s.",
				dcrutil.Amount(totalSent), tx.TxHash())
		}
	}

	return nil
}

// returnCoins unlocks coins and removes them from the fundingCoins map.
// Requires fundingMtx to be write-locked.
func (dcr *ExchangeWallet) returnCoins(unspents asset.Coins) ([]*fundingCoin, error) {
	if len(unspents) == 0 {
		return nil, fmt.Errorf("cannot return zero coins")
	}

	ops := make([]*wire.OutPoint, 0, len(unspents))
	fundingCoins := make([]*fundingCoin, 0, len(unspents))

	dcr.log.Debugf("returning coins %s", unspents)
	for _, unspent := range unspents {
		op, err := dcr.convertCoin(unspent)
		if err != nil {
			return nil, fmt.Errorf("error converting coin: %w", err)
		}
		ops = append(ops, op.wireOutPoint()) // op.tree may be wire.TxTreeUnknown, but that's fine since wallet.LockUnspent doesn't rely on it
		if fCoin, ok := dcr.fundingCoins[op.pt]; ok {
			fundingCoins = append(fundingCoins, fCoin)
		} else {
			dcr.log.Warnf("returning coin %s that is not cached as a funding coin", op)
			fundingCoins = append(fundingCoins, &fundingCoin{op: op})
		}
	}

	if err := dcr.wallet.LockUnspent(dcr.ctx, true, ops); err != nil {
		return nil, err
	}

	for _, fCoin := range fundingCoins {
		delete(dcr.fundingCoins, fCoin.op.pt)
	}

	return fundingCoins, nil
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (dcr *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[outPoint]bool)
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock() // stay locked until we update the map and lock them in the wallet
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		pt := newOutPoint(txHash, vout)
		fundingCoin, found := dcr.fundingCoins[pt]
		if found {
			coins = append(coins, fundingCoin.op)
			continue
		}
		notFound[pt] = true
	}
	if len(notFound) == 0 {
		return coins, nil
	}

	// Check locked outputs for not found coins.
	for _, acct := range dcr.fundingAccounts() {
		lockedOutputs, err := dcr.wallet.LockedOutputs(dcr.ctx, acct)
		if err != nil {
			return nil, err
		}
		for _, output := range lockedOutputs {
			txHash, err := chainhash.NewHashFromStr(output.Txid)
			if err != nil {
				return nil, fmt.Errorf("error decoding txid from rpc server %s: %w", output.Txid, err)
			}
			pt := newOutPoint(txHash, output.Vout)
			if !notFound[pt] {
				continue
			}
			txOut, err := dcr.wallet.UnspentOutput(dcr.ctx, txHash, output.Vout, output.Tree)
			if err != nil {
				return nil, fmt.Errorf("gettxout error for locked output %v: %w", pt.String(), err)
			}
			var address string
			if len(txOut.Addresses) > 0 {
				address = txOut.Addresses[0]
			}
			coin := newOutput(txHash, output.Vout, toAtoms(output.Amount), output.Tree)
			coins = append(coins, coin)
			dcr.fundingCoins[pt] = &fundingCoin{
				op:   coin,
				addr: address,
			}
			delete(notFound, pt)
			if len(notFound) == 0 {
				return coins, nil
			}
		}
	}

	// Some funding coins still not found after checking locked outputs.
	// Check wallet unspent outputs as last resort. Lock the coins if found.
	coinsToLock := make([]*wire.OutPoint, 0, len(notFound))
	for _, acct := range dcr.fundingAccounts() {
		unspents, err := dcr.wallet.Unspents(dcr.ctx, acct)
		if err != nil {
			return nil, err
		}
		for _, txout := range unspents {
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, fmt.Errorf("error decoding txid from rpc server %s: %w", txout.TxID, err)
			}
			pt := newOutPoint(txHash, txout.Vout)
			if !notFound[pt] {
				continue
			}
			coinsToLock = append(coinsToLock, wire.NewOutPoint(txHash, txout.Vout, txout.Tree))
			coin := newOutput(txHash, txout.Vout, toAtoms(txout.Amount), txout.Tree)
			coins = append(coins, coin)
			dcr.fundingCoins[pt] = &fundingCoin{
				op:   coin,
				addr: txout.Address,
			}
			delete(notFound, pt)
			if len(notFound) == 0 {
				break
			}
		}
	}

	// Return an error if some coins are still not found.
	if len(notFound) != 0 {
		ids := make([]string, 0, len(notFound))
		for pt := range notFound {
			ids = append(ids, pt.String())
		}
		return nil, fmt.Errorf("funding coins not found: %s", strings.Join(ids, ", "))
	}

	dcr.log.Debugf("Locking funding coins that were unlocked %v", coinsToLock)
	err := dcr.wallet.LockUnspent(dcr.ctx, false, coinsToLock)
	if err != nil {
		return nil, err
	}

	return coins, nil
}

// Swap sends the swaps in a single transaction. The Receipts returned can be
// used to refund a failed transaction. The Input coins are manually unlocked
// because they're not auto-unlocked by the wallet and therefore inaccurately
// included as part of the locked balance despite being spent.
func (dcr *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	var totalOut uint64
	// Start with an empty MsgTx.
	baseTx := wire.NewMsgTx()
	// Add the funding utxos.
	totalIn, err := dcr.addInputCoins(baseTx, swaps.Inputs)
	if err != nil {
		return nil, nil, 0, err
	}

	customCfg := new(swapOptions)
	err = config.Unmapify(swaps.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing swap options: %w", err)
	}

	contracts := make([][]byte, 0, len(swaps.Contracts))
	refundAddrs := make([]stdaddr.Address, 0, len(swaps.Contracts))
	// Add the contract outputs.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddrV2 is the address belonging to the key that will be
		// used to sign and refund a swap past its encoded refund locktime.
		revokeAddrV2, err := dcr.wallet.ExternalAddress(dcr.ctx, dcr.depositAccount())
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating revocation address: %w", err)
		}
		refundAddrs = append(refundAddrs, revokeAddrV2)
		// Create the contract, a P2SH redeem script.
		contractScript, err := dexdcr.MakeContract(contract.Address, revokeAddrV2.String(), contract.SecretHash, int64(contract.LockTime), dcr.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("unable to create pubkey script for address %s: %w", contract.Address, err)
		}
		contracts = append(contracts, contractScript)
		// Make the P2SH address and pubkey script.
		scriptAddr, err := stdaddr.NewAddressScriptHashV0(contractScript, dcr.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error encoding script address: %w", err)
		}
		p2shScriptVer, p2shScript := scriptAddr.PaymentScript()
		// Add the transaction output.
		txOut := newTxOut(int64(contract.Value), p2shScriptVer, p2shScript)
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

	feeRate, err := calcBumpedRate(swaps.FeeRate, customCfg.FeeBump)
	if err != nil {
		dcr.log.Errorf("ignoring invalid fee bump factor, %s: %v", float64PtrStr(customCfg.FeeBump), err)
	}

	// Add change, sign, and send the transaction.
	dcr.fundingMtx.Lock()         // before generating change output
	defer dcr.fundingMtx.Unlock() // hold until after returnCoins and lockFundingCoins(change)
	// Sign the tx but don't send the transaction yet until
	// the individual swap refund txs are prepared and signed.
	changeAcct := dcr.depositAccount()
	cfg := dcr.config()
	if swaps.LockChange && cfg.tradingAccount != "" {
		// Change will likely be used to fund more swaps, send to trading
		// account.
		changeAcct = cfg.tradingAccount
	}
	msgTx, change, changeAddr, fees, err := dcr.signTxAndAddChange(baseTx, feeRate, -1, changeAcct)
	if err != nil {
		return nil, nil, 0, err
	}

	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		output := newOutput(&txHash, uint32(i), contract.Value, wire.TxTreeRegular)
		signedRefundTx, err := dcr.refundTx(output.ID(), contracts[i], contract.Value, refundAddrs[i], swaps.FeeRate)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating refund tx: %w", err)
		}
		refundB, err := signedRefundTx.Bytes()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error serializing refund tx: %w", err)
		}
		receipts = append(receipts, &swapReceipt{
			output:       output,
			contract:     contracts[i],
			expiration:   time.Unix(int64(contract.LockTime), 0).UTC(),
			signedRefund: refundB,
		})
	}

	// Refund txs prepared and signed. Can now broadcast the swap(s).
	err = dcr.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}

	// Return spent outputs.
	_, err = dcr.returnCoins(swaps.Inputs)
	if err != nil {
		dcr.log.Errorf("error unlocking swapped coins", swaps.Inputs)
	}

	// Lock the change coin, if requested.
	if swaps.LockChange {
		dcr.log.Debugf("locking change coin %s", change)
		err = dcr.lockFundingCoins([]*fundingCoin{{
			op:   change,
			addr: changeAddr,
		}})
		if err != nil {
			dcr.log.Warnf("Failed to lock dcr change coin %s", change)
		}
	}

	// If change is nil, return a nil asset.Coin.
	var changeCoin asset.Coin
	if change != nil {
		changeCoin = change
	}
	return receipts, changeCoin, fees, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. FeeSuggestion is just a fallback if an internal estimate using
// the wallet's redeem confirm block target setting is not available.
func (dcr *ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx()
	var totalIn uint64
	var contracts [][]byte
	var addresses []stdaddr.Address
	for _, r := range form.Redemptions {
		if r.Spends == nil {
			return nil, nil, 0, fmt.Errorf("no audit info")
		}

		cinfo, err := convertAuditInfo(r.Spends, dcr.chainParams)
		if err != nil {
			return nil, nil, 0, err
		}

		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		contract := cinfo.contract
		_, receiver, _, secretHash, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error extracting swap addresses: %w", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, 0, fmt.Errorf("secret hash mismatch. %x != %x", checkSecretHash[:], secretHash)
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, contract)
		prevOut := cinfo.output.wireOutPoint()
		txIn := wire.NewTxIn(prevOut, int64(cinfo.output.value), []byte{})
		msgTx.AddTxIn(txIn)
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := msgTx.SerializeSize() + dexdcr.RedeemSwapSigScriptSize*len(form.Redemptions) + dexdcr.P2PKHOutputSize

	customCfg := new(redeemOptions)
	err := config.Unmapify(form.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing selected swap options: %w", err)
	}

	rawFeeRate := dcr.targetFeeRateWithFallback(dcr.config().redeemConfTarget, form.FeeSuggestion)
	feeRate, err := calcBumpedRate(rawFeeRate, customCfg.FeeBump)
	if err != nil {
		dcr.log.Errorf("calcBumpRate error: %v", err)
	}
	fee := feeRate * uint64(size)
	if fee > totalIn {
		// Double check that the fee bump isn't the issue.
		feeRate = rawFeeRate
		fee = feeRate * uint64(size)
		if fee > totalIn {
			return nil, nil, 0, fmt.Errorf("redeem tx not worth the fees")
		}
		dcr.log.Warnf("Ignoring fee bump (%v) resulting in fees > redemption", float64PtrStr(customCfg.FeeBump))
	}

	// Send the funds back to the exchange wallet.
	txOut, _, err := dcr.makeChangeOut(dcr.depositAccount(), totalIn-fee)
	if err != nil {
		return nil, nil, 0, err
	}
	// One last check for dust.
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, nil, 0, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign the inputs.
	for i, r := range form.Redemptions {
		contract := contracts[i]
		redeemSig, redeemPubKey, err := dcr.createSig(msgTx, i, contract, addresses[i])
		if err != nil {
			return nil, nil, 0, err
		}
		redeemSigScript, err := dexdcr.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return nil, nil, 0, err
		}
		msgTx.TxIn[i].SignatureScript = redeemSigScript
	}
	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := dcr.wallet.SendRawTransaction(dcr.ctx, msgTx, false)
	if err != nil {
		return nil, nil, 0, err
	}
	if *txHash != checkHash {
		return nil, nil, 0, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	coinIDs := make([]dex.Bytes, 0, len(form.Redemptions))
	for i := range form.Redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}

	return coinIDs, newOutput(txHash, 0, uint64(txOut.Value), wire.TxTreeRegular), fee, nil
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. A slice of pubkeys required to spend the Coin and a
// signature for each pubkey are returned.
func (dcr *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	op, err := dcr.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %w", err)
	}

	// First check if we have the funding coin cached. If so, grab the address
	// from there.
	dcr.fundingMtx.RLock()
	fCoin, found := dcr.fundingCoins[op.pt]
	dcr.fundingMtx.RUnlock()
	var addr string
	if found {
		addr = fCoin.addr
	} else {
		// Check if we can get the address from wallet.UnspentOutput.
		// op.tree may be wire.TxTreeUnknown but wallet.UnspentOutput is
		// able to deal with that and find the actual tree.
		txOut, err := dcr.wallet.UnspentOutput(dcr.ctx, op.txHash(), op.vout(), op.tree)
		if err != nil {
			dcr.log.Errorf("gettxout error for SignMessage coin %s: %v", op, err)
		} else if txOut != nil {
			if len(txOut.Addresses) != 1 {
				// TODO: SignMessage is usually called for coins selected by
				// FundOrder. Should consider rejecting/ignoring multisig ops
				// in FundOrder to prevent this SignMessage error from killing
				// order placements.
				return nil, nil, fmt.Errorf("multi-sig not supported")
			}
			addr = txOut.Addresses[0]
			found = true
		}
	}
	// Could also try the gettransaction endpoint, which is supposed to return
	// information about wallet transactions, but which (I think?) doesn't list
	// ssgen outputs.
	if !found {
		return nil, nil, fmt.Errorf("did not locate coin %s. is this a coin returned from Fund?", coin)
	}
	address, err := stdaddr.DecodeAddress(addr, dcr.chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("error decoding address: %w", err)
	}
	priv, err := dcr.wallet.AddressPrivKey(dcr.ctx, address)
	if err != nil {
		return nil, nil, err
	}
	defer priv.Zero()
	hash := chainhash.HashB(msg) // legacy servers will not accept this signature!
	signature := ecdsa.Sign(priv, hash)
	pubkeys = append(pubkeys, priv.PubKey().SerializeCompressed())
	sigs = append(sigs, signature.Serialize()) // DER format
	return pubkeys, sigs, nil
}

// AuditContract retrieves information about a swap contract from the provided
// txData if it represents a valid transaction that pays to the contract at the
// specified coinID. The txData may be empty to attempt retrieval of the
// transaction output from the network, but it is only ensured to succeed for a
// full node or, if the tx is confirmed, an SPV wallet. Normally the server
// should communicate this txData, and the caller can decide to require it. The
// ability to work with an empty txData is a convenience for recovery tools and
// testing, and it may change in the future if a GetTxData method is added for
// this purpose. Optionally, attempt is also made to broadcasted the txData to
// the blockchain network but it is not necessary that the broadcast succeeds
// since the contract may have already been broadcasted.
func (dcr *ExchangeWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// If no tx data is provided, attempt to get the required data (the txOut)
	// from the wallet. If this is a full node wallet, a simple gettxout RPC is
	// sufficient with no pkScript or "since" time. If this is an SPV wallet,
	// only a confirmed counterparty contract can be located, and only one
	// within ContractSearchLimit. As such, this mode of operation is not
	// intended for normal server-coordinated operation.
	var contractTx *wire.MsgTx
	var contractTxOut *wire.TxOut
	var txTree int8
	if len(txData) == 0 {
		// Fall back to gettxout, but we won't have the tx to rebroadcast.
		output, err := dcr.wallet.UnspentOutput(dcr.ctx, txHash, vout, wire.TxTreeUnknown)
		if err == nil {
			contractTxOut = output.TxOut
			txTree = output.Tree
		} else {
			// Next, try a block filters scan.
			scriptAddr, err := stdaddr.NewAddressScriptHashV0(contract, dcr.chainParams)
			if err != nil {
				return nil, fmt.Errorf("error encoding script address: %w", err)
			}
			_, pkScript := scriptAddr.PaymentScript()
			outFound, _, err := dcr.externalTxOutput(dcr.ctx, newOutPoint(txHash, vout),
				pkScript, time.Now().Add(-ContractSearchLimit))
			if err != nil {
				return nil, fmt.Errorf("error finding unspent contract: %s:%d : %w", txHash, vout, err)
			}
			contractTxOut = outFound.TxOut
			txTree = outFound.tree
		}
	} else {
		contractTx, err = msgTxFromBytes(txData)
		if err != nil {
			return nil, fmt.Errorf("invalid contract tx data: %w", err)
		}
		if err = blockchain.CheckTransactionSanity(contractTx, dcr.chainParams); err != nil {
			return nil, fmt.Errorf("invalid contract tx data: %w", err)
		}
		if checkHash := contractTx.TxHash(); checkHash != *txHash {
			return nil, fmt.Errorf("invalid contract tx data: expected hash %s, got %s", txHash, checkHash)
		}
		if int(vout) >= len(contractTx.TxOut) {
			return nil, fmt.Errorf("invalid contract tx data: no output at %d", vout)
		}
		contractTxOut = contractTx.TxOut[vout]
		txTree = determineTxTree(contractTx)
	}

	// Validate contract output.
	// Script must be P2SH, with 1 address and 1 required signature.
	scriptClass, addrs := stdscript.ExtractAddrs(contractTxOut.Version, contractTxOut.PkScript, dcr.chainParams)
	if scriptClass != stdscript.STScriptHash {
		return nil, fmt.Errorf("unexpected script class %d", scriptClass)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}
	// Compare the contract hash to the P2SH address.
	contractHash := dcrutil.Hash160(contract)
	addr := addrs[0]
	addrScript, err := dexdcr.AddressScript(addr)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(contractHash, addrScript) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addrScript)
	}

	// The counter-party should have broadcasted the contract tx but rebroadcast
	// just in case to ensure that the tx is sent to the network. Do not block
	// because this is not required and does not affect the audit result.
	if rebroadcast && contractTx != nil {
		go func() {
			if hashSent, err := dcr.wallet.SendRawTransaction(dcr.ctx, contractTx, true); err != nil {
				dcr.log.Debugf("Rebroadcasting counterparty contract %v (THIS MAY BE NORMAL): %v", txHash, err)
			} else if !hashSent.IsEqual(txHash) {
				dcr.log.Errorf("Counterparty contract %v was rebroadcast as %v!", txHash, hashSent)
			}
		}()
	}

	return &asset.AuditInfo{
		Coin:       newOutput(txHash, vout, uint64(contractTxOut.Value), txTree),
		Contract:   contract,
		SecretHash: secretHash,
		Recipient:  receiver.String(),
		Expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

func determineTxTree(msgTx *wire.MsgTx) int8 {
	// stake.DetermineTxType will produce correct results if we pass true for
	// isTreasuryEnabled regardless of whether the treasury vote has activated
	// or not.
	// The only possibility for wrong results is passing isTreasuryEnabled=false
	// _after_ the treasury vote activates - some stake tree votes may identify
	// as regular tree transactions.
	// Could try with isTreasuryEnabled false, then true and if neither comes up
	// as a stake transaction, then we infer regular, but that isn't necessary
	// as explained above.
	isTreasuryEnabled := true
	// Consider the automatic ticket revocations agenda NOT active. Specifying
	// true just adds the constraints that revocations must have an empty
	// signature script for its input and must have zero fee. Thus, false will
	// correctly identify consensus-validated transactions before OR after
	// activation of this agenda.
	isAutoRevocationsEnabled := false
	if stake.DetermineTxType(msgTx, isTreasuryEnabled, isAutoRevocationsEnabled) != stake.TxTypeRegular {
		return wire.TxTreeStake
	}
	return wire.TxTreeRegular
}

// lookupTxOutput attempts to find and return details for the specified output,
// first checking for an unspent output and if not found, checking wallet txs.
// Returns asset.CoinNotFoundError if the output is not found.
// NOTE: This method is only guaranteed to return results for outputs belonging
// to transactions that are tracked by the wallet, although full node wallets
// are able to look up non-wallet outputs that are unspent.
func (dcr *ExchangeWallet) lookupTxOutput(ctx context.Context, txHash *chainhash.Hash, vout uint32) (*wire.TxOut, uint32, bool, error) {
	// Check for an unspent output.
	output, err := dcr.wallet.UnspentOutput(ctx, txHash, vout, wire.TxTreeUnknown)
	if err == nil {
		return output.TxOut, output.Confirmations, false, nil
	} else if !errors.Is(err, asset.CoinNotFoundError) {
		return nil, 0, false, err
	}

	// Check wallet transactions.
	tx, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, 0, false, err // asset.CoinNotFoundError if not found
	}
	msgTx, err := msgTxFromHex(tx.Hex)
	if err != nil {
		return nil, 0, false, fmt.Errorf("invalid hex for tx %s: %v", txHash, err)
	}
	if int(vout) >= len(msgTx.TxOut) {
		return nil, 0, false, fmt.Errorf("tx %s has no output at %d", txHash, vout)
	}

	txOut := msgTx.TxOut[vout]
	confs := uint32(tx.Confirmations)

	// We have the requested output. Check if it is spent.
	if confs == 0 {
		// Only counts as spent if spent in a mined transaction,
		// unconfirmed tx outputs can't be spent in a mined tx.
		return txOut, confs, false, nil
	}

	if !dcr.wallet.SpvMode() {
		// A mined output that is not found by wallet.UnspentOutput
		// is spent if the wallet is connected to a full node.
		dcr.log.Debugf("Output %s:%d that was not reported as unspent is considered SPENT, spv mode = false.",
			txHash, vout)
		return txOut, confs, true, nil
	}

	// For SPV wallets, only consider the output spent if it pays to the
	// wallet because outputs that don't pay to the wallet may be unspent
	// but still not found by wallet.UnspentOutput.
	var outputPaysToWallet bool
	for _, details := range tx.Details {
		if details.Vout == vout {
			outputPaysToWallet = details.Category == wallet.CreditReceive.String()
			break
		}
	}
	if outputPaysToWallet {
		dcr.log.Debugf("Output %s:%d was not reported as unspent, pays to the wallet and is considered SPENT.",
			txHash, vout)
		return txOut, confs, true, nil
	}

	// Assume unspent even though the spend status is not really known.
	dcr.log.Debugf("Output %s:%d was not reported as unspent, does not pay to the wallet and is assumed UNSPENT.",
		txHash, vout)
	return txOut, confs, false, nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (dcr *ExchangeWallet) LocktimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	dcr.tipMtx.RLock()
	blockHash := dcr.currentTip.hash
	dcr.tipMtx.RUnlock()
	hdr, err := dcr.wallet.GetBlockHeader(ctx, blockHash)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("unable to retrieve the block header: %w", err)
	}
	return time.Unix(hdr.MedianTime, 0).After(contractExpiry), contractExpiry, nil
}

// FindRedemption watches for the input that spends the specified contract
// coin, and returns the spending input and the contract's secret key when it
// finds a spender.
//
// This method blocks until the redemption is found, an error occurs or the
// provided context is canceled.
func (dcr *ExchangeWallet) FindRedemption(ctx context.Context, coinID, _ dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot decode contract coin id: %w", err)
	}

	// Add this contract to the findRedemptionQueue before performing
	// initial redemption search (see below). The initial search done
	// below only checks tx inputs in mempool and blocks starting from
	// the block in which the contract coin is mined up till the current
	// best block (for mined contracts, that is).
	// Adding this contract to the findRedemptionQueue now makes it
	// possible to find the redemption if the contract is redeemed in a
	// later transaction. Additional redemption searches are triggered
	// for all contracts in the findRedemptionQueue whenever a new block
	// or a re-org is observed in the dcr.monitorBlocks goroutine.
	// This contract will be removed from the findRedemptionQueue when
	// the redemption is found or if the provided context is canceled
	// before the redemption is found.
	contractOutpoint := newOutPoint(txHash, vout)
	resultChan, contractBlock, err := dcr.queueFindRedemptionRequest(ctx, contractOutpoint)
	if err != nil {
		return nil, nil, err
	}

	// Run initial search for redemption. If this contract is unmined,
	// only scan mempool transactions as mempool contracts can only be
	// spent by another mempool tx. If the contract is mined, scan all
	// mined tx inputs starting from the block in which the contract is
	// mined, up till the current best block. If the redemption is not
	// found in that block range, proceed to check mempool.
	if contractBlock == nil {
		dcr.findRedemptionsInMempool([]outPoint{contractOutpoint})
	} else {
		dcr.tipMtx.RLock()
		bestBlock := dcr.currentTip
		dcr.tipMtx.RUnlock()
		dcr.findRedemptionsInBlockRange(contractBlock.height, bestBlock.height, []outPoint{contractOutpoint})
	}

	// Wait for a find redemption result or context cancellation.
	// If the context is cancelled during an active mempool or block
	// range search, the contract will be removed from the queue and
	// there will be no further redemption searches for the contract.
	// See findRedemptionsIn{Mempool,BlockRange} -> findRedemptionsInTx.
	// If there is no active redemption search for this contract and
	// the context is canceled while waiting for new blocks to search,
	// the context cancellation will be caught here and the contract
	// will be removed from queue to prevent further searches when new
	// blocks are observed.
	var result *findRedemptionResult
	select {
	case result = <-resultChan:
	case <-ctx.Done():
	}

	// If this contract is still in the findRedemptionQueue, remove from the queue
	// to prevent further redemption search attempts for this contract.
	dcr.findRedemptionMtx.Lock()
	delete(dcr.findRedemptionQueue, contractOutpoint)
	dcr.findRedemptionMtx.Unlock()

	// result would be nil if ctx is canceled or the result channel
	// is closed without data, which would happen if the redemption
	// search is aborted when this ExchangeWallet is shut down.
	if result != nil {
		return result.RedemptionCoinID, result.Secret, result.Err
	}
	return nil, nil, fmt.Errorf("aborted search for redemption of contract %s: %w",
		contractOutpoint, ctx.Err())
}

// queueFindRedemptionRequest extracts the contract hash and tx block (if mined)
// of the provided contract outpoint, creates a find redemption request and adds
// it to the findRedemptionQueue. Returns error if a find redemption request is
// already queued for the contract or if the contract hash or block info cannot
// be extracted.
func (dcr *ExchangeWallet) queueFindRedemptionRequest(ctx context.Context, contractOutpoint outPoint) (chan *findRedemptionResult, *block, error) {
	dcr.findRedemptionMtx.Lock()
	defer dcr.findRedemptionMtx.Unlock()

	if _, inQueue := dcr.findRedemptionQueue[contractOutpoint]; inQueue {
		return nil, nil, fmt.Errorf("duplicate find redemption request for %s", contractOutpoint.String())
	}
	txHash, vout := contractOutpoint.txHash, contractOutpoint.vout
	tx, err := dcr.wallet.GetTransaction(dcr.ctx, &txHash)
	if err != nil {
		return nil, nil, err
	}
	msgTx, err := msgTxFromHex(tx.Hex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid contract tx hex %s: %w", tx.Hex, err)
	}
	if int(vout) > len(msgTx.TxOut)-1 {
		return nil, nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
	}
	contractScript := msgTx.TxOut[vout].PkScript
	contractScriptVer := msgTx.TxOut[vout].Version
	if !stdscript.IsScriptHashScript(contractScriptVer, contractScript) {
		return nil, nil, fmt.Errorf("coin %s not a valid contract", contractOutpoint.String())
	}
	var contractBlock *block
	if tx.BlockHash != "" {
		blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid blockhash %s for contract %s: %w", tx.BlockHash, contractOutpoint.String(), err)
		}
		header, err := dcr.wallet.GetBlockHeader(dcr.ctx, blockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("error fetching block header %s for contract %s: %w",
				tx.BlockHash, contractOutpoint.String(), err)
		}
		contractBlock = &block{height: int64(header.Height), hash: blockHash}
	}

	resultChan := make(chan *findRedemptionResult, 1)
	dcr.findRedemptionQueue[contractOutpoint] = &findRedemptionReq{
		ctx:                     ctx,
		contractP2SHScript:      contractScript,
		contractOutputScriptVer: contractScriptVer,
		resultChan:              resultChan,
	}
	return resultChan, contractBlock, nil
}

// findRedemptionsInMempool attempts to find spending info for the specified
// contracts by searching every input of all txs in the mempool.
// If spending info is found for any contract, the contract is purged from the
// findRedemptionQueue and the contract's secret (if successfully parsed) or any
// error that occurs during parsing is returned to the redemption finder via the
// registered result chan.
func (dcr *ExchangeWallet) findRedemptionsInMempool(contractOutpoints []outPoint) {
	contractsCount := len(contractOutpoints)
	dcr.log.Debugf("finding redemptions for %d contracts in mempool", contractsCount)

	var totalFound, totalCanceled int
	logAbandon := func(reason string) {
		// Do not remove the contracts from the findRedemptionQueue
		// as they could be subsequently redeemed in some mined tx(s),
		// which would be captured when a new tip is reported.
		if totalFound+totalCanceled > 0 {
			dcr.log.Debugf("%d redemptions found, %d canceled out of %d contracts in mempool",
				totalFound, totalCanceled, contractsCount)
		}
		dcr.log.Errorf("abandoning mempool redemption search for %d contracts because of %s",
			contractsCount-totalFound-totalCanceled, reason)
	}

	mempooler, is := dcr.wallet.(Mempooler)
	if !is || dcr.wallet.SpvMode() {
		return
	}

	mempoolTxs, err := mempooler.GetRawMempool(dcr.ctx)
	if err != nil {
		logAbandon(fmt.Sprintf("error retrieving transactions: %v", err))
		return
	}

	for _, txHash := range mempoolTxs {
		tx, err := dcr.wallet.GetRawTransaction(dcr.ctx, txHash)
		if err != nil {
			logAbandon(fmt.Sprintf("getrawtransaction error for tx hash %v: %v", txHash, err))
			return
		}
		found, canceled := dcr.findRedemptionsInTx("mempool", tx, contractOutpoints)
		totalFound += found
		totalCanceled += canceled
		if totalFound+totalCanceled == contractsCount {
			break
		}
	}

	dcr.log.Debugf("%d redemptions found, %d canceled out of %d contracts in mempool",
		totalFound, totalCanceled, contractsCount)
}

// findRedemptionsInBlockRange attempts to find spending info for the specified
// contracts by checking the cfilters of each block in the provided range for
// likely inclusion of ANY of the specified contracts' P2SH script. If a block's
// cfilters reports possible inclusion of ANY of the contracts' P2SH script,
// all inputs of the matching block's txs are checked to determine if any of the
// inputs spends any of the provided contracts.
// If spending info is found for any contract, the contract is purged from the
// findRedemptionQueue and the contract's secret (if successfully parsed) or any
// error that occurs during parsing is returned to the redemption finder via the
// registered result chan.
// If spending info is not found for any of these contracts after checking the
// specified block range, a mempool search is triggered to attempt finding unmined
// redemptions for the remaining contracts.
// NOTE:
// Any error encountered while checking a block's cfilters or fetching a matching
// block's txs compromises the redemption search for this set of contracts because
// subsequent attempts to find these contracts' redemption will not repeat any
// block in the specified range unless the contracts are first removed from the
// findRedemptionQueue. Thus, any such error will cause this set of contracts to
// be purged from the findRedemptionQueue. The error will be propagated to the
// redemption finder(s) and these may re-call dcr.FindRedemption to restart find
// redemption attempts for any of these contracts.
func (dcr *ExchangeWallet) findRedemptionsInBlockRange(startBlockHeight, endBlockHeight int64, contractOutpoints []outPoint) {
	totalContracts := len(contractOutpoints)
	dcr.log.Debugf("finding redemptions for %d contracts in blocks %d - %d",
		totalContracts, startBlockHeight, endBlockHeight)

	var lastScannedBlockHeight int64
	var totalFound, totalCanceled int

rangeBlocks:
	for blockHeight := startBlockHeight; blockHeight <= endBlockHeight; blockHeight++ {
		// Get the hash for this block.
		blockHash, err := dcr.wallet.GetBlockHash(dcr.ctx, blockHeight)
		if err != nil { // unable to get block hash is a fatal error
			err = fmt.Errorf("unable to get hash for block %d: %w", blockHeight, err)
			dcr.fatalFindRedemptionsError(err, contractOutpoints)
			return
		}

		// Combine the p2sh scripts for all contracts (excluding contracts whose redemption
		// have been found) to check against this block's cfilters.
		dcr.findRedemptionMtx.RLock()
		contractP2SHScripts := make([][]byte, 0)
		for _, contractOutpoint := range contractOutpoints {
			if req, stillInQueue := dcr.findRedemptionQueue[contractOutpoint]; stillInQueue {
				contractP2SHScripts = append(contractP2SHScripts, req.contractP2SHScript)
			}
		}
		dcr.findRedemptionMtx.RUnlock()

		bingo, err := dcr.wallet.MatchAnyScript(dcr.ctx, blockHash, contractP2SHScripts)
		if err != nil { // error retrieving a block's cfilters is a fatal error
			err = fmt.Errorf("MatchAnyScript error for block %d (%s): %w", blockHeight, blockHash, err)
			dcr.fatalFindRedemptionsError(err, contractOutpoints)
			return
		}

		if !bingo {
			lastScannedBlockHeight = blockHeight
			continue // block does not reference any of these contracts, continue to next block
		}

		// Pull the block info to confirm if any of its inputs spends a contract of interest.
		blk, err := dcr.wallet.GetBlock(dcr.ctx, blockHash)
		if err != nil { // error pulling a matching block's transactions is a fatal error
			err = fmt.Errorf("error retrieving transactions for block %d (%s): %w",
				blockHeight, blockHash, err)
			dcr.fatalFindRedemptionsError(err, contractOutpoints)
			return
		}

		lastScannedBlockHeight = blockHeight
		scanPoint := fmt.Sprintf("block %d", blockHeight)
		for _, tx := range append(blk.Transactions, blk.STransactions...) {
			found, canceled := dcr.findRedemptionsInTx(scanPoint, tx, contractOutpoints)
			totalFound += found
			totalCanceled += canceled
			if totalFound+totalCanceled == totalContracts {
				break rangeBlocks
			}
		}
	}

	dcr.log.Debugf("%d redemptions found, %d canceled out of %d contracts in blocks %d to %d",
		totalFound, totalCanceled, totalContracts, startBlockHeight, lastScannedBlockHeight)

	// Search for redemptions in mempool if there are yet unredeemed
	// contracts after searching this block range.
	pendingContractsCount := totalContracts - totalFound - totalCanceled
	if pendingContractsCount > 0 {
		dcr.findRedemptionMtx.RLock()
		pendingContracts := make([]outPoint, 0, pendingContractsCount)
		for _, contractOutpoint := range contractOutpoints {
			if _, pending := dcr.findRedemptionQueue[contractOutpoint]; pending {
				pendingContracts = append(pendingContracts, contractOutpoint)
			}
		}
		dcr.findRedemptionMtx.RUnlock()
		dcr.findRedemptionsInMempool(pendingContracts)
	}
}

// findRedemptionsInTx checks if any input of the passed tx spends any of the
// specified contract outpoints. If spending info is found for any contract, the
// contract's secret or any error encountered while trying to parse the secret
// is returned to the redemption finder via the registered result chan; and the
// contract is purged from the findRedemptionQueue.
// Returns the number of redemptions found and canceled.
func (dcr *ExchangeWallet) findRedemptionsInTx(scanPoint string, tx *wire.MsgTx, contractOutpoints []outPoint) (found, cancelled int) {
	dcr.findRedemptionMtx.Lock()
	defer dcr.findRedemptionMtx.Unlock()

	redeemTxHash := tx.TxHash()

	for _, contractOutpoint := range contractOutpoints {
		req, exists := dcr.findRedemptionQueue[contractOutpoint]
		if !exists {
			continue // no find request for this outpoint (impossible now?)
		}
		if req.canceled() {
			cancelled++
			delete(dcr.findRedemptionQueue, contractOutpoint)
			continue // this find request has been cancelled
		}

		for i, txIn := range tx.TxIn {
			prevOut := &txIn.PreviousOutPoint
			if prevOut.Index != contractOutpoint.vout || prevOut.Hash != contractOutpoint.txHash {
				continue // input doesn't redeem this contract, check next input
			}
			found++

			scriptHash := dexdcr.ExtractScriptHash(req.contractOutputScriptVer, req.contractP2SHScript)
			secret, err := dexdcr.FindKeyPush(req.contractOutputScriptVer, txIn.SignatureScript, scriptHash, dcr.chainParams)
			if err != nil {
				dcr.log.Errorf("Error parsing contract secret for %s from tx input %s:%d in %s: %v",
					contractOutpoint.String(), redeemTxHash, i, scanPoint, err)
				req.resultChan <- &findRedemptionResult{
					Err: err,
				}
			} else {
				dcr.log.Infof("Redemption for contract %s found in tx input %s:%d in %s",
					contractOutpoint.String(), redeemTxHash, i, scanPoint)
				req.resultChan <- &findRedemptionResult{
					RedemptionCoinID: toCoinID(&redeemTxHash, uint32(i)),
					Secret:           secret,
				}
			}

			delete(dcr.findRedemptionQueue, contractOutpoint)
			break // stop checking inputs for this contract
		}
	}

	return
}

// fatalFindRedemptionsError should be called when an error occurs that prevents
// redemption search for the specified contracts from continuing reliably. The
// error will be propagated to the seeker(s) of these contracts' redemptions via
// the registered result channels and the contracts will be removed from the
// findRedemptionQueue.
func (dcr *ExchangeWallet) fatalFindRedemptionsError(err error, contractOutpoints []outPoint) {
	dcr.findRedemptionMtx.Lock()
	dcr.log.Debugf("stopping redemption search for %d contracts in queue: %v", len(contractOutpoints), err)
	for _, contractOutpoint := range contractOutpoints {
		req, exists := dcr.findRedemptionQueue[contractOutpoint]
		if !exists {
			continue
		}
		req.resultChan <- &findRedemptionResult{
			Err: err,
		}
		delete(dcr.findRedemptionQueue, contractOutpoint)
	}
	dcr.findRedemptionMtx.Unlock()
}

// Refund refunds a contract. This can only be used after the time lock has
// expired. This MUST return an asset.CoinNotFoundError error if the coin is
// spent. If the provided fee rate is zero, an internal estimate will be used,
// otherwise it will be used directly, but this behavior may change.
// NOTE: The contract cannot be retrieved from the unspent coin info as the
// wallet does not store it, even though it was known when the init transaction
// was created. The client should store this information for persistence across
// sessions.
func (dcr *ExchangeWallet) Refund(coinID, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	// Caller should provide a non-zero fee rate, so we could just do
	// dcr.feeRateWithFallback(feeRate), but be permissive for now.
	if feeRate == 0 {
		feeRate = dcr.targetFeeRateWithFallback(2, 0)
	}
	msgTx, err := dcr.refundTx(coinID, contract, 0, nil, feeRate)
	if err != nil {
		return nil, fmt.Errorf("error creating refund tx: %w", err)
	}

	checkHash := msgTx.TxHash()
	refundHash, err := dcr.wallet.SendRawTransaction(dcr.ctx, msgTx, false)
	if err != nil {
		return nil, err
	}
	if *refundHash != checkHash {
		return nil, fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", checkHash, *refundHash)
	}
	return toCoinID(refundHash, 0), nil
}

// refundTx crates and signs a contract's refund transaction. If refundAddr is
// not supplied, one will be requested from the wallet. If val is not supplied
// it will be retrieved with gettxout.
func (dcr *ExchangeWallet) refundTx(coinID, contract dex.Bytes, val uint64, refundAddr stdaddr.Address, feeRate uint64) (*wire.MsgTx, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Grab the output, make sure it's unspent and get the value if not supplied.
	if val == 0 {
		utxo, _, spent, err := dcr.lookupTxOutput(dcr.ctx, txHash, vout)
		if err != nil {
			return nil, fmt.Errorf("error finding unspent contract: %w", err)
		}
		if utxo == nil {
			return nil, asset.CoinNotFoundError
		}
		if spent {
			// Refund MUST signal to caller that it is spent via
			// asset.CoinNotFoundError so that it knows to begin looking for the
			// counterparty's redeem and move on to redeem too.
			return nil, fmt.Errorf("contract %s:%d is spent (%w)", txHash, vout, asset.CoinNotFoundError)
		}
		val = uint64(utxo.Value)
	}
	sender, _, lockTime, _, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// Create the transaction that spends the contract.
	msgTx := wire.NewMsgTx()
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout, wire.TxTreeRegular)
	txIn := wire.NewTxIn(prevOut, int64(val), []byte{})
	// Enable the OP_CHECKLOCKTIMEVERIFY opcode to be used.
	//
	// https://github.com/decred/dcrd/blob/8f5270b707daaa1ecf24a1ba02b3ff8a762674d3/txscript/opcode.go#L981-L998
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.
	size := msgTx.SerializeSize() + dexdcr.RefundSigScriptSize + dexdcr.P2PKHOutputSize
	fee := feeRate * uint64(size)
	if fee > val {
		return nil, fmt.Errorf("refund tx not worth the fees")
	}

	if refundAddr == nil {
		refundAddr, err = dcr.wallet.ExternalAddress(dcr.ctx, dcr.depositAccount())
		if err != nil {
			return nil, fmt.Errorf("error getting new address from the wallet: %w", err)
		}
	}
	pkScriptVer, pkScript := refundAddr.PaymentScript()
	txOut := newTxOut(int64(val-fee), pkScriptVer, pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := dcr.createSig(msgTx, 0, contract, sender)
	if err != nil {
		return nil, err
	}
	redeemSigScript, err := dexdcr.RefundP2SHContract(contract, refundSig, refundPubKey)
	if err != nil {
		return nil, err
	}
	txIn.SignatureScript = redeemSigScript
	return msgTx, nil
}

// DepositAddress returns an address for depositing funds into the exchange
// wallet.
func (dcr *ExchangeWallet) DepositAddress() (string, error) {
	addr, err := dcr.wallet.ExternalAddress(dcr.ctx, dcr.depositAccount())
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// RedemptionAddress gets an address for use in redeeming the counterparty's
// swap. This would be included in their swap initialization.
func (dcr *ExchangeWallet) RedemptionAddress() (string, error) {
	return dcr.DepositAddress()
}

// NewAddress returns a new address from the wallet. This satisfies the
// NewAddresser interface.
func (dcr *ExchangeWallet) NewAddress() (string, error) {
	return dcr.DepositAddress()
}

// Unlock unlocks the exchange wallet.
func (dcr *ExchangeWallet) Unlock(pw []byte) error {
	// We must unlock all accounts, including any unmixed account, which is used
	// to supply keys to the refund path of the swap contract script.
	for _, acct := range dcr.allAccounts() {
		unlocked, err := dcr.wallet.AccountUnlocked(dcr.ctx, acct)
		if err != nil {
			return err
		}
		if unlocked {
			continue // attempt to unlock the other account
		}
		err = dcr.wallet.UnlockAccount(dcr.ctx, pw, acct)
		if err != nil {
			return err
		}
	}
	return nil
}

// Lock locks the exchange wallet.
func (dcr *ExchangeWallet) Lock() error {
	cfg := dcr.config()
	if cfg.unmixedAccount != "" {
		return nil // don't lock if mixing is enabled
	}
	return dcr.wallet.LockAccount(dcr.ctx, cfg.primaryAcct)
}

// Locked will be true if the wallet is currently locked.
// Q: why are we ignoring RPC errors in this?
func (dcr *ExchangeWallet) Locked() bool {
	for _, acct := range dcr.allAccounts() {
		unlocked, err := dcr.wallet.AccountUnlocked(dcr.ctx, acct)
		if err != nil {
			dcr.log.Errorf("error checking account lock status %v", err)
			unlocked = false // assume wallet is unlocked?
		}
		if !unlocked {
			return true // Locked is true if any of the funding accounts is locked.
		}
	}
	return false
}

// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
// pay the registration fee using the provided feeRate.
func (dcr *ExchangeWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	cfg := dcr.config()
	const inputCount = 5 // buffer so this estimate is higher than actual reg tx fee.
	if feeRate == 0 || feeRate > cfg.feeRateLimit {
		feeRate = cfg.fallbackFeeRate
	}
	return (dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2 + inputCount*dexdcr.P2PKHInputSize) * feeRate
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
// Withdraw satisfies asset.Withdrawer.
func (dcr *ExchangeWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	addr, err := stdaddr.DecodeAddress(address, dcr.chainParams)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %s", address)
	}
	msgTx, sentVal, err := dcr.withdraw(addr, value, dcr.feeRateWithFallback(feeRate))
	if err != nil {
		return nil, err
	}
	return newOutput(msgTx.CachedTxHash(), 0, sentVal, wire.TxTreeRegular), nil
}

// Send sends the exact value to the specified address. This is different from
// Withdraw, which subtracts the tx fees from the amount sent. feeRate is in
// units of atoms/byte.
func (dcr *ExchangeWallet) Send(address string, value, feeRate uint64) (asset.Coin, error) {
	addr, err := stdaddr.DecodeAddress(address, dcr.chainParams)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %s", address)
	}
	msgTx, sentVal, err := dcr.sendToAddress(addr, value, dcr.feeRateWithFallback(feeRate))
	if err != nil {
		return nil, err
	}
	return newOutput(msgTx.CachedTxHash(), 0, sentVal, wire.TxTreeRegular), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (dcr *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// SwapConfirmations gets the number of confirmations and the spend status for
// the specified swap. The contract and matchTime are provided so that wallets
// may search for the coin using light filters.
//
// For a non-SPV wallet, if the swap appears spent but it cannot be located in a
// block with a cfilters scan, this will return asset.CoinNotFoundError. For SPV
// wallets, it is not an error if the transaction cannot be located SPV wallets
// cannot see non-wallet transactions until they are mined.
//
// If the coin is located, but recognized as spent, no error is returned.
func (dcr *ExchangeWallet) SwapConfirmations(ctx context.Context, coinID, contract dex.Bytes, matchTime time.Time) (confs uint32, spent bool, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return 0, false, err
	}

	ctx, cancel := context.WithTimeout(ctx, confCheckTimeout)
	defer cancel()

	// Check if we can find the contract onchain without using cfilters.
	_, confs, spent, err = dcr.lookupTxOutput(ctx, txHash, vout)
	if err == nil {
		return confs, spent, nil
	} else if !errors.Is(err, asset.CoinNotFoundError) {
		return 0, false, err
	}

	// Prepare the pkScript to find the contract output using block filters.
	scriptAddr, err := stdaddr.NewAddressScriptHashV0(contract, dcr.chainParams)
	if err != nil {
		return 0, false, fmt.Errorf("error encoding script address: %w", err)
	}
	_, p2shScript := scriptAddr.PaymentScript()

	// Find the contract and its spend status using block filters.
	dcr.log.Debugf("Contract output %s:%d NOT yet found, will attempt finding it with block filters.", txHash, vout)
	confs, spent, err = dcr.lookupTxOutWithBlockFilters(ctx, newOutPoint(txHash, vout), p2shScript, matchTime)
	// Don't trouble the caller if we're using an SPV wallet and the transaction
	// cannot be located.
	if errors.Is(err, asset.CoinNotFoundError) && dcr.wallet.SpvMode() {
		dcr.log.Debugf("SwapConfirmations - cfilters scan did not find %v:%d. "+
			"Assuming in mempool.", txHash, vout)
		err = nil
	}
	return confs, spent, err
}

// RegFeeConfirmations gets the number of confirmations for the specified
// output.
func (dcr *ExchangeWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return 0, err
	}
	tx, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err != nil {
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// addInputCoins adds inputs to the MsgTx to spend the specified outputs.
func (dcr *ExchangeWallet) addInputCoins(msgTx *wire.MsgTx, coins asset.Coins) (uint64, error) {
	var totalIn uint64
	for _, coin := range coins {
		op, err := dcr.convertCoin(coin)
		if err != nil {
			return 0, err
		}
		if op.value == 0 {
			return 0, fmt.Errorf("zero-valued output detected for %s:%d", op.txHash(), op.vout())
		}
		if op.tree == wire.TxTreeUnknown { // Set the correct prevout tree if unknown.
			unspentPrevOut, err := dcr.wallet.UnspentOutput(dcr.ctx, op.txHash(), op.vout(), op.tree)
			if err != nil {
				return 0, fmt.Errorf("unable to determine tree for prevout %s: %v", op.pt, err)
			}
			op.tree = unspentPrevOut.Tree
		}
		totalIn += op.value
		prevOut := op.wireOutPoint()
		txIn := wire.NewTxIn(prevOut, int64(op.value), []byte{})
		msgTx.AddTxIn(txIn)
	}
	return totalIn, nil
}

func (dcr *ExchangeWallet) shutdown() {
	// Close all open channels for contract redemption searches
	// to prevent leakages and ensure goroutines that are started
	// to wait on these channels end gracefully.
	dcr.findRedemptionMtx.Lock()
	for contractOutpoint, req := range dcr.findRedemptionQueue {
		close(req.resultChan)
		delete(dcr.findRedemptionQueue, contractOutpoint)
	}
	dcr.findRedemptionMtx.Unlock()

	// Disconnect the wallet. For rpc wallets, this shuts down
	// the rpcclient.Client.
	if dcr.wallet != nil {
		dcr.wallet.Disconnect()
	}
}

// SyncStatus is information about the blockchain sync status.
func (dcr *ExchangeWallet) SyncStatus() (bool, float32, error) {
	return dcr.wallet.SyncStatus(dcr.ctx)
}

// Combines the RPC type with the spending input information.
type compositeUTXO struct {
	rpc   *walletjson.ListUnspentResult
	input *dexdcr.SpendInfo
	confs int64
	// TODO: consider including isDexChange bool for consumer
}

// parseUTXOs constructs and returns a list of compositeUTXOs from the provided
// set of RPC utxos, including basic information required to spend each rpc utxo.
// The returned list is sorted by ascending value.
func (dcr *ExchangeWallet) parseUTXOs(unspents []*walletjson.ListUnspentResult) ([]*compositeUTXO, error) {
	utxos := make([]*compositeUTXO, 0, len(unspents))
	for _, utxo := range unspents {
		if !utxo.Spendable {
			continue
		}
		scriptPK, err := hex.DecodeString(utxo.ScriptPubKey)
		if err != nil {
			return nil, fmt.Errorf("error decoding pubkey script for %s, script = %s: %w", utxo.TxID, utxo.ScriptPubKey, err)
		}
		redeemScript, err := hex.DecodeString(utxo.RedeemScript)
		if err != nil {
			return nil, fmt.Errorf("error decoding redeem script for %s, script = %s: %w", utxo.TxID, utxo.RedeemScript, err)
		}

		// NOTE: listunspent does not indicate script version, so for the
		// purposes of our funding coins, we are going to assume 0.
		nfo, err := dexdcr.InputInfo(0, scriptPK, redeemScript, dcr.chainParams)
		if err != nil {
			if errors.Is(err, dex.UnsupportedScriptError) {
				continue
			}
			return nil, fmt.Errorf("error reading asset info: %w", err)
		}
		if nfo.ScriptType == dexdcr.ScriptUnsupported || nfo.NonStandardScript {
			// InputInfo sets NonStandardScript for P2SH with non-standard
			// redeem scripts. Don't return these since they cannot fund
			// arbitrary txns.
			continue
		}
		utxos = append(utxos, &compositeUTXO{
			rpc:   utxo,
			input: nfo,
			confs: utxo.Confirmations,
		})
	}
	// Sort in ascending order by amount (smallest first).
	sort.Slice(utxos, func(i, j int) bool { return utxos[i].rpc.Amount < utxos[j].rpc.Amount })
	return utxos, nil
}

// lockedAtoms is the total value of locked outputs, as locked with LockUnspent.
func (dcr *ExchangeWallet) lockedAtoms(acct string) (uint64, error) {
	lockedOutpoints, err := dcr.wallet.LockedOutputs(dcr.ctx, acct)
	if err != nil {
		return 0, err
	}
	var sum uint64
	for _, op := range lockedOutpoints {
		sum += toAtoms(op.Amount)
	}
	return sum, nil
}

// convertCoin converts the asset.Coin to an output whose tree may be unknown.
// Use wallet.UnspentOutput to determine the output tree where necessary.
func (dcr *ExchangeWallet) convertCoin(coin asset.Coin) (*output, error) {
	op, _ := coin.(*output)
	if op != nil {
		return op, nil
	}
	txHash, vout, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, err
	}
	return newOutput(txHash, vout, coin.Value(), wire.TxTreeUnknown), nil
}

// withdraw sends the amount to the address. Fees are subtracted from the
// sent value.
func (dcr *ExchangeWallet) withdraw(addr stdaddr.Address, val, feeRate uint64) (*wire.MsgTx, uint64, error) {
	if val == 0 {
		return nil, 0, fmt.Errorf("cannot withdraw value = 0")
	}
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		return sum+toAtoms(unspent.rpc.Amount) >= val
	}
	coins, _, _, _, err := dcr.fund(enough)
	if err != nil {
		return nil, 0, fmt.Errorf("unable to withdraw %s DCR to address %s with feeRate %d atoms/byte: %w",
			amount(val), addr, feeRate, err)
	}

	msgTx, sentVal, err := dcr.sendCoins(addr, coins, val, feeRate, true)
	if err != nil {
		if _, retErr := dcr.returnCoins(coins); retErr != nil {
			dcr.log.Errorf("Failed to unlock coins: %v", retErr)
		}
		return nil, 0, err
	}
	return msgTx, sentVal, nil
}

// sendToAddress sends an exact amount to an address. Transaction fees will be
// in addition to the sent amount, and the output will be the zeroth output.
// TODO: Just use the sendtoaddress rpc since dcrwallet respects locked utxos!
func (dcr *ExchangeWallet) sendToAddress(addr stdaddr.Address, amt, feeRate uint64) (*wire.MsgTx, uint64, error) {
	baseSize := uint32(dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2) // may be extra if change gets omitted (see signTxAndAddChange)
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		txFee := uint64(baseSize+size+unspent.input.Size()) * feeRate
		return sum+toAtoms(unspent.rpc.Amount) >= amt+txFee
	}
	coins, _, _, _, err := dcr.fund(enough)
	if err != nil {
		return nil, 0, fmt.Errorf("Unable to send %s DCR with fee rate of %d atoms/byte: %w",
			amount(amt), feeRate, err)
	}

	msgTx, sentVal, err := dcr.sendCoins(addr, coins, amt, feeRate, false)
	if err != nil {
		if _, retErr := dcr.returnCoins(coins); retErr != nil {
			dcr.log.Errorf("Failed to unlock coins: %v", retErr)
		}
		return nil, 0, err
	}
	return msgTx, sentVal, nil
}

// sendCoins sends the amount to the address as the zeroth output, spending the
// specified coins. If subtract is true, the transaction fees will be taken from
// the sent value, otherwise it will taken from the change output. If there is
// change, it will be at index 1.
func (dcr *ExchangeWallet) sendCoins(addr stdaddr.Address, coins asset.Coins, val, feeRate uint64, subtract bool) (*wire.MsgTx, uint64, error) {
	baseTx := wire.NewMsgTx()
	_, err := dcr.addInputCoins(baseTx, coins)
	if err != nil {
		return nil, 0, err
	}
	payScriptVer, payScript := addr.PaymentScript()
	txOut := newTxOut(int64(val), payScriptVer, payScript)
	baseTx.AddTxOut(txOut)

	var feeSource int32 // subtract from vout 0
	if !subtract {
		feeSource = -1 // subtract from change
	}

	tx, err := dcr.sendWithReturn(baseTx, feeRate, feeSource)
	if err != nil {
		return nil, 0, err
	}
	return tx, uint64(tx.TxOut[0].Value), err
}

// sendAll sends the maximum sendable amount (total input amount minus fees) to
// the provided account as a single output, spending the specified coins.
func (dcr *ExchangeWallet) sendAll(coins asset.Coins, destAcct string) (*wire.MsgTx, uint64, error) {
	addr, err := dcr.wallet.InternalAddress(dcr.ctx, destAcct)
	if err != nil {
		return nil, 0, err
	}

	baseTx := wire.NewMsgTx()
	totalIn, err := dcr.addInputCoins(baseTx, coins)
	if err != nil {
		return nil, 0, err
	}
	payScriptVer, payScript := addr.PaymentScript()
	txOut := newTxOut(int64(totalIn), payScriptVer, payScript)
	baseTx.AddTxOut(txOut)

	feeRate := dcr.targetFeeRateWithFallback(2, 0)
	tx, err := dcr.sendWithReturn(baseTx, feeRate, 0) // subtract from vout 0
	return tx, uint64(txOut.Value), err
}

// newTxOut returns a new transaction output with the given parameters.
func newTxOut(amount int64, pkScriptVer uint16, pkScript []byte) *wire.TxOut {
	return &wire.TxOut{
		Value:    amount,
		Version:  pkScriptVer,
		PkScript: pkScript,
	}
}

// msgTxFromHex creates a wire.MsgTx by deserializing the hex transaction.
func msgTxFromHex(txHex string) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx()
	if err := msgTx.Deserialize(hex.NewDecoder(strings.NewReader(txHex))); err != nil {
		return nil, err
	}
	return msgTx, nil
}

// msgTxFromBytes creates a wire.MsgTx by deserializing the transaction bytes.
func msgTxFromBytes(txB []byte) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx()
	if err := msgTx.Deserialize(bytes.NewReader(txB)); err != nil {
		return nil, err
	}
	return msgTx, nil
}

func msgTxToHex(msgTx *wire.MsgTx) (string, error) {
	b, err := msgTx.Bytes()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (dcr *ExchangeWallet) makeChangeOut(changeAcct string, val uint64) (*wire.TxOut, stdaddr.Address, error) {
	changeAddr, err := dcr.wallet.InternalAddress(dcr.ctx, changeAcct)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %w", err)
	}
	changeScriptVersion, changeScript := changeAddr.PaymentScript()
	return newTxOut(int64(val), changeScriptVersion, changeScript), changeAddr, nil
}

// sendWithReturn sends the unsigned transaction, adding a change output unless
// the amount is dust. subtractFrom indicates the output from which fees should
// be subtracted, where -1 indicates fees should come out of a change output.
func (dcr *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx, feeRate uint64, subtractFrom int32) (*wire.MsgTx, error) {
	signedTx, _, _, _, err := dcr.signTxAndAddChange(baseTx, feeRate, subtractFrom, dcr.depositAccount())
	if err != nil {
		return nil, err
	}

	err = dcr.broadcastTx(signedTx)
	return signedTx, err
}

// signTxAndAddChange signs the passed msgTx, adding a change output that pays
// an address from the specified changeAcct, unless the change amount is dust.
// subtractFrom indicates the output from which fees should be subtracted, where
// -1 indicates fees should come out of a change output.
func (dcr *ExchangeWallet) signTxAndAddChange(baseTx *wire.MsgTx, feeRate uint64, subtractFrom int32, changeAcct string) (*wire.MsgTx, *output, string, uint64, error) {
	// Sign the transaction to get an initial size estimate and calculate
	// whether a change output would be dust.
	sigCycles := 1
	msgTx, err := dcr.wallet.SignRawTransaction(dcr.ctx, baseTx)
	if err != nil {
		return nil, nil, "", 0, err
	}

	totalIn, totalOut, remaining, _, size := reduceMsgTx(msgTx)
	if totalIn < totalOut {
		return nil, nil, "", 0, fmt.Errorf("unbalanced transaction")
	}

	minFee := feeRate * size
	if subtractFrom == -1 && minFee > remaining {
		return nil, nil, "", 0, fmt.Errorf("not enough funds to cover minimum fee rate of %v atoms/B: %s > %s remaining",
			feeRate, amount(minFee), amount(remaining))
	}
	if int(subtractFrom) >= len(baseTx.TxOut) {
		return nil, nil, "", 0, fmt.Errorf("invalid subtractFrom output %d for tx with %d outputs",
			subtractFrom, len(baseTx.TxOut))
	}

	// Add a change output if there is enough remaining.
	var changeAdded bool
	var changeAddress stdaddr.Address
	var changeOutput *wire.TxOut
	minFeeWithChange := (size + dexdcr.P2PKHOutputSize) * feeRate
	if remaining > minFeeWithChange {
		changeValue := remaining - minFeeWithChange
		if subtractFrom >= 0 {
			// Subtract the additional fee needed for the added change output
			// from the specified existing output.
			changeValue = remaining
		}
		if !dexdcr.IsDustVal(dexdcr.P2PKHOutputSize, changeValue, feeRate) {
			if subtractFrom >= 0 { // only subtract after dust check
				baseTx.TxOut[subtractFrom].Value -= int64(minFeeWithChange)
				remaining += minFeeWithChange
			}
			changeOutput, changeAddress, err = dcr.makeChangeOut(changeAcct, changeValue)
			if err != nil {
				return nil, nil, "", 0, err
			}
			dcr.log.Debugf("Change output size = %d, addr = %s", changeOutput.SerializeSize(), changeAddress.String())
			changeAdded = true
			baseTx.AddTxOut(changeOutput) // unsigned txn
			remaining -= changeValue
		}
	}

	lastFee := remaining

	// If change added or subtracting from an existing output, iterate on fees.
	if changeAdded || subtractFrom >= 0 {
		subtractee := changeOutput
		if subtractFrom >= 0 {
			subtractee = baseTx.TxOut[subtractFrom]
		}
		// The amount available for fees is the sum of what is presently
		// allocated to fees (lastFee) and the value of the subtractee output,
		// which add to fees or absorb excess fees from lastFee.
		reservoir := lastFee + uint64(subtractee.Value)

		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]bool{}
		for {
			// Each cycle, sign the transaction and see if there is a need to
			// raise or lower the fees.
			sigCycles++
			msgTx, err = dcr.wallet.SignRawTransaction(dcr.ctx, baseTx)
			if err != nil {
				return nil, nil, "", 0, err
			}
			size = uint64(msgTx.SerializeSize())
			reqFee := feeRate * size
			if reqFee > reservoir {
				// IsDustVal check must be bugged.
				dcr.log.Errorf("reached the impossible place. in = %.8f, out = %.8f, reqFee = %.8f, lastFee = %.8f, raw tx = %x",
					toDCR(totalIn), toDCR(totalOut), toDCR(reqFee), toDCR(lastFee), dcr.wireBytes(msgTx))
				return nil, nil, "", 0, fmt.Errorf("change error")
			}

			// If 1) lastFee == reqFee, nothing changed since the last cycle.
			// And there is likely no room for improvement. If 2) The reqFee
			// required for a transaction of this size is less than the
			// currently signed transaction fees, but we've already tried it,
			// then it must have a larger serialize size, so the current fee is
			// as good as it gets.
			if lastFee == reqFee || (lastFee > reqFee && tried[reqFee]) {
				break
			}

			// The minimum fee for a transaction of this size is either higher or
			// lower than the fee in the currently signed transaction, and it hasn't
			// been tried yet, so try it now.
			tried[lastFee] = true
			subtractee.Value = int64(reservoir - reqFee) // next
			lastFee = reqFee
			if dexdcr.IsDust(subtractee, feeRate) {
				// Another condition that should be impossible, but check anyway in case
				// the maximum fee was underestimated causing the first check to be
				// missed.
				dcr.log.Errorf("reached the impossible place. in = %.8f, out = %.8f, reqFee = %.8f, lastFee = %.8f, raw tx = %x",
					toDCR(totalIn), toDCR(totalOut), toDCR(reqFee), toDCR(lastFee), dcr.wireBytes(msgTx))
				return nil, nil, "", 0, fmt.Errorf("dust error")
			}
			continue
		}
	}

	// Double check the resulting txns fee and fee rate.
	_, _, checkFee, checkRate, size := reduceMsgTx(msgTx)
	if checkFee != lastFee {
		return nil, nil, "", 0, fmt.Errorf("fee mismatch! %.8f != %.8f, raw tx: %x", toDCR(checkFee), toDCR(lastFee), dcr.wireBytes(msgTx))
	}
	// Ensure the effective fee rate is at least the required fee rate.
	if checkRate < feeRate {
		return nil, nil, "", 0, fmt.Errorf("final fee rate for %s, %d, is lower than expected, %d. raw tx: %x",
			msgTx.CachedTxHash(), checkRate, feeRate, dcr.wireBytes(msgTx))
	}
	// This is a last ditch effort to catch ridiculously high fees. Right now,
	// it's just erroring for fees more than triple the expected rate, which is
	// admittedly un-scientific. This should account for any signature length
	// related variation as well as a potential dust change output with no
	// subtractee specified, in which case the dust goes to the miner.
	if changeAdded && checkRate > feeRate*3 {
		return nil, nil, "", 0, fmt.Errorf("final fee rate for %s, %d, is seemingly outrageous, target = %d, raw tx = %x",
			msgTx.CachedTxHash(), checkRate, feeRate, dcr.wireBytes(msgTx))
	}

	txHash := msgTx.TxHash()
	dcr.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, txHash, feeRate, checkRate, checkFee, size, changeAdded)

	var change *output
	var changeAddr string
	if changeAdded {
		change = newOutput(&txHash, uint32(len(msgTx.TxOut)-1), uint64(changeOutput.Value), wire.TxTreeRegular)
		changeAddr = changeAddress.String()
	}

	return msgTx, change, changeAddr, lastFee, nil
}

func (dcr *ExchangeWallet) broadcastTx(signedTx *wire.MsgTx) error {
	txHash, err := dcr.wallet.SendRawTransaction(dcr.ctx, signedTx, false)
	if err != nil {
		return fmt.Errorf("sendrawtx error: %w, raw tx: %x", err, dcr.wireBytes(signedTx))
	}
	checkHash := signedTx.TxHash()
	if *txHash != checkHash {
		return fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s, raw tx: %x", *txHash, checkHash, dcr.wireBytes(signedTx))
	}
	return nil
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (dcr *ExchangeWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr stdaddr.Address) (sig, pubkey []byte, err error) {
	sigType, err := dexdcr.AddressSigType(addr)
	if err != nil {
		return nil, nil, err
	}

	priv, err := dcr.wallet.AddressPrivKey(dcr.ctx, addr)
	if err != nil {
		return nil, nil, err
	}
	defer priv.Zero()

	sig, err = sign.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, priv.Serialize(), sigType)
	if err != nil {
		return nil, nil, err
	}

	return sig, priv.PubKey().SerializeCompressed(), nil
}

func (dcr *ExchangeWallet) checkPeers() {
	ctx, cancel := context.WithTimeout(dcr.ctx, 2*time.Second)
	defer cancel()
	numPeers, err := dcr.wallet.PeerCount(ctx)
	if err != nil { // e.g. dcrd passthrough fail in non-SPV mode
		prevPeer := atomic.SwapUint32(&dcr.lastPeerCount, 0)
		if prevPeer != 0 {
			dcr.log.Errorf("Failed to get peer count: %v", err)
			dcr.peersChange(0, err)
		}
		return
	}
	prevPeer := atomic.SwapUint32(&dcr.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		dcr.peersChange(numPeers, nil)
	}
}

func (dcr *ExchangeWallet) monitorPeers(ctx context.Context) {
	ticker := time.NewTicker(peerCountTicker)
	defer ticker.Stop()
	for {
		dcr.checkPeers()

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// monitorBlocks pings for new blocks and runs the tipChange callback function
// when the block changes. New blocks are also scanned for potential contract
// redeems.
func (dcr *ExchangeWallet) monitorBlocks(ctx context.Context) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()

	var walletBlock <-chan *block
	if notifier, isNotifier := dcr.wallet.(tipNotifier); isNotifier {
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

	// checkTip captures queuedBlock and walletBlock.
	checkTip := func() {
		ctxInternal, cancel0 := context.WithTimeout(ctx, 4*time.Second)
		defer cancel0()

		newTip, err := dcr.getBestBlock(ctxInternal)
		if err != nil {
			dcr.handleTipChange(ctx, nil, 0, fmt.Errorf("failed to get best block: %w", err))
			return
		}

		dcr.tipMtx.RLock()
		sameTip := dcr.currentTip.hash.IsEqual(newTip.hash)
		dcr.tipMtx.RUnlock()

		if sameTip {
			return
		}

		if walletBlock == nil {
			dcr.handleTipChange(ctx, newTip.hash, newTip.height, nil)
			return
		}

		// Queue it for reporting, but don't send it right away. Give the wallet
		// a chance to provide their block update. SPV wallet may need more time
		// after storing the block header to fetch and scan filters and issue
		// the FilteredBlockConnected report.
		if queuedBlock != nil {
			queuedBlock.queue.Stop()
		}
		blockAllowance := walletBlockAllowance
		ctxInternal, cancel1 := context.WithTimeout(ctx, 4*time.Second)
		defer cancel1()
		synced, _, err := dcr.wallet.SyncStatus(ctxInternal)
		if err != nil {
			dcr.log.Errorf("Error retrieving sync status before queuing polled block: %v", err)
		} else if !synced {
			blockAllowance *= 10
		}
		queuedBlock = &polledBlock{
			block: newTip,
			queue: time.AfterFunc(blockAllowance, func() {
				dcr.log.Warnf("Reporting a block found in polling that the wallet apparently "+
					"never reported: %s (%d). If you see this message repeatedly, it may indicate "+
					"an issue with the wallet.", newTip.hash, newTip.height)
				dcr.handleTipChange(ctx, newTip.hash, newTip.height, nil)
			}),
		}
	}

	for {
		select {
		case <-ticker.C:
			checkTip()

		case walletTip := <-walletBlock:
			if queuedBlock != nil && walletTip.height >= queuedBlock.height {
				if !queuedBlock.queue.Stop() && walletTip.hash == queuedBlock.hash {
					continue
				}
				queuedBlock = nil
			}
			dcr.handleTipChange(ctx, walletTip.hash, walletTip.height, nil)

		case <-ctx.Done():
			return
		}

		// Ensure context cancellation takes priority before the next iteration.
		if ctx.Err() != nil {
			return
		}
	}
}

func (dcr *ExchangeWallet) handleTipChange(ctx context.Context, newTipHash *chainhash.Hash, newTipHeight int64, err error) {
	if err != nil {
		go dcr.tipChange(err)
		return
	}

	dcr.tipMtx.Lock()
	defer dcr.tipMtx.Unlock()

	prevTip := dcr.currentTip
	dcr.currentTip = &block{newTipHeight, newTipHash}
	dcr.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTipHeight, newTipHash)
	go dcr.tipChange(nil)

	// Search for contract redemption in new blocks if there
	// are contracts pending redemption.
	dcr.findRedemptionMtx.RLock()
	pendingContractsCount := len(dcr.findRedemptionQueue)
	contractOutpoints := make([]outPoint, 0, pendingContractsCount)
	for contractOutpoint := range dcr.findRedemptionQueue {
		contractOutpoints = append(contractOutpoints, contractOutpoint)
	}
	dcr.findRedemptionMtx.RUnlock()
	if pendingContractsCount == 0 {
		return
	}

	notifyFatalFindRedemptionError := func(s string, a ...interface{}) {
		dcr.fatalFindRedemptionsError(fmt.Errorf("tipChange handler - "+s, a...), contractOutpoints)
	}

	// Check if the previous tip is still part of the mainchain (prevTip confs >= 0).
	// Redemption search would typically resume from prevTipHeight + 1 unless the
	// previous tip was re-orged out of the mainchain, in which case redemption
	// search will resume from the mainchain ancestor of the previous tip.
	prevTipHeader, isMainchain, _, err := dcr.blockHeader(ctx, prevTip.hash)
	if err != nil {
		// Redemption search cannot continue reliably without knowing if there
		// was a reorg, cancel all find redemption requests in queue.
		notifyFatalFindRedemptionError("blockHeader error for prev tip hash %s: %w",
			prevTip.hash, err)
		return
	}

	startHeight := int64(prevTipHeader.Height + 1)
	if !isMainchain {
		// The previous tip is no longer part of the mainchain. Crawl blocks
		// backwards until finding a mainchain block. Start with the block
		// that is the immediate ancestor to the previous tip.
		ancestorBlockHash := &prevTipHeader.PrevBlock
		for {
			aBlock, isMainchain, _, err := dcr.blockHeader(ctx, ancestorBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("Error getting block header %s: %w", ancestorBlockHash, err)
				return
			}
			if isMainchain {
				// Found the mainchain ancestor of previous tip.
				startHeight = int64(aBlock.Height)
				dcr.log.Debugf("reorg detected from height %d to %d", aBlock.Height, newTipHeight)
				break
			}
			if aBlock.Height == 0 {
				// Crawled back to genesis block without finding a mainchain ancestor
				// for the previous tip. Should never happen!
				notifyFatalFindRedemptionError("no mainchain ancestor for orphaned block %s", prevTip.hash)
				return
			}
			ancestorBlockHash = &aBlock.PrevBlock
		}
	}

	// Run the redemption search from the startHeight determined above up
	// till the current tip height.
	go dcr.findRedemptionsInBlockRange(startHeight, newTipHeight, contractOutpoints)
}

func (dcr *ExchangeWallet) getBestBlock(ctx context.Context) (*block, error) {
	hash, height, err := dcr.wallet.GetBestBlock(ctx)
	if err != nil {
		return nil, err
	}
	return &block{hash: hash, height: height}, nil
}

// mainchainAncestor crawls blocks backwards starting at the provided hash
// until finding a mainchain block. Returns the first mainchain block found.
func (dcr *ExchangeWallet) mainchainAncestor(ctx context.Context, blockHash *chainhash.Hash) (*chainhash.Hash, int64, error) {
	checkHash := blockHash
	for {
		checkBlock, isMainchain, _, err := dcr.blockHeader(ctx, checkHash)
		if err != nil {
			return nil, 0, fmt.Errorf("getblockheader error for block %s: %w", checkHash, err)
		}
		if isMainchain {
			// This is a mainchain block, return the hash and height.
			return checkHash, int64(checkBlock.Height), nil
		}
		if checkBlock.Height == 0 {
			return nil, 0, fmt.Errorf("no mainchain ancestor for block %s", blockHash)
		}
		checkHash = &checkBlock.PrevBlock
	}
}

// blockHeader returns the *BlockHeader for the specified block hash, and bools
// indicating if the block is mainchain, and approved by stakeholders.
// validMainchain will always be false if mainchain is false; mainchain can be
// true for an invalidated block.
func (dcr *ExchangeWallet) blockHeader(ctx context.Context, blockHash *chainhash.Hash) (blockHeader *BlockHeader, mainchain, validMainchain bool, err error) {
	blockHeader, err = dcr.wallet.GetBlockHeader(ctx, blockHash)
	if err != nil {
		return nil, false, false, fmt.Errorf("GetBlockHeader error for block %s: %w", blockHash, err)
	}
	if blockHeader.Confirmations < 0 { // not mainchain, really just == -1, but catch all unexpected
		dcr.log.Warnf("Block %v is a SIDE CHAIN block at height %d!", blockHash, blockHeader.Height)
		return blockHeader, false, false, nil
	}

	// It's mainchain. Now check if there is a validating block.
	if blockHeader.NextHash == nil { // we're at the tip
		return blockHeader, true, true, nil
	}

	nextHeader, err := dcr.wallet.GetBlockHeader(ctx, blockHeader.NextHash)
	if err != nil {
		return nil, false, false, fmt.Errorf("error fetching validating block: %w", err)
	}

	validMainchain = nextHeader.VoteBits&1 != 0
	if !validMainchain {
		dcr.log.Warnf("Block %v found in mainchain, but stakeholder DISAPPROVED!", blockHash)
	}
	return blockHeader, true, validMainchain, nil
}

func (dcr *ExchangeWallet) cachedBestBlock() block {
	dcr.tipMtx.RLock()
	defer dcr.tipMtx.RUnlock()
	return *dcr.currentTip
}

// wireBytes dumps the serialized transaction bytes.
func (dcr *ExchangeWallet) wireBytes(tx *wire.MsgTx) []byte {
	s, err := tx.Bytes()
	// wireBytes is just used for logging, and a serialization error is
	// extremely unlikely, so just log the error and return the nil bytes.
	if err != nil {
		dcr.log.Errorf("error serializing transaction: %v", err)
	}
	return s
}

// Convert the DCR value to atoms.
func toAtoms(v float64) uint64 {
	return uint64(math.Round(v * conventionalConversionFactor))
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

// reduceMsgTx computes the total input and output amounts, the resulting
// absolute fee and fee rate, and the serialized transaction size.
func reduceMsgTx(tx *wire.MsgTx) (in, out, fees, rate, size uint64) {
	for _, txIn := range tx.TxIn {
		in += uint64(txIn.ValueIn)
	}
	for _, txOut := range tx.TxOut {
		out += uint64(txOut.Value)
	}
	fees = in - out
	size = uint64(tx.SerializeSize())
	rate = fees / size
	return
}

// toDCR returns a float representation in conventional units for the given
// atoms.
func toDCR(v uint64) float64 {
	return dcrutil.Amount(v).ToCoin()
}

// calcBumpedRate calculated a bump on the baseRate. If bump is nil, the
// baseRate is returned directly. If *bump is out of range, an error is
// returned.
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
