// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && linux && cgo

package cgo

// On Linux, the monero_wallet2_api_c library should be in a standard
// library path (/usr/local/lib, /usr/lib) or specified via CGO_LDFLAGS.
//
// To build with a custom library path:
//   CGO_LDFLAGS="-L/path/to/monero_c/lib -Wl,-rpath,/path/to/monero_c/lib" go build
//
// The -Wl,-rpath option embeds the library path into the binary so it can
// find the library at runtime without setting LD_LIBRARY_PATH.
