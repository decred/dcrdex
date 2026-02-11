// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && darwin && cgo

package cxmr

// On macOS, the libwallet2_api_c.dylib library is included in
// client/asset/xmr/lib/darwin-amd64/ (Intel) or darwin-arm64/ (Apple Silicon)
// and linked automatically via cgo LDFLAGS in wallet.go.
//
// At runtime, copy the appropriate library next to the executable:
//
//	# Apple Silicon (M1/M2/M3)
//	cp client/asset/xmr/lib/darwin-arm64/libwallet2_api_c.dylib ./
//
//	# Intel
//	cp client/asset/xmr/lib/darwin-amd64/libwallet2_api_c.dylib ./
//
// Or install system-wide:
//
//	sudo cp client/asset/xmr/lib/darwin-*/libwallet2_api_c.dylib /usr/local/lib/
