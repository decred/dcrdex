// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

const (
	defaultMainnet     = "localhost:9110"
	defaultTestnet3    = "localhost:19110"
	defaultSimnet      = "localhost:19557"
	defaultAccountName = "default"
)

var (
	// A global *chaincfg.Params will be set if loadConfig completes without
	// error.
	chainParams       *chaincfg.Params
	dcrwHomeDir       = dcrutil.AppDataDir("dcrwallet", false)
	defaultRPCCert    = filepath.Join(dcrwHomeDir, "rpc.cert")
	defaultConfigPath = filepath.Join(dcrwHomeDir, "dcrwallet.conf")
)

// DCRConfig is passed to the constructor.
type DCRConfig struct {
	// RPCUser is the RPC username provided to dcrwallet configuration.
	RPCUser string `ini:"username"`
	// RPCPass is the RPC password provided to dcrwallet configuration.
	RPCPass string `ini:"password"`
	// RPCListen is the RPC network address provided to dcrwallet configuration.
	// If the value is an empty string, it will be set to a default value for the
	// network.
	RPCListen string `ini:"rpclisten"`
	// RPCCert is the filepath to the dcrwallet TLS certificate. If it is not
	// provided, the default dcrwallet location will be assumed.
	RPCCert string `ini:"rpccert"`
	// Context should be canceled when the application exits. This will cause
	// some cleanup to be performed during shutdown.
	Context context.Context
}

// loadConfig loads the DCRConfig from file. If no values are found for
// RPCListen or RPCCert in the specified file, default values will be used.
// If configPath is an empty string, loadConfig will attempt to read settings
// directly from the default dcrwallet.conf filepath. If there is no error, the
// module-level chainParams variable will be set appropriately for the network.
func loadConfig(settings map[string]string, network dex.Network) (*DCRConfig, error) {
	cfg := new(DCRConfig)
	cfgData := config.OptionsMapToINIData(settings)
	err := config.Parse(cfgData, cfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing config: %v", err)
	}

	missing := ""
	if cfg.RPCUser == "" {
		missing += " username"
	}
	if cfg.RPCPass == "" {
		missing += " password"
	}
	if missing != "" {
		return nil, fmt.Errorf("missing dcrwallet rpc credentials:%s", missing)
	}

	// Get network settings. Zero value is mainnet, but unknown non-zero cfg.Net
	// is an error.
	var defaultServer string
	switch network {
	case dex.Simnet:
		chainParams = chaincfg.SimNetParams()
		defaultServer = defaultSimnet
	case dex.Testnet:
		chainParams = chaincfg.TestNet3Params()
		defaultServer = defaultTestnet3
	case dex.Mainnet:
		chainParams = chaincfg.MainNetParams()
		defaultServer = defaultMainnet
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
	if cfg.RPCListen == "" {
		cfg.RPCListen = defaultServer
	}
	if cfg.RPCCert == "" {
		cfg.RPCCert = defaultRPCCert
	}

	return cfg, nil
}
