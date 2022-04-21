// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package doge

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure. For details on DOGE-specific limits, see:
	// https://github.com/dogecoin/dogecoin/blob/master/doc/fee-recommendation.md
	// and https://github.com/dogecoin/dogecoin/discussions/2347
	// Dogecoin Core v1.14.5 adopts proposed fees for tx creation:
	// https://github.com/dogecoin/dogecoin/releases/tag/v1.14.5
	// https://github.com/dogecoin/dogecoin/commit/9c6af6d84179e46002338bb5b9a69c6f2367c731
	// These limits were applied to mining and relay in v1.14.4.
	DefaultFee          = 4_000  // 0.04 DOGE/kB, 4x the 0.01 recommended by dogecoin core (DEFAULT_TRANSACTION_FEE)
	DefaultFeeRateLimit = 50_000 // 0.5 DOGE/kB, where v1.14.5 considers 1.0 DOGE/kB "high" (HIGH_TX_FEE_PER_KB)
)

var (
	UnitInfo = dex.UnitInfo{
		AtomicUnit: "Sats",
		Conventional: dex.Denomination{
			Unit:             "DOGE",
			ConversionFactor: 1e8,
		},
	}

	// MainNetParams are the clone parameters for mainnet.
	MainNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x1e,
		ScriptHashAddrID: 0x16,
		CoinbaseMaturity: 30,
		Net:              0xc0c0c0c0,
	})
	// TestNet4Params are the clone parameters for testnet.
	TestNet4Params = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x71,
		ScriptHashAddrID: 0xc4,
		CoinbaseMaturity: 30,
		Net:              0xfcc1b7dc,
	})
	// RegressionNetParams are the clone parameters for simnet.
	RegressionNetParams = btc.ReadCloneParams(&btc.CloneParams{
		PubKeyHashAddrID: 0x6f,
		ScriptHashAddrID: 0xc4,
		CoinbaseMaturity: 60,
		// Net is not the standard for DOGE simnet, since they never changed it
		// from the BTC value. The only place we currently use Net is in
		// btcd/chaincfg.Register, where it is checked to prevent duplicate
		// registration, so our only requirement is that it is unique. This one
		// was just generated with a prng.
		Net: 0xfabfb5da,
	})
)

func init() {
	for _, params := range []*chaincfg.Params{MainNetParams, TestNet4Params, RegressionNetParams} {
		err := chaincfg.Register(params)
		if err != nil {
			panic("failed to register doge parameters: " + err.Error())
		}
	}
}
