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
	//   init:            0xf7c752f9217e359718dc9d9a17f3a04d3bfb3af6923fa43e2d668eda978319d0
	//   redeem:          0x958268be6bfe6db7088440f7ba6a78ec718da5a575674a75f781975974e5f965
	//   refund:          0xaf49b64a327051dcf304b418f43781bedd79680329b8503a0c3f91e900ba609f
	//   gasless redeem:  0xf997a66ddb1f92e6b9011a168153d787940dd2b50b1c80701630f283265a994c
	v1Gases = &dexeth.Gases{
		// Mainnet measurements:
		// Swaps (n=1..5):   [54296 84005 113714 143412 173111]
		Swap:    70_584,
		SwapAdd: 38_613,
		// Redeems (n=1..5): [44911 58254 71598 84907 98254]
		Redeem:    58_384,
		RedeemAdd: 17_335,
		// Refunds (n=1..6): [47987 47987 47987 47987 47975 42859]
		Refund: 61_269,

		// Signed redeems (n=1..5): [85211 82157 96133 110135 124140]
		// The first time someone does a gasless redeem, it will be more expensive due
		// to the first-time cost of initializing the on chain nonce.
		SignedRedeem:    110_774,
		SignedRedeemAdd: 18_201,
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
			dex.Testnet: common.HexToAddress("0xAB51BFA44Eee1EBD0E7861c0e8ba7c37842530ba"),
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
				Address: common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {
						Gas: dexeth.Gases{
							// Gas values are from usdt.base mainnet.
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
							Approve:   60_732,
							Transfer:  67_358,
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
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
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
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
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
							// Gas values are from usdt.base mainnet.
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
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
							// Gas values are from usdt.base mainnet.
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
							Approve:   60_732,
							Transfer:  67_358,
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
							Swap:      117_171,
							SwapAdd:   38_613,
							Redeem:    64_412,
							RedeemAdd: 17_335,
							Refund:    68_328,
							Approve:   60_732,
							Transfer:  67_358,
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

// MaybeReadSimnetAddrs attempts to read the info files generated by the eth
// simnet harness to populate swap contract and token addresses in
// ContractAddresses and Tokens.
func MaybeReadSimnetAddrs() {
	dexeth.MaybeReadSimnetAddrsDir("base", ContractAddresses, MultiBalanceAddresses, Tokens[usdcTokenID].NetTokens[dex.Simnet], Tokens[usdtTokenID].NetTokens[dex.Simnet])
}
