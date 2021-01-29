// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package bch

import (
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	bchchaincfg "github.com/gcash/bchd/chaincfg"
)

var (
	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: bchchaincfg.MainNetParams.LegacyPubKeyHashAddrID,
		ScriptHashAddrID: bchchaincfg.MainNetParams.LegacyScriptHashAddrID,
		Bech32HRPSegwit:  "bitcoincash",
		CoinbaseMaturity: 100,
		// BitcoinCash didn't change any of these net identifiers, so these are
		// all spoofed. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net: 0xe8f3e1e3,
	})
	// TestNet3Params are the clone parameters for testnet.
	TestNet3Params = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: bchchaincfg.TestNet3Params.LegacyPubKeyHashAddrID,
		ScriptHashAddrID: bchchaincfg.TestNet3Params.LegacyScriptHashAddrID,
		Bech32HRPSegwit:  "bchtest",
		CoinbaseMaturity: 100,
		Net:              0xf4f3e5f4,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: bchchaincfg.RegressionNetParams.LegacyPubKeyHashAddrID,
		ScriptHashAddrID: bchchaincfg.RegressionNetParams.LegacyScriptHashAddrID,
		Bech32HRPSegwit:  "bchreg",
		CoinbaseMaturity: 100,
		Net:              0xee87f733,
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet3Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register bch parameters: " + err.Error())
		}
	}
}
