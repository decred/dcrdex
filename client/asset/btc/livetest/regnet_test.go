// +build harness

package livetest

import (
	"testing"

	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
)

const (
	alphaAddress = "mjqAiNeRe8jWzTUnEYL9CYa2YKjFWQDjJY"
)

var (
	tBTC = &dex.Asset{
		ID:       0,
		Symbol:   "btc",
		SwapSize: dexbtc.InitTxSize,
		FeeRate:  2,
		LotSize:  1e6,
		RateStep: 10,
		SwapConf: 1,
		FundConf: 1,
	}
)

func TestWallet(t *testing.T) {
	Run(t, btc.NewWallet, alphaAddress, tBTC)
}
