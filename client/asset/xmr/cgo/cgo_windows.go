// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && windows && cgo

package cgo

// On Windows, the monero_wallet2_api_c.dll should be in the PATH or in the
// same directory as the executable.
//
// To build with a custom library path:
//   CGO_LDFLAGS="-L/path/to/monero_c/lib" go build
//
// At runtime, ensure the DLL is findable by Windows' library search order.
