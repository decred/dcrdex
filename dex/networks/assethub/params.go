// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package assethub holds the network parameters for Kusama Asset Hub's
// Ethereum side (pallet-revive), where KSM is the native asset and assets
// registered with the assets pallet appear as ERC-20 precompiles.
//
// Testnet is Polkadot Hub TestNet on Paseo: Kusama has no testnet of its own,
// it is itself the canary network.
package assethub

import (
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// KsmBipID is KSM's SLIP-0044 coin type.
	KsmBipID = 434

	MainnetChainID = 420420418 // Kusama Asset Hub
	TestnetChainID = 420420417 // Polkadot Hub TestNet (Paseo)
	SimnetChainID  = 420420420 // a local revive dev node
)

var (
	ChainIDs = map[dex.Network]int64{
		dex.Mainnet: MainnetChainID,
		dex.Testnet: TestnetChainID,
		dex.Simnet:  SimnetChainID,
	}

	// The Ethereum side of revive carries the native asset at 18 decimals, so
	// the atomic unit is gwei as it is for ETH.
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "gwei",
		Conventional: dex.Denomination{
			Unit:             "KSM",
			ConversionFactor: 1e9,
		},
		Alternatives: []dex.Denomination{
			{
				Unit:             "mKSM",
				ConversionFactor: 1e6,
			},
		},
		FeeRateDenom: "gas",
	}

	// pallet-revive meters weight and maps it onto an Ethereum-shaped gas
	// number, so its gas is not Ethereum's. Measured on Polkadot Hub TestNet
	// on 2026-10-09 with the unmodified v1 contract:
	//   initiate 5,428   redeem 2,951-2,956   refund 2,463
	//   token: approve 3,993   initiate 6,440   redeem 747-3,386
	// The base fee was a flat 1,000 gwei there and 10,000 gwei on Kusama
	// Asset Hub the same day. Limits below are Ethereum's, which sit 10 to 40
	// times above what revive charges, so a swap never runs out of gas at the
	// cost of reserving more than it spends. Batches untested.
	v1Gases = &dexeth.Gases{
		Swap:            70_612,
		SwapAdd:         38_673,
		Redeem:          58_484,
		RedeemAdd:       17_472,
		Refund:          61_378,
		SignedRedeem:    110_895,
		SignedRedeemAdd: 12_639,
	}

	tokenV1Gases = dexeth.Gases{
		Swap:      135_447,
		SwapAdd:   38_646,
		Redeem:    124_928,
		RedeemAdd: 17_440,
		Refund:    87_834,
		Approve:   63_550,
		Transfer:  82_124,
	}

	VersionedGases = map[uint32]*dexeth.Gases{
		1: v1Gases,
	}

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		1: {
			// Not deployed on Kusama Asset Hub yet.
			dex.Mainnet: {},
			// Deployed in tx
			// 0x85d49c9e7d0e19a4dcb39c4f3bd4d2343b3cfcc83f64c969924f95699eb2b75c.
			dex.Testnet: common.HexToAddress("0x3f6a6e614983ab6c5d177314324cca1f56eb7913"),
			dex.Simnet:  {},
		},
	}

	MultiBalanceAddresses = map[dex.Network]common.Address{}

	DusdTokenID, _ = dex.BipSymbolID("dusd.ksm")

	Tokens = map[uint32]*dexeth.Token{
		DusdTokenID: TokenDUSD,
	}

	// TokenDUSD is dUSD, a dollar stablecoin. On Kusama Asset Hub it is
	// trust-backed asset 50000002, reached through the assets pallet's ERC-20
	// precompile: the asset id in the first four bytes, 0x01200000 in the
	// last four.
	TokenDUSD = &dexeth.Token{
		EVMFactor: new(int64),
		Token: &dex.Token{
			ParentID: KsmBipID,
			Name:     "dUSD",
			UnitInfo: dex.UnitInfo{
				AtomicUnit: "µUSD",
				Conventional: dex.Denomination{
					Unit:             "dUSD",
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
				Address: common.HexToAddress("0x02faf08200000000000000000000000001200000"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {Gas: tokenV1Gases},
				},
			},
			dex.Testnet: {
				// A mock dUSD (an ordinary ERC-20) on Polkadot Hub TestNet.
				Address: common.HexToAddress("0x010ca163c417fabf7223f23a1ba7aeaca8ca571e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {Gas: tokenV1Gases},
				},
			},
		},
	}
)
