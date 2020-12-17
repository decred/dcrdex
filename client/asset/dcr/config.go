// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
)

const (
	defaultMainnet  = "localhost:9110"
	defaultTestnet3 = "localhost:19110"
	defaultSimnet   = "localhost:19557"
)

var (
	// A global *chaincfg.Params will be set if loadConfig completes without
	// error.
	chainParams       *chaincfg.Params
	dcrwHomeDir       = dcrutil.AppDataDir("dcrwallet", false)
	defaultRPCCert    = filepath.Join(dcrwHomeDir, "rpc.cert")
	defaultConfigPath = filepath.Join(dcrwHomeDir, "dcrwallet.conf")
)

// Config holds the parameters needed to initialize an RPC connection to a dcr
// wallet. Default values are used for RPCListen and/or RPCCert if not set.
type Config struct {
	RPCUser          string  `ini:"username"`
	RPCPass          string  `ini:"password"`
	RPCListen        string  `ini:"rpclisten"`
	RPCCert          string  `ini:"rpccert"`
	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
	// Context should be canceled when the application exits. This will cause
	// some cleanup to be performed during shutdown.
	Context context.Context `ini:"-"`
}

// loadConfig loads the Config from a settings map. If no values are found for
// RPCListen or RPCCert in the specified file, default values will be used. If
// there is no error, the module-level chainParams variable will be set
// appropriately for the network.
func loadConfig(settings map[string]string, network dex.Network) (*Config, error) {
	cfg := new(Config)
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
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
