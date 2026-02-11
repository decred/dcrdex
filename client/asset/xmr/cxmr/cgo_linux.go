// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && linux && cgo

package cxmr

// On Linux, the libwallet2_api_c.so library is included in
// client/asset/xmr/lib/linux-amd64/ and linked automatically via cgo
// LDFLAGS in wallet.go.
//
// At runtime, the library is searched for in the following order:
//  1. Same directory as the executable ($ORIGIN)
//  2. /usr/lib/bisonw/
//
// For development, copy the library next to the executable:
//
//	cp client/asset/xmr/lib/linux-amd64/libwallet2_api_c.so ./
//
// For system packaging, install to the application library directory:
//
//	sudo mkdir -p /usr/lib/bisonw
//	sudo cp client/asset/xmr/lib/linux-amd64/libwallet2_api_c.so /usr/lib/bisonw/
