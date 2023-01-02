// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/jessevdk/go-flags"
)

const exampleConf = `; ws://address:port of the authorized port or ipc filepath of local full geth node
addr=ws://123.123.123.123:12345 or ~/.geth/geth.ipc
; jwt hex secret shared with a geth full node when connecting remotely over websocket
; can also be a file path to the jwt secret. Not needed for ipc
jwt=0xabababababababababababababababababababababababababababababababab
`

var (
	exConfStr  = fmt.Sprintf("\n\nExample config contents:\n\n%s\n", exampleConf)
	ethHomeDir = dcrutil.AppDataDir("ethereum", false)
	defaultIPC = filepath.Join(ethHomeDir, "geth/geth.ipc")
)

type config struct {
	// ADDR is the location to connect to. Can be a UNIX ipc file or the
	// address of a full geth node's authorized port. The geth node must
	// have a jwt secret set to be active.
	ADDR string `long:"addr" description:"Location of ipc file or ws://address:port of a geth full node's authorized port."`
	// A 32 byte hex shared with the full geth node, used to insert a
	// signed token into the websocket connection request's header and
	// needed for communication over websocket. Not needed for ipc
	// communication. Can also be a file that contains the hex.
	JWT string `long:"jwt" description:"The jwt secret or path to secret file needed to connect to a geth full node if connecting over websocket."`
}

// For tokens, the file at the config path can contain overrides for
// token gas values. Gas used for token swaps is dependent on the token contract
// implementation, and can change without notice. The operator can specify
// custom gas values to be used for funding balance validation calculations.
type configuredTokenGases struct {
	Swap   uint64 `ini:"swap"`
	Redeem uint64 `ini:"redeem"`
}

// loadConfig loads the config from file. If configPath is an empty string,
// loadConfig will attempt to read settings directly from the default geth.conf
// file path. If there is no error, the module-level chainParams variable will
// be set appropriately for the network.
func loadConfig(configPath string, net dex.Network, logger dex.Logger) (*config, error) {
	switch net {
	case dex.Simnet:
	case dex.Testnet:
	case dex.Mainnet:
		// TODO: Allow. When?
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", net)
	}

	cfg := new(config)

	// NOTE: ipc only is deprecated. Both ipc and websocket are ok! Check
	// if the ipc file location, rather than a config file location, is set
	// in markets.json. Remove this check at some point as it is only
	// relevant for developers.
	if strings.HasSuffix(configPath, ".ipc") || configPath == "" {
		ipc := configPath
		if ipc == "" {
			ipc = defaultIPC
		}
		cfg.ADDR = ipc
		logger.Warnf("Geth ipc location is set in markets.json. The ipc "+
			"location should be included in a new file and that file's "+
			"location included in markets.json.%s", exConfStr)
		return cfg, nil
	}

	// IgnoreUnknown allows us to have the option to read directly from the
	// geth.conf file.
	parser := flags.NewParser(cfg, flags.IgnoreUnknown)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no eth config file found at %s", configPath)
	}

	// The config file exists, so attempt to parse it.
	err := flags.NewIniParser(parser).ParseFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing eth ini file: %w", err)
	}

	// Check for missing credentials.
	if cfg.ADDR == "" {
		return nil, fmt.Errorf("config missing addr: %s", exConfStr)
	}

	if !strings.HasSuffix(cfg.ADDR, ".ipc") {
		if cfg.JWT == "" {
			return nil, fmt.Errorf("config missing jwt secret: %s", exConfStr)
		}
		if cfg.JWT, err = dexeth.FindJWTHex(cfg.JWT); err != nil {
			return nil, fmt.Errorf("problem with jwt hex: %v: %s", err, exConfStr)
		}
	}

	if strings.HasSuffix(cfg.ADDR, ".ipc") {
		// Clean file path.
		cfg.ADDR = dex.CleanAndExpandPath(cfg.ADDR)
	}

	return cfg, nil
}
