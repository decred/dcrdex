// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/client/mm/binance"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	var global bool
	flag.BoolVar(&global, "global", false, "use Binance Global (binance.com)")
	flag.Parse()

	apiKey, apiSecret := os.Getenv("KEY"), os.Getenv("SECRET")
	if len(apiKey) == 0 || len(apiSecret) == 0 {
		return fmt.Errorf("must specify both KEY and SECRET as environment variables")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bn := binance.New(&libxc.CEXConfig{
		Net:       dex.Mainnet,
		APIKey:    apiKey,
		SecretKey: apiSecret,
		Logger:    dex.StdOutLogger("BN", dex.LevelWarn, false),
		Notify: func(i interface{}) {
			// Do nothing
		},
	}, !global)

	mkts, err := bn.MatchedMarkets(ctx)
	if err != nil {
		return fmt.Errorf("error getting market info: %w", err)
	}

	marketPrinted := make(map[string]bool)

	for _, mkt := range mkts {
		if marketPrinted[mkt.Slug] {
			continue
		}
		marketPrinted[mkt.Slug] = true
		ui, err := asset.UnitInfo(mkt.BaseID)
		if err != nil {
			return fmt.Errorf("no unit info for asset %d", mkt.BaseID)
		}
		fmt.Printf("%s: %d %s (%s %s)\n", mkt.Slug, mkt.LotSize, ui.AtomicUnit, ui.ConventionalString(mkt.LotSize), ui.Conventional.Unit)
	}
	return nil
}
