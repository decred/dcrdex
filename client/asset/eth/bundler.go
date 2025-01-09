package eth

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	v6EP "decred.org/dcrdex/dex/networks/eth/contracts/entrypoints/0.6"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

type getUserOpReceiptResult struct {
	actualGasCost *big.Int
	receipt       *types.Receipt
	success       bool
	nonce         *big.Int
}

type estimateBundlerGasResult struct {
	PreVerificationGas   string `json:"preVerificationGas"`
	VerificationGasLimit string `json:"verificationGasLimit"`
	CallGasLimit         string `json:"callGasLimit"`
}

// bundler is an interface to interact with an ERC-4337 bundler.
type bundler interface {
	// supportedEntryPoints returns the entry points supported by the bundler.
	supportedEntryPoints(ctx context.Context) ([]common.Address, error)
	// sendUserOp sends a user operation to the bundler.
	sendUserOp(ctx context.Context, userOp *userOpParam) (common.Hash, error)
	// getUserOpReceipt returns the receipt of a user operation.
	// If receipt is nil, the bundler has not yet sent the user operation to the
	// network.
	getUserOpReceipt(ctx context.Context, userOpHash common.Hash) (*getUserOpReceiptResult, error)
	// getNonce returns the nonce to use when sending a user operation. The
	// entrypoint contains nonces based on a 24 byte key. Since we are only
	// using account abstraction for redemptions, we use the participant's
	// address as the key.
	getNonce(opts *bind.CallOpts, ethSwapAddr, participantAddr common.Address) (*big.Int, error)
	// estimateGas estimates the gas required to send a user operation. The
	// returned values should be used when sending the user operation.
	estimateGas(ctx context.Context, userOp *userOpParam) (*estimateBundlerGasResult, error)
}

type rpcBundler struct {
	rpcClient  *rpc.Client
	epAddress  common.Address
	entryPoint *v6EP.EntrypointCaller
}

var _ bundler = (*rpcBundler)(nil)

func newBundler(ctx context.Context, endpoint string, epAddr common.Address, backend bind.ContractCaller) (*rpcBundler, error) {
	rpcClient, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	entryPoint, err := v6EP.NewEntrypointCaller(epAddr, backend)
	if err != nil {
		return nil, err
	}

	b := &rpcBundler{
		rpcClient:  rpcClient,
		entryPoint: entryPoint,
		epAddress:  epAddr,
	}

	// Check if the entry point is supported by bundler
	entryPoints, err := b.supportedEntryPoints(ctx)
	if err != nil {
		return nil, err
	}

	found := false
	for _, ep := range entryPoints {
		if ep == epAddr {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("entry point %s is not supported", epAddr.Hex())
	}

	return b, nil
}

func (b *rpcBundler) supportedEntryPoints(ctx context.Context) ([]common.Address, error) {
	var res []string
	err := b.rpcClient.CallContext(ctx, &res, "eth_supportedEntryPoints")
	if err != nil {
		return nil, err
	}

	entryPoints := make([]common.Address, len(res))
	for i, v := range res {
		entryPoints[i] = common.HexToAddress(v)
	}

	return entryPoints, nil
}

type userOpParam struct {
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

func (b *rpcBundler) sendUserOp(ctx context.Context, userOp *userOpParam) (common.Hash, error) {
	var res string
	err := b.rpcClient.CallContext(ctx, &res, "eth_sendUserOperation", *userOp, b.epAddress)
	if err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(res), nil
}

func (b *rpcBundler) getUserOpReceipt(ctx context.Context, userOpHash common.Hash) (*getUserOpReceiptResult, error) {
	var res struct {
		// UserOpHash    string         `json:"userOpHash"`
		// EntryPoint    string         `json:"entryPoint"`
		// Sender        string         `json:"sender"`
		Nonce string `json:"nonce"`
		// Paymaster     string         `json:"paymaster"`
		ActualGasCost string `json:"actualGasCost"`
		// ActualGasUsed string         `json:"actualGasUsed"`
		Success bool `json:"success"`
		// Reason        string         `json:"reason"`
		// Logs          []string       `json:"logs"`
		Receipt *types.Receipt `json:"receipt"`
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

func (b *rpcBundler) getNonce(opts *bind.CallOpts, ethSwapAddr, participantAddr common.Address) (*big.Int, error) {
	keyB := make([]byte, 24)
	copy(keyB[:20], participantAddr[:])
	key := new(big.Int).SetBytes(keyB)
	return b.entryPoint.GetNonce(opts, ethSwapAddr, key)
}

func (b *rpcBundler) estimateGas(ctx context.Context, userOp *userOpParam) (*estimateBundlerGasResult, error) {
	var res estimateBundlerGasResult
	err := b.rpcClient.CallContext(ctx, &res, "eth_estimateUserOperationGas", *userOp, b.epAddress)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
