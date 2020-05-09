// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/btcsuite/btcutil"
)

// NetPorts are a set of port to use with the different networks.
type NetPorts struct {
	Mainnet string
	Testnet string
	Simnet  string
}

// RPCPorts are the default BTC ports.
var RPCPorts = NetPorts{
	Mainnet: "8332",
	Testnet: "18332",
	Simnet:  "18443",
}

const (
	defaultHost = "localhost"
)

// Config holds the parameters needed to initialize an RPC connection to a btc
// wallet or backend. Default values are used for RPCBind and/or RPCPort if not
// set.
type Config struct {
	RPCUser string `ini:"rpcuser, JSON-RPC Username, JSON-RPC user"`
	RPCPass string `ini:"rpcpassword, JSON-RPC Password, JSON-RPC password"`
	RPCBind string `ini:"rpcbind, JSON-RPC Address, Can be <addr> or <addr>:<port>, which would override rpcport"`
	RPCPort int    `ini:"rpcport, JSON-RPC Port, JSON-RPC port"`
}

// LoadConfigFromPath loads the configuration settings from the specified filepath.
func LoadConfigFromPath(cfgPath string, name string, network dex.Network, ports NetPorts) (*Config, error) {
	cfg := &Config{}
	if err := config.ParseInto(cfgPath, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config file: %v", err)
	}
	return checkConfig(cfg, name, network, ports)
}

// LoadConfigFromSettings loads the configuration settings from a settings map.
func LoadConfigFromSettings(settings map[string]string, name string, network dex.Network, ports NetPorts) (*Config, error) {
	cfg := &Config{}
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing connection settings: %v", err)
	}
	return checkConfig(cfg, name, network, ports)
}

func checkConfig(cfg *Config, name string, network dex.Network, ports NetPorts) (*Config, error) {
	if cfg.RPCUser == "" {
		return nil, fmt.Errorf("no rpcuser set in %q config file", name)
	}
	if cfg.RPCPass == "" {
		return nil, fmt.Errorf("no rpcpassword set in %q config file", name)
	}

	host := defaultHost
	var port string
	switch network {
	case dex.Mainnet:
		port = ports.Mainnet
	case dex.Testnet:
		port = ports.Testnet
	case dex.Regtest:
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

// SystemConfigPath will return the default config file path for bitcoin-like
// assets.
func SystemConfigPath(asset string) string {
	homeDir := btcutil.AppDataDir(asset, false)
	return filepath.Join(homeDir, fmt.Sprintf("%s.conf", asset))
}
