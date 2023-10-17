// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcutil"
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

// RPCConfig holds the parameters needed to initialize an RPC connection to a btc
// wallet or backend. Default values are used for RPCBind and/or RPCPort if not
// set.
type RPCConfig struct {
	RPCUser    string `ini:"rpcuser"`
	RPCPass    string `ini:"rpcpassword"`
	RPCBind    string `ini:"rpcbind"`
	RPCPort    int    `ini:"rpcport"`
	RPCConnect string `ini:"rpcconnect"` // (bitcoin-cli) if set, reflected in RPCBind
	// IsPublicProvider: Set rpcbind with a URL with protocol https, and we'll
	// assume it's a public RPC provider. This means that we assume TLS and
	// permit omission of the RPCUser and RPCPass, since they might be encoded
	// in the URL.
	IsPublicProvider bool
}

func CheckRPCConfig(cfg *RPCConfig, name string, network dex.Network, ports NetPorts) error {

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

	StandardizeRPCConf(cfg, port)

	// When using a public provider, the credentials can be in the url's path.
	if !cfg.IsPublicProvider {
		if cfg.RPCUser == "" {
			return fmt.Errorf("no rpcuser set in %q config file", name)
		}
		if cfg.RPCPass == "" {
			return fmt.Errorf("no rpcpassword set in %q config file", name)
		}
	}

	return nil
}

// StandardizeRPCConf standardizes the RPCBind and RPCPort fields, and returns
// the updated RPCBind field. defaultPort must be either an empty string or a
// valid representation of a positive 16-bit integer.
func StandardizeRPCConf(cfg *RPCConfig, defaultPort string) {
	host := "127.0.0.1" // default if not in RPCBind or RPCConnect
	port := strconv.Itoa(cfg.RPCPort)
	if cfg.RPCPort <= 0 {
		port = defaultPort
	}

	if cfg.RPCBind != "" {
		// Allow RPC providers
		if strings.HasPrefix(cfg.RPCBind, "https://") {
			cfg.IsPublicProvider = true
			cfg.RPCBind = cfg.RPCBind[len("https://"):]
			port = ""
		}

		h, p, err := net.SplitHostPort(cfg.RPCBind)
		if err != nil {
			// Will error for i.e. "localhost", but not for "localhost:" or ":1234"
			host = cfg.RPCBind // use port from RPCPort
		} else {
			if h != "" {
				host = h
			}
			// If RPCBind includes a port, it takes precedence over RPCPort.
			if p != "" {
				port = p
			}
		}
	}

	// If RPCConnect is set, that's how the user has bitcoin-cli configured, so
	// use that instead of rpcbind's host or default (localhost).
	if cfg.RPCConnect != "" {
		host = cfg.RPCConnect
		// RPCConnect does not include port, so use what we got above.
	}

	if port != "" {
		cfg.RPCBind = net.JoinHostPort(host, port)
		cfg.RPCPort, _ = strconv.Atoi(port)
	} else {
		cfg.RPCBind = host
	}
}

// SystemConfigPath will return the default config file path for bitcoin-like
// assets.
func SystemConfigPath(asset string) string {
	homeDir := btcutil.AppDataDir(asset, false)
	return filepath.Join(homeDir, fmt.Sprintf("%s.conf", asset))
}
