//go:build harness && lgpl

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
)

func parseWalletType(t string) (core.SimWalletType, error) {
	switch t {
	case "core":
		return core.WTCoreClone, nil
	case "spv":
		return core.WTSPVNative, nil
	case "electrum":
		return core.WTElectrum, nil
	default:
		return 0, errors.New("invalid wallet type")
	}
}

func main() {
	var baseSymbol, quoteSymbol, base1Node, quote1Node, base2Node, quote2Node, regAsset string
	var base1SPV, quote1SPV, base2SPV, quote2SPV, debug, trace, list bool
	var base1Type, quote1Type, base2Type, quote2Type string
	var tests flagArray
	flag.Var(&tests, "t", "the test(s) to run. multiple --t flags OK")
	flag.StringVar(&baseSymbol, "base", "dcr", "the bot program to run")
	flag.StringVar(&quoteSymbol, "quote", "btc", "the bot program to run")
	flag.StringVar(&base1Node, "base1node", "beta", "the harness node to connect to for the first client's base asset. only RPC wallets")
	flag.StringVar(&quote1Node, "quote1node", "beta", "the harness node to connect to for the first client's quote asset. only RPC wallets")
	flag.StringVar(&base2Node, "base2node", "gamma", "the harness node to connect to for the second client's base asset. only RPC wallets")
	flag.StringVar(&quote2Node, "quote2node", "gamma", "the harness node to connect to for the second client's quote asset. only RPC wallets")
	flag.StringVar(&regAsset, "regasset", "", "the asset to use for registration. default is base asset")
	flag.StringVar(&base1Type, "base1type", "core", "the wallet type for the first client's base asset (core, spv, electrum). ignored for eth")
	flag.StringVar(&quote1Type, "quote1type", "core", "the wallet type for the first client's quote asset (core, spv, electrum). ignored for eth")
	flag.StringVar(&base2Type, "base2type", "core", "the wallet type for the second client's base asset (core, spv, electrum). ignored for eth")
	flag.StringVar(&quote2Type, "quote2type", "core", "the wallet type for the second client's quote asset (core, spv, electrum). ignored for eth")
	flag.BoolVar(&base1SPV, "base1spv", false, "use native SPV wallet for the first client's base asset (removed: use base1type)")
	flag.BoolVar(&quote1SPV, "quote1spv", false, "use native SPV wallet for the first client's quote asset (removed: use quote1type)")
	flag.BoolVar(&base2SPV, "base2spv", false, "use native SPV wallet for the second client's base asset (removed: use base2type)")
	flag.BoolVar(&quote2SPV, "quote2spv", false, "use native SPV wallet for the second client's quote asset (removed: use quote2type)")
	flag.BoolVar(&list, "list", false, "list available tests")
	flag.BoolVar(&debug, "debug", false, "log at logging level debug")
	flag.BoolVar(&trace, "trace", false, "log at logging level trace")
	flag.Parse()

	if list {
		for _, s := range core.SimTests() {
			fmt.Println(s)
		}
		return
	}

	if base1SPV {
		fmt.Fprintln(os.Stderr, "base1spv is removed - use base1type=spv instead")
		os.Exit(1)
	}
	if quote1SPV {
		fmt.Fprintln(os.Stderr, "quote1spv is removed - use quote1type=spv instead")
		os.Exit(1)
	}
	if base2SPV {
		fmt.Fprintln(os.Stderr, "base2spv is removed - use base2type=spv instead")
		os.Exit(1)
	}
	if quote2SPV {
		fmt.Fprintln(os.Stderr, "quote2spv is removed - use quote2type=spv instead")
		os.Exit(1)
	}

	logLevel := dex.LevelInfo
	switch {
	case trace:
		logLevel = dex.LevelTrace
	case debug:
		logLevel = dex.LevelDebug
	}

	if len(tests) == 0 {
		tests = []string{"success"}
	}

	if regAsset == "" {
		regAsset = baseSymbol
	}

	b1wt, err := parseWalletType(base1Type)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid base1 wallet type %q", base1Type)
		os.Exit(1)
	}
	q1wt, err := parseWalletType(quote1Type)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid quote1 wallet type %q", quote1Type)
		os.Exit(1)
	}
	b2wt, err := parseWalletType(base2Type)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid base2 wallet type %q", base2Type)
		os.Exit(1)
	}
	q2wt, err := parseWalletType(quote2Type)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid quote2 wallet type %q", quote2Type)
		os.Exit(1)
	}

	err = core.RunSimulationTest(&core.SimulationConfig{
		BaseSymbol:        baseSymbol,
		QuoteSymbol:       quoteSymbol,
		RegistrationAsset: regAsset,
		Client1: &core.SimClient{
			BaseWalletType:  b1wt,
			QuoteWalletType: q1wt,
			BaseNode:        base1Node,
			QuoteNode:       quote1Node,
		},
		Client2: &core.SimClient{
			BaseWalletType:  b2wt,
			QuoteWalletType: q2wt,
			BaseNode:        base2Node,
			QuoteNode:       quote2Node,
		},
		Tests:  tests,
		Logger: dex.StdOutLogger("T", logLevel),
	})
	if err != nil {
		fmt.Println("ERROR:", err)
	} else {
		fmt.Println("SUCCESS!")
	}
}

type flagArray []string

func (f *flagArray) String() string {
	return "my string representation"
}

func (f *flagArray) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func init() {
	dexeth.MaybeReadSimnetAddrs()
}
