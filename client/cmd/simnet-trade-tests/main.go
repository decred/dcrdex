//go:build harness && lgpl

package main

import (
	"flag"
	"fmt"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
)

func main() {
	var baseSymbol, quoteSymbol, base1Node, quote1Node, base2Node, quote2Node, regAsset string
	var base1SPV, quote1SPV, base2SPV, quote2SPV, debug, trace, list bool
	var tests flagArray
	flag.Var(&tests, "t", "the test(s) to run. multiple --t flags OK")
	flag.StringVar(&baseSymbol, "base", "dcr", "the bot program to run")
	flag.StringVar(&quoteSymbol, "quote", "btc", "the bot program to run")
	flag.StringVar(&base1Node, "base1node", "beta", "the harness node to connect to for the first client's base asset. only RPC wallets")
	flag.StringVar(&quote1Node, "quote1node", "beta", "the harness node to connect to for the first client's quote asset. only RPC wallets")
	flag.StringVar(&base2Node, "base2node", "gamma", "the harness node to connect to for the second client's base asset. only RPC wallets")
	flag.StringVar(&quote2Node, "quote2node", "gamma", "the harness node to connect to for the second client's quote asset. only RPC wallets")
	flag.StringVar(&regAsset, "regasset", "", "the asset to use for registration. default is base asset")
	flag.BoolVar(&base1SPV, "base1spv", false, "use SPV wallet for the first client's base asset")
	flag.BoolVar(&quote1SPV, "quote1spv", false, "use SPV wallet for the first client's quote asset")
	flag.BoolVar(&base2SPV, "base2spv", false, "use SPV wallet for the second client's base asset")
	flag.BoolVar(&quote2SPV, "quote2spv", false, "use SPV wallet for the second client's quote asset")
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

	err := core.RunSimulationTest(&core.SimulationConfig{
		BaseSymbol:        baseSymbol,
		QuoteSymbol:       quoteSymbol,
		RegistrationAsset: regAsset,
		Client1: &core.SimClient{
			BaseSPV:   base1SPV,
			QuoteSPV:  quote1SPV,
			BaseNode:  base1Node,
			QuoteNode: quote1Node,
		},
		Client2: &core.SimClient{
			BaseSPV:   base2SPV,
			QuoteSPV:  quote2SPV,
			BaseNode:  base2Node,
			QuoteNode: quote2Node,
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
