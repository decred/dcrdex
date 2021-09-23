// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const (
	initiateFuncName = "initiate"
	numInputArgs     = 3
	redeemFuncName   = "redeem"
	numRedeemArgs    = 2
)

// ParseInitiateData accepts call data from a transaction that pays to a
// contract with extra data. It will error if the call data does not call
// initiate with expected argument types. It returns the participant address,
// the secret hash, and locktime.
func ParseInitiateData(calldata []byte) (*common.Address, [32]byte, int64, error) {
	fail := func(err error) (*common.Address, [32]byte, int64, error) {
		return nil, [32]byte{}, 0, err
	}
	decoded, err := parseCallData(calldata, ETHSwapABI)
	if err != nil {
		return fail(fmt.Errorf("unable to parse call data: %v", err))
	}
	if decoded.name != initiateFuncName {
		return fail(fmt.Errorf("expected %v function but got %v", initiateFuncName, decoded.name))
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	if len(args) != numInputArgs {
		return fail(fmt.Errorf("expected %v input args but got %v", numInputArgs, len(args)))
	}
	locktime, ok := args[0].value.(*big.Int)
	if !ok {
		return fail(fmt.Errorf("expected first arg of type *big.Int but got %T", args[0].value))
	}
	if !locktime.IsInt64() {
		return fail(fmt.Errorf("locktime %v cannot be expressed as an int64", locktime))
	}
	secretHash, ok := args[1].value.([32]byte)
	if !ok {
		return fail(fmt.Errorf("expected second arg of type [32]byte but got %T", args[1].value))
	}
	participantAddr, ok := args[2].value.(common.Address)
	if !ok {
		return fail(fmt.Errorf("expected third arg of type common.Address but got %T", args[2].value))
	}
	return &participantAddr, secretHash, locktime.Int64(), nil
}

// ParseRedeemData accepts call data from a transaction that pays to a
// contract with extra data. It will error if the call data does not call
// redeem with expected argument types. It returns the secret and secret hash
// in that order.
func ParseRedeemData(calldata []byte) (secret [32]byte, secretHash [32]byte, err error) {
	fail := func(err error) ([32]byte, [32]byte, error) {
		return [32]byte{}, [32]byte{}, err
	}
	decoded, err := parseCallData(calldata, ETHSwapABI)
	if err != nil {
		return fail(fmt.Errorf("unable to parse call data: %v", err))
	}
	if decoded.name != redeemFuncName {
		return fail(fmt.Errorf("expected %v function but got %v", redeemFuncName, decoded.name))
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	if len(args) != numRedeemArgs {
		return fail(fmt.Errorf("expected %v redeem args but got %v", numRedeemArgs, len(args)))
	}
	secret, ok := args[0].value.([32]byte)
	if !ok {
		return fail(fmt.Errorf("expected first arg of type [32]byte but got %T", args[0].value))
	}
	secretHash, ok = args[1].value.([32]byte)
	if !ok {
		return fail(fmt.Errorf("expected second arg of type [32]byte but got %T", args[1].value))
	}
	return secret, secretHash, nil
}
