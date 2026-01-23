// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbase "decred.org/dcrdex/dex/networks/base"
	dexerc20 "decred.org/dcrdex/dex/networks/erc20"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
)

const simnetBridgeName = "simnet"

var (
	usdtBaseID, _ = dex.BipSymbolID("usdt.base")
)

var simnetHarnessDir = map[uint32]string{
	ethID:     "eth",
	polygonID: "polygon",
	baseID:    "base",
}

var simnetTokenScript = map[uint32]string{
	usdcEthID:     "sendUSDC",
	usdtEthID:     "sendUSDT",
	usdcPolygonID: "sendUSDC",
	usdtPolygonID: "sendUSDT",
	usdcBaseID:    "sendUSDC",
	usdtBaseID:    "sendUSDT",
}

type simnetBridge struct {
	net     dex.Network
	assetID uint32
	cb      bind.ContractBackend
	addr    common.Address
	chainID *big.Int
	log     dex.Logger

	// Mock approval state tracking
	approvedAssets map[uint32]bool
	approvalsMtx   sync.RWMutex
}

func newSimnetBridge(assetID uint32, net dex.Network, cb bind.ContractBackend, addr common.Address, chainID *big.Int, log dex.Logger) (*simnetBridge, error) {
	if net != dex.Simnet {
		return nil, fmt.Errorf("simnet bridge only available on simnet, got %s", net)
	}

	return &simnetBridge{
		net:            net,
		assetID:        assetID,
		cb:             cb,
		addr:           addr,
		chainID:        chainID,
		log:            log,
		approvedAssets: make(map[uint32]bool),
	}, nil
}

var simnetBridgePaths = map[uint32][]uint32{
	ethID:         {polygonID, baseID},
	polygonID:     {ethID, baseID},
	baseID:        {ethID, polygonID},
	usdcEthID:     {usdcPolygonID, usdcBaseID},
	usdcPolygonID: {usdcEthID, usdcBaseID},
	usdcBaseID:    {usdcEthID, usdcPolygonID},
	usdtEthID:     {usdtPolygonID, usdtBaseID},
	usdtPolygonID: {usdtEthID, usdtBaseID},
	usdtBaseID:    {usdtEthID, usdtPolygonID},
}

func (b *simnetBridge) bridgeContractAddr(ctx context.Context, assetID uint32) (common.Address, error) {
	return common.HexToAddress(fmt.Sprintf("0x%040x", assetID)), nil
}

func (b *simnetBridge) bridgeContractAllowance(ctx context.Context, assetID uint32) (*big.Int, error) {
	b.approvalsMtx.RLock()
	defer b.approvalsMtx.RUnlock()

	if b.approvedAssets[assetID] {
		return new(big.Int).SetBytes(common.MaxHash.Bytes()), nil
	}
	return big.NewInt(0), nil
}

func (b *simnetBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int, assetID uint32) (*types.Transaction, error) {
	b.approvalsMtx.Lock()
	b.approvedAssets[assetID] = amount.Cmp(big.NewInt(0)) > 0
	b.approvalsMtx.Unlock()

	action := "approved"
	if amount.Cmp(big.NewInt(0)) == 0 {
		action = "unapproved"
	}
	b.log.Infof("Simnet bridge: %s asset %d", action, assetID)

	to := b.addr
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   b.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
		GasTipCap: txOpts.GasTipCap,
		GasFeeCap: txOpts.GasFeeCap,
		Gas:       50000,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      nil,
	})

	signedTx, err := txOpts.Signer(b.addr, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := b.cb.SendTransaction(txOpts.Context, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx, nil
}

func (b *simnetBridge) requiresBridgeContractApproval(assetID uint32) bool {
	return asset.TokenInfo(assetID) != nil
}

func (b *simnetBridge) initiateBridge(txOpts *bind.TransactOpts, sourceAssetID, destAssetID uint32, amount *big.Int) (*types.Transaction, error) {
	destinations, ok := simnetBridgePaths[sourceAssetID]
	if !ok {
		return nil, fmt.Errorf("source asset %d not supported for simnet bridge", sourceAssetID)
	}
	if !slices.Contains(destinations, destAssetID) {
		return nil, fmt.Errorf("bridge from %d to %d not supported on simnet", sourceAssetID, destAssetID)
	}

	bridgeAddr, err := b.bridgeContractAddr(txOpts.Context, sourceAssetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge contract address: %w", err)
	}

	var tx *types.Transaction
	token := asset.TokenInfo(sourceAssetID)

	if token != nil {
		tokenAddr := common.HexToAddress(token.ContractAddress)
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   b.chainID,
			Nonce:     txOpts.Nonce.Uint64(),
			GasTipCap: txOpts.GasTipCap,
			GasFeeCap: txOpts.GasFeeCap,
			Gas:       100_000,
			To:        &tokenAddr,
			Value:     big.NewInt(0),
			Data:      erc20TransferData(bridgeAddr, amount),
		})
	} else {
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   b.chainID,
			Nonce:     txOpts.Nonce.Uint64(),
			GasTipCap: txOpts.GasTipCap,
			GasFeeCap: txOpts.GasFeeCap,
			Gas:       21000,
			To:        &bridgeAddr,
			Value:     amount,
			Data:      nil,
		})
	}

	signedTx, err := txOpts.Signer(b.addr, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := b.cb.SendTransaction(txOpts.Context, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	if err := b.sendToDestination(destAssetID, amount); err != nil {
		return nil, fmt.Errorf("failed to send funds to destination: %w", err)
	}

	return signedTx, nil
}

func erc20TransferData(to common.Address, amount *big.Int) []byte {
	erc20ABI, err := abi.JSON(strings.NewReader(dexerc20.IERC20ABI))
	if err != nil {
		panic(fmt.Errorf("failed to parse erc20 abi: %w", err))
	}

	data, err := erc20ABI.Pack("transfer", to, amount)
	if err != nil {
		panic(fmt.Errorf("failed to pack erc20 transfer data: %w", err))
	}

	return data
}

// sendToDestination executes the harness script to allocate funds on the
// destination chain. For base assets, it uses sendtoaddress. For tokens,
// it uses sendUSDC or sendUSDT.
func (b *simnetBridge) sendToDestination(destAssetID uint32, amount *big.Int) error {
	var destDir string
	var scriptName string

	if tok := asset.TokenInfo(destAssetID); tok != nil {
		destDir = simnetHarnessDir[tok.ParentID]
		scriptName = simnetTokenScript[destAssetID]
		if destDir == "" || scriptName == "" {
			return fmt.Errorf("unknown token destination asset ID: %d", destAssetID)
		}
	} else if dir, ok := simnetHarnessDir[destAssetID]; ok {
		destDir = dir
		scriptName = "sendtoaddress"
	} else {
		return fmt.Errorf("unknown destination asset ID: %d", destAssetID)
	}

	ui, err := asset.UnitInfo(destAssetID)
	if err != nil {
		return fmt.Errorf("failed to get unit info for destination asset %d: %w", destAssetID, err)
	}
	if ui.Conventional.ConversionFactor == 0 {
		return fmt.Errorf("zero conversion factor for destination asset %d", destAssetID)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	scriptPath := filepath.Join(homeDir, "dextest", destDir, "harness-ctl", scriptName)

	// Check if script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("harness script not found: %s", scriptPath)
	}

	amountAtomic, err := evmToDEXAtomic(destAssetID, amount)
	if err != nil {
		return err
	}
	if !amountAtomic.IsUint64() {
		return fmt.Errorf("amount too large: %s", amountAtomic)
	}

	addrStr := b.addr.Hex()
	addrStr = strings.TrimPrefix(addrStr, "0x")
	amountStr := ui.ConventionalString(amountAtomic.Uint64())
	amountStr = strings.TrimRight(strings.TrimRight(amountStr, "0"), ".")

	scriptDir := filepath.Dir(scriptPath)
	b.log.Infof("Simnet bridge: running %s %s %s on %s", scriptName, addrStr, amountStr, destDir)
	cmd := exec.Command(scriptPath, addrStr, amountStr)
	cmd.Dir = scriptDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w, output: %s", scriptName, err, string(output))
	}

	b.log.Infof("Simnet bridge: %s output: %s", scriptName, strings.TrimSpace(string(output)))
	return nil
}

func evmToDEXAtomic(assetID uint32, evmAmount *big.Int) (*big.Int, error) {
	if evmAmount == nil {
		return nil, fmt.Errorf("nil amount")
	}
	if evmAmount.Sign() < 0 {
		return nil, fmt.Errorf("negative amount: %s", evmAmount)
	}

	if tok, found := dexeth.Tokens[assetID]; found {
		return new(big.Int).SetUint64(tok.EVMToAtomic(evmAmount)), nil
	}
	if tok, found := dexpolygon.Tokens[assetID]; found {
		return new(big.Int).SetUint64(tok.EVMToAtomic(evmAmount)), nil
	}
	if tok, found := dexbase.Tokens[assetID]; found {
		return new(big.Int).SetUint64(tok.EVMToAtomic(evmAmount)), nil
	}

	return new(big.Int).SetUint64(dexeth.WeiToGwei(evmAmount)), nil
}

// getCompletionData retrieves the data needed to complete the bridge.
// For simnet, bridge completion is instant so we return empty data immediately.
func (b *simnetBridge) getCompletionData(ctx context.Context, sourceAssetID uint32, bridgeTxID string) ([]byte, error) {
	// Return empty data to indicate completion is ready
	return []byte{}, nil
}

// completeBridge completes the bridge on the destination chain.
// For simnet, this is a no-op since funds are "instantly" available.
func (b *simnetBridge) completeBridge(txOpts *bind.TransactOpts, destAssetID uint32, mintInfo []byte) (*types.Transaction, error) {
	// No completion transaction needed for simnet
	return nil, nil
}

// getFollowUpCompletionData returns data for follow-up completion.
// For simnet, no follow-up is required.
func (b *simnetBridge) getFollowUpCompletionData(ctx context.Context, completionTxID string) (required bool, data []byte, err error) {
	return false, nil, nil
}

// completeFollowUpBridge executes any follow-up completion.
// For simnet, no follow-up is required.
func (b *simnetBridge) completeFollowUpBridge(txOpts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return nil, nil
}

// initiateBridgeGas returns the gas cost for initiating a bridge.
func (b *simnetBridge) initiateBridgeGas(sourceAssetID uint32) uint64 {
	return 21000
}

// completeBridgeGas returns the gas cost for completing a bridge.
func (b *simnetBridge) completeBridgeGas(destAssetID uint32) uint64 {
	return 0
}

// followUpCompleteBridgeGas returns the gas cost for follow-up completion.
func (b *simnetBridge) followUpCompleteBridgeGas() uint64 {
	return 0
}

// requiresCompletion returns whether completion is required on destination.
// For simnet, we simulate instant completion.
func (b *simnetBridge) requiresCompletion(destAssetID uint32) bool {
	return false
}

// requiresFollowUpCompletion returns whether follow-up completion is required.
func (b *simnetBridge) requiresFollowUpCompletion(destAssetID uint32) bool {
	return false
}

// verifyBridgeCompletion verifies the bridge was completed.
// For simnet, this always returns true.
func (b *simnetBridge) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return true, nil
}

// supportedDestinations returns destinations supported from the source asset.
func (b *simnetBridge) supportedDestinations(sourceAssetID uint32) []uint32 {
	destinations, ok := simnetBridgePaths[sourceAssetID]
	if !ok {
		return nil
	}
	return destinations
}

// bridgeLimits returns the min/max amounts for bridging.
// For simnet, there are no limits.
func (b *simnetBridge) bridgeLimits(sourceAssetID, destAssetID uint32) (min, max *big.Int, hasLimits bool, err error) {
	return big.NewInt(0), big.NewInt(0), false, nil
}
