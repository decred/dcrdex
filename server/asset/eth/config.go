// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v3"
)

var (
	ethHomeDir = dcrutil.AppDataDir("ethereum", false)
	defaultIPC = filepath.Join(ethHomeDir, "geth/geth.ipc")
)

type config struct {
	// IPC is the location of the inner process communication socket.
	IPC string `long:"ipc" description:"Location of the geth ipc socket."`
}

// load checks the network and sets the ipc location if not supplied.
//
// TODO: Test this with windows.
func load(IPC string, network dex.Network) (*config, error) {
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
		IPC: IPC,
	}

	if cfg.IPC == "" {
		cfg.IPC = defaultIPC
	}

	return cfg, nil
}
