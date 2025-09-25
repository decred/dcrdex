package xmr

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/xmr/txn"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexxmr "decred.org/dcrdex/dex/networks/xmr"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	version       = 0
	BipID         = 128
	walletTypeRPC = "XmrRPC"
	TxMeanSize    = 2000 // https://xmrchain.net/txpool
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "toolsdir",
			DisplayName: "Monero CLI tools folder",
			Description: "Required. The path to the Monero CLI folder you downloaded from Monero github." +
				" This should be the Latest release version." +
				" A linux example is '/home/<user>/monero-x86_64-linux-gnu-v0.18.4.2'." +
				" If you later change this setting you need restart bisonw for the changes to take effect.",
			DefaultValue:  "",
			ShowByDefault: true,
			Required:      true,
		},
		{
			Key:         "feepriority",
			DisplayName: "Transaction Priority",
			Description: "Set a priority for transaction fees. This will result in more or less fees." +
				" Accepted Values are: 0-4 for: 'default', 'unimportant', 'normal', 'elevated', 'priority'." +
				" You can change the value here at any time but unless there is severe congestion just use 0-default. ",
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
			Seeded:      true,
			Tab:         "External",
			Description: "Connect to Monero",
			ConfigOpts:  configOpts,
			NoAuth:      true,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

type Driver struct{}

// Driver implements asset.Driver and asset.Creator.
var _ asset.Driver = (*Driver)(nil)
var _ asset.Creator = (*Driver)(nil)

// Open creates the XMR exchange wallet.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return newWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Monero
// which should be the output's stealth address.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Monero transactions have outputs but no amounts so the coinID
	// will just be the tx hash for now; representing the full output
	// amount sent. Change is unknown but the fee is known.
	if len(coinID) != txn.KeyLen {
		return "", fmt.Errorf("bad tx_hash size")
	}
	return hex.EncodeToString(coinID), nil
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

// Exists will be true if the specified wallet exists.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	exists := !walletFilesMissing(dataDir)
	return exists, nil
}

// Create creates a new wallet.
func (d *Driver) Create(cwp *asset.CreateWalletParams) error {
	configSettings, err := parseWalletConfig(cwp.Settings)
	if err != nil {
		return err
	}
	err = checkConfig(configSettings)
	if err != nil {
		return err
	}
	if len(cwp.Pass) != 32 {
		return fmt.Errorf("bad password length %d expected 32", len(cwp.Pass))
	}
	var pw string
	if cwp.Net == dex.Simnet {
		pw = "sim"
	} else {
		pw = hex.EncodeToString(cwp.Pass)
	}
	ks := new(keystore)
	err = ks.put(pw, cwp.Net)
	if err != nil {
		return fmt.Errorf("failed to store pw %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cliToolsDir := configSettings.CliToolsDir
	trustedDaemons := getTrustedDaemons(cwp.Net, true, cwp.DataDir) // true: can be used for cli
	if len(trustedDaemons) == 0 {
		return fmt.Errorf("no trusted damons")
	}
	return cliGenerateRefreshWallet(ctx, trustedDaemons[0], cwp.Net, cwp.DataDir, cliToolsDir, pw, true)
}

func parseWalletConfig(settings map[string]string) (*configSettings, error) {
	xwSettings := new(configSettings)
	err := config.Unmapify(settings, &xwSettings)
	if err != nil {
		return nil, fmt.Errorf("error parsing wallet settings: %w", err)
	}
	return xwSettings, nil
}

func checkConfig(s *configSettings) error {
	_, dir := filepath.Split(s.CliToolsDir)
	err := checkToolsVersion(dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(s.CliToolsDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("empty folder %s", dir)
	}
	walletRpc := WalletServerRpcName
	cli := CliName
	if runtime.GOOS == "windows" {
		walletRpc += ".exe"
		cli += ".exe"
	}
	gotWalletRpc := false
	gotCli := false
	for _, entry := range entries {
		if gotWalletRpc && gotCli {
			break
		}
		if entry.IsDir() {
			continue
		}
		if walletRpc == entry.Name() {
			gotWalletRpc = true
			continue
		}
		if cli == entry.Name() {
			gotCli = true
			continue
		}
	}
	if !gotWalletRpc || !gotCli {
		var missingWalletRpc string
		if !gotWalletRpc {
			missingWalletRpc = walletRpc
		}
		var missingCli string
		if !gotCli {
			missingCli = cli
		}
		return fmt.Errorf("missing wallet tools: %s %s in folder %s", missingWalletRpc, missingCli, dir)
	}
	return nil
}

func checkToolsVersion(dir string) error {
	if !strings.HasPrefix(dir, "monero") {
		return fmt.Errorf("tools folder does not start with 'monero'")
	}
	i := strings.LastIndex(dir, "v")
	if i < 0 {
		return fmt.Errorf("start of version string 'v' not found in %s", dir)
	}
	last := dir[i:]
	if len(last) <= 1 {
		return fmt.Errorf("version too short %s", dir)
	}
	ver := last[1:]
	mv, err := newMoneroVersionFromVersionString(ver)
	if err != nil {
		return err
	}
	if !mv.valid() {
		return fmt.Errorf("invalid version %s", ver)
	}
	return nil
}

// newWallet constructs an unconnected exchange wallet.
func newWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	configSettings, err := parseWalletConfig(cfg.Settings)
	if err != nil {
		return nil, err
	}
	err = checkConfig(configSettings)
	if err != nil {
		return nil, err
	}
	feePriority, err := strconv.ParseUint(configSettings.FeePriorityStr, 10, 8)
	if err != nil {
		return nil, err
	}
	if feePriority > 4 {
		return nil, fmt.Errorf("invalid fee priority %d", feePriority)
	}
	xmrpc, err := newXmrRpc(cfg, configSettings, network, logger)
	if err != nil {
		return nil, err
	}
	xw := wallet{
		net:         network,
		log:         logger,
		walletInfo:  *WalletInfo,
		feePriority: rpc.Priority(feePriority),
		locked:      false,
		xmrpc:       xmrpc,
	}
	return &xw, nil
}

type configSettings struct {
	CliToolsDir    string `ini:"toolsdir"`
	FeePriorityStr string `ini:"feepriority"`
}

type coin struct {
	cid   []byte
	txid  string
	value uint64
}

func newCoin(txHash string, value uint64) (*coin, error) {
	cid, err := hex.DecodeString(txHash)
	if err != nil {
		return nil, err
	}
	return &coin{
		cid:   cid,
		txid:  txHash,
		value: value,
	}, nil
}

// coin implements asset.Coin
var _ asset.Coin = (*coin)(nil)

func (c *coin) ID() dex.Bytes  { return c.cid }
func (c *coin) String() string { return c.txid }
func (c *coin) Value() uint64  { return c.value }
func (c *coin) TxID() string   { return c.txid }

type wallet struct {
	net         dex.Network
	log         dex.Logger
	walletInfo  asset.WalletInfo
	feePriority rpc.Priority
	locked      bool
	xmrpc       *xmrRpc
}

//////////////////
// asset.Wallet //
//////////////////

// wallet implements asset.Wallet
var _ asset.Wallet = (*wallet)(nil)

// Connect starts monero-wallet-rpc. The configured monerod should already be running.
func (x *wallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return x.xmrpc.connect(ctx)
}

// Info returns a set of basic information about the wallet driver.
func (x *wallet) Info() *asset.WalletInfo {
	return &x.walletInfo
}

// Balance should return the balance of the wallet, categorized by
// available, immature, and locked by dex.
func (x *wallet) Balance() (*asset.Balance, error) {
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
func (x *wallet) DepositAddress() (string, error) {
	return x.xmrpc.getNewAddress(MainAccountIndex)
}

// OwnsDepositAddress indicates if the provided address can be used
// to deposit funds into the wallet.
func (x *wallet) OwnsDepositAddress(address string) (bool, error) {
	return x.xmrpc.isOurAddress(MainAccountIndex, address)
}

// SyncStatus is information about the blockchain sync status. It should
// only indicate synced when there are network peers and all blocks on the
// network have been processed by the wallet.
func (x *wallet) SyncStatus() (*asset.SyncStatus, error) {
	return x.xmrpc.syncStatus()
}

// Send sends the exact value to the specified address. This is different
// from Withdraw, which subtracts the tx fees from the amount sent.
// The feerate param is ignored and the currently configured priority used.
func (x *wallet) Send(address string, value uint64 /*feeRate*/, _ uint64) (asset.Coin, error) {
	txHash, err := x.xmrpc.transferSimple(value, address, 0, x.feePriority)
	if err != nil {
		return nil, err
	}
	return newCoin(txHash, value)
}

// StandardSendFee returns the fee for a "standard" send tx.
func (x *wallet) StandardSendFee(feeRate uint64) uint64 {
	return feeRate * TxMeanSize
}

// ValidateAddress checks that the provided address is valid.
func (x *wallet) ValidateAddress(address string) bool {
	return x.xmrpc.validateAddress(address)
}

//---------------------------------------
// Below are unsupported until tradeable
//---------------------------------------

func (x *wallet) FundOrder(_ *asset.Order) (coins asset.Coins, redeemScripts []dex.Bytes, fees uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *wallet) MaxOrder(_ *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) PreSwap(_ *asset.PreSwapForm) (*asset.PreSwap, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) PreRedeem(_ *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) ReturnCoins(_ asset.Coins) error {
	return asset.ErrUnsupported
}
func (x *wallet) FundingCoins(_ []dex.Bytes) (asset.Coins, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) Swap(_ *asset.Swaps) (receipts []asset.Receipt, changeCoin asset.Coin, feesPaid uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *wallet) Redeem(redeems *asset.RedeemForm) (ins []dex.Bytes, out asset.Coin, feesPaid uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *wallet) SignMessage(_ asset.Coin, _ dex.Bytes) (pubkeys []dex.Bytes, sigs []dex.Bytes, err error) {
	return nil, nil, asset.ErrUnsupported
}
func (x *wallet) AuditContract(coinID dex.Bytes, contract dex.Bytes, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	return false, time.Time{}, asset.ErrUnsupported
}
func (x *wallet) FindRedemption(ctx context.Context, coinID dex.Bytes, contract dex.Bytes) (redemptionCoin dex.Bytes, secret dex.Bytes, err error) {
	return nil, nil, asset.ErrUnsupported
}
func (x *wallet) Refund(coinID dex.Bytes, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) RedemptionAddress() (string, error) {
	return "", asset.ErrUnsupported
}
func (x *wallet) LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error) {
	return true, asset.ErrUnsupported
}
func (x *wallet) SwapConfirmations(ctx context.Context, coinID dex.Bytes, contract dex.Bytes, matchTime time.Time) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrUnsupported
}
func (x *wallet) ValidateSecret(secret []byte, secretHash []byte) bool {
	return false
}
func (x *wallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	return 0, asset.ErrUnsupported
}
func (x *wallet) ConfirmRedemption(coinID dex.Bytes, redemption *asset.Redemption, feeSuggestion uint64) (*asset.ConfirmRedemptionStatus, error) {
	return nil, asset.ErrUnsupported
}
func (x *wallet) SingleLotSwapRefundFees(version uint32, feeRate uint64, useSafeTxSize bool) (uint64, uint64, error) {
	return 0, 0 /* asset.ErrUnsupported */, nil
}
func (x *wallet) SingleLotRedeemFees(version uint32, feeRate uint64) (uint64, error) {
	return 0 /* asset.ErrUnsupported */, nil
}
func (x *wallet) FundMultiOrder(ord *asset.MultiOrder, maxLock uint64) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingFees uint64, err error) {
	return nil, nil, 0, asset.ErrUnsupported
}
func (x *wallet) MaxFundingFees(numTrades uint32, feeRate uint64, options map[string]string) uint64 {
	return 0
}

//////////////////////////
// asset.TxFeeEstimator //
//////////////////////////

// wallet implements asset.TxFeeEstimator
var _ asset.TxFeeEstimator = (*wallet)(nil)

// EstimateSendTxFee returns a tx fee rate estimate for sending or withdrawing
// the provided amount using the provided feeRate. This uses actual utxos to
// calculate the tx fee where possible and ensures the wallet has enough to
// cover send value and minimum fees.
//
// Note: this is an expensive call as it needs a tx to be built.
//
// The feerate param is ignored and the currently configured priority used.
// The maxwithdraw param is ignored.
func (x *wallet) EstimateSendTxFee(address string, value, _ /*feerate*/ uint64, subtract, _ /*maxWithdraw*/ bool) (uint64, bool, error) {
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

// wallet implements asset.FeeRater
var _ asset.FeeRater = (*wallet)(nil)

// default priority fee rate is returned; or 0
func (x *wallet) FeeRate() uint64 {
	fee, err := x.xmrpc.getFeeRate()
	if err != nil {
		return 0
	}
	return fee
}

//////////////////////
// asset.Withdrawer //
//////////////////////

// wallet implements asset.Withdrawer
var _ asset.Withdrawer = (*wallet)(nil)

// Withdraw withdraws funds to the specified address. Fees are subtracted
// from the value.
// The feerate param is ignored and the currently configured priority used.
func (x *wallet) Withdraw(address string, value, _ /*feerate*/ uint64) (asset.Coin, error) {
	txHash, err := x.xmrpc.withdrawSimple(address, value, x.feePriority)
	if err != nil {
		return nil, err
	}
	return newCoin(txHash, value)
}

////////////////////////
// asset.NewAddresser //
////////////////////////

// wallet implements asset.NewAddresser
var _ asset.NewAddresser = (*wallet)(nil)

func (x *wallet) NewAddress() (string, error) {
	return x.xmrpc.getNewAddress(MainAccountIndex)
}

func (x *wallet) AddressUsed(address string) (bool, error) {
	return x.xmrpc.getAddressUsage(MainAccountIndex, address)
}

////////////////////////////
// asset.LiveReconfigurer //
////////////////////////////

// xmrWallet implements asset.Reconfigure
var _ asset.LiveReconfigurer = (*wallet)(nil)

// Reconfigure attempts to reconfigure the wallet. If reconfiguration
// requires a restart, the Wallet should still validate as much
// configuration as possible.
//
// - currentAddress ignored.
// - no restart
func (x *wallet) Reconfigure(reconfCtx context.Context, cfg *asset.WalletConfig, _ /*currentAddress*/ string) (bool, error) {
	if x.xmrpc.isSyncing() {
		return false, errSyncing
	}
	configSettings, err := parseWalletConfig(cfg.Settings)
	if err != nil {
		return false, err
	}
	err = checkConfig(configSettings)
	if err != nil {
		return false, err
	}
	feePriority, err := strconv.ParseUint(configSettings.FeePriorityStr, 10, 8)
	if err != nil {
		return false, fmt.Errorf("invalid fee priority")
	}
	if feePriority > 4 {
		return false, fmt.Errorf("invalid fee priority %d", feePriority)
	}
	x.feePriority = rpc.Priority(feePriority)
	return false, nil
}

/////////////////////////
// asset.Authenticator //
/////////////////////////

// This is a dummy implementation just to make the ui look nicer otherwise
// it would not show [Ready] after startup and clicking [Unlock].

var _ asset.Authenticator = (*wallet)(nil)

// Unlock unlocks the exchange wallet.
func (x *wallet) Unlock(pw []byte) error {
	x.locked = false
	return nil
}

// Lock locks the exchange wallet.
func (x *wallet) Lock() error {
	x.locked = true
	return nil
}

// Locked will be true if the wallet is currently locked.
func (x *wallet) Locked() bool {
	return x.locked
}

//////////////////
// asset.Sender //
//////////////////

// Sender does wallet sends with more granular control parameters

var _ asset.Sender = (*wallet)(nil)

func (x *wallet) SendAndLock(address string, value uint64, unlock uint64, feeRate uint64) (asset.Coin, error) {
	txHash, err := x.xmrpc.transferSimple(value, address, unlock, x.feePriority)
	if err != nil {
		return nil, err
	}
	return newCoin(txHash, value)
}
