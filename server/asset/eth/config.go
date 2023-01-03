// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"path/filepath"

	"github.com/decred/dcrd/dcrutil/v4"
)

var (
	ethHomeDir = dcrutil.AppDataDir("ethereum", false)
	defaultIPC = filepath.Join(ethHomeDir, "geth/geth.ipc")
)

// For tokens, the file at the config path can contain overrides for
// token gas values. Gas used for token swaps is dependent on the token contract
// implementation, and can change without notice. The operator can specify
// custom gas values to be used for funding balance validation calculations.
type configuredTokenGases struct {
	Swap   uint64 `ini:"swap"`
	Redeem uint64 `ini:"redeem"`
}
