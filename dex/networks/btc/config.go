// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"decred.org/dcrdex/dex"
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

// RPCConfig holds the parameters needed to initialize an RPC connection to a btc
// wallet or backend. Default values are used for RPCBind and/or RPCPort if not
// set.
type RPCConfig struct {
	RPCUser    string `ini:"rpcuser"`
	RPCPass    string `ini:"rpcpassword"`
	RPCBind    string `ini:"rpcbind"`
	RPCPort    int    `ini:"rpcport"`
	RPCConnect string `ini:"rpcconnect"` // (bitcoin-cli) if set, reflected in RPCBind
}

func CheckRPCConfig(cfg *RPCConfig, name string, network dex.Network, ports NetPorts) error {
	if cfg.RPCUser == "" {
		return fmt.Errorf("no rpcuser set in %q config file", name)
	}
	if cfg.RPCPass == "" {
		return fmt.Errorf("no rpcpassword set in %q config file", name)
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
		return fmt.Errorf("unknown network ID %v", network)
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

	return nil
}

// SystemConfigPath will return the default config file path for bitcoin-like
// assets.
func SystemConfigPath(asset string) string {
	homeDir := btcutil.AppDataDir(asset, false)
	return filepath.Join(homeDir, fmt.Sprintf("%s.conf", asset))
}
