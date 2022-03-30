// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	// MinimumTxOverhead
	// 4 header + 4 nVersionGroup + 1 varint input count + 1 varint output count
	// + 4 lockTime + 4 nExpiryHeight + 8 valueBalanceSapling + 1 varint nSpendsSapling
	// + 1 varint nOutputsSapling + 1 varint nJoinSplit
	MinimumTxOverhead = 29

	InitTxSizeBase = MinimumTxOverhead + btc.P2PKHOutputSize + btc.P2SHOutputSize // 29 + 34 + 32 = 95
	InitTxSize     = InitTxSizeBase + btc.RedeemP2PKHInputSize                    // 95 + 149 = 244
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Zats",
		Conventional: dex.Denomination{
			Unit:             "ZEC",
			ConversionFactor: 1e8,
		},
	}

	// MainNetParams are the clone parameters for mainnet. ZCash,
	// like Decred, uses two bytes for their address IDs. We will convert
	// between address types on the fly and use these spoof parameters
	// internally.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		ScriptHashAddrID: 0xBD,
		PubKeyHashAddrID: 0xB8,
		CoinbaseMaturity: 100,
		Net:              0x24e92764,
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x25,
		ScriptHashAddrID: 0xBA,
		CoinbaseMaturity: 100,
		Net:              0xfa1af9bf,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x25,
		ScriptHashAddrID: 0xBA,
		CoinbaseMaturity: 100,
		Net:              0xaae83f5f,
	})

	// MainNetAddressParams are used for string address parsing. We use a
	// spoofed address internally, since ZCash uses a two-byte address ID
	// instead of a 1-byte ID.
	MainNetAddressParams = &AddressParams{
		ScriptHashAddrID: [2]byte{0x1C, 0xBD},
		PubKeyHashAddrID: [2]byte{0x1C, 0xB8},
	}

	// TestNet4AddressParams are used for string address parsing.
	TestNet4AddressParams = &AddressParams{
		ScriptHashAddrID: [2]byte{0x1C, 0xBA},
		PubKeyHashAddrID: [2]byte{0x1D, 0x25},
	}

	// RegressionNetAddressParams are used for string address parsing.
	RegressionNetAddressParams = &AddressParams{
		ScriptHashAddrID: [2]byte{0x1C, 0xBA},
		PubKeyHashAddrID: [2]byte{0x1D, 0x25},
	}
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register zec parameters: " + err.Error())
		}
	}
}
