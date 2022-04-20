// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import "decred.org/dcrdex/dex"

var UnitInfo = dex.UnitInfo{
	AtomicUnit: "Sats",
	Conventional: dex.Denomination{
		Unit:             "BTC",
		ConversionFactor: 1e8,
	},
	Alternatives: []dex.Denomination{
		{
			Unit:             "mBTC",
			ConversionFactor: 1e5,
		},
		{
			Unit:             "µBTC",
			ConversionFactor: 1e2,
		},
	},
	FeeRateUnit: "Sats/B",
}
