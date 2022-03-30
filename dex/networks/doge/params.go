// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package doge

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Sats",
		Conventional: dex.Denomination{
			Unit:             "DOGE",
			ConversionFactor: 1e8,
		},
	}

	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x1e,
		ScriptHashAddrID: 0x16,
		CoinbaseMaturity: 30,
		Net:              0xc0c0c0c0,
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x71,
		ScriptHashAddrID: 0xc4,
		CoinbaseMaturity: 30,
		Net:              0xfcc1b7dc,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		CoinbaseMaturity: 60,
		// Net is not the standard for DOGE simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net: 0xfabfb5da,
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register doge parameters: " + err.Error())
		}
	}
}
