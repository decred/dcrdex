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
	err := runCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
