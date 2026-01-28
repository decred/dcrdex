// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build !xmr

// Package xmr provides Monero wallet functionality.
//
// This package requires the monero_c library and must be built with the
// "xmr" build tag:
//
//	go build -tags xmr
//
// See client/asset/xmr/cxmr/doc.go for library installation instructions.
package xmr
