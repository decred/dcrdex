// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zcl

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Zats",
		Conventional: dex.Denomination{
			Unit:             "ZCL",
			ConversionFactor: 1e8,
		},
	}

	// MainNetParams are the clone parameters for mainnet. Zcash,
	// like Decred, uses two bytes for their address IDs. We will convert
	// between address types on the fly and use these spoof parameters
	// internally.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		ScriptHashAddrID: 0xBD,
		PubKeyHashAddrID: 0xB8,
		CoinbaseMaturity: 100,
		Net:              0xfe2d578a, // random
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet4",
		PubKeyHashAddrID: 0x25,
		ScriptHashAddrID: 0xBA,
		CoinbaseMaturity: 100,
		Net:              0xea3102f7, // random
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x25,
		ScriptHashAddrID: 0xBA,
		CoinbaseMaturity: 100,
		Net:              0x4b7b0349, // random
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register zec parameters: " + err.Error())
		}
	}
}
