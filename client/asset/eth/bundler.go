package eth

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"decred.org/dcrdex/dex/networks/eth/contracts/entrypoint"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// getUserOpReceiptResult represents the result of a user operation receipt query.
type getUserOpReceiptResult struct {
	actualGasCost *big.Int
	receipt       *types.Receipt
	success       bool
	nonce         *big.Int
}

// estimateBundlerGasResult holds gas estimation results for a user operation.
// Each string is a hex string starting with "0x".
type estimateBundlerGasResult struct {
	PreVerificationGas   string `json:"preVerificationGas"`
	VerificationGasLimit string `json:"verificationGasLimit"`
	CallGasLimit         string `json:"callGasLimit"`
}

// totalGas calculates the total gas required based on pre-verification,
// verification, and call gas limits. Incorrectly formatted gas values are
// ignored.
func (r *estimateBundlerGasResult) totalGas() uint64 {
	preVerificationGas, _ := strconv.ParseUint(strings.TrimPrefix(r.PreVerificationGas, "0x"), 16, 64)
	verificationGasLimit, _ := strconv.ParseUint(strings.TrimPrefix(r.VerificationGasLimit, "0x"), 16, 64)
	callGasLimit, _ := strconv.ParseUint(strings.TrimPrefix(r.CallGasLimit, "0x"), 16, 64)
	return preVerificationGas + verificationGasLimit + callGasLimit
}

// bundlerImpl defines the type for different bundler implementations.
type bundlerImpl uint8

const (
	rundler bundlerImpl = iota
	skandha
	pimlico
)

// bundler is an interface to interact with an ERC-4337 bundler.
type bundler interface {
	supportedEntryPoints(ctx context.Context) (map[common.Address]interface{}, error)
	sendUserOp(ctx context.Context, userOp *userOp) (common.Hash, error)
	getUserOpReceipt(ctx context.Context, userOpHash common.Hash) (*getUserOpReceiptResult, error)
	withNonce(opts *bind.CallOpts, ethSwapAddr, participantAddr common.Address, f func(*big.Int) error) error
	estimateGas(ctx context.Context, userOp *userOp) (*estimateBundlerGasResult, error)
	getGasPrice(ctx context.Context) (maxFeePerGas, maxPriorityFeePerGas string, err error)
}

// rpcBundler implements the bundler interface.
type rpcBundler struct {
	rpcClient             *rpc.Client
	entryPointAddress     common.Address
	entryPoint            *entrypoint.EntrypointCaller
	nonceMtx              sync.Mutex
	bundlerImplementation bundlerImpl
	getBaseFee            func(ctx context.Context) (*big.Int, error)
}

var _ bundler = (*rpcBundler)(nil)

// newBundler creates a new bundler instance with the specified endpoint. If
// the bundler does not support the specified entrypoint, or is not one of the
// supported bundler implementations, an error is returned.
func newBundler(ctx context.Context, endpoint string, entryPointAddr common.Address, backend bind.ContractCaller, getBaseFee func(ctx context.Context) (*big.Int, error)) (*rpcBundler, error) {
	rpcClient, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	entryPoint, err := entrypoint.NewEntrypointCaller(entryPointAddr, backend)
	if err != nil {
		return nil, err
	}

	b := &rpcBundler{
		rpcClient:         rpcClient,
		entryPoint:        entryPoint,
		entryPointAddress: entryPointAddr,
		getBaseFee:        getBaseFee,
	}

	// Verify that the entry point is supported by the bundler.
	entryPoints, err := b.supportedEntryPoints(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := entryPoints[entryPointAddr]; !ok {
		supported := make([]string, 0, len(entryPoints))
		for ep := range entryPoints {
			supported = append(supported, ep.Hex())
		}
		return nil, fmt.Errorf("entry point %s not supported; supported entry points: %v", entryPointAddr.Hex(), supported)
	}

	impl, err := b.implementation(ctx)
	if err != nil {
		return nil, err
	}
	b.bundlerImplementation = impl

	return b, nil
}

// supportedEntryPoints returns the entry points supported by the bundler.
func (b *rpcBundler) supportedEntryPoints(ctx context.Context) (map[common.Address]interface{}, error) {
	var res []string
	err := b.rpcClient.CallContext(ctx, &res, "eth_supportedEntryPoints")
	if err != nil {
		return nil, err
	}

	entryPoints := make(map[common.Address]interface{}, len(res))
	for _, v := range res {
		entryPoints[common.HexToAddress(v)] = true
	}

	return entryPoints, nil
}

// implementation determines the specific bundler implementation by probing
// the gas price rpc methods.
func (b *rpcBundler) implementation(ctx context.Context) (bundlerImpl, error) {
	_, _, err := b.rundlerGetGasPrice(ctx)
	if err == nil {
		return rundler, nil
	}

	_, _, err = b.skandhaGetGasPrice(ctx)
	if err == nil {
		return skandha, nil
	}

	_, _, err = b.pimlicoGetGasPrice(ctx)
	if err == nil {
		return pimlico, nil
	}

	return 0, fmt.Errorf("unknown bundler implementation. Supported implementations: rundler, skandha, pimlico")
}

// userOp represents a user operation as defined by ERC-4337.
type userOp struct {
	Sender               string `json:"sender"`
	Nonce                string `json:"nonce"`
	InitCode             string `json:"initCode"`
	CallData             string `json:"callData"`
	CallGasLimit         string `json:"callGasLimit"`
	VerificationGasLimit string `json:"verificationGasLimit"`
	PreVerificationGas   string `json:"preVerificationGas"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
	PaymasterAndData     string `json:"paymasterAndData"`
	Signature            string `json:"signature"`
}

func (op *userOp) hash(entryPoint common.Address, chainID *big.Int) (common.Hash, error) {
	parseBigInt := func(hexStr string) *big.Int {
		intB := common.FromHex(hexStr)
		result := new(big.Int)
		result.SetBytes(intB)
		return result
	}

	address, _ := abi.NewType("address", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)
	bytes32, _ := abi.NewType("bytes32", "", nil)

	args := abi.Arguments{
		{Name: "sender", Type: address},
		{Name: "nonce", Type: uint256},
		{Name: "hashInitCode", Type: bytes32},
		{Name: "hashCallData", Type: bytes32},
		{Name: "callGasLimit", Type: uint256},
		{Name: "verificationGasLimit", Type: uint256},
		{Name: "preVerificationGas", Type: uint256},
		{Name: "maxFeePerGas", Type: uint256},
		{Name: "maxPriorityFeePerGas", Type: uint256},
		{Name: "hashPaymasterAndData", Type: bytes32},
	}

	sender := common.HexToAddress(op.Sender)
	nonce := parseBigInt(op.Nonce)
	callGasLimit := parseBigInt(op.CallGasLimit)
	verificationGasLimit := parseBigInt(op.VerificationGasLimit)
	preVerificationGas := parseBigInt(op.PreVerificationGas)
	maxFeePerGas := parseBigInt(op.MaxFeePerGas)
	maxPriorityFeePerGas := parseBigInt(op.MaxPriorityFeePerGas)
	initCodeHash := crypto.Keccak256Hash(common.FromHex(op.InitCode))
	callDataHash := crypto.Keccak256Hash(common.FromHex(op.CallData))
	paymasterAndDataHash := crypto.Keccak256Hash(common.FromHex(op.PaymasterAndData))

	packed, err := args.Pack(
		sender,
		nonce,
		initCodeHash,
		callDataHash,
		callGasLimit,
		verificationGasLimit,
		preVerificationGas,
		maxFeePerGas,
		maxPriorityFeePerGas,
		paymasterAndDataHash,
	)
	if err != nil {
		return common.Hash{}, err
	}

	return crypto.Keccak256Hash(
		crypto.Keccak256(packed),
		common.LeftPadBytes(entryPoint.Bytes(), 32),
		common.LeftPadBytes(chainID.Bytes(), 32),
	), nil
}

// sendUserOp sends a user operation to the bundler via the eth_sendUserOperation RPC method.
// Returns the hash of the user operation if successful.
func (b *rpcBundler) sendUserOp(ctx context.Context, userOp *userOp) (common.Hash, error) {
	var res string
	err := b.rpcClient.CallContext(ctx, &res, "eth_sendUserOperation", *userOp, b.entryPointAddress)
	if err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(res), nil
}

// getUserOpReceipt returns the receipt of a user operation.
// If receipt is nil, the user operation has not yet been included in a block.
func (b *rpcBundler) getUserOpReceipt(ctx context.Context, userOpHash common.Hash) (*getUserOpReceiptResult, error) {
	var res struct {
		Nonce         string         `json:"nonce"`
		ActualGasCost string         `json:"actualGasCost"`
		Success       bool           `json:"success"`
		Receipt       *types.Receipt `json:"receipt"`
	}
	err := b.rpcClient.CallContext(ctx, &res, "eth_getUserOperationReceipt", userOpHash)
	if err != nil {
		return nil, err
	}
	if res.Receipt == nil {
		return &getUserOpReceiptResult{}, nil
	}

	actualGasCost := new(big.Int)
	_, ok := actualGasCost.SetString(strings.TrimPrefix(res.ActualGasCost, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("failed to parse actual gas cost: %s", res.ActualGasCost)
	}

	nonce := new(big.Int)
	_, ok = nonce.SetString(strings.TrimPrefix(res.Nonce, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("failed to parse nonce: %s", res.Nonce)
	}

	return &getUserOpReceiptResult{
		nonce:         nonce,
		actualGasCost: actualGasCost,
		receipt:       res.Receipt,
		success:       res.Success,
	}, nil
}

// withNonce retrieves the nonce for a participant and passes it to the provided function.
// A mutex is held during the entire function call to prevent race conditions.
func (b *rpcBundler) withNonce(opts *bind.CallOpts, ethSwapAddr, participantAddr common.Address, f func(*big.Int) error) error {
	b.nonceMtx.Lock()
	defer b.nonceMtx.Unlock()

	keyB := make([]byte, 24)
	copy(keyB[:20], participantAddr[:])
	key := new(big.Int).SetBytes(keyB)
	nonce, err := b.entryPoint.GetNonce(opts, ethSwapAddr, key)
	if err != nil {
		return err
	}

	return f(nonce)
}

// estimateGas estimates the gas required for a user operation.
// The userOp must be properly formatted for the bundler's eth_estimateUserOperationGas method.
func (b *rpcBundler) estimateGas(ctx context.Context, userOp *userOp) (*estimateBundlerGasResult, error) {
	var res estimateBundlerGasResult
	err := b.rpcClient.CallContext(ctx, &res, "eth_estimateUserOperationGas", *userOp, b.entryPointAddress)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// rundlerGetGasPrice retrieves gas prices for the Rundler implementation.
// Rundler only returns the max priority fee per gas, and not the max fee per
// gas, so we calculate it using 2 * baseFee + maxPriorityFeePerGas.
func (b *rpcBundler) rundlerGetGasPrice(ctx context.Context) (string, string, error) {
	baseFee, err := b.getBaseFee(ctx)
	if err != nil {
		return "", "", err
	}

	var res string
	err = b.rpcClient.CallContext(ctx, &res, "rundler_maxPriorityFeePerGas")
	if err != nil {
		return "", "", err
	}

	maxPriorityFeePerGas, ok := new(big.Int).SetString(strings.TrimPrefix(res, "0x"), 16)
	if !ok {
		return "", "", fmt.Errorf("failed to parse max priority fee per gas: %s", res)
	}
	maxFeePerGasBI := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), maxPriorityFeePerGas)
	maxFeePerGas := "0x" + maxFeePerGasBI.Text(16)

	return maxFeePerGas, res, nil
}

// skandhaGetGasPrice retrieves gas prices for the Skandha implementation.
func (b *rpcBundler) skandhaGetGasPrice(ctx context.Context) (maxFeePerGas, maxPriorityFeePerGas string, err error) {
	res := struct {
		MaxFeePerGas         string `json:"maxFeePerGas"`
		MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
	}{}
	err = b.rpcClient.CallContext(ctx, &res, "skandha_getGasPrice")
	if err != nil {
		return "", "", err
	}
	return res.MaxFeePerGas, res.MaxPriorityFeePerGas, nil
}

// pimlicoGetGasPrice retrieves gas prices for the Pimlico implementation.
func (b *rpcBundler) pimlicoGetGasPrice(ctx context.Context) (maxFeePerGas, maxPriorityFeePerGas string, err error) {
	var res struct {
		Fast struct {
			MaxFeePerGas         string `json:"maxFeePerGas"`
			MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
		} `json:"fast"`
	}

	err = b.rpcClient.CallContext(ctx, &res, "pimlico_getUserOperationGasPrice")
	if err != nil {
		return "", "", err
	}

	return res.Fast.MaxFeePerGas, res.Fast.MaxPriorityFeePerGas, nil
}

// getGasPrice returns the bundler's suggested max fee per gas and max priority fee per gas.
func (b *rpcBundler) getGasPrice(ctx context.Context) (maxFeePerGas, maxPriorityFeePerGas string, err error) {
	switch b.bundlerImplementation {
	case rundler:
		return b.rundlerGetGasPrice(ctx)
	case skandha:
		return b.skandhaGetGasPrice(ctx)
	case pimlico:
		return b.pimlicoGetGasPrice(ctx)
	}
	return "", "", fmt.Errorf("unsupported bundler implementation: %d", b.bundlerImplementation)
}
