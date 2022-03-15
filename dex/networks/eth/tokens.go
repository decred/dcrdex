// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
)

// Token is the definition of an ERC20 token, including all of its network and
// version variants.
type Token struct {
	*dex.Token
	// NetTokens is a mapping of token addresses for each network available.
	NetTokens map[dex.Network]*NetToken `json:"netAddrs"`
	// EVMFactor allows for arbitrary ERC20 decimals. For an ERC20 contract,
	// the relation
	//    math.Log10(UnitInfo.Conventional.ConversionFactor) + Token.EVMFactor = decimals
	// should hold true.
	// Since most assets will use a value of 9 here, a default value of 9 will
	// be used in AtomicToEVM and EVMToAtomic if EVMFactor is not set.
	EVMFactor *int64 `json:"evmFactor"` // default 9
}

// factor calculates the conversion factor to and from DEX atomic units to the
// units used for EVM operations.
func (t *Token) factor() *big.Int {
	var evmFactor int64 = 9
	if t.EVMFactor != nil {
		evmFactor = *t.EVMFactor
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(evmFactor), nil)
}

// AtomicToEVM converts from DEX atomic units to EVM units.
func (t *Token) AtomicToEVM(v uint64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(v)), t.factor())
}

// EVMToAtomic converts from raw EVM units to DEX atomic units.
func (t *Token) EVMToAtomic(v *big.Int) uint64 {
	vDEX := new(big.Int).Div(v, t.factor())
	if vDEX.IsUint64() {
		return vDEX.Uint64()
	}
	return 0
}

// NetToken are the addresses associated with the token and its versioned swap
// contracts.
type NetToken struct {
	// Address is the token contract address.
	Address common.Address `json:"address"`
	// SwapContracts is the versioned swap contracts bound to the token address.
	SwapContracts map[uint32]*SwapContract `json:"swapContracts"`
}

// SwapContract represents a single swap contract instance.
type SwapContract struct {
	Address common.Address
	Gas     Gases
}

var Tokens = map[uint32]*Token{
	// testTokenID = 'dextt.eth' is the ID used for the test token from
	// dex/networks/erc20/contracts/TestToken.sol that is deployed on the simnet
	// harness, and possibly other networks too if needed for testing.
	testTokenID: {
		Token: &dex.Token{
			ParentID: EthBipID,
			Name:     "DCRDEXTestToken",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "Dextoshi",
				Conventional: dex.Denomination{
					Unit:             "DEXTT",
					ConversionFactor: GweiFactor,
				},
			},
		},
		NetTokens: map[dex.Network]*NetToken{
			dex.Mainnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*SwapContract{},
			},
			dex.Testnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*SwapContract{},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Gas: Gases{
							Swap:      174_000,
							SwapAdd:   115_000,
							Redeem:    70_000,
							RedeemAdd: 33_000,
							Refund:    50_000, // [48149 48149 48137 48149 48137]
							// Approve is the gas used to call the approve
							// method of the contract. For Approve transactions,
							// the very first approval for an account-spender
							// pair takes more than subsequent approvals. The
							// results are repeated for a different account's
							// first approvals on the same contract, so it's not
							// just the global first.
							Approve:  46_000, //  [44465 27365 27365 27365 27365]
							Transfer: 27_000, // [24964 24964 24964 24964 24964]
						},
					},
				},
			},
		},
	},
}

// VersionedNetworkToken retrieves the token, token address, and swap contract
// address for the token asset.
func VersionedNetworkToken(assetID uint32, contractVer uint32, net dex.Network) (token *Token,
	tokenAddr, contractAddr common.Address, err error) {

	token, found := Tokens[assetID]
	if !found {
		return nil, common.Address{}, common.Address{}, fmt.Errorf("token %d not found", assetID)
	}
	addrs, found := token.NetTokens[net]
	if !found {
		return nil, common.Address{}, common.Address{}, fmt.Errorf("token %d has no network %s", assetID, net)
	}
	contract, found := addrs.SwapContracts[contractVer]
	if !found {
		return nil, common.Address{}, common.Address{}, fmt.Errorf("token %d version %d has no network %s token info", assetID, contractVer, net)
	}
	return token, addrs.Address, contract.Address, nil
}
