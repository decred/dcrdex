// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path/filepath"
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
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func registerToken(tokenID uint32, desc string) {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		panic("token " + strconv.Itoa(int(tokenID)) + " not known")
	}
	asset.RegisterToken(tokenID, token.Token, &asset.WalletDefinition{
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

	// confCheckTimeout is the amount of time allowed to check for
	// confirmations. Testing on testnet has shown spikes up to 2.5
	// seconds. This value may need to be adjusted in the future.
	confCheckTimeout = 4 * time.Second
)

var (
	testTokenID, _ = dex.BipSymbolID("dextt.eth")
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker     = time.Second
	peerCountTicker = 5 * time.Second
	configOpts      = []*asset.ConfigOption{
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
				Type:        walletTypeGeth,
				Tab:         "Internal",
				Description: "Use the built-in DEX wallet (geth light node)",
				ConfigOpts:  configOpts,
				Seeded:      true,
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

	findRedemptionCoinID = []byte("FindRedemption Coin")

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
// In ETH there are 3 possible coin IDs:
//  1. A transaction hash. 32 bytes
//  2. An encoded ETH funding coin id which includes the account address and
//     amount. 20 + 8 = 28 bytes
//  3. An encoded token funding coin id which includes the account address, an
//     a token value, and fees. 20 + 8 + 8 = 36 bytes
//  4. A byte encoded string of the account address. 40 or 42 (with 0x) bytes
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
	}
	return "", fmt.Errorf("unknown coin ID format: %x", coinID)
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
// satisfied by *nodeClient. For testing, it can be satisfied by a stub.
type ethFetcher interface {
	address() common.Address
	addressBalance(ctx context.Context, addr common.Address) (*big.Int, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	chainConfig() *params.ChainConfig
	connect(ctx context.Context) error
	peerCount() uint32
	contractBackend() bind.ContractBackend
	lock() error
	locked() bool
	pendingTransactions() ([]*types.Transaction, error)
	shutdown()
	sendSignedTransaction(ctx context.Context, tx *types.Transaction) error
	sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error)
	signData(data []byte) (sig, pubKey []byte, err error)
	syncProgress() ethereum.SyncProgress
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate *big.Int) (*bind.TransactOpts, error)
	currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error)
	unlock(pw string) error
}

// Check that assetWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*ETHWallet)(nil)
var _ asset.Wallet = (*TokenWallet)(nil)
var _ asset.AccountLocker = (*ETHWallet)(nil)
var _ asset.AccountLocker = (*TokenWallet)(nil)
var _ asset.TokenMaster = (*ETHWallet)(nil)
var _ asset.WalletRestorer = (*assetWallet)(nil)

type baseWallet struct {
	ctx         context.Context // the asset subsystem starts with Connect(ctx)
	net         dex.Network
	node        ethFetcher
	addr        common.Address
	log         dex.Logger
	gasFeeLimit uint64

	walletsMtx sync.RWMutex
	wallets    map[uint32]*assetWallet

	// nonceSendMtx should be locked for the node.txOpts -> tx send sequence
	// for all txs, to ensure nonce correctness.
	nonceSendMtx sync.Mutex
}

// assetWallet is a wallet backend for Ethereum and Eth tokens. The backend is
// how the DEX client app communicates with the Ethereum blockchain and wallet.
// assetWallet satisfies the dex.Wallet interface.
type assetWallet struct {
	*baseWallet
	assetID   uint32
	tipChange func(error)
	log       dex.Logger

	lockedFunds struct {
		mtx                sync.RWMutex
		initiateReserves   uint64
		redemptionReserves uint64
		refundReserves     uint64
	}

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[[32]byte]*findRedemptionRequest

	contractors map[uint32]contractor // version -> contractor

	evmify  func(uint64) *big.Int
	atomize func(*big.Int) uint64
}

// ETHWallet implements some Ethereum-specific methods.
type ETHWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	*assetWallet

	lastPeerCount uint32
	peersChange   func(uint32, error)

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
}

// Info returns basic information about the wallet and asset.
func (*ETHWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// Info returns basic information about the wallet and asset.
func (w *TokenWallet) Info() *asset.WalletInfo {
	return &asset.WalletInfo{
		Name:     w.token.Name,
		UnitInfo: w.token.UnitInfo,
	}
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

	extKey, err := keygen.GenDeepChild(createWalletParams.Seed, seedDerivationPath)
	if err != nil {
		return err
	}
	defer extKey.Zero()
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
func NewWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (*ETHWallet, error) {
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
		wallets:     make(map[uint32]*assetWallet),
	}

	w := &assetWallet{
		baseWallet:         eth,
		log:                logger.SubLogger("ETH"),
		assetID:            BipID,
		tipChange:          assetCFG.TipChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		contractors:        make(map[uint32]contractor),
		evmify:             dexeth.GweiToWei,
		atomize:            dexeth.WeiToGwei,
	}

	w.wallets = map[uint32]*assetWallet{
		BipID: w,
	}

	return &ETHWallet{
		assetWallet: w,
		peersChange: assetCFG.PeersChange,
	}, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

func (eth *ETHWallet) shutdown() {
	eth.node.shutdown()
}

// Connect connects to the node RPC server. Satisfies dex.Connector.
func (w *ETHWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	w.ctx = ctx
	err := w.node.connect(ctx)
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
	w.ctx = ctx

	if w.parent.ctx == nil || w.parent.ctx.Err() != nil {
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
func (w *ETHWallet) OpenTokenWallet(tokenID uint32, settings map[string]string, tipChange func(error)) (asset.Wallet, error) {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		return nil, fmt.Errorf("token %d not found", tokenID)
	}

	cfg, err := parseTokenWalletConfig(settings)
	if err != nil {
		return nil, err
	}

	aw := &assetWallet{
		baseWallet:         w.baseWallet,
		log:                w.baseWallet.log.SubLogger(strings.ToUpper(dex.BipIDSymbol(tokenID))),
		assetID:            tokenID,
		tipChange:          tipChange,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
		contractors:        make(map[uint32]contractor),
		evmify:             token.AtomicToEVM,
		atomize:            token.EVMToAtomic,
	}
	err = aw.loadContractors(w.ctx, tokenID)
	if err != nil {
		return nil, err
	}

	w.baseWallet.walletsMtx.Lock()
	w.baseWallet.wallets[tokenID] = aw
	w.baseWallet.walletsMtx.Unlock()

	return &TokenWallet{
		assetWallet: aw,
		cfg:         cfg,
		parent:      w.assetWallet,
		token:       token,
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
		return fmt.Errorf("attempting to lock more for %s than is currently available. %d > %d",
			t, amt, balance.Available)
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
	est, err := w.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.AssetConfig, ord.RedeemConfig, nil)
	return est, err
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration.
func (w *TokenWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	est, err := w.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.AssetConfig, ord.RedeemConfig, w.parent)
	return est, err
}

func (w *assetWallet) maxOrder(lotSize uint64, feeSuggestion uint64, dexSwapCfg, dexRedeemCfg *dex.Asset, feeWallet *assetWallet) (*asset.SwapEstimate, error) {
	balance, err := w.Balance()
	if err != nil {
		return nil, err
	}
	if balance.Available == 0 {
		return &asset.SwapEstimate{}, nil
	}

	g, err := w.initGasEstimate(1, dexSwapCfg, dexRedeemCfg)
	if err != nil {
		return nil, fmt.Errorf("gasEstimate error: %w", err)
	}

	oneFee := g.oneGas * dexSwapCfg.MaxFeeRate
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
	return w.estimateSwap(lots, lotSize, feeSuggestion, dexSwapCfg, dexRedeemCfg)
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
	maxEst, err := w.maxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig, req.RedeemConfig, feeWallet)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est, err := w.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, req.AssetConfig, req.RedeemConfig)
	if err != nil {
		return nil, err
	}

	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// estimateSwap prepares an *asset.SwapEstimate. The estimate does not include
// funds that might be locked for refunds.
func (w *assetWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, dexSwapCfg, dexRedeemCfg *dex.Asset) (*asset.SwapEstimate, error) {
	if lots == 0 {
		return &asset.SwapEstimate{}, nil
	}

	oneSwap, nSwap, err := w.swapGas(int(lots), dexSwapCfg)
	if err != nil {
		return nil, fmt.Errorf("error getting swap gas estimate: %w", err)
	}

	value := lots * lotSize
	allowanceGas, err := w.allowanceGasRequired(dexRedeemCfg)
	if err != nil {
		return nil, err
	}

	oneGasMax := oneSwap*lots + allowanceGas
	maxFees := oneGasMax * dexSwapCfg.MaxFeeRate

	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: oneGasMax * feeSuggestion,
		RealisticBestCase:  (nSwap + allowanceGas) * feeSuggestion,
	}, nil
}

// allowanceGasRequired estimates the gas that is required to issue an approval
// for a token. If the dexRedeemCfg is nil or is not for a fee-family erc20
// asset, no error is returned and the return value will be zero.
func (w *assetWallet) allowanceGasRequired(dexRedeemCfg *dex.Asset) (uint64, error) {
	if dexRedeemCfg == nil || dexRedeemCfg.ID == BipID {
		return 0, nil
	}
	redeemWallet := w.wallet(dexRedeemCfg.ID)
	if redeemWallet == nil {
		return 0, nil
	}
	currentAllowance, err := redeemWallet.tokenAllowance()
	if err != nil {
		return 0, err
	}
	// No reason to do anything if the allowance is > the unlimited
	// allowance approval threshold.
	if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) < 0 {
		return redeemWallet.approvalGas(unlimitedAllowance, dexRedeemCfg.Version)
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
	oneRedeem, nRedeem, err := w.redeemGas(int(req.Lots), req.AssetConfig)
	if err != nil {
		return nil, err
	}

	// Note: Any gas required for erc20 contract approval will also have been
	// summed in the PreSwap estimate if the swap asset is a fee-family asset.
	allowanceGas, err := w.allowanceGasRequired(req.AssetConfig)
	if err != nil {
		return nil, err
	}

	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  (nRedeem + allowanceGas) * req.FeeSuggestion,
			RealisticWorstCase: (oneRedeem*req.Lots + allowanceGas) * req.FeeSuggestion,
		},
	}, nil
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
	cfg := ord.DEXConfig

	if w.gasFeeLimit < cfg.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			w.gasFeeLimit)
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), cfg, ord.RedeemConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error estimating swap gas: %v", err)
	}

	ethToLock := cfg.MaxFeeRate*g.Swap*ord.MaxSwapCount + ord.Value
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
	cfg := ord.DEXConfig

	if w.gasFeeLimit < cfg.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			w.gasFeeLimit)
	}

	g, err := w.initGasEstimate(int(ord.MaxSwapCount), cfg, ord.RedeemConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error estimating swap gas: %v", err)
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

	ethToLock := cfg.MaxFeeRate * g.Swap * ord.MaxSwapCount
	coin := w.createTokenFundingCoin(ord.Value, ethToLock)

	if err := w.parent.lockFunds(ethToLock, initiationReserve); err != nil {
		return nil, nil, err
	}

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
	oneGas, nGas, nSwap, nRedeem, nRefund uint64 // nRefund unused?
}

// initGasEstimate gets the best available gas estimate for n initiations. A
// live estimate is checked against the server's configured values and our own
// known values and errors or logs generated in certain cases.
func (w *assetWallet) initGasEstimate(n int, dexSwapCfg, dexRedeemCfg *dex.Asset) (est *gasEstimate, err error) {
	est = new(gasEstimate)

	g := w.gases(dexSwapCfg.Version)
	if g == nil {
		return nil, fmt.Errorf("no gas table")
	}

	est.Refund = g.Refund
	// est.nRefund = n * g.Refund // ?

	est.Swap, est.nSwap, err = w.swapGas(n, dexSwapCfg)
	if err != nil {
		return nil, fmt.Errorf("error calculating swap gas: %w", err)
	}

	est.oneGas = est.Swap + est.Redeem
	est.nGas = est.nSwap + est.nRedeem

	if dexRedeemCfg == nil {
		return // redeeming asset is not ETH or an ETH token
	}

	if redeemW := w.wallet(dexRedeemCfg.ID); redeemW != nil {
		est.Redeem, est.nRedeem, err = redeemW.redeemGas(n, dexRedeemCfg)
		if err != nil {
			return nil, fmt.Errorf("error calculating fee-family redeem gas: %w", err)
		}
	}

	return
}

// swapGas estimates gas for a number of initiations. swapGas will error if we
// cannot get a live estimate from the contractor, which will happen if the
// wallet has no balance.
func (w *assetWallet) swapGas(n int, dexCfg *dex.Asset) (oneSwap, nSwap uint64, err error) {
	oneSwap = dexCfg.SwapSize

	g := w.gases(dexCfg.Version)
	if g == nil {
		return 0, 0, fmt.Errorf("no gases known for %d version %d", w.assetID, dexCfg.Version)
	}

	// Warn of mismatch in gas estimates.
	if g.Swap > oneSwap {
		w.log.Warnf("Server has a lower configured swap gas value, which is odd")
		oneSwap = g.Swap
	}

	// We have no way of updating the value of SwapAdd without a version change,
	// but we're not gonna worry about accuracy for nSwap, since it's only used
	// for estimates and never for dex-validated values like order funding.
	nSwap = oneSwap + uint64(n-1)*g.SwapAdd

	// If a live estimate is greater than our estimate from configured values,
	// use the live estimate with a warning.
	if gasEst, err := w.estimateInitGas(w.ctx, n, dexCfg.Version); err != nil {
		w.log.Errorf("(%d) error estimating swap gas: %v", w.assetID, err)
		return 0, 0, err
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
func (w *assetWallet) redeemGas(n int, nfo *dex.Asset) (oneGas, nGas uint64, err error) {
	g := w.gases(nfo.Version)
	if g == nil {
		return 0, 0, fmt.Errorf("no gas table for redemption asset %d", nfo.ID)
	}
	redeemGas := nfo.RedeemSize
	if g.Redeem > redeemGas {
		// This probably won't happen.
		w.log.Debugf("Server's reported redeem gas estimate %d is less than ours %d. Using our estimate.", g.Redeem, redeemGas)
		redeemGas = g.Redeem
	} else if nfo.RedeemSize > 2*g.Redeem {
		return 0, 0, fmt.Errorf("server's reported redemption costs are more than twice the expected value. %d > 2 * %d",
			nfo.RedeemSize, g.Redeem)
	}
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
		w.log.Warnf("Approve gas estimate %d is greater than the expected value %d. Using live estimate + 10%.")
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
	return fmt.Sprintf("{ tx hash: %x, contract address: %s, secret hash: %x }",
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

	cfg := swaps.AssetConfig

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

	oneSwap, _, err := w.swapGas(1, cfg)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}

	gasLimit := oneSwap * uint64(len(swaps.Contracts))
	fees := gasLimit * swaps.FeeRate

	if swapVal+fees > reservedVal {
		return fail("unfunded swap: %d < %d", reservedVal, swapVal+fees)
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, gasLimit, cfg.Version)
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
			ver:          cfg.Version,
			contractAddr: dexeth.ContractAddresses[cfg.Version][w.net].String(),
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

	cfg := swaps.AssetConfig

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

	oneSwap, _, err := w.swapGas(1, cfg)
	if err != nil {
		return fail("error getting gas fees: %v", err)
	}

	gasLimit := oneSwap * uint64(len(swaps.Contracts))
	fees := gasLimit * swaps.FeeRate

	if swapVal > reservedVal {
		return fail("unfunded token swap: %d < %d", reservedVal, swapVal)
	}

	if fees > reservedParent {
		return fail("unfunded token swap fees: %d < %d", reservedParent, fees)
	}

	tx, err := w.initiate(w.ctx, w.assetID, swaps.Contracts, swaps.FeeRate, gasLimit, cfg.Version)
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
			ver:          cfg.Version,
			contractAddr: erc20.ContractAddresses[cfg.Version][w.net].String(),
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
	return w.assetWallet.Redeem(form, nil)
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (w *TokenWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return w.assetWallet.Redeem(form, w.parent)
}

func (w *assetWallet) Redeem(form *asset.RedeemForm, feeWallet *assetWallet) ([]dex.Bytes, asset.Coin, uint64, error) {
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
		redeemedValue += swapData.Value
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

	tx, err := w.redeem(w.ctx, w.assetID, form.Redemptions, gasFeeCap, gasLimit, contractVer)
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

// maybeApproveTokenSwapContract checks whether a token's swap contract needs
// to be approved for the wallet address on the erc20 contract.
func (w *TokenWallet) maybeApproveTokenSwapContract(dexRedeemCfg *dex.Asset, redemptionReserves uint64) error {
	if txHashI := w.approval.Load(); txHashI != nil {
		return fmt.Errorf("an approval is already pending (%s). wait until it is mined "+
			"before ordering again", txHashI.(common.Hash))
	}
	// Check if we need to up the allowance.
	currentAllowance, err := w.tokenAllowance()
	if err != nil {
		return fmt.Errorf("error retrieving current allowance: %w", err)
	}
	if currentAllowance.Cmp(unlimitedAllowanceReplenishThreshold) >= 0 {
		return nil
	}
	ethBal, err := w.parent.balance()
	if err != nil {
		return fmt.Errorf("error getting eth balance: %w", err)
	}
	approveGas, err := w.approvalGas(unlimitedAllowance, dexRedeemCfg.Version)
	if err != nil {
		return fmt.Errorf("error estimating allowance gas: %w", err)
	}
	if (approveGas*dexRedeemCfg.MaxFeeRate)+redemptionReserves > ethBal.Available {
		return fmt.Errorf("parent balance %d doesn't cover contract approval (%d) and tx fees (%d)",
			ethBal.Available, approveGas*dexRedeemCfg.MaxFeeRate, redemptionReserves)
	}
	tx, err := w.approveToken(unlimitedAllowance, dexRedeemCfg.MaxFeeRate, dexRedeemCfg.Version)
	if err != nil {
		return fmt.Errorf("token contract approval error: %w", err)
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
	txOpts, err := w.node.txOpts(w.ctx, 0, approveGas, dexeth.GweiToWei(maxFeeRate))
	if err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return tx, w.withTokenContractor(w.assetID, contractVer, func(c tokenContractor) error {
		tx, err = c.approve(txOpts, amount)
		return err
	})
}

// ReserveNRedemptions locks funds for redemption. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *ETHWallet) ReserveNRedemptions(n uint64, dexRedeemCfg *dex.Asset) (uint64, error) { // TODO: dexSwapCfg -> maxFeeRate, version
	g := w.gases(dexRedeemCfg.Version)
	if g == nil {
		return 0, fmt.Errorf("no gas table")
	}
	redeemCost := g.Redeem * dexRedeemCfg.MaxFeeRate
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
func (w *TokenWallet) ReserveNRedemptions(n uint64, dexRedeemCfg *dex.Asset) (uint64, error) {
	g := w.gases(dexRedeemCfg.Version)
	if g == nil {
		return 0, fmt.Errorf("no gas table")
	}
	redeemCost := g.Redeem * dexRedeemCfg.MaxFeeRate
	reserve := redeemCost * n
	if err := w.maybeApproveTokenSwapContract(dexRedeemCfg, reserve); err != nil {
		return 0, err
	}

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
func (w *ETHWallet) ReserveNRefunds(n uint64, dexSwapCfg *dex.Asset) (uint64, error) { // TODO: dexSwapCfg -> maxFeeRate, version
	g := w.gases(dexSwapCfg.Version)
	if g == nil {
		return 0, errors.New("no gas table")
	}
	return reserveNRefunds(w.assetWallet, n, dexSwapCfg, g)
}

// ReserveNRefunds locks funds for doing refunds. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (w *TokenWallet) ReserveNRefunds(n uint64, dexSwapCfg *dex.Asset) (uint64, error) {
	g := w.gases(dexSwapCfg.Version)
	if g == nil {
		return 0, errors.New("no gas table")
	}
	return reserveNRefunds(w.parent, n, dexSwapCfg, g)
}

func reserveNRefunds(w *assetWallet, n uint64, dexSwapCfg *dex.Asset, g *dexeth.Gases) (uint64, error) {
	refundCost := g.Refund * dexSwapCfg.MaxFeeRate
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
func (eth *baseWallet) AuditContract(coinID, contract, serializedTx dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	tx := new(types.Transaction)
	err := tx.UnmarshalBinary(serializedTx)
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
		id:    txHash,
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
func (w *assetWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return false, time.Time{}, err
	}

	swap, err := w.swap(w.ctx, secretHash, contractVer)
	if err != nil {
		return false, time.Time{}, err
	}

	// Time is not yet set for uninitiated swaps.
	if swap.State == dexeth.SSNone {
		return false, time.Time{}, asset.ErrSwapNotInitiated
	}

	header, err := w.node.bestHeader(w.ctx)
	if err != nil {
		return false, time.Time{}, err
	}
	blockTime := time.Unix(int64(header.Time), 0)
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
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, nil, err
	}

	// See if it's ready right away.
	secret, err = w.findSecret(secretHash, contractVer)
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

func (w *assetWallet) findSecret(secretHash [32]byte, contractVer uint32) ([]byte, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	swap, err := w.swap(ctx, secretHash, contractVer)
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
func (w *assetWallet) Refund(_, contract dex.Bytes, feeSuggestion uint64) (dex.Bytes, error) {
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

	tx, err := w.refund(secretHash, feeSuggestion, version)
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

// RestorationInfo returns information about how to restore the wallet in
// various external wallets.
func (w *assetWallet) RestorationInfo(seed []byte) ([]*asset.WalletRestoration, error) {
	extKey, err := keygen.GenDeepChild(seed, seedDerivationPath)
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
func (w *assetWallet) SwapConfirmations(ctx context.Context, _ dex.Bytes, contract dex.Bytes, _ time.Time) (confs uint32, spent bool, err error) {
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

// Send sends the exact value to the specified address.
func (w *ETHWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if !common.IsHexAddress(addr) {
		return nil, fmt.Errorf("invalid hex address %q", addr)
	}

	bal, err := w.Balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	if avail < value {
		return nil, fmt.Errorf("not enough funds to send: have %d gwei need %d gwei", avail, value)
	}
	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	maxFee := defaultSendGasLimit * dexeth.WeiToGwei(maxFeeRate)
	if avail < value+maxFee {
		return nil, fmt.Errorf("not enough funds to send: cannot cover value %d, max fee of %d gwei", value, maxFee)
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

// Send sends the exact value to the specified address. The fees are taken
// from the parent wallet.
func (w *TokenWallet) Send(addr string, value, _ uint64) (asset.Coin, error) {
	if !common.IsHexAddress(addr) {
		return nil, fmt.Errorf("invalid hex address %q", addr)
	}

	bal, err := w.Balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	if avail < value {
		return nil, fmt.Errorf("not enough tokens: have %d gwei need %d gwei", avail, value)
	}

	ethBal, err := w.parent.Balance()
	if err != nil {
		return nil, fmt.Errorf("error getting base chain balance: %w", err)
	}

	g := w.gases(contractVersionNewest)
	if g == nil {
		return nil, fmt.Errorf("gas table not found")
	}

	maxFeeRate, err := w.recommendedMaxFeeRate(w.ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting max fee rate: %w", err)
	}

	maxFee := dexeth.WeiToGwei(maxFeeRate) * g.Transfer
	if ethBal.Available < maxFee {
		return nil, fmt.Errorf("insufficient balance to cover token transfer fees. %d < %d",
			ethBal.Available, maxFee)
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
func (eth *baseWallet) SyncStatus() (bool, float32, error) {
	prog := eth.node.syncProgress()
	if prog.HighestBlock != 0 {
		// HighestBlock was set. This means syncing started and is
		// finished if CurrentBlock is higher. CurrentBlock will
		// continue to go up even if we are not in a syncing state.
		// HighestBlock will not.
		if prog.CurrentBlock >= prog.HighestBlock {
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
	bh, err := eth.node.bestHeader(eth.ctx)
	if err != nil {
		return false, 0, err
	}
	// Time in the header is in seconds.
	timeDiff := time.Now().Unix() - int64(bh.Time)
	if timeDiff > dexeth.MaxBlockInterval && eth.net != dex.Simnet {
		eth.log.Debugf("Time since last block (%d sec) exceeds %d sec."+
			"Assuming not in sync.", timeDiff, dexeth.MaxBlockInterval)
		return false, 0, nil
	}
	return eth.node.peerCount() > 0, 1.0, nil
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
	base, tip, err := eth.node.currentFees(eth.ctx)
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
	prevPeer := atomic.SwapUint32(&eth.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		eth.peersChange(numPeers, nil)
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

	go func() {
		for _, w := range eth.walletList() {
			w.tipChange(nil)
		}
	}()
	go func() {
		for _, w := range eth.walletList() {
			w.checkFindRedemptions()
		}
	}()
}

// checkFindRedemptions checks queued findRedemptionRequests.
func (w *assetWallet) checkFindRedemptions() {
	for secretHash, req := range w.findRedemptionRequests() {
		secret, err := w.findSecret(secretHash, req.contractVer)
		if err != nil {
			w.sendFindRedemptionResult(req, secretHash, nil, err)
		} else if len(secret) > 0 {
			w.sendFindRedemptionResult(req, secretHash, secret, nil)
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
	txOpts, err := w.node.txOpts(w.ctx, amt, defaultSendGasLimit, maxFeeRate)
	if err != nil {
		return nil, err
	}
	return w.node.sendTransaction(w.ctx, txOpts, addr, nil)
}

// sendToAddr sends funds to the address.
func (w *TokenWallet) sendToAddr(addr common.Address, amt uint64, maxFeeRate *big.Int) (tx *types.Transaction, err error) {
	w.baseWallet.nonceSendMtx.Lock()
	defer w.baseWallet.nonceSendMtx.Unlock()
	g := w.gases(contractVersionNewest)
	if g == nil {
		return nil, fmt.Errorf("no gas table")
	}
	return tx, w.withTokenContractor(w.assetID, contractVersionNewest, func(c tokenContractor) error {
		txOpts, err := w.node.txOpts(w.ctx, 0, g.Transfer, nil)
		if err != nil {
			return err
		}
		tx, err = c.transfer(txOpts, addr, w.evmify(amt))
		return err
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
	txOpts, _ := w.node.txOpts(ctx, val, gasLimit, dexeth.GweiToWei(maxFeeRate))
	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.initiate(txOpts, contracts)
		return err
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
func (w *assetWallet) loadContractors(ctx context.Context, tokenID uint32) error {
	token, found := dexeth.Tokens[tokenID]
	if !found {
		return fmt.Errorf("token %d not found", tokenID)
	}
	netToken, found := token.NetTokens[w.net]
	if !found {
		return fmt.Errorf("token %d not found", tokenID)
	}

	for ver := range netToken.SwapContracts {
		constructor, found := tokenContractorConstructors[ver]
		if !found {
			w.log.Errorf("contractor constructor not found for token %s, version %d", token.Name, ver)
			continue
		}
		c, err := constructor(w.net, tokenID, w.addr, w.node.contractBackend())
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
func (w *assetWallet) redeem(ctx context.Context, assetID uint32, redemptions []*asset.Redemption, maxFeeRate, gasLimit uint64, contractVer uint32) (tx *types.Transaction, err error) {
	w.nonceSendMtx.Lock()
	defer w.nonceSendMtx.Unlock()
	txOpts, err := w.node.txOpts(ctx, 0, gasLimit, dexeth.GweiToWei(maxFeeRate))
	if err != nil {
		return nil, err
	}

	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.redeem(txOpts, redemptions)
		return err
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
	txOpts, err := w.node.txOpts(w.ctx, 0, gas.Refund, dexeth.GweiToWei(maxFeeRate))
	if err != nil {
		return nil, fmt.Errorf("addSignerToOpts error: %w", err)
	}

	return tx, w.withContractor(contractVer, func(c contractor) error {
		tx, err = c.refund(txOpts, secretHash)
		return err
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
func GetGasEstimates(ctx context.Context, cl *nodeClient, c contractor, maxSwaps int, g *dexeth.Gases, waitForMined func()) error {
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
			txOpts, err := cl.txOpts(ctx, 0, g.Approve*2, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for approve: %v", err)
			}
			approveTx, err = tokenContractor.approve(txOpts, new(big.Int).SetBytes(encode.RandomBytes(8)))
			if err != nil {
				return fmt.Errorf("error estimating approve gas: %v", err)
			}
			txOpts, err = cl.txOpts(ctx, 0, g.Transfer*2, nil)
			if err != nil {
				return fmt.Errorf("error constructing signed tx opts for transfer: %v", err)
			}
			transferTx, err = tokenContractor.transfer(txOpts, cl.address(), dexeth.GweiToWei(1))
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
		txOpts, err := cl.txOpts(ctx, optsVal, g.SwapN(n)*2, nil)
		if err != nil {
			return fmt.Errorf("error constructing signed tx opts for %d swaps: %v", n, err)
		}
		tx, err := c.initiate(txOpts, contracts)
		if err != nil {
			return fmt.Errorf("initiate error for %d swaps: %v", n, err)
		}
		waitForMined()
		receipt, err := cl.transactionReceipt(ctx, tx.Hash())
		if err != nil {
			return err
		}
		if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
			return err
		}
		stats.swaps = append(stats.swaps, receipt.GasUsed)

		if isToken {
			receipt, err = cl.transactionReceipt(ctx, approveTx.Hash())
			if err != nil {
				return err
			}
			if err := checkTxStatus(receipt, txOpts.GasLimit); err != nil {
				return err
			}
			stats.approves = append(stats.approves, receipt.GasUsed)
			receipt, err = cl.transactionReceipt(ctx, transferTx.Hash())
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

		txOpts, _ = cl.txOpts(ctx, 0, g.RedeemN(n)*2, nil)
		tx, err = c.redeem(txOpts, redemptions)
		if err != nil {
			return fmt.Errorf("redeem error for %d swaps: %v", n, err)
		}
		waitForMined()
		receipt, err = cl.transactionReceipt(ctx, tx.Hash())
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
