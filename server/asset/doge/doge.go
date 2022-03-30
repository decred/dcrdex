// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package doge

import (
	"fmt"
	"math"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdoge "decred.org/dcrdex/dex/networks/doge"
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

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexdoge.UnitInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 3
	assetName = "doge"
	feeConfs  = 10
)

// NewBackend generates the network parameters and creates a ltc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = dexdoge.MainNetParams
	case dex.Testnet:
		params = dexdoge.TestNet4Params
	case dex.Regtest:
		params = dexdoge.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "22555",
		Testnet: "44555",
		Simnet:  "18332",
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("dogecoin")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name: assetName,
		// Segwit may be enabled in v1.15
		// https://www.reddit.com/r/dogecoindev/comments/7m8zjz/dogecoin_114_segwit_checks/
		// If so, it may work differently than Bitcoin.
		// https://github.com/dogecoin/dogecoin/discussions/2264
		// Looks like Segwit will be false for a little while longer. Should
		// think about how to transition once activated.
		Segwit:      false,
		ConfigPath:  configPath,
		Logger:      logger,
		Net:         network,
		ChainParams: params,
		Ports:       ports,
		FeeEstimator: func(cl *btc.RPCClient) (uint64, error) {
			var r float64
			if err := cl.Call("estimatefee", []interface{}{feeConfs}, &r); err != nil {
				return 0, err
			}
			if r <= 0 {
				return 0, fmt.Errorf("fee could not be estimated")
			}
			return uint64(math.Round(r * 1e8)), nil
		},
	})
}
