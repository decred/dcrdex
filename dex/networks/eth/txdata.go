// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ParseInitiateData parses the calldata used to call the initiate function of a
// specific version of the swap contract. It returns the the list of initiations
// done in the call and errors if the call data does not call initiate initiate
// with expected argument types.
func ParseInitiateData(calldata []byte, contractVersion uint32) (map[[SecretHashSize]byte]*Initiation, error) {
	txDataHandler, ok := txDataHandlers[contractVersion]
	if !ok {
		return nil, fmt.Errorf("contract version %v does not exist", contractVersion)
	}

	return txDataHandler.parseInitiateData(calldata)
}

// ParseRedeemData parses the calldata used to call the redeem function of a
// specific version of the swap contract. It returns the the list of redemptions
// done in the call and errors if the call data does not call redeem with expected
// argument types.
func ParseRedeemData(calldata []byte, contractVersion uint32) (map[[SecretHashSize]byte]*Redemption, error) {
	txDataHandler, ok := txDataHandlers[contractVersion]
	if !ok {
		return nil, fmt.Errorf("contract version %v does not exist", contractVersion)
	}

	return txDataHandler.parseRedeemData(calldata)
}

// ParseRefundData parses the calldata used to call the refund function of a
// specific version of the swap contract. It returns the secret hash and errors
// if the call data does not call refund with expected argument types.
func ParseRefundData(calldata []byte, contractVersion uint32) ([32]byte, error) {
	txDataHandler, ok := txDataHandlers[contractVersion]
	if !ok {
		return [32]byte{}, fmt.Errorf("contract version %v does not exist", contractVersion)
	}

	return txDataHandler.parseRefundData(calldata)
}

// ABIs maps each swap contract's version to that version's parsed ABI.
var ABIs = initAbis()

func initAbis() map[uint32]*abi.ABI {
	v0ABI, err := abi.JSON(strings.NewReader(swapv0.ETHSwapABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse abi: %v", err))
	}

	return map[uint32]*abi.ABI{
		0: &v0ABI,
	}
}

type txDataHandler interface {
	parseInitiateData([]byte) (map[[SecretHashSize]byte]*Initiation, error)
	parseRedeemData([]byte) (map[[SecretHashSize]byte]*Redemption, error)
	parseRefundData([]byte) ([32]byte, error)
}

var txDataHandlers = map[uint32]txDataHandler{
	0: newTxDataV0(),
}

type txDataHandlerV0 struct {
	initiateFuncName string
	redeemFuncName   string
	refundFuncName   string
}

func newTxDataV0() *txDataHandlerV0 {
	return &txDataHandlerV0{
		initiateFuncName: "initiate",
		redeemFuncName:   "redeem",
		refundFuncName:   "refund",
	}
}

func (t *txDataHandlerV0) parseInitiateData(calldata []byte) (map[[SecretHashSize]byte]*Initiation, error) {
	decoded, err := parseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != t.initiateFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", t.initiateFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	const numArgs = 1
	if len(args) != numArgs {
		return nil, fmt.Errorf("expected %v input args but got %v", numArgs, len(args))
	}
	initiations, ok := args[0].value.([]struct {
		RefundTimestamp *big.Int       `json:"refundTimestamp"`
		SecretHash      [32]byte       `json:"secretHash"`
		Participant     common.Address `json:"participant"`
		Value           *big.Int       `json:"value"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swapv0.ETHSwapInitiation but got %T", args[0].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv0.ETHSwapInitiation are the same, other than the tags.
	if len(initiations) > 0 {
		_ = swapv0.ETHSwapInitiation(initiations[0])
	}

	toReturn := make(map[[SecretHashSize]byte]*Initiation)
	for _, init := range initiations {
		gweiValue, err := ToGwei(init.Value)
		if err != nil {
			return nil, fmt.Errorf("cannot convert wei to gwei: %w", err)
		}

		toReturn[init.SecretHash] = &Initiation{
			LockTime:    time.Unix(init.RefundTimestamp.Int64(), 0),
			SecretHash:  init.SecretHash,
			Participant: init.Participant,
			Value:       gweiValue,
		}
	}

	return toReturn, nil
}

func (t *txDataHandlerV0) parseRedeemData(calldata []byte) (map[[SecretHashSize]byte]*Redemption, error) {
	decoded, err := parseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != t.redeemFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", t.redeemFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	const numArgs = 1
	if len(args) != numArgs {
		return nil, fmt.Errorf("expected %v redeem args but got %v", numArgs, len(args))
	}
	redemptions, ok := args[0].value.([]struct {
		Secret     [32]byte `json:"secret"`
		SecretHash [32]byte `json:"secretHash"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swapv0.ETHSwapRedemption but got %T", args[0].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv0.ETHSwapRedemption are the same, other than the tags.
	if len(redemptions) > 0 {
		_ = swapv0.ETHSwapRedemption(redemptions[0])
	}

	toReturn := make(map[[SecretHashSize]byte]*Redemption)
	for _, redemption := range redemptions {
		toReturn[redemption.SecretHash] = &Redemption{
			SecretHash: redemption.SecretHash,
			Secret:     redemption.Secret,
		}
	}

	return toReturn, nil
}

func (t *txDataHandlerV0) parseRefundData(calldata []byte) ([32]byte, error) {
	var secretHash [32]byte

	decoded, err := parseCallData(calldata, ABIs[0])
	if err != nil {
		return secretHash, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.name != t.refundFuncName {
		return secretHash, fmt.Errorf("expected %v function but got %v", t.refundFuncName, decoded.name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	const numArgs = 1
	if len(args) != numArgs {
		return secretHash, fmt.Errorf("expected %v redeem args but got %v", numArgs, len(args))
	}
	secretHash, ok := args[0].value.([32]byte)
	if !ok {
		return secretHash, fmt.Errorf("expected first arg of type [32]byte but got %T", args[0].value)
	}

	return secretHash, nil
}
