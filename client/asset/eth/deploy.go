// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"
	"os"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	erc20v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	ethv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
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
	contractVer uint32,
	tokenAddress common.Address,
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

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, walletDir, credentialsPath, chainCfg, log, net)
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

	txData, err := ContractDeployer.txData(contractVer, tokenAddress)
	if err != nil {
		return err
	}

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

	const deploymentGas = 1_500_000 // eth v0: 825_466, token v0 687_67
	if gas == 0 {
		gas = deploymentGas
	}
	fees := feeRate * gas * 5 / 4 // 20% buffer on fee rate to allow for variation

	log.Infof("Estimated fees: %s", ui.ConventionalString(fees))

	gweiBal := dexeth.WeiToGwei(baseChainBal)
	if fees < gweiBal {
		log.Infof("👍 Current balance (%s %s) sufficient for fees (%s)",
			ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(fees))
		return nil
	}

	shortage := fees - gweiBal
	log.Infof("❌ Current balance (%s %s) insufficient for fees (%s). Send %s more to %s",
		ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(fees),
		ui.ConventionalString(shortage), cl.address())

	return nil
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
	contractVer uint32,
	tokenAddress common.Address,
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

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, walletDir, credentialsPath, chainCfg, log, net)
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

	txData, err := ContractDeployer.txData(contractVer, tokenAddress)
	if err != nil {
		return err
	}

	// We may be able to get a proper estimate.
	gas, err := cl.EstimateGas(ctx, ethereum.CallMsg{
		From: cl.address(),
		To:   nil, // special value means deploy contract
		Data: txData,
	})
	gas = gas * 5 / 4
	if err != nil {
		return fmt.Errorf("EstimateGas error: %v", err)
	}

	fees := feeRate * gas // no fee-rate buffer

	log.Infof("Estimated fees: %s", ui.ConventionalString(fees))

	gweiBal := dexeth.WeiToGwei(baseChainBal)
	if fees >= gweiBal {
		feesWithBuffer := fees * 5 / 4
		shortage := feesWithBuffer - gweiBal
		return fmt.Errorf("❌ Current balance (%s %s) insufficient for fees (%s). Send %s more to %s",
			ui.ConventionalString(gweiBal), ui.Conventional.Unit, ui.ConventionalString(feesWithBuffer),
			ui.ConventionalString(shortage), cl.address())
	}

	txOpts, err := cl.txOpts(ctx, 0, gas, dexeth.GweiToWei(feeRate), nil)
	if err != nil {
		return err
	}

	isToken := tokenAddress != (common.Address{})
	var contractAddr common.Address
	var tx *types.Transaction
	if isToken {
		switch contractVer {
		case 0:
			contractAddr, tx, _, err = erc20v0.DeployERC20Swap(txOpts, cl.contractBackend(), tokenAddress)
		}
	} else {
		switch contractVer {
		case 0:
			contractAddr, tx, _, err = ethv0.DeployETHSwap(txOpts, cl.contractBackend())
		}
	}
	if err != nil {
		return fmt.Errorf("error deploying contract: %w", err)
	}
	if tx == nil {
		return fmt.Errorf("no deployment function for version %d", contractVer)
	}

	log.Infof("👍 Contract %s launched with tx %s", contractAddr, tx.Hash())

	return nil
}

// ReturnETH returns the remaining base asset balance from the deployment/getgas
// wallet to the specified return address.
func (contractDeployer) ReturnETH(
	ctx context.Context,
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

	cl, feeRate, err := ContractDeployer.nodeAndRate(ctx, walletDir, credentialsPath, chainCfg, log, net)
	if err != nil {
		return err
	}
	defer cl.shutdown()

	bigBal, err := cl.addressBalance(ctx, cl.address())
	if err != nil {
		return fmt.Errorf("error getting eth balance: %v", err)
	}
	bal := dexeth.WeiToGwei(bigBal)

	log.Infof("Balance: %s %s", ui.ConventionalString(bal), ui.Conventional.Unit)

	var gas uint64 = defaultSendGasLimit
	fees := feeRate * gas

	log.Infof("Fees: %s %s", ui.ConventionalString(fees), ui.Conventional.Unit)

	if fees >= bal {
		return fmt.Errorf("tx fees of %s %s exceed balance of %s",
			ui.ConventionalString(fees), ui.Conventional.Unit, ui.ConventionalString(bal))
	}

	sendAmt := bal - fees

	log.Infof("Net Send: %s %s", ui.ConventionalString(sendAmt), ui.Conventional.Unit)

	txOpts, err := cl.txOpts(ctx, sendAmt, gas, dexeth.GweiToWei(feeRate), nil)
	if err != nil {
		return fmt.Errorf("error constructing tx opts: %w", err)
	}

	tx, err := cl.sendTransaction(ctx, txOpts, returnAddr, nil)
	if err != nil {
		return fmt.Errorf("error sending tx: %w", err)
	}

	log.Infof("Transaction ID: %s", tx.Hash())

	return nil
}

func (contractDeployer) nodeAndRate(
	ctx context.Context,
	walletDir,
	credentialsPath string,
	chainCfg *params.ChainConfig,
	log dex.Logger,
	net dex.Network,
) (*multiRPCClient, uint64, error) {

	creds, provider, err := getFileCredentials(credentialsPath, net)
	if err != nil {
		return nil, 0, err
	}

	pw := []byte("abc")
	chainID := chainCfg.ChainID.Int64()

	if err := CreateEVMWallet(chainID, &asset.CreateWalletParams{
		Type:     walletTypeRPC,
		Seed:     creds.Seed,
		Pass:     pw,
		Settings: map[string]string{providersKey: provider},
		DataDir:  walletDir,
		Net:      net,
		Logger:   log,
	}, nil /* we don't need the full api, skipConnect = true allows for nil compat */, true); err != nil {
		return nil, 0, fmt.Errorf("error creating wallet: %w", err)
	}

	cl, err := newMultiRPCClient(walletDir, []string{provider}, log, chainCfg, net)
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
