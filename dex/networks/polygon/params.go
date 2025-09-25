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
			Unit:             "POL",
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
	}

	// First swap used 134434 gas Recommended Gases.Swap = 174764
	//   4 additional swaps averaged 112609 gas each. Recommended Gases.SwapAdd = 146391
	//   [134434 247061 359676 472279 584870]
	// First redeem used 60454 gas. Recommended Gases.Redeem = 78590
	//   4 additional redeems averaged 31623 gas each. recommended Gases.RedeemAdd = 41109
	//   [60454 92095 123724 155329 186946]
	// Average of 5 refunds: 42707. Recommended Gases.Refund = 55519
	//   [42700 42712 42712 42712 42700]
	v0Gases = &dexeth.Gases{
		Swap:      174_000, // 134_482 https://polygonscan.com/tx/0xd568d6c832d0a96dee25212e7b08643ba395459b5b0df20d99463ec0fbca575f
		SwapAdd:   146_000,
		Redeem:    78_000, // 60_466 https://polygonscan.com/tx/0xf671574a711b4bc31daa1431dcf029818d6b5eb2276f4205ff17f58b66d85605
		RedeemAdd: 41_000,
		Refund:    55_000,
	}

	v1Gases = &dexeth.Gases{
		// First swap used 48801 gas Recommended Gases.Swap = 63441
		Swap: 63_441,
		// 	4 additional swaps averaged 26695 gas each. Recommended Gases.SwapAdd = 34703
		// 	[48801 75511 102209 128895 155582]
		SwapAdd: 34_703,
		// First redeem used 40032 gas. Recommended Gases.Redeem = 52041
		Redeem: 52_041,
		// 	4 additional redeems averaged 10950 gas each. recommended Gases.RedeemAdd = 14235
		// 	[40032 50996 61949 72890 83832]
		RedeemAdd: 14_235,
		// *** Compare expected Swap + Redeem = 88k with UniSwap v2: 102k, v3: 127k
		// *** A 1-match order is cheaper than UniSwap.
		// Average of 5 refunds: 40390. Recommended Gases.Refund = 52507
		// 	[40381 40393 40393 40393 40393]
		Refund: 52_507,

		GaslessRedeemVerification:       83_000,
		GaslessRedeemVerificationAdd:    11_000,
		GaslessRedeemPreVerification:    70_000,
		GaslessRedeemPreVerificationAdd: 6_000,
		GaslessRedeemCall:               120_000,
		GaslessRedeemCallAdd:            13_000,
	}

	VersionedGases = map[uint32]*dexeth.Gases{
		0: v0Gases,
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Mainnet: common.HexToAddress("0x74922db9f530b22d7ebea8e6920e256cf3cc0542"), // txid: 0x9ef5e128119da8e66d364b7e656758ea7232d2dfb14c8463abac1f5ac62c986f
			dex.Testnet: common.HexToAddress("0x73bc803A2604b2c58B8680c3CE1b14489842EF16"), // txid: 0x88f656a8e432fdd50f33e67bdc39a66d24f663e33792bfab16b033dd2c609a99
			dex.Simnet:  common.HexToAddress(""),                                           // Filled in by MaybeReadSimnetAddrs
		},
		1: {
			dex.Mainnet: common.HexToAddress("0xcb9B5AD64FD3fc20215f744293d95887c888B8a5"), // txid: 0x35e5318f3b91b9890a59b0907c6fe9603cc46651111ee18e4df142c7a39cdc10
			dex.Testnet: common.HexToAddress("0x47f50Aea57221F591aCdD5C61EcBCc4250F15937"), // txid: 0xf582a60ff370f24e5c2836feeddbf586635282dd7d4e5e972d3d8ca503b47e0e
			dex.Simnet:  common.HexToAddress(""),                                           // Filled in by MaybeReadSimnetAddrs
		},
	}

	MultiBalanceAddresses = map[dex.Network]common.Address{
		dex.Mainnet: common.HexToAddress("0x23d8203d8E3c839F359bcC85BFB71cf0d707EDF0"), // tx: 0xc593222106c700b153977fdf290f8d9656610cd2dd88522724e85b3f7fd600cf
		dex.Testnet: common.HexToAddress("0xa958d5B8a3a29E3f5f41742Fbb939A0dd93EB418"), // tx 0x692cf15b145cb45c0098bedf8a55d067b1ac994973bb62000c046b8453d8b624
	}

	usdcTokenID, _ = dex.BipSymbolID("usdc.polygon")
	usdtTokenID, _ = dex.BipSymbolID("usdt.polygon")
	wethTokenID, _ = dex.BipSymbolID("weth.polygon")
	wbtcTokenID, _ = dex.BipSymbolID("wbtc.polygon")

	Tokens = map[uint32]*dexeth.Token{
		usdcTokenID: TokenUSDC,
		usdtTokenID: TokenUSDT,
		wethTokenID: TokenWETH,
		wbtcTokenID: TokenWBTC,
	}

	TokenUSDC = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: PolygonBipID,
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
		NetTokens: map[dex.Network]*dexeth.NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0x3c499c542cef5e3811e1192ce70d8cc03d5c3359"), // https://polygonscan.com/address/0x3c499c542cef5e3811e1192ce70d8cc03d5c3359
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						// deploy tx: https://polygonscan.com/tx/0x6e8b2a7f1f81ff8c4638937ff1f75474211b5e6c7418899f6c935ad823cdbdca
						// swap contract: https://polygonscan.com/address/0x1C152f7f91E03BcA4B0000Be9f694A62A9548e7B
						Address: common.HexToAddress("0x1C152f7f91E03BcA4B0000Be9f694A62A9548e7B"),
						Gas: dexeth.Gases{
							// First swap used 187856 gas Recommended Gases.Swap = 244212
							// 	2 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
							// 	[187856 300435 413038]
							Swap:    244_212,
							SwapAdd: 146_368,
							// First redeem used 79024 gas. Recommended Gases.Redeem = 102731
							// 	2 additional redeems averaged 31623 gas each. recommended Gases.RedeemAdd = 41109
							// 	[79024 110641 142270]
							Redeem:    102_731,
							RedeemAdd: 41_109,
							// Average of 3 refunds: 64298. Recommended Gases.Refund = 83587
							// 	[64302 64290 64302]
							Refund: 83_587,
							// Average of 2 approvals: 60166. Recommended Gases.Approve = 78215
							// 	[60166 60166]
							Approve: 78_215,
							// Average of 1 transfers: 65500. Recommended Gases.Transfer = 85150
							// 	[65500]
							Transfer: 85_150,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 98322 gas Recommended Gases.Swap = 127818
							// 	1 additional swaps averaged 26503 gas each. Recommended Gases.SwapAdd = 34453
							// 	[98322 124825]
							// First redeem used 54684 gas. Recommended Gases.Redeem = 71089
							// 	1 additional redeems averaged 10722 gas each. recommended Gases.RedeemAdd = 13938
							// 	[54684 65406]
							// Average of 2 refunds: 60205. Recommended Gases.Refund = 78266
							// 	[60205 60205]
							// Average of 2 approvals: 55785. Recommended Gases.Approve = 72520
							// 	[55785 55785]
							// Average of 1 transfers: 62135. Recommended Gases.Transfer = 80775
							// 	[62135]
							Swap:      127_818,
							SwapAdd:   34_453,
							Redeem:    71_089,
							RedeemAdd: 13_938,
							Refund:    78_266,
							Approve:   72_520,
							Transfer:  80_775,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582"), // https://amoy.polygonscan.com/address/0x41E94Eb019C0762f9Bfcf9Fb1E58725BfB0e7582#readContract
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						// deploy tx: https://amoy.polygonscan.com/tx/0x61315c5cf90fdf2cd4a89d04cce6496c9ee7a2ecc18dbc5f76e4baacc80bda5e
						// swap contract: https://amoy.polygonscan.com/address/0xca70D818ffff2Cd235Ab90b4fec3e6394014D294
						Address: common.HexToAddress("0xca70D818ffff2Cd235Ab90b4fec3e6394014D294"),
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
							Refund: 90_558, // On Amaoy recommended was actually 79067
							// Average of 2 approvals: 58634. Recommended Gases.Approve = 76224
							// 	[58634 58634]
							Approve: 76_224,
							// Average of 1 transfers: 63705. Recommended Gases.Transfer = 82816
							// 	[63705]
							Transfer: 82_816,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 98322 gas Recommended Gases.Swap = 127818
							// 	2 additional swaps averaged 26495 gas each. Recommended Gases.SwapAdd = 34443
							// 	[98322 124825 151313]
							// First redeem used 54684 gas. Recommended Gases.Redeem = 71089
							// 	2 additional redeems averaged 10708 gas each. recommended Gases.RedeemAdd = 13920
							// 	[54684 65406 76100]
							// Average of 3 refunds: 57705. Recommended Gases.Refund = 75016
							// 	[57705 57705 57705]
							// Average of 2 approvals: 55785. Recommended Gases.Approve = 72520
							// 	[55785 55785]
							// Average of 1 transfers: 62135. Recommended Gases.Transfer = 80775
							// 	[62135]
							Swap:      127_818,
							SwapAdd:   34_443,
							Redeem:    71_089,
							RedeemAdd: 13_920,
							Refund:    75_016,
							Approve:   72_520,
							Transfer:  80_775,
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						Address: common.Address{}, // Set in MaybeReadSimnetAddrs
						Gas: dexeth.Gases{
							Swap:      223_163,
							SwapAdd:   146_399,
							Redeem:    82_121,
							RedeemAdd: 41_113,
							Refund:    62_527,
							Approve:   58_180,
							Transfer:  64_539,
						},
					},
					1: {
						Gas: dexeth.Gases{
							Swap:      114_515,
							SwapAdd:   34_672,
							Redeem:    58_272,
							RedeemAdd: 14_207,
							Refund:    61_911,
							Approve:   58_180,
							Transfer:  66_961,
						},
					},
				},
			},
		},
	}

	TokenUSDT = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: PolygonBipID,
			Name:     "Tether",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "microUSD",
				Conventional: dex.Denomination{
					Unit:             "USDT",
					ConversionFactor: 1e6,
				},
			},
		},
		NetTokens: map[dex.Network]*dexeth.NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0xc2132D05D31c914a87C6611C10748AEb04B58e8F"), // https://polygonscan.com/address/0xc2132D05D31c914a87C6611C10748AEb04B58e8F
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						// swap contract: https://polygonscan.com/address/0x97a53fEF7854f4CB846F2eaCCf847229F1E10e4f
						Address: common.HexToAddress("0x97a53fEF7854f4CB846F2eaCCf847229F1E10e4f"),
						// Results from client's GetGasEstimates.
						//
						// First swap used 181278 gas Recommended Gases.Swap = 235661
						//   4 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
						//   [181278 293869 406448 519051 631642]
						// First redeem used 70794 gas. Recommended Gases.Redeem = 92032
						//   4 additional redeems averaged 31629 gas each. recommended Gases.RedeemAdd = 41117
						//   [70794 102411 134028 165670 197311]
						// Average of 5 refunds: 63123. Recommended Gases.Refund = 82059
						//   [63126 63126 63114 63126 63126]
						// Average of 2 approvals: 52072. Recommended Gases.Approve = 67693
						//   [52072 52072]
						// Average of 1 transfers: 57270. Recommended Gases.Transfer = 74451
						//   [57270]
						Gas: dexeth.Gases{
							Swap:      235_661,
							SwapAdd:   146_368,
							Redeem:    122_032,
							RedeemAdd: 61_117,
							Refund:    82_059,
							Approve:   67_693,
							Transfer:  74_451,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 95187 gas Recommended Gases.Swap = 123743
							// 	1 additional swaps averaged 26503 gas each. Recommended Gases.SwapAdd = 34453
							// 	[95187 121690]
							// First redeem used 49819 gas. Recommended Gases.Redeem = 64764
							// 	1 additional redeems averaged 10722 gas each. recommended Gases.RedeemAdd = 13938
							// 	[49819 60541]
							// Average of 2 refunds: 62502. Recommended Gases.Refund = 81252
							// 	[62502 62502]
							// Average of 2 approvals: 52072. Recommended Gases.Approve = 67693
							// 	[52072 52072]
							// Average of 1 transfers: 57270. Recommended Gases.Transfer = 74451
							// 	[57270]
							Swap:      123_743,
							SwapAdd:   34_453,
							Redeem:    72_237, // using eth testnet value which is higher
							RedeemAdd: 13_928,
							Refund:    81_252,
							Approve:   67_693,
							Transfer:  82_180, // using eth testnet value which is higher
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						Address: common.Address{}, // Set in MaybeReadSimnetAddrs
						Gas: dexeth.Gases{
							Swap:      223_163,
							SwapAdd:   146_399,
							Redeem:    82_121,
							RedeemAdd: 41_113,
							Refund:    62_527,
							Approve:   58_180,
							Transfer:  64_539,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 95187 gas Recommended Gases.Swap = 123743
							// 	1 additional swaps averaged 26503 gas each. Recommended Gases.SwapAdd = 34453
							// 	[95187 121690]
							// First redeem used 49819 gas. Recommended Gases.Redeem = 64764
							// 	1 additional redeems averaged 10722 gas each. recommended Gases.RedeemAdd = 13938
							// 	[49819 60541]
							// Average of 2 refunds: 62502. Recommended Gases.Refund = 81252
							// 	[62502 62502]
							// Average of 2 approvals: 52072. Recommended Gases.Approve = 67693
							// 	[52072 52072]
							// Average of 1 transfers: 57270. Recommended Gases.Transfer = 74451
							// 	[57270]
							Swap:      123_743,
							SwapAdd:   34_453,
							Redeem:    72_237, // using eth testnet value which is higher
							RedeemAdd: 13_928,
							Refund:    81_252,
							Approve:   67_693,
							Transfer:  82_180, // using eth testnet value which is higher
						},
					},
				},
			},
		},
	}

	// TokenWETH is for Wrapped ETH.
	TokenWETH = &dexeth.Token{
		Token: &dex.Token{
			ParentID: PolygonBipID,
			Name:     "Wrapped Ether",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "gwei",
				Conventional: dex.Denomination{
					Unit:             "WETH",
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
		NetTokens: map[dex.Network]*dexeth.NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0x7ceb23fd6bc0add59e62ac25578270cff1b9f619"), // https://polygonscan.com/token/0x7ceb23fd6bc0add59e62ac25578270cff1b9f619
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						// deploy tx: https://polygonscan.com/tx/0xc569774add0a9f41eace3ff6289eafd4c17fbcaafcf8b7758e0a5c4d74dcf307
						// swap contract: https://polygonscan.com/address/0x878dF60d47Afa9C665dFaDCB6BF4e303C080032f
						Address: common.HexToAddress("0x878dF60d47Afa9C665dFaDCB6BF4e303C080032f"),
						Gas: dexeth.Gases{
							// First swap used 158846 gas Recommended Gases.Swap = 206499
							// 	4 additional swaps averaged 112618 gas each. Recommended Gases.SwapAdd = 146403
							// 	[158846 271461 384064 496691 609318]
							Swap:    206_499,
							SwapAdd: 146_403,
							// First redeem used 70222 gas. Recommended Gases.Redeem = 91288
							// 	4 additional redeems averaged 31629 gas each. recommended Gases.RedeemAdd = 41117
							// 	[70222 101851 133468 165110 196739]
							// Observed an 87,334 in the wild, so bumping this
							// a bit.
							Redeem:    104_800,
							RedeemAdd: 41_117,
							// Average of 5 refunds: 50354. Recommended Gases.Refund = 65460
							// 	[50350 50362 50338 50362 50362]
							Refund: 65_460,
							// 	[46712 26812]
							Approve: 56_054,
							// Average of 1 transfers: 51910. Recommended Gases.Transfer = 67483
							// 	[51910]
							Transfer: 67_483,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 89867 gas Recommended Gases.Swap = 116827
							// 	1 additional swaps averaged 26527 gas each. Recommended Gases.SwapAdd = 34485
							// 	[89867 116394]
							// First redeem used 44483 gas. Recommended Gases.Redeem = 57827
							// 	1 additional redeems averaged 10746 gas each. recommended Gases.RedeemAdd = 13969
							// 	[44483 55229]
							// Average of 2 refunds: 49766. Recommended Gases.Refund = 64695
							// 	[49766 49766]
							// Average of 2 approvals: 46712. Recommended Gases.Approve = 60725
							// 	[46712 46712]
							// Average of 1 transfers: 51910. Recommended Gases.Transfer = 67483
							// 	[51910]
							Swap:      116_827,
							SwapAdd:   34_485,
							Redeem:    57_827,
							RedeemAdd: 13_969,
							Refund:    64_695,
							Approve:   60_725,
							Transfer:  67_483,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x52ef3d68bab452a294342dc3e5f464d7f610f72e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// First swap used 89867 gas Recommended Gases.Swap = 116827
							// 	1 additional swaps averaged 26527 gas each. Recommended Gases.SwapAdd = 34485
							// 	[89867 116394]
							// First redeem used 44483 gas. Recommended Gases.Redeem = 57827
							// 	1 additional redeems averaged 10746 gas each. recommended Gases.RedeemAdd = 13969
							// 	[44483 55229]
							// Average of 2 refunds: 49766. Recommended Gases.Refund = 64695
							// 	[49766 49766]
							// Average of 2 approvals: 46712. Recommended Gases.Approve = 60725
							// 	[46712 46712]
							// Average of 1 transfers: 51910. Recommended Gases.Transfer = 67483
							// 	[51910]
							Swap:      116_827,
							SwapAdd:   34_485,
							Redeem:    57_827,
							RedeemAdd: 13_969,
							Refund:    64_695,
							Approve:   60_725,
							Transfer:  67_483,
						},
					},
				},
			},
		},
	}

	// TokenWBTC is for Wrapped BTC.
	TokenWBTC = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: PolygonBipID,
			Name:     "Wrapped Bitcoin",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "Sats",
				Conventional: dex.Denomination{
					Unit:             "WBTC",
					ConversionFactor: 1e8,
				},
				Alternatives: []dex.Denomination{
					{
						Unit:             "mWBTC",
						ConversionFactor: 1e5,
					},
					{
						Unit:             "µWBTC",
						ConversionFactor: 1e2,
					},
				},
				FeeRateDenom: "gas",
			},
		},
		NetTokens: map[dex.Network]*dexeth.NetToken{
			dex.Mainnet: {
				Address: common.HexToAddress("0x1bfd67037b42cf73acf2047067bd4f2c47d9bfd6"), // https://polygonscan.com/token/0x1bfd67037b42cf73acf2047067bd4f2c47d9bfd6
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {
						// deploy tx: https://polygonscan.com/tx/0xcd84a1fa2f890d5fc1fcb0dde6c5a3bb50d9b25927ec5666b96b5ad3d6902b0a
						// swap contract: https://polygonscan.com/address/0x625B7Ecd21B25b0808c4221dA281CD3A82f8b797
						Address: common.HexToAddress("0x625B7Ecd21B25b0808c4221dA281CD3A82f8b797"),
						Gas: dexeth.Gases{
							// First swap used 181278 gas Recommended Gases.Swap = 235661
							// 	4 additional swaps averaged 112591 gas each. Recommended Gases.SwapAdd = 146368
							// 	[181278 293857 406460 519039 631642]
							Swap:    235_661,
							SwapAdd: 146_368,
							// First redeem used 70794 gas. Recommended Gases.Redeem = 92032
							// 	4 additional redeems averaged 31629 gas each. recommended Gases.RedeemAdd = 41117
							// 	[70794 102399 134052 165646 197311]
							// Values around 92k were observed in the wild, so
							// this limit has been bumped up from the
							// recommendation.
							Redeem:    110_032,
							RedeemAdd: 41_117,
							// Average of 5 refunds: 63126. Recommended Gases.Refund = 82063
							// 	[63126 63126 63126 63126 63126]
							Refund: 82_063,
							// Average of 2 approvals: 52072. Recommended Gases.Approve = 67693
							// 	[52072 52072]
							Approve: 67_693,
							// Average of 1 transfers: 57270. Recommended Gases.Transfer = 74451
							// 	[57270]
							Transfer: 74_451,
						},
					},
					1: {
						Gas: dexeth.Gases{
							// First swap used 95187 gas Recommended Gases.Swap = 123743
							// 	1 additional swaps averaged 26503 gas each. Recommended Gases.SwapAdd = 34453
							// 	[95187 121690]
							// First redeem used 49819 gas. Recommended Gases.Redeem = 64764
							// 	1 additional redeems averaged 10722 gas each. recommended Gases.RedeemAdd = 13938
							// 	[49819 60541]
							// Average of 2 refunds: 62502. Recommended Gases.Refund = 81252
							// 	[62502 62502]
							// Average of 2 approvals: 52072. Recommended Gases.Approve = 67693
							// 	[52072 52072]
							// Average of 1 transfers: 57270. Recommended Gases.Transfer = 74451
							// 	[57270]
							Swap:      123_743,
							SwapAdd:   34_453,
							Redeem:    64_764,
							RedeemAdd: 13_938,
							Refund:    81_252,
							Approve:   67_693,
							Transfer:  74_451,
						},
					},
				},
			},
		},
	}
)

// EntryPoints is a map of network to the ERC-4337 entrypoint address.
// Currently only the v0.6 entrypoint is supported.
var EntryPoints = map[dex.Network]common.Address{
	// dex.Simnet:  common.Address{}, // populated by MaybeReadSimnetAddrs
	dex.Testnet: common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"),
}

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	dexeth.MaybeReadSimnetAddrsDir("polygon", ContractAddresses, MultiBalanceAddresses, EntryPoints, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
