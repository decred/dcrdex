// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package ltc

import (
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var (
	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x30,
		ScriptHashAddrID: 0x32,
		Bech32HRPSegwit:  "ltc",
		CoinbaseMaturity: 100,
		Net:              0xdbb6c0fb,
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0x3a,
		Bech32HRPSegwit:  "tltc",
		CoinbaseMaturity: 100,
		Net:              0xf1c8d2fd,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0x3a,
		Bech32HRPSegwit:  "rltc",
		CoinbaseMaturity: 100,
		// Net is not the standard for LTC simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net: 0x9acb0442,
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register ltc parameters: " + err.Error())
		}
	}
}
