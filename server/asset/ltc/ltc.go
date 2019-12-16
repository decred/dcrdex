// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"context"
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/ltcsuite/ltcd/chaincfg"
)

// NewBackend generates the network parameters and creates a ltc backend as a
// btc clone using an asset/btc helper function.
func NewBackend(ctx context.Context, configPath string, logger dex.Logger, network dex.Network) (asset.DEXAsset, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = &chaincfg.MainNetParams
	case dex.Testnet:
		params = &chaincfg.TestNet4Params
	case dex.Regtest:
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Convert the ltcd params to btcd params.
	btcParams, err := btc.ReadCloneParams(params)
	if err != nil {
		return nil, fmt.Errorf("error converting parameters: %v", err)
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

	return btc.NewBTCClone(ctx, configPath, logger, network, btcParams, ports)
}
