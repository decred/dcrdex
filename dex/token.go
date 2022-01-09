// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

// Gases is a the gas required for various operations.
// NOTE: For ERC20 tokens, the implementation can cause the gas to vary, or the
// implementation itself can change with a proxy delegate. The gas needed for
// initiations should be estimated directly (with EstimateGas) at order-time
// wherever possible.
type Gases struct {
	// Swap is the amount of gas needed to initialize a single ethereum swap.
	Swap uint64 `json:"swap"`
	// SwapAdd is the amount of gas needed to initialize additional swaps in
	// the same transaction.
	SwapAdd uint64 `json:"swapAdd"`
	// Redeem is the amount of gas it costs to redeem a swap.
	Redeem uint64 `json:"redeem"`
	// RedeemAdd is the amount of gas needed to redeem additional swaps in the
	// same transaction.
	RedeemAdd uint64 `json:"redeemAdd"`
	// Refund is the amount of gas needed to refund a swap.
	Refund uint64 `json:"refund"`
	// Approve is the amount of gas needed to approve the swap contract for
	// transferring tokens.
	Approve uint64 `json:"approve"`
	// Transfer is the amount of gas needed to transfer tokens.
	Transfer uint64 `json:"transfer"`
}

// SwapN calculates the gas needed to initiate n swaps.
func (g *Gases) SwapN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Swap + g.SwapAdd*(uint64(n)-1)
}

// RedeemN calculates the gas needed to redeem n swaps.
func (g *Gases) RedeemN(n int) uint64 {
	if n <= 0 {
		return 0
	}
	return g.Redeem + g.RedeemAdd*(uint64(n)-1)
}

// Token is a generic representation of a token-type asset.
type Token struct {
	// ParentID is the asset ID of the token's parent asset.
	ParentID uint32 `json:"parentID"`
	// Name is the display name of the token asset.
	Name string `json:"name"`
	// UnitInfo is the UnitInfo for the token.
	UnitInfo UnitInfo `json:"unitInfo"`
	// Gas is the Gases for the token.
	Gas Gases `json:"gas"`
	// NetAddresses is a mapping of token addresses for each network available.
	NetAddresses map[Network]*TokenAddresses `json:"netAddrs"`
}

// TokenAddresses are the addresses associated with the token and it's versioned
// swap contracts. The addresses are encoded as [20]byte since it works for
// erc20 on ETH, but a custom interface or maybe an fmt.Stringer might be used
// to generalize this in the future. We can't encode as common.Address because
// it would require an lgpl build, and that would preclude the use of the type
// in higher-level packages like core.
type TokenAddresses struct {
	// Address is the token contract address.
	Address [20]byte `json:"address"` // Will marshal as array of ints [0, 254, 12, ...]
	// SwapContracts is the versioned swap contracts bound to the token address.
	SwapContracts map[uint32][20]byte `json:"swapContracts"`
}
