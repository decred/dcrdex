// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	erc20v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	multibal "decred.org/dcrdex/dex/networks/eth/contracts/multibalance"
	ethv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// contractDeployer deploys a dcrdex swap contract for an evm-compatible
// blockchain. contractDeployer can deploy both base asset contracts or ERC20
// contracts. contractDeployer is used by the cmd/deploy/deploy utility.
type contractDeployer byte

var ContractDeployer contractDeployer

// EstimateDeployFunding estimates the fees required to deploy a contract.
// The gas estimate is only accurate if sufficient funds are in the wallet (so
// that estimateGas succeeds), otherwise a generously-padded estimate is
// generated.
func (contractDeployer) EstimateDeployFunding(
	ctx context.Context,
	chain string,
	contractVer uint32,
	tokenAddress common.Address,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {
	txData, err := ContractDeployer.txData(contractVer, tokenAddress)
	if err != nil {
		return err
	}
	const deploymentGas = 1_000_000 // eth v0: 687_671, token v0 825_478
	return ContractDeployer.estimateDeployFunding(ctx, txData, deploymentGas, chain, credentialsPath, chainCfg, ui, log, net)
}

func (contractDeployer) estimateDeployFunding(
	ctx context.Context,
	txData []byte,
	deploymentGas uint64,
	chain string,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, chain, walletDir, credentialsPath, chainCfg, log, net)
	if err != nil {
		return err
	}
	defer cl.shutdown()

	log.Infof("Address: %s", cl.address())

	baseChainBal, err := cl.addressBalance(ctx, cl.address())
	if err != nil {
		return fmt.Errorf("error getting eth balance: %v", err)
	}

	log.Infof("Balance: %s %s", ui.ConventionalString(dexeth.WeiToGwei(baseChainBal)), ui.Conventional.Unit)

	var gas uint64
	if baseChainBal.Cmp(new(big.Int)) > 0 {
		// We may be able to get a proper estimate.
		gas, err = cl.EstimateGas(ctx, ethereum.CallMsg{
			From: cl.creds.addr,
			To:   nil, // special value means deploy contract
			Data: txData,
		})
		gas = gas * 5 / 4 // 20% buffer on gas
		if err != nil {
			log.Debugf("EstimateGas error: %v", err)
			log.Info("Unable to get on-chain estimate. balance probably too low. Falling back to rough estimate")
		}
	}

	if gas == 0 {
		gas = deploymentGas
	}
	fees := feeRate * gas

	log.Infof("Estimated fees: %s", ui.ConventionalString(fees))

	gweiBal := dexeth.WeiToGwei(baseChainBal)
	if fees < gweiBal {
		log.Infof("👍 Current balance (%s %s) sufficient for fees (%s)",
			ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(fees))
		return nil
	}

	shortage := fees - gweiBal
	log.Infof("❌ Current balance (%[1]s %[2]s) insufficient for fees (%[3]s). Send %[4]s %[2]s to %[5]s",
		ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(fees),
		ui.ConventionalString(shortage), cl.address())

	return nil
}

func (contractDeployer) EstimateMultiBalanceDeployFunding(
	ctx context.Context,
	chain string,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {
	const deploymentGas = 400_000 // 302_647 for https://goerli.etherscan.io/tx/0x540d3e82888b18f89566a988712a7c2ecd45bd2df472f8dd689e319ae9fa4445
	txData := common.FromHex(multibal.MultiBalanceV0MetaData.Bin)
	return ContractDeployer.estimateDeployFunding(ctx, txData, deploymentGas, chain, credentialsPath, chainCfg, ui, log, net)
}

func (contractDeployer) txData(contractVer uint32, tokenAddr common.Address) (txData []byte, err error) {
	var abi *abi.ABI
	var bytecode []byte
	isToken := tokenAddr != (common.Address{})
	if isToken {
		switch contractVer {
		case 0:
			bytecode = common.FromHex(erc20v0.ERC20SwapBin)
			abi, err = erc20v0.ERC20SwapMetaData.GetAbi()
		}
	} else {
		switch contractVer {
		case 0:
			bytecode = common.FromHex(ethv0.ETHSwapBin)
			abi, err = ethv0.ETHSwapMetaData.GetAbi()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing ABI: %w", err)
	}
	if abi == nil {
		return nil, fmt.Errorf("no abi data for version %d", contractVer)
	}
	txData = bytecode
	if isToken {
		argData, err := abi.Pack("", tokenAddr)
		if err != nil {
			return nil, fmt.Errorf("error packing token address: %w", err)
		}
		txData = append(txData, argData...)
	}
	return
}

// DeployContract deployes a dcrdex swap contract.
func (contractDeployer) DeployContract(
	ctx context.Context,
	chain string,
	contractVer uint32,
	tokenAddress common.Address,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {
	txData, err := ContractDeployer.txData(contractVer, tokenAddress)
	if err != nil {
		return err
	}

	var deployer deployerFunc
	isToken := tokenAddress != (common.Address{})
	if isToken {
		switch contractVer {
		case 0:
			deployer = func(txOpts *bind.TransactOpts, cb bind.ContractBackend) (common.Address, *types.Transaction, error) {
				contractAddr, tx, _, err := erc20v0.DeployERC20Swap(txOpts, cb, tokenAddress)
				return contractAddr, tx, err
			}

		}
	} else {
		switch contractVer {
		case 0:
			deployer = func(txOpts *bind.TransactOpts, cb bind.ContractBackend) (common.Address, *types.Transaction, error) {
				contractAddr, tx, _, err := ethv0.DeployETHSwap(txOpts, cb)
				return contractAddr, tx, err
			}
		}
	}
	if deployer == nil {
		return fmt.Errorf("contract version unknown")
	}

	return ContractDeployer.deployContract(ctx, txData, deployer, chain, credentialsPath, chainCfg, ui, log, net)
}

type deployerFunc func(txOpts *bind.TransactOpts, cb bind.ContractBackend) (common.Address, *types.Transaction, error)

// DeployContract deployes a dcrdex swap contract.
func (contractDeployer) deployContract(
	ctx context.Context,
	txData []byte,
	deployer deployerFunc,
	chain string,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, chain, walletDir, credentialsPath, chainCfg, log, net)
	if err != nil {
		return err
	}
	defer cl.shutdown()

	log.Infof("Address: %s", cl.address())

	baseChainBal, err := cl.addressBalance(ctx, cl.address())
	if err != nil {
		return fmt.Errorf("error getting eth balance: %v", err)
	}

	log.Infof("Balance: %s %s", ui.ConventionalString(dexeth.WeiToGwei(baseChainBal)), ui.Conventional.Unit)

	// We may be able to get a proper estimate.
	gas, err := cl.EstimateGas(ctx, ethereum.CallMsg{
		From: cl.address(),
		To:   nil, // special value means deploy contract
		Data: txData,
	})
	if err != nil {
		return fmt.Errorf("EstimateGas error: %v", err)
	}

	log.Infof("Estimated fees: %s", ui.ConventionalString(feeRate*gas))

	gas *= 5 / 4 // Add 20% buffer
	feesWithBuffer := feeRate * gas

	gweiBal := dexeth.WeiToGwei(baseChainBal)
	if feesWithBuffer >= gweiBal {
		shortage := feesWithBuffer - gweiBal
		return fmt.Errorf("❌ Current balance (%[1]s %[2]s) insufficient for fees (%[3]s). Send %[4]s %[2]s to %[5]s",
			ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(feesWithBuffer),
			ui.ConventionalString(shortage), cl.address())
	}

	txOpts, err := cl.txOpts(ctx, 0, gas, dexeth.GweiToWei(feeRate), nil)
	if err != nil {
		return err
	}

	contractAddr, tx, err := deployer(txOpts, cl.contractBackend())
	if err != nil {
		return err
	}

	log.Infof("👍 Contract %s launched with tx %s", contractAddr, tx.Hash())

	return nil
}

// ReturnETH returns the remaining base asset balance from the deployment/getgas
// wallet to the specified return address.
func (contractDeployer) ReturnETH(
	ctx context.Context,
	chain string,
	returnAddr common.Address,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {

	walletDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(walletDir)

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, chain, walletDir, credentialsPath, chainCfg, log, net)
	if err != nil {
		return err
	}
	defer cl.shutdown()

	return GetGas.returnFunds(ctx, cl, dexeth.GweiToWei(feeRate), returnAddr, nil, ui, log, net)
}

func (contractDeployer) nodeAndRate(
	ctx context.Context,
	chain string,
	walletDir,
	credentialsPath string,

	chainCfg *params.ChainConfig,
	log dex.Logger,
	net dex.Network,
) (*multiRPCClient, uint64, error) {

	seed, providers, err := getFileCredentials(chain, credentialsPath, net)
	if err != nil {
		return nil, 0, err
	}

	pw := []byte("abc")
	chainID := chainCfg.ChainID.Int64()

	if err := CreateEVMWallet(chainID, &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     seed,
		Pass:     pw,
		Settings: map[string]string{providersKey: strings.Join(providers, " ")},
		DataDir:  walletDir,
		Net:      net,
		Logger:   log,
	}, nil /* we don't need the full api, skipConnect = true allows for nil compat */, true); err != nil {
		return nil, 0, fmt.Errorf("error creating wallet: %w", err)
	}

	cl, err := newMultiRPCClient(walletDir, providers, log, chainCfg, net)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating rpc client: %w", err)
	}

	if err := cl.unlock(string(pw)); err != nil {
		return nil, 0, fmt.Errorf("error unlocking rpc client: %w", err)
	}

	if err = cl.connect(ctx); err != nil {
		return nil, 0, fmt.Errorf("error connecting: %w", err)
	}

	base, tip, err := cl.currentFees(ctx)
	if err != nil {
		cl.shutdown()
		return nil, 0, fmt.Errorf("Error estimating fee rate: %v", err)
	}

	feeRate := dexeth.WeiToGwei(new(big.Int).Add(tip, new(big.Int).Mul(base, big.NewInt(2))))
	return cl, feeRate, nil
}

// DeployMultiBalance deployes a contract with a function for reading all
// balances in one call.
func (contractDeployer) DeployMultiBalance(
	ctx context.Context,
	chain string,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	ui *dex.UnitInfo,
	log dex.Logger,
	net dex.Network,
) error {
	txData := common.FromHex(multibal.MultiBalanceV0MetaData.Bin)
	deployer := func(txOpts *bind.TransactOpts, cb bind.ContractBackend) (common.Address, *types.Transaction, error) {
		contractAddr, tx, _, err := multibal.DeployMultiBalanceV0(txOpts, cb)
		return contractAddr, tx, err
	}
	return ContractDeployer.deployContract(ctx, txData, deployer, chain, credentialsPath, chainCfg, ui, log, net)
}
