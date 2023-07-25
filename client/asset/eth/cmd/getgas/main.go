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
	"strings"

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
	flag.StringVar(&token, "token", "", "symbol of the token. if token is not specified, will check gas for base chain")
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
		credentialsPath = filepath.Join(u.HomeDir, "dextest", "credentials.json")
	}

	if readCreds {
		addr, providers, err := eth.GetGas.ReadCredentials(chain, credentialsPath, net)
		if err != nil {
			return err
		}
		fmt.Println("Credentials successfully parsed")
		fmt.Println("Address:", addr)
		fmt.Println("Providers:", strings.Join(providers, ", "))
		return nil
	}

	if maxSwaps < 2 {
		return fmt.Errorf("n cannot be < 2")
	}

	if token == "" {
		token = chain
	}

	// Allow specification of tokens without .[chain] suffix.
	if chain != token && !strings.Contains(token, ".") {
		token = token + "." + chain
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

	walletParams := func(
		gases map[uint32]*dexeth.Gases,
		contracts map[uint32]map[dex.Network]common.Address,
		tokens map[uint32]*dexeth.Token,
		compatLookup func(net dex.Network) (c eth.CompatibilityData, err error),
		chainCfg func(net dex.Network) (c *params.ChainConfig, err error),
		bui *dex.UnitInfo,
	) (*eth.GetGasWalletParams, error) {

		wParams := new(eth.GetGasWalletParams)
		wParams.BaseUnitInfo = bui
		if token != chain {
			var exists bool
			tkn, exists := tokens[assetID]
			if !exists {
				return nil, fmt.Errorf("specified token %s does not exist on base chain %s", token, chain)
			}
			wParams.Token = tkn
			wParams.UnitInfo = &tkn.UnitInfo
			netToken, exists := tkn.NetTokens[net]
			if !exists {
				return nil, fmt.Errorf("no %s token on %s network %s", tkn.Name, chain, net)
			}
			swapContract, exists := netToken.SwapContracts[contractVer]
			if !exists {
				return nil, fmt.Errorf("no verion %d contract for %s token on %s network %s", contractVer, tkn.Name, chain, net)
			}
			wParams.Gas = &swapContract.Gas
		} else {
			wParams.UnitInfo = bui
			g, exists := gases[contractVer]
			if !exists {
				return nil, fmt.Errorf("no verion %d contract for %s network %s", contractVer, chain, net)
			}
			wParams.Gas = g
			cs, exists := contracts[contractVer]
			if !exists {
				return nil, fmt.Errorf("no version %d base chain swap contract on %s", contractVer, chain)
			}
			wParams.ContractAddr, exists = cs[net]
			if !exists {
				return nil, fmt.Errorf("no version %d base chain swap contract on %s network %s", contractVer, chain, net)
			}
		}
		wParams.ChainCfg, err = chainCfg(net)
		if err != nil {
			return nil, fmt.Errorf("error finding chain config: %v", err)
		}
		compat, err := compatLookup(net)
		if err != nil {
			return nil, fmt.Errorf("error finding api compatibility data: %v", err)
		}
		wParams.Compat = &compat
		return wParams, nil
	}

	var wParams *eth.GetGasWalletParams
	switch chain {
	case "eth":
		wParams, err = walletParams(dexeth.VersionedGases, dexeth.ContractAddresses, dexeth.Tokens,
			eth.NetworkCompatibilityData, eth.ChainGenesis, &dexeth.UnitInfo)
	case "polygon":
		wParams, err = walletParams(dexpolygon.VersionedGases, dexpolygon.ContractAddresses, dexpolygon.Tokens,
			polygon.NetworkCompatibilityData, polygon.ChainGenesis, &dexpolygon.UnitInfo)
	default:
		return fmt.Errorf("chain %s not known", chain)
	}
	if err != nil {
		return fmt.Errorf("error generating wallet params: %w", err)
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
