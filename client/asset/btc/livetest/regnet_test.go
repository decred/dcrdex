// +build harness

package livetest

import (
	"fmt"
	"testing"

	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

const (
	alphaAddress = "mjqAiNeRe8jWzTUnEYL9CYa2YKjFWQDjJY"
)

var (
	tBTC = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   10,
		LotSize:      1e6,
		RateStep:     10,
		SwapConf:     1,
	}
)

func TestWallet(t *testing.T) {
	fmt.Println("////////// WITHOUT SPLIT FUNDING TRANSACTIONS //////////")
	Run(t, btc.NewWallet, alphaAddress, tBTC, false)
	fmt.Println("////////// WITH SPLIT FUNDING TRANSACTIONS //////////")
	Run(t, btc.NewWallet, alphaAddress, tBTC, true)
}
