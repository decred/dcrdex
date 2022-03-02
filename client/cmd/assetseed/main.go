// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"decred.org/dcrdex/client/core"
)

func main() {
	var appSeed string
	flag.StringVar(&appSeed, "seed", "", "DEX client application seed (128 hexadecimal characters)")
	var assetID uint
	flag.UintVar(&assetID, "asset", 0, "Asset ID. BIP-0044 coin type (integer).\n"+
		"See https://github.com/satoshilabs/slips/blob/master/slip-0044.md")
	flag.Parse()

	if appSeed == "" {
		flag.Usage()
		os.Exit(1)
	}

	appSeedB, err := hex.DecodeString(appSeed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad app seed: %v\n", err)
		os.Exit(1)
	}

	if len(appSeedB) != 64 {
		fmt.Fprintf(os.Stderr, "app seed is %d bytes, expected 64\n", len(appSeedB))
		os.Exit(1)
	}

	seed, walletPW := core.AssetSeedAndPass(uint32(assetID), appSeedB)
	fmt.Printf("seed: %x\n", seed)
	fmt.Printf("wallet pass bytes: %x\n", walletPW)

	os.Exit(0)
}
