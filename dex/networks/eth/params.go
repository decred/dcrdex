// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import "decred.org/dcrdex/dex"

var UnitInfo = dex.UnitInfo{
	AtomicUnit: "gwei",
	Conventional: dex.Denomination{
		Unit:             "ETH",
		ConversionFactor: 1e9,
	},
}
