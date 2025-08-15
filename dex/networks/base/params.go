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

	ContractAddresses = map[uint32]map[dex.Network]common.Address{
		1: {
			dex.Testnet: common.HexToAddress("0x4F8024dd716E4ec9AC8E0CD971616F73e3f6625d"), // txid: https://base-sepolia.blockscout.com/tx/0xa8137a8f84b28149f27045a4d6c15cceb784f3886f8a476790002e4da4728979
		},
	}

	VersionedGases = map[uint32]*dexeth.Gases{}

	usdcTokenID, _ = dex.BipSymbolID("usdc.base")

	Tokens = map[uint32]*dexeth.Token{
		usdcTokenID: TokenUSDC,
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
				Address: common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e"),
				SwapContracts: map[uint32]*dexeth.SwapContract{
					1: {},
				},
			},
			dex.Simnet: {
				Address:       common.Address{},
				SwapContracts: map[uint32]*dexeth.SwapContract{},
			},
		},
	}
)
