// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"decred.org/dcrdex/server/mesh"
)

const (
	commandKindInit   = "init"
	commandKindRedeem = "redeem"
)

// Commands returns the mesh command handlers.
func (s *Swapper) Commands() map[string]mesh.CommandExecutor {
	return map[string]mesh.CommandExecutor{
		commandKindInit:   s.executeInit,
		commandKindRedeem: s.executeRedeem,
	}
}
