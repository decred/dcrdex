// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package base

import (
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
)

const (
	BaseBipID = 8453 // weth.base
)

var (
	UnitInfo = dex.UnitInfo{
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
	}

	// Mainnet v1 swap evidence:
	//   init:            0x24bb1a2f39061f7476d2997f743a02baf28e7ce68e0274c25dc57cc64694d03e
	//   redeem:          0x8de1f2639448cc457673ee8750398efc33f035dc60035536dfc18033c02d35ff
	//   refund:          0x2c65a5ed943ac7460c306977641652c19f1c2c9782046f07f49df51ea5b4b3b5
	//   gasless redeem:  0x01ea080b31b2b81e185eed6d1f36c47bf518b6de50bc2a487954719c4ab5c629
	v1Gases = &dexeth.Gases{
		// Mainnet measurements:
		// Swaps (n=1..5):   [54262 83982 113702 143423 173145]
		Swap:    70_540,
		SwapAdd: 38_636,
		// Redeems (n=1..5): [44934 58300 71655 85023 98405]
		Redeem:    58_414,
		RedeemAdd: 17_377,
		// Refunds (n=1..6): [48032 48032 48032 48032 48032 42892]
		Refund: 61_327,

		// Gasless redeem (mainnet, v0.7 EntryPoint):
		// Verification (n=1..5): [176220 221701 273162 352264 394093]
		GaslessRedeemVerification:    229_086,
		GaslessRedeemVerificationAdd: 70_808,
		// PreVerification (n=1..5): [47217 49358 51584 53740 55976]
		GaslessRedeemPreVerification:    61_382,
		GaslessRedeemPreVerificationAdd: 2_845,
		// Call gas uses the contract's hard minimums from validateUserOp.
		// The EntryPoint passes callGasLimit directly to the inner call
		// (Exec.call), so the full amount is available to redeemAA.
		GaslessRedeemCall:    100_000, // MIN_CALL_GAS_BASE (75k) + MIN_CALL_GAS_PER_REDEMPTION (25k)
		GaslessRedeemCallAdd: 25_000,  // MIN_CALL_GAS_PER_REDEMPTION
	}

	VersionedGases = map[uint32]*dexeth.Gases{
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Simnet: common.HexToAddress(""), // Filled in by MaybeReadSimnetAddrs
		},
		1: {
			dex.Mainnet: common.HexToAddress("0x6971221538F885CF22A39Bb2cb1747FA2A4a34ba"), // txid: 0xd27ca1eb23b74fb41687b1a6573c3d8053d4ac20b061e01b9c1a7dfa7a6e3518
			dex.Testnet: common.HexToAddress("0x7F6130a86B6ffF6a9387004dBc0984C5FBB4d43F"), // txid: 0x47fadb3c3bfc25b8b0737593b2779770a52655a440f509769b614a10aae13518
			dex.Simnet:  common.HexToAddress(""),                                           // Filled in by MaybeReadSimnetAddrs
		},
	}

	MultiBalanceAddresses = map[dex.Network]common.Address{}

	usdcTokenID, _ = dex.BipSymbolID("usdc.base")
	usdtTokenID, _ = dex.BipSymbolID("usdt.base")
	wbtcTokenID, _ = dex.BipSymbolID("wbtc.base")

	Tokens = map[uint32]*dexeth.Token{
		usdcTokenID: TokenUSDC,
		usdtTokenID: TokenUSDT,
		wbtcTokenID: TokenWBTC,
	}

	TokenUSDC = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: BaseBipID,
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
				Address: common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {},
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
			ParentID: BaseBipID,
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
				Address: common.HexToAddress("0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Mainnet v1 token swap evidence:
							//   approve:   0x9b9e137d05f00a375f62eb644ddf06805716ddc4be5181669f61e39680cc5c64
							//   transfer:  0x8251856dbe1360b52ab040946d6590b293c89a8a4aa00cd247a46e57a881f3fa
							//   init:      0x3fb6a4bba5ce7a3b410b67a2488f83cfd497485c84ee99e2be5cf2665c7084fc
							//   redeem:    0x3a7e69928361c2523682530c4a90302607f4912d378a295d9851b844803bdbd1
							//   refund:    0xc0f051a4686e5eab54abd5b01f3a500a7b311ecbfb1113ad6f2881964dce01a9
							// Mainnet measurements:
							// Swaps (n=1):   [90132]
							// Redeems (n=1): [49548]
							// Refunds (n=1..2): [57628 47493]
							// Approvals: [46717 46717]
							// Transfers: [51814]
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x8d9cb8f3191fd685e2c14d2ac3fb2b16d44eafc3"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {},
					1: {
						Gas: dexeth.Gases{
							Swap:      123_743,
							SwapAdd:   34_453,
							Redeem:    72_237,
							RedeemAdd: 13_928,
							Refund:    81_252,
							Approve:   67_693,
							Transfer:  82_180,
						},
					},
				},
			},
		},
	}

	TokenWBTC = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: BaseBipID,
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
				Address: common.HexToAddress("0x0555E30da8f98308EdB960aa94C0Db47230d2B9c"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x78c8587b0b4d50b3a2110bc8188eef195cfa7f11"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      119_568,
							SwapAdd:   38_604,
							Redeem:    64_457,
							RedeemAdd: 17_342,
							Refund:    72_712,
							Approve:   60_732,
							Transfer:  67_342,
						},
					},
				},
			},
			dex.Simnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{0: {}, 1: {}},
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
	dexeth.MaybeReadSimnetAddrsDir("base", ContractAddresses, MultiBalanceAddresses, EntryPoints, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
