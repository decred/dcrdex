// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/dexnet"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	walletjson "decred.org/dcrwallet/v5/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v5/wallet"
	_ "decred.org/dcrwallet/v5/wallet/drivers/bdb"
	"github.com/decred/dcrd/blockchain/stake/v5"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
	vspdjson "github.com/decred/vspd/types/v2"
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

	// freshFeeAge is the expiry age for cached fee rates of external origin,
	// past which fetchFeeFromOracle should be used to refresh the rate.
	freshFeeAge = time.Minute

	// requiredConfTxConfirms is the amount of confirms a redeem or refund
	// transaction needs before the trade is considered confirmed. The tx is
	// monitored until this number of confirms is reached. Two to make sure
	// the block containing the tx is stakeholder-approved
	requiredConfTxConfirms = 2

	vspFileName = "vsp.json"

	defaultCSPPMainnet  = "mix.decred.org:5760"
	defaultCSPPTestnet3 = "mix.decred.org:15760"

	ticketSize               = dexdcr.MsgTxOverhead + dexdcr.P2PKHInputSize + 2*dexdcr.P2SHOutputSize /* stakesubmission and sstxchanges */ + 32 /* see e.g. RewardCommitmentScript */
	minVSPTicketPurchaseSize = dexdcr.MsgTxOverhead + dexdcr.P2PKHInputSize + dexdcr.P2PKHOutputSize + ticketSize
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

	// maxMempoolAge is the max amount of time the wallet will let a
	// redeem or refund transaction sit in mempool from the time it is first seen
	// until it attempts to abandon it and try to send a new transaction.
	// This is necessary because transactions with already spent inputs may
	// be tried over and over with wallet in SPV mode.
	maxMempoolAge = time.Hour * 2

	WalletOpts = []*asset.ConfigOption{
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
			Key:          "gaplimit",
			DisplayName:  "Address Gap Limit",
			Description:  "The gap limit for used address discovery",
			DefaultValue: wallet.DefaultGapLimit,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to " +
				"fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order " +
				"until the first swap contract is broadcast during match settlement, or " +
				"the order is canceled. This an extra transaction for which network " +
				"mining fees are paid.",
			IsBoolean:    true,
			DefaultValue: true, // cheap fees, helpful for bond reserves, and adjustable at order-time
		},
		{
			Key:         "apifeefallback",
			DisplayName: "External fee rate estimates",
			Description: "Allow fee rate estimation from a block explorer API. " +
				"This is useful as a fallback for SPV wallets and RPC wallets " +
				"that have recently been started.",
			IsBoolean:    true,
			DefaultValue: true,
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

	multiFundingOpts = []*asset.OrderOption{
		{
			ConfigOption: asset.ConfigOption{
				Key:         multiSplitKey,
				DisplayName: "Allow multi split",
				Description: "Allow split funding transactions that pre-size outputs to " +
					"prevent excessive overlock.",
				IsBoolean:    true,
				DefaultValue: true,
			},
		},
		{
			ConfigOption: asset.ConfigOption{
				Key:         multiSplitBufferKey,
				DisplayName: "Multi split buffer",
				Description: "Add an integer percent buffer to split output amounts to " +
					"facilitate output reuse. This is only required for quote assets.",
				DefaultValue: 5,
				DependsOn:    multiSplitKey,
			},
			QuoteAssetOnly: true,
			XYRange: &asset.XYRange{
				Start: asset.XYRangePoint{
					Label: "0%",
					X:     0,
					Y:     0,
				},
				End: asset.XYRangePoint{
					Label: "100%",
					X:     100,
					Y:     100,
				},
				XUnit:  "%",
				YUnit:  "%",
				RoundX: true,
				RoundY: true,
			},
		},
	}

	// WalletInfo defines some general information about a Decred wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Decred",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexdcr.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:             walletTypeSPV,
				Tab:              "Native",
				Description:      "Use the built-in SPV wallet",
				ConfigOpts:       WalletOpts,
				Seeded:           true,
				MultiFundingOpts: multiFundingOpts,
			},
			{
				Type:              walletTypeDcrwRPC,
				Tab:               "External",
				Description:       "Connect to dcrwallet",
				DefaultConfigPath: defaultConfigPath,
				ConfigOpts:        append(rpcOpts, WalletOpts...),
				MultiFundingOpts:  multiFundingOpts,
			},
		},
	}
	swapFeeBumpKey      = "swapfeebump"
	splitKey            = "swapsplit"
	multiSplitKey       = "multisplit"
	multiSplitBufferKey = "multisplitbuffer"
	redeemFeeBumpFee    = "redeemfeebump"
	client              http.Client
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

func (op *output) TxID() string {
	return op.txHash().String()
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
		recoveryCfg.NumInternalAddresses, recoveryCfg.GapLimit, chainParams)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexdcr.MinLotSize(maxFeeRate)
}

func init() {
	asset.Register(BipID, &Driver{})
}

// RecoveryCfg is the information that is transferred from the old wallet
// to the new one when the wallet is recovered.
type RecoveryCfg struct {
	NumExternalAddresses uint32 `ini:"numexternaladdr"`
	NumInternalAddresses uint32 `ini:"numinternaladdr"`
	GapLimit             uint32 `ini:"gaplimit"`
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
	useSplitTx       bool
	fallbackFeeRate  uint64
	feeRateLimit     uint64
	redeemConfTarget uint64
	apiFeeFallback   bool
}

// mempoolTx holds a refund or redeem.
type mempoolTx struct {
	txHash    chainhash.Hash
	firstSeen time.Time
	txType    asset.ConfirmTxType
}

// vsp holds info needed for purchasing tickets from a vsp. PubKey is from the
// vsp and is used for verifying communications.
type vsp struct {
	URL           string  `json:"url"`
	FeePercentage float64 `json:"feepercent"`
	PubKey        string  `json:"pubkey"`
}

// rescanProgress is the progress of an asynchronous rescan.
type rescanProgress struct {
	scannedThrough int64
}

// ExchangeWallet is a wallet backend for Decred. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	bondReserves atomic.Uint64
	cfgV         atomic.Value // *exchangeWalletConfig

	ctx            context.Context // the asset subsystem starts with Connect(ctx)
	wg             sync.WaitGroup
	wallet         Wallet
	chainParams    *chaincfg.Params
	log            dex.Logger
	network        dex.Network
	emit           *asset.WalletEmitter
	lastPeerCount  uint32
	peersChange    func(uint32, error)
	vspFilepath    string
	walletType     string
	walletDir      string
	startingBlocks atomic.Uint64

	oracleFeesMtx sync.Mutex
	oracleFees    map[uint64]feeStamped // conf target => fee rate
	oracleFailing bool

	handleTipMtx sync.Mutex
	currentTip   atomic.Value // *block

	// Coins returned by Fund are cached for quick reference.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*fundingCoin

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[outPoint]*findRedemptionReq

	externalTxMtx   sync.RWMutex
	externalTxCache map[chainhash.Hash]*externalTx

	// TODO: Consider persisting mempool txs on file.
	mempoolTxsMtx sync.RWMutex
	mempoolTxs    map[[32]byte]*mempoolTx // keyed by secret hash

	vspV atomic.Value // *vsp

	connected atomic.Bool

	subsidyCache *blockchain.SubsidyCache

	ticketBuyer struct {
		running            atomic.Bool
		remaining          atomic.Int32
		unconfirmedTickets map[chainhash.Hash]struct{}
	}

	// Embedding wallets can set cycleMixer, which will be triggered after
	// new block are seen.
	cycleMixer func()
	mixing     atomic.Bool

	pendingTxsMtx sync.RWMutex
	pendingTxs    map[chainhash.Hash]*btc.ExtendedWalletTx

	receiveTxLastQuery atomic.Uint64

	txHistoryDB      atomic.Value // *btc.BadgerTxDB
	syncingTxHistory atomic.Bool

	previouslySynced atomic.Bool

	rescan struct {
		sync.RWMutex
		progress *rescanProgress // nil = no rescan in progress
	}
}

func (dcr *ExchangeWallet) config() *exchangeWalletConfig {
	return dcr.cfgV.Load().(*exchangeWalletConfig)
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)
var _ asset.FeeRater = (*ExchangeWallet)(nil)
var _ asset.Withdrawer = (*ExchangeWallet)(nil)
var _ asset.LiveReconfigurer = (*ExchangeWallet)(nil)
var _ asset.TxFeeEstimator = (*ExchangeWallet)(nil)
var _ asset.Bonder = (*ExchangeWallet)(nil)
var _ asset.Authenticator = (*ExchangeWallet)(nil)
var _ asset.TicketBuyer = (*ExchangeWallet)(nil)
var _ asset.WalletHistorian = (*ExchangeWallet)(nil)
var _ asset.NewAddresser = (*ExchangeWallet)(nil)

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
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
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

	var w asset.Wallet = dcr

	switch cfg.Type {
	case walletTypeDcrwRPC, walletTypeLegacy:
		dcr.wallet, err = newRPCWallet(cfg.Settings, logger, network)
		if err != nil {
			return nil, err
		}
	case walletTypeSPV:
		dcr.wallet, err = openSPVWallet(cfg.DataDir, walletCfg.GapLimit, chainParams, logger)
		if err != nil {
			return nil, err
		}
		w, err = initNativeWallet(dcr)
		if err != nil {
			return nil, err
		}
	default:
		makeCustomWallet, ok := customWalletConstructors[cfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown wallet type %q", cfg.Type)
		}

		// Create custom wallet and return early if we encounter any error.
		dcr.wallet, err = makeCustomWallet(cfg.Settings, chainParams, logger)
		if err != nil {
			return nil, fmt.Errorf("custom wallet setup error: %v", err)
		}
	}

	return w, nil
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

	return &exchangeWalletConfig{
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

	dir := filepath.Join(cfg.DataDir, chainParams.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("unable to create wallet dir: %v", err)
	}

	vspFilepath := filepath.Join(dir, vspFileName)

	w := &ExchangeWallet{
		log:                 logger,
		chainParams:         chainParams,
		network:             network,
		emit:                cfg.Emit,
		peersChange:         cfg.PeersChange,
		fundingCoins:        make(map[outPoint]*fundingCoin),
		findRedemptionQueue: make(map[outPoint]*findRedemptionReq),
		externalTxCache:     make(map[chainhash.Hash]*externalTx),
		oracleFees:          make(map[uint64]feeStamped),
		mempoolTxs:          make(map[[32]byte]*mempoolTx),
		vspFilepath:         vspFilepath,
		walletType:          cfg.Type,
		subsidyCache:        blockchain.NewSubsidyCache(chainParams),
		pendingTxs:          make(map[chainhash.Hash]*btc.ExtendedWalletTx),
		walletDir:           dir,
	}

	if b, err := os.ReadFile(vspFilepath); err == nil {
		var v vsp
		err = json.Unmarshal(b, &v)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal vsp file: %v", err)
		}
		w.vspV.Store(&v)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("unable to read vsp file: %v", err)
	}

	w.cfgV.Store(walletCfg)

	return w, nil
}

// openSPVWallet opens the previously created native SPV wallet.
func openSPVWallet(dataDir string, gapLimit uint32, chainParams *chaincfg.Params, log dex.Logger) (*spvWallet, error) {
	dir := filepath.Join(dataDir, chainParams.Name, "spv")
	if exists, err := walletExists(dir); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("wallet at %q doesn't exists", dir)
	}

	return &spvWallet{
		dir:         dir,
		chainParams: chainParams,
		log:         log.SubLogger("SPV"),
		blockCache: blockCache{
			blocks: make(map[chainhash.Hash]*cachedBlock),
		},
		tipChan:  make(chan *block, 16),
		gapLimit: gapLimit,
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

func (dcr *ExchangeWallet) txHistoryDBPath(walletID string) string {
	return filepath.Join(dcr.walletDir, fmt.Sprintf("txhistorydb-%s", walletID))
}

// findExistingAddressBasedTxHistoryDB finds the path of a tx history db that
// was created using an address controlled by the wallet. This should only be
// used for RPC wallets, as SPV wallets are able to get the first address
// generated by the wallet.
func (dcr *ExchangeWallet) findExistingAddressBasedTxHistoryDB() (string, error) {
	dir, err := os.Open(dcr.walletDir)
	if err != nil {
		return "", fmt.Errorf("error opening wallet directory: %w", err)
	}
	defer dir.Close()

	entries, err := dir.Readdir(0)
	if err != nil {
		return "", fmt.Errorf("error reading wallet directory: %w", err)
	}

	pattern := regexp.MustCompile(`^txhistorydb-(.+)$`)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		match := pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}

		address := match[1]

		decodedAddr, err := stdaddr.DecodeAddress(address, dcr.chainParams)
		if err != nil {
			continue
		}
		owns, err := dcr.wallet.WalletOwnsAddress(dcr.ctx, decodedAddr)
		if err != nil {
			continue
		}
		if owns {
			return filepath.Join(dcr.walletDir, entry.Name()), nil
		}
	}

	return "", nil
}

func (dcr *ExchangeWallet) startTxHistoryDB(ctx context.Context) (*dex.ConnectionMaster, error) {
	var dbPath string
	if spvWallet, ok := dcr.wallet.(*spvWallet); ok {
		initialAddress, err := spvWallet.InitialAddress(ctx)
		if err != nil {
			return nil, err
		}

		dbPath = dcr.txHistoryDBPath(initialAddress)
	}

	if dbPath == "" {
		addressPath, err := dcr.findExistingAddressBasedTxHistoryDB()
		if err != nil {
			return nil, err
		}
		if addressPath != "" {
			dbPath = addressPath
		}
	}

	if dbPath == "" {
		depositAddr, err := dcr.DepositAddress()
		if err != nil {
			return nil, fmt.Errorf("error getting deposit address: %w", err)
		}
		dbPath = dcr.txHistoryDBPath(depositAddr)
	}

	dcr.log.Debugf("Using tx history db at %s", dbPath)

	db := btc.NewBadgerTxDB(dbPath, dcr.log)
	dcr.txHistoryDB.Store(db)

	cm := dex.NewConnectionMaster(db)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to tx history db: %w", err)
	}

	var success bool
	defer func() {
		if !success {
			cm.Disconnect()
		}
	}()

	pendingTxs, err := db.GetPendingTxs()
	if err != nil {
		return nil, fmt.Errorf("failed to load unconfirmed txs: %v", err)
	}

	dcr.pendingTxsMtx.Lock()
	for _, tx := range pendingTxs {
		txHash, err := chainhash.NewHashFromStr(tx.ID)
		if err != nil {
			dcr.log.Errorf("Invalid txid %v from tx history db: %v", tx.ID, err)
			continue
		}
		dcr.pendingTxs[*txHash] = tx
	}
	dcr.pendingTxsMtx.Unlock()

	lastQuery, err := db.GetLastReceiveTxQuery()
	if errors.Is(err, btc.ErrNeverQueried) {
		lastQuery = 0
	} else if err != nil {
		return nil, fmt.Errorf("failed to load last query time: %v", err)
	}

	dcr.receiveTxLastQuery.Store(lastQuery)

	success = true
	return cm, nil
}

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
	tip, err := dcr.getBestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("error initializing best block for DCR: %w", err)
	}
	dcr.currentTip.Store(tip)
	dcr.startingBlocks.Store(uint64(tip.height))

	dbCM, err := dcr.startTxHistoryDB(ctx)
	if err != nil {
		return nil, err
	}

	success = true // All good, don't disconnect the wallet when this method returns.
	dcr.connected.Store(true)

	dcr.wg.Add(1)
	go func() {
		defer dcr.wg.Done()
		defer dbCM.Disconnect()
		dcr.monitorBlocks(ctx)
		dcr.shutdown()
	}()

	dcr.wg.Add(1)
	go func() {
		defer dcr.wg.Done()
		dcr.monitorPeers(ctx)
	}()

	dcr.wg.Add(1)
	go func() {
		defer dcr.wg.Done()
		dcr.syncTxHistory(ctx, uint64(tip.height))
	}()

	return &dcr.wg, nil
}

// Reconfigure attempts to reconfigure the wallet.
func (dcr *ExchangeWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, currentAddress string) (restart bool, err error) {
	dcrCfg := new(walletConfig)
	_, err = loadConfig(cfg.Settings, dcr.network, dcrCfg)
	if err != nil {
		return false, err
	}

	restart, err = dcr.wallet.Reconfigure(ctx, cfg, dcr.network, currentAddress)
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
	accts := dcr.wallet.Accounts()
	if accts.UnmixedAccount != "" {
		return accts.UnmixedAccount
	}
	return accts.PrimaryAccount
}

// fundingAccounts returns the primary account along with any configured trading
// account which may contain spendable outputs (split tx outputs or chained swap
// change).
func (dcr *ExchangeWallet) fundingAccounts() []string {
	accts := dcr.wallet.Accounts()
	if accts.UnmixedAccount == "" {
		return []string{accts.PrimaryAccount}
	}
	return []string{accts.PrimaryAccount, accts.TradingAccount}
}

func (dcr *ExchangeWallet) allAccounts() []string {
	accts := dcr.wallet.Accounts()
	if accts.UnmixedAccount == "" {
		return []string{accts.PrimaryAccount}
	}
	return []string{accts.PrimaryAccount, accts.TradingAccount, accts.UnmixedAccount}
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

func (dcr *ExchangeWallet) balance() (*asset.Balance, error) {
	accts := dcr.wallet.Accounts()

	locked, err := dcr.lockedAtoms(accts.PrimaryAccount)
	if err != nil {
		return nil, err
	}
	ab, err := dcr.wallet.AccountBalance(dcr.ctx, 0, accts.PrimaryAccount)
	if err != nil {
		return nil, err
	}
	bal := &asset.Balance{
		Available: toAtoms(ab.Spendable) - locked,
		Immature: toAtoms(ab.ImmatureCoinbaseRewards) +
			toAtoms(ab.ImmatureStakeGeneration),
		Locked: locked + toAtoms(ab.LockedByTickets),
		Other:  make(map[asset.BalanceCategory]asset.CustomBalance),
	}

	bal.Other[asset.BalanceCategoryStaked] = asset.CustomBalance{
		Amount: toAtoms(ab.LockedByTickets),
	}

	if accts.UnmixedAccount == "" {
		return bal, nil
	}

	// Mixing is enabled, consider ...
	// 1) trading account spendable (-locked) as available,
	// 2) all unmixed funds as immature, and
	// 3) all locked utxos in the trading account as locked (for swapping).
	tradingAcctBal, err := dcr.wallet.AccountBalance(dcr.ctx, 0, accts.TradingAccount)
	if err != nil {
		return nil, err
	}
	tradingAcctLocked, err := dcr.lockedAtoms(accts.TradingAccount)
	if err != nil {
		return nil, err
	}
	unmixedAcctBal, err := dcr.wallet.AccountBalance(dcr.ctx, 0, accts.UnmixedAccount)
	if err != nil {
		return nil, err
	}

	bal.Available += toAtoms(tradingAcctBal.Spendable) - tradingAcctLocked
	bal.Immature += toAtoms(unmixedAcctBal.Total)
	bal.Locked += tradingAcctLocked

	bal.Other[asset.BalanceCategoryUnmixed] = asset.CustomBalance{
		Amount: toAtoms(unmixedAcctBal.Total),
	}

	return bal, nil
}

// Balance should return the total available funds in the wallet.
func (dcr *ExchangeWallet) Balance() (*asset.Balance, error) {
	bal, err := dcr.balance()
	if err != nil {
		return nil, err
	}

	reserves := dcr.bondReserves.Load()
	if reserves > bal.Available { // unmixed (immature) probably needs to trickle in
		dcr.log.Warnf("Available balance is below configured reserves: %f < %f",
			toDCR(bal.Available), toDCR(reserves))
		bal.ReservesDeficit = reserves - bal.Available
		reserves = bal.Available
	}

	bal.BondReserves = reserves
	bal.Available -= reserves
	bal.Locked += reserves

	return bal, nil
}

func bondsFeeBuffer(highFeeRate uint64) uint64 {
	const inputCount uint64 = 12 // plan for lots of inputs
	largeBondTxSize := dexdcr.MsgTxOverhead + dexdcr.P2SHOutputSize + 1 + dexdcr.BondPushDataSize +
		dexdcr.P2PKHOutputSize + inputCount*dexdcr.P2PKHInputSize
	// Normally we can plan on just 2 parallel "tracks" (single bond overlap
	// when bonds are expired and waiting to refund) but that may increase
	// temporarily if target tier is adjusted up.
	const parallelTracks uint64 = 4
	return parallelTracks * largeBondTxSize * highFeeRate
}

// BondsFeeBuffer suggests how much extra may be required for the transaction
// fees part of required bond reserves when bond rotation is enabled.
func (dcr *ExchangeWallet) BondsFeeBuffer(feeRate uint64) uint64 {
	if feeRate == 0 {
		feeRate = dcr.targetFeeRateWithFallback(2, 0)
	}
	feeRate *= 2 // double the current live fee rate estimate
	return bondsFeeBuffer(feeRate)
}

func (dcr *ExchangeWallet) SetBondReserves(reserves uint64) {
	dcr.bondReserves.Store(reserves)
}

// FeeRate satisfies asset.FeeRater.
func (dcr *ExchangeWallet) FeeRate() uint64 {
	const confTarget = 2 // 1 historically gives crazy rates
	rate, err := dcr.feeRate(confTarget)
	if err != nil && dcr.network != dex.Simnet { // log and return 0
		dcr.log.Errorf("feeRate error: %v", err)
	}
	return rate
}

// feeRate returns the current optimal fee rate in atoms / byte.
func (dcr *ExchangeWallet) feeRate(confTarget uint64) (uint64, error) {
	if dcr.ctx == nil {
		return 0, errors.New("not connected")
	}
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

	dcr.log.Tracef("Retrieving fee rate from external fee oracle for %d target blocks", confTarget)
	dcrPerKB, err := fetchFeeFromOracle(dcr.ctx, dcr.network, confTarget)
	if err != nil {
		// Just log it and return zero. If we return an error, it's just logged
		// anyway, and we want to meter these logs.
		dcr.log.Meter("feeRate.fetch.fail", time.Hour).Errorf("external fee rate API failure: %v", err)
		// Flag the oracle as failing so subsequent requests don't also try and
		// fail after the request timeout. Remove the flag after a bit.
		dcr.oracleFailing = true
		time.AfterFunc(freshFeeAge, func() {
			dcr.oracleFeesMtx.Lock()
			dcr.oracleFailing = false
			dcr.oracleFeesMtx.Unlock()
		})
		return 0, nil
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
	var uri string
	if net == dex.Testnet {
		uri = fmt.Sprintf("https://testnet.dcrdata.org/insight/api/utils/estimatefee?nbBlocks=%d", nb)
	} else { // mainnet and simnet
		uri = fmt.Sprintf("https://explorer.dcrdata.org/insight/api/utils/estimatefee?nbBlocks=%d", nb)
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var resp map[uint64]float64
	if err := dexnet.Get(ctx, uri, &resp); err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("null response")
	}
	dcrPerKB, ok := resp[nb]
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
	_, est, err := dcr.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.MaxFeeRate)
	return est, err
}

// maxOrder gets the estimate for MaxOrder, and also returns the
// []*compositeUTXO and network fee rate to be used for further order estimation
// without additional calls to listunspent.
func (dcr *ExchangeWallet) maxOrder(lotSize, feeSuggestion, maxFeeRate uint64) (utxos []*compositeUTXO, est *asset.SwapEstimate, err error) {
	if lotSize == 0 {
		return nil, nil, errors.New("cannot divide by lotSize zero")
	}

	utxos, err = dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}
	avail := sumUTXOs(utxos)

	// Start by attempting max lots with a basic fee.
	basicFee := dexdcr.InitTxSize * maxFeeRate
	// NOTE: Split tx is an order-time option. The max order is generally
	// attainable when split is used, regardless of whether they choose it on
	// the order form. Allow the split for max order purposes.
	trySplitTx := true

	// Find the max lots we can fund.
	maxLotsInt := int(avail / (lotSize + basicFee))
	oneLotTooMany := sort.Search(maxLotsInt+1, func(lots int) bool {
		_, _, _, err = dcr.estimateSwap(uint64(lots), lotSize, feeSuggestion, maxFeeRate, utxos, trySplitTx, 1.0)
		// The only failure mode of estimateSwap -> dcr.fund is when there is
		// not enough funds.
		return err != nil
	})

	maxLots := uint64(oneLotTooMany - 1)
	if oneLotTooMany == 0 {
		maxLots = 0
	}

	if maxLots > 0 {
		est, _, _, err = dcr.estimateSwap(maxLots, lotSize, feeSuggestion, maxFeeRate, utxos, trySplitTx, 1.0)
		return utxos, est, err
	}

	return nil, &asset.SwapEstimate{
		FeeReservesPerLot: basicFee,
	}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (dcr *ExchangeWallet) estimateSwap(lots, lotSize, feeSuggestion, maxFeeRate uint64, utxos []*compositeUTXO,
	trySplit bool, feeBump float64) (*asset.SwapEstimate, bool /*split used*/, uint64 /* locked */, error) {
	// If there is a fee bump, the networkFeeRate can be higher than the
	// MaxFeeRate
	bumpedMaxRate := maxFeeRate
	bumpedNetRate := feeSuggestion
	if feeBump > 1 {
		bumpedMaxRate = uint64(math.Ceil(float64(bumpedMaxRate) * feeBump))
		bumpedNetRate = uint64(math.Ceil(float64(bumpedNetRate) * feeBump))
	}

	feeReservesPerLot := bumpedMaxRate * dexdcr.InitTxSize

	val := lots * lotSize
	// The orderEnough func does not account for a split transaction at the
	// start, so it is possible that funding for trySplit would actually choose
	// more UTXOs. Actual order funding accounts for this. For this estimate, we
	// will just not use a split tx if the split-adjusted required funds exceeds
	// the total value of the UTXO selected with this enough closure.
	sum, _, inputsSize, _, _, _, err := tryFund(utxos, orderEnough(val, lots, bumpedMaxRate, trySplit))
	if err != nil {
		return nil, false, 0, err
	}

	avail := sumUTXOs(utxos)
	reserves := dcr.bondReserves.Load()

	digestInputs := func(inputsSize uint32) (reqFunds, maxFees, estHighFees, estLowFees uint64) {
		// NOTE: reqFunds = val + fees, so change (extra) will be sum-reqFunds
		reqFunds = calc.RequiredOrderFunds(val, uint64(inputsSize), lots,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, bumpedMaxRate) // as in tryFund's enough func
		maxFees = reqFunds - val

		estHighFunds := calc.RequiredOrderFunds(val, uint64(inputsSize), lots,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, bumpedNetRate)
		estHighFees = estHighFunds - val

		estLowFunds := calc.RequiredOrderFunds(val, uint64(inputsSize), 1,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, bumpedNetRate) // best means single multi-lot match, even better than batch
		estLowFees = estLowFunds - val
		return
	}

	reqFunds, maxFees, estHighFees, estLowFees := digestInputs(inputsSize)

	// Math for split transactions is a little different.
	if trySplit {
		splitMaxFees := splitTxBaggage * bumpedMaxRate
		splitFees := splitTxBaggage * bumpedNetRate
		reqTotal := reqFunds + splitMaxFees // ~ rather than actually fund()ing again
		// We must consider splitMaxFees otherwise we'd skip the split on
		// account of excess baggage.
		if reqTotal <= sum && sum-reqTotal >= reserves { // avail-sum+extra > reserves
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees + splitMaxFees,
				RealisticBestCase:  estLowFees + splitFees,
				RealisticWorstCase: estHighFees + splitFees,
				FeeReservesPerLot:  feeReservesPerLot,
			}, true, reqFunds, nil // requires reqTotal, but locks reqFunds in the split output
		}
	}

	if sum > avail-reserves { // no split means no change available for reserves
		if trySplit { // if we already tried with a split, that's the best we can do
			return nil, false, 0, errors.New("balance too low to both fund order and maintain bond reserves")
		}
		// Like the fund() method, try with some utxos taken out of the mix for
		// reserves, as precise in value as possible.
		kept := leastOverFund(reserveEnough(reserves), utxos)
		utxos = utxoSetDiff(utxos, kept)
		sum, _, inputsSize, _, _, _, err = tryFund(utxos, orderEnough(val, lots, bumpedMaxRate, false))
		if err != nil { // no joy with the reduced set
			return nil, false, 0, err
		}
		_, maxFees, estHighFees, estLowFees = digestInputs(inputsSize)
	}

	// No split transaction.
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              val,
		MaxFees:            maxFees,
		RealisticBestCase:  estLowFees,
		RealisticWorstCase: estHighFees,
		FeeReservesPerLot:  feeReservesPerLot,
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
	utxos, maxEst, err := dcr.maxOrder(req.LotSize, req.FeeSuggestion, req.MaxFeeRate)
	if err != nil {
		return nil, err
	}
	if maxEst.Lots < req.Lots { // changing options isn't going to fix this, only lots
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
	bump, err := customCfg.feeBump()
	if err != nil {
		return nil, err
	}

	// Get the estimate for the requested number of lots.
	est, _, _, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion,
		req.MaxFeeRate, utxos, split, bump)
	if err != nil {
		dcr.log.Warnf("estimateSwap failure: %v", err)
	}

	// Always offer the split option, even for non-standing orders since
	// immediately spendable change many be desirable regardless.
	opts := []*asset.OrderOption{dcr.splitOption(req, utxos, bump)}

	// Figure out what our maximum available fee bump is, within our 2x hard
	// limit.
	var maxBump float64
	var maxBumpEst *asset.SwapEstimate
	for maxBump = 2.0; maxBump > 1.01; maxBump -= 0.1 {
		if est == nil {
			break
		}
		tryEst, splitUsed, _, err := dcr.estimateSwap(req.Lots, req.LotSize,
			req.FeeSuggestion, req.MaxFeeRate, utxos, split, maxBump)
		// If the split used wasn't the configured value, this option is not
		// available.
		if err == nil && split == splitUsed {
			maxBumpEst = tryEst
			break
		}
	}

	if maxBumpEst != nil {
		noBumpEst, _, _, err := dcr.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion,
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
		Estimate: est, // may be nil so we can present options, which in turn affect estimate feasibility
		Options:  opts,
	}, nil
}

// SingleLotSwapRefundFees returns the fees for a swap and refund transaction
// for a single lot.
func (dcr *ExchangeWallet) SingleLotSwapRefundFees(_ uint32, feeSuggestion uint64, useSafeTxSize bool) (swapFees uint64, refundFees uint64, err error) {
	var numInputs uint64
	if useSafeTxSize {
		numInputs = 12
	} else {
		numInputs = 2
	}
	swapTxSize := dexdcr.InitTxSizeBase + (numInputs * dexdcr.P2PKHInputSize)
	refundTxSize := dexdcr.MsgTxOverhead + dexdcr.TxInOverhead + dexdcr.RefundSigScriptSize + dexdcr.P2PKHOutputSize
	return swapTxSize * feeSuggestion, uint64(refundTxSize) * feeSuggestion, nil
}

// MaxFundingFees returns the maximum funding fees for an order/multi-order.
func (dcr *ExchangeWallet) MaxFundingFees(numTrades uint32, feeRate uint64, options map[string]string) uint64 {
	customCfg, err := decodeFundMultiOptions(options)
	if err != nil {
		dcr.log.Errorf("Error decoding multi-fund settings: %v", err)
		return 0
	}
	if !customCfg.Split {
		return 0
	}

	const numInputs = 12 // plan for lots of inputs to get a safe estimate
	splitTxSize := dexdcr.MsgTxOverhead + (numInputs * dexdcr.P2PKHInputSize) + (uint64(numTrades+1) * dexdcr.P2PKHOutputSize)
	return splitTxSize * dcr.config().feeRateLimit
}

// splitOption constructs an *asset.OrderOption with customized text based on the
// difference in fees between the configured and test split condition.
func (dcr *ExchangeWallet) splitOption(req *asset.PreSwapForm, utxos []*compositeUTXO, bump float64) *asset.OrderOption {
	opt := &asset.OrderOption{
		ConfigOption: asset.ConfigOption{
			Key:           splitKey,
			DisplayName:   "Pre-size Funds",
			IsBoolean:     true,
			DefaultValue:  dcr.config().useSplitTx, // not nil interface
			ShowByDefault: true,
		},
		Boolean: &asset.BooleanConfig{},
	}

	noSplitEst, _, noSplitLocked, err := dcr.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, false, bump)
	if err != nil {
		dcr.log.Errorf("estimateSwap (no split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate without a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}
	splitEst, splitUsed, splitLocked, err := dcr.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, true, bump)
	if err != nil {
		dcr.log.Errorf("estimateSwap (with split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate with a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}

	if !splitUsed || splitLocked >= noSplitLocked { // locked check should be redundant
		opt.Boolean.Reason = "avoids no DCR overlock for this order (ignored)"
		opt.Description = "A split transaction for this order avoids no DCR overlock, but adds additional fees."
		opt.DefaultValue = false
		return opt // not enabled by default, but explain why
	}

	overlock := noSplitLocked - splitLocked
	pctChange := (float64(splitEst.RealisticWorstCase)/float64(noSplitEst.RealisticWorstCase) - 1) * 100
	if pctChange > 1 {
		opt.Boolean.Reason = fmt.Sprintf("+%d%% fees, avoids %s DCR overlock", int(math.Round(pctChange)), amount(overlock))
	} else {
		opt.Boolean.Reason = fmt.Sprintf("+%.1f%% fees, avoids %s DCR overlock", pctChange, amount(overlock))
	}

	xtraFees := splitEst.RealisticWorstCase - noSplitEst.RealisticWorstCase
	opt.Description = fmt.Sprintf("Using a split transaction may prevent temporary overlock of %s DCR, but for additional fees of %s DCR",
		amount(overlock), amount(xtraFees))

	return opt
}

func (dcr *ExchangeWallet) preRedeem(numLots, feeSuggestion uint64, options map[string]string) (*asset.PreRedeem, error) {
	cfg := dcr.config()

	feeRate := feeSuggestion
	if feeRate == 0 { // or just document that the caller must set it?
		feeRate = dcr.targetFeeRateWithFallback(cfg.redeemConfTarget, feeSuggestion)
	}
	// Best is one transaction with req.Lots inputs and 1 output.
	var best uint64 = dexdcr.MsgTxOverhead
	// Worst is req.Lots transactions, each with one input and one output.
	var worst uint64 = dexdcr.MsgTxOverhead * numLots
	var inputSize uint64 = dexdcr.TxInOverhead + dexdcr.RedeemSwapSigScriptSize
	var outputSize uint64 = dexdcr.P2PKHOutputSize
	best += inputSize*numLots + outputSize
	worst += (inputSize + outputSize) * numLots

	// Read the order options.
	customCfg := new(redeemOptions)
	err := config.Unmapify(options, customCfg)
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

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (dcr *ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return dcr.preRedeem(req.Lots, req.FeeSuggestion, req.SelectedOptions)
}

// SingleLotRedeemFees returns the fees for a redeem transaction for a single lot.
func (dcr *ExchangeWallet) SingleLotRedeemFees(_ uint32, feeSuggestion uint64) (uint64, error) {
	preRedeem, err := dcr.preRedeem(1, feeSuggestion, nil)
	if err != nil {
		return 0, err
	}

	dcr.log.Tracef("SingleLotRedeemFees: worst case = %d, feeSuggestion = %d", preRedeem.Estimate.RealisticWorstCase, feeSuggestion)

	return preRedeem.Estimate.RealisticWorstCase, nil
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// The returned []dex.Bytes contains the redeem scripts for the selected coins.
// Equal number of coins and redeemed scripts must be returned. A nil or empty
// dex.Bytes should be appended to the redeem scripts collection for coins with
// no redeem script.
func (dcr *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	cfg := dcr.config()

	// Consumer checks dex asset version, so maybe this is not our job:
	// if ord.DEXConfig.Version != dcr.Info().Version {
	// 	return nil, nil, fmt.Errorf("asset version mismatch: server = %d, client = %d",
	// 		ord.DEXConfig.Version, dcr.Info().Version)
	// }
	if ord.Value == 0 {
		return nil, nil, 0, fmt.Errorf("cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, 0, fmt.Errorf("cannot fund a zero-lot order")
	}
	if ord.FeeSuggestion > ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf("fee suggestion %d > max fee rate %d", ord.FeeSuggestion, ord.MaxFeeRate)
	}
	if ord.FeeSuggestion > cfg.feeRateLimit {
		return nil, nil, 0, fmt.Errorf("suggested fee > configured limit. %d > %d", ord.FeeSuggestion, cfg.feeRateLimit)
	}
	// Check wallet's fee rate limit against server's max fee rate
	if cfg.feeRateLimit < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(BipID), ord.MaxFeeRate, cfg.feeRateLimit)
	}

	customCfg := new(swapOptions)
	err := config.Unmapify(ord.Options, customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("Error parsing swap options")
	}

	// Check ord.Options for a FeeBump here
	bumpedMaxRate, err := calcBumpedRate(ord.MaxFeeRate, customCfg.FeeBump)
	if err != nil {
		dcr.log.Errorf("calcBumpRate error: %v", err)
	}

	// If a split is not requested, but is forced, create an extra output from
	// the split tx to help avoid a forced split in subsequent orders.
	var extraSplitOutput uint64
	useSplit := cfg.useSplitTx
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	changeForReserves := useSplit && dcr.wallet.Accounts().UnmixedAccount == ""
	reserves := dcr.bondReserves.Load()
	coins, redeemScripts, sum, inputsSize, err := dcr.fund(reserves,
		orderEnough(ord.Value, ord.MaxSwapCount, bumpedMaxRate, changeForReserves))
	if err != nil {
		if !changeForReserves && reserves > 0 { // split not selected, or it's a mixing account where change isn't usable
			// Force a split if funding failure may be due to reserves.
			dcr.log.Infof("Retrying order funding with a forced split transaction to help respect reserves.")
			useSplit = true
			keepForSplitToo := reserves + (bumpedMaxRate * dexdcr.P2PKHInputSize) // so we fail before split() if it's really that tight
			coins, redeemScripts, sum, inputsSize, err = dcr.fund(keepForSplitToo,
				orderEnough(ord.Value, ord.MaxSwapCount, bumpedMaxRate, useSplit))
			// And make an extra output for the reserves amount plus additional
			// fee buffer (double) to help avoid this for a while in the future.
			// This also deals with mixing wallets not having usable change.
			extraSplitOutput = reserves + bondsFeeBuffer(cfg.feeRateLimit)
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error funding order value of %s DCR: %w",
				amount(ord.Value), err)
		}
	}

	// Send a split, if preferred or required.
	if useSplit {
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
			if rawFeeRate > ord.MaxFeeRate {
				rawFeeRate = ord.MaxFeeRate
			}
		}
		splitFeeRate, err := calcBumpedRate(rawFeeRate, customCfg.FeeBump)
		if err != nil {
			dcr.log.Errorf("calcBumpRate error: %v", err)
		}

		splitCoins, split, fees, err := dcr.split(ord.Value, ord.MaxSwapCount, coins,
			inputsSize, splitFeeRate, bumpedMaxRate, extraSplitOutput)
		if err != nil { // potentially try again with extraSplitOutput=0 if it wasn't already
			if _, errRet := dcr.returnCoins(coins); errRet != nil {
				dcr.log.Warnf("Failed to unlock funding coins %v: %v", coins, errRet)
			}
			return nil, nil, 0, err
		}
		if split {
			return splitCoins, []dex.Bytes{nil}, fees, nil // no redeem script required for split tx output
		}
		return splitCoins, redeemScripts, 0, nil // splitCoins == coins
	}

	dcr.log.Infof("Funding %s DCR order with coins %v worth %s", amount(ord.Value), coins, amount(sum))

	return coins, redeemScripts, 0, nil
}

// fundMultiOptions are the possible order options when calling FundMultiOrder.
type fundMultiOptions struct {
	// Split, if true, and multi-order cannot be funded with the existing UTXOs
	// in the wallet without going over the maxLock limit, a split transaction
	// will be created with one output per order.
	//
	// Use the multiSplitKey const defined above in the options map to set this option.
	Split bool `ini:"multisplit"`
	// SplitBuffer, if set, will instruct the wallet to add a buffer onto each
	// output of the multi-order split transaction (if the split is needed).
	// SplitBuffer is defined as a percentage of the output. If a .1 BTC output
	// is required for an order and SplitBuffer is set to 5, a .105 BTC output
	// will be created.
	//
	// The motivation for this is to assist market makers in having to do the
	// least amount of splits as possible. It is useful when DCR is the quote
	// asset on a market, and the price is increasing. During a market maker's
	// operation, it will frequently have to cancel and replace orders as the
	// rate moves. If BTC is the quote asset on a market, and the rate has
	// lightly increased, the market maker will need to lock slightly more of
	// the quote asset for the same amount of lots of the base asset. If there
	// is no split buffer, this may necessitate a new split transaction.
	//
	// Use the multiSplitBufferKey const defined above in the options map to set this.
	SplitBuffer float64 `ini:"multisplitbuffer"`
}

func decodeFundMultiOptions(options map[string]string) (*fundMultiOptions, error) {
	opts := new(fundMultiOptions)
	return opts, config.Unmapify(options, opts)
}

// orderWithLeastOverFund returns the index of the order from a slice of orders
// that requires the least over-funding without using more than maxLock. It
// also returns the UTXOs that were used to fund the order. If none can be
// funded without using more than maxLock, -1 is returned.
func (dcr *ExchangeWallet) orderWithLeastOverFund(maxLock, feeRate uint64, orders []*asset.MultiOrderValue, utxos []*compositeUTXO) (orderIndex int, leastOverFundingUTXOs []*compositeUTXO) {
	minOverFund := uint64(math.MaxUint64)
	orderIndex = -1
	for i, value := range orders {
		enough := orderEnough(value.Value, value.MaxSwapCount, feeRate, false)
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
func (dcr *ExchangeWallet) fundsRequiredForMultiOrders(orders []*asset.MultiOrderValue, feeRate uint64, splitBuffer float64) ([]uint64, uint64) {
	requiredForOrders := make([]uint64, len(orders))
	var totalRequired uint64

	for i, value := range orders {
		req := calc.RequiredOrderFunds(value.Value, dexdcr.P2PKHInputSize, value.MaxSwapCount,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, feeRate)
		req = uint64(math.Round(float64(req) * (100 + splitBuffer) / 100))
		requiredForOrders[i] = req
		totalRequired += req
	}

	return requiredForOrders, totalRequired
}

// fundMultiBestEffort makes a best effort to fund every order. If it is not
// possible, it returns coins for the orders that could be funded. The coins
// that fund each order are returned in the same order as the values that were
// passed in. If a split is allowed and all orders cannot be funded, nil slices
// are returned.
func (dcr *ExchangeWallet) fundMultiBestEffort(keep, maxLock uint64, values []*asset.MultiOrderValue,
	maxFeeRate uint64, splitAllowed bool) ([]asset.Coins, [][]dex.Bytes, []*fundingCoin, error) {
	utxos, err := dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	var avail uint64
	for _, utxo := range utxos {
		avail += toAtoms(utxo.rpc.Amount)
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
			orderIndex, fundingUTXOs := dcr.orderWithLeastOverFund(maxLock-totalFunded, maxFeeRate, remainingOrders, remainingUTXOs)
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
			enough := orderEnough(value.Value, value.MaxSwapCount, maxFeeRate, false)

			var fundingUTXOs []*compositeUTXO
			if maxLock > 0 {
				if maxLock < totalFunded {
					// Should never happen unless there is a bug in leastOverFundWithLimit
					dcr.log.Errorf("maxLock < totalFunded. %d < %d", maxLock, totalFunded)
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

	returnValues := func(allFundingUTXOs [][]*compositeUTXO) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingCoins []*fundingCoin, err error) {
		coins = make([]asset.Coins, len(allFundingUTXOs))
		fundingCoins = make([]*fundingCoin, 0, len(allFundingUTXOs))
		redeemScripts = make([][]dex.Bytes, len(allFundingUTXOs))
		for i, fundingUTXOs := range allFundingUTXOs {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, output := range fundingUTXOs {
				txHash, err := chainhash.NewHashFromStr(output.rpc.TxID)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("error decoding txid: %w", err)
				}
				coins[i][j] = newOutput(txHash, output.rpc.Vout, toAtoms(output.rpc.Amount), output.rpc.Tree)
				fundingCoins = append(fundingCoins, &fundingCoin{
					op:   newOutput(txHash, output.rpc.Vout, toAtoms(output.rpc.Amount), output.rpc.Tree),
					addr: output.rpc.Address,
				})
				redeemScript, err := hex.DecodeString(output.rpc.RedeemScript)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("error decoding redeem script for %s, script = %s: %w",
						txHash, output.rpc.RedeemScript, err)
				}
				redeemScripts[i][j] = redeemScript
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
func (dcr *ExchangeWallet) fundMultiSplitTx(orders []*asset.MultiOrderValue, utxos []*compositeUTXO,
	splitTxFeeRate, maxFeeRate uint64, splitBuffer float64, keep, maxLock uint64) (bool, asset.Coins, []*fundingCoin) {
	_, totalOutputRequired := dcr.fundsRequiredForMultiOrders(orders, maxFeeRate, splitBuffer)

	var splitTxSizeWithoutInputs uint32 = dexdcr.MsgTxOverhead
	numOutputs := len(orders)
	if keep > 0 {
		numOutputs++
	}
	splitTxSizeWithoutInputs += uint32(dexdcr.P2PKHOutputSize * numOutputs)

	enough := func(sum uint64, size uint32, utxo *compositeUTXO) (bool, uint64) {
		totalSum := sum + toAtoms(utxo.rpc.Amount)
		totalSize := size + utxo.input.Size()
		splitTxFee := uint64(splitTxSizeWithoutInputs+totalSize) * splitTxFeeRate
		req := totalOutputRequired + splitTxFee
		return totalSum >= req, totalSum - req
	}

	var avail uint64
	for _, utxo := range utxos {
		avail += toAtoms(utxo.rpc.Amount)
	}

	fundSplitCoins, _, spents, _, inputsSize, err := dcr.fundInternalWithUTXOs(utxos, keep, enough, false)
	if err != nil {
		return false, nil, nil
	}

	if maxLock > 0 {
		totalSize := inputsSize + uint64(splitTxSizeWithoutInputs)
		if totalOutputRequired+(totalSize*splitTxFeeRate) > maxLock {
			return false, nil, nil
		}
	}

	return true, fundSplitCoins, spents
}

// submitMultiSplitTx creates a multi-split transaction using fundingCoins with
// one output for each order, and submits it to the network.
func (dcr *ExchangeWallet) submitMultiSplitTx(fundingCoins asset.Coins, _ /* spents */ []*fundingCoin, orders []*asset.MultiOrderValue,
	maxFeeRate, splitTxFeeRate uint64, splitBuffer float64) ([]asset.Coins, uint64, error) {
	baseTx := wire.NewMsgTx()
	_, err := dcr.addInputCoins(baseTx, fundingCoins)
	if err != nil {
		return nil, 0, err
	}

	accts := dcr.wallet.Accounts()
	getAddr := func() (stdaddr.Address, error) {
		if accts.TradingAccount != "" {
			return dcr.wallet.ExternalAddress(dcr.ctx, accts.TradingAccount)
		}
		return dcr.wallet.ExternalAddress(dcr.ctx, accts.PrimaryAccount)
	}

	requiredForOrders, _ := dcr.fundsRequiredForMultiOrders(orders, maxFeeRate, splitBuffer)
	outputAddresses := make([]stdaddr.Address, len(orders))
	for i, req := range requiredForOrders {
		outputAddr, err := getAddr()
		if err != nil {
			return nil, 0, err
		}
		outputAddresses[i] = outputAddr
		payScriptVer, payScript := outputAddr.PaymentScript()
		txOut := newTxOut(int64(req), payScriptVer, payScript)
		baseTx.AddTxOut(txOut)
	}

	tx, err := dcr.sendWithReturn(baseTx, splitTxFeeRate, -1)
	if err != nil {
		return nil, 0, err
	}

	coins := make([]asset.Coins, len(orders))
	fcs := make([]*fundingCoin, len(orders))
	for i := range coins {
		coins[i] = asset.Coins{newOutput(tx.CachedTxHash(), uint32(i), uint64(tx.TxOut[i].Value), wire.TxTreeRegular)}
		fcs[i] = &fundingCoin{
			op:   newOutput(tx.CachedTxHash(), uint32(i), uint64(tx.TxOut[i].Value), wire.TxTreeRegular),
			addr: outputAddresses[i].String(),
		}
	}
	err = dcr.lockFundingCoins(fcs)
	if err != nil {
		return nil, 0, fmt.Errorf("lockFundingCoins: %w", err)
	}

	var totalOut uint64
	for _, txOut := range tx.TxOut {
		totalOut += uint64(txOut.Value)
	}

	var totalIn uint64
	for _, txIn := range fundingCoins {
		totalIn += txIn.Value()
	}

	txHash := tx.CachedTxHash()
	dcr.addTxToHistory(&asset.WalletTransaction{
		Type: asset.Split,
		ID:   txHash.String(),
		Fees: totalIn - totalOut,
	}, txHash, true)

	return coins, totalIn - totalOut, nil
}

// fundMultiWithSplit creates a split transaction to fund multiple orders. It
// attempts to fund as many of the orders as possible without a split transaction,
// and only creates a split transaction for the remaining orders. This is only
// called after it has been determined that all of the orders cannot be funded
// without a split transaction.
func (dcr *ExchangeWallet) fundMultiWithSplit(keep, maxLock uint64, values []*asset.MultiOrderValue,
	splitTxFeeRate, maxFeeRate uint64, splitBuffer float64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	utxos, err := dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	var avail uint64
	for _, utxo := range utxos {
		avail += toAtoms(utxo.rpc.Amount)
	}

	canFund, splitCoins, splitSpents := dcr.fundMultiSplitTx(values, utxos, splitTxFeeRate, maxFeeRate, splitBuffer, keep, maxLock)
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
		orderIndex, fundingUTXOs := dcr.orderWithLeastOverFund(maxLock-totalFunded, maxFeeRate, remainingOrders, remainingUTXOs)
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
			canFund, newSplitCoins, newSpents := dcr.fundMultiSplitTx(newRemainingOrders, remainingUTXOs,
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
		splitOutputCoins, splitFees, err = dcr.submitMultiSplitTx(splitCoins,
			splitSpents, remainingOrders, maxFeeRate, splitTxFeeRate, splitBuffer)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating split transaction: %w", err)
		}
	}

	coins := make([]asset.Coins, len(values))
	redeemScripts := make([][]dex.Bytes, len(values))
	spents := make([]*fundingCoin, 0, len(values))

	var splitIndex int

	for i := range values {
		if fundingUTXOs, ok := indexToFundingCoins[i]; ok {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, unspent := range fundingUTXOs {
				txHash, err := chainhash.NewHashFromStr(unspent.rpc.TxID)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("error decoding txid from rpc server %s: %w", unspent.rpc.TxID, err)
				}
				output := newOutput(txHash, unspent.rpc.Vout, toAtoms(unspent.rpc.Amount), unspent.rpc.Tree)
				coins[i][j] = output
				fc := &fundingCoin{
					op:   output,
					addr: unspent.rpc.Address,
				}
				spents = append(spents, fc)
				redeemScript, err := hex.DecodeString(unspent.rpc.RedeemScript)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("error decoding redeem script for %s, script = %s: %w",
						txHash, unspent.rpc.RedeemScript, err)
				}
				redeemScripts[i][j] = redeemScript
			}
		} else {
			coins[i] = splitOutputCoins[splitIndex]
			redeemScripts[i] = []dex.Bytes{nil}
			splitIndex++
		}
	}

	err = dcr.lockFundingCoins(spents)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("lockFundingCoins: %w", err)
	}

	return coins, redeemScripts, splitFees, nil
}

// fundMulti first attempts to fund each of the orders with with the available
// UTXOs. If a split is not allowed, it will fund the orders that it was able
// to fund. If splitting is allowed, a split transaction will be created to fund
// all of the orders.
func (dcr *ExchangeWallet) fundMulti(maxLock uint64, values []*asset.MultiOrderValue, splitTxFeeRate, maxFeeRate uint64, allowSplit bool, splitBuffer float64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock()

	reserves := dcr.bondReserves.Load()

	coins, redeemScripts, fundingCoins, err := dcr.fundMultiBestEffort(reserves, maxLock, values, maxFeeRate, allowSplit)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(coins) == len(values) || !allowSplit {
		err = dcr.lockFundingCoins(fundingCoins)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("lockFundingCoins: %w", err)
		}
		return coins, redeemScripts, 0, nil
	}

	return dcr.fundMultiWithSplit(reserves, maxLock, values, splitTxFeeRate, maxFeeRate, splitBuffer)
}

func (dcr *ExchangeWallet) FundMultiOrder(mo *asset.MultiOrder, maxLock uint64) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingFees uint64, err error) {
	var totalRequiredForOrders uint64
	for _, value := range mo.Values {
		if value.Value == 0 {
			return nil, nil, 0, fmt.Errorf("cannot fund value = 0")
		}
		if value.MaxSwapCount == 0 {
			return nil, nil, 0, fmt.Errorf("cannot fund zero-lot order")
		}
		req := calc.RequiredOrderFunds(value.Value, dexdcr.P2PKHInputSize, value.MaxSwapCount,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, mo.MaxFeeRate)
		totalRequiredForOrders += req
	}

	if maxLock < totalRequiredForOrders && maxLock != 0 {
		return nil, nil, 0, fmt.Errorf("maxLock < totalRequiredForOrders (%d < %d)", maxLock, totalRequiredForOrders)
	}

	if mo.FeeSuggestion > mo.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf("fee suggestion %d > max fee rate %d", mo.FeeSuggestion, mo.MaxFeeRate)
	}

	cfg := dcr.config()
	if cfg.feeRateLimit < mo.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(BipID), mo.MaxFeeRate, cfg.feeRateLimit)
	}

	bal, err := dcr.Balance()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting balance: %w", err)
	}
	if bal.Available < totalRequiredForOrders {
		return nil, nil, 0, fmt.Errorf("insufficient funds. %d < %d", bal.Available, totalRequiredForOrders)
	}

	customCfg, err := decodeFundMultiOptions(mo.Options)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error decoding options: %w", err)
	}

	return dcr.fundMulti(maxLock, mo.Values, mo.FeeSuggestion, mo.MaxFeeRate, customCfg.Split, customCfg.SplitBuffer)
}

// fundOrder finds coins from a set of UTXOs for a specified value. This method
// is the same as "fund", except the UTXOs must be passed in, and fundingMtx
// must be held by the caller.
func (dcr *ExchangeWallet) fundInternalWithUTXOs(utxos []*compositeUTXO, keep uint64, // leave utxos for this reserve amt
	enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64), lock bool) (
	coins asset.Coins, redeemScripts []dex.Bytes, spents []*fundingCoin, sum, size uint64, err error) {
	avail := sumUTXOs(utxos)
	if keep > avail { // skip utxo selection if we can't possibly make reserves
		return nil, nil, nil, 0, 0, asset.ErrInsufficientBalance
	}

	var sz uint32

	// First take some UTXOs out of the mix for any keep amount. Select these
	// with the objective of being as close to the amount as possible, unlike
	// tryFund that minimizes the number of UTXOs chosen. By doing this first,
	// we may be making the order spend a larger number of UTXOs, but we
	// mitigate subsequent order funding failure due to reserves because we know
	// this order will leave behind sufficient UTXOs without relying on change.
	if keep > 0 {
		kept := leastOverFund(reserveEnough(keep), utxos)
		dcr.log.Debugf("Setting aside %v DCR in %d UTXOs to respect the %v DCR reserved amount",
			toDCR(sumUTXOs(kept)), len(kept), toDCR(keep))
		utxosPruned := utxoSetDiff(utxos, kept)
		sum, _, sz, coins, spents, redeemScripts, err = tryFund(utxosPruned, enough)
		if err != nil { // try with the full set
			dcr.log.Debugf("Unable to fund order with UTXOs set aside (%v), trying again with full UTXO set.", err)
		} // else spents is populated
	}
	if len(spents) == 0 { // either keep is zero or it failed with utxosPruned
		// Without utxos set aside for keep, we have to consider any spendable
		// change (extra) that the enough func grants us.
		var extra uint64
		sum, extra, sz, coins, spents, redeemScripts, err = tryFund(utxos, enough)
		if err != nil {
			return nil, nil, nil, 0, 0, err
		}
		if avail-sum+extra < keep {
			return nil, nil, nil, 0, 0, asset.ErrInsufficientBalance
		}
		// else we got lucky with the legacy funding approach and there was
		// either available unspent or the enough func granted spendable change.
		if keep > 0 && extra > 0 {
			dcr.log.Debugf("Funding succeeded with %f DCR in spendable change.", toDCR(extra))
		}
	}

	if lock {
		err = dcr.lockFundingCoins(spents)
		if err != nil {
			return nil, nil, nil, 0, 0, err
		}
	}
	return coins, redeemScripts, spents, sum, uint64(sz), nil
}

// fund finds coins for the specified value. A function is provided that can
// check whether adding the provided output would be enough to satisfy the
// needed value. Preference is given to selecting coins with 1 or more confs,
// falling back to 0-conf coins where there are not enough 1+ confs coins. If
// change should not be considered "kept" (e.g. no preceding split txn, or
// mixing sends change to umixed account where it is unusable for reserves),
// caller should return 0 extra from enough func.
func (dcr *ExchangeWallet) fund(keep uint64, // leave utxos for this reserve amt
	enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64)) (
	coins asset.Coins, redeemScripts []dex.Bytes, sum, size uint64, err error) {

	// Keep a consistent view of spendable and locked coins in the wallet and
	// the fundingCoins map to make this safe for concurrent use.
	dcr.fundingMtx.Lock()         // before listing unspents in wallet
	defer dcr.fundingMtx.Unlock() // hold until lockFundingCoins (wallet and map)

	utxos, err := dcr.spendableUTXOs()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	coins, redeemScripts, _, sum, size, err = dcr.fundInternalWithUTXOs(utxos, keep, enough, true)
	return coins, redeemScripts, sum, size, err
}

// spendableUTXOs generates a slice of spendable *compositeUTXO.
func (dcr *ExchangeWallet) spendableUTXOs() ([]*compositeUTXO, error) {
	accts := dcr.wallet.Accounts()
	unspents, err := dcr.wallet.Unspents(dcr.ctx, accts.PrimaryAccount)
	if err != nil {
		return nil, err
	}
	if accts.TradingAccount != "" {
		// Trading account may contain spendable utxos such as unspent split tx
		// outputs that are unlocked/returned. TODO: Care should probably be
		// taken to ensure only unspent split tx outputs are selected and other
		// unmixed outputs in the trading account are ignored.
		tradingAcctSpendables, err := dcr.wallet.Unspents(dcr.ctx, accts.TradingAccount)
		if err != nil {
			return nil, err
		}
		unspents = append(unspents, tradingAcctSpendables...)
	}
	if len(unspents) == 0 {
		return nil, fmt.Errorf("insufficient funds. 0 DCR available to spend in account %q", accts.PrimaryAccount)
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

// tryFund attempts to use the provided UTXO set to satisfy the enough function
// with the fewest number of inputs. The selected utxos are not locked. If the
// requirement can be satisfied without 0-conf utxos, that set will be selected
// regardless of whether the 0-conf inclusive case would be cheaper. The
// provided UTXOs must be sorted in ascending order by value.
func tryFund(utxos []*compositeUTXO,
	enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64)) (
	sum, extra uint64, size uint32, coins asset.Coins, spents []*fundingCoin, redeemScripts []dex.Bytes, err error) {

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
		ok, _ := enough(sum, size, utxo)
		return ok
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
			// No need to check idx == n. We already verified that the last
			// utxo passes above.
			final := okUTXOs[idx]
			_, extra = enough(sum, size, final) // sort.Search might not have called isEnough for this utxo last
			if err = addUTXO(final); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// First try with confs>0.
	ok, err := tryUTXOs(1)
	if err != nil {
		return 0, 0, 0, nil, nil, nil, err
	}

	// Fallback to allowing 0-conf outputs.
	if !ok {
		ok, err = tryUTXOs(0)
		if err != nil {
			return 0, 0, 0, nil, nil, nil, err
		}
		if !ok {
			return 0, 0, 0, nil, nil, nil, fmt.Errorf("not enough to cover requested funds. "+
				"%s DCR available in %d UTXOs (%w)", amount(sum), len(coins), asset.ErrInsufficientBalance)
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
	splitFeeRate, bumpedMaxRate, extraOutput uint64) (asset.Coins, bool, uint64, error) {

	// Calculate the extra fees associated with the additional inputs, outputs,
	// and transaction overhead, and compare to the excess that would be locked.
	baggageFees := bumpedMaxRate * splitTxBaggage
	if extraOutput > 0 {
		baggageFees += bumpedMaxRate * dexdcr.P2PKHOutputSize
	}

	var coinSum uint64
	for _, coin := range coins {
		coinSum += coin.Value()
	}

	valStr := amount(value).String()

	excess := coinSum - calc.RequiredOrderFunds(value, inputsSize, lots,
		dexdcr.InitTxSizeBase, dexdcr.InitTxSize, bumpedMaxRate)

	if baggageFees > excess {
		dcr.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. %s > %s.",
			amount(baggageFees), amount(excess))
		dcr.log.Infof("Funding %s DCR order with coins %v worth %s", valStr, coins, amount(coinSum))
		// NOTE: The caller may be expecting a split to happen to maintain
		// reserves via the change from the split, but the amount held locked
		// when skipping the split in this case is roughly equivalent to the
		// loss to fees in a split. This trivial amount is of no concern because
		// the reserves should be buffered for amounts much larger than the fees
		// on a single transaction.
		return coins, false, 0, nil
	}

	// Generate an address to receive the sized outputs. If mixing is enabled on
	// the wallet, generate the address from the external branch of the trading
	// account. The external branch is used so that if this split output isn't
	// spent, it won't be transferred to the unmixed account for re-mixing.
	// Instead, it'll simply be unlocked in the trading account and can thus be
	// used to fund future orders.
	accts := dcr.wallet.Accounts()
	getAddr := func() (stdaddr.Address, error) {
		if accts.TradingAccount != "" {
			return dcr.wallet.ExternalAddress(dcr.ctx, accts.TradingAccount)
		}
		return dcr.wallet.ExternalAddress(dcr.ctx, accts.PrimaryAccount)
	}
	addr, err := getAddr()
	if err != nil {
		return nil, false, 0, fmt.Errorf("error creating split transaction address: %w", err)
	}

	var addr2 stdaddr.Address
	if extraOutput > 0 {
		addr2, err = getAddr()
		if err != nil {
			return nil, false, 0, fmt.Errorf("error creating secondary split transaction address: %w", err)
		}
	}

	reqFunds := calc.RequiredOrderFunds(value, dexdcr.P2PKHInputSize, lots,
		dexdcr.InitTxSizeBase, dexdcr.InitTxSize, bumpedMaxRate)

	dcr.fundingMtx.Lock()         // before generating the new output in sendCoins
	defer dcr.fundingMtx.Unlock() // after locking it (wallet and map)

	msgTx, sentVal, err := dcr.sendCoins(coins, addr, addr2, reqFunds, extraOutput, splitFeeRate, false)
	if err != nil {
		return nil, false, 0, fmt.Errorf("error sending split transaction: %w", err)
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

	totalOut := uint64(0)
	for i := 0; i < len(msgTx.TxOut); i++ {
		totalOut += uint64(msgTx.TxOut[i].Value)
	}

	txHash := msgTx.CachedTxHash()
	dcr.addTxToHistory(&asset.WalletTransaction{
		Type: asset.Split,
		ID:   txHash.String(),
		Fees: coinSum - totalOut,
	}, txHash, true)

	dcr.log.Infof("Funding %s DCR order with split output coin %v from original coins %v", valStr, op, coins)
	dcr.log.Infof("Sent split transaction %s to accommodate swap of size %s + fees = %s DCR",
		op.txHash(), valStr, amount(reqFunds))

	return asset.Coins{op}, true, coinSum - totalOut, nil
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

func (dcr *ExchangeWallet) unlockFundingCoins(fCoins []*fundingCoin) error {
	wireOPs := make([]*wire.OutPoint, 0, len(fCoins))
	for _, c := range fCoins {
		wireOPs = append(wireOPs, wire.NewOutPoint(c.op.txHash(), c.op.vout(), c.op.tree))
	}
	err := dcr.wallet.LockUnspent(dcr.ctx, true, wireOPs)
	if err != nil {
		return err
	}
	for _, c := range fCoins {
		delete(dcr.fundingCoins, c.op.pt)
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
	accts := dcr.wallet.Accounts()
	if err != nil || accts.UnmixedAccount == "" {
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
		if addrInfo.Branch == acctInternalBranch && addrInfo.Account == accts.TradingAccount {
			coinsToTransfer = append(coinsToTransfer, coin.op)
		}
	}

	if len(coinsToTransfer) > 0 {
		tx, totalSent, err := dcr.sendAll(coinsToTransfer, accts.UnmixedAccount)
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
	if swaps.FeeRate == 0 {
		return nil, nil, 0, fmt.Errorf("cannot send swap with with zero fee rate")
	}

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
	tradingAccount := dcr.wallet.Accounts().TradingAccount
	if swaps.LockChange && tradingAccount != "" {
		// Change will likely be used to fund more swaps, send to trading
		// account.
		changeAcct = tradingAccount
	}
	msgTx, change, changeAddr, fees, err := dcr.signTxAndAddChange(baseTx, feeRate, -1, changeAcct)
	if err != nil {
		return nil, nil, 0, err
	}

	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.CachedTxHash()
	for i, contract := range swaps.Contracts {
		output := newOutput(txHash, uint32(i), contract.Value, wire.TxTreeRegular)
		signedRefundTx, _, _, err := dcr.refundTx(output.ID(), contracts[i], contract.Value, refundAddrs[i], swaps.FeeRate)
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
	_, err = dcr.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}

	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Swap,
		ID:     txHash.String(),
		Amount: totalOut,
		Fees:   fees,
	}, txHash, true)

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
	txOut, _, err := dcr.makeExternalOut(dcr.depositAccount(), totalIn-fee)
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
	txHash, err := dcr.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}

	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Redeem,
		ID:     txHash.String(),
		Amount: totalIn,
		Fees:   fee,
	}, txHash, true)

	coinIDs := make([]dex.Bytes, 0, len(form.Redemptions))
	dcr.mempoolTxsMtx.Lock()
	for i := range form.Redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
		var secretHash [32]byte
		copy(secretHash[:], form.Redemptions[i].Spends.SecretHash)
		dcr.mempoolTxs[secretHash] = &mempoolTx{txHash: *txHash, firstSeen: time.Now(), txType: asset.CTRedeem}
	}
	dcr.mempoolTxsMtx.Unlock()
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
		if err = blockchain.CheckTransactionSanity(contractTx, uint64(dcr.chainParams.MaxTxSize)); err != nil {
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
	if stake.DetermineTxType(msgTx) != stake.TxTypeRegular {
		return wire.TxTreeStake
	}
	return wire.TxTreeRegular
}

// lookupTxOutput attempts to find and return details for the specified output,
// first checking for an unspent output and if not found, checking wallet txs.
// Returns asset.CoinNotFoundError if the output is not found.
//
// NOTE: This method is only guaranteed to return results for outputs belonging
// to transactions that are tracked by the wallet, although full node wallets
// are able to look up non-wallet outputs that are unspent.
//
// If the value of the spent flag is -1, it could not be determined with the SPV
// wallet if it is spent, and the caller should perform a block filters scan to
// locate a (mined) spending transaction if needed.
func (dcr *ExchangeWallet) lookupTxOutput(ctx context.Context, txHash *chainhash.Hash, vout uint32) (txOut *wire.TxOut, confs uint32, spent int8, err error) {
	// Check for an unspent output.
	output, err := dcr.wallet.UnspentOutput(ctx, txHash, vout, wire.TxTreeUnknown)
	if err == nil {
		return output.TxOut, output.Confirmations, 0, nil
	} else if !errors.Is(err, asset.CoinNotFoundError) {
		return nil, 0, 0, err
	}

	// Check wallet transactions.
	tx, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, 0, 0, err // asset.CoinNotFoundError if not found
	}
	if int(vout) >= len(tx.MsgTx.TxOut) {
		return nil, 0, 0, fmt.Errorf("tx %s has no output at %d", txHash, vout)
	}

	txOut = tx.MsgTx.TxOut[vout]
	confs = uint32(tx.Confirmations)

	// We have the requested output. Check if it is spent.
	if confs == 0 {
		// Only counts as spent if spent in a mined transaction,
		// unconfirmed tx outputs can't be spent in a mined tx.

		// There is a dcrwallet bug by which the user can shut down at the wrong
		// time and a tx will never be marked as confirmed. We'll force a
		// cfilter scan for unconfirmed txs until the bug is resolved.
		// https://github.com/decred/dcrdex/pull/2444
		if dcr.wallet.SpvMode() {
			return txOut, confs, -1, nil
		}
		return txOut, confs, 0, nil
	}

	if !dcr.wallet.SpvMode() {
		// A mined output that is not found by wallet.UnspentOutput
		// is spent if the wallet is connected to a full node.
		dcr.log.Debugf("Output %s:%d that was not reported as unspent is considered SPENT, spv mode = false.",
			txHash, vout)
		return txOut, confs, 1, nil
	}

	// For SPV wallets, only consider the output spent if it pays to the wallet
	// because outputs that don't pay to the wallet may be unspent but still not
	// found by wallet.UnspentOutput. NOTE: Swap contracts never pay to wallet
	// (p2sh with no imported redeem script), so this is not an expected outcome
	// for swap contract outputs!
	//
	// for _, details := range tx.Details {
	// 	if details.Vout == vout && details.Category == wallet.CreditReceive.String() {
	// 		dcr.log.Tracef("Output %s:%d was not reported as unspent, pays to the wallet and is considered SPENT.",
	// 			txHash, vout)
	// 		return txOut, confs, 1, nil
	// 	}
	// }

	// Spend status is unknown.  Caller may scan block filters if needed.
	dcr.log.Tracef("Output %s:%d was not reported as unspent by SPV wallet. Spend status UNKNOWN.",
		txHash, vout)
	return txOut, confs, -1, nil // unknown spend status
}

// LockTimeExpired returns true if the specified locktime has expired, making it
// possible to redeem the locked coins.
func (dcr *ExchangeWallet) LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error) {
	blockHash := dcr.cachedBestBlock().hash
	hdr, err := dcr.wallet.GetBlockHeader(ctx, blockHash)
	if err != nil {
		return false, fmt.Errorf("unable to retrieve the block header: %w", err)
	}
	return time.Unix(hdr.MedianTime, 0).After(lockTime), nil
}

// ContractLockTimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (dcr *ExchangeWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	expired, err := dcr.LockTimeExpired(ctx, contractExpiry)
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
		bestBlock := dcr.cachedBestBlock()
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
	if int(vout) > len(tx.MsgTx.TxOut)-1 {
		return nil, nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
	}
	contractScript := tx.MsgTx.TxOut[vout].PkScript
	contractScriptVer := tx.MsgTx.TxOut[vout].Version
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
		tx, err := dcr.wallet.GetTransaction(dcr.ctx, txHash)
		if err != nil {
			logAbandon(fmt.Sprintf("getrawtransaction error for tx hash %v: %v", txHash, err))
			return
		}
		found, canceled := dcr.findRedemptionsInTx("mempool", tx.MsgTx, contractOutpoints)
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
	msgTx, refundVal, fee, err := dcr.refundTx(coinID, contract, 0, nil, feeRate)
	if err != nil {
		return nil, fmt.Errorf("error creating refund tx: %w", err)
	}

	refundHash, err := dcr.broadcastTx(msgTx)
	if err != nil {
		return nil, err
	}
	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Refund,
		ID:     refundHash.String(),
		Amount: refundVal,
		Fees:   fee,
	}, refundHash, true)

	return toCoinID(refundHash, 0), nil
}

// refundTx crates and signs a contract's refund transaction. If refundAddr is
// not supplied, one will be requested from the wallet. If val is not supplied
// it will be retrieved with gettxout.
func (dcr *ExchangeWallet) refundTx(coinID, contract dex.Bytes, val uint64, refundAddr stdaddr.Address, feeRate uint64) (tx *wire.MsgTx, refundVal, txFee uint64, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, 0, 0, err
	}
	// Grab the output, make sure it's unspent and get the value if not supplied.
	if val == 0 {
		utxo, _, spent, err := dcr.lookupTxOutput(dcr.ctx, txHash, vout)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("error finding unspent contract: %w", err)
		}
		if utxo == nil {
			return nil, 0, 0, asset.CoinNotFoundError
		}
		val = uint64(utxo.Value)

		switch spent {
		case 0: // unspent, proceed to create refund tx
		case 1, -1: // spent or unknown
			// Attempt to identify if it was manually refunded with the backup
			// transaction, in which case we can skip broadcast and record the
			// spending transaction we may locate as below.

			// First find the block containing the output itself.
			scriptAddr, err := stdaddr.NewAddressScriptHashV0(contract, dcr.chainParams)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("error encoding contract address: %w", err)
			}
			_, pkScript := scriptAddr.PaymentScript()
			outFound, _, err := dcr.externalTxOutput(dcr.ctx, newOutPoint(txHash, vout),
				pkScript, time.Now().Add(-60*24*time.Hour)) // search up to 60 days ago
			if err != nil {
				return nil, 0, 0, err // possibly the contract is still in mempool
			}
			// Try to find a transaction that spends it.
			spent, err := dcr.isOutputSpent(dcr.ctx, outFound) // => findTxOutSpender
			if err != nil {
				return nil, 0, 0, fmt.Errorf("error checking if contract %v:%d is spent: %w", txHash, vout, err)
			}
			if spent {
				spendTx := outFound.spenderTx
				// Refunds are not batched, so input 0 is always the spender.
				if dexdcr.IsRefundScript(utxo.Version, spendTx.TxIn[0].SignatureScript, contract) {
					return spendTx, 0, 0, nil
				} // otherwise it must be a redeem
				return nil, 0, 0, fmt.Errorf("contract %s:%d is spent in %v (%w)",
					txHash, vout, spendTx.TxHash(), asset.CoinNotFoundError)
			}
		}
	}

	sender, _, lockTime, _, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error extracting swap addresses: %w", err)
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
		return nil, 0, 0, fmt.Errorf("refund tx not worth the fees")
	}

	if refundAddr == nil {
		refundAddr, err = dcr.wallet.ExternalAddress(dcr.ctx, dcr.depositAccount())
		if err != nil {
			return nil, 0, 0, fmt.Errorf("error getting new address from the wallet: %w", err)
		}
	}
	pkScriptVer, pkScript := refundAddr.PaymentScript()
	txOut := newTxOut(int64(val-fee), pkScriptVer, pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, 0, 0, fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := dcr.createSig(msgTx, 0, contract, sender)
	if err != nil {
		return nil, 0, 0, err
	}
	redeemSigScript, err := dexdcr.RefundP2SHContract(contract, refundSig, refundPubKey)
	if err != nil {
		return nil, 0, 0, err
	}
	txIn.SignatureScript = redeemSigScript
	return msgTx, val, fee, nil
}

// DepositAddress returns an address for depositing funds into the exchange
// wallet.
func (dcr *ExchangeWallet) DepositAddress() (string, error) {
	acct := dcr.depositAccount()
	addr, err := dcr.wallet.ExternalAddress(dcr.ctx, acct)
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

// AddressUsed checks if a wallet address has been used.
func (dcr *ExchangeWallet) AddressUsed(addrStr string) (bool, error) {
	return dcr.wallet.AddressUsed(dcr.ctx, addrStr)
}

// Unlock unlocks the exchange wallet.
func (dcr *ExchangeWallet) Unlock(pw []byte) error {
	// Older SPV wallet potentially need an upgrade while we have a password.
	acctsToUnlock := dcr.allAccounts()
	if upgrader, is := dcr.wallet.(interface {
		upgradeAccounts(ctx context.Context, pw []byte) error
	}); is {
		if err := upgrader.upgradeAccounts(dcr.ctx, pw); err != nil {
			return fmt.Errorf("error upgrading accounts: %w", err)
		}
		// For the native wallet, we unlock all accounts regardless. Otherwise,
		// the accounts won't be properly unlocked after ConfigureFundsMixer
		// is called. We could consider taking a password for
		// ConfigureFundsMixer OR have Core take the password and call Unlock
		// after ConfigureFundsMixer.
		acctsToUnlock = nativeAccounts
	}

	// We must unlock all accounts, including any unmixed account, which is used
	// to supply keys to the refund path of the swap contract script.
	for _, acct := range acctsToUnlock {
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
	accts := dcr.wallet.Accounts()
	if accts.UnmixedAccount != "" {
		return fmt.Errorf("cannot lock RPC mixing wallet") // don't lock if mixing is enabled
	}
	return dcr.wallet.LockAccount(dcr.ctx, accts.PrimaryAccount)
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

func bondPushDataScript(ver uint16, acctID []byte, lockTimeSec int64, pkh []byte) ([]byte, error) {
	pushData := make([]byte, 2+len(acctID)+4+20)
	var offset int
	binary.BigEndian.PutUint16(pushData[offset:], ver)
	offset += 2
	copy(pushData[offset:], acctID[:])
	offset += len(acctID)
	binary.BigEndian.PutUint32(pushData[offset:], uint32(lockTimeSec))
	offset += 4
	copy(pushData[offset:], pkh)
	return txscript.NewScriptBuilder().
		AddOp(txscript.OP_RETURN).
		AddData(pushData).
		Script()
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
func (dcr *ExchangeWallet) MakeBondTx(ver uint16, amt, feeRate uint64, lockTime time.Time,
	bondKey *secp256k1.PrivateKey, acctID []byte) (*asset.Bond, func(), error) {
	if ver != 0 {
		return nil, nil, errors.New("only version 0 bonds supported")
	}
	if until := time.Until(lockTime); until >= 365*12*time.Hour /* ~6 months */ {
		return nil, nil, fmt.Errorf("that lock time is nuts: %v", lockTime)
	} else if until < 0 {
		return nil, nil, fmt.Errorf("that lock time is already passed: %v", lockTime)
	}

	pk := bondKey.PubKey().SerializeCompressed()
	pkh := stdaddr.Hash160(pk)

	feeRate = dcr.feeRateWithFallback(feeRate)
	baseTx := wire.NewMsgTx()
	const scriptVersion = 0

	// TL output.
	lockTimeSec := lockTime.Unix()
	if lockTimeSec >= dexdcr.MaxCLTVScriptNum || lockTimeSec <= 0 {
		return nil, nil, fmt.Errorf("invalid lock time %v", lockTime)
	}
	bondScript, err := dexdcr.MakeBondScript(ver, uint32(lockTimeSec), pkh)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build bond output redeem script: %w", err)
	}
	bondAddr, err := stdaddr.NewAddressScriptHash(scriptVersion, bondScript, dcr.chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build bond output payment script: %w", err)
	}
	bondPkScriptVer, bondPkScript := bondAddr.PaymentScript()
	txOut := newTxOut(int64(amt), bondPkScriptVer, bondPkScript)
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, nil, fmt.Errorf("bond output is dust")
	}
	baseTx.AddTxOut(txOut)

	// Acct ID commitment and bond details output, v0. The integers are encoded
	// with big-endian byte order and a fixed number of bytes, unlike in Script,
	// for natural visual inspection of the version and lock time.
	commitPkScript, err := bondPushDataScript(ver, acctID, lockTimeSec, pkh)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build acct commit output script: %w", err)
	}
	acctOut := newTxOut(0, scriptVersion, commitPkScript) // value zero
	baseTx.AddTxOut(acctOut)

	// NOTE: this "fund -> addInputCoins -> signTxAndAddChange -> lock prevouts"
	// sequence might be best encapsulated in a fundRawTransactionMethod.
	baseSize := uint32(baseTx.SerializeSize()) + dexdcr.P2PKHOutputSize // uint32(dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*3)
	enough := sendEnough(amt, feeRate, false, baseSize, true)
	coins, _, _, _, err := dcr.fund(0, enough)
	if err != nil {
		return nil, nil, fmt.Errorf("Unable to send %s DCR with fee rate of %d atoms/byte: %w",
			amount(amt), feeRate, err)
	}

	var txIDToRemoveFromHistory *chainhash.Hash // will be non-nil if tx was added to history

	abandon := func() { // if caller does not broadcast, or we fail in this method
		_, err := dcr.returnCoins(coins)
		if err != nil {
			dcr.log.Errorf("error returning coins for unused bond tx: %v", coins)
		}
		if txIDToRemoveFromHistory != nil {
			dcr.removeTxFromHistory(txIDToRemoveFromHistory)
		}
	}

	var success bool
	defer func() {
		if !success {
			abandon()
		}
	}()

	_, err = dcr.addInputCoins(baseTx, coins)
	if err != nil {
		return nil, nil, err
	}

	signedTx, _, _, fee, err := dcr.signTxAndAddChange(baseTx, feeRate, -1, dcr.depositAccount())
	if err != nil {
		return nil, nil, err
	}
	txHash := signedTx.CachedTxHash() // spentAmt := amt + fees

	signedTxBytes, err := signedTx.Bytes()
	if err != nil {
		return nil, nil, err
	}
	unsignedTxBytes, err := baseTx.Bytes()
	if err != nil {
		return nil, nil, err
	}

	// Prep the redeem / refund tx.
	redeemMsgTx, err := dcr.makeBondRefundTxV0(txHash, 0, amt, bondScript, bondKey, feeRate)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create bond redemption tx: %w", err)
	}
	redeemTx, err := redeemMsgTx.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize bond redemption tx: %w", err)
	}

	bond := &asset.Bond{
		Version:    ver,
		AssetID:    BipID,
		Amount:     amt,
		CoinID:     toCoinID(txHash, 0),
		Data:       bondScript,
		SignedTx:   signedTxBytes,
		UnsignedTx: unsignedTxBytes,
		RedeemTx:   redeemTx,
	}
	success = true

	bondInfo := &asset.BondTxInfo{
		AccountID: acctID,
		LockTime:  uint64(lockTimeSec),
		BondID:    pkh,
	}
	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:     asset.CreateBond,
		ID:       txHash.String(),
		Amount:   amt,
		Fees:     fee,
		BondInfo: bondInfo,
	}, txHash, false)

	txIDToRemoveFromHistory = txHash

	return bond, abandon, nil
}

func (dcr *ExchangeWallet) makeBondRefundTxV0(txid *chainhash.Hash, vout uint32, amt uint64,
	script []byte, priv *secp256k1.PrivateKey, feeRate uint64) (*wire.MsgTx, error) {
	lockTime, pkhPush, err := dexdcr.ExtractBondDetailsV0(0, script)
	if err != nil {
		return nil, err
	}

	pk := priv.PubKey().SerializeCompressed()
	pkh := stdaddr.Hash160(pk)
	if !bytes.Equal(pkh, pkhPush) {
		return nil, asset.ErrIncorrectBondKey
	}

	redeemMsgTx := wire.NewMsgTx()
	// Transaction LockTime must be <= spend time, and >= the CLTV lockTime, so
	// we use exactly the CLTV's value. This limits the CLTV value to 32-bits.
	redeemMsgTx.LockTime = lockTime
	bondPrevOut := wire.NewOutPoint(txid, vout, wire.TxTreeRegular)
	txIn := wire.NewTxIn(bondPrevOut, int64(amt), []byte{})
	txIn.Sequence = wire.MaxTxInSequenceNum - 1 // not finalized, do not disable cltv
	redeemMsgTx.AddTxIn(txIn)

	// Calculate fees and add the refund output.
	redeemSize := redeemMsgTx.SerializeSize() + dexdcr.RedeemBondSigScriptSize + dexdcr.P2PKHOutputSize
	fee := feeRate * uint64(redeemSize)
	if fee > amt {
		return nil, fmt.Errorf("irredeemable bond at fee rate %d atoms/byte", feeRate)
	}

	redeemAddr, err := dcr.wallet.ExternalAddress(dcr.ctx, dcr.wallet.Accounts().PrimaryAccount)
	if err != nil {
		return nil, fmt.Errorf("error getting new address from the wallet: %w", translateRPCCancelErr(err))
	}
	redeemScriptVer, redeemPkScript := redeemAddr.PaymentScript()
	redeemTxOut := newTxOut(int64(amt-fee), redeemScriptVer, redeemPkScript)
	if dexdcr.IsDust(redeemTxOut, feeRate) { // hard to imagine
		return nil, fmt.Errorf("redeem output is dust")
	}
	redeemMsgTx.AddTxOut(redeemTxOut)

	// CalcSignatureHash and ecdsa.Sign with secp256k1 private key.
	redeemInSig, err := sign.RawTxInSignature(redeemMsgTx, 0, script, txscript.SigHashAll,
		priv.Serialize(), dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, fmt.Errorf("error creating signature for bond redeem input script '%v': %w", redeemAddr, err)
	}

	bondRedeemSigScript, err := dexdcr.RefundBondScript(script, redeemInSig, pk)
	if err != nil {
		return nil, fmt.Errorf("failed to build bond redeem input script: %w", err)
	}
	redeemMsgTx.TxIn[0].SignatureScript = bondRedeemSigScript

	return redeemMsgTx, nil
}

// RefundBond refunds a bond output to a new wallet address given the redeem
// script and private key. After broadcasting, the output paying to the wallet
// is returned.
func (dcr *ExchangeWallet) RefundBond(ctx context.Context, ver uint16, coinID, script []byte,
	amt uint64, privKey *secp256k1.PrivateKey) (asset.Coin, error) {
	if ver != 0 {
		return nil, errors.New("only version 0 bonds supported")
	}
	lockTime, pkhPush, err := dexdcr.ExtractBondDetailsV0(0, script)
	if err != nil {
		return nil, err
	}
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	feeRate := dcr.targetFeeRateWithFallback(2, 0)

	msgTx, err := dcr.makeBondRefundTxV0(txHash, vout, amt, script, privKey, feeRate)
	if err != nil {
		return nil, err
	}

	redeemHash, err := dcr.wallet.SendRawTransaction(ctx, msgTx, false)
	if err != nil { // TODO: we need to be much smarter about these send error types/codes
		return nil, translateRPCCancelErr(err)
	}

	refundAmt := msgTx.TxOut[0].Value
	bondInfo := &asset.BondTxInfo{
		LockTime: uint64(lockTime),
		BondID:   pkhPush,
	}
	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:     asset.RedeemBond,
		ID:       redeemHash.String(),
		Amount:   amt,
		Fees:     amt - uint64(refundAmt),
		BondInfo: bondInfo,
	}, redeemHash, true)

	return newOutput(redeemHash, 0, uint64(refundAmt), wire.TxTreeRegular), nil

	/* If we need to find the actual unspent bond transaction for any of:
	   (1) the output amount, (2) the commitment output data, or (3) to ensure
	   it is unspent, we can locate it as follows:

	// First try without cfilters (gettxout or gettransaction). If bond was
	// funded by this wallet or had a change output paying to this wallet, it
	// should be found here.
	txOut, _, spent, err := dcr.lookupTxOutput(ctx, txHash, vout)
	if err == nil {
		if spent {
			return nil, errors.New("bond already spent")
		}
		return dcr.makeBondRefundTxV0(txHash, vout, uint64(txOut.Value), script, privKey, feeRate)
	}
	if !errors.Is(err, asset.CoinNotFoundError) {
		dcr.log.Warnf("Unexpected error looking up bond output %v:%d", txHash, vout)
	}

	// Try block filters. This would only be required if the bond tx is foreign.
	// In general, the bond should have been created with this wallet.
	// I was hesitant to even support this, but might as well cover this edge.
	// NOTE: An alternative is to have the caller provide the amount, which is
	// all we're getting from the located tx output!
	scriptAddr, err := stdaddr.NewAddressScriptHashV0(script, dcr.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error encoding script address: %w", err)
	}
	_, pkScript := scriptAddr.PaymentScript()
	outFound, _, err := dcr.externalTxOutput(dcr.ctx, newOutPoint(txHash, vout),
		pkScript, time.Now().Add(-365*24*time.Hour)) // long!
	if err != nil {
		return nil, err // may be asset.CoinNotFoundError
	}
	txOut = outFound.TxOut // outFound.tree
	spent, err = dcr.isOutputSpent(ctx, outFound)
	if err != nil {
		return nil, fmt.Errorf("error checking if output %v:%d is spent: %w", txHash, vout, err)
	}
	if spent {
		return nil, errors.New("bond already spent")
	}

	return dcr.makeBondRefundTxV0(txHash, vout, uint64(txOut.Value), script, privKey, feeRate)
	*/
}

// FindBond finds the bond with coinID and returns the values used to create it.
func (dcr *ExchangeWallet) FindBond(ctx context.Context, coinID []byte, searchUntil time.Time) (bond *asset.BondDetails, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	decodeV0BondTx := func(msgTx *wire.MsgTx) (*asset.BondDetails, error) {
		if len(msgTx.TxOut) < 2 {
			return nil, fmt.Errorf("tx %s is not a v0 bond transaction: too few outputs", txHash)
		}
		_, lockTime, pkh, err := dexdcr.ExtractBondCommitDataV0(0, msgTx.TxOut[1].PkScript)
		if err != nil {
			return nil, fmt.Errorf("unable to extract bond commitment details from output 1 of %s: %v", txHash, err)
		}
		// Sanity check.
		bondScript, err := dexdcr.MakeBondScript(0, lockTime, pkh[:])
		if err != nil {
			return nil, fmt.Errorf("failed to build bond output redeem script: %w", err)
		}
		bondAddr, err := stdaddr.NewAddressScriptHash(0, bondScript, dcr.chainParams)
		if err != nil {
			return nil, fmt.Errorf("failed to build bond output payment script: %w", err)
		}
		_, bondScriptWOpcodes := bondAddr.PaymentScript()
		if !bytes.Equal(bondScriptWOpcodes, msgTx.TxOut[0].PkScript) {
			return nil, fmt.Errorf("bond script does not match commit data for %s: %x != %x",
				txHash, bondScript, msgTx.TxOut[0].PkScript)
		}
		return &asset.BondDetails{
			Bond: &asset.Bond{
				Version: 0,
				AssetID: BipID,
				Amount:  uint64(msgTx.TxOut[0].Value),
				CoinID:  coinID,
				Data:    bondScript,
				//
				// SignedTx and UnsignedTx not populated because this is
				// an already posted bond and these fields are no longer used.
				// SignedTx, UnsignedTx []byte
				//
				// RedeemTx cannot be populated because we do not have
				// the private key that only core knows. Core will need
				// the BondPKH to determine what the private key was.
				// RedeemTx []byte
			},
			LockTime: time.Unix(int64(lockTime), 0),
			CheckPrivKey: func(bondKey *secp256k1.PrivateKey) bool {
				pk := bondKey.PubKey().SerializeCompressed()
				pkhB := stdaddr.Hash160(pk)
				return bytes.Equal(pkh[:], pkhB)
			},
		}, nil
	}

	// If the bond was funded by this wallet or had a change output paying
	// to this wallet, it should be found here.
	tx, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err == nil {
		return decodeV0BondTx(tx.MsgTx)
	}
	if !errors.Is(err, asset.CoinNotFoundError) {
		dcr.log.Warnf("Unexpected error looking up bond output %v:%d", txHash, vout)
	}

	// The bond was not funded by this wallet or had no change output when
	// restored from seed. This is not a problem. However, we are unable to
	// use filters because we don't know any output scripts. Brute force
	// finding the transaction.
	blockHash, _, err := dcr.wallet.GetBestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get best hash: %v", err)
	}
	var (
		blk   *wire.MsgBlock
		msgTx *wire.MsgTx
	)
out:
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("bond search stopped: %w", err)
		}
		blk, err = dcr.wallet.GetBlock(ctx, blockHash)
		if err != nil {
			return nil, fmt.Errorf("error retrieving block %s: %w", blockHash, err)
		}
		if blk.Header.Timestamp.Before(searchUntil) {
			return nil, fmt.Errorf("searched blocks until %v but did not find the bond tx %s", searchUntil, txHash)
		}
		for _, tx := range blk.Transactions {
			if tx.TxHash() == *txHash {
				dcr.log.Debugf("Found mined tx %s in block %s.", txHash, blk.BlockHash())
				msgTx = tx
				break out
			}
		}

		if string(blk.Header.PrevBlock[:]) == "" {
			return nil, fmt.Errorf("did not find the bond tx %s", txHash)
		}

		blockHash = &blk.Header.PrevBlock
	}
	return decodeV0BondTx(msgTx)
}

// SendTransaction broadcasts a valid fully-signed transaction.
func (dcr *ExchangeWallet) SendTransaction(rawTx []byte) ([]byte, error) {
	msgTx, err := msgTxFromBytes(rawTx)
	if err != nil {
		return nil, err
	}
	txHash, err := dcr.wallet.SendRawTransaction(dcr.ctx, msgTx, false)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}
	dcr.markTxAsSubmitted(txHash)
	return toCoinID(txHash, 0), nil
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

	selfSend, err := dcr.OwnsDepositAddress(address)
	if err != nil {
		dcr.log.Errorf("error checking if address %q is owned: %v", address, err)
	}
	txType := asset.Send
	if selfSend {
		txType = asset.SelfSend
	}

	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:      txType,
		ID:        msgTx.CachedTxHash().String(),
		Amount:    sentVal,
		Fees:      value - sentVal,
		Recipient: &address,
	}, msgTx.CachedTxHash(), true)

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
	msgTx, sentVal, fee, err := dcr.sendToAddress(addr, value, dcr.feeRateWithFallback(feeRate))
	if err != nil {
		return nil, err
	}

	selfSend, err := dcr.OwnsDepositAddress(address)
	if err != nil {
		dcr.log.Errorf("error checking if address %q is owned: %v", address, err)
	}
	txType := asset.Send
	if selfSend {
		txType = asset.SelfSend
	}

	dcr.addTxToHistory(&asset.WalletTransaction{
		Type:      txType,
		ID:        msgTx.CachedTxHash().String(),
		Amount:    sentVal,
		Fees:      fee,
		Recipient: &address,
	}, msgTx.CachedTxHash(), true)

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
	var spendFlag int8
	_, confs, spendFlag, err = dcr.lookupTxOutput(ctx, txHash, vout)
	if err == nil {
		if spendFlag != -1 {
			return confs, spendFlag > 0, nil
		} // else go on to block filters scan
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
	dcr.bondReserves.Store(0)
	// or should it remember reserves in case we reconnect? There's a
	// reReserveFunds Core method for this... unclear

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
func (dcr *ExchangeWallet) SyncStatus() (ss *asset.SyncStatus, err error) {
	defer func() {
		var synced bool
		if ss != nil {
			synced = ss.Synced
		}

		if wasSynced := dcr.previouslySynced.Swap(synced); synced && !wasSynced {
			tip := dcr.cachedBestBlock()
			go dcr.syncTxHistory(dcr.ctx, uint64(tip.height))
		}
	}()

	// If we have a rescan running, do different math.
	dcr.rescan.RLock()
	rescanProgress := dcr.rescan.progress
	dcr.rescan.RUnlock()

	if rescanProgress != nil {
		height := dcr.cachedBestBlock().height
		if height < rescanProgress.scannedThrough {
			height = rescanProgress.scannedThrough
		}
		txHeight := uint64(rescanProgress.scannedThrough)
		return &asset.SyncStatus{
			Synced:         false,
			TargetHeight:   uint64(height),
			StartingBlocks: dcr.startingBlocks.Load(),
			Blocks:         uint64(height),
			Transactions:   &txHeight,
		}, nil
	}

	// No rescan in progress. Ask wallet.
	ss, err = dcr.wallet.SyncStatus(dcr.ctx)
	if err != nil {
		return nil, err
	}
	ss.StartingBlocks = dcr.startingBlocks.Load()
	return ss, nil
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
	baseSize := uint32(dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2)
	reportChange := dcr.wallet.Accounts().UnmixedAccount == "" // otherwise change goes to unmixed account
	enough := sendEnough(val, feeRate, true, baseSize, reportChange)
	reserves := dcr.bondReserves.Load()
	coins, _, _, _, err := dcr.fund(reserves, enough)
	if err != nil {
		return nil, 0, fmt.Errorf("unable to withdraw %s DCR to address %s with feeRate %d atoms/byte: %w",
			amount(val), addr, feeRate, err)
	}

	msgTx, sentVal, err := dcr.sendCoins(coins, addr, nil, val, 0, feeRate, true)
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
func (dcr *ExchangeWallet) sendToAddress(addr stdaddr.Address, amt, feeRate uint64) (*wire.MsgTx, uint64, uint64, error) {
	baseSize := uint32(dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2) // may be extra if change gets omitted (see signTxAndAddChange)
	reportChange := dcr.wallet.Accounts().UnmixedAccount == ""          // otherwise change goes to unmixed account
	enough := sendEnough(amt, feeRate, false, baseSize, reportChange)
	reserves := dcr.bondReserves.Load()
	coins, _, _, _, err := dcr.fund(reserves, enough)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("Unable to send %s DCR with fee rate of %d atoms/byte: %w",
			amount(amt), feeRate, err)
	}

	msgTx, sentVal, err := dcr.sendCoins(coins, addr, nil, amt, 0, feeRate, false)
	if err != nil {
		if _, retErr := dcr.returnCoins(coins); retErr != nil {
			dcr.log.Errorf("Failed to unlock coins: %v", retErr)
		}
		return nil, 0, 0, err
	}

	var totalOut uint64
	for _, txOut := range msgTx.TxOut {
		totalOut += uint64(txOut.Value)
	}
	var totalIn uint64
	for _, coin := range coins {
		totalIn += coin.Value()
	}

	return msgTx, sentVal, totalIn - totalOut, nil
}

// sendCoins sends the amount to the address as the zeroth output, spending the
// specified coins. If subtract is true, the transaction fees will be taken from
// the sent value, otherwise it will taken from the change output. If there is
// change, it will be at index 1.
//
// An optional second output may be generated with the second address and amount
// arguments, if addr2 is non-nil. Note that to omit the extra output, the
// *interface* must be nil, not just the concrete type, so be cautious with
// concrete address types because a nil pointer wrap into a non-nil std.Address!
func (dcr *ExchangeWallet) sendCoins(coins asset.Coins, addr, addr2 stdaddr.Address, val, val2, feeRate uint64,
	subtract bool) (*wire.MsgTx, uint64, error) {
	baseTx := wire.NewMsgTx()
	_, err := dcr.addInputCoins(baseTx, coins)
	if err != nil {
		return nil, 0, err
	}
	payScriptVer, payScript := addr.PaymentScript()
	txOut := newTxOut(int64(val), payScriptVer, payScript)
	baseTx.AddTxOut(txOut)
	if addr2 != nil {
		payScriptVer, payScript := addr2.PaymentScript()
		txOut := newTxOut(int64(val2), payScriptVer, payScript)
		baseTx.AddTxOut(txOut)
	}

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

func (dcr *ExchangeWallet) makeExternalOut(acct string, val uint64) (*wire.TxOut, stdaddr.Address, error) {
	addr, err := dcr.wallet.ExternalAddress(dcr.ctx, acct)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %w", err)
	}
	changeScriptVersion, changeScript := addr.PaymentScript()
	return newTxOut(int64(val), changeScriptVersion, changeScript), addr, nil
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

	_, err = dcr.broadcastTx(signedTx)
	return signedTx, err
}

// signTxAndAddChange signs the passed msgTx, adding a change output that pays
// an address from the specified changeAcct, unless the change amount is dust.
// subtractFrom indicates the output from which fees should be subtracted, where
// -1 indicates fees should come out of a change output. baseTx may be modified
// with an added change output or a reduced value of the subtractFrom output.
func (dcr *ExchangeWallet) signTxAndAddChange(baseTx *wire.MsgTx, feeRate uint64,
	subtractFrom int32, changeAcct string) (*wire.MsgTx, *output, string, uint64, error) {
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

// ValidateAddress checks that the provided address is valid.
func (dcr *ExchangeWallet) ValidateAddress(address string) bool {
	_, err := stdaddr.DecodeAddress(address, dcr.chainParams)
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
func (dcr *ExchangeWallet) EstimateSendTxFee(address string, sendAmount, feeRate uint64, subtract, _ bool) (fee uint64, isValidAddress bool, err error) {
	if sendAmount == 0 {
		return 0, false, fmt.Errorf("cannot check fee: send amount = 0")
	}

	feeRate = dcr.feeRateWithFallback(feeRate)

	var pkScript []byte
	var payScriptVer uint16
	if addr, err := stdaddr.DecodeAddress(address, dcr.chainParams); err == nil {
		payScriptVer, pkScript = addr.PaymentScript()
		isValidAddress = true
	} else {
		// use a dummy 25-byte p2pkh script
		pkScript = dummyP2PKHScript
	}

	tx := wire.NewMsgTx()

	tx.AddTxOut(newTxOut(int64(sendAmount), payScriptVer, pkScript)) // payScriptVer is default zero

	utxos, err := dcr.spendableUTXOs()
	if err != nil {
		return 0, false, err
	}

	minTxSize := uint32(tx.SerializeSize())
	reportChange := dcr.wallet.Accounts().UnmixedAccount == ""
	enough := sendEnough(sendAmount, feeRate, subtract, minTxSize, reportChange)
	sum, extra, inputsSize, _, _, _, err := tryFund(utxos, enough)
	if err != nil {
		return 0, false, err
	}

	reserves := dcr.bondReserves.Load()
	avail := sumUTXOs(utxos)
	if avail-sum+extra /* avail-sendAmount-fees */ < reserves {
		return 0, false, errors.New("violates reserves")
	}

	txSize := uint64(minTxSize + inputsSize)
	estFee := txSize * feeRate
	remaining := sum - sendAmount

	// Check if there will be a change output if there is enough remaining.
	estFeeWithChange := (txSize + dexdcr.P2PKHOutputSize) * feeRate
	var changeValue uint64
	if remaining > estFeeWithChange {
		changeValue = remaining - estFeeWithChange
	}

	if subtract {
		// fees are already included in sendAmount, anything else is change.
		changeValue = remaining
	}

	var finalFee uint64
	if dexdcr.IsDustVal(dexdcr.P2PKHOutputSize, changeValue, feeRate) {
		// remaining cannot cover a non-dust change and the fee for the change.
		finalFee = estFee + remaining
	} else {
		// additional fee will be paid for non-dust change
		finalFee = estFeeWithChange
	}
	return finalFee, isValidAddress, nil
}

// StandardSendFee returns the fees for a simple send tx with one input and two
// outputs.
func (dcr *ExchangeWallet) StandardSendFee(feeRate uint64) uint64 {
	var baseSize uint64 = dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2 + dexdcr.P2PKHInputSize
	return feeRate * baseSize
}

func (dcr *ExchangeWallet) isNative() bool {
	return dcr.walletType == walletTypeSPV
}

// currentAgendas gets the most recent agendas from the chain params. The caller
// must populate the CurrentChoice field of the agendas.
func currentAgendas(chainParams *chaincfg.Params) (agendas []*asset.TBAgenda) {
	var bestID uint32
	for deploymentID := range chainParams.Deployments {
		if bestID == 0 || deploymentID > bestID {
			bestID = deploymentID
		}
	}
	for _, deployment := range chainParams.Deployments[bestID] {
		v := deployment.Vote
		agenda := &asset.TBAgenda{
			ID:          v.Id,
			Description: v.Description,
		}
		for _, choice := range v.Choices {
			agenda.Choices = append(agenda.Choices, &asset.TBChoice{
				ID:          choice.Id,
				Description: choice.Description,
			})
		}
		agendas = append(agendas, agenda)
	}
	return
}

func (dcr *ExchangeWallet) StakeStatus() (*asset.TicketStakingStatus, error) {
	if !dcr.connected.Load() {
		return nil, errors.New("not connected, login first")
	}
	// Try to get tickets first, because this will error for older RPC + SPV
	// wallets.
	tickets, err := dcr.tickets(dcr.ctx)
	if err != nil {
		if errors.Is(err, oldSPVWalletErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("error retrieving tickets: %w", err)
	}
	sinfo, err := dcr.wallet.StakeInfo(dcr.ctx)
	if err != nil {
		return nil, err
	}

	isRPC := !dcr.isNative()
	var vspURL string
	if !isRPC {
		if v := dcr.vspV.Load(); v != nil {
			vspURL = v.(*vsp).URL
		}
	} else {
		rpcW, ok := dcr.wallet.(*rpcWallet)
		if !ok {
			return nil, errors.New("wallet not an *rpcWallet")
		}
		walletInfo, err := rpcW.walletInfo(dcr.ctx)
		if err != nil {
			return nil, fmt.Errorf("error retrieving wallet info: %w", err)
		}
		vspURL = walletInfo.VSP
	}
	voteChoices, tSpends, treasuryPolicy, err := dcr.wallet.VotingPreferences(dcr.ctx)
	if err != nil {
		return nil, fmt.Errorf("error retrieving stances: %w", err)
	}
	agendas := currentAgendas(dcr.chainParams)
	for _, agenda := range agendas {
		for _, c := range voteChoices {
			if c.AgendaID == agenda.ID {
				agenda.CurrentChoice = c.ChoiceID
				break
			}
		}
	}

	return &asset.TicketStakingStatus{
		TicketPrice:   uint64(sinfo.Sdiff),
		VotingSubsidy: dcr.voteSubsidy(dcr.cachedBestBlock().height),
		VSP:           vspURL,
		IsRPC:         isRPC,
		Tickets:       tickets,
		Stances: asset.Stances{
			Agendas:        agendas,
			TreasurySpends: tSpends,
			TreasuryKeys:   treasuryPolicy,
		},
		Stats: dcr.ticketStatsFromStakeInfo(sinfo),
	}, nil
}

func (dcr *ExchangeWallet) ticketStatsFromStakeInfo(sinfo *wallet.StakeInfoData) asset.TicketStats {
	return asset.TicketStats{
		TotalRewards: uint64(sinfo.TotalSubsidy),
		TicketCount:  sinfo.OwnMempoolTix + sinfo.Unspent + sinfo.Immature + sinfo.Voted + sinfo.Revoked,
		Votes:        sinfo.Voted,
		Revokes:      sinfo.Revoked,
		Mempool:      sinfo.OwnMempoolTix,
		Queued:       uint32(dcr.ticketBuyer.remaining.Load()),
	}
}

func (dcr *ExchangeWallet) voteSubsidy(tipHeight int64) uint64 {
	// Chance of a given ticket voting in a block is
	// p = chainParams.TicketsPerBlock / (chainParams.TicketPoolSize * chainParams.TicketsPerBlock)
	//   = 1 / chainParams.TicketPoolSize
	// Expected number of blocks to vote is
	// 1 / p = chainParams.TicketPoolSize
	expectedBlocksToVote := int64(dcr.chainParams.TicketPoolSize)
	voteHeightExpectationValue := tipHeight + expectedBlocksToVote
	return uint64(dcr.subsidyCache.CalcStakeVoteSubsidyV3(voteHeightExpectationValue, blockchain.SSVDCP0012))
}

// tickets gets tickets from the wallet and changes the status of "unspent"
// tickets that haven't reached expiration "live".
// DRAFT NOTE: From dcrwallet:
//
//	TicketStatusUnspent is a matured ticket that has not been spent.  It
//	is only used under SPV mode where it is unknown if a ticket is live,
//	was missed, or expired.
//
// But if the ticket has not reached a certain number of confirmations, we
// can say for sure it's not expired. With auto-revocations, "missed" or
// "expired" tickets are actually "revoked", I think.
// The only thing I can't figure out is how SPV wallets set the spender in the
// case of an auto-revocation. It might be happening here
// https://github.com/decred/dcrwallet/blob/a87fa843495ec57c1d3b478c2ceb3876c3749af5/wallet/chainntfns.go#L770-L775
// If we're seeing auto-revocations, we're fine to make the changes in this
// method.
func (dcr *ExchangeWallet) tickets(ctx context.Context) ([]*asset.Ticket, error) {
	tickets, err := dcr.wallet.Tickets(ctx)
	if err != nil {
		return nil, fmt.Errorf("error retrieving tickets: %w", err)
	}
	// Adjust status for SPV tickets that aren't expired.
	oldestTicketsBlock := dcr.cachedBestBlock().height - int64(dcr.chainParams.TicketExpiry) - int64(dcr.chainParams.TicketMaturity)
	for _, t := range tickets {
		if t.Status != asset.TicketStatusUnspent {
			continue
		}
		if t.Tx.BlockHeight == -1 || t.Tx.BlockHeight > oldestTicketsBlock {
			t.Status = asset.TicketStatusLive
		}
	}
	return tickets, nil
}

func vspInfo(ctx context.Context, uri string) (*vspdjson.VspInfoResponse, error) {
	suffix := "/api/v3/vspinfo"
	path, err := neturl.JoinPath(uri, suffix)
	if err != nil {
		return nil, err
	}
	var info vspdjson.VspInfoResponse
	return &info, dexnet.Get(ctx, path, &info)
}

// SetVSP sets the VSP provider. Ability to set can be checked with StakeStatus
// first. Only non-RPC (internal) wallets can be set. Part of the
// asset.TicketBuyer interface.
func (dcr *ExchangeWallet) SetVSP(url string) error {
	if !dcr.isNative() {
		return errors.New("cannot set vsp for external wallet")
	}
	info, err := vspInfo(dcr.ctx, url)
	if err != nil {
		return err
	}
	v := vsp{
		URL:           url,
		PubKey:        base64.StdEncoding.EncodeToString(info.PubKey),
		FeePercentage: info.FeePercentage,
	}
	b, err := json.Marshal(&v)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dcr.vspFilepath, b, 0666); err != nil {
		return err
	}
	dcr.vspV.Store(&v)
	return nil
}

// PurchaseTickets purchases n number of tickets. Part of the asset.TicketBuyer
// interface.
func (dcr *ExchangeWallet) PurchaseTickets(n int, feeSuggestion uint64) error {
	if n < 1 {
		return nil
	}
	if !dcr.connected.Load() {
		return errors.New("not connected, login first")
	}
	bal, err := dcr.Balance()
	if err != nil {
		return fmt.Errorf("error getting balance: %v", err)
	}
	isRPC := !dcr.isNative()
	if isRPC {
		rpcW, ok := dcr.wallet.(*rpcWallet)
		if !ok {
			return errors.New("wallet not an *rpcWallet")
		}
		walletInfo, err := rpcW.walletInfo(dcr.ctx)
		if err != nil {
			return fmt.Errorf("error retrieving wallet info: %w", err)
		}
		if walletInfo.SPV && walletInfo.VSP == "" {
			return errors.New("a vsp must best set to purchase tickets with an spv wallet")
		}
	}
	sinfo, err := dcr.wallet.StakeInfo(dcr.ctx)
	if err != nil {
		return fmt.Errorf("stakeinfo error: %v", err)
	}
	// I think we need to set this, otherwise we probably end up with default
	// of DefaultRelayFeePerKb = 1e4 => 10 atoms/byte.
	feePerKB := dcrutil.Amount(dcr.feeRateWithFallback(feeSuggestion) * 1000)
	if err := dcr.wallet.SetTxFee(dcr.ctx, feePerKB); err != nil {
		return fmt.Errorf("error setting wallet tx fee: %w", err)
	}

	// Get a minimum size assuming a single-input split tx.
	fees := feePerKB * minVSPTicketPurchaseSize / 1000
	ticketPrice := sinfo.Sdiff + fees
	total := uint64(n) * uint64(ticketPrice)
	if bal.Available < total {
		return fmt.Errorf("available balance %s is lower than projected cost %s for %d tickets",
			dcrutil.Amount(bal.Available), dcrutil.Amount(total), n)
	}
	remain := dcr.ticketBuyer.remaining.Add(int32(n))
	dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Remaining: uint32(remain)})
	go dcr.runTicketBuyer()

	return nil
}

const ticketDataRoute = "ticketPurchaseUpdate"

// TicketPurchaseUpdate is an update from the asynchronous ticket purchasing
// loop.
type TicketPurchaseUpdate struct {
	Err       string             `json:"err,omitempty"`
	Remaining uint32             `json:"remaining"`
	Tickets   []*asset.Ticket    `json:"tickets"`
	Stats     *asset.TicketStats `json:"stats,omitempty"`
}

// runTicketBuyer attempts to buy requested tickets. Because of a dcrwallet bug,
// its possible that (Wallet).PurchaseTickets will purchase fewer tickets than
// requested, without error. To work around this bug, we add requested tickets
// to ExchangeWallet.ticketBuyer.remaining, and re-run runTicketBuyer every
// block.
func (dcr *ExchangeWallet) runTicketBuyer() {
	tb := &dcr.ticketBuyer
	if !tb.running.CompareAndSwap(false, true) {
		// already running
		return
	}
	defer tb.running.Store(false)
	var ok bool
	defer func() {
		if !ok {
			tb.remaining.Store(0)
		}
	}()

	if tb.unconfirmedTickets == nil {
		tb.unconfirmedTickets = make(map[chainhash.Hash]struct{})
	}

	remain := tb.remaining.Load()
	if remain < 1 {
		return
	}

	for txHash := range tb.unconfirmedTickets {
		tx, err := dcr.wallet.GetTransaction(dcr.ctx, &txHash)
		if err != nil {
			dcr.log.Errorf("GetTransaction error ticket tx %s: %v", txHash, err)
			dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: err.Error()})
			return
		}
		if tx.Confirmations > 0 {
			delete(tb.unconfirmedTickets, txHash)
		}
	}
	if len(tb.unconfirmedTickets) > 0 {
		ok = true
		dcr.log.Tracef("Skipping ticket purchase attempt because there are still %d unconfirmed tickets", len(tb.unconfirmedTickets))
		return
	}

	dcr.log.Tracef("Attempting to purchase %d tickets", remain)

	bal, err := dcr.Balance()
	if err != nil {
		dcr.log.Errorf("GetBalance error: %v", err)
		dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: err.Error()})
		return
	}
	sinfo, err := dcr.wallet.StakeInfo(dcr.ctx)
	if err != nil {
		dcr.log.Errorf("StakeInfo error: %v", err)
		dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: err.Error()})
		return
	}
	if dcrutil.Amount(bal.Available) < sinfo.Sdiff*dcrutil.Amount(remain) {
		dcr.log.Errorf("Insufficient balance %s to purchase %d ticket at price %s: %v", dcrutil.Amount(bal.Available), remain, sinfo.Sdiff, err)
		dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: "insufficient balance"})
		return
	}

	var tickets []*asset.Ticket
	if !dcr.isNative() {
		tickets, err = dcr.wallet.PurchaseTickets(dcr.ctx, int(remain), "", "", false)
	} else {
		v := dcr.vspV.Load()
		if v == nil {
			err = errors.New("no vsp set")
		} else {
			vInfo := v.(*vsp)
			tickets, err = dcr.wallet.PurchaseTickets(dcr.ctx, int(remain), vInfo.URL, vInfo.PubKey, dcr.mixing.Load())
		}
	}
	if err != nil {
		dcr.log.Errorf("PurchaseTickets error: %v", err)
		dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: err.Error()})
		return
	}
	purchased := int32(len(tickets))
	remain = tb.remaining.Add(-purchased)
	// sanity check
	if remain < 0 {
		remain = 0
		tb.remaining.Store(remain)
	}
	stats := dcr.ticketStatsFromStakeInfo(sinfo)
	stats.Mempool += uint32(len(tickets))
	stats.Queued = uint32(remain)
	dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{
		Tickets:   tickets,
		Remaining: uint32(remain),
		Stats:     &stats,
	})
	for _, ticket := range tickets {
		txHash, err := chainhash.NewHashFromStr(ticket.Tx.Hash)
		if err != nil {
			dcr.log.Errorf("NewHashFromStr error for ticket hash %s: %v", ticket.Tx.Hash, err)
			dcr.emit.Data(ticketDataRoute, &TicketPurchaseUpdate{Err: err.Error()})
			return
		}
		tb.unconfirmedTickets[*txHash] = struct{}{}
		dcr.addTxToHistory(&asset.WalletTransaction{
			Type:   asset.TicketPurchase,
			ID:     txHash.String(),
			Amount: ticket.Tx.TicketPrice,
			Fees:   ticket.Tx.Fees,
		}, txHash, true)
	}
	ok = true
}

// SetVotingPreferences sets the vote choices for all active tickets and future
// tickets. Nil maps can be provided for no change. Part of the
// asset.TicketBuyer interface.
func (dcr *ExchangeWallet) SetVotingPreferences(choices map[string]string, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {
	if !dcr.connected.Load() {
		return errors.New("not connected, login first")
	}
	return dcr.wallet.SetVotingPreferences(dcr.ctx, choices, tspendPolicy, treasuryPolicy)
}

// ListVSPs lists known available voting service providers.
func (dcr *ExchangeWallet) ListVSPs() ([]*asset.VotingServiceProvider, error) {
	if dcr.network == dex.Simnet {
		const simnetVSPUrl = "http://127.0.0.1:19591"
		vspi, err := vspInfo(dcr.ctx, simnetVSPUrl)
		if err != nil {
			dcr.log.Warnf("Error getting simnet VSP info: %v", err)
			return []*asset.VotingServiceProvider{}, nil
		}
		return []*asset.VotingServiceProvider{{
			URL:           simnetVSPUrl,
			Network:       dex.Simnet,
			Launched:      uint64(time.Now().Add(-time.Hour * 24 * 180).UnixMilli()),
			LastUpdated:   uint64(time.Now().Add(-time.Minute * 15).UnixMilli()),
			APIVersions:   vspi.APIVersions,
			FeePercentage: vspi.FeePercentage,
			Closed:        vspi.VspClosed,
			Voting:        vspi.Voting,
			Voted:         vspi.Voted,
			Revoked:       vspi.Revoked,
			VSPDVersion:   vspi.VspdVersion,
			BlockHeight:   vspi.BlockHeight,
			NetShare:      vspi.NetworkProportion,
		}}, nil
	}

	// This struct is not quite compatible with vspdjson.VspInfoResponse.
	var res map[string]*struct {
		Network       string  `json:"network"`
		Launched      uint64  `json:"launched"`    // seconds
		LastUpdated   uint64  `json:"lastupdated"` // seconds
		APIVersions   []int64 `json:"apiversions"`
		FeePercentage float64 `json:"feepercentage"`
		Closed        bool    `json:"closed"`
		Voting        int64   `json:"voting"`
		Voted         int64   `json:"voted"`
		Revoked       int64   `json:"revoked"`
		VSPDVersion   string  `json:"vspdversion"`
		BlockHeight   uint32  `json:"blockheight"`
		NetShare      float32 `json:"estimatednetworkproportion"`
	}
	if err := dexnet.Get(dcr.ctx, "https://api.decred.org/?c=vsp", &res); err != nil {
		return nil, err
	}
	vspds := make([]*asset.VotingServiceProvider, 0)
	for host, v := range res {
		net, err := dex.NetFromString(v.Network)
		if err != nil {
			dcr.log.Warnf("error parsing VSP network from %q", v.Network)
		}
		if net != dcr.network {
			continue
		}
		vspds = append(vspds, &asset.VotingServiceProvider{
			URL:           "https://" + host,
			Network:       net,
			Launched:      v.Launched * 1000,    // to milliseconds
			LastUpdated:   v.LastUpdated * 1000, // to milliseconds
			APIVersions:   v.APIVersions,
			FeePercentage: v.FeePercentage,
			Closed:        v.Closed,
			Voting:        v.Voting,
			Voted:         v.Voted,
			Revoked:       v.Revoked,
			VSPDVersion:   v.VSPDVersion,
			BlockHeight:   v.BlockHeight,
			NetShare:      v.NetShare,
		})
	}
	return vspds, nil
}

// TicketPage fetches a page of tickets within a range of block numbers with a
// target page size and optional offset. scanStart is the block in which to
// start the scan. The scan progresses in reverse block number order, starting
// at scanStart and going to progressively lower blocks. scanStart can be set to
// -1 to indicate the current chain tip.
func (dcr *ExchangeWallet) TicketPage(scanStart int32, n, skipN int) ([]*asset.Ticket, error) {
	if !dcr.connected.Load() {
		return nil, errors.New("not connected, login first")
	}
	pager, is := dcr.wallet.(ticketPager)
	if !is {
		return nil, errors.New("ticket pagination not supported for this wallet")
	}
	return pager.TicketPage(dcr.ctx, scanStart, n, skipN)
}

func (dcr *ExchangeWallet) broadcastTx(signedTx *wire.MsgTx) (*chainhash.Hash, error) {
	txHash, err := dcr.wallet.SendRawTransaction(dcr.ctx, signedTx, false)
	if err != nil {
		return nil, fmt.Errorf("sendrawtx error: %w, raw tx: %x", err, dcr.wireBytes(signedTx))
	}
	checkHash := signedTx.TxHash()
	if *txHash != checkHash {
		return nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s, raw tx: %x", *txHash, checkHash, dcr.wireBytes(signedTx))
	}
	return txHash, nil
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

func (dcr *ExchangeWallet) txDB() *btc.BadgerTxDB {
	db := dcr.txHistoryDB.Load()
	if db == nil {
		return nil
	}
	return db.(*btc.BadgerTxDB)
}

func rpcTxFee(tx *ListTransactionsResult) uint64 {
	if tx.Fee != nil {
		if *tx.Fee < 0 {
			return toAtoms(-*tx.Fee)
		}
		return toAtoms(*tx.Fee)
	}
	return 0
}

func isRegularMix(tx *wire.MsgTx) (isMix bool, mixDenom int64) {
	if len(tx.TxOut) < 3 || len(tx.TxIn) < 3 {
		return false, 0
	}

	mixedOuts := make(map[int64]uint32)
	for _, o := range tx.TxOut {
		val := o.Value
		if _, ok := splitPointMap[val]; ok {
			mixedOuts[val]++
			continue
		}
	}

	var mixCount uint32
	for val, count := range mixedOuts {
		if count < 3 {
			continue
		}
		if val > mixDenom {
			mixDenom = val
			mixCount = count
		}
	}

	// TODO: revisit the input count requirements
	isMix = mixCount >= uint32(len(tx.TxOut)/2)
	return
}

// isMixedSplitTx tests if a transaction is a CSPP-mixed ticket split
// transaction (the transaction that creates appropriately-sized outputs to be
// spent by a ticket purchase). This dumbly checks for at least three outputs
// of the same size and three of other sizes. It could be smarter by checking
// for ticket price + ticket fee outputs, but it's impossible to know the fee
// after the fact although it`s probably the default fee.
func isMixedSplitTx(tx *wire.MsgTx) (isMix bool, tikPrice int64) {
	if len(tx.TxOut) < 6 || len(tx.TxIn) < 3 {
		return false, 0
	}
	values := make(map[int64]int)
	for _, o := range tx.TxOut {
		values[o.Value]++
	}

	var numPossibleTickets int
	for k, v := range values {
		if v > numPossibleTickets {
			numPossibleTickets = v
			tikPrice = k
		}
	}
	numOtherOut := len(tx.TxOut) - numPossibleTickets

	// NOTE: The numOtherOut requirement may be too strict,
	if numPossibleTickets < 3 || numOtherOut < 3 {
		return false, 0
	}

	return true, tikPrice
}

func isMixTx(tx *wire.MsgTx) (isMix bool, mixDenom int64) {
	if isMix, mixDenom = isRegularMix(tx); isMix {
		return
	}

	return isMixedSplitTx(tx)
}

// idUnknownTx identifies the type and details of a transaction either made
// or received by the wallet.
func (dcr *ExchangeWallet) idUnknownTx(ctx context.Context, ltxr *ListTransactionsResult) (*asset.WalletTransaction, error) {
	txHash, err := chainhash.NewHashFromStr(ltxr.TxID)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx hash %s: %v", ltxr.TxID, err)
	}
	tx, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("GetTransaction error: %v", err)
	}

	var totalIn uint64
	for _, txIn := range tx.MsgTx.TxIn {
		if txIn.ValueIn > 0 {
			totalIn += uint64(txIn.ValueIn)
		}
	}

	var totalOut uint64
	for _, txOut := range tx.MsgTx.TxOut {
		totalOut += uint64(txOut.Value)
	}

	fee := rpcTxFee(ltxr)
	if fee == 0 && totalIn > totalOut {
		fee = totalIn - totalOut
	}

	switch *ltxr.TxType {
	case walletjson.LTTTVote:
		return &asset.WalletTransaction{
			Type:   asset.TicketVote,
			ID:     ltxr.TxID,
			Amount: totalOut,
			Fees:   fee,
		}, nil
	case walletjson.LTTTRevocation:
		return &asset.WalletTransaction{
			Type:   asset.TicketRevocation,
			ID:     ltxr.TxID,
			Amount: totalOut,
			Fees:   fee,
		}, nil
	case walletjson.LTTTTicket:
		return &asset.WalletTransaction{
			Type:   asset.TicketPurchase,
			ID:     ltxr.TxID,
			Amount: totalOut,
			Fees:   fee,
		}, nil
	}

	txIsBond := func(msgTx *wire.MsgTx) (bool, *asset.BondTxInfo) {
		if len(msgTx.TxOut) < 2 {
			return false, nil
		}
		const scriptVer = 0
		acctID, lockTime, pkHash, err := dexdcr.ExtractBondCommitDataV0(scriptVer, msgTx.TxOut[1].PkScript)
		if err != nil {
			return false, nil
		}
		return true, &asset.BondTxInfo{
			AccountID: acctID[:],
			LockTime:  uint64(lockTime),
			BondID:    pkHash[:],
		}
	}
	if isBond, bondInfo := txIsBond(tx.MsgTx); isBond {
		return &asset.WalletTransaction{
			Type:     asset.CreateBond,
			ID:       ltxr.TxID,
			Amount:   uint64(tx.MsgTx.TxOut[0].Value),
			Fees:     fee,
			BondInfo: bondInfo,
		}, nil
	}

	// Any other P2SH may be a swap or a send. We cannot determine unless we
	// look up the transaction that spends this UTXO.
	txPaysToScriptHash := func(msgTx *wire.MsgTx) (v uint64) {
		for _, txOut := range msgTx.TxOut {
			if txscript.IsPayToScriptHash(txOut.PkScript) {
				v += uint64(txOut.Value)
			}
		}
		return
	}
	if v := txPaysToScriptHash(tx.MsgTx); v > 0 {
		return &asset.WalletTransaction{
			Type:   asset.SwapOrSend,
			ID:     ltxr.TxID,
			Amount: v,
			Fees:   fee,
		}, nil
	}

	// Helper function will help us identify inputs that spend P2SH contracts.
	containsContractAtPushIndex := func(msgTx *wire.MsgTx, idx int, isContract func(contract []byte) bool) bool {
	txinloop:
		for _, txIn := range msgTx.TxIn {
			// not segwit
			const scriptVer = 0
			tokenizer := txscript.MakeScriptTokenizer(scriptVer, txIn.SignatureScript)
			for i := 0; i <= idx; i++ { // contract is 5th item item in redemption and 4th in refund
				if !tokenizer.Next() {
					continue txinloop
				}
			}
			if isContract(tokenizer.Data()) {
				return true
			}
		}
		return false
	}

	// Swap redemptions and refunds
	contractIsSwap := func(contract []byte) bool {
		_, _, _, _, err := dexdcr.ExtractSwapDetails(contract, dcr.chainParams)
		return err == nil
	}
	redeemsSwap := func(msgTx *wire.MsgTx) bool {
		return containsContractAtPushIndex(msgTx, 4, contractIsSwap)
	}
	if redeemsSwap(tx.MsgTx) {
		return &asset.WalletTransaction{
			Type:   asset.Redeem,
			ID:     ltxr.TxID,
			Amount: totalOut + fee,
			Fees:   fee,
		}, nil
	}
	refundsSwap := func(msgTx *wire.MsgTx) bool {
		return containsContractAtPushIndex(msgTx, 3, contractIsSwap)
	}
	if refundsSwap(tx.MsgTx) {
		return &asset.WalletTransaction{
			Type:   asset.Refund,
			ID:     ltxr.TxID,
			Amount: totalOut + fee,
			Fees:   fee,
		}, nil
	}

	// Bond refunds
	redeemsBond := func(msgTx *wire.MsgTx) (bool, *asset.BondTxInfo) {
		var bondInfo *asset.BondTxInfo
		isBond := func(contract []byte) bool {
			const scriptVer = 0
			lockTime, pkHash, err := dexdcr.ExtractBondDetailsV0(scriptVer, contract)
			if err != nil {
				return false
			}
			bondInfo = &asset.BondTxInfo{
				AccountID: []byte{}, // Could look for the bond tx to get this, I guess.
				LockTime:  uint64(lockTime),
				BondID:    pkHash[:],
			}
			return true
		}
		return containsContractAtPushIndex(msgTx, 2, isBond), bondInfo
	}
	if isBondRedemption, bondInfo := redeemsBond(tx.MsgTx); isBondRedemption {
		return &asset.WalletTransaction{
			Type:     asset.RedeemBond,
			ID:       ltxr.TxID,
			Amount:   totalOut,
			Fees:     fee,
			BondInfo: bondInfo,
		}, nil
	}

	const scriptVersion = 0

	allOutputsPayUs := func(msgTx *wire.MsgTx) bool {
		for _, txOut := range msgTx.TxOut {
			_, addrs := stdscript.ExtractAddrs(scriptVersion, txOut.PkScript, dcr.chainParams)
			if len(addrs) != 1 { // sanity check
				return false
			}

			addr := addrs[0]
			owns, err := dcr.wallet.WalletOwnsAddress(ctx, addr)
			if err != nil {
				dcr.log.Errorf("walletOwnsAddress error: %w", err)
				return false
			}
			if !owns {
				return false
			}
		}

		return true
	}

	if ltxr.Send && allOutputsPayUs(tx.MsgTx) && len(tx.MsgTx.TxIn) == 1 {
		return &asset.WalletTransaction{
			Type: asset.Split,
			ID:   ltxr.TxID,
			Fees: fee,
		}, nil
	}

	if isMix, mixDenom := isMixTx(tx.MsgTx); isMix {
		var mixedAmount uint64
		for _, txOut := range tx.MsgTx.TxOut {
			if txOut.Value == mixDenom {
				_, addrs := stdscript.ExtractAddrs(scriptVersion, txOut.PkScript, dcr.chainParams)
				if err != nil {
					dcr.log.Errorf("ExtractAddrs error: %w", err)
					continue
				}
				if len(addrs) != 1 { // sanity check
					continue
				}

				addr := addrs[0]
				owns, err := dcr.wallet.WalletOwnsAddress(ctx, addr)
				if err != nil {
					dcr.log.Errorf("walletOwnsAddress error: %w", err)
					continue
				}

				if owns {
					mixedAmount += uint64(txOut.Value)
				}
			}
		}

		return &asset.WalletTransaction{
			Type:   asset.Mix,
			ID:     ltxr.TxID,
			Amount: mixedAmount,
			Fees:   fee,
		}, nil
	}

	getRecipient := func(msgTx *wire.MsgTx, receive bool) *string {
		for _, txOut := range msgTx.TxOut {
			_, addrs := stdscript.ExtractAddrs(scriptVersion, txOut.PkScript, dcr.chainParams)
			if err != nil {
				dcr.log.Errorf("ExtractAddrs error: %w", err)
				continue
			}
			if len(addrs) != 1 { // sanity check
				continue
			}

			addr := addrs[0]
			owns, err := dcr.wallet.WalletOwnsAddress(ctx, addr)
			if err != nil {
				dcr.log.Errorf("walletOwnsAddress error: %w", err)
				continue
			}

			if receive == owns {
				str := addr.String()
				return &str
			}
		}
		return nil
	}

	txOutDirection := func(msgTx *wire.MsgTx) (in, out uint64) {
		for _, txOut := range msgTx.TxOut {
			_, addrs := stdscript.ExtractAddrs(scriptVersion, txOut.PkScript, dcr.chainParams)
			if err != nil {
				dcr.log.Errorf("ExtractAddrs error: %w", err)
				continue
			}
			if len(addrs) != 1 { // sanity check
				continue
			}

			addr := addrs[0]
			owns, err := dcr.wallet.WalletOwnsAddress(ctx, addr)
			if err != nil {
				dcr.log.Errorf("walletOwnsAddress error: %w", err)
				continue
			}
			if owns {
				in += uint64(txOut.Value)
			} else {
				out += uint64(txOut.Value)
			}
		}
		return
	}

	in, out := txOutDirection(tx.MsgTx)

	if ltxr.Send {
		txType := asset.Send
		amt := out
		if allOutputsPayUs(tx.MsgTx) {
			txType = asset.SelfSend
			amt = in
		}
		return &asset.WalletTransaction{
			Type:      txType,
			ID:        ltxr.TxID,
			Amount:    amt,
			Fees:      fee,
			Recipient: getRecipient(tx.MsgTx, false),
		}, nil
	}

	return &asset.WalletTransaction{
		Type:      asset.Receive,
		ID:        ltxr.TxID,
		Amount:    in,
		Fees:      fee,
		Recipient: getRecipient(tx.MsgTx, true),
	}, nil
}

// addUnknownTransactionsToHistory checks for any transactions the wallet has
// made or received that are not part of the transaction history. It scans
// from the last point to which it had previously scanned to the current tip.
func (dcr *ExchangeWallet) addUnknownTransactionsToHistory(tip uint64) {
	txHistoryDB := dcr.txDB()

	const blockQueryBuffer = 3
	var blockToQuery uint64
	lastQuery := dcr.receiveTxLastQuery.Load()
	if lastQuery == 0 {
		// TODO: use wallet birthday instead of block 0.
		// blockToQuery = 0
	} else if lastQuery < tip-blockQueryBuffer {
		blockToQuery = lastQuery - blockQueryBuffer
	} else {
		blockToQuery = tip - blockQueryBuffer
	}

	txs, err := dcr.wallet.ListSinceBlock(dcr.ctx, int32(blockToQuery))
	if err != nil {
		dcr.log.Errorf("Error listing transactions since block %d: %v", blockToQuery, err)
		return
	}

	for _, tx := range txs {
		if dcr.ctx.Err() != nil {
			return
		}
		txHash, err := chainhash.NewHashFromStr(tx.TxID)
		if err != nil {
			dcr.log.Errorf("Error decoding tx hash %s: %v", tx.TxID, err)
			continue
		}
		_, err = txHistoryDB.GetTx(txHash.String())
		if err == nil {
			continue
		}
		if !errors.Is(err, asset.CoinNotFoundError) {
			dcr.log.Errorf("Error getting tx %s: %v", txHash.String(), err)
			continue
		}
		wt, err := dcr.idUnknownTx(dcr.ctx, &tx)
		if err != nil {
			dcr.log.Errorf("error identifying transaction: %v", err)
			continue
		}

		if tx.BlockIndex != nil && *tx.BlockIndex > 0 && *tx.BlockIndex < int64(tip-blockQueryBuffer) {
			wt.BlockNumber = uint64(*tx.BlockIndex)
			wt.Timestamp = uint64(tx.BlockTime)
		}

		// Don't send notifications for the initial sync to avoid spamming the
		// front end. A notification is sent at the end of the initial sync.
		dcr.addTxToHistory(wt, txHash, true, blockToQuery == 0)
	}

	dcr.receiveTxLastQuery.Store(tip)
	err = txHistoryDB.SetLastReceiveTxQuery(tip)
	if err != nil {
		dcr.log.Errorf("Error setting last query to %d: %v", tip, err)
	}

	if blockToQuery == 0 {
		dcr.emit.TransactionHistorySyncedNote()
	}
}

// syncTxHistory checks to see if there are any transactions which the wallet
// has made or received that are not part of the transaction history, then
// identifies and adds them. It also checks all the pending transactions to see
// if they have been mined into a block, and if so, updates the transaction
// history to reflect the block height.
func (dcr *ExchangeWallet) syncTxHistory(ctx context.Context, tip uint64) {
	if !dcr.syncingTxHistory.CompareAndSwap(false, true) {
		return
	}
	defer dcr.syncingTxHistory.Store(false)

	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return
	}

	ss, err := dcr.SyncStatus()
	if err != nil {
		dcr.log.Errorf("Error getting sync status: %v", err)
		return
	}
	if !ss.Synced {
		return
	}

	dcr.addUnknownTransactionsToHistory(tip)

	pendingTxsCopy := make(map[chainhash.Hash]btc.ExtendedWalletTx, len(dcr.pendingTxs))
	dcr.pendingTxsMtx.RLock()
	for hash, tx := range dcr.pendingTxs {
		pendingTxsCopy[hash] = *tx
	}
	dcr.pendingTxsMtx.RUnlock()

	handlePendingTx := func(txHash chainhash.Hash, tx *btc.ExtendedWalletTx) {
		if !tx.Submitted {
			return
		}

		gtr, err := dcr.wallet.GetTransaction(ctx, &txHash)
		if errors.Is(err, asset.CoinNotFoundError) {
			err = txHistoryDB.RemoveTx(txHash.String())
			if err == nil {
				dcr.pendingTxsMtx.Lock()
				delete(dcr.pendingTxs, txHash)
				dcr.pendingTxsMtx.Unlock()
			} else {
				// Leave it in the pendingPendingTxs and attempt to remove it
				// again next time.
				dcr.log.Errorf("Error removing tx %s from the history store: %v", txHash.String(), err)
			}
			return
		}
		if err != nil {
			dcr.log.Errorf("Error getting transaction %s: %v", txHash, err)
			return
		}

		var updated bool
		if gtr.BlockHash != "" {
			blockHash, err := chainhash.NewHashFromStr(gtr.BlockHash)
			if err != nil {
				dcr.log.Errorf("Error decoding block hash %s: %v", gtr.BlockHash, err)
				return
			}
			block, err := dcr.wallet.GetBlockHeader(ctx, blockHash)
			if err != nil {
				dcr.log.Errorf("Error getting block height for %s: %v", blockHash, err)
				return
			}
			blockHeight := block.Height
			if tx.BlockNumber != uint64(blockHeight) || tx.Timestamp != uint64(block.Timestamp.Unix()) {
				tx.BlockNumber = uint64(blockHeight)
				tx.Timestamp = uint64(block.Timestamp.Unix())
				updated = true
			}
		} else if gtr.BlockHash == "" && tx.BlockNumber != 0 {
			tx.BlockNumber = 0
			tx.Timestamp = 0
			updated = true
		}

		var confs uint64
		if tx.BlockNumber > 0 && tip >= tx.BlockNumber {
			confs = tip - tx.BlockNumber + 1
		}
		if confs >= defaultRedeemConfTarget {
			tx.Confirmed = true
			updated = true
		}

		if updated {
			err = txHistoryDB.StoreTx(tx)
			if err != nil {
				dcr.log.Errorf("Error updating tx %s: %v", txHash, err)
				return
			}

			dcr.pendingTxsMtx.Lock()
			if tx.Confirmed {
				delete(dcr.pendingTxs, txHash)
			} else {
				dcr.pendingTxs[txHash] = tx
			}
			dcr.pendingTxsMtx.Unlock()

			dcr.emit.TransactionNote(tx.WalletTransaction, false)
		}
	}

	for hash, tx := range pendingTxsCopy {
		if dcr.ctx.Err() != nil {
			return
		}
		handlePendingTx(hash, &tx)
	}
}

func (dcr *ExchangeWallet) markTxAsSubmitted(txHash *chainhash.Hash) {
	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return
	}

	err := txHistoryDB.MarkTxAsSubmitted(txHash.String())
	if err != nil {
		dcr.log.Errorf("failed to mark tx as submitted in tx history db: %v", err)
	}

	dcr.pendingTxsMtx.Lock()
	wt, found := dcr.pendingTxs[*txHash]
	dcr.pendingTxsMtx.Unlock()

	if !found {
		dcr.log.Errorf("Transaction %s not found in pending txs", txHash)
		return
	}

	wt.Submitted = true

	dcr.emit.TransactionNote(wt.WalletTransaction, true)
}

func (dcr *ExchangeWallet) removeTxFromHistory(txHash *chainhash.Hash) {
	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return
	}

	dcr.pendingTxsMtx.Lock()
	delete(dcr.pendingTxs, *txHash)
	dcr.pendingTxsMtx.Unlock()

	err := txHistoryDB.RemoveTx(txHash.String())
	if err != nil {
		dcr.log.Errorf("failed to remove tx from tx history db: %v", err)
	}
}

func (dcr *ExchangeWallet) addTxToHistory(wt *asset.WalletTransaction, txHash *chainhash.Hash, submitted bool, skipNotes ...bool) {
	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return
	}

	ewt := &btc.ExtendedWalletTx{
		WalletTransaction: wt,
		Submitted:         submitted,
	}

	if wt.BlockNumber == 0 {
		dcr.pendingTxsMtx.Lock()
		dcr.pendingTxs[*txHash] = ewt
		dcr.pendingTxsMtx.Unlock()
	}

	err := txHistoryDB.StoreTx(ewt)
	if err != nil {
		dcr.log.Errorf("failed to store tx in tx history db: %v", err)
	}

	skipNote := len(skipNotes) > 0 && skipNotes[0]
	if submitted && !skipNote {
		dcr.emit.TransactionNote(wt, true)
	}
}

func (dcr *ExchangeWallet) checkPeers(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
		dcr.checkPeers(ctx)

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (dcr *ExchangeWallet) emitTipChange(height int64) {
	var data any
	sinfo, err := dcr.wallet.StakeInfo(dcr.ctx)
	if err != nil {
		dcr.log.Errorf("Error getting stake info for tip change notification data: %v", err)
	} else {
		data = &struct {
			TicketPrice   uint64            `json:"ticketPrice"`
			VotingSubsidy uint64            `json:"votingSubsidy"`
			Stats         asset.TicketStats `json:"stats"`
		}{
			TicketPrice:   uint64(sinfo.Sdiff),
			Stats:         dcr.ticketStatsFromStakeInfo(sinfo),
			VotingSubsidy: dcr.voteSubsidy(height),
		}
	}

	dcr.emit.TipChange(uint64(height), data)
	go dcr.runTicketBuyer()
}

func (dcr *ExchangeWallet) emitBalance() {
	if bal, err := dcr.Balance(); err != nil {
		dcr.log.Errorf("Error getting balance after mempool tx notification: %v", err)
	} else {
		dcr.emit.BalanceChange(bal)
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
			dcr.log.Errorf("failed to get best block: %v", err)
			return
		}

		if dcr.cachedBestBlock().hash.IsEqual(newTip.hash) {
			return
		}

		if walletBlock == nil {
			dcr.handleTipChange(ctx, newTip.hash, newTip.height)
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
		ss, err := dcr.wallet.SyncStatus(ctxInternal)
		if err != nil {
			dcr.log.Errorf("Error retrieving sync status before queuing polled block: %v", err)
		} else if !ss.Synced {
			blockAllowance *= 10
		}
		queuedBlock = &polledBlock{
			block: newTip,
			queue: time.AfterFunc(blockAllowance, func() {
				if ss, _ := dcr.SyncStatus(); ss != nil && ss.Synced {
					dcr.log.Warnf("Reporting a block found in polling that the wallet apparently "+
						"never reported: %s (%d). If you see this message repeatedly, it may indicate "+
						"an issue with the wallet.", newTip.hash, newTip.height)
				}
				dcr.handleTipChange(ctx, newTip.hash, newTip.height)
			}),
		}
	}

	for {
		select {
		case <-ticker.C:
			checkTip()

		case walletTip := <-walletBlock:
			if walletTip == nil {
				// Mempool tx seen.
				dcr.emitBalance()

				tip := dcr.cachedBestBlock()
				dcr.syncTxHistory(ctx, uint64(tip.height))
				continue
			}
			if queuedBlock != nil && walletTip.height >= queuedBlock.height {
				if !queuedBlock.queue.Stop() && walletTip.hash == queuedBlock.hash {
					continue
				}
				queuedBlock = nil
			}
			dcr.handleTipChange(ctx, walletTip.hash, walletTip.height)
		case <-ctx.Done():
			return
		}

		// Ensure context cancellation takes priority before the next iteration.
		if ctx.Err() != nil {
			return
		}
	}
}

func (dcr *ExchangeWallet) handleTipChange(ctx context.Context, newTipHash *chainhash.Hash, newTipHeight int64) {
	if dcr.ctx.Err() != nil {
		return
	}

	// Lock to avoid concurrent handleTipChange execution for simplicity.
	dcr.handleTipMtx.Lock()
	defer dcr.handleTipMtx.Unlock()

	prevTip := dcr.currentTip.Swap(&block{newTipHeight, newTipHash}).(*block)

	dcr.log.Tracef("tip change: %d (%s) => %d (%s)", prevTip.height, prevTip.hash, newTipHeight, newTipHash)

	dcr.emitTipChange(newTipHeight)

	if dcr.cycleMixer != nil {
		dcr.cycleMixer()
	}

	dcr.wg.Add(1)
	go func() {
		dcr.syncTxHistory(ctx, uint64(newTipHeight))
		dcr.wg.Done()
	}()

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

	startHeight := prevTip.height + 1

	// Redemption search would be compromised if the starting point cannot
	// be determined, as searching just the new tip might result in blocks
	// being omitted from the search operation. If that happens, cancel all
	// find redemption requests in queue.
	notifyFatalFindRedemptionError := func(s string, a ...any) {
		dcr.fatalFindRedemptionsError(fmt.Errorf("tipChange handler - "+s, a...), contractOutpoints)
	}

	// Check if the previous tip is still part of the mainchain (prevTip confs >= 0).
	// Redemption search would typically resume from prevTip.height + 1 unless the
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
	if !isMainchain {
		// The previous tip is no longer part of the mainchain. Crawl blocks
		// backwards until finding a mainchain block. Start with the block
		// that is the immediate ancestor to the previous tip.
		ancestorBlockHash, ancestorHeight, err := dcr.mainchainAncestor(ctx, &prevTipHeader.PrevBlock)
		if err != nil {
			notifyFatalFindRedemptionError("find mainchain ancestor for prev block: %s: %w", prevTipHeader.PrevBlock, err)
			return
		}

		dcr.log.Debugf("reorg detected during tip change from height %d (%s) to %d (%s)",
			ancestorHeight, ancestorBlockHash, newTipHeight, newTipHash)

		startHeight = ancestorHeight // have to recheck orphaned blocks again
	}

	// Run the redemption search from the startHeight determined above up
	// till the current tip height.
	dcr.findRedemptionsInBlockRange(startHeight, newTipHeight, contractOutpoints)
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
			// Crawled back to genesis block without finding a mainchain ancestor
			// for the previous tip. Should never happen!
			return nil, 0, fmt.Errorf("no mainchain ancestor found for block %s", blockHash)
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

func (dcr *ExchangeWallet) cachedBestBlock() *block {
	return dcr.currentTip.Load().(*block)
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
func toDCR[V uint64 | int64](v V) float64 {
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

// WalletTransaction returns a transaction that either the wallet has made or
// one in which the wallet has received funds. The txID should be either a
// coin ID or a transaction hash in hexadecimal form.
func (dcr *ExchangeWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	coinID, err := hex.DecodeString(txID)
	if err == nil {
		txHash, _, err := decodeCoinID(coinID)
		if err == nil {
			txID = txHash.String()
		}
	}

	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return nil, fmt.Errorf("tx database not initialized")
	}

	tx, err := txHistoryDB.GetTx(txID)
	if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
		return nil, err
	}
	if tx != nil && tx.Confirmed {
		return tx, nil
	}

	txHash, err := chainhash.NewHashFromStr(txID)
	if err != nil {
		return nil, fmt.Errorf("error decoding txid %s: %w", txID, err)
	}
	gtr, err := dcr.wallet.GetTransaction(ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("error getting transaction %s: %w", txID, err)
	}
	if len(gtr.Details) == 0 {
		return nil, fmt.Errorf("no details found for transaction %s", txID)
	}

	var blockHeight, blockTime uint64
	if gtr.BlockHash != "" {
		blockHash, err := chainhash.NewHashFromStr(gtr.BlockHash)
		if err != nil {
			return nil, fmt.Errorf("error decoding block hash %s: %w", gtr.BlockHash, err)
		}
		blockHeader, err := dcr.wallet.GetBlockHeader(ctx, blockHash)
		if err != nil {
			return nil, fmt.Errorf("error getting block header for block %s: %w", blockHash, err)
		}
		blockHeight = uint64(blockHeader.Height)
		blockTime = uint64(blockHeader.Timestamp.Unix())
	}

	updated := tx == nil
	if tx == nil {
		blockIndex := int64(blockHeight)
		regularTx := walletjson.LTTTRegular
		tx, err = dcr.idUnknownTx(ctx, &ListTransactionsResult{
			TxID:       txID,
			BlockIndex: &blockIndex,
			BlockTime:  int64(blockTime),
			Send:       gtr.Details[0].Category == "send",
			TxType:     &regularTx,
		})
		if err != nil {
			return nil, fmt.Errorf("xerror identifying transaction: %v", err)
		}
	}

	if tx.BlockNumber != blockHeight || tx.Timestamp != blockTime {
		tx.BlockNumber = blockHeight
		tx.Timestamp = blockTime
		updated = true
	}

	if updated {
		dcr.addTxToHistory(tx, txHash, true)
	}

	// If the wallet knows about the transaction, it will be part of the
	// available balance, so we always return Confirmed = true.
	tx.Confirmed = true
	return tx, nil
}

// TxHistory returns all the transactions the wallet has made. If refID is nil,
// then transactions starting from the most recent are returned (past is ignored).
// If past is true, the transactions prior to the refID are returned, otherwise
// the transactions after the refID are returned. n is the number of
// transactions to return. If n is <= 0, all the transactions will be returned.
func (dcr *ExchangeWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	txHistoryDB := dcr.txDB()
	if txHistoryDB == nil {
		return nil, fmt.Errorf("tx database not initialized")
	}

	return txHistoryDB.GetTxs(n, refID, past)
}

// ConfirmTransaction returns how many confirmations a redemption or refund has.
// Normally this is very straightforward. However there are two situations that
// have come up that this also handles. One is when the wallet can not find the
// transaction. This is most likely because the fee was set too low and the tx
// was removed from the mempool. In the case where it is not found, this will
// send a new tx using the provided fee suggestion. The second situation
// this watches for is a transaction that we can find but has been sitting in
// the mempool for a long time. This has been observed with the wallet in SPV
// mode and the transaction inputs having been spent by another transaction. The
// wallet will not pick up on this so we could tell it to abandon the original
// transaction and, again, send a new one using the provided feeSuggestion, but
// only warning for now. This method should not be run for the same tx concurrently.
func (dcr *ExchangeWallet) ConfirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx, feeSuggestion uint64) (*asset.ConfirmTxStatus, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	var secretHash [32]byte
	copy(secretHash[:], confirmTx.SecretHash())
	dcr.mempoolTxsMtx.RLock()
	mTx, have := dcr.mempoolTxs[secretHash]
	dcr.mempoolTxsMtx.RUnlock()

	var deleteMempoolTx bool
	defer func() {
		if deleteMempoolTx {
			dcr.mempoolTxsMtx.Lock()
			delete(dcr.mempoolTxs, secretHash)
			dcr.mempoolTxsMtx.Unlock()
		}
	}()

	tx, err := dcr.wallet.GetTransaction(dcr.ctx, txHash)
	if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
		return nil, fmt.Errorf("problem searching for %s transaction %s: %w", confirmTx.TxType(), txHash, err)
	}
	if err == nil {
		if have && mTx.txHash == *txHash {
			if tx.Confirmations == 0 && time.Now().After(mTx.firstSeen.Add(maxMempoolAge)) {
				// Transaction has been sitting in the mempool
				// for a long time now.
				//
				// TODO: Consider abandoning.
				txAge := time.Since(mTx.firstSeen)
				dcr.log.Warnf("%s transaction %v has been in the mempool for %v which is too long.", confirmTx.TxType(), txHash, txAge)
			}
		} else {
			if have {
				// This should not happen. Core has told us to
				// watch a new redeem or refund with a different
				// transaction hash for a trade we were already watching.
				return nil, fmt.Errorf("tx were were watching %s for %s with secret hash %x being "+
					"replaced by tx %s. core should not be replacing the transaction. maybe ConfirmTransaction "+
					"is being run concurrently for the same tx", mTx.txHash, confirmTx.TxType(), secretHash, *txHash)
			}
			// Will hit this if bisonw was restarted with an actively
			// redeeming or maybe refunding swap.
			dcr.mempoolTxsMtx.Lock()
			dcr.mempoolTxs[secretHash] = &mempoolTx{txHash: *txHash, firstSeen: time.Now(), txType: confirmTx.TxType()}
			dcr.mempoolTxsMtx.Unlock()
		}
		if tx.Confirmations >= requiredConfTxConfirms {
			deleteMempoolTx = true
		}
		return &asset.ConfirmTxStatus{
			Confs:  uint64(tx.Confirmations),
			Req:    requiredConfTxConfirms,
			CoinID: coinID,
		}, nil
	}

	// Transaction is missing from the point of view of our wallet!
	// Unlikely, but possible it was spent by another transaction.Check if
	// the contract is still an unspent output.

	swapHash, vout, err := decodeCoinID(confirmTx.SpendsCoinID())
	if err != nil {
		return nil, err
	}

	_, _, spentStatus, err := dcr.lookupTxOutput(dcr.ctx, swapHash, vout)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %w", err)
	}

	switch spentStatus {
	case -1, 1:
		// First find the block containing the output itself.
		scriptAddr, err := stdaddr.NewAddressScriptHashV0(confirmTx.Contract(), dcr.chainParams)
		if err != nil {
			return nil, fmt.Errorf("error encoding contract address: %w", err)
		}
		_, pkScript := scriptAddr.PaymentScript()
		outFound, block, err := dcr.externalTxOutput(dcr.ctx, newOutPoint(swapHash, vout),
			pkScript, time.Now().Add(-60*24*time.Hour)) // search up to 60 days ago
		if err != nil {
			return nil, err // possibly the contract is still in mempool
		}
		spent, err := dcr.isOutputSpent(dcr.ctx, outFound)
		if err != nil {
			return nil, fmt.Errorf("error checking if contract %v:%d is spent: %w", *swapHash, vout, err)
		}
		if !spent {
			break
		}
		vin := -1
		spendTx := outFound.spenderTx
		for i := range spendTx.TxIn {
			sigScript := spendTx.TxIn[i].SignatureScript
			sigScriptLen := len(sigScript)
			if sigScriptLen < dexdcr.SwapContractSize {
				continue
			}
			// The spent contract is at the end of the signature
			// script. Lop off the front half.
			script := sigScript[sigScriptLen-dexdcr.SwapContractSize:]
			_, _, _, sh, err := dexdcr.ExtractSwapDetails(script, dcr.chainParams)
			if err != nil {
				// This is not our script, but not necessarily
				// a problem.
				dcr.log.Tracef("Error encountered searching for the input that spends %v, "+
					"extracting swap details from vin %d of %d. Probably not a problem: %v.",
					spendTx.TxHash(), i, len(spendTx.TxIn), err)
				continue
			}
			if bytes.Equal(sh[:], secretHash[:]) {
				vin = i
				break
			}
		}
		if vin >= 0 {
			_, height, err := dcr.wallet.GetBestBlock(dcr.ctx)
			if err != nil {
				return nil, err
			}
			confs := uint64(height - block.height)
			hash := spendTx.TxHash()
			if confs < requiredConfTxConfirms {
				dcr.mempoolTxsMtx.Lock()
				dcr.mempoolTxs[secretHash] = &mempoolTx{txHash: hash, firstSeen: time.Now(), txType: confirmTx.TxType()}
				dcr.mempoolTxsMtx.Unlock()
			}
			return &asset.ConfirmTxStatus{
				Confs:  confs,
				Req:    requiredConfTxConfirms,
				CoinID: toCoinID(&hash, uint32(vin)),
			}, nil
		}
		dcr.log.Warnf("Contract coin %v spent by someone but not sure who.", confirmTx.SpendsCoinID())
		// Incorrect, but we will be in a loop of erroring if we don't
		// return something. We were unable to find the spender for some
		// reason.

		// May be still in the map if abandonTx failed.
		deleteMempoolTx = true

		return &asset.ConfirmTxStatus{
			Confs:  requiredConfTxConfirms,
			Req:    requiredConfTxConfirms,
			CoinID: coinID,
		}, nil
	}

	// The contract has not yet been redeemed or refunded, but it seems the
	// spending tx has disappeared. Assume the fee was too low at the time
	// and it was eventually purged from the mempool. Attempt to spend again
	// with a currently reasonable fee.

	var newCoinID dex.Bytes
	if confirmTx.IsRedeem() {
		form := &asset.RedeemForm{
			Redemptions: []*asset.Redemption{
				{
					Spends: confirmTx.Spends(),
					Secret: confirmTx.Secret(),
				},
			},
			FeeSuggestion: feeSuggestion,
		}
		_, coin, _, err := dcr.Redeem(form)
		if err != nil {
			return nil, fmt.Errorf("unable to re-redeem %s: %w", confirmTx.SpendsCoinID(), err)
		}
		newCoinID = coin.ID()
	} else {
		spendsCoinID := confirmTx.SpendsCoinID()
		newCoinID, err = dcr.Refund(spendsCoinID, confirmTx.Contract(), feeSuggestion)
		if err != nil {
			return nil, fmt.Errorf("unable to re-refund %s: %w", spendsCoinID, err)
		}
	}

	newTxHash, _, err := decodeCoinID(newCoinID)
	if err != nil {
		return nil, err
	}

	dcr.mempoolTxsMtx.Lock()
	dcr.mempoolTxs[secretHash] = &mempoolTx{txHash: *newTxHash, firstSeen: time.Now(), txType: confirmTx.TxType()}
	dcr.mempoolTxsMtx.Unlock()

	return &asset.ConfirmTxStatus{
		Confs:  0,
		Req:    requiredConfTxConfirms,
		CoinID: coinID,
	}, nil
}

var _ asset.GeocodeRedeemer = (*ExchangeWallet)(nil)

// RedeemGeocode redeems funds from a geocode game tx to this wallet.
func (dcr *ExchangeWallet) RedeemGeocode(code []byte, msg string) (dex.Bytes, uint64, error) {
	msgLen := len([]byte(msg))
	if msgLen > stdscript.MaxDataCarrierSizeV0 {
		return nil, 0, fmt.Errorf("message is too long. must be %d > %d", msgLen, stdscript.MaxDataCarrierSizeV0)
	}

	k, err := hdkeychain.NewMaster(code, dcr.chainParams)
	if err != nil {
		return nil, 0, fmt.Errorf("error generating key from bond: %w", err)
	}
	gameKey, err := k.SerializedPrivKey()
	if err != nil {
		return nil, 0, fmt.Errorf("error serializing private key: %w", err)
	}

	gamePub := k.SerializedPubKey()
	gameAddr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(dcrutil.Hash160(gamePub), dcr.chainParams)
	if err != nil {
		return nil, 0, fmt.Errorf("error generating address: %w", err)
	}

	gameTxs, err := getDcrdataTxs(dcr.ctx, gameAddr.String(), dcr.network)
	if err != nil {
		return nil, 0, fmt.Errorf("error getting tx from dcrdata: %w", err)
	}

	_, gameScript := gameAddr.PaymentScript()

	feeRate, err := dcr.feeRate(2)
	if err != nil {
		return nil, 0, fmt.Errorf("error getting tx fee rate: %w", err)
	}

	redeemTx := wire.NewMsgTx()
	var redeemable int64
	for _, gameTx := range gameTxs {
		txHash := gameTx.TxHash()
		for vout, txOut := range gameTx.TxOut {
			if bytes.Equal(txOut.PkScript, gameScript) {
				redeemable += txOut.Value
				prevOut := wire.NewOutPoint(&txHash, uint32(vout), wire.TxTreeRegular)
				redeemTx.AddTxIn(wire.NewTxIn(prevOut, txOut.Value, gameScript))
			}
		}
	}

	if len(redeemTx.TxIn) == 0 {
		return nil, 0, fmt.Errorf("no spendable game outputs found in %d txs for address %s", len(gameTxs), gameAddr)
	}

	var txSize uint64 = dexdcr.MsgTxOverhead + uint64(len(redeemTx.TxIn))*dexdcr.P2PKHInputSize + dexdcr.P2PKHOutputSize
	if msgLen > 0 {
		txSize += dexdcr.TxOutOverhead + 1 /* opreturn */ + uint64(wire.VarIntSerializeSize(uint64(msgLen))) + uint64(msgLen)
	}
	fees := feeRate * txSize

	if uint64(redeemable) < fees {
		return nil, 0, fmt.Errorf("estimated fees %d are less than the redeemable value %d", fees, redeemable)
	}
	win := uint64(redeemable) - fees
	if dexdcr.IsDustVal(dexdcr.P2PKHOutputSize, win, feeRate) {
		return nil, 0, fmt.Errorf("received value is dust after fees: %d - %d = %d", redeemable, fees, win)
	}

	redeemAddr, err := dcr.wallet.ExternalAddress(dcr.ctx, dcr.depositAccount())
	if err != nil {
		return nil, 0, fmt.Errorf("error getting redeem address: %w", err)
	}
	_, redeemScript := redeemAddr.PaymentScript()

	redeemTx.AddTxOut(wire.NewTxOut(int64(win), redeemScript))
	if msgLen > 0 {
		msgScript, err := txscript.NewScriptBuilder().AddOp(txscript.OP_RETURN).AddData([]byte(msg)).Script()
		if err != nil {
			return nil, 0, fmt.Errorf("error building message script: %w", err)
		}
		redeemTx.AddTxOut(wire.NewTxOut(0, msgScript))
	}

	for vin, txIn := range redeemTx.TxIn {
		redeemInSig, err := sign.RawTxInSignature(redeemTx, vin, gameScript, txscript.SigHashAll,
			gameKey, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return nil, 0, fmt.Errorf("error creating signature for input script: %w", err)
		}
		txIn.SignatureScript, err = txscript.NewScriptBuilder().AddData(redeemInSig).AddData(gamePub).Script()
		if err != nil {
			return nil, 0, fmt.Errorf("error building p2pkh sig script: %w", err)
		}
	}

	redeemHash, err := dcr.broadcastTx(redeemTx)
	if err != nil {
		return nil, 0, fmt.Errorf("error broadcasting tx: %w", err)
	}

	return toCoinID(redeemHash, 0), win, nil
}

func getDcrdataTxs(ctx context.Context, addr string, net dex.Network) (txs []*wire.MsgTx, _ error) {
	apiRoot := "https://dcrdata.decred.org/api/"
	switch net {
	case dex.Testnet:
		apiRoot = "https://testnet.dcrdata.org/api/"
	case dex.Simnet:
		apiRoot = "http://127.0.0.1:17779/api/"
	}

	var resp struct {
		Txs []struct {
			TxID string `json:"txid"`
		} `json:"address_transactions"`
	}
	if err := dexnet.Get(ctx, apiRoot+"address/"+addr, &resp); err != nil {
		return nil, fmt.Errorf("error getting address info for address %q: %w", addr, err)
	}
	for _, tx := range resp.Txs {
		txID := tx.TxID

		// tx/hex response is a hex string but is not JSON encoded.
		r, err := http.DefaultClient.Get(apiRoot + "tx/hex/" + txID)
		if err != nil {
			return nil, fmt.Errorf("error getting transaction %q: %w", txID, err)
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading response body: %w", err)
		}
		r.Body.Close()
		hexTx := string(b)
		txB, err := hex.DecodeString(hexTx)
		if err != nil {
			return nil, fmt.Errorf("error decoding hex for tx %q: %w", txID, err)
		}

		tx, err := msgTxFromBytes(txB)
		if err != nil {
			return nil, fmt.Errorf("error deserializing tx %x: %w", txID, err)
		}
		txs = append(txs, tx)
	}

	return
}
