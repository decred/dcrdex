// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package dgb

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	// DEFAULT_TRANSACTION_MINFEE is 100 (mintxfee default is 0.001 DGB/kB)
	// DEFAULT_DISCARD_FEE is 10 (minrelaytxfee and blockmintxfee defaults are 0.00001 DGB/kB)
	// HIGH_TX_FEE_PER_KB is 100,000 (1 DGB/kB), warning level
	// DEFAULT_TRANSACTION_MAXFEE is 100,000 (maxtxfee default is 0.1 DGB/kB)
	DefaultFee          = 200  // 0.002 DGB/kB
	DefaultFeeRateLimit = 5000 // 0.05 DGB/kB
)

// v7.17.4 defaults:
// -fallbackfee= (default: 0.0002) fee estimation fallback
// -mintxfee= (default: 0.001)  for tx creation
// -maxtxfee= (default: 1.00)
// -minrelaytxfee= (default: 0.00001)
// -blockmintxfee= (default: 0.00001)

func mustHash(hash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		panic(err.Error())
	}
	return h
}

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "digiSatoshi",
		Conventional: dex.Denomination{
			Unit:             "DGB",
			ConversionFactor: 1e8,
		},
		FeeRateUnit: "Sats/vB",
	}

	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x1e, // 30 - start with D
		ScriptHashAddrID: 0x3f, // 63 - start with S, "old" was 5
		Bech32HRPSegwit:  "dgb",
		CoinbaseMaturity: 100, // 8 before block "multiAlgoDiffChangeTarget" (145000)
		Net:              0xfac3b6da,
		GenesisHash:      mustHash("7497ea1b465eb39f1c8f507bc877078fe016d6fcb6dfad3a64c98dcc6e1e8496"),
	})
	// TestNetParams are the clone parameters for testnet.
	TestNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet4",
		PubKeyHashAddrID: 0x7e, // 126 - start with t
		ScriptHashAddrID: 0x8c, // 140 - start with s
		Bech32HRPSegwit:  "dgbt",
		CoinbaseMaturity: 100,
		Net:              0xfdc8bddd,
		GenesisHash:      mustHash("308ea0711d5763be2995670dd9ca9872753561285a84da1d58be58acaa822252"),
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x7e, // 126
		ScriptHashAddrID: 0x8c, // 140
		Bech32HRPSegwit:  "dgbrt",
		CoinbaseMaturity: 100,
		// Net is not the standard for DGB simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net:         0xfa55b279,
		GenesisHash: mustHash("4598a0f2b823aaf9e77ee6d5e46f1edb824191dcd48b08437b7cec17e6ae6e26"), // TODO or unused with simnet?
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNetParams, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register dgb parameters: " + err.Error())
		}
	}
}
