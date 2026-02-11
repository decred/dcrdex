// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr

/*
Package cxmr provides Go bindings to monero_c, a C wrapper around Monero's
wallet2 library.

# Building

This package requires the monero_c shared library to be installed. The library
can be built from source at https://github.com/MrCyjaneK/monero_c

On Linux, the library should be installed to /usr/local/lib or another path
in the linker search path. Alternatively, set CGO_LDFLAGS:

	CGO_LDFLAGS="-L/path/to/lib" go build

On macOS, you may need to set DYLD_LIBRARY_PATH at runtime:

	DYLD_LIBRARY_PATH=/path/to/lib ./your-binary

On Windows, ensure the DLL is in the PATH or in the same directory as the
executable.

# Usage

	wm := cxmr.GetWalletManager()
	wallet, err := wm.CreateWallet("/path/to/wallet", "password", "English", cxmr.NetworkMainnet)
	if err != nil {
		log.Fatal(err)
	}
	defer wm.CloseWallet(wallet, true)

	// Connect to daemon
	wallet.Init("http://localhost:18081", "", "", false, false, "")
	wallet.Refresh()

	// Get balance
	balance := wallet.Balance(0)
	unlocked := wallet.UnlockedBalance(0)

	// Get address
	addr := wallet.Address(0, 0)

	// Send transaction
	tx, err := wallet.CreateTransaction(destAddr, amount, cxmr.PriorityDefault, 0)
	if err != nil {
		log.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
*/
package cxmr
