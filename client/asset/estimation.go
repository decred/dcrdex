// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

// SwapEstimate is an estimate of the fees and locked amounts associated with
// an order.
type SwapEstimate struct {
	// Lots is the number of lots in the order.
	Lots uint64 `json:"lots"`
	// Value is the total value of the order.
	Value uint64 `json:"value"`
	// MaxFees is the maximum possible fees that can be assessed for the order's
	// swaps.
	MaxFees uint64 `json:"maxFees"`
	// RealisticWorstCase is an estimation of the fees that might be assessed in
	// a worst-case scenario of 1 tx per 1 lot match, but at the prevailing fee
	// rate estimate.
	RealisticWorstCase uint64 `json:"realisticWorstCase"`
	// RealisticBestCase is an estimation of the fees that might be assessed in
	// a best-case scenario of 1 tx and 1 output for the entire order.
	RealisticBestCase uint64 `json:"realisticBestCase"`
	// FeeReservesPerLot is the amount that must be reserved per lot to cover
	// fees for swap transactions.
	FeeReservesPerLot uint64 `json:"feeReservesPerLot"`
}

// RedeemEstimate is an estimate of the range of fees that might realistically
// be assessed to the redemption transaction.
type RedeemEstimate struct {
	// RealisticBestCase is the best-case scenario fees of a single transaction
	// with a match covering the entire order, at the prevailing fee rate.
	RealisticBestCase uint64 `json:"realisticBestCase"`
	// RealisticWorstCase is the worst-case scenario fees of all 1-lot matches,
	// each with their own call to Redeem.
	RealisticWorstCase uint64 `json:"realisticWorstCase"`
}

// PreSwapForm can be used to get a swap fees estimate.
type PreSwapForm struct {
	// Version is the asset version. Most backends only support one version.
	Version uint32
	// LotSize is the lot size for the calculation. For quote assets, LotSize
	// should be based on either the user's limit order rate, or some measure
	// of the current market rate.
	LotSize uint64
	// Lots is the number of lots in the order.
	Lots uint64
	// MaxFeeRate is the highest possible fee rate that may be required for the
	// swap initialization transaction.
	MaxFeeRate uint64
	// Immediate should be set to true if this is for an order that is not a
	// standing order, likely a market order or a limit order with immediate
	// time-in-force.
	Immediate bool
	// FeeSuggestion is a suggested fee from the server. If a split transaction
	// is used, the fee rate used should be at least the suggested fee, else
	// zero-conf coins might be rejected.
	FeeSuggestion uint64
	// SelectedOptions is any options that the user has selected. The available
	// PreOrder options and values can be inter-dependent, so when a user
	// selects an option, a new PreOrder can be generated to updated the
	// options available and recalculate the effects.
	SelectedOptions map[string]string

	// The following fields are only used for some assets where the redeemed/to
	// asset may require funds in this "from" asset. For example, buying ERC20
	// tokens with ETH. The funds may be used for withdraw approval for the
	// redeemed token contract, or just the redeem tx gas.

	// RedeemVersion is the asset version of the "to" asset with the redeem
	// transaction. Most backends only support one version.
	RedeemVersion uint32
	// RedeemAssetID is the asset ID of the "to" asset.
	RedeemAssetID uint32
}

// PreSwap is a SwapEstimate returned from Wallet.PreSwap.
type PreSwap struct {
	Estimate *SwapEstimate  `json:"estimate"`
	Options  []*OrderOption `json:"options"`
}

// PreRedeemForm can be used to get a redemption estimate.
type PreRedeemForm struct {
	// Version is the asset version. Most backends only support one version.
	Version uint32
	// Lots is the number of lots in the order.
	Lots uint64
	// FeeSuggestion is a suggested fee from the server.
	FeeSuggestion uint64
	// SelectedOptions is any options that the user has selected.
	SelectedOptions map[string]string
}

// PreRedeem is an estimate of the fees for redemption. The struct will be
// expanded in in-progress work to accommodate order-time options.
type PreRedeem struct {
	Estimate *RedeemEstimate `json:"estimate"`
	Options  []*OrderOption  `json:"options"`
}

// MaxOrderForm is used to get a SwapEstimate from the Wallet's MaxOrder method.
type MaxOrderForm struct {
	LotSize       uint64
	FeeSuggestion uint64
	AssetVersion  uint32 // the init/from asset version
	MaxFeeRate    uint64 // max fee rate of init/from asset

	// The following Redeem asset fields are only required for certain assets
	// where redeeming the other asset may require funds of the "from" asset
	// (this wallet's asset) to be reserved. e.g. ERC20 tokens bought with ETH.

	RedeemVersion uint32 // the redeem/to asset version
	RedeemAssetID uint32 // the redeem/to asset ID
}
