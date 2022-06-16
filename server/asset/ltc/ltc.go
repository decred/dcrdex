// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
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

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexltc.UnitInfo
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 1
	BipID     = 2
	assetName = "ltc"
)

// NewBackend generates the network parameters and creates a ltc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexltc.MainNetParams
	case dex.Testnet:
		params = dexltc.TestNet4Params
	case dex.Regtest:
		params = dexltc.RegressionNetParams
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

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:                 assetName,
		Segwit:               true,
		ConfigPath:           configPath,
		Logger:               logger,
		Net:                  network,
		ChainParams:          params,
		Ports:                ports,
		BlockDeserializer:    dexltc.DeserializeBlockBytes,
		NoCompetitionFeeRate: 10,
		// It looks like if you set it to 1, litecoind just returns data for 2
		// anyway.
		FeeConfs:     2,
		MaxFeeBlocks: 20,
	})
}
