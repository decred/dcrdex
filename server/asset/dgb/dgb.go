// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dgb

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdgb "decred.org/dcrdex/dex/networks/dgb"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var maxFeeBlocks = 16

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the DGB backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// DigiByte.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Digibyte and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexdgb.UnitInfo
}

// MinBondSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the bond and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinBondSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinBondSize(maxFeeRate, false)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "DigiByte"
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 20
	assetName = "dgb"
	feeConfs  = 3
)

// NewBackend generates the network parameters and creates a dgb backend as a
// btc clone using an asset/btc helper function.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexdgb.MainNetParams
	case dex.Testnet:
		params = dexdgb.TestNetParams
	case dex.Regtest:
		params = dexdgb.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", cfg.Net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "14022",
		Testnet: "14023",
		Simnet:  "18443",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("digibyte")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:                 assetName,
		Segwit:               true,
		ConfigPath:           configPath,
		Logger:               cfg.Logger,
		Net:                  cfg.Net,
		ChainParams:          params,
		Ports:                ports,
		FeeConfs:             feeConfs,
		NoCompetitionFeeRate: 210, // 0.0021 DGB/kB
		MaxFeeBlocks:         maxFeeBlocks,
		RelayAddr:            cfg.RelayAddr,
	})
}
