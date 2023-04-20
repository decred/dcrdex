// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"decred.org/dcrdex/client/asset/eth"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("could not get the current user: %w", err)
	}
	defaultCredsPath := filepath.Join(u.HomeDir, "ethtest", "getgas-credentials.json")

	var maxSwaps, contractVerI int
	var token, credentialsPath, returnAddr string
	var useTestnet, useMainnet, useSimnet, trace, debug, readCreds, fundingReq bool
	flag.BoolVar(&readCreds, "readcreds", false, "does not run gas estimates. read the credentials file and print the address")
	flag.BoolVar(&fundingReq, "fundingrequired", false, "does not run gas estimates. calculate the funding required by the wallet to get estimates")
	flag.StringVar(&returnAddr, "return", "", "does not run gas estimates. return ethereum funds to supplied address")
	flag.BoolVar(&useMainnet, "mainnet", false, "use mainnet")
	flag.BoolVar(&useTestnet, "testnet", false, "use testnet")
	flag.BoolVar(&useSimnet, "simnet", false, "use simnet")
	flag.BoolVar(&trace, "trace", false, "use simnet")
	flag.BoolVar(&debug, "debug", false, "use simnet")
	flag.IntVar(&maxSwaps, "n", 5, "max number of swaps per transaction. minimum is 2. test will run from 2 swap up to n swaps.")
	flag.StringVar(&token, "token", "eth", "symbol of the token. if token is not specified, will check gas for Ethereum")
	flag.IntVar(&contractVerI, "ver", 0, "contract version")
	flag.StringVar(&credentialsPath, "creds", defaultCredsPath, "path for JSON credentials file")
	flag.Parse()

	if !useMainnet && !useTestnet && !useSimnet {
		return fmt.Errorf("no network specified. add flag --mainnet, --testnet, or --simnet")
	}
	if (useMainnet && useTestnet) || (useMainnet && useSimnet) || (useTestnet && useSimnet) {
		return fmt.Errorf("more than one network specified")
	}

	net := dex.Mainnet
	if useSimnet {
		net = dex.Simnet
	}
	if useTestnet {
		net = dex.Testnet
	}

	if readCreds {
		addr, provider, err := eth.GetGas.ReadCredentials(credentialsPath, net)
		if err != nil {
			return err
		}
		fmt.Println("Credentials successfully parsed")
		fmt.Println("Address:", addr)
		fmt.Println("Provider:", provider)
		return nil
	}

	if maxSwaps < 2 {
		return fmt.Errorf("n cannot be < 2")
	}
	assetID, found := dex.BipSymbolID(token)
	if !found {
		return fmt.Errorf("asset %s not known", token)
	}
	contractVer := uint32(contractVerI)

	logLvl := dex.LevelInfo
	if debug {
		logLvl = dex.LevelDebug
	}
	if trace {
		logLvl = dex.LevelTrace
	}

	log := dex.StdOutLogger("GG", logLvl)

	switch {
	case fundingReq:
		return eth.GetGas.EstimateFunding(ctx, net, assetID, contractVer, maxSwaps, credentialsPath, log)
	case returnAddr != "":
		return eth.GetGas.ReturnETH(ctx, credentialsPath, returnAddr, net, log)
	default:
		return eth.GetGas.Estimate(ctx, net, assetID, contractVer, maxSwaps, credentialsPath, log)
	}
}
