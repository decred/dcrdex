// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.
//
// When using eth, this app also imports go-ethereum code and so carries the
// burden of go-ethereum's GNU Lesser General Public License.

//go:build lgpl

package main

import (
	_ "decred.org/dcrdex/client/asset/eth" // register eth asset
)
