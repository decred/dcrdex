// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

// An orderID is a 256 bit number encoded as a hex string.
const orderIdLen = 2 * order.OrderIDSize // 2 * 32

// VersionResponse holds bisonw and bisonw rpc server version.
type VersionResponse struct {
	RPCServerVer *dex.Semver `json:"rpcServerVersion"`
	BWVersion    *SemVersion `json:"dexcVersion"`
}

// SemVersion holds a semver version JSON object.
type SemVersion struct {
	VersionString string `json:"versionString"`
	Major         uint32 `json:"major"`
	Minor         uint32 `json:"minor"`
	Patch         uint32 `json:"patch"`
	Prerelease    string `json:"prerelease,omitempty"`
	BuildMetadata string `json:"buildMetadata,omitempty"`
}

//
// System param types
//

// HelpParams is the parameter type for the help route.
type HelpParams struct {
	HelpWith         string `json:"helpWith,omitempty"`
	IncludePasswords bool   `json:"includePasswords,omitempty"`
}

// InitParams is the parameter type for the init route.
type InitParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	Seed    *string          `json:"seed,omitempty"`
}

// LoginParams is the parameter type for the login route.
type LoginParams struct {
	AppPass encode.PassBytes `json:"appPass"`
}

//
// Wallet param types
//

// NewWalletParams is the parameter type for the newwallet route.
type NewWalletParams struct {
	AppPass    encode.PassBytes  `json:"appPass"`
	WalletPass encode.PassBytes  `json:"walletPass"`
	AssetID    uint32            `json:"assetID"`
	WalletType string            `json:"walletType"`
	Config     map[string]string `json:"config,omitempty"`
}

// ReconfigureWalletParams is the parameter type for the reconfigurewallet route.
type ReconfigureWalletParams struct {
	AppPass     encode.PassBytes  `json:"appPass"`
	NewWalletPW encode.PassBytes  `json:"newWalletPW,omitempty"`
	AssetID     uint32            `json:"assetID"`
	WalletType  string            `json:"walletType"`
	Config      map[string]string `json:"config,omitempty"`
}

// OpenWalletParams is the parameter type for the openwallet route.
type OpenWalletParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	AssetID uint32           `json:"assetID"`
}

// CloseWalletParams is the parameter type for the closewallet route.
type CloseWalletParams struct {
	AssetID uint32 `json:"assetID"`
}

// WalletBalanceParams is the parameter type for the walletbalance route.
type WalletBalanceParams struct {
	AssetID uint32 `json:"assetID"`
}

// WalletStateParams is the parameter type for the walletstate route.
type WalletStateParams struct {
	AssetID uint32 `json:"assetID"`
}

// ToggleWalletStatusParams is the parameter type for the togglewalletstatus route.
type ToggleWalletStatusParams struct {
	AssetID uint32 `json:"assetID"`
	Disable bool   `json:"disable"`
}

// RescanWalletParams is the parameter type for the rescanwallet route.
type RescanWalletParams struct {
	AssetID uint32 `json:"assetID"`
	Force   bool   `json:"force,omitempty"`
}

//
// Trading param types
//

// tradeResponse is used when responding to the trade route.
type tradeResponse struct {
	OrderID string `json:"orderID"`
	Sig     string `json:"sig"`
	Stamp   uint64 `json:"stamp"`
	Error   error  `json:"error,omitempty"`
}

// myOrdersResponse is used when responding to the myorders route.
type myOrdersResponse []*myOrder

// myOrder represents an order when responding to the myorders route.
type myOrder struct {
	Host        string   `json:"host"`
	MarketName  string   `json:"marketName"`
	BaseID      uint32   `json:"baseID"`
	QuoteID     uint32   `json:"quoteID"`
	ID          string   `json:"id"` // Can be empty if part of an InFlightOrder.
	Type        string   `json:"type"`
	Sell        bool     `json:"sell"`
	Stamp       uint64   `json:"stamp"`
	SubmitTime  uint64   `json:"submitTime"`
	Age         string   `json:"age"`
	Rate        uint64   `json:"rate,omitempty"`
	Quantity    uint64   `json:"quantity"`
	Filled      uint64   `json:"filled"`
	Settled     uint64   `json:"settled"`
	Status      string   `json:"status"`
	Cancelling  bool     `json:"cancelling,omitempty"`
	Canceled    bool     `json:"canceled,omitempty"`
	TimeInForce string   `json:"tif,omitempty"`
	Matches     []*match `json:"matches,omitempty"`
}

// match represents a match on an order. An order may have many matches.
type match struct {
	MatchID       string `json:"matchID"`
	Status        string `json:"status"`
	Revoked       bool   `json:"revoked"`
	Rate          uint64 `json:"rate"`
	Qty           uint64 `json:"qty"`
	Side          string `json:"side"`
	FeeRate       uint64 `json:"feeRate"`
	Swap          string `json:"swap,omitempty"`
	CounterSwap   string `json:"counterSwap,omitempty"`
	Redeem        string `json:"redeem,omitempty"`
	CounterRedeem string `json:"counterRedeem,omitempty"`
	Refund        string `json:"refund,omitempty"`
	Stamp         uint64 `json:"stamp"`
	IsCancel      bool   `json:"isCancel"`
}

// TradeParams is the parameter type for the trade route.
type TradeParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	core.TradeForm
}

// MultiTradeParams is the parameter type for the multitrade route.
type MultiTradeParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	core.MultiTradeForm
}

// CancelParams is the parameter type for the cancel route.
type CancelParams struct {
	OrderID string `json:"orderID"` // hex-encoded order ID
}

// OrderBookParams is the parameter type for the orderbook route.
type OrderBookParams struct {
	Host    string `json:"host"`
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	NOrders uint64 `json:"nOrders,omitempty"`
}

// MyOrdersParams is the parameter type for the myorders route.
type MyOrdersParams struct {
	Host  string  `json:"host,omitempty"`
	Base  *uint32 `json:"base,omitempty"`
	Quote *uint32 `json:"quote,omitempty"`
}

//
// Transaction param types
//

// SendParams is the parameter type for the send and withdraw routes.
type SendParams struct {
	AppPass  encode.PassBytes `json:"appPass"`
	AssetID  uint32           `json:"assetID"`
	Value    uint64           `json:"value"`
	Address  string           `json:"address"`
	Subtract bool             `json:"subtract,omitempty"`
}

// BchWithdrawParams is the parameter type for the withdrawbchspv route.
type BchWithdrawParams struct {
	AppPass   encode.PassBytes `json:"appPass"`
	Recipient string           `json:"recipient"`
}

// AbandonTxParams is the parameter type for the abandontx route.
type AbandonTxParams struct {
	AssetID uint32 `json:"assetID"`
	TxID    string `json:"txID"`
}

// AppSeedParams is the parameter type for the appseed route.
type AppSeedParams struct {
	AppPass encode.PassBytes `json:"appPass"`
}

// DeleteRecordsParams is the parameter type for the deletearchivedrecords route.
type DeleteRecordsParams struct {
	OlderThanMs *int64 `json:"olderThanMs,omitempty"`
	MatchesFile string `json:"matchesFile,omitempty"`
	OrdersFile  string `json:"ordersFile,omitempty"`
}

// deleteRecordsForm is the internal representation after processing DeleteRecordsParams.
type deleteRecordsForm struct {
	olderThan                     *time.Time
	ordersFileStr, matchesFileStr string
}

// NotificationsParams is the parameter type for the notifications route.
type NotificationsParams struct {
	N int `json:"n"`
}

// TxHistoryParams is the parameter type for the txhistory and bridgehistory routes.
type TxHistoryParams struct {
	AssetID uint32  `json:"assetID"`
	N       int     `json:"n,omitempty"`
	RefID   *string `json:"refID,omitempty"`
	Past    bool    `json:"past,omitempty"`
}

// WalletTxParams is the parameter type for the wallettx route.
type WalletTxParams struct {
	AssetID uint32 `json:"assetID"`
	TxID    string `json:"txID"`
}

//
// DEX param types
//

// getBondAssetsResponse is the getbondassets response payload.
type getBondAssetsResponse struct {
	Expiry uint64                     `json:"expiry"`
	Assets map[string]*core.BondAsset `json:"assets"`
}

// DiscoverAcctParams is the parameter type for the discoveracct route.
type DiscoverAcctParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	Addr    string           `json:"addr"`
	Cert    string           `json:"cert,omitempty"`
}

// GetDEXConfigParams is the parameter type for the getdexconfig route.
type GetDEXConfigParams struct {
	Host string `json:"host"`
	Cert string `json:"cert,omitempty"`
}

// BondAssetsParams is the parameter type for the bondassets route.
type BondAssetsParams struct {
	Host string `json:"host"`
	Cert string `json:"cert,omitempty"`
}

// core.BondOptionsForm is used directly for the bondopts route (already has JSON tags).
// core.PostBondForm is used directly for the postbond route (already has JSON tags).

//
// Market Making param types
//

// MMAvailableBalancesParams is the parameter type for the mmavailablebalances route.
type MMAvailableBalancesParams struct {
	mm.MarketWithHost
	CexBaseID  uint32  `json:"cexBaseID,omitempty"`
	CexQuoteID uint32  `json:"cexQuoteID,omitempty"`
	CexName    *string `json:"cexName,omitempty"`
}

// StartBotParams is the parameter type for the startmmbot route.
type StartBotParams struct {
	AppPass     encode.PassBytes   `json:"appPass"`
	CfgFilePath string             `json:"cfgFilePath"`
	Market      *mm.MarketWithHost `json:"market,omitempty"`
}

// StopBotParams is the parameter type for the stopmmbot route.
type StopBotParams struct {
	Market *mm.MarketWithHost `json:"market,omitempty"` // nil = stop all
}

// UpdateRunningBotParams is the parameter type for the updaterunningbotcfg route.
type UpdateRunningBotParams struct {
	CfgFilePath string                `json:"cfgFilePath"`
	Market      mm.MarketWithHost     `json:"market"`
	Balances    *mm.BotInventoryDiffs `json:"balances,omitempty"`
}

// UpdateRunningBotInventoryParams is the parameter type for the updaterunningbotinv route.
type UpdateRunningBotInventoryParams struct {
	Market   mm.MarketWithHost     `json:"market"`
	Balances *mm.BotInventoryDiffs `json:"balances"`
}

//
// Staking param types
//

// StakeStatusParams is the parameter type for the stakestatus route.
type StakeStatusParams struct {
	AssetID uint32 `json:"assetID"`
}

// SetVSPParams is the parameter type for the setvsp route.
type SetVSPParams struct {
	AssetID uint32 `json:"assetID"`
	Addr    string `json:"addr"`
}

// PurchaseTicketsParams is the parameter type for the purchasetickets route.
type PurchaseTicketsParams struct {
	AppPass encode.PassBytes `json:"appPass"`
	AssetID uint32           `json:"assetID"`
	N       int              `json:"n"`
}

// SetVotingPreferencesParams is the parameter type for the setvotingprefs route.
type SetVotingPreferencesParams struct {
	AssetID        uint32            `json:"assetID"`
	Choices        map[string]string `json:"choices,omitempty"`
	TSpendPolicy   map[string]string `json:"tSpendPolicy,omitempty"`
	TreasuryPolicy map[string]string `json:"treasuryPolicy,omitempty"`
}

//
// Bridge param types
//

// CheckBridgeApprovalParams is the parameter type for the checkbridgeapproval route.
type CheckBridgeApprovalParams struct {
	AssetID    uint32 `json:"assetID"`
	BridgeName string `json:"bridgeName"`
}

// ApproveBridgeParams is the parameter type for the approvebridgecontract route.
type ApproveBridgeParams struct {
	AssetID    uint32 `json:"assetID"`
	BridgeName string `json:"bridgeName"`
	Approve    bool   `json:"approve"`
}

// BridgeParams is the parameter type for the bridge route.
type BridgeParams struct {
	FromAssetID uint32 `json:"fromAssetID"`
	ToAssetID   uint32 `json:"toAssetID"`
	Amt         uint64 `json:"amt"`
	BridgeName  string `json:"bridgeName"`
}

// PendingBridgesParams is the parameter type for the pendingbridges route.
type PendingBridgesParams struct {
	AssetID uint32 `json:"assetID"`
}

// SupportedBridgesParams is the parameter type for the supportedbridges route.
type SupportedBridgesParams struct {
	AssetID uint32 `json:"assetID"`
}

// BridgeFeesAndLimitsParams is the parameter type for the bridgefeesandlimits route.
type BridgeFeesAndLimitsParams struct {
	FromAssetID uint32 `json:"fromAssetID"`
	ToAssetID   uint32 `json:"toAssetID"`
	BridgeName  string `json:"bridgeName"`
}

// BridgeHistory reuses TxHistoryParams.

//
// Multisig param types
//

// PaymentMultisigPubkeyParams is the parameter type for the paymentmultisigpubkey route.
type PaymentMultisigPubkeyParams struct {
	AssetID uint32 `json:"assetID"`
}

// CsvFileParams is the parameter type for handlers that take a single CSV file path.
type CsvFileParams struct {
	CsvFilePath string `json:"csvFilePath"`
}

// SignMultisigParams is the parameter type for the signmultisig route.
type SignMultisigParams struct {
	CsvFilePath string `json:"csvFilePath"`
	SignIdx     int    `json:"signIdx"`
}

//
// Peers param types
//

// WalletPeersParams is the parameter type for the walletpeers route.
type WalletPeersParams struct {
	AssetID uint32 `json:"assetID"`
}

// AddRemovePeerParams is the parameter type for the addwalletpeer and removewalletpeer routes.
type AddRemovePeerParams struct {
	AssetID uint32 `json:"assetID"`
	Address string `json:"address"`
}

// MMReportParams is the parameter type for the mmreport route.
type MMReportParams struct {
	Host       string `json:"host"`
	BaseID     uint32 `json:"baseID"`
	QuoteID    uint32 `json:"quoteID"`
	StartEpoch uint64 `json:"startEpoch"`
	EndEpoch   uint64 `json:"endEpoch"`
	OutFile    string `json:"outFile"`
}

// PruneMMSnapshotsParams is the parameter type for the prunemmsnapshots route.
type PruneMMSnapshotsParams struct {
	Host        string `json:"host"`
	BaseID      uint32 `json:"baseID"`
	QuoteID     uint32 `json:"quoteID"`
	MinEpochIdx uint64 `json:"minEpochIdx"`
}

// DeployContractParams is the parameter type for the deploycontract route.
type DeployContractParams struct {
	AppPass      encode.PassBytes `json:"appPass"`
	Chains       []string         `json:"chains"`
	ContractVer  *uint32          `json:"contractVer,omitempty"`
	TokenAddress *string          `json:"tokenAddress,omitempty"`
	Bytecode     *string          `json:"bytecode,omitempty"`
}

// TestContractGasParams is the parameter type for the testcontractgas route.
type TestContractGasParams struct {
	AppPass  encode.PassBytes `json:"appPass"`
	Chains   []string         `json:"chains"`
	Tokens   []string         `json:"tokens,omitempty"`
	MaxSwaps *int             `json:"maxSwaps,omitempty"`
}
