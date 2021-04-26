// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"decred.org/dcrdex/server/asset"
	_ "decred.org/dcrdex/server/asset/bch"
	_ "decred.org/dcrdex/server/asset/btc"
	_ "decred.org/dcrdex/server/asset/dcr"
	_ "decred.org/dcrdex/server/asset/ltc"
)

var symbol string

func main() {
	flag.StringVar(&symbol, "asset", "dcr", "Symbol of asset for the coin ID to decode.")
	flag.Parse()

	if n := flag.NArg(); n != 1 {
		fmt.Fprintf(os.Stderr, "expected 1 argument, got %v\n", n)
		os.Exit(1)
	}

	coinID, err := hex.DecodeString(flag.Arg(0))
	if err != nil {
		if err = tryFile(flag.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	coinIDStr, err := asset.DecodeCoinID(symbol, coinID)
	if err != nil {
		fmt.Printf("Trying to open file %q", flag.Arg(0))
		if err = tryFile(flag.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	fmt.Fprintf(os.Stdout, "%v\n", coinIDStr)
	os.Exit(0)
}

func tryFile(file string) error {
	fmt.Printf("Trying to open file %q.\n", file)

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		coinID, err := hex.DecodeString(line)
		if err != nil {
			return err
		}
		coinIDStr, err := asset.DecodeCoinID(symbol, coinID)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%v\n", coinIDStr)
	}

	return scanner.Err()
}
