// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package firo

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	DefaultFee          = 1  // 0.00001000 FIRO/kB
	DefaultFeeRateLimit = 20 // 0.00020000 FIRO/kB
)

// Firo v0.14.12.1 defaults:
// -fallbackfee= (default: 20000) 	wallet.h: DEFAULT_FALLBACK_FEE unused afaics
// -mintxfee= (default: 1000)  		for tx creation
// -maxtxfee= (default: 1000000000) 10 FIRO .. also looks unused
// -minrelaytxfee= (default: 1000) 	0.00001 firo,
// -blockmintxfee= (default: 1000)

func mustHash(hash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		panic(err.Error())
	}
	return h
}

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "satoshi",
		Conventional: dex.Denomination{
			Unit:             "FIRO",
			ConversionFactor: 1e8,
		},
	}

	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "mainnet",
		PubKeyHashAddrID: 0x52, // 82 - start with 'a' & occasionally 'Z'
		ScriptHashAddrID: 0x07, // 07 - start with 3 or 4
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		Net:              0xe3d9fef1,
		GenesisHash:      mustHash("4381deb85b1b2c9843c222944b616d997516dcbd6a964e1eaf0def0830695233"),
	})
	// TestNetParams are the clone parameters for testnet.
	TestNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "testnet3",
		PubKeyHashAddrID: 0x41, // 65 - start with T
		ScriptHashAddrID: 0xb2, // 178 - start with 2
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		Net:              0xcffcbeea,
		GenesisHash:      mustHash("aa22adcc12becaf436027ffe62a8fb21b234c58c23865291e5dc52cf53f64fca"),
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		Name:             "regtest",
		PubKeyHashAddrID: 0x41, // 65 - start with T
		ScriptHashAddrID: 0xb2, // 178 - start with 2
		Bech32HRPSegwit:  "",   // no segwit
		CoinbaseMaturity: 100,
		// Net is not the standard for Firo simnet, since they never changed it
		// from the BTC regtest value: 0xfabfb5da
		// The only place we currently use Net is in btcd/chaincfg.Register,
		// where it is checked to prevent duplicate registration, so our only
		// requirement is that it is unique. This one was just generated with a prng.
		Net:         0x7da7d6db,
		GenesisHash: mustHash("a42b98f04cc2916e8adfb5d9db8a2227c4629bc205748ed2f33180b636ee885b"),
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNetParams, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register firo parameters: " + err.Error())
		}
	}
}
