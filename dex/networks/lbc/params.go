// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	// DefaultFee is a reasonable fallback fee rate in atoms/byte (≈ LBC/kB * 1e5).
	// LBC block time is ~150s; start conservative and tune with mainnet data.
	DefaultFee = 20
	// DefaultFeeRateLimit is the highest fee rate (atoms/byte) we will pay by default.
	DefaultFeeRateLimit = 1000
)

func mustHash(hash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		panic(err.Error())
	}
	return h
}

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Sats",
		Conventional: dex.Denomination{
			Unit:             "LBC",
			ConversionFactor: 1e8,
		},
		Alternatives: []dex.Denomination{
			{
				Unit:             "mLBC",
				ConversionFactor: 1e5,
			},
			{
				Unit:             "µLBC",
				ConversionFactor: 1e2,
			},
		},
		FeeRateDenom: "vB",
	}

	// MainNetParams are the clone parameters for mainnet.
	// Values from lbryio/lbcd chaincfg.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x55,
		ScriptHashAddrID: 0x7a,
		Bech32HRPSegwit:  "lbc",
		CoinbaseMaturity: 100,
		Net:              0xf1aae4fa,
		GenesisHash:      mustHash("9c89283ba0f3227f6c03b70216b9f665f0118d5e0fa729cedf4fb34d6a34f463"),
	})
	// TestNet3Params are the clone parameters for testnet3.
	TestNet3Params = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet3",
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		Bech32HRPSegwit:  "tlbc",
		CoinbaseMaturity: 100,
		Net:              0xe1aae4fa,
		// Same genesis block as mainnet in lbryio/lbcd.
		GenesisHash: mustHash("9c89283ba0f3227f6c03b70216b9f665f0118d5e0fa729cedf4fb34d6a34f463"),
	})
	// RegressionNetParams are the clone parameters for regtest (simnet in dcrdex).
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		Bech32HRPSegwit:  "rlbc",
		CoinbaseMaturity: 100,
		Net:              0xd1aae4fa,
		GenesisHash:      mustHash("6e3fcf1299d4ec5d79c3a4c91d624a4acf9e2e173d95a1a0504f677669687556"),
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet3Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register lbc parameters: " + err.Error())
		}
	}
}
