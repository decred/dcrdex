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
	//   init:            0x950dd233b2cf6440c7e6e46dcb915abb98e97a881f48bc051cfde79f73252789
	//   redeem:          0xf432a7e4aac1e962913c58088c0eff44514ae1f1dfb705445a123a0e21703404
	//   refund:          0x330e5c42837a681dbbd7b39f09f6b0a358ac833069f08e6adad38e8fb29c2a15
	//   gasless redeem:  0xc8a5fcf24d826d12030aef8b57b30eec3bc7d738e4953443c5d2db1083ec2aaf
	v1Gases = &dexeth.Gases{
		// Mainnet measurements:
		// Swaps (n=1..5):   [54296 83993 113714 143424 173123]
		Swap:    70_584,
		SwapAdd: 38_617,
		// Redeems (n=1..5): [44899 58242 71586 84943 98266]
		Redeem:    58_368,
		RedeemAdd: 17_343,
		// Refunds (n=1..6): [47987 47975 47987 47987 47975 42859]
		Refund: 61_266,

		// Gasless redeem (mainnet, v0.7 EntryPoint):
		// Verification (n=1..5): [117397 147644 181866 234477 262279]
		GaslessRedeemVerification:    152_616,
		GaslessRedeemVerificationAdd: 47_086,
		// PreVerification (n=1..5): [47211 49369 51541 53731 55894]
		GaslessRedeemPreVerification:    61_374,
		GaslessRedeemPreVerificationAdd: 2_821,
		// Call gas uses the contract's hard minimums from validateUserOp.
		// The EntryPoint passes callGasLimit directly to the inner call
		// (Exec.call), so the full amount is available to redeemAA.
		// Raw bundler estimates (n=1..5): [32197 43109 54023 58056 67762]
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
			dex.Testnet: common.HexToAddress("0xbeA9D54f2bD1F54e9130D17d834C1172eD314A39"), // txid: 0x89fdba013c832fced6cbac89b1057a400f4eb955bf174d102fba3517e78dbbbf
			dex.Mainnet: common.HexToAddress("0xf4D3c25017928c563A074C6c6880dC6787E19bE0"), // txid: 0x0384462e9bdd13b1233eef1982d8c5ed23ce0a79694b4ec0ca6c95c28764535d
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
							// Swap/Redeem/Refund values are from usdt.base mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   72_520,
							Transfer:  80_775,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Testnet measurements (Base Sepolia, 2026-02-20):
							// Swaps (n=1..5):   [103963 133635 163321 193019 222694]
							// Redeems (n=1..5): [59881 73188 86508 99830 113140]
							// Refunds (n=1..6): [68102 68004 68004 68016 68016 57814]
							// Approvals: [55785 55785]
							// Transfers: [62135]
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   72_520,
							Transfer:  80_775,
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
							//   approve:   0xfd8c5925cf4bf6c2e64478b789bd5dcb74ca822197239402d9636dfab45b3af1
							//   transfer:  0x9438673bec407308099b4d4d6762746542a74baf24472dbb4713a6313382540e
							//   init:      0x869c8f0dbee0539a9d9b7efba7b0105222174e8faf59773ff67174aab0128441
							//   redeem:    0x5a1719de92c69c41d4bef18a1e8cb5013a608fcfb8c4cd2e263357e5dfa136b6
							//   refund:    0x936e962e270393f83cfef29bdf67c5da01dfbb1940cd76af25f7fff7a84345e2
							// Mainnet measurements:
							// Swaps (n=1..5):   [90132 119816 149502 179176 208875]
							// Redeems (n=1..5): [49560 62879 76199 89497 102807]
							// Refunds (n=1..6): [57628 57552 57552 57539 57552 47493]
							// Approvals: [46717 46717]
							// Transfers: [51814]
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   60_732,
							Transfer:  67_358,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x8d9cb8f3191fd685e2c14d2ac3fb2b16d44eafc3"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   60_732,
							Transfer:  67_358,
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
							// Swap/Redeem/Refund values are from usdt.base mainnet.
							// Approve and Transfer are testnet estimates.
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   58_180,
							Transfer:  66_961,
						},
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x78c8587b0b4d50b3a2110bc8188eef195cfa7f11"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							Swap:      117_171,
							SwapAdd:   38_590,
							Redeem:    64_428,
							RedeemAdd: 17_304,
							Refund:    72_651,
							Approve:   58_180,
							Transfer:  66_961,
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
