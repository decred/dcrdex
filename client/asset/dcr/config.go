// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	defaultMainnet  = "localhost:9110"
	defaultTestnet3 = "localhost:19110"
	defaultSimnet   = "localhost:19557"
)

var (
	// A global *chaincfg.Params will be set if loadConfig completes without
	// error.
	dcrwHomeDir       = dcrutil.AppDataDir("dcrwallet", false)
	defaultRPCCert    = filepath.Join(dcrwHomeDir, "rpc.cert")
	defaultConfigPath = filepath.Join(dcrwHomeDir, "dcrwallet.conf")

	// May 26, 2022
	defaultWalletBirthdayUnix = 1653599386
)

type walletConfig struct {
	PrimaryAccount   string  `ini:"account"`
	UnmixedAccount   string  `ini:"unmixedaccount"`
	TradingAccount   string  `ini:"tradingaccount"`
	UseSplitTx       bool    `ini:"txsplit"`
	FallbackFeeRate  float64 `ini:"fallbackfee"`
	FeeRateLimit     float64 `ini:"feeratelimit"`
	RedeemConfTarget uint64  `ini:"redeemconftarget"`
	ActivelyUsed     bool    `ini:"special:activelyUsed"` //injected by core
}

type rpcConfig struct {
	Account   string `ini:"account"`
	RPCUser   string `ini:"username"`
	RPCPass   string `ini:"password"`
	RPCListen string `ini:"rpclisten"`
	RPCCert   string `ini:"rpccert"`
}

func loadRPCConfig(settings map[string]string, network dex.Network) (*rpcConfig, *chaincfg.Params, error) {
	cfg := new(rpcConfig)
	chainParams, err := loadConfig(settings, network, cfg)
	if err != nil {
		return nil, nil, err
	}
	var defaultServer string
	switch network {
	case dex.Simnet:
		defaultServer = defaultSimnet
	case dex.Testnet:
		defaultServer = defaultTestnet3
	case dex.Mainnet:
		defaultServer = defaultMainnet
	default:
		return nil, nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
	if cfg.RPCListen == "" {
		cfg.RPCListen = defaultServer
	}
	if cfg.RPCCert == "" {
		cfg.RPCCert = defaultRPCCert
	} else {
		cfg.RPCCert = dex.CleanAndExpandPath(cfg.RPCCert)
	}
	return cfg, chainParams, nil
}

// loadConfig loads the Config from a settings map. If no values are found for
// RPCListen or RPCCert in the specified file, default values will be used. If
// there is no error, the module-level chainParams variable will be set
// appropriately for the network.
func loadConfig(settings map[string]string, network dex.Network, cfg interface{}) (*chaincfg.Params, error) {
	if err := config.Unmapify(settings, cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return parseChainParams(network)
}

func parseChainParams(network dex.Network) (*chaincfg.Params, error) {
	// Get network settings. Zero value is mainnet, but unknown non-zero cfg.Net
	// is an error.
	switch network {
	case dex.Simnet:
		return chaincfg.SimNetParams(), nil
	case dex.Testnet:
		return chaincfg.TestNet3Params(), nil
	case dex.Mainnet:
		return chaincfg.MainNetParams(), nil
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}
}
