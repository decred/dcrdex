// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/kvdb"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/keygen"
	dexeth "decred.org/dcrdex/dex/networks/eth"
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

func registerToken(tokenID uint32, desc string, nets ...dex.Network) {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
		Type:        "token",
		Description: desc,
	}, nets...)
}

func init() {
	asset.Register(BipID, &Driver{})
	// Test token
	registerToken(simnetTokenID, "A token wallet for the DEX test token. Used for testing DEX software.", dex.Simnet)
	registerToken(usdcTokenID, "The USDC Ethereum ERC20 token.", dex.Testnet)
}

const (
	// BipID is the BIP-0044 asset ID.
	BipID               = 60
	defaultGasFee       = 82  // gwei
	defaultGasFeeLimit  = 200 // gwei
	defaultSendGasLimit = 21_000

	walletTypeGeth = "geth"
	walletTypeRPC  = "rpc"

	providersKey = "providers"

	// confCheckTimeout is the amount of time allowed to check for
	// confirmations. Testing on testnet has shown spikes up to 2.5
	// seconds. This value may need to be adjusted in the future.
	confCheckTimeout                 = 4 * time.Second
	dynamicSwapOrRedemptionFeesConfs = 2

	// coinIDTakerFoundMakerRedemption is a prefix to identify one of CoinID formats,
	// see DecodeCoinID func for details.
	coinIDTakerFoundMakerRedemption = "TakerFoundMakerRedemption:"
)

var (
	simnetTokenID, _ = dex.BipSymbolID("dextt.eth")
	usdcTokenID, _   = dex.BipSymbolID("usdc.eth")
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker     = time.Second
	peerCountTicker = 5 * time.Second
	WalletOpts      = []*asset.ConfigOption{
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
				"providers, use an https address. Only url-based authentication " +
				"is supported. For a local node, use the filepath to an IPC file.",
			Repeatable: providerDelimiter,
			Required:   true,
		},
	}
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = &asset.WalletInfo{
		Name: "Ethereum",
		// Version and SupportedVersions set in Driver methods.
		UnitInfo: dexeth.UnitInfo,
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
				Tab:         "External",
				Description: "Infrastructure providers (e.g. Infura) or local nodes",
				ConfigOpts:  append(RPCOpts, WalletOpts...),
				Seeded:      true,
				NoAuth:      true,
			},
		},
	}

	chainIDs = map[dex.Network]int64{
		dex.Mainnet: 1,
		dex.Testnet: 5,  // Görli
		dex.Simnet:  42, // see dex/testing/eth/harness.sh
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
	return NewWallet(cfg, logger, network)
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

func ethWalletInfo() *asset.WalletInfo {
	wi := *WalletInfo
	var bestVer uint32
	for ver := range contractorConstructors {
		wi.SupportedVersions = append(wi.SupportedVersions, ver)
		if ver > bestVer {
			bestVer = ver
		}
	}
	wi.Version = bestVer
	return &wi
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return ethWalletInfo()
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
	return len(ks.Wallets()) > 0, nil
}

func (d *Driver) Create(params *asset.CreateWalletParams) error {
	return CreateWallet(params)
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
	pendingTransactions() ([]*types.Transaction, error)
	shutdown()
	sendSignedTransaction(ctx context.Context, tx *types.Transaction) error
	sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error)
	signData(data []byte) (sig, pubKey []byte, err error)
	syncProgress(context.Context) (*ethereum.SyncProgress, error)
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	getTransaction(context.Context, common.Hash) (*types.Transaction, int64, error)
	txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, nonce *big.Int) (*bind.TransactOpts, error)
	currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error)
	unlock(pw string) error
	getConfirmedNonce(context.Context, int64) (uint64, error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, *types.Transaction, error)
}

// monitoredTx is used to keep track of redemption transactions that have not
// yet been confirmed. If a transaction has to be replaced due to the fee
// being too low or another transaction being mined with the same nonce,
// the replacement transaction's ID is recorded in the replacementTx field.
// replacedTx is used to maintain a doubly linked list, which allows deletion
// of transactions that were replaced after a transaction is confirmed.
type monitoredTx struct {
	tx             *types.Transaction
	blockSubmitted uint64

	// This mutex must be held during the entire process of confirming
	// a transaction. This is to avoid confirmations of the same
	// transactions happening concurrently resulting in more than one
	// replacement for the same transaction.
	mtx           sync.Mutex
	replacementTx *common.Hash
	// replacedTx could be set when the tx is created, be immutable, and not
	// need the mutex, but since Redeem doesn't know if the transaction is a
	// replacement or a new one, this variable is set in recordReplacementTx.
	replacedTx *common.Hash
}

// MarshalBinary marshals a monitoredTx into a byte array.
// It satisfies the encoding.BinaryMarshaler interface for monitoredTx.
func (m *monitoredTx) MarshalBinary() (data []byte, err error) {
	b := encode.BuildyBytes{0}
	txB, err := m.tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error marshaling tx: %v", err)
	}
	b = b.AddData(txB)

	blockB := make([]byte, 8)
	binary.BigEndian.PutUint64(blockB, m.blockSubmitted)
	b = b.AddData(blockB)

	if m.replacementTx != nil {
		replacementTxHash := m.replacementTx[:]
		b = b.AddData(replacementTxHash)
	}

	return b, nil
}

// UnmarshalBinary loads a data from a marshalled byte array into a
// monitoredTx.
func (m *monitoredTx) UnmarshalBinary(data []byte) error {
	ver, pushes, err := encode.DecodeBlob(data)
	if err != nil {
		return err
	}
	if ver != 0 {
		return fmt.Errorf("unknown version %d", ver)
	}
	if len(pushes) != 2 && len(pushes) != 3 {
		return fmt.Errorf("wrong number of pushes %d", len(pushes))
	}
	m.tx = &types.Transaction{}
	if err := m.tx.UnmarshalBinary(pushes[0]); err != nil {
		return fmt.Errorf("error reading tx: %w", err)
	}

	m.blockSubmitted = binary.BigEndian.Uint64(pushes[1])

	if len(pushes) == 3 {
		var replacementTxHash common.Hash
		copy(replacementTxHash[:], pushes[2])
		m.replacementTx = &replacementTxHash
	}

	return nil
}

// Check that assetWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*ETHWallet)(nil)
var _ asset.Wallet = (*TokenWallet)(nil)
var _ asset.AccountLocker = (*ETHWallet)(nil)
var _ asset.AccountLocker = (*TokenWallet)(nil)
var _ asset.TokenMaster = (*ETHWallet)(nil)
var _ asset.WalletRestorer = (*assetWallet)(nil)
var _ asset.LiveReconfigurer = (*ETHWallet)(nil)
var _ asset.LiveReconfigurer = (*TokenWallet)(nil)
var _ asset.TxFeeEstimator = (*ETHWallet)(nil)
var _ asset.TxFeeEstimator = (*TokenWallet)(nil)
var _ asset.DynamicSwapOrRedemptionFeeChecker = (*ETHWallet)(nil)
var _ asset.DynamicSwapOrRedemptionFeeChecker = (*TokenWallet)(nil)
var _ asset.BotWallet = (*assetWallet)(nil)

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

	settingsMtx sync.RWMutex
	settings    map[string]string

	gasFeeLimitV uint64 // atomic

	walletsMtx sync.RWMutex
	wallets    map[uint32]*assetWallet

	monitoredTxsMtx sync.RWMutex
	monitoredTxs    map[common.Hash]*monitoredTx
	monitoredTxDB   kvdb.KeyValueDB

	// nonceSendMtx should be locked for the node.txOpts -> tx send sequence
	// for all txs, to ensure nonce correctness.
	nonceSendMtx sync.Mutex
}

// assetWallet is a wallet backend for Ethereum and Eth tokens. The backend is
// how the DEX client app communicates with the Ethereum blockchain and wallet.
// assetWallet satisfies the dex.Wallet interface.
type assetWallet struct {
	*baseWallet
	assetID    uint32
	tipChange  func(error)
	log        dex.Logger
	atomicUnit string

	lockedFunds struct {
		mtx                sync.RWMutex
		initiateReserves   uint64
		redemptionReserves uint64
		refundReserves     uint64
	}

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[[32]byte]*findRedemptionRequest

	lastPeerCount uint32
	peersChange   func(uint32, error)

	contractors map[uint32]contractor // version -> contractor

	evmify  func(uint64) *big.Int
	atomize func(*big.Int) uint64

	maxSwapsInTx   uint64
	maxRedeemsInTx uint64
}

// ETHWallet implements some Ethereum-specific methods.
type ETHWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	*assetWallet

	tipMtx     sync.RWMutex
	currentTip *types.Header
}

// TokenWallet implements some token-specific methods.
type TokenWallet struct {
	*assetWallet

	cfg      *tokenWalletConfig
	parent   *assetWallet
	approval atomic.Value
	token    *dexeth.Token
	netToken *dexeth.NetToken
}

// maxProportionOfBlockGasLimitToUse sets the maximum proportion of a block's
// gas limit that a swap and redeem transaction will use. Since it is set to
// 4, the max that will be used is 25% (1/4) of the block's gas limit.
const maxProportionOfBlockGasLimitToUse = 4

// Info returns basic information about the wallet and asset.
func (w *ETHWallet) Info() *asset.WalletInfo {
	wi := ethWalletInfo()
	wi.MaxSwapsInTx = w.maxSwapsInTx
	wi.MaxRedeemsInTx = w.maxRedeemsInTx
	return wi
}

// Info returns basic information about the wallet and asset.
func (w *TokenWallet) Info() *asset.WalletInfo {
	var bestVer uint32
	var vers []uint32
	netToken := w.token.NetTokens[w.net]
	if netToken != nil {
		for ver := range netToken.SwapContracts {
			vers = append(vers, ver)
			if ver > bestVer {
				bestVer = ver
			}
		}
	} // maybe panic

	return &asset.WalletInfo{
		Name:              w.token.Name,
		Version:           bestVer,
		SupportedVersions: vers,
		UnitInfo:          w.token.UnitInfo,
		MaxSwapsInTx:      w.maxSwapsInTx,
		MaxRedeemsInTx:    w.maxRedeemsInTx,
	}
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

// CreateWallet creates a new internal ETH wallet and stores the private key
// derived from the wallet seed.
func CreateWallet(cfg *asset.CreateWalletParams) error {
	return createWallet(cfg, false)
}

func createWallet(createWalletParams *asset.CreateWalletParams, skipConnect bool) error {
	switch createWalletParams.Type {
	case walletTypeGeth:
		return asset.ErrWalletTypeDisabled
	case walletTypeRPC:
	default:
		return fmt.Errorf("wallet type %q unrecognized", createWalletParams.Type)
	}

	walletDir := getWalletDir(createWalletParams.DataDir, createWalletParams.Net)

	walletSeed, err := genWalletSeed(createWalletParams.Seed)
	if err != nil {
		return err
	}
	defer encode.ClearBytes(walletSeed)

	extKey, err := keygen.GenDeepChild(walletSeed, seedDerivationPath)
	if err != nil {
		return err
	}
	defer extKey.Zero()
	privateKey, err := extKey.SerializedPrivKey()
	defer encode.ClearBytes(privateKey)
	if err != nil {
		return err
	}

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

		// Check that we can connect to all endpoints.
		providerDef := createWalletParams.Settings[providersKey]
		if len(providerDef) == 0 {
			return errors.New("no providers specified")
		}
		endpoints := strings.Split(providerDef, providerDelimiter)
		n := len(endpoints)

		// TODO: This procedure may actually work for walletTypeGeth too.
		ks := keystore.NewKeyStore(filepath.Join(walletDir, "keystore"), keystore.LightScryptN, keystore.LightScryptP)

		priv, err := crypto.ToECDSA(privateKey)
		if err != nil {
			return err
		}

		if !skipConnect {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			var unknownEndpoints []string

			for _, endpoint := range endpoints {
				known, compliant := providerIsCompliant(endpoint)
				if known && !compliant {
					return fmt.Errorf("provider %q is known to have an insufficient API for DEX", endpoint)
				} else if !known {
					unknownEndpoints = append(unknownEndpoints, endpoint)
				}
			}

			if len(unknownEndpoints) > 0 && createWalletParams.Net == dex.Mainnet {
				providers, err := connectProviders(ctx, unknownEndpoints, createWalletParams.Logger, big.NewInt(chainIDs[createWalletParams.Net]))
				if err != nil {
					return err
				}
				defer func() {
					for _, p := range providers {
						p.ec.Close()
					}
				}()
				if len(providers) != n {
					return fmt.Errorf("Could not connect to all providers")
				}
				if err := checkProvidersCompliance(ctx, walletDir, providers, createWalletParams.Logger); err != nil {
					return err
				}
			}
		}
		return importKeyToKeyStore(ks, priv, createWalletParams.Pass)
	}

	return fmt.Errorf("unknown wallet type %q", createWalletParams.Type)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. It starts an internal light node.
func NewWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (w *ETHWallet, err error) {
	// var cl ethFetcher
	switch assetCFG.Type {
	case walletTypeGeth:
		return nil, asset.ErrWalletTypeDisabled
	case walletTypeRPC:
		if _, found := assetCFG.Settings[providersKey]; !found {
			return nil, errors.New("no providers specified")
		}
	default:
		return nil, fmt.Errorf("unknown wallet type %q", assetCFG.Type)
	}

	cfg, err := parseWalletConfig(assetCFG.Settings)
	if err != nil {
		return nil, err
	}

	gasFeeLimit := cfg.GasFeeLimit
	if gasFeeLimit == 0 {
		gasFeeLimit = defaultGasFeeLimit
	}

	eth := &baseWallet{
		log:          logger,
		net:          net,
		dir:          assetCFG.DataDir,
		walletType:   assetCFG.Type,
		settings:     assetCFG.Settings,
		gasFeeLimitV: gasFeeLimit,
		wallets:      make(map[uint32]*assetWallet),
		monitoredTxs: make(map[common.Hash]*monitoredTx),
	}

	gasCeil := ethconfig.Defaults.Miner.GasCeil
	var maxSwapGas, maxRedeemGas uint64
	for _, gases := range dexeth.VersionedGases {
		if gases.Swap > maxSwapGas {
			maxSwapGas = gases.Swap
		}
		if gases.Redeem > maxRedeemGas {
			maxRedeemGas = gases.Redeem
		}
	}

	aw := &assetWallet{
		baseWallet:         eth,
		log:                logger.SubLogger("ETH"),
		assetID:            BipID,
		tipChange:          assetCFG.TipChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		peersChange:        assetCFG.PeersChange,
		contractors:        make(map[uint32]contractor),
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
		atomicUnit:         dexeth.UnitInfo.AtomicUnit,
		maxSwapsInTx:       gasCeil / maxProportionOfBlockGasLimitToUse / maxSwapGas,
		maxRedeemsInTx:     gasCeil / maxProportionOfBlockGasLimitToUse / maxRedeemGas,
	}

	aw.wallets = map[uint32]*assetWallet{
		BipID: aw,
	}

	return &ETHWallet{
		assetWallet: aw,
	}, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

func (w *ETHWallet) shutdown() {
	w.node.shutdown()
	if err := w.monitoredTxDB.Close(); err != nil {
		w.log.Errorf("error closing tx db: %v", err)
	}
}

// loadMonitoredTxs takes all of the monitored tx from the db and puts them
// into an in memory map.
func loadMonitoredTxs(db kvdb.KeyValueDB) (map[common.Hash]*monitoredTx, error) {
	monitoredTxs := make(map[common.Hash]*monitoredTx)

	if err := db.ForEach(func(k, v []byte) error {
		var h common.Hash
		copy(h[:], k)

		txRec := &monitoredTx{}
		if err := txRec.UnmarshalBinary(v); err != nil {
			return err
		}

		monitoredTxs[h] = txRec
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to load txs to monitor: %w", err)
	}

	return monitoredTxs, nil
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
		endpoints := strings.Split(w.settings[providersKey], " ")
		ethCfg, err := ethChainConfig(w.net)
		if err != nil {
			return nil, err
		}
		chainConfig := ethCfg.Genesis.Config

		// Point to a harness node on simnet, if not specified.
		if w.net == dex.Simnet && len(endpoints) == 0 {
			u, _ := user.Current()
			endpoints = append(endpoints, filepath.Join(u.HomeDir, "dextest", "eth", "beta", "node", "geth.ipc"))
		}

		cl, err = newMultiRPCClient(w.dir, endpoints, w.log.SubLogger("RPC"), chainConfig, big.NewInt(chainIDs[w.net]), w.net)
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
		c, err := constructor(w.net, w.addr, w.node.contractBackend())
		if err != nil {
			return nil, fmt.Errorf("error constructor version %d contractor: %v", ver, err)
		}
		w.contractors[ver] = c
	}

	db, err := kvdb.NewFileDB(filepath.Join(w.dir, "tx.db"), w.log.SubLogger("TXDB"))
	if err != nil {
		return nil, err
	}

	w.monitoredTxDB = db
	w.monitoredTxs, err = loadMonitoredTxs(w.monitoredTxDB)
	if err != nil {
		return nil, err
	}

	// Initialize the best block.
	bestHdr, err := w.node.bestHeader(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block hash from geth: %w", err)
	}
	w.tipMtx.Lock()
	w.currentTip = bestHdr
	w.tipMtx.Unlock()
	height := w.currentTip.Number
	// NOTE: We should be using the tipAtConnect to set Progress in SyncStatus.
	atomic.StoreInt64(&w.tipAtConnect, height.Int64())
	w.log.Infof("Connected to geth, at height %d", height)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorBlocks(ctx, w.tipChange)
		w.shutdown()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorPeers(ctx)
	}()
	return &wg, nil
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

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case <-w.parent.ctx.Done():
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

	if rpc, is := w.node.(*multiRPCClient); is {
		if err := rpc.reconfigure(ctx, cfg.Settings); err != nil {
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

func (eth *baseWallet) walletList() []*assetWallet {
	eth.walletsMtx.RLock()
	defer eth.walletsMtx.RUnlock()
	m := make([]*assetWallet, 0, len(eth.wallets))
	for _, w := range eth.wallets {
		m = append(m, w)
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
func (*baseWallet) CreateTokenWallet(tokenID uint32, _ map[string]string) error {
	// Just check that the token exists for now.
	if dexeth.Tokens[tokenID] == nil {
		return fmt.Errorf("token not found for asset ID %d", tokenID)
	}
	return nil
}

// OpenTokenWallet creates a new TokenWallet.
func (w *ETHWallet) OpenTokenWallet(tokenCfg *asset.TokenConfig) (asset.Wallet, error) {
	token, found := dexeth.Tokens[tokenCfg.AssetID]
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

	gasCeil := ethconfig.Defaults.Miner.GasCeil
	var maxSwapGas, maxRedeemGas uint64
	for _, contract := range netToken.SwapContracts {
		if contract.Gas.Swap > maxSwapGas {
			maxSwapGas = contract.Gas.Swap
		}
		if contract.Gas.Redeem > maxRedeemGas {
			maxRedeemGas = contract.Gas.Redeem
		}
	}

	aw := &assetWallet{
		baseWallet:         w.baseWallet,
		log:                w.baseWallet.log.SubLogger(strings.ToUpper(dex.BipIDSymbol(tokenCfg.AssetID))),
		assetID:            tokenCfg.AssetID,
		tipChange:          tokenCfg.TipChange,
		peersChange:        tokenCfg.PeersChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		contractors:        make(map[uint32]contractor),
		evmify:             token.AtomicToEVM,
		atomize:            token.EVMToAtomic,
		atomicUnit:         token.UnitInfo.AtomicUnit,
		maxSwapsInTx:       gasCeil / maxProportionOfBlockGasLimitToUse / maxSwapGas,
		maxRedeemsInTx:     gasCeil / maxProportionOfBlockGasLimitToUse / maxRedeemGas,
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
			dex.BipIDSymbol(w.assetID), t, amt, balance.Available, w.atomicUnit)
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
	return &asset.Balance{
		Available: w.atomize(bal.Current) - locked - w.atomize(bal.PendingOut),
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
	return w.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.MaxFeeRate, ord.AssetVersion,
		ord.RedeemVersion, ord.RedeemAssetID, nil)
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration.
func (w *TokenWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return w.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.MaxFeeRate, ord.AssetVersion,
		ord.RedeemVersion, ord.RedeemAssetID, w.parent)
}

func (w *assetWallet) maxOrder(lotSize uint64, feeSuggestion, maxFeeRate uint64, ver uint32,
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

	oneFee := g.oneGas * maxFeeRate
	var lots uint64
	if feeWallet == nil {
		lots = balance.Available / (lotSize + oneFee)
	} else { // token
		lots = balance.Available / lotSize
		parentBal, err := feeWallet.Balance()
		if err != nil {
			return nil, fmt.Errorf("error getting base chain balance: %w", err)
		}
		feeLots := parentBal.Available / oneFee
		if feeLots < lots {
			w.log.Infof("MaxOrder reducing lots because of low fee reserves: %d -> %d", lots, feeLots)
			lots = feeLots
		}
	}

	if lots < 1 {
		return &asset.SwapEstimate{}, nil
	}
	return w.estimateSwap(lots, lotSize, feeSuggestion, maxFeeRate, ver, redeemVer, redeemAssetID)
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
	maxEst, err := w.maxOrder(req.LotSize, req.FeeSuggestion, req.MaxFeeRate, req.Version,
		req.RedeemVersion, req.RedeemAssetID, feeWallet)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est, err := w.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, req.MaxFeeRate,
		req.Version, req.RedeemVersion, req.RedeemAssetID)
	if err != nil {
		return nil, err
	}

	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// SingleLotSwapFees is a fallback for PreSwap that uses estimation when funds
// aren't available. The returned fees are the RealisticWorstCase. The Lots
// field of the PreSwapForm is ignored and assumed to be a single lot.
func (w *assetWallet) SingleLotSwapFees(form *asset.PreSwapForm) (fees uint64, err error) {
	g := w.gases(form.Version)
	if g == nil {
		return 0, fmt.Errorf("no gases known for %d version %d", w.assetID, form.Version)
	}
	return g.Swap * form.FeeSuggestion, nil
}

// estimateSwap prepares an *asset.SwapEstimate. The estimate does not include
// funds that might be locked for refunds.
func (w *assetWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, maxFeeRate uint64, ver uint32,
	redeemVer, redeemAssetID uint32) (*asset.SwapEstimate, error) {
	if lots == 0 {
		return &asset.SwapEstimate{}, nil
	}

	oneSwap, nSwap, _, err := w.swapGas(int(lots), ver)
	if err != nil {
		return nil, fmt.Errorf("error getting swap gas estimate: %w", err)
	}

	value := lots * lotSize
	allowanceGas, err := w.allowanceGasRequired(redeemVer, redeemAssetID)
	if err != nil {
		return nil, err
	}

	oneGasMax := oneSwap*lots + allowanceGas
	maxFees := oneGasMax * maxFeeRate

	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: oneGasMax * feeSuggestion,
		RealisticBestCase:  (nSwap + allowanceGas) * feeSuggestion,
	}, nil
}

// allowanceGasRequired estimates the gas that is required to issue an approval
// for a token. This is required to redeem ERC20 tokens, and the assetID and
// version correspond to the redeem asset. If the asset is not a fee-family
// erc20 asset, no error is returned and the return value will be zero.
func (w *assetWallet) allowanceGasRequired(ver, assetID uint32) (uint64, error) {
	if assetID == BipID {
		return 0, nil // it's eth (no allowance)
	}
	redeemWallet := w.wallet(assetID)
	if redeemWallet == nil {
		return 0, nil // not an erc20
	}
	currentAllowance, err := redeemWallet.tokenAllowance()
	if err != nil {
		return 0, err
	}
	// No reason to do anything if the allowance is > the unlimited
	// allowance approval threshold.
	if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) < 0 {
		return redeemWallet.approvalGas(unlimitedAllowance, ver)
	}
	return 0, nil
}

// gases gets the gas table for the specified contract version.
func (w *assetWallet) gases(contractVer uint32) *dexeth.Gases {
	return gases(w.assetID, contractVer, w.net)
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

// SingleLotRedeemFees is a fallback for PreRedeem that uses estimation when
// funds aren't available. The returned fees are the RealisticWorstCase.  The
// Lots field of the PreSwapForm is ignored and assumed to be a single lot.
func (w *assetWallet) SingleLotRedeemFees(form *asset.PreRedeemForm) (fees uint64, err error) {
	g := w.gases(form.Version)
	if g == nil {
		return 0, fmt.Errorf("no gases known for %d version %d", w.assetID, form.Version)
	}
	return g.Redeem * form.FeeSuggestion, nil
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
func (w *ETHWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), ord.Version,
		ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, fmt.Errorf("error estimating swap gas: %v", err)
	}

	ethToLock := ord.MaxFeeRate*g.Swap*ord.MaxSwapCount + ord.Value
	// Note: In a future refactor, we could lock the redemption funds here too
	// and signal to the user so that they don't call `RedeemN`. This has the
	// same net effect, but avoids a lockFunds -> unlockFunds for us and likely
	// some work for the caller as well. We can't just always do it that way and
	// remove RedeemN, since we can't guarantee that the redemption asset is in
	// our fee-family. though it could still be an AccountRedeemer.

	coin := w.createFundingCoin(ethToLock)

	if err = w.lockFunds(ethToLock, initiationReserve); err != nil {
		return nil, nil, err
	}

	return asset.Coins{coin}, []dex.Bytes{nil}, nil
}

// FundOrder locks value for use in an order.
func (w *TokenWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {

	if w.gasFeeLimit() < ord.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			dex.BipIDSymbol(w.assetID), ord.MaxFeeRate, w.gasFeeLimit())
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), ord.Version,
		ord.RedeemVersion, ord.RedeemAssetID)
	if err != nil {
		return nil, nil, fmt.Errorf("error estimating swap gas: %v", err)
	}

	ethToLock := ord.MaxFeeRate * g.Swap * ord.MaxSwapCount

	if err := w.maybeApproveTokenSwapContract(ord.Version, ord.MaxFeeRate, ethToLock); err != nil {
		return nil, nil, fmt.Errorf("error issuing approval: %w", err)
	}

	var success bool
	if err = w.lockFunds(ord.Value, initiationReserve); err != nil {
		return nil, nil, fmt.Errorf("error locking token funds: %v", err)
	}
	defer func() {
		if !success {
			w.unlockFunds(ord.Value, initiationReserve)
		}
	}()

	if err := w.parent.lockFunds(ethToLock, initiationReserve); err != nil {
		return nil, nil, err
	}

	coin := w.createTokenFundingCoin(ord.Value, ethToLock)

	success = true
	return asset.Coins{coin}, []dex.Bytes{nil}, nil
}

// gasEstimates are estimates of gas required for operations involving a swap
// combination of (swap asset, redeem asset, # lots).
type gasEstimate struct {
	// The embedded are best calculated values for Swap gases, and redeem costs
	// IF the redeem asset is a fee-family asset. Otherwise Redeem and RedeemAdd
	// are zero.
	dexeth.Gases
	// Additional fields are based on live estimates of the swap.
	oneGas, nGas, nSwap, nRedeem uint64
}

// initGasEstimate gets the best available gas estimate for n initiations. A
// live estimate is checked against the server's configured values and our own
// known values and errors or logs generated in certain cases.
func (w *assetWallet) initGasEstimate(n int, initVer, redeemVer, redeemAssetID uint32) (est *gasEstimate, err error) {
	est = new(gasEstimate)

	g := w.gases(initVer)
	if g == nil {
		return nil, fmt.Errorf("no gas table")
	}

	est.Refund = g.Refund

	est.Swap, est.nSwap, _, err = w.swapGas(n, initVer)
	if err != nil {
		return nil, fmt.Errorf("error calculating swap gas: %w", err)
	}

	est.oneGas = est.Swap + est.Redeem
	est.nGas = est.nSwap + est.nRedeem

	if redeemW := w.wallet(redeemAssetID); redeemW != nil {
		est.Redeem, est.nRedeem, err = redeemW.redeemGas(n, redeemVer)
		if err != nil {
			return nil, fmt.Errorf("error calculating fee-family redeem gas: %w", err)
		}
	}

	return
}

// swapGas estimates gas for a number of initiations. swapGas will error if we
// cannot get a live estimate from the contractor, which will happen if the
// wallet has no balance.
func (w *assetWallet) swapGas(n int, ver uint32) (oneSwap, nSwap uint64, approved bool, err error) {
	g := w.gases(ver)
	if g == nil {
		return 0, 0, false, fmt.Errorf("no gases known for %d version %d", w.assetID, ver)
	}
	oneSwap = g.Swap

	// We have no way of updating the value of SwapAdd without a version change,
	// but we're not gonna worry about accuracy for nSwap, since it's only used
	// for estimates and never for dex-validated values like order funding.
	nSwap = oneSwap + uint64(n-1)*g.SwapAdd

	// If we're not approved, we can't get a live estimate, so this is as
	// far as we can go.
	if approved, err = w.isApproved(); err != nil {
		return 0, 0, false, fmt.Errorf("error checking approval on transferFrom failure: %w", err)
	} else if !approved {
		w.log.Debug("Skipping live swap gas because contract is not approved for transferFrom")
		return oneSwap, nSwap, false, nil
	}

	// If we've approved the contract to transfer, we can get a live
	// estimate to double check.

	// If a live estimate is greater than our estimate from configured values,
	// use the live estimate with a warning.
	if gasEst, err := w.estimateInitGas(w.ctx, n, ver); err != nil {
		w.log.Errorf("(%d) error estimating swap gas: %v", w.assetID, err)
		// TODO: investigate "gas required exceeds allowance".
		return 0, 0, false, err
	} else if gasEst > nSwap {
		w.log.Warnf("Swap gas estimate %d is greater than the server's configured value %d. Using live estimate + 10%.", gasEst, nSwap)
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
	fail := func(s string, a ...interface{}) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	receipts := make([]asset.Receipt, 0, len(swaps.Contracts))

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

	oneSwap, _, _, err := w.swapGas(1, swaps.Version)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}

	gasLimit := oneSwap * uint64(len(swaps.Contracts))
	fees := gasLimit * swaps.FeeRate

	if swapVal+fees > reservedVal {
		return fail("unfunded swap: %d < %d", reservedVal, swapVal+fees)
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, gasLimit, swaps.Version)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	txHash := tx.Hash()
	for _, swap := range swaps.Contracts {
		var secretHash [dexeth.SecretHashSize]byte
		copy(secretHash[:], swap.SecretHash)
		receipts = append(receipts, &swapReceipt{
			expiration:   time.Unix(int64(swap.LockTime), 0),
			value:        swap.Value,
			txHash:       txHash,
			secretHash:   secretHash,
			ver:          swaps.Version,
			contractAddr: dexeth.ContractAddresses[swaps.Version][w.net].String(),
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
	fail := func(s string, a ...interface{}) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	receipts := make([]asset.Receipt, 0, len(swaps.Contracts))

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

	oneSwap, _, approved, err := w.swapGas(1, swaps.Version)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}
	if !approved && w.approval.Load() == nil {
		return fail("cannot initiate token swap without approval")
	}

	gasLimit := oneSwap * uint64(len(swaps.Contracts))
	fees := gasLimit * swaps.FeeRate

	if swapVal > reservedVal {
		return fail("unfunded token swap: %d < %d", reservedVal, swapVal)
	}

	if fees > reservedParent {
		return fail("unfunded token swap fees: %d < %d", reservedParent, fees)
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
	gasEst, err := w.estimateRedeemGas(w.ctx, secrets, contractVer)
	if err != nil {
		return fail(fmt.Errorf("error getting redemption estimate: %w", err))
	}
	if gasEst > gasLimit {
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

// isApproved checks whether the contract is approved to transfer tokens on our
// behalf. For Ethereum, isApproved always returns (true, nil).
func (w *assetWallet) isApproved() (bool, error) {
	if w.assetID == BipID {
		return true, nil
	}
	currentAllowance, err := w.tokenAllowance()
	if err != nil {
		return false, fmt.Errorf("error retrieving current allowance: %w", err)
	}
	return currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) >= 0, nil
}

// maybeApproveTokenSwapContract checks whether a token's swap contract needs
// to be approved for the wallet address on the erc20 contract.
func (w *TokenWallet) maybeApproveTokenSwapContract(ver uint32, maxFeeRate, swapReserves uint64) error {
	// Check if we need to up the allowance.
	if approved, err := w.isApproved(); err != nil {
		return err
	} else if approved {
		return nil
	}

	// Check that we don't already have an approval pending.
	if txHashI := w.approval.Load(); txHashI != nil {
		return fmt.Errorf("an approval is already pending (%s). wait until it is mined "+
			"before ordering again", txHashI.(common.Hash))
	}

	ethBal, err := w.parent.balance()
	if err != nil {
		return fmt.Errorf("error getting eth balance: %w", err)
	}
	approveGas, err := w.approvalGas(unlimitedAllowance, ver)
	if err != nil {
		return fmt.Errorf("error estimating allowance gas: %w", err)
	}
	if (approveGas*maxFeeRate)+swapReserves > ethBal.Available {
		return fmt.Errorf("parent balance %d doesn't cover contract approval (%d) and tx fees (%d)",
			ethBal.Available, approveGas*maxFeeRate, swapReserves)
	}
	tx, err := w.approveToken(unlimitedAllowance, maxFeeRate, ver)
	if err != nil {
		return fmt.Errorf("token contract approval error (using max fee rate %d): %w", maxFeeRate, err)
	}
	w.approval.Store(tx.Hash())
	return nil
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
func (w *assetWallet) tokenAllowance() (allowance *big.Int, err error) {
	return allowance, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		allowance, err = c.allowance(w.ctx)
		return err
	})
}

// approveToken approves the token swap contract to spend tokens on behalf of
// account handled by the wallet.
func (w *assetWallet) approveToken(amount *big.Int, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(w.ctx, 0, approveGas, dexeth.GweiToWei(maxFeeRate), nil)
	if err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return tx, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
		tx, err = c.approve(txOpts, amount)
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		w.log.Infof("Approval sent for %s at token address %s, nonce = %s",
			dex.BipIDSymbol(w.assetID), c.tokenAddress(), txOpts.Nonce)
		return nil
	})
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
// is insufficient spendable balance. If an approval is necessary to increase
// the allowance to facilitate redemption, the approval is performed here.
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

	tx, err := w.refund(secretHash, feeRate, version)
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

// TODO: Lock, Unlock, and Locked should probably be part of an optional
// asset.Authenticator interface that isn't implemented by token wallets.
// This is easy to accomplish here, but would require substantial updates to
// client/core.
// The addition of an ETHWallet type that implements asset.Authenticator would
// also facilitate the appropriate compartmentalization of our asset.TokenMaster
// methods, which token wallets also won't need.

// Unlock unlocks the exchange wallet.
func (eth *baseWallet) Unlock(pw []byte) error {
	return eth.node.unlock(string(pw))
}

// Lock locks the exchange wallet.
func (eth *ETHWallet) Lock() error {
	return eth.node.lock()
}

// Lock does nothing for tokens. See above TODO.
func (eth *TokenWallet) Lock() error {
	return nil
}

// Locked will be true if the wallet is currently locked.
func (eth *baseWallet) Locked() bool {
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
func (w *ETHWallet) canSend(value uint64, isPreEstimate bool) (uint64, *big.Int, error) {
	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	maxFee := defaultSendGasLimit * dexeth.WeiToGwei(maxFeeRate)

	if !isPreEstimate {
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
func (w *TokenWallet) canSend(value uint64, isPreEstimate bool) (uint64, *big.Int, error) {
	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	g := w.gases(contractVersionNewest)
	if g == nil {
		return 0, nil, fmt.Errorf("gas table not found")
	}

	maxFee := dexeth.WeiToGwei(maxFeeRate) * g.Transfer

	if !isPreEstimate {
		bal, err := w.Balance()
		if err != nil {
			return 0, nil, err
		}
		avail := bal.Available
		if avail < value {
			return 0, nil, fmt.Errorf("not enough tokens: have %[1]d %[3]s need %[2]d %[3]s", avail, value, w.atomicUnit)
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
	maxFee, _, err := w.canSend(value, addr == "")
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
	maxFee, _, err := w.canSend(value, addr == "")
	if err != nil {
		return 0, false, err
	}
	return maxFee, w.ValidateAddress(addr), nil

}

// RestorationInfo returns information about how to restore the wallet in
// various external wallets.
func (w *assetWallet) RestorationInfo(seed []byte) ([]*asset.WalletRestoration, error) {
	walletSeed, err := genWalletSeed(seed)
	if err != nil {
		return nil, err
	}
	defer encode.ClearBytes(walletSeed)

	extKey, err := keygen.GenDeepChild(walletSeed, seedDerivationPath)
	if err != nil {
		return nil, err
	}
	defer extKey.Zero()
	privateKey, err := extKey.SerializedPrivKey()
	defer encode.ClearBytes(privateKey)
	if err != nil {
		return nil, err
	}

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
	confs = uint32(hdr.Number.Uint64() - swapData.BlockHeight + 1)

	return
}

// Send sends the exact value to the specified address. The provided fee rate is
// ignored since all sends will use an internally derived fee rate.
func (w *ETHWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	_, maxFeeRate, err := w.canSend(value, false)
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
	return &coin{id: txHash, value: value}, nil
}

// Send sends the exact value to the specified address. Fees are taken from the
// parent wallet. The provided fee rate is ignored since all sends will use an
// internally derived fee rate.
func (w *TokenWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if err := isValidSend(addr, value, false); err != nil {
		return nil, err
	}

	_, maxFeeRate, err := w.canSend(value, false)
	if err != nil {
		return nil, err
	}

	tx, err := w.sendToAddr(common.HexToAddress(addr), value, maxFeeRate)
	if err != nil {
		return nil, err
	}
	txHash := tx.Hash()
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
	prog, err := eth.node.syncProgress(eth.ctx)
	if err != nil {
		return false, 0, err
	}
	checkHeaderTime := func() (bool, error) {
		bh, err := eth.node.bestHeader(eth.ctx)
		if err != nil {
			return false, err
		}
		// Time in the header is in seconds.
		timeDiff := time.Now().Unix() - int64(bh.Time)
		if timeDiff > dexeth.MaxBlockInterval && eth.net != dex.Simnet {
			eth.log.Infof("Time since last eth block (%d sec) exceeds %d sec."+
				"Assuming not in sync. Ensure your computer's system clock "+
				"is correct.", timeDiff, dexeth.MaxBlockInterval)
			return false, nil
		}
		return true, nil
	}
	if prog.HighestBlock != 0 {
		// HighestBlock was set. This means syncing started and is
		// finished if CurrentBlock is higher. CurrentBlock will
		// continue to go up even if we are not in a syncing state.
		// HighestBlock will not.
		if prog.CurrentBlock >= prog.HighestBlock {
			fresh, err := checkHeaderTime()
			if err != nil {
				return false, 0, err
			}
			if !fresh {
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
	fresh, err := checkHeaderTime()
	if err != nil {
		return false, 0, err
	}
	if !fresh {
		return false, 0, nil
	}
	return eth.node.peerCount() > 0, 1.0, nil
}

// DynamicSwapFeesPaid returns fees for initiation transactions. Part of the
// asset.DynamicSwapOrRedemptionFeeChecker interface.
func (eth *assetWallet) DynamicSwapFeesPaid(ctx context.Context, coinID, contractData dex.Bytes) (fee uint64, secretHashes [][]byte, err error) {
	return eth.swapOrRedemptionFeesPaid(ctx, coinID, contractData, true)
}

// DynamicRedemptionFeesPaid returns fees for redemption transactions. Part of
// the asset.DynamicSwapOrRedemptionFeeChecker interface.
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

	for _, w := range eth.walletList() {
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
func (eth *ETHWallet) monitorBlocks(ctx context.Context, reportErr func(error)) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			eth.checkForNewBlocks(reportErr)
		case <-ctx.Done():
			return
		}
	}
}

// checkForNewBlocks checks for new blocks. When a tip change is detected, the
// tipChange callback function is invoked and a goroutine is started to check
// if any contracts in the findRedemptionQueue are redeemed in the new blocks.
func (eth *ETHWallet) checkForNewBlocks(reportErr func(error)) {
	ctx, cancel := context.WithTimeout(eth.ctx, 2*time.Second)
	defer cancel()
	bestHdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		go reportErr(fmt.Errorf("failed to get best hash: %w", err))
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

	walletList := eth.walletList()

	go func() {
		for _, w := range walletList {
			w.tipChange(nil)
		}
	}()
	go func() {
		for _, w := range walletList {
			w.checkFindRedemptions()
		}
	}()
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
	if err := w.monitoredTxDB.Store(originalHash[:], originalTx); err != nil {
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
	if err := w.monitoredTxDB.Store(replacementHash[:], replacementTx); err != nil {
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
	for _, hash := range txsToDelete {
		if err := w.monitoredTxDB.Delete(hash[:]); err != nil {
			w.log.Errorf("failed to delete monitored tx: %v", err)
		}
	}

	// Delete from the database immediately, but keep in the memory map a bit
	// longer to allow time for other matches that used the same transaction
	// to complete. If they are cleared too early there will just be an error
	// message stating that the monitored tx is missing, but no other issue.
	go func() {
		timer := time.NewTimer(3 * time.Minute)
		<-timer.C
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
	if err := w.monitoredTxDB.Store(h[:], monitoredTx); err != nil {
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
// to call the function.
func (w *ETHWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption) (*asset.ConfirmRedemptionStatus, error) {
	return w.confirmRedemption(coinID, redemption, nil)
}

// ConfirmRedemption checks the status of a redemption. If it is determined
// that a transaction will not be mined, this function will submit a new
// transaction to replace the old one. The caller is notified of this by having
// a different coinID in the returned asset.ConfirmRedemptionStatus as was used
// to call the function.
func (w *TokenWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption) (*asset.ConfirmRedemptionStatus, error) {
	return w.confirmRedemption(coinID, redemption, w.parent)
}

const (
	txConfsNeededToConfirm               = 10
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
func (w *assetWallet) checkUnconfirmedRedemption(secretHash common.Hash, contractVer uint32, txHash common.Hash, tx *types.Transaction, currentTip uint64, feeWallet *assetWallet, monitoredTx *monitoredTx) (*asset.ConfirmRedemptionStatus, error) {
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
	confirmedNonce, err := w.node.getConfirmedNonce(w.ctx, int64(currentTip))
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
// when geth can also not find the transaction, instead of resubmitting an
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
		w.log.Errorf("ConfirmRedemption: geth could not find tx: %s", txHash)
		swapIsRedeemed, err := w.swapIsRedeemed(secretHash, contractVer)
		if err != nil {
			return nil, err
		}
		if swapIsRedeemed {
			return confStatus(txConfsNeededToConfirm, txHash), nil
		}

		// If geth cannot find the transaction, and it also wasn't among the
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

	return w.checkUnconfirmedRedemption(secretHash, contractVer, txHash, tx, currentTip, feeWallet, nil)
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
			w.log.Errorf("ConfirmRedemption: geth could not find tx: %s", txHash)
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
		replacementTxHash, err := w.resubmitRedemption(tx, contractVer, nil, feeWallet, monitoredTx)
		if err != nil {
			return nil, err
		}
		return confStatus(0, *replacementTxHash), nil
	}
	if confirmations > 0 {
		return confStatus(confirmations, txHash), nil
	}

	// If the transaction is unconfirmed, check to see if it should be
	// resubmitted once every blocksToWaitBeforeCheckingIfReplaced blocks.
	if blocksSinceSubmission%blocksToWaitBeforeCheckingIfReplaced != 0 || blocksSinceSubmission == 0 {
		return confStatus(0, txHash), nil
	}

	return w.checkUnconfirmedRedemption(secretHash, contractVer, txHash, tx, currentTip, feeWallet, monitoredTx)
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

func (w *assetWallet) balanceWithTxPool() (*Balance, error) {
	isETH := w.assetID == BipID
	var confirmed *big.Int
	var err error
	if isETH {
		confirmed, err = w.node.addressBalance(w.ctx, w.addr)
	} else {
		confirmed, err = w.tokenBalance()
	}
	if err != nil {
		return nil, fmt.Errorf("balance error: %v", err)
	}

	pendingTxs, err := w.node.pendingTransactions()
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

		if isETH {
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
		} else if isETH {
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
	if assetID == BipID {
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
	token, found := dexeth.Tokens[w.assetID]
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
		c, err := constructor(w.net, w.assetID, w.addr, w.node.contractBackend())
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
func (w *assetWallet) redeem(ctx context.Context, assetID uint32, redemptions []*asset.Redemption, maxFeeRate, gasLimit uint64, contractVer uint32, nonceOverride *uint64) (tx *types.Transaction, err error) {
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
func (w *assetWallet) refund(secretHash [32]byte, maxFeeRate uint64, contractVer uint32) (tx *types.Transaction, err error) {
	gas := w.gases(contractVer)
	if gas == nil {
		return nil, fmt.Errorf("no gas table for asset %d, version %d", w.assetID, contractVer)
	}
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(w.ctx, 0, gas.Refund, dexeth.GweiToWei(maxFeeRate), nil)
	if err != nil {
		return nil, err
	}

	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.refund(txOpts, secretHash)
		if err != nil {
			c.voidUnusedNonce()
			return err
		}
		return nil
	})
}

// isRedeemable checks if the swap identified by secretHash is redeemable using secret.
func (w *assetWallet) isRedeemable(secretHash [32]byte, secret [32]byte, contractVer uint32) (redeemable bool, err error) {
	return redeemable, w.withContractor(contractVer, func(c contractor) error {
		redeemable, err = c.isRedeemable(secretHash, secret)
		return err
	})
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

// getGasEstimate is used to get a gas table for an asset's contract(s). The
// provided gases, g, should be generous estimates of what the gas might be.
// Errors are thrown if the provided estimates are too small by more than a
// factor of 2. The account should already have a trading balance of at least
// maxSwaps gwei (token or eth), and sufficient eth balance to cover the
// requisite tx fees.
func GetGasEstimates(ctx context.Context, cl ethFetcher, c contractor, maxSwaps int, g *dexeth.Gases,
	toAddress common.Address, waitForMined func(), waitForReceipt func(ethFetcher, *types.Transaction) (*types.Receipt, error)) error {
	tokenContractor, isToken := c.(tokenContractor)

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

	defer func() {
		if len(stats.swaps) == 0 {
			return
		}
		firstSwap := stats.swaps[0]
		fmt.Printf("  First swap used %d gas \n", firstSwap)
		if len(stats.swaps) > 1 {
			fmt.Printf("    %d additional swaps averaged %d gas each\n", len(stats.swaps)-1, avgDiff(stats.swaps))
			fmt.Printf("    %+v \n", stats.swaps)
		}
		if len(stats.redeems) == 0 {
			return
		}
		firstRedeem := stats.redeems[0]
		fmt.Printf("  First redeem used %d gas \n", firstRedeem)
		if len(stats.redeems) > 1 {
			fmt.Printf("    %d additional redeems averaged %d gas each\n", len(stats.redeems)-1, avgDiff(stats.redeems))
			fmt.Printf("    %+v \n", stats.redeems)
		}
		fmt.Printf("  Average of %d refunds: %d \n", len(stats.refunds), avg(stats.refunds))
		fmt.Printf("    %+v \n", stats.refunds)
		if !isToken {
			return
		}
		fmt.Printf("  Average of %d approvals: %d \n", len(stats.approves), avg(stats.approves))
		fmt.Printf("    %+v \n", stats.approves)
		fmt.Printf("  Average of %d transfers: %d \n", len(stats.transfers), avg(stats.transfers))
		fmt.Printf("    %+v \n", stats.transfers)
	}()

	for n := 1; n <= maxSwaps; n++ {
		// Estimate approve.
		var approveTx, transferTx *types.Transaction
		if isToken {
			txOpts, err := cl.txOpts(ctx, 0, g.Approve*2, nil, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for approve: %v", err)
			}
			approveTx, err = tokenContractor.approve(txOpts, new(big.Int).SetBytes(encode.RandomBytes(8)))
			if err != nil {
				return fmt.Errorf("error estimating approve gas: %v", err)
			}
			txOpts, err = cl.txOpts(ctx, 0, g.Transfer*2, nil, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for transfer: %v", err)
			}
			transferTx, err = tokenContractor.transfer(txOpts, toAddress, big.NewInt(1))
			if err != nil {
				return fmt.Errorf("error estimating transfer gas: %v", err)
			}
		}

		contracts := make([]*asset.Contract, 0, n)
		secrets := make([][32]byte, 0, n)
		for i := 0; i < n; i++ {
			secretB := encode.RandomBytes(32)
			var secret [32]byte
			copy(secret[:], secretB)
			secretHash := sha256.Sum256(secretB)
			contracts = append(contracts, &asset.Contract{
				Address:    cl.address().String(),
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
		tx, err := c.initiate(txOpts, contracts)
		if err != nil {
			return fmt.Errorf("initiate error for %d swaps: %v", n, err)
		}
		waitForMined()
		receipt, err := waitForReceipt(cl, tx)
		if err != nil {
			return err
		}
		if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return err
		}
		stats.swaps = append(stats.swaps, receipt.GasUsed)

		if isToken {
			receipt, err = waitForReceipt(cl, approveTx)
			if err != nil {
				return err
			}
			if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
				return err
			}
			stats.approves = append(stats.approves, receipt.GasUsed)
			receipt, err = waitForReceipt(cl, transferTx)
			if err != nil {
				return err
			}
			if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
				return err
			}
			stats.transfers = append(stats.transfers, receipt.GasUsed)
		}

		// Estimate a refund
		var firstSecretHash [32]byte
		copy(firstSecretHash[:], contracts[0].SecretHash)
		refundGas, err := c.estimateRefundGas(ctx, firstSecretHash)
		if err != nil {
			return fmt.Errorf("error estimate refund gas: %w", err)
		}
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
			return err
		}
		tx, err = c.redeem(txOpts, redemptions)
		if err != nil {
			return fmt.Errorf("redeem error for %d swaps: %v", n, err)
		}
		waitForMined()
		receipt, err = waitForReceipt(cl, tx)
		if err != nil {
			return err
		}
		if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return err
		}
		stats.redeems = append(stats.redeems, receipt.GasUsed)
	}
	return nil
}
