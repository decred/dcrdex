// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build !systray

package main

import (
	"fmt"
	"os"
)

func main() {
	// Wrap the actual main so defers run in it.
	// Parse configuration.
	cfg, err := configure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v", err)
		os.Exit(1)
	}
	err = runCore(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
