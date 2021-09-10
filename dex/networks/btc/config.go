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
// wallet or backend. When constructed with LoadConfigFromPath or
// LoadConfigFromSettings from a settings map, the following is true:
//  - Default values are used for RPCBind and/or RPCPort if not set.
//  - RPCPort is ignored if RPCBind already includes a port, otherwise if set,
//    RPCPort is joined with the host in RPCBind.
//  - If set, RPCConnect will be reflected in RPCBind, overriding any host
//    originally from that setting.
// In short, RPCBind will contain both host and port that may or may not be
// specified in RPCConnect and RPCPort.
type Config struct {
	RPCUser    string `ini:"rpcuser"`
	RPCPass    string `ini:"rpcpassword"`
	RPCBind    string `ini:"rpcbind"`
	RPCPort    int    `ini:"rpcport"`    // reflected in RPCBind unless it already included a port
	RPCConnect string `ini:"rpcconnect"` // (bitcoin-cli) if set, reflected in RPCBind

	// Below fields are only used on the client. They would not be in the
	// bitcoin.conf file.

	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	FeeRateLimit     float64 `ini:"feeratelimit"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
}

// LoadConfigFromPath loads the configuration settings from the specified filepath.
func LoadConfigFromPath(cfgPath string, name string, network dex.Network, ports NetPorts) (*Config, error) {
	cfg := &Config{}
	if err := config.ParseInto(cfgPath, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}
	return checkConfig(cfg, name, network, ports)
}

// LoadConfigFromSettings loads the configuration settings from a settings map.
func LoadConfigFromSettings(settings map[string]string, name string, network dex.Network, ports NetPorts) (*Config, error) {
	cfg := &Config{}
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing connection settings: %w", err)
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

	// If RPCBind includes a port, it takes precedence over RPCPort.
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
				// Patch cfg.RPCPort for consistency with RPCBind.
				if rpcPort, err := strconv.Atoi(port); err != nil {
					// port was already parsed as an int by SplitHostPort, but
					// be cautious.
					cfg.RPCPort = rpcPort
				}
			}
		}
	}

	// If RPCConnect is set, that's how the user has bitcoin-cli configured, so
	// use that instead of rpcbind's host or default (localhost).
	if cfg.RPCConnect != "" {
		host = cfg.RPCConnect
		// RPCConnect does not include port, so use what we got above.
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
