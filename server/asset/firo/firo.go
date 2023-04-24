// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package firo

import (
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the DGB backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
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
	return dexfiro.UnitInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 136 // Zcoin XZC
	assetName = "firo"
)

// NewBackend generates the network parameters and creates a dgb backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexfiro.MainNetParams
	case dex.Testnet:
		params = dexfiro.TestNetParams
	case dex.Regtest:
		params = dexfiro.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8168",
		Testnet: "18168",
		Simnet:  "18444", // Regtest
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("firo")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:                 assetName,
		Segwit:               false,
		ConfigPath:           configPath,
		Logger:               logger,
		Net:                  network,
		ChainParams:          params,
		Ports:                ports,
		FeeConfs:             1,    // the default
		ManualMedianFee:      true, // no getblockstats
		NoCompetitionFeeRate: 1,    // 0.00001000 FIRO/kB
		MaxFeeBlocks:         16,   // copied from dgb
		BooleanGetBlockRPC:   true,
		// Firo actually has estimatesmartfee, but with a big warning
		// WARNING: This interface is unstable and may disappear or change!
		// and it also doesn't accept an estimate_mode argument.
		DumbFeeEstimates: true})
}

// Firo v0.14.12.1 defaults:
// -fallbackfee= (default: 20000) 	wallet.h: DEFAULT_FALLBACK_FEE unused afaics
// -mintxfee= (default: 1000)  		for tx creation
// -maxtxfee= (default: 1000000000) 10 FIRO .. also looks unused
// -minrelaytxfee= (default: 1000) 	0.00001 firo,
// -blockmintxfee= (default: 1000)
