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
		Version:      0, // match btc.version
		SwapSize:     dexbtc.InitTxSizeSegwit,
		SwapSizeBase: dexbtc.InitTxSizeBaseSegwit,
		MaxFeeRate:   10,
		SwapConf:     1,
	}
)

func TestWallet(t *testing.T) {
	const lotSize = 1e6
	fmt.Println("////////// WITHOUT SPLIT FUNDING TRANSACTIONS //////////")
	Run(t, btc.NewWallet, alphaAddress, lotSize, tBTC, false)
	fmt.Println("////////// WITH SPLIT FUNDING TRANSACTIONS //////////")
	Run(t, btc.NewWallet, alphaAddress, lotSize, tBTC, true)
}
