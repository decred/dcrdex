// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
)

// Config holds the parameters needed to initialize an ETH wallet.
type Config struct {
	AppDir         string  `ini:"appdir"`
	NodeListenAddr string  `ini:"nodelistenaddr"`
	GasFee         float64 `ini:"gasfee"`
}

// loadConfig loads the Config from a setting map and checks the network.
//
// TODO: Test this with windows.
func loadConfig(settings map[string]string, network dex.Network) (*Config, error) {
	cfg := new(Config)
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}
	switch network {
	case dex.Simnet:
	case dex.Testnet:
	case dex.Mainnet:
		// TODO: Allow.
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
	return cfg, nil
}
