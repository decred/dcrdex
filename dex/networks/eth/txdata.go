// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

const (
	initiateFuncName = "initiate"
	redeemFuncName   = "redeem"
	refundFuncName   = "refund"
)

// ABIs maps each swap contract's version to that version's parsed ABI.
var ABIs = initAbis()

func initAbis() map[uint32]*abi.ABI {
	v0ABI, err := abi.JSON(strings.NewReader(swapv0.ETHSwapABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse v0 abi: %v", err))
	}

	v1ABI, err := abi.JSON(strings.NewReader(swapv1.ETHSwapABI))
	if err != nil {
		panic(fmt.Sprintf("failed to parse v1 abi: %v", err))
	}

	return map[uint32]*abi.ABI{
		0: &v0ABI,
		1: &v1ABI,
	}
}

// ParseInitiateData parses the calldata used to call the initiate function of a
// specific version of the swap contract. It returns the the list of initiations
// done in the call and errors if the call data does not call initiate initiate
// with expected argument types.
func ParseInitiateDataV0(calldata []byte) (map[[SecretHashSize]byte]*Initiation, error) {
	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != initiateFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", initiateFuncName, decoded.Name)
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

	toReturn := make(map[[SecretHashSize]byte]*Initiation, len(initiations))
	for _, init := range initiations {
		toReturn[init.SecretHash] = &Initiation{
			LockTime:    time.Unix(init.RefundTimestamp.Int64(), 0),
			SecretHash:  init.SecretHash,
			Participant: init.Participant,
			Value:       init.Value,
			ValueGwei:   WeiToGwei(init.Value),
		}
	}

	return toReturn, nil
}

// ParseRedeemData parses the calldata used to call the redeem function of a
// specific version of the swap contract. It returns the the list of redemptions
// done in the call and errors if the call data does not call redeem with expected
// argument types.
func ParseRedeemDataV0(calldata []byte) (map[[SecretHashSize]byte]*Redemption, error) {
	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != redeemFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", redeemFuncName, decoded.Name)
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

	toReturn := make(map[[SecretHashSize]byte]*Redemption, len(redemptions))
	for _, redemption := range redemptions {
		toReturn[redemption.SecretHash] = &Redemption{
			SecretHash: redemption.SecretHash,
			Secret:     redemption.Secret,
		}
	}

	return toReturn, nil
}

// ParseRefundData parses the calldata used to call the refund function of a
// specific version of the swap contract. It returns the secret hash and errors
// if the call data does not call refund with expected argument types.
func ParseRefundDataV0(calldata []byte) ([32]byte, error) {
	var secretHash [32]byte

	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return secretHash, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != refundFuncName {
		return secretHash, fmt.Errorf("expected %v function but got %v", refundFuncName, decoded.Name)
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

type ContractV1 struct {
	*dex.SwapContractDetails
	Initiator string
}

type RedemptionV1 struct {
	Secret   [32]byte
	Contract *ContractV1
}

func ParseInitiateDataV1(calldata []byte) (map[[SecretHashSize]byte]*ContractV1, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != initiateFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", initiateFuncName, decoded.Name)
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
		SecretHash      [32]byte       `json:"secretHash"`
		Initiator       common.Address `json:"initiator"`
		RefundTimestamp uint64         `json:"refundTimestamp"`
		Participant     common.Address `json:"participant"`
		Value           uint64         `json:"value"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swapv1.ETHSwapContract but got %T", args[0].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv0.ETHSwapInitiation are the same, other than the tags.
	if len(initiations) > 0 {
		_ = swapv1.ETHSwapContract(initiations[0])
	}

	toReturn := make(map[[SecretHashSize]byte]*ContractV1, len(initiations))
	for _, init := range initiations {
		toReturn[init.SecretHash] = &ContractV1{
			SwapContractDetails: &dex.SwapContractDetails{
				From:       init.Initiator.String(),
				To:         init.Participant.String(),
				Value:      init.Value,
				SecretHash: init.SecretHash[:],
				LockTime:   init.RefundTimestamp,
			},
			Initiator: init.Initiator.String(),
		}
	}

	return toReturn, nil
}

func ParseRedeemDataV1(calldata []byte) (map[[SecretHashSize]byte]*RedemptionV1, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != redeemFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", redeemFuncName, decoded.Name)
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
		C struct {
			SecretHash      [32]uint8      `json:"secretHash"`
			Initiator       common.Address `json:"initiator"`
			RefundTimestamp uint64         `json:"refundTimestamp"`
			Participant     common.Address `json:"participant"`
			Value           uint64         `json:"value"`
		} `json:"c"`
		Secret [32]uint8 `json:"secret"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type []swapv0.ETHSwapRedemption but got %T", args[0].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv0.ETHSwapRedemption are the same, other than the tags.
	if len(redemptions) > 0 {
		// Why can't I do ETHSwapRedemption directly?
		_ = swapv1.ETHSwapContract(redemptions[0].C)
	}

	toReturn := make(map[[SecretHashSize]byte]*RedemptionV1, len(redemptions))
	for _, r := range redemptions {
		toReturn[r.C.SecretHash] = &RedemptionV1{
			Contract: &ContractV1{
				SwapContractDetails: &dex.SwapContractDetails{
					From:       r.C.Initiator.String(),
					To:         r.C.Participant.String(),
					Value:      r.C.Value,
					SecretHash: r.C.SecretHash[:],
					LockTime:   r.C.RefundTimestamp,
				},
				Initiator: r.C.Initiator.String(),
			},
			Secret: r.Secret,
		}
	}

	return toReturn, nil
}

func ParseRefundDataV1(calldata []byte) (*ContractV1, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != refundFuncName {
		return nil, fmt.Errorf("expected %v function but got %v", refundFuncName, decoded.Name)
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
	contract, ok := args[0].value.(struct {
		SecretHash      [32]byte       `json:"secretHash"`
		Initiator       common.Address `json:"initiator"`
		RefundTimestamp uint64         `json:"refundTimestamp"`
		Participant     common.Address `json:"participant"`
		Value           uint64         `json:"value"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type [32]byte but got %T", args[0].value)
	}

	return &ContractV1{
		SwapContractDetails: &dex.SwapContractDetails{
			From:       contract.Initiator.String(),
			To:         contract.Participant.String(),
			Value:      contract.Value,
			LockTime:   contract.RefundTimestamp,
			SecretHash: contract.SecretHash[:],
		},
		Initiator: contract.Initiator.String(),
	}, nil
}
