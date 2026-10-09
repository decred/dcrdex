// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"

	"decred.org/dcrdex/server/mesh"
)

func mergeMeshCommands(dst map[string]mesh.CommandExecutor, src map[string]mesh.CommandExecutor) error {
	for kind, exec := range src {
		if kind == "" {
			return fmt.Errorf("empty mesh command kind")
		}
		if exec == nil {
			return fmt.Errorf("nil mesh command handler for %q", kind)
		}
		if dst[kind] != nil {
			return fmt.Errorf("duplicate mesh command kind %q", kind)
		}
		dst[kind] = exec
	}
	return nil
}

func mergeMeshEvents(dst map[string]mesh.EventApplier, src map[string]mesh.EventApplier) error {
	for kind, apply := range src {
		if kind == "" {
			return fmt.Errorf("empty mesh event kind")
		}
		if apply == nil {
			return fmt.Errorf("nil mesh event handler for %q", kind)
		}
		if dst[kind] != nil {
			return fmt.Errorf("duplicate mesh event kind %q", kind)
		}
		dst[kind] = apply
	}
	return nil
}
