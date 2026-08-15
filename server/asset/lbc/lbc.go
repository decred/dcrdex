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

const (
	version   = 0
	BipID     = 140
	assetName = "lbc"
	feeConfs  = 2
)

var maxFeeBlocks = 20

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the LBC backend.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for LBC.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexlbc.UnitInfo
}

// MinBondSize calculates the minimum bond size for a given fee rate.
func (d *Driver) MinBondSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinBondSize(maxFeeRate, false)
}

// MinLotSize calculates the minimum lot size for a given fee rate.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "LBRY Credits"
}

func init() {
	asset.Register(BipID, &Driver{})
}

// NewBackend generates the network parameters and creates an LBC backend as a
// btc clone. Connects to lbcd (node RPC), not lbcwallet.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexlbc.MainNetParams
	case dex.Testnet:
		params = dexlbc.TestNet3Params
	case dex.Regtest:
		params = dexlbc.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", cfg.Net)
	}

	// Node RPC ports (lbcd).
	ports := dexbtc.NetPorts{
		Mainnet: "9245",
		Testnet: "19245",
		Simnet:  "29245",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("lbcd")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name: assetName,
		// Match client: non-segwit until lbcwallet address-type RPC is aligned.
		Segwit:               false,
		ConfigPath:           configPath,
		Logger:               cfg.Logger,
		Net:                  cfg.Net,
		ChainParams:          params,
		Ports:                ports,
		BlockDeserializer:    dexlbc.DeserializeBlockBytes,
		NoCompetitionFeeRate: dexlbc.DefaultFee,
		FeeConfs:             feeConfs,
		MaxFeeBlocks:         maxFeeBlocks,
		RelayAddr:            cfg.RelayAddr,
	})
}
