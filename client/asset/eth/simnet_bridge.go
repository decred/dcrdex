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

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

const simnetBridgeName = "simnet"

// Token asset IDs for simnet bridge support
var (
	simnetUsdcEthID, _     = dex.BipSymbolID("usdc.eth")
	simnetUsdtEthID, _     = dex.BipSymbolID("usdt.eth")
	simnetUsdcPolygonID, _ = dex.BipSymbolID("usdc.polygon")
	simnetUsdtPolygonID, _ = dex.BipSymbolID("usdt.polygon")
	simnetUsdcBaseID, _    = dex.BipSymbolID("usdc.base")
	simnetUsdtBaseID, _    = dex.BipSymbolID("usdt.base")
)

// simnetHarnessDir maps base chain asset IDs to their harness directory names.
var simnetHarnessDir = map[uint32]string{
	ethID:     "eth",
	polygonID: "polygon",
	baseID:    "base",
}

// simnetTokenParent maps token asset IDs to their parent chain asset ID.
var simnetTokenParent = map[uint32]uint32{
	simnetUsdcEthID:     ethID,
	simnetUsdtEthID:     ethID,
	simnetUsdcPolygonID: polygonID,
	simnetUsdtPolygonID: polygonID,
	simnetUsdcBaseID:    baseID,
	simnetUsdtBaseID:    baseID,
}

// simnetTokenScript maps token asset IDs to their harness script name.
var simnetTokenScript = map[uint32]string{
	simnetUsdcEthID:     "sendUSDC",
	simnetUsdtEthID:     "sendUSDT",
	simnetUsdcPolygonID: "sendUSDC",
	simnetUsdtPolygonID: "sendUSDT",
	simnetUsdcBaseID:    "sendUSDC",
	simnetUsdtBaseID:    "sendUSDT",
}

// simnetBridge is a mock bridge implementation for testing on simnet.
// It allows testing the bridge UI without requiring real cross-chain infrastructure.
// The bridge executes harness scripts to allocate funds on the destination chain.
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

// newSimnetBridge creates a new simnet bridge for testing.
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

// simnetBridgePaths defines the supported bridge paths on simnet.
// Maps source asset ID -> destination asset IDs
var simnetBridgePaths = map[uint32][]uint32{
	// Base chain assets
	ethID:     {polygonID, baseID},
	polygonID: {ethID, baseID},
	baseID:    {ethID, polygonID},
	// USDC tokens
	simnetUsdcEthID:     {simnetUsdcPolygonID, simnetUsdcBaseID},
	simnetUsdcPolygonID: {simnetUsdcEthID, simnetUsdcBaseID},
	simnetUsdcBaseID:    {simnetUsdcEthID, simnetUsdcPolygonID},
	// USDT tokens
	simnetUsdtEthID:     {simnetUsdtPolygonID, simnetUsdtBaseID},
	simnetUsdtPolygonID: {simnetUsdtEthID, simnetUsdtBaseID},
	simnetUsdtBaseID:    {simnetUsdtEthID, simnetUsdtPolygonID},
}

// bridgeContractAddr returns the bridge contract address.
// For simnet, this returns a deterministic mock address based on the asset ID.
// This is needed for the pending approval tracking system.
func (b *simnetBridge) bridgeContractAddr(ctx context.Context, assetID uint32) (common.Address, error) {
	// Return a deterministic address based on asset ID for tracking
	return common.HexToAddress(fmt.Sprintf("0x%040x", assetID)), nil
}

// bridgeContractAllowance returns the token allowance for the bridge contract.
// For simnet, this returns the allowance based on the mock approval state.
func (b *simnetBridge) bridgeContractAllowance(ctx context.Context, assetID uint32) (*big.Int, error) {
	b.approvalsMtx.RLock()
	defer b.approvalsMtx.RUnlock()

	if b.approvedAssets[assetID] {
		// Return max allowance when approved
		return new(big.Int).SetBytes(common.MaxHash.Bytes()), nil
	}
	// Return zero allowance when not approved
	return big.NewInt(0), nil
}

// approveBridgeContract approves the bridge contract to spend tokens.
// For simnet, this updates the mock approval state and returns a mock transaction.
// If amount > 0, the asset is approved. If amount == 0, it is unapproved.
func (b *simnetBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int, assetID uint32) (*types.Transaction, error) {
	b.approvalsMtx.Lock()
	// Update approval state: amount > 0 means approve, amount == 0 means unapprove
	b.approvedAssets[assetID] = amount.Cmp(big.NewInt(0)) > 0
	b.approvalsMtx.Unlock()

	action := "approved"
	if amount.Cmp(big.NewInt(0)) == 0 {
		action = "unapproved"
	}
	b.log.Infof("Simnet bridge: %s asset %d", action, assetID)

	// Create a mock self-transfer transaction to simulate the approval tx
	to := b.addr
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   b.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
		GasTipCap: txOpts.GasTipCap,
		GasFeeCap: txOpts.GasFeeCap,
		Gas:       50000, // Approval gas estimate
		To:        &to,
		Value:     big.NewInt(0),
		Data:      nil,
	})

	// Sign the transaction
	signedTx, err := txOpts.Signer(b.addr, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send the transaction to the network
	if err := b.cb.SendTransaction(txOpts.Context, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx, nil
}

// requiresBridgeContractApproval returns whether approval is required.
// For simnet bridge, approval is required for tokens but not for base chain assets.
func (b *simnetBridge) requiresBridgeContractApproval(assetID uint32) bool {
	// Tokens require approval, base chain assets do not
	_, isToken := simnetTokenParent[assetID]
	return isToken
}

// initiateBridge initiates the bridge transaction.
// For simnet, this creates a self-transfer transaction on the source chain
// and executes the harness script on the destination chain.
func (b *simnetBridge) initiateBridge(txOpts *bind.TransactOpts, sourceAssetID, destAssetID uint32, amount *big.Int) (*types.Transaction, error) {
	// Verify the bridge path is supported
	destinations, ok := simnetBridgePaths[sourceAssetID]
	if !ok {
		return nil, fmt.Errorf("source asset %d not supported for simnet bridge", sourceAssetID)
	}

	if !slices.Contains(destinations, destAssetID) {
		return nil, fmt.Errorf("bridge from %d to %d not supported on simnet", sourceAssetID, destAssetID)
	}

	// Determine transaction value - for tokens, we send 0 ETH
	// For base chain assets, we send the actual amount
	var txValue *big.Int
	_, isToken := simnetTokenParent[sourceAssetID]
	if isToken {
		txValue = big.NewInt(0)
	} else {
		txValue = amount
	}

	// Create an EIP-1559 transaction (DynamicFeeTx) for the self-transfer
	// This simulates locking/burning funds on the source chain
	to := b.addr
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   b.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
		GasTipCap: txOpts.GasTipCap,
		GasFeeCap: txOpts.GasFeeCap,
		Gas:       21000, // Standard transfer gas
		To:        &to,
		Value:     txValue,
		Data:      nil,
	})

	// Sign the transaction using the txOpts signer
	signedTx, err := txOpts.Signer(b.addr, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	// Send the transaction to the network
	if err := b.cb.SendTransaction(txOpts.Context, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// Execute the harness script to send funds on the destination chain
	if err := b.sendToDestination(destAssetID, amount); err != nil {
		return nil, fmt.Errorf("failed to send funds to destination: %w", err)
	}

	return signedTx, nil
}

// sendToDestination executes the harness script to allocate funds on the
// destination chain. For base assets, it uses sendtoaddress. For tokens,
// it uses sendUSDC or sendUSDT.
func (b *simnetBridge) sendToDestination(destAssetID uint32, amount *big.Int) error {
	var destDir string
	var scriptName string

	// Check if destination is a token or base chain asset
	if parentID, isToken := simnetTokenParent[destAssetID]; isToken {
		destDir = simnetHarnessDir[parentID]
		scriptName = simnetTokenScript[destAssetID]
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
	conversionFactor := new(big.Float).SetUint64(ui.Conventional.ConversionFactor)

	// Build path to script
	// The harness scripts are in ~/dextest/{chain}/harness-ctl/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	scriptPath := filepath.Join(homeDir, "dextest", destDir, "harness-ctl", scriptName)

	// Check if script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("harness script not found: %s", scriptPath)
	}

	// Convert amount from atomic units to conventional units
	amountConv := new(big.Float).Quo(
		new(big.Float).SetInt(amount),
		conversionFactor,
	)

	addrStr := b.addr.Hex()
	addrStr = strings.TrimPrefix(addrStr, "0x")
	amountStr := amountConv.Text('f', 18)

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
	return 21000 // Standard ETH transfer gas
}

// completeBridgeGas returns the gas cost for completing a bridge.
func (b *simnetBridge) completeBridgeGas(destAssetID uint32) uint64 {
	return 0 // No completion transaction for simnet
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
