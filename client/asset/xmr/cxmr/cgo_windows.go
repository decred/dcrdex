// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr && windows && cgo

package cxmr

// On Windows, the libwallet2_api_c.dll library is included in
// client/asset/xmr/lib/windows-amd64/ and linked automatically via cgo
// LDFLAGS in wallet.go.
//
// At runtime, copy the DLL next to the executable:
//
//	copy client\asset\xmr\lib\windows-amd64\libwallet2_api_c.dll .\
//
// Windows searches for DLLs in the executable's directory first, so this
// is the simplest approach.
