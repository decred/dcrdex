// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"math/big"
	"strings"

	"decred.org/dcrdex/dex/networks/eth/swap"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const (
	initiateFuncName = "initiate"
	numInputArgs     = 1
	redeemFuncName   = "redeem"
	numRedeemArgs    = 1
	refundFuncName   = "refund"
	numRefundArgs    = 1
)

// ParseInitiateData accepts call data from a transaction that pays to a
// contract with extra data. It will error if the call data does not call
// initiate with expected argument types. It returns the array of initiations
// with which initiate was called.
func ParseInitiateData(calldata []byte) ([]swap.ETHSwapInitiation, error) {
	decoded, err := parseCallData(calldata, swap.ETHSwapABI)
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != initiateFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", initiateFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	if len(args) != numInputArgs {
		return nil, fmt.Errorf("expected %v input args but got %v", numInputArgs, len(args))
	}
	initiations, ok := args[0].value.([]struct {
		RefundTimestamp *big.Int       `json:"refundTimestamp"`
		SecretHash      [32]byte       `json:"secretHash"`
		Participant     common.Address `json:"participant"`
		Value           *big.Int       `json:"value"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swap.ETHSwapInitiation but got %T", args[0].value)
	}

	toReturn := make([]swap.ETHSwapInitiation, 0, len(initiations))
	for _, init := range initiations {
		toReturn = append(toReturn, swap.ETHSwapInitiation(init))
	}

	return toReturn, nil
}

// ParseRedeemData accepts call data from a transaction that pays to a
// contract with extra data. It will error if the call data does not call
// redeem with expected argument types. It returns the secret and secret hash
// in that order.
func ParseRedeemData(calldata []byte) ([]swap.ETHSwapRedemption, error) {
	decoded, err := parseCallData(calldata, swap.ETHSwapABI)
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != redeemFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", redeemFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	if len(args) != numRedeemArgs {
		return nil, fmt.Errorf("expected %v redeem args but got %v", numRedeemArgs, len(args))
	}
	redemptions, ok := args[0].value.([]struct {
		Secret     [32]byte `json:"secret"`
		SecretHash [32]byte `json:"secretHash"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swap.ETHSwapRedemption but got %T", args[0].value)
	}
	toReturn := make([]swap.ETHSwapRedemption, 0, len(redemptions))
	for _, redemption := range redemptions {
		toReturn = append(toReturn, swap.ETHSwapRedemption(redemption))
	}

	return toReturn, nil
}

// ParseRedeemData accepts call data from a transaction that pays to a
// contract with extra data. It will error if the call data does not call
// redeem with expected argument types. It returns the secret and secret hash
// in that order.
func ParseRefundData(calldata []byte) ([32]byte, error) {
	var secretHash [32]byte

	decoded, err := parseCallData(calldata, swap.ETHSwapABI)
	if err != nil {
		return secretHash, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != refundFuncName {
		return secretHash, fmt.Errorf("expected %v function but got %v", refundFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	if len(args) != numRedeemArgs {
		return secretHash, fmt.Errorf("expected %v redeem args but got %v", numRedeemArgs, len(args))
	}
	secretHash, ok := args[0].value.([32]byte)
	if !ok {
		return secretHash, fmt.Errorf("expected first arg of type [32]byte but got %T", args[0].value)
	}

	return secretHash, nil
}

// PackInitiateData converts a list of swap.ETHSwapInitiation to call data for the
// initiate function.
func PackInitiateData(initiations []swap.ETHSwapInitiation) ([]byte, error) {
	parsedAbi, err := abi.JSON(strings.NewReader(swap.ETHSwapABI))
	if err != nil {
		return nil, err
	}
	return parsedAbi.Pack(initiateFuncName, initiations)
}

// PackRedeemData converts a list of swap.ETHSwapRedemption to call data for the
// redeem function.
func PackRedeemData(redemptions []swap.ETHSwapRedemption) ([]byte, error) {
	parsedAbi, err := abi.JSON(strings.NewReader(swap.ETHSwapABI))
	if err != nil {
		return nil, err
	}
	return parsedAbi.Pack(redeemFuncName, redemptions)
}

// PackRedeemData converts a secret hash to call data for the refund function.
func PackRefundData(secretHash [32]byte) ([]byte, error) {
	parsedAbi, err := abi.JSON(strings.NewReader(swap.ETHSwapABI))
	if err != nil {
		return nil, err
	}
	return parsedAbi.Pack(refundFuncName, secretHash)
}
