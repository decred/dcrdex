// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcutil"
	flags "github.com/jessevdk/go-flags"
)

type NetPorts struct {
	Mainnet string
	Testnet string
	Simnet  string
}

var btcPorts = NetPorts{
	Mainnet: "8332",
	Testnet: "18332",
	Simnet:  "18443",
}

const (
	defaultHost = "localhost"
)

type Config struct {
	RPCUser string `long:"rpcuser" description:"JSON-RPC user"`
	RPCPass string `long:"rpcpassword" description:"JSON-RPC password"`
	RPCBind string `long:"rpcbind" description:"RPC address. Can be <addr> or <addr>:<port>, which would override rpcport"`
	RPCPort int    `long:"rpcport" description:"JSON-RPC port"`
}

func LoadConfig(configPath string, network asset.Network, ports NetPorts) (*Config, error) {
	cfg := &Config{}
	// Since we are not reading command-line arguments, and the Config fields
	// share names with the bitcoind configuration options, passing just
	// IgnoreUnknown allows us to have the option to read directly from the
	// bitcoin.conf file.
	parser := flags.NewParser(cfg, flags.IgnoreUnknown)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no BTC config file found at %s", configPath)
	}
	// The config file exists, so attempt to parse it.
	err := flags.NewIniParser(parser).ParseFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing BTC ini file: %v", err)
	}

	if cfg.RPCUser == "" {
		return nil, fmt.Errorf("no rpcuser set in BTC config file")
	}
	if cfg.RPCPass == "" {
		return nil, fmt.Errorf("no rpcpassword set in BTC config file")
	}

	host := defaultHost
	var port string
	switch network {
	case asset.Mainnet:
		port = ports.Mainnet
	case asset.Testnet:
		port = ports.Testnet
	case asset.Regtest:
		port = ports.Simnet
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// RPCPort overrides network default
	if cfg.RPCPort != 0 {
		port = strconv.Itoa(cfg.RPCPort)
	}

	// if RPCBind includes a port, it takes precedence over RPCPort
	if cfg.RPCBind != "" {
		h, p, err := net.SplitHostPort(cfg.RPCBind)
		if err != nil {
			// Will error for i.e. "localhost", but not for "localhost:" or ":1234"
			host = cfg.RPCBind
		} else {
			if h != "" {
				host = h
			}
			if p != "" {
				port = p
			}
		}
	}

	// overwrite rpcbind to use for rpcclient connection
	cfg.RPCBind = net.JoinHostPort(host, port)

	return cfg, nil
}

func SystemConfigPath(asset string) string {
	homeDir := btcutil.AppDataDir(asset, false)
	return filepath.Join(homeDir, fmt.Sprintf("%s.conf", asset))
}
