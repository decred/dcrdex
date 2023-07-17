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
	"decred.org/dcrdex/client/asset/polygon"
	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
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

	var maxSwaps, contractVerI int
	var chain, token, credentialsPath, returnAddr string
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
	flag.StringVar(&chain, "chain", "eth", "symbol of the base chain")
	flag.StringVar(&token, "token", "eth", "symbol of the token. if token is not specified, will check gas for ETH")
	flag.IntVar(&contractVerI, "ver", 0, "contract version")
	flag.StringVar(&credentialsPath, "creds", "", "path for JSON credentials file. default eth: ~/ethtest/getgas-credentials.json, default polygon: ~/ethtest/getgas-credentials_polygon.json")
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
		dexeth.MaybeReadSimnetAddrs()
		dexpolygon.MaybeReadSimnetAddrs()
	}
	if useTestnet {
		net = dex.Testnet
	}

	if credentialsPath == "" {
		switch chain {
		case "eth":
			credentialsPath = filepath.Join(u.HomeDir, "ethtest", "getgas-credentials.json")
		case "polygon":
			credentialsPath = filepath.Join(u.HomeDir, "ethtest", "getgas-credentials_polygon.json")
		}
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

	if chain == "polygon" && token == "eth" {
		token = "polygon" // chain takes precedence, adjust the default.
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

	var g *dexeth.Gases
	var tkn *dexeth.Token
	var ui, bui *dex.UnitInfo
	var compat eth.CompatibilityData
	var chainCfg *params.ChainConfig
	var contractAddr common.Address
	switch chain {
	case "eth":
		bui = &dexeth.UnitInfo
		g = dexeth.VersionedGases[contractVer]
		ui = &dexeth.UnitInfo
		if token != chain {
			tkn = dexeth.Tokens[assetID]
			ui = &tkn.UnitInfo
			g = &tkn.NetTokens[net].SwapContracts[contractVer].Gas
		}
		contractAddr = dexeth.ContractAddresses[contractVer][net]
		chainCfg, err = eth.ChainGenesis(net)
		if err != nil {
			return fmt.Errorf("error finding chain config: %v", err)
		}
		compat, err = eth.NetworkCompatibilityData(net)
		if err != nil {
			return fmt.Errorf("error finding api compatibility data: %v", err)
		}
	case "polygon":
		g = dexpolygon.VersionedGases[contractVer]
		bui = &dexpolygon.UnitInfo
		ui = bui
		if token != chain {
			tkn = dexpolygon.Tokens[assetID]
			ui = &tkn.UnitInfo
			g = &tkn.NetTokens[net].SwapContracts[contractVer].Gas
		}
		contractAddr = dexpolygon.ContractAddresses[contractVer][net]
		chainCfg, err = polygon.ChainGenesis(net)
		if err != nil {
			return fmt.Errorf("error finding chain config: %v", err)
		}
		compat, err = polygon.NetworkCompatibilityData(net)
		if err != nil {
			return fmt.Errorf("error finding api compatibility data: %v", err)
		}
	}

	wParams := &eth.GetGasWalletParams{
		ChainCfg:     chainCfg,
		Gas:          g,
		Token:        tkn,
		BaseUnitInfo: bui,
		UnitInfo:     ui,
		Compat:       &compat,
		ContractAddr: contractAddr,
	}

	switch {
	case fundingReq:
		return eth.GetGas.EstimateFunding(ctx, net, assetID, contractVer, maxSwaps, credentialsPath, wParams, log)
	case returnAddr != "":
		return eth.GetGas.Return(ctx, assetID, credentialsPath, returnAddr, wParams, net, log)
	default:
		return eth.GetGas.Estimate(ctx, net, assetID, contractVer, maxSwaps, credentialsPath, wParams, log)
	}
}
