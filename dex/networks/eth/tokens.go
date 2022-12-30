// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"math/big"
	"os"
	"os/user"
	"path/filepath"

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
			dex.Mainnet: { // no dextt on mainnet
				Address:       common.Address{},
				SwapContracts: map[uint32]*SwapContract{},
			},
			dex.Testnet: { // no dextt on goerli
				Address:       common.Address{},
				SwapContracts: map[uint32]*SwapContract{},
			},
			dex.Simnet: {
				// ERC20 token contract address. The simnet harness writes this
				// address to file. Live tests must populate this field.
				Address: common.Address{},
				SwapContracts: map[uint32]*SwapContract{
					0: {
						// Swap contract address. The simnet harness writes this
						// address to file. Live tests must populate this field.
						Address: common.Address{},
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							// First swap used 171756 gas
							//   4 additional swaps averaged 112607 gas each
							//   [171756 284366 396976 509586 622184]
							// First redeem used 63214 gas
							//   4 additional redeems averaged 31641 gas each
							//   [63214 94858 126502 158135 189779]
							// Average of 5 refunds: 48127
							//   [48127 48127 48127 48127 48127]
							//
							// Approve is the gas used to call the approve
							// method of the contract. For Approve transactions,
							// the very first approval for an account-spender
							// pair takes more than subsequent approvals. The
							// results are repeated for a different account's
							// first approvals on the same contract, so it's not
							// just the global first.
							// Average of 5 approvals: 27365
							//   [44465 27365 27365 27365 27365]
							//
							// The first transfer to an address the contract has
							// not seen before will insert a new key into the
							// contract's token map. The amount of extra gas
							// this consumes seems to depend on the size of the
							// map and is not noticeable on simnet.
							// Average of 5 transfers: 32540
							//   [32540 32540 32540 32540 32540]
							Swap:      174_000,
							SwapAdd:   115_000,
							Redeem:    70_000,
							RedeemAdd: 33_000,
							Refund:    50_000,
							Approve:   46_000,
							Transfer:  35_000,
						},
					},
				},
			},
		},
	},
	usdcTokenID: {
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: EthBipID,
			Name:     "USDC",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "microUSD",
				Conventional: dex.Denomination{
					Unit:             "USDC",
					ConversionFactor: 1e6,
				},
			},
		},
		NetTokens: map[dex.Network]*NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), // https://etherscan.io/address/0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48
				SwapContracts: map[uint32]*SwapContract{
					0: { // https://etherscan.io/address/0x1bbd020ddd6dc01f974aa74d2d727b2a6782f32d#code
						Address: common.HexToAddress("0x1bbd020DDD6dc01f974Aa74D2D727B2A6782F32D"),
						Gas: Gases{
							Swap:      175_000,
							SwapAdd:   115_000,
							Redeem:    87_000,
							RedeemAdd: 33_000,
							Refund:    67_000,
							Approve:   65_000,
							Transfer:  70_000,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x07865c6e87b9f70255377e024ace6630c1eaa37f"),
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Address: common.HexToAddress("0xA7Af47DB3296206eA543A82ffBF7Fc312698E6C9"),
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							// First swap used 170853 gas
							//   4 additional swaps averaged 112583 gas each
							//   [170853 283439 396025 508563 621185]
							// First redeem used 83874 gas
							//   4 additional redeems averaged 31641 gas each
							//   [83874 115518 147138 178747 210439]
							// Average of 5 refunds: 64334
							//   [64337 64337 64337 64337 64325]
							//
							// Approve is the gas used to call the approve
							// method of the contract. For Approve transactions,
							// the very first approval for an account-spender
							// pair takes more than subsequent approvals. The
							// results are repeated for a different account's
							// first approvals on the same contract, so it's not
							// just the global first.
							// Average of 5 approvals: 46222
							//   [59902 42802 42802 42802 42802]
							//
							//
							// The first transfer to an address the contract has
							// not seen before will insert a new key into the
							// contract's token map. The amount of extra gas
							// this consumes seems to depend on the size of the
							// map.
							// Average of 5 transfers: 51820
							//   [65500 48400 48400 48400 48400]
							Swap:      175_000,
							SwapAdd:   115_000,
							Redeem:    87_000,
							RedeemAdd: 33_000,
							Refund:    67_000,
							Approve:   65_000,
							Transfer:  70_000,
						},
					},
				},
			},
			dex.Simnet: { // no usdc on simnet, dextt instead
				Address:       common.Address{},
				SwapContracts: map[uint32]*SwapContract{},
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

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	usr, err := user.Current()
	if err != nil {
		return
	}

	ethPath := filepath.Join(usr.HomeDir, "dextest", "eth")
	fi, err := os.Stat(ethPath)
	if err != nil {
		return
	}
	if !fi.IsDir() {
		return
	}

	ethSwapContractAddrFile := filepath.Join(ethPath, "eth_swap_contract_address.txt")
	tokenSwapContractAddrFile := filepath.Join(ethPath, "erc20_swap_contract_address.txt")
	testTokenContractAddrFile := filepath.Join(ethPath, "test_token_contract_address.txt")

	ContractAddresses[0][dex.Simnet] = getContractAddrFromFile(ethSwapContractAddrFile)

	token := Tokens[testTokenID].NetTokens[dex.Simnet]
	token.SwapContracts[0].Address = getContractAddrFromFile(tokenSwapContractAddrFile)
	token.Address = getContractAddrFromFile(testTokenContractAddrFile)
}

func getContractAddrFromFile(fileName string) (addr common.Address) {
	addrBytes, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Printf("error reading contract address: %v \n", err)
		return
	}
	addrLen := len(addrBytes)
	if addrLen == 0 {
		fmt.Printf("no contract address found at %v \n", fileName)
	}
	addrStr := string(addrBytes[:addrLen-1])
	return common.HexToAddress(addrStr)
}
