// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

const (
	mainnetName     = "mainnet"
	testnetName     = "testnet3"
	simnetName      = "simnet"
	defaultMainnet  = "localhost:9109"
	defaultTestnet3 = "localhost:19109"
	defaultSimnet   = "localhost:19556"
)

var (
	// A global *chaincfg.Params will be set if tidyConfig completes without
	// error.
	chainParams              *chaincfg.Params
	dcrdHomeDir              = dcrutil.AppDataDir("dcrd", false)
	defaultDaemonRPCCertFile = filepath.Join(dcrdHomeDir, "rpc.cert")
)

// DCRConfig is passed to the constructor.
type DCRConfig struct {
	// Net should one of "mainnet", "testnet3", "simnet". Any other value will
	// cause an tidyConfig error.
	Net string
	// DcrdUser is the RPC username provided to dcrd configuration as the rpcuser
	// parameter.
	DcrdUser string
	// DcrdPass is the RPC password provided to dcrd configuration as the rpcpass
	// parameter.
	DcrdPass string
	// DcrdServ is the RPC network address provided to dcrd configuration as the
	// rpclisten parameter. If the value is an empty string, it will be set
	// to a default value for the network.
	DcrdServ string
	// DcrdServ is the filepath to the dcrd TLS certificate. If it is not
	// provided, the default dcrd location will be assumed.
	DcrdCert string
	// Context should be cancelled when the program exits. This will cause some
	// cleanup to be performed during shutdown.
	Context context.Context
}

// tidyConfig initializes and parses the DCRConfig. If there is no error, the
// module-level chainParams variable will be set appropriately for the network.
func tidyConfig(cfg *DCRConfig) error {
	// Check for missing credentials. The user and password must be set.
	missing := ""
	if cfg.DcrdUser == "" {
		missing += " username"
	}
	if cfg.DcrdPass == "" {
		missing += " password"
	}
	if missing != "" {
		return fmt.Errorf("missing dcrd credentials:%s", missing)
	}

	// Get network settings. Configuration defaults to mainnet, but unknown
	// non-empty cfg.Net is an error.
	var defaultServer string
	switch cfg.Net {
	case simnetName:
		chainParams = chaincfg.SimNetParams()
		defaultServer = defaultSimnet
	case testnetName:
		chainParams = chaincfg.TestNet3Params()
		defaultServer = defaultTestnet3
	case mainnetName, "":
		chainParams = chaincfg.MainNetParams()
		defaultServer = defaultMainnet
	default:
		return fmt.Errorf("unknown network: %s", cfg.Net)
	}
	if cfg.DcrdServ == "" {
		cfg.DcrdServ = defaultServer
	}
	if cfg.DcrdCert == "" {
		cfg.DcrdCert = defaultDaemonRPCCertFile
	}

	return nil
}
