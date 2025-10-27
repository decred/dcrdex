// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package lbc

import "decred.org/dcrdex/dex"

var UnitInfo = dex.UnitInfo{
	AtomicUnit: "Sats",
	Conventional: dex.Denomination{
		Unit:             "LBC",
		ConversionFactor: 1e8,
	},
	Alternatives: []dex.Denomination{
		{
			Unit:             "mLBC",
			ConversionFactor: 1e5,
		},
		{
			Unit:             "µLBC",
			ConversionFactor: 1e2,
		},
	},
	FeeRateDenom: "fee",
}
