// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package shc

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexshc "decred.org/dcrdex/dex/networks/shc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

var maxFeeBlocks = 16

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the Sharecoin backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Sharecoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Sharecoin and Bitcoin have the same tx hash and output format - see
	// the matching comment in client/asset/shc/shc.go.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexshc.UnitInfo
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
	return "Sharecoin"
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version = 0
	// BipID must match the same self-assigned value used in
	// client/asset/shc/shc.go - see the comment there for why this isn't
	// an officially-registered SLIP-44 index.
	BipID     = 8443000
	assetName = "shc"
	feeConfs  = 3
)

// NewBackend generates the network parameters and creates a shc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexshc.MainNetParams
	default:
		return nil, fmt.Errorf("unsupported network for shc: %v (mainnet only)", cfg.Net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8332",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("sharecoin")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:        assetName,
		Segwit:      true,
		ConfigPath:  configPath,
		Logger:      cfg.Logger,
		Net:         cfg.Net,
		ChainParams: params,
		Ports:       ports,
		FeeConfs:    feeConfs,
		// Estimate, not yet verified against a real fee market - see the
		// matching comment in dex/networks/shc/params.go.
		NoCompetitionFeeRate: 1000,
		MaxFeeBlocks:         maxFeeBlocks,
		RelayAddr:            cfg.RelayAddr,
	})
}
