// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/dex/networks/erc20"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	multibal "decred.org/dcrdex/dex/networks/eth/contracts/multibalance"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
	"github.com/tyler-smith/go-bip39"
)

func init() {
	dexeth.MaybeReadSimnetAddrs()
}

func registerToken(tokenID uint32, desc string) {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	netAddrs := make(map[dex.Network]string)
	for net, netToken := range token.NetTokens {
		netAddrs[net] = netToken.Address.String()
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Ethereum token",
		Description: desc,
	}, netAddrs)
}

func init() {
	asset.Register(BipID, &Driver{})
	registerToken(usdcTokenID, "The USDC Ethereum ERC20 token.")
	registerToken(usdtTokenID, "The USDT Ethereum ERC20 token.")
}

const (
	// BipID is the BIP-0044 asset ID for Ethereum.
	BipID               = 60
	defaultGasFee       = 82  // gwei
	defaultGasFeeLimit  = 200 // gwei
	defaultSendGasLimit = 21_000

	walletTypeGeth  = "geth"
	walletTypeRPC   = "rpc"
	walletTypeToken = "token"

	providersKey = "providers"

	// confCheckTimeout is the amount of time allowed to check for
	// confirmations. Testing on testnet has shown spikes up to 2.5
	// seconds. This value may need to be adjusted in the future.
	confCheckTimeout                 = 4 * time.Second
	dynamicSwapOrRedemptionFeesConfs = 2

	// coinIDTakerFoundMakerRedemption is a prefix to identify one of CoinID formats,
	// see DecodeCoinID func for details.
	coinIDTakerFoundMakerRedemption = "TakerFoundMakerRedemption:"

	// maxTxFeeGwei is the default max amount of eth that can be used in one
	// transaction. This is set by the host in the case of providers. The
	// internal node currently has no max but also cannot be used since the
	// merge.
	//
	// TODO: Find a way to ask the host about their config set max fee and
	// gas values.
	maxTxFeeGwei = 1_000_000_000
)

var (
	usdcTokenID, _ = dex.BipSymbolID("usdc.eth")
	usdtTokenID, _ = dex.BipSymbolID("usdt.eth")
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker     = time.Second
	peerCountTicker = 5 * time.Second
	walletOpts      = []*asset.ConfigOption{
		{
			Key:         "gasfeelimit",
			DisplayName: "Gas Fee Limit",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If gasfeelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: gwei / gas",
			DefaultValue: defaultGasFeeLimit,
		},
	}
	RPCOpts = []*asset.ConfigOption{
		{
			Key:         providersKey,
			DisplayName: "Provider",
			Description: "Specify one or more providers. For infrastructure " +
				"providers, prefer using wss address. Only url-based authentication " +
				"is supported. For a local node, use the filepath to an IPC file.",
			Repeatable:   providerDelimiter,
			RepeatN:      2,
			DefaultValue: "",
		},
	}
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = asset.WalletInfo{
		Name:    "Ethereum",
		Version: 0,
		// SupportedVersions: For Ethereum, the server backend maintains a
		// single protocol version, so tokens and ETH have the same set of
		// supported versions. Even though the SupportedVersions are made
		// accessible for tokens via (*TokenWallet).Info, the versions are not
		// exposed though any Driver methods or assets/driver functions. Use the
		// parent wallet's WalletInfo via (*Driver).Info if you need a token's
		// supported versions before a wallet is available.
		SupportedVersions: []uint32{0},
		UnitInfo:          dexeth.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			// {
			// 	Type:        walletTypeGeth,
			// 	Tab:         "Native",
			// 	Description: "Use the built-in DEX wallet (geth light node)",
			// 	ConfigOpts:  WalletOpts,
			// 	Seeded:      true,
			// },
			{
				Type:        walletTypeRPC,
				Tab:         "RPC",
				Description: "Infrastructure providers (e.g. Infura) or local nodes",
				ConfigOpts:  append(RPCOpts, walletOpts...),
				Seeded:      true,
				GuideLink:   "https://github.com/decred/dcrdex/blob/master/docs/wiki/Ethereum.md",
			},
			// MaxSwapsInTx and MaxRedeemsInTx are set in (Wallet).Info, since
			// the value cannot be known until we connect and get network info.
		},
		IsAccountBased: true,
	}

	// unlimitedAllowance is the maximum supported allowance for an erc20
	// contract, and is effectively unlimited.
	unlimitedAllowance = ethmath.MaxBig256
	// unlimitedAllowanceReplenishThreshold is the threshold below which we will
	// require a new approval. In practice, this will never be hit, but any
	// allowance below this will signal that WE didn't set it, and we'll require
	// an upgrade to unlimited (since we don't support limited allowance yet).
	unlimitedAllowanceReplenishThreshold = new(big.Int).Div(unlimitedAllowance, big.NewInt(2))

	seedDerivationPath = []uint32{
		hdkeychain.HardenedKeyStart + 44, // purpose 44' for HD wallets
		hdkeychain.HardenedKeyStart + 60, // eth coin type 60'
		hdkeychain.HardenedKeyStart,      // account 0'
		0,                                // branch 0
		0,                                // index 0
	}
)

// perTxGasLimit is the most gas we can use on a transaction. It is the lower of
// either the per tx or per block gas limit.
func perTxGasLimit(gasFeeLimit uint64) uint64 {
	// maxProportionOfBlockGasLimitToUse sets the maximum proportion of a
	// block's gas limit that a swap and redeem transaction will use. Since it
	// is set to 4, the max that will be used is 25% (1/4) of the block's gas
	// limit.
	const maxProportionOfBlockGasLimitToUse = 4

	// blockGasLimit is the amount of gas we can use in one transaction
	// according to the block gas limit.

	// Ethereum GasCeil: 30_000_000, Polygon: 8_000_000
	blockGasLimit := ethconfig.Defaults.Miner.GasCeil / maxProportionOfBlockGasLimitToUse

	// txGasLimit is the amount of gas we can use in one transaction
	// according to the default transaction gas fee limit.
	txGasLimit := maxTxFeeGwei / gasFeeLimit

	if blockGasLimit > txGasLimit {
		return txGasLimit
	}
	return blockGasLimit
}

// WalletConfig are wallet-level configuration settings.
type WalletConfig struct {
	GasFeeLimit uint64 `ini:"gasfeelimit"`
}

// parseWalletConfig parses the settings map into a *WalletConfig.
func parseWalletConfig(settings map[string]string) (cfg *WalletConfig, err error) {
	cfg = new(WalletConfig)
	err = config.Unmapify(settings, &cfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing wallet config: %w", err)
	}
	return cfg, nil
}

// Driver implements asset.Driver.
type Driver struct{}

// Check that Driver implements Driver and Creator.
var _ asset.Driver = (*Driver)(nil)
var _ asset.Creator = (*Driver)(nil)

// Open opens the ETH exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return newWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Ethereum.
// These are supported coin ID formats:
//  1. A transaction hash. 32 bytes
//  2. An encoded ETH funding coin id which includes the account address and
//     amount. 20 + 8 = 28 bytes
//  3. An encoded token funding coin id which includes the account address,
//     a token value, and fees. 20 + 8 + 8 = 36 bytes
//  4. A byte encoded string of the account address. 40 or 42 (with 0x) bytes
//  5. A byte encoded string which represents specific case where Taker found
//     Maker redemption on his own (while Maker failed to notify him about it
//     first). 26 (`TakerFoundMakerRedemption:` prefix) + 42 (Maker address
//     with 0x) bytes
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	switch len(coinID) {
	case common.HashLength:
		var txHash common.Hash
		copy(txHash[:], coinID)
		return txHash.String(), nil
	case fundingCoinIDSize:
		c, err := decodeFundingCoin(coinID)
		if err != nil {
			return "", err
		}
		return c.String(), nil
	case tokenFundingCoinIDSize:
		c, err := decodeTokenFundingCoin(coinID)
		if err != nil {
			return "", err
		}
		return c.String(), nil
	case common.AddressLength * 2, common.AddressLength*2 + 2:
		hexAddr := string(coinID)
		if !common.IsHexAddress(hexAddr) {
			return "", fmt.Errorf("invalid hex address %q", coinID)
		}
		return common.HexToAddress(hexAddr).String(), nil
	case len(coinIDTakerFoundMakerRedemption) + common.AddressLength*2 + 2:
		coinIDStr := string(coinID)
		if !strings.HasPrefix(coinIDStr, coinIDTakerFoundMakerRedemption) {
			return "", fmt.Errorf("coinID %q has no %s prefix", coinID, coinIDTakerFoundMakerRedemption)
		}
		return coinIDStr, nil
	}

	return "", fmt.Errorf("unknown coin ID format: %x", coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	wi := WalletInfo
	return &wi
}

// Exists checks the existence of the wallet.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	switch walletType {
	case walletTypeGeth, walletTypeRPC:
	default:
		return false, fmt.Errorf("wallet type %q unrecognized", walletType)
	}

	keyStoreDir := filepath.Join(getWalletDir(dataDir, net), "keystore")
	ks := keystore.NewKeyStore(keyStoreDir, keystore.LightScryptN, keystore.LightScryptP)
	// NOTE: If the keystore did not exist, a warning from an internal KeyStore
	// goroutine may be printed to this effect. Not an issue.
	return len(ks.Wallets()) > 0, nil
}

func (d *Driver) Create(cfg *asset.CreateWalletParams) error {
	comp, err := NetworkCompatibilityData(cfg.Net)
	if err != nil {
		return fmt.Errorf("error finding compatibility data: %v", err)
	}
	return CreateEVMWallet(dexeth.ChainIDs[cfg.Net], cfg, &comp, false)
}

// Balance is the current balance, including information about the pending
// balance.
type Balance struct {
	Current, PendingIn, PendingOut *big.Int
}

// ethFetcher represents a blockchain information fetcher. In practice, it is
// satisfied by *nodeClient. For testing, it can be satisfied by a stub.
type ethFetcher interface {
	address() common.Address
	addressBalance(ctx context.Context, addr common.Address) (*big.Int, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	chainConfig() *params.ChainConfig
	connect(ctx context.Context) error
	peerCount() uint32
	contractBackend() bind.ContractBackend
	headerByHash(ctx context.Context, txHash common.Hash) (*types.Header, error)
	lock() error
	locked() bool
	shutdown()
	sendSignedTransaction(ctx context.Context, tx *types.Transaction) error
	sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error)
	signData(data []byte) (sig, pubKey []byte, err error)
	syncProgress(context.Context) (progress *ethereum.SyncProgress, tipTime uint64, err error)
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	getTransaction(context.Context, common.Hash) (*types.Transaction, int64, error)
	txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, nonce *big.Int) (*bind.TransactOpts, error)
	currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error)
	unlock(pw string) error
	getConfirmedNonce(context.Context) (uint64, error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error)
}

// txPoolFetcher can be implemented by node types that support fetching of
// txpool transactions.
type txPoolFetcher interface {
	pendingTransactions() ([]*types.Transaction, error)
}

type pendingApproval struct {
	txHash    common.Hash
	onConfirm func()
}

type cachedBalance struct {
	stamp  time.Time
	height uint64
	bal    *big.Int
}

// Check that assetWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*ETHWallet)(nil)
var _ asset.Wallet = (*TokenWallet)(nil)
var _ asset.AccountLocker = (*ETHWallet)(nil)
var _ asset.AccountLocker = (*TokenWallet)(nil)
var _ asset.TokenMaster = (*ETHWallet)(nil)
var _ asset.WalletRestorer = (*ETHWallet)(nil)
var _ asset.LiveReconfigurer = (*ETHWallet)(nil)
var _ asset.LiveReconfigurer = (*TokenWallet)(nil)
var _ asset.TxFeeEstimator = (*ETHWallet)(nil)
var _ asset.TxFeeEstimator = (*TokenWallet)(nil)
var _ asset.DynamicSwapper = (*ETHWallet)(nil)
var _ asset.DynamicSwapper = (*TokenWallet)(nil)
var _ asset.Authenticator = (*ETHWallet)(nil)
var _ asset.TokenApprover = (*TokenWallet)(nil)
var _ asset.WalletHistorian = (*ETHWallet)(nil)
var _ asset.WalletHistorian = (*TokenWallet)(nil)

type baseWallet struct {
	// The asset subsystem starts with Connect(ctx). This ctx will be initialized
	// in parent ETHWallet once and re-used in child TokenWallet instances.
	ctx        context.Context
	net        dex.Network
	node       ethFetcher
	addr       common.Address
	log        dex.Logger
	dir        string
	walletType string

	multiBalanceAddress  common.Address
	multiBalanceContract *multibal.MultiBalanceV0

	baseChainID uint32
	chainCfg    *params.ChainConfig
	chainID     int64
	compat      *CompatibilityData
	tokens      map[uint32]*dexeth.Token

	tipMtx     sync.RWMutex
	currentTip *types.Header

	settingsMtx sync.RWMutex
	settings    map[string]string

	gasFeeLimitV uint64 // atomic

	walletsMtx sync.RWMutex
	wallets    map[uint32]*assetWallet

	monitoredTxsMtx sync.RWMutex
	monitoredTxs    map[common.Hash]*monitoredTx

	pendingTxsMtx sync.RWMutex
	pendingTxs    map[uint64]*extendedWalletTx // nonce -> tx

	// nonceSendMtx should be locked for the node.txOpts -> tx send sequence
	// for all txs, to ensure nonce correctness.
	nonceSendMtx sync.Mutex

	balances struct {
		sync.Mutex
		m map[uint32]*cachedBalance
	}

	txDB txDB
}

// assetWallet is a wallet backend for Ethereum and Eth tokens. The backend is
// how the DEX client app communicates with the Ethereum blockchain and wallet.
// assetWallet satisfies the dex.Wallet interface.
type assetWallet struct {
	*baseWallet
	assetID   uint32
	emit      *asset.WalletEmitter
	log       dex.Logger
	ui        dex.UnitInfo
	connected atomic.Bool
	wi        asset.WalletInfo

	versionedContracts map[uint32]common.Address
	versionedGases     map[uint32]*dexeth.Gases

	maxSwapGas   uint64
	maxRedeemGas uint64

	lockedFunds struct {
		mtx                sync.RWMutex
		initiateReserves   uint64
		redemptionReserves uint64
		refundReserves     uint64
	}

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[[32]byte]*findRedemptionRequest

	approvalsMtx     sync.RWMutex
	pendingApprovals map[uint32]*pendingApproval
	approvalCache    map[uint32]bool

	lastPeerCount uint32
	peersChange   func(uint32, error)

	contractors map[uint32]contractor // version -> contractor

	evmify  func(uint64) *big.Int
	atomize func(*big.Int) uint64

	// pendingTxCheckBal is protected by the pendingTxsMtx. We use this field
	// as a secondary check to see if we need to request confirmations for
	// pending txs, since tips are cached for up to 10 seconds. We check the
	// status of pending txs if the tip has changed OR if the balance has
	// changed.
	pendingTxCheckBal *big.Int
}

// ETHWallet implements some Ethereum-specific methods.
type ETHWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect     int64
	defaultProviders []string

	*assetWallet
}

// TokenWallet implements some token-specific methods.
type TokenWallet struct {
	*assetWallet

	cfg      *tokenWalletConfig
	parent   *assetWallet
	token    *dexeth.Token
	netToken *dexeth.NetToken
}

func (w *assetWallet) maxSwapsAndRedeems() (maxSwaps, maxRedeems uint64) {
	txGasLimit := perTxGasLimit(atomic.LoadUint64(&w.gasFeeLimitV))
	return txGasLimit / w.maxSwapGas, txGasLimit / w.maxRedeemGas
}

// Info returns basic information about the wallet and asset.
func (w *assetWallet) Info() *asset.WalletInfo {
	wi := w.wi
	maxSwaps, maxRedeems := w.maxSwapsAndRedeems()
	wi.MaxSwapsInTx = maxSwaps
	wi.MaxRedeemsInTx = maxRedeems
	return &wi
}

// genWalletSeed uses the wallet seed passed from core as the entropy for
// generating a BIP-39 mnemonic. Then it returns the wallet seed generated
// from the mnemonic which can be used to derive a private key.
func genWalletSeed(entropy []byte) ([]byte, error) {
	if len(entropy) < 32 || len(entropy) > 64 {
		return nil, fmt.Errorf("wallet entropy must be 32 to 64 bytes long")
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("error deriving mnemonic: %w", err)
	}
	return bip39.NewSeed(mnemonic, ""), nil
}

func privKeyFromSeed(seed []byte) (pk []byte, zero func(), err error) {
	walletSeed, err := genWalletSeed(seed)
	if err != nil {
		return nil, nil, err
	}
	defer encode.ClearBytes(walletSeed)

	extKey, err := keygen.GenDeepChild(walletSeed, seedDerivationPath)
	if err != nil {
		return nil, nil, err
	}
	// defer extKey.Zero()
	pk, err = extKey.SerializedPrivKey()
	if err != nil {
		extKey.Zero()
		return nil, nil, err
	}
	return pk, extKey.Zero, nil
}

func CreateEVMWallet(chainID int64, createWalletParams *asset.CreateWalletParams, compat *CompatibilityData, skipConnect bool) error {
	switch createWalletParams.Type {
	case walletTypeGeth:
		return asset.ErrWalletTypeDisabled
	case walletTypeRPC:
	default:
		return fmt.Errorf("wallet type %q unrecognized", createWalletParams.Type)
	}

	walletDir := getWalletDir(createWalletParams.DataDir, createWalletParams.Net)

	privateKey, zero, err := privKeyFromSeed(createWalletParams.Seed)
	if err != nil {
		return err
	}
	defer zero()

	switch createWalletParams.Type {
	// case walletTypeGeth:
	// 	node, err := prepareNode(&nodeConfig{
	// 		net:    createWalletParams.Net,
	// 		appDir: walletDir,
	// 	})
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer node.Close()
	// 	return importKeyToNode(node, privateKey, createWalletParams.Pass)
	case walletTypeRPC:
		// Make the wallet dir if it does not exist, otherwise we may fail to
		// write the compliant-providers.json file. Create the keystore
		// subdirectory as well to avoid a "failed to watch keystore folder"
		// error from the keystore's internal account cache supervisor.
		keystoreDir := filepath.Join(walletDir, "keystore")
		if err := os.MkdirAll(keystoreDir, 0700); err != nil {
			return err
		}

		// TODO: This procedure may actually work for walletTypeGeth too.
		ks := keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP)

		priv, err := crypto.ToECDSA(privateKey)
		if err != nil {
			return err
		}

		// If the user supplied endpoints, check them now.
		providerDef := createWalletParams.Settings[providersKey]
		if !skipConnect && len(providerDef) > 0 {
			endpoints := strings.Split(providerDef, providerDelimiter)
			if err := createAndCheckProviders(context.Background(), walletDir, endpoints,
				big.NewInt(chainID), compat, createWalletParams.Net, createWalletParams.Logger); err != nil {
				return fmt.Errorf("create and check providers: %v", err)
			}
		}
		return importKeyToKeyStore(ks, priv, createWalletParams.Pass)
	}

	return fmt.Errorf("unknown wallet type %q", createWalletParams.Type)
}

// newWallet is the constructor for an Ethereum asset.Wallet.
func newWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (w *ETHWallet, err error) {
	chainCfg, err := ChainConfig(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Ethereum genesis configuration for network %s", net)
	}
	comp, err := NetworkCompatibilityData(net)
	if err != nil {
		return nil, fmt.Errorf("failed to locate Ethereum compatibility data: %s", net)
	}
	contracts := make(map[uint32]common.Address, 1)
	for ver, netAddrs := range dexeth.ContractAddresses {
		for netw, addr := range netAddrs {
			if netw == net {
				contracts[ver] = addr
				break
			}
		}
	}

	var defaultProviders []string
	switch net {
	case dex.Simnet:
		u, _ := user.Current()
		defaultProviders = []string{filepath.Join(u.HomeDir, "dextest", "eth", "alpha", "node", "geth.ipc")}
	case dex.Testnet:
		defaultProviders = []string{
			"https://rpc.ankr.com/eth_sepolia",
			"https://ethereum-sepolia.blockpi.network/v1/rpc/public",
			"https://eth-sepolia.public.blastapi.io",
			"https://endpoints.omniatech.io/v1/eth/sepolia/public",
			"https://rpc-sepolia.rockx.com",
			"https://rpc.sepolia.org",
			"https://eth-sepolia-public.unifra.io",
		}
	case dex.Mainnet:
		defaultProviders = []string{
			"https://rpc.ankr.com/eth",
			"https://ethereum.blockpi.network/v1/rpc/public",
			"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7",
			"https://rpc.builder0x69.io",
			"https://rpc.flashbots.net",
			"wss://eth.llamarpc.com",
		}
	}

	return NewEVMWallet(&EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           assetCFG,
		CompatData:         &comp,
		VersionedGases:     dexeth.VersionedGases,
		Tokens:             dexeth.Tokens,
		Logger:             logger,
		BaseChainContracts: contracts,
		MultiBalAddress:    dexeth.MultiBalanceAddresses[net],
		WalletInfo:         WalletInfo,
		Net:                net,
		DefaultProviders:   defaultProviders,
	})
}

// EVMWalletConfig is the configuration for an evm-compatible wallet.
type EVMWalletConfig struct {
	BaseChainID        uint32
	ChainCfg           *params.ChainConfig
	AssetCfg           *asset.WalletConfig
	CompatData         *CompatibilityData
	VersionedGases     map[uint32]*dexeth.Gases
	Tokens             map[uint32]*dexeth.Token
	Logger             dex.Logger
	BaseChainContracts map[uint32]common.Address
	DefaultProviders   []string
	MultiBalAddress    common.Address
	WalletInfo         asset.WalletInfo
	Net                dex.Network
}

func NewEVMWallet(cfg *EVMWalletConfig) (w *ETHWallet, err error) {
	assetID := cfg.BaseChainID
	chainID := cfg.ChainCfg.ChainID.Int64()

	// var cl ethFetcher
	switch cfg.AssetCfg.Type {
	case walletTypeGeth:
		return nil, asset.ErrWalletTypeDisabled
	case walletTypeRPC:
		if providerDef := cfg.AssetCfg.Settings[providersKey]; len(providerDef) == 0 && len(cfg.DefaultProviders) == 0 {
			return nil, errors.New("no providers specified")
		}
	default:
		return nil, fmt.Errorf("unknown wallet type %q", cfg.AssetCfg.Type)
	}

	wCfg, err := parseWalletConfig(cfg.AssetCfg.Settings)
	if err != nil {
		return nil, err
	}

	gasFeeLimit := wCfg.GasFeeLimit
	if gasFeeLimit == 0 {
		gasFeeLimit = defaultGasFeeLimit
	}
	eth := &baseWallet{
		net:          cfg.Net,
		baseChainID:  cfg.BaseChainID,
		chainCfg:     cfg.ChainCfg,
		chainID:      chainID,
		compat:       cfg.CompatData,
		tokens:       cfg.Tokens,
		log:          cfg.Logger,
		dir:          cfg.AssetCfg.DataDir,
		walletType:   cfg.AssetCfg.Type,
		settings:     cfg.AssetCfg.Settings,
		gasFeeLimitV: gasFeeLimit,
		wallets:      make(map[uint32]*assetWallet),
		monitoredTxs: make(map[common.Hash]*monitoredTx),
		pendingTxs:   make(map[uint64]*extendedWalletTx),
		// Can be empty
		multiBalanceAddress: cfg.MultiBalAddress,
	}

	var maxSwapGas, maxRedeemGas uint64
	for _, gases := range cfg.VersionedGases {
		if gases.Swap > maxSwapGas {
			maxSwapGas = gases.Swap
		}
		if gases.Redeem > maxRedeemGas {
			maxRedeemGas = gases.Redeem
		}
	}

	txGasLimit := perTxGasLimit(gasFeeLimit)

	if maxSwapGas == 0 || txGasLimit < maxSwapGas {
		return nil, errors.New("max swaps cannot be zero or undefined")
	}
	if maxRedeemGas == 0 || txGasLimit < maxRedeemGas {
		return nil, errors.New("max redeems cannot be zero or undefined")
	}

	aw := &assetWallet{
		baseWallet:         eth,
		log:                cfg.Logger,
		assetID:            assetID,
		versionedContracts: cfg.BaseChainContracts,
		versionedGases:     cfg.VersionedGases,
		maxSwapGas:         maxSwapGas,
		maxRedeemGas:       maxRedeemGas,
		emit:               cfg.AssetCfg.Emit,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		pendingApprovals:   make(map[uint32]*pendingApproval),
		approvalCache:      make(map[uint32]bool),
		peersChange:        cfg.AssetCfg.PeersChange,
		contractors:        make(map[uint32]contractor),
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
		ui:                 dexeth.UnitInfo,
		pendingTxCheckBal:  new(big.Int),
		wi:                 cfg.WalletInfo,
	}

	maxSwaps, maxRedeems := aw.maxSwapsAndRedeems()

	cfg.Logger.Infof("ETH wallet will support a maximum of %d swaps and %d redeems per transaction.",
		maxSwaps, maxRedeems)

	aw.wallets = map[uint32]*assetWallet{
		assetID: aw,
	}

	return &ETHWallet{
		assetWallet:      aw,
		defaultProviders: cfg.DefaultProviders,
	}, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

// Connect connects to the node RPC server. Satisfies dex.Connector.
func (w *ETHWallet) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	var cl ethFetcher
	switch w.walletType {
	case walletTypeGeth:
		// cl, err = newNodeClient(getWalletDir(w.dir, w.net), w.net, w.log.SubLogger("NODE"))
		// if err != nil {
		// 	return nil, err
		// }
		return nil, asset.ErrWalletTypeDisabled
	case walletTypeRPC:
		w.settingsMtx.RLock()
		defer w.settingsMtx.RUnlock()
		endpoints := w.defaultProviders
		if providerDef, found := w.settings[providersKey]; found && len(providerDef) > 0 {
			endpoints = strings.Split(providerDef, " ")
		}
		cl, err = newMultiRPCClient(w.dir, endpoints, w.log.SubLogger("RPC"), w.chainCfg, w.net)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown wallet type %q", w.walletType)
	}

	w.node = cl
	w.addr = cl.address()
	w.ctx = ctx // TokenWallet will re-use this ctx.

	err = w.node.connect(ctx)
	if err != nil {
		return nil, err
	}

	for ver, constructor := range contractorConstructors {
		contractAddr, exists := w.versionedContracts[ver]
		if !exists || contractAddr == (common.Address{}) {
			return nil, fmt.Errorf("no contract address for version %d, net %s", ver, w.net)
		}
		c, err := constructor(contractAddr, w.addr, w.node.contractBackend())
		if err != nil {
			return nil, fmt.Errorf("error constructor version %d contractor: %v", ver, err)
		}
		w.contractors[ver] = c
	}

	if w.multiBalanceAddress != (common.Address{}) {
		w.multiBalanceContract, err = multibal.NewMultiBalanceV0(w.multiBalanceAddress, cl.contractBackend())
		if err != nil {
			w.log.Errorf("Error loading MultiBalance contract: %v", err)
		}
	}

	w.txDB = newBadgerTxDB(filepath.Join(w.dir, "tx.db"), w.log.SubLogger("TXDB"))
	wg, err := w.txDB.connect(ctx)
	if err != nil {
		return nil, err
	}

	w.monitoredTxs, err = w.txDB.getMonitoredTxs()
	if err != nil {
		return nil, err
	}

	w.pendingTxs, err = w.txDB.getPendingTxs()
	if err != nil {
		return nil, err
	}

	// Initialize the best block.
	bestHdr, err := w.node.bestHeader(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block hash: %w", err)
	}

	w.tipMtx.Lock()
	w.currentTip = bestHdr
	w.tipMtx.Unlock()
	height := w.currentTip.Number
	// NOTE: We should be using the tipAtConnect to set Progress in SyncStatus.
	atomic.StoreInt64(&w.tipAtConnect, height.Int64())
	w.log.Infof("Connected to eth (%s), at height %d", w.walletType, height)

	w.connected.Store(true)

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorBlocks(ctx)
		w.node.shutdown()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorPeers(ctx)
	}()

	go func() {
		<-ctx.Done()
		w.connected.Store(false)
	}()

	return wg, nil
}

// Connect waits for context cancellation and closes the WaitGroup. Satisfies
// dex.Connector.
func (w *TokenWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if w.parent.ctx == nil || w.parent.ctx.Err() != nil {
		return nil, fmt.Errorf("parent wallet not connected")
	}

	err := w.loadContractors()
	if err != nil {
		return nil, err
	}

	w.connected.Store(true)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case <-w.parent.ctx.Done():
			w.connected.Store(false)
		}
	}()

	return &wg, nil
}

// Reconfigure attempts to reconfigure the wallet.
func (w *ETHWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, currentAddress string) (restart bool, err error) {
	walletCfg, err := parseWalletConfig(cfg.Settings)
	if err != nil {
		return false, err
	}

	gasFeeLimit := walletCfg.GasFeeLimit
	if walletCfg.GasFeeLimit == 0 {
		gasFeeLimit = defaultGasFeeLimit
	}

	// For now, we only are supporting multiRPCClient nodes. If we re-implement
	// P2P nodes, we'll have to add protection to the node field to allow for
	// reconfiguration of type.
	// We also need to consider how to handle the pendingTxs if we switch node
	// types, since right now we only use that map for multiRPCClient.

	if rpc, is := w.node.(*multiRPCClient); is {
		walletDir := getWalletDir(w.dir, w.net)
		providerDef := cfg.Settings[providersKey]
		var endpoints []string
		if len(providerDef) > 0 {
			endpoints = strings.Split(providerDef, " ")
		} else {
			endpoints = w.defaultProviders
		}

		if err := rpc.reconfigure(ctx, endpoints, w.compat, walletDir); err != nil {
			return false, err
		}
	}

	w.settingsMtx.Lock()
	w.settings = cfg.Settings
	w.settingsMtx.Unlock()

	atomic.StoreUint64(&w.baseWallet.gasFeeLimitV, gasFeeLimit)

	return false, nil
}

// Reconfigure attempts to reconfigure the wallet. The token wallet has
// no configurations.
func (w *TokenWallet) Reconfigure(context.Context, *asset.WalletConfig, string) (bool, error) {
	return false, nil
}

func (eth *baseWallet) connectedWallets() []*assetWallet {
	eth.walletsMtx.RLock()
	defer eth.walletsMtx.RUnlock()

	m := make([]*assetWallet, 0, len(eth.wallets))

	for _, w := range eth.wallets {
		if w.connected.Load() {
			m = append(m, w)
		}
	}
	return m
}

func (eth *baseWallet) wallet(assetID uint32) *assetWallet {
	eth.walletsMtx.RLock()
	defer eth.walletsMtx.RUnlock()
	return eth.wallets[assetID]
}

func (eth *baseWallet) gasFeeLimit() uint64 {
	return atomic.LoadUint64(&eth.gasFeeLimitV)
}

// tokenWalletConfig is the configuration options for token wallets.
type tokenWalletConfig struct {
	// LimitAllowance disabled for now.
	// See https://github.com/decred/dcrdex/pull/1394#discussion_r780479402.
	// LimitAllowance bool `ini:"limitAllowance"`
}

// parseTokenWalletConfig parses the settings map into a *tokenWalletConfig.
func parseTokenWalletConfig(settings map[string]string) (cfg *tokenWalletConfig, err error) {
	cfg = new(tokenWalletConfig)
	err = config.Unmapify(settings, cfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing wallet config: %w", err)
	}
	return cfg, nil
}

// CreateTokenWallet "creates" a wallet for a token. There is really nothing
// to do, except check that the token exists.
func (w *baseWallet) CreateTokenWallet(tokenID uint32, _ map[string]string) error {
	// Just check that the token exists for now.
	if w.tokens[tokenID] == nil {
		return fmt.Errorf("token not found for asset ID %d", tokenID)
	}
	return nil
}

// OpenTokenWallet creates a new TokenWallet.
func (w *ETHWallet) OpenTokenWallet(tokenCfg *asset.TokenConfig) (asset.Wallet, error) {
	token, found := w.tokens[tokenCfg.AssetID]
	if !found {
		return nil, fmt.Errorf("token %d not found", tokenCfg.AssetID)
	}

	cfg, err := parseTokenWalletConfig(tokenCfg.Settings)
	if err != nil {
		return nil, err
	}

	netToken := token.NetTokens[w.net]
	if netToken == nil || len(netToken.SwapContracts) == 0 {
		return nil, fmt.Errorf("could not find token with ID %d on network %s", w.assetID, w.net)
	}

	var maxSwapGas, maxRedeemGas uint64
	for _, contract := range netToken.SwapContracts {
		if contract.Gas.Swap > maxSwapGas {
			maxSwapGas = contract.Gas.Swap
		}
		if contract.Gas.Redeem > maxRedeemGas {
			maxRedeemGas = contract.Gas.Redeem
		}
	}

	txGasLimit := perTxGasLimit(atomic.LoadUint64(&w.gasFeeLimitV))

	if maxSwapGas == 0 || txGasLimit < maxSwapGas {
		return nil, errors.New("max swaps cannot be zero or undefined")
	}
	if maxRedeemGas == 0 || txGasLimit < maxRedeemGas {
		return nil, errors.New("max redeems cannot be zero or undefined")
	}

	contracts := make(map[uint32]common.Address)
	gases := make(map[uint32]*dexeth.Gases)
	for ver, c := range netToken.SwapContracts {
		contracts[ver] = c.Address
		gases[ver] = &c.Gas
	}

	aw := &assetWallet{
		baseWallet:         w.baseWallet,
		log:                w.baseWallet.log.SubLogger(strings.ToUpper(dex.BipIDSymbol(tokenCfg.AssetID))),
		assetID:            tokenCfg.AssetID,
		versionedContracts: contracts,
		versionedGases:     gases,
		maxSwapGas:         maxSwapGas,
		maxRedeemGas:       maxRedeemGas,
		emit:               tokenCfg.Emit,
		peersChange:        tokenCfg.PeersChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		pendingApprovals:   make(map[uint32]*pendingApproval),
		approvalCache:      make(map[uint32]bool),
		contractors:        make(map[uint32]contractor),
		evmify:             token.AtomicToEVM,
		atomize:            token.EVMToAtomic,
		ui:                 token.UnitInfo,
		wi: asset.WalletInfo{
			Name:              token.Name,
			Version:           w.wi.Version,
			SupportedVersions: w.wi.SupportedVersions,
			UnitInfo:          token.UnitInfo,
		},
		pendingTxCheckBal: new(big.Int),
	}

	w.baseWallet.walletsMtx.Lock()
	w.baseWallet.wallets[tokenCfg.AssetID] = aw
	w.baseWallet.walletsMtx.Unlock()

	return &TokenWallet{
		assetWallet: aw,
		cfg:         cfg,
		parent:      w.assetWallet,
		token:       token,
		netToken:    netToken,
	}, nil
}

// OwnsDepositAddress indicates if an address belongs to the wallet. The address
// need not be a EIP55-compliant formatted address. It may or may not have a 0x
// prefix, and case is not important.
//
// In Ethereum, an address is an account.
func (eth *baseWallet) OwnsDepositAddress(address string) (bool, error) {
	if !common.IsHexAddress(address) {
		return false, errors.New("invalid address")
	}
	addr := common.HexToAddress(address)
	return addr == eth.addr, nil
}

func (w *assetWallet) amtString(amt uint64) string {
	return fmt.Sprintf("%s %s", w.ui.ConventionalString(amt), w.ui.Conventional.Unit)
}

// fundReserveType represents the various uses for which funds need to be locked:
// initiations, redemptions, and refunds.
type fundReserveType uint32

const (
	initiationReserve fundReserveType = iota
	redemptionReserve
	refundReserve
)

func (f fundReserveType) String() string {
	switch f {
	case initiationReserve:
		return "initiation"
	case redemptionReserve:
		return "redemption"
	case refundReserve:
		return "refund"
	default:
		return ""
	}
}

// fundReserveOfType returns a pointer to the funds reserved for a particular
// use case.
func (w *assetWallet) fundReserveOfType(t fundReserveType) *uint64 {
	switch t {
	case initiationReserve:
		return &w.lockedFunds.initiateReserves
	case redemptionReserve:
		return &w.lockedFunds.redemptionReserves
	case refundReserve:
		return &w.lockedFunds.refundReserves
	default:
		panic(fmt.Sprintf("invalid fund reserve type: %v", t))
	}
}

// lockFunds locks funds for a use case.
func (w *assetWallet) lockFunds(amt uint64, t fundReserveType) error {
	balance, err := w.balance()
	if err != nil {
		return err
	}

	if balance.Available < amt {
		return fmt.Errorf("attempting to lock more %s for %s than is currently available. %d > %d %s",
			dex.BipIDSymbol(w.assetID), t, amt, balance.Available, w.ui.AtomicUnit)
	}

	w.lockedFunds.mtx.Lock()
	defer w.lockedFunds.mtx.Unlock()

	*w.fundReserveOfType(t) += amt
	return nil
}

// unlockFunds unlocks funds for a use case.
func (w *assetWallet) unlockFunds(amt uint64, t fundReserveType) {
	w.lockedFunds.mtx.Lock()
	defer w.lockedFunds.mtx.Unlock()

	reserve := w.fundReserveOfType(t)

	if *reserve < amt {
		*reserve = 0
		w.log.Errorf("attempting to unlock more for %s than is currently locked - %d > %d. "+
			"clearing all locked funds", t, amt, *reserve)
		return
	}

	*reserve -= amt
}

// amountLocked returns the total amount currently locked.
func (w *assetWallet) amountLocked() uint64 {
	w.lockedFunds.mtx.RLock()
	defer w.lockedFunds.mtx.RUnlock()
	return w.lockedFunds.initiateReserves + w.lockedFunds.redemptionReserves + w.lockedFunds.refundReserves
}

// Balance returns the available and locked funds (token or eth).
func (w *assetWallet) Balance() (*asset.Balance, error) {
	return w.balance()
}

// balance returns the total available funds in the account.
func (w *assetWallet) balance() (*asset.Balance, error) {
	bal, err := w.balanceWithTxPool()
	if err != nil {
		return nil, fmt.Errorf("pending balance error: %w", err)
	}

	locked := w.amountLocked()
	var available uint64
	if w.atomize(bal.Current) > locked+w.atomize(bal.PendingOut) {
		available = w.atomize(bal.Current) - locked - w.atomize(bal.PendingOut)
	}

	return &asset.Balance{
		Available: available,
		Locked:    locked,
		Immature:  w.atomize(bal.PendingIn),
	}, nil
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
func (w *ETHWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return w.maxOrder(ord.LotSize, ord.MaxFeeRate, ord.AssetVersion,
		ord.RedeemVersion, ord.RedeemAssetID, nil)
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration.
func (w *TokenWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return w.maxOrder(ord.LotSize, ord.MaxFeeRate, ord.AssetVersion,
		ord.RedeemVersion, ord.RedeemAssetID, w.parent)
}

func (w *assetWallet) maxOrder(lotSize uint64, maxFeeRate uint64, ver uint32,
	redeemVer, redeemAssetID uint32, feeWallet *assetWallet) (*asset.SwapEstimate, error) {
	balance, err := w.Balance()
	if err != nil {
		return nil, err
	}
	if balance.Available == 0 {
		return &asset.SwapEstimate{}, nil
	}

	g, err := w.initGasEstimate(1, ver, redeemVer, redeemAssetID)
	if err != nil {
		return nil, fmt.Errorf("gasEstimate error: %w", err)
	}

	refundCost := g.Refund * maxFeeRate
	oneFee := g.oneGas * maxFeeRate
	var lots uint64
	if feeWallet == nil {
		lots = balance.Available / (lotSize + oneFee + refundCost)
	} else { // token
		lots = balance.Available / lotSize
		parentBal, err := feeWallet.Balance()
		if err != nil {
			return nil, fmt.Errorf("error getting base chain balance: %w", err)
		}
		feeLots := parentBal.Available / (oneFee + refundCost)
		if feeLots < lots {
			w.log.Infof("MaxOrder reducing lots because of low fee reserves: %d -> %d", lots, feeLots)
			lots = feeLots
		}
	}

	if lots < 1 {
		return &asset.SwapEstimate{}, nil
	}
	return w.estimateSwap(lots, lotSize, maxFeeRate, ver, redeemVer, redeemAssetID)
}

// PreSwap gets order estimates based on the available funds and the wallet
// configuration.
func (w *ETHWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	return w.preSwap(req, nil)
}

// PreSwap gets order estimates based on the available funds and the wallet
// configuration.
func (w *TokenWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	return w.preSwap(req, w.parent)
}

func (w *assetWallet) preSwap(req *asset.PreSwapForm, feeWallet *assetWallet) (*asset.PreSwap, error) {
	maxEst, err := w.maxOrder(req.LotSize, req.MaxFeeRate, req.Version,
		req.RedeemVersion, req.RedeemAssetID, feeWallet)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est, err := w.estimateSwap(req.Lots, req.LotSize, req.MaxFeeRate,
		req.Version, req.RedeemVersion, req.RedeemAssetID)
	if err != nil {
		return nil, err
	}

	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// MaxFundingFees returns 0 because ETH does not have funding fees.
func (w *baseWallet) MaxFundingFees(_ uint32, _ uint64, _ map[string]string) uint64 {
	return 0
}

// SingleLotSwapRefundFees returns the fees for a swap transaction for a single lot.
func (w *assetWallet) SingleLotSwapRefundFees(version uint32, feeSuggestion uint64, _ bool) (swapFees uint64, refundFees uint64, err error) {
	g := w.gases(version)
	if g == nil {
		return 0, 0, fmt.Errorf("no gases known for %d version %d", w.assetID, version)
	}
	return g.Swap * feeSuggestion, g.Refund * feeSuggestion, nil
}

// estimateSwap prepares an *asset.SwapEstimate. The estimate does not include
// funds that might be locked for refunds.
func (w *assetWallet) estimateSwap(lots, lotSize uint64, maxFeeRate uint64, ver uint32,
	redeemVer, redeemAssetID uint32) (*asset.SwapEstimate, error) {
	if lots == 0 {
		return &asset.SwapEstimate{}, nil
	}

	rateNow, err := w.currentFeeRate(w.ctx)
	if err != nil {
		return nil, err
	}
	rate, err := dexeth.WeiToGweiUint64(rateNow)
	if err != nil {
		return nil, fmt.Errorf("invalid current fee rate: %v", err)
	}

	// This is an estimate, so we use the (lower) live gas estimates.
	oneSwap, err := w.estimateInitGas(w.ctx, 1, ver)
	if err != nil {
		return nil, fmt.Errorf("(%d) error estimating swap gas: %v", w.assetID, err)
	}

	// NOTE: nSwap is neither best nor worst case. A single match can be
	// multiple lots. See RealisticBestCase descriptions.

	value := lots * lotSize
	oneGasMax := oneSwap * lots
	maxFees := oneGasMax * maxFeeRate

	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: oneGasMax * rate,
		RealisticBestCase:  oneSwap * rate, // not even batch, just perfect match
	}, nil
}

// gases gets the gas table for the specified contract version.
func (w *assetWallet) gases(contractVer uint32) *dexeth.Gases {
	return gases(contractVer, w.versionedGases)
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (w *assetWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	oneRedeem, nRedeem, err := w.redeemGas(int(req.Lots), req.Version)
	if err != nil {
		return nil, err
	}

	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  nRedeem * req.FeeSuggestion,
			RealisticWorstCase: oneRedeem * req.Lots * req.FeeSuggestion,
		},
	}, nil
}

// SingleLotRedeemFees returns the fees for a redeem transaction for a single lot.
func (w *assetWallet) SingleLotRedeemFees(version uint32, feeSuggestion uint64) (fees uint64, err error) {
	g := w.gases(version)
	if g == nil {
		return 0, fmt.Errorf("no gases known for %d version %d", w.assetID, version)
	}

	return g.Redeem * feeSuggestion, nil
}

// coin implements the asset.Coin interface for ETH
type coin struct {
	id common.Hash
	// the value can be determined from the coin id, but for some
	// coin ids a lookup would be required from the blockchain to
	// determine its value, so this field is used as a cache.
	value uint64
}

// ID is the ETH coins ID. For functions related to funding an order,
// the ID must contain an encoded fundingCoinID, but when returned from
// Swap, it will contain the transaction hash used to initiate the swap.
func (c *coin) ID() dex.Bytes {
	return c.id[:]
}

func (c *coin) TxID() string {
	return c.String()
}

// String is a string representation of the coin.
func (c *coin) String() string {
	return c.id.String()
}

// Value returns the value in gwei of the coin.
func (c *coin) Value() uint64 {
	return c.value
}

var _ asset.Coin = (*coin)(nil)

func (eth *ETHWallet) createFundingCoin(amount uint64) *fundingCoin {
	return createFundingCoin(eth.addr, amount)
}

func (eth *TokenWallet) createTokenFundingCoin(amount, fees uint64) *tokenFundingCoin {
	return createTokenFundingCoin(eth.addr, amount, fees)
}

// FundOrder locks value for use in an order.
func (w *ETHWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), ord.Version,
		ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error estimating swap gas: %v", err)
	}

	ethToLock := ord.MaxFeeRate*g.Swap*ord.MaxSwapCount + ord.Value
	// Note: In a future refactor, we could lock the redemption funds here too
	// and signal to the user so that they don't call `RedeemN`. This has the
	// same net effect, but avoids a lockFunds -> unlockFunds for us and likely
	// some work for the caller as well. We can't just always do it that way and
	// remove RedeemN, since we can't guarantee that the redemption asset is in
	// our fee-family. though it could still be an AccountRedeemer.
	w.log.Debugf("Locking %s to swap %s in up to %d swaps at a fee rate of %d gwei/gas using up to %d gas per swap",
		w.amtString(ethToLock), w.amtString(ord.Value), ord.MaxSwapCount, ord.MaxFeeRate, g.Swap)

	coin := w.createFundingCoin(ethToLock)

	if err = w.lockFunds(ethToLock, initiationReserve); err != nil {
		return nil, nil, 0, err
	}

	return asset.Coins{coin}, []dex.Bytes{nil}, 0, nil
}

// FundOrder locks value for use in an order.
func (w *TokenWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	approvalStatus, err := w.approvalStatus(ord.Version)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting approval status: %v", err)
	}
	if approvalStatus != asset.Approved {
		return nil, nil, 0, asset.ErrUnapprovedToken
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), ord.Version,
		ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error estimating swap gas: %v", err)
	}

	ethToLock := ord.MaxFeeRate * g.Swap * ord.MaxSwapCount

	var success bool
	if err = w.lockFunds(ord.Value, initiationReserve); err != nil {
		return nil, nil, 0, fmt.Errorf("error locking token funds: %v", err)
	}
	defer func() {
		if !success {
			w.unlockFunds(ord.Value, initiationReserve)
		}
	}()

	w.log.Debugf("Locking %s to swap %s in up to %d swaps at a fee rate of %d gwei/gas using up to %d gas per swap",
		w.parent.amtString(ethToLock), w.amtString(ord.Value), ord.MaxSwapCount, ord.MaxFeeRate, g.Swap)
	if err := w.parent.lockFunds(ethToLock, initiationReserve); err != nil {
		return nil, nil, 0, err
	}

	coin := w.createTokenFundingCoin(ord.Value, ethToLock)

	success = true
	return asset.Coins{coin}, []dex.Bytes{nil}, 0, nil
}

// FundMultiOrder funds multiple orders in one shot. No special handling is
// required for ETH as ETH does not over-lock during funding.
func (w *ETHWallet) FundMultiOrder(ord *asset.MultiOrder, maxLock uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	g, err := w.initGasEstimate(1, ord.Version, ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error estimating swap gas: %v", err)
	}

	var totalToLock uint64
	allCoins := make([]asset.Coins, 0, len(ord.Values))
	for _, value := range ord.Values {
		toLock := ord.MaxFeeRate*g.Swap*value.MaxSwapCount + value.Value
		allCoins = append(allCoins, asset.Coins{w.createFundingCoin(toLock)})
		totalToLock += toLock
	}

	if maxLock > 0 && maxLock < totalToLock {
		return nil, nil, 0, fmt.Errorf("insufficient funds to lock %d for %d orders", totalToLock, len(ord.Values))
	}

	if err = w.lockFunds(totalToLock, initiationReserve); err != nil {
		return nil, nil, 0, err
	}

	redeemScripts := make([][]dex.Bytes, len(ord.Values))
	for i := range redeemScripts {
		redeemScripts[i] = []dex.Bytes{nil}
	}

	return allCoins, redeemScripts, 0, nil
}

// FundMultiOrder funds multiple orders in one shot. No special handling is
// required for ETH as ETH does not over-lock during funding.
func (w *TokenWallet) FundMultiOrder(ord *asset.MultiOrder, maxLock uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	approvalStatus, err := w.approvalStatus(ord.Version)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting approval status: %v", err)
	}
	if approvalStatus != asset.Approved {
		return nil, nil, 0, asset.ErrUnapprovedToken
	}

	g, err := w.initGasEstimate(1, ord.Version,
		ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error estimating swap gas: %v", err)
	}

	var totalETHToLock, totalTokenToLock uint64
	allCoins := make([]asset.Coins, 0, len(ord.Values))
	for _, value := range ord.Values {
		ethToLock := ord.MaxFeeRate * g.Swap * value.MaxSwapCount
		tokenToLock := value.Value
		allCoins = append(allCoins, asset.Coins{w.createTokenFundingCoin(tokenToLock, ethToLock)})
		totalETHToLock += ethToLock
		totalTokenToLock += tokenToLock
	}

	var success bool
	if err = w.lockFunds(totalTokenToLock, initiationReserve); err != nil {
		return nil, nil, 0, fmt.Errorf("error locking token funds: %v", err)
	}
	defer func() {
		if !success {
			w.unlockFunds(totalTokenToLock, initiationReserve)
		}
	}()

	if err := w.parent.lockFunds(totalETHToLock, initiationReserve); err != nil {
		return nil, nil, 0, err
	}

	redeemScripts := make([][]dex.Bytes, len(ord.Values))
	for i := range redeemScripts {
		redeemScripts[i] = []dex.Bytes{nil}
	}

	success = true
	return allCoins, redeemScripts, 0, nil
}

// gasEstimates are estimates of gas required for operations involving a swap
// combination of (swap asset, redeem asset, # lots).
type gasEstimate struct {
	// The embedded are best calculated values for Swap gases, and redeem costs
	// IF the redeem asset is a fee-family asset. Otherwise Redeem and RedeemAdd
	// are zero.
	dexeth.Gases
	// Additional fields may be based on live estimates of the swap. Both oneGas
	// and nGas will include both swap and redeem gas, but note that the redeem
	// gas may be zero if the redeemed asset is not ETH or an ETH token.
	oneGas, nGas, nSwap, nRedeem uint64
}

// initGasEstimate gets the best available gas estimate for n initiations. A
// live estimate is checked against the server's configured values and our own
// known values and errors or logs generated in certain cases.
func (w *assetWallet) initGasEstimate(n int, initVer, redeemVer, redeemAssetID uint32) (est *gasEstimate, err error) {
	est = new(gasEstimate)

	// Get the refund gas.
	if g := w.gases(initVer); g == nil {
		return nil, fmt.Errorf("no gas table")
	} else { // scoping g
		est.Refund = g.Refund
	}

	est.Swap, est.nSwap, err = w.swapGas(n, initVer)
	if err != nil {
		return nil, fmt.Errorf("error calculating swap gas: %w", err)
	}

	est.oneGas = est.Swap
	est.nGas = est.nSwap

	if redeemW := w.wallet(redeemAssetID); redeemW != nil {
		est.Redeem, est.nRedeem, err = redeemW.redeemGas(n, redeemVer)
		if err != nil {
			return nil, fmt.Errorf("error calculating fee-family redeem gas: %w", err)
		}
		est.oneGas += est.Redeem
		est.nGas += est.nRedeem
	}

	return
}

// swapGas estimates gas for a number of initiations. swapGas will error if we
// cannot get a live estimate from the contractor, which will happen if the
// wallet has no balance. A live gas estimate will always be attempted, and used
// if our expected gas values are lower (anomalous).
func (w *assetWallet) swapGas(n int, ver uint32) (oneSwap, nSwap uint64, err error) {
	g := w.gases(ver)
	if g == nil {
		return 0, 0, fmt.Errorf("no gases known for %d version %d", w.assetID, ver)
	}
	oneSwap = g.Swap

	// We have no way of updating the value of SwapAdd without a version change,
	// but we're not gonna worry about accuracy for nSwap, since it's only used
	// for estimates and never for dex-validated values like order funding.
	nSwap = oneSwap + uint64(n-1)*g.SwapAdd

	// The amount we can estimate and ultimately the amount we can use in a
	// single transaction is limited by the block gas limit or the tx gas
	// limit. Core will use the largest gas among all versions when
	// determining the maximum number of swaps that can be in one
	// transaction. Limit our gas estimate to the same number of swaps.
	nMax := n
	maxSwaps, _ := w.maxSwapsAndRedeems()
	var nRemain, nFull int
	if uint64(n) > maxSwaps {
		nMax = int(maxSwaps)
		nFull = n / nMax
		nSwap = (oneSwap + uint64(nMax-1)*g.SwapAdd) * uint64(nFull)
		nRemain = n % nMax
		if nRemain != 0 {
			nSwap += oneSwap + uint64(nRemain-1)*g.SwapAdd
		}
	}

	// If a live estimate is greater than our estimate from configured values,
	// use the live estimate with a warning.
	gasEst, err := w.estimateInitGas(w.ctx, nMax, ver)
	if err != nil {
		return 0, 0, err
		// Or we could go with what we know? But this estimate error could be a
		// hint that the transaction would fail, and we don't have a way to
		// recover from that. Play it safe and allow caller to retry assuming
		// the error is transient with the provider.
		// w.log.Errorf("(%d) error estimating swap gas (using expected gas cap instead): %v", w.assetID, err)
		// return oneSwap, nSwap, true, nil
	}
	if nMax != n {
		// If we needed to adjust the max earlier, and the estimate did
		// not error, multiply the estimate by the number of full
		// transactions and add the estimate of the remainder.
		gasEst *= uint64(nFull)
		if nRemain > 0 {
			remainEst, err := w.estimateInitGas(w.ctx, nRemain, ver)
			if err != nil {
				w.log.Errorf("(%d) error estimating swap gas for remainder: %v", w.assetID, err)
				return 0, 0, err
			}
			gasEst += remainEst
		}
	}
	if gasEst > nSwap {
		w.log.Warnf("Swap gas estimate %d is greater than the server's configured value %d. Using live estimate + 10%%.", gasEst, nSwap)
		nSwap = gasEst * 11 / 10 // 10% buffer
		if n == 1 && nSwap > oneSwap {
			oneSwap = nSwap
		}
	}
	return
}

// redeemGas gets an accurate estimate for redemption gas. We allow a DEX server
// some latitude in adjusting the redemption gas, up to 2x our local estimate.
func (w *assetWallet) redeemGas(n int, ver uint32) (oneGas, nGas uint64, err error) {
	g := w.gases(ver)
	if g == nil {
		return 0, 0, fmt.Errorf("no gas table for redemption asset %d", w.assetID)
	}
	redeemGas := g.Redeem
	// Not concerned with the accuracy of nGas. It's never used outside of
	// best case estimates.
	return redeemGas, redeemGas + (uint64(n)-1)*g.RedeemAdd, nil
}

// approvalGas gets the best available estimate for an approval tx, which is
// the greater of the asset's registered value and a live estimate. It is an
// error if a live estimate cannot be retrieved, which will be the case if the
// user's eth balance is insufficient to cover tx fees for the approval.
func (w *assetWallet) approvalGas(newGas *big.Int, ver uint32) (uint64, error) {
	ourGas := w.gases(ver)
	if ourGas == nil {
		return 0, fmt.Errorf("no gases known for %d version %d", w.assetID, ver)
	}

	approveGas := ourGas.Approve

	if approveEst, err := w.estimateApproveGas(newGas); err != nil {
		return 0, fmt.Errorf("error estimating approve gas: %v", err)
	} else if approveEst > approveGas {
		w.log.Warnf("Approve gas estimate %d is greater than the expected value %d. Using live estimate + 10%%.", approveEst, approveGas)
		return approveEst * 11 / 10, nil
	}
	return approveGas, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (w *ETHWallet) ReturnCoins(coins asset.Coins) error {
	var amt uint64
	for _, ci := range coins {
		c, is := ci.(*fundingCoin)
		if !is {
			return fmt.Errorf("unknown coin type %T", c)
		}
		if c.addr != w.addr {
			return fmt.Errorf("coin is not funded by this wallet. coin address %s != our address %s", c.addr, w.addr)
		}
		amt += c.amt

	}
	w.unlockFunds(amt, initiationReserve)
	return nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (w *TokenWallet) ReturnCoins(coins asset.Coins) error {
	var amt, fees uint64
	for _, ci := range coins {
		c, is := ci.(*tokenFundingCoin)
		if !is {
			return fmt.Errorf("unknown coin type %T", c)
		}
		if c.addr != w.addr {
			return fmt.Errorf("coin is not funded by this wallet. coin address %s != our address %s", c.addr, w.addr)
		}
		amt += c.amt
		fees += c.fees
	}
	if fees > 0 {
		w.parent.unlockFunds(fees, initiationReserve)
	}
	w.unlockFunds(amt, initiationReserve)
	return nil
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (w *ETHWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	var amt uint64
	for _, id := range ids {
		c, err := decodeFundingCoin(id)
		if err != nil {
			return nil, fmt.Errorf("error decoding funding coin ID: %w", err)
		}
		if c.addr != w.addr {
			return nil, fmt.Errorf("funding coin has wrong address. %s != %s", c.addr, w.addr)
		}
		amt += c.amt
		coins = append(coins, c)
	}
	if err := w.lockFunds(amt, initiationReserve); err != nil {
		return nil, err
	}

	return coins, nil
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (w *TokenWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	var amt, fees uint64
	for _, id := range ids {
		c, err := decodeTokenFundingCoin(id)
		if err != nil {
			return nil, fmt.Errorf("error decoding funding coin ID: %w", err)
		}
		if c.addr != w.addr {
			return nil, fmt.Errorf("funding coin has wrong address. %s != %s", c.addr, w.addr)
		}

		amt += c.amt
		fees += c.fees
		coins = append(coins, c)
	}

	var success bool
	if fees > 0 {
		if err := w.parent.lockFunds(fees, initiationReserve); err != nil {
			return nil, fmt.Errorf("error unlocking parent asset fees: %w", err)
		}
		defer func() {
			if !success {
				w.parent.unlockFunds(fees, initiationReserve)
			}
		}()
	}
	if err := w.lockFunds(amt, initiationReserve); err != nil {
		return nil, err
	}

	success = true
	return coins, nil
}

// swapReceipt implements the asset.Receipt interface for ETH.
type swapReceipt struct {
	txHash     common.Hash
	secretHash [dexeth.SecretHashSize]byte
	// expiration and value can be determined with a blockchain
	// lookup, but we cache these values to avoid this.
	expiration   time.Time
	value        uint64
	ver          uint32
	contractAddr string // specified by ver, here for naive consumers
}

// Expiration returns the time after which the contract can be
// refunded.
func (r *swapReceipt) Expiration() time.Time {
	return r.expiration
}

// Coin returns the coin used to fund the swap.
func (r *swapReceipt) Coin() asset.Coin {
	return &coin{
		value: r.value,
		id:    r.txHash, // server's idea of ETH coin ID encoding
	}
}

// Contract returns the swap's identifying data, which the concatenation of the
// contract version and the secret hash.
func (r *swapReceipt) Contract() dex.Bytes {
	return dexeth.EncodeContractData(r.ver, r.secretHash)
}

// String returns a string representation of the swapReceipt. The secret hash
// and contract address should be included in this to facilitate a manual refund
// since the secret hash identifies the swap in the contract (for v0). Although
// the user can pick this information from the transaction's "to" address and
// the calldata, this simplifies the process.
func (r *swapReceipt) String() string {
	return fmt.Sprintf("{ tx hash: %s, contract address: %s, secret hash: %x }",
		r.txHash, r.contractAddr, r.secretHash)
}

// SignedRefund returns an empty byte array. ETH does not support a pre-signed
// redeem script because the nonce needed in the transaction cannot be previously
// determined.
func (*swapReceipt) SignedRefund() dex.Bytes {
	return dex.Bytes{}
}

var _ asset.Receipt = (*swapReceipt)(nil)

// Swap sends the swaps in a single transaction. The fees used returned are the
// max fees that will possibly be used, since in ethereum with EIP-1559 we cannot
// know exactly how much fees will be used.
func (w *ETHWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	if swaps.FeeRate == 0 {
		return nil, nil, 0, fmt.Errorf("cannot send swap with with zero fee rate")
	}

	fail := func(s string, a ...any) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	var reservedVal uint64
	for _, input := range swaps.Inputs { // Should only ever be 1 input, I think.
		c, is := input.(*fundingCoin)
		if !is {
			return fail("wrong coin type: %T", input)
		}
		reservedVal += c.amt
	}

	var swapVal uint64
	for _, contract := range swaps.Contracts {
		swapVal += contract.Value
	}

	// Set the gas limit as high as reserves will allow.
	n := len(swaps.Contracts)
	oneSwap, nSwap, err := w.swapGas(n, swaps.Version)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}
	gasLimit := oneSwap * uint64(n) // naive unbatched, higher but not realistic
	fees := gasLimit * swaps.FeeRate
	if swapVal+fees > reservedVal {
		if n == 1 {
			return fail("unfunded swap: %d < %d", reservedVal, swapVal+fees)
		}
		w.log.Warnf("Unexpectedly low reserves for %d swaps: %d < %d", n, reservedVal, swapVal+fees)
		// Since this is a batch swap, attempt to use the realistic limits.
		gasLimit = nSwap
		fees = gasLimit * swaps.FeeRate
		if swapVal+fees > reservedVal {
			// If the live gas estimate is giving us an unrealistically high
			// value, we're in trouble, so we might consider a third fallback
			// that only uses our known gases:
			//   g := w.gases(swaps.Version)
			//   nSwap = g.Swap + uint64(n-1)*g.SwapAdd
			// But we've not swapped yet and we don't want a failed transaction,
			// so we will do nothing.
			return fail("unfunded swap: %d < %d", reservedVal, swapVal+fees)
		}
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, gasLimit, swaps.Version)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	txHash := tx.Hash()
	w.addToTxHistory(tx.Nonce(), swapVal, swaps.FeeRate*gasLimit, w.assetID, txHash, asset.Swap, nil)

	receipts := make([]asset.Receipt, 0, n)
	for _, swap := range swaps.Contracts {
		var secretHash [dexeth.SecretHashSize]byte
		copy(secretHash[:], swap.SecretHash)
		receipts = append(receipts, &swapReceipt{
			expiration:   time.Unix(int64(swap.LockTime), 0),
			value:        swap.Value,
			txHash:       txHash,
			secretHash:   secretHash,
			ver:          swaps.Version,
			contractAddr: w.versionedContracts[swaps.Version].String(),
		})
	}

	var change asset.Coin
	if swaps.LockChange {
		w.unlockFunds(swapVal+fees, initiationReserve)
		change = w.createFundingCoin(reservedVal - swapVal - fees)
	} else {
		w.unlockFunds(reservedVal, initiationReserve)
	}

	return receipts, change, fees, nil
}

// Swap sends the swaps in a single transaction. The fees used returned are the
// max fees that will possibly be used, since in ethereum with EIP-1559 we cannot
// know exactly how much fees will be used.
func (w *TokenWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	if swaps.FeeRate == 0 {
		return nil, nil, 0, fmt.Errorf("cannot send swap with with zero fee rate")
	}

	fail := func(s string, a ...any) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	var reservedVal, reservedParent uint64
	for _, input := range swaps.Inputs { // Should only ever be 1 input, I think.
		c, is := input.(*tokenFundingCoin)
		if !is {
			return fail("wrong coin type: %T", input)
		}
		reservedVal += c.amt
		reservedParent += c.fees
	}

	var swapVal uint64
	for _, contract := range swaps.Contracts {
		swapVal += contract.Value
	}

	if swapVal > reservedVal {
		return fail("unfunded token swap: %d < %d", reservedVal, swapVal)
	}

	n := len(swaps.Contracts)
	oneSwap, nSwap, err := w.swapGas(n, swaps.Version)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}

	gasLimit := oneSwap * uint64(n)
	fees := gasLimit * swaps.FeeRate
	if fees > reservedParent {
		if n == 1 {
			return fail("unfunded token swap fees: %d < %d", reservedParent, fees)
		}
		// Since this is a batch swap, attempt to use the realistic limits.
		w.log.Warnf("Unexpectedly low reserves for %d swaps: %d < %d", n, reservedVal, swapVal+fees)
		gasLimit = nSwap
		fees = gasLimit * swaps.FeeRate
		if fees > reservedParent {
			return fail("unfunded token swap fees: %d < %d", reservedParent, fees)
		} // See (*ETHWallet).Swap comments for a third option.
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, gasLimit, swaps.Version)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	if w.netToken.SwapContracts[swaps.Version] == nil {
		return fail("unable to find contract address for asset %d contract version %d", w.assetID, swaps.Version)
	}

	contractAddr := w.netToken.SwapContracts[swaps.Version].Address.String()

	txHash := tx.Hash()
	w.addToTxHistory(tx.Nonce(), swapVal, swaps.FeeRate*gasLimit, w.assetID, txHash, asset.Swap, nil)

	receipts := make([]asset.Receipt, 0, n)
	for _, swap := range swaps.Contracts {
		var secretHash [dexeth.SecretHashSize]byte
		copy(secretHash[:], swap.SecretHash)
		receipts = append(receipts, &swapReceipt{
			expiration:   time.Unix(int64(swap.LockTime), 0),
			value:        swap.Value,
			txHash:       txHash,
			secretHash:   secretHash,
			ver:          swaps.Version,
			contractAddr: contractAddr,
		})
	}

	var change asset.Coin
	if swaps.LockChange {
		w.unlockFunds(swapVal, initiationReserve)
		w.parent.unlockFunds(fees, initiationReserve)
		change = w.createTokenFundingCoin(reservedVal-swapVal, reservedParent-fees)
	} else {
		w.unlockFunds(reservedVal, initiationReserve)
		w.parent.unlockFunds(reservedParent, initiationReserve)
	}

	return receipts, change, fees, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. All redemptions must be for the same contract version because the
// current API requires a single transaction reported (asset.Coin output), but
// conceptually a batch of redeems could be processed for any number of
// different contract addresses with multiple transactions. (buck: what would
// the difference from calling Redeem repeatedly?)
func (w *ETHWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return w.assetWallet.Redeem(form, nil, nil)
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (w *TokenWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return w.assetWallet.Redeem(form, w.parent, nil)
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. The nonceOverride paramater is used to specify a specific nonce
// to be used for the redemption transaction. It is needed when resubmitting
// a redemption with a fee too low to be mined.
func (w *assetWallet) Redeem(form *asset.RedeemForm, feeWallet *assetWallet, nonceOverride *uint64) ([]dex.Bytes, asset.Coin, uint64, error) {
	fail := func(err error) ([]dex.Bytes, asset.Coin, uint64, error) {
		return nil, nil, 0, err
	}

	n := uint64(len(form.Redemptions))

	if n == 0 {
		return fail(errors.New("Redeem: must be called with at least 1 redemption"))
	}

	var contractVer uint32 // require a consistent version since this is a single transaction
	secrets := make([][32]byte, 0, n)
	var redeemedValue uint64
	for i, redemption := range form.Redemptions {
		// NOTE: redemption.Spends.SecretHash is a dup of the hash extracted
		// from redemption.Spends.Contract. Even for scriptable UTXO assets, the
		// redeem script in this Contract field is redundant with the SecretHash
		// field as ExtractSwapDetails can be applied to extract the hash.
		ver, secretHash, err := dexeth.DecodeContractData(redemption.Spends.Contract)
		if err != nil {
			return fail(fmt.Errorf("Redeem: invalid versioned swap contract data: %w", err))
		}
		if i == 0 {
			contractVer = ver
		} else if contractVer != ver {
			return fail(fmt.Errorf("Redeem: inconsistent contract versions in RedeemForm.Redemptions: "+
				"%d != %d", contractVer, ver))
		}

		// Use the contract's free public view function to validate the secret
		// against the secret hash, and ensure the swap is otherwise redeemable
		// before broadcasting our secrets, which is especially important if we
		// are maker (the swap initiator).
		var secret [32]byte
		copy(secret[:], redemption.Secret)
		secrets = append(secrets, secret)
		redeemable, err := w.isRedeemable(secretHash, secret, ver)
		if err != nil {
			return fail(fmt.Errorf("Redeem: failed to check if swap is redeemable: %w", err))
		}
		if !redeemable {
			return fail(fmt.Errorf("Redeem: secretHash %x not redeemable with secret %x",
				secretHash, secret))
		}

		swapData, err := w.swap(w.ctx, secretHash, ver)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error finding swap state: %w", err)
		}
		redeemedValue += w.atomize(swapData.Value)
	}

	g := w.gases(contractVer)
	if g == nil {
		return fail(fmt.Errorf("no gas table"))
	}

	if feeWallet == nil {
		feeWallet = w
	}
	bal, err := feeWallet.Balance()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting balance in excessive gas fee recovery: %v", err)
	}

	gasLimit, gasFeeCap := g.Redeem*n, form.FeeSuggestion
	originalFundsReserved := gasLimit * gasFeeCap

	/* We could get a gas estimate via RPC, but this will reveal the secret key
	   before submitting the redeem transaction. This is not OK for maker.
	   Disable for now.

	if gasEst, err := w.estimateRedeemGas(w.ctx, secrets, contractVer); err != nil {
		return fail(fmt.Errorf("error getting redemption estimate: %w", err))
	} else if gasEst > gasLimit {
		// This is sticky. We only reserved so much for redemption, so accepting
		// a gas limit higher than anticipated could potentially mess us up. On
		// the other hand, we don't want to simply reject the redemption.
		// Let's see if it looks like we can cover the fee. If so go ahead, up
		// to a limit.
		candidateLimit := gasEst * 11 / 10 // Add 10% for good measure.
		// Cannot be more than double.
		if candidateLimit > gasLimit*2 {
			return fail(fmt.Errorf("cannot recover from excessive gas estimate %d > 2 * %d", candidateLimit, gasLimit))
		}
		additionalFundsNeeded := (candidateLimit - gasLimit) * form.FeeSuggestion
		if bal.Available < additionalFundsNeeded {
			return fail(fmt.Errorf("no balance available for gas overshoot recovery. %d < %d", bal.Available, additionalFundsNeeded))
		}
		w.log.Warnf("live gas estimate %d exceeded expected max value %d. using higher limit %d for redemption", gasEst, gasLimit, candidateLimit)
		gasLimit = candidateLimit
	}
	*/

	// If the base fee is higher than the FeeSuggestion we attempt to increase
	// the gasFeeCap to 2*baseFee. If we don't have enough funds, we use the
	// funds we have available.
	baseFee, _, err := w.node.currentFees(w.ctx)
	if err != nil {
		return fail(fmt.Errorf("Error getting net fee state: %w", err))
	}
	baseFeeGwei := dexeth.WeiToGwei(baseFee)
	if baseFeeGwei > form.FeeSuggestion {
		additionalFundsNeeded := (2 * baseFeeGwei * gasLimit) - originalFundsReserved
		if bal.Available > additionalFundsNeeded {
			gasFeeCap = 2 * baseFeeGwei
		} else {
			gasFeeCap = (bal.Available + originalFundsReserved) / gasLimit
		}
		w.log.Warnf("base fee %d > server max fee rate %d. using %d as gas fee cap for redemption", baseFeeGwei, form.FeeSuggestion, gasFeeCap)
	}

	hdr, err := w.node.bestHeader(w.ctx)
	if err != nil {
		return fail(fmt.Errorf("error fetching best header: %w", err))
	}

	tx, err := w.redeem(w.ctx, w.assetID, form.Redemptions, gasFeeCap, gasLimit, contractVer, nonceOverride)
	if err != nil {
		return fail(fmt.Errorf("Redeem: redeem error: %w", err))
	}

	txHash := tx.Hash()
	w.addToTxHistory(tx.Nonce(), redeemedValue, gasFeeCap*gasLimit, w.assetID, txHash, asset.Redeem, nil)

	txs := make([]dex.Bytes, len(form.Redemptions))
	for i := range txs {
		txs[i] = txHash[:]
	}

	w.monitorTx(tx, hdr.Number.Uint64())

	outputCoin := &coin{
		id:    txHash,
		value: redeemedValue,
	}

	// This is still a fee estimate. If we add a redemption confirmation method
	// as has been discussed, then maybe the fees can be updated there.
	fees := g.RedeemN(len(form.Redemptions)) * form.FeeSuggestion

	return txs, outputCoin, fees, nil
}

// recoverPubkey recovers the uncompressed public key from the signature and the
// message hash that was signed. The signature should be a compact signature
// generated by a geth wallet, with the format [R || S || V], with the recover
// bit V at the end. See go-ethereum/crypto.Sign.
func recoverPubkey(msgHash, sig []byte) ([]byte, error) {
	// Using Decred's ecdsa.RecoverCompact requires moving the recovery byte to
	// the beginning of the serialized compact signature and adding back in the
	// compactSigMagicOffset scalar.
	sigBTC := make([]byte, 65)
	sigBTC[0] = sig[64] + 27 // compactSigMagicOffset
	copy(sigBTC[1:], sig)
	pubKey, _, err := ecdsa.RecoverCompact(sigBTC, msgHash)
	if err != nil {
		return nil, err
	}
	return pubKey.SerializeUncompressed(), nil
}

// tokenBalance checks the token balance of the account handled by the wallet.
func (w *assetWallet) tokenBalance() (bal *big.Int, err error) {
	// We don't care about the version.
	return bal, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		bal, err = c.balance(w.ctx)
		return err
	})
}

// tokenAllowance checks the amount of tokens that the swap contract is approved
// to spend on behalf of the account handled by the wallet.
func (w *assetWallet) tokenAllowance(version uint32) (allowance *big.Int, err error) {
	return allowance, w.withTokenContractor(w.assetID, version, func(c tokenContractor) error {
		allowance, err = c.allowance(w.ctx)
		return err
	})
}

// approveToken approves the token swap contract to spend tokens on behalf of
// account handled by the wallet.
func (w *assetWallet) approveToken(amount *big.Int, maxFeeRate, gasLimit uint64, contractVer uint32) (tx *types.Transaction, err error) {
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(w.ctx, 0, gasLimit, dexeth.GweiToWei(maxFeeRate), nil)
	if err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return tx, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
		tx, err = c.approve(txOpts, amount)
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		w.log.Infof("Approval sent for %s at token address %s, nonce = %s, txID = %s",
			dex.BipIDSymbol(w.assetID), c.tokenAddress(), txOpts.Nonce, tx.Hash().Hex())

		txHash := tx.Hash()
		txType := asset.ApproveToken
		if amount.Cmp(big.NewInt(0)) == 0 {
			txType = asset.RevokeTokenApproval
		}
		w.addToTxHistory(tx.Nonce(), 0, maxFeeRate*gasLimit, w.assetID, txHash, txType, nil)

		return nil
	})
}

func (w *assetWallet) approvalStatus(version uint32) (asset.ApprovalStatus, error) {
	if w.assetID == w.baseChainID {
		return asset.Approved, nil
	}

	// If the result has been cached, return what is in the cache.
	// The cache is cleared if an approval/unapproval tx is done.
	w.approvalsMtx.RLock()
	if approved, cached := w.approvalCache[version]; cached {
		w.approvalsMtx.RUnlock()
		if approved {
			return asset.Approved, nil
		} else {
			return asset.NotApproved, nil
		}
	}

	if _, pending := w.pendingApprovals[version]; pending {
		w.approvalsMtx.RUnlock()
		return asset.Pending, nil
	}
	w.approvalsMtx.RUnlock()

	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	currentAllowance, err := w.tokenAllowance(version)
	if err != nil {
		return asset.NotApproved, fmt.Errorf("error retrieving current allowance: %w", err)
	}
	if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) >= 0 {
		w.approvalCache[version] = true
		return asset.Approved, nil
	}
	w.approvalCache[version] = false
	return asset.NotApproved, nil
}

// ApproveToken sends an approval transaction for a specific version of
// the token's swap contract. An error is returned if an approval has
// already been done or is pending. The onConfirm callback is called
// when the approval transaction is confirmed.
func (w *TokenWallet) ApproveToken(assetVer uint32, onConfirm func()) (string, error) {
	approvalStatus, err := w.approvalStatus(assetVer)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus == asset.Approved {
		return "", fmt.Errorf("token is already approved")
	}
	if approvalStatus == asset.Pending {
		return "", fmt.Errorf("approval is already pending")
	}

	feeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGwei(feeRate)
	approvalGas, err := w.approvalGas(unlimitedAllowance, assetVer)
	if err != nil {
		return "", fmt.Errorf("error calculating approval gas: %w", err)
	}

	ethBal, err := w.parent.balance()
	if err != nil {
		return "", fmt.Errorf("error getting eth balance: %w", err)
	}
	if ethBal.Available < approvalGas*feeRateGwei {
		return "", fmt.Errorf("insufficient fee balance for approval. required: %d, available: %d",
			approvalGas*feeRateGwei, ethBal.Available)
	}

	tx, err := w.approveToken(unlimitedAllowance, feeRateGwei, approvalGas, assetVer)
	if err != nil {
		return "", fmt.Errorf("error approving token: %w", err)
	}

	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	delete(w.approvalCache, assetVer)
	w.pendingApprovals[assetVer] = &pendingApproval{
		txHash:    tx.Hash(),
		onConfirm: onConfirm,
	}

	return tx.Hash().Hex(), nil
}

// UnapproveToken removes the approval for a specific version of the token's
// swap contract.
func (w *TokenWallet) UnapproveToken(assetVer uint32, onConfirm func()) (string, error) {
	approvalStatus, err := w.approvalStatus(assetVer)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus == asset.NotApproved {
		return "", fmt.Errorf("token is not approved")
	}
	if approvalStatus == asset.Pending {
		return "", fmt.Errorf("approval is pending")
	}

	feeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGwei(feeRate)
	approvalGas, err := w.approvalGas(big.NewInt(0), assetVer)
	if err != nil {
		return "", fmt.Errorf("error calculating approval gas: %w", err)
	}

	ethBal, err := w.parent.balance()
	if err != nil {
		return "", fmt.Errorf("error getting eth balance: %w", err)
	}
	if ethBal.Available < approvalGas*feeRateGwei {
		return "", fmt.Errorf("insufficient eth balance for unapproval. required: %d, available: %d",
			approvalGas*feeRateGwei, ethBal.Available)
	}

	tx, err := w.approveToken(big.NewInt(0), feeRateGwei, approvalGas, assetVer)
	if err != nil {
		return "", fmt.Errorf("error unapproving token: %w", err)
	}

	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	delete(w.approvalCache, assetVer)
	w.pendingApprovals[assetVer] = &pendingApproval{
		txHash:    tx.Hash(),
		onConfirm: onConfirm,
	}

	return tx.Hash().Hex(), nil
}

// ApprovalFee returns the estimated fee for an approval transaction.
func (w *TokenWallet) ApprovalFee(assetVer uint32, approve bool) (uint64, error) {
	var allowance *big.Int
	if approve {
		allowance = unlimitedAllowance
	} else {
		allowance = big.NewInt(0)
	}
	approvalGas, err := w.approvalGas(allowance, assetVer)
	if err != nil {
		return 0, fmt.Errorf("error calculating approval gas: %w", err)
	}

	feeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, fmt.Errorf("error calculating approval fee rate: %w", err)
	}

	feeRateGwei := dexeth.WeiToGwei(feeRate)

	return approvalGas * feeRateGwei, nil
}

// ApprovalStatus returns the approval status for each version of the
// token's swap contract.
func (w *TokenWallet) ApprovalStatus() map[uint32]asset.ApprovalStatus {
	versions := w.Info().SupportedVersions

	statuses := map[uint32]asset.ApprovalStatus{}
	for _, version := range versions {
		status, err := w.approvalStatus(version)
		if err != nil {
			w.log.Errorf("error checking approval status for version %d: %w", version, err)
			continue
		}
		statuses[version] = status
	}

	return statuses
}

// ReserveNRedemptions locks funds for redemption. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *ETHWallet) ReserveNRedemptions(n uint64, ver uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(ver)
	if g == nil {
		return 0, fmt.Errorf("no gas table")
	}
	redeemCost := g.Redeem * maxFeeRate
	reserve := redeemCost * n

	if err := w.lockFunds(reserve, redemptionReserve); err != nil {
		return 0, err
	}

	return reserve, nil
}

// ReserveNRedemptions locks funds for redemption. It is an error if there
// is insufficient spendable balance.
// Part of the AccountLocker interface.
func (w *TokenWallet) ReserveNRedemptions(n uint64, ver uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(ver)
	if g == nil {
		return 0, fmt.Errorf("no gas table")
	}
	reserve := g.Redeem * maxFeeRate * n

	if err := w.parent.lockFunds(reserve, redemptionReserve); err != nil {
		return 0, err
	}

	return reserve, nil
}

// UnlockRedemptionReserves unlocks the specified amount from redemption
// reserves. Part of the AccountLocker interface.
func (w *ETHWallet) UnlockRedemptionReserves(reserves uint64) {
	unlockRedemptionReserves(w.assetWallet, reserves)
}

// UnlockRedemptionReserves unlocks the specified amount from redemption
// reserves. Part of the AccountLocker interface.
func (w *TokenWallet) UnlockRedemptionReserves(reserves uint64) {
	unlockRedemptionReserves(w.parent, reserves)
}

func unlockRedemptionReserves(w *assetWallet, reserves uint64) {
	w.unlockFunds(reserves, redemptionReserve)
}

// ReReserveRedemption checks out an amount for redemptions. Use
// ReReserveRedemption after initializing a new asset.Wallet.
// Part of the AccountLocker interface.
func (w *ETHWallet) ReReserveRedemption(req uint64) error {
	return w.lockFunds(req, redemptionReserve)
}

// ReReserveRedemption checks out an amount for redemptions. Use
// ReReserveRedemption after initializing a new asset.Wallet.
// Part of the AccountLocker interface.
func (w *TokenWallet) ReReserveRedemption(req uint64) error {
	return w.parent.lockFunds(req, redemptionReserve)
}

// ReserveNRefunds locks funds for doing refunds. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *ETHWallet) ReserveNRefunds(n uint64, ver uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(ver)
	if g == nil {
		return 0, errors.New("no gas table")
	}
	return reserveNRefunds(w.assetWallet, n, maxFeeRate, g)
}

// ReserveNRefunds locks funds for doing refunds. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *TokenWallet) ReserveNRefunds(n uint64, ver uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(ver)
	if g == nil {
		return 0, errors.New("no gas table")
	}
	return reserveNRefunds(w.parent, n, maxFeeRate, g)
}

func reserveNRefunds(w *assetWallet, n, maxFeeRate uint64, g *dexeth.Gases) (uint64, error) {
	refundCost := g.Refund * maxFeeRate
	reserve := refundCost * n

	if err := w.lockFunds(reserve, refundReserve); err != nil {
		return 0, err
	}
	return reserve, nil
}

// UnlockRefundReserves unlocks the specified amount from refund
// reserves. Part of the AccountLocker interface.
func (w *ETHWallet) UnlockRefundReserves(reserves uint64) {
	unlockRefundReserves(w.assetWallet, reserves)
}

// UnlockRefundReserves unlocks the specified amount from refund
// reserves. Part of the AccountLocker interface.
func (w *TokenWallet) UnlockRefundReserves(reserves uint64) {
	unlockRefundReserves(w.parent, reserves)
}

func unlockRefundReserves(w *assetWallet, reserves uint64) {
	w.unlockFunds(reserves, refundReserve)
}

// ReReserveRefund checks out an amount for doing refunds. Use ReReserveRefund
// after initializing a new assetWallet. Part of the AccountLocker
// interface.
func (w *ETHWallet) ReReserveRefund(req uint64) error {
	return w.lockFunds(req, refundReserve)
}

// ReReserveRefund checks out an amount for doing refunds. Use ReReserveRefund
// after initializing a new assetWallet. Part of the AccountLocker
// interface.
func (w *TokenWallet) ReReserveRefund(req uint64) error {
	return w.parent.lockFunds(req, refundReserve)
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (eth *baseWallet) SignMessage(_ asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	sig, pubKey, err := eth.node.signData(msg)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error signing data: %w", err)
	}

	return []dex.Bytes{pubKey}, []dex.Bytes{sig}, nil
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap. coinID is expected to be the transaction id, and must
// be the same as the hash of serializedTx. contract is expected to be
// (contractVersion|secretHash) where the secretHash uniquely keys the swap.
func (w *assetWallet) AuditContract(coinID, contract, serializedTx dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	tx := new(types.Transaction)
	err := tx.UnmarshalBinary(serializedTx)
	if err != nil {
		return nil, fmt.Errorf("AuditContract: failed to unmarshal transaction: %w", err)
	}

	txHash := tx.Hash()
	if !bytes.Equal(coinID, txHash[:]) {
		return nil, fmt.Errorf("AuditContract: coin id != txHash - coin id: %x, txHash: %s", coinID, tx.Hash())
	}

	version, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("AuditContract: failed to decode contract data: %w", err)
	}

	initiations, err := dexeth.ParseInitiateData(tx.Data(), version)
	if err != nil {
		return nil, fmt.Errorf("AuditContract: failed to parse initiate data: %w", err)
	}

	initiation, ok := initiations[secretHash]
	if !ok {
		return nil, errors.New("AuditContract: tx does not initiate secret hash")
	}

	coin := &coin{
		id:    txHash,
		value: w.atomize(initiation.Value),
	}

	// The counter-party should have broadcasted the contract tx but rebroadcast
	// just in case to ensure that the tx is sent to the network. Do not block
	// because this is not required and does not affect the audit result.
	if rebroadcast {
		go func() {
			if err := w.node.sendSignedTransaction(w.ctx, tx); err != nil {
				w.log.Debugf("Rebroadcasting counterparty contract %v (THIS MAY BE NORMAL): %v", txHash, err)
			}
		}()
	}

	return &asset.AuditInfo{
		Recipient:  initiation.Participant.Hex(),
		Expiration: initiation.LockTime,
		Coin:       coin,
		Contract:   contract,
		SecretHash: secretHash[:],
	}, nil
}

// LockTimeExpired returns true if the specified locktime has expired, making it
// possible to redeem the locked coins.
func (w *assetWallet) LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error) {
	header, err := w.node.bestHeader(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to retrieve block header: %w", err)
	}
	blockTime := time.Unix(int64(header.Time), 0)
	return lockTime.Before(blockTime), nil
}

// ContractLockTimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (w *assetWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return false, time.Time{}, err
	}

	swap, err := w.swap(ctx, secretHash, contractVer)
	if err != nil {
		return false, time.Time{}, err
	}

	// Time is not yet set for uninitiated swaps.
	if swap.State == dexeth.SSNone {
		return false, time.Time{}, asset.ErrSwapNotInitiated
	}

	expired, err := w.LockTimeExpired(ctx, swap.LockTime)
	if err != nil {
		return false, time.Time{}, err
	}
	return expired, swap.LockTime, nil
}

// findRedemptionResult is used internally for queued findRedemptionRequests.
type findRedemptionResult struct {
	err       error
	secret    []byte
	makerAddr string
}

// findRedemptionRequest is a request that is waiting on a redemption result.
type findRedemptionRequest struct {
	contractVer uint32
	res         chan *findRedemptionResult
}

// sendFindRedemptionResult sends the result or logs a message if it cannot be
// sent.
func (eth *baseWallet) sendFindRedemptionResult(req *findRedemptionRequest, secretHash [32]byte,
	secret []byte, makerAddr string, err error) {
	select {
	case req.res <- &findRedemptionResult{secret: secret, makerAddr: makerAddr, err: err}:
	default:
		eth.log.Info("findRedemptionResult channel blocking for request %s", secretHash)
	}
}

// findRedemptionRequests creates a copy of the findRedemptionReqs map.
func (w *assetWallet) findRedemptionRequests() map[[32]byte]*findRedemptionRequest {
	w.findRedemptionMtx.RLock()
	defer w.findRedemptionMtx.RUnlock()
	reqs := make(map[[32]byte]*findRedemptionRequest, len(w.findRedemptionReqs))
	for secretHash, req := range w.findRedemptionReqs {
		reqs[secretHash] = req
	}
	return reqs
}

// FindRedemption checks the contract for a redemption. If the swap is initiated
// but un-redeemed and un-refunded, FindRedemption will block until a redemption
// is seen.
func (w *assetWallet) FindRedemption(ctx context.Context, _, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	// coinIDTmpl is a template for constructing Coin ID when Taker
	// (aka participant) finds redemption himself. %s represents Maker Ethereum
	// account address so that user, as Taker, could manually look it up in case
	// he needs it. Ideally we'd want to have transaction ID there instead of
	// account address, but that's currently impossible to get in Ethereum smart
	// contract, so we are basically doing the next best thing here.
	const coinIDTmpl = coinIDTakerFoundMakerRedemption + "%s"

	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, nil, err
	}

	// See if it's ready right away.
	secret, makerAddr, err := w.findSecret(secretHash, contractVer)
	if err != nil {
		return nil, nil, err
	}

	if len(secret) > 0 {
		return dex.Bytes(fmt.Sprintf(coinIDTmpl, makerAddr)), secret, nil
	}

	// Not ready. Queue the request.
	req := &findRedemptionRequest{
		contractVer: contractVer,
		res:         make(chan *findRedemptionResult, 1),
	}

	w.findRedemptionMtx.Lock()

	if w.findRedemptionReqs[secretHash] != nil {
		w.findRedemptionMtx.Unlock()
		return nil, nil, fmt.Errorf("duplicate find redemption request for %x", secretHash)
	}

	w.findRedemptionReqs[secretHash] = req

	w.findRedemptionMtx.Unlock()

	var res *findRedemptionResult
	select {
	case res = <-req.res:
	case <-ctx.Done():
	}

	w.findRedemptionMtx.Lock()
	delete(w.findRedemptionReqs, secretHash)
	w.findRedemptionMtx.Unlock()

	if res == nil {
		return nil, nil, fmt.Errorf("context cancelled for find redemption request %x", secretHash)
	}

	if res.err != nil {
		return nil, nil, res.err
	}

	return dex.Bytes(fmt.Sprintf(coinIDTmpl, res.makerAddr)), res.secret[:], nil
}

// findSecret returns redemption secret from smart contract that Maker put there
// redeeming Taker swap along with Maker Ethereum account address. Returns empty
// values if Maker hasn't redeemed yet.
func (w *assetWallet) findSecret(secretHash [32]byte, contractVer uint32) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	swap, err := w.swap(ctx, secretHash, contractVer)
	if err != nil {
		return nil, "", err
	}

	switch swap.State {
	case dexeth.SSInitiated:
		return nil, "", nil // no Maker redeem yet, but keep checking
	case dexeth.SSRedeemed:
		return swap.Secret[:], swap.Initiator.String(), nil
	case dexeth.SSNone:
		return nil, "", fmt.Errorf("swap %x does not exist", secretHash)
	case dexeth.SSRefunded:
		return nil, "", fmt.Errorf("swap %x is already refunded", secretHash)
	}
	return nil, "", fmt.Errorf("unrecognized swap state %v", swap.State)
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
func (w *assetWallet) Refund(_, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	version, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to decode contract: %w", err)
	}

	swap, err := w.swap(w.ctx, secretHash, version)
	if err != nil {
		return nil, err
	}
	// It's possible the swap was refunded by someone else. In that case we
	// cannot know the refunding tx hash.
	switch swap.State {
	case dexeth.SSInitiated: // good, check refundability
	case dexeth.SSNone:
		return nil, asset.ErrSwapNotInitiated
	case dexeth.SSRefunded:
		w.log.Infof("Swap with secret hash %x already refunded.", secretHash)
		zeroHash := common.Hash{}
		return zeroHash[:], nil
	case dexeth.SSRedeemed:
		w.log.Infof("Swap with secret hash %x already redeemed with secret key %x.",
			secretHash, swap.Secret)
		return nil, asset.CoinNotFoundError // so caller knows to FindRedemption
	}

	refundable, err := w.isRefundable(secretHash, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to check isRefundable: %w", err)
	}
	if !refundable {
		return nil, fmt.Errorf("Refund: swap with secret hash %x is not refundable", secretHash)
	}

	tx, fees, err := w.refund(secretHash, feeRate, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to call refund: %w", err)
	}

	txHash := tx.Hash()
	refundValue := dexeth.WeiToGwei(swap.Value)
	w.addToTxHistory(tx.Nonce(), refundValue, fees, w.assetID, txHash, asset.Refund, nil)

	return txHash[:], nil
}

// DepositAddress returns an address for the exchange wallet. This implementation
// is idempotent, always returning the same address for a given assetWallet.
func (eth *baseWallet) DepositAddress() (string, error) {
	return eth.addr.String(), nil
}

// RedemptionAddress gets an address for use in redeeming the counterparty's
// swap. This would be included in their swap initialization.
func (eth *baseWallet) RedemptionAddress() (string, error) {
	return eth.addr.String(), nil
}

// Unlock unlocks the exchange wallet.
func (eth *ETHWallet) Unlock(pw []byte) error {
	return eth.node.unlock(string(pw))
}

// Lock locks the exchange wallet.
func (eth *ETHWallet) Lock() error {
	return eth.node.lock()
}

// Locked will be true if the wallet is currently locked.
func (eth *ETHWallet) Locked() bool {
	return eth.node.locked()
}

// SendTransaction broadcasts a valid fully-signed transaction.
func (eth *baseWallet) SendTransaction(rawTx []byte) ([]byte, error) {
	tx := new(types.Transaction)
	err := tx.UnmarshalBinary(rawTx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}
	if err := eth.node.sendSignedTransaction(eth.ctx, tx); err != nil {
		return nil, err
	}
	return tx.Hash().Bytes(), nil
}

// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
// pay the registration fee using the provided feeRate.
func (w *ETHWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	return feeRate * defaultSendGasLimit
}

// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
// pay the registration fee using the provided feeRate.
func (w *TokenWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	g := w.gases(contractVersionNewest)
	if g == nil {
		w.log.Errorf("no gas table")
		return math.MaxUint64
	}
	return g.Transfer * feeRate
}

// ValidateAddress checks whether the provided address is a valid hex-encoded
// Ethereum address.
func (w *ETHWallet) ValidateAddress(address string) bool {
	return common.IsHexAddress(address)
}

// ValidateAddress checks whether the provided address is a valid hex-encoded
// Ethereum address.
func (w *TokenWallet) ValidateAddress(address string) bool {
	return common.IsHexAddress(address)
}

// isValidSend is a helper function for both token and ETH wallet. It returns an
// error if subtract is true, addr is invalid or value is zero.
func isValidSend(addr string, value uint64, subtract bool) error {
	if value == 0 {
		return fmt.Errorf("cannot send zero amount")
	}
	if subtract {
		return fmt.Errorf("wallet does not support subtracting network fee from send amount")
	}
	if !common.IsHexAddress(addr) {
		return fmt.Errorf("invalid hex address %q", addr)
	}
	return nil
}

// canSend ensures that the wallet has enough to cover send value and returns
// the fee rate and max fee required for the send tx. If isPreEstimate is false,
// wallet balance must be enough to cover total spend.
func (w *ETHWallet) canSend(value uint64, verifyBalance, isPreEstimate bool) (uint64, *big.Int, error) {
	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	maxFee := defaultSendGasLimit * dexeth.WeiToGwei(maxFeeRate)

	if isPreEstimate {
		maxFee = maxFee * 12 / 10 // 20% buffer
	}

	if verifyBalance {
		bal, err := w.Balance()
		if err != nil {
			return 0, nil, err
		}
		avail := bal.Available
		if avail < value {
			return 0, nil, fmt.Errorf("not enough funds to send: have %d gwei need %d gwei", avail, value)
		}

		if avail < value+maxFee {
			return 0, nil, fmt.Errorf("available funds %d gwei cannot cover value being sent: need %d gwei + %d gwei max fee", avail, value, maxFee)
		}
	}
	return maxFee, maxFeeRate, nil
}

// canSend ensures that the wallet has enough to cover send value and returns
// the fee rate and max fee required for the send tx.
func (w *TokenWallet) canSend(value uint64, verifyBalance, isPreEstimate bool) (uint64, *big.Int, error) {
	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	g := w.gases(contractVersionNewest)
	if g == nil {
		return 0, nil, fmt.Errorf("gas table not found")
	}

	maxFee := dexeth.WeiToGwei(maxFeeRate) * g.Transfer

	if isPreEstimate {
		maxFee = maxFee * 12 / 10 // 20% buffer
	}

	if verifyBalance {
		bal, err := w.Balance()
		if err != nil {
			return 0, nil, err
		}
		avail := bal.Available
		if avail < value {
			return 0, nil, fmt.Errorf("not enough tokens: have %[1]d %[3]s need %[2]d %[3]s", avail, value, w.ui.AtomicUnit)
		}

		ethBal, err := w.parent.Balance()
		if err != nil {
			return 0, nil, fmt.Errorf("error getting base chain balance: %w", err)
		}

		if ethBal.Available < maxFee {
			return 0, nil, fmt.Errorf("insufficient balance to cover token transfer fees. %d < %d",
				ethBal.Available, maxFee)
		}
	}
	return maxFee, maxFeeRate, nil
}

// EstimateSendTxFee returns a tx fee estimate for a send tx. The provided fee
// rate is ignored since all sends will use an internally derived fee rate. If
// an address is provided, it will ensure wallet has enough to cover total
// spend.
func (w *ETHWallet) EstimateSendTxFee(addr string, value, _ uint64, subtract bool) (uint64, bool, error) {
	if err := isValidSend(addr, value, subtract); err != nil && addr != "" { // fee estimate for a send tx.
		return 0, false, err
	}
	maxFee, _, err := w.canSend(value, addr != "", true)
	if err != nil {
		return 0, false, err
	}
	return maxFee, w.ValidateAddress(addr), nil
}

// EstimateSendTxFee returns a tx fee estimate for a send tx. The provided fee
// rate is ignored since all sends will use an internally derived fee rate. If
// an address is provided, it will ensure wallet has enough to cover total
// spend.
func (w *TokenWallet) EstimateSendTxFee(addr string, value, _ uint64, subtract bool) (fee uint64, isValidAddress bool, err error) {
	if err := isValidSend(addr, value, subtract); err != nil && addr != "" { // fee estimate for a send tx.
		return 0, false, err
	}
	maxFee, _, err := w.canSend(value, addr != "", true)
	if err != nil {
		return 0, false, err
	}
	return maxFee, w.ValidateAddress(addr), nil

}

// RestorationInfo returns information about how to restore the wallet in
// various external wallets.
func (w *ETHWallet) RestorationInfo(seed []byte) ([]*asset.WalletRestoration, error) {
	privateKey, zero, err := privKeyFromSeed(seed)
	if err != nil {
		return nil, err
	}
	defer zero()

	return []*asset.WalletRestoration{
		{
			Target:   "MetaMask",
			Seed:     hex.EncodeToString(privateKey),
			SeedName: "Private Key",
			Instructions: "Accounts can be imported by private key only if MetaMask has already be initialized. " +
				"If this is your first time installing MetaMask, create a new wallet and secret recovery phrase. " +
				"Then, to import your DEX account into MetaMask, follow the steps below:\n" +
				`1. Open the settings menu
				 2. Select "Import Account"
				 3. Make sure "Private Key" is selected, and enter the private key above into the box`,
		},
	}, nil
}

// SwapConfirmations gets the number of confirmations and the spend status
// for the specified swap.
func (w *assetWallet) SwapConfirmations(ctx context.Context, coinID dex.Bytes, contract dex.Bytes, _ time.Time) (confs uint32, spent bool, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return 0, false, err
	}

	ctx, cancel := context.WithTimeout(ctx, confCheckTimeout)
	defer cancel()

	hdr, err := w.node.bestHeader(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("error fetching best header: %w", err)
	}

	swapData, err := w.swap(w.ctx, secretHash, contractVer)
	if err != nil {
		return 0, false, fmt.Errorf("error finding swap state: %w", err)
	}

	if swapData.State == dexeth.SSNone {
		return 0, false, asset.ErrSwapNotInitiated
	}

	spent = swapData.State >= dexeth.SSRedeemed
	tip := hdr.Number.Uint64()
	// TODO: If tip < swapData.BlockHeight (which has been observed), what does
	// that mean? Are we using the wrong provider in a multi-provider setup? How
	// do we resolve provider relevance?
	if tip >= swapData.BlockHeight {
		confs = uint32(hdr.Number.Uint64() - swapData.BlockHeight + 1)
	}
	return
}

// Send sends the exact value to the specified address. The provided fee rate is
// ignored since all sends will use an internally derived fee rate.
func (w *ETHWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	maxFee, maxFeeRate, err := w.canSend(value, true, false)
	if err != nil {
		return nil, err
	}
	// TODO: Subtract option.
	// if avail < value+maxFee {
	// 	value -= maxFee
	// }

	tx, err := w.sendToAddr(common.HexToAddress(addr), value, maxFeeRate)
	if err != nil {
		return nil, err
	}

	txHash := tx.Hash()
	txType := asset.Send
	if strings.EqualFold(addr, w.addr.String()) {
		txType = asset.SelfSend
	}

	w.addToTxHistory(tx.Nonce(), value, maxFee, w.assetID, txHash, txType, &addr)

	return &coin{id: txHash, value: value}, nil
}

// Send sends the exact value to the specified address. Fees are taken from the
// parent wallet. The provided fee rate is ignored since all sends will use an
// internally derived fee rate.
func (w *TokenWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	maxFee, maxFeeRate, err := w.canSend(value, true, false)
	if err != nil {
		return nil, err
	}

	tx, err := w.sendToAddr(common.HexToAddress(addr), value, maxFeeRate)
	if err != nil {
		return nil, err
	}

	txHash := tx.Hash()
	w.addToTxHistory(tx.Nonce(), value, maxFee, w.assetID, txHash, asset.Send, &addr)

	return &coin{id: txHash, value: value}, nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (*baseWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// SyncStatus is information about the blockchain sync status.
//
// TODO: Since the merge, the sync status from a geth full node, namely the
// prog.CurrentBlock prog.HighestBlock, always seem to be the same number.
// Initial sync will always be zero. Later when restarting the node they move
// together but never indicate the highest known block on the chain. Further
// more, requesting the best block header starts to fail after a few tries
// during initial sync. Investigate how to get correct sync progress.
func (eth *baseWallet) SyncStatus() (bool, float32, error) {
	prog, tipTime, err := eth.node.syncProgress(eth.ctx)
	if err != nil {
		return false, 0, err
	}
	checkHeaderTime := func() bool {
		// Time in the header is in seconds.
		timeDiff := time.Now().Unix() - int64(tipTime)
		if timeDiff > dexeth.MaxBlockInterval && eth.net != dex.Simnet {
			eth.log.Infof("Time since block (%d sec) exceeds %d sec. "+
				"Assuming not in sync. Ensure your computer's system clock "+
				"is correct.", timeDiff, dexeth.MaxBlockInterval)
			return false
		}
		return true
	}
	if prog.HighestBlock != 0 {
		// HighestBlock was set. This means syncing started and is
		// finished if CurrentBlock is higher. CurrentBlock will
		// continue to go up even if we are not in a syncing state.
		// HighestBlock will not.
		if prog.CurrentBlock >= prog.HighestBlock {
			if fresh := checkHeaderTime(); !fresh {
				return false, 0, nil
			}
			return eth.node.peerCount() > 0, 1.0, nil
		}

		// We are certain we are syncing and can return progress.
		var ratio float32
		if prog.HighestBlock != 0 {
			ratio = float32(prog.CurrentBlock) / float32(prog.HighestBlock)
		}
		return false, ratio, nil
	}

	// HighestBlock is zero if syncing never happened or if it just hasn't
	// started. Syncing only happens if the light node gets a header that
	// is over one block higher than the current block or the server
	// indicates a reorg. It's possible that a light client never enters a
	// syncing state. In order to discern if syncing has begun when
	// HighestBlock is not set, check that the best header came in under
	// dexeth.MaxBlockInterval and guess.
	if fresh := checkHeaderTime(); !fresh {
		return false, 0, nil
	}
	return eth.node.peerCount() > 0, 1.0, nil
}

// DynamicSwapFeesPaid returns fees for initiation transactions. Part of the
// asset.DynamicSwapper interface.
func (eth *assetWallet) DynamicSwapFeesPaid(ctx context.Context, coinID, contractData dex.Bytes) (fee uint64, secretHashes [][]byte, err error) {
	return eth.swapOrRedemptionFeesPaid(ctx, coinID, contractData, true)
}

// DynamicRedemptionFeesPaid returns fees for redemption transactions. Part of
// the asset.DynamicSwapper interface.
func (eth *assetWallet) DynamicRedemptionFeesPaid(ctx context.Context, coinID, contractData dex.Bytes) (fee uint64, secretHashes [][]byte, err error) {
	return eth.swapOrRedemptionFeesPaid(ctx, coinID, contractData, false)
}

// swapOrRedemptionFeesPaid returns exactly how much gwei was used to send an
// initiation or redemption transaction. It also returns the secret hashes
// included with this init or redeem. Secret hashes are sorted so returns are
// always the same, but the order may not be the same as they exist in the
// transaction on chain. The transaction must be already mined for this
// function to work. Returns asset.CoinNotFoundError for unmined txn. Returns
// asset.ErrNotEnoughConfirms for txn with too few confirmations. Will also
// error if the secret hash in the contractData is not found in the transaction
// secret hashes.
func (eth *baseWallet) swapOrRedemptionFeesPaid(ctx context.Context, coinID, contractData dex.Bytes,
	isInit bool) (fee uint64, secretHashes [][]byte, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return 0, nil, err
	}

	var txHash common.Hash
	copy(txHash[:], coinID)
	receipt, tx, err := eth.node.transactionReceipt(ctx, txHash)
	if err != nil {
		return 0, nil, err
	}

	hdr, err := eth.node.headerByHash(ctx, receipt.BlockHash)
	if err != nil {
		return 0, nil, fmt.Errorf("error getting header %s: %w", receipt.BlockHash, err)
	}
	if hdr == nil {
		return 0, nil, fmt.Errorf("header for hash %v not found", receipt.BlockHash)
	}

	bestHdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		return 0, nil, err
	}

	confs := bestHdr.Number.Int64() - hdr.Number.Int64() + 1
	if confs < dynamicSwapOrRedemptionFeesConfs {
		return 0, nil, asset.ErrNotEnoughConfirms
	}

	effectiveGasPrice := new(big.Int).Add(hdr.BaseFee, tx.EffectiveGasTipValue(hdr.BaseFee))
	bigFees := new(big.Int).Mul(effectiveGasPrice, big.NewInt(int64(receipt.GasUsed)))
	if isInit {
		inits, err := dexeth.ParseInitiateData(tx.Data(), contractVer)
		if err != nil {
			return 0, nil, fmt.Errorf("invalid initiate data: %v", err)
		}
		secretHashes = make([][]byte, 0, len(inits))
		for k := range inits {
			copyK := k
			secretHashes = append(secretHashes, copyK[:])
		}
	} else {
		redeems, err := dexeth.ParseRedeemData(tx.Data(), contractVer)
		if err != nil {
			return 0, nil, fmt.Errorf("invalid redeem data: %v", err)
		}
		secretHashes = make([][]byte, 0, len(redeems))
		for k := range redeems {
			copyK := k
			secretHashes = append(secretHashes, copyK[:])
		}
	}
	sort.Slice(secretHashes, func(i, j int) bool { return bytes.Compare(secretHashes[i], secretHashes[j]) < 0 })
	var found bool
	for i := range secretHashes {
		if bytes.Equal(secretHash[:], secretHashes[i]) {
			found = true
			break
		}
	}
	if !found {
		return 0, nil, fmt.Errorf("secret hash %x not found in transaction", secretHash)
	}
	return dexeth.WeiToGwei(bigFees), secretHashes, nil
}

// RegFeeConfirmations gets the number of confirmations for the specified
// transaction.
func (eth *baseWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	var txHash common.Hash
	copy(txHash[:], coinID)
	return eth.node.transactionConfirmations(ctx, txHash)
}

// currentFeeRate gives the current rate of transactions being mined. Only
// use this to provide informative realistic estimates of actual fee *use*. For
// transaction planning, use recommendedMaxFeeRate.
func (eth *baseWallet) currentFeeRate(ctx context.Context) (*big.Int, error) {
	base, tip, err := eth.node.currentFees(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error getting net fee state: %v", err)
	}

	return new(big.Int).Add(tip, base), nil
}

// recommendedMaxFeeRate finds a recommended max fee rate using the somewhat
// standard baseRate * 2 + tip formula.
func (eth *baseWallet) recommendedMaxFeeRate(ctx context.Context) (*big.Int, error) {
	base, tip, err := eth.node.currentFees(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error getting net fee state: %v", err)
	}

	return new(big.Int).Add(
		tip,
		new(big.Int).Mul(base, big.NewInt(2)),
	), nil
}

// FeeRate satisfies asset.FeeRater.
func (eth *baseWallet) FeeRate() uint64 {
	feeRate, err := eth.recommendedMaxFeeRate(eth.ctx)
	if err != nil {
		eth.log.Errorf("Error getting net fee state: %v", err)
		return 0
	}

	feeRateGwei, err := dexeth.WeiToGweiUint64(feeRate)
	if err != nil {
		eth.log.Errorf("Failed to convert wei to gwei: %v", err)
		return 0
	}

	return feeRateGwei
}

func (eth *ETHWallet) checkPeers() {
	numPeers := eth.node.peerCount()

	for _, w := range eth.connectedWallets() {
		prevPeer := atomic.SwapUint32(&w.lastPeerCount, numPeers)
		if prevPeer != numPeers {
			w.peersChange(numPeers, nil)
		}
	}
}

func (eth *ETHWallet) monitorPeers(ctx context.Context) {
	ticker := time.NewTicker(peerCountTicker)
	defer ticker.Stop()
	for {
		eth.checkPeers()

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
func (eth *ETHWallet) monitorBlocks(ctx context.Context) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			eth.checkForNewBlocks(ctx)
		case <-ctx.Done():
			return
		}
		if ctx.Err() != nil { // shutdown during last check, disallow chance of another tick
			return
		}
	}
}

// checkForNewBlocks checks for new blocks. When a tip change is detected, the
// tipChange callback function is invoked and a goroutine is started to check
// if any contracts in the findRedemptionQueue are redeemed in the new blocks.
func (eth *ETHWallet) checkForNewBlocks(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	bestHdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		eth.log.Errorf("failed to get best hash: %v", err)
		return
	}
	bestHash := bestHdr.Hash()
	// This method is called frequently. Don't hold write lock
	// unless tip has changed.
	eth.tipMtx.RLock()
	currentTipHash := eth.currentTip.Hash()
	eth.tipMtx.RUnlock()
	if currentTipHash == bestHash {
		return
	}

	eth.tipMtx.Lock()
	defer eth.tipMtx.Unlock()

	prevTip := eth.currentTip
	eth.currentTip = bestHdr

	eth.log.Debugf("tip change: %s (%s) => %s (%s)", prevTip.Number,
		currentTipHash, bestHdr.Number, bestHash)

	connectedWallets := eth.connectedWallets()

	go func() {
		for _, w := range connectedWallets {
			w.emit.TipChange(bestHdr.Number.Uint64())
		}
	}()

	go func() {
		for _, w := range connectedWallets {
			w.checkFindRedemptions()
		}
	}()

	go func() {
		for _, w := range connectedWallets {
			w.checkPendingApprovals()
		}
	}()

	go eth.checkPendingTxs()
}

// getLatestMonitoredTx looks up a txHash in the monitoredTxs map. If the
// transaction has been replaced, the latest in the chain of transactions
// is returned.
//
// !!WARNING!!: The latest transaction is returned with its mutex locked.
// It must be unlocked by the caller. This is done in order to prevent
// another transaction starting the redemption process before
// a potential replacement.
func (w *assetWallet) getLatestMonitoredTx(txHash common.Hash) (*monitoredTx, error) {
	maxLoops := 100 // avoid an infinite loop in case of a cycle
	for i := 0; i < maxLoops; i++ {
		w.monitoredTxsMtx.RLock()
		tx, found := w.monitoredTxs[txHash]
		w.monitoredTxsMtx.RUnlock()
		if !found {
			return nil, fmt.Errorf("%s not found among monitored transactions", txHash)
		}
		tx.mtx.Lock()
		if tx.replacementTx == nil {
			return tx, nil
		}
		txHash = *tx.replacementTx
		tx.mtx.Unlock()
	}
	return nil, fmt.Errorf("there is a cycle in the monitored transactions")
}

// recordReplacementTx updates a monitoredTx with a replacement transaction.
// This change is also stored in the db.
//
// originalTx's mtx must be held when calling this function.
func (w *assetWallet) recordReplacementTx(originalTx *monitoredTx, replacementHash common.Hash) error {
	originalTx.replacementTx = &replacementHash
	originalHash := originalTx.tx.Hash()
	if err := w.txDB.storeMonitoredTx(originalHash, originalTx); err != nil {
		return fmt.Errorf("error recording replacement tx: %v", err)
	}

	w.monitoredTxsMtx.Lock()
	defer w.monitoredTxsMtx.Unlock()

	replacementTx, found := w.monitoredTxs[replacementHash]
	if !found {
		w.log.Errorf("could not find replacement monitored tx %s", replacementHash)
	}
	replacementTx.mtx.Lock()
	defer replacementTx.mtx.Unlock()
	replacementTx.replacedTx = &originalHash
	if err := w.txDB.storeMonitoredTx(replacementHash, replacementTx); err != nil {
		return fmt.Errorf("error recording replaced tx: %v", err)
	}

	return nil
}

// txsToDelete retraces the doubly linked list to find the all the
// ancestors of a monitoredTx.
func (w *assetWallet) txsToDelete(tx *monitoredTx) []common.Hash {
	txsToDelete := []common.Hash{tx.tx.Hash()}

	maxLoops := 100 // avoid an infinite loop in case of a cycle
	for i := 0; i < maxLoops; i++ {
		if tx.replacedTx == nil {
			return txsToDelete
		}
		txsToDelete = append(txsToDelete, *tx.replacedTx)
		var found bool
		tx, found = w.monitoredTxs[*tx.replacedTx]
		if !found {
			w.log.Errorf("failed to find replaced tx: %v", *tx.replacedTx)
			return txsToDelete
		}
	}

	w.log.Errorf("found cycle while clearing monitored txs")
	return txsToDelete
}

// clearMonitoredTx removes a monitored tx and all of its ancestors from the
// monitoredTxs map and the underlying database.
func (w *assetWallet) clearMonitoredTx(tx *monitoredTx) {
	if tx == nil {
		return
	}

	w.monitoredTxsMtx.Lock()
	defer w.monitoredTxsMtx.Unlock()

	txsToDelete := w.txsToDelete(tx)
	err := w.txDB.removeMonitoredTxs(txsToDelete)
	if err != nil {
		w.log.Errorf("Error removing monitored txs: %v", err)
		// Don't remove these txs from the memory map, so that the removal
		// from the db can be attempted again.
		return
	}

	// Delete from the database immediately, but keep in the memory map a bit
	// longer to allow time for other matches that used the same transaction
	// to complete. If they are cleared too early there will just be an error
	// message stating that the monitored tx is missing, but no other issue.
	go func() {
		timer := time.NewTimer(3 * time.Minute)
		select {
		case <-w.ctx.Done():
			return
		case <-timer.C:
		}
		w.monitoredTxsMtx.Lock()
		defer w.monitoredTxsMtx.Unlock()
		for _, hash := range txsToDelete {
			delete(w.monitoredTxs, hash)
		}
	}()
}

// monitorTx adds a transaction to the map of monitored transactions and also
// stores it in the db.
func (w *assetWallet) monitorTx(tx *types.Transaction, blockSubmitted uint64) {
	w.monitoredTxsMtx.Lock()
	defer w.monitoredTxsMtx.Unlock()

	monitoredTx := &monitoredTx{
		tx:             tx,
		blockSubmitted: blockSubmitted,
	}
	h := tx.Hash()
	if err := w.txDB.storeMonitoredTx(h, monitoredTx); err != nil {
		w.log.Errorf("error storing monitored tx: %v", err)
	}

	w.monitoredTxs[tx.Hash()] = monitoredTx
}

// resubmitRedemption resubmits a redemption transaction. Only the redemptions
// in the batch that are still redeemable are included in the new transaction.
// nonceOverride is set to a non-nil value when a specific nonce is required
// (when a transaction has not been mined due to a low fee).
func (w *assetWallet) resubmitRedemption(tx *types.Transaction, contractVersion uint32, nonceOverride *uint64, feeWallet *assetWallet, monitoredTx *monitoredTx) (*common.Hash, error) {
	parsedRedemptions, err := dexeth.ParseRedeemData(tx.Data(), contractVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redeem data: %w", err)
	}

	redemptions := make([]*asset.Redemption, 0, len(parsedRedemptions))

	// Whether or not a swap can be redeemed is checked in Redeem, but here
	// we filter out unredeemable swaps in case one of the swaps in the tx
	// was refunded/redeemed but the others were not.
	for _, r := range parsedRedemptions {
		redeemable, err := w.isRedeemable(r.SecretHash, r.Secret, contractVersion)
		if err != nil {
			return nil, err
		} else if !redeemable {
			w.log.Warnf("swap %x is not redeemable. not resubmitting", r.SecretHash)
			continue
		}

		contractData := dexeth.EncodeContractData(contractVersion, r.SecretHash)
		redemptions = append(redemptions, &asset.Redemption{
			Spends: &asset.AuditInfo{
				Contract:   contractData,
				SecretHash: r.SecretHash[:],
			},
			Secret: r.Secret[:],
		})
	}
	if len(redemptions) == 0 {
		return nil, fmt.Errorf("the swaps in tx %s are no longer redeemable. not resubmitting.", tx.Hash())
	}

	txs, _, _, err := w.Redeem(&asset.RedeemForm{
		Redemptions:   redemptions,
		FeeSuggestion: w.gasFeeLimit(),
	}, feeWallet, nonceOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to resubmit redemption: %v", err)
	}
	if len(txs) == 0 {
		return nil, fmt.Errorf("Redeem did not return a tx id")
	}

	var replacementHash common.Hash
	copy(replacementHash[:], txs[0])

	w.log.Infof("Redemption transaction %s was broadcast to replace transaction %s (original tx: %s)", replacementHash, monitoredTx.tx.Hash(), tx.Hash())

	if monitoredTx != nil {
		err = w.recordReplacementTx(monitoredTx, replacementHash)
		if err != nil {
			w.log.Errorf("failed to record %s as a replacement for %s", replacementHash, tx.Hash())
		}
	}

	return &replacementHash, nil
}

// swapIsRedeemed checks if a swap is in the redeemed state. ErrSwapRefunded
// is returned if the swap has been refunded.
func (w *assetWallet) swapIsRedeemed(secretHash common.Hash, contractVersion uint32) (bool, error) {
	swap, err := w.swap(w.ctx, secretHash, contractVersion)
	if err != nil {
		return false, err
	}

	switch swap.State {
	case dexeth.SSRedeemed:
		return true, nil
	case dexeth.SSRefunded:
		return false, asset.ErrSwapRefunded
	default:
		return false, nil
	}
}

// ConfirmRedemption checks the status of a redemption. If it is determined
// that a transaction will not be mined, this function will submit a new
// transaction to replace the old one. The caller is notified of this by having
// a different coinID in the returned asset.ConfirmRedemptionStatus as was used
// to call the function. Fee argument is ignored since it is calculated from
// the best header.
func (w *ETHWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, _ uint64) (*asset.ConfirmRedemptionStatus, error) {
	return w.confirmRedemption(coinID, redemption, nil)
}

// ConfirmRedemption checks the status of a redemption. If it is determined
// that a transaction will not be mined, this function will submit a new
// transaction to replace the old one. The caller is notified of this by having
// a different coinID in the returned asset.ConfirmRedemptionStatus as was used
// to call the function. Fee argument is ignored since it is calculated from
// the best header.
func (w *TokenWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, _ uint64) (*asset.ConfirmRedemptionStatus, error) {
	return w.confirmRedemption(coinID, redemption, w.parent)
}

const (
	txConfsNeededToConfirm               = 3
	blocksToWaitBeforeCoinNotFound       = 10
	blocksToWaitBeforeCheckingIfReplaced = 10
)

func confStatus(confs uint64, txHash common.Hash) *asset.ConfirmRedemptionStatus {
	return &asset.ConfirmRedemptionStatus{
		Confs:  confs,
		Req:    txConfsNeededToConfirm,
		CoinID: txHash[:],
	}
}

// checkUnconfirmedRedemption is called when a transaction has not yet been
// confirmed. It does the following:
// -- checks if the swap has already been redeemed by another tx
// -- resubmits the tx with a new nonce if it has been nonce replaced
// -- resubmits the tx with the same nonce but higher fee if the fee is too low
// -- otherwise, resubmits the same tx to ensure propagation
func (w *assetWallet) checkUnconfirmedRedemption(secretHash common.Hash, contractVer uint32, txHash common.Hash, tx *types.Transaction, feeWallet *assetWallet, monitoredTx *monitoredTx) (*asset.ConfirmRedemptionStatus, error) {
	// Check if the swap has been redeemed by another transaction we are unaware of.
	swapIsRedeemed, err := w.swapIsRedeemed(secretHash, contractVer)
	if err != nil {
		return nil, err
	}
	if swapIsRedeemed {
		w.clearMonitoredTx(monitoredTx)
		return confStatus(txConfsNeededToConfirm, txHash), nil
	}

	// Resubmit the transaction if another transaction with the same nonce has
	// already been confirmed.
	confirmedNonce, err := w.node.getConfirmedNonce(w.ctx)
	if err != nil {
		return nil, err
	}
	if confirmedNonce > tx.Nonce() {
		replacementTxHash, err := w.resubmitRedemption(tx, contractVer, nil, feeWallet, monitoredTx)
		if err != nil {
			return nil, err
		}
		return confStatus(0, *replacementTxHash), nil
	}

	// Resubmit the transaction if the current base fee is higher than the gas
	// fee cap in the transaction.
	baseFee, _, err := w.node.currentFees(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting net fee state: %w", err)
	}
	if baseFee.Cmp(tx.GasFeeCap()) > 0 {
		w.log.Errorf("redemption tx %s has a gas fee cap %v lower than the current base fee %v",
			txHash, tx.GasFeeCap(), baseFee)
		nonce := tx.Nonce()
		replacementTxHash, err := w.resubmitRedemption(tx, contractVer, &nonce, feeWallet, monitoredTx)
		if err != nil {
			return nil, err
		}
		return confStatus(0, *replacementTxHash), nil
	}

	// Resend the transaction in case it has not been mined because it was not
	// successfully propagated.
	err = w.node.sendSignedTransaction(w.ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to resubmit transaction %w", err)
	}

	return confStatus(0, txHash), nil
}

// confirmRedemptionWithoutMonitoredTx checks the confirmation status of a
// redemption transaction. It is called when a monitored tx cannot be
// found. The main difference between the regular path and this one is that
// when we can also not find the transaction, instead of resubmitting an
// entire redemption batch, a new transaction containing only the swap we are
// searching for will be created.
func (w *assetWallet) confirmRedemptionWithoutMonitoredTx(txHash common.Hash, redemption *asset.Redemption, feeWallet *assetWallet) (*asset.ConfirmRedemptionStatus, error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(redemption.Spends.Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to decode contract data: %w", err)
	}
	hdr, err := w.node.bestHeader(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get best header: %w", err)
	}
	currentTip := hdr.Number.Uint64()

	tx, txBlock, err := w.node.getTransaction(w.ctx, txHash)
	if errors.Is(err, asset.CoinNotFoundError) {
		w.log.Errorf("ConfirmRedemption: could not find tx: %s", txHash)
		swapIsRedeemed, err := w.swapIsRedeemed(secretHash, contractVer)
		if err != nil {
			return nil, err
		}
		if swapIsRedeemed {
			return confStatus(txConfsNeededToConfirm, txHash), nil
		}

		// If we cannot find the transaction, and it also wasn't among the
		// monitored txs, we will resubmit the swap individually.
		txs, _, _, err := w.Redeem(&asset.RedeemForm{
			Redemptions:   []*asset.Redemption{redemption},
			FeeSuggestion: w.FeeRate(),
		}, nil, nil)
		if err != nil {
			return nil, err
		}
		if len(txs) == 0 {
			return nil, errors.New("no txs returned while resubmitting redemption")
		}
		var resubmittedTxHash common.Hash
		copy(resubmittedTxHash[:], txs[0])
		return confStatus(0, resubmittedTxHash), nil
	}
	if err != nil {
		return nil, err
	}

	var confirmations uint64
	if txBlock > 0 {
		if currentTip >= uint64(txBlock) {
			confirmations = currentTip - uint64(txBlock) + 1
		}
	}
	if confirmations >= txConfsNeededToConfirm {
		receipt, _, err := w.node.transactionReceipt(w.ctx, txHash)
		if err != nil {
			return nil, err
		}
		if receipt.Status == types.ReceiptStatusSuccessful {
			return confStatus(txConfsNeededToConfirm, txHash), nil
		}
		replacementTxHash, err := w.resubmitRedemption(tx, contractVer, nil, feeWallet, nil)
		if err != nil {
			return nil, err
		}
		return confStatus(0, *replacementTxHash), nil
	}
	if confirmations > 0 {
		return confStatus(confirmations, txHash), nil
	}

	return w.checkUnconfirmedRedemption(secretHash, contractVer, txHash, tx, feeWallet, nil)
}

// confirmRedemption checks the confirmation status of a redemption transaction.
// It will resubmit transactions if it has been determined that the transaction
// cannot be mined.
func (w *assetWallet) confirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, feeWallet *assetWallet) (*asset.ConfirmRedemptionStatus, error) {
	if len(coinID) != common.HashLength {
		return nil, fmt.Errorf("expected coin ID to be a transaction hash, but it has a length of %d",
			len(coinID))
	}
	var txHash common.Hash
	copy(txHash[:], coinID)

	monitoredTx, err := w.getLatestMonitoredTx(txHash)
	if err != nil {
		w.log.Errorf("getLatestMonitoredTx error: %v", err)
		return w.confirmRedemptionWithoutMonitoredTx(txHash, redemption, feeWallet)
	}
	// This mutex is locked inside of getLatestMonitoredTx.
	defer monitoredTx.mtx.Unlock()
	monitoredTxHash := monitoredTx.tx.Hash()
	if monitoredTxHash != txHash {
		w.log.Debugf("tx %s was replaced by %s since the last attempt to confirm redemption",
			txHash, monitoredTxHash)
		txHash = monitoredTxHash
	}

	contractVer, secretHash, err := dexeth.DecodeContractData(redemption.Spends.Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to decode contract data: %w", err)
	}
	hdr, err := w.node.bestHeader(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get best header: %w", err)
	}
	currentTip := hdr.Number.Uint64()

	var blocksSinceSubmission uint64
	if currentTip > monitoredTx.blockSubmitted {
		blocksSinceSubmission = currentTip - monitoredTx.blockSubmitted
	}

	tx, txBlock, err := w.node.getTransaction(w.ctx, txHash)
	if errors.Is(err, asset.CoinNotFoundError) {
		if blocksSinceSubmission > 2 {
			w.log.Errorf("ConfirmRedemption: could not find tx: %s", txHash)
		}

		if blocksSinceSubmission < blocksToWaitBeforeCoinNotFound {
			return confStatus(0, txHash), nil
		}

		swapIsRedeemed, err := w.swapIsRedeemed(secretHash, contractVer)
		if err != nil {
			return nil, err
		}
		if swapIsRedeemed {
			return confStatus(txConfsNeededToConfirm, txHash), nil
		}

		replacementTxHash, err := w.resubmitRedemption(monitoredTx.tx, contractVer, nil, feeWallet, monitoredTx)
		if err != nil {
			return nil, err
		}
		return confStatus(0, *replacementTxHash), nil
	}
	if err != nil {
		return nil, err
	}

	var confirmations uint64
	if txBlock > 0 {
		if currentTip >= uint64(txBlock) {
			confirmations = currentTip - uint64(txBlock) + 1
		}
	}
	if confirmations >= txConfsNeededToConfirm {
		receipt, _, err := w.node.transactionReceipt(w.ctx, txHash)
		if err != nil {
			return nil, err
		}
		if receipt.Status == types.ReceiptStatusSuccessful {
			w.clearMonitoredTx(monitoredTx)
			return confStatus(txConfsNeededToConfirm, txHash), nil
		}

		w.log.Errorf("Redemption transaction rejected!!! Tx %s failed to redeem %s funds", tx.Hash(), dex.BipIDSymbol(w.assetID))
		// Only broadcast a single replacement before giving up.
		if monitoredTx.replacedTx == nil {
			w.log.Infof("Attempting to resubmit a %s redemption with secret hash %s", dex.BipIDSymbol(w.assetID), hex.EncodeToString(secretHash[:]))
			replacementTxHash, err := w.resubmitRedemption(tx, contractVer, nil, feeWallet, monitoredTx)
			if err != nil {
				return nil, err
			}
			return confStatus(0, *replacementTxHash), nil
		}
		// We've failed to redeem twice. We can't keep broadcasting txs into the
		// void. We have to give up. Print a bunch of errors and then report
		// the redemption as confirmed so we'll stop following it.
		if monitoredTx.errorsBroadcasted < 100 {
			monitoredTx.errorsBroadcasted++
			return nil, fmt.Errorf("failed to redeem %s swap with secret hash %s twice. not trying again",
				dex.BipIDSymbol(w.assetID), hex.EncodeToString(secretHash[:]))
		}
		const aTonOfFakeConfs = 1e3
		return confStatus(aTonOfFakeConfs, txHash), nil
	}
	if confirmations > 0 {
		return confStatus(confirmations, txHash), nil
	}

	// If the transaction is unconfirmed, check to see if it should be
	// resubmitted once every blocksToWaitBeforeCheckingIfReplaced blocks.
	if blocksSinceSubmission%blocksToWaitBeforeCheckingIfReplaced != 0 || blocksSinceSubmission == 0 {
		return confStatus(0, txHash), nil
	}

	return w.checkUnconfirmedRedemption(secretHash, contractVer, txHash, tx, feeWallet, monitoredTx)
}

// checkFindRedemptions checks queued findRedemptionRequests.
func (w *assetWallet) checkFindRedemptions() {
	for secretHash, req := range w.findRedemptionRequests() {
		secret, makerAddr, err := w.findSecret(secretHash, req.contractVer)
		if err != nil {
			w.sendFindRedemptionResult(req, secretHash, nil, "", err)
		} else if len(secret) > 0 {
			w.sendFindRedemptionResult(req, secretHash, secret, makerAddr, nil)
		}
	}
}

func (w *assetWallet) checkPendingApprovals() {
	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	for version, pendingApproval := range w.pendingApprovals {
		confs, err := w.node.transactionConfirmations(w.ctx, pendingApproval.txHash)

		if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
			w.log.Errorf("error getting confirmations for tx %s: %v", pendingApproval.txHash, err)
			continue
		}

		if confs > 0 {
			go pendingApproval.onConfirm()
			delete(w.pendingApprovals, version)
		}
	}
}

// sumPendingTxs sums the expected incoming and outgoing values in pending
// transactions stored in pendingTxs. Not used if the node is a
// txPoolFetcher.
func (w *assetWallet) sumPendingTxs(bal *big.Int) (out, in uint64) {
	isToken := w.assetID != w.baseChainID

	pendingTxsCopy := make(map[uint64]*extendedWalletTx, len(w.pendingTxs))
	w.pendingTxsMtx.Lock()
	for nonce, tx := range w.pendingTxs {
		pendingTxsCopy[nonce] = tx
	}
	balanceHasChanged := w.pendingTxCheckBal == nil || bal.Cmp(w.pendingTxCheckBal) != 0
	w.pendingTxCheckBal = bal
	w.pendingTxsMtx.Unlock()

	w.tipMtx.RLock()
	tip := w.currentTip.Number.Uint64()
	w.tipMtx.RUnlock()

	addPendingTx := func(txAssetID uint32, pt *extendedWalletTx) {
		if txAssetID == w.assetID {
			if asset.IncomingTxType(pt.Type) {
				in += pt.Amount
			} else {
				out += pt.Amount
			}
		}
		if !isToken {
			out += pt.Fees
		}
	}

	processPendingTx := func(nonce uint64, wt *extendedWalletTx) {
		wt.mtx.Lock()
		defer wt.mtx.Unlock()

		// Already confirmed, but still in the unconfirmed txs map waiting for
		// txConfsNeededToConfirm confirmations.
		if wt.BlockNumber != 0 {
			return
		}

		txAssetID := w.baseChainID
		if wt.TokenID != nil {
			txAssetID = *wt.TokenID
		}
		if isToken && w.assetID != txAssetID {
			return
		}

		if tip == wt.lastCheck || !balanceHasChanged {
			// Expect nothing has changed since our last check.
			addPendingTx(txAssetID, wt)
			return
		}

		givenUp := w.checkPendingTx(nonce, wt)
		if givenUp {
			return
		}

		if wt.BlockNumber == 0 {
			addPendingTx(txAssetID, wt)
		}
	}

	for nonce, wt := range pendingTxsCopy {
		processPendingTx(nonce, wt)
	}

	return
}

func (w *assetWallet) getConfirmedBalance() (*big.Int, error) {
	now := time.Now()
	w.tipMtx.RLock()
	tipHeight := w.currentTip.Number.Uint64()
	w.tipMtx.RUnlock()

	w.balances.Lock()
	defer w.balances.Unlock()

	if w.balances.m == nil {
		w.balances.m = make(map[uint32]*cachedBalance)
	}
	// Check to see if we already have one up-to-date
	cached := w.balances.m[w.assetID]
	if cached != nil && cached.height == tipHeight && time.Since(cached.stamp) < time.Minute {
		return cached.bal, nil
	}

	if w.multiBalanceContract == nil {
		var bal *big.Int
		var err error
		if w.assetID == w.baseChainID {
			bal, err = w.node.addressBalance(w.ctx, w.addr)
		} else {
			bal, err = w.tokenBalance()
		}
		if err != nil {
			return nil, err
		}
		w.balances.m[w.assetID] = &cachedBalance{
			stamp:  now,
			height: tipHeight,
			bal:    bal,
		}
		return bal, nil
	}

	// Either not cached, or outdated. Fetch anew.
	var tokenAddrs []common.Address
	idIndexes := map[int]uint32{
		0: w.baseChainID,
	}
	i := 1
	for assetID, tkn := range w.tokens {
		netToken := tkn.NetTokens[w.net]
		if netToken == nil || netToken.Address == (common.Address{}) {
			continue
		}
		tokenAddrs = append(tokenAddrs, netToken.Address)
		idIndexes[i] = assetID
		i++
	}
	callOpts := &bind.CallOpts{
		From:    w.addr,
		Context: w.ctx,
	}

	bals, err := w.multiBalanceContract.Balances(callOpts, w.addr, tokenAddrs)
	if err != nil {
		return nil, fmt.Errorf("error fetching multi-balance: %w", err)
	}
	if len(bals) != len(idIndexes) {
		return nil, fmt.Errorf("wrong number of balances in multi-balance result. wanted %d, got %d", len(idIndexes), len(bals))
	}
	var reqBal *big.Int
	for i, bal := range bals {
		assetID := idIndexes[i]
		if assetID == w.assetID {
			reqBal = bal
		}
		w.balances.m[assetID] = &cachedBalance{
			stamp:  now,
			height: tipHeight,
			bal:    bal,
		}
	}
	if reqBal == nil {
		return nil, fmt.Errorf("requested asset not in multi-balance result: %v", err)
	}
	return reqBal, nil
}

func (w *assetWallet) balanceWithTxPool() (*Balance, error) {
	isToken := w.assetID != w.baseChainID
	confirmed, err := w.getConfirmedBalance()
	if err != nil {
		return nil, fmt.Errorf("balance error: %v", err)
	}

	txPoolNode, is := w.node.(txPoolFetcher)
	if !is {
		for {
			out, in := w.sumPendingTxs(confirmed)
			checkBal, err := w.getConfirmedBalance()
			if err != nil {
				return nil, fmt.Errorf("balance consistency check error: %v", err)
			}
			outEVM := w.evmify(out)
			// If our balance has gone down in the interim, we'll use the lower
			// balance, but ensure that we're not setting up an underflow for
			// available balance.
			if checkBal.Cmp(confirmed) != 0 {
				w.log.Debugf("balance changed while checking pending txs. Trying again.")
				confirmed = checkBal
				continue
			}
			if outEVM.Cmp(confirmed) > 0 {
				return nil, fmt.Errorf("balance undeflow detected. pending out (%s) > balance (%s)", outEVM, confirmed)
			}

			return &Balance{
				Current:    confirmed,
				PendingOut: outEVM,
				PendingIn:  w.evmify(in),
			}, nil
		}
	}

	pendingTxs, err := txPoolNode.pendingTransactions()
	if err != nil {
		return nil, fmt.Errorf("error getting pending txs: %w", err)
	}

	outgoing := new(big.Int)
	incoming := new(big.Int)
	zero := new(big.Int)

	addFees := func(tx *types.Transaction) {
		gas := new(big.Int).SetUint64(tx.Gas())
		// For legacy transactions, GasFeeCap returns gas price
		if gasFeeCap := tx.GasFeeCap(); gasFeeCap != nil {
			outgoing.Add(outgoing, new(big.Int).Mul(gas, gasFeeCap))
		} else {
			w.log.Errorf("unable to calculate fees for tx %s", tx.Hash())
		}
	}

	ethSigner := types.LatestSigner(w.node.chainConfig()) // "latest" good for pending

	for _, tx := range pendingTxs {
		from, _ := ethSigner.Sender(tx)
		if from != w.addr {
			continue
		}

		if !isToken {
			// Add tx fees
			addFees(tx)
		}

		var contractOut uint64
		for ver, c := range w.contractors {
			in, out, err := c.value(w.ctx, tx)
			if err != nil {
				w.log.Errorf("version %d contractor incomingValue error: %v", ver, err)
				continue
			}
			contractOut += out
			if in > 0 {
				incoming.Add(incoming, w.evmify(in))
			}
		}
		if contractOut > 0 {
			outgoing.Add(outgoing, w.evmify(contractOut))
		} else if !isToken {
			// Count withdraws and sends for ETH.
			v := tx.Value()
			if v.Cmp(zero) > 0 {
				outgoing.Add(outgoing, v)
			}
		}
	}

	return &Balance{
		Current:    confirmed,
		PendingOut: outgoing,
		PendingIn:  incoming,
	}, nil
}

// sendToAddr sends funds to the address.
func (w *ETHWallet) sendToAddr(addr common.Address, amt uint64, maxFeeRate *big.Int) (tx *types.Transaction, err error) {
	w.baseWallet.nonceSendMtx.Lock()
	defer w.baseWallet.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(w.ctx, amt, defaultSendGasLimit, maxFeeRate, nil)
	if err != nil {
		return nil, err
	}
	tx, err = w.node.sendTransaction(w.ctx, txOpts, addr, nil)
	if err != nil {
		if mRPC, is := w.node.(*multiRPCClient); is {
			mRPC.voidUnusedNonce()
		}
		return nil, err
	}
	return tx, nil
}

// sendToAddr sends funds to the address.
func (w *TokenWallet) sendToAddr(addr common.Address, amt uint64, maxFeeRate *big.Int) (tx *types.Transaction, err error) {
	w.baseWallet.nonceSendMtx.Lock()
	defer w.baseWallet.nonceSendMtx.Unlock()
	g := w.gases(contractVersionNewest)
	if g == nil {
		return nil, fmt.Errorf("no gas table")
	}

	txOpts, err := w.node.txOpts(w.ctx, 0, g.Transfer, nil, nil)
	if err != nil {
		return nil, err
	}
	return tx, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		tx, err = c.transfer(txOpts, addr, w.evmify(amt))
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		return nil
	})
}

// swap gets a swap keyed by secretHash in the contract.
func (w *assetWallet) swap(ctx context.Context, secretHash [32]byte, contractVer uint32) (swap *dexeth.SwapState, err error) {
	return swap, w.withContractor(contractVer, func(c contractor) error {
		swap, err = c.swap(ctx, secretHash)
		return err
	})
}

// initiate initiates multiple swaps in the same transaction.
func (w *assetWallet) initiate(ctx context.Context, assetID uint32, contracts []*asset.Contract,
	maxFeeRate, gasLimit uint64, contractVer uint32) (tx *types.Transaction, err error) {

	var val uint64
	if assetID == w.baseChainID {
		for _, c := range contracts {
			val += c.Value
		}
	}
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(ctx, val, gasLimit, dexeth.GweiToWei(maxFeeRate), nil)
	if err != nil {
		return nil, err
	}
	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.initiate(txOpts, contracts)
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		return nil
	})
}

// estimateInitGas checks the amount of gas that is used for the
// initialization.
func (w *assetWallet) estimateInitGas(ctx context.Context, numSwaps int, contractVer uint32) (gas uint64, err error) {
	return gas, w.withContractor(contractVer, func(c contractor) error {
		gas, err = c.estimateInitGas(ctx, numSwaps)
		return err
	})
}

// estimateRedeemGas checks the amount of gas that is used for the redemption.
// Only used with testing and development tools like the
// nodeclient_harness_test.go suite (GetGasEstimates, testRedeemGas, etc.).
// Never use this with a public RPC provider, especially as maker, since it
// reveals the secret keys.
func (w *assetWallet) estimateRedeemGas(ctx context.Context, secrets [][32]byte, contractVer uint32) (gas uint64, err error) {
	return gas, w.withContractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRedeemGas(ctx, secrets)
		return err
	})
}

// estimateRefundGas checks the amount of gas that is used for a refund.
func (w *assetWallet) estimateRefundGas(ctx context.Context, secretHash [32]byte, contractVer uint32) (gas uint64, err error) {
	return gas, w.withContractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRefundGas(ctx, secretHash)
		return err
	})
}

// loadContractors prepares the token contractors and add them to the map.
func (w *assetWallet) loadContractors() error {
	token, found := w.tokens[w.assetID]
	if !found {
		return fmt.Errorf("token %d not found", w.assetID)
	}
	netToken, found := token.NetTokens[w.net]
	if !found {
		return fmt.Errorf("token %d not found", w.assetID)
	}

	for ver := range netToken.SwapContracts {
		constructor, found := tokenContractorConstructors[ver]
		if !found {
			w.log.Errorf("contractor constructor not found for token %s, version %d", token.Name, ver)
			continue
		}
		c, err := constructor(w.net, token, w.addr, w.node.contractBackend())
		if err != nil {
			return fmt.Errorf("error constructing token %s contractor version %d: %w", token.Name, ver, err)
		}

		if netToken.Address != c.tokenAddress() {
			return fmt.Errorf("wrong %s token address. expected %s, got %s", token.Name, netToken.Address, c.tokenAddress())
		}

		w.contractors[ver] = c
	}
	return nil
}

// withContractor runs the provided function with the versioned contractor.
func (w *assetWallet) withContractor(contractVer uint32, f func(contractor) error) error {
	if contractVer == contractVersionNewest {
		var bestVer uint32
		var bestContractor contractor
		for ver, c := range w.contractors {
			if ver >= bestVer {
				bestContractor = c
				bestVer = ver
			}
		}
		return f(bestContractor)
	}
	contractor, found := w.contractors[contractVer]
	if !found {
		return fmt.Errorf("no version %d contractor for asset %d", contractVer, w.assetID)
	}
	return f(contractor)
}

// withTokenContractor runs the provided function with the tokenContractor.
func (w *assetWallet) withTokenContractor(assetID, ver uint32, f func(tokenContractor) error) error {
	return w.withContractor(ver, func(c contractor) error {
		tc, is := c.(tokenContractor)
		if !is {
			return fmt.Errorf("contractor for %d %T is not a tokenContractor", assetID, c)
		}
		return f(tc)
	})
}

// estimateApproveGas estimates the gas required for a transaction approving a
// spender for an ERC20 contract.
func (w *assetWallet) estimateApproveGas(newGas *big.Int) (gas uint64, err error) {
	return gas, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		gas, err = c.estimateApproveGas(w.ctx, newGas)
		return err
	})
}

// estimateTransferGas estimates the gas needed for a token transfer call to an
// ERC20 contract.
func (w *assetWallet) estimateTransferGas(val uint64) (gas uint64, err error) {
	return gas, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		gas, err = c.estimateTransferGas(w.ctx, w.evmify(val))
		return err
	})
}

// redeem redeems a swap contract. Any on-chain failure, such as this secret not
// matching the hash, will not cause this to error.
func (w *assetWallet) redeem(ctx context.Context, assetID uint32 /* ?? */, redemptions []*asset.Redemption,
	maxFeeRate, gasLimit uint64, contractVer uint32, nonceOverride *uint64) (tx *types.Transaction, err error) {
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	var nonce *big.Int
	if nonceOverride != nil {
		nonce = new(big.Int).SetUint64(*nonceOverride)
	}
	txOpts, err := w.node.txOpts(ctx, 0, gasLimit, dexeth.GweiToWei(maxFeeRate), nonce)
	if err != nil {
		return nil, err
	}

	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.redeem(txOpts, redemptions)
		if err != nil {
			// If we did not override the nonce for a replacement
			// transaction, make it available for the next transaction
			// on error.
			if nonceOverride == nil {
				c.voidUnusedNonce()
			}
			return err
		}
		return nil
	})
}

// refund refunds a swap contract using the account controlled by the wallet.
// Any on-chain failure, such as the locktime not being past, will not cause
// this to error.
func (w *assetWallet) refund(secretHash [32]byte, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, fees uint64, err error) {
	gas := w.gases(contractVer)
	if gas == nil {
		return nil, 0, fmt.Errorf("no gas table for asset %d, version %d", w.assetID, contractVer)
	}
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(w.ctx, 0, gas.Refund, dexeth.GweiToWei(maxFeeRate), nil)
	if err != nil {
		return nil, 0, err
	}

	return tx, gas.Refund * maxFeeRate, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.refund(txOpts, secretHash)
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		return nil
	})
}

// isRedeemable checks if the swap identified by secretHash is redeemable using
// secret. This must NOT be a contractor call.
func (w *assetWallet) isRedeemable(secretHash [32]byte, secret [32]byte, contractVer uint32) (redeemable bool, err error) {
	swap, err := w.swap(w.ctx, secretHash, contractVer)
	if err != nil {
		return false, err
	}

	if swap.State != dexeth.SSInitiated {
		return false, nil
	}

	return w.ValidateSecret(secret[:], secretHash[:]), nil
}

func (w *assetWallet) isRefundable(secretHash [32]byte, contractVer uint32) (refundable bool, err error) {
	return refundable, w.withContractor(contractVer, func(c contractor) error {
		refundable, err = c.isRefundable(secretHash)
		return err
	})
}

func checkTxStatus(receipt *types.Receipt, gasLimit uint64) error {
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("transaction status failed")

	}

	if receipt.GasUsed > gasLimit {
		return fmt.Errorf("gas used, %d, appears to have exceeded limit of %d", receipt.GasUsed, gasLimit)
	}

	return nil
}

// emitTransactionNote sends a TransactionNote to the base asset wallet and
// also the wallet, if applicable.
func (w *baseWallet) emitTransactionNote(tx *asset.WalletTransaction, new bool) {
	w.walletsMtx.RLock()
	baseWallet, found := w.wallets[w.baseChainID]
	var tokenWallet *assetWallet
	if tx.TokenID != nil {
		tokenWallet = w.wallets[*tx.TokenID]
	}
	w.walletsMtx.RUnlock()

	if found {
		baseWallet.emit.TransactionNote(tx, new)
	} else {
		w.log.Error("emitTransactionNote: base asset wallet not found")
	}
	if tokenWallet != nil {
		tokenWallet.emit.TransactionNote(tx, new)
	}
}

// checkPendingTx checks the confirmation status of a transaction. The
// BlockNumber, Fees, and Timestamp fields of the extendedWalletTx are updated
// if the transaction is confirmed, and if the transaction has reached the
// required number of confirmations, it is removed from w.pendingTxs.
// True is returned from this function if we have given up on the transaction, and it
// should not be considered in the pending tx calculation.
//
// extendedWalletTx.mtx MUST be held while calling this function, but the
// w.pendingTxsMtx MUST NOT be held.
func (w *baseWallet) checkPendingTx(nonce uint64, pendingTx *extendedWalletTx) (givenUp bool) {
	w.tipMtx.RLock()
	tip := w.currentTip.Number.Uint64()
	w.tipMtx.RUnlock()

	var updated bool
	defer func() {
		if givenUp {
			err := w.txDB.removeTx(pendingTx.ID)
			if err != nil {
				w.log.Errorf("failed to remove tx from db: %v", err)
			} else {
				w.pendingTxsMtx.Lock()
				delete(w.pendingTxs, nonce)
				w.pendingTxsMtx.Unlock()
			}
			return
		}

		if updated || !pendingTx.savedToDB {
			err := w.txDB.storeTx(nonce, pendingTx)
			if err != nil {
				w.log.Errorf("error updating tx in db: %w", err)
				pendingTx.savedToDB = false
				return
			}

			pendingTx.savedToDB = true
			if pendingTx.Confirmed {
				w.pendingTxsMtx.Lock()
				delete(w.pendingTxs, nonce)
				w.pendingTxsMtx.Unlock()
			}

			w.emitTransactionNote(pendingTx.WalletTransaction, false)
		}
	}()

	if pendingTx.lastCheck == tip {
		return false
	}
	pendingTx.lastCheck = tip

	h, err := common.ParseHexOrString(pendingTx.ID)
	if err != nil {
		w.log.Errorf("error parsing tx hash %s: %v", pendingTx.ID, err)
		return
	}
	txHash := common.BytesToHash(h)
	receipt, tx, err := w.node.transactionReceipt(w.ctx, txHash)
	if err != nil {
		if errors.Is(err, asset.CoinNotFoundError) && pendingTx.BlockNumber > 0 {
			w.log.Warnf("TxID %v was previously confirmed but now cannot be found", pendingTx.ID)
			pendingTx.BlockNumber = 0
			pendingTx.Timestamp = 0
			updated = true
		}
		if !errors.Is(err, asset.CoinNotFoundError) {
			w.log.Errorf("Error getting confirmations for pending tx %s: %v", txHash, err)
		}
		if time.Since(time.Unix(int64(pendingTx.SubmissionTime), 0)) > time.Minute*6 {
			givenUp = true
		}

		return
	}

	if receipt.BlockNumber == nil || receipt.BlockNumber.Cmp(new(big.Int)) == 0 {
		if pendingTx.BlockNumber > 0 {
			w.log.Warnf("TxID %v was previously confirmed but now is not confirmed", pendingTx.ID)
			pendingTx.BlockNumber = 0
			pendingTx.Timestamp = 0
			updated = true
		}
		return
	}
	hdr, err := w.node.headerByHash(w.ctx, receipt.BlockHash)
	if err != nil {
		w.log.Errorf("Error getting header for hash %v: %v", receipt.BlockHash, err)
		return
	}
	if hdr == nil {
		w.log.Errorf("Header for hash %v is nil", receipt.BlockHash)
		return
	}

	effectiveGasPrice := new(big.Int).Add(hdr.BaseFee, tx.EffectiveGasTipValue(hdr.BaseFee))
	bigFees := new(big.Int).Mul(effectiveGasPrice, big.NewInt(int64(receipt.GasUsed)))
	fees := dexeth.WeiToGwei(bigFees)
	blockNumber := receipt.BlockNumber.Uint64()
	if pendingTx.BlockNumber != blockNumber || pendingTx.Fees != fees || pendingTx.Timestamp != hdr.Time {
		pendingTx.Fees = dexeth.WeiToGwei(bigFees)
		pendingTx.BlockNumber = blockNumber
		pendingTx.Timestamp = hdr.Time
		updated = true
	}

	var confs uint64
	if blockNumber > 0 && tip >= blockNumber {
		confs = tip - blockNumber + 1
	}
	if confs >= txConfsNeededToConfirm {
		if !pendingTx.Confirmed {
			updated = true
		}
		pendingTx.Confirmed = true
	}

	return
}

// checkPendingTxs checks the confirmation status of all pending transactions.
func (w *baseWallet) checkPendingTxs() {
	pendingTxsCopy := make(map[uint64]*extendedWalletTx, len(w.pendingTxs))
	w.pendingTxsMtx.Lock()
	for nonce, tx := range w.pendingTxs {
		pendingTxsCopy[nonce] = tx
	}
	w.pendingTxsMtx.Unlock()

	for nonce, pendingTx := range pendingTxsCopy {
		pendingTx.mtx.Lock()
		w.checkPendingTx(nonce, pendingTx)
		pendingTx.mtx.Unlock()
	}
}

const txHistoryNonceKey = "Nonce"

func (w *baseWallet) addToTxHistory(nonce, amount, fees uint64, assetID uint32, txHash common.Hash, txType asset.TransactionType, recipient *string) {
	var tokenAssetID *uint32
	if assetID != w.baseChainID {
		tokenAssetID = &assetID
	}

	wt := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:      txType,
			ID:        txHash.String(),
			Amount:    amount,
			Fees:      fees,
			TokenID:   tokenAssetID,
			Recipient: recipient,
			AdditionalData: map[string]string{
				txHistoryNonceKey: strconv.FormatUint(nonce, 10),
			},
		},
		SubmissionTime: uint64(time.Now().Unix()),
		savedToDB:      true,
	}

	err := w.txDB.storeTx(nonce, wt)
	if err != nil {
		w.log.Errorf("error storing tx in db: %v", err)
		wt.savedToDB = false
	}

	w.pendingTxsMtx.Lock()
	w.pendingTxs[nonce] = wt
	w.pendingTxsMtx.Unlock()

	w.emitTransactionNote(wt.WalletTransaction, true)
}

// TxHistory returns all the transactions the wallet has made. This
// includes the ETH wallet and all token wallets. If refID is nil, then
// transactions starting from the most recent are returned (past is ignored).
// If past is true, the transactions prior to the refID are returned, otherwise
// the transactions after the refID are returned. n is the number of
// transactions to return. If n is <= 0, all the transactions will be returned.
func (w *ETHWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	baseChainWallet := w.wallet(w.baseChainID)
	if baseChainWallet == nil || !baseChainWallet.connected.Load() {
		return nil, fmt.Errorf("wallet not connected")
	}

	return w.txDB.getTxs(n, refID, past, nil)
}

// TxHistory returns all the transactions the token wallet has made. If refID
// is nil, then transactions starting from the most recent are returned (past
// is ignored). If past is true, the transactions prior to the refID are
// returned, otherwise the transactions after the refID are returned. n is the
// number of transactions to return. If n is <= 0, all the transactions will be
// returned.
func (w *TokenWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	baseChainWallet := w.wallet(w.baseChainID)
	if baseChainWallet == nil || !baseChainWallet.connected.Load() {
		return nil, fmt.Errorf("wallet not connected")
	}

	return w.txDB.getTxs(n, refID, past, &w.assetID)
}

func (w *ETHWallet) getReceivingTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txHash := common.HexToHash(txID)
	tx, blockHeight, err := w.node.getTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}

	if *tx.To() != w.addr {
		return nil, asset.CoinNotFoundError
	}

	w.tipMtx.RLock()
	tipHeight := w.currentTip.Number.Uint64()
	w.tipMtx.RUnlock()

	var confirmed bool
	if blockHeight < 0 {
		blockHeight = 0
		// TODO: check when this stops being pending
	} else if tipHeight-txConfsNeededToConfirm+1 >= uint64(blockHeight) {
		confirmed = true
	}

	return &asset.WalletTransaction{
		Type:        asset.Receive,
		ID:          txHash.String(),
		Amount:      dexeth.WeiToGwei(tx.Value()),
		BlockNumber: uint64(blockHeight),
		Confirmed:   confirmed,
		AdditionalData: map[string]string{
			txHistoryNonceKey: strconv.FormatUint(tx.Nonce(), 10),
		},
	}, nil
}

// WalletTransaction returns a transaction that either the wallet has made or
// one in which the wallet has received funds.
func (w *ETHWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txHash := common.HexToHash(txID)
	txID = txHash.String()
	txs, err := w.TxHistory(1, &txID, false)
	if errors.Is(err, asset.CoinNotFoundError) {
		return w.getReceivingTransaction(ctx, txID)
	}
	if err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, asset.CoinNotFoundError
	}

	tx := txs[0]
	if tx.BlockNumber > 0 {
		tx.Confirmed = true
	}

	return tx, nil
}

func (w *TokenWallet) getReceivingTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txHash := common.HexToHash(txID)
	tx, blockHeight, err := w.node.getTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}
	if *tx.To() != w.netToken.Address {
		return nil, asset.CoinNotFoundError
	}

	receivingAddr, value, err := erc20.ParseTransferData(tx.Data())
	if err != nil {
		return nil, asset.CoinNotFoundError
	}

	if receivingAddr != w.addr {
		return nil, asset.CoinNotFoundError
	}

	w.tipMtx.RLock()
	tipHeight := w.currentTip.Number.Uint64()
	w.tipMtx.RUnlock()

	var confirmed bool
	if blockHeight < 0 {
		blockHeight = 0
		// TODO: check when this stops being pending
	} else if tipHeight-txConfsNeededToConfirm+1 >= uint64(blockHeight) {
		confirmed = true
	}

	return &asset.WalletTransaction{
		Type:        asset.Receive,
		ID:          txID,
		Amount:      w.atomize(value),
		BlockNumber: uint64(blockHeight),
		Confirmed:   confirmed,
		AdditionalData: map[string]string{
			txHistoryNonceKey: strconv.FormatUint(tx.Nonce(), 10),
		},
	}, nil
}

func (w *TokenWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txID = common.HexToHash(txID).String()
	txs, err := w.TxHistory(1, &txID, false)
	if errors.Is(err, asset.CoinNotFoundError) {
		return w.getReceivingTransaction(ctx, txID)
	}
	if err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, asset.CoinNotFoundError
	}

	tx := txs[0]
	if tx.BlockNumber > 0 {
		tx.Confirmed = true
	}

	return tx, nil
}

// providersFile reads a file located at ~/dextest/credentials.json.
// The file contains seed and provider information for wallets used for
// getgas, deploy, and nodeclient testing. If simnet providers are not
// specified, getFileCredentials will add the simnet alpha node.
type providersFile struct {
	Seed      dex.Bytes                                                   `json:"seed"`
	Providers map[string] /* symbol */ map[string] /* network */ []string `json:"providers"`
}

// getFileCredentials reads the file at path and extracts the seed and the
// provider for the network.
func getFileCredentials(chain, path string, net dex.Network) (seed []byte, providers []string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading credentials file: %v", err)
	}
	var p providersFile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, nil, fmt.Errorf("error parsing credentials file: %v", err)
	}
	if len(p.Seed) == 0 {
		return nil, nil, fmt.Errorf("must provide both seeds in credentials file")
	}
	seed = p.Seed
	for _, uri := range p.Providers[chain][net.String()] {
		if !strings.HasPrefix(uri, "#") && !strings.HasPrefix(uri, ";") {
			providers = append(providers, uri)
		}
	}
	if net == dex.Simnet && len(providers) == 0 {
		u, _ := user.Current()
		switch chain {
		case "polygon":
			providers = []string{filepath.Join(u.HomeDir, "dextest", chain, "alpha", "bor", "bor.ipc")}
		default:
			providers = []string{filepath.Join(u.HomeDir, "dextest", chain, "alpha", "node", "geth.ipc")}
		}
	}
	return
}

// quickNode constructs a multiRPCClient and a contractor for the specified
// asset. The client is connected and unlocked.
func quickNode(ctx context.Context, walletDir string, contractVer uint32,
	seed []byte, providers []string, wParams *GetGasWalletParams, net dex.Network, log dex.Logger) (*multiRPCClient, contractor, error) {

	pw := []byte("abc")
	chainID := wParams.ChainCfg.ChainID.Int64()

	if err := CreateEVMWallet(chainID, &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     seed,
		Pass:     pw,
		Settings: map[string]string{providersKey: strings.Join(providers, " ")},
		DataDir:  walletDir,
		Net:      net,
		Logger:   log,
	}, wParams.Compat, false); err != nil {
		return nil, nil, fmt.Errorf("error creating initiator wallet: %v", err)
	}

	cl, err := newMultiRPCClient(walletDir, providers, log, wParams.ChainCfg, net)
	if err != nil {
		return nil, nil, fmt.Errorf("error opening initiator rpc client: %v", err)
	}

	if err = cl.connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("error connecting: %v", err)
	}

	success := false
	defer func() {
		if !success {
			cl.shutdown()
		}
	}()

	if err = cl.unlock(string(pw)); err != nil {
		return nil, nil, fmt.Errorf("error unlocking initiator node: %v", err)
	}

	var c contractor
	if wParams.Token == nil {
		ctor := contractorConstructors[contractVer]
		if ctor == nil {
			return nil, nil, fmt.Errorf("no contractor constructor for eth contract version %d", contractVer)
		}
		c, err = ctor(wParams.ContractAddr, cl.address(), cl.contractBackend())
		if err != nil {
			return nil, nil, fmt.Errorf("contractor constructor error: %v", err)
		}
	} else {
		ctor := tokenContractorConstructors[contractVer]
		if ctor == nil {
			return nil, nil, fmt.Errorf("no token contractor constructor for eth contract version %d", contractVer)
		}
		c, err = ctor(net, wParams.Token, cl.address(), cl.contractBackend())
		if err != nil {
			return nil, nil, fmt.Errorf("token contractor constructor error: %v", err)
		}
	}
	success = true
	return cl, c, nil
}

// waitForConfirmation waits for the specified transaction to have > 0
// confirmations.
func waitForConfirmation(ctx context.Context, cl ethFetcher, txHash common.Hash) error {
	bestHdr, err := cl.bestHeader(ctx)
	if err != nil {
		return fmt.Errorf("error getting best header: %w", err)
	}
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			hdr, _ := cl.bestHeader(ctx)
			if hdr != nil && hdr.Number.Cmp(bestHdr.Number) > 0 {
				bestHdr = hdr
				confs, err := cl.transactionConfirmations(ctx, txHash)
				if err != nil {
					if !errors.Is(err, asset.CoinNotFoundError) {
						return fmt.Errorf("error getting transaction confirmations: %v", err)
					}
					continue
				}
				if confs > 0 {
					return nil
				}
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// runSimnetMiner starts a gouroutine to generate a simnet block every 5 seconds
// until the ctx is canceled. By default, the eth harness will mine a block
// every 15s. We want to speed it up a bit for e.g. GetGas testing.
func runSimnetMiner(ctx context.Context, symbol string, log dex.Logger) {
	log.Infof("Starting the simnet miner")
	go func() {
		tick := time.NewTicker(time.Second * 5)
		u, err := user.Current()
		if err != nil {
			log.Criticalf("cannot run simnet miner because we couldn't get the current user")
			return
		}
		for {
			select {
			case <-tick.C:
				log.Debugf("Mining a simnet block")
				mine := exec.CommandContext(ctx, "./mine-alpha", "1")
				mine.Dir = filepath.Join(u.HomeDir, "dextest", symbol, "harness-ctl")
				b, err := mine.CombinedOutput()
				if err != nil {
					log.Errorf("Mining error: %v", err)
					log.Errorf("Mining error output: %s", string(b))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

type getGas byte

// GetGas provides access to the gas estimation utilities.
var GetGas getGas

// GetGasWalletParams are the configuration parameters required to estimate
// swap contract gas usage.
type GetGasWalletParams struct {
	ChainCfg     *params.ChainConfig
	Gas          *dexeth.Gases
	Token        *dexeth.Token
	UnitInfo     *dex.UnitInfo
	BaseUnitInfo *dex.UnitInfo
	Compat       *CompatibilityData
	ContractAddr common.Address // Base chain contract addr.
}

// ReadCredentials reads the credentials for the network from the credentials
// file.
func (getGas) ReadCredentials(chain, credentialsPath string, net dex.Network) (addr string, providers []string, err error) {
	seed, providers, err := getFileCredentials(chain, credentialsPath, net)
	if err != nil {
		return "", nil, err
	}
	privB, zero, err := privKeyFromSeed(seed)
	if err != nil {
		return "", nil, err
	}
	defer zero()
	privateKey, err := crypto.ToECDSA(privB)
	if err != nil {
		return "", nil, err
	}

	addr = crypto.PubkeyToAddress(privateKey.PublicKey).String()
	return
}

func getGetGasClientWithEstimatesAndBalances(ctx context.Context, net dex.Network, assetID, contractVer uint32, maxSwaps int,
	walletDir string, providers []string, seed []byte, wParams *GetGasWalletParams, log dex.Logger) (cl *multiRPCClient, c contractor,
	ethReq, swapReq, feeRate uint64, ethBal, tokenBal *big.Int, err error) {

	cl, c, err = quickNode(ctx, walletDir, contractVer, seed, providers, wParams, net, log)
	if err != nil {
		return nil, nil, 0, 0, 0, nil, nil, fmt.Errorf("error creating initiator wallet: %v", err)
	}

	var success bool
	defer func() {
		if !success {
			cl.shutdown()
		}
	}()

	base, tip, err := cl.currentFees(ctx)
	if err != nil {
		return nil, nil, 0, 0, 0, nil, nil, fmt.Errorf("Error estimating fee rate: %v", err)
	}

	ethBal, err = cl.addressBalance(ctx, cl.address())
	if err != nil {
		return nil, nil, 0, 0, 0, nil, nil, fmt.Errorf("error getting eth balance: %v", err)
	}

	feeRate = dexeth.WeiToGwei(new(big.Int).Add(tip, new(big.Int).Mul(base, big.NewInt(2))))

	// Check that we have a balance for swaps and fees.
	g := wParams.Gas
	const swapVal = 1
	n := uint64(maxSwaps)
	swapReq = n * (n + 1) / 2 * swapVal // Sum of positive integers up to n
	m := n - 1                          // for n swaps there will be (0 + 1 + 2 + ... + n - 1) AddSwaps
	initGas := (g.Swap * n) + g.SwapAdd*(m*(m+1)/2)
	redeemGas := (g.Redeem * n) + g.RedeemAdd*(m*(m+1)/2)

	fees := (initGas + redeemGas + defaultSendGasLimit) * // fees for participant wallet
		2 * // fudge factor
		6 / 5 * // base rate increase accommodation
		feeRate

	isToken := wParams.Token != nil
	ethReq = fees + swapReq
	if isToken {
		tc := c.(tokenContractor)
		tokenBal, err = tc.balance(ctx)
		if err != nil {
			return nil, nil, 0, 0, 0, nil, nil, fmt.Errorf("error fetching token balance: %v", err)
		}

		fees += (g.Transfer*2 + g.Approve*2*2 /* two approvals */ + defaultSendGasLimit /* approval client fee funding tx */) *
			2 * 6 / 5 * feeRate // Adding 20% in case base rate goes up
		ethReq = fees
	}
	success = true
	return
}

func (getGas) chainForAssetID(assetID uint32) string {
	ti := asset.TokenInfo(assetID)
	if ti == nil {
		return dex.BipIDSymbol(assetID)
	}
	return dex.BipIDSymbol(ti.ParentID)
}

// EstimateFunding estimates how much funding is needed for estimating gas, and
// prints helpful messages for the user.
func (getGas) EstimateFunding(ctx context.Context, net dex.Network, assetID, contractVer uint32,
	maxSwaps int, credentialsPath string, wParams *GetGasWalletParams, log dex.Logger) error {

	symbol := dex.BipIDSymbol(assetID)
	log.Infof("Estimating required funding for up to %d swaps of asset %s, contract version %d on %s", maxSwaps, symbol, contractVer, net)

	seed, providers, err := getFileCredentials(GetGas.chainForAssetID(assetID), credentialsPath, net)
	if err != nil {
		return err
	}

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, _, ethReq, swapReq, _, ethBalBig, tokenBalBig, err := getGetGasClientWithEstimatesAndBalances(ctx, net, assetID, contractVer, maxSwaps, walletDir, providers, seed, wParams, log)
	if err != nil {
		return err
	}
	defer cl.shutdown()
	ethBal := dexeth.WeiToGwei(ethBalBig)

	ui := wParams.UnitInfo
	assetFmt := ui.ConventionalString
	bui := wParams.BaseUnitInfo
	ethFmt := bui.ConventionalString

	log.Info("Address:", cl.address())
	log.Infof("%s balance: %s", bui.Conventional.Unit, ethFmt(ethBal))

	isToken := wParams.Token != nil
	tokenBalOK := true
	if isToken {
		log.Infof("%s required for fees: %s", bui.Conventional.Unit, ethFmt(ethReq))

		ui, err := asset.UnitInfo(assetID)
		if err != nil {
			return fmt.Errorf("error getting unit info for %d: %v", assetID, err)
		}

		tokenBal := wParams.Token.EVMToAtomic(tokenBalBig)
		log.Infof("%s balance: %s", ui.Conventional.Unit, assetFmt(tokenBal))
		log.Infof("%s required for trading: %s", ui.Conventional.Unit, assetFmt(swapReq))
		if tokenBal < swapReq {
			tokenBalOK = false
			log.Infof("❌ Insufficient %[2]s balance. Deposit %[1]s %[2]s before getting a gas estimate",
				assetFmt(swapReq-tokenBal), ui.Conventional.Unit)
		}

	} else {
		log.Infof("%s required: %s (swaps) + %s (fees) = %s",
			ui.Conventional.Unit, ethFmt(swapReq), ethFmt(ethReq-swapReq), ethFmt(ethReq))
	}

	if ethBal < ethReq {
		// Add 10% for fee drift.
		ethRecommended := ethReq * 11 / 10
		log.Infof("❌ Insufficient %s balance. Deposit about %s %s before getting a gas estimate",
			bui.Conventional.Unit, ethFmt(ethRecommended-ethBal), bui.Conventional.Unit)
	} else if tokenBalOK {
		log.Infof("👍 You have sufficient funding to run a gas estimate")
	}

	return nil
}

// Return returns the estimation wallet's base-chain or token balance to a
// specified address, if it is more than fees required to send.
func (getGas) Return(
	ctx context.Context,
	assetID uint32,
	credentialsPath,
	returnAddr string,
	wParams *GetGasWalletParams,
	net dex.Network,
	log dex.Logger,
) error {

	const contractVer = 0 // Doesn't matter

	if !common.IsHexAddress(returnAddr) {
		return fmt.Errorf("supplied return address %q is not an Ethereum address", returnAddr)
	}

	seed, providers, err := getFileCredentials(GetGas.chainForAssetID(assetID), credentialsPath, net)
	if err != nil {
		return err
	}

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, _, err := quickNode(ctx, walletDir, contractVer, seed, providers, wParams, net, log)
	if err != nil {
		return fmt.Errorf("error creating initiator wallet: %v", err)
	}
	defer cl.shutdown()

	base, tip, err := cl.currentFees(ctx)
	if err != nil {
		return fmt.Errorf("Error estimating fee rate: %v", err)
	}

	recommendedFeeRate := new(big.Int).Add(tip, new(big.Int).Mul(base, big.NewInt(2)))

	return GetGas.returnFunds(ctx, cl, recommendedFeeRate, common.HexToAddress(returnAddr), wParams.Token, wParams.UnitInfo, log, net)
}

func (getGas) returnFunds(
	ctx context.Context,
	cl *multiRPCClient,
	feeRate *big.Int,
	returnAddr common.Address,
	token *dexeth.Token, // nil for base chain
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {

	bigEthBal, err := cl.addressBalance(ctx, cl.address())
	if err != nil {
		return fmt.Errorf("error getting eth balance: %v", err)
	}
	ethBal := dexeth.WeiToGwei(bigEthBal)

	if token != nil {
		nt, found := token.NetTokens[net]
		if !found {
			return fmt.Errorf("no %s token for %s", token.Name, net)
		}
		var g dexeth.Gases
		for _, sc := range nt.SwapContracts {
			g = sc.Gas
			break
		}
		fees := g.Transfer * dexeth.WeiToGwei(feeRate)
		if fees > ethBal {
			return fmt.Errorf("not enough base chain balance (%s) to cover fees (%s)",
				dexeth.UnitInfo.ConventionalString(ethBal), dexeth.UnitInfo.ConventionalString(fees))
		}

		tokenContract, err := erc20.NewIERC20(nt.Address, cl.contractBackend())
		if err != nil {
			return fmt.Errorf("NewIERC20 error: %v", err)
		}

		callOpts := &bind.CallOpts{
			From:    cl.address(),
			Context: ctx,
		}

		bigTokenBal, err := tokenContract.BalanceOf(callOpts, cl.address())
		if err != nil {
			return fmt.Errorf("error getting token balance: %w", err)
		}

		txOpts, err := cl.txOpts(ctx, 0, g.Transfer, feeRate, nil)
		if err != nil {
			return fmt.Errorf("error generating tx opts: %w", err)
		}

		tx, err := tokenContract.Transfer(txOpts, returnAddr, bigTokenBal)
		if err != nil {
			return fmt.Errorf("error transferring tokens : %w", err)
		}
		log.Infof("Sent %s in transaction %s", ui.ConventionalString(token.EVMToAtomic(bigTokenBal)), tx.Hash())
		return nil
	}

	bigFees := new(big.Int).Mul(new(big.Int).SetUint64(defaultSendGasLimit), feeRate)

	fees := dexeth.WeiToGwei(bigFees)

	ethFmt := ui.ConventionalString
	if fees >= ethBal {
		return fmt.Errorf("balance is lower than projected fees: %s < %s", ethFmt(ethBal), ethFmt(fees))
	}

	remainder := ethBal - fees
	txOpts, err := cl.txOpts(ctx, remainder, defaultSendGasLimit, feeRate, nil)
	if err != nil {
		return fmt.Errorf("error generating tx opts: %w", err)
	}
	tx, err := cl.sendTransaction(ctx, txOpts, returnAddr, nil)
	if err != nil {
		return fmt.Errorf("error sending funds: %w", err)
	}
	log.Infof("Sent %s in transaction %s", ui.ConventionalString(remainder), tx.Hash())
	return nil
}

// Estimate gets estimates useful for populating dexeth.Gases fields. Initiation
// and redeeem transactions with 1, 2, ... , and n swaps per transaction will be
// sent, for a total of n * (n + 1) / 2 total swaps in 2 * n transactions. If
// this is a token, and additional 1 approval transaction and 1 transfer
// transaction will be sent. The transfer transaction will send 1 atom to a
// random address (with zero token balance), to maximize gas costs. This atom is
// not recoverable. If you run this function with insufficient or zero ETH
// and/or token balance on the seed, the function will error with a message
// indicating the amount of funding needed to run.
func (getGas) Estimate(ctx context.Context, net dex.Network, assetID, contractVer uint32, maxSwaps int,
	credentialsPath string, wParams *GetGasWalletParams, log dex.Logger) error {

	symbol := dex.BipIDSymbol(assetID)
	log.Infof("Getting gas estimates for up to %d swaps of asset %s, contract version %d on %s", maxSwaps, symbol, contractVer, symbol)

	isToken := wParams.Token != nil

	seed, providers, err := getFileCredentials(GetGas.chainForAssetID(assetID), credentialsPath, net)
	if err != nil {
		return err
	}

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, c, ethReq, swapReq, feeRate, ethBal, tokenBal, err := getGetGasClientWithEstimatesAndBalances(ctx, net, assetID, contractVer, maxSwaps, walletDir, providers, seed, wParams, log)
	if err != nil {
		return err
	}
	defer cl.shutdown()

	log.Infof("Initiator address: %s", cl.address())

	ui := wParams.UnitInfo
	assetFmt := ui.ConventionalString
	bui := wParams.BaseUnitInfo
	bUnit := bui.Conventional.Unit
	ethFmt := bui.ConventionalString

	log.Infof("%s balance: %s", bUnit, ethFmt(dexeth.WeiToGwei(ethBal)))
	atomicBal := dexeth.WeiToGwei(ethBal)
	if atomicBal < ethReq {
		return fmt.Errorf("%s balance insufficient to get gas estimates. current: %[2]s, required ~ %[3]s %[1]s. send %[1]s to %[4]s",
			bUnit, ethFmt(atomicBal), ethFmt(ethReq*5/4), cl.address())
	}

	// Run the miner now, in case we need it for the approval client preload.
	if net == dex.Simnet {
		symbolParts := strings.Split(symbol, ".") // e.g. usdc.polygon, usdc.eth
		runSimnetMiner(ctx, symbolParts[len(symbolParts)-1], log)
	}

	var approvalClient *multiRPCClient
	var approvalContractor tokenContractor
	if isToken {

		atomicBal := wParams.Token.EVMToAtomic(tokenBal)

		convUnit := ui.Conventional.Unit
		log.Infof("%s balance: %s %s", strings.ToUpper(symbol), assetFmt(atomicBal), convUnit)
		log.Infof("%d %s required for swaps", swapReq, ui.AtomicUnit)
		log.Infof("%d gwei %s required for fees", ethReq, bui.Conventional.Unit)
		if atomicBal < swapReq {
			return fmt.Errorf("%[3]s balance insufficient to get gas estimates. current: %[1]s, required ~ %[2]s %[3]s. send %[3]s to %[4]s",
				assetFmt(atomicBal), assetFmt(swapReq), convUnit, cl.address())
		}

		var mrc contractor
		approvalClient, mrc, err = quickNode(ctx, filepath.Join(walletDir, "ac_dir"), contractVer, encode.RandomBytes(32), providers, wParams, net, log)
		if err != nil {
			return fmt.Errorf("error creating approval contract node: %v", err)
		}
		approvalContractor = mrc.(tokenContractor)
		defer approvalClient.shutdown()

		// TODO: We're overloading by probably 140% here, but this is what
		// we've reserved in our fee checks. Is it worth recovering unused
		// balance?
		feePreload := wParams.Gas.Approve * 2 * 6 / 5 * feeRate
		txOpts, err := cl.txOpts(ctx, feePreload, defaultSendGasLimit, nil, nil)
		if err != nil {
			return fmt.Errorf("error creating tx opts for sending fees for approval client: %v", err)
		}

		tx, err := cl.sendTransaction(ctx, txOpts, approvalClient.address(), nil)
		if err != nil {
			return fmt.Errorf("error sending fee reserves to approval client: %v", err)
		}
		if err = waitForConfirmation(ctx, approvalClient, tx.Hash()); err != nil {
			return fmt.Errorf("error waiting for approval fee funding tx: %w", err)
		}

	} else {
		log.Infof("%d gwei %s required for fees and swaps", ethReq, bui.Conventional.Unit)
	}

	log.Debugf("Getting gas estimates")
	return getGasEstimates(ctx, cl, approvalClient, c, approvalContractor, maxSwaps, wParams.Gas, log)
}

// getGasEstimate is used to get a gas table for an asset's contract(s). The
// provided gases, g, should be generous estimates of what the gas might be.
// Errors are thrown if the provided estimates are too small by more than a
// factor of 2. The account should already have a trading balance of at least
// maxSwaps gwei (token or eth), and sufficient eth balance to cover the
// requisite tx fees.
//
// acl (approval client) and ac (approval contractor) should be an ethFetcher
// and tokenContractor for a fresh account. They are used to get the approval
// gas estimate. These are only needed when the asset is a token. For eth, they
// can be nil.
func getGasEstimates(ctx context.Context, cl, acl ethFetcher, c contractor, ac tokenContractor,
	maxSwaps int, g *dexeth.Gases, log dex.Logger) (err error) {

	tc, isToken := c.(tokenContractor)

	stats := struct {
		swaps     []uint64
		redeems   []uint64
		refunds   []uint64
		approves  []uint64
		transfers []uint64
	}{}

	avg := func(vs []uint64) uint64 {
		var sum uint64
		for _, v := range vs {
			sum += v
		}
		return sum / uint64(len(vs))
	}

	avgDiff := func(vs []uint64) uint64 {
		diffs := make([]uint64, 0, len(vs)-1)
		for i := 0; i < len(vs)-1; i++ {
			diffs = append(diffs, vs[i+1]-vs[i])
		}
		return avg(diffs)
	}

	recommendedGas := func(v uint64) uint64 {
		return v * 13 / 10
	}

	defer func() {
		if len(stats.swaps) == 0 {
			return
		}
		firstSwap := stats.swaps[0]
		fmt.Printf("  First swap used %d gas Recommended Gases.Swap = %d\n", firstSwap, recommendedGas(firstSwap))
		if len(stats.swaps) > 1 {
			swapAdd := avgDiff(stats.swaps)
			fmt.Printf("    %d additional swaps averaged %d gas each. Recommended Gases.SwapAdd = %d\n",
				len(stats.swaps)-1, swapAdd, recommendedGas(swapAdd))
			fmt.Printf("    %+v \n", stats.swaps)
		}
		if len(stats.redeems) == 0 {
			return
		}
		firstRedeem := stats.redeems[0]
		fmt.Printf("  First redeem used %d gas. Recommended Gases.Redeem = %d\n", firstRedeem, recommendedGas(firstRedeem))
		if len(stats.redeems) > 1 {
			redeemAdd := avgDiff(stats.redeems)
			fmt.Printf("    %d additional redeems averaged %d gas each. recommended Gases.RedeemAdd = %d\n",
				len(stats.redeems)-1, redeemAdd, recommendedGas(redeemAdd))
			fmt.Printf("    %+v \n", stats.redeems)
		}
		redeemGas := avg(stats.refunds)
		fmt.Printf("  Average of %d refunds: %d. Recommended Gases.Refund = %d\n",
			len(stats.refunds), redeemGas, recommendedGas(redeemGas))
		fmt.Printf("    %+v \n", stats.refunds)
		if !isToken {
			return
		}
		approveGas := avg(stats.approves)
		fmt.Printf("  Average of %d approvals: %d. Recommended Gases.Approve = %d\n",
			len(stats.approves), approveGas, recommendedGas(approveGas))
		fmt.Printf("    %+v \n", stats.approves)
		transferGas := avg(stats.transfers)
		fmt.Printf("  Average of %d transfers: %d. Recommended Gases.Transfer = %d\n",
			len(stats.transfers), transferGas, recommendedGas(transferGas))
		fmt.Printf("    %+v \n", stats.transfers)
	}()

	// Estimate approve for tokens.
	if isToken {
		sendApprove := func(cl ethFetcher, c tokenContractor) error {
			txOpts, err := cl.txOpts(ctx, 0, g.Approve*2, nil, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for approve: %w", err)
			}
			tx, err := c.approve(txOpts, unlimitedAllowance)
			if err != nil {
				return fmt.Errorf("error estimating approve gas: %w", err)
			}
			if err = waitForConfirmation(ctx, cl, tx.Hash()); err != nil {
				return fmt.Errorf("error waiting for approve transaction: %w", err)
			}
			receipt, _, err := cl.transactionReceipt(ctx, tx.Hash())
			if err != nil {
				return fmt.Errorf("error getting receipt for approve tx: %w", err)
			}
			if err = checkTxStatus(receipt, g.Approve*2); err != nil {
				return fmt.Errorf("approve tx failed status check: [%w]. %d gas used out of %d available", err, receipt.GasUsed, g.Approve*2)
			}

			log.Infof("%d gas used for approval tx", receipt.GasUsed)
			stats.approves = append(stats.approves, receipt.GasUsed)
			return nil
		}

		log.Debugf("Sending approval transaction for random test client")
		if err = sendApprove(acl, ac); err != nil {
			return fmt.Errorf("error sending approve transaction for the new client: %w", err)
		}

		log.Debugf("Sending approval transaction for initiator")
		if err = sendApprove(cl, tc); err != nil {
			return fmt.Errorf("error sending approve transaction for the initiator: %w", err)
		}

		txOpts, err := cl.txOpts(ctx, 0, g.Transfer*2, nil, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for transfer: %w", err)
		}
		log.Debugf("Sending transfer transaction")
		// Transfer should be to a random address to maximize costs. This is a
		// sacrificial atom.
		var randomAddr common.Address
		copy(randomAddr[:], encode.RandomBytes(20))
		transferTx, err := tc.transfer(txOpts, randomAddr, big.NewInt(1))
		if err != nil {
			return fmt.Errorf("transfer error: %w", err)
		}
		if err = waitForConfirmation(ctx, cl, transferTx.Hash()); err != nil {
			return fmt.Errorf("error waiting for transfer tx: %w", err)
		}
		receipt, _, err := cl.transactionReceipt(ctx, transferTx.Hash())
		if err != nil {
			return fmt.Errorf("error getting tx receipt for transfer tx: %w", err)
		}
		if err = checkTxStatus(receipt, g.Transfer*2); err != nil {
			return fmt.Errorf("transfer tx failed status check: [%w]. %d gas used out of %d available", err, receipt.GasUsed, g.Transfer*2)
		}
		log.Infof("%d gas used for transfer tx", receipt.GasUsed)
		stats.transfers = append(stats.transfers, receipt.GasUsed)
	}

	for n := 1; n <= maxSwaps; n++ {
		contracts := make([]*asset.Contract, 0, n)
		secrets := make([][32]byte, 0, n)
		for i := 0; i < n; i++ {
			secretB := encode.RandomBytes(32)
			var secret [32]byte
			copy(secret[:], secretB)
			secretHash := sha256.Sum256(secretB)
			contracts = append(contracts, &asset.Contract{
				Address:    cl.address().String(), // trading with self
				Value:      1,
				SecretHash: secretHash[:],
				LockTime:   uint64(time.Now().Add(-time.Hour).Unix()),
			})
			secrets = append(secrets, secret)
		}

		var optsVal uint64
		if !isToken {
			optsVal = uint64(n)
		}

		// Send the inits
		txOpts, err := cl.txOpts(ctx, optsVal, g.SwapN(n)*2, nil, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for %d swaps: %v", n, err)
		}
		log.Debugf("Sending %d swaps", n)
		tx, err := c.initiate(txOpts, contracts)
		if err != nil {
			return fmt.Errorf("initiate error for %d swaps: %v", n, err)
		}
		if err = waitForConfirmation(ctx, cl, tx.Hash()); err != nil {
			return fmt.Errorf("error waiting for init tx to be mined: %w", err)
		}
		receipt, _, err := cl.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("error getting init tx receipt: %w", err)
		}
		if err = checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return fmt.Errorf("init tx failed status check: %w", err)
		}
		log.Infof("%d gas used for %d initiation txs", receipt.GasUsed, n)
		stats.swaps = append(stats.swaps, receipt.GasUsed)

		// Estimate a refund
		var firstSecretHash [32]byte
		copy(firstSecretHash[:], contracts[0].SecretHash)
		refundGas, err := c.estimateRefundGas(ctx, firstSecretHash)
		if err != nil {
			return fmt.Errorf("error estimate refund gas: %w", err)
		}
		log.Infof("%d gas estimated for a refund", refundGas)
		stats.refunds = append(stats.refunds, refundGas)

		redemptions := make([]*asset.Redemption, 0, n)
		for i, contract := range contracts {
			redemptions = append(redemptions, &asset.Redemption{
				Spends: &asset.AuditInfo{
					SecretHash: contract.SecretHash,
				},
				Secret: secrets[i][:],
			})
		}

		txOpts, err = cl.txOpts(ctx, 0, g.RedeemN(n)*2, nil, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for %d redeems: %v", n, err)
		}
		log.Debugf("Sending %d redemption txs", n)
		tx, err = c.redeem(txOpts, redemptions)
		if err != nil {
			return fmt.Errorf("redeem error for %d swaps: %v", n, err)
		}
		if err = waitForConfirmation(ctx, cl, tx.Hash()); err != nil {
			return fmt.Errorf("error waiting for redeem tx to be mined: %w", err)
		}
		receipt, _, err = cl.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("error getting redeem tx receipt: %w", err)
		}
		if err = checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return fmt.Errorf("redeem tx failed status check: %w", err)
		}
		log.Infof("%d gas used for %d redemptions", receipt.GasUsed, n)
		stats.redeems = append(stats.redeems, receipt.GasUsed)
	}

	return nil
}
