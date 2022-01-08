// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/keygen"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func registerToken(tokenID uint32, desc string) {
	token := dexeth.Tokens[tokenID]
	asset.RegisterToken(tokenID, token, &asset.WalletDefinition{
		Type:        "token",
		Description: desc,
	})
}

func init() {
	asset.Register(BipID, &Driver{})

	// Test token
	registerToken(testTokenID, "A token wallet for the DEX test token. Used for testing DEX software.")
}

const (
	// BipID is the BIP-0044 asset ID.
	BipID               = 60
	defaultGasFee       = 82  // gwei
	defaultGasFeeLimit  = 200 // gwei
	defaultSendGasLimit = 21_000

	walletTypeGeth = "geth"
)

var (
	defaultAppDir = dcrutil.AppDataDir("dexethclient", false)
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker = time.Second
	configOpts  = []*asset.ConfigOption{
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
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = &asset.WalletInfo{
		Name:     "Ethereum",
		UnitInfo: dexeth.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:              walletTypeGeth,
				Tab:               "Internal",
				Description:       "Use the built-in DEX wallet with snap sync",
				ConfigOpts:        configOpts,
				Seeded:            true,
				DefaultConfigPath: defaultAppDir, // Incorrect if changed by user?
			},
		},
	}

	chainIDs = map[dex.Network]int64{
		dex.Mainnet: 1,
		dex.Testnet: 5,  // Görli
		dex.Simnet:  42, // see dex/testing/eth/harness.sh
	}

	minGasTipCap = dexeth.GweiToWei(2)

	unlimitedAllowance                   = ethmath.MaxBig256
	unlimitedAllowanceReplenishThreshold = new(big.Int).Div(unlimitedAllowance, big.NewInt(2))

	findRedemptionCoinID = []byte("FindRedemption Coin")
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
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	id, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "", nil
	}
	return id.String(), nil
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// Exists checks the existence of the wallet.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeGeth {
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
// satisfied by rpcclient. For testing, it can be satisfied by a stub.
type ethFetcher interface {
	address() common.Address
	bestBlockHash(ctx context.Context) (common.Hash, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	block(ctx context.Context, hash common.Hash) (*types.Block, error)
	connect(ctx context.Context) error
	shutdown()
	syncProgress() ethereum.SyncProgress
	lock() error
	locked() bool
	unlock(pw string) error
	signData(addr common.Address, data []byte) ([]byte, error)
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	sendSignedTransaction(ctx context.Context, tx *types.Transaction) error
	// Contract- and asset-dependent methods.
	sendToAddr(ctx context.Context, assetID uint32, addr common.Address, val uint64) (*types.Transaction, error)
	initiate(ctx context.Context, assetID uint32, contracts []*asset.Contract, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	isRedeemable(assetID uint32, secretHash, secret [32]byte, contractVer uint32) (bool, error)
	redeem(ctx context.Context, assetID uint32, redemptions []*asset.Redemption, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	isRefundable(assetID uint32, secretHash [32]byte, contractVer uint32) (bool, error)
	refund(ctx context.Context, assetID uint32, secretHash [32]byte, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	swap(ctx context.Context, assetID uint32, secretHash [32]byte, contractVer uint32) (*dexeth.SwapState, error)
	balance(ctx context.Context, assetID uint32) (*Balance, error)
	// Token-specific methods.
	loadToken(ctx context.Context, tokenID uint32) (*dex.Gases, error)
	tokenAllowance(ctx context.Context, assetID uint32, contractVer uint32) (*big.Int, error)
	approveToken(ctx context.Context, assetID uint32, amount *big.Int, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
}

// Check that ExchangeWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*AssetWallet)(nil)
var _ asset.TokenMaster = (*AssetWallet)(nil)

type baseWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64
	ctx          context.Context // the asset subsystem starts with Connect(ctx)
	net          dex.Network
	node         ethFetcher
	addr         common.Address
	log          dex.Logger
	gasFeeLimit  uint64

	tipMtx     sync.RWMutex
	currentTip *types.Block

	walletsMtx sync.RWMutex
	wallets    map[uint32]*AssetWallet
}

type tokenConfig struct {
	*tokenWalletConfig
	gas    *dex.Gases
	parent *AssetWallet
}

// AssetWallet is a wallet backend for Ethereum. The backend is how the DEX
// client app communicates with the Ethereum blockchain and wallet. AssetWallet
// satisfies the dex.Wallet interface.
type AssetWallet struct {
	*baseWallet
	assetID   uint32
	tipChange func(error)

	locked    uint64
	lockedMtx sync.RWMutex

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[[32]byte]*findRedemptionRequest

	// token is only used by token wallets.
	token *tokenConfig
}

// Info returns basic information about the wallet and asset.
func (*baseWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// CreateWallet creates a new internal ETH wallet and stores the private key
// derived from the wallet seed.
func CreateWallet(createWalletParams *asset.CreateWalletParams) error {
	if createWalletParams.Type != walletTypeGeth {
		return fmt.Errorf("wallet type %q unrecognized", createWalletParams.Type)
	}
	walletDir := getWalletDir(createWalletParams.DataDir, createWalletParams.Net)
	node, err := prepareNode(&nodeConfig{
		net:    createWalletParams.Net,
		appDir: walletDir,
	})
	if err != nil {
		return err
	}

	// m/44'/60'/0'/0/0
	path := []uint32{hdkeychain.HardenedKeyStart + 44,
		hdkeychain.HardenedKeyStart + 60,
		hdkeychain.HardenedKeyStart,
		0,
		0}
	extKey, err := keygen.GenDeepChild(createWalletParams.Seed, path)
	defer extKey.Zero()
	if err != nil {
		return err
	}

	privateKey, err := extKey.SerializedPrivKey()
	defer encode.ClearBytes(privateKey)
	if err != nil {
		return err
	}

	err = importKeyToNode(node, privateKey, createWalletParams.Pass)
	if err != nil {
		return err
	}

	return node.Close()
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. It starts an internal light node.
func NewWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (*AssetWallet, error) {
	cl, err := newNodeClient(getWalletDir(assetCFG.DataDir, net), net, logger.SubLogger("NODE"))
	if err != nil {
		return nil, err
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
		log:         logger,
		net:         net,
		node:        cl,
		addr:        cl.address(),
		gasFeeLimit: gasFeeLimit,
		wallets:     make(map[uint32]*AssetWallet),
	}

	w := &AssetWallet{
		baseWallet:         eth,
		assetID:            BipID,
		tipChange:          assetCFG.TipChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
	}

	w.wallets = map[uint32]*AssetWallet{
		BipID: w,
	}

	return w, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

func (eth *baseWallet) shutdown() {
	eth.node.shutdown()
}

// Connect connects to the node RPC server. A dex.Connector.
func (w *AssetWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	w.ctx = ctx

	// If this is a token wallet, just wait for the provided context or the
	// parent context to be canceled.
	if w.token != nil {
		if w.token.parent.ctx == nil || w.token.parent.ctx.Err() != nil {
			return nil, fmt.Errorf("parent not connected")
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
			case <-w.baseWallet.ctx.Done():
			}
		}()
		return &wg, nil
	}

	err := w.node.connect(ctx)
	if err != nil {
		return nil, err
	}

	// Initialize the best block.
	bestHash, err := w.node.bestBlockHash(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block hash from geth: %w", err)
	}
	block, err := w.node.block(ctx, bestHash)
	if err != nil {
		return nil, fmt.Errorf("error getting best block from geth: %w", err)
	}
	w.tipMtx.Lock()
	w.currentTip = block
	w.tipMtx.Unlock()
	height := w.currentTip.NumberU64()
	// NOTE: We should be using the tipAtConnect to set Progress in SyncStatus.
	atomic.StoreInt64(&w.baseWallet.tipAtConnect, int64(height))
	w.log.Infof("Connected to geth, at height %d", height)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorBlocks(ctx, w.tipChange)
		w.shutdown()
	}()
	return &wg, nil
}

// tokenWalletConfig is the configuration options for token wallets.
type tokenWalletConfig struct {
	// LimitAllowance disabled for now.
	// See https://github.com/decred/dcrdex/pull/1394#discussion_r780479402.
	// LimitAllowance bool `ini:"limitAllowance"`
}

// parseTokenWalletConfig parses the settings map into a *WalletConfig.
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
func (eth *baseWallet) CreateTokenWallet(tokenID uint32, _ map[string]string) error {
	// Just check that the token exists for now.
	if dexeth.Tokens[tokenID] == nil {
		return fmt.Errorf("token not found for asset ID %d", tokenID)
	}
	return nil
}

// OpenTokenWallet creates a new AssetWallet for a token.
func (w *AssetWallet) OpenTokenWallet(tokenID uint32, settings map[string]string, tipChange func(error)) (asset.Wallet, error) {
	if w.assetID != BipID {
		return nil, fmt.Errorf("token wallets create other token wallets")
	}
	cfg, err := parseTokenWalletConfig(settings)
	if err != nil {
		return nil, err
	}
	gases, err := w.node.loadToken(w.ctx, tokenID)
	if err != nil {
		return nil, err
	}
	tokenWallet := &AssetWallet{
		baseWallet: w.baseWallet,
		assetID:    tokenID,
		token: &tokenConfig{
			tokenWalletConfig: cfg,
			parent:            w,
			gas:               gases,
		},
		tipChange:          tipChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
	}

	w.baseWallet.walletsMtx.Lock()
	w.baseWallet.wallets[tokenID] = tokenWallet
	w.baseWallet.walletsMtx.Unlock()

	return tokenWallet, nil
}

// OwnsAddress indicates if an address belongs to the wallet. The address need
// not be a EIP55-compliant formatted address. It may or may not have a 0x
// prefix, and case is not important.
//
// In Ethereum, an address is an account.
func (eth *baseWallet) OwnsAddress(address string) (bool, error) {
	if !common.IsHexAddress(address) {
		return false, errors.New("invalid address")
	}
	addr := common.HexToAddress(address)
	return addr == eth.addr, nil
}

// Balance returns the total available funds in the account. The eth node
// returns balances in wei. Those are flored and stored as gwei, or 1e9 wei.
func (w *AssetWallet) Balance() (*asset.Balance, error) {
	w.lockedMtx.Lock()
	defer w.lockedMtx.Unlock()

	return w.balance()
}

// balance returns the total available funds in the account.
// This function expects eth.lockedMtx to be held.
func (w *AssetWallet) balance() (*asset.Balance, error) {
	bal, err := w.node.balance(w.ctx, w.assetID)
	if err != nil {
		return nil, err
	}

	locked := w.locked + dexeth.WeiToGwei(bal.PendingOut)
	return &asset.Balance{
		Available: dexeth.WeiToGwei(bal.Current) - locked,
		Locked:    locked,
		Immature:  dexeth.WeiToGwei(bal.PendingIn),
	}, nil
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
func (w *AssetWallet) MaxOrder(lotSize uint64, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	balance, err := w.Balance()
	if err != nil {
		return nil, err
	}
	if balance.Available == 0 {
		return &asset.SwapEstimate{}, nil
	}
	gas := gases(w.assetID, nfo.Version)
	if gas == nil {
		return nil, fmt.Errorf("no gas table for %d", w.assetID)
	}
	maxTxFee := gas.Swap * nfo.MaxFeeRate
	var lots uint64
	if w.assetID == BipID {
		lots = balance.Available / (lotSize + maxTxFee)
	} else {
		lots = balance.Available / lotSize
	}

	if lots < 1 {
		return &asset.SwapEstimate{}, nil
	}
	return w.estimateSwap(lots, lotSize, feeSuggestion, nfo)
}

// PreSwap gets order estimates based on the available funds and the wallet
// configuration.
func (w *AssetWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	maxEst, err := w.MaxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est, err := w.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, req.AssetConfig)
	if err != nil {
		return nil, err
	}

	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (w *AssetWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	if lots == 0 {
		return &asset.SwapEstimate{}, nil
	}
	value := lots * lotSize
	var allowanceGas, oneGas uint64
	if w.token != nil {
		oneGas = w.token.gas.Swap
		// Need to check if we need to add gas for changing the token contract
		// approval.
		currentAllowance, err := w.node.tokenAllowance(w.ctx, w.assetID, nfo.Version)
		if err != nil {
			return nil, err
		}
		// No reason to do anything if the allowance is > the unlimited
		// allowance approval threshold.
		if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) < 0 {
			allowanceGas = w.token.gas.Approve
		}
	} else {
		oneGas = dexeth.InitGas(1, nfo.Version)
	}

	maxFees := (oneGas*lots + allowanceGas) * nfo.MaxFeeRate
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: (oneGas*lots + allowanceGas) * feeSuggestion,
		RealisticBestCase:  (dexeth.InitGas(int(lots), nfo.Version) + allowanceGas) * feeSuggestion,
	}, nil
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (w *AssetWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	if w.token == nil {
		return &asset.PreRedeem{
			Estimate: &asset.RedeemEstimate{
				RealisticBestCase:  dexeth.RedeemGas(int(req.Lots), 0) * req.FeeSuggestion,
				RealisticWorstCase: dexeth.RedeemGas(1, 0) * req.FeeSuggestion * req.Lots,
			},
		}, nil
	}

	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  w.token.gas.RedeemN(int(req.Lots)) * req.FeeSuggestion,
			RealisticWorstCase: w.token.gas.Redeem * req.FeeSuggestion * req.Lots,
		},
	}, nil
}

// coin implements the asset.Coin interface for ETH
type coin struct {
	id dex.Bytes
	// the value can be determined from the coin id, but for some
	// coin ids a lookup would be required from the blockchain to
	// determine its value, so this field is used as a cache.
	value uint64
}

type tokenCoin struct {
	coin
	fees uint64
}

// ID is the ETH coins ID. For functions related to funding an order,
// the ID must contain an encoded fundingCoinID, but when returned from
// Swap, it will contain the transaction hash used to initiate the swap.
func (c *coin) ID() dex.Bytes {
	return c.id
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

// decodeFundingCoinID decodes a coin id into a coin object. This function ensures
// that the id contains an encoded fundingCoinID whose address is the same as
// the one managed by this wallet.
func (eth *baseWallet) decodeFundingCoinID(id []byte) (*coin, error) {
	fundingCoinID, err := decodeFundingCoinID(id)
	if err != nil {
		return nil, err
	}

	if fundingCoinID.Address != eth.addr {
		return nil, fmt.Errorf("coin address %x != wallet address %x",
			fundingCoinID.Address, eth.addr)
	}

	return &coin{
		id:    fundingCoinID.Encode(),
		value: fundingCoinID.Amount,
	}, nil
}

func (eth *baseWallet) createFundingCoin(amount uint64) *coin {
	id := createFundingCoinID(eth.addr, amount)
	return &coin{
		id:    id.Encode(),
		value: amount,
	}
}

func (eth *baseWallet) createTokenFundingCoin(amount, fees uint64) *tokenCoin {
	id := createTokenFundingCoinID(eth.addr, amount, fees)
	return &tokenCoin{
		coin: coin{
			id:    id.Encode(),
			value: amount,
		},
		fees: fees,
	}
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// In UTXO based coins, the returned []dex.Bytes contains the redeem scripts for the
// selected coins, but since there are no redeem scripts in Ethereum, nil is returned.
// Equal number of coins and redeem scripts must be returned.
func (w *AssetWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	cfg := ord.DEXConfig

	if w.gasFeeLimit < cfg.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			cfg.Symbol,
			cfg.MaxFeeRate,
			w.gasFeeLimit)
	}
	maxSwapFees := cfg.MaxFeeRate * cfg.SwapSize * ord.MaxSwapCount
	refundFees := dexeth.RefundGas(0) * w.gasFeeLimit
	ethNeeded := maxSwapFees + refundFees
	ethWallet := w
	var success bool
	var coin asset.Coin
	if w.assetID == BipID {
		ethNeeded += ord.Value
		coin = w.createFundingCoin(ethNeeded)
	} else {
		// Check if we need to up the allowance.
		ethWallet = w.token.parent
		currentAllowance, err := w.node.tokenAllowance(w.ctx, w.assetID, cfg.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("error retrieving current allowance: %w", err)
		}
		if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) < 0 {
			ethBal, err := ethWallet.balance()
			if err != nil {
				return nil, nil, fmt.Errorf("error getting eth balance: %w", err)
			}
			if (w.token.gas.Approve*cfg.MaxFeeRate)+ethNeeded > ethBal.Available {
				return nil, nil, fmt.Errorf("parent balance %d doesn't cover setAllowance (%d) and fees (%d)",
					ethBal.Available, w.token.gas.Approve*cfg.MaxFeeRate, ethNeeded)
			}
			if _, err := w.node.approveToken(w.ctx, w.assetID, unlimitedAllowance, cfg.MaxFeeRate, cfg.Version); err != nil {
				return nil, nil, fmt.Errorf("token contract approval error: %w", err)
			}
		}
		if err := w.lockFunds(ord.Value); err != nil {
			return nil, nil, fmt.Errorf("error locking token funds: %v", err)
		}
		defer func() {
			if success {
				return
			}
			if err := w.unlockFunds(ord.Value); err != nil {
				w.log.Errorf("error unlocking funds afeter failed FundOrder: %v", err)
			}
		}()

		coin = w.createTokenFundingCoin(ord.Value, ethNeeded)
	}

	err := ethWallet.lockFunds(ethNeeded)
	if err != nil {
		return nil, nil, err
	}

	success = true
	return asset.Coins{coin}, []dex.Bytes{nil}, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (w *AssetWallet) ReturnCoins(coins asset.Coins) error {
	var amt uint64
	for i := range coins {
		coin, err := w.decodeFundingCoinID(coins[i].ID())
		if err != nil {
			return fmt.Errorf("ReturnCoins: unable to decode funding coin id: %w", err)
		}
		amt += coin.Value()
	}
	return w.unlockFunds(amt)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (w *AssetWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	var totalValue uint64
	for _, id := range ids {
		coin, err := w.decodeFundingCoinID(id)
		if err != nil {
			return nil, fmt.Errorf("FundingCoins: unable to decode funding coin id: %w", err)
		}
		coins = append(coins, coin)
		totalValue += coin.Value()
	}

	err := w.lockFunds(totalValue)
	if err != nil {
		return nil, err
	}

	return coins, nil
}

// lockFunds locks funds held by the wallet.
func (w *AssetWallet) lockFunds(amt uint64) error {
	w.lockedMtx.Lock()
	defer w.lockedMtx.Unlock()

	balance, err := w.balance()
	if err != nil {
		return err
	}

	if balance.Available < amt {
		return fmt.Errorf("attempting to lock more than is currently available. %d > %d",
			amt, balance.Available)
	}

	w.locked += amt
	return nil
}

// unlockFunds unlocks funds held by the wallet.
func (w *AssetWallet) unlockFunds(amt uint64) error {
	w.lockedMtx.Lock()
	defer w.lockedMtx.Unlock()

	if w.locked < amt {
		return fmt.Errorf("attempting to unlock more than is currently locked. %d > %d",
			amt, w.locked)
	}

	w.locked -= amt
	return nil
}

// swapReceipt implements the asset.Receipt interface for ETH.
type swapReceipt struct {
	txHash     common.Hash
	secretHash [dexeth.SecretHashSize]byte
	// expiration and value can be determined with a blockchain
	// lookup, but we cache these values to avoid this.
	expiration time.Time
	value      uint64
	ver        uint32
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
		id:    r.txHash[:], // server's idea of ETH coin ID encoding
	}
}

// Contract returns the swap's identifying data, which the concatenation of the
// contract version and the secret hash.
func (r *swapReceipt) Contract() dex.Bytes {
	return dexeth.EncodeContractData(r.ver, r.secretHash)
}

// String returns a string representation of the swapReceipt.
func (r *swapReceipt) String() string {
	return fmt.Sprintf("{ tx hash: %x, secret hash: %x }", r.txHash, r.secretHash)
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
func (w *AssetWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	fail := func(s string, a ...interface{}) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, fmt.Errorf(s, a...)
	}

	receipts := make([]asset.Receipt, 0, len(swaps.Contracts))

	// For now, reject swaps where # inputs > 1. Core doesn't support grouping
	// from multiple orders for now.
	if len(swaps.Inputs) != 1 {
		return fail("expected 1 input coin. got %d", len(swaps.Inputs))
	}

	isToken := w.token != nil
	var reservedVal, reservedParent uint64
	if isToken {
		c, err := decodeTokenFundingCoinID(swaps.Inputs[0].ID())
		if err != nil {
			return fail("error decoding coin ID: %v", err)
		}
		reservedVal, reservedParent = c.TokenValue, c.Fees
	} else {
		c, err := decodeFundingCoinID(swaps.Inputs[0].ID())
		if err != nil {
			return fail("error decoding coin ID: %v", err)
		}
		reservedVal = c.Amount
	}

	var swapVal uint64
	for _, contract := range swaps.Contracts {
		swapVal += contract.Value
	}

	fees := dexeth.InitGas(1, swaps.AssetVersion) * uint64(len(swaps.Contracts)) * swaps.FeeRate

	if isToken {
		if swapVal > reservedVal {
			return fail("unfunded token swap: %d < %d", reservedVal, swapVal)
		}
		if fees > reservedParent {
			return fail("unfunded token swap fees: %d < %d", reservedParent, fees)
		}
	} else {
		if swapVal+fees > reservedVal {
			return fail("unfunded swap: %d < %d", reservedVal, swapVal+fees)
		}
	}

	tx, err := w.node.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, swaps.AssetVersion)
	if err != nil {
		return fail("Swap: initiate error: %w", err)
	}

	txHash := tx.Hash()
	for _, swap := range swaps.Contracts {
		var secretHash [dexeth.SecretHashSize]byte
		copy(secretHash[:], swap.SecretHash)
		receipts = append(receipts,
			&swapReceipt{
				expiration: encode.UnixTimeMilli(int64(swap.LockTime)),
				value:      swap.Value,
				txHash:     txHash,
				secretHash: secretHash,
				ver:        swaps.AssetVersion,
			})
	}

	var change asset.Coin
	if swaps.LockChange {
		if isToken {
			// For tokens, we'll unlock the swap val and the fees separately for
			// the child and parent asset respectively.
			if err := w.unlockFunds(swapVal); err != nil {
				w.log.Errorf("error unlocking ")
			}
			if err := w.token.parent.unlockFunds(fees); err != nil {
				w.log.Errorf("error unlocking parent asset funds: %v", err)
			}
			change = w.createTokenFundingCoin(reservedVal-swapVal, reservedParent-fees)
		} else {
			if err := w.unlockFunds(swapVal + fees); err != nil {
				w.log.Errorf("error unlocking ")
			}
			change = w.createFundingCoin(reservedVal - swapVal - fees)
		}
	} else {
		if err := w.unlockFunds(reservedVal); err != nil {
			w.log.Errorf("error unlocking parent asset funds: %v", err)
		}
		if isToken {
			if err := w.token.parent.unlockFunds(reservedParent); err != nil {
				w.log.Errorf("error unlocking parent asset funds: %v", err)
			}
		}
	}

	return receipts, change, fees, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. All redemptions must be for the same contract version because the
// current API requires a single transaction reported (asset.Coin output), but
// conceptually a batch of redeems could be processed for any number of
// different contract addresses with multiple transactions.
func (w *AssetWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	fail := func(err error) ([]dex.Bytes, asset.Coin, uint64, error) {
		return nil, nil, 0, err
	}

	if len(form.Redemptions) == 0 {
		return fail(errors.New("Redeem: must be called with at least 1 redemption"))
	}

	var contractVersion uint32 // require a consistent version since this is a single transaction
	inputs := make([]dex.Bytes, 0, len(form.Redemptions))
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
			contractVersion = ver
		} else if contractVersion != ver {
			return fail(fmt.Errorf("Redeem: inconsistent contract versions in RedeemForm.Redemptions: "+
				"%d != %d", contractVersion, ver))
		}

		// Use the contract's free public view function to validate the secret
		// against the secret hash, and ensure the swap is otherwise redeemable
		// before broadcasting our secrets, which is especially important if we
		// are maker (the swap initiator).
		var secret [32]byte
		copy(secret[:], redemption.Secret)
		redeemable, err := w.node.isRedeemable(w.assetID, secretHash, secret, ver)
		if err != nil {
			return fail(fmt.Errorf("Redeem: failed to check if swap is redeemable: %w", err))
		}
		if !redeemable {
			return fail(fmt.Errorf("Redeem: secretHash %x not redeemable with secret %x",
				secretHash, secret))
		}

		swapData, err := w.node.swap(w.ctx, w.assetID, secretHash, ver)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Redeem: error finding swap state: %w", err)
		}
		redeemedValue += swapData.Value
		inputs = append(inputs, redemption.Spends.Coin.ID())
	}

	outputCoin := w.createFundingCoin(redeemedValue)
	fundsRequired := dexeth.RedeemGas(len(form.Redemptions), contractVersion) * form.FeeSuggestion

	// TODO: make sure the amount we locked for redemption is enough to cover the gas
	// fees. Also unlock coins.
	_, err := w.node.redeem(w.ctx, w.assetID, form.Redemptions, form.FeeSuggestion, contractVersion)
	if err != nil {
		return fail(fmt.Errorf("Redeem: redeem error: %w", err))
	}

	return inputs, outputCoin, fundsRequired, nil
}

// recoverPubkey recovers the uncompressed public key from the signature and the
// message hash that wsa signed. The signature should be a compact signature
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

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (eth *baseWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	_, err = eth.decodeFundingCoinID(coin.ID())
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error decoding coin: %w", err)
	}

	sig, err := eth.node.signData(eth.addr, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error signing data: %w", err)
	}

	pubKey, err := recoverPubkey(crypto.Keccak256(msg), sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %w", err)
	}

	return []dex.Bytes{pubKey}, []dex.Bytes{sig}, nil
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap. coinID is expected to be the transaction id, and must
// be the same as the hash of txData. contract is expected to be
// (contractVersion|secretHash) where the secretHash uniquely keys the swap.
func (eth *baseWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	tx := new(types.Transaction)
	err := tx.UnmarshalBinary(txData)
	if err != nil {
		return nil, fmt.Errorf("AuditContract: failed to unmarshal transaction: %w", err)
	}

	txHash := tx.Hash()
	if !bytes.Equal(coinID, txHash[:]) {
		return nil, fmt.Errorf("AuditContract: coin id != txHash - coin id: %x, txHash: %x", coinID, tx.Hash())
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
		id:    coinID,
		value: initiation.Value,
	}

	// The counter-party should have broadcasted the contract tx but rebroadcast
	// just in case to ensure that the tx is sent to the network. Do not block
	// because this is not required and does not affect the audit result.
	if rebroadcast {
		go func() {
			if err := eth.node.sendSignedTransaction(eth.ctx, tx); err != nil {
				eth.log.Debugf("Rebroadcasting counterparty contract %v (THIS MAY BE NORMAL): %v", txHash, err)
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

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (w *AssetWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return false, time.Time{}, err
	}

	header, err := w.node.bestHeader(w.ctx)
	if err != nil {
		return false, time.Time{}, err
	}
	blockTime := time.Unix(int64(header.Time), 0)

	swap, err := w.node.swap(w.ctx, w.assetID, secretHash, contractVer)
	if err != nil {
		return false, time.Time{}, err
	}
	return swap.LockTime.Before(blockTime), swap.LockTime, nil
}

// findRedemptionResult is used internally for queued findRedemptionRequests.
type findRedemptionResult struct {
	err    error
	secret []byte
}

// findRedemptionRequest is a request that is waiting on a redemption result.
type findRedemptionRequest struct {
	contractVer uint32
	res         chan *findRedemptionResult
}

// sendFindRedemptionResult sends the result or logs a message if it cannot be
// sent.
func (eth *baseWallet) sendFindRedemptionResult(req *findRedemptionRequest, secretHash [32]byte, secret []byte, err error) {
	select {
	case req.res <- &findRedemptionResult{secret: secret, err: err}:
	default:
		eth.log.Info("findRedemptionResult channel blocking for request %s", secretHash)
	}
}

// findRedemptionRequests creates a copy of the findRedemptionReqs map.
func (w *AssetWallet) findRedemptionRequests() map[[32]byte]*findRedemptionRequest {
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
func (w *AssetWallet) FindRedemption(ctx context.Context, _, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, nil, err
	}

	// See if it's ready right away.
	secret, err = w.findSecret(ctx, secretHash, contractVer)
	if err != nil {
		return nil, nil, err
	}

	if len(secret) > 0 {
		return findRedemptionCoinID, secret, nil
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

	return findRedemptionCoinID, res.secret[:], nil
}

func (w *AssetWallet) findSecret(ctx context.Context, secretHash [32]byte, contractVer uint32) ([]byte, error) {
	// Add a reasonable timeout here.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	swap, err := w.node.swap(ctx, w.assetID, secretHash, contractVer)
	if err != nil {
		return nil, err
	}

	switch swap.State {
	case dexeth.SSInitiated:
		return nil, nil // no redeem yet, but keep checking
	case dexeth.SSRedeemed:
		return swap.Secret[:], nil
	case dexeth.SSNone:
		return nil, fmt.Errorf("swap %x does not exist", secretHash)
	case dexeth.SSRefunded:
		return nil, fmt.Errorf("swap %x is already refunded", secretHash)
	}
	return nil, fmt.Errorf("unrecognized swap state %v", swap.State)
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
func (w *AssetWallet) Refund(_, contract dex.Bytes, feeSuggestion uint64) (dex.Bytes, error) {
	version, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to decode contract: %w", err)
	}

	refundable, err := w.node.isRefundable(w.assetID, secretHash, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to check isRefundable: %w", err)
	}
	if !refundable {
		return nil, fmt.Errorf("Refund: swap with secret hash %x is not refundable", secretHash)
	}

	tx, err := w.node.refund(w.ctx, w.assetID, secretHash, feeSuggestion, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to call refund: %w", err)
	}

	w.unlockFunds(w.gasFeeLimit * dexeth.RefundGas(version))

	txHash := tx.Hash()
	return txHash[:], nil
}

// Address returns an address for the exchange wallet.
func (eth *baseWallet) Address() (string, error) {
	return eth.addr.String(), nil
}

// Unlock unlocks the exchange wallet.
func (eth *baseWallet) Unlock(pw []byte) error {
	return eth.node.unlock(string(pw))
}

// Lock locks the exchange wallet.
func (eth *baseWallet) Lock() error {
	return eth.node.lock()
}

// Locked will be true if the wallet is currently locked.
func (eth *baseWallet) Locked() bool {
	return eth.node.locked()
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (w *AssetWallet) PayFee(address string, regFee, _ uint64) (asset.Coin, error) {
	w.lockedMtx.Lock()
	defer w.lockedMtx.Unlock()

	bal, err := w.balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	maxFee := defaultSendGasLimit * w.gasFeeLimit
	need := regFee + maxFee
	if avail < need {
		return nil, fmt.Errorf("not enough funds to pay fee: have %d gwei need %d gwei", avail, need)
	}
	tx, err := w.node.sendToAddr(w.ctx, w.assetID, common.HexToAddress(address), regFee)
	if err != nil {
		return nil, err
	}
	txHash := tx.Hash()
	return &coin{id: txHash[:], value: dexeth.WeiToGwei(tx.Value())}, nil
}

// SwapConfirmations gets the number of confirmations and the spend status
// for the specified swap.
func (w *AssetWallet) SwapConfirmations(ctx context.Context, _ dex.Bytes, contract dex.Bytes, _ time.Time) (confs uint32, spent bool, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return 0, false, err
	}

	hdr, err := w.node.bestHeader(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("error fetching best header: %w", err)
	}

	swapData, err := w.node.swap(ctx, w.assetID, secretHash, contractVer)
	if err != nil {
		return 0, false, fmt.Errorf("error finding swap state: %w", err)
	}

	if swapData.State == dexeth.SSNone {
		return 0, false, asset.CoinNotFoundError
	}

	spent = swapData.State >= dexeth.SSRedeemed
	confs = uint32(hdr.Number.Uint64() - swapData.BlockHeight + 1)

	return
}

// Withdraw withdraws funds to the specified address. Value is gwei. The fee is
// subtracted from the total balance if it cannot be sent otherwise.
func (w *AssetWallet) Withdraw(addr string, value, _ uint64) (asset.Coin, error) {
	w.lockedMtx.Lock()
	defer w.lockedMtx.Unlock()

	bal, err := w.balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	if avail < value {
		return nil, fmt.Errorf("not enough funds to withdraw: have %d gwei need %d gwei", avail, value)
	}
	maxFee := defaultSendGasLimit * w.gasFeeLimit
	if avail < maxFee {
		return nil, fmt.Errorf("not enough funds to withdraw: cannot cover max fee of %d gwei", maxFee)
	}
	if avail < value+maxFee {
		value -= maxFee
	}
	tx, err := w.node.sendToAddr(w.ctx, w.assetID, common.HexToAddress(addr), value)
	if err != nil {
		return nil, err
	}
	txHash := tx.Hash()
	return &coin{id: txHash[:], value: dexeth.WeiToGwei(tx.Value())}, nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (*baseWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// SyncStatus is information about the blockchain sync status.
func (eth *baseWallet) SyncStatus() (bool, float32, error) {
	// node.SyncProgress will return nil both before syncing has begun and
	// after it has finished. In order to discern when syncing has begun,
	// check that the best header came in under dexeth.MaxBlockInterval.
	prog := eth.node.syncProgress()
	syncing := prog.CurrentBlock >= prog.HighestBlock
	if !syncing {
		var ratio float32
		if prog.HighestBlock != 0 {
			ratio = float32(prog.CurrentBlock) / float32(prog.HighestBlock)
		}
		return false, ratio, nil
	}
	bh, err := eth.node.bestHeader(eth.ctx)
	if err != nil {
		return false, 0, err
	}
	// Time in the header is in seconds.
	nowInSecs := time.Now().Unix() / 1000
	timeDiff := nowInSecs - int64(bh.Time)
	var progress float32
	if timeDiff < dexeth.MaxBlockInterval {
		progress = 1
	}
	return progress == 1, progress, nil
}

// RegFeeConfirmations gets the number of confirmations for the specified
// transaction.
func (eth *baseWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	var txHash common.Hash
	copy(txHash[:], coinID)
	return eth.node.transactionConfirmations(ctx, txHash)
}

// monitorBlocks pings for new blocks and runs the tipChange callback function
// when the block changes. New blocks are also scanned for potential contract
// redeems.
func (eth *baseWallet) monitorBlocks(ctx context.Context, reportErr func(error)) {
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
func (eth *baseWallet) checkForNewBlocks(reportErr func(error)) {
	ctx, cancel := context.WithTimeout(eth.ctx, 2*time.Second)
	defer cancel()
	bestHash, err := eth.node.bestBlockHash(ctx)
	if err != nil {
		go reportErr(fmt.Errorf("failed to get best hash: %w", err))
		return
	}
	// This method is called frequently. Don't hold write lock
	// unless tip has changed.
	eth.tipMtx.RLock()
	currentTipHash := eth.currentTip.Hash()
	eth.tipMtx.RUnlock()
	if currentTipHash == bestHash {
		return
	}

	newTip, err := eth.node.block(ctx, bestHash)
	if err != nil {
		go reportErr(fmt.Errorf("failed to get best block: %w", err))
		return
	}

	eth.tipMtx.Lock()
	defer eth.tipMtx.Unlock()

	prevTip := eth.currentTip
	eth.currentTip = newTip
	eth.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.NumberU64(),
		prevTip.Hash(), newTip.NumberU64(), newTip.Hash())

	go func() {
		for _, w := range eth.wallets {
			w.tipChange(nil)
		}
	}()
	go func() {
		for _, w := range eth.wallets {
			w.checkFindRedemptions()
		}
	}()
}

// checkFindRedemptions checks queued findRedemptionRequests.
func (w *AssetWallet) checkFindRedemptions() {
	for secretHash, req := range w.findRedemptionRequests() {
		secret, err := w.findSecret(w.ctx, secretHash, req.contractVer)
		if err != nil {
			w.sendFindRedemptionResult(req, secretHash, nil, err)
		} else if len(secret) > 0 {
			w.sendFindRedemptionResult(req, secretHash, secret, nil)
		}
	}
}
