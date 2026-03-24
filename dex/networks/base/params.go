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
	//   init:            0x725c73b787f532b71b788a06cf314c9c5d8687db34e43a7f16efbb116caa838e
	//   redeem:          0x9cf20be6655781b214d2a75fb5bd045d6137ac62590b9a18a8c93453dd52b4fe
	//   refund:          0x0b276e69bcec2150045d6bb3e559df24393a28762683552b7023cc0f07480c4f
	//   gasless redeem:  0xee5ec36a7f98bbd295dabff726846330c7b7375ec44127a5f6c307497bdb2ae0
	v1Gases = &dexeth.Gases{
		// Mainnet measurements:
		// Swaps (n=1..5):   [54317 84045 113821 143562 173328]
		Swap:    70_612,
		SwapAdd: 38_677,
		// Redeems (n=1..5): [44988 58406 71873 85305 98751]
		Redeem:    58_484,
		RedeemAdd: 17_472,
		// Refunds (n=1..6): [48069 48057 48069 48057 48069 42940]
		Refund: 61_373,

		// Signed redeems (n=1..5): [85304 82202 96202 110204 124161]
		// The first time someone does a gasless redeem, it will be more expensive due
		// to the first-time cost of initializing the on chain nonce.
		SignedRedeem:    110_895,
		SignedRedeemAdd: 12_628,
	}

	// tokenV1Gases are the gas estimates for v1 token swap contracts, measured
	// from usdt.base mainnet. These values are used for all tokens on all
	// networks since the v1 contract handles all ERC20 tokens identically.
	// Redeem includes a buffer for cold storage costs when the redeemer has
	// never held the token (SSTORE zero-to-nonzero + cold access surcharges).
	tokenV1Gases = dexeth.Gases{
		Swap:      119_624,
		SwapAdd:   38_646,
		Redeem:    110_028,
		RedeemAdd: 17_440,
		Refund:    72_758,
		Approve:   60_732,
		Transfer:  67_358,
	}

	VersionedGases = map[uint32]*dexeth.Gases{
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		0: {
			dex.Simnet: common.HexToAddress(""), // Filled in by MaybeReadSimnetAddrs
		},
		1: {
			dex.Mainnet: common.HexToAddress("0x85c4e942AE2729a3C61275d4ff90A245c81592ae"), // tx 0x7afd0e21841fc1ba77356be3d93a0ad8bbfe1b6279a2c885b8320c8e0e74c216
			dex.Testnet: common.HexToAddress("0x60bd71f644b7519BC13dA76D8E121b38ef738fCe"), // tx 0x46a314b14d9f565d1b962b3f5e9e8930c7650e307c7cf56d06705add94154595
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
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {},
					1: {
						Gas: tokenV1Gases,
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
						// Mainnet v1 token swap evidence:
						//   approve:   0xb891f4089c124c1ebe230553f99627b28040c9db3457d38f29dac71a563c77cf
						//   transfer:  0xecdad782d8e91b2e3cbcef3996a577cb67d4b12cd45767335bb1b0abf7e26213
						//   init:      0xe7c1b95344b7381a3e20205d9ff0a08b9fc19cec858b6bb8357818b2a7614495
						//   redeem:    0xf0c189d0b2a4fe0533097e7fbf53a802517f1247ded3f3268a638c090636edf3
						//   refund:    0x6148f72d43bc71414df56a6cf192581fb259de4d6265770114ce66790bec9e52
						// Mainnet measurements:
						// Swaps (n=1..5):   [92019 121758 151487 181216 210934]
						// Redeems (n=1..5): [49625 63043 76474 89895 103292]
						// Refunds (n=1..6): [57698 57634 57634 57634 57634 47574]
						// Approvals: [46717 46717]
						// Transfers: [51814]
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x8d9cb8f3191fd685e2c14d2ac3fb2b16d44eafc3"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Simnet: {
				Address: common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{
					0: {},
					1: {
						Gas: tokenV1Gases,
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
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Testnet: {
				Address: common.HexToAddress("0x78c8587b0b4d50b3a2110bc8188eef195cfa7f11"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: tokenV1Gases,
					},
				},
			},
			dex.Simnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{0: {}, 1: {Gas: tokenV1Gases}},
			},
		},
	}
)

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	dexeth.MaybeReadSimnetAddrsDir("base", ContractAddresses, MultiBalanceAddresses, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
