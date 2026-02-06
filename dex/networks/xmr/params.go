// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package xmr

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	DefaultFee          = 3  // 0.00003000 XMR/kB
	DefaultFeeRateLimit = 30 // 0.00030000 XMR/kB
)

func mustHash(hash string) *chainhash.Hash {
	h, err := chainhash.NewHashFromStr(hash)
	if err != nil {
		panic(err.Error())
	}
	return h
}

var NetRpcPorts = btc.NetPorts{
	Mainnet: "18081",
	Testnet: "28081", // testnet (not stagenet)
	Simnet:  "18081",
}

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Atoms",
		Conventional: dex.Denomination{
			Unit:             "XMR",
			ConversionFactor: 1e12,
		},
		Alternatives: []dex.Denomination{
			{
				Unit:             "mXMR",
				ConversionFactor: 1e9,
			},
			{
				Unit:             "µXMR",
				ConversionFactor: 1e6,
			},
		},
		FeeRateDenom: "B",
	}

	// MainNetParams are the parameters for mainnet.
	MainNetParams = &chaincfg.Params{
		Name:             "mainnet",
		DefaultPort:      "18080",
		CoinbaseMaturity: 10,
		// The only place we currently use Net is in btcd/chaincfg.Register,
		// where it is checked to prevent duplicate registration, so our only
		// requirement is that it is unique. This one was generated with a prng.
		Net:         0x77bb11a0,
		GenesisHash: mustHash("418015bb9ae982a1975da7d79277c2705727a56894ba0fb246adaabb1f4632e3"),
	}

	// TestNetParams are the parameters for testnet.
	TestNetParams = &chaincfg.Params{
		Name:             "testnet",
		DefaultPort:      "28080",
		CoinbaseMaturity: 10,
		Net:              0x77bb11a1,
		GenesisHash:      mustHash("48ca7cd3c8de5b6a4d53d2861fbdaedca141553559f9be9520068053cda8430b"),
	}

	// RegressionNetParams are the parameters for simnet.
	RegressionNetParams = &chaincfg.Params{
		Name:             "regtest",
		DefaultPort:      "18080", // usually made the same as mainnet but with a daemon started with --regtest
		CoinbaseMaturity: 10,
		Net:              0x77bb11a2,
		GenesisHash:      mustHash("0000000000000000000000000000000000000000000000000000000000000000"), // fakechain
	}
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNetParams, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register xmr parameters: " + err.Error())
		}
	}
}
