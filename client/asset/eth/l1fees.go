// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// gasPriceOracleAddr is the OP Stack GasPriceOracle precompile address.
// See https://docs.optimism.io/chain/addresses
var gasPriceOracleAddr = common.HexToAddress("0x420000000000000000000000000000000000000F")

// gasPriceOracleABI is the minimal ABI for the OP Stack GasPriceOracle's
// getL1Fee method: getL1Fee(bytes) returns (uint256).
var gasPriceOracleABI *abi.ABI

func init() {
	const abiJSON = `[{"inputs":[{"internalType":"bytes","name":"_data","type":"bytes"}],"name":"getL1Fee","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(fmt.Sprintf("failed to parse GasPriceOracle ABI: %v", err))
	}
	gasPriceOracleABI = &parsed
}

// wrapCalldata wraps the given calldata in a dummy DynamicFeeTx and returns
// the RLP-encoded transaction. The OP Stack GasPriceOracle.getL1Fee expects
// the full RLP-encoded transaction, not just the calldata, because the L1 fee
// is based on the total serialized size including the transaction envelope.
// A zero ChainID is used because getL1Fee only cares about the serialized
// size, not the chain — the size difference from ChainID encoding is negligible.
func wrapCalldata(calldata []byte) ([]byte, error) {
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(0),
		Nonce:     0,
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(0),
		Gas:       0,
		To:        &common.Address{},
		Value:     big.NewInt(0),
		Data:      calldata,
	})
	return tx.MarshalBinary()
}

// l1FeeForCalldata calls the OP Stack GasPriceOracle precompile to estimate
// the L1 fee for a transaction with the given calldata. The calldata is wrapped
// in a dummy RLP-encoded transaction before being passed to getL1Fee, which
// expects the full serialized transaction.
func l1FeeForCalldata(ctx context.Context, cb bind.ContractBackend, calldata []byte) (*big.Int, error) {
	rawTx, err := wrapCalldata(calldata)
	if err != nil {
		return nil, fmt.Errorf("error wrapping calldata in dummy tx: %w", err)
	}
	data, err := gasPriceOracleABI.Pack("getL1Fee", rawTx)
	if err != nil {
		return nil, fmt.Errorf("error packing getL1Fee: %w", err)
	}

	result, err := cb.CallContract(ctx, ethereum.CallMsg{
		To:   &gasPriceOracleAddr,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error calling GasPriceOracle.getL1Fee: %w", err)
	}

	out, err := gasPriceOracleABI.Unpack("getL1Fee", result)
	if err != nil {
		return nil, fmt.Errorf("error unpacking getL1Fee result: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty result from getL1Fee")
	}
	fee, ok := out[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T for getL1Fee result", out[0])
	}
	return fee, nil
}
