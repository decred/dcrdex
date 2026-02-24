// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"decred.org/dcrdex/dex/networks/eth/contracts/entrypoint"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	InitiateMethodName = "initiate"
	RedeemMethodName   = "redeem"
	RefundMethodName   = "refund"
	RedeemAAMethodName = "redeemAA"
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

// ParseInitiateDataV0 parses the calldata used to call the initiate function of a
// specific version of the swap contract. It returns the list of initiations
// done in the call and errors if the call data does not call initiate with
// expected argument types.
func ParseInitiateDataV0(calldata []byte) (map[[SecretHashSize]byte]*Initiation, error) {
	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != InitiateMethodName {
		return nil, fmt.Errorf("expected %v function but got %v", InitiateMethodName, decoded.Name)
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
		}
	}

	return toReturn, nil
}

// ParseRedeemDataV0 parses the calldata used to call the redeem function of a
// specific version of the swap contract. It returns the list of redemptions
// done in the call and errors if the call data does not call redeem with expected
// argument types.
func ParseRedeemDataV0(calldata []byte) (map[[SecretHashSize]byte]*Redemption, error) {
	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != RedeemMethodName {
		return nil, fmt.Errorf("expected %v function but got %v", RedeemMethodName, decoded.Name)
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

// ParseRefundDataV0 parses the calldata used to call the refund function of a
// specific version of the swap contract. It returns the secret hash and errors
// if the call data does not call refund with expected argument types.
func ParseRefundDataV0(calldata []byte) ([32]byte, error) {
	var secretHash [32]byte

	decoded, err := ParseCallData(calldata, ABIs[0])
	if err != nil {
		return secretHash, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != RefundMethodName {
		return secretHash, fmt.Errorf("expected %v function but got %v", RefundMethodName, decoded.Name)
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

type RedemptionV1 struct {
	Secret   [32]byte
	Contract *SwapVector
}

func ParseInitiateDataV1(calldata []byte) (common.Address, map[[SecretHashSize]byte]*SwapVector, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != InitiateMethodName {
		return common.Address{}, nil, fmt.Errorf("expected %v function but got %v", InitiateMethodName, decoded.Name)
	}
	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by ParseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.
	const numArgs = 2
	if len(args) != numArgs {
		return common.Address{}, nil, fmt.Errorf("expected %v input args but got %v", numArgs, len(args))
	}

	tokenAddr, ok := args[0].value.(common.Address)
	if !ok {
		return common.Address{}, nil, fmt.Errorf("expected first init arg to be an address but was %T", args[0].value)
	}

	initiations, ok := args[1].value.([]struct {
		SecretHash      [32]byte       `json:"secretHash"`
		Value           *big.Int       `json:"value"`
		Initiator       common.Address `json:"initiator"`
		RefundTimestamp uint64         `json:"refundTimestamp"`
		Participant     common.Address `json:"participant"`
	})
	if !ok {
		return common.Address{}, nil, fmt.Errorf("expected second arg of type []swapv1.ETHSwapContract but got %T", args[0].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv1.ETHSwapVector are the same, other than the tags.
	if len(initiations) > 0 {
		_ = swapv1.ETHSwapVector(initiations[0])
	}

	toReturn := make(map[[SecretHashSize]byte]*SwapVector, len(initiations))
	for _, init := range initiations {
		toReturn[init.SecretHash] = &SwapVector{
			From:       init.Initiator,
			To:         init.Participant,
			Value:      init.Value,
			SecretHash: init.SecretHash,
			LockTime:   init.RefundTimestamp,
		}
	}

	return tokenAddr, toReturn, nil
}

func ParseRedeemDataV1(calldata []byte) (common.Address, map[[SecretHashSize]byte]*RedemptionV1, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != RedeemMethodName && decoded.Name != RedeemAAMethodName {
		return common.Address{}, nil, fmt.Errorf("expected %v function but got %v", RedeemMethodName, decoded.Name)
	}

	args := decoded.inputs
	// Any difference in number of args and types than what we expect
	// should be caught by parseCallData, but checking again anyway.
	//
	// TODO: If any of the checks prove redundant, remove them.

	// redeem has 2 args: (token, redemptions)
	// redeemAA has 2 args: (redemptions, opNonce)
	if len(args) != 2 {
		return common.Address{}, nil, fmt.Errorf("expected 2 redeem args but got %v", len(args))
	}

	redemptionsIndex := 0
	var tokenAddr common.Address
	if decoded.Name == RedeemMethodName {
		var ok bool
		tokenAddr, ok = args[0].value.(common.Address)
		if !ok {
			return common.Address{}, nil, fmt.Errorf("expected first redeem arg to be an address but was %T", args[0].value)
		}
		redemptionsIndex = 1
	}

	redemptions, ok := args[redemptionsIndex].value.([]struct {
		V struct {
			SecretHash      [32]uint8      `json:"secretHash"`
			Value           *big.Int       `json:"value"`
			Initiator       common.Address `json:"initiator"`
			RefundTimestamp uint64         `json:"refundTimestamp"`
			Participant     common.Address `json:"participant"`
		} `json:"v"`
		Secret [32]uint8 `json:"secret"`
	})
	if !ok {
		return common.Address{}, nil, fmt.Errorf("expected %d arg of type []swapv1.ETHSwapRedemption but got %T", redemptionsIndex, args[redemptionsIndex].value)
	}

	// This is done for the compiler to ensure that the type defined above and
	// swapv1.ETHSwapVector are the same, other than the tags.
	if len(redemptions) > 0 {
		_ = swapv1.ETHSwapVector(redemptions[0].V)
	}
	toReturn := make(map[[SecretHashSize]byte]*RedemptionV1, len(redemptions))
	for _, r := range redemptions {
		toReturn[r.V.SecretHash] = &RedemptionV1{
			Contract: &SwapVector{
				From:       r.V.Initiator,
				To:         r.V.Participant,
				Value:      r.V.Value,
				SecretHash: r.V.SecretHash,
				LockTime:   r.V.RefundTimestamp,
			},
			Secret: r.Secret,
		}
	}

	return tokenAddr, toReturn, nil
}

func ParseRefundDataV1(calldata []byte) (*SwapVector, error) {
	decoded, err := ParseCallData(calldata, ABIs[1])
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != RefundMethodName {
		return nil, fmt.Errorf("expected %v function but got %v", RefundMethodName, decoded.Name)
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
		Value           *big.Int       `json:"value"`
		Initiator       common.Address `json:"initiator"`
		RefundTimestamp uint64         `json:"refundTimestamp"`
		Participant     common.Address `json:"participant"`
	})
	if !ok {
		return nil, fmt.Errorf("expected first arg of type [32]byte but got %T", args[0].value)
	}

	return &SwapVector{
		From:       contract.Initiator,
		To:         contract.Participant,
		Value:      contract.Value,
		LockTime:   contract.RefundTimestamp,
		SecretHash: contract.SecretHash,
	}, nil
}

// ParseHandleOpsData parses the calldata used to call the handleOps function of a
// version 0.7 entrypoint.
func ParseHandleOpsData(calldata []byte) ([]entrypoint.PackedUserOperation, error) {
	epABI, err := entrypoint.EntrypointMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("unable to get abi: %v", err)
	}

	decoded, err := ParseCallData(calldata, epABI)
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}

	userOps, ok := decoded.inputs[0].value.([]struct {
		Sender             common.Address `json:"sender"`
		Nonce              *big.Int       `json:"nonce"`
		InitCode           []uint8        `json:"initCode"`
		CallData           []uint8        `json:"callData"`
		AccountGasLimits   [32]uint8      `json:"accountGasLimits"`
		PreVerificationGas *big.Int       `json:"preVerificationGas"`
		GasFees            [32]uint8      `json:"gasFees"`
		PaymasterAndData   []uint8        `json:"paymasterAndData"`
		Signature          []uint8        `json:"signature"`
	})
	if !ok {
		return nil, fmt.Errorf("invalid user op type: %T", decoded.inputs[0].value)
	}

	toReturn := make([]entrypoint.PackedUserOperation, len(userOps))
	for i, userOp := range userOps {
		toReturn[i] = entrypoint.PackedUserOperation{
			Sender:             userOp.Sender,
			Nonce:              userOp.Nonce,
			InitCode:           userOp.InitCode,
			CallData:           userOp.CallData,
			AccountGasLimits:   userOp.AccountGasLimits,
			PreVerificationGas: userOp.PreVerificationGas,
			GasFees:            userOp.GasFees,
			PaymasterAndData:   userOp.PaymasterAndData,
		}
	}

	return toReturn, nil
}

// InnerHandleOpData contains the fields extracted from a v0.7 EntryPoint
// innerHandleOp call.
type InnerHandleOpData struct {
	CallData   []byte
	Sender     common.Address
	UserOpHash [32]byte
}

// ParseInnerHandleOpData parses the calldata used to call the innerHandleOp
// function of a v0.7 entrypoint.
func ParseInnerHandleOpData(calldata []byte) (*InnerHandleOpData, error) {
	const innerHandleOpABIJSON = `[{"name":"innerHandleOp","type":"function","inputs":[` +
		`{"name":"callData","type":"bytes"},` +
		`{"name":"opInfo","type":"tuple","components":[` +
		`{"name":"mUserOp","type":"tuple","components":[` +
		`{"name":"sender","type":"address"},` +
		`{"name":"nonce","type":"uint256"},` +
		`{"name":"verificationGasLimit","type":"uint256"},` +
		`{"name":"callGasLimit","type":"uint256"},` +
		`{"name":"paymasterVerificationGasLimit","type":"uint256"},` +
		`{"name":"paymasterPostOpGasLimit","type":"uint256"},` +
		`{"name":"preVerificationGas","type":"uint256"},` +
		`{"name":"paymaster","type":"address"},` +
		`{"name":"maxFeePerGas","type":"uint256"},` +
		`{"name":"maxPriorityFeePerGas","type":"uint256"}]},` +
		`{"name":"userOpHash","type":"bytes32"},` +
		`{"name":"prefund","type":"uint256"},` +
		`{"name":"contextOffset","type":"uint256"},` +
		`{"name":"preOpGas","type":"uint256"}]}` +
		`],"outputs":[{"name":"actualGasCost","type":"uint256"}]}]`

	parsed, err := abi.JSON(strings.NewReader(innerHandleOpABIJSON))
	if err != nil {
		return nil, fmt.Errorf("unable to parse innerHandleOp ABI: %v", err)
	}

	decoded, err := ParseCallData(calldata, &parsed)
	if err != nil {
		return nil, fmt.Errorf("unable to parse call data: %v", err)
	}
	if decoded.Name != "innerHandleOp" {
		return nil, fmt.Errorf("expected innerHandleOp but got %v", decoded.Name)
	}

	if len(decoded.inputs) < 2 {
		return nil, fmt.Errorf("expected at least 2 args but got %v", len(decoded.inputs))
	}

	cd, ok := decoded.inputs[0].value.([]byte)
	if !ok {
		return nil, fmt.Errorf("expected callData as []byte but got %T", decoded.inputs[0].value)
	}

	opInfo, ok := decoded.inputs[1].value.(struct {
		MUserOp struct {
			Sender                        common.Address `json:"sender"`
			Nonce                         *big.Int       `json:"nonce"`
			VerificationGasLimit          *big.Int       `json:"verificationGasLimit"`
			CallGasLimit                  *big.Int       `json:"callGasLimit"`
			PaymasterVerificationGasLimit *big.Int       `json:"paymasterVerificationGasLimit"`
			PaymasterPostOpGasLimit       *big.Int       `json:"paymasterPostOpGasLimit"`
			PreVerificationGas            *big.Int       `json:"preVerificationGas"`
			Paymaster                     common.Address `json:"paymaster"`
			MaxFeePerGas                  *big.Int       `json:"maxFeePerGas"`
			MaxPriorityFeePerGas          *big.Int       `json:"maxPriorityFeePerGas"`
		} `json:"mUserOp"`
		UserOpHash    [32]byte `json:"userOpHash"`
		Prefund       *big.Int `json:"prefund"`
		ContextOffset *big.Int `json:"contextOffset"`
		PreOpGas      *big.Int `json:"preOpGas"`
	})
	if !ok {
		return nil, fmt.Errorf("expected opInfo tuple but got %T", decoded.inputs[1].value)
	}

	return &InnerHandleOpData{
		CallData:   cd,
		Sender:     opInfo.MUserOp.Sender,
		UserOpHash: opInfo.UserOpHash,
	}, nil
}

// HashUserOp hashes a user operation for a version 0.7 entrypoint.
func HashUserOp(op entrypoint.PackedUserOperation, epAddress common.Address, chainID *big.Int) (common.Hash, error) {
	address, _ := abi.NewType("address", "", nil)
	uint256, _ := abi.NewType("uint256", "", nil)
	bytes32, _ := abi.NewType("bytes32", "", nil)

	args := abi.Arguments{
		{Name: "sender", Type: address},
		{Name: "nonce", Type: uint256},
		{Name: "hashInitCode", Type: bytes32},
		{Name: "hashCallData", Type: bytes32},
		{Name: "accountGasLimits", Type: bytes32},
		{Name: "preVerificationGas", Type: uint256},
		{Name: "gasFees", Type: bytes32},
		{Name: "hashPaymasterAndData", Type: bytes32},
	}

	packed, _ := args.Pack(
		op.Sender,
		op.Nonce,
		crypto.Keccak256Hash(op.InitCode),
		crypto.Keccak256Hash(op.CallData),
		op.AccountGasLimits,
		op.PreVerificationGas,
		op.GasFees,
		crypto.Keccak256Hash(op.PaymasterAndData),
	)

	return crypto.Keccak256Hash(
		crypto.Keccak256(packed),
		common.LeftPadBytes(epAddress[:], 32),
		common.LeftPadBytes(chainID.Bytes(), 32),
	), nil
}
