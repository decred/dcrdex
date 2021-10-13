// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v4"
)

var (
	ethHomeDir = dcrutil.AppDataDir("ethereum", false)
	defaultIPC = filepath.Join(ethHomeDir, "geth/geth.ipc")
)

type config struct {
	// ipc is the location of the inner process communication socket.
	ipc string
	// network is the network the dex is meant to be running on.
	network dex.Network
}

// load checks the network and sets the ipc location if not supplied.
//
// TODO: Test this with windows.
func load(ipc string, network dex.Network) (*config, error) {
	switch network {
	case dex.Simnet:
	case dex.Testnet:
	case dex.Mainnet:
		// TODO: Allow.
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(network))
	}

	cfg := &config{
		ipc:     ipc,
		network: network,
	}

	if cfg.ipc == "" {
		cfg.ipc = defaultIPC
	}

	return cfg, nil
}
