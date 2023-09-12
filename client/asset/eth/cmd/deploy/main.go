// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

/*
	deploy is a utility for deploying swap contracts. Examples of use:

	1) Estimate the funding required to deploy a base asset swap contract to
	   Ethereum
		./deploy --mainnet --chain eth --fundingrequired
	   This will use the current prevailing fee rate to estimate the fee
	   requirements for the deployment transaction. The estimate is only
	   accurate if there are already enough funds in the wallet (so estimateGas
	   can be used), otherwise, a generously-padded constant is used to estimate
	   the gas requirements.

	2) Deploy a base asset swap contract to Ethereum.
		./deploy --mainnet --chain eth

	3) Deploy a token swap contract to Polygon.
		./deploy --mainnet --chain polygon --tokenaddr 0x2791bca1f2de4661ed88a30c99a7a9449aa84174

	4) Return remaining Goerli testnet ETH balance to specified address.
		./deploy --testnet --chain eth --returnaddr 0x18d65fb8d60c1199bb1ad381be47aa692b482605

	IMPORTANT: deploy uses the same wallet configuration as getgas. See getgas
	README for instructions.

	5) Test reading of the Polygon credentials file.
		./deploy --chain polygon --mainnet --readcreds
*/

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/client/asset"
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
	defaultCredentialsPath := filepath.Join(u.HomeDir, "dextest", "credentials.json")

	var contractVerI int
	var chain, credentialsPath, tokenAddress, returnAddr string
	var useTestnet, useMainnet, useSimnet, trace, debug, readCreds, fundingReq, multiBal bool
	flag.BoolVar(&readCreds, "readcreds", false, "does not run gas estimates. read the credentials file and print the address")
	flag.BoolVar(&fundingReq, "fundingrequired", false, "does not run gas estimates. calculate the funding required by the wallet to get estimates")
	flag.StringVar(&returnAddr, "return", "", "does not run gas estimates. return ethereum funds to supplied address")
	flag.BoolVar(&useMainnet, "mainnet", false, "use mainnet")
	flag.BoolVar(&useTestnet, "testnet", false, "use testnet")
	flag.BoolVar(&useSimnet, "simnet", false, "use simnet")
	flag.BoolVar(&trace, "trace", false, "use simnet")
	flag.BoolVar(&debug, "debug", false, "use simnet")
	flag.StringVar(&chain, "chain", "eth", "symbol of the base chain")
	flag.StringVar(&tokenAddress, "tokenaddr", "", "launches an erc20-linked contract with this token. default launches a base chain contract")
	flag.IntVar(&contractVerI, "ver", 0, "contract version")
	flag.StringVar(&credentialsPath, "creds", defaultCredentialsPath, "path for JSON credentials file.")
	flag.BoolVar(&multiBal, "multibalance", false, "deploy / estimate the MultiBalanceV0 contract instead of the swap contract")
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

	assetID, found := dex.BipSymbolID(chain)
	if !found {
		return fmt.Errorf("asset %s not known", chain)
	}

	if tkn := asset.TokenInfo(assetID); tkn != nil {
		return fmt.Errorf("specified chain is not a base chain. appears to be token %s", tkn.Name)
	}

	contractVer := uint32(contractVerI)

	logLvl := dex.LevelInfo
	if debug {
		logLvl = dex.LevelDebug
	}
	if trace {
		logLvl = dex.LevelTrace
	}

	var tokenAddr common.Address
	if tokenAddress != "" {
		if !common.IsHexAddress(tokenAddress) {
			return fmt.Errorf("token address %q does not appear to be valid", tokenAddress)
		}
		tokenAddr = common.HexToAddress(tokenAddress)
	}

	log := dex.StdOutLogger("DEPLOY", logLvl)

	var bui *dex.UnitInfo
	var chainCfg *params.ChainConfig
	switch chain {
	case "eth":
		bui = &dexeth.UnitInfo
		chainCfg, err = eth.ChainConfig(net)
		if err != nil {
			return fmt.Errorf("error finding chain config: %v", err)
		}
	case "polygon":
		bui = &dexpolygon.UnitInfo
		chainCfg, err = polygon.ChainConfig(net)
		if err != nil {
			return fmt.Errorf("error finding chain config: %v", err)
		}
	}

	switch {
	case fundingReq:
		if multiBal {
			return eth.ContractDeployer.EstimateMultiBalanceDeployFunding(
				ctx,
				chain,
				credentialsPath,
				chainCfg,
				bui,
				log,
				net,
			)
		}
		return eth.ContractDeployer.EstimateDeployFunding(
			ctx,
			chain,
			contractVer,
			tokenAddr,
			credentialsPath,
			chainCfg,
			bui,
			log,
			net,
		)
	case returnAddr != "":
		if !common.IsHexAddress(returnAddr) {
			return fmt.Errorf("return address %q is not valid", returnAddr)
		}
		addr := common.HexToAddress(returnAddr)
		return eth.ContractDeployer.ReturnETH(ctx, chain, addr, credentialsPath, chainCfg, bui, log, net)
	case multiBal:
		return eth.ContractDeployer.DeployMultiBalance(
			ctx,
			chain,
			credentialsPath,
			chainCfg,
			bui,
			log,
			net,
		)
	default:
		return eth.ContractDeployer.DeployContract(
			ctx,
			chain,
			contractVer,
			tokenAddr,
			credentialsPath,
			chainCfg,
			bui,
			log,
			net,
		)
	}
}
