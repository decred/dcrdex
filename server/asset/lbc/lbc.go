// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexlbc "decred.org/dcrdex/dex/networks/lbc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the LBC backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for LBRY Credits.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// LBRY Credits and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexlbc.UnitInfo
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// MinBondSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the bond and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinBondSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinBondSize(maxFeeRate, true)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, true)
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "LBRY Credits"
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 1
	BipID     = 140
	assetName = "lbc"
)

// NewBackend generates the network parameters and creates a lbc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexlbc.MainNetParams
	case dex.Testnet:
		params = dexlbc.TestNet4Params
	case dex.Regtest:
		params = dexlbc.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", cfg.Net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "9245",
		Testnet: "19245",
		Simnet:  "39245",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("lbry-credits")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:                 assetName,
		Segwit:               true,
		ConfigPath:           configPath,
		Logger:               cfg.Logger,
		Net:                  cfg.Net,
		ChainParams:          params,
		Ports:                ports,
		BlockDeserializer:    dexlbc.DeserializeBlockBytes,
		NoCompetitionFeeRate: 10,
		// It looks like if you set it to 1, lbcd just returns data for 2
		// anyway.
		FeeConfs:     2,
		MaxFeeBlocks: 20,
		RelayAddr:    cfg.RelayAddr,
	})
}
