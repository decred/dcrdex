package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

// geogame is a utility for generating an address from a prepaid bond for use
// in geocache-like DCR giveaway game.

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	var testnet bool
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
	flag.Parse()

	chainParams := chaincfg.MainNetParams()
	if testnet {
		chainParams = chaincfg.TestNet3Params()
	}

	args := flag.Args()

	if len(args) != 1 {
		return fmt.Errorf("specify a prepaid bond")
	}
	codeHex := args[0]
	if len(codeHex) != 32 {
		return fmt.Errorf("prepaid bond %q has wrong length %d", codeHex, len(codeHex))
	}
	code, err := hex.DecodeString(codeHex)
	if err != nil {
		return fmt.Errorf("error decoding prepaid bond: %w", err)
	}

	k, err := hdkeychain.NewMaster(code, chainParams)
	if err != nil {
		return fmt.Errorf("error generating key from bond: %w", err)
	}

	pkh := dcrutil.Hash160(k.SerializedPubKey())
	addr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkh, chainParams)
	if err != nil {
		return fmt.Errorf("error generating address: %w", err)
	}

	fmt.Println(addr)

	return nil
}
