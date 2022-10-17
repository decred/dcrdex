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
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("SUCCESS!")
	os.Exit(0)
}

func run() error {
	var baseSymbol, quoteSymbol, base1Node, quote1Node, base2Node, quote2Node, regAsset string
	var base1SPV, quote1SPV, base2SPV, quote2SPV, debug, trace, list, all, runOnce bool
	var base1Type, quote1Type, base2Type, quote2Type string
	var tests, except flagArray
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
	flag.BoolVar(&all, "all", false, "run all tests for the asset")
	flag.Var(&except, "except", "can only be used with the \"all\" flag and removes unwanted tests. multiple --except flags OK")
	flag.BoolVar(&runOnce, "runonce", false, "some tests will run twice to swap maker/taker. Set to only run once. default is false")
	flag.Parse()

	if list {
		for _, s := range core.SimTests() {
			fmt.Println(s)
		}
		return nil
	}

	if base1SPV {
		return errors.New("base1spv is removed - use base1type=spv instead")
	}
	if quote1SPV {
		return errors.New("quote1spv is removed - use quote1type=spv instead")
	}
	if base2SPV {
		return errors.New("base2spv is removed - use base2type=spv instead")
	}
	if quote2SPV {
		return errors.New("quote2spv is removed - use quote2type=spv instead")
	}

	logLevel := dex.LevelInfo
	switch {
	case trace:
		logLevel = dex.LevelTrace
	case debug:
		logLevel = dex.LevelDebug
	}

	// Assume any trailing arguments are test names.
	tests = append(tests, flag.Args()...)

	// Check that all test names are valid.
	allTests := core.SimTests()
	var containsAll bool
	if len(tests) == 0 {
		tests = []string{"success"}
	} else {
		for _, t := range tests {
			if t == "all" {
				containsAll = true
				break
			}
			var found bool
			for _, at := range allTests {
				if at == t {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("test %s is unknown. Possible tests are %s", t, allTests)
			}
		}
	}

	if regAsset == "" {
		regAsset = baseSymbol
	}

	b1wt, err := parseWalletType(base1Type)
	if err != nil {
		return fmt.Errorf("invalid base1 wallet type %q", base1Type)
	}
	q1wt, err := parseWalletType(quote1Type)
	if err != nil {
		return fmt.Errorf("invalid quote1 wallet type %q", quote1Type)
	}
	b2wt, err := parseWalletType(base2Type)
	if err != nil {
		return fmt.Errorf("invalid base2 wallet type %q", base2Type)
	}
	q2wt, err := parseWalletType(quote2Type)
	if err != nil {
		return fmt.Errorf("invalid quote2 wallet type %q", quote2Type)
	}

	if all || containsAll {
		tests = allTests
		if len(except) > 0 {
			for i := len(tests) - 1; i >= 0; i-- {
				for _, ex := range except {
					if tests[i] == ex {
						tests = append(tests[:i], tests[i+1:]...)
						break
					}
				}
			}
		}
	}

	return core.RunSimulationTest(&core.SimulationConfig{
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
		Tests:   tests,
		Logger:  dex.StdOutLogger("T", logLevel),
		RunOnce: runOnce,
	})
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
