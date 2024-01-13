// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dash

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdash "decred.org/dcrdex/dex/networks/dash"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the Dash backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Dash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Dash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexdash.UnitInfo
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "Dash"
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

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 5
	assetName = "dash"
)

// NewBackend generates the network parameters and creates a dash backend as a
// btc clone using an asset/btc helper function.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexdash.MainNetParams
	case dex.Testnet:
		params = dexdash.TestNetParams
	case dex.Regtest:
		params = dexdash.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", cfg.Net)
	}

	// Designate the clone ports.
	ports := dexbtc.NetPorts{
		Mainnet: "9998",
		Testnet: "19998",
		Simnet:  "19898",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("dash")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:        assetName,
		Segwit:      false,
		ConfigPath:  configPath,
		Logger:      cfg.Logger,
		Net:         cfg.Net,
		ChainParams: params,
		Ports:       ports,
		// getblockstats exists
		// estimatesmartfee exists but no estimatefee
		NoCompetitionFeeRate: 1,
		// masternode finalization Dash InstantSend 2 blocks
		FeeConfs:     2,
		MaxFeeBlocks: 16,
		RelayAddr:    cfg.RelayAddr,
	})
}
