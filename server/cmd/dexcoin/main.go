// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	_ "decred.org/dcrdex/server/asset/bch"
	_ "decred.org/dcrdex/server/asset/btc"
	_ "decred.org/dcrdex/server/asset/dcr"
	_ "decred.org/dcrdex/server/asset/ltc"
)

type coinDecoder func([]byte) (string, error)

func main() {
	var symbol string
	flag.StringVar(&symbol, "asset", "dcr", "Symbol of asset for the coin ID to decode.")
	flag.Parse()

	if n := flag.NArg(); n != 1 {
		fmt.Fprintf(os.Stderr, "expected 1 argument, got %v\n", n)
		os.Exit(1)
	}

	assetID, ok := dex.BipSymbolID(symbol)
	if !ok {
		fmt.Fprintf(os.Stderr, "asset %s not known \n", symbol)
		os.Exit(1)
	}

	coinID, err := hex.DecodeString(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	coinIDStr, err := asset.DecodeCoinID(assetID, coinID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%v\n", coinIDStr)
	os.Exit(0)
}
