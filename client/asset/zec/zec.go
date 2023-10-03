// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
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
	minNetworkVersion   = 5040250 // v5.4.2
	walletTypeRPC       = "zcashdRPC"

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

	// requiredRedeemConfirms is the amount of confirms a redeem transaction
	// needs before the trade is considered confirmed. The redeem is
	// monitored until this number of confirms is reached.
	requiredRedeemConfirms = 1
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
		Version:           version,
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
		ar:   ar,
		node: cl,
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
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

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

	// Coins returned by Fund are cached for quick reference.
	cm *btc.CoinManager

	tipMtx     sync.RWMutex
	currentTip *btc.BlockVector

	reserves atomic.Uint64
}

var _ asset.FeeRater = (*zecWallet)(nil)
var _ asset.Wallet = (*zecWallet)(nil)

// DRAFT TODO: Implement LiveReconfigurer
// var _ asset.LiveReconfigurer = (*zecWallet)(nil)

func (w *zecWallet) CallRPC(method string, args []any, thing any) error {
	return btc.Call(w.ctx, w.node, method, args, thing)
}

// FeeRate returns the asset standard fee rate for Zcash.
func (w *zecWallet) FeeRate() uint64 {
	return dexzec.LegacyFeeRate
}

func (w *zecWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	w.ctx = ctx
	var wg sync.WaitGroup

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
	atomic.StoreInt64(&w.tipAtConnect, w.currentTip.Height)

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
	return &wg, nil
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
			return getWalletTransaction(w, h)
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
				w.log.Errorf("failed to get best block header from node: %w", err)
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
	w.log.Debugf("tip change: %d (%s) => %d (%s)", prevTip.Height, prevTip.Hash, newTip.Height, newTip.Hash)
	w.emit.TipChange(uint64(newTip.Height))

	w.rf.ReportNewTip(ctx, prevTip, newTip)
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

	// Start by attempting max lots with a basic fee.
	lots := avail / lotSize
	for lots > 0 {
		est, _, _, err := w.estimateSwap(lots, lotSize, feeSuggestion, maxFeeRate, utxos, bals.orchard, true)
		// The only failure mode of estimateSwap -> zec.fund is when there is
		// not enough funds, so if an error is encountered, count down the lots
		// and repeat until we have enough.
		if err != nil {
			lots--
			continue
		}
		return utxos, bals, est, nil
	}

	return utxos, bals, &asset.SwapEstimate{}, nil
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
	transparent uint64
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
		sapling:     zeroConf.Sapling,
		transparent: zeroConf.Transparent,
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

func (w *zecWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, feeSuggestion uint64) (*asset.ConfirmRedemptionStatus, error) {
	txHash, _, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	tx, err := getWalletTransaction(w, txHash)
	// redemption transaction found, return its confirms.
	//
	// TODO: Investigate the case where this redeem has been sitting in the
	// mempool for a long amount of time, possibly requiring some action by
	// us to get it unstuck.
	if err == nil {
		return &asset.ConfirmRedemptionStatus{
			Confs:  tx.Confirmations,
			Req:    requiredRedeemConfirms,
			CoinID: coinID,
		}, nil
	}

	// Redemption transaction is missing from the point of view of our node!
	// Unlikely, but possible it was redeemed by another transaction. Check
	// if the contract is still an unspent output.

	swapHash, vout, err := decodeCoinID(redemption.Spends.Coin.ID())
	if err != nil {
		return nil, err
	}

	utxo, _, err := getTxOut(w, swapHash, vout)
	if err != nil {
		return nil, newError(errNoTx, "error finding unspent contract %s with swap hash %v vout %d: %w", redemption.Spends.Coin.ID(), swapHash, vout, err)
	}
	if utxo == nil {
		// TODO: Spent, but by who. Find the spending tx.
		w.log.Warnf("Contract coin %v with swap hash %v vout %d spent by someone but not sure who.", redemption.Spends.Coin.ID(), swapHash, vout)
		// Incorrect, but we will be in a loop of erroring if we don't
		// return something.
		return &asset.ConfirmRedemptionStatus{
			Confs:  requiredRedeemConfirms,
			Req:    requiredRedeemConfirms,
			CoinID: coinID,
		}, nil
	}

	// The contract has not yet been redeemed, but it seems the redeeming
	// tx has disappeared. Assume the fee was too low at the time and it
	// was eventually purged from the mempool. Attempt to redeem again with
	// a currently reasonable fee.

	form := &asset.RedeemForm{
		Redemptions: []*asset.Redemption{redemption},
	}
	_, coin, _, err := w.Redeem(form)
	if err != nil {
		return nil, fmt.Errorf("unable to re-redeem %s with swap hash %v vout %d: %w", redemption.Spends.Coin.ID(), swapHash, vout, err)
	}
	return &asset.ConfirmRedemptionStatus{
		Confs:  0,
		Req:    requiredRedeemConfirms,
		CoinID: coin.ID(),
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

func (w *zecWallet) DepositAddress() (string, error) {
	return transparentAddressString(w)
}

func (w *zecWallet) NewAddress() (string, error) {
	return w.DepositAddress()
}

// DEPRECATED
func (w *zecWallet) EstimateRegistrationTxFee(feeRate uint64) uint64 {
	return math.MaxUint64
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
		req := dexzec.RequiredOrderFunds(v.Value, 1, swapInputSize+1, v.MaxSwapCount)
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

	customCfg, err := decodeFundMultiSettings(mo.Options)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error decoding options: %w", err)
	}

	return w.fundMulti(maxLock, mo.Values, mo.FeeSuggestion, mo.MaxFeeRate, customCfg.Split)
}

func (w *zecWallet) fundMulti(maxLock uint64, values []*asset.MultiOrderValue, splitTxFeeRate, maxFeeRate uint64, allowSplit bool) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	reserves := w.reserves.Load()

	coins, redeemScripts, fundingCoins, spents, err := w.cm.FundMultiBestEffort(reserves, maxLock, values, maxFeeRate, allowSplit)
	if err != nil {
		return nil, nil, 0, codedError(errFunding, err)
	}
	if len(coins) == len(values) || !allowSplit {
		w.cm.LockOutputsMap(fundingCoins)
		lockUnspent(w, false, spents)
		return coins, redeemScripts, 0, nil
	}

	return w.fundMultiWithSplit(reserves, maxLock, values)
}

// fundMultiWithSplit creates a split transaction to fund multiple orders. It
// attempts to fund as many of the orders as possible without a split transaction,
// and only creates a split transaction for the remaining orders. This is only
// called after it has been determined that all of the orders cannot be funded
// without a split transaction.
func (w *zecWallet) fundMultiWithSplit(
	keep, maxLock uint64,
	values []*asset.MultiOrderValue,
) ([]asset.Coins, [][]dex.Bytes, uint64, error) {

	utxos, _, avail, err := w.cm.SpendableUTXOs(0)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	canFund, splitCoins, splitSpents := w.fundMultiSplitTx(values, utxos, keep, maxLock)
	if !canFund {
		return nil, nil, 0, fmt.Errorf("cannot fund all with split")
	}

	remainingUTXOs := utxos
	remainingOrders := values

	// The return values must be in the same order as the values that were
	// passed in, so we keep track of the original indexes here.
	indexToFundingCoins := make(map[int][]*btc.CompositeUTXO, len(values))
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
		orderIndex, fundingUTXOs := w.cm.OrderWithLeastOverFund(maxLock-totalFunded, 0, remainingOrders, remainingUTXOs)
		if orderIndex == -1 {
			break
		}
		totalFunded += btc.SumUTXOs(fundingUTXOs)
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
		remainingUTXOs = btc.UTxOSetDiff(remainingUTXOs, fundingUTXOs)

		// Then we make sure that a split transaction can be created for
		// any remaining orders without using the utxos returned by
		// orderWithLeastOverFund.
		if len(newRemainingOrders) > 0 {
			canFund, newSplitCoins, newSpents := w.fundMultiSplitTx(newRemainingOrders, remainingUTXOs, keep, maxLock-totalFunded)
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
		splitOutputCoins, splitFees, err = w.submitMultiSplitTx(splitCoins, splitSpents, remainingOrders)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error creating split transaction: %w", err)
		}
	}

	coins := make([]asset.Coins, len(values))
	redeemScripts := make([][]dex.Bytes, len(values))
	spents := make([]*btc.Output, 0, len(values))

	var splitIndex int
	locks := make([]*btc.UTxO, 0)
	for i := range values {
		if fundingUTXOs, ok := indexToFundingCoins[i]; ok {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, unspent := range fundingUTXOs {
				output := btc.NewOutput(unspent.TxHash, unspent.Vout, unspent.Amount)
				locks = append(locks, &btc.UTxO{
					TxHash:  unspent.TxHash,
					Vout:    unspent.Vout,
					Amount:  unspent.Amount,
					Address: unspent.Address,
				})
				coins[i][j] = output
				spents = append(spents, output)
				redeemScripts[i][j] = unspent.RedeemScript
			}
		} else {
			coins[i] = splitOutputCoins[splitIndex]
			redeemScripts[i] = []dex.Bytes{nil}
			splitIndex++
		}
	}

	w.cm.LockOutputs(locks)

	lockUnspent(w, false, spents)

	return coins, redeemScripts, splitFees, nil
}

func (w *zecWallet) submitMultiSplitTx(fundingCoins asset.Coins, spents []*btc.Output, orders []*asset.MultiOrderValue) ([]asset.Coins, uint64, error) {
	baseTx, totalIn, _, err := w.fundedTx(spents)
	if err != nil {
		return nil, 0, err
	}

	// DRAFT TODO: Should we lock these without locking with CoinManager?
	lockUnspent(w, false, spents)
	var success bool
	defer func() {
		if !success {
			lockUnspent(w, true, spents)
		}
	}()

	requiredForOrders, totalRequired := w.fundsRequiredForMultiOrders(orders, uint64(len(spents)), dexbtc.RedeemP2WPKHInputTotalSize)

	outputAddresses := make([]btcutil.Address, len(orders))
	for i, req := range requiredForOrders {
		outputAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
		if err != nil {
			return nil, 0, err
		}
		outputAddresses[i] = outputAddr
		script, err := txscript.PayToAddrScript(outputAddr)
		if err != nil {
			return nil, 0, err
		}
		baseTx.AddTxOut(wire.NewTxOut(int64(req), script))
	}

	changeAddr, err := transparentAddress(w, w.addrParams, w.btcParams)
	if err != nil {
		return nil, 0, err
	}
	tx, err := w.sendWithReturn(baseTx, changeAddr, totalIn, totalRequired)
	if err != nil {
		return nil, 0, err
	}

	txHash := tx.TxHash()
	coins := make([]asset.Coins, len(orders))
	ops := make([]*btc.Output, len(orders))
	locks := make([]*btc.UTxO, len(coins))
	for i := range coins {
		coins[i] = asset.Coins{btc.NewOutput(&txHash, uint32(i), uint64(tx.TxOut[i].Value))}
		ops[i] = btc.NewOutput(&txHash, uint32(i), uint64(tx.TxOut[i].Value))
		locks[i] = &btc.UTxO{
			TxHash:  &txHash,
			Vout:    uint32(i),
			Amount:  uint64(tx.TxOut[i].Value),
			Address: outputAddresses[i].String(),
		}
	}
	w.cm.LockOutputs(locks)
	lockUnspent(w, false, ops)

	var totalOut uint64
	for _, txOut := range tx.TxOut {
		totalOut += uint64(txOut.Value)
	}

	success = true
	return coins, totalIn - totalOut, nil
}

func (w *zecWallet) fundsRequiredForMultiOrders(orders []*asset.MultiOrderValue, inputCount, inputsSize uint64) ([]uint64, uint64) {
	requiredForOrders := make([]uint64, len(orders))
	var totalRequired uint64

	for i, value := range orders {
		req := dexzec.RequiredOrderFunds(value.Value, inputCount, inputsSize, value.MaxSwapCount)
		requiredForOrders[i] = req
		totalRequired += req
	}

	return requiredForOrders, totalRequired
}

// fundMultiSplitTx uses the utxos provided and attempts to fund a multi-split
// transaction to fund each of the orders. If successful, it returns the
// funding coins and outputs.
func (w *zecWallet) fundMultiSplitTx(
	orders []*asset.MultiOrderValue,
	utxos []*btc.CompositeUTXO,
	keep, maxLock uint64,
) (bool, asset.Coins, []*btc.Output) {

	_, totalOutputRequired := w.fundsRequiredForMultiOrders(orders, uint64(len(utxos)), dexbtc.RedeemP2PKHInputSize)

	outputsSize := uint64(dexbtc.P2WPKHOutputSize) * uint64(len(utxos)+1)
	// splitTxSizeWithoutInputs := dexbtc.MinimumTxOverhead + outputsSize

	enough := func(inputCount, inputsSize, sum uint64) (bool, uint64) {
		fees := dexzec.TxFeesZIP317(inputsSize+uint64(wire.VarIntSerializeSize(inputCount)), outputsSize, 0, 0, 0, 0)
		req := totalOutputRequired + fees
		return sum >= req, sum - req
	}

	fundSplitCoins, _, spents, _, inputsSize, _, err := w.cm.FundWithUTXOs(utxos, keep, false, enough)
	if err != nil {
		return false, nil, nil
	}

	if maxLock > 0 {
		fees := dexzec.TxFeesZIP317(inputsSize+uint64(wire.VarIntSerializeSize(uint64(len(spents)))), outputsSize, 0, 0, 0, 0)
		if totalOutputRequired+fees > maxLock {
			return false, nil, nil
		}
	}

	return true, fundSplitCoins, spents
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
	const numInputs = 12 // plan for lots of inputs to get a safe estimate

	return shieldedSplitFees + dexzec.TxFeesZIP317(1, uint64(numTrades+1)*dexbtc.P2PKHOutputSize, 0, 0, 0, 0)
}

func (w *zecWallet) OwnsDepositAddress(addrStr string) (bool, error) {
	return ownsAddress(w, addrStr)
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
	return w.DepositAddress()
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
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := getWalletTransaction(w, txHash)
	if err != nil {
		return 0, err
	}
	return uint32(tx.Confirmations), nil
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

func (w *zecWallet) EstimateSendTxFee(
	addrStr string, val, _ /* feeRate*/ uint64, _ /* subtract */ bool,
) (
	fee uint64, isValidAddress bool, err error,
) {

	isValidAddress = w.ValidateAddress(addrStr)

	const minConfs = 0
	_, _, spents, _, inputsSize, _, err := w.cm.Fund(w.reserves.Load(), minConfs, false, sendEnough(val))
	if err != nil {
		return 0, isValidAddress, newError(errFunding, "error funding transaction: %w", err)
	}
	fee = dexzec.TxFeesZIP317(inputsSize+uint64(wire.VarIntSerializeSize(uint64(len(spents)))), 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
	return
}

func (w *zecWallet) Send(address string, value, feeRate uint64) (string, asset.Coin, error) {
	txHash, vout, sent, err := w.send(address, value, false)
	if err != nil {
		return "", nil, err
	}
	return txHash.String(), btc.NewOutput(txHash, vout, sent), nil
}

// TransactionConfirmations gets the number of confirmations for the specified
// transaction.
func (w *zecWallet) TransactionConfirmations(ctx context.Context, txID string) (confs uint32, err error) {
	return
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
	var txOutsSize uint64
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
		txOutsSize += uint64(txOut.SerializeSize())
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
		w.cm.LockOutputs([]*btc.UTxO{{
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
	return uint32(tx.Confirmations), true, nil
}

func (w *zecWallet) SyncStatus() (bool, float32, error) {
	ss, err := syncStatus(w)
	if err != nil {
		return false, 0, err
	}

	if ss.Target == 0 { // do not say progress = 1
		return false, 0, nil
	}
	if ss.Syncing {
		ogTip := atomic.LoadInt64(&w.tipAtConnect)
		totalToSync := ss.Target - int32(ogTip)
		var progress float32 = 1
		if totalToSync > 0 {
			progress = 1 - (float32(ss.Target-ss.Height) / float32(totalToSync))
		}
		return false, progress, nil
	}

	// It looks like we are ready based on syncStatus, but that may just be
	// comparing wallet height to known chain height. Now check peers.
	numPeers, err := peerCount(w)
	if err != nil {
		return false, 0, err
	}
	return numPeers > 0, 1, nil
}

func (w *zecWallet) ValidateAddress(address string) bool {
	_, err := dexzec.DecodeAddress(address, w.addrParams, w.btcParams)
	return err == nil
}

func (w *zecWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

func (w *zecWallet) useSplitTx() bool {
	return w.walletCfg.Load().(*WalletConfig).UseSplitTx
}

var _ asset.ShieldedWallet = (*zecWallet)(nil)

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
	return w.NewShieldedAddress()
}

// ShieldedStatus list the last address and the balance in the shielded
// account.
func (w *zecWallet) ShieldedStatus() (status *asset.ShieldedStatus, err error) {
	// z_listaccounts to get account 1 addresses
	// DRAFT NOTE: It sucks that we need to list all accounts here. The zeroth
	// account is our transparent addresses, and they'll all be included in the
	// result. Should probably open a PR at zcash/zcash to add the ability to
	// list a single account.
	status = new(asset.ShieldedStatus)
	status.LastAddress, err = w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}

	bals, err := zGetBalanceForAccount(w, shieldedAcctNumber, 0)
	if err != nil {
		return nil, err
	}

	status.Balance = bals.Orchard

	return status, nil
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
		Available: bals.orchard.avail + bals.transparent,
		Immature:  bals.orchard.maturing,
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

// NewShieldedAddress creates a new shielded address. A shielded address can be
// be reused without sacrifice of privacy on-chain, but that doesn't stop
// meat-space coordination to reduce privacy.
func (w *zecWallet) NewShieldedAddress() (string, error) {
	// An orchard address is the same as a unified address with only an orchard
	// receiver.
	addrRes, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{orchardAddressType})
	if err != nil {
		return "", err
	}
	w.lastAddress.Store(addrRes.Address)
	return addrRes.Address, nil
}

// ShieldFunds moves funds from the transparent account to the shielded account.
func (w *zecWallet) ShieldFunds(ctx context.Context, transparentVal uint64) ([]byte, error) {
	bal, err := w.Balance()
	if err != nil {
		return nil, err
	}
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal.Available < fees || bal.Available-fees < transparentVal {
		return nil, asset.ErrInsufficientBalance
	}
	oneAddr, err := w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}
	// DRAFT TODO: Using ANY_TADDR can fail if some of the balance is in
	// coinbase txs, which have special handling requirements. The user would
	// need to either 1) Send all coinbase outputs to a transparent address, or
	// 2) use z_shieldcoinbase from their zcash-cli interface.
	const anyTAddr = "ANY_TADDR"
	txHash, err := w.sendOne(ctx, anyTAddr, oneAddr, transparentVal, AllowRevealedSenders)
	if err != nil {
		return nil, err
	}
	return txHash[:], nil
}

// UnshieldFunds moves funds from the shielded account to the transparent
// account.
func (w *zecWallet) UnshieldFunds(ctx context.Context, amt uint64) ([]byte, error) {
	bals, err := zGetBalanceForAccount(w, shieldedAcctNumber, minOrchardConfs)
	if err != nil {
		return nil, fmt.Errorf("z_getbalance error: %w", err)
	}
	bal := bals.Orchard
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal < fees || bal-fees < amt {
		return nil, asset.ErrInsufficientBalance
	}

	unified, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return nil, fmt.Errorf("z_getaddressforaccount error: %w", err)
	}

	receivers, err := zGetUnifiedReceivers(w, unified.Address)
	if err != nil {
		return nil, fmt.Errorf("z_getunifiedreceivers error: %w", err)
	}

	txHash, err := w.sendOneShielded(ctx, receivers.Transparent, amt, AllowRevealedRecipients)
	if err != nil {
		return nil, err
	}

	return txHash[:], nil
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

// SendShielded sends funds from the shielded account to the provided shielded
// or transparent address.
func (w *zecWallet) SendShielded(ctx context.Context, toAddr string, amt uint64) ([]byte, error) {
	bals, err := zGetBalanceForAccount(w, shieldedAcctNumber, minOrchardConfs)
	if err != nil {
		return nil, err
	}
	bal := bals.Orchard
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal < fees || bal-fees < amt {
		return nil, asset.ErrInsufficientBalance
	}

	res, err := zValidateAddress(w, toAddr)
	if err != nil {
		return nil, fmt.Errorf("error validating address: %w", err)
	}

	favoredReceiverAndPolicy := func(r *unifiedReceivers) (string, privacyPolicy, error) {
		switch {
		case r.Orchard != "":
			return r.Orchard, FullPrivacy, nil
		case r.Sapling != "":
			return r.Sapling, AllowRevealedAmounts, nil
		case r.Transparent != "":
			return r.Transparent, AllowRevealedRecipients, nil
		default:
			return "", "", fmt.Errorf("no known receiver types")
		}
	}

	var priv privacyPolicy
	switch res.AddressType {
	case unifiedAddressType:
		// Could be orchard, or other unified..
		receivers, err := zGetUnifiedReceivers(w, toAddr)
		if err != nil {
			return nil, fmt.Errorf("error getting unified receivers: %w", err)
		}
		toAddr, priv, err = favoredReceiverAndPolicy(receivers)
		if err != nil {
			return nil, fmt.Errorf("error parsing unified receiver: %w", err)
		}
	case transparentAddressType:
		priv = AllowRevealedRecipients
	case saplingAddressType:
		priv = AllowRevealedAmounts
	default:
		return nil, fmt.Errorf("unknown address type: %q", res.AddressType)
	}

	txHash, err := w.sendOneShielded(ctx, toAddr, amt, priv)
	if err != nil {
		return nil, err
	}

	return txHash[:], nil
}

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// estimateFee returns the asset standard legacy fee rate.
func estimateFee(context.Context, btc.RawRequester, uint64) (uint64, error) {
	return dexzec.LegacyFeeRate, nil
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
