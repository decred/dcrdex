// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jessevdk/go-flags"
)

var (
	ethHomeDir = dcrutil.AppDataDir("ethereum", false)
	defaultIPC = filepath.Join(ethHomeDir, "geth/geth.ipc")
)

type config struct {
	// IPC is the location of the inner process communication socket.
	IPC string `long:"ipc" description:"Location of the geth ipc socket."`
	// ContractAddr is the base 58 address of the already deployed swap
	// contract to use in swaps.
	ContractAddr string `long:"contractaddr" description:"Address of the deployed eth swap contract."`
}

// load checks the network and sets the ipc location if not supplied.
//
// TODO: Test this with windows.
func loadConfig(configPath string, network dex.Network) (*config, error) {
	switch network {
	case dex.Simnet:
	case dex.Testnet:
	case dex.Mainnet:
		// TODO: Allow.
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}

	cfg := new(config)

	parser := flags.NewParser(cfg, flags.None)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no %q config file found at %s", assetName, configPath)
	}

	// The config file exists, so attempt to parse it.
	err := flags.NewIniParser(parser).ParseFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing %q config file: %w", assetName, err)
	}

	if cfg.IPC == "" {
		cfg.IPC = defaultIPC
	}

	if cfg.ContractAddr == "" {
		return nil, errors.New("no swap contract address specified in config file")
	}
	if !common.IsHexAddress(cfg.ContractAddr) {
		return nil, errors.New("contract address is structually invalid")
	}

	return cfg, nil
}
