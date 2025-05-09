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
	"decred.org/dcrdex/dex/utils"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/tyler-smith/go-bip39"
)

func registerToken(tokenID uint32, desc string) {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	netAddrs := make(map[dex.Network]string)
	netAssetVersions := make(map[dex.Network][]uint32, 3)
	for net, netToken := range token.NetTokens {
		netAddrs[net] = netToken.Address.String()
		netAssetVersions[net] = make([]uint32, 0, 1)
		for ver := range netToken.SwapContracts {
			netAssetVersions[net] = append(netAssetVersions[net], ver)
		}
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        walletTypeToken,
		Tab:         "Ethereum token",
		Description: desc,
	}, netAddrs, netAssetVersions)
}

func init() {
	dexeth.MaybeReadSimnetAddrs()
	asset.Register(BipID, &Driver{})
	registerToken(usdcEthID, "The USDC Ethereum ERC20 token.")
	registerToken(usdtEthID, "The USDT Ethereum ERC20 token.")
	registerToken(maticEthID, "The MATIC Ethereum ERC20 token.")
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

	// onChainDataFetchTimeout is the max amount of time allocated to fetching
	// on-chain data. Testing on testnet has shown spikes up to 2.5 seconds
	// (but on internet, with Tor, it could actually take up to 30 seconds easily).
	// Setting it to 10 seconds for now until https://github.com/decred/dcrdex/issues/3184
	// is resolved.
	onChainDataFetchTimeout = 10 * time.Second

	// coinIDTakerFoundMakerRedemption is a prefix to identify one of CoinID formats,
	// see DecodeCoinID func for details.
	coinIDTakerFoundMakerRedemption = "TakerFoundMakerRedemption:"

	LiveEstimateFailedError = dex.ErrorKind("live gas estimate failed")

	// txAgeOut is the amount of time after which we forego any tx
	// synchronization efforts for unconfirmed pending txs.
	txAgeOut = 2 * time.Hour
	// stateUpdateTick is the minimum amount of time between checks for
	// new block and updating of pending txs, counter-party redemptions and
	// approval txs.
	// HTTP RPC clients meter tip header calls to minimum 10 seconds.
	// WebSockets will stay up-to-date, so can expect new blocks often.
	// A shorter blockTicker would be too much for e.g. Polygon where the block
	// time is 2 or 3 seconds. We'd be doing a ton of calls for pending tx
	// updates.
	stateUpdateTick = time.Second * 5
	// maxUnindexedTxs is the number of pending txs we will allow to be
	// unverified on-chain before we halt broadcasting of new txs.
	maxUnindexedTxs = 10
	peerCountTicker = 5 * time.Second // no rpc calls here
)

var (
	walletOpts = []*asset.ConfigOption{
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
			DisplayName: "RPC Provider",
			Description: "Specify one or more RPC providers. For infrastructure " +
				"providers, prefer using wss address. Only url-based authentication " +
				"is supported. For a local node, use the filepath to an IPC file.",
			Repeatable:   providerDelimiter,
			RepeatN:      2,
			DefaultValue: "",
		},
	}
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = asset.WalletInfo{
		Name: "Ethereum",
		// SupportedVersions: For Ethereum, the server backend maintains a
		// single protocol version, so tokens and ETH have the same set of
		// supported versions. Even though the SupportedVersions are made
		// accessible for tokens via (*TokenWallet).Info, the versions are not
		// exposed though any Driver methods or assets/driver functions. Use the
		// parent wallet's WalletInfo via (*Driver).Info if you need a token's
		// supported versions before a wallet is available.
		SupportedVersions: []uint32{0, 1},
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

// safeConfs returns the confirmations for a given tip and block number,
// returning 0 if the block number is zero or if the tip is lower than the
// block number.
func safeConfs(tip, blockNum uint64) uint64 {
	if blockNum == 0 {
		return 0
	}
	if tip < blockNum {
		return 0
	}
	return tip - blockNum + 1
}

// safeConfsBig is safeConfs but with a *big.Int blockNum. A nil blockNum will
// result in a zero.
func safeConfsBig(tip uint64, blockNum *big.Int) uint64 {
	if blockNum == nil {
		return 0
	}
	return safeConfs(tip, blockNum.Uint64())
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
// satisfied by *multiRPCClient. For testing, it can be satisfied by a stub.
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
	sendSignedTransaction(ctx context.Context, tx *types.Transaction, filts ...acceptabilityFilter) error
	sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte, filts ...acceptabilityFilter) (*types.Transaction, error)
	signData(data []byte) (sig, pubKey []byte, err error)
	syncProgress(context.Context) (progress *ethereum.SyncProgress, tipTime uint64, err error)
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	getTransaction(context.Context, common.Hash) (*types.Transaction, int64, error)
	txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, tipCap, nonce *big.Int) (*bind.TransactOpts, error)
	currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error)
	unlock(pw string) error
	getConfirmedNonce(context.Context) (uint64, error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	transactionAndReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error)
	nonce(ctx context.Context) (confirmed, next *big.Int, err error)
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

	finalizeConfs uint64

	multiBalanceAddress  common.Address
	multiBalanceContract *multibal.MultiBalanceV0

	baseChainID  uint32
	chainCfg     *params.ChainConfig
	chainID      int64
	compat       *CompatibilityData
	tokens       map[uint32]*dexeth.Token
	maxTxFeeGwei uint64

	startingBlocks atomic.Uint64

	tipMtx     sync.RWMutex
	currentTip *types.Header

	settingsMtx sync.RWMutex
	settings    map[string]string

	gasFeeLimitV uint64 // atomic

	walletsMtx sync.RWMutex
	wallets    map[uint32]*assetWallet

	nonceMtx            sync.RWMutex
	pendingTxs          []*extendedWalletTx
	confirmedNonceAt    *big.Int
	pendingNonceAt      *big.Int
	recoveryRequestSent bool

	balances struct {
		sync.Mutex
		m map[uint32]*cachedBalance
	}

	currentFees struct {
		sync.Mutex
		blockNum uint64
		baseRate *big.Int
		tipRate  *big.Int
	}

	txDB txDB
}

// assetWallet is a wallet backend for Ethereum and Eth tokens. The backend is
// how Bison Wallet communicates with the Ethereum blockchain and wallet.
// assetWallet satisfies the dex.Wallet interface.
type assetWallet struct {
	*baseWallet
	assetID   uint32
	emit      *asset.WalletEmitter
	log       dex.Logger
	ui        dex.UnitInfo
	connected atomic.Bool
	wi        asset.WalletInfo
	tokenAddr common.Address // empty address for base chain asset

	versionedContracts map[uint32]common.Address
	versionedGases     map[uint32]*dexeth.Gases

	lockedFunds struct {
		mtx                sync.RWMutex
		initiateReserves   uint64
		redemptionReserves uint64
		refundReserves     uint64
	}

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[string]*findRedemptionRequest

	approvalsMtx     sync.RWMutex
	pendingApprovals map[common.Address]*pendingApproval
	approvalCache    map[common.Address]bool

	lastPeerCount uint32
	peersChange   func(uint32, error)

	contractorV0 contractor
	contractorV1 contractor

	evmify  func(uint64) *big.Int
	atomize func(*big.Int) uint64

	// pendingTxCheckBal is protected by the nonceMtx. We use this field
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

// perTxGasLimit is the most gas we can use on a transaction. It is the lower of
// either the block's gas limit or the limit based on our maximum allowable
// fees.
func (w *assetWallet) perTxGasLimit(feeRateGwei uint64) uint64 {
	blockGasLimit := w.tip().GasLimit
	maxFeeBasedGasLimit := w.maxTxFeeGwei / feeRateGwei
	return min(maxFeeBasedGasLimit, blockGasLimit)
}

// maxSwapsOrRedeems calculates the maximum number of swaps or redemptions that
// can go in a single transaction. If feeRateGwei is not provided, the
// prevailing fee rate will be used.
func (w *assetWallet) maxSwapsOrRedeems(oneGas, gasAdd uint64, feeRateGwei uint64) (n int, err error) {
	feeRate := dexeth.GweiToWei(feeRateGwei)
	if feeRateGwei == 0 {
		feeRate, err = w.currentFeeRate(w.ctx)
		if err != nil {
			return 0, err
		}
	}

	txGasLimit := w.perTxGasLimit(dexeth.WeiToGweiCeil(feeRate))
	if oneGas > txGasLimit {
		return 0, fmt.Errorf("tx limit zero")
	}
	return 1 + int((txGasLimit-oneGas)/gasAdd), nil
}

var _ asset.MaxMatchesCounter = (*assetWallet)(nil)

// MaxSwaps is the maximum matches than can go in a swap tx.
func (w *assetWallet) MaxSwaps(assetVer uint32, feeRateGwei uint64) (int, error) {
	g := w.gases(contractVersion(assetVer))
	return w.maxSwapsOrRedeems(g.Swap, g.SwapAdd, feeRateGwei)
}

// MaxRedeems is the maximum matches than can go in a redeem tx.
func (w *assetWallet) MaxRedeems(assetVer uint32) (int, error) {
	g := w.gases(contractVersion(assetVer))
	return w.maxSwapsOrRedeems(g.Redeem, g.RedeemAdd, 0)
}

// Info returns basic information about the wallet and asset.
func (w *assetWallet) Info() *asset.WalletInfo {
	wi := w.wi
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

// contractVersion converts a server's asset protocol version to a swap contract
// version. It applies to both tokens and eth right now, but that may not always
// be the case.
func contractVersion(assetVer uint32) uint32 {
	if assetVer == asset.VersionNewest {
		return dexeth.ContractVersionNewest
	}
	return dexeth.ProtocolVersion(assetVer).ContractVersion()
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
				big.NewInt(chainID), compat, createWalletParams.Net, createWalletParams.Logger, false); err != nil {
				return fmt.Errorf("create and check providers: %v", err)
			}
		}
		return importKeyToKeyStore(ks, priv, createWalletParams.Pass)
	}

	return fmt.Errorf("unknown wallet type %q", createWalletParams.Type)
}

// newWallet is the constructor for an Ethereum asset.Wallet.
func newWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (w asset.Wallet, err error) {
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

	evmWallet, err := NewEVMWallet(&EVMWalletConfig{
		BaseChainID:        BipID,
		ChainCfg:           chainCfg,
		AssetCfg:           assetCFG,
		CompatData:         &comp,
		VersionedGases:     dexeth.VersionedGases,
		Tokens:             dexeth.Tokens,
		FinalizeConfs:      3,
		Logger:             logger,
		BaseChainContracts: contracts,
		MultiBalAddress:    dexeth.MultiBalanceAddresses[net],
		WalletInfo:         WalletInfo,
		Net:                net,
		DefaultProviders:   defaultProviders,
		MaxTxFeeGwei:       dexeth.GweiFactor, // 1 ETH
	})
	if err != nil {
		return nil, err
	}

	if _, supported := PolygonBridgeSupportedAsset(BipID, net); supported {
		return &ETHBridgeWallet{
			ETHWallet: evmWallet,
		}, nil
	}

	return evmWallet, nil
}

// EVMWalletConfig is the configuration for an evm-compatible wallet.
type EVMWalletConfig struct {
	BaseChainID        uint32
	ChainCfg           *params.ChainConfig
	AssetCfg           *asset.WalletConfig
	CompatData         *CompatibilityData
	VersionedGases     map[uint32]*dexeth.Gases
	Tokens             map[uint32]*dexeth.Token
	FinalizeConfs      uint64
	Logger             dex.Logger
	BaseChainContracts map[uint32]common.Address
	DefaultProviders   []string
	MultiBalAddress    common.Address // If empty, separate calls for N tokens + 1
	WalletInfo         asset.WalletInfo
	Net                dex.Network
	// MaxTxFeeGwei is the absolute maximum fees we will allow for a single tx.
	// It should be set to a relatively large value.
	MaxTxFeeGwei uint64
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
		net:                 cfg.Net,
		baseChainID:         cfg.BaseChainID,
		chainCfg:            cfg.ChainCfg,
		chainID:             chainID,
		compat:              cfg.CompatData,
		tokens:              cfg.Tokens,
		log:                 cfg.Logger,
		dir:                 cfg.AssetCfg.DataDir,
		walletType:          cfg.AssetCfg.Type,
		finalizeConfs:       cfg.FinalizeConfs,
		settings:            cfg.AssetCfg.Settings,
		gasFeeLimitV:        gasFeeLimit,
		wallets:             make(map[uint32]*assetWallet),
		multiBalanceAddress: cfg.MultiBalAddress,
		maxTxFeeGwei:        cfg.MaxTxFeeGwei,
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

	aw := &assetWallet{
		baseWallet:         eth,
		log:                cfg.Logger,
		assetID:            assetID,
		versionedContracts: cfg.BaseChainContracts,
		versionedGases:     cfg.VersionedGases,
		emit:               cfg.AssetCfg.Emit,
		findRedemptionReqs: make(map[string]*findRedemptionRequest),
		pendingApprovals:   make(map[common.Address]*pendingApproval),
		approvalCache:      make(map[common.Address]bool),
		peersChange:        cfg.AssetCfg.PeersChange,
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
		ui:                 dexeth.UnitInfo,
		pendingTxCheckBal:  new(big.Int),
		wi:                 cfg.WalletInfo,
	}

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

func migrateLegacyTxDB(legacyDBPath string, newDB txDB, log dex.Logger) error {
	if _, err := os.Stat(legacyDBPath); err != nil {
		return nil
	}

	legacyDB, err := newBadgerTxDB(legacyDBPath, log.SubLogger("LEGACYDB"))
	if err != nil {
		return fmt.Errorf("error opening legacy db: %w", err)
	}

	entries, err := legacyDB.getAllEntries()
	if err != nil {
		return fmt.Errorf("error getting legacy entries: %w", err)
	}

	for _, tx := range entries {
		if err := newDB.storeTx(tx); err != nil {
			return fmt.Errorf("error storing legacy tx: %w", err)
		}
	}

	log.Infof("Successfully migrated %d transactions from legacy database", len(entries))

	legacyDB.Close()
	return os.RemoveAll(legacyDBPath)
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
		rpcCl, err := newMultiRPCClient(w.dir, endpoints, w.log.SubLogger("RPC"), w.chainCfg, w.finalizeConfs, w.net)
		if err != nil {
			return nil, err
		}
		rpcCl.finalizeConfs = w.finalizeConfs
		cl = rpcCl
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
			w.log.Debugf("no eth swap contract address for version %d, net %s", ver, w.net)
			continue
		}
		c, err := constructor(w.net, contractAddr, w.addr, w.node.contractBackend())
		if err != nil {
			return nil, fmt.Errorf("error constructor version %d contractor: %v", ver, err)
		}
		switch ver {
		case 0:
			w.contractorV0 = c
		case 1:
			w.contractorV1 = c
		}
	}

	if w.multiBalanceAddress != (common.Address{}) {
		w.multiBalanceContract, err = multibal.NewMultiBalanceV0(w.multiBalanceAddress, cl.contractBackend())
		if err != nil {
			w.log.Errorf("Error loading MultiBalance contract: %v", err)
		}
	}

	w.txDB, err = NewTxDB(filepath.Join(w.dir, "txhistorydb-lexi"), w.log.SubLogger("TXDB"), w.baseChainID)
	if err != nil {
		return nil, err
	}

	legacyDBPath := filepath.Join(w.dir, "txhistorydb")
	err = migrateLegacyTxDB(legacyDBPath, w.txDB, w.log)
	if err != nil {
		return nil, err
	}

	txCM := dex.NewConnectionMaster(w.txDB)
	if err := txCM.ConnectOnce(ctx); err != nil {
		return nil, err
	}

	pendingTxs, err := w.txDB.getPendingTxs()
	if err != nil {
		return nil, err
	}
	sort.Slice(pendingTxs, func(i, j int) bool {
		return pendingTxs[i].Nonce.Cmp(pendingTxs[j].Nonce) < 0
	})

	// Initialize the best block.
	bestHdr, err := w.node.bestHeader(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block hash: %w", err)
	}
	confirmedNonce, nextNonce, err := w.node.nonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("error establishing nonce: %w", err)
	}

	w.tipMtx.Lock()
	w.currentTip = bestHdr
	w.tipMtx.Unlock()
	w.startingBlocks.Store(bestHdr.Number.Uint64())

	w.nonceMtx.Lock()
	w.pendingTxs = pendingTxs
	w.confirmedNonceAt = confirmedNonce
	w.pendingNonceAt = nextNonce
	w.nonceMtx.Unlock()

	if w.log.Level() <= dex.LevelDebug {
		var highestPendingNonce, lowestPendingNonce uint64
		for _, pendingTx := range pendingTxs {
			n := pendingTx.Nonce.Uint64()
			if n > highestPendingNonce {
				highestPendingNonce = n
			}
			if lowestPendingNonce == 0 || n < lowestPendingNonce {
				lowestPendingNonce = n
			}
		}
		w.log.Debugf("Synced with header %s and confirmed nonce %s, pending nonce %s, %d pending txs from nonce %d to nonce %d",
			bestHdr.Number, confirmedNonce, nextNonce, len(pendingTxs), highestPendingNonce, lowestPendingNonce)
	}

	height := bestHdr.Number
	// NOTE: We should be using the tipAtConnect to set Progress in SyncStatus.
	atomic.StoreInt64(&w.tipAtConnect, height.Int64())
	w.log.Infof("Connected to eth (%s), at height %d", w.walletType, height)

	var wg sync.WaitGroup

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

	w.connected.Store(true)
	go func() {
		<-ctx.Done()
		txCM.Wait()
		w.connected.Store(false)
	}()

	return &wg, nil
}

// Connect waits for context cancellation and closes the WaitGroup. Satisfies
// dex.Connector.
func (w *TokenWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if w.parent.ctx == nil || w.parent.ctx.Err() != nil {
		return nil, fmt.Errorf("parent wallet not connected")
	}

	err := w.loadContractors(w.parent)
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

// Connect connects the wallet and the bridge. Satisfies dex.Connector.
func (w *ETHBridgeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg, err := w.ETHWallet.Connect(ctx)
	if err != nil {
		return nil, err
	}

	var bridge bridge
	switch w.assetID {
	case ethID:
		bridge, err = newPolygonBridgeEth(w.node.contractBackend(), w.net, w.addr, w.log)
	default:
		err = fmt.Errorf("bridge not supported for asset %d", w.assetID)
	}
	if err != nil {
		return nil, err
	}

	w.manager, err = newBridgeManager(ctx, w.assetID, w.assetID, bridge, w.ETHWallet.emit, w.txDB, time.Minute, w.log)
	if err != nil {
		return nil, err
	}

	return wg, nil
}

// Connect connects the wallet and the bridge. Satisfies dex.Connector.
func (w *TokenBridgeWallet) Connect(ctx context.Context) (wg *sync.WaitGroup, err error) {
	wg, err = w.TokenWallet.Connect(ctx)
	if err != nil {
		return nil, err
	}

	var bridge bridge
	switch {
	case isUSDCBridgeSupported(w.assetID, w.net):
		bridge, err = newUsdcBridge(w.assetID, w.net, w.netToken.Address, w.node.contractBackend(), w.addr, w.node)
	case w.baseChainID == polygonID:
		bridge, err = newPolygonBridgePolygonErc20(w.node.contractBackend(), w.assetID, w.netToken.Address, w.log, w.net)
	case w.assetID == maticEthID:
		bridge, err = newPolygonBridgeEthPOL(w.node.contractBackend(), w.assetID, w.netToken.Address, w.net, w.addr, w.log)
	case w.baseChainID == ethID:
		bridge, err = newPolygonBridgeEthErc20(ctx, w.node.contractBackend(), w.assetID, w.netToken.Address, w.net, w.addr, w.log)
	default:
		err = fmt.Errorf("bridge not supported for asset %d", w.assetID)
	}
	if err != nil {
		return nil, err
	}

	w.manager, err = newBridgeManager(ctx, w.assetID, w.baseChainID, bridge, w.TokenWallet.emit, w.txDB, time.Minute, w.log)
	if err != nil {
		return nil, err
	}

	return wg, nil
}

func (w *baseWallet) tip() *types.Header {
	w.tipMtx.RLock()
	defer w.tipMtx.RUnlock()
	return w.currentTip
}

// tipHeight gets the current best header's tip height.
func (w *baseWallet) tipHeight() uint64 {
	return w.tip().Number.Uint64()
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
		var defaultProviders bool
		var endpoints []string
		if len(providerDef) > 0 {
			endpoints = strings.Split(providerDef, " ")
		} else {
			endpoints = w.defaultProviders
			defaultProviders = true
		}

		if err := rpc.reconfigure(ctx, endpoints, w.compat, walletDir, defaultProviders); err != nil {
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

type genTxResult struct {
	tx                       *types.Transaction
	txType                   asset.TransactionType
	amt                      uint64
	recipient                *string
	bridgeCounterpartAssetID *uint32
	bridgeCounterpartTxID    *string
	bridgeCompletionTime     *uint64
}

// transactionGenerator is an action that uses a nonce and returns a tx, it's
// type specifier, and its value.
type transactionGenerator func(nonce *big.Int) (*genTxResult, error)

// withNonce is called with a function intended to generate a new transaction
// using the next available nonce. If the function returns a non-nil tx, the
// nonce will be treated as used, and an extendedWalletTransaction will be
// generated, stored, and queued for monitoring.
func (w *assetWallet) withNonce(ctx context.Context, f transactionGenerator) (err error) {
	w.nonceMtx.Lock()
	defer w.nonceMtx.Unlock()
	if err = nonceIsSane(w.pendingTxs, w.pendingNonceAt); err != nil {
		return err
	}
	nonce := func() *big.Int {
		n := new(big.Int).Set(w.confirmedNonceAt)
		for _, pendingTx := range w.pendingTxs {
			if pendingTx.Nonce.Cmp(n) < 0 {
				continue
			}
			if pendingTx.Nonce.Cmp(n) == 0 {
				n.Add(n, big.NewInt(1))
			} else {
				break
			}
		}
		return n
	}

	n := nonce()
	w.log.Trace("Nonce chosen for tx generator =", n)

	// Make a first attempt with our best-known nonce.
	genTxResult, submitErr := f(n)
	if submitErr != nil && strings.Contains(submitErr.Error(), "nonce too low") {
		w.log.Warnf("Too-low nonce detected. Attempting recovery")
		confirmedNonceAt, pendingNonceAt, err := w.node.nonce(ctx)
		if err != nil {
			return fmt.Errorf("error during too-low nonce recovery: %v", err)
		}
		w.confirmedNonceAt = confirmedNonceAt
		w.pendingNonceAt = pendingNonceAt
		newNonce := nonce()
		if newNonce == n {
			return fmt.Errorf("best RPC nonce %d not better than our best nonce %d", newNonce, n)
		}
		n = newNonce
		// Try again.
		genTxResult, submitErr = f(n)
		if submitErr == nil {
			w.log.Info("Nonce recovered and transaction broadcast")
		}
	}
	if submitErr != nil {
		return submitErr
	}

	if genTxResult != nil {
		et := w.extendedTx(genTxResult)
		w.pendingTxs = append(w.pendingTxs, et)
		if n.Cmp(w.pendingNonceAt) >= 0 {
			w.pendingNonceAt.Add(n, big.NewInt(1))
		}
		w.emitTransactionNote(et.WalletTransaction, true)
		w.log.Tracef("Transaction %s generated for nonce %s", et.ID, n)
	}

	return nil
}

// nonceIsSane performs sanity checks on pending txs.
func nonceIsSane(pendingTxs []*extendedWalletTx, pendingNonceAt *big.Int) error {
	if len(pendingTxs) == 0 && pendingNonceAt == nil {
		return errors.New("no pending txs and no best nonce")
	}
	var lastNonce uint64
	var numNotIndexed, confirmedTip int
	for i, pendingTx := range pendingTxs {
		if !pendingTx.savedToDB {
			return errors.New("tx database problem detected")
		}
		nonce := pendingTx.Nonce.Uint64()
		if nonce < lastNonce {
			return fmt.Errorf("pending txs not sorted")
		}
		if pendingTx.Confirmed || pendingTx.BlockNumber != 0 {
			if confirmedTip != i {
				return fmt.Errorf("confirmed tx sequence error. pending tx %s is confirmed but older txs were not", pendingTx.ID)

			}
			confirmedTip = i + 1
			continue
		}
		lastNonce = nonce
		age := pendingTx.age()
		// Only allow a handful of txs that we haven't been seen on-chain yet.
		if age > stateUpdateTick*10 {
			numNotIndexed++
		}
		if age >= txAgeOut {
			// If any tx is unindexed and aged out, wait for user to fix it.
			return fmt.Errorf("tx %s is aged out. waiting for user to take action", pendingTx.ID)
		}

	}
	if numNotIndexed >= maxUnindexedTxs {
		return fmt.Errorf("%d unindexed txs has reached the limit of %d", numNotIndexed, maxUnindexedTxs)
	}
	return nil
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

type pendingBridge struct {
	destAssetID uint32
	amount      uint64
}

// bridgeManager manages pending bridges. It periodically checks if the bridges
// are ready for completion by querying the bridge interface for completion
// data. When a bridge is ready, it emits a BridgeReadyToComplete notification.
// The manager handles adding and removing pending bridges and automatically
// starts and stops monitoring based on the presence of pending bridges.
type bridgeManager struct {
	bridge          bridge
	ctx             context.Context
	emit            *asset.WalletEmitter
	log             dex.Logger
	monitorInterval time.Duration
	txDB            txDB

	mtx            sync.RWMutex
	stopMonitoring context.CancelFunc
	pendingBridges map[string]*pendingBridge
}

func newBridgeManager(ctx context.Context, assetID, baseChainID uint32, bridge bridge, emit *asset.WalletEmitter, txDB txDB, monitorInterval time.Duration, log dex.Logger) (*bridgeManager, error) {
	manager := &bridgeManager{
		bridge:          bridge,
		ctx:             ctx,
		emit:            emit,
		txDB:            txDB,
		pendingBridges:  make(map[string]*pendingBridge),
		log:             log.SubLogger("BridgeManager"),
		monitorInterval: monitorInterval,
	}

	pendingBridges, err := txDB.getPendingBridges()
	if err != nil {
		return nil, fmt.Errorf("error getting pending bridges: %v", err)
	}

	// Filter the pending bridges that originated from assetID.
	for _, tx := range pendingBridges {
		txAssetID := baseChainID
		if tx.TokenID != nil {
			txAssetID = *tx.TokenID
		}
		if assetID != txAssetID {
			continue
		}

		if tx.BridgeCounterpartTx == nil {
			manager.log.Errorf("Bridge tx %s has no counterpart tx. This should have been added when the transaction was created", tx.ID)
			continue
		}

		manager.pendingBridges[tx.ID] = &pendingBridge{
			destAssetID: tx.BridgeCounterpartTx.AssetID,
			amount:      tx.Amount,
		}
	}

	if len(manager.pendingBridges) > 0 {
		manager.startMonitoring()
	}

	return manager, nil
}

func (bm *bridgeManager) addPendingBridge(initiationTxID string, destAssetID uint32, amount uint64) {
	bm.mtx.Lock()
	defer bm.mtx.Unlock()

	bm.pendingBridges[initiationTxID] = &pendingBridge{
		destAssetID: destAssetID,
		amount:      amount,
	}

	bm.startMonitoring()
}

// checkPendingBridges checks if completion data is available for any pending
// bridges, and if so, emits a BridgeReadyToComplete notification.
func (bm *bridgeManager) checkPendingBridges(ctx context.Context) {
	bm.mtx.RLock()
	pendingBridges := utils.CopyMap(bm.pendingBridges)
	bm.mtx.RUnlock()

	for initiationTxID, pendingBridge := range pendingBridges {
		data, err := bm.bridge.getCompletionData(ctx, initiationTxID)
		if err != nil {
			bm.log.Tracef("Unable to get mint info for bridge with initiation tx ID %s: %v", initiationTxID, err)
			continue
		}

		bm.emit.BridgeReadyToComplete(pendingBridge.destAssetID, initiationTxID, pendingBridge.amount, data)
	}
}

// markBridgeComplete is called when the destination wallet has confirmed the
// completion of a bridge. The pending bridge is removed from the manager and
// the db is updated to reflect the completion.
func (bm *bridgeManager) markBridgeComplete(initiationTxID, completionTxID string, completionTime uint64) {
	bm.mtx.Lock()
	delete(bm.pendingBridges, initiationTxID)
	if len(bm.pendingBridges) == 0 && bm.stopMonitoring != nil {
		bm.stopMonitoring()
		bm.stopMonitoring = nil
	}
	bm.mtx.Unlock()

	bridgeTx, err := bm.txDB.getTx(common.HexToHash(initiationTxID))
	if err != nil {
		bm.log.Errorf("error getting bridge tx: %v", err)
		return
	} else if bridgeTx == nil {
		bm.log.Errorf("could not find initiation tx %v", initiationTxID)
		return
	} else if bridgeTx.BridgeCounterpartTx == nil {
		bm.log.Errorf("Initiation tx %s has no counterpart tx. This should have been added when the transaction was created", initiationTxID)
		return
	}

	bridgeTx.BridgeCounterpartTx.ID = completionTxID
	bridgeTx.BridgeCounterpartTx.CompletionTime = completionTime
	if err := bm.txDB.storeTx(bridgeTx); err != nil {
		bm.log.Errorf("error storing completed bridge tx: %v", err)
	}
}

// bridgeManager.mtx MUST be held while calling this function.
func (bm *bridgeManager) startMonitoring() {
	if bm.stopMonitoring != nil { // already running
		return
	}

	ctx, cancel := context.WithCancel(bm.ctx)
	bm.stopMonitoring = cancel

	go func() {
		ticker := time.NewTicker(bm.monitorInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				bm.checkPendingBridges(ctx)
			}
		}
	}()
}

// ETHBridgeWallet is an ETHWallet that supports bridging funds to other chains.
type ETHBridgeWallet struct {
	manager *bridgeManager

	*ETHWallet
}

var _ asset.Bridger = (*ETHBridgeWallet)(nil)

// TokenBridgeWallet is a TokenWallet that supports bridging funds to other
// chains.
type TokenBridgeWallet struct {
	manager *bridgeManager

	*TokenWallet
}

var _ asset.Bridger = (*TokenBridgeWallet)(nil)

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

	supportedAssetVersions := make([]uint32, 0, 1)
	contracts := make(map[uint32]common.Address)
	gases := make(map[uint32]*dexeth.Gases)
	for ver, c := range netToken.SwapContracts {
		contracts[ver] = c.Address
		gases[ver] = &c.Gas
		supportedAssetVersions = append(supportedAssetVersions, ver)
	}

	aw := &assetWallet{
		baseWallet:         w.baseWallet,
		log:                w.baseWallet.log.SubLogger(strings.ToUpper(dex.BipIDSymbol(tokenCfg.AssetID))),
		assetID:            tokenCfg.AssetID,
		versionedContracts: contracts,
		versionedGases:     gases,
		emit:               tokenCfg.Emit,
		peersChange:        tokenCfg.PeersChange,
		findRedemptionReqs: make(map[string]*findRedemptionRequest),
		pendingApprovals:   make(map[common.Address]*pendingApproval),
		approvalCache:      make(map[common.Address]bool),
		evmify:             token.AtomicToEVM,
		atomize:            token.EVMToAtomic,
		ui:                 token.UnitInfo,
		wi: asset.WalletInfo{
			Name:              token.Name,
			SupportedVersions: supportedAssetVersions,
			UnitInfo:          token.UnitInfo,
		},
		tokenAddr:         netToken.Address,
		pendingTxCheckBal: new(big.Int),
	}

	w.baseWallet.walletsMtx.Lock()
	w.baseWallet.wallets[tokenCfg.AssetID] = aw
	w.baseWallet.walletsMtx.Unlock()

	tokenWallet := &TokenWallet{
		assetWallet: aw,
		cfg:         cfg,
		parent:      w.assetWallet,
		token:       token,
		netToken:    netToken,
	}

	usdcBridgeSupported := isUSDCBridgeSupported(tokenCfg.AssetID, w.net)
	_, polygonBridgeSupported := PolygonBridgeSupportedAsset(tokenCfg.AssetID, w.net)
	if usdcBridgeSupported || polygonBridgeSupported {
		return &TokenBridgeWallet{
			TokenWallet: tokenWallet,
		}, nil
	}

	return tokenWallet, nil
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

func (w *assetWallet) maxOrder(lotSize uint64, maxFeeRate uint64, initAssetVer,
	redeemAssetVer, redeemAssetID uint32, feeWallet *assetWallet) (*asset.SwapEstimate, error) {
	balance, err := w.Balance()
	if err != nil {
		return nil, err
	}
	initContractVer := contractVersion(initAssetVer)
	// Get the refund gas.
	if g := w.gases(initContractVer); g == nil {
		return nil, fmt.Errorf("no gas table")
	}

	g, err := w.initGasEstimate(1, initContractVer, contractVersion(redeemAssetVer), redeemAssetID, maxFeeRate)
	liveEstimateFailed := errors.Is(err, LiveEstimateFailedError)
	if err != nil && !liveEstimateFailed {
		return nil, fmt.Errorf("gasEstimate error: %w", err)
	}

	refundCost := g.Refund * maxFeeRate
	oneFee := g.oneGas * maxFeeRate
	feeReservesPerLot := oneFee + refundCost
	var lots uint64
	if feeWallet == nil {
		lots = balance.Available / (lotSize + feeReservesPerLot)
	} else { // token
		lots = balance.Available / lotSize
		parentBal, err := feeWallet.Balance()
		if err != nil {
			return nil, fmt.Errorf("error getting base chain balance: %w", err)
		}
		feeLots := parentBal.Available / feeReservesPerLot
		if feeLots < lots {
			w.log.Infof("MaxOrder reducing lots because of low fee reserves: %d -> %d", lots, feeLots)
			lots = feeLots
		}
	}

	if lots < 1 {
		return &asset.SwapEstimate{
			FeeReservesPerLot: feeReservesPerLot,
		}, nil
	}
	return w.estimateSwap(lots, lotSize, maxFeeRate, initContractVer, feeReservesPerLot)
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
	maxEst, err := w.maxOrder(req.LotSize, req.MaxFeeRate, req.AssetVersion,
		req.RedeemVersion, req.RedeemAssetID, feeWallet)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est, err := w.estimateSwap(req.Lots, req.LotSize, req.MaxFeeRate,
		contractVersion(req.AssetVersion), maxEst.FeeReservesPerLot)
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
func (w *assetWallet) SingleLotSwapRefundFees(assetVer uint32, feeSuggestion uint64, _ bool) (swapFees uint64, refundFees uint64, err error) {
	contractVer := contractVersion(assetVer)
	g := w.gases(contractVer)
	if g == nil {
		return 0, 0, fmt.Errorf("no gases known for %d contract version %d", w.assetID, contractVersion(assetVer))
	}
	return g.Swap * feeSuggestion, g.Refund * feeSuggestion, nil
}

// estimateSwap prepares an *asset.SwapEstimate. The estimate does not include
// funds that might be locked for refunds.
func (w *assetWallet) estimateSwap(
	lots, lotSize uint64, maxFeeRate uint64, contractVer uint32, feeReservesPerLot uint64,
) (*asset.SwapEstimate, error) {

	if lots == 0 {
		return &asset.SwapEstimate{
			FeeReservesPerLot: feeReservesPerLot,
		}, nil
	}

	feeRate, err := w.currentFeeRate(w.ctx)
	if err != nil {
		return nil, err
	}
	feeRateGwei := dexeth.WeiToGweiCeil(feeRate)
	// This is an estimate, so we use the (lower) live gas estimates.
	oneSwap, err := w.estimateInitGas(w.ctx, 1, contractVer)
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
		RealisticWorstCase: oneGasMax * feeRateGwei,
		RealisticBestCase:  oneSwap * feeRateGwei, // not even batch, just perfect match
		FeeReservesPerLot:  feeReservesPerLot,
	}, nil
}

// gases gets the gas table for the specified contract version.
func (w *assetWallet) gases(contractVer uint32) *dexeth.Gases {
	return gases(contractVer, w.versionedGases)
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (w *assetWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	oneRedeem, nRedeem, err := w.redeemGas(int(req.Lots), contractVersion(req.AssetVersion))
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
func (w *assetWallet) SingleLotRedeemFees(assetVer uint32, feeSuggestion uint64) (fees uint64, err error) {
	g := w.gases(contractVersion(assetVer))
	if g == nil {
		return 0, fmt.Errorf("no gases known for %d, constract version %d", w.assetID, contractVersion(assetVer))
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
	if ord.MaxFeeRate < dexeth.MinGasTipCap {
		return nil, nil, 0, fmt.Errorf("%v: server's max fee rate is lower than our min gas tip cap. %d < %d",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, dexeth.MinGasTipCap)
	}

	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	contractVer := contractVersion(ord.AssetVersion)

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), contractVer,
		ord.RedeemVersion, ord.RedeemAssetID, ord.MaxFeeRate)
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
	if ord.MaxFeeRate < dexeth.MinGasTipCap {
		return nil, nil, 0, fmt.Errorf("%v: server's max fee rate is lower than our min gas tip cap. %d < %d",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, dexeth.MinGasTipCap)
	}

	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, 0, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	approvalStatus, err := w.swapContractApprovalStatus(ord.AssetVersion)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting approval status: %v", err)
	}
	switch approvalStatus {
	case asset.NotApproved:
		return nil, nil, 0, asset.ErrUnapprovedToken
	case asset.Pending:
		return nil, nil, 0, asset.ErrApprovalPending
	case asset.Approved:
	default:
		return nil, nil, 0, fmt.Errorf("unknown approval status %d", approvalStatus)
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), contractVersion(ord.AssetVersion),
		ord.RedeemVersion, ord.RedeemAssetID, ord.MaxFeeRate)
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

	g, err := w.initGasEstimate(1, ord.AssetVersion, ord.RedeemVersion, ord.RedeemAssetID, ord.MaxFeeRate)
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

	approvalStatus, err := w.swapContractApprovalStatus(ord.AssetVersion)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting approval status: %v", err)
	}
	switch approvalStatus {
	case asset.NotApproved:
		return nil, nil, 0, asset.ErrUnapprovedToken
	case asset.Pending:
		return nil, nil, 0, asset.ErrApprovalPending
	case asset.Approved:
	default:
		return nil, nil, 0, fmt.Errorf("unknown approval status %d", approvalStatus)
	}

	g, err := w.initGasEstimate(1, ord.AssetVersion,
		ord.RedeemVersion, ord.RedeemAssetID, ord.MaxFeeRate)
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
func (w *assetWallet) initGasEstimate(n int, initContractVer, redeemContractVer, redeemAssetID uint32, feeRateGwei uint64) (est *gasEstimate, err error) {
	est = new(gasEstimate)

	// Get the refund gas.
	if g := w.gases(initContractVer); g == nil {
		return nil, fmt.Errorf("no gas table")
	} else { // scoping g
		est.Refund = g.Refund
	}

	est.Swap, est.nSwap, err = w.swapGas(n, initContractVer, feeRateGwei)
	if err != nil && !errors.Is(err, LiveEstimateFailedError) {
		return nil, err
	}
	// Could be LiveEstimateFailedError. Still populate static estimates if we
	// couldn't get live. Error is still propagated.
	est.oneGas = est.Swap
	est.nGas = est.nSwap

	if redeemW := w.wallet(redeemAssetID); redeemW != nil {
		var er error
		est.Redeem, est.nRedeem, er = redeemW.redeemGas(n, redeemContractVer)
		if err != nil {
			return nil, fmt.Errorf("error calculating fee-family redeem gas: %w", er)
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
func (w *assetWallet) swapGas(n int, contractVer uint32, feeRateGwei uint64) (oneSwapGas, nSwapGas uint64, err error) {
	g := w.gases(contractVer)
	if g == nil {
		return 0, 0, fmt.Errorf("no gases known for %d contract version %d", w.assetID, contractVer)
	}
	oneSwapGas = g.Swap

	// The amount we can estimate and ultimately the amount we can use in a
	// single transaction is limited by the block gas limit or the tx gas
	// limit. Core will use the largest gas among all versions when
	// determining the maximum number of swaps that can be in one
	// transaction. Limit our gas estimate to the same number of swaps/redeems..
	maxSwapsPerTx, err := w.maxSwapsOrRedeems(g.Swap, g.SwapAdd, feeRateGwei)
	if err != nil {
		return 0, 0, fmt.Errorf("error calculating max swaps: %w", err)
	}

	if nFull := n / maxSwapsPerTx; nFull > 0 {
		fullGas := g.SwapN(maxSwapsPerTx)
		if fullGasEst, err := w.estimateInitGas(w.ctx, maxSwapsPerTx, contractVer); err != nil {
			w.log.Errorf("(%d) error estimating swap gas for full txs: %v", w.assetID, err)
			return 0, 0, errors.Join(err, LiveEstimateFailedError)
		} else if fullGasEst > fullGas {
			w.log.Warnf("%d-tx (full) swap gas estimate %d is greater than the server's configured value %d. Using live estimate + 10%%.",
				nFull, fullGasEst, fullGas)
			fullGas = fullGasEst * 11 / 10
		}
		nSwapGas = uint64(nFull) * fullGas
	}
	if nRemain := n & maxSwapsPerTx; nRemain > 0 {
		remainGas := g.SwapN(nRemain)
		if remainGasEst, err := w.estimateInitGas(w.ctx, nRemain, contractVer); err != nil {
			w.log.Errorf("(%d) error estimating swap gas for remainder: %v", w.assetID, err)
			return 0, 0, errors.Join(err, LiveEstimateFailedError)
		} else if remainGasEst > remainGas {
			w.log.Warnf("%d-tx swap gas estimate %d is greater than the server's configured value %d. Using live estimate + 10%%.",
				nRemain, remainGasEst, remainGas)
			remainGas = remainGasEst * 11 / 10
		}
		nSwapGas += remainGas
	}
	return
}

// redeemGas gets an accurate estimate for redemption gas. We allow a DEX server
// some latitude in adjusting the redemption gas, up to 2x our local estimate.
func (w *assetWallet) redeemGas(n int, contractVer uint32) (oneGas, nGas uint64, err error) {
	g := w.gases(contractVer)
	if g == nil {
		return 0, 0, fmt.Errorf("no gas table for redemption asset %d", w.assetID)
	}
	redeemGas := g.Redeem
	// Not concerned with the accuracy of nGas. It's never used outside of
	// best case estimates.
	return redeemGas, g.RedeemN(n), nil
}

// approvalGas gets the best available estimate for an approval tx, which is
// the greater of the asset's registered value and a live estimate. It is an
// error if a live estimate cannot be retrieved, which will be the case if the
// user's eth balance is insufficient to cover tx fees for the approval.
func (w *assetWallet) approvalGas(newGas *big.Int, contractVer uint32) (uint64, error) {
	ourGas := w.gases(contractVer)
	if ourGas == nil {
		return 0, fmt.Errorf("no gases known for %d contract version %d", w.assetID, contractVer)
	}

	approveGas := ourGas.Approve

	if approveEst, err := w.estimateApproveGas(contractVer, newGas); err != nil {
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
	txHash  common.Hash
	locator []byte
	// expiration and value can be determined with a blockchain
	// lookup, but we cache these values to avoid this.
	expiration   time.Time
	value        uint64
	contractVer  uint32
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
	return dexeth.EncodeContractData(r.contractVer, r.locator)
}

// String returns a string representation of the swapReceipt. The secret hash
// and contract address should be included in this to facilitate a manual refund
// since the secret hash identifies the swap in the contract (for v0). Although
// the user can pick this information from the transaction's "to" address and
// the calldata, this simplifies the process.
func (r *swapReceipt) String() string {
	return fmt.Sprintf("{ tx hash: %s, contract address: %s, locator: %x }",
		r.txHash, r.contractAddr, r.locator)
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

	contractVer := contractVersion(swaps.AssetVersion)
	n := len(swaps.Contracts)
	oneSwap, nSwap, err := w.swapGas(n, contractVer, swaps.FeeRate)
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

	maxFeeRate := dexeth.GweiToWei(swaps.FeeRate)
	_, tipRate, err := w.currentNetworkFees(w.ctx)
	if err != nil {
		return fail("Swap: failed to get network tip cap: %w", err)
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, gasLimit, maxFeeRate, tipRate, contractVer)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	txHash := tx.Hash()
	receipts := make([]asset.Receipt, 0, n)
	for _, swap := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			expiration:   time.Unix(int64(swap.LockTime), 0),
			value:        swap.Value,
			txHash:       txHash,
			locator:      acToLocator(contractVer, swap, dexeth.GweiToWei(swap.Value), w.addr),
			contractVer:  contractVer,
			contractAddr: w.versionedContracts[contractVer].String(),
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

// acToLocator converts the asset.Contract to a version-specific locator.
func acToLocator(contractVer uint32, swap *asset.Contract, evmValue *big.Int, from common.Address) []byte {
	switch contractVer {
	case 0:
		return swap.SecretHash
	case 1:
		var secretHash [32]byte
		copy(secretHash[:], swap.SecretHash)
		return (&dexeth.SwapVector{
			From:       from,
			To:         common.HexToAddress(swap.Address),
			Value:      evmValue,
			SecretHash: secretHash,
			LockTime:   swap.LockTime,
		}).Locator()
	default:
		panic("need to add a version in acToLocator")
	}
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
	contractVer := contractVersion(swaps.AssetVersion)
	oneSwap, nSwap, err := w.swapGas(n, contractVer, swaps.FeeRate)
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

	maxFeeRate := dexeth.GweiToWei(swaps.FeeRate)
	_, tipRate, err := w.currentNetworkFees(w.ctx)
	if err != nil {
		return fail("Swap: failed to get network tip cap: %w", err)
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, gasLimit, maxFeeRate, tipRate, contractVer)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	if w.netToken.SwapContracts[swaps.AssetVersion] == nil {
		return fail("unable to find contract address for asset %d contract version %d", w.assetID, swaps.AssetVersion)
	}

	contractAddr := w.netToken.SwapContracts[contractVer].Address.String()

	txHash := tx.Hash()
	receipts := make([]asset.Receipt, 0, n)
	for _, swap := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			expiration:   time.Unix(int64(swap.LockTime), 0),
			value:        swap.Value,
			txHash:       txHash,
			locator:      acToLocator(contractVer, swap, w.evmify(swap.Value), w.addr),
			contractVer:  contractVer,
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
// redemption. The nonceOverride parameter is used to specify a specific nonce
// to be used for the redemption transaction. It is needed when resubmitting a
// redemption with a fee too low to be mined.
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
	locators := make([][]byte, 0, n)
	var redeemedValue uint64
	for i, redemption := range form.Redemptions {
		// NOTE: redemption.Spends.SecretHash is a dup of the hash extracted
		// from redemption.Spends.Contract. Even for scriptable UTXO assets, the
		// redeem script in this Contract field is redundant with the SecretHash
		// field as ExtractSwapDetails can be applied to extract the hash.
		ver, locator, err := dexeth.DecodeContractData(redemption.Spends.Contract)
		if err != nil {
			return fail(fmt.Errorf("Redeem: invalid versioned swap contract data: %w", err))
		}

		locators = append(locators, locator)
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
		redeemable, err := w.isRedeemable(locator, secret, ver)
		if err != nil {
			return fail(fmt.Errorf("Redeem: failed to check if swap is redeemable: %w", err))
		}
		if !redeemable {
			return fail(fmt.Errorf("Redeem: version %d locator %x not redeemable with secret %x",
				ver, locator, secret))
		}

		status, vector, err := w.statusAndVector(w.ctx, locator, contractVer)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error finding swap state: %w", err)
		}
		if status.Step != dexeth.SSInitiated {
			return nil, nil, 0, asset.ErrSwapNotInitiated
		}
		redeemedValue += w.atomize(vector.Value)
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
	baseFee, tipRate, err := w.currentNetworkFees(w.ctx)
	if err != nil {
		return fail(fmt.Errorf("Error getting net fee state: %w", err))
	}
	baseFeeGwei := dexeth.WeiToGweiCeil(baseFee)
	if baseFeeGwei > form.FeeSuggestion {
		additionalFundsNeeded := (2 * baseFeeGwei * gasLimit) - originalFundsReserved
		if bal.Available > additionalFundsNeeded {
			gasFeeCap = 2 * baseFeeGwei
		} else {
			gasFeeCap = (bal.Available + originalFundsReserved) / gasLimit
		}
		w.log.Warnf("base fee %d > server max fee rate %d. using %d as gas fee cap for redemption", baseFeeGwei, form.FeeSuggestion, gasFeeCap)
	}

	tx, err := w.redeem(w.ctx, form.Redemptions, gasFeeCap, tipRate, gasLimit, contractVer)
	if err != nil {
		return fail(fmt.Errorf("Redeem: redeem error: %w", err))
	}

	txHash := tx.Hash()

	txs := make([]dex.Bytes, len(form.Redemptions))
	for i := range txs {
		txs[i] = txHash[:]
	}

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
	return bal, w.withTokenContractor(w.assetID, dexeth.ContractVersionERC20, func(c tokenContractor) error {
		bal, err = c.balance(w.ctx)
		return err
	})
}

// tokenAllowance checks the amount of tokens that the swap contract is approved
// to spend on behalf of the account handled by the wallet.
func (w *assetWallet) tokenAllowance(contractVer uint32) (allowance *big.Int, err error) {
	return allowance, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
		allowance, err = c.allowance(w.ctx)
		return err
	})
}

// approveToken approves the token swap contract to spend tokens on behalf of
// account handled by the wallet.
func (w *assetWallet) approveToken(ctx context.Context, amount *big.Int, gasLimit uint64, maxFeeRate, tipRate *big.Int, contractVer uint32) (*types.Transaction, error) {
	res := &genTxResult{
		tx:     nil,
		txType: asset.ApproveToken,
		amt:    w.atomize(amount),
	}

	return res.tx, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(w.ctx, 0, gasLimit, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, fmt.Errorf("addSignerToOpts error: %w", err)
		}

		return res, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
			res.tx, err = c.approve(txOpts, amount)
			if err != nil {
				return err
			}
			w.log.Infof("Approval sent for %s at token address %s, nonce = %s, txID = %s",
				dex.BipIDSymbol(w.assetID), c.tokenAddress(), txOpts.Nonce, res.tx.Hash().Hex())
			return nil
		})
	})
}

func (w *assetWallet) approvalStatus(contract common.Address, checkAllowance func() (*big.Int, error)) (asset.ApprovalStatus, error) {
	if w.assetID == w.baseChainID {
		return asset.Approved, nil
	}

	// If the result has been cached, return what is in the cache.
	// The cache is cleared if an approval/unapproval tx is done.
	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	if approved, cached := w.approvalCache[contract]; cached {
		if approved {
			return asset.Approved, nil
		} else {
			return asset.NotApproved, nil
		}
	}

	if _, pending := w.pendingApprovals[contract]; pending {
		return asset.Pending, nil
	}

	currentAllowance, err := checkAllowance()
	if err != nil {
		return asset.NotApproved, fmt.Errorf("error retrieving current allowance: %w", err)
	}
	if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) >= 0 {
		w.approvalCache[contract] = true
		return asset.Approved, nil
	}
	w.approvalCache[contract] = false

	return asset.NotApproved, nil
}

func (w *TokenWallet) swapContractApprovalStatus(assetVer uint32) (asset.ApprovalStatus, error) {
	contract, found := w.versionedContracts[assetVer]
	if !found {
		return asset.NotApproved, fmt.Errorf("no contract address found for asset %d contract version %d", w.assetID, assetVer)
	}
	return w.approvalStatus(contract, func() (*big.Int, error) {
		return w.tokenAllowance(assetVer)
	})
}

// ApproveToken sends an approval transaction for a specific version of
// the token's swap contract. An error is returned if an approval has
// already been done or is pending. The onConfirm callback is called
// when the approval transaction is confirmed.
func (w *TokenWallet) ApproveToken(assetVer uint32, onConfirm func()) (string, error) {
	contract, found := w.versionedContracts[assetVer]
	if !found {
		return "", fmt.Errorf("no contract address found for asset %d contract version %d", w.assetID, assetVer)
	}

	approvalStatus, err := w.swapContractApprovalStatus(assetVer)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus == asset.Approved {
		return "", fmt.Errorf("token is already approved")
	}
	if approvalStatus == asset.Pending {
		return "", asset.ErrApprovalPending
	}

	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)
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

	tx, err := w.approveToken(w.ctx, unlimitedAllowance, approvalGas, maxFeeRate, tipRate, assetVer)
	if err != nil {
		return "", fmt.Errorf("error approving token: %w", err)
	}

	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	delete(w.approvalCache, contract)
	w.pendingApprovals[contract] = &pendingApproval{
		txHash:    tx.Hash(),
		onConfirm: onConfirm,
	}

	return tx.Hash().Hex(), nil
}

// UnapproveToken removes the approval for a specific version of the token's
// swap contract.
func (w *TokenWallet) UnapproveToken(assetVer uint32, onConfirm func()) (string, error) {
	contract, found := w.versionedContracts[assetVer]
	if !found {
		return "", fmt.Errorf("no contract address found for asset %d contract version %d", w.assetID, assetVer)
	}

	approvalStatus, err := w.swapContractApprovalStatus(assetVer)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus == asset.NotApproved {
		return "", fmt.Errorf("token is not approved")
	}
	if approvalStatus == asset.Pending {
		return "", asset.ErrApprovalPending
	}

	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)
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

	tx, err := w.approveToken(w.ctx, big.NewInt(0), approvalGas, maxFeeRate, tipRate, assetVer)
	if err != nil {
		return "", fmt.Errorf("error unapproving token: %w", err)
	}

	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	delete(w.approvalCache, contract)
	w.pendingApprovals[contract] = &pendingApproval{
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

	feeRateGwei, err := w.recommendedMaxFeeRateGwei(w.ctx)
	if err != nil {
		return 0, fmt.Errorf("error calculating approval fee rate: %w", err)
	}

	return approvalGas * feeRateGwei, nil
}

// ApprovalStatus returns the approval status for each version of the
// token's swap contract.
func (w *TokenWallet) ApprovalStatus() map[uint32]asset.ApprovalStatus {
	statuses := map[uint32]asset.ApprovalStatus{}
	for _, assetVer := range w.wi.SupportedVersions {
		status, err := w.swapContractApprovalStatus(assetVer)
		if err != nil {
			w.log.Errorf("error checking approval status for swap contract version %d: %w", assetVer, err)
			continue
		}
		statuses[assetVer] = status
	}

	return statuses
}

func (w *assetWallet) bridgeContractApprovalStatus(ctx context.Context, bridge bridge) (asset.ApprovalStatus, error) {
	if !bridge.requiresBridgeContractApproval() {
		return asset.Approved, nil
	}
	return w.approvalStatus(bridge.bridgeContractAddr(), func() (*big.Int, error) {
		return bridge.bridgeContractAllowance(ctx)
	})
}

// BridgeContractApprovalStatus returns whether the bridge contract has been
// approved to spend tokens on behalf of the account handled by the wallet.
func (w *TokenBridgeWallet) BridgeContractApprovalStatus(ctx context.Context) (asset.ApprovalStatus, error) {
	return w.bridgeContractApprovalStatus(ctx, w.manager.bridge)
}

// BridgeContractApprovalStatus returns whether the bridge contract has been
// approved to spend tokens on behalf of the account handled by the wallet.
func (w *ETHBridgeWallet) BridgeContractApprovalStatus(ctx context.Context) (asset.ApprovalStatus, error) {
	return w.bridgeContractApprovalStatus(ctx, w.manager.bridge)
}

func (w *assetWallet) approveBridgeContract(ctx context.Context, bridge bridge) (string, error) {
	approvalStatus, err := w.bridgeContractApprovalStatus(ctx, bridge)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus == asset.Approved {
		return "", fmt.Errorf("bridge contract is already approved")
	}
	if approvalStatus == asset.Pending {
		return "", fmt.Errorf("approval is already pending")
	}

	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)
	approvalGas, err := w.approvalGas(unlimitedAllowance, dexeth.ContractVersionNewest)
	if err != nil {
		return "", fmt.Errorf("error calculating approval gas: %w", err)
	}

	ethBal, err := w.balance()
	if err != nil {
		return "", fmt.Errorf("error getting eth balance: %w", err)
	}
	if ethBal.Available < approvalGas*feeRateGwei {
		return "", fmt.Errorf("insufficient fee balance for approval. required: %d, available: %d",
			approvalGas*feeRateGwei, ethBal.Available)
	}

	var txID string
	return txID, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(ctx, 0, approvalGas, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, fmt.Errorf("txOpts error: %w", err)
		}

		tx, err := bridge.approveBridgeContract(txOpts, unlimitedAllowance)
		if err != nil {
			return nil, fmt.Errorf("error approving bridge contract: %w", err)
		}

		txID = tx.Hash().Hex()

		w.approvalsMtx.Lock()
		delete(w.approvalCache, bridge.bridgeContractAddr())
		w.pendingApprovals[bridge.bridgeContractAddr()] = &pendingApproval{
			txHash:    tx.Hash(),
			onConfirm: func() {},
		}
		w.approvalsMtx.Unlock()

		return &genTxResult{
			tx:     tx,
			txType: asset.ApproveToken,
			amt:    w.atomize(unlimitedAllowance),
		}, nil
	})
}

// ApproveBridgeContract approves the bridge contract to spend tokens on behalf
// of the account handled by the wallet.
func (w *ETHBridgeWallet) ApproveBridgeContract(ctx context.Context) (string, error) {
	return w.approveBridgeContract(ctx, w.manager.bridge)
}

// ApproveBridgeContract approves the bridge contract to spend tokens on behalf
// of the account handled by the wallet.
func (w *TokenBridgeWallet) ApproveBridgeContract(ctx context.Context) (string, error) {
	return w.approveBridgeContract(ctx, w.manager.bridge)
}

func (w *assetWallet) unapproveBridgeContract(ctx context.Context, bridge bridge) (string, error) {
	approvalStatus, err := w.bridgeContractApprovalStatus(ctx, bridge)
	if err != nil {
		return "", fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus != asset.Approved {
		return "", fmt.Errorf("bridge contract is not approved")
	}

	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating approval fee rate: %w", err)
	}
	feeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)
	approvalGas, err := w.approvalGas(new(big.Int), dexeth.ContractVersionNewest)
	if err != nil {
		return "", fmt.Errorf("error calculating approval gas: %w", err)
	}

	ethBal, err := w.balance()
	if err != nil {
		return "", fmt.Errorf("error getting eth balance: %w", err)
	}
	if ethBal.Available < approvalGas*feeRateGwei {
		return "", fmt.Errorf("insufficient fee balance for approval. required: %d, available: %d",
			approvalGas*feeRateGwei, ethBal.Available)
	}

	var txID string
	return txID, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(ctx, 0, approvalGas, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, fmt.Errorf("txOpts error: %w", err)
		}

		tx, err := bridge.approveBridgeContract(txOpts, new(big.Int))
		if err != nil {
			return nil, fmt.Errorf("error approving bridge contract: %w", err)
		}

		txID = tx.Hash().Hex()

		w.approvalsMtx.Lock()
		delete(w.approvalCache, bridge.bridgeContractAddr())
		w.pendingApprovals[bridge.bridgeContractAddr()] = &pendingApproval{
			txHash:    tx.Hash(),
			onConfirm: func() {},
		}
		w.approvalsMtx.Unlock()

		return &genTxResult{
			tx:     tx,
			txType: asset.ApproveToken,
			amt:    w.atomize(new(big.Int)),
		}, nil
	})
}

// UnapproveBridgeContract removes the approval for the bridge contract.
func (w *ETHBridgeWallet) UnapproveBridgeContract(ctx context.Context) (string, error) {
	return w.unapproveBridgeContract(ctx, w.manager.bridge)
}

// UnapproveBridgeContract removes the approval for the bridge contract.
func (w *TokenBridgeWallet) UnapproveBridgeContract(ctx context.Context) (string, error) {
	return w.unapproveBridgeContract(ctx, w.manager.bridge)
}

func (w *assetWallet) initiateBridge(ctx context.Context, amt uint64, dest uint32, bridge bridge) (txID string, completionTxRequired bool, err error) {
	approvalStatus, err := w.bridgeContractApprovalStatus(ctx, bridge)
	if err != nil {
		return "", false, fmt.Errorf("error checking approval status: %w", err)
	}
	if approvalStatus != asset.Approved {
		return "", false, fmt.Errorf("bridge contract is not approved")
	}

	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(ctx)
	if err != nil {
		return "", false, fmt.Errorf("error calculating bridge fee rate: %w", err)
	}

	return txID, completionTxRequired, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(ctx, 0, bridge.initiateBridgeGas(), maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, fmt.Errorf("txOpts error: %w", err)
		}

		var tx *types.Transaction
		tx, completionTxRequired, err = bridge.initiateBridge(txOpts, dest, w.evmify(amt))
		if err != nil {
			return nil, err
		}

		var bridgeCounterpartyTxID *string
		var bridgeCompletionTime *uint64
		if !completionTxRequired {
			id := asset.NoCompletionRequiredBridgeTxID
			bridgeCounterpartyTxID = &id
			tip := w.tip()
			bridgeCompletionTime = &tip.Time
		}

		txID = tx.Hash().Hex()

		return &genTxResult{
			tx:                       tx,
			txType:                   asset.InitiateBridge,
			amt:                      amt,
			bridgeCounterpartAssetID: &dest,
			bridgeCounterpartTxID:    bridgeCounterpartyTxID,
			bridgeCompletionTime:     bridgeCompletionTime,
		}, nil
	})
}

// InitiateBridge initiates bridging funds from one chain to another.
func (w *ETHBridgeWallet) InitiateBridge(ctx context.Context, amt uint64, dest uint32) (string, error) {
	txID, completionTxRequired, err := w.initiateBridge(ctx, amt, dest, w.manager.bridge)
	if err != nil {
		return "", err
	}

	if completionTxRequired {
		w.manager.addPendingBridge(txID, dest, amt)
	}

	return txID, nil
}

// Bridge initiates bridging funds from one chain to another.
func (w *TokenBridgeWallet) InitiateBridge(ctx context.Context, amt uint64, dest uint32) (string, error) {
	txID, completionTxRequired, err := w.initiateBridge(ctx, amt, dest, w.manager.bridge)
	if err != nil {
		return "", err
	}

	if completionTxRequired {
		w.manager.addPendingBridge(txID, dest, amt)
	}

	return txID, nil
}

// MarkBridgeComplete is called when the bridge completion transaction has
// been confirmed on the destination chain.
func (w *ETHBridgeWallet) MarkBridgeComplete(initiationTxID, completionTxID string, completionTime uint64) {
	w.manager.markBridgeComplete(initiationTxID, completionTxID, completionTime)
}

// MarkBridgeComplete is called when the bridge completion transaction has
// been confirmed on the destination chain.
func (w *TokenBridgeWallet) MarkBridgeComplete(initiationTxID, completionTxID string, completionTime uint64) {
	w.manager.markBridgeComplete(initiationTxID, completionTxID, completionTime)
}

func (w *assetWallet) pendingBridges() ([]*asset.WalletTransaction, error) {
	pendingBridges, err := w.txDB.getPendingBridges()
	if err != nil {
		return nil, fmt.Errorf("error getting pending bridges: %w", err)
	}

	wt := make([]*asset.WalletTransaction, 0, len(pendingBridges))
	for _, pb := range pendingBridges {
		wt = append(wt, pb.WalletTransaction)
	}

	return wt, nil
}

// PendingBridges lists all uncompleted bridge transactions on the blockchain.
func (w *ETHBridgeWallet) PendingBridges() ([]*asset.WalletTransaction, error) {
	return w.pendingBridges()
}

// PendingBridges lists all uncompleted bridge transactions on the blockchain.
func (w *TokenBridgeWallet) PendingBridges() ([]*asset.WalletTransaction, error) {
	return w.pendingBridges()
}

func (w *assetWallet) bridgeHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	var refHash *common.Hash
	if refID != nil {
		rh := common.HexToHash(*refID)
		refHash = &rh
	}
	return w.txDB.getBridges(n, refHash, past)
}

// BridgeHistory retrieves a record of bridge initiations on the blockchain.
//
// When n <= 0:
// - getTxs returns all transactions, ordered in reverse by completion time.
//
// When no refID is provided:
// - Returns the n most recent bridge transactions, ordered in reverse by completion time.
// - The 'past' argument is ignored.
//
// When refID is provided:
// - Returns n transactions, starting with the referenced transaction.
// - If past = false: Results are ordered by completion time (ascending).
// - If past = true: Results are ordered by completion time (descending).
// - The referenced transaction is included in the results.
// - Returns asset.CoinNotFoundError if the refID is not found.
func (w *ETHBridgeWallet) BridgeHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	return w.bridgeHistory(n, refID, past)
}

// BridgeHistory retrieves a record of bridge initiations on the blockchain.
//
// When n <= 0:
// - getTxs returns all transactions, ordered in reverse by completion time.
//
// When no refID is provided:
// - Returns the n most recent bridge transactions, ordered in reverse by completion time.
// - The 'past' argument is ignored.
//
// When refID is provided:
// - Returns n transactions, starting with the referenced transaction.
// - If past = false: Results are ordered by completion time (ascending).
// - If past = true: Results are ordered by completion time (descending).
// - The referenced transaction is included in the results.
// - Returns asset.CoinNotFoundError if the refID is not found.
func (w *TokenBridgeWallet) BridgeHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	return w.bridgeHistory(n, refID, past)
}

func (w *assetWallet) completeBridge(ctx context.Context, data []byte, bridgeTx *asset.BridgeCounterpartTx, amount uint64, bridge bridge) (txID string, err error) {
	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(ctx)
	if err != nil {
		return "", fmt.Errorf("error calculating bridge fee rate: %w", err)
	}

	return txID, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(ctx, 0, bridge.completeBridgeGas(), maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, fmt.Errorf("txOpts error: %w", err)
		}

		tx, err := bridge.completeBridge(txOpts, data)
		if err != nil {
			return nil, err
		}

		txID = tx.Hash().Hex()

		return &genTxResult{
			tx:                       tx,
			txType:                   asset.CompleteBridge,
			amt:                      amount,
			bridgeCounterpartAssetID: &bridgeTx.AssetID,
			bridgeCounterpartTxID:    &bridgeTx.ID,
		}, nil
	})
}

func (w *assetWallet) getBridgeCompletion(initiationTxID string) (*extendedWalletTx, error) {
	// Check if the completion is still pending
	w.nonceMtx.RLock()
	for _, tx := range w.pendingTxs {
		if tx.Type == asset.CompleteBridge && tx.BridgeCounterpartTx != nil && tx.BridgeCounterpartTx.ID == initiationTxID {
			w.nonceMtx.RUnlock()
			return tx, nil
		}
	}
	w.nonceMtx.RUnlock()

	// If not pending, check if it's in the DB
	return w.txDB.getBridgeCompletion(initiationTxID)
}

func (w *assetWallet) completeBrigdeIfNeeded(ctx context.Context, bridgeTx *asset.BridgeCounterpartTx, amount uint64, data []byte, bridge bridge) error {
	wt, err := w.getBridgeCompletion(bridgeTx.ID)
	if err != nil {
		return fmt.Errorf("error getting bridge completion: %w", err)
	}
	if wt != nil {
		if wt.Confirmed {
			w.emitBridgeCompletedNote(wt.WalletTransaction)
		}
		return nil
	}

	_, err = w.completeBridge(ctx, data, bridgeTx, amount, bridge)
	if err != nil {
		return fmt.Errorf("error completing bridge: %w", err)
	}

	return nil
}

// CompleteBridges completes a bridge by submitting a transaction that mints
// or unlocks coins on the destination chain.
func (w *ETHBridgeWallet) CompleteBridge(ctx context.Context, bridgeTx *asset.BridgeCounterpartTx, amount uint64, data []byte) error {
	return w.completeBrigdeIfNeeded(ctx, bridgeTx, amount, data, w.manager.bridge)
}

// CompleteBridges completes a bridge by submitting a transaction that mints
// or unlocks coins on the destination chain.
func (w *TokenBridgeWallet) CompleteBridge(ctx context.Context, bridgeTx *asset.BridgeCounterpartTx, amount uint64, data []byte) error {
	return w.completeBrigdeIfNeeded(ctx, bridgeTx, amount, data, w.manager.bridge)
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
func (w *TokenWallet) ReserveNRedemptions(n uint64, assetVer uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(contractVersion(assetVer))
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
func (w *ETHWallet) ReserveNRefunds(n uint64, assetVer uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(contractVersion(assetVer))
	if g == nil {
		return 0, errors.New("no gas table")
	}
	return reserveNRefunds(w.assetWallet, n, maxFeeRate, g)
}

// ReserveNRefunds locks funds for doing refunds. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *TokenWallet) ReserveNRefunds(n uint64, assetVer uint32, maxFeeRate uint64) (uint64, error) {
	g := w.gases(contractVersion(assetVer))
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

	version, locator, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("AuditContract: failed to decode contract data: %w", err)
	}

	var val uint64
	var participant string
	var lockTime time.Time
	var secretHashB []byte
	switch version {
	case 0:
		initiations, err := dexeth.ParseInitiateDataV0(tx.Data())
		if err != nil {
			return nil, fmt.Errorf("AuditContract: failed to parse initiate data: %w", err)
		}

		secretHash, err := dexeth.ParseV0Locator(locator)
		if err != nil {
			return nil, fmt.Errorf("error parsing v0 locator (%x): %w", locator, err)
		}

		initiation, ok := initiations[secretHash]
		if !ok {
			return nil, errors.New("AuditContract: tx does not initiate secret hash")
		}
		val = w.atomize(initiation.Value)
		participant = initiation.Participant.String()
		lockTime = initiation.LockTime
		secretHashB = secretHash[:]
	case 1:
		vec, err := dexeth.ParseV1Locator(locator)
		if err != nil {
			return nil, err
		}
		tokenAddr, txVectors, err := dexeth.ParseInitiateDataV1(tx.Data())
		if err != nil {
			return nil, fmt.Errorf("AuditContract: failed to parse initiate data: %w", err)
		}
		if tokenAddr != w.tokenAddr {
			return nil, fmt.Errorf("address in init tx data is incorrect. %s != %s", tokenAddr, w.tokenAddr)
		}
		txVec, ok := txVectors[vec.SecretHash]
		if !ok {
			return nil, errors.New("AuditContract: tx does not initiate secret hash")
		}
		if !dexeth.CompareVectors(vec, txVec) {
			return nil, fmt.Errorf("tx vector doesn't match expectation. %+v != %+v", txVec, vec)
		}
		val = w.atomize(vec.Value)
		participant = vec.To.String()
		lockTime = time.Unix(int64(vec.LockTime), 0)
		secretHashB = vec.SecretHash[:]
	default:
		return nil, fmt.Errorf("unknown contract version %d", version)
	}

	coin := &coin{
		id:    txHash,
		value: val,
	}

	return &asset.AuditInfo{
		Recipient:  participant,
		Expiration: lockTime,
		Coin:       coin,
		Contract:   contract,
		SecretHash: secretHashB,
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
	contractVer, locator, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return false, time.Time{}, err
	}

	status, vec, err := w.statusAndVector(ctx, locator, contractVer)
	if err != nil {
		return false, time.Time{}, err
	} else if status.Step == dexeth.SSNone {
		return false, time.Time{}, asset.ErrSwapNotInitiated
	}

	lockTime := time.Unix(int64(vec.LockTime), 0)

	expired, err := w.LockTimeExpired(ctx, lockTime)
	if err != nil {
		return false, time.Time{}, err
	}
	return expired, lockTime, nil
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
func (eth *baseWallet) sendFindRedemptionResult(req *findRedemptionRequest, locator, secret []byte, makerAddr string, err error) {
	select {
	case req.res <- &findRedemptionResult{secret: secret, makerAddr: makerAddr, err: err}:
	default:
		eth.log.Info("findRedemptionResult channel blocking for request %x", locator)
	}
}

// findRedemptionRequests creates a copy of the findRedemptionReqs map.
func (w *assetWallet) findRedemptionRequests() map[string]*findRedemptionRequest {
	w.findRedemptionMtx.RLock()
	defer w.findRedemptionMtx.RUnlock()
	reqs := make(map[string]*findRedemptionRequest, len(w.findRedemptionReqs))
	for loc, req := range w.findRedemptionReqs {
		reqs[loc] = req
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

	contractVer, locator, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, nil, err
	}

	// See if it's ready right away.
	secret, makerAddr, err := w.findSecret(locator, contractVer)
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

	locatorKey := string(locator)

	w.findRedemptionMtx.Lock()

	if w.findRedemptionReqs[locatorKey] != nil {
		w.findRedemptionMtx.Unlock()
		return nil, nil, fmt.Errorf("duplicate find redemption request for %x", locator)
	}

	w.findRedemptionReqs[locatorKey] = req

	w.findRedemptionMtx.Unlock()

	var res *findRedemptionResult
	select {
	case res = <-req.res:
	case <-ctx.Done():
	}

	w.findRedemptionMtx.Lock()
	delete(w.findRedemptionReqs, locatorKey)
	w.findRedemptionMtx.Unlock()

	if res == nil {
		return nil, nil, fmt.Errorf("context cancelled for find redemption request %x", locator)
	}

	if res.err != nil {
		return nil, nil, res.err
	}

	return dex.Bytes(fmt.Sprintf(coinIDTmpl, res.makerAddr)), res.secret[:], nil
}

func (w *assetWallet) findSecret(locator []byte, contractVer uint32) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	status, vector, err := w.statusAndVector(ctx, locator, contractVer)
	if err != nil {
		return nil, "", err
	}

	switch status.Step {
	case dexeth.SSInitiated:
		return nil, "", nil // no Maker redeem yet, but keep checking
	case dexeth.SSRedeemed:
		return status.Secret[:], vector.From.String(), nil
	case dexeth.SSNone:
		return nil, "", fmt.Errorf("swap %x does not exist", locator)
	case dexeth.SSRefunded:
		return nil, "", fmt.Errorf("swap %x is already refunded", locator)
	}
	return nil, "", fmt.Errorf("unrecognized swap state %v", status.Step)
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
func (w *assetWallet) Refund(_, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	contractVer, locator, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to decode contract: %w", err)
	}

	status, vector, err := w.statusAndVector(w.ctx, locator, contractVer)
	if err != nil {
		return nil, err
	}
	// It's possible the swap was refunded by someone else. In that case we
	// cannot know the refunding tx hash.
	switch status.Step {
	case dexeth.SSInitiated: // good, check refundability
	case dexeth.SSNone:
		return nil, asset.ErrSwapNotInitiated
	case dexeth.SSRefunded:
		w.log.Infof("Swap with locator %x already refunded.", locator)
		zeroHash := common.Hash{}
		return zeroHash[:], nil
	case dexeth.SSRedeemed:
		w.log.Infof("Swap with locator %x already redeemed with secret key %x.",
			locator, status.Secret)
		return nil, asset.CoinNotFoundError // so caller knows to FindRedemption
	}

	refundable, err := w.isRefundable(locator, contractVer)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to check isRefundable: %w", err)
	}
	if !refundable {
		return nil, fmt.Errorf("Refund: swap with locator %x is not refundable", locator)
	}

	maxFeeRate := dexeth.GweiToWei(feeRate)
	_, tipRate, err := w.currentNetworkFees(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to get network tip cap: %w", err)
	}

	tx, err := w.refund(locator, w.atomize(vector.Value), maxFeeRate, tipRate, contractVer)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to call refund: %w", err)
	}

	txHash := tx.Hash()
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
func (w *ETHWallet) canSend(value uint64, verifyBalance, isPreEstimate bool) (maxFee uint64, maxFeeRate, tipRate *big.Int, err error) {
	maxFeeRate, tipRate, err = w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}
	maxFeeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)

	maxFee = defaultSendGasLimit * maxFeeRateGwei

	if isPreEstimate {
		maxFee = maxFee * 12 / 10 // 20% buffer
	}

	if verifyBalance {
		bal, err := w.Balance()
		if err != nil {
			return 0, nil, nil, err
		}
		avail := bal.Available
		if avail < value {
			return 0, nil, nil, fmt.Errorf("not enough funds to send: have %d gwei need %d gwei", avail, value)
		}

		if avail < value+maxFee {
			return 0, nil, nil, fmt.Errorf("available funds %d gwei cannot cover value being sent: need %d gwei + %d gwei max fee", avail, value, maxFee)
		}
	}
	return
}

// canSend ensures that the wallet has enough to cover send value and returns
// the fee rate and max fee required for the send tx.
func (w *TokenWallet) canSend(value uint64, verifyBalance, isPreEstimate bool) (maxFee uint64, maxFeeRate, tipRate *big.Int, err error) {
	maxFeeRate, tipRate, err = w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}
	maxFeeRateGwei := dexeth.WeiToGweiCeil(maxFeeRate)

	g := w.gases(dexeth.ContractVersionERC20)
	if g == nil {
		return 0, nil, nil, fmt.Errorf("gas table not found")
	}

	maxFee = maxFeeRateGwei * g.Transfer

	if isPreEstimate {
		maxFee = maxFee * 12 / 10 // 20% buffer
	}

	if verifyBalance {
		bal, err := w.Balance()
		if err != nil {
			return 0, nil, nil, err
		}
		avail := bal.Available
		if avail < value {
			return 0, nil, nil, fmt.Errorf("not enough tokens: have %[1]d %[3]s need %[2]d %[3]s", avail, value, w.ui.AtomicUnit)
		}

		ethBal, err := w.parent.Balance()
		if err != nil {
			return 0, nil, nil, fmt.Errorf("error getting base chain balance: %w", err)
		}

		if ethBal.Available < maxFee {
			return 0, nil, nil, fmt.Errorf("insufficient balance to cover token transfer fees. %d < %d",
				ethBal.Available, maxFee)
		}
	}
	return
}

// EstimateSendTxFee returns a tx fee estimate for a send tx. The provided fee
// rate is ignored since all sends will use an internally derived fee rate. If
// an address is provided, it will ensure wallet has enough to cover total
// spend.
func (w *ETHWallet) EstimateSendTxFee(addr string, value, _ uint64, _, maxWithdraw bool) (uint64, bool, error) {
	if err := isValidSend(addr, value, maxWithdraw); err != nil && addr != "" { // fee estimate for a send tx.
		return 0, false, err
	}
	maxFee, _, _, err := w.canSend(value, addr != "", true)
	if err != nil {
		return 0, false, err
	}
	return maxFee, w.ValidateAddress(addr), nil
}

// StandardSendFee returns the fees for a simple send tx.
func (w *ETHWallet) StandardSendFee(feeRate uint64) uint64 {
	return defaultSendGasLimit * feeRate
}

// EstimateSendTxFee returns a tx fee estimate for a send tx. The provided fee
// rate is ignored since all sends will use an internally derived fee rate. If
// an address is provided, it will ensure wallet has enough to cover total
// spend.
func (w *TokenWallet) EstimateSendTxFee(addr string, value, _ uint64, _, maxWithdraw bool) (fee uint64, isValidAddress bool, err error) {
	if err := isValidSend(addr, value, maxWithdraw); err != nil && addr != "" { // fee estimate for a send tx.
		return 0, false, err
	}
	maxFee, _, _, err := w.canSend(value, addr != "", true)
	if err != nil {
		return 0, false, err
	}
	return maxFee, w.ValidateAddress(addr), nil
}

// StandardSendFee returns the fees for a simple send tx.
func (w *TokenWallet) StandardSendFee(feeRate uint64) uint64 {
	g := w.gases(dexeth.ContractVersionNewest)
	if g == nil {
		w.log.Errorf("error getting gases for token %s", w.token.Name)
		return 0
	}
	return g.Transfer * feeRate
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

	ctx, cancel := context.WithTimeout(ctx, onChainDataFetchTimeout)
	defer cancel()

	tip := w.tipHeight()

	status, err := w.status(ctx, secretHash, contractVer)
	if err != nil {
		return 0, false, fmt.Errorf("error finding swap state: %w", err)
	}

	if status.Step == dexeth.SSNone {
		return 0, false, asset.ErrSwapNotInitiated
	}

	spent = status.Step >= dexeth.SSRedeemed
	if spent && contractVer == 1 {
		// Gotta get the confirimations directly.
		var txHash common.Hash
		copy(txHash[:], coinID)
		confs, err = w.node.transactionConfirmations(ctx, txHash)
		if err != nil {
			return 0, false, fmt.Errorf("error finding swap state: %w", err)
		}
		return
	}

	// TODO: If tip < swapData.BlockHeight (which has been observed), what does
	// that mean? Are we using the wrong provider in a multi-provider setup? How
	// do we resolve provider relevance?
	if tip >= status.BlockHeight {
		confs = uint32(w.tipHeight() - status.BlockHeight + 1)
	}
	return
}

// Send sends the exact value to the specified address. The provided fee rate is
// ignored since all sends will use an internally derived fee rate.
func (w *ETHWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	_ /* maxFee */, maxFeeRate, tipRate, err := w.canSend(value, true, false)
	if err != nil {
		return nil, err
	}
	// TODO: Subtract option.
	// if avail < value+maxFee {
	// 	value -= maxFee
	// }

	tx, err := w.sendToAddr(common.HexToAddress(addr), value, maxFeeRate, tipRate)
	if err != nil {
		return nil, err
	}

	txHash := tx.Hash()

	return &coin{id: txHash, value: value}, nil
}

// Send sends the exact value to the specified address. Fees are taken from the
// parent wallet. The provided fee rate is ignored since all sends will use an
// internally derived fee rate.
func (w *TokenWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	_ /* maxFee */, maxFeeRate, tipRate, err := w.canSend(value, true, false)
	if err != nil {
		return nil, err
	}

	tx, err := w.sendToAddr(common.HexToAddress(addr), value, maxFeeRate, tipRate)
	if err != nil {
		return nil, err
	}

	return &coin{id: tx.Hash(), value: value}, nil
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
func (eth *baseWallet) SyncStatus() (*asset.SyncStatus, error) {
	prog, tipTime, err := eth.node.syncProgress(eth.ctx)
	if err != nil {
		return nil, err
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
	return &asset.SyncStatus{
		Synced:         checkHeaderTime() && eth.node.peerCount() > 0,
		StartingBlocks: eth.startingBlocks.Load(),
		TargetHeight:   prog.HighestBlock,
		Blocks:         prog.CurrentBlock,
	}, nil
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
func (w *baseWallet) swapOrRedemptionFeesPaid(
	ctx context.Context,
	coinID dex.Bytes,
	contractData dex.Bytes,
	isInit bool,
) (fee uint64, locators [][]byte, err error) {
	contractVer, locator, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return 0, nil, err
	}

	var txHash common.Hash
	copy(txHash[:], coinID)

	tip := w.tipHeight()

	var blockNum uint64
	var tx *types.Transaction
	if w.withLocalTxRead(txHash, func(wt *extendedWalletTx) {
		blockNum = wt.BlockNumber
		fee = wt.Fees
		tx, err = wt.tx()
		if err != nil {
			w.log.Errorf("Error decoding wallet transaction %s: %v", txHash, err)
		}
	}) && err == nil {
		if confs := safeConfs(tip, blockNum); confs < w.finalizeConfs {
			return 0, nil, asset.ErrNotEnoughConfirms
		}
		locators, _, err = extractSecretHashes(tx, contractVer, isInit)
		return
	}

	// We don't have information locally. This really shouldn't happen anymore,
	// but let's look on-chain anyway.

	receipt, tx, err := w.node.transactionAndReceipt(ctx, txHash)
	if err != nil {
		return 0, nil, err
	}

	if confs := safeConfsBig(tip, receipt.BlockNumber); confs < w.finalizeConfs {
		return 0, nil, asset.ErrNotEnoughConfirms
	}

	bigFees := new(big.Int).Mul(receipt.EffectiveGasPrice, big.NewInt(int64(receipt.GasUsed)))
	fee = dexeth.WeiToGweiCeil(bigFees)
	locators, _, err = extractSecretHashes(tx, contractVer, isInit)
	if err != nil {
		return 0, nil, err
	}

	sort.Slice(locators, func(i, j int) bool { return bytes.Compare(locators[i], locators[j]) < 0 })
	var found bool
	for i := range locators {
		if bytes.Equal(locator, locators[i]) {
			found = true
			break
		}
	}
	if !found {
		return 0, nil, fmt.Errorf("locator %x not found in transaction", locator)
	}
	return dexeth.WeiToGweiCeil(bigFees), locators, nil
}

// extractSecretHashes extracts the secret hashes from the reedeem or swap tx
// data. The returned hashes are sorted lexicographically.
func extractSecretHashes(tx *types.Transaction, contractVer uint32, isInit bool) (locators, secretHashes [][]byte, err error) {
	defer func() {
		sort.Slice(secretHashes, func(i, j int) bool { return bytes.Compare(secretHashes[i], secretHashes[j]) < 0 })
	}()

	switch contractVer {
	case 0:
		if isInit {
			inits, err := dexeth.ParseInitiateDataV0(tx.Data())
			if err != nil {
				return nil, nil, fmt.Errorf("invalid initiate data: %v", err)
			}
			locators = make([][]byte, 0, len(inits))
			for k := range inits {
				copyK := k
				locators = append(locators, copyK[:])
			}
		} else {
			redeems, err := dexeth.ParseRedeemDataV0(tx.Data())
			if err != nil {
				return nil, nil, fmt.Errorf("invalid redeem data: %v", err)
			}
			locators = make([][]byte, 0, len(redeems))
			for k := range redeems {
				copyK := k
				locators = append(locators, copyK[:])
			}
		}
		return locators, locators, nil
	case 1:
		if isInit {
			_, vectors, err := dexeth.ParseInitiateDataV1(tx.Data())
			if err != nil {
				return nil, nil, fmt.Errorf("invalid initiate data: %v", err)
			}
			locators = make([][]byte, 0, len(vectors))
			secretHashes = make([][]byte, 0, len(vectors))
			for _, vec := range vectors {
				locators = append(locators, vec.Locator())
				secretHashes = append(secretHashes, vec.SecretHash[:])
			}
		} else {
			_, redeems, err := dexeth.ParseRedeemDataV1(tx.Data())
			if err != nil {
				return nil, nil, fmt.Errorf("invalid redeem data: %v", err)
			}
			locators = make([][]byte, 0, len(redeems))
			secretHashes = make([][]byte, 0, len(redeems))
			for secretHash, r := range redeems {
				locators = append(locators, r.Contract.Locator())
				secretHashes = append(secretHashes, secretHash[:])
			}
		}
		return locators, secretHashes, nil
	default:
		return nil, nil, fmt.Errorf("unknown server version %d", contractVer)
	}
}

// RegFeeConfirmations gets the number of confirmations for the specified
// transaction.
func (w *baseWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	var txHash common.Hash
	copy(txHash[:], coinID)
	if found, txData := w.localTxStatus(txHash); found {
		if tip := w.tipHeight(); txData.blockNum != 0 && txData.blockNum < tip {
			return uint32(tip - txData.blockNum + 1), nil
		}
		return 0, nil
	}

	return w.node.transactionConfirmations(ctx, txHash)
}

// currentNetworkFees give the current base fee rate (from the best header),
// and recommended tip cap.
func (w *baseWallet) currentNetworkFees(ctx context.Context) (baseRate, tipRate *big.Int, err error) {
	tip := w.tipHeight()
	c := &w.currentFees
	c.Lock()
	defer c.Unlock()
	if tip > 0 && c.blockNum == tip {
		return c.baseRate, c.tipRate, nil
	}
	c.baseRate, c.tipRate, err = w.node.currentFees(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Error getting net fee state: %v", err)
	}
	c.blockNum = tip
	return c.baseRate, c.tipRate, nil
}

// currentFeeRate gives the current rate of transactions being mined. Only
// use this to provide informative realistic estimates of actual fee *use*. For
// transaction planning, use recommendedMaxFeeRateGwei.
func (w *baseWallet) currentFeeRate(ctx context.Context) (_ *big.Int, err error) {
	b, t, err := w.currentNetworkFees(ctx)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Add(b, t), nil
}

// recommendedMaxFeeRate finds a recommended max fee rate using the somewhat
// standard baseRate * 2 + tip formula.
func (eth *baseWallet) recommendedMaxFeeRate(ctx context.Context) (maxFeeRate, tipRate *big.Int, err error) {
	base, tip, err := eth.currentNetworkFees(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Error getting net fee state: %v", err)
	}

	return new(big.Int).Add(
		tip,
		new(big.Int).Mul(base, big.NewInt(2)),
	), tip, nil
}

// recommendedMaxFeeRateGwei gets the recommended max fee rate and converts it
// to gwei.
func (w *baseWallet) recommendedMaxFeeRateGwei(ctx context.Context) (uint64, error) {
	feeRate, _, err := w.recommendedMaxFeeRate(ctx)
	if err != nil {
		return 0, err
	}
	return dexeth.WeiToGweiSafe(feeRate)
}

// FeeRate satisfies asset.FeeRater.
func (eth *baseWallet) FeeRate() uint64 {
	r, err := eth.recommendedMaxFeeRateGwei(eth.ctx)
	if err != nil {
		eth.log.Errorf("Error getting max fee recommendation: %v", err)
		return 0
	}
	return r
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
	ticker := time.NewTicker(stateUpdateTick)
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
	ctx, cancel := context.WithTimeout(ctx, onChainDataFetchTimeout)
	defer cancel()
	bestHdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		eth.log.Errorf("failed to get best hash: %v", err)
		return
	}
	bestHash := bestHdr.Hash()
	// This method is called frequently. Don't hold write lock
	// unless tip has changed.
	currentTipHash := eth.tip().Hash()
	if currentTipHash == bestHash {
		return
	}

	eth.tipMtx.Lock()
	prevTip := eth.currentTip
	eth.currentTip = bestHdr
	eth.tipMtx.Unlock()

	eth.log.Tracef("tip change: %s (%s) => %s (%s)", prevTip.Number,
		currentTipHash, bestHdr.Number, bestHash)

	eth.checkPendingTxs()
	for _, w := range eth.connectedWallets() {
		w.checkFindRedemptions()
		w.checkPendingApprovals()
		w.emit.TipChange(bestHdr.Number.Uint64())
	}
}

// ConfirmTransaction checks the status of a redemption or refund. If a tx
// has been fee-replaced, the caller is notified of this by having a different
// coinID in the returned asset.ConfirmTxStatus as was used to call the
// function. Fee argument is ignored since it is calculated from the best
// header.
func (w *ETHWallet) ConfirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx, _ uint64) (*asset.ConfirmTxStatus, error) {
	return w.confirmTransaction(coinID, confirmTx)
}

// ConfirmTransaction checks the status of a redemption or refund. If a tx
// has been fee-replaced, the caller is notified of this by having a different
// coinID in the returned asset.ConfirmTxStatus as was used to call the
// function. Fee argument is ignored since it is calculated from the best
// header.
func (w *TokenWallet) ConfirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx, _ uint64) (*asset.ConfirmTxStatus, error) {
	return w.confirmTransaction(coinID, confirmTx)
}

func confStatus(confs, req uint64, txHash common.Hash) *asset.ConfirmTxStatus {
	return &asset.ConfirmTxStatus{
		Confs:  confs,
		Req:    req,
		CoinID: txHash[:],
	}
}

// confirmTransaction checks the confirmation status of a transaction.
func (w *assetWallet) confirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx) (*asset.ConfirmTxStatus, error) {
	if len(coinID) != common.HashLength {
		return nil, fmt.Errorf("expected coin ID to be a transaction hash, but it has a length of %d",
			len(coinID))
	}
	var txHash common.Hash
	copy(txHash[:], coinID)

	contractVer, locator, err := dexeth.DecodeContractData(confirmTx.Contract())
	if err != nil {
		return nil, fmt.Errorf("failed to decode contract data: %w", err)
	}

	tip := w.tipHeight()

	// If we have local information, use that.
	if found, s := w.localTxStatus(txHash); found {
		if s.assumedLost {
			return nil, asset.ErrTxLost
		}
		if len(s.nonceReplacement) > 0 {
			// Tell core to update it's coin ID.
			txHash = common.HexToHash(s.nonceReplacement)
		}

		var confirmStatus *asset.ConfirmTxStatus
		if s.blockNum != 0 && s.blockNum <= tip {
			confirmStatus = confStatus(tip-s.blockNum+1, w.finalizeConfs, txHash)
		} else {
			// Apparently not mined yet.
			confirmStatus = confStatus(0, w.finalizeConfs, txHash)
		}
		if s.receipt != nil && s.receipt.Status != types.ReceiptStatusSuccessful && confirmStatus.Confs >= w.finalizeConfs {
			return nil, asset.ErrTxRejected
		}
		return confirmStatus, nil
	}

	// We know nothing of the tx locally. This shouldn't really happen, but
	// we'll look for it on-chain anyway.
	r, err := w.node.transactionReceipt(w.ctx, txHash)
	if err != nil {
		if errors.Is(err, asset.CoinNotFoundError) {
			// We don't know it ourselves and we can't see it on-chain. This
			// used to be a CoinNotFoundError, but since we have local tx
			// storage, we'll assume it's lost to space and time now.
			return nil, asset.ErrTxLost
		}
		return nil, err
	}

	// We could potentially grab the tx, check the from address, and store it
	// to our db right here, but I suspect that this case would be exceedingly
	// rare anyway.

	confs := safeConfsBig(tip, r.BlockNumber)
	if confs >= w.finalizeConfs {
		if r.Status == types.ReceiptStatusSuccessful {
			return confStatus(w.finalizeConfs, w.finalizeConfs, txHash), nil
		}
		// We weren't able to redeem. Perhaps fees were too low, but we'll
		// check the status in the contract for a couple of other conditions.
		status, err := w.status(w.ctx, locator, contractVer)
		if err != nil {
			return nil, fmt.Errorf("error pulling swap data from contract: %v", err)
		}
		if confirmTx.IsRedeem() {
			switch status.Step {
			case dexeth.SSRedeemed:
				w.log.Infof("Redemption in tx %s was apparently redeemed by another tx. OK.", txHash)
				return confStatus(w.finalizeConfs, w.finalizeConfs, txHash), nil
			case dexeth.SSRefunded:
				return nil, asset.ErrSwapRefunded
			}
		} else {
			switch status.Step {
			case dexeth.SSRedeemed:
				return nil, asset.ErrSwapRedeemed
			case dexeth.SSRefunded:
				w.log.Infof("Refund in tx %s was apparently redeemed by another tx. OK.", txHash)
				return confStatus(w.finalizeConfs, w.finalizeConfs, txHash), nil
			}
		}
		err = fmt.Errorf("tx %s failed to %s %s funds", txHash, confirmTx.TxType(), dex.BipIDSymbol(w.assetID))
		return nil, errors.Join(err, asset.ErrTxRejected)
	}
	return confStatus(confs, w.finalizeConfs, txHash), nil
}

// withLocalTxRead runs a function that reads a pending or DB tx under
// read-lock. Certain DB transactions in undeterminable states will not be
// used.
func (w *baseWallet) withLocalTxRead(txHash common.Hash, f func(*extendedWalletTx)) bool {
	withPendingTxRead := func(txHash common.Hash, f func(*extendedWalletTx)) bool {
		w.nonceMtx.RLock()
		defer w.nonceMtx.RUnlock()
		for _, pendingTx := range w.pendingTxs {
			if pendingTx.txHash == txHash {
				f(pendingTx)
				return true
			}
		}
		return false
	}
	if withPendingTxRead(txHash, f) {
		return true
	}
	// Could be finalized and in the database.
	if confirmedTx, err := w.txDB.getTx(txHash); err != nil {
		w.log.Errorf("Error getting DB transaction: %v", err)
	} else if confirmedTx != nil {
		if !confirmedTx.Confirmed && confirmedTx.Receipt == nil && !confirmedTx.AssumedLost && confirmedTx.NonceReplacement == "" {
			// If it's in the db but not in pendingTxs, and we know nothing
			// about the tx, don't use it.
			return false
		}
		f(confirmedTx)
		return true
	}
	return false
}

// walletTxStatus is data copied from an extendedWalletTx.
type walletTxStatus struct {
	confirmed        bool
	blockNum         uint64
	nonceReplacement string
	feeReplacement   bool
	receipt          *types.Receipt
	assumedLost      bool
}

// localTxStatus looks for an extendedWalletTx and copies critical values to
// a walletTxStatus for use without mutex protection.
func (w *baseWallet) localTxStatus(txHash common.Hash) (_ bool, s *walletTxStatus) {
	return w.withLocalTxRead(txHash, func(wt *extendedWalletTx) {
		s = &walletTxStatus{
			confirmed:        wt.Confirmed,
			blockNum:         wt.BlockNumber,
			nonceReplacement: wt.NonceReplacement,
			feeReplacement:   wt.FeeReplacement,
			receipt:          wt.Receipt,
			assumedLost:      wt.AssumedLost,
		}
	}), s
}

// checkFindRedemptions checks queued findRedemptionRequests.
func (w *assetWallet) checkFindRedemptions() {
	for loc, req := range w.findRedemptionRequests() {
		if w.ctx.Err() != nil {
			return
		}
		locator := []byte(loc)
		secret, makerAddr, err := w.findSecret(locator, req.contractVer)
		if err != nil {
			w.sendFindRedemptionResult(req, locator, nil, "", err)
		} else if len(secret) > 0 {
			w.sendFindRedemptionResult(req, locator, secret, makerAddr, nil)
		}
	}
}

func (w *assetWallet) checkPendingApprovals() {
	w.approvalsMtx.Lock()
	defer w.approvalsMtx.Unlock()

	for version, pendingApproval := range w.pendingApprovals {
		if w.ctx.Err() != nil {
			return
		}
		var confirmed bool
		if found, txData := w.localTxStatus(pendingApproval.txHash); found {
			confirmed = txData.blockNum > 0 && txData.blockNum <= w.tipHeight()
		} else {
			confs, err := w.node.transactionConfirmations(w.ctx, pendingApproval.txHash)
			if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
				w.log.Errorf("error getting confirmations for tx %s: %v", pendingApproval.txHash, err)
				continue
			}
			confirmed = confs > 0
		}
		if confirmed {
			go pendingApproval.onConfirm()
			delete(w.pendingApprovals, version)
		}
	}
}

// sumPendingTxs sums the expected incoming and outgoing values in pending
// transactions stored in pendingTxs. Not used if the node is a
// txPoolFetcher.
func (w *assetWallet) sumPendingTxs() (out, in uint64) {
	isToken := w.assetID != w.baseChainID

	sumPendingTx := func(pendingTx *extendedWalletTx) {
		// Already confirmed, but still in the unconfirmed txs map waiting for
		// txConfsNeededToConfirm confirmations.
		if pendingTx.BlockNumber != 0 {
			return
		}

		txAssetID := w.baseChainID
		if pendingTx.TokenID != nil {
			txAssetID = *pendingTx.TokenID
		}

		if txAssetID == w.assetID {
			if asset.IncomingTxType(pendingTx.Type) {
				in += pendingTx.Amount
			} else {
				out += pendingTx.Amount
			}
		}
		if !isToken {
			out += pendingTx.Fees
		}
	}

	w.nonceMtx.RLock()
	defer w.nonceMtx.RUnlock()

	for _, pendingTx := range w.pendingTxs {
		sumPendingTx(pendingTx)
	}

	return
}

func (w *assetWallet) getConfirmedBalance() (*big.Int, error) {
	now := time.Now()
	tip := w.tipHeight()

	w.balances.Lock()
	defer w.balances.Unlock()

	if w.balances.m == nil {
		w.balances.m = make(map[uint32]*cachedBalance)
	}
	// Check to see if we already have one up-to-date
	cached := w.balances.m[w.assetID]
	if cached != nil && cached.height == tip && time.Since(cached.stamp) < time.Minute {
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
			height: tip,
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
			height: tip,
			bal:    bal,
		}
	}
	if reqBal == nil {
		return nil, fmt.Errorf("requested asset not in multi-balance result: %v", err)
	}
	return reqBal, nil
}

func (w *assetWallet) contractors() map[uint32]contractor {
	return map[uint32]contractor{0: w.contractorV0, 1: w.contractorV1}
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
			out, in := w.sumPendingTxs()
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
				return nil, fmt.Errorf("balance underflow detected. pending out (%s) > balance (%s)", outEVM, confirmed)
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
		for ver, c := range w.contractors() {
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

// Uncomment here and in sendToAddr to test actionTypeLostNonce.
// var nonceBorked atomic.Bool
// func (w *ETHWallet) borkNonce(tx *types.Transaction) {
// 	fmt.Printf("\n##### losing tx for lost nonce testing\n\n")
// 	txHash := tx.Hash()
// 	v := big.NewInt(dexeth.GweiFactor)
// 	spoofTx := types.NewTransaction(tx.Nonce(), w.addr, v, defaultSendGasLimit, v, nil)
// 	spoofHash := tx.Hash()
// 	pendingSpoofTx := w.extendedTx(spoofTx, asset.SelfSend, dexeth.GweiFactor)
// 	w.nonceMtx.Lock()
// 	for i, pendingTx := range w.pendingTxs {
// 		if pendingTx.txHash == txHash {
// 			w.pendingTxs[i] = pendingSpoofTx
// 			w.tryStoreDBTx(pendingSpoofTx)
// 			fmt.Printf("\n##### Replaced pending tx %s with spoof tx %s, nonce %d \n\n", txHash, spoofHash, tx.Nonce())
// 			break
// 		}
// 	}
// 	w.nonceMtx.Unlock()
// }

// Uncomment here and in sendToAddr to test actionTypeMissingNonces.
// var nonceFuturized atomic.Bool

// Uncomment here and in sendToAddr to test actionTypeTooCheap
// var nonceSkimped atomic.Bool

// sendToAddr sends funds to the address.
func (w *ETHWallet) sendToAddr(addr common.Address, amt uint64, maxFeeRate, tipRate *big.Int) (*types.Transaction, error) {

	// Uncomment here and above to test actionTypeLostNonce.
	// Also change txAgeOut to like 1 minute.
	// if nonceBorked.CompareAndSwap(false, true) {
	// 	defer w.borkNonce(tx)
	// }

	recipient := addr.Hex()
	res := &genTxResult{
		tx:        nil,
		txType:    asset.Send,
		amt:       amt,
		recipient: &recipient,
	}
	if addr == w.addr {
		res.txType = asset.SelfSend
	}

	return res.tx, w.withNonce(w.ctx, func(nonce *big.Int) (*genTxResult, error) {

		// Uncomment here and above to test actionTypeMissingNonces.
		// if nonceFuturized.CompareAndSwap(false, true) {
		// 	fmt.Printf("\n##### advancing nonce for missing nonce testing\n\n")
		// 	nonce.Add(nonce, big.NewInt(3))
		// }

		// Uncomment here and above to test actionTypeTooCheap.
		// if nonceSkimped.CompareAndSwap(false, true) {
		// 	fmt.Printf("\n##### lower max fee rate to test cheap tx handling\n\n")
		// 	maxFeeRate.SetUint64(1)
		// 	tipRate.SetUint64(0)
		// }

		txOpts, err := w.node.txOpts(w.ctx, amt, defaultSendGasLimit, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, err
		}
		res.tx, err = w.node.sendTransaction(w.ctx, txOpts, addr, nil)
		if err != nil {
			return nil, err
		}
		return res, nil
	})
}

// sendToAddr sends funds to the address.
func (w *TokenWallet) sendToAddr(addr common.Address, amt uint64, maxFeeRate, tipRate *big.Int) (*types.Transaction, error) {
	g := w.gases(dexeth.ContractVersionERC20)
	if g == nil {
		return nil, fmt.Errorf("no gas table")
	}

	recipient := addr.Hex()
	res := &genTxResult{
		tx:        nil,
		txType:    asset.Send,
		amt:       amt,
		recipient: &recipient,
	}
	if addr == w.addr {
		res.txType = asset.SelfSend
	}

	return res.tx, w.withNonce(w.ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(w.ctx, 0, g.Transfer, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, err
		}
		return res, w.withTokenContractor(w.assetID, dexeth.ContractVersionERC20, func(c tokenContractor) error {
			res.tx, err = c.transfer(txOpts, addr, w.evmify(amt))
			if err != nil {
				return err
			}
			return nil
		})
	})

}

// status fetches the SwapStatus for the locator and contract version.
func (w *assetWallet) status(ctx context.Context, locator []byte, contractVer uint32) (s *dexeth.SwapStatus, err error) {
	return s, w.withContractor(contractVer, func(c contractor) error {
		s, err = c.status(ctx, locator)
		return err
	})
}

// vector fetches the SwapVector for the locator and contract version.
func (w *assetWallet) vector(ctx context.Context, locator []byte, contractVer uint32) (v *dexeth.SwapVector, err error) {
	return v, w.withContractor(contractVer, func(c contractor) error {
		v, err = c.vector(ctx, locator)
		return err
	})
}

// statusAndVector fetches the SwapStatus and SwapVector for the locator and
// contract version.
func (w *assetWallet) statusAndVector(ctx context.Context, locator []byte, contractVer uint32) (s *dexeth.SwapStatus, v *dexeth.SwapVector, err error) {
	return s, v, w.withContractor(contractVer, func(c contractor) error {
		s, v, err = c.statusAndVector(ctx, locator)
		return err
	})
}

// initiate initiates multiple swaps in the same transaction.
func (w *assetWallet) initiate(
	ctx context.Context, assetID uint32, contracts []*asset.Contract, gasLimit uint64, maxFeeRate, tipRate *big.Int, contractVer uint32,
) (tx *types.Transaction, err error) {

	var val, amt uint64
	for _, c := range contracts {
		amt += c.Value
		if assetID == w.baseChainID {
			val += c.Value
		}
	}

	res := &genTxResult{
		tx:     nil,
		txType: asset.Swap,
		amt:    amt,
	}

	return res.tx, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(ctx, val, gasLimit, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, err
		}

		return res, w.withContractor(contractVer, func(c contractor) error {
			res.tx, err = c.initiate(txOpts, contracts)
			return err
		})
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
func (w *assetWallet) estimateRedeemGas(ctx context.Context, secrets [][32]byte, locators [][]byte, contractVer uint32) (gas uint64, err error) {
	return gas, w.withContractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRedeemGas(ctx, secrets, locators)
		return err
	})
}

// estimateRefundGas checks the amount of gas that is used for a refund.
func (w *assetWallet) estimateRefundGas(ctx context.Context, locator []byte, contractVer uint32) (gas uint64, err error) {
	return gas, w.withContractor(contractVer, func(c contractor) error {
		gas, err = c.estimateRefundGas(ctx, locator)
		return err
	})
}

// loadContractors prepares the token contractors and add them to the map.
func (w *assetWallet) loadContractors(parent *assetWallet) error {
	token, found := w.tokens[w.assetID]
	if !found {
		return fmt.Errorf("token %d not found", w.assetID)
	}
	netToken, found := token.NetTokens[w.net]
	if !found {
		return fmt.Errorf("token %d not found", w.assetID)
	}

	if _, found := netToken.SwapContracts[0]; found {
		c, err := newV0TokenContractor(w.net, token, w.addr, w.node.contractBackend())
		if err != nil {
			return fmt.Errorf("error constructing token %s contractor version 0: %w", token.Name, err)
		}
		if netToken.Address != c.tokenAddress() {
			return fmt.Errorf("wrong %s token address. expected %s, got %s", token.Name, netToken.Address, c.tokenAddress())
		}
		w.contractorV0 = c
	}

	if _, found := netToken.SwapContracts[1]; found {
		if parent.contractorV1 == nil {
			return errors.New("can't construct version 1 contractor if parent doesn't have the unified contractor")
		}
		cgen, ok := parent.contractorV1.(unifiedContractor)
		if !ok {
			return errors.New("parent contractor ain't unified")
		}
		c, err := cgen.tokenContractor(token)
		if err != nil {
			return fmt.Errorf("error constructing version 1 token %s contractor: %w", token.Name, err)
		}
		w.contractorV1 = c
	}
	return nil
}

// withContractor runs the provided function with the versioned contractor.
func (w *assetWallet) withContractor(contractVer uint32, f func(contractor) error) error {
	if contractVer == dexeth.ContractVersionERC20 {
		// For ERC20 methods, use the most recent contractor version.
		var bestVer uint32
		var bestContractor contractor
		for ver, c := range w.contractors() {
			if ver >= bestVer {
				bestContractor = c
				bestVer = ver
			}
		}
		return f(bestContractor)
	}
	var c contractor
	switch contractVer {
	case 0:
		if w.contractorV0 == nil {
			return errors.New("no version 0 contractor")
		}
		c = w.contractorV0
	case 1:
		if w.contractorV1 == nil {
			return errors.New("no version 1 contractor")
		}
		c = w.contractorV1
	}
	return f(c)
}

// withTokenContractor runs the provided function with the tokenContractor.
func (w *assetWallet) withTokenContractor(assetID, contractVer uint32, f func(tokenContractor) error) error {
	return w.withContractor(contractVer, func(c contractor) error {
		tc, is := c.(tokenContractor)
		if !is {
			return fmt.Errorf("contractor for %s version %d is not a tokenContractor. type = %T", w.ui.Conventional.Unit, contractVer, c)
		}
		return f(tc)
	})
}

// estimateApproveGas estimates the gas required for a transaction approving a
// spender for an ERC20 contract.
func (w *assetWallet) estimateApproveGas(contractVer uint32, newGas *big.Int) (gas uint64, err error) {
	return gas, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
		gas, err = c.estimateApproveGas(w.ctx, newGas)
		return err
	})
}

// Can uncomment here and in redeem to test rejected redemption reauthorization.
// var firstRedemptionBorked atomic.Bool

// Uncomment here and below to test core's handling of lost redemption txs.
// var firstRedemptionLost atomic.Bool

// redeem redeems a swap contract. Any on-chain failure, such as this secret not
// matching the hash, will not cause this to error.
func (w *assetWallet) redeem(
	ctx context.Context,
	redemptions []*asset.Redemption,
	maxFeeRate uint64,
	tipRate *big.Int,
	gasLimit uint64,
	contractVer uint32,
) (*types.Transaction, error) {

	// // Uncomment here and above to test core's handling of ErrTxLost from
	// if firstRedemptionLost.CompareAndSwap(false, true) {
	// 	fmt.Printf("\n##### Spoofing tx for lost tx testing\n\n")
	// 	return types.NewTransaction(10, w.addr, big.NewInt(dexeth.GweiFactor), gasLimit, dexeth.GweiToWei(maxFeeRate), nil), nil
	// }

	res := &genTxResult{
		tx:     nil,
		txType: asset.Redeem,
		amt:    0,
	}

	return res.tx, w.withNonce(ctx, func(nonce *big.Int) (*genTxResult, error) {
		for _, r := range redemptions {
			res.amt += r.Spends.Coin.Value()
		}

		// Uncomment here and above to test rejected redemption handling.
		// if firstRedemptionBorked.CompareAndSwap(false, true) {
		// 	fmt.Printf("\n##### Borking gas limit for rejected tx testing\n\n")
		// 	gasLimit /= 4
		// }

		txOpts, err := w.node.txOpts(ctx, 0, gasLimit, dexeth.GweiToWei(maxFeeRate), tipRate, nonce)
		if err != nil {
			return nil, err
		}

		return res, w.withContractor(contractVer, func(c contractor) error {
			res.tx, err = c.redeem(txOpts, redemptions)
			return err
		})
	})
}

// refund refunds a swap contract using the account controlled by the wallet.
// Any on-chain failure, such as the locktime not being past, will not cause
// this to error.
func (w *assetWallet) refund(locator []byte, amt uint64, maxFeeRate, tipRate *big.Int, contractVer uint32) (*types.Transaction, error) {
	gas := w.gases(contractVer)
	if gas == nil {
		return nil, fmt.Errorf("no gas table for asset %d, version %d", w.assetID, contractVer)
	}

	res := &genTxResult{
		tx:     nil,
		txType: asset.Refund,
		amt:    amt,
	}

	return res.tx, w.withNonce(w.ctx, func(nonce *big.Int) (*genTxResult, error) {
		txOpts, err := w.node.txOpts(w.ctx, 0, gas.Refund, maxFeeRate, tipRate, nonce)
		if err != nil {
			return nil, err
		}
		return res, w.withContractor(contractVer, func(c contractor) error {
			res.tx, err = c.refund(txOpts, locator)
			return err
		})
	})
}

// isRedeemable checks if the swap identified by secretHash is redeemable using secret.
func (w *assetWallet) isRedeemable(locator []byte, secret [32]byte, contractVer uint32) (redeemable bool, err error) {
	status, err := w.status(w.ctx, locator, contractVer)
	if err != nil {
		return false, err
	}

	if status.Step != dexeth.SSInitiated {
		return false, nil
	}

	vector, err := w.vector(w.ctx, locator, contractVer)
	if err != nil {
		return false, err
	}

	return w.ValidateSecret(secret[:], vector.SecretHash[:]), nil
}

func (w *assetWallet) isRefundable(locator []byte, contractVer uint32) (refundable bool, err error) {
	return refundable, w.withContractor(contractVer, func(c contractor) error {
		refundable, err = c.isRefundable(locator)
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

func (w *baseWallet) emitBridgeCompletedNote(tx *asset.WalletTransaction) {
	assetID := w.baseChainID
	if tx.TokenID != nil {
		assetID = *tx.TokenID
	}

	w.walletsMtx.RLock()
	wallet := w.wallets[assetID]
	w.walletsMtx.RUnlock()
	if wallet == nil {
		w.log.Errorf("emitBridgeCompletedNote: wallet not found for asset ID %d", assetID)
		return
	}

	if tx.BridgeCounterpartTx == nil {
		w.log.Errorf("emitBridgeCompletedNote: bridge counterpart tx not found for tx ID %s", tx.ID)
		return
	}

	wallet.emit.BridgeCompleted(tx.BridgeCounterpartTx.AssetID, tx.BridgeCounterpartTx.ID, tx.ID, tx.Timestamp)
}
func findMissingNonces(confirmedAt, pendingAt *big.Int, pendingTxs []*extendedWalletTx) (ns []uint64) {
	pendingTxMap := make(map[uint64]struct{})
	// It's not clear whether all providers will update PendingNonceAt if
	// there are gaps. geth doesn't do it on simnet, apparently. We'll use
	// our own pendingTxs max nonce as a backup.
	nonceHigh := big.NewInt(-1)
	for _, pendingTx := range pendingTxs {
		if pendingTx.indexed && pendingTx.Nonce.Cmp(nonceHigh) > 0 {
			nonceHigh.Set(pendingTx.Nonce)
		}
		pendingTxMap[pendingTx.Nonce.Uint64()] = struct{}{}
	}
	nonceHigh.Add(nonceHigh, big.NewInt(1))
	if pendingAt.Cmp(nonceHigh) > 0 {
		nonceHigh.Set(pendingAt)
	}
	for n := confirmedAt.Uint64(); n < nonceHigh.Uint64(); n++ {
		if _, found := pendingTxMap[n]; !found {
			ns = append(ns, n)
		}
	}
	return
}

func (w *baseWallet) missingNoncesActionID() string {
	return fmt.Sprintf("missingNonces_%d", w.baseChainID)
}

// updatePendingTx checks the confirmation status of a transaction. The
// BlockNumber, Fees, and Timestamp fields of the extendedWalletTx are updated
// if the transaction is confirmed, and if the transaction has reached the
// required number of confirmations, it is removed from w.pendingTxs.
//
// w.nonceMtx must be held.
func (w *baseWallet) updatePendingTx(tip uint64, pendingTx *extendedWalletTx) {
	if pendingTx.Confirmed && pendingTx.savedToDB {
		return
	}
	waitingOnConfs := pendingTx.BlockNumber > 0 && safeConfs(tip, pendingTx.BlockNumber) < w.finalizeConfs
	if waitingOnConfs {
		// We're just waiting on confs. Don't check again until we expect to be
		// finalized.
		return
	}
	// Only check when the tip has changed.
	if pendingTx.lastCheck == tip {
		return
	}
	pendingTx.lastCheck = tip

	var updated bool
	defer func() {
		if updated || !pendingTx.savedToDB {
			w.tryStoreDBTx(pendingTx)
			w.emitTransactionNote(pendingTx.WalletTransaction, false)
			if pendingTx.Type == asset.CompleteBridge && pendingTx.Confirmed {
				w.emitBridgeCompletedNote(pendingTx.WalletTransaction)
			}
		}
	}()

	receipt, tx, err := w.node.transactionAndReceipt(w.ctx, pendingTx.txHash)
	if w.log.Level() == dex.LevelTrace {
		w.log.Tracef("Attempted to fetch tx and receipt for %s. receipt = %+v, tx = %+v, err = %+v", pendingTx.txHash, receipt, tx, err)
	}
	if err != nil {
		if errors.Is(err, asset.CoinNotFoundError) {
			pendingTx.indexed = tx != nil
			// transactionAndReceipt will return a CoinNotFound for either no
			// reciept or no tx. If they report the tx, we'll consider the tx
			// to be "indexed", and won't send lost tx action-required requests.
			if pendingTx.BlockNumber > 0 {
				w.log.Warnf("Transaction %s was previously mined but now cannot be confirmed. Might be normal.", pendingTx.txHash)
				pendingTx.BlockNumber = 0
				pendingTx.Timestamp = 0
				updated = true
			}
		} else {
			w.log.Errorf("Error getting confirmations for pending tx %s: %v", pendingTx.txHash, err)
		}
		return
	}

	pendingTx.Receipt = receipt
	pendingTx.indexed = true
	pendingTx.Rejected = receipt.Status != types.ReceiptStatusSuccessful
	updated = true

	if receipt.BlockNumber == nil || receipt.BlockNumber.Cmp(new(big.Int)) == 0 {
		if pendingTx.BlockNumber > 0 {
			w.log.Warnf("Transaction %s was previously mined but is now unconfirmed", pendingTx.txHash)
			pendingTx.Timestamp = 0
			pendingTx.BlockNumber = 0
		}
		return
	}
	effectiveGasPrice := receipt.EffectiveGasPrice
	// NOTE: Would be nice if the receipt contained the block time so we could
	// set the timestamp without having to fetch the header. We could use
	// SubmissionTime, I guess. Less accurate, but probably not by much.
	// NOTE: I don't really know why effectiveGasPrice would be nil, but the
	// code I'm replacing got it from the header, so I'm gonna add this check
	// just in case.
	if pendingTx.Timestamp == 0 || effectiveGasPrice == nil {
		hdr, err := w.node.headerByHash(w.ctx, receipt.BlockHash)
		if err != nil {
			w.log.Errorf("Error getting header for hash %v: %v", receipt.BlockHash, err)
			return
		}
		if hdr == nil {
			w.log.Errorf("Header for hash %v is nil", receipt.BlockHash)
			return
		}
		pendingTx.Timestamp = hdr.Time
		if effectiveGasPrice == nil {
			effectiveGasPrice = new(big.Int).Add(hdr.BaseFee, tx.EffectiveGasTipValue(hdr.BaseFee))
		}
	}

	bigFees := new(big.Int).Mul(effectiveGasPrice, big.NewInt(int64(receipt.GasUsed)))
	pendingTx.Fees = dexeth.WeiToGweiCeil(bigFees)
	pendingTx.BlockNumber = receipt.BlockNumber.Uint64()
	pendingTx.Confirmed = safeConfs(tip, pendingTx.BlockNumber) >= w.finalizeConfs
}

// checkPendingTxs checks the confirmation status of all pending transactions.
func (w *baseWallet) checkPendingTxs() {
	tip := w.tipHeight()

	w.nonceMtx.Lock()
	defer w.nonceMtx.Unlock()

	// If we have pending txs, trace log the before and after.
	if w.log.Level() == dex.LevelTrace {
		if nPending := len(w.pendingTxs); nPending > 0 {
			defer func() {
				w.log.Tracef("Checked %d pending txs. Finalized %d", nPending, nPending-len(w.pendingTxs))
			}()
		}

	}

	// keepFromIndex will be the index of the first un-finalized tx.
	// lastConfirmed, will be the index of the last confirmed tx. All txs with
	// nonces lower that lastConfirmed should also be confirmed, or else
	// something isn't right and we may need to request user input.
	var keepFromIndex int
	var lastConfirmed int = -1
	for i, pendingTx := range w.pendingTxs {
		if w.ctx.Err() != nil {
			return
		}
		w.updatePendingTx(tip, pendingTx)
		if pendingTx.Confirmed {
			lastConfirmed = i
			if pendingTx.Nonce.Cmp(w.confirmedNonceAt) == 0 {
				w.confirmedNonceAt.Add(w.confirmedNonceAt, big.NewInt(1))
			}
			if pendingTx.savedToDB {
				if i == keepFromIndex {
					// This is what we expect. No tx should be confirmed before a
					// tx with a lower nonce. We'll delete at least up to this
					// one.
					keepFromIndex = i + 1
				}
			}
			// This transaction is finalized. If we had previously sought action
			// on it, cancel that request.
			if pendingTx.actionRequested {
				pendingTx.actionRequested = false
				w.resolveAction(pendingTx.ID, pendingTx.TokenID)
			}
		}
	}

	// If we have missing nonces, send an alert.
	if !w.recoveryRequestSent && len(findMissingNonces(w.confirmedNonceAt, w.pendingNonceAt, w.pendingTxs)) != 0 {
		w.recoveryRequestSent = true
		w.requestAction(actionTypeMissingNonces, w.missingNoncesActionID(), nil, nil)
	}

	// Loop again, classifying problems and sending action requests.
	for i, pendingTx := range w.pendingTxs {
		if pendingTx.Confirmed || pendingTx.BlockNumber > 0 {
			continue
		}
		if time.Since(pendingTx.actionIgnored) < txAgeOut {
			// They asked us to keep waiting.
			continue
		}
		age := pendingTx.age()
		// i < lastConfirmed means unconfirmed nonce < a confirmed nonce.
		if (i < lastConfirmed) ||
			w.confirmedNonceAt.Cmp(pendingTx.Nonce) > 0 ||
			(age >= txAgeOut && pendingTx.Receipt == nil && !pendingTx.indexed) {

			// The tx in our records wasn't accepted. Where's the right one?
			req := newLostNonceNote(*pendingTx.WalletTransaction, pendingTx.Nonce.Uint64())
			pendingTx.actionRequested = true
			w.requestAction(actionTypeLostNonce, pendingTx.ID, req, pendingTx.TokenID)
			continue
		}
		// Recheck fees periodically.
		const feeCheckInterval = time.Minute * 5
		if time.Since(pendingTx.lastFeeCheck) < feeCheckInterval {
			continue
		}
		pendingTx.lastFeeCheck = time.Now()
		tx, err := pendingTx.tx()
		if err != nil {
			w.log.Errorf("Error decoding raw tx %s for fee check: %v", pendingTx.ID, err)
			continue
		}
		txCap := tx.GasFeeCap()
		baseRate, tipRate, err := w.currentNetworkFees(w.ctx)
		if err != nil {
			w.log.Errorf("Error getting base fee: %w", err)
			continue
		}
		if txCap.Cmp(baseRate) < 0 {
			maxFees := new(big.Int).Add(tipRate, new(big.Int).Mul(baseRate, big.NewInt(2)))
			maxFees.Mul(maxFees, new(big.Int).SetUint64(tx.Gas()))
			req := newLowFeeNote(*pendingTx.WalletTransaction, dexeth.WeiToGweiCeil(maxFees))
			pendingTx.actionRequested = true
			w.requestAction(actionTypeTooCheap, pendingTx.ID, req, pendingTx.TokenID)
			continue
		}
		// Fees look good and there's no reason to believe this tx will
		// not be mined. Do we do anything?
		// actionRequired(actionTypeStuckTx, pendingTx)
	}

	// Delete finalized txs from local tracking.
	w.pendingTxs = w.pendingTxs[keepFromIndex:]

	// Re-broadcast any txs that are not indexed and haven't been re-broadcast
	// in a while, logging any errors as warnings.
	const rebroadcastPeriod = time.Minute * 5
	for _, pendingTx := range w.pendingTxs {
		if pendingTx.Confirmed || pendingTx.BlockNumber > 0 ||
			pendingTx.actionRequested || // Waiting on action
			pendingTx.indexed || // Provider knows about it
			time.Since(pendingTx.lastBroadcast) < rebroadcastPeriod {

			continue
		}
		pendingTx.lastBroadcast = time.Now()
		tx, err := pendingTx.tx()
		if err != nil {
			w.log.Errorf("Error decoding raw tx %s for rebroadcast: %v", pendingTx.ID, err)
			continue
		}
		if err := w.node.sendSignedTransaction(w.ctx, tx, allowAlreadyKnownFilter); err != nil {
			w.log.Warnf("Error rebroadcasting tx %s: %v", pendingTx.ID, err)
		} else {
			w.log.Infof("Rebroadcasted un-indexed transaction %s", pendingTx.ID)
		}
	}
}

// Required Actions: Extraordinary conditions that might require user input.

var _ asset.ActionTaker = (*assetWallet)(nil)

const (
	actionTypeMissingNonces = "missingNonces"
	actionTypeLostNonce     = "lostNonce"
	actionTypeTooCheap      = "tooCheap"
)

// TransactionActionNote is used to request user action on transactions in
// abnormal states.
type TransactionActionNote struct {
	Tx      *asset.WalletTransaction `json:"tx"`
	Nonce   uint64                   `json:"nonce,omitempty"`
	NewFees uint64                   `json:"newFees,omitempty"`
}

// newLostNonceNote is information regarding a tx that appears to be lost.
func newLostNonceNote(tx asset.WalletTransaction, nonce uint64) *TransactionActionNote {
	return &TransactionActionNote{
		Tx:    &tx,
		Nonce: nonce,
	}
}

// newLowFeeNote is data about a tx that is stuck in mempool with too-low fees.
func newLowFeeNote(tx asset.WalletTransaction, newFees uint64) *TransactionActionNote {
	return &TransactionActionNote{
		Tx:      &tx,
		NewFees: newFees,
	}
}

// parse the pending tx and index from the slice.
func pendingTxWithID(txID string, pendingTxs []*extendedWalletTx) (int, *extendedWalletTx) {
	for i, pendingTx := range pendingTxs {
		if pendingTx.ID == txID {
			return i, pendingTx
		}
	}
	return 0, nil
}

// amendPendingTx is called with a function that intends to modify a pendingTx
// under mutex lock.
func (w *assetWallet) amendPendingTx(txID string, f func(common.Hash, *types.Transaction, *extendedWalletTx, int) error) error {
	txHash := common.HexToHash(txID)
	if txHash == (common.Hash{}) {
		return fmt.Errorf("invalid tx ID %q", txID)
	}
	w.nonceMtx.Lock()
	defer w.nonceMtx.Unlock()
	idx, pendingTx := pendingTxWithID(txID, w.pendingTxs)
	if pendingTx == nil {
		// Nothing to do anymore.
		return nil
	}
	tx, err := pendingTx.tx()
	if err != nil {
		return fmt.Errorf("error decoding transaction: %w", err)
	}
	if err := f(txHash, tx, pendingTx, idx); err != nil {
		return err
	}
	w.emit.ActionResolved(txID)
	pendingTx.actionRequested = false
	return nil
}

// userActionBumpFees is a request by a user to resolve a actionTypeTooCheap
// condition.
func (w *assetWallet) userActionBumpFees(actionB []byte) error {
	var action struct {
		TxID string `json:"txID"`
		Bump *bool  `json:"bump"`
	}
	if err := json.Unmarshal(actionB, &action); err != nil {
		return fmt.Errorf("error unmarshaling bump action: %v", err)
	}
	if action.Bump == nil {
		return errors.New("no bump value specified")
	}
	return w.amendPendingTx(action.TxID, func(txHash common.Hash, tx *types.Transaction, pendingTx *extendedWalletTx, idx int) error {
		if !*action.Bump {
			pendingTx.actionIgnored = time.Now()
			return nil
		}

		nonce := new(big.Int).SetUint64(tx.Nonce())
		maxFeeRate, tipCap, err := w.recommendedMaxFeeRate(w.ctx)
		if err != nil {
			return fmt.Errorf("error getting new fee rate: %w", err)
		}
		txOpts, err := w.node.txOpts(w.ctx, 0 /* set below */, tx.Gas(), maxFeeRate, tipCap, nonce)
		if err != nil {
			return fmt.Errorf("error preparing tx opts: %w", err)
		}
		txOpts.Value = tx.Value()
		addr := tx.To()
		if addr == nil {
			return errors.New("pending tx has no recipient?")
		}

		newTx, err := w.node.sendTransaction(w.ctx, txOpts, *addr, tx.Data())
		if err != nil {
			return fmt.Errorf("error sending bumped-fee transaction: %w", err)
		}

		var bridgeDestAssetID *uint32
		var bridgeDestTxID *string
		if pendingTx.BridgeCounterpartTx != nil {
			bridgeDestAssetID = &pendingTx.BridgeCounterpartTx.AssetID
			bridgeDestTxID = &pendingTx.BridgeCounterpartTx.ID
		}

		res := &genTxResult{
			tx:                       newTx,
			txType:                   pendingTx.Type,
			amt:                      pendingTx.Amount,
			bridgeCounterpartAssetID: bridgeDestAssetID,
			bridgeCounterpartTxID:    bridgeDestTxID,
			recipient:                pendingTx.Recipient,
		}
		newPendingTx := w.extendedTx(res)

		pendingTx.NonceReplacement = newPendingTx.ID
		pendingTx.FeeReplacement = true

		w.tryStoreDBTx(pendingTx)
		w.tryStoreDBTx(newPendingTx)

		w.pendingTxs[idx] = newPendingTx
		return nil
	})
}

// tryStoreDBTx attempts to store the DB tx and logs errors internally. This
// method sets the savedToDB flag, so if this tx is in pendingTxs, the nonceMtx
// must be held for reading.
func (w *baseWallet) tryStoreDBTx(wt *extendedWalletTx) {
	err := w.txDB.storeTx(wt)
	if err != nil {
		w.log.Errorf("Error storing tx %s to DB: %v", wt.txHash, err)
	}
	wt.savedToDB = err == nil
}

// userActionNonceReplacement is a request by a user to resolve a
// actionTypeLostNonce condition.
func (w *assetWallet) userActionNonceReplacement(actionB []byte) error {
	var action struct {
		TxID          string `json:"txID"`
		Abandon       *bool  `json:"abandon"`
		ReplacementID string `json:"replacementID"`
	}
	if err := json.Unmarshal(actionB, &action); err != nil {
		return fmt.Errorf("error unmarshaling user action: %v", err)
	}
	if action.Abandon == nil {
		return fmt.Errorf("no abandon value provided for user action for tx %s", action.TxID)
	}
	abandon := *action.Abandon
	if !abandon && action.ReplacementID == "" { // keep waiting
		return w.amendPendingTx(action.TxID, func(_ common.Hash, _ *types.Transaction, pendingTx *extendedWalletTx, idx int) error {
			pendingTx.actionIgnored = time.Now()
			return nil
		})
	}
	if abandon { // abandon
		return w.amendPendingTx(action.TxID, func(txHash common.Hash, _ *types.Transaction, wt *extendedWalletTx, idx int) error {
			w.log.Infof("Abandoning transaction %s via user action", txHash)
			wt.AssumedLost = true
			w.tryStoreDBTx(wt)
			pendingTx := w.pendingTxs[idx]
			if pendingTx.Nonce.Cmp(w.confirmedNonceAt) == 0 {
				w.pendingNonceAt.Add(w.pendingNonceAt, big.NewInt(-1))
			}
			copy(w.pendingTxs[idx:], w.pendingTxs[idx+1:])
			w.pendingTxs = w.pendingTxs[:len(w.pendingTxs)-1]
			return nil
		})
	}

	replacementHash := common.HexToHash(action.ReplacementID)
	replacementTx, _, err := w.node.getTransaction(w.ctx, replacementHash)
	if err != nil {
		return fmt.Errorf("error fetching nonce replacement tx: %v", err)
	}

	from, err := types.LatestSigner(w.node.chainConfig()).Sender(replacementTx)
	if err != nil {
		return fmt.Errorf("error parsing originator address from specified replacement tx %s: %w", from, err)
	}
	if from != w.addr {
		return fmt.Errorf("specified replacement tx originator %s is not you", from)
	}

	return w.amendPendingTx(action.TxID, func(txHash common.Hash, oldTx *types.Transaction, pendingTx *extendedWalletTx, idx int) error {
		if replacementTx.Nonce() != pendingTx.Nonce.Uint64() {
			return fmt.Errorf("nonce replacement doesn't have the right nonce. %d != %s", replacementTx.Nonce(), pendingTx.Nonce)
		}
		recipient := w.addr.Hex()
		newPendingTx := w.extendedTx(&genTxResult{
			tx:        replacementTx,
			txType:    asset.Unknown,
			amt:       0,
			recipient: &recipient,
		})
		pendingTx.NonceReplacement = newPendingTx.ID
		var oldTo, newTo common.Address
		if oldAddr := oldTx.To(); oldAddr != nil {
			oldTo = *oldAddr
		}
		if newAddr := replacementTx.To(); newAddr != nil {
			newTo = *newAddr
		}
		if bytes.Equal(oldTx.Data(), replacementTx.Data()) && oldTo == newTo {
			pendingTx.FeeReplacement = true
		}
		w.tryStoreDBTx(pendingTx)
		w.tryStoreDBTx(newPendingTx)
		w.pendingTxs[idx] = newPendingTx
		return nil
	})
}

// userActionRecoverNonces, if recover is true, examines our confirmed and
// pending nonces and our pendingTx set and sends zero-value txs to ourselves
// to fill any gaps or replace any rogue transactions. This should never happen
// if we're only running one instance of this wallet.
func (w *assetWallet) userActionRecoverNonces(actionB []byte) error {
	var action struct {
		Recover *bool `json:"recover"`
	}
	if err := json.Unmarshal(actionB, &action); err != nil {
		return fmt.Errorf("error unmarshaling user action: %v", err)
	}
	if action.Recover == nil {
		return errors.New("no recover value specified")
	}
	if !*action.Recover {
		// Don't reset recoveryRequestSent. They won't see this message again until
		// they reboot.
		w.emit.ActionResolved(w.missingNoncesActionID())
		return nil
	}
	maxFeeRate, tipRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return fmt.Errorf("error getting max fee rate for nonce resolution: %v", err)
	}
	w.nonceMtx.Lock()
	defer w.nonceMtx.Unlock()
	missingNonces := findMissingNonces(w.confirmedNonceAt, w.pendingNonceAt, w.pendingTxs)
	if len(missingNonces) == 0 {
		w.emit.ActionResolved(w.missingNoncesActionID())
		return nil
	}
	for i, n := range missingNonces {
		nonce := new(big.Int).SetUint64(n)
		txOpts, err := w.node.txOpts(w.ctx, 0, defaultSendGasLimit, maxFeeRate, tipRate, nonce)
		if err != nil {
			return fmt.Errorf("error getting tx opts for nonce resolution: %v", err)
		}
		var skip bool
		tx, err := w.node.sendTransaction(w.ctx, txOpts, w.addr, nil, func(err error) (discard, propagate, fail bool) {
			if errorFilter(err, "replacement transaction underpriced") {
				skip = true
				return true, false, false
			}
			return false, false, true
		})
		if err != nil {
			return fmt.Errorf("error sending tx %d for nonce resolution: %v", nonce, err)
		}
		if skip {
			w.log.Warnf("skipping storing underpriced replacement tx for nonce %d", nonce)
		} else {
			recipient := w.addr.Hex()
			pendingTx := w.extendAndStoreTx(&genTxResult{
				tx:        tx,
				txType:    asset.SelfSend,
				amt:       0,
				recipient: &recipient,
			}, nil)
			w.emitTransactionNote(pendingTx.WalletTransaction, true)
			w.pendingTxs = append(w.pendingTxs, pendingTx)
			sort.Slice(w.pendingTxs, func(i, j int) bool {
				return w.pendingTxs[i].Nonce.Cmp(w.pendingTxs[j].Nonce) < 0
			})
		}
		if i < len(missingNonces)-1 {
			select {
			case <-time.After(time.Second * 1):
			case <-w.ctx.Done():
				return nil
			}
		}
	}
	w.emit.ActionResolved(w.missingNoncesActionID())
	return nil
}

// requestAction sends a ActionRequired notification up the chain of command.
// nonceMtx must be locked.
func (w *baseWallet) requestAction(actionID, uniqueID string, req *TransactionActionNote, tokenID *uint32) {
	assetID := w.baseChainID
	if tokenID != nil {
		assetID = *tokenID
	}
	aw := w.wallet(assetID)
	if aw == nil { // sanity
		return
	}
	aw.emit.ActionRequired(uniqueID, actionID, req)
}

// resolveAction sends a ActionResolved notification up the chain of command.
// nonceMtx must be locked.
func (w *baseWallet) resolveAction(uniqueID string, tokenID *uint32) {
	assetID := w.baseChainID
	if tokenID != nil {
		assetID = *tokenID
	}
	aw := w.wallet(assetID)
	if aw == nil { // sanity
		return
	}
	aw.emit.ActionResolved(uniqueID)
}

// TakeAction satisfies asset.ActionTaker. This handles responses from the
// user for an ActionRequired request, usually for a stuck tx or otherwise
// abnormal condition.
func (w *assetWallet) TakeAction(actionID string, actionB []byte) error {
	switch actionID {
	case actionTypeTooCheap:
		return w.userActionBumpFees(actionB)
	case actionTypeMissingNonces:
		return w.userActionRecoverNonces(actionB)
	case actionTypeLostNonce:
		return w.userActionNonceReplacement(actionB)
	default:
		return fmt.Errorf("unknown action %q", actionID)
	}
}

const txHistoryNonceKey = "Nonce"

// transactionFeeLimit calculates the maximum tx fees that are allowed by a tx.
func transactionFeeLimit(tx *types.Transaction) *big.Int {
	fees := new(big.Int)
	feeCap, tipCap := tx.GasFeeCap(), tx.GasTipCap()
	if feeCap != nil && tipCap != nil {
		fees.Add(fees, feeCap)
		fees.Add(fees, tipCap)
		fees.Mul(fees, new(big.Int).SetUint64(tx.Gas()))
	}
	return fees
}

// extendedTx generates an *extendedWalletTx for a newly-broadcasted tx and
// stores it in the DB.
func (w *assetWallet) extendedTx(genTxResult *genTxResult) *extendedWalletTx {
	var tokenAssetID *uint32
	if w.assetID != w.baseChainID {
		tokenAssetID = &w.assetID
	}
	return w.baseWallet.extendAndStoreTx(genTxResult, tokenAssetID)
}

func (w *baseWallet) extendAndStoreTx(genTxResult *genTxResult, tokenAssetID *uint32) *extendedWalletTx {
	nonce := genTxResult.tx.Nonce()
	rawTx, err := genTxResult.tx.MarshalBinary()
	if err != nil {
		w.log.Errorf("Error marshaling tx %q: %v", genTxResult.tx.Hash(), err)
	}

	if genTxResult.recipient == nil {
		if to := genTxResult.tx.To(); to != nil {
			s := to.String()
			genTxResult.recipient = &s
		}
	}

	now := time.Now()

	wt := &extendedWalletTx{
		WalletTransaction: &asset.WalletTransaction{
			Type:      genTxResult.txType,
			ID:        genTxResult.tx.Hash().String(),
			Amount:    genTxResult.amt,
			Fees:      dexeth.WeiToGweiCeil(transactionFeeLimit(genTxResult.tx)), // updated later
			TokenID:   tokenAssetID,
			Recipient: genTxResult.recipient,
			AdditionalData: map[string]string{
				txHistoryNonceKey: strconv.FormatUint(nonce, 10),
			},
		},
		SubmissionTime: uint64(now.Unix()),
		RawTx:          rawTx,
		Nonce:          big.NewInt(int64(nonce)),
		txHash:         genTxResult.tx.Hash(),
		savedToDB:      true,
		lastBroadcast:  now,
		lastFeeCheck:   now,
	}

	if genTxResult.bridgeCounterpartAssetID != nil {
		var cpTxID string
		var cpAssetID uint32
		var completionTime uint64
		if genTxResult.bridgeCounterpartTxID != nil {
			cpTxID = *genTxResult.bridgeCounterpartTxID
		}
		if genTxResult.bridgeCounterpartAssetID != nil {
			cpAssetID = *genTxResult.bridgeCounterpartAssetID
		}
		if genTxResult.bridgeCompletionTime != nil {
			completionTime = *genTxResult.bridgeCompletionTime
		}
		wt.BridgeCounterpartTx = &asset.BridgeCounterpartTx{
			AssetID:        cpAssetID,
			ID:             cpTxID,
			CompletionTime: completionTime,
		}
	}

	w.tryStoreDBTx(wt)

	return wt
}

// TxHistory returns all the transactions the wallet has made. This
// includes the ETH wallet and all token wallets. If refID is nil, then
// transactions starting from the most recent are returned (past is ignored).
// If past is true, the transactions prior to the refID are returned, otherwise
// the transactions after the refID are returned. n is the number of
// transactions to return. If n is <= 0, all the transactions will be returned.
func (w *ETHWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	return w.txHistory(n, refID, past, nil)
}

// TxHistory returns all the transactions the token wallet has made. If refID
// is nil, then transactions starting from the most recent are returned (past
// is ignored). If past is true, the transactions prior to the refID are
// returned, otherwise the transactions after the refID are returned. n is the
// number of transactions to return. If n is <= 0, all the transactions will be
// returned.
func (w *TokenWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	return w.txHistory(n, refID, past, &w.assetID)
}

func (w *baseWallet) txHistory(n int, refID *string, past bool, assetID *uint32) ([]*asset.WalletTransaction, error) {
	var hashID *common.Hash
	if refID != nil {
		h := common.HexToHash(*refID)
		if h == (common.Hash{}) {
			return nil, fmt.Errorf("invalid reference ID %q provided", *refID)
		}
		hashID = &h
	}
	return w.txDB.getTxs(n, hashID, past, assetID)
}

func (w *ETHWallet) getReceivingTransaction(ctx context.Context, txHash common.Hash) (*asset.WalletTransaction, error) {
	tx, blockHeight, err := w.node.getTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}

	if *tx.To() != w.addr {
		return nil, asset.CoinNotFoundError
	}

	addr := w.addr.String()
	return &asset.WalletTransaction{
		Type:      asset.Receive,
		ID:        tx.Hash().String(),
		Amount:    w.atomize(tx.Value()),
		Recipient: &addr,
		AdditionalData: map[string]string{
			txHistoryNonceKey: strconv.FormatUint(tx.Nonce(), 10),
		},
		// For receiving transactions, if the transaction is mined, it is
		// confirmed confirmed, because the value received will be part of
		// the available balance.
		Confirmed: blockHeight > 0,
	}, nil
}

// WalletTransaction returns a transaction that either the wallet has made or
// one in which the wallet has received funds.
func (w *ETHWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txHash := common.HexToHash(txID)
	var localTx asset.WalletTransaction
	if w.withLocalTxRead(txHash, func(wt *extendedWalletTx) {
		localTx = *wt.WalletTransaction
	}) {
		// Do not modify tx in pending tx map. Only modify copy.
		// Confirmed should be true, because the value is part of the available
		// balance.
		if localTx.BlockNumber > 0 {
			localTx.Confirmed = true
		}
		return &localTx, nil
	}
	return w.getReceivingTransaction(ctx, txHash)
}

// extractValueFromTransferLog checks the Transfer event logs in the
// transaction, finds the log that sends tokens to the wallet's address,
// and returns the value of the transfer.
func (w *TokenWallet) extractValueFromTransferLog(receipt *types.Receipt) (v uint64, err error) {
	return v, w.withTokenContractor(w.assetID, dexeth.ContractVersionERC20, func(c tokenContractor) error {
		v, err = c.parseTransfer(receipt)
		return err
	})
}

func (w *TokenWallet) getReceivingTransaction(ctx context.Context, txHash common.Hash) (*asset.WalletTransaction, error) {
	receipt, _, err := w.node.transactionAndReceipt(ctx, txHash)
	if err != nil {
		return nil, err
	}

	blockHeight := receipt.BlockNumber.Int64()
	value, err := w.extractValueFromTransferLog(receipt)
	if err != nil {
		w.log.Errorf("Error extracting value from transfer log: %v", err)
		return &asset.WalletTransaction{
			Type:        asset.Unknown,
			ID:          txHash.String(),
			BlockNumber: uint64(blockHeight),
			Confirmed:   blockHeight > 0,
		}, nil
	}

	addr := w.addr.String()
	return &asset.WalletTransaction{
		Type:        asset.Receive,
		ID:          txHash.String(),
		BlockNumber: uint64(blockHeight),
		TokenID:     &w.assetID,
		Amount:      value,
		Recipient:   &addr,
		// For receiving transactions, if the transaction is mined, it is
		// confirmed confirmed, because the value received will be part of
		// the available balance.
		Confirmed: blockHeight > 0,
	}, nil
}

func (w *TokenWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	txHash := common.HexToHash(txID)
	var localTx asset.WalletTransaction
	if w.withLocalTxRead(txHash, func(wt *extendedWalletTx) {
		localTx = *wt.WalletTransaction
	}) {
		// Do not modify tx in pending tx map. Only modify copy.
		// Confirmed should be true, because the value is part of the available
		// balance.
		if localTx.BlockNumber > 0 {
			localTx.Confirmed = true
		}
		return &localTx, nil
	}
	return w.getReceivingTransaction(ctx, txHash)
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
	if len(providers) == 0 {
		return nil, nil, fmt.Errorf("no providers in the file at %s for chain %s, network %s", path, chain, net)
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

	cl, err := newMultiRPCClient(walletDir, providers, log, wParams.ChainCfg, 3, net)
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
		c, err = ctor(net, wParams.ContractAddr, cl.address(), cl.contractBackend())
		if err != nil {
			return nil, nil, fmt.Errorf("contractor constructor error: %v", err)
		}
	} else {
		switch contractVer {
		case 0:
			c, err = newV0TokenContractor(net, wParams.Token, cl.address(), cl.contractBackend())
			if err != nil {
				return nil, nil, fmt.Errorf("token contractor constructor error: %v", err)
			}
		case 1:
			bc, err := newV1Contractor(net, wParams.ContractAddr, cl.address(), cl.contractBackend())
			if err != nil {
				return nil, nil, fmt.Errorf("base contractor constructor error: %v", err)
			}
			c, err = bc.(unifiedContractor).tokenContractor(wParams.Token)
			if err != nil {
				return nil, nil, fmt.Errorf("tokenContractor error: %v", err)
			}
		}
	}
	success = true
	return cl, c, nil
}

// waitForConfirmation waits for the specified transaction to have > 0
// confirmations.
func waitForConfirmation(ctx context.Context, desc string, cl ethFetcher, txHash common.Hash, log dex.Logger) error {
	bestHdr, err := cl.bestHeader(ctx)
	if err != nil {
		return fmt.Errorf("error getting best header: %w", err)
	}
	ticker := time.NewTicker(stateUpdateTick)
	lastReport := time.Now()
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
				if time.Since(lastReport) > time.Second*30 {
					log.Infof("Awaiting confirmations for %s tx %s", desc, txHash)
					lastReport = time.Now()
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

func getGetGasClientWithEstimatesAndBalances(
	ctx context.Context,
	net dex.Network,
	contractVer uint32,
	maxSwaps int,
	walletDir string,
	providers []string,
	seed []byte,
	wParams *GetGasWalletParams,
	log dex.Logger,
) (
	cl *multiRPCClient,
	c contractor,
	ethReq,
	swapReq,
	feeRate uint64,
	ethBal,
	tokenBal *big.Int,
	err error,
) {

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

	feeRate = dexeth.WeiToGweiCeil(new(big.Int).Add(tip, new(big.Int).Mul(base, big.NewInt(2))))

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

	cl, _, ethReq, swapReq, _, ethBalBig, tokenBalBig, err := getGetGasClientWithEstimatesAndBalances(ctx, net, contractVer, maxSwaps, walletDir, providers, seed, wParams, log)
	if err != nil {
		return fmt.Errorf("%s: getGetGasClientWithEstimatesAndBalances error: %w", symbol, err)
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

	baseRate, tipRate, err := cl.currentFees(ctx)
	if err != nil {
		return fmt.Errorf("Error estimating fee rate: %v", err)
	}
	maxFeeRate := new(big.Int).Add(tipRate, new(big.Int).Mul(baseRate, big.NewInt(2)))

	return GetGas.returnFunds(ctx, cl, maxFeeRate, tipRate, common.HexToAddress(returnAddr), wParams.Token, wParams.UnitInfo, log, net)
}

func (getGas) returnFunds(
	ctx context.Context,
	cl *multiRPCClient,
	maxFeeRate *big.Int,
	tipRate *big.Int,
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
		fees := g.Transfer * dexeth.WeiToGweiCeil(maxFeeRate)
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

		txOpts, err := cl.txOpts(ctx, 0, g.Transfer, maxFeeRate, tipRate, nil)
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

	bigFees := new(big.Int).Mul(new(big.Int).SetUint64(defaultSendGasLimit), maxFeeRate)

	fees := dexeth.WeiToGweiCeil(bigFees)

	ethFmt := ui.ConventionalString
	if fees >= ethBal {
		return fmt.Errorf("balance is lower than projected fees: %s < %s", ethFmt(ethBal), ethFmt(fees))
	}

	remainder := ethBal - fees

	txOpts, err := cl.txOpts(ctx, remainder, defaultSendGasLimit, maxFeeRate, tipRate, nil)
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

	if *wParams.Gas == (dexeth.Gases{}) {
		return fmt.Errorf("empty gas table. put some estimates in VersionedGases or Tokens for this contract")
	}

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

	cl, c, ethReq, swapReq, feeRate, ethBal, tokenBal, err := getGetGasClientWithEstimatesAndBalances(ctx, net, contractVer, maxSwaps, walletDir, providers, seed, wParams, log)
	if err != nil {
		return fmt.Errorf("%s: getGetGasClientWithEstimatesAndBalances error: %w", symbol, err)
	}
	defer cl.shutdown()

	log.Infof("Initiator address: %s", cl.address())

	ui := wParams.UnitInfo
	assetFmt := ui.ConventionalString
	bui := wParams.BaseUnitInfo
	bUnit := bui.Conventional.Unit
	ethFmt := bui.ConventionalString

	atomicBal := dexeth.WeiToGwei(ethBal)
	log.Infof("%s balance: %s", bUnit, ethFmt(atomicBal))
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
	evmify := dexeth.GweiToWei
	if isToken {
		evmify = wParams.Token.AtomicToEVM
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
		txOpts, err := cl.txOpts(ctx, feePreload, defaultSendGasLimit, nil, nil, nil)
		if err != nil {
			return fmt.Errorf("error creating tx opts for sending fees for approval client: %v", err)
		}

		tx, err := cl.sendTransaction(ctx, txOpts, approvalClient.address(), nil)
		if err != nil {
			return fmt.Errorf("error sending fee reserves to approval client: %v", err)
		}
		log.Infof("Funded approval client gas with %s in transaction %s", wParams.UnitInfo.FormatAtoms(feePreload), tx.Hash())
		if err = waitForConfirmation(ctx, "approval client fee funding", approvalClient, tx.Hash(), log); err != nil {
			return fmt.Errorf("error waiting for approval fee funding tx: %w", err)
		}

	} else {
		log.Infof("%d gwei %s required for fees and swaps", ethReq, bui.Conventional.Unit)
	}

	log.Debugf("Getting gas estimates")
	return getGasEstimates(ctx, cl, approvalClient, c, approvalContractor, maxSwaps, contractVer, wParams.Gas, evmify, log)
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
	maxSwaps int, contractVer uint32, g *dexeth.Gases, evmify func(v uint64) *big.Int, log dex.Logger) (err error) {

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

	baseRate, tipRate, err := cl.currentFees(ctx)
	if err != nil {
		return fmt.Errorf("error getting network fees: %v", err)
	}

	maxFeeRate := new(big.Int).Add(tipRate, new(big.Int).Mul(baseRate, big.NewInt(2)))

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

	logTx := func(tag string, n int, tx *types.Transaction) {
		log.Infof("%s %d tx, hash = %s, nonce = %d, maxFeeRate = %s, tip cap = %s",
			tag, n, tx.Hash(), tx.Nonce(), tx.GasFeeCap(), tx.GasTipCap())
	}

	// Estimate approve for tokens.
	if isToken {
		sendApprove := func(cl ethFetcher, c tokenContractor) error {
			txOpts, err := cl.txOpts(ctx, 0, g.Approve*2, maxFeeRate, tipRate, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for approve: %w", err)
			}
			tx, err := c.approve(txOpts, unlimitedAllowance)
			if err != nil {
				return fmt.Errorf("error estimating approve gas: %w", err)
			}
			logTx("Approve", 1, tx)
			if err = waitForConfirmation(ctx, "approval", cl, tx.Hash(), log); err != nil {
				return fmt.Errorf("error waiting for approve transaction: %w", err)
			}
			receipt, _, err := cl.transactionAndReceipt(ctx, tx.Hash())
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

		txOpts, err := cl.txOpts(ctx, 0, g.Transfer*2, maxFeeRate, tipRate, nil)
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
		logTx("Transfer", 1, transferTx)
		if err = waitForConfirmation(ctx, "transfer", cl, transferTx.Hash(), log); err != nil {
			return fmt.Errorf("error waiting for transfer tx: %w", err)
		}
		receipt, _, err := cl.transactionAndReceipt(ctx, transferTx.Hash())
		if err != nil {
			return fmt.Errorf("error getting tx receipt for transfer tx: %w", err)
		}
		if err = checkTxStatus(receipt, g.Transfer*2); err != nil {
			return fmt.Errorf("transfer tx failed status check: [%w]. %d gas used out of %d available", err, receipt.GasUsed, g.Transfer*2)
		}
		log.Infof("%d gas used for transfer tx", receipt.GasUsed)
		stats.transfers = append(stats.transfers, receipt.GasUsed)
	}

	var v uint64 = 1
	for n := 1; n <= maxSwaps; n++ {
		contracts := make([]*asset.Contract, 0, n)
		secrets := make([][32]byte, 0, n)
		lockTime := time.Now().Add(-time.Hour)
		for i := 0; i < n; i++ {
			secretB := encode.RandomBytes(32)
			var secret [32]byte
			copy(secret[:], secretB)
			secretHash := sha256.Sum256(secretB)
			contracts = append(contracts, &asset.Contract{
				Address:    cl.address().String(), // trading with self
				Value:      v,
				SecretHash: secretHash[:],
				LockTime:   uint64(lockTime.Unix()),
			})
			secrets = append(secrets, secret)
		}

		var optsVal uint64
		if !isToken {
			optsVal = uint64(n)
		}

		// Send the inits
		txOpts, err := cl.txOpts(ctx, optsVal, g.SwapN(n)*2, maxFeeRate, tipRate, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for %d swaps: %v", n, err)
		}
		log.Debugf("Sending %d swaps", n)
		tx, err := c.initiate(txOpts, contracts)
		if err != nil {
			return fmt.Errorf("initiate error for %d swaps: %v", n, err)
		}
		logTx("Initiate", n, tx)
		if err = waitForConfirmation(ctx, "init", cl, tx.Hash(), log); err != nil {
			return fmt.Errorf("error waiting for init tx to be mined: %w", err)
		}
		receipt, _, err := cl.transactionAndReceipt(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("error getting init tx receipt: %w", err)
		}
		if err = checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return fmt.Errorf("init tx failed status check: %w", err)
		}
		log.Infof("%d gas used for %d initiations in tx %s", receipt.GasUsed, n, tx.Hash())
		stats.swaps = append(stats.swaps, receipt.GasUsed)

		// Estimate a refund
		refundGas, err := c.estimateRefundGas(ctx, acToLocator(contractVer, contracts[0], evmify(v), cl.address()))
		if err != nil {
			return fmt.Errorf("error estimate refund gas: %w", err)
		}
		log.Infof("%d gas estimated for a refund", refundGas)
		stats.refunds = append(stats.refunds, refundGas)

		redemptions := make([]*asset.Redemption, 0, n)
		for i, contract := range contracts {
			redemptions = append(redemptions, &asset.Redemption{
				Spends: &asset.AuditInfo{
					Recipient:  cl.address().String(),
					Expiration: lockTime,
					Contract:   dexeth.EncodeContractData(contractVer, acToLocator(contractVer, contract, evmify(v), cl.address())),
					SecretHash: contract.SecretHash,
				},
				Secret: secrets[i][:],
			})
		}

		txOpts, err = cl.txOpts(ctx, 0, g.RedeemN(n)*2, maxFeeRate, tipRate, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for %d redeems: %v", n, err)
		}
		log.Debugf("Sending %d redemptions", n)
		tx, err = c.redeem(txOpts, redemptions)
		if err != nil {
			return fmt.Errorf("redeem error for %d swaps: %v", n, err)
		}
		logTx("Redeem", n, tx)
		if err = waitForConfirmation(ctx, "redeem", cl, tx.Hash(), log); err != nil {
			return fmt.Errorf("error waiting for redeem tx to be mined: %w", err)
		}
		receipt, _, err = cl.transactionAndReceipt(ctx, tx.Hash())
		if err != nil {
			return fmt.Errorf("error getting redeem tx receipt: %w", err)
		}
		if err = checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return fmt.Errorf("redeem tx failed status check: %w", err)
		}
		log.Infof("%d gas used for %d redemptions in tx %s", receipt.GasUsed, n, tx.Hash())
		stats.redeems = append(stats.redeems, receipt.GasUsed)
	}

	return nil
}

// newTxOpts is a constructor for a TransactOpts.
func newTxOpts(ctx context.Context, from common.Address, val, maxGas uint64, maxFeeRate, gasTipCap *big.Int) *bind.TransactOpts {
	// We'll enforce dexeth.MinGasTipCap since the server does, but this isn't
	// necessarily a constant for all networks or under all conditions.
	minGasWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
	if gasTipCap.Cmp(minGasWei) < 0 {
		gasTipCap.Set(minGasWei)
	}
	// This is enforced by concensus. We shouldn't be able to get here with a
	// swap tx.
	if gasTipCap.Cmp(maxFeeRate) > 0 {
		gasTipCap.Set(maxFeeRate)
	}
	return &bind.TransactOpts{
		Context:   ctx,
		From:      from,
		Value:     dexeth.GweiToWei(val),
		GasFeeCap: maxFeeRate,
		GasTipCap: gasTipCap,
		GasLimit:  maxGas,
	}
}

func gases(contractVer uint32, versionedGases map[uint32]*dexeth.Gases) *dexeth.Gases {
	if contractVer != dexeth.ContractVersionNewest {
		return versionedGases[contractVer]
	}
	var bestVer uint32
	var bestGases *dexeth.Gases
	for ver, gases := range versionedGases {
		if ver >= bestVer {
			bestGases = gases
			bestVer = ver
		}
	}
	return bestGases
}
