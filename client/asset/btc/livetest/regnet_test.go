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
	alphaAddress = "bcrt1qy7agjj62epx0ydnqskgwlcfwu52xjtpj36hr0d"
)

var (
	tBTC = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		SwapSize:     dexbtc.InitTxSizeSegwit,
		SwapSizeBase: dexbtc.InitTxSizeBaseSegwit,
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
