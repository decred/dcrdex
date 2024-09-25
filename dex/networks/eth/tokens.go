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
	usdcTokenID: {
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: EthBipID,
			Name:     "USDC",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "µUSD",
				Conventional: dex.Denomination{
					Unit:             "USDC",
					ConversionFactor: 1e6,
				},
				Alternatives: []dex.Denomination{
					{
						Unit:             "cents",
						ConversionFactor: 1e2,
					},
				},
				FeeRateDenom: "gas",
			},
		},
		NetTokens: map[dex.Network]*NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), // https://etherscan.io/address/0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48
				SwapContracts: map[uint32]*SwapContract{
					0: { // https://etherscan.io/address/0x1bbd020ddd6dc01f974aa74d2d727b2a6782f32d#code
						Address: common.HexToAddress("0x1bbd020DDD6dc01f974Aa74D2D727B2A6782F32D"),
						// USDC's contract is upgradable, using a proxy call, so
						// gas cost could change without notice, so we do not
						// want to set limits too low, even with live estimates.
						Gas: Gases{
							Swap:      242_000,
							SwapAdd:   146_400,
							Redeem:    102_700,
							RedeemAdd: 31_600,
							Refund:    77_000,
							Approve:   78_400,
							Transfer:  85_100,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238"),
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Address: common.HexToAddress("0xFDEF71277d4518Ca3373CA06562100FDB0050211"), // tx 0x29e0e146e8c956156f61e86b970de97eb8958bf10671018085dd4816c566a2a3
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							// First swap used 184413 gas Recommended Gases.Swap = 239736
							// 	2 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
							// 	[184413 297004 409595]
							// First redeem used 75659 gas. Recommended Gases.Redeem = 98356
							// 	2 additional redeems averaged 31617 gas each. recommended Gases.RedeemAdd = 41102
							// 	[75659 107276 138893]
							// Average of 3 refunds: 61022. Recommended Gases.Refund = 79328
							// 	[60829 61410 60829]
							// Average of 2 approvals: 55773. Recommended Gases.Approve = 72504
							// 	[55773 55773]
							// Average of 1 transfers: 62135. Recommended Gases.Transfer = 80775
							// 	[62135]
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
							//
							// Then buffered by about 30%...
							Swap:      242_000, // actual ~187,880 -- https://goerli.etherscan.io/tx/0x352baccafa96bb09d5c118f8dcce26e34267beb8bcda9c026f8d5353abea50fd, verified on mainnet at 188,013 gas
							SwapAdd:   146_400, // actual ~112,639 (300,519 for 2) -- https://goerli.etherscan.io/tx/0x97f9a1ed69883a6e701f37883ef74d79a709e0edfc4a45987fa659700663f40e
							Redeem:    109_000, // actual ~83,850 (initial receive, subsequent ~79,012) -- https://goerli.etherscan.io/tx/0x96f007036b01eb2e44615dc67d3e99748bc133496187348b2af26834f46bfdc8, verified on mainnet at 79,113 gas for subsequent
							RedeemAdd: 41_102,  // actual ~31,641 (110,653 for 2) -- https://goerli.etherscan.io/tx/0xcf717512796868273ed93c37fa139973c9b8305a736c4a3b50ac9f35ae747f99
							Refund:    79_328,  // actual ~59,152 -- https://goerli.etherscan.io/tx/0xc5692ad0e6d86b721af75ff3b4b7c2e17d939918db030ebf5444ccf840c7a90b
							Approve:   78_400,  // actual ~60,190 (initial) -- https://goerli.etherscan.io/tx/0xd695fd174dede7bb798488ead7fed5ef33bcd79932b0fa35db0d17c84c97a8a1, verified on mainnet at 60,311
							Transfer:  85_100,  // actual ~65,524 (initial receive, subsequent 48,424)
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Address: common.Address{},
						Gas: Gases{
							Swap:      242_000,
							SwapAdd:   146_400,
							Redeem:    109_000,
							RedeemAdd: 31_600,
							Refund:    77_000,
							Approve:   78_400,
							Transfer:  85_100,
						}},
				},
			},
		},
	},
	usdtTokenID: {
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: EthBipID,
			Name:     "Tether",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "microUSD",
				Conventional: dex.Denomination{
					Unit:             "USDT",
					ConversionFactor: 1e6,
				},
			},
		},
		NetTokens: map[dex.Network]*NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0xdac17f958d2ee523a2206206994597c13d831ec7"), // https://etherscan.io/address/0xdac17f958d2ee523a2206206994597c13d831ec7
				SwapContracts: map[uint32]*SwapContract{
					0: {
						// swap contract: https://etherscan.io/address/0x97a53fEF7854f4CB846F2eaCCf847229F1E10e4f
						Address: common.HexToAddress("0x97a53fEF7854f4CB846F2eaCCf847229F1E10e4f"),
						// USDT's contract is upgradable, using a proxy call, so
						// gas cost could change without notice, so we do not
						// want to set limits too low, even with live estimates.
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							// First swap used 181405 gas Recommended Gases.Swap = 235826
							//  1 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
							//  [181405 293996]
							// First redeem used 76487 gas. Recommended Gases.Redeem = 99433
							//  1 additional redeems averaged 31617 gas each. recommended Gases.RedeemAdd = 41102
							//  [76487 108104]
							// Average of 2 refunds: 61963. Recommended Gases.Refund = 80551
							//  [61684 62242]
							// Average of 2 approvals: 48897. Recommended Gases.Approve = 63566
							//  [48897 48897]
							// Average of 1 transfers: 63173. Recommended Gases.Transfer = 82124
							//  [63173]
							Swap:      235_826,
							SwapAdd:   146_368,
							Redeem:    99_433,
							RedeemAdd: 41_102,
							Refund:    80_551,
							Approve:   63_566,
							Transfer:  82_124,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x7169D38820dfd117C3FA1f22a697dBA58d90BA06"),
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Address: common.HexToAddress("0x97a53fEF7854f4CB846F2eaCCf847229F1E10e4f"),
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							// First swap used 181441 gas Recommended Gases.Swap = 235873
							//   4 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
							//   [181441 294032 406623 519202 631805]
							// First redeem used 76530 gas. Recommended Gases.Redeem = 99489
							//   4 additional redeems averaged 31626 gas each. recommended Gases.RedeemAdd = 41113
							//   [76530 108159 139800 171418 203035]
							// Average of 5 refunds: 62183. Recommended Gases.Refund = 80837
							//   [61739 62297 62297 62285 62297]
							// Average of 2 approvals: 48930. Recommended Gases.Approve = 63609
							//   [48930 48930]
							// Average of 1 transfers: 63228. Recommended Gases.Transfer = 82196
							//   [63228]
							Swap:      235_873,
							SwapAdd:   146_368,
							Redeem:    99_489,
							RedeemAdd: 41_113,
							Refund:    80_837,
							Approve:   63_609,
							Transfer:  82_196,
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*SwapContract{
					0: {
						Address: common.Address{},
						Gas: Gases{
							Swap:      242_000,
							SwapAdd:   146_400,
							Redeem:    109_000,
							RedeemAdd: 31_600,
							Refund:    77_000,
							Approve:   78_400,
							Transfer:  85_100,
						}},
				},
			},
		},
	},
	maticTokenID: {
		Token: &dex.Token{
			ParentID: EthBipID,
			Name:     "MATIC",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "gwei",
				Conventional: dex.Denomination{
					Unit:             "MATIC",
					ConversionFactor: 1e9,
				},
				Alternatives: []dex.Denomination{
					{
						Unit:             "Szabos",
						ConversionFactor: 1e6,
					},
					{
						Unit:             "Finneys",
						ConversionFactor: 1e3,
					},
				},
				FeeRateDenom: "gas",
			},
		},
		NetTokens: map[dex.Network]*NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0x7d1afa7b718fb893db30a3abc0cfc608aacfebb0"), // https://etherscan.io/address/0x7d1afa7b718fb893db30a3abc0cfc608aacfebb0
				SwapContracts: map[uint32]*SwapContract{
					0: {
						// swap contract: https://etherscan.io/address/0x9572727D79FD074D3Ac731c584bf51dCF7459C12
						Address: common.HexToAddress("0x9572727D79FD074D3Ac731c584bf51dCF7459C12"),
						Gas: Gases{
							// Results from client's GetGasEstimates.
							//
							//	First swap used 178058 gas Recommended Gases.Swap = 231475
							//	 1 additional swaps averaged 112627 gas each. Recommended Gases.SwapAdd = 146415
							//		 178058 290685]
							// First redeem used 67456 gas. Recommended Gases.Redeem = 87692
							//	 1 additional redeems averaged 31641 gas each. recommended Gases.RedeemAdd = 41133
							//		 67456 99097]
							// Average of 2 refunds: 52875. Recommended Gases.Refund = 68737
							//		 52563 53187]
							// Average of 2 approvals: 48764. Recommended Gases.Approve = 63393
							//		 48764 48764]
							// Average of 1 transfers: 53944. Recommended Gases.Transfer = 70127
							//		 53944]

							Swap:      231_475,
							SwapAdd:   146_415,
							Redeem:    87_692,
							RedeemAdd: 41_133,
							Refund:    68_737,
							Approve:   63_393,
							Transfer:  70_127,
						},
					},
				},
			},
		},
	},
}

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	MaybeReadSimnetAddrsDir("eth", ContractAddresses, MultiBalanceAddresses, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}

func MaybeReadSimnetAddrsDir(
	dir string,
	contractsAddrs map[uint32]map[dex.Network]common.Address,
	multiBalandAddresses map[dex.Network]common.Address,
	usdcToken *NetToken,
	usdtToken *NetToken,
) {

	usr, err := user.Current()
	if err != nil {
		return
	}

	harnessDir := filepath.Join(usr.HomeDir, "dextest", dir)
	fi, err := os.Stat(harnessDir)
	if err != nil {
		return
	}
	if !fi.IsDir() {
		return
	}

	ethSwapContractAddrFile := filepath.Join(harnessDir, "eth_swap_contract_address.txt")
	testUSDCSwapContractAddrFile := filepath.Join(harnessDir, "usdc_swap_contract_address.txt")
	testUSDCContractAddrFile := filepath.Join(harnessDir, "test_usdc_contract_address.txt")
	testUSDTSwapContractAddrFile := filepath.Join(harnessDir, "usdt_swap_contract_address.txt")
	testUSDTContractAddrFile := filepath.Join(harnessDir, "test_usdt_contract_address.txt")
	multiBalanceContractAddrFile := filepath.Join(harnessDir, "multibalance_address.txt")

	contractsAddrs[0][dex.Simnet] = maybeGetContractAddrFromFile(ethSwapContractAddrFile)
	multiBalandAddresses[dex.Simnet] = maybeGetContractAddrFromFile(multiBalanceContractAddrFile)

	usdcToken.SwapContracts[0].Address = maybeGetContractAddrFromFile(testUSDCSwapContractAddrFile)
	usdcToken.Address = maybeGetContractAddrFromFile(testUSDCContractAddrFile)

	usdtToken.SwapContracts[0].Address = maybeGetContractAddrFromFile(testUSDTSwapContractAddrFile)
	usdtToken.Address = maybeGetContractAddrFromFile(testUSDTContractAddrFile)
}

func maybeGetContractAddrFromFile(fileName string) (addr common.Address) {
	addrBytes, err := os.ReadFile(fileName)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Printf("error reading contract address: %v \n", err)
		return
	}
	addrLen := len(addrBytes)
	if addrLen == 0 {
		fmt.Printf("no contract address found at %v \n", fileName)
		return
	}
	addrStr := string(addrBytes[:addrLen-1])
	return common.HexToAddress(addrStr)
}
