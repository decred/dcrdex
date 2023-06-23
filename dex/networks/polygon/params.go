// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package polygon

import (
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
)

const (
	PolygonBipID = 966
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "gwei",
		Conventional: dex.Denomination{
			Unit:             "MATIC",
			ConversionFactor: 1e9,
		},
	}

	v0Gases = &dexeth.Gases{
		Swap:      174500,
		SwapAdd:   146400,
		Redeem:    78600,
		RedeemAdd: 41000,
		Refund:    57000,
	}

	VersionedGases = map[uint32]*dexeth.Gases{
		0: v0Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			// dex.Mainnet: common.HexToAddress(""),
			// dex.Testnet: common.HexToAddress(""),
			dex.Simnet: common.HexToAddress(""), // Filled in by MaybeReadSimnetAddrs
		},
	}

	testTokenID, _ = dex.BipSymbolID("dextt.polygon")
	usdcTokenID, _ = dex.BipSymbolID("usdc.polygon")

	Tokens = map[uint32]*dexeth.Token{
		testTokenID: {
			Token: &dex.Token{
				ParentID: PolygonBipID,
				Name:     "DCRDEXTestToken",
				UnitInfo: dex.UnitInfo{
					AtomicUnit: "Dextoshi",
					Conventional: dex.Denomination{
						Unit:             "DEXTT",
						ConversionFactor: dexeth.GweiFactor,
					},
				},
			},
			NetTokens: map[dex.Network]*dexeth.NetToken{
				dex.Simnet: {
					// ERC20 token contract address. The simnet harness writes this
					// address to file. Live tests must populate this field.
					Address: common.Address{},
					SwapContracts: map[uint32]*dexeth.SwapContract{
						0: {
							Address: common.Address{}, // Set in MaybeReadSimnetAddrs
							Gas: dexeth.Gases{
								// 	First swap used 171664 gas Recommended Gases.Swap = 223163
								// 	4 additional swaps averaged 112615 gas each. Recommended Gases.SwapAdd = 146399
								// 	[171664 284279 396882 509509 622124]
								Swap:    223_163,
								SwapAdd: 146_399,
								// First redeem used 63170 gas. Recommended Gases.Redeem = 82121
								// 	4 additional redeems averaged 31626 gas each. recommended Gases.RedeemAdd = 41113
								// 	[63170 94799 126416 158058 189675]
								Redeem:    82_121,
								RedeemAdd: 41_113,
								// Average of 5 refunds: 48098. Recommended Gases.Refund = 62527
								// 	[48098 48098 48098 48098 48098]
								Refund: 62_527,
								// Average of 2 approvals: 44754. Recommended Gases.Approve = 58180
								// 	[44754 44754]
								Approve: 58_180,
								// Average of 1 transfers: 49646. Recommended Gases.Transfer = 64539
								// 	[49646]
								Transfer: 64_539,
							},
						},
					},
				},
			},
		},

		usdcTokenID: {
			EVMFactor: new(int64),
			Token: &dex.Token{
				ParentID: PolygonBipID,
				Name:     "USDC",
				UnitInfo: dex.UnitInfo{
					AtomicUnit: "microUSD",
					Conventional: dex.Denomination{
						Unit:             "USDC",
						ConversionFactor: 1e6,
					},
				},
			},
			NetTokens: map[dex.Network]*dexeth.NetToken{
				dex.Mainnet: {
					Address: common.HexToAddress("0x2791bca1f2de4661ed88a30c99a7a9449aa84174"), // https://polygonscan.com/address/0x2791bca1f2de4661ed88a30c99a7a9449aa84174
					SwapContracts: map[uint32]*dexeth.SwapContract{
						0: {
							// deploy tx: https://polygonscan.com/tx/0xe5a22c2c5c9a216dd648f12d1654c0e3a4e8d541f62eefe2a938cfe015826ccb
							// swap contract: https://polygonscan.com/address/0x73bc803A2604b2c58B8680c3CE1b14489842EF16
							Address: common.HexToAddress("0x73bc803A2604b2c58B8680c3CE1b14489842EF16"),
							Gas: dexeth.Gases{
								// First swap used 187794 gas Recommended Gases.Swap = 244132
								// 	4 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
								// 	[187794 300385 412976 525567 638158]
								Swap:    244_132,
								SwapAdd: 146_368,
								// First redeem used 77040 gas. Recommended Gases.Redeem = 100152
								// 	4 additional redeems averaged 31629 gas each. recommended Gases.RedeemAdd = 41117
								// 	[77040 108669 140298 171928 203557]
								Redeem:    100_152,
								RedeemAdd: 41_117,
								// Average of 5 refunds: 69474. Recommended Gases.Refund = 90316
								// 	[69474 69474 69474 69474 69474]
								Refund: 90_316,
								// Average of 2 approvals: 58446. Recommended Gases.Approve = 75979
								// 	[58446 58446]
								Approve: 75_979,
								// Average of 1 transfers: 63504. Recommended Gases.Transfer = 82555
								// 	[63504]
								Transfer: 82_555,
							},
						},
					},
				},
				dex.Testnet: {
					Address: common.HexToAddress("0x0fa8781a83e46826621b3bc094ea2a0212e71b23"), // https://polygonscan.com/address/0x2791bca1f2de4661ed88a30c99a7a9449aa84174
					SwapContracts: map[uint32]*dexeth.SwapContract{
						0: {
							// deploy tx: https://polygonscan.com/tx/0xfc27a89e5befba05df5ebe64670d5df89f635957f3fbeb2d4240cd3f0e540022
							// swap contract: https://polygonscan.com/address/0x73bc803A2604b2c58B8680c3CE1b14489842EF16
							Address: common.HexToAddress("0x73bc803A2604b2c58B8680c3CE1b14489842EF16"),
							Gas: dexeth.Gases{
								// First swap used 187982 gas Recommended Gases.Swap = 244376
								// 	4 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
								// 	[187982 300573 413152 525755 638346]
								Swap:    244_376,
								SwapAdd: 146_368,
								// First redeem used 77229 gas. Recommended Gases.Redeem = 100397
								// 	4 additional redeems averaged 31629 gas each. recommended Gases.RedeemAdd = 41117
								// 	[77229 108858 140475 172105 203746]
								Redeem:    100_397,
								RedeemAdd: 41_117,
								// Average of 5 refunds: 69660. Recommended Gases.Refund = 90558
								// 	[69663 69663 69651 69663 69663]
								Refund: 90_558,
								// Average of 2 approvals: 58634. Recommended Gases.Approve = 76224
								// 	[58634 58634]
								Approve: 76_224,
								// Average of 1 transfers: 63705. Recommended Gases.Transfer = 82816
								// 	[63705]
								Transfer: 82_816,
							},
						},
					},
				},
			},
		},
	}
)

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	dexeth.MaybeReadSimnetAddrsDir("polygon", ContractAddresses, Tokens[testTokenID].NetTokens[dex.Simnet])
}
