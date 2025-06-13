package xmr

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexxmr "decred.org/dcrdex/dex/networks/xmr"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	version           = 0
	BipID             = 128
	minNetworkVersion = 180400 // v0.18.4.0-release
	walletTypeRPC     = "XmrRPC"
	TxMeanSize        = 2000 // https://xmrchain.net/txpool
)

// var errDevNoImpl = errors.New("development: not yet implemented") // dev only

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:          "toolsdir",
			DisplayName:  "Monero CLI tools folder",
			Description:  "The path to the Monero CLI folder you downloaded from Monero github. A linux example is '/home/<user>/monero-x86_64-linux-gnu-v0.18.4'",
			DefaultValue: "/home/dev/monero-x86_64-linux-gnu-v0.18.4.0", // dev TODO(xmr) remove
			// DefaultValue: "", // the real default
			ShowByDefault: true,
			Required:      true,
		},
		{
			Key:         "serveraddr",
			DisplayName: "Daemon Address:Port",
			Description: "Address and port of a Monero daemon: http://<address>:<port>. The default is 'http://localhost:18081'." +
				" Using the default implies a local Monero daemon for which you have downloaded the full chain. Any other will be" +
				" a remote daemon",
			DefaultValue: "http://node.monerodevs.org:18089", // dev TODO(xmr) remove
			// DefaultValue: "http://localhost:18081", // the real default
			ShowByDefault: true,
			Required:      true,
		},
		{
			Key:           "serverpass",
			DisplayName:   "Daemon JSON-RPC Password",
			Description:   "RPC 'username:password' to access the Monero daemon (usually none) .. Should match the daemon's requirement.",
			DefaultValue:  "",
			ShowByDefault: false,
			NoEcho:        true,
			// https://localmonero.co/knowledge/remote-nodes-privacy
		},
		{
			Key:         "feepriority",
			DisplayName: "Transaction Priority",
			Description: "Set a priority for transaction fees. This will result in more or less fees." +
				" Accepted Values are: 0-4 for: 'default', 'unimportant', 'normal', 'elevated', 'priority'." +
				" You could change the value here but unless there is severe congestion just use 0-default. ",
			DefaultValue:  "0",
			MinValue:      0,
			MaxValue:      4,
			ShowByDefault: true,
			// Monero fees cannot be chosen, only a fee priority. The fees are set as a function of priority when
			// building a tx so this Settings value can be changed by the user (reconfigure) before building a tx.
			// https://monero.stackexchange.com/questions/4544/what-does-the-default-priority-priority-0-do-in-monero-wallet-cli
		},
	}

	// WalletInfo defines some general information about a Monero wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Monero",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexxmr.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:        walletTypeRPC,
			Tab:         "External",
			Description: "Connect to Monero over RPC",
			ConfigOpts:  configOpts,
			NoAuth:      false,
			GuideLink:   "https://monero.fail",
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

type Driver struct{}

// Driver implements asset.Driver.
var _ asset.Driver = (*Driver)(nil)

// Open creates the XMR exchange wallet.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return newWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Monero.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Monero transactions don't have bitcoin-like outputs, so the coinID
	// will just be the tx hash.
	if len(coinID) == chainhash.HashSize {
		var txHash chainhash.Hash
		copy(txHash[:], coinID)
		return txHash.String(), nil
	}
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// MinLotSize calculates the minimum lot size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return 1 // TODO(xmr) fix when tradeable
}

func parseWalletConfig(settings map[string]string) (*xmrConfigSettings, error) {
	xwSettings := new(xmrConfigSettings)
	err := config.Unmapify(settings, &xwSettings)
	if err != nil {
		return nil, fmt.Errorf("error parsing wallet settings: %v", err)
	}
	return xwSettings, nil
}

// newWallet constructs the unconnected exchange wallet.
func newWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	xmrConfigSettings, err := parseWalletConfig(cfg.Settings)
	if err != nil {
		return nil, err
	}
	feePriority, err := strconv.ParseUint(xmrConfigSettings.FeePriorityStr, 10, 8)
	if err != nil {
		return nil, err
	}
	if feePriority > 4 {
		return nil, fmt.Errorf("invalid fee priority %d", feePriority)
	}
	xmrpc, err := newXmrRpc(cfg, xmrConfigSettings, network, logger)
	if err != nil {
		return nil, err
	}
	xw := xmrWallet{
		xmrpc:       xmrpc,
		log:         logger,
		walletInfo:  *WalletInfo,
		feePriority: rpc.Priority(feePriority),
	}
	return &xw, nil
}

type xmrConfigSettings struct {
	CliToolsDir    string `ini:"toolsdir"`
	ServerAddr     string `ini:"serveraddr"`
	ServerRpcPass  string `ini:"serverpass"`
	FeePriorityStr string `ini:"feepriority"`
}

type xmrCoin struct {
	cid   []byte
	txid  string
	value uint64
}

func newXmrCoin(txHash string, value uint64) (*xmrCoin, error) {
	cid, err := hex.DecodeString(txHash)
	if err != nil {
		return nil, err
	}
	return &xmrCoin{
		cid:   cid,
		txid:  txHash,
		value: value,
	}, nil
}

// xmrCoin implements asset.Coin
var _ asset.Coin = (*xmrCoin)(nil)

func (c *xmrCoin) ID() dex.Bytes  { return c.cid }
func (c *xmrCoin) String() string { return c.txid }
func (c *xmrCoin) Value() uint64  { return c.value }
func (c *xmrCoin) TxID() string   { return c.txid }

type xmrWallet struct {
	xmrpc       *xmrRpc
	log         dex.Logger
	walletInfo  asset.WalletInfo
	feePriority rpc.Priority
}

//////////////////
// asset.Wallet //
//////////////////

// xmrWallet implements asset.Wallet
var _ asset.Wallet = (*xmrWallet)(nil)

// Connect starts monero-wallet-rpc. The configured monerod should already be running.
func (x *xmrWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return x.xmrpc.connect(ctx)
}

// Info returns a set of basic information about the wallet driver.
func (x *xmrWallet) Info() *asset.WalletInfo {
	return &x.walletInfo
}

// Balance should return the balance of the wallet, categorized by
// available, immature, and locked by dex.
func (x *xmrWallet) Balance() (*asset.Balance, error) {
	total, mature, err := x.xmrpc.getBalance()
	if err != nil {
		return nil, err
	}
	locked := uint64(0) // TODO(xmr) impl when tradeable
	immature := total - mature
	available := mature - locked
	return &asset.Balance{
		Available: available,
		Immature:  immature,
		Locked:    locked,
	}, nil
}

// DepositAddress returns an address for depositing funds into Bison Wallet.
func (x *xmrWallet) DepositAddress() (string, error) {
	return x.xmrpc.getNewAddress(MainAccountIndex)
}

// OwnsDepositAddress indicates if the provided address can be used
// to deposit funds into the wallet.
func (x *xmrWallet) OwnsDepositAddress(address string) (bool, error) {
	return x.xmrpc.isOurAddress(MainAccountIndex, address)
}

// SyncStatus is information about the blockchain sync status. It should
// only indicate synced when there are network peers and all blocks on the
// network have been processed by the wallet.
func (x *xmrWallet) SyncStatus() (*asset.SyncStatus, error) {
	return x.xmrpc.syncStatus()
}

// Send sends the exact value to the specified address. This is different
// from Withdraw, which subtracts the tx fees from the amount sent.
// The feerate param is ignored and the currently configured priority used.
func (x *xmrWallet) Send(address string, value uint64, _ uint64) (asset.Coin, error) {
	txHash, err := x.xmrpc.transferSimple(value, address, x.feePriority)
	if err != nil {
		return nil, err
	}
	return newXmrCoin(txHash, value)
}

// StandardSendFee returns the fee for a "standard" send tx.
func (x *xmrWallet) StandardSendFee(feeRate uint64) uint64 {
	return feeRate * TxMeanSize
}

// ValidateAddress checks that the provided address is valid.
func (x *xmrWallet) ValidateAddress(address string) bool {
	return x.xmrpc.validateAddress(address)
}

//---------------------------------------
// Below are unsupported until tradeable
//---------------------------------------

func (x *xmrWallet) FundOrder(_ *asset.Order) (coins asset.Coins, redeemScripts []dex.Bytes, fees uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *xmrWallet) MaxOrder(_ *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) PreSwap(_ *asset.PreSwapForm) (*asset.PreSwap, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) PreRedeem(_ *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) ReturnCoins(_ asset.Coins) error {
	return asset.ErrUnsupported
}
func (x *xmrWallet) FundingCoins(_ []dex.Bytes) (asset.Coins, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) Swap(_ *asset.Swaps) (receipts []asset.Receipt, changeCoin asset.Coin, feesPaid uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *xmrWallet) Redeem(redeems *asset.RedeemForm) (ins []dex.Bytes, out asset.Coin, feesPaid uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *xmrWallet) SignMessage(_ asset.Coin, _ dex.Bytes) (pubkeys []dex.Bytes, sigs []dex.Bytes, err error) {
	return nil, nil, asset.ErrUnsupported
}
func (x *xmrWallet) AuditContract(coinID dex.Bytes, contract dex.Bytes, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	return false, time.Time{}, asset.ErrUnsupported
}
func (x *xmrWallet) FindRedemption(ctx context.Context, coinID dex.Bytes, contract dex.Bytes) (redemptionCoin dex.Bytes, secret dex.Bytes, err error) {
	return nil, nil, asset.ErrUnsupported
}
func (x *xmrWallet) Refund(coinID dex.Bytes, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) RedemptionAddress() (string, error) {
	return "", asset.ErrUnsupported
}
func (x *xmrWallet) LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error) {
	return true, asset.ErrUnsupported
}
func (x *xmrWallet) SwapConfirmations(ctx context.Context, coinID dex.Bytes, contract dex.Bytes, matchTime time.Time) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrUnsupported
}
func (x *xmrWallet) ValidateSecret(secret []byte, secretHash []byte) bool {
	return false
}
func (x *xmrWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	return 0, asset.ErrUnsupported
}
func (x *xmrWallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, feeSuggestion uint64) (*asset.ConfirmRedemptionStatus, error) {
	return nil, asset.ErrUnsupported
}
func (x *xmrWallet) SingleLotSwapRefundFees(version uint32, feeRate uint64, useSafeTxSize bool) (uint64, uint64, error) {
	return 0, 0 /* asset.ErrUnsupported */, nil
}
func (x *xmrWallet) SingleLotRedeemFees(version uint32, feeRate uint64) (uint64, error) {
	return 0 /* asset.ErrUnsupported */, nil
}
func (x *xmrWallet) FundMultiOrder(ord *asset.MultiOrder, maxLock uint64) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingFees uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *xmrWallet) MaxFundingFees(numTrades uint32, feeRate uint64, options map[string]string) uint64 {
	return 0
}

//////////////////////////
// asset.TxFeeEstimator //
//////////////////////////

// xmrWallet implements asset.TxFeeEstimator
var _ asset.TxFeeEstimator = (*xmrWallet)(nil)

// EstimateSendTxFee returns a tx fee rate estimate for sending or withdrawing
// the provided amount using the provided feeRate. This uses actual utxos to
// calculate the tx fee where possible and ensures the wallet has enough to
// cover send value and minimum fees.
//
// Note: this is an expensive call as it needs a tx to be built (and signed)
// The feerate param is ignored and the currently configured priority used.
// The maxwithdraw param is ignored.
func (x *xmrWallet) EstimateSendTxFee(address string, value, _ /*feerate*/ uint64, subtract, _ /*maxWithdraw*/ bool) (uint64, bool, error) {
	if !x.ValidateAddress(address) {
		return 0, false, fmt.Errorf("invalid xmr address: %s for net: %s", address, x.xmrpc.net.String())
	}
	fee, err := x.xmrpc.estimateTxFeeAtoms(value, address, subtract, x.feePriority)
	if err != nil {
		return 0, true, err
	}
	return fee, true, nil
}

////////////////////
// asset.FeeRater //
////////////////////

// xmrWallet implements asset.FeeRater
var _ asset.FeeRater = (*xmrWallet)(nil)

// default priority fee rate is returned; or 0
func (x *xmrWallet) FeeRate() uint64 {
	fee, err := x.xmrpc.getFeeRate()
	if err != nil {
		return 0
	}
	return fee
}

//////////////////////
// asset.Withdrawer //
//////////////////////

// xmrWallet implements asset.Withdrawer
var _ asset.Withdrawer = (*xmrWallet)(nil)

// Withdraw withdraws funds to the specified address. Fees are subtracted
// from the value.
// The feerate param is ignored and the currently configured priority used.
func (x *xmrWallet) Withdraw(address string, value, _ /*feerate*/ uint64) (asset.Coin, error) {
	txHash, err := x.xmrpc.withdrawSimple(address, value, x.feePriority)
	if err != nil {
		return nil, err
	}
	return newXmrCoin(txHash, value)
}

////////////////////////
// asset.NewAddresser //
////////////////////////

// xmrWallet implements asset.NewAddresser
var _ asset.NewAddresser = (*xmrWallet)(nil)

func (x *xmrWallet) NewAddress() (string, error) {
	return x.xmrpc.getNewAddress(MainAccountIndex)
}

func (x *xmrWallet) AddressUsed(address string) (bool, error) {
	return x.xmrpc.getAddressUsage(MainAccountIndex, address)
}

/////////////////////////
// asset.Authenticator //
/////////////////////////

// xmrWallet implements asset.Authenticator
var _ asset.Authenticator = (*xmrWallet)(nil)

// dummy impl.
var locked = false

func (x *xmrWallet) Unlock(pw []byte) error {
	x.log.Debugf("Xmr Wallet - Unlock (%s)", pw)
	locked = false
	return nil
}

func (x *xmrWallet) Lock() error {
	x.log.Debugf("Xmr Wallet - Lock")
	locked = true
	return nil
}

func (x *xmrWallet) Locked() bool {
	return locked
}

/////////////////////
// asset.Rescanner //
/////////////////////

// xmrWallet implements asset.Rescanner
var _ asset.Rescanner = (*xmrWallet)(nil)

// Rescan performs a rescan and blocks until it is done. If no birthday is
// provided, internal wallets may use a birthday concurrent with the
// earliest date at which a wallet was possible, which is asset-dependent.
//
// Note: this does 'rescan_spent' where wallet-rpc calls 'is_key_image_spent'
// on the server for each key image it has; and this may alter the balance.
// The server must be 'trusted'
func (x *xmrWallet) Rescan(rescanCtx context.Context, bday /* unix time seconds */ uint64) error {
	return x.xmrpc.rescanSpents(rescanCtx)
}

////////////////////////////
// asset.LiveReconfigurer //
////////////////////////////

// xmrWallet implements asset.Reconfigure
var _ asset.LiveReconfigurer = (*xmrWallet)(nil)

// Reconfigure attempts to reconfigure the wallet. If reconfiguration
// requires a restart, the Wallet should still validate as much
// configuration as possible.
//
// This is very simple:
// - Only fee priority in settings considered.
// - currentAddress ignored.
// - No restart.
func (x *xmrWallet) Reconfigure(reconfCtx context.Context, cfg *asset.WalletConfig, _ string) (bool, error) {
	xmrConfigSettings, err := parseWalletConfig(cfg.Settings)
	if err != nil {
		return false, err
	}
	feePriority, err := strconv.ParseUint(xmrConfigSettings.FeePriorityStr, 10, 8)
	if err != nil {
		return false, fmt.Errorf("invalid fee priority")
	}
	if feePriority > 4 {
		return false, fmt.Errorf("invalid fee priority %d", feePriority)
	}
	x.feePriority = rpc.Priority(feePriority)
	return false, nil
}
