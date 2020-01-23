// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	flags "github.com/jessevdk/go-flags"
)

const (
	defaultMainnet  = "localhost:9109"
	defaultTestnet3 = "localhost:19109"
	defaultSimnet   = "localhost:19556"
)

var (
	// A global *chaincfg.Params will be set if loadConfig completes without
	// error.
	chainParams       *chaincfg.Params
	dcrdHomeDir       = dcrutil.AppDataDir("dcrd", false)
	defaultRPCCert    = filepath.Join(dcrdHomeDir, "rpc.cert")
	defaultConfigPath = filepath.Join(dcrdHomeDir, "dcrd.conf")
)

// DCRConfig is passed to the constructor.
type DCRConfig struct {
	// RPCUser is the RPC username provided to dcrd configuration as the rpcuser
	// parameter.
	RPCUser string `long:"rpcuser" description:"Username for RPC connections"`
	// RPCPass is the RPC password provided to dcrd configuration as the rpcpass
	// parameter.
	RPCPass string `long:"rpcpass" description:"Password for RPC connections"`
	// RPCListen is the RPC network address provided to dcrd configuration as the
	// rpclisten parameter. If the value is an empty string, it will be set
	// to a default value for the network.
	RPCListen string `long:"rpclisten" description:"dcrd interface/port for RPC connections (default port: 9109, testnet: 19109)"`
	// RPCCert is the filepath to the dcrd TLS certificate. If it is not
	// provided, the default dcrd location will be assumed.
	RPCCert string `long:"rpccert" description:"File containing the certificate file"`
	// Context should be canceled when the program exits. This will cause some
	// cleanup to be performed during shutdown.
	Context context.Context
}

// loadConfig loads the DCRConfig from file. If no values are found for
// RPCListen or RPCCert in the specified file, default values will be used.
// If configPath is an empty string, loadConfig will attempt to read settings
// directly from the default dcrd.conf filpath. If there is no error, the
// module-level chainParams variable will be set appropriately for the network.
func loadConfig(configPath string, network dex.Network) (*DCRConfig, error) {
	// Check for missing credentials. The user and password must be set.
	cfg := new(DCRConfig)

	// Since we are not reading command-line arguments, and the DCRConfig fields
	// share names with the dcrd configuration options, passing just
	// IgnoreUnknown allows us to have the option to read directly from the
	// dcrd.conf file.
	parser := flags.NewParser(cfg, flags.IgnoreUnknown)

	// If no path provided, use default dcrd path.
	if configPath == "" {
		configPath = defaultConfigPath
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no %q config file found at %s", assetName, configPath)
	} else {
		// The config file exists, so attempt to parse it.
		err = flags.NewIniParser(parser).ParseFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("error parsing %q ini file: %v", assetName, err)
		}
	}

	missing := ""
	if cfg.RPCUser == "" {
		missing += " username"
	}
	if cfg.RPCPass == "" {
		missing += " password"
	}
	if missing != "" {
		return nil, fmt.Errorf("missing dcrd credentials: %s", missing)
	}

	// Get network settings. Configuration defaults to mainnet, but unknown
	// non-empty cfg.Net is an error.
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
