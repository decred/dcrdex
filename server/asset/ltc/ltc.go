// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = &dexbtc.CloneParams{
		PubKeyHashAddrID: 0x30,
		ScriptHashAddrID: 0x32,
		Bech32HRPSegwit:  "ltc",
		CoinbaseMaturity: 100,
		Net:              0xdbb6c0fb,
	}
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = &dexbtc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0x3a,
		Bech32HRPSegwit:  "tltc",
		CoinbaseMaturity: 100,
		Net:              0xf1c8d2fd,
	}
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = &dexbtc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0x3a,
		Bech32HRPSegwit:  "rltc",
		CoinbaseMaturity: 100,
		Net:              0x12141c16,
	}
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the LTC backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Litecoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Litecoin and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

func init() {
	asset.Register(assetName, &Driver{})
}

const assetName = "ltc"

// NewBackend generates the network parameters and creates a ltc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *dexbtc.CloneParams
	switch network {
	case dex.Mainnet:
		params = MainNetParams
	case dex.Testnet:
		params = TestNet4Params
	case dex.Regtest:
		params = RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "9332",
		Testnet: "19332",
		Simnet:  "19443",
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("litecoin")
	}

	return btc.NewBTCClone(assetName, configPath, logger, network, dexbtc.ReadCloneParams(params), ports)
}
