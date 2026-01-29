// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"encoding/hex"

	"decred.org/dcrdex/dex"
)

var UnitInfo = dex.UnitInfo{
	AtomicUnit: "atoms",
	Conventional: dex.Denomination{
		Unit:             "DCR",
		ConversionFactor: 1e8,
	},
	Alternatives: []dex.Denomination{
		{
			Unit:             "mDCR",
			ConversionFactor: 1e5,
		},
		{
			Unit:             "µDCR",
			ConversionFactor: 1e2,
		},
	},
	FeeRateDenom: "B",
}

// mustDecodeHex decodes a hex string and panics on error. Used for compile-time
// constants.
func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("invalid hex in Pi key constant: " + err.Error())
	}
	return b
}

// MainNetPiKeys are the Politeia public keys for mainnet treasury voting.
// These keys are used to sign treasury spend transactions (tspends).
// Source: https://github.com/decred/dcrd/blob/master/chaincfg/mainnetparams.go
var MainNetPiKeys = [][]byte{
	mustDecodeHex("03f6e7041f1cf51ee10e0a01cd2b0385ce3cd9debaabb2296f7e9dee9329da946c"),
	mustDecodeHex("0319a37405cb4d1691971847d7719cfce70857c0f6e97d7c9174a3998cf0ab86dd"),
}

// TestNet3PiKeys are the Politeia public keys for testnet3 treasury voting.
// Source: https://github.com/decred/dcrd/blob/master/chaincfg/testnetparams.go
var TestNet3PiKeys = [][]byte{
	mustDecodeHex("03f86d30788f205a548cc55a1d677e5d5f84f60af29cdc358e4e44fabc9b5aa965"),
}

// SimNetPiKeys is empty because simnet uses generated fake keys for testing.
var SimNetPiKeys = [][]byte{}

// PiKeysForNet returns the default Politeia public keys for the given network.
// These are the treasury keys that users can set voting policies for.
func PiKeysForNet(net dex.Network) [][]byte {
	switch net {
	case dex.Mainnet:
		return MainNetPiKeys
	case dex.Testnet:
		return TestNet3PiKeys
	default:
		return SimNetPiKeys
	}
}
