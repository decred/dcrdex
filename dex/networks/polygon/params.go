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

	// Mainnet v1 swap evidence:
	//   init:            0x4c6030c8682ad7f213830113c414c1625410b3196c35606b5eb15b9dcefdb927
	//   redeem:          0x4a41e0ce2450095631a53546db05a767e8731979bcf0f64edc1a00fc816a549f
	//   refund:          0x04738e930cd46cca4ec95e3652370f586b1c0627abbb385f609a238e5876f5be
	//   gasless redeem:  0xb9721c8663d7a209ded9459785f7ed3d4885623785490f0b2b78f9e93f4e1ffd
	v1Gases = &dexeth.Gases{
		// Mainnet measurements:
		// Swaps (n=1..5):   [54296 84005 113702 143412 173135]
		Swap:    70_584,
		SwapAdd: 38_621,
		// Redeems (n=1..5): [44911 58254 71586 84931 98290]
		Redeem:    58_384,
		RedeemAdd: 17_347,
		// Refunds (n=1..6): [47987 47987 47975 47975 47987 42847]
		Refund: 61_263,

		// Gasless redeem (testnet, v0.7 EntryPoint):
		// Mainnet bundler requires paymaster on Polygon. Using testnet values.
		// Verification (n=1..5): [117397 147644 181866 234477 262279]
		GaslessRedeemVerification:    152_616,
		GaslessRedeemVerificationAdd: 47_086,
		// PreVerification (n=1..5): [46968 49068 51204 53304 55428]
		GaslessRedeemPreVerification:    61_058,
		GaslessRedeemPreVerificationAdd: 2_749,
		// Call gas uses the contract's hard minimums from validateUserOp.
		// The EntryPoint passes callGasLimit directly to the inner call
		// (Exec.call), so the full amount is available to redeemAA.
		// Raw bundler estimates (n=1..5): [32197 43109 54023 58056 67762]
		GaslessRedeemCall:    100_000, // MIN_CALL_GAS_BASE (75k) + MIN_CALL_GAS_PER_REDEMPTION (25k)
		GaslessRedeemCallAdd: 25_000,  // MIN_CALL_GAS_PER_REDEMPTION
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
			dex.Mainnet: common.HexToAddress("0xeb88905501f4148Eed241fC75287035674403109"), // txid: 0x07fa8565d1f8916695cba7488fcaad778286d4a0a14a9406608f5838e4c3b6f7
			dex.Testnet: common.HexToAddress("0x6f338bF2D3DA244A464352AD13F1F01c4C1a47dC"), // txid: 0xcb9bbfae0825749e3da2ac9fa53497b17aedfa2276e6952618a02640de1d02d9
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
							// Swap/Redeem/Refund values are from usdt.polygon mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
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
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
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
							// Mainnet v1 token swap evidence:
							//   approve:   0xb5514d134c934f1cc5e5c15ca4bb1722c6ea894e2d8778d248482d05cb220631
							//   transfer:  0x12102938ace7237992fd462c38dcefd09493bb5a90c9919bbad525c1aae608e5
							//   init:      0x02ceb2ffc3052d91a91a0d59a6a807c7800fb76dce24a9951238ae0391204584
							//   redeem:    0x6aa85aa2f6c9af4214fb725f07e6b7c09e4cd7203575a2f284076cc9d74d722f
							//   refund:    0x8d003486899fa72fd80dbd8616133738d8fc9e1c189443095f1cd245798d9406
							// Mainnet measurements:
							// Swaps (n=1..5):   [105225 134897 164595 194281 223968]
							// Redeems (n=1..5): [57118 70425 83757 97055 110401]
							// Refunds (n=1..6): [69073 69073 69073 69073 69073 55051]
							// Approvals: [51998 51998]
							// Transfers: [59372]
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
							Approve:   67_597,
							Transfer:  77_183,
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
							// Swap/Redeem/Refund values are from usdt.polygon mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
							Approve:   67_693,
							Transfer:  74_451,
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
							// Swap/Redeem/Refund values are from usdt.polygon mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
							Approve:   49_428,
							Transfer:  67_233,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x52ef3d68bab452a294342dc3e5f464d7f610f72e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Testnet measurements (Amoy, 2026-02-20):
							// Swaps (n=1..5):   [95221 124929 154627 184349 214060]
							// Redeems (n=1..5): [49488 62819 76151 89521 102867]
							// Refunds (n=1..6): [57666 57430 57430 57430 57430 47421]
							// Approvals: [46572 29472]
							// Transfers: [51718]
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
							Approve:   49_428,
							Transfer:  67_233,
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
							// Swap/Redeem/Refund values are from usdt.polygon mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      136_792,
							SwapAdd:   38_590,
							Redeem:    74_253,
							RedeemAdd: 17_316,
							Refund:    86_756,
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
var EntryPoints = map[dex.Network]common.Address{
	// dex.Simnet:  common.Address{}, // populated by MaybeReadSimnetAddrs
	dex.Mainnet: dexeth.CanonicalEntryPointV07,
	dex.Testnet: dexeth.CanonicalEntryPointV07,
}

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	dexeth.MaybeReadSimnetAddrsDir("polygon", ContractAddresses, MultiBalanceAddresses, EntryPoints, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
