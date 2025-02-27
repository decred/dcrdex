// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mnemonic"
)

func main() {
	if err := run(); err != nil {
		flag.Usage()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	var appSeedHex, appMnemonic string
	flag.StringVar(&appSeedHex, "seedhex", "", "Bison Wallet application seed (128 hexadecimal characters)")
	flag.StringVar(&appMnemonic, "mnemonic", "", "Bison Wallet application seed (15 word mnemonic)")
	var assetID uint
	flag.UintVar(&assetID, "asset", 0, "Asset ID. BIP-0044 coin type (integer).\n"+
		"See https://github.com/satoshilabs/slips/blob/master/slip-0044.md")
	flag.Parse()

	if appSeedHex == "" && appMnemonic == "" {
		return errors.New("no app seed set")
	}

	if tkn := asset.TokenInfo(uint32(assetID)); tkn != nil {
		return fmt.Errorf("this is a token. did you want asset ID %d for %s?\n", tkn.ParentID, asset.Asset(tkn.ParentID).Info.Name)
	}

	var (
		appSeedB []byte
		bday     time.Time
		err      error
	)

	if appSeedHex != "" {
		appSeedB, err = hex.DecodeString(appSeedHex)
		if err != nil {
			return fmt.Errorf("bad app seed: %v\n", err)
		}

		if len(appSeedB) != 64 {
			return fmt.Errorf("app seed is %d bytes, expected 64\n", len(appSeedB))
		}
	} else {
		appSeedB, bday, err = mnemonic.DecodeMnemonic(appMnemonic)
		if err != nil {
			return fmt.Errorf("bad mnemonic: %v\n", err)
		}
	}

	seed, _ := core.AssetSeedAndPass(uint32(assetID), appSeedB)
	fmt.Printf("%x\n", seed)
	if !bday.IsZero() {
		fmt.Printf("birthday: %v\n", bday)
	}

	return nil
}
