// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import "decred.org/dcrdex/server/mesh"

const (
	commandKindPostBond           = "postbond"
	commandKindCreatePrepaidBonds = "create_prepaid_bonds"
	commandKindForgiveReputation  = "forgive_reputation"
)

// Commands returns the mesh command handlers.
func (auth *AuthManager) Commands() map[string]mesh.CommandExecutor {
	return map[string]mesh.CommandExecutor{
		commandKindPostBond:           auth.executePostBond,
		commandKindCreatePrepaidBonds: auth.executeCreatePrepaidBonds,
		commandKindForgiveReputation:  auth.executeForgiveReputation,
	}
}
