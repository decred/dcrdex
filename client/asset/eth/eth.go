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
	MinGasTipCap       = 2   // gwei
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
var _ asset.Creator = (*Driver)(nil)

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
	initiate(txOpts *bind.TransactOpts, netID int64, initiations []dexeth.ETHSwapInitiation) (*types.Transaction, error)
	lock(ctx context.Context, acct *accounts.Account) error
	nodeInfo(ctx context.Context) (*p2p.NodeInfo, error)
	pendingTransactions(ctx context.Context) ([]*types.Transaction, error)
	peers(ctx context.Context) ([]*p2p.PeerInfo, error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	sendTransaction(ctx context.Context, tx map[string]string) (common.Hash, error)
	shutdown()
	syncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
	redeem(txOpts *bind.TransactOpts, netID int64, redemptions []dexeth.ETHSwapRedemption) (*types.Transaction, error)
	refund(opts *bind.TransactOpts, netID int64, secretHash [32]byte) (*types.Transaction, error)
	swap(ctx context.Context, from *accounts.Account, secretHash [32]byte) (*dexeth.ETHSwapSwap, error)
	unlock(ctx context.Context, pw string, acct *accounts.Account) error
	signData(addr common.Address, data []byte) ([]byte, error)
	suggestGasTipCap(ctx context.Context) (*big.Int, error)
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

	networkID int64

	internalNode *node.Node

	tipMtx     sync.RWMutex
	currentTip *types.Block

	acct *accounts.Account

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

	err = importKeyToNode(node, privateKey, createWalletParams.Pass)
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

	accounts, err := exportAccountsFromNode(node)
	if err != nil {
		return nil, err
	}
	if len(accounts) != 1 {
		return nil,
			fmt.Errorf("NewWallet: eth node keystore should only contain 1 account, but contains %d",
				len(accounts))
	}

	networkID, err := getNetworkID(network)
	if err != nil {
		return nil, fmt.Errorf("NewWallet: unable to get network ID: %w", err)
	}

	return &ExchangeWallet{
		log:          logger,
		tipChange:    assetCFG.TipChange,
		internalNode: node,
		lockedFunds:  make(map[string]uint64),
		acct:         &accounts[0],
		networkID:    networkID,
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
	return addr == eth.acct.Address, nil
}

// Balance returns the total available funds in the account. The eth node
// returns balances in wei. Those are flored and stored as gwei, or 1e9 wei.
func (eth *ExchangeWallet) Balance() (*asset.Balance, error) {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	return eth.balanceImpl()
}

// txEffectOnBalance calculates the effects of a transaction on the balance
// of the sender. It only takes into account the transaction types expected
// to be sent by this wallet, including withdrawals, initiations, redeems,
// and refunds.
func (eth *ExchangeWallet) txEffectOnBalance(tx *types.Transaction) (incoming *big.Int, outgoing *big.Int, err error) {
	incoming, outgoing = new(big.Int), new(big.Int)

	// Max possible gas fee
	feeCap := tx.GasFeeCap()
	gas := big.NewInt(int64(tx.Gas()))
	outgoing.Mul(feeCap, gas)

	// If the tx contains value, it is either an initiation or withdrawal.
	// In either case we don't need to parse the tx data, so we can return
	// here.
	txValue := tx.Value()
	if txValue.Cmp(new(big.Int)) > 0 {
		outgoing.Add(outgoing, txValue)
		return incoming, outgoing, nil
	}

	if redemptions, err := dexeth.ParseRedeemData(tx.Data()); err == nil {
		for _, redemption := range redemptions {
			swap, err := eth.node.swap(eth.ctx, eth.acct, redemption.SecretHash)
			if err != nil {
				return nil, nil, err
			}
			incoming.Add(incoming, swap.Value)
		}
		return incoming, outgoing, nil
	}

	if refundHash, err := dexeth.ParseRefundData(tx.Data()); err == nil {
		swap, err := eth.node.swap(eth.ctx, eth.acct, refundHash)
		if err != nil {
			return nil, nil, err
		}
		incoming.Add(incoming, swap.Value)
		return incoming, outgoing, nil
	}

	eth.log.Warnf("unexpected eth transaction type: %v", tx)

	return incoming, outgoing, nil
}

// pendingBalances returns the incoming and outgoing funds as a result
// of currently pending transactions. This is used instead of the eth
// client's PendingBalanceAt function because pending incoming and outgoing
// funds must be treated differently in the eth wallet's Balance method.
func (eth *ExchangeWallet) pendingBalances() (incoming uint64, outgoing uint64, err error) {
	pendingTxs, err := eth.node.pendingTransactions(eth.ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, tx := range pendingTxs {
		in, out, err := eth.txEffectOnBalance(tx)
		if err != nil {
			return 0, 0, err
		}

		inGwei, err := srveth.ToGwei(in)
		if err != nil {
			return 0, 0, err
		}

		outGwei, err := srveth.ToGwei(out)
		if err != nil {
			return 0, 0, err
		}

		incoming += inGwei
		outgoing += outGwei
	}

	return incoming, outgoing, nil
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

	pendingIncoming, pendingOutgoing, err := eth.pendingBalances()
	if err != nil {
		return nil, fmt.Errorf("Balance: failed to get pending balances: %v", err)
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
		Available: gweiBal - amountLocked - pendingOutgoing,
		Locked:    amountLocked,
		Immature:  pendingIncoming,
	}
	return bal, nil
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
	maxTxFee := srveth.InitGas * nfo.MaxFeeRate
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
	maxFees := srveth.InitGas * nfo.MaxFeeRate * lots
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              value,
		MaxFees:            maxFees,
		RealisticWorstCase: lots * srveth.InitGas * feeSuggestion,
		RealisticBestCase:  srveth.InitGas * feeSuggestion,
		Locked:             value + maxFees,
	}
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (*ExchangeWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticBestCase:  srveth.RedeemGas * req.FeeSuggestion,
			RealisticWorstCase: srveth.RedeemGas * req.FeeSuggestion * req.Lots,
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
// the ID must contain an encoded AmountCoinID, but when returned from
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

// decodeAmountCoinID decodes a coin id into a coin object. This function ensures
// that the id contains an encoded AmountCoinID whose address is the same as
// the one managed by this wallet.
func (eth *ExchangeWallet) decodeAmountCoinID(id []byte) (*coin, error) {
	coinID, err := srveth.DecodeCoinID(id)
	if err != nil {
		return nil, err
	}

	amountCoinID, ok := coinID.(*srveth.AmountCoinID)
	if !ok {
		return nil, errors.New("coin id must be amount coin id")
	}

	if amountCoinID.Address != eth.acct.Address {
		return nil, fmt.Errorf("coin address %x != wallet address %x",
			amountCoinID.Address, eth.acct.Address)
	}

	return &coin{
		id:    amountCoinID.Encode(),
		value: amountCoinID.Amount,
	}, nil
}

func (eth *ExchangeWallet) createAmountCoin(amount uint64) *coin {
	id := srveth.CreateAmountCoinID(eth.acct.Address, amount)
	return &coin{
		id:    id.Encode(),
		value: amount,
	}
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// In UTXO based coins, the returned []dex.Bytes contains the redeem scripts for the
// selected coins, but since there are no redeem scripts in Ethereum, nil is returned.
// Equal number of coins and redeem scripts must be returned.
func (eth *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	maxSwapFees := ord.DEXConfig.MaxFeeRate * ord.DEXConfig.SwapSize * ord.MaxSwapCount
	refundFees := srveth.RefundGas * ord.DEXConfig.MaxFeeRate
	fundsNeeded := ord.Value + maxSwapFees + refundFees
	coins := asset.Coins{eth.createAmountCoin(fundsNeeded)}
	eth.lockedFundsMtx.Lock()
	err := eth.lockFunds(coins)
	eth.lockedFundsMtx.Unlock()
	if err != nil {
		return nil, nil, err
	}

	return coins, []dex.Bytes{nil}, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (eth *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()
	return eth.unlockFunds(unspents)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (eth *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	coins := make([]asset.Coin, 0, len(ids))
	for _, id := range ids {
		coin, err := eth.decodeAmountCoinID(id)
		if err != nil {
			return nil, fmt.Errorf("FundingCoins: unable to decode amount coin id: %w", err)
		}
		coins = append(coins, coin)
	}

	eth.lockedFundsMtx.Lock()
	err := eth.lockFunds(coins)
	eth.lockedFundsMtx.Unlock()
	if err != nil {
		return nil, err
	}

	return coins, nil
}

// lockFunds adds coins to the map of locked funds.
//
// lockedFundsMtx MUST be held when calling this function.
func (eth *ExchangeWallet) lockFunds(coins asset.Coins) error {
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

// unlockFunds removes coins from the map of locked funds.
//
// lockedFundsMtx MUST be held when calling this function.
func (eth *ExchangeWallet) unlockFunds(coins asset.Coins) error {
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

// swapReceipt implements the asset.Receipt interface for ETH.
type swapReceipt struct {
	txHash     common.Hash
	secretHash [32]byte
	// expiration and value can be determined with a blockchain
	// lookup, but we cache these values to avoid this.
	expiration time.Time
	value      uint64
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
		id:    dex.Bytes(r.txHash[:]),
	}
}

// Contract returns the swap's secret hash.
func (r *swapReceipt) Contract() dex.Bytes {
	return dex.Bytes(r.secretHash[:])
}

// String returns a string representation of the swapReceipt.
func (r *swapReceipt) String() string {
	return fmt.Sprintf("tx hash: %x, secret hash: %x", r.txHash, r.secretHash)
}

// SignedRefund returns an empty byte array. ETH does not support a pre-signed
// redeem script becuase the nonce needed in the transaction cannot be previously
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

	initiations := make([]dexeth.ETHSwapInitiation, 0, len(swaps.Contracts))
	receipts := make([]asset.Receipt, 0, len(swaps.Contracts))

	eth.lockedFundsMtx.Lock()
	defer eth.lockedFundsMtx.Unlock()

	var totalInputValue uint64
	for _, input := range swaps.Inputs {
		if _, ok := eth.lockedFunds[input.ID().String()]; !ok {
			return fail(fmt.Errorf("Swap: attempting to use coin that was not locked"))
		}
		totalInputValue += input.Value()
	}

	var totalContractValue uint64
	secretHashMap := make(map[string]bool)
	for _, contract := range swaps.Contracts {
		totalContractValue += contract.Value

		if len(contract.SecretHash) != srveth.SecretHashSize {
			return fail(
				fmt.Errorf("Swap: expected secret hash of length %v, but got %v",
					srveth.SecretHashSize, len(contract.SecretHash)))
		}
		encSecretHash := hex.EncodeToString(contract.SecretHash)
		if _, ok := secretHashMap[encSecretHash]; ok {
			return fail(fmt.Errorf("Swap: secret hashes must be unique"))
		}
		secretHashMap[encSecretHash] = true
		var secretHash [32]byte
		copy(secretHash[:], contract.SecretHash)

		if !common.IsHexAddress(contract.Address) {
			return fail(fmt.Errorf("Swap: address in contract is not valid %v", contract.Address))
		}
		address := common.HexToAddress(contract.Address)

		initiations = append(initiations,
			dexeth.ETHSwapInitiation{
				RefundTimestamp: big.NewInt(0).SetUint64(contract.LockTime),
				SecretHash:      secretHash,
				Participant:     address,
				Value:           srveth.ToWei(contract.Value),
			})
	}

	// If the (baseFee + GasTipCap) in the block that the transaction is mined
	// is less than the GasFeeCap, then some money will be saved. These funds
	// will be unlocked when the entire order has finished executing.
	maxPossibleFee := uint64(len(initiations)) * srveth.InitGas * swaps.FeeRate

	totalUsedValue := totalContractValue + maxPossibleFee
	if totalInputValue < totalUsedValue {
		return fail(fmt.Errorf("Swap: coin inputs value %d < required %d", totalInputValue, totalUsedValue))
	}

	suggestedGasTipCap, err := eth.node.suggestGasTipCap(eth.ctx)
	if err != nil {
		return fail(fmt.Errorf("Swap: failed to get suggested gas tip cap %w", err))
	}

	var gasTipCap *big.Int
	suggestedGasTipCapGwei, err := srveth.ToGwei(suggestedGasTipCap)
	if err != nil {
		return fail(fmt.Errorf("Swap: failed to convert to gwei: %w", err))
	}
	if suggestedGasTipCapGwei > MinGasTipCap {
		gasTipCap = suggestedGasTipCap
	} else {
		gasTipCap = srveth.ToWei(MinGasTipCap)
	}

	opts := bind.TransactOpts{
		From:      eth.acct.Address,
		Value:     srveth.ToWei(totalContractValue),
		GasFeeCap: srveth.ToWei(swaps.FeeRate),
		GasTipCap: gasTipCap,
		Context:   eth.ctx}

	tx, err := eth.node.initiate(&opts, eth.networkID, initiations)
	if err != nil {
		return fail(fmt.Errorf("Swap: initiate error: %w", err))
	}

	var txHash [common.HashLength]byte
	copy(txHash[:], tx.Hash().Bytes())
	for _, initiation := range initiations {
		gweiValue, err := srveth.ToGwei(initiation.Value)
		if err != nil {
			return fail(fmt.Errorf("Swap: failed to convert wei to gwei: %w", err))
		}
		receipts = append(receipts,
			&swapReceipt{
				expiration: time.Unix(initiation.RefundTimestamp.Int64(), 0),
				value:      gweiValue,
				txHash:     txHash,
				secretHash: initiation.SecretHash,
			})
	}

	eth.unlockFunds(swaps.Inputs)
	var change asset.Coin
	changeAmount := totalInputValue - totalUsedValue
	if changeAmount > 0 {
		change = eth.createAmountCoin(changeAmount)
	}
	if swaps.LockChange && change != nil {
		eth.lockFunds(asset.Coins{change})
	}

	return receipts, change, maxPossibleFee, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (*ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrNotImplemented
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (eth *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	_, err = eth.decodeAmountCoinID(coin.ID())
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error decoding coin: %w", err)
	}

	sig, err := eth.node.signData(eth.acct.Address, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error signing data: %w", err)
	}

	pubKey, err := secp256k1.RecoverPubkey(crypto.Keccak256(msg), sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %w", err)
	}

	return []dex.Bytes{pubKey}, []dex.Bytes{sig}, nil
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap.
func (*ExchangeWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
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
func (*ExchangeWallet) Refund(coinID, contract dex.Bytes, feeSuggestion uint64) (dex.Bytes, error) {
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
				if a.Address == eth.acct.Address {
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
func (eth *ExchangeWallet) Withdraw(addr string, value, feeSuggestion uint64) (asset.Coin, error) {
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
}
