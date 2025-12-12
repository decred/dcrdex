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

	// TODO: Fix these. Taken from polygon.
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
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Simnet: common.HexToAddress(""), // Filled in by MaybeReadSimnetAddrs
		},
		1: {
			dex.Testnet: common.HexToAddress("0x4F8024dd716E4ec9AC8E0CD971616F73e3f6625d"), // txid: https://base-sepolia.blockscout.com/tx/0xa8137a8f84b28149f27045a4d6c15cceb784f3886f8a476790002e4da4728979
			dex.Mainnet: common.HexToAddress(""),
			dex.Simnet:  common.HexToAddress(""), // Filled in by MaybeReadSimnetAddrs
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
				Address:       common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
			},
			dex.Testnet: {
				Address:       common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
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
				Address:       common.HexToAddress("0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
			},
			dex.Testnet: {
				// Is USDCT tether?
				Address:       common.HexToAddress("0xb72fdb9f8190d8e1141e6a8e9c0732b0f4d93c09"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
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
				Address:       common.HexToAddress("0x0555E30da8f98308EdB960aa94C0Db47230d2B9c"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
			},
			dex.Testnet: {
				Address:       common.HexToAddress("0x78c8587b0b4d50b3a2110bc8188eef195cfa7f11"),
				SwapContracts: map[uint32]*dexeth.SwapContract{},
			},
			dex.Simnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{0: {}, 1: {}},
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
	dexeth.MaybeReadSimnetAddrsDir("base", ContractAddresses, MultiBalanceAddresses, EntryPoints, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
