// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/rpcclient/v8"
)

const (
	version = 0
	BipID   = 133
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee          = 10
	defaultFeeRateLimit = 1000
	minNetworkVersion   = 5090150 // v5.9.1
	walletTypeRPC       = "zcashdRPC"

	// defaultConfTarget is the amount of confirmations required to consider
	// a transaction is confirmed for tx history.
	defaultConfTarget = 1

	// transparentAcctNumber = 0
	shieldedAcctNumber = 0

	transparentAddressType = "p2pkh"
	orchardAddressType     = "orchard"
	saplingAddressType     = "sapling"
	unifiedAddressType     = "unified"

	minOrchardConfs = 1
	// nActionsOrchardEstimate is used for tx fee estimation. Scanning 1000
	// previous blocks, only found 1 with a tx with > 6 nActionsOrchard. Most
	// are 2.
	nActionsOrchardEstimate = 6

	blockTicker     = time.Second
	peerCountTicker = 5 * time.Second

	// requiredConfTxConfirms is the amount of confirms a redeem or refund
	// transaction needs before the trade is considered confirmed. The
	// redeem is monitored until this number of confirms is reached.
	requiredConfTxConfirms = 1

	depositAddrPrefix = "unified:"
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Zcash's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Zcash's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcbind",
			DisplayName: "JSON-RPC Address",
			Description: "<addr> or <addr>:<port> (default 'localhost')",
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Port for RPC connections (if not set in Address)",
		},
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Zcash's 'fallbackfee' rate. Units: ZEC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Zcash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Zcash",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexzec.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to zcashd",
			DefaultConfigPath: dexbtc.SystemConfigPath("zcash"),
			ConfigOpts:        configOpts,
			NoAuth:            true,
		}},
	}

	feeReservesPerLot = dexzec.TxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the ZEC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Zcash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Zcash shielded transactions don't have transparent outputs, so the coinID
	// will just be the tx hash.
	if len(coinID) == chainhash.HashSize {
		var txHash chainhash.Hash
		copy(txHash[:], coinID)
		return txHash.String(), nil
	}
	// For transparent transactions, Zcash and Bitcoin have the same tx hash
	// and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexzec.MinLotSize(maxFeeRate)
}

// WalletConfig are wallet-level configuration settings.
type WalletConfig struct {
	UseSplitTx       bool   `ini:"txsplit"`
	RedeemConfTarget uint64 `ini:"redeemconftarget"`
	ActivelyUsed     bool   `ini:"special_activelyUsed"` // injected by core
}

func newRPCConnection(cfg *dexbtc.RPCConfig) (*rpcclient.Client, error) {
	return rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         cfg.RPCBind,
		User:         cfg.RPCUser,
		Pass:         cfg.RPCPass,
	}, nil)
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the zcashd config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	var btcParams *chaincfg.Params
	var addrParams *dexzec.AddressParams
	switch net {
	case dex.Mainnet:
		btcParams = dexzec.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzec.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzec.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8232",
		Testnet: "18232",
		Simnet:  "18232",
	}

	var walletCfg WalletConfig
	err := config.Unmapify(cfg.Settings, &walletCfg)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("error creating data directory at %q: %w", cfg.DataDir, err)
	}

	ar, err := btc.NewAddressRecycler(filepath.Join(cfg.DataDir, "recycled-addrs.txt"), logger)
	if err != nil {
		return nil, fmt.Errorf("error creating address recycler: %w", err)
	}

	var rpcCfg dexbtc.RPCConfig
	if err := config.Unmapify(cfg.Settings, &rpcCfg); err != nil {
		return nil, fmt.Errorf("error reading settings: %w", err)
	}

	if err := dexbtc.CheckRPCConfig(&rpcCfg, "Zcash", net, ports); err != nil {
		return nil, fmt.Errorf("rpc config error: %v", err)
	}

	cl, err := newRPCConnection(&rpcCfg)
	if err != nil {
		return nil, fmt.Errorf("error constructing rpc client: %w", err)
	}

	zw := &zecWallet{
		peersChange: cfg.PeersChange,
		emit:        cfg.Emit,
		log:         logger,
		net:         net,
		btcParams:   btcParams,
		addrParams:  addrParams,
		decodeAddr: func(addr string, net *chaincfg.Params) (btcutil.Address, error) {
			return dexzec.DecodeAddress(addr, addrParams, btcParams)
		},
		ar:         ar,
		node:       cl,
		walletDir:  cfg.DataDir,
		pendingTxs: make(map[chainhash.Hash]*btc.ExtendedWalletTx),
	}
	zw.walletCfg.Store(&walletCfg)
	zw.prepareCoinManager()
	zw.prepareRedemptionFinder()
	return zw, nil
}

type rpcCaller interface {
	CallRPC(method string, args []any, thing any) error
}

// zecWallet is an asset.Wallet for Zcash. zecWallet is a shielded-first wallet.
// This has a number of implications.
//  1. zecWallet will, by default, keep funds in the shielded pool.
//  2. If you send funds with z_sendmany, the change will go to the shielded pool.
//     This means that we cannot maintain a transparent pool at all, since this
//     behavior is unavoidable in the Zcash API, so sending funds to ourselves,
//     for instance, would probably result in sending more into shielded than
//     we wanted. This does not apply to fully transparent transactions such as
//     swaps and redemptions.
//  3. When funding is requested for an order, we will generally generate a
//     "split transaction", but one that moves funds from the shielded pool to
//     a transparent receiver. This will be default behavior for Zcash now, and
//     cannot be disabled via configuration.
//  4. ...
type zecWallet struct {
	ctx           context.Context
	log           dex.Logger
	net           dex.Network
	lastAddress   atomic.Value // "string"
	btcParams     *chaincfg.Params
	addrParams    *dexzec.AddressParams
	node          btc.RawRequester
	ar            *btc.AddressRecycler
	rf            *btc.RedemptionFinder
	decodeAddr    dexbtc.AddressDecoder
	lastPeerCount uint32
	peersChange   func(uint32, error)
	emit          *asset.WalletEmitter
	walletCfg     atomic.Value // *WalletConfig
	walletDir     string

	// Coins returned by Fund are cached for quick reference.
	cm *btc.CoinManager

	tipAtConnect atomic.Uint64

	tipMtx     sync.RWMutex
	currentTip *btc.BlockVector

	reserves atomic.Uint64

	pendingTxsMtx sync.RWMutex
	pendingTxs    map[chainhash.Hash]*btc.ExtendedWalletTx

	receiveTxLastQuery atomic.Uint64

	txHistoryDB      atomic.Value // *btc.BadgerTxDB
	syncingTxHistory atomic.Bool
}

var _ asset.FeeRater = (*zecWallet)(nil)
var _ asset.Wallet = (*zecWallet)(nil)
var _ asset.WalletHistorian = (*zecWallet)(nil)
var _ asset.NewAddresser = (*zecWallet)(nil)

// TODO: Implement LiveReconfigurer
// var _ asset.LiveReconfigurer = (*zecWallet)(nil)

func (w *zecWallet) CallRPC(method string, args []any, thing any) error {
	return btc.Call(w.ctx, w.node, method, args, thing)
}

// FeeRate returns the asset standard fee rate for Zcash.
func (w *zecWallet) FeeRate() uint64 {
	return 5000 // per logical action
}

func (w *zecWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	w.ctx = ctx

	if err := w.connectRPC(ctx); err != nil {
		return nil, err
	}

	fullTBalance, err := getBalance(w)
	if err != nil {
		return nil, fmt.Errorf("error getting account-wide t-balance: %w", err)
	}

	const minConfs = 0
	acctBal, err := zGetBalanceForAccount(w, shieldedAcctNumber, minConfs)
	if err != nil {
		return nil, fmt.Errorf("error getting account balance: %w", err)
	}

	locked, err := w.lockedZats()
	if err != nil {
		return nil, fmt.Errorf("error getting locked zats: %w", err)
	}

	if acctBal.Transparent+locked != fullTBalance {
		return nil, errors.New(
			"there appears to be some transparent balance that is not in the zeroth account. " +
				"To operate correctly, all balance must be in the zeroth account. " +
				"Move all balance to the zeroth account to use the Zcash wallet",
		)
	}

	// Initialize the best block.
	bestBlockHdr, err := getBestBlockHeader(w)
	if err != nil {
		return nil, fmt.Errorf("error initializing best block: %w", err)
	}
	bestBlockHash, err := chainhash.NewHashFromStr(bestBlockHdr.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid best block hash from node: %v", err)
	}

	bestBlock := &btc.BlockVector{Height: bestBlockHdr.Height, Hash: *bestBlockHash}
	w.log.Infof("Connected wallet with current best block %v (%d)", bestBlock.Hash, bestBlock.Height)
	w.tipMtx.Lock()
	w.currentTip = bestBlock
	w.tipMtx.Unlock()
	w.tipAtConnect.Store(uint64(w.currentTip.Height))

	wg, err := w.startTxHistoryDB(ctx)
	if err != nil {
		return nil, err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		w.ar.WriteRecycledAddrsToFile()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.watchBlocks(ctx)
		w.rf.CancelRedemptionSearches()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.monitorPeers(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.syncTxHistory(uint64(w.currentTip.Height))
	}()

	return wg, nil
}

func (w *zecWallet) monitorPeers(ctx context.Context) {
	ticker := time.NewTicker(peerCountTicker)
	defer ticker.Stop()
	for {
		w.checkPeers()

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (w *zecWallet) checkPeers() {
	numPeers, err := peerCount(w)
	if err != nil {
		prevPeer := atomic.SwapUint32(&w.lastPeerCount, 0)
		if prevPeer != 0 {
			w.log.Errorf("Failed to get peer count: %v", err)
			w.peersChange(0, err)
		}
		return
	}
	prevPeer := atomic.SwapUint32(&w.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		w.peersChange(numPeers, nil)
	}
}

func (w *zecWallet) prepareCoinManager() {
	w.cm = btc.NewCoinManager(
		w.log,
		w.btcParams,
		func(val, lots, maxFeeRate uint64, reportChange bool) btc.EnoughFunc { // orderEnough
			return func(inputCount, inputsSize, sum uint64) (bool, uint64) {
				req := dexzec.RequiredOrderFunds(val, inputCount, inputsSize, lots)
				if sum >= req {
					excess := sum - req
					if !reportChange || isDust(val, dexbtc.P2SHOutputSize) {
						excess = 0
					}
					return true, excess
				}
				return false, 0
			}
		},
		func() ([]*btc.ListUnspentResult, error) { // list
			return listUnspent(w)
		},
		func(unlock bool, ops []*btc.Output) error { // lock
			return lockUnspent(w, unlock, ops)
		},
		func() ([]*btc.RPCOutpoint, error) { // listLocked
			return listLockUnspent(w, w.log)
		},
		func(txHash *chainhash.Hash, vout uint32) (*wire.TxOut, error) {
			walletTx, err := getWalletTransaction(w, txHash)
			if err != nil {
				return nil, err
			}
			tx, err := dexzec.DeserializeTx(walletTx.Bytes)
			if err != nil {
				w.log.Warnf("Invalid transaction %v (%x): %v", txHash, walletTx.Bytes, err)
				return nil, nil
			}
			if vout >= uint32(len(tx.TxOut)) {
				w.log.Warnf("Invalid vout %d for %v", vout, txHash)
				return nil, nil
			}
			return tx.TxOut[vout], nil
		},
		func(addr btcutil.Address) (string, error) {
			return dexzec.EncodeAddress(addr, w.addrParams)
		},
	)
}

func (w *zecWallet) prepareRedemptionFinder() {
	w.rf = btc.NewRedemptionFinder(
		w.log,
		func(h *chainhash.Hash) (*btc.GetTransactionResult, error) {
			tx, err := getWalletTransaction(w, h)
			if err != nil {
				return nil, err
			}
			if tx.Confirmations < 0 {
				tx.Confirmations = 0
			}
			return &btc.GetTransactionResult{
				Confirmations: uint64(tx.Confirmations),
				BlockHash:     tx.BlockHash,
				BlockTime:     tx.BlockTime,
				TxID:          tx.TxID,
				Time:          tx.Time,
				TimeReceived:  tx.TimeReceived,
				Bytes:         tx.Bytes,
			}, nil
		},
		func(h *chainhash.Hash) (int32, error) {
			return getBlockHeight(w, h)
		},
		func(h chainhash.Hash) (*wire.MsgBlock, error) {
			blk, err := getBlock(w, h)
			if err != nil {
				return nil, err
			}
			return &blk.MsgBlock, nil
		},
		func(h *chainhash.Hash) (hdr *btc.BlockHeader, mainchain bool, err error) {
			return getVerboseBlockHeader(w, h)
		},
		hashTx,
		deserializeTx,
		func() (int32, error) {
			return getBestBlockHeight(w)
		},
		func(ctx context.Context, reqs map[btc.OutPoint]*btc.FindRedemptionReq, blockHash chainhash.Hash) (discovered map[btc.OutPoint]*btc.FindRedemptionResult) {
			blk, err := getBlock(w, blockHash)
			if err != nil {
				w.log.Errorf("RPC GetBlock error: %v", err)
				return
			}
			return btc.SearchBlockForRedemptions(ctx, reqs, &blk.MsgBlock, false, hashTx, w.btcParams)
		},
		func(h int64) (*chainhash.Hash, error) {
			return getBlockHash(w, h)
		},
		func(ctx context.Context, reqs map[btc.OutPoint]*btc.FindRedemptionReq) (discovered map[btc.OutPoint]*btc.FindRedemptionResult) {
			getRawMempool := func() ([]*chainhash.Hash, error) {
				return getRawMempool(w)
			}
			getMsgTx := func(txHash *chainhash.Hash) (*wire.MsgTx, error) {
				tx, err := getZecTransaction(w, txHash)
				if err != nil {
					return nil, err
				}
				return tx.MsgTx, nil
			}
			return btc.FindRedemptionsInMempool(ctx, w.log, reqs, getRawMempool, getMsgTx, false, hashTx, w.btcParams)
		},
	)
}

func (w *zecWallet) connectRPC(ctx context.Context) error {
	netVer, _, err := getVersion(w)
	if err != nil {
		return fmt.Errorf("error getting version: %w", err)
	}
	if netVer < minNetworkVersion {
		return fmt.Errorf("reported node version %d is less than minimum %d", netVer, minNetworkVersion)
	}
	chainInfo, err := getBlockchainInfo(w)
	if err != nil {
		return fmt.Errorf("getblockchaininfo error: %w", err)
	}
	if !btc.ChainOK(w.net, chainInfo.Chain) {
		return errors.New("wrong net")
	}
	// Make sure we have zeroth and first account or are able to create them.
	accts, err := zListAccounts(w)
	if err != nil {
		return fmt.Errorf("error listing Zcash accounts: %w", err)
	}

	createAccount := func(n uint32) error {
		for _, acct := range accts {
			if acct.Number == n {
				return nil
			}
		}
		acctNumber, err := zGetNewAccount(w)
		if err != nil {
			if strings.Contains(err.Error(), "zcashd-wallet-tool") {
				return fmt.Errorf("account %d does not exist and cannot be created because wallet seed backup has not been acknowledged with the zcashd-wallet-tool utility", n)
			}
			return fmt.Errorf("error creating account %d: %w", n, err)
		}
		if acctNumber != n {
			return fmt.Errorf("no account %d found and newly created account has unexpected account number %d", n, acctNumber)
		}
		return nil
	}
	if err := createAccount(shieldedAcctNumber); err != nil {
		return err
	}
	return nil
}

// watchBlocks pings for new blocks and runs the tipChange callback function
// when the block changes.
func (w *zecWallet) watchBlocks(ctx context.Context) {
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()

	for {
		select {

		// Poll for the block. If the wallet offers tip reports, delay reporting
		// the tip to give the wallet a moment to request and scan block data.
		case <-ticker.C:
			newTipHdr, err := getBestBlockHeader(w)
			if err != nil {
				w.log.Errorf("failed to get best block header from node: %v", err)
				continue
			}
			newTipHash, err := chainhash.NewHashFromStr(newTipHdr.Hash)
			if err != nil {
				w.log.Errorf("invalid best block hash from node: %v", err)
				continue
			}

			w.tipMtx.RLock()
			sameTip := w.currentTip.Hash == *newTipHash
			w.tipMtx.RUnlock()
			if sameTip {
				continue
			}

			newTip := &btc.BlockVector{Height: newTipHdr.Height, Hash: *newTipHash}
			w.reportNewTip(ctx, newTip)

		case <-ctx.Done():
			return
		}

		// Ensure context cancellation takes priority before the next iteration.
		if ctx.Err() != nil {
			return
		}
	}
}

// reportNewTip sets the currentTip. The tipChange callback function is invoked
// and a goroutine is started to check if any contracts in the
// findRedemptionQueue are redeemed in the new blocks.
func (w *zecWallet) reportNewTip(ctx context.Context, newTip *btc.BlockVector) {
	w.tipMtx.Lock()
	defer w.tipMtx.Unlock()

	prevTip := w.currentTip
	w.currentTip = newTip
	w.log.Tracef("tip change: %d (%s) => %d (%s)", prevTip.Height, prevTip.Hash, newTip.Height, newTip.Hash)
	w.emit.TipChange(uint64(newTip.Height))

	w.rf.ReportNewTip(ctx, prevTip, newTip)

	w.syncTxHistory(uint64(newTip.Height))
}

type swapOptions struct {
	Split *bool `ini:"swapsplit"`
	// DRAFT TODO: Strip swapfeebump from PreSwap results.
	// FeeBump *float64 `ini:"swapfeebump"`
}

func (w *zecWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	ordValStr := btcutil.Amount(ord.Value).String()
	w.log.Debugf("Attempting to fund Zcash order, maxFeeRate = %d, max swaps = %d",
		ord.MaxFeeRate, ord.MaxSwapCount)

	if ord.Value == 0 {
		return nil, nil, 0, newError(errNoFundsRequested, "cannot fund value = 0")
	}
	if ord.MaxSwapCount == 0 {
		return nil, nil, 0, fmt.Errorf("cannot fund a zero-lot order")
	}

	var customCfg swapOptions
	err := config.Unmapify(ord.Options, &customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing swap options: %w", err)
	}

	useSplit := w.useSplitTx()
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	bals, err := w.balances()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("balances error: %w", err)
	}

	utxos, _, _, err := w.cm.SpendableUTXOs(0)
	if err != nil {
		return nil, nil, 0, newError(errFunding, "error listing utxos: %w", err)
	}

	sum, size, shieldedSplitNeeded, shieldedSplitFees, coins, fundingCoins, redeemScripts, spents, err := w.fund(
		ord.Value, ord.MaxSwapCount, utxos, bals.orchard,
	)
	if err != nil {
		return nil, nil, 0, err
	}

	inputsSize := size + uint64(wire.VarIntSerializeSize(uint64(len(coins))))
	var transparentSplitFees uint64

	if shieldedSplitNeeded > 0 {
		acctAddr, err := w.lastShieldedAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("lastShieldedAddress error: %w", err)
		}

		toAddrBTC, err := transparentAddress(w, w.addrParams, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("DepositAddress error: %w", err)
		}

		toAddrStr, err := dexzec.EncodeAddress(toAddrBTC, w.addrParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("EncodeAddress error: %w", err)
		}

		pkScript, err := txscript.PayToAddrScript(toAddrBTC)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("PayToAddrScript error: %w", err)
		}

		txHash, err := w.sendOne(w.ctx, acctAddr, toAddrStr, shieldedSplitNeeded, AllowRevealedRecipients)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error sending shielded split tx: %w", err)
		}

		tx, err := getTransaction(w, txHash)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error retreiving split transaction %s: %w", txHash, err)
		}

		var splitOutput *wire.TxOut
		var splitOutputIndex int
		for vout, txOut := range tx.TxOut {
			if txOut.Value >= int64(shieldedSplitNeeded) && bytes.Equal(txOut.PkScript, pkScript) {
				splitOutput = txOut
				splitOutputIndex = vout
				break
			}
		}
		if splitOutput == nil {
			return nil, nil, 0, fmt.Errorf("split output of size %d not found in transaction %s", shieldedSplitNeeded, txHash)
		}

		op := btc.NewOutput(txHash, uint32(splitOutputIndex), uint64(splitOutput.Value))
		fundingCoins = map[btc.OutPoint]*btc.UTxO{op.Pt: {
			TxHash:  &op.Pt.TxHash,
			Vout:    op.Pt.Vout,
			Address: toAddrStr,
			Amount:  shieldedSplitNeeded,
		}}
		coins = []asset.Coin{op}
		redeemScripts = []dex.Bytes{nil}
		spents = []*btc.Output{op}
	} else if useSplit {
		// No shielded split needed. Should we do a split to avoid overlock.
		splitTxFees := dexzec.TxFeesZIP317(inputsSize, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
		requiredForOrderWithoutSplit := dexzec.RequiredOrderFunds(ord.Value, uint64(len(coins)), inputsSize, ord.MaxSwapCount)
		excessWithoutSplit := sum - requiredForOrderWithoutSplit
		if splitTxFees >= excessWithoutSplit {
			w.log.Debugf("Skipping split transaction because cost is greater than potential over-lock. "+
				"%s > %s", btcutil.Amount(splitTxFees), btcutil.Amount(excessWithoutSplit))
		} else {
			splitOutputVal := dexzec.RequiredOrderFunds(ord.Value, 1, dexbtc.RedeemP2PKHInputSize, ord.MaxSwapCount)
			transparentSplitFees = splitTxFees
			baseTx, _, _, err := w.fundedTx(spents)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("fundedTx error: %w", err)
			}

			splitOutputAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("transparentAddress (0) error: %w", err)
			}
			splitOutputScript, err := txscript.PayToAddrScript(splitOutputAddr)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("split output addr PayToAddrScript error: %w", err)
			}
			baseTx.AddTxOut(wire.NewTxOut(int64(splitOutputVal), splitOutputScript))

			changeAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("transparentAddress (1) error: %w", err)
			}

			splitTx, _, err := w.signTxAndAddChange(baseTx, changeAddr, sum, splitOutputVal, transparentSplitFees)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("signTxAndAddChange error: %v", err)
			}

			splitTxHash, err := sendRawTransaction(w, splitTx)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("sendRawTransaction error: %w", err)
			}

			if *splitTxHash != splitTx.TxHash() {
				return nil, nil, 0, errors.New("split tx had unexpected hash")
			}

			w.log.Debugf("Sent split tx spending %d outputs with a sum value of %d to get a sized output of value %d",
				len(coins), sum, splitOutputVal)

			op := btc.NewOutput(splitTxHash, 0, splitOutputVal)

			addrStr, err := dexzec.EncodeAddress(splitOutputAddr, w.addrParams)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error stringing address %q: %w", splitOutputAddr, err)
			}

			fundingCoins = map[btc.OutPoint]*btc.UTxO{op.Pt: {
				TxHash:  &op.Pt.TxHash,
				Vout:    op.Pt.Vout,
				Address: addrStr,
				Amount:  splitOutputVal,
			}}
			coins = []asset.Coin{op}
			redeemScripts = []dex.Bytes{nil}
			spents = []*btc.Output{op}
		}
	}

	w.log.Debugf("Funding %s ZEC order with coins %v worth %s", ordValStr, coins, btcutil.Amount(sum))

	w.cm.LockOutputsMap(fundingCoins)
	err = lockUnspent(w, false, spents)
	if err != nil {
		return nil, nil, 0, newError(errLockUnspent, "LockUnspent error: %w", err)
	}
	return coins, redeemScripts, shieldedSplitFees + transparentSplitFees, nil
}

// Redeem sends the redemption transaction, completing the atomic swap.
func (w *zecWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	// Create a transaction that spends the referenced contract.
	tx := dexzec.NewTxFromMsgTx(wire.NewMsgTx(dexzec.VersionNU5), dexzec.MaxExpiryHeight)
	var totalIn uint64
	contracts := make([][]byte, 0, len(form.Redemptions))
	prevScripts := make([][]byte, 0, len(form.Redemptions))
	addresses := make([]btcutil.Address, 0, len(form.Redemptions))
	values := make([]int64, 0, len(form.Redemptions))
	var txInsSize uint64
	for _, r := range form.Redemptions {
		if r.Spends == nil {
			return nil, nil, 0, fmt.Errorf("no audit info")
		}

		cinfo, err := btc.ConvertAuditInfo(r.Spends, w.decodeAddr, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("ConvertAuditInfo error: %w", err)
		}

		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		contract := cinfo.Contract()
		_, receiver, _, secretHash, err := dexbtc.ExtractSwapDetails(contract, false /* segwit */, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error extracting swap addresses: %w", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, 0, fmt.Errorf("secret hash mismatch")
		}
		pkScript, err := w.scriptHashScript(contract)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error constructs p2sh script: %v", err)
		}
		prevScripts = append(prevScripts, pkScript)
		addresses = append(addresses, receiver)
		contracts = append(contracts, contract)
		txIn := wire.NewTxIn(cinfo.Output.WireOutPoint(), nil, nil)
		tx.AddTxIn(txIn)
		values = append(values, int64(cinfo.Output.Val))
		totalIn += cinfo.Output.Val
		txInsSize = tx.SerializeSize()
	}

	txInsSize += uint64(wire.VarIntSerializeSize(uint64(len(tx.TxIn))))
	txOutsSize := uint64(1 + dexbtc.P2PKHOutputSize)
	fee := dexzec.TxFeesZIP317(txInsSize, txOutsSize, 0, 0, 0, 0)

	// Send the funds back to the exchange wallet.
	redeemAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting new address from the wallet: %w", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change script: %w", err)
	}
	val := totalIn - fee
	txOut := wire.NewTxOut(int64(val), pkScript)
	// One last check for dust.
	if isDust(val, dexbtc.P2PKHOutputSize) {
		return nil, nil, 0, fmt.Errorf("redeem output is dust")
	}
	tx.AddTxOut(txOut)

	for i, r := range form.Redemptions {
		contract := contracts[i]
		addr := addresses[i]

		addrStr, err := dexzec.EncodeAddress(addr, w.addrParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("EncodeAddress error: %w", err)
		}

		privKey, err := dumpPrivKey(w, addrStr)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("dumpPrivKey error: %w", err)
		}
		defer privKey.Zero()

		redeemSig, err := signTx(tx.MsgTx, i, contract, txscript.SigHashAll, privKey, values, prevScripts)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("tx signing error: %w", err)
		}
		redeemPubKey := privKey.PubKey().SerializeCompressed()

		tx.TxIn[i].SignatureScript, err = dexbtc.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("RedeemP2SHContract error: %w", err)
		}
	}

	// Send the transaction.
	txHash, err := sendRawTransaction(w, tx)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error sending tx: %w", err)
	}

	w.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Redeem,
		ID:     txHash.String(),
		Amount: totalIn,
		Fees:   fee,
	}, txHash, true)

	// Log the change output.
	coinIDs := make([]dex.Bytes, 0, len(form.Redemptions))
	for i := range form.Redemptions {
		coinIDs = append(coinIDs, btc.ToCoinID(txHash, uint32(i)))
	}
	return coinIDs, btc.NewOutput(txHash, 0, uint64(txOut.Value)), fee, nil
}

// scriptHashAddress returns a new p2sh address.
func (w *zecWallet) scriptHashAddress(contract []byte) (btcutil.Address, error) {
	return btcutil.NewAddressScriptHash(contract, w.btcParams)
}

func (w *zecWallet) scriptHashScript(contract []byte) ([]byte, error) {
	addr, err := w.scriptHashAddress(contract)
	if err != nil {
		return nil, err
	}
	return txscript.PayToAddrScript(addr)
}

func (w *zecWallet) ReturnCoins(unspents asset.Coins) error {
	return w.cm.ReturnCoins(unspents)
}

func (w *zecWallet) MaxOrder(ord *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	_, _, maxEst, err := w.maxOrder(ord.LotSize, ord.FeeSuggestion, ord.MaxFeeRate)
	return maxEst, err
}

func (w *zecWallet) maxOrder(lotSize, feeSuggestion, maxFeeRate uint64) (utxos []*btc.CompositeUTXO, bals *balances, est *asset.SwapEstimate, err error) {
	if lotSize == 0 {
		return nil, nil, nil, errors.New("cannot divide by lotSize zero")
	}

	utxos, _, avail, err := w.cm.SpendableUTXOs(0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error parsing unspent outputs: %w", err)
	}

	bals, err = w.balances()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error getting current balance: %w", err)
	}

	avail += bals.orchard.avail

	// Find the max lots we can fund.
	maxLotsInt := int(avail / lotSize)
	oneLotTooMany := sort.Search(maxLotsInt+1, func(lots int) bool {
		_, _, _, err = w.estimateSwap(uint64(lots), lotSize, feeSuggestion, maxFeeRate, utxos, bals.orchard, true)
		// The only failure mode of estimateSwap -> zec.fund is when there is
		// not enough funds.
		return err != nil
	})

	maxLots := uint64(oneLotTooMany - 1)
	if oneLotTooMany == 0 {
		maxLots = 0
	}

	if maxLots > 0 {
		est, _, _, err = w.estimateSwap(maxLots, lotSize, feeSuggestion, maxFeeRate, utxos, bals.orchard, true)
		return utxos, bals, est, err
	}

	return utxos, bals, &asset.SwapEstimate{FeeReservesPerLot: feeReservesPerLot}, nil
}

// estimateSwap prepares an *asset.SwapEstimate.
func (w *zecWallet) estimateSwap(
	lots, lotSize, feeSuggestion, maxFeeRate uint64,
	utxos []*btc.CompositeUTXO,
	orchardBal *balanceBreakdown,
	trySplit bool,
) (*asset.SwapEstimate, bool /*split used*/, uint64 /*amt locked*/, error) {

	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.Amount
	}
	val := lots * lotSize
	sum, inputsSize, shieldedSplitNeeded, shieldedSplitFees, coins, _, _, _, err := w.fund(val, lots, utxos, orchardBal)
	if err != nil {
		return nil, false, 0, fmt.Errorf("error funding swap value %s: %w", btcutil.Amount(val), err)
	}

	if shieldedSplitNeeded > 0 {
		// DRAFT TODO: Do we need to "lock" anything here?
		const splitLocked = 0
		return &asset.SwapEstimate{
			Lots:               lots,
			Value:              val,
			MaxFees:            shieldedSplitFees,
			RealisticBestCase:  shieldedSplitFees,
			RealisticWorstCase: shieldedSplitFees,
			FeeReservesPerLot:  feeReservesPerLot,
		}, true, splitLocked, nil
	}

	digestInputs := func(inputsSize uint64, withSplit bool) (reqFunds, maxFees, estHighFees, estLowFees uint64) {
		n := uint64(len(coins))

		splitFees := shieldedSplitFees
		if withSplit {
			splitInputsSize := inputsSize + uint64(wire.VarIntSerializeSize(n))
			splitOutputsSize := uint64(2*dexbtc.P2PKHOutputSize + 1)
			splitFees = dexzec.TxFeesZIP317(splitInputsSize, splitOutputsSize, 0, 0, 0, 0)
			inputsSize = dexbtc.RedeemP2PKHInputSize
			n = 1
		}

		firstSwapInputsSize := inputsSize + uint64(wire.VarIntSerializeSize(n))
		singleOutputSize := uint64(dexbtc.P2SHOutputSize+dexbtc.P2PKHOutputSize) + 1
		estLowFees = dexzec.TxFeesZIP317(firstSwapInputsSize, singleOutputSize, 0, 0, 0, 0)

		req := dexzec.RequiredOrderFunds(val, n, inputsSize, lots)
		maxFees = req - val + splitFees
		estHighFees = maxFees
		return
	}

	// Math for split transactions is a little different.
	if trySplit {
		baggage := dexzec.TxFeesZIP317(inputsSize, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
		excess := sum - dexzec.RequiredOrderFunds(val, uint64(len(coins)), inputsSize, lots)
		if baggage >= excess {
			reqFunds, maxFees, estHighFees, estLowFees := digestInputs(inputsSize, true)
			return &asset.SwapEstimate{
				Lots:               lots,
				Value:              val,
				MaxFees:            maxFees,
				RealisticBestCase:  estLowFees,
				RealisticWorstCase: estHighFees,
				FeeReservesPerLot:  feeReservesPerLot,
			}, true, reqFunds, nil
		}
	}

	_, maxFees, estHighFees, estLowFees := digestInputs(inputsSize, false)
	return &asset.SwapEstimate{
		Lots:               lots,
		Value:              val,
		MaxFees:            maxFees,
		RealisticBestCase:  estLowFees,
		RealisticWorstCase: estHighFees,
		FeeReservesPerLot:  feeReservesPerLot,
	}, false, sum, nil
}

// PreSwap get order estimates and order options based on the available funds
// and user-selected options.
func (w *zecWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	// Start with the maxOrder at the default configuration. This gets us the
	// utxo set, the network fee rate, and the wallet's maximum order size. The
	// utxo set can then be used repeatedly in estimateSwap at virtually zero
	// cost since there are no more RPC calls.
	utxos, bals, maxEst, err := w.maxOrder(req.LotSize, req.FeeSuggestion, req.MaxFeeRate)
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
	useSplit := w.useSplitTx()
	if customCfg.Split != nil {
		useSplit = *customCfg.Split
	}

	// Always offer the split option, even for non-standing orders since
	// immediately spendable change many be desirable regardless.
	opts := []*asset.OrderOption{w.splitOption(req, utxos, bals.orchard)}

	est, _, _, err := w.estimateSwap(req.Lots, req.LotSize, req.FeeSuggestion, req.MaxFeeRate, utxos, bals.orchard, useSplit)
	if err != nil {
		return nil, err
	}
	return &asset.PreSwap{
		Estimate: est, // may be nil so we can present options, which in turn affect estimate feasibility
		Options:  opts,
	}, nil
}

// splitOption constructs an *asset.OrderOption with customized text based on the
// difference in fees between the configured and test split condition.
func (w *zecWallet) splitOption(req *asset.PreSwapForm, utxos []*btc.CompositeUTXO, orchardBal *balanceBreakdown) *asset.OrderOption {
	opt := &asset.OrderOption{
		ConfigOption: asset.ConfigOption{
			Key:           "swapsplit",
			DisplayName:   "Pre-size Funds",
			IsBoolean:     true,
			DefaultValue:  w.useSplitTx(), // not nil interface
			ShowByDefault: true,
		},
		Boolean: &asset.BooleanConfig{},
	}

	noSplitEst, _, noSplitLocked, err := w.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, orchardBal, false)
	if err != nil {
		w.log.Errorf("estimateSwap (no split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate without a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}
	splitEst, splitUsed, splitLocked, err := w.estimateSwap(req.Lots, req.LotSize,
		req.FeeSuggestion, req.MaxFeeRate, utxos, orchardBal, true)
	if err != nil {
		w.log.Errorf("estimateSwap (with split) error: %v", err)
		opt.Boolean.Reason = fmt.Sprintf("estimate with a split failed with \"%v\"", err)
		return opt // utility and overlock report unavailable, but show the option
	}

	if !splitUsed || splitLocked >= noSplitLocked { // locked check should be redundant
		opt.Boolean.Reason = "avoids no ZEC overlock for this order (ignored)"
		opt.Description = "A split transaction for this order avoids no ZEC overlock, " +
			"but adds additional fees."
		opt.DefaultValue = false
		return opt // not enabled by default, but explain why
	}

	overlock := noSplitLocked - splitLocked
	pctChange := (float64(splitEst.RealisticWorstCase)/float64(noSplitEst.RealisticWorstCase) - 1) * 100
	if pctChange > 1 {
		opt.Boolean.Reason = fmt.Sprintf("+%d%% fees, avoids %s ZEC overlock", int(math.Round(pctChange)), btcutil.Amount(overlock).String())
	} else {
		opt.Boolean.Reason = fmt.Sprintf("+%.1f%% fees, avoids %s ZEC overlock", pctChange, btcutil.Amount(overlock).String())
	}

	xtraFees := splitEst.RealisticWorstCase - noSplitEst.RealisticWorstCase
	opt.Description = fmt.Sprintf("Using a split transaction to prevent temporary overlock of %s ZEC, but for additional fees of %s ZEC",
		btcutil.Amount(overlock).String(), btcutil.Amount(xtraFees).String())

	return opt
}

// SingleLotSwapRefundFees returns the fees for a swap and refund transaction
// for a single lot.
func (w *zecWallet) SingleLotSwapRefundFees(_ uint32, feeSuggestion uint64, useSafeTxSize bool) (swapFees uint64, refundFees uint64, err error) {
	var numInputs uint64
	if useSafeTxSize {
		numInputs = 12
	} else {
		numInputs = 2
	}

	inputsSize := numInputs*dexbtc.RedeemP2PKHInputSize + 1
	outputsSize := uint64(dexbtc.P2PKHOutputSize + 1)
	swapFees = dexzec.TxFeesZIP317(inputsSize, outputsSize, 0, 0, 0, 0)
	refundFees = dexzec.TxFeesZIP317(dexbtc.RefundSigScriptSize+1, dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)

	return swapFees, refundFees, nil
}

func (w *zecWallet) PreRedeem(form *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return w.preRedeem(form.Lots, form.FeeSuggestion, form.SelectedOptions)
}

func (w *zecWallet) preRedeem(numLots, _ uint64, options map[string]string) (*asset.PreRedeem, error) {
	singleInputsSize := uint64(dexbtc.TxInOverhead + dexbtc.RedeemSwapSigScriptSize + 1)
	singleOutputsSize := uint64(dexbtc.P2PKHOutputSize + 1)
	singleMatchFees := dexzec.TxFeesZIP317(singleInputsSize, singleOutputsSize, 0, 0, 0, 0)
	return &asset.PreRedeem{
		Estimate: &asset.RedeemEstimate{
			RealisticWorstCase: singleMatchFees * numLots,
			RealisticBestCase:  singleMatchFees,
		},
	}, nil
}

func (w *zecWallet) SingleLotRedeemFees(_ uint32, feeSuggestion uint64) (uint64, error) {
	singleInputsSize := uint64(dexbtc.TxInOverhead + dexbtc.RedeemSwapSigScriptSize + 1)
	singleOutputsSize := uint64(dexbtc.P2PKHOutputSize + 1)
	return dexzec.TxFeesZIP317(singleInputsSize, singleOutputsSize, 0, 0, 0, 0), nil
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (w *zecWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	return w.cm.FundingCoins(ids)
}

func decodeCoinID(coinID dex.Bytes) (*chainhash.Hash, uint32, error) {
	if len(coinID) != 36 {
		return nil, 0, fmt.Errorf("coin ID wrong length. expected 36, got %d", len(coinID))
	}
	var txHash chainhash.Hash
	copy(txHash[:], coinID[:32])
	return &txHash, binary.BigEndian.Uint32(coinID[32:]), nil
}

// fundedTx creates and returns a new MsgTx with the provided coins as inputs.
func (w *zecWallet) fundedTx(coins []*btc.Output) (*dexzec.Tx, uint64, []btc.OutPoint, error) {
	baseTx := zecTx(wire.NewMsgTx(dexzec.VersionNU5))
	totalIn, pts, err := w.addInputsToTx(baseTx, coins)
	if err != nil {
		return nil, 0, nil, err
	}
	return baseTx, totalIn, pts, nil
}

func (w *zecWallet) addInputsToTx(tx *dexzec.Tx, coins []*btc.Output) (uint64, []btc.OutPoint, error) {
	var totalIn uint64
	// Add the funding utxos.
	pts := make([]btc.OutPoint, 0, len(coins))
	for _, op := range coins {
		totalIn += op.Val
		txIn := wire.NewTxIn(op.WireOutPoint(), []byte{}, nil)
		tx.AddTxIn(txIn)
		pts = append(pts, op.Pt)
	}
	return totalIn, pts, nil
}

// signTxAndAddChange signs the passed tx and adds a change output if the change
// wouldn't be dust. Returns but does NOT broadcast the signed tx.
func (w *zecWallet) signTxAndAddChange(baseTx *dexzec.Tx, addr btcutil.Address, totalIn, totalOut, fees uint64) (*dexzec.Tx, *btc.Output, error) {

	makeErr := func(s string, a ...any) (*dexzec.Tx, *btc.Output, error) {
		return nil, nil, fmt.Errorf(s, a...)
	}

	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	remaining := totalIn - totalOut
	if fees > remaining {
		b, _ := baseTx.Bytes()
		return makeErr("not enough funds to cover minimum fee rate. %s < %s, raw tx: %x",
			btcutil.Amount(totalIn), btcutil.Amount(fees+totalOut), b)
	}

	// Create a change output.
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return makeErr("error creating change script: %v", err)
	}

	changeIdx := len(baseTx.TxOut)
	changeValue := remaining - fees
	changeOutput := wire.NewTxOut(int64(changeValue), changeScript)

	// If the change is not dust, recompute the signed txn size and iterate on
	// the fees vs. change amount.
	changeAdded := !isDust(changeValue, dexbtc.P2PKHOutputSize)
	if changeAdded {
		// Add the change output.
		baseTx.AddTxOut(changeOutput)
	} else {
		w.log.Debugf("Foregoing change worth up to %v because it is dust", changeOutput.Value)
	}

	msgTx, err := signTxByRPC(w, baseTx)
	if err != nil {
		b, _ := baseTx.Bytes()
		return makeErr("signing error: %v, raw tx: %x", err, b)
	}

	txHash := msgTx.TxHash()
	var change *btc.Output
	if changeAdded {
		change = btc.NewOutput(&txHash, uint32(changeIdx), uint64(changeOutput.Value))
	}

	return msgTx, change, nil
}

type balanceBreakdown struct {
	avail     uint64
	maturing  uint64
	noteCount uint32
}

type balances struct {
	orchard     *balanceBreakdown
	sapling     uint64
	transparent *balanceBreakdown
}

func (b *balances) available() uint64 {
	return b.orchard.avail + b.transparent.avail
}

func (w *zecWallet) balances() (*balances, error) {
	zeroConf, err := zGetBalanceForAccount(w, shieldedAcctNumber, 0)
	if err != nil {
		return nil, fmt.Errorf("z_getbalanceforaccount (0) error: %w", err)
	}
	mature, err := zGetBalanceForAccount(w, shieldedAcctNumber, minOrchardConfs)
	if err != nil {
		return nil, fmt.Errorf("z_getbalanceforaccount (3) error: %w", err)
	}
	noteCounts, err := zGetNotesCount(w)
	if err != nil {
		return nil, fmt.Errorf("z_getnotescount error: %w", err)
	}
	return &balances{
		orchard: &balanceBreakdown{
			avail:     mature.Orchard,
			maturing:  zeroConf.Orchard - mature.Orchard,
			noteCount: noteCounts.Orchard,
		},
		sapling: zeroConf.Sapling,
		transparent: &balanceBreakdown{
			avail:    mature.Transparent,
			maturing: zeroConf.Transparent - mature.Transparent,
		},
	}, nil
}

func (w *zecWallet) fund(
	val, maxSwapCount uint64,
	utxos []*btc.CompositeUTXO,
	orchardBal *balanceBreakdown,
) (
	sum, size, shieldedSplitNeeded, shieldedSplitFees uint64,
	coins asset.Coins,
	fundingCoins map[btc.OutPoint]*btc.UTxO,
	redeemScripts []dex.Bytes,
	spents []*btc.Output,
	err error,
) {

	var shieldedAvail uint64
	reserves := w.reserves.Load()
	nActionsOrchard := uint64(orchardBal.noteCount)
	enough := func(inputCount, inputsSize, sum uint64) (bool, uint64) {
		req := dexzec.RequiredOrderFunds(val, inputCount, inputsSize, maxSwapCount)
		if sum >= req {
			return true, sum - req + shieldedAvail
		}
		if shieldedAvail == 0 {
			return false, 0
		}
		shieldedFees := dexzec.TxFeesZIP317(inputsSize+uint64(wire.VarIntSerializeSize(inputCount)), dexbtc.P2PKHOutputSize+1, 0, 0, 0, nActionsOrchard)
		req = dexzec.RequiredOrderFunds(val, 1, dexbtc.RedeemP2PKHInputSize, maxSwapCount)
		if sum+shieldedAvail >= req+shieldedFees {
			return true, sum + shieldedAvail - (req + shieldedFees)
		}
		return false, 0
	}

	coins, fundingCoins, spents, redeemScripts, size, sum, err = w.cm.FundWithUTXOs(utxos, reserves, false, enough)
	if err == nil {
		return
	}

	shieldedAvail = orchardBal.avail

	if shieldedAvail >= reserves {
		shieldedAvail -= reserves
		reserves = 0
	} else {
		reserves -= shieldedAvail
		shieldedAvail = 0
	}

	if shieldedAvail == 0 {
		err = codedError(errFunding, err)
		return
	}

	shieldedSplitNeeded = dexzec.RequiredOrderFunds(val, 1, dexbtc.RedeemP2PKHInputSize, maxSwapCount)

	// If we don't have any utxos see if a straight shielded split will get us there.
	if len(utxos) == 0 {
		// Can we do it with just the shielded balance?
		shieldedSplitFees = dexzec.TxFeesZIP317(0, dexbtc.P2SHOutputSize+1, 0, 0, 0, nActionsOrchard)
		req := val + shieldedSplitFees
		if shieldedAvail < req {
			// err is still the error from the last call to FundWithUTXOs
			err = codedError(errShieldedFunding, err)
		} else {
			err = nil
		}
		return
	}

	// Check with both transparent and orchard funds. (shieldedAvail has been
	// set, which changes the behavior of enough.
	coins, fundingCoins, spents, redeemScripts, size, sum, err = w.cm.FundWithUTXOs(utxos, reserves, false, enough)
	if err != nil {
		err = codedError(errFunding, err)
		return
	}

	req := dexzec.RequiredOrderFunds(val, uint64(len(coins)), size, maxSwapCount)
	if req > sum {
		txOutsSize := uint64(dexbtc.P2PKHOutputSize + 1) // 1 for varint
		shieldedSplitFees = dexzec.TxFeesZIP317(size+uint64(wire.VarIntSerializeSize(uint64(len(coins)))), txOutsSize, 0, 0, 0, nActionsOrchard)
	}

	if shieldedAvail+sum < shieldedSplitNeeded+shieldedSplitFees {
		err = newError(errInsufficientBalance, "not enough to cover requested funds. "+
			"%d available in %d UTXOs, %d available from shielded (after bond reserves), total avail = %d, total needed = %d",
			sum, len(coins), shieldedAvail, shieldedAvail+sum, shieldedSplitNeeded+shieldedSplitFees)
		return
	}

	return
}

func (w *zecWallet) SetBondReserves(reserves uint64) {
	w.reserves.Store(reserves)
}

func (w *zecWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, false /* segwit */, w.btcParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// If no tx data is provided, attempt to get the required data (the txOut)
	// from the wallet. If this is a full node wallet, a simple gettxout RPC is
	// sufficient with no pkScript or "since" time. If this is an SPV wallet,
	// only a confirmed counterparty contract can be located, and only one
	// within ContractSearchLimit. As such, this mode of operation is not
	// intended for normal server-coordinated operation.
	var tx *dexzec.Tx
	var txOut *wire.TxOut
	if len(txData) == 0 {
		// Fall back to gettxout, but we won't have the tx to rebroadcast.
		txOut, _, err = getTxOut(w, txHash, vout)
		if err != nil || txOut == nil {
			return nil, fmt.Errorf("error finding unspent contract: %s:%d : %w", txHash, vout, err)
		}
	} else {
		tx, err = dexzec.DeserializeTx(txData)
		if err != nil {
			return nil, fmt.Errorf("coin not found, and error encountered decoding tx data: %v", err)
		}
		if len(tx.TxOut) <= int(vout) {
			return nil, fmt.Errorf("specified output %d not found in decoded tx %s", vout, txHash)
		}
		txOut = tx.TxOut[vout]
	}

	// Check for standard P2SH. NOTE: btc.scriptHashScript(contract) should
	// equal txOut.PkScript. All we really get from the TxOut is the *value*.
	scriptClass, addrs, numReq, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, w.btcParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting script addresses from '%x': %w", txOut.PkScript, err)
	}
	if scriptClass != txscript.ScriptHashTy {
		return nil, fmt.Errorf("unexpected script class. expected %s, got %s", txscript.ScriptHashTy, scriptClass)
	}
	// Compare the contract hash to the P2SH address.
	contractHash := btcutil.Hash160(contract)
	// These last two checks are probably overkill.
	if numReq != 1 {
		return nil, fmt.Errorf("unexpected number of signatures expected for P2SH script: %d", numReq)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}

	addr := addrs[0]
	if !bytes.Equal(contractHash, addr.ScriptAddress()) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addr.ScriptAddress())
	}

	// Broadcast the transaction, but do not block because this is not required
	// and does not affect the audit result.
	if rebroadcast && tx != nil {
		go func() {
			if hashSent, err := sendRawTransaction(w, tx); err != nil {
				w.log.Debugf("Rebroadcasting counterparty contract %v (THIS MAY BE NORMAL): %v", txHash, err)
			} else if !hashSent.IsEqual(txHash) {
				w.log.Errorf("Counterparty contract %v was rebroadcast as %v!", txHash, hashSent)
			}
		}()
	}

	addrStr, err := dexzec.EncodeAddress(receiver, w.addrParams)
	if err != nil {
		w.log.Errorf("Failed to stringify receiver address %v (default): %v", receiver, err)
	}

	return &asset.AuditInfo{
		Coin:       btc.NewOutput(txHash, vout, uint64(txOut.Value)),
		Recipient:  addrStr,
		Contract:   contract,
		SecretHash: secretHash,
		Expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

func (w *zecWallet) ConfirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx, feeSuggestion uint64) (*asset.ConfirmTxStatus, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	tx, err := getWalletTransaction(w, txHash)
	// transaction found, return its confirms.
	//
	// TODO: Investigate the case where this tx has been sitting in the
	// mempool for a long amount of time, possibly requiring some action by
	// us to get it unstuck.
	if err == nil {
		if tx.Confirmations < 0 {
			tx.Confirmations = 0
		}
		return &asset.ConfirmTxStatus{
			Confs:  uint64(tx.Confirmations),
			Req:    requiredConfTxConfirms,
			CoinID: coinID,
		}, nil
	}

	// Transaction is missing from the point of view of our node!
	// Unlikely, but possible it was redeemed by another transaction. Check
	// if the contract is still an unspent output.

	swapHash, vout, err := decodeCoinID(confirmTx.SpendsCoinID())
	if err != nil {
		return nil, err
	}

	utxo, _, err := getTxOut(w, swapHash, vout)
	if err != nil {
		return nil, newError(errNoTx, "error finding unspent contract %s with swap hash %v vout %d: %w", confirmTx.SpendsCoinID(), swapHash, vout, err)
	}
	if utxo == nil {
		// TODO: Spent, but by who. Find the spending tx.
		w.log.Warnf("Contract coin %v with swap hash %v vout %d spent by someone but not sure who.", confirmTx.SpendsCoinID(), swapHash, vout)
		// Incorrect, but we will be in a loop of erroring if we don't
		// return something.
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
		_, coin, _, err := w.Redeem(form)
		if err != nil {
			return nil, fmt.Errorf("unable to re-redeem %s with swap hash %v vout %d: %w", confirmTx.SpendsCoinID(), swapHash, vout, err)
		}
		newCoinID = coin.ID()
	} else {
		spendsCoinID := confirmTx.SpendsCoinID()
		newCoinID, err = w.Refund(spendsCoinID, confirmTx.Contract(), feeSuggestion)
		if err != nil {
			return nil, fmt.Errorf("unable to re-refund %s: %w", spendsCoinID, err)
		}
	}

	return &asset.ConfirmTxStatus{
		Confs:  0,
		Req:    requiredConfTxConfirms,
		CoinID: newCoinID,
	}, nil
}

func (w *zecWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexbtc.ExtractSwapDetails(contract, false /* segwit */, w.btcParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %w", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	expired, err := w.LockTimeExpired(ctx, contractExpiry)
	if err != nil {
		return false, time.Time{}, err
	}
	return expired, contractExpiry, nil
}

type depositAddressJSON struct {
	Unified     string `json:"unified"`
	Transparent string `json:"transparent"`
	Orchard     string `json:"orchard"`
	Sapling     string `json:"sapling,omitempty"`
}

func (w *zecWallet) DepositAddress() (string, error) {
	addrRes, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return "", err
	}
	receivers, err := zGetUnifiedReceivers(w, addrRes.Address)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(&depositAddressJSON{
		Unified:     addrRes.Address,
		Transparent: receivers.Transparent,
		Orchard:     receivers.Orchard,
		Sapling:     receivers.Sapling,
	})
	if err != nil {
		return "", fmt.Errorf("error encoding address JSON: %w", err)
	}
	return depositAddrPrefix + string(b), nil

}

func (w *zecWallet) NewAddress() (string, error) {
	return w.DepositAddress()
}

// AddressUsed checks if a wallet address has been used.
func (w *zecWallet) AddressUsed(addrStr string) (bool, error) {
	// TODO: Resolve with new unified address encoding in https://github.com/decred/dcrdex/pull/2675
	recv, err := getReceivedByAddress(w, addrStr)
	return recv != 0, err
}

func (w *zecWallet) FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	return w.rf.FindRedemption(ctx, coinID)
}

func (w *zecWallet) FundMultiOrder(mo *asset.MultiOrder, maxLock uint64) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingFees uint64, err error) {
	w.log.Debugf("Attempting to fund a multi-order for ZEC")

	var totalRequiredForOrders uint64
	var swapInputSize uint64 = dexbtc.RedeemP2PKHInputSize
	for _, v := range mo.Values {
		if v.Value == 0 {
			return nil, nil, 0, newError(errBadInput, "cannot fund value = 0")
		}
		if v.MaxSwapCount == 0 {
			return nil, nil, 0, fmt.Errorf("cannot fund zero-lot order")
		}
		req := dexzec.RequiredOrderFunds(v.Value, 1, swapInputSize, v.MaxSwapCount)
		totalRequiredForOrders += req
	}

	if maxLock < totalRequiredForOrders && maxLock != 0 {
		return nil, nil, 0, newError(errMaxLock, "maxLock < totalRequiredForOrders (%d < %d)", maxLock, totalRequiredForOrders)
	}

	bal, err := w.Balance()
	if err != nil {
		return nil, nil, 0, newError(errFunding, "error getting wallet balance: %w", err)
	}
	if bal.Available < totalRequiredForOrders {
		return nil, nil, 0, newError(errInsufficientBalance, "insufficient funds. %d < %d", bal.Available, totalRequiredForOrders)
	}

	reserves := w.reserves.Load()

	const multiSplitAllowed = true

	coins, redeemScripts, fundingCoins, spents, err := w.cm.FundMultiBestEffort(reserves, maxLock, mo.Values, mo.MaxFeeRate, multiSplitAllowed)
	if err != nil {
		return nil, nil, 0, codedError(errFunding, err)
	}
	if len(coins) == len(mo.Values) {
		w.cm.LockOutputsMap(fundingCoins)
		lockUnspent(w, false, spents)
		return coins, redeemScripts, 0, nil
	}

	recips := make([]*zSendManyRecipient, len(mo.Values))
	addrs := make([]string, len(mo.Values))
	orderReqs := make([]uint64, len(mo.Values))
	var txWasBroadcast bool
	defer func() {
		if txWasBroadcast || len(addrs) == 0 {
			return
		}
		w.ar.ReturnAddresses(addrs)
	}()
	for i, v := range mo.Values {
		addr, err := w.recyclableAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error getting address for split tx: %v", err)
		}
		orderReqs[i] = dexzec.RequiredOrderFunds(v.Value, 1, dexbtc.RedeemP2PKHInputSize, v.MaxSwapCount)
		addrs[i] = addr
		recips[i] = &zSendManyRecipient{Address: addr, Amount: btcutil.Amount(orderReqs[i]).ToBTC()}
	}

	txHash, err := w.sendManyShielded(recips)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("sendManyShielded error: %w", err)
	}

	tx, err := getTransaction(w, txHash)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error retreiving split transaction %s: %w", txHash, err)
	}

	txWasBroadcast = true

	fundingFees = tx.RequiredTxFeesZIP317()

	txOuts := make(map[uint32]*wire.TxOut, len(mo.Values))
	for vout, txOut := range tx.TxOut {
		txOuts[uint32(vout)] = txOut
	}

	coins = make([]asset.Coins, len(mo.Values))
	utxos := make([]*btc.UTxO, len(mo.Values))
	ops := make([]*btc.Output, len(mo.Values))
	redeemScripts = make([][]dex.Bytes, len(mo.Values))
next:
	for i, v := range mo.Values {
		orderReq := orderReqs[i]
		for vout, txOut := range txOuts {
			if uint64(txOut.Value) == orderReq {
				_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, w.btcParams)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("error extracting addresses error: %w", err)
				}
				if len(addrs) != 1 {
					return nil, nil, 0, fmt.Errorf("unexpected multi-sig (%d)", len(addrs))
				}
				addr := addrs[0]
				addrStr, err := dexzec.EncodeAddress(addr, w.addrParams)
				if err != nil {
					return nil, nil, 0, fmt.Errorf("error encoding Zcash transparent address: %w", err)
				}
				utxos[i] = &btc.UTxO{
					TxHash:  txHash,
					Vout:    vout,
					Address: addrStr,
					Amount:  orderReq,
				}
				ops[i] = btc.NewOutput(txHash, vout, orderReq)
				coins[i] = asset.Coins{ops[i]}
				redeemScripts[i] = []dex.Bytes{nil}
				delete(txOuts, vout)
				continue next
			}
		}
		return nil, nil, 0, fmt.Errorf("failed to find output coin for multisplit value %s at index %d", btcutil.Amount(v.Value), i)
	}
	w.cm.LockUTXOs(utxos)
	if err := lockUnspent(w, false, ops); err != nil {
		return nil, nil, 0, fmt.Errorf("error locking unspents: %w", err)
	}

	return coins, redeemScripts, fundingFees, nil
}

func (w *zecWallet) sendManyShielded(recips []*zSendManyRecipient) (*chainhash.Hash, error) {
	lastAddr, err := w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}

	operationID, err := zSendMany(w, lastAddr, recips, NoPrivacy)
	if err != nil {
		return nil, fmt.Errorf("z_sendmany error: %w", err)
	}

	return w.awaitSendManyOperation(w.ctx, w, operationID)
}

func (w *zecWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

func (w *zecWallet) LockTimeExpired(_ context.Context, lockTime time.Time) (bool, error) {
	chainStamper := func(blockHash *chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error) {
		hdr, err := getBlockHeader(w, blockHash)
		if err != nil {
			return
		}
		return hdr.Timestamp, &hdr.PrevBlock, nil
	}

	w.tipMtx.RLock()
	tip := w.currentTip
	w.tipMtx.RUnlock()

	medianTime, err := btc.CalcMedianTime(chainStamper, &tip.Hash) // TODO: pass ctx
	if err != nil {
		return false, fmt.Errorf("error getting median time: %w", err)
	}
	return medianTime.After(lockTime), nil
}

// fundMultiOptions are the possible order options when calling FundMultiOrder.
type fundMultiOptions struct {
	// Split, if true, and multi-order cannot be funded with the existing UTXOs
	// in the wallet without going over the maxLock limit, a split transaction
	// will be created with one output per order.
	//
	// Use the multiSplitKey const defined above in the options map to set this option.
	Split bool `ini:"multisplit"`
}

func decodeFundMultiSettings(settings map[string]string) (*fundMultiOptions, error) {
	opts := new(fundMultiOptions)
	return opts, config.Unmapify(settings, opts)
}

func (w *zecWallet) MaxFundingFees(numTrades uint32, feeRate uint64, settings map[string]string) uint64 {
	customCfg, err := decodeFundMultiSettings(settings)
	if err != nil {
		w.log.Errorf("Error decoding multi-fund settings: %v", err)
		return 0
	}

	// Assume a split from shielded
	txOutsSize := uint64(numTrades*dexbtc.P2PKHOutputSize + 1) // 1 for varint
	shieldedSplitFees := dexzec.TxFeesZIP317(0, txOutsSize, 0, 0, 0, nActionsOrchardEstimate)

	if !customCfg.Split {
		return shieldedSplitFees
	}

	return shieldedSplitFees + dexzec.TxFeesZIP317(1, uint64(numTrades+1)*dexbtc.P2PKHOutputSize, 0, 0, 0, 0)
}

func (w *zecWallet) OwnsDepositAddress(addrStr string) (bool, error) {
	if strings.HasPrefix(addrStr, depositAddrPrefix) {
		var addrs depositAddressJSON
		if err := json.Unmarshal([]byte(addrStr[len(depositAddrPrefix):]), &addrs); err != nil {
			return false, fmt.Errorf("error decoding unified address info: %w", err)
		}
		addrStr = addrs.Unified
	}
	res, err := zValidateAddress(w, addrStr)
	if err != nil {
		return false, fmt.Errorf("error validating address: %w", err)
	}
	return res.IsMine, nil
}

func (w *zecWallet) RedemptionAddress() (string, error) {
	return w.recyclableAddress()
}

// A recyclable address is a redemption or refund address that may be recycled
// if unused. If already recycled addresses are available, one will be returned.
func (w *zecWallet) recyclableAddress() (string, error) {
	var returns []string
	defer w.ar.ReturnAddresses(returns)
	for {
		addr := w.ar.Address()
		if addr == "" {
			break
		}
		if owns, err := w.OwnsDepositAddress(addr); owns {
			return addr, nil
		} else if err != nil {
			w.log.Errorf("Error checking ownership of recycled address %q: %v", addr, err)
			returns = append(returns, addr)
		}
	}
	return transparentAddressString(w)
}

func (w *zecWallet) Refund(coinID, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	// TODO: I'd recommend not passing a pkScript without a limited startTime
	// to prevent potentially long searches. In this case though, the output
	// will be found in the wallet and won't need to be searched for, only
	// the spender search will be conducted using the pkScript starting from
	// the block containing the original tx. The script can be gotten from
	// the wallet tx though and used for the spender search, while not passing
	// a script here to ensure no attempt is made to find the output without
	// a limited startTime.
	utxo, _, err := getTxOut(w, txHash, vout)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %w", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError // spent
	}
	msgTx, err := w.refundTx(txHash, vout, contract, uint64(utxo.Value), nil, feeRate)
	if err != nil {
		return nil, fmt.Errorf("error creating refund tx: %w", err)
	}

	refundHash, err := w.broadcastTx(dexzec.NewTxFromMsgTx(msgTx, dexzec.MaxExpiryHeight))
	if err != nil {
		return nil, fmt.Errorf("broadcastTx: %w", err)
	}

	tx := zecTx(msgTx)
	w.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Refund,
		ID:     refundHash.String(),
		Amount: uint64(utxo.Value),
		Fees:   tx.RequiredTxFeesZIP317(),
	}, refundHash, true)

	return btc.ToCoinID(refundHash, 0), nil
}

func (w *zecWallet) broadcastTx(tx *dexzec.Tx) (*chainhash.Hash, error) {
	rawTx := func() string {
		b, err := tx.Bytes()
		if err != nil {
			return "serialization error: " + err.Error()
		}
		return dex.Bytes(b).String()
	}
	txHash, err := sendRawTransaction(w, tx)
	if err != nil {
		return nil, fmt.Errorf("sendrawtx error: %v: %s", err, rawTx())
	}
	checkHash := tx.TxHash()
	if *txHash != checkHash {
		return nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s. raw tx: %s", checkHash, txHash, rawTx())
	}
	return txHash, nil
}

func (w *zecWallet) refundTx(txHash *chainhash.Hash, vout uint32, contract dex.Bytes, val uint64, refundAddr btcutil.Address, fees uint64) (*wire.MsgTx, error) {
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(contract, false, w.btcParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %w", err)
	}

	// Create the transaction that spends the contract.
	msgTx := wire.NewMsgTx(dexzec.VersionNU5)
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	// Enable the OP_CHECKLOCKTIMEVERIFY opcode to be used.
	//
	// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.

	if refundAddr == nil {
		refundAddr, err = transparentAddress(w, w.addrParams, w.btcParams)
		if err != nil {
			return nil, fmt.Errorf("error getting new address from the wallet: %w", err)
		}
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating change script: %w", err)
	}
	txOut := wire.NewTxOut(int64(val-fees), pkScript)
	// One last check for dust.
	if isDust(uint64(txOut.Value), uint64(txOut.SerializeSize())) {
		return nil, fmt.Errorf("refund output is dust. value = %d, size = %d", txOut.Value, txOut.SerializeSize())
	}
	msgTx.AddTxOut(txOut)

	prevScript, err := w.scriptHashScript(contract)
	if err != nil {
		return nil, fmt.Errorf("error constructing p2sh script: %w", err)
	}

	refundSig, refundPubKey, err := w.createSig(msgTx, 0, contract, sender, []int64{int64(val)}, [][]byte{prevScript})
	if err != nil {
		return nil, fmt.Errorf("createSig: %w", err)
	}
	txIn.SignatureScript, err = dexbtc.RefundP2SHContract(contract, refundSig, refundPubKey)
	if err != nil {
		return nil, fmt.Errorf("RefundP2SHContract: %w", err)
	}
	return msgTx, nil
}

func (w *zecWallet) createSig(msgTx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address, vals []int64, prevScripts [][]byte) (sig, pubkey []byte, err error) {
	addrStr, err := dexzec.EncodeAddress(addr, w.addrParams)
	if err != nil {
		return nil, nil, fmt.Errorf("error encoding address: %w", err)
	}
	privKey, err := dumpPrivKey(w, addrStr)
	if err != nil {
		return nil, nil, fmt.Errorf("dumpPrivKey error: %w", err)
	}
	defer privKey.Zero()

	sig, err = signTx(msgTx, idx, pkScript, txscript.SigHashAll, privKey, vals, prevScripts)
	if err != nil {
		return nil, nil, err
	}

	return sig, privKey.PubKey().SerializeCompressed(), nil
}

func (w *zecWallet) RegFeeConfirmations(_ context.Context, id dex.Bytes) (confs uint32, err error) {
	return 0, errors.New("legacy registration not supported")
}

func sendEnough(val uint64) btc.EnoughFunc {
	return func(inputCount, inputsSize, sum uint64) (bool, uint64) {
		fees := dexzec.TxFeesZIP317(inputCount*dexbtc.RedeemP2PKHInputSize+1, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
		total := fees + val
		if sum >= total {
			return true, sum - total
		}
		return false, 0
	}
}

// fundOrchard checks whether a send of the specified amount can be funded from
// the Orchard pool alone.
func (w *zecWallet) fundOrchard(amt uint64) (sum, fees uint64, n int, funded bool, err error) {
	unfiltered, err := zListUnspent(w)
	if err != nil {
		return 0, 0, 0, false, err
	}
	unspents := make([]*zListUnspentResult, 0, len(unfiltered))
	for _, u := range unfiltered {
		if u.Account == shieldedAcctNumber && u.Confirmations >= minOrchardConfs {
			unspents = append(unspents, u)
		}
	}
	sort.Slice(unspents, func(i, j int) bool {
		return unspents[i].Amount < unspents[j].Amount
	})
	var u *zListUnspentResult
	for n, u = range unspents {
		sum += toZats(u.Amount)
		fees = dexzec.TxFeesZIP317(0, 0, 0, 0, 0, uint64(n))
		if sum > amt+fees {
			funded = true
			return
		}
	}
	return
}

func (w *zecWallet) EstimateSendTxFee(
	addrStr string, amt, _ /* feeRate*/ uint64, _ /* subtract */, maxWithdraw bool,
) (
	fees uint64, isValidAddress bool, err error,
) {

	res, err := zValidateAddress(w, addrStr)
	isValidAddress = err == nil && res.IsValid

	if maxWithdraw {
		addrType := transparentAddressType
		if isValidAddress {
			addrType = res.AddressType
		}
		fees, err = w.maxWithdrawFees(addrType)
		return
	}

	orchardSum, orchardFees, orchardN, orchardFunded, err := w.fundOrchard(amt)
	if err != nil {
		return 0, false, fmt.Errorf("error checking orchard funding: %w", err)
	}
	if orchardFunded {
		return orchardFees, isValidAddress, nil
	}

	bal, err := zGetBalanceForAccount(w, shieldedAcctNumber, 0)
	if err != nil {
		return 0, false, fmt.Errorf("z_getbalanceforaccount (0) error: %w", err)
	}

	remain := amt - orchardSum - bal.Sapling /* sapling fees not accounted for */

	const minConfs = 0
	_, _, spents, _, inputsSize, _, err := w.cm.Fund(w.reserves.Load(), minConfs, false, sendEnough(remain))
	if err != nil {
		return 0, false, fmt.Errorf("error funding value %d with %d transparent and %d orchard balances: %w",
			amt, bal.Transparent, orchardSum, err)
	}

	fees = dexzec.TxFeesZIP317(inputsSize+uint64(wire.VarIntSerializeSize(uint64(len(spents)))), 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, uint64(orchardN))
	return
}

func (w *zecWallet) maxWithdrawFees(addrType string) (uint64, error) {
	unfiltered, err := listUnspent(w)
	if err != nil {
		return 0, fmt.Errorf("listunspent error: %w", err)
	}
	numInputs := uint64(len(unfiltered))
	noteCounts, err := zGetNotesCount(w)
	if err != nil {
		return 0, fmt.Errorf("balances error: %w", err)
	}

	txInsSize := numInputs*dexbtc.RedeemP2PKHInputSize + uint64(wire.VarIntSerializeSize(numInputs))
	nActionsOrchard := uint64(noteCounts.Orchard)
	var txOutsSize, nOutputsSapling uint64
	switch addrType {
	case unifiedAddressType, orchardAddressType:
		nActionsOrchard++
	case saplingAddressType:
		nOutputsSapling = 1
	default: // transparent
		txOutsSize = dexbtc.P2PKHOutputSize + 1
	}
	// TODO: Do we get nJoinSplit from noteCounts.Sprout somehow?
	return dexzec.TxFeesZIP317(txInsSize, txOutsSize, uint64(noteCounts.Sapling), nOutputsSapling, 0, nActionsOrchard), nil
}

type txCoin struct {
	txHash *chainhash.Hash
	v      uint64
}

var _ asset.Coin = (*txCoin)(nil)

// ID is a unique identifier for this coin.
func (c *txCoin) ID() dex.Bytes {
	return c.txHash[:]
}

// String is a string representation of the coin.
func (c *txCoin) String() string {
	return c.txHash.String()
}

// Value is the available quantity, in atoms/satoshi.
func (c *txCoin) Value() uint64 {
	return c.v
}

// TxID is the ID of the transaction that created the coin.
func (c *txCoin) TxID() string {
	return c.txHash.String()
}

func (w *zecWallet) Send(addr string, value, feeRate uint64) (asset.Coin, error) {
	txHash, err := w.sendShielded(w.ctx, addr, value)
	if err != nil {
		return nil, err
	}

	selfSend, err := w.OwnsDepositAddress(addr)
	if err != nil {
		w.log.Errorf("error checking if address %q is owned: %v", addr, err)
	}
	txType := asset.Send
	if selfSend {
		txType = asset.SelfSend
	}

	tx, err := getTransaction(w, txHash)
	if err != nil {
		return nil, fmt.Errorf("unable to find tx after send %s: %v", txHash, err)
	}

	w.addTxToHistory(&asset.WalletTransaction{
		Type:   txType,
		ID:     txHash.String(),
		Amount: value,
		Fees:   tx.RequiredTxFeesZIP317(),
	}, txHash, true)

	return &txCoin{
		txHash: txHash,
		v:      value,
	}, nil
}

// StandardSendFee returns the fees for a simple send tx with one input and two
// outputs.
func (w *zecWallet) StandardSendFee(feeRate uint64) uint64 {
	return dexzec.TxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
}

// TransactionConfirmations gets the number of confirmations for the specified
// transaction.
func (w *zecWallet) TransactionConfirmations(ctx context.Context, txID string) (confs uint32, err error) {
	txHash, err := chainhash.NewHashFromStr(txID)
	if err != nil {
		return 0, fmt.Errorf("error decoding txid %q: %w", txID, err)
	}
	tx, err := getWalletTransaction(w, txHash)
	if err != nil {
		return 0, err
	}
	if tx.Confirmations < 0 {
		tx.Confirmations = 0
	}

	return uint32(tx.Confirmations), nil
}

// send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of sats/byte.
func (w *zecWallet) send(addrStr string, val uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	addr, err := dexzec.DecodeAddress(addrStr, w.addrParams, w.btcParams)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("invalid address: %s", addrStr)
	}
	pay2script, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("PayToAddrScript error: %w", err)
	}

	const minConfs = 0
	_, _, spents, _, inputsSize, _, err := w.cm.Fund(w.reserves.Load(), minConfs, false, sendEnough(val))
	if err != nil {
		return nil, 0, 0, newError(errFunding, "error funding transaction: %w", err)
	}

	fundedTx, totalIn, _, err := w.fundedTx(spents)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error adding inputs to transaction: %w", err)
	}

	fees := dexzec.TxFeesZIP317(inputsSize, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
	var toSend uint64
	if subtract {
		toSend = val - fees
	} else {
		toSend = val
	}
	fundedTx.AddTxOut(wire.NewTxOut(int64(toSend), pay2script))

	changeAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error creating change address: %w", err)
	}

	signedTx, _, err := w.signTxAndAddChange(fundedTx, changeAddr, totalIn, val, fees)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("signTxAndAddChange error: %v", err)
	}

	txHash, err := w.broadcastTx(signedTx)
	if err != nil {
		return nil, 0, 0, err
	}

	return txHash, 0, toSend, nil
}

func (w *zecWallet) sendWithReturn(baseTx *dexzec.Tx, addr btcutil.Address, totalIn, totalOut uint64) (*dexzec.Tx, error) {
	txInsSize := uint64(len(baseTx.TxIn))*dexbtc.RedeemP2PKHInputSize + 1
	var txOutsSize uint64 = uint64(wire.VarIntSerializeSize(uint64(len(baseTx.TxOut) + 1)))
	for _, txOut := range baseTx.TxOut {
		txOutsSize += uint64(txOut.SerializeSize())
	}

	fees := dexzec.TxFeesZIP317(txInsSize, txOutsSize, 0, 0, 0, 0)

	signedTx, _, err := w.signTxAndAddChange(baseTx, addr, totalIn, totalOut, fees)
	if err != nil {
		return nil, err
	}

	_, err = w.broadcastTx(signedTx)
	return signedTx, err
}

func (w *zecWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {

	op, err := btc.ConvertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %w", err)
	}
	utxo := w.cm.LockedOutput(op.Pt)

	if utxo == nil {
		return nil, nil, fmt.Errorf("no utxo found for %s", op)
	}
	privKey, err := dumpPrivKey(w, utxo.Address)
	if err != nil {
		return nil, nil, err
	}
	defer privKey.Zero()
	pk := privKey.PubKey()
	hash := chainhash.HashB(msg) // legacy servers will not accept this signature!
	sig := ecdsa.Sign(privKey, hash)
	pubkeys = append(pubkeys, pk.SerializeCompressed())
	sigs = append(sigs, sig.Serialize()) // DER format serialization
	return
}

func (w *zecWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	contracts := make([][]byte, 0, len(swaps.Contracts))
	var totalOut uint64
	// Start with an empty MsgTx.
	coins := make([]*btc.Output, len(swaps.Inputs))
	for i, coin := range swaps.Inputs {
		c, err := btc.ConvertCoin(coin)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error converting coin ID: %w", err)
		}
		coins[i] = c
	}
	baseTx, totalIn, pts, err := w.fundedTx(coins)
	if err != nil {
		return nil, nil, 0, err
	}

	var customCfg swapOptions
	err = config.Unmapify(swaps.Options, &customCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error parsing swap options: %w", err)
	}

	refundAddrs := make([]btcutil.Address, 0, len(swaps.Contracts))

	// Add the contract outputs.
	// TODO: Make P2WSH contract and P2WPKH change outputs instead of
	// legacy/non-segwit swap contracts pkScripts.
	var swapOutsSize uint64
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address belonging to the key that may be used to
		// sign and refund a swap past its encoded refund locktime.
		revokeAddrStr, err := w.recyclableAddress()
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating revocation address: %w", err)
		}
		revokeAddr, err := dexzec.DecodeAddress(revokeAddrStr, w.addrParams, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("refund address decode error: %v", err)
		}
		refundAddrs = append(refundAddrs, revokeAddr)

		contractAddr, err := dexzec.DecodeAddress(contract.Address, w.addrParams, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("contract address decode error: %v", err)
		}

		// Create the contract, a P2SH redeem script.
		contractScript, err := dexbtc.MakeContract(contractAddr, revokeAddr,
			contract.SecretHash, int64(contract.LockTime), false, w.btcParams)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("unable to create pubkey script for address %s: %w", contract.Address, err)
		}
		contracts = append(contracts, contractScript)

		// Make the P2SH address and pubkey script.
		scriptAddr, err := w.scriptHashAddress(contractScript)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error encoding script address: %w", err)
		}

		pkScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating pubkey script: %w", err)
		}

		// Add the transaction output.
		txOut := wire.NewTxOut(int64(contract.Value), pkScript)
		swapOutsSize += uint64(txOut.SerializeSize())
		baseTx.AddTxOut(txOut)
	}
	if totalIn < totalOut {
		return nil, nil, 0, newError(errInsufficientBalance, "unfunded contract. %d < %d", totalIn, totalOut)
	}

	// Ensure we have enough outputs before broadcasting.
	swapCount := len(swaps.Contracts)
	if len(baseTx.TxOut) < swapCount {
		return nil, nil, 0, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}

	// Grab a change address.
	changeAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error creating change address: %w", err)
	}

	txInsSize := uint64(len(coins))*dexbtc.RedeemP2PKHInputSize + 1
	txOutsSize := swapOutsSize + dexbtc.P2PKHOutputSize
	fees := dexzec.TxFeesZIP317(txInsSize, txOutsSize, 0, 0, 0, 0)

	// Sign, add change, but don't send the transaction yet until
	// the individual swap refund txs are prepared and signed.
	msgTx, change, err := w.signTxAndAddChange(baseTx, changeAddr, totalIn, totalOut, fees)
	if err != nil {
		return nil, nil, 0, err
	}
	txHash := msgTx.TxHash()

	// Prepare the receipts.
	receipts := make([]asset.Receipt, 0, swapCount)
	for i, contract := range swaps.Contracts {
		output := btc.NewOutput(&txHash, uint32(i), contract.Value)
		refundAddr := refundAddrs[i]
		signedRefundTx, err := w.refundTx(&output.Pt.TxHash, output.Pt.Vout, contracts[i], contract.Value, refundAddr, fees)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating refund tx: %w", err)
		}
		refundBuff := new(bytes.Buffer)
		err = signedRefundTx.Serialize(refundBuff)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error serializing refund tx: %w", err)
		}
		receipts = append(receipts, &btc.SwapReceipt{
			Output:            output,
			SwapContract:      contracts[i],
			ExpirationTime:    time.Unix(int64(contract.LockTime), 0).UTC(),
			SignedRefundBytes: refundBuff.Bytes(),
		})
	}

	// Refund txs prepared and signed. Can now broadcast the swap(s).
	_, err = w.broadcastTx(msgTx)
	if err != nil {
		return nil, nil, 0, err
	}

	w.addTxToHistory(&asset.WalletTransaction{
		Type:   asset.Swap,
		ID:     txHash.String(),
		Amount: totalOut,
		Fees:   msgTx.RequiredTxFeesZIP317(),
	}, &txHash, true)

	// If change is nil, return a nil asset.Coin.
	var changeCoin asset.Coin
	if change != nil {
		changeCoin = change
	}

	if swaps.LockChange && change != nil {
		// Lock the change output
		w.log.Debugf("locking change coin %s", change)
		err = lockUnspent(w, false, []*btc.Output{change})
		if err != nil {
			// The swap transaction is already broadcasted, so don't fail now.
			w.log.Errorf("failed to lock change output: %v", err)
		}

		addrStr, err := dexzec.EncodeAddress(changeAddr, w.addrParams)
		if err != nil {
			w.log.Errorf("Failed to stringify address %v (default encoding): %v", changeAddr, err)
			addrStr = changeAddr.String() // may or may not be able to retrieve the private keys for the next swap!
		}
		w.cm.LockUTXOs([]*btc.UTxO{{
			TxHash:  &change.Pt.TxHash,
			Vout:    change.Pt.Vout,
			Address: addrStr,
			Amount:  change.Val,
		}})
	}

	w.cm.UnlockOutPoints(pts)

	return receipts, changeCoin, fees, nil
}

func (w *zecWallet) SwapConfirmations(_ context.Context, id dex.Bytes, contract dex.Bytes, startTime time.Time) (uint32, bool, error) {
	txHash, vout, err := decodeCoinID(id)
	if err != nil {
		return 0, false, err
	}
	// Check for an unspent output.
	txOut, err := getTxOutput(w, txHash, vout)
	if err == nil && txOut != nil {
		return uint32(txOut.Confirmations), false, nil
	}
	// Check wallet transactions.
	tx, err := getWalletTransaction(w, txHash)
	if err != nil {
		if errors.Is(err, asset.CoinNotFoundError) {
			return 0, false, asset.CoinNotFoundError
		}
		return 0, false, newError(errNoTx, "gettransaction error; %w", err)
	}
	if tx.Confirmations < 0 {
		tx.Confirmations = 0
	}
	return uint32(tx.Confirmations), true, nil
}

func (w *zecWallet) SyncStatus() (*asset.SyncStatus, error) {
	ss, err := syncStatus(w)
	if err != nil {
		return nil, err
	}
	ss.StartingBlocks = w.tipAtConnect.Load()

	if ss.TargetHeight == 0 { // do not say progress = 1
		return &asset.SyncStatus{}, nil
	}
	if ss.Synced {
		numPeers, err := peerCount(w)
		if err != nil {
			return nil, err
		}
		ss.Synced = numPeers > 0
	}
	return ss, nil
}

func (w *zecWallet) ValidateAddress(addr string) bool {
	res, err := zValidateAddress(w, addr)
	if err != nil {
		w.log.Errorf("z_validateaddress error for address %q: %w", addr, err)
		return false
	}
	return res.IsValid
}

func (w *zecWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

func (w *zecWallet) useSplitTx() bool {
	return w.walletCfg.Load().(*WalletConfig).UseSplitTx
}

func transparentAddressString(c rpcCaller) (string, error) {
	// One of the address types MUST be shielded.
	addrRes, err := zGetAddressForAccount(c, shieldedAcctNumber, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return "", err
	}
	receivers, err := zGetUnifiedReceivers(c, addrRes.Address)
	if err != nil {
		return "", err
	}
	return receivers.Transparent, nil
}

func transparentAddress(c rpcCaller, addrParams *dexzec.AddressParams, btcParams *chaincfg.Params) (btcutil.Address, error) {
	addrStr, err := transparentAddressString(c)
	if err != nil {
		return nil, err
	}
	return dexzec.DecodeAddress(addrStr, addrParams, btcParams)
}

func (w *zecWallet) lastShieldedAddress() (addr string, err error) {
	if addrPtr := w.lastAddress.Load(); addrPtr != nil {
		return addrPtr.(string), nil
	}
	accts, err := zListAccounts(w)
	if err != nil {
		return "", err
	}
	for _, acct := range accts {
		if acct.Number != shieldedAcctNumber {
			continue
		}
		if len(acct.Addresses) == 0 {
			break // generate first address
		}
		lastAddr := acct.Addresses[len(acct.Addresses)-1].UnifiedAddr
		w.lastAddress.Store(lastAddr)
		return lastAddr, nil // Orchard = Unified for account 1
	}
	return w.newShieldedAddress()
}

// Balance adds a sum of shielded pool balances to the transparent balance info.
//
// Since v5.4.0, the getnewaddress RPC is deprecated if favor of using unified
// addresses from accounts generated with z_getnewaccount. Addresses are
// generated from account 0. Any addresses previously generated using
// getnewaddress belong to a legacy account that is not listed with
// z_listaccount, nor addressable with e.g. z_getbalanceforaccount. For
// transparent addresses, we still use the getbalance RPC, which combines
// transparent balance from both legacy and generated accounts. This matches the
// behavior of the listunspent RPC, and conveniently makes upgrading simple. So
// even though we ONLY use account 0 to generate t-addresses, any account's
// transparent outputs are eligible for trading. To minimize confusion, we don't
// add transparent receivers to addresses generated from the shielded account.
// This doesn't preclude a user doing something silly with zcash-cli.
func (w *zecWallet) Balance() (*asset.Balance, error) {

	bals, err := w.balances()
	if err != nil {
		return nil, codedError(errBalanceRetrieval, err)
	}
	locked, err := w.lockedZats()
	if err != nil {
		return nil, err
	}

	bal := &asset.Balance{
		Available: bals.orchard.avail + bals.transparent.avail,
		Immature:  bals.orchard.maturing + bals.transparent.maturing,
		Locked:    locked,
		Other:     make(map[asset.BalanceCategory]asset.CustomBalance),
	}

	bal.Other[asset.BalanceCategoryShielded] = asset.CustomBalance{
		Amount: bals.orchard.avail, // + bals.orchard.maturing,
	}

	reserves := w.reserves.Load()
	if reserves > bal.Available {
		w.log.Warnf("Available balance is below configured reserves: %s < %s",
			btcutil.Amount(bal.Available), btcutil.Amount(reserves))
		bal.ReservesDeficit = reserves - bal.Available
		reserves = bal.Available
	}

	bal.BondReserves = reserves
	bal.Available -= reserves
	bal.Locked += reserves

	return bal, nil
}

// lockedSats is the total value of locked outputs, as locked with LockUnspent.
func (w *zecWallet) lockedZats() (uint64, error) {
	lockedOutpoints, err := listLockUnspent(w, w.log)
	if err != nil {
		return 0, err
	}
	var sum uint64
	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return 0, err
		}
		pt := btc.NewOutPoint(txHash, rpcOP.Vout)
		utxo := w.cm.LockedOutput(pt)
		if utxo != nil {
			sum += utxo.Amount
			continue
		}
		tx, err := getWalletTransaction(w, txHash)
		if err != nil {
			return 0, err
		}
		txOut, err := btc.TxOutFromTxBytes(tx.Bytes, rpcOP.Vout, deserializeTx, hashTx)
		if err != nil {
			return 0, err
		}
		sum += uint64(txOut.Value)
	}
	return sum, nil
}

// newShieldedAddress creates a new shielded address.
func (w *zecWallet) newShieldedAddress() (string, error) {
	// An orchard address is the same as a unified address with only an orchard
	// receiver.
	addrRes, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{orchardAddressType})
	if err != nil {
		return "", err
	}
	w.lastAddress.Store(addrRes.Address)
	return addrRes.Address, nil
}

// sendOne is a helper function for doing a z_sendmany with a single recipient.
func (w *zecWallet) sendOne(ctx context.Context, fromAddr, toAddr string, amt uint64, priv privacyPolicy) (*chainhash.Hash, error) {
	recip := singleSendManyRecipient(toAddr, amt)

	operationID, err := zSendMany(w, fromAddr, recip, priv)
	if err != nil {
		return nil, fmt.Errorf("z_sendmany error: %w", err)
	}

	return w.awaitSendManyOperation(ctx, w, operationID)
}

// awaitSendManyOperation waits for the asynchronous result from a z_sendmany
// operation.
func (w *zecWallet) awaitSendManyOperation(ctx context.Context, c rpcCaller, operationID string) (*chainhash.Hash, error) {
	for {
		res, err := zGetOperationResult(c, operationID)
		if err != nil && !errors.Is(err, ErrEmptyOpResults) {
			return nil, fmt.Errorf("error getting operation result: %w", err)
		}
		if res != nil {
			switch res.Status {
			case "failed":
				return nil, fmt.Errorf("z_sendmany operation failed: %s", res.Error.Message)

			case "success":
				if res.Result == nil {
					return nil, errors.New("async operation result = 'success' but no Result field")
				}
				txHash, err := chainhash.NewHashFromStr(res.Result.TxID)
				if err != nil {
					return nil, fmt.Errorf("error decoding txid: %w", err)
				}
				return txHash, nil
			default:
				w.log.Warnf("unexpected z_getoperationresult status %q: %+v", res.Status)
			}

		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (w *zecWallet) sendOneShielded(ctx context.Context, toAddr string, amt uint64, priv privacyPolicy) (*chainhash.Hash, error) {
	lastAddr, err := w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}
	return w.sendOne(ctx, lastAddr, toAddr, amt, priv)
}

func (w *zecWallet) sendShielded(ctx context.Context, toAddr string, amt uint64) (*chainhash.Hash, error) {
	res, err := zValidateAddress(w, toAddr)
	if err != nil {
		return nil, fmt.Errorf("error validating address: %w", err)
	}

	if !res.IsValid {
		return nil, fmt.Errorf("invalid address %q", toAddr)
	}

	// TODO: We're using NoPrivacy for everything except orchard-to-orchard
	// sends. This can potentially be tightened up, but why? The RPC provides
	// no insight into calculating what privacy level is needed, so we would
	// have to do a bunch of calculations regarding sources of funds and
	// potential for change outputs. In the end, it changes nothing, and zcashd
	// will optimize privacy the best it can.
	priv := NoPrivacy
	if res.AddressType == unifiedAddressType {
		r, err := zGetUnifiedReceivers(w, toAddr)
		if err != nil {
			return nil, fmt.Errorf("error getting unified receivers: %w", err)
		}
		if r.Orchard != "" {
			if _, _, _, isFunded, err := w.fundOrchard(amt); err != nil {
				return nil, fmt.Errorf("error checking orchard funding: %w", err)
			} else if isFunded {
				priv = FullPrivacy
			}
		}
	}

	txHash, err := w.sendOneShielded(ctx, toAddr, amt, priv)
	if err != nil {
		return nil, err
	}

	return txHash, nil
}

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// signTx signs the transaction input with Zcash's BLAKE-2B sighash digest.
// Won't work with shielded or blended transactions.
func signTx(btcTx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, amts []int64, prevScripts [][]byte) ([]byte, error) {

	tx := zecTx(btcTx)

	sigHash, err := tx.SignatureDigest(idx, hashType, pkScript, amts, prevScripts)
	if err != nil {
		return nil, fmt.Errorf("sighash calculation error: %v", err)
	}

	return append(ecdsa.Sign(key, sigHash[:]).Serialize(), byte(hashType)), nil
}

// isDust returns true if the output will be rejected as dust.
func isDust(val, outputSize uint64) bool {
	// See https://github.com/zcash/zcash/blob/5066efbb98bc2af5eed201212d27c77993950cee/src/primitives/transaction.h#L630
	// https://github.com/zcash/zcash/blob/5066efbb98bc2af5eed201212d27c77993950cee/src/primitives/transaction.cpp#L127
	// Also see informative comments hinting towards future changes at
	// https://github.com/zcash/zcash/blob/master/src/policy/policy.h
	sz := outputSize + 148                        // 148 accounts for an input on spending tx
	const oneThirdDustThresholdRate = 100         // zats / kB
	nFee := oneThirdDustThresholdRate * sz / 1000 // This is different from BTC
	if nFee == 0 {
		nFee = oneThirdDustThresholdRate
	}

	return val < 3*nFee
}

// Convert the ZEC value to satoshi.
func toZats(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

func hashTx(tx *wire.MsgTx) *chainhash.Hash {
	h := zecTx(tx).TxHash()
	return &h
}

func deserializeTx(b []byte) (*wire.MsgTx, error) {
	tx, err := dexzec.DeserializeTx(b)
	if err != nil {
		return nil, err
	}
	return tx.MsgTx, nil
}

func (w *zecWallet) txDB() *btc.BadgerTxDB {
	db := w.txHistoryDB.Load()
	if db == nil {
		return nil
	}
	return db.(*btc.BadgerTxDB)
}

func (w *zecWallet) listSinceBlock(start int64) ([]btcjson.ListTransactionsResult, error) {
	hash, err := getBlockHash(w, start)
	if err != nil {
		return nil, err
	}

	res, err := listSinceBlock(w, hash)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (w *zecWallet) addTxToHistory(wt *asset.WalletTransaction, txHash *chainhash.Hash, submitted bool, skipNotes ...bool) {
	txHistoryDB := w.txDB()
	if txHistoryDB == nil {
		return
	}

	ewt := &btc.ExtendedWalletTx{
		WalletTransaction: wt,
		Submitted:         submitted,
	}

	if wt.BlockNumber == 0 {
		w.pendingTxsMtx.Lock()
		w.pendingTxs[*txHash] = ewt
		w.pendingTxsMtx.Unlock()
	}

	err := txHistoryDB.StoreTx(ewt)
	if err != nil {
		w.log.Errorf("failed to store tx in tx history db: %v", err)
	}

	skipNote := len(skipNotes) > 0 && skipNotes[0]
	if submitted && !skipNote {
		w.emit.TransactionNote(wt, true)
	}
}

func (w *zecWallet) txHistoryDBPath(walletID string) string {
	return filepath.Join(w.walletDir, fmt.Sprintf("txhistorydb-%s", walletID))
}

// findExistingAddressBasedTxHistoryDB finds the path of a tx history db that
// was created using an address controlled by the wallet. This should only be
// used for RPC wallets, as SPV wallets are able to get the first address
// generated by the wallet.
func (w *zecWallet) findExistingAddressBasedTxHistoryDB(encSeed string) (string, error) {
	dir, err := os.Open(w.walletDir)
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

		name := match[1]
		if name == encSeed {
			return filepath.Join(w.walletDir, entry.Name()), nil
		}
	}

	return "", nil
}

func (w *zecWallet) startTxHistoryDB(ctx context.Context) (*sync.WaitGroup, error) {
	wInfo, err := walletInfo(w)
	if err != nil {
		return nil, err
	}
	encSeed := wInfo.MnemonicSeedfp
	name, err := w.findExistingAddressBasedTxHistoryDB(encSeed)
	if err != nil {
		return nil, err
	}
	dbPath := name

	if dbPath == "" {
		dbPath = w.txHistoryDBPath(encSeed)
	}

	w.log.Debugf("Using tx history db at %s", dbPath)

	db := btc.NewBadgerTxDB(dbPath, w.log)
	w.txHistoryDB.Store(db)

	wg, err := db.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("error connecting to tx history db: %w", err)
	}

	pendingTxs, err := db.GetPendingTxs()
	if err != nil {
		return nil, fmt.Errorf("failed to load unconfirmed txs: %v", err)
	}

	w.pendingTxsMtx.Lock()
	for _, tx := range pendingTxs {
		txHash, err := chainhash.NewHashFromStr(tx.ID)
		if err != nil {
			w.log.Errorf("Invalid txid %v from tx history db: %v", tx.ID, err)
			continue
		}
		w.pendingTxs[*txHash] = tx
	}
	w.pendingTxsMtx.Unlock()

	lastQuery, err := db.GetLastReceiveTxQuery()
	if errors.Is(err, btc.ErrNeverQueried) {
		lastQuery = 0
	} else if err != nil {
		return nil, fmt.Errorf("failed to load last query time: %v", err)
	}

	w.receiveTxLastQuery.Store(lastQuery)

	return wg, nil
}

// WalletTransaction returns a transaction that either the wallet has made or
// one in which the wallet has received funds.
func (w *zecWallet) WalletTransaction(_ context.Context, txID string) (*asset.WalletTransaction, error) {
	coinID, err := hex.DecodeString(txID)
	if err == nil {
		txHash, _, err := decodeCoinID(coinID)
		if err == nil {
			txID = txHash.String()
		}
	}

	txHistoryDB := w.txDB()
	tx, err := txHistoryDB.GetTx(txID)
	if err != nil {
		return nil, err
	}

	if tx == nil {
		txHash, err := chainhash.NewHashFromStr(txID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid %s: %w", txID, err)
		}

		gtr, err := getTransaction(w, txHash)
		if err != nil {
			return nil, fmt.Errorf("error getting transaction %s: %w", txID, err)
		}

		var (
			blockHeight int32
			blockTime   int64
		)
		if gtr.blockHash != nil {
			block, _, err := getVerboseBlockHeader(w, gtr.blockHash)
			if err != nil {
				return nil, fmt.Errorf("error getting block height for %s: %v", gtr.blockHash, err)
			}
			blockHeight = int32(block.Height)
			blockTime = block.Time
		}

		tx, err = w.idUnknownTx(&btcjson.ListTransactionsResult{
			BlockHeight: &blockHeight,
			BlockTime:   blockTime,
			TxID:        txID,
		})
		if err != nil {
			return nil, fmt.Errorf("error identifying transaction: %v", err)
		}

		tx.BlockNumber = uint64(blockHeight)
		tx.Timestamp = uint64(blockTime)
		tx.Confirmed = true
		w.addTxToHistory(tx, txHash, true, false)
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
func (w *zecWallet) TxHistory(n int, refID *string, past bool) ([]*asset.WalletTransaction, error) {
	txHistoryDB := w.txDB()
	if txHistoryDB == nil {
		return nil, fmt.Errorf("tx database not initialized")
	}

	return txHistoryDB.GetTxs(n, refID, past)
}

const sendCategory = "send"

// idUnknownTx identifies the type and details of a transaction either made
// or recieved by the wallet.
func (w *zecWallet) idUnknownTx(tx *btcjson.ListTransactionsResult) (*asset.WalletTransaction, error) {
	txHash, err := chainhash.NewHashFromStr(tx.TxID)
	if err != nil {
		return nil, fmt.Errorf("error decoding tx hash %s: %v", tx.TxID, err)
	}
	msgTx, err := getTransaction(w, txHash)
	if err != nil {
		return nil, err
	}

	var totalOut uint64
	for _, txOut := range msgTx.TxOut {
		totalOut += uint64(txOut.Value)
	}

	fee := msgTx.RequiredTxFeesZIP317()

	txIsBond := func(msgTx *zTx) (bool, *asset.BondTxInfo) {
		if len(msgTx.TxOut) < 2 {
			return false, nil
		}
		const scriptVer = 0
		acctID, lockTime, pkHash, err := dexbtc.ExtractBondCommitDataV0(scriptVer, msgTx.TxOut[1].PkScript)
		if err != nil {
			return false, nil
		}
		return true, &asset.BondTxInfo{
			AccountID: acctID[:],
			LockTime:  uint64(lockTime),
			BondID:    pkHash[:],
		}
	}
	if isBond, bondInfo := txIsBond(msgTx); isBond {
		return &asset.WalletTransaction{
			Type:     asset.CreateBond,
			ID:       tx.TxID,
			Amount:   uint64(msgTx.TxOut[0].Value),
			Fees:     fee,
			BondInfo: bondInfo,
		}, nil
	}

	// Any other P2SH may be a swap or a send. We cannot determine unless we
	// look up the transaction that spends this UTXO.
	txPaysToScriptHash := func(msgTx *zTx) (v uint64) {
		for _, txOut := range msgTx.TxOut {
			if txscript.IsPayToScriptHash(txOut.PkScript) {
				v += uint64(txOut.Value)
			}
		}
		return
	}
	if v := txPaysToScriptHash(msgTx); v > 0 {
		return &asset.WalletTransaction{
			Type:   asset.SwapOrSend,
			ID:     tx.TxID,
			Amount: v,
			Fees:   fee,
		}, nil
	}

	// Helper function will help us identify inputs that spend P2SH contracts.
	containsContractAtPushIndex := func(msgTx *zTx, idx int, isContract func(contract []byte) bool) bool {
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
		_, _, _, _, err := dexbtc.ExtractSwapDetails(contract, false /* segwit */, w.btcParams)
		return err == nil
	}
	redeemsSwap := func(msgTx *zTx) bool {
		return containsContractAtPushIndex(msgTx, 4, contractIsSwap)
	}
	if redeemsSwap(msgTx) {
		return &asset.WalletTransaction{
			Type:   asset.Redeem,
			ID:     tx.TxID,
			Amount: totalOut + fee,
			Fees:   fee,
		}, nil
	}
	refundsSwap := func(msgTx *zTx) bool {
		return containsContractAtPushIndex(msgTx, 3, contractIsSwap)
	}
	if refundsSwap(msgTx) {
		return &asset.WalletTransaction{
			Type:   asset.Refund,
			ID:     tx.TxID,
			Amount: totalOut + fee,
			Fees:   fee,
		}, nil
	}

	// Bond refunds
	redeemsBond := func(msgTx *zTx) (bool, *asset.BondTxInfo) {
		var bondInfo *asset.BondTxInfo
		isBond := func(contract []byte) bool {
			const scriptVer = 0
			lockTime, pkHash, err := dexbtc.ExtractBondDetailsV0(scriptVer, contract)
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
	if isBondRedemption, bondInfo := redeemsBond(msgTx); isBondRedemption {
		return &asset.WalletTransaction{
			Type:     asset.RedeemBond,
			ID:       tx.TxID,
			Amount:   totalOut,
			Fees:     fee,
			BondInfo: bondInfo,
		}, nil
	}

	const scriptVersion = 0

	allOutputsPayUs := func(msgTx *zTx) bool {
		for _, txOut := range msgTx.TxOut {
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, w.btcParams)
			if err != nil {
				w.log.Errorf("ExtractAddrs error: %w", err)
				return false
			}
			if len(addrs) != 1 { // sanity check
				return false
			}

			addr, err := dexzec.EncodeAddress(addrs[0], w.addrParams)
			if err != nil {
				w.log.Errorf("unable to encode address: %w", err)
				return false
			}
			owns, err := w.OwnsDepositAddress(addr)
			if err != nil {
				w.log.Errorf("w.OwnsDepositAddress error: %w", err)
				return false
			}
			if !owns {
				return false
			}
		}

		return true
	}

	if tx.Category == sendCategory && allOutputsPayUs(msgTx) && len(msgTx.TxIn) == 1 {
		return &asset.WalletTransaction{
			Type: asset.Split,
			ID:   tx.TxID,
			Fees: fee,
		}, nil
	}

	txOutDirection := func(msgTx *zTx) (in, out uint64) {
		for _, txOut := range msgTx.TxOut {
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, w.btcParams)
			if err != nil {
				w.log.Errorf("ExtractAddrs error: %w", err)
				continue
			}
			if len(addrs) != 1 { // sanity check
				continue
			}

			addr, err := dexzec.EncodeAddress(addrs[0], w.addrParams)
			if err != nil {
				w.log.Errorf("unable to encode address: %w", err)
				continue
			}
			owns, err := w.OwnsDepositAddress(addr)
			if err != nil {
				w.log.Errorf("w.OwnsDepositAddress error: %w", err)
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

	in, out := txOutDirection(msgTx)

	getRecipient := func(msgTx *zTx, receive bool) *string {
		for _, txOut := range msgTx.TxOut {
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, w.btcParams)
			if err != nil {
				w.log.Errorf("ExtractAddrs error: %w", err)
				continue
			}
			if len(addrs) != 1 { // sanity check
				continue
			}

			addr, err := dexzec.EncodeAddress(addrs[0], w.addrParams)
			if err != nil {
				w.log.Errorf("unable to encode address: %w", err)
				continue
			}
			owns, err := w.OwnsDepositAddress(addr)
			if err != nil {
				w.log.Errorf("w.OwnsDepositAddress error: %w", err)
				continue
			}

			if receive == owns {
				return &addr
			}
		}
		return nil
	}

	if tx.Category == sendCategory {
		txType := asset.Send
		if allOutputsPayUs(msgTx) {
			txType = asset.SelfSend
		}
		return &asset.WalletTransaction{
			Type:      txType,
			ID:        tx.TxID,
			Amount:    out,
			Fees:      fee,
			Recipient: getRecipient(msgTx, false),
		}, nil
	}

	return &asset.WalletTransaction{
		Type:      asset.Receive,
		ID:        tx.TxID,
		Amount:    in,
		Fees:      fee,
		Recipient: getRecipient(msgTx, true),
	}, nil
}

// addUnknownTransactionsToHistory checks for any transactions the wallet has
// made or recieved that are not part of the transaction history. It scans
// from the last point to which it had previously scanned to the current tip.
func (w *zecWallet) addUnknownTransactionsToHistory(tip uint64) {
	txHistoryDB := w.txDB()

	// Zcash has a maximum reorg length of 100 blocks.
	const blockQueryBuffer = 100
	var blockToQuery uint64
	lastQuery := w.receiveTxLastQuery.Load()
	if lastQuery == 0 {
		// TODO: use wallet birthday instead of block 0.
		// blockToQuery = 0
	} else if lastQuery < tip-blockQueryBuffer {
		blockToQuery = lastQuery - blockQueryBuffer
	} else {
		blockToQuery = tip - blockQueryBuffer
	}

	txs, err := w.listSinceBlock(int64(blockToQuery))
	if err != nil {
		w.log.Errorf("Error listing transactions since block %d: %v", blockToQuery, err)
		return
	}

	for _, tx := range txs {
		if w.ctx.Err() != nil {
			return
		}
		txHash, err := chainhash.NewHashFromStr(tx.TxID)
		if err != nil {
			w.log.Errorf("Error decoding tx hash %s: %v", tx.TxID, err)
			continue
		}
		_, err = txHistoryDB.GetTx(txHash.String())
		if err == nil {
			continue
		}
		if !errors.Is(err, asset.CoinNotFoundError) {
			w.log.Errorf("Error getting tx %s: %v", txHash.String(), err)
			continue
		}
		wt, err := w.idUnknownTx(&tx)
		if err != nil {
			w.log.Errorf("error identifying transaction: %v", err)
			continue
		}

		if tx.BlockIndex != nil && *tx.BlockIndex > 0 && *tx.BlockIndex < int64(tip-blockQueryBuffer) {
			wt.BlockNumber = uint64(*tx.BlockIndex)
			wt.Timestamp = uint64(tx.BlockTime)
		}

		// Don't send notifications for the initial sync to avoid spamming the
		// front end. A notification is sent at the end of the initial sync.
		w.addTxToHistory(wt, txHash, true, blockToQuery == 0)
	}

	w.receiveTxLastQuery.Store(tip)
	err = txHistoryDB.SetLastReceiveTxQuery(tip)
	if err != nil {
		w.log.Errorf("Error setting last query to %d: %v", tip, err)
	}

	if blockToQuery == 0 {
		w.emit.TransactionHistorySyncedNote()
	}
}

// syncTxHistory checks to see if there are any transactions which the wallet
// has made or recieved that are not part of the transaction history, then
// identifies and adds them. It also checks all the pending transactions to see
// if they have been mined into a block, and if so, updates the transaction
// history to reflect the block height.
func (w *zecWallet) syncTxHistory(tip uint64) {
	if !w.syncingTxHistory.CompareAndSwap(false, true) {
		return
	}
	defer w.syncingTxHistory.Store(false)

	txHistoryDB := w.txDB()
	if txHistoryDB == nil {
		// It's actually impossible to get here, because we error and return
		// early in Connect if startTxHistoryDB returns an error, but we'll
		// log this for good measure anyway.
		w.log.Error("Transaction history database was not initialized")
		return
	}

	ss, err := w.SyncStatus()
	if err != nil {
		w.log.Errorf("Error getting sync status: %v", err)
		return
	}
	if !ss.Synced {
		return
	}

	w.addUnknownTransactionsToHistory(tip)

	pendingTxsCopy := make(map[chainhash.Hash]btc.ExtendedWalletTx, len(w.pendingTxs))
	w.pendingTxsMtx.RLock()
	for hash, tx := range w.pendingTxs {
		pendingTxsCopy[hash] = *tx
	}
	w.pendingTxsMtx.RUnlock()

	handlePendingTx := func(txHash chainhash.Hash, tx *btc.ExtendedWalletTx) {
		if !tx.Submitted {
			return
		}

		gtr, err := getTransaction(w, &txHash)
		if errors.Is(err, asset.CoinNotFoundError) {
			err = txHistoryDB.RemoveTx(txHash.String())
			if err == nil {
				w.pendingTxsMtx.Lock()
				delete(w.pendingTxs, txHash)
				w.pendingTxsMtx.Unlock()
			} else {
				// Leave it in the pendingPendingTxs and attempt to remove it
				// again next time.
				w.log.Errorf("Error removing tx %s from the history store: %v", txHash.String(), err)
			}
			return
		}
		if err != nil {
			if w.ctx.Err() != nil {
				return
			}
			w.log.Errorf("Error getting transaction %s: %v", txHash, err)
			return
		}

		var updated bool
		if gtr.blockHash != nil && *gtr.blockHash != (chainhash.Hash{}) {
			block, _, err := getVerboseBlockHeader(w, gtr.blockHash)
			if err != nil {
				w.log.Errorf("Error getting block height for %s: %v", gtr.blockHash, err)
				return
			}
			blockHeight := block.Height
			if tx.BlockNumber != uint64(blockHeight) || tx.Timestamp != uint64(block.Time) {
				tx.BlockNumber = uint64(blockHeight)
				tx.Timestamp = uint64(block.Time)
				updated = true
			}
		} else if gtr.blockHash == nil && tx.BlockNumber != 0 {
			tx.BlockNumber = 0
			tx.Timestamp = 0
			updated = true
		}

		var confs uint64
		if tx.BlockNumber > 0 && tip >= tx.BlockNumber {
			confs = tip - tx.BlockNumber + 1
		}
		if confs >= defaultConfTarget {
			tx.Confirmed = true
			updated = true
		}

		if updated {
			err = txHistoryDB.StoreTx(tx)
			if err != nil {
				w.log.Errorf("Error updating tx %s: %v", txHash, err)
				return
			}

			w.pendingTxsMtx.Lock()
			if tx.Confirmed {
				delete(w.pendingTxs, txHash)
			} else {
				w.pendingTxs[txHash] = tx
			}
			w.pendingTxsMtx.Unlock()

			w.emit.TransactionNote(tx.WalletTransaction, false)
		}
	}

	for hash, tx := range pendingTxsCopy {
		handlePendingTx(hash, &tx)
	}
}
