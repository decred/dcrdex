// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import "decred.org/dcrdex/dex"

var UnitInfo = dex.UnitInfo{
	AtomicUnit: "atoms",
	Conventional: dex.Denomination{
		Unit:             "DCR",
		ConversionFactor: 1e8,
	},
}
