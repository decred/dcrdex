//go:build !harness && !botlive

package core

import (
	"errors"
	"testing"

	"decred.org/dcrdex/dex"
)

type tBasisPricer struct {
	price       *stampedPrice
	conversions map[uint32]float64
}

func (bp *tBasisPricer) cachedOraclePrice(mktName string) *stampedPrice {
	return bp.price
}

func (bp *tBasisPricer) fiatConversions() map[uint32]float64 {
	return bp.conversions
}

func TestBasisPrice(t *testing.T) {
	mkt := &Market{
		RateStep:   1,
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
		AtomToConv: 1,
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	tests := []*struct {
		name         string
		midGap       uint64
		oraclePrice  uint64
		oracleBias   float64
		oracleWeight float64
		conversions  map[uint32]float64
		exp          uint64
	}{
		{
			name:   "just mid-gap is enough",
			midGap: 123e5,
			exp:    123e5,
		},
		{
			name:         "mid-gap + oracle weight",
			midGap:       1950,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          1975,
		},
		{
			name:         "adjusted mid-gap + oracle weight",
			midGap:       1000, // adjusted to 1940
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          1970,
		},
		{
			name:         "no mid-gap effectively sets oracle weight to 100%",
			midGap:       0,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          2000,
		},
		{
			name:         "mid-gap + oracle weight + oracle bias",
			midGap:       1950,
			oraclePrice:  2000,
			oracleBias:   -0.01, // minus 20
			oracleWeight: 0.75,
			exp:          1972, // 0.25 * 1950 + 0.75 * (2000 - 20) = 1972
		},
		{
			name:         "no mid-gap and no oracle weight falls back to fiat ratio",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			conversions: map[uint32]float64{
				btcBipID: 80,
				dcrBipID: 2,
			},
			exp: 2500000, // (2 / 80) * 1e8
		},
		{
			name:         "no mid-gap and no oracle weight and a missing fiat conversion fails to produce result",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			conversions: map[uint32]float64{
				btcBipID: 80,
			},
			exp: 0,
		},
	}

	for _, tt := range tests {
		bp := &tBasisPricer{
			price:       &stampedPrice{price: mkt.MsgRateToConventional(tt.oraclePrice)},
			conversions: tt.conversions,
		}
		rate := basisPrice("", mkt, tt.oracleBias, tt.oracleWeight, tt.midGap, bp, log)
		if rate != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, rate, tt.exp)
		}
	}
}

type tBotFeeEstimator struct {
	buySwap, buyRedeem, sellSwap, sellRedeem uint64
	buyErr, sellErr                          error
}

func (fe *tBotFeeEstimator) feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error) {
	if form.Sell {
		return fe.sellSwap, fe.sellRedeem, fe.sellErr
	}
	return fe.buySwap, fe.buyRedeem, fe.buyErr
}

func TestBreakEvenHalfSpread(t *testing.T) {
	mkt := &Market{
		LotSize:    20e8, // 20 DCR
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
		AtomToConv: 1,
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	tests := []*struct {
		name           string
		basisPrice     uint64
		sellSwapFees   uint64
		sellRedeemFees uint64
		buySwapFees    uint64
		buyRedeemFees  uint64
		exp            uint64
		buyErr         error
		sellErr        error
		expErr         bool
	}{
		{
			name:   "basis price = 0 not allowed",
			expErr: true,
		},
		{
			name:   "estimator buy error propagates",
			buyErr: errors.New("t"),
			expErr: true,
		},
		{
			name:    "estimator sell error propagates",
			sellErr: errors.New("t"),
			expErr:  true,
		},
		{
			name:           "simple",
			basisPrice:     4e7, // 0.4 BTC/DCR, quote lot = 8 BTC
			buySwapFees:    200, // BTC
			buyRedeemFees:  100, // DCR
			sellSwapFees:   300, // DCR
			sellRedeemFees: 50,  // BTC
			// total btc (quote) fees, Q = 250
			// total dcr (base) fees, B = 400
			// g = (pB + Q) / (B + 2L)
			// g = (0.4*400 + 250) / (400 + 40e8) = 1.02e-7 // atomic units
			// g = 10 // msg-rate units
			exp: 10,
		},
	}

	for _, tt := range tests {
		fe := &tBotFeeEstimator{
			sellSwap:   tt.sellSwapFees,
			sellRedeem: tt.sellRedeemFees,
			buySwap:    tt.buySwapFees,
			buyRedeem:  tt.buyRedeemFees,
			buyErr:     tt.buyErr,
			sellErr:    tt.sellErr,
		}

		halfSpread, err := breakEvenHalfSpread("", mkt, tt.basisPrice, fe, log)
		if (err != nil) != tt.expErr {
			t.Fatalf("expErr = %t, err = %v", tt.expErr, err)
		}
		if halfSpread != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, halfSpread, tt.exp)
		}
	}
}
