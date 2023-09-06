// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.
//
// By default, this app also imports go-ethereum code and so carries the burden
// of go-ethereum's GNU Lesser General Public License. If that is unacceptable,
// build with the nolgpl tag.

//go:build !nolgpl

package app

import (
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon network
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
)

func init() {
	dexeth.MaybeReadSimnetAddrs()
	dexpolygon.MaybeReadSimnetAddrs()

}
