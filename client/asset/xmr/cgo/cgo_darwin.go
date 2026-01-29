// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && darwin && cgo

package cgo

// On macOS, the monero_wallet2_api_c library should be in /usr/local/lib
// or specified via CGO_LDFLAGS.
//
// To build with a custom library path:
//   CGO_LDFLAGS="-L/path/to/monero_c/lib -Wl,-rpath,/path/to/monero_c/lib" go build
//
// At runtime, you may need to set DYLD_LIBRARY_PATH if the library is not
// in a standard location:
//   DYLD_LIBRARY_PATH=/path/to/monero_c/lib ./bisonw
