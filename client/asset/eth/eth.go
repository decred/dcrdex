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
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	testTokenID, _ = dex.BipSymbolID("dextt.eth")
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
// In ETH there are 3 possible coin IDs:
//   1. A transaction hash
//   2. An encoded funding coin id which includes the account address and amount.
//   3. A byte encoded string of the account address.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txHashID, err := dexeth.DecodeCoinID(coinID)
	if err == nil {
		return txHashID.String(), nil
	}

	fundingCoinID, err := decodeFundingCoinID(coinID)
	if err == nil {
		return fundingCoinID.String(), nil
	}

	addressID := string(coinID)
	if len(addressID) == 42 && addressID[0:2] == "0x" {
		return addressID, nil
	}

	return "", fmt.Errorf("%x is not a valid ETH coin id", coinID)
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
	balance(ctx context.Context) (*Balance, error)
	bestBlockHash(ctx context.Context) (common.Hash, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	block(ctx context.Context, hash common.Hash) (*types.Block, error)
	connect(ctx context.Context) error
	initiate(ctx context.Context, contracts []*asset.Contract, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	shutdown()
	syncProgress() ethereum.SyncProgress
	peerCount() uint32
	isRedeemable(secretHash, secret [32]byte, contractVer uint32) (bool, error)
	redeem(ctx context.Context, redemptions []*asset.Redemption, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	isRefundable(secretHash [32]byte, contractVer uint32) (bool, error)
	refund(ctx context.Context, secretHash [32]byte, maxFeeRate uint64, contractVer uint32) (*types.Transaction, error)
	swap(ctx context.Context, secretHash [32]byte, contractVer uint32) (*dexeth.SwapState, error)
	lock() error
	locked() bool
	unlock(pw string) error
	signData(data []byte) (sig, pubKey []byte, err error)
	sendToAddr(ctx context.Context, addr common.Address, val uint64) (*types.Transaction, error)
	transactionConfirmations(context.Context, common.Hash) (uint32, error)
	sendSignedTransaction(ctx context.Context, tx *types.Transaction) error
}

// Check that ExchangeWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)
var _ asset.AccountLocker = (*ExchangeWallet)(nil)

// fundReserveType represents the various uses for which funds need to be locked:
// initiations, redemptions, and refunds.
type fundReserveType uint32

const (
	fundsReserveType fundReserveType = iota
	initiationReserve
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

// fundReserves is a struct which keeps track of the funds locked for
// initiations, redemptions, and refunds.
type fundReserves struct {
	initiateReserves   uint64
	redemptionReserves uint64
	refundReserves     uint64
}

// fundReserveOfType returns a pointer to the funds reserved for a particular
// use case.
func (r *fundReserves) fundReserveOfType(t fundReserveType) (*uint64, error) {
	switch t {
	case initiationReserve:
		return &r.initiateReserves, nil
	case redemptionReserve:
		return &r.redemptionReserves, nil
	case refundReserve:
		return &r.refundReserves, nil
	default:
		return nil, fmt.Errorf("invalid fund reserve type: %v", t)
	}
}

// lock locks funds for a use case.
func (r *fundReserves) lock(amt uint64, t fundReserveType, getBalanceFn func() (*asset.Balance, error)) error {
	reserve, err := r.fundReserveOfType(t)
	if err != nil {
		return err
	}

	balance, err := getBalanceFn()
	if err != nil {
		return err
	}

	if balance.Available < amt {
		return fmt.Errorf("attempting to lock more for %s than is currently available. %d > %d",
			t, amt, balance.Available)
	}

	*reserve += amt
	return nil
}

// unlock unlocks funds for a use case.
func (r *fundReserves) unlock(amt uint64, t fundReserveType) error {
	reserve, err := r.fundReserveOfType(t)
	if err != nil {
		return err
	}

	if *reserve < amt {
		*reserve = 0
		return fmt.Errorf("attempting to unlock more for %s than is currently locked - %d > %d. "+
			"clearing all locked funds", t, amt, *reserve)
	}

	*reserve -= amt
	return nil
}

// amount returns the total amount currently locked.
func (r fundReserves) amount() uint64 {
	return r.initiateReserves + r.redemptionReserves + r.refundReserves
}

// ExchangeWallet is a wallet backend for Ethereum. The backend is how the DEX
// client app communicates with the Ethereum blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	ctx           context.Context // the asset subsystem starts with Connect(ctx)
	net           dex.Network
	node          ethFetcher
	addr          common.Address
	log           dex.Logger
	tipChange     func(error)
	lastPeerCount uint32
	peersChange   func(uint32)
	gasFeeLimit   uint64

	tipMtx     sync.RWMutex
	currentTip *types.Block

	lockedFundsMtx sync.RWMutex
	lockedFunds    fundReserves

	findRedemptionMtx  sync.RWMutex
	findRedemptionReqs map[[32]byte]*findRedemptionRequest
}

// Info returns basic information about the wallet and asset.
func (*ExchangeWallet) Info() *asset.WalletInfo {
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
func NewWallet(assetCFG *asset.WalletConfig, logger dex.Logger, net dex.Network) (*ExchangeWallet, error) {
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

	return &ExchangeWallet{
		log:                logger,
		net:                net,
		node:               cl,
		addr:               cl.address(),
		tipChange:          assetCFG.TipChange,
		peersChange:        assetCFG.PeersChange,
		gasFeeLimit:        gasFeeLimit,
		findRedemptionReqs: make(map[[32]byte]*findRedemptionRequest),
	}, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

func (eth *ExchangeWallet) shutdown() {
	eth.node.shutdown()
}

// Connect connects to the node RPC server. A dex.Connector.
func (eth *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	eth.ctx = ctx

	err := eth.node.connect(ctx)
	if err != nil {
		return nil, err
	}

	// Initialize the best block.
	bestHash, err := eth.node.bestBlockHash(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting best block hash from geth: %w", err)
	}
	block, err := eth.node.block(ctx, bestHash)
	if err != nil {
		return nil, fmt.Errorf("error getting best block from geth: %w", err)
	}
	eth.tipMtx.Lock()
	eth.currentTip = block
	eth.tipMtx.Unlock()
	height := eth.currentTip.NumberU64()
	atomic.StoreInt64(&eth.tipAtConnect, int64(height))
	eth.log.Infof("Connected to geth, at height %d", height)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eth.monitorBlocks(ctx)
		eth.shutdown()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		eth.monitorPeers(ctx)
	}()
	return &wg, nil
}

// OwnsAddress indicates if an address belongs to the wallet. The address need
// not be a EIP55-compliant formatted address. It may or may not have a 0x
// prefix, and case is not important.
//
// In Ethereum, an address is an account.
func (eth *ExchangeWallet) OwnsAddress(address string) (bool, error) {
	if !common.IsHexAddress(address) {
		return false, errors.New("invalid address")
	}
	addr := common.HexToAddress(address)
	return addr == eth.addr, nil
}

// Balance returns the total available funds in the account. The eth node
// returns balances in wei. Those are flored and stored as gwei, or 1e9 wei.
func (eth *ExchangeWallet) Balance() (*asset.Balance, error) {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	return eth.balance()
}

func (eth *ExchangeWallet) balance() (*asset.Balance, error) {
	bal, err := eth.node.balance(eth.ctx)
	if err != nil {
		return nil, err
	}

	locked := eth.lockedFunds.amount()
	return &asset.Balance{
		Available: dexeth.WeiToGwei(bal.Current) - locked - dexeth.WeiToGwei(bal.PendingOut),
		Locked:    locked,
		Immature:  dexeth.WeiToGwei(bal.PendingIn),
	}, nil
}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
func (eth *ExchangeWallet) MaxOrder(lotSize uint64, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	balance, err := eth.Balance()
	if err != nil {
		return nil, err
	}
	if balance.Available == 0 {
		return &asset.SwapEstimate{}, nil
	}
	maxTxFee := dexeth.InitGas(1, nfo.Version) * nfo.MaxFeeRate
	lots := balance.Available / (lotSize + maxTxFee)
	if lots < 1 {
		return &asset.SwapEstimate{}, nil
	}
	est := eth.estimateSwap(lots, lotSize, feeSuggestion, nfo)
	return est, nil
}

// PreSwap gets order estimates based on the available funds and the wallet
// configuration.
func (eth *ExchangeWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	maxEst, err := eth.MaxOrder(req.LotSize, req.FeeSuggestion, req.AssetConfig)
	if err != nil {
		return nil, err
	}

	if maxEst.Lots < req.Lots {
		return nil, fmt.Errorf("%d lots available for %d-lot order", maxEst.Lots, req.Lots)
	}

	est := eth.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, req.AssetConfig)
	return &asset.PreSwap{
		Estimate: est,
	}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (eth *ExchangeWallet) estimateSwap(lots, lotSize, feeSuggestion uint64, nfo *dex.Asset) *asset.SwapEstimate {
	if lots == 0 {
		return &asset.SwapEstimate{}
	}
	value := lots * lotSize
	maxFees := dexeth.InitGas(1, nfo.Version) * nfo.MaxFeeRate * lots
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: lots * dexeth.InitGas(1, nfo.Version) * feeSuggestion,
		RealisticBestCase:  dexeth.InitGas(int(lots), nfo.Version) * feeSuggestion,
	}
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (*ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  dexeth.RedeemGas(int(req.Lots), 0) * req.FeeSuggestion,
			RealisticWorstCase: dexeth.RedeemGas(1, 0) * req.FeeSuggestion * req.Lots,
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

// fundingCoin is a coin that also implements asset.RecoveryCoin, enabling
// a custom database record and special handling of the recovery ID for use in
// later calls to FundingCoin.
type fundingCoin struct {
	*coin
	recoveryID []byte
}

func (c *fundingCoin) RecoveryID() dex.Bytes {
	return c.recoveryID[:]
}

func (c *fundingCoin) String() string {
	return fmt.Sprintf("{%s:%x}", []byte(c.id), c.recoveryID)
}

var _ asset.Coin = (*coin)(nil)

// decodeFundingCoinID decodes a coin id into a coin object. This function ensures
// that the id contains an encoded fundingCoinID whose address is the same as
// the one managed by this wallet.
func (eth *ExchangeWallet) decodeFundingCoinID(id []byte) (*coin, error) {
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

func (eth *ExchangeWallet) createFundingCoin(amount uint64) *fundingCoin {
	id := createFundingCoinID(eth.addr, amount)
	return &fundingCoin{
		coin: &coin{
			id:    []byte(eth.addr.String()),
			value: amount,
		},
		recoveryID: id.Encode(),
	}
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// In UTXO based coins, the returned []dex.Bytes contains the redeem scripts for the
// selected coins, but since there are no redeem scripts in Ethereum, nil is returned.
// Equal number of coins and redeem scripts must be returned.
func (eth *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	if eth.gasFeeLimit < ord.DEXConfig.MaxFeeRate {
		return nil, nil, fmt.Errorf(
			"%v: server's max fee rate %v higher than configured fee rate limit %v",
			ord.DEXConfig.Symbol,
			ord.DEXConfig.MaxFeeRate,
			eth.gasFeeLimit)
	}
	maxSwapFees := ord.DEXConfig.MaxFeeRate * ord.DEXConfig.SwapSize * ord.MaxSwapCount
	initiationFunds := ord.Value + maxSwapFees
	coins := asset.Coins{eth.createFundingCoin(initiationFunds)}
	err := eth.lockedFunds.lock(initiationFunds, initiationReserve, eth.balance)
	if err != nil {
		return nil, nil, err
	}

	return coins, []dex.Bytes{nil}, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (eth *ExchangeWallet) ReturnCoins(coins asset.Coins) error {
	var amt uint64
	for i := range coins {
		switch c := coins[i].(type) {
		case *fundingCoin:
			amt += c.value
		default:
			return fmt.Errorf("unsupported funding coin type for coin %[1]s: %[1]T", coins[i])
		}
	}
	err := eth.lockedFunds.unlock(amt, initiationReserve)
	if err != nil {
		eth.log.Error(err)
	}
	return nil
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (eth *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	var totalValue uint64
	for _, id := range ids {
		coin, err := eth.decodeFundingCoinID(id)
		if err != nil {
			return nil, fmt.Errorf("FundingCoins: unable to decode funding coin id: %w", err)
		}
		coins = append(coins, coin)
		totalValue += coin.Value()
	}

	err := eth.lockedFunds.lock(totalValue, initiationReserve, eth.balance)
	if err != nil {
		return nil, err
	}

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
		id:    r.txHash[:], // server's idea of ETH coin ID encoding
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
func (eth *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	fail := func(err error) ([]asset.Receipt, asset.Coin, uint64, error) {
		return nil, nil, 0, err
	}

	receipts := make([]asset.Receipt, 0, len(swaps.Contracts))

	var totalInputValue uint64
	for _, input := range swaps.Inputs {
		totalInputValue += input.Value()
	}

	var totalContractValue uint64
	for _, contract := range swaps.Contracts {
		totalContractValue += contract.Value
	}

	fees := dexeth.InitGas(1, swaps.AssetVersion) * uint64(len(swaps.Contracts)) * swaps.FeeRate

	totalSpend := fees + totalContractValue
	if totalSpend > totalInputValue {
		return nil, nil, 0, fmt.Errorf("unfunded contract. %d < %d", totalInputValue, totalSpend)
	}

	tx, err := eth.node.initiate(eth.ctx, swaps.Contracts, swaps.FeeRate, swaps.AssetVersion)
	if err != nil {
		return fail(fmt.Errorf("Swap: initiate error: %w", err))
	}

	txHash := tx.Hash()
	for _, swap := range swaps.Contracts {
		var secretHash [dexeth.SecretHashSize]byte
		copy(secretHash[:], swap.SecretHash)
		receipts = append(receipts,
			&swapReceipt{
				expiration:   encode.UnixTimeMilli(int64(swap.LockTime)),
				value:        swap.Value,
				txHash:       txHash,
				secretHash:   secretHash,
				ver:          swaps.AssetVersion,
				contractAddr: dexeth.ContractAddresses[swaps.AssetVersion][eth.net].String(),
			})
	}

	var change asset.Coin
	if swaps.LockChange {
		eth.lockedFunds.unlock(totalSpend, initiationReserve)
		change = eth.createFundingCoin(totalInputValue - totalSpend)
	} else {
		eth.lockedFunds.unlock(totalInputValue, initiationReserve)
	}

	return receipts, change, fees, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. All redemptions must be for the same contract version because the
// current API requires a single transaction reported (asset.Coin output), but
// conceptually a batch of redeems could be processed for any number of
// different contract addresses with multiple transactions.
func (eth *ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	fail := func(err error) ([]dex.Bytes, asset.Coin, uint64, error) {
		return nil, nil, 0, err
	}

	if len(form.Redemptions) == 0 {
		return fail(errors.New("Redeem: must be called with at least 1 redemption"))
	}

	var contractVersion uint32 // require a consistent version since this is a single transaction
	var redeemedValue, unlocked uint64
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
		redeemable, err := eth.node.isRedeemable(secretHash, secret, ver)
		if err != nil {
			return fail(fmt.Errorf("Redeem: failed to check if swap is redeemable: %w", err))
		}
		if !redeemable {
			return fail(fmt.Errorf("Redeem: secretHash %x not redeemable with secret %x",
				secretHash, secret))
		}

		swapData, err := eth.node.swap(eth.ctx, secretHash, ver)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("Redeem: error finding swap state: %w", err)
		}
		redeemedValue += swapData.Value
		unlocked += redemption.UnlockedReserves
	}

	outputCoin := eth.createFundingCoin(redeemedValue)
	fundsRequired := dexeth.RedeemGas(len(form.Redemptions), contractVersion) * form.FeeSuggestion

	// TODO: make sure the amount we locked for redemption is enough to cover the gas
	// fees. Also unlock coins.
	tx, err := eth.node.redeem(eth.ctx, form.Redemptions, form.FeeSuggestion, contractVersion)
	if err != nil {
		return fail(fmt.Errorf("Redeem: redeem error: %w", err))
	}

	eth.UnlockRedemptionReserves(unlocked)

	txHash := tx.Hash()
	txs := make([]dex.Bytes, len(form.Redemptions))
	for i := range txs {
		txs[i] = txHash[:]
	}

	return txs, outputCoin, fundsRequired, nil
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

// ReserveNRedemption locks funds for redemption. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (eth *ExchangeWallet) ReserveNRedemption(n, feeRate uint64, assetVer uint32) (uint64, error) {
	redeemCost := dexeth.RedeemGas(1, assetVer) * feeRate
	reserve := redeemCost * n

	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	err := eth.lockedFunds.lock(reserve, redemptionReserve, eth.balance)

	return reserve, err
}

// UnlockRedemptionReserves unlocks the specified amount from redemption
// reserves. Part of the AccountLocker interface.
func (eth *ExchangeWallet) UnlockRedemptionReserves(reserves uint64) {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	err := eth.lockedFunds.unlock(reserves, redemptionReserve)
	if err != nil {
		eth.log.Error(err)
	}
}

// ReReserveRedemption checks out an amount for redemptions. Use
// ReReserveRedemption after initializing a new ExchangeWallet.
// Part of the AccountLocker interface.
func (eth *ExchangeWallet) ReReserveRedemption(req uint64) error {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	return eth.lockedFunds.lock(req, redemptionReserve, eth.balance)
}

// ReserveNRefund locks funds for doing refunds. It is an error if there
// is insufficient spendable balance. Part of the AccountLocker interface.
func (eth *ExchangeWallet) ReserveNRefund(n uint64, assetVer uint32) (uint64, error) {
	refundCost := dexeth.RefundGas(assetVer) * eth.gasFeeLimit
	reserve := refundCost * n

	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	err := eth.lockedFunds.lock(reserve, refundReserve, eth.balance)

	return reserve, err
}

// UnlockRefundReserves unlocks the specified amount from refund
// reserves. Part of the AccountLocker interface.
func (eth *ExchangeWallet) UnlockRefundReserves(reserves uint64) {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	err := eth.lockedFunds.unlock(reserves, refundReserve)
	if err != nil {
		eth.log.Error(err)
	}
}

// ReReserveRefund checks out an amount for doing refunds. Use ReReserveRefund
// after initializing a new ExchangeWallet. Part of the AccountLocker
// interface.
func (eth *ExchangeWallet) ReReserveRefund(req uint64) error {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	return eth.lockedFunds.lock(req, refundReserve, eth.balance)
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (eth *ExchangeWallet) SignMessage(_ asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
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
func (eth *ExchangeWallet) AuditContract(coinID, contract, serializedTx dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
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
func (eth *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return false, time.Time{}, err
	}

	swap, err := eth.node.swap(eth.ctx, secretHash, contractVer)
	if err != nil {
		return false, time.Time{}, err
	}

	// Time is not yet set for uninitiated swaps.
	if swap.State == dexeth.SSNone {
		return false, time.Time{}, asset.ErrSwapNotInitiated
	}

	header, err := eth.node.bestHeader(eth.ctx)
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
func (eth *ExchangeWallet) sendFindRedemptionResult(req *findRedemptionRequest, secretHash [32]byte, secret []byte, err error) {
	select {
	case req.res <- &findRedemptionResult{secret: secret, err: err}:
	default:
		eth.log.Info("findRedemptionResult channel blocking for request %s", secretHash)
	}
}

// findRedemptionRequests creates a copy of the findRedemptionReqs map.
func (eth *ExchangeWallet) findRedemptionRequests() map[[32]byte]*findRedemptionRequest {
	eth.findRedemptionMtx.RLock()
	defer eth.findRedemptionMtx.RUnlock()
	reqs := make(map[[32]byte]*findRedemptionRequest, len(eth.findRedemptionReqs))
	for secretHash, req := range eth.findRedemptionReqs {
		reqs[secretHash] = req
	}
	return reqs
}

// FindRedemption checks the contract for a redemption. If the swap is initiated
// but un-redeemed and un-refunded, FindRedemption will block until a redemption
// is seen.
func (eth *ExchangeWallet) FindRedemption(ctx context.Context, _, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, nil, err
	}

	// See if it's ready right away.
	secret, err = eth.findSecret(ctx, secretHash, contractVer)
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

	eth.findRedemptionMtx.Lock()

	if eth.findRedemptionReqs[secretHash] != nil {
		eth.findRedemptionMtx.Unlock()
		return nil, nil, fmt.Errorf("duplicate find redemption request for %x", secretHash)
	}

	eth.findRedemptionReqs[secretHash] = req

	eth.findRedemptionMtx.Unlock()

	var res *findRedemptionResult
	select {
	case res = <-req.res:
	case <-ctx.Done():
	}

	eth.findRedemptionMtx.Lock()
	delete(eth.findRedemptionReqs, secretHash)
	eth.findRedemptionMtx.Unlock()

	if res == nil {
		return nil, nil, fmt.Errorf("context cancelled for find redemption request %x", secretHash)
	}

	if res.err != nil {
		return nil, nil, res.err
	}

	return findRedemptionCoinID, res.secret[:], nil
}

func (eth *ExchangeWallet) findSecret(ctx context.Context, secretHash [32]byte, contractVer uint32) ([]byte, error) {
	// Add a reasonable timeout here.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	swap, err := eth.node.swap(ctx, secretHash, contractVer)
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
func (eth *ExchangeWallet) Refund(_, contract dex.Bytes, _ uint64) (dex.Bytes, error) {
	version, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to decode contract: %w", err)
	}

	unlockFunds := func() {
		eth.lockedFunds.unlock(eth.gasFeeLimit*dexeth.RefundGas(version), refundReserve)
	}

	swap, err := eth.node.swap(eth.ctx, secretHash, version)
	if err != nil {
		return nil, err
	}
	// It's possible the swap was refunded by someone else. In that case we
	// cannot know the refunding tx hash.
	if swap.State == dexeth.SSRefunded {
		eth.log.Infof("Swap with secret hash %x already refunded.", secretHash)
		unlockFunds()
		zeroHash := common.Hash{}
		return zeroHash[:], nil
	}

	refundable, err := eth.node.isRefundable(secretHash, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to check isRefundable: %w", err)
	}
	if !refundable {
		return nil, fmt.Errorf("Refund: swap with secret hash %x is not refundable", secretHash)
	}

	tx, err := eth.node.refund(eth.ctx, secretHash, eth.gasFeeLimit, version)
	if err != nil {
		return nil, fmt.Errorf("Refund: failed to call refund: %w", err)
	}

	unlockFunds()

	txHash := tx.Hash()
	return txHash[:], nil
}

// Address returns an address for the exchange wallet. This implementation is
// idempotent, always returning the same address for a given ExchangeWallet.
func (eth *ExchangeWallet) Address() (string, error) {
	return eth.addr.String(), nil
}

// Unlock unlocks the exchange wallet.
func (eth *ExchangeWallet) Unlock(pw []byte) error {
	return eth.node.unlock(string(pw))
}

// Lock locks the exchange wallet.
func (eth *ExchangeWallet) Lock() error {
	return eth.node.lock()
}

// Locked will be true if the wallet is currently locked.
func (eth *ExchangeWallet) Locked() bool {
	return eth.node.locked()
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (eth *ExchangeWallet) PayFee(address string, regFee, _ uint64) (asset.Coin, error) {
	bal, err := eth.Balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	maxFee := defaultSendGasLimit * eth.gasFeeLimit
	need := regFee + maxFee
	if avail < need {
		return nil, fmt.Errorf("not enough funds to pay fee: have %d gwei need %d gwei", avail, need)
	}
	tx, err := eth.node.sendToAddr(eth.ctx, common.HexToAddress(address), regFee)
	if err != nil {
		return nil, err
	}
	txHash := tx.Hash()
	return &coin{id: txHash[:], value: dexeth.WeiToGwei(tx.Value())}, nil
}

// SwapConfirmations gets the number of confirmations and the spend status
// for the specified swap.
func (eth *ExchangeWallet) SwapConfirmations(ctx context.Context, _ dex.Bytes, contract dex.Bytes, _ time.Time) (confs uint32, spent bool, err error) {
	contractVer, secretHash, err := dexeth.DecodeContractData(contract)
	if err != nil {
		return 0, false, err
	}

	hdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("error fetching best header: %w", err)
	}

	swapData, err := eth.node.swap(ctx, secretHash, contractVer)
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

// Withdraw withdraws funds to the specified address. Value is gwei. The fee is
// subtracted from the total balance if it cannot be sent otherwise.
func (eth *ExchangeWallet) Withdraw(addr string, value, _ uint64) (asset.Coin, error) {
	bal, err := eth.Balance()
	if err != nil {
		return nil, err
	}
	avail := bal.Available
	if avail < value {
		return nil, fmt.Errorf("not enough funds to withdraw: have %d gwei need %d gwei", avail, value)
	}
	maxFee := defaultSendGasLimit * eth.gasFeeLimit
	if avail < maxFee {
		return nil, fmt.Errorf("not enough funds to withdraw: cannot cover max fee of %d gwei", maxFee)
	}
	if avail < value+maxFee {
		value -= maxFee
	}
	tx, err := eth.node.sendToAddr(eth.ctx, common.HexToAddress(addr), value)
	if err != nil {
		return nil, err
	}
	txHash := tx.Hash()
	return &coin{id: txHash[:], value: dexeth.WeiToGwei(tx.Value())}, nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (*ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// SyncStatus is information about the blockchain sync status.
func (eth *ExchangeWallet) SyncStatus() (bool, float32, error) {
	// node.syncProgress will return a zero value both before syncing has begun
	// and after it has finished. In order to discern when syncing has begun,
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

	// According to syncProgress we are at the highest network block, but check
	// the time since the block.
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
func (eth *ExchangeWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	var txHash common.Hash
	copy(txHash[:], coinID)
	return eth.node.transactionConfirmations(ctx, txHash)
}

func (eth *ExchangeWallet) checkPeers() {
	numPeers := eth.node.peerCount()
	prevPeer := atomic.SwapUint32(&eth.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		eth.peersChange(numPeers)
	}
}

func (eth *ExchangeWallet) monitorPeers(ctx context.Context) {
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
func (eth *ExchangeWallet) monitorBlocks(ctx context.Context) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			eth.checkForNewBlocks()
		case <-ctx.Done():
			return
		}
	}
}

// checkForNewBlocks checks for new blocks. When a tip change is detected, the
// tipChange callback function is invoked and a goroutine is started to check
// if any contracts in the findRedemptionQueue are redeemed in the new blocks.
func (eth *ExchangeWallet) checkForNewBlocks() {
	ctx, cancel := context.WithTimeout(eth.ctx, 2*time.Second)
	defer cancel()
	bestHash, err := eth.node.bestBlockHash(ctx)
	if err != nil {
		go eth.tipChange(fmt.Errorf("failed to get best hash: %w", err))
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
		go eth.tipChange(fmt.Errorf("failed to get best block: %w", err))
		return
	}

	eth.tipMtx.Lock()
	defer eth.tipMtx.Unlock()

	prevTip := eth.currentTip
	eth.currentTip = newTip
	eth.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.NumberU64(),
		prevTip.Hash(), newTip.NumberU64(), newTip.Hash())
	go eth.tipChange(nil)
	go eth.checkFindRedemptions()
}

// checkFindRedemptions checks queued findRedemptionRequests.
func (eth *ExchangeWallet) checkFindRedemptions() {
	for secretHash, req := range eth.findRedemptionRequests() {
		secret, err := eth.findSecret(eth.ctx, secretHash, req.contractVer)
		if err != nil {
			eth.sendFindRedemptionResult(req, secretHash, nil, err)
		} else if len(secret) > 0 {
			eth.sendFindRedemptionResult(req, secretHash, secret, nil)
		}
	}
}
