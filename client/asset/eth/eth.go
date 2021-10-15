// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/keygen"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	srveth "decred.org/dcrdex/server/asset/eth"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	// BipID is the BIP-0044 asset ID.
	BipID              = 60
	defaultGasFee      = 82  // gwei
	defaultGasFeeLimit = 200 // gwei

	RedeemGas = 63000 // gas
)

var (
	defaultAppDir = dcrutil.AppDataDir("dexethclient", false)
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker = time.Second
	configOpts  = []*asset.ConfigOption{
		// TODO: Use this limit.
		{
			Key:         "gasfeelimit",
			DisplayName: "Gas Fee Limit",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If gasfeelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: gwei / gas",
			DefaultValue: defaultGasFeeLimit,
		},
		{
			Key:          "appdir",
			DisplayName:  "DCR Dex Ethereum directory location.",
			Description:  "Location of the ethereum client data. This SHOULD NOT be a directory used by other ethereum applications. The default is recommended.",
			DefaultValue: defaultAppDir,
		},
	}
	// WalletInfo defines some general information about a Ethereum wallet.
	WalletInfo = &asset.WalletInfo{
		Name:     "Ethereum",
		UnitInfo: dexeth.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:              "geth",
				Tab:               "Internal",
				Description:       "Use the built-in DEX wallet with snap sync",
				ConfigOpts:        configOpts,
				Seeded:            true,
				DefaultConfigPath: defaultAppDir, // Incorrect if changed by user?
			},
		},
	}

	mainnetContractAddr = common.HexToAddress("")
)

// Check that Driver implements asset.Driver.
var _ asset.Driver = (*Driver)(nil)

// Driver implements asset.Driver.
type Driver struct{}

// Open opens the ETH exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Ethereum.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	id, err := srveth.DecodeCoinID(coinID)
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
	return false, errors.New("unimplemented")
}

func (d *Driver) Create(params *asset.CreateWalletParams) error {
	return CreateWallet(params)
}

// rawWallet is an unexported return type from the eth client. Watch for changes at
// https://github.com/ethereum/go-ethereum/blob/c503f98f6d5e80e079c1d8a3601d188af2a899da/internal/ethapi/api.go#L227-L253
type rawWallet struct {
	URL      string             `json:"url"`
	Status   string             `json:"status"`
	Failure  string             `json:"failure,omitempty"`
	Accounts []accounts.Account `json:"accounts,omitempty"`
}

// ethFetcher represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient. For testing, it can be satisfied by a stub.
type ethFetcher interface {
	accounts() []*accounts.Account
	addPeer(ctx context.Context, peer string) error
	balance(ctx context.Context, addr *common.Address) (*big.Int, error)
	bestBlockHash(ctx context.Context) (common.Hash, error)
	bestHeader(ctx context.Context) (*types.Header, error)
	block(ctx context.Context, hash common.Hash) (*types.Block, error)
	blockNumber(ctx context.Context) (uint64, error)
	connect(ctx context.Context, node *node.Node, contractAddr *common.Address) error
	listWallets(ctx context.Context) ([]rawWallet, error)
	initiate(opts *bind.TransactOpts, netID int64, refundTimestamp int64, secretHash [32]byte, participant *common.Address) (*types.Transaction, error)
	estimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	lock(ctx context.Context, acct *accounts.Account) error
	nodeInfo(ctx context.Context) (*p2p.NodeInfo, error)
	pendingTransactions(ctx context.Context) ([]*types.Transaction, error)
	peers(ctx context.Context) ([]*p2p.PeerInfo, error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	sendTransaction(ctx context.Context, tx map[string]string) (common.Hash, error)
	shutdown()
	syncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	redeem(opts *bind.TransactOpts, netID int64, secret, secretHash [32]byte) (*types.Transaction, error)
	refund(opts *bind.TransactOpts, netID int64, secretHash [32]byte) (*types.Transaction, error)
	swap(ctx context.Context, from *accounts.Account, secretHash [32]byte) (*dexeth.ETHSwapSwap, error)
	unlock(ctx context.Context, pw string, acct *accounts.Account) error
	signData(addr common.Address, data []byte) ([]byte, error)
}

// Check that ExchangeWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)

// ExchangeWallet is a wallet backend for Ethereum. The backend is how the DEX
// client app communicates with the Ethereum blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	ctx       context.Context // the asset subsystem starts with Connect(ctx)
	node      ethFetcher
	log       dex.Logger
	tipChange func(error)

	internalNode *node.Node

	tipMtx     sync.RWMutex
	currentTip *types.Block

	acct *accounts.Account

	cachedInitGas    uint64
	cachedInitGasMtx sync.Mutex

	lockedFunds    map[string]uint64 // gwei
	lockedFundsMtx sync.RWMutex
}

// Info returns basic information about the wallet and asset.
func (*ExchangeWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// CreateWallet creates a new internal ETH wallet and stores the private key
// derived from the wallet seed.
func CreateWallet(createWalletParams *asset.CreateWalletParams) error {
	walletDir := getWalletDir(createWalletParams.DataDir, createWalletParams.Net)
	nodeCFG := &nodeConfig{
		net:    createWalletParams.Net,
		appDir: walletDir,
	}
	node, err := prepareNode(nodeCFG)
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

	importKeyToNode(node, privateKey, createWalletParams.Pass)
	if err != nil {
		return err
	}

	return node.Close()
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. It starts an internal light node.
func NewWallet(assetCFG *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	walletDir := getWalletDir(assetCFG.DataDir, network)
	nodeCFG := &nodeConfig{
		net:    network,
		appDir: walletDir,
		logger: logger.SubLogger("NODE"),
	}

	node, err := prepareNode(nodeCFG)
	if err != nil {
		return nil, err
	}

	err = startNode(node, network)
	if err != nil {
		return nil, err
	}

	accounts := exportAccountsFromNode(node)
	if len(accounts) != 1 {
		return nil,
			fmt.Errorf("NewWallet: eth node keystore should only contain 1 account, but contains %d",
				len(accounts))
	}

	return &ExchangeWallet{
		log:          logger,
		tipChange:    assetCFG.TipChange,
		internalNode: node,
		lockedFunds:  make(map[string]uint64),
		acct:         &accounts[0],
	}, nil
}

func getWalletDir(dataDir string, network dex.Network) string {
	return filepath.Join(dataDir, network.String())
}

func (eth *ExchangeWallet) shutdown() {
	eth.node.shutdown()
	eth.internalNode.Close()
	eth.internalNode.Wait()
}

// Connect connects to the node RPC server. A dex.Connector.
func (eth *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	c := rpcclient{}
	err := c.connect(ctx, eth.internalNode, &mainnetContractAddr)
	if err != nil {
		return nil, err
	}
	eth.node = &c
	eth.ctx = ctx

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
	return &wg, nil
}

// OwnsAddress indicates if an address belongs to the wallet.
//
// In Ethereum, an address is an account.
func (eth *ExchangeWallet) OwnsAddress(address string) (bool, error) {
	return strings.ToLower(eth.acct.Address.String()) == strings.ToLower(address), nil
}

// Balance returns the total available funds in the account. The eth node
// returns balances in wei. Those are flored and stored as gwei, or 1e9 wei.
//
// TODO: Return Immature and Locked values.
func (eth *ExchangeWallet) Balance() (*asset.Balance, error) {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	return eth.balanceImpl()
}

// balanceImpl returns the total available funds in the account.
// This function expects eth.lockedFundsMtx to be held.
func (eth *ExchangeWallet) balanceImpl() (*asset.Balance, error) {
	if eth.acct == nil {
		return nil, errors.New("account not set")
	}
	bigBal, err := eth.node.balance(eth.ctx, &eth.acct.Address)
	if err != nil {
		return nil, err
	}
	gweiBal, err := srveth.ToGwei(bigBal)
	if err != nil {
		return nil, err
	}

	var amountLocked uint64
	for _, value := range eth.lockedFunds {
		amountLocked += value
	}
	if amountLocked > gweiBal {
		return nil,
			fmt.Errorf("amount locked: %v > available: %v", amountLocked, gweiBal)
	}

	bal := &asset.Balance{
		Available: gweiBal - amountLocked,
		Locked:    amountLocked,
		// Immature: , How to know?
	}
	return bal, nil
}

// getInitGas gets an estimate for the gas required for initiating a swap. The
// result is cached and reused in subsequent requests.
//
// This function will return an error if the wallet has a non zero balance.
func (eth *ExchangeWallet) getInitGas() (uint64, error) {
	eth.cachedInitGasMtx.Lock()
	defer eth.cachedInitGasMtx.Unlock()

	if eth.cachedInitGas > 0 {
		return eth.cachedInitGas, nil
	}

	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	parsedAbi, err := abi.JSON(strings.NewReader(dexeth.ETHSwapABI))
	if err != nil {
		return 0, err
	}
	data, err := parsedAbi.Pack("initiate", big.NewInt(1), secretHash, &eth.acct.Address)
	if err != nil {
		return 0, err
	}
	msg := ethereum.CallMsg{
		From:  eth.acct.Address,
		To:    &mainnetContractAddr,
		Value: big.NewInt(1),
		Gas:   0,
		Data:  data,
	}
	gas, err := eth.node.estimateGas(eth.ctx, msg)
	if err != nil {
		return 0, err
	}
	eth.cachedInitGas = gas
	return gas, nil
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
	initGas, err := eth.getInitGas()

	if err != nil {
		eth.log.Warnf("error getting init gas, falling back to server's value: %v", err)
		initGas = nfo.SwapSize
	}
	maxTxFee := initGas * nfo.MaxFeeRate
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
	initGas, err := eth.getInitGas()
	if err != nil {
		eth.log.Warnf("error getting init gas, falling back to server's value: %v", err)
		initGas = nfo.SwapSize
	}
	realisticTxFee := initGas * feeSuggestion
	maxTxFee := initGas * nfo.MaxFeeRate
	value := lots * lotSize
	maxFees := lots * maxTxFee
	realisticWorstCase := realisticTxFee * lots
	realisticBestCase := realisticTxFee
	locked := value + maxFees
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: realisticWorstCase,
		RealisticBestCase:  realisticBestCase,
		Locked:             locked,
	}
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (*ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  RedeemGas * req.FeeSuggestion,
			RealisticWorstCase: RedeemGas * req.FeeSuggestion * req.Lots,
		},
	}, nil
}

// coin implements the asset.Coin interface for ETH
type coin struct {
	id srveth.AmountCoinID
}

// ID is the ETH coins ID. It includes the address the coins came from (20 bytes)
// and the value of the coin (8 bytes).
func (c *coin) ID() dex.Bytes {
	serializedBytes := c.id.Encode()
	return dex.Bytes(serializedBytes)
}

// String is a string representation of the coin.
func (c *coin) String() string {
	return c.id.String()
}

// Value returns the value in gwei of the coin.
func (c *coin) Value() uint64 {
	return c.id.Amount
}

var _ asset.Coin = (*coin)(nil)

// decodeCoinID decodes a coin id into a coin object. The coin id
// must contain an AmountCoinID.
func decodeCoinID(coinID []byte) (*coin, error) {
	id, err := srveth.DecodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	amountCoinID, ok := id.(*srveth.AmountCoinID)
	if !ok {
		return nil,
			fmt.Errorf("coinID is expected to be an amount coin id")
	}

	return &coin{
		id: *amountCoinID,
	}, nil
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// In UTXO based coins, the returned []dex.Bytes contains the redeem scripts for the
// selected coins, but since there are no redeem scripts in Ethereum, nil is returned.
// Equal number of coins and redeem scripts must be returned.
func (eth *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	maxFees := ord.DEXConfig.MaxFeeRate * ord.DEXConfig.SwapSize * ord.MaxSwapCount
	fundsNeeded := ord.Value + maxFees

	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))
	var address [20]byte
	copy(address[:], eth.acct.Address.Bytes())
	coin := coin{
		id: srveth.AmountCoinID{
			Address: address,
			Amount:  fundsNeeded,
			Nonce:   nonce,
		},
	}
	coins := asset.Coins{&coin}

	err := eth.lockFunds(coins)
	if err != nil {
		return nil, nil, err
	}

	return coins, []dex.Bytes{nil}, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (eth *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	return eth.unlockFunds(unspents)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (eth *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	for _, id := range ids {
		coin, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(coin.id.Address.Bytes(), eth.acct.Address.Bytes()) {
			return nil, fmt.Errorf("FundingCoins: coin address %v != wallet address %v",
				coin.id.Address, eth.acct.Address)
		}

		coins = append(coins, coin)
	}

	err := eth.lockFunds(coins)
	if err != nil {
		return nil, err
	}

	return coins, nil
}

// lockFunds adds coins to the map of locked funds.
func (eth *ExchangeWallet) lockFunds(coins asset.Coins) error {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	currentlyLocking := make(map[string]bool)
	var amountToLock uint64
	for _, coin := range coins {
		hexID := hex.EncodeToString(coin.ID())
		if _, ok := eth.lockedFunds[hexID]; ok {
			return fmt.Errorf("cannot lock funds that are already locked: %v", hexID)
		}
		if _, ok := currentlyLocking[hexID]; ok {
			return fmt.Errorf("attempting to lock duplicate coins: %v", hexID)
		}
		currentlyLocking[hexID] = true
		amountToLock += coin.Value()
	}

	balance, err := eth.balanceImpl()
	if err != nil {
		return err
	}

	if balance.Available < amountToLock {
		return fmt.Errorf("currently available %v < locking %v",
			balance.Available, amountToLock)
	}

	for _, coin := range coins {
		eth.lockedFunds[hex.EncodeToString(coin.ID())] = coin.Value()
	}
	return nil
}

// lockFunds removes coins from the map of locked funds.
func (eth *ExchangeWallet) unlockFunds(coins asset.Coins) error {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	currentlyUnlocking := make(map[string]bool)
	for _, coin := range coins {
		hexID := hex.EncodeToString(coin.ID())
		if _, ok := eth.lockedFunds[hexID]; !ok {
			return fmt.Errorf("cannot unlock coin ID that is not locked: %v", coin.ID())
		}
		if _, ok := currentlyUnlocking[hexID]; ok {
			return fmt.Errorf("attempting to unlock duplicate coins: %v", hexID)
		}
		currentlyUnlocking[hexID] = true
	}

	for _, coin := range coins {
		delete(eth.lockedFunds, hex.EncodeToString(coin.ID()))
	}

	return nil
}

// Swap sends the swaps in a single transaction. The Receipts returned can be
// used to refund a failed transaction. The Input coins are manually unlocked
// because they're not auto-unlocked by the wallet and therefore inaccurately
// included as part of the locked balance despite being spent.
func (*ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrNotImplemented
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (*ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrNotImplemented
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (e *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	ethCoin, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, nil, err
	}

	if !bytes.Equal(ethCoin.id.Address.Bytes(), e.acct.Address.Bytes()) {
		return nil, nil, fmt.Errorf("SignMessage: coin address: %v != wallet address: %v",
			ethCoin.id.Address, e.acct.Address)
	}

	sig, err := e.node.signData(e.acct.Address, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error signing data: %v", err)
	}

	pubKey, err := secp256k1.RecoverPubkey(crypto.Keccak256(msg), sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %v", err)
	}

	return []dex.Bytes{pubKey}, []dex.Bytes{sig}, nil
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap.
func (*ExchangeWallet) AuditContract(coinID, contract, txData dex.Bytes, _ time.Time) (*asset.AuditInfo, error) {
	return nil, asset.ErrNotImplemented
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (*ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	return false, time.Time{}, asset.ErrNotImplemented
}

// FindRedemption watches for the input that spends the specified contract
// coin, and returns the spending input and the contract's secret key when it
// finds a spender.
//
// This method blocks until the redemption is found, an error occurs or the
// provided context is canceled.
func (*ExchangeWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	return nil, nil, asset.ErrNotImplemented
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
func (*ExchangeWallet) Refund(coinID, contract dex.Bytes) (dex.Bytes, error) {
	return nil, asset.ErrNotImplemented
}

// Address returns an address for the exchange wallet.
func (eth *ExchangeWallet) Address() (string, error) {
	return eth.acct.Address.String(), nil
}

// Unlock unlocks the exchange wallet.
func (eth *ExchangeWallet) Unlock(pw []byte) error {
	return eth.node.unlock(eth.ctx, string(pw), eth.acct)
}

// Lock locks the exchange wallet.
func (eth *ExchangeWallet) Lock() error {
	return eth.node.lock(eth.ctx, eth.acct)
}

// Locked will be true if the wallet is currently locked.
func (eth *ExchangeWallet) Locked() bool {
	wallets, err := eth.node.listWallets(eth.ctx)
	if err != nil {
		eth.log.Errorf("list wallets error: %v", err)
		return false
	}
	var wallet rawWallet
	findWallet := func() bool {
		for _, w := range wallets {
			for _, a := range w.Accounts {
				if bytes.Equal(a.Address[:], eth.acct.Address[:]) {
					wallet = w
					return true
				}
			}
		}
		return false
	}
	if !findWallet() {
		eth.log.Error("unable to find wallet for account: %v", eth.acct)
		return false
	}
	return wallet.Status != "Unlocked"
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
//
// NOTE: PayFee is not intended to be used with Ethereum at this time.
func (*ExchangeWallet) PayFee(address string, regFee, feeRateSuggestion uint64) (asset.Coin, error) {
	return nil, asset.ErrNotImplemented
}

func (*ExchangeWallet) SwapConfirmations(ctx context.Context, id dex.Bytes, contract dex.Bytes, startTime time.Time) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrNotImplemented
}

// sendToAddr sends funds from acct to addr.
func (eth *ExchangeWallet) sendToAddr(addr common.Address, amt, gasPrice *big.Int) (common.Hash, error) {
	tx := map[string]string{
		"from":     fmt.Sprintf("0x%x", eth.acct.Address),
		"to":       fmt.Sprintf("0x%x", addr),
		"value":    fmt.Sprintf("0x%x", amt),
		"gasPrice": fmt.Sprintf("0x%x", gasPrice),
	}
	return eth.node.sendTransaction(eth.ctx, tx)
}

// Withdraw withdraws funds to the specified address. Value is gwei.
//
// TODO: Return the asset.Coin.
// TODO: Subtract fees from the value.
func (eth *ExchangeWallet) Withdraw(addr string, value uint64) (asset.Coin, error) {
	bigVal := big.NewInt(0).SetUint64(value)
	gweiFactorBig := big.NewInt(srveth.GweiFactor)
	_, err := eth.sendToAddr(common.HexToAddress(addr),
		bigVal.Mul(bigVal, gweiFactorBig), big.NewInt(0).SetUint64(defaultGasFee))
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (*ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Confirmations gets the number of confirmations for the specified coin ID.
func (*ExchangeWallet) Confirmations(ctx context.Context, id dex.Bytes) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrNotImplemented
}

// SyncStatus is information about the blockchain sync status.
func (eth *ExchangeWallet) SyncStatus() (bool, float32, error) {
	// node.SyncProgress will return nil both before syncing has begun and
	// after it has finished. In order to discern when syncing has begun,
	// check that the best header came in under srveth.MaxBlockInterval.
	sp, err := eth.node.syncProgress(eth.ctx)
	if err != nil {
		return false, 0, err
	}
	if sp != nil {
		var ratio float32
		if sp.HighestBlock != 0 {
			ratio = float32(sp.CurrentBlock) / float32(sp.HighestBlock)
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
	if timeDiff < srveth.MaxBlockInterval {
		progress = 1
	}
	return progress == 1, progress, nil
}

// RefundAddress extracts and returns the refund address from a contract.
func (eth *ExchangeWallet) RefundAddress(contract dex.Bytes) (string, error) {
	return "", asset.ErrNotImplemented
}

func (eth *ExchangeWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	return 0, asset.ErrNotImplemented
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
	sameTip := bytes.Equal(currentTipHash[:], bestHash[:])
	if sameTip {
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
}
