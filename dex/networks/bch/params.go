// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package bch

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Sats",
		Conventional: dex.Denomination{
			Unit:             "BCH",
			ConversionFactor: 1e8,
		},
	}
	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x00,
		ScriptHashAddrID: 0x05,
		Bech32HRPSegwit:  "bitcoincash",
		CoinbaseMaturity: 100,
		Net:              0xe8f3e1e3,
	})
	// TestNet4Params are the clone parameters for testnet4.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet4",
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		Bech32HRPSegwit:  "bchtest",
		CoinbaseMaturity: 100,
		Net:              0xafdab7e2,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		Bech32HRPSegwit:  "bchreg",
		CoinbaseMaturity: 100,
		// Net is not the standard for BCH simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net: 0xee87f733,
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register bch parameters: " + err.Error())
		}
	}
}
