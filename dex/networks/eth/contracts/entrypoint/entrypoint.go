// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package entrypoint

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// EntryPointMemoryUserOp is an auto generated low-level Go binding around an user-defined struct.
type EntryPointMemoryUserOp struct {
	Sender               common.Address
	Nonce                *big.Int
	CallGasLimit         *big.Int
	VerificationGasLimit *big.Int
	PreVerificationGas   *big.Int
	Paymaster            common.Address
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
}

// EntryPointUserOpInfo is an auto generated low-level Go binding around an user-defined struct.
type EntryPointUserOpInfo struct {
	MUserOp       EntryPointMemoryUserOp
	UserOpHash    [32]byte
	Prefund       *big.Int
	ContextOffset *big.Int
	PreOpGas      *big.Int
}

// IEntryPointUserOpsPerAggregator is an auto generated low-level Go binding around an user-defined struct.
type IEntryPointUserOpsPerAggregator struct {
	UserOps    []UserOperation
	Aggregator common.Address
	Signature  []byte
}

// IStakeManagerDepositInfo is an auto generated low-level Go binding around an user-defined struct.
type IStakeManagerDepositInfo struct {
	Deposit         *big.Int
	Staked          bool
	Stake           *big.Int
	UnstakeDelaySec uint32
	WithdrawTime    *big.Int
}

// UserOperation is an auto generated low-level Go binding around an user-defined struct.
type UserOperation struct {
	Sender               common.Address
	Nonce                *big.Int
	InitCode             []byte
	CallData             []byte
	CallGasLimit         *big.Int
	VerificationGasLimit *big.Int
	PreVerificationGas   *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	PaymasterAndData     []byte
	Signature            []byte
}

// EntrypointMetaData contains all meta data concerning the Entrypoint contract.
var EntrypointMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"preOpGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"paid\",\"type\":\"uint256\"},{\"internalType\":\"uint48\",\"name\":\"validAfter\",\"type\":\"uint48\"},{\"internalType\":\"uint48\",\"name\":\"validUntil\",\"type\":\"uint48\"},{\"internalType\":\"bool\",\"name\":\"targetSuccess\",\"type\":\"bool\"},{\"internalType\":\"bytes\",\"name\":\"targetResult\",\"type\":\"bytes\"}],\"name\":\"ExecutionResult\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"opIndex\",\"type\":\"uint256\"},{\"internalType\":\"string\",\"name\":\"reason\",\"type\":\"string\"}],\"name\":\"FailedOp\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"SenderAddressResult\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"aggregator\",\"type\":\"address\"}],\"name\":\"SignatureValidationFailed\",\"type\":\"error\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"preOpGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"prefund\",\"type\":\"uint256\"},{\"internalType\":\"bool\",\"name\":\"sigFailed\",\"type\":\"bool\"},{\"internalType\":\"uint48\",\"name\":\"validAfter\",\"type\":\"uint48\"},{\"internalType\":\"uint48\",\"name\":\"validUntil\",\"type\":\"uint48\"},{\"internalType\":\"bytes\",\"name\":\"paymasterContext\",\"type\":\"bytes\"}],\"internalType\":\"structIEntryPoint.ReturnInfo\",\"name\":\"returnInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"senderInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"factoryInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"paymasterInfo\",\"type\":\"tuple\"}],\"name\":\"ValidationResult\",\"type\":\"error\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"preOpGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"prefund\",\"type\":\"uint256\"},{\"internalType\":\"bool\",\"name\":\"sigFailed\",\"type\":\"bool\"},{\"internalType\":\"uint48\",\"name\":\"validAfter\",\"type\":\"uint48\"},{\"internalType\":\"uint48\",\"name\":\"validUntil\",\"type\":\"uint48\"},{\"internalType\":\"bytes\",\"name\":\"paymasterContext\",\"type\":\"bytes\"}],\"internalType\":\"structIEntryPoint.ReturnInfo\",\"name\":\"returnInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"senderInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"factoryInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"paymasterInfo\",\"type\":\"tuple\"},{\"components\":[{\"internalType\":\"address\",\"name\":\"aggregator\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"stake\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"internalType\":\"structIStakeManager.StakeInfo\",\"name\":\"stakeInfo\",\"type\":\"tuple\"}],\"internalType\":\"structIEntryPoint.AggregatorStakeInfo\",\"name\":\"aggregatorInfo\",\"type\":\"tuple\"}],\"name\":\"ValidationResultWithAggregation\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"factory\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"paymaster\",\"type\":\"address\"}],\"name\":\"AccountDeployed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[],\"name\":\"BeforeExecution\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"totalDeposit\",\"type\":\"uint256\"}],\"name\":\"Deposited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"aggregator\",\"type\":\"address\"}],\"name\":\"SignatureAggregatorChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"totalStaked\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"unstakeDelaySec\",\"type\":\"uint256\"}],\"name\":\"StakeLocked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"withdrawTime\",\"type\":\"uint256\"}],\"name\":\"StakeUnlocked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"withdrawAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"StakeWithdrawn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"paymaster\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"actualGasCost\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"actualGasUsed\",\"type\":\"uint256\"}],\"name\":\"UserOperationEvent\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"revertReason\",\"type\":\"bytes\"}],\"name\":\"UserOperationRevertReason\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"withdrawAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Withdrawn\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"SIG_VALIDATION_FAILED\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"}],\"name\":\"_validateSenderAndPaymaster\",\"outputs\":[],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"unstakeDelaySec\",\"type\":\"uint32\"}],\"name\":\"addStake\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"depositTo\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"deposits\",\"outputs\":[{\"internalType\":\"uint112\",\"name\":\"deposit\",\"type\":\"uint112\"},{\"internalType\":\"bool\",\"name\":\"staked\",\"type\":\"bool\"},{\"internalType\":\"uint112\",\"name\":\"stake\",\"type\":\"uint112\"},{\"internalType\":\"uint32\",\"name\":\"unstakeDelaySec\",\"type\":\"uint32\"},{\"internalType\":\"uint48\",\"name\":\"withdrawTime\",\"type\":\"uint48\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"getDepositInfo\",\"outputs\":[{\"components\":[{\"internalType\":\"uint112\",\"name\":\"deposit\",\"type\":\"uint112\"},{\"internalType\":\"bool\",\"name\":\"staked\",\"type\":\"bool\"},{\"internalType\":\"uint112\",\"name\":\"stake\",\"type\":\"uint112\"},{\"internalType\":\"uint32\",\"name\":\"unstakeDelaySec\",\"type\":\"uint32\"},{\"internalType\":\"uint48\",\"name\":\"withdrawTime\",\"type\":\"uint48\"}],\"internalType\":\"structIStakeManager.DepositInfo\",\"name\":\"info\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint192\",\"name\":\"key\",\"type\":\"uint192\"}],\"name\":\"getNonce\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"}],\"name\":\"getSenderAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"}],\"name\":\"getUserOpHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation[]\",\"name\":\"userOps\",\"type\":\"tuple[]\"},{\"internalType\":\"contractIAggregator\",\"name\":\"aggregator\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structIEntryPoint.UserOpsPerAggregator[]\",\"name\":\"opsPerAggregator\",\"type\":\"tuple[]\"},{\"internalType\":\"addresspayable\",\"name\":\"beneficiary\",\"type\":\"address\"}],\"name\":\"handleAggregatedOps\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation[]\",\"name\":\"ops\",\"type\":\"tuple[]\"},{\"internalType\":\"addresspayable\",\"name\":\"beneficiary\",\"type\":\"address\"}],\"name\":\"handleOps\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint192\",\"name\":\"key\",\"type\":\"uint192\"}],\"name\":\"incrementNonce\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"components\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"paymaster\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"}],\"internalType\":\"structEntryPoint.MemoryUserOp\",\"name\":\"mUserOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"prefund\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"contextOffset\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preOpGas\",\"type\":\"uint256\"}],\"internalType\":\"structEntryPoint.UserOpInfo\",\"name\":\"opInfo\",\"type\":\"tuple\"},{\"internalType\":\"bytes\",\"name\":\"context\",\"type\":\"bytes\"}],\"name\":\"innerHandleOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"actualGasCost\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"uint192\",\"name\":\"\",\"type\":\"uint192\"}],\"name\":\"nonceSequenceNumber\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"op\",\"type\":\"tuple\"},{\"internalType\":\"address\",\"name\":\"target\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"targetCallData\",\"type\":\"bytes\"}],\"name\":\"simulateHandleOp\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"}],\"name\":\"simulateValidation\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"unlockStake\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"withdrawAddress\",\"type\":\"address\"}],\"name\":\"withdrawStake\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"withdrawAddress\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"withdrawAmount\",\"type\":\"uint256\"}],\"name\":\"withdrawTo\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
	Bin: "0x60a0604052604051620000129062000055565b604051809103906000f0801580156200002f573d6000803e3d6000fd5b506001600160a01b03166080523480156200004957600080fd5b50600160025562000063565b6102338062004faa83390190565b608051614f2462000086600039600081816116700152613a2b0152614f246000f3fe6080604052600436106101635760003560e01c80638f41ec5a116100c0578063bb9fe6bf11610074578063d6383f9411610059578063d6383f94146104f5578063ee21942314610515578063fc7e286d1461053557600080fd5b8063bb9fe6bf146104c0578063c23a5cea146104d557600080fd5b80639b249f69116100a55780639b249f691461046d578063a61935311461048d578063b760faf9146104ad57600080fd5b80638f41ec5a14610438578063957122ab1461044d57600080fd5b8063205c2878116101175780634b1d7cf5116100fc5780634b1d7cf5146102765780635287ce121461029657806370a08231146103e557600080fd5b8063205c28781461023657806335567e1a1461025657600080fd5b80631b2e01b8116101485780631b2e01b8146101ab5780631d732756146101f65780631fad948c1461021657600080fd5b80630396cb60146101785780630bd28e3b1461018b57600080fd5b36610173576101713361060f565b005b600080fd5b610171610186366004613db1565b61068a565b34801561019757600080fd5b506101716101a6366004613e04565b610a0d565b3480156101b757600080fd5b506101e36101c6366004613e4c565b600160209081526000928352604080842090915290825290205481565b6040519081526020015b60405180910390f35b34801561020257600080fd5b506101e36102113660046140a0565b610a55565b34801561022257600080fd5b506101716102313660046141ab565b610c08565b34801561024257600080fd5b50610171610251366004614202565b610d85565b34801561026257600080fd5b506101e3610271366004613e4c565b610f87565b34801561028257600080fd5b506101716102913660046141ab565b611002565b3480156102a257600080fd5b506103866102b136600461422e565b6040805160a0810182526000808252602082018190529181018290526060810182905260808101919091525073ffffffffffffffffffffffffffffffffffffffff1660009081526020818152604091829020825160a08101845281546dffffffffffffffffffffffffffff80821683526e010000000000000000000000000000820460ff161515948301949094526f0100000000000000000000000000000090049092169282019290925260019091015463ffffffff81166060830152640100000000900465ffffffffffff16608082015290565b6040805182516dffffffffffffffffffffffffffff908116825260208085015115159083015283830151169181019190915260608083015163ffffffff169082015260809182015165ffffffffffff169181019190915260a0016101ed565b3480156103f157600080fd5b506101e361040036600461422e565b73ffffffffffffffffffffffffffffffffffffffff166000908152602081905260409020546dffffffffffffffffffffffffffff1690565b34801561044457600080fd5b506101e3600181565b34801561045957600080fd5b5061017161046836600461424b565b6114d8565b34801561047957600080fd5b506101716104883660046142d0565b611630565b34801561049957600080fd5b506101e36104a836600461432b565b611737565b6101716104bb36600461422e565b61060f565b3480156104cc57600080fd5b50610171611779565b3480156104e157600080fd5b506101716104f036600461422e565b611930565b34801561050157600080fd5b50610171610510366004614360565b611c30565b34801561052157600080fd5b5061017161053036600461432b565b611d5e565b34801561054157600080fd5b506105c261055036600461422e565b600060208190529081526040902080546001909101546dffffffffffffffffffffffffffff808316926e010000000000000000000000000000810460ff16926f010000000000000000000000000000009091049091169063ffffffff811690640100000000900465ffffffffffff1685565b604080516dffffffffffffffffffffffffffff96871681529415156020860152929094169183019190915263ffffffff16606082015265ffffffffffff909116608082015260a0016101ed565b61061981346120c6565b73ffffffffffffffffffffffffffffffffffffffff811660008181526020818152604091829020805492516dffffffffffffffffffffffffffff909316835292917f2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c491015b60405180910390a25050565b33600090815260208190526040902063ffffffff821661070b576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601a60248201527f6d757374207370656369667920756e7374616b652064656c617900000000000060448201526064015b60405180910390fd5b600181015463ffffffff9081169083161015610783576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601c60248201527f63616e6e6f7420646563726561736520756e7374616b652074696d65000000006044820152606401610702565b80546000906107b69034906f0100000000000000000000000000000090046dffffffffffffffffffffffffffff166143f1565b905060008111610822576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601260248201527f6e6f207374616b652073706563696669656400000000000000000000000000006044820152606401610702565b6dffffffffffffffffffffffffffff81111561089a576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152600e60248201527f7374616b65206f766572666c6f770000000000000000000000000000000000006044820152606401610702565b6040805160a08101825283546dffffffffffffffffffffffffffff90811682526001602080840182815286841685870190815263ffffffff808b16606088019081526000608089018181523380835296829052908a9020985189549551945189166f01000000000000000000000000000000027fffffff0000000000000000000000000000ffffffffffffffffffffffffffffff9515156e010000000000000000000000000000027fffffffffffffffffffffffffffffffffff0000000000000000000000000000009097169190991617949094179290921695909517865551949092018054925165ffffffffffff16640100000000027fffffffffffffffffffffffffffffffffffffffffffff00000000000000000000909316949093169390931717905590517fa5ae833d0bb1dcd632d98a8b70973e8516812898e19bf27b70071ebc8dc52c0190610a00908490879091825263ffffffff16602082015260400190565b60405180910390a2505050565b33600090815260016020908152604080832077ffffffffffffffffffffffffffffffffffffffffffffffff851684529091528120805491610a4d83614404565b919050555050565b6000805a9050333014610ac4576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601760248201527f4141393220696e7465726e616c2063616c6c206f6e6c790000000000000000006044820152606401610702565b8451604081015160608201518101611388015a1015610b07577fdeaddead0000000000000000000000000000000000000000000000000000000060005260206000fd5b875160009015610ba8576000610b24846000015160008c866121c3565b905080610ba6576000610b386108006121db565b805190915015610ba057846000015173ffffffffffffffffffffffffffffffffffffffff168a602001517f1c4fada7374c0a9ee8841fc38afe82932dc0f8e69012e927f061a8bae611a201876020015184604051610b979291906144aa565b60405180910390a35b60019250505b505b600088608001515a8603019050610bfa6000838b8b8b8080601f016020809104026020016040519081016040528093929190818152602001838380828437600092019190915250889250612207915050565b9a9950505050505050505050565b610c106125e5565b8160008167ffffffffffffffff811115610c2c57610c2c613e81565b604051908082528060200260200182016040528015610c6557816020015b610c52613d0d565b815260200190600190039081610c4a5790505b50905060005b82811015610cde576000828281518110610c8757610c876144c3565b60200260200101519050600080610cc2848a8a87818110610caa57610caa6144c3565b9050602002810190610cbc91906144f2565b85612656565b91509150610cd3848383600061289a565b505050600101610c6b565b506040516000907fbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f972908290a160005b83811015610d6857610d5c81888884818110610d2b57610d2b6144c3565b9050602002810190610d3d91906144f2565b858481518110610d4f57610d4f6144c3565b6020026020010151612aef565b90910190600101610d0d565b50610d738482612c74565b505050610d806001600255565b505050565b33600090815260208190526040902080546dffffffffffffffffffffffffffff16821115610e0f576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601960248201527f576974686472617720616d6f756e7420746f6f206c61726765000000000000006044820152606401610702565b8054610e2c9083906dffffffffffffffffffffffffffff16614530565b81547fffffffffffffffffffffffffffffffffffff0000000000000000000000000000166dffffffffffffffffffffffffffff919091161781556040805173ffffffffffffffffffffffffffffffffffffffff851681526020810184905233917fd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb910160405180910390a260008373ffffffffffffffffffffffffffffffffffffffff168360405160006040518083038185875af1925050503d8060008114610f11576040519150601f19603f3d011682016040523d82523d6000602084013e610f16565b606091505b5050905080610f81576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601260248201527f6661696c656420746f20776974686472617700000000000000000000000000006044820152606401610702565b50505050565b73ffffffffffffffffffffffffffffffffffffffff8216600090815260016020908152604080832077ffffffffffffffffffffffffffffffffffffffffffffffff8516845290915290819020549082901b7fffffffffffffffffffffffffffffffffffffffffffffffff000000000000000016175b92915050565b61100a6125e5565b816000805b82811015611203573686868381811061102a5761102a6144c3565b905060200281019061103c9190614543565b905036600061104b8380614577565b90925090506000611062604085016020860161422e565b90507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff73ffffffffffffffffffffffffffffffffffffffff821601611103576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601760248201527f4141393620696e76616c69642061676772656761746f720000000000000000006044820152606401610702565b73ffffffffffffffffffffffffffffffffffffffff8116156111e05773ffffffffffffffffffffffffffffffffffffffff811663e3563a4f848461114a60408901896145df565b6040518563ffffffff1660e01b815260040161116994939291906147f5565b60006040518083038186803b15801561118157600080fd5b505afa925050508015611192575060015b6111e0576040517f86a9f75000000000000000000000000000000000000000000000000000000000815273ffffffffffffffffffffffffffffffffffffffff82166004820152602401610702565b6111ea82876143f1565b95505050505080806111fb90614404565b91505061100f565b5060008167ffffffffffffffff81111561121f5761121f613e81565b60405190808252806020026020018201604052801561125857816020015b611245613d0d565b81526020019060019003908161123d5790505b506040519091507fbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f97290600090a16000805b8481101561136d57368888838181106112a4576112a46144c3565b90506020028101906112b69190614543565b90503660006112c58380614577565b909250905060006112dc604085016020860161422e565b90508160005b818110156113545760008989815181106112fe576112fe6144c3565b602002602001015190506000806113218b898987818110610caa57610caa6144c3565b915091506113318483838961289a565b8a61133b81614404565b9b5050505050808061134c90614404565b9150506112e2565b505050505050808061136590614404565b915050611289565b50600080915060005b858110156114935736898983818110611391576113916144c3565b90506020028101906113a39190614543565b90506113b5604082016020830161422e565b73ffffffffffffffffffffffffffffffffffffffff167f575ff3acadd5ab348fe1855e217e0f3678f8d767d7494c9f9fefbee2e17cca4d60405160405180910390a23660006114048380614577565b90925090508060005b8181101561147b5761144f8885858481811061142b5761142b6144c3565b905060200281019061143d91906144f2565b8b8b81518110610d4f57610d4f6144c3565b61145990886143f1565b96508761146581614404565b985050808061147390614404565b91505061140d565b5050505050808061148b90614404565b915050611376565b506040516000907f575ff3acadd5ab348fe1855e217e0f3678f8d767d7494c9f9fefbee2e17cca4d908290a26114c98682612c74565b5050505050610d806001600255565b831580156114fb575073ffffffffffffffffffffffffffffffffffffffff83163b155b15611562576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601960248201527f41413230206163636f756e74206e6f74206465706c6f796564000000000000006044820152606401610702565b601481106115f457600061157960148284866148ac565b611582916148d6565b60601c9050803b6000036115f2576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601b60248201527f41413330207061796d6173746572206e6f74206465706c6f79656400000000006044820152606401610702565b505b6040517f08c379a00000000000000000000000000000000000000000000000000000000081526020600482015260006024820152604401610702565b6040517f570e1a3600000000000000000000000000000000000000000000000000000000815260009073ffffffffffffffffffffffffffffffffffffffff7f0000000000000000000000000000000000000000000000000000000000000000169063570e1a36906116a7908690869060040161491e565b6020604051808303816000875af11580156116c6573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906116ea9190614932565b6040517f6ca7b80600000000000000000000000000000000000000000000000000000000815273ffffffffffffffffffffffffffffffffffffffff82166004820152909150602401610702565b600061174282612dbb565b6040805160208101929092523090820152466060820152608001604051602081830303815290604052805190602001209050919050565b3360009081526020819052604081206001810154909163ffffffff90911690036117ff576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152600a60248201527f6e6f74207374616b6564000000000000000000000000000000000000000000006044820152606401610702565b80546e010000000000000000000000000000900460ff1661187c576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601160248201527f616c726561647920756e7374616b696e670000000000000000000000000000006044820152606401610702565b60018101546000906118949063ffffffff164261494f565b6001830180547fffffffffffffffffffffffffffffffffffffffffffff000000000000ffffffff1664010000000065ffffffffffff84169081029190911790915583547fffffffffffffffffffffffffffffffffff00ffffffffffffffffffffffffffff16845560405190815290915033907ffa9b3c14cc825c412c9ed81b3ba365a5b459439403f18829e572ed53a4180f0a9060200161067e565b33600090815260208190526040902080546f0100000000000000000000000000000090046dffffffffffffffffffffffffffff16806119cb576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601460248201527f4e6f207374616b6520746f2077697468647261770000000000000000000000006044820152606401610702565b6001820154640100000000900465ffffffffffff16611a46576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601d60248201527f6d7573742063616c6c20756e6c6f636b5374616b6528292066697273740000006044820152606401610702565b60018201544264010000000090910465ffffffffffff161115611ac5576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601b60248201527f5374616b65207769746864726177616c206973206e6f742064756500000000006044820152606401610702565b6001820180547fffffffffffffffffffffffffffffffffffffffffffff0000000000000000000016905581547fffffff0000000000000000000000000000ffffffffffffffffffffffffffffff1682556040805173ffffffffffffffffffffffffffffffffffffffff851681526020810183905233917fb7c918e0e249f999e965cafeb6c664271b3f4317d296461500e71da39f0cbda3910160405180910390a260008373ffffffffffffffffffffffffffffffffffffffff168260405160006040518083038185875af1925050503d8060008114611bc0576040519150601f19603f3d011682016040523d82523d6000602084013e611bc5565b606091505b5050905080610f81576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601860248201527f6661696c656420746f207769746864726177207374616b6500000000000000006044820152606401610702565b611c38613d0d565b611c4185612dd4565b600080611c5060008885612656565b915091506000611c608383612ec7565b9050611c6b43600052565b6000611c7960008a87612aef565b9050611c8443600052565b6000606073ffffffffffffffffffffffffffffffffffffffff8a1615611d14578973ffffffffffffffffffffffffffffffffffffffff168989604051611ccb929190614975565b6000604051808303816000865af19150503d8060008114611d08576040519150601f19603f3d011682016040523d82523d6000602084013e611d0d565b606091505b5090925090505b8660800151838560200151866040015185856040517f8b7ac98000000000000000000000000000000000000000000000000000000000815260040161070296959493929190614985565b611d66613d0d565b611d6f82612dd4565b600080611d7e60008585612656565b845160a001516040805180820182526000808252602080830182815273ffffffffffffffffffffffffffffffffffffffff958616835282825284832080546dffffffffffffffffffffffffffff6f01000000000000000000000000000000918290048116875260019283015463ffffffff9081169094528d51518851808a018a5287815280870188815291909a16875286865288872080549390930490911689529101549091169052835180850190945281845283015293955091935090366000611e4c60408a018a6145df565b909250905060006014821015611e63576000611e7e565b611e716014600084866148ac565b611e7a916148d6565b60601c5b6040805180820182526000808252602080830182815273ffffffffffffffffffffffffffffffffffffffff861683529082905292902080546f0100000000000000000000000000000090046dffffffffffffffffffffffffffff1682526001015463ffffffff1690915290915093505050506000611efc8686612ec7565b90506000816000015190506000600173ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff1614905060006040518060c001604052808b6080015181526020018b6040015181526020018315158152602001856020015165ffffffffffff168152602001856040015165ffffffffffff168152602001611f938c6060015190565b9052905073ffffffffffffffffffffffffffffffffffffffff831615801590611fd3575073ffffffffffffffffffffffffffffffffffffffff8316600114155b1561208c5760408051808201825273ffffffffffffffffffffffffffffffffffffffff851680825282518084018452600080825260208083018281529382528181529085902080546f0100000000000000000000000000000090046dffffffffffffffffffffffffffff1683526001015463ffffffff169092529082015290517ffaecb4e4000000000000000000000000000000000000000000000000000000008152610702908390899089908c908690600401614a27565b808686896040517fe0cff05f0000000000000000000000000000000000000000000000000000000081526004016107029493929190614ab4565b73ffffffffffffffffffffffffffffffffffffffff82166000908152602081905260408120805490919061210b9084906dffffffffffffffffffffffffffff166143f1565b90506dffffffffffffffffffffffffffff811115612185576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601060248201527f6465706f736974206f766572666c6f77000000000000000000000000000000006044820152606401610702565b81547fffffffffffffffffffffffffffffffffffff0000000000000000000000000000166dffffffffffffffffffffffffffff919091161790555050565b6000806000845160208601878987f195945050505050565b60603d828111156121e95750815b604051602082018101604052818152816000602083013e9392505050565b6000805a85519091506000908161221d82612fad565b60a083015190915073ffffffffffffffffffffffffffffffffffffffff81166122495782519350612497565b80935060008851111561249757868202955060028a600281111561226f5761226f614b0b565b146123075760608301516040517fa9a2340900000000000000000000000000000000000000000000000000000000815273ffffffffffffffffffffffffffffffffffffffff83169163a9a23409916122cf908e908d908c90600401614b3a565b600060405180830381600088803b1580156122e957600080fd5b5087f11580156122fd573d6000803e3d6000fd5b5050505050612497565b60608301516040517fa9a2340900000000000000000000000000000000000000000000000000000000815273ffffffffffffffffffffffffffffffffffffffff83169163a9a2340991612362908e908d908c90600401614b3a565b600060405180830381600088803b15801561237c57600080fd5b5087f19350505050801561238e575060015b6124975761239a614b9a565b806308c379a00361242a57506123ae614bb6565b806123b9575061242c565b8b816040516020016123cb9190614c5e565b604080517fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe0818403018152908290527f220266b600000000000000000000000000000000000000000000000000000000825261070292916004016144aa565b505b8a6040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526012908201527f4141353020706f73744f70207265766572740000000000000000000000000000606082015260800190565b5a85038701965081870295508589604001511015612519578a6040517f220266b600000000000000000000000000000000000000000000000000000000815260040161070291815260406020808301829052908201527f414135312070726566756e642062656c6f772061637475616c476173436f7374606082015260800190565b604089015186900361252b85826120c6565b6000808c600281111561254057612540614b0b565b1490508460a0015173ffffffffffffffffffffffffffffffffffffffff16856000015173ffffffffffffffffffffffffffffffffffffffff168c602001517f49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f8860200151858d8f6040516125cd949392919093845291151560208401526040830152606082015260800190565b60405180910390a45050505050505095945050505050565b6002805403612650576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601f60248201527f5265656e7472616e637947756172643a207265656e7472616e742063616c6c006044820152606401610702565b60028055565b60008060005a845190915061266b8682612fdd565b61267486611737565b6020860152604081015160608201516080830151171760e087013517610100870135176effffffffffffffffffffffffffffff811115612710576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601860248201527f41413934206761732076616c756573206f766572666c6f7700000000000000006044820152606401610702565b60008061271c846130fd565b905061272a8a8a8a84613157565b85516020870151919950919350612741919061346d565b6127b057896040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052601a908201527f4141323520696e76616c6964206163636f756e74206e6f6e6365000000000000606082015260800190565b6127b943600052565b60a084015160609073ffffffffffffffffffffffffffffffffffffffff16156127ee576127e98b8b8b85876134c8565b975090505b60005a87039050808b60a00135101561286c578b6040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052601e908201527f41413430206f76657220766572696669636174696f6e4761734c696d69740000606082015260800190565b60408a018390528160608b015260c08b01355a8803018a608001818152505050505050505050935093915050565b6000806128a68561378b565b915091508173ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff161461294857856040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526014908201527f41413234207369676e6174757265206572726f72000000000000000000000000606082015260800190565b80156129b957856040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526017908201527f414132322065787069726564206f72206e6f7420647565000000000000000000606082015260800190565b60006129c48561378b565b9250905073ffffffffffffffffffffffffffffffffffffffff811615612a4f57866040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526014908201527f41413334207369676e6174757265206572726f72000000000000000000000000606082015260800190565b8115612ae657866040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526021908201527f41413332207061796d61737465722065787069726564206f72206e6f7420647560608201527f6500000000000000000000000000000000000000000000000000000000000000608082015260a00190565b50505050505050565b6000805a90506000612b02846060015190565b905030631d732756612b1760608801886145df565b87856040518563ffffffff1660e01b8152600401612b389493929190614ca3565b6020604051808303816000875af1925050508015612b91575060408051601f3d9081017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe0168201909252612b8e91810190614d64565b60015b612c6857600060206000803e506000517f21522153000000000000000000000000000000000000000000000000000000008101612c3357866040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052600f908201527f41413935206f7574206f66206761730000000000000000000000000000000000606082015260800190565b600085608001515a612c459086614530565b612c4f91906143f1565b9050612c5f886002888685612207565b94505050612c6b565b92505b50509392505050565b73ffffffffffffffffffffffffffffffffffffffff8216612cf1576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601860248201527f4141393020696e76616c69642062656e656669636961727900000000000000006044820152606401610702565b60008273ffffffffffffffffffffffffffffffffffffffff168260405160006040518083038185875af1925050503d8060008114612d4b576040519150601f19603f3d011682016040523d82523d6000602084013e612d50565b606091505b5050905080610d80576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601f60248201527f41413931206661696c65642073656e6420746f2062656e6566696369617279006044820152606401610702565b6000612dc6826137de565b805190602001209050919050565b3063957122ab612de760408401846145df565b612df4602086018661422e565b612e026101208701876145df565b6040518663ffffffff1660e01b8152600401612e22959493929190614d7d565b60006040518083038186803b158015612e3a57600080fd5b505afa925050508015612e4b575060015b612ec457612e57614b9a565b806308c379a003612eb85750612e6b614bb6565b80612e765750612eba565b805115612eb4576000816040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029291906144aa565b5050565b505b3d6000803e3d6000fd5b50565b6040805160608101825260008082526020820181905291810182905290612eed846138be565b90506000612efa846138be565b825190915073ffffffffffffffffffffffffffffffffffffffff8116612f1e575080515b602080840151604080860151928501519085015191929165ffffffffffff8083169085161015612f4c578193505b8065ffffffffffff168365ffffffffffff161115612f68578092505b50506040805160608101825273ffffffffffffffffffffffffffffffffffffffff909416845265ffffffffffff92831660208501529116908201529250505092915050565b60c081015160e082015160009190808203612fc9575092915050565b612fd58248830161393c565b949350505050565b612fea602083018361422e565b73ffffffffffffffffffffffffffffffffffffffff16815260208083013590820152608080830135604083015260a0830135606083015260c0808401359183019190915260e08084013591830191909152610100830135908201523660006130566101208501856145df565b909250905080156130f05760148110156130cc576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601d60248201527f4141393320696e76616c6964207061796d6173746572416e64446174610000006044820152606401610702565b6130da6014600083856148ac565b6130e3916148d6565b60601c60a0840152610f81565b600060a084015250505050565b60a0810151600090819073ffffffffffffffffffffffffffffffffffffffff1661312857600161312b565b60035b60ff16905060008360800151828560600151028560400151010190508360c00151810292505050919050565b60008060005a855180519192509061317c898861317760408c018c6145df565b613954565b60a082015161318a43600052565b600073ffffffffffffffffffffffffffffffffffffffff82166131f35773ffffffffffffffffffffffffffffffffffffffff83166000908152602081905260409020546dffffffffffffffffffffffffffff168881116131ec578089036131ef565b60005b9150505b606084015160208a01516040517f3a871cdd00000000000000000000000000000000000000000000000000000000815273ffffffffffffffffffffffffffffffffffffffff861692633a871cdd929091613253918f918790600401614dc0565b60206040518083038160008887f1935050505080156132ad575060408051601f3d9081017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe01682019092526132aa91810190614d64565b60015b613357576132b9614b9a565b806308c379a0036132ea57506132cd614bb6565b806132d857506132ec565b8b816040516020016123cb9190614de5565b505b8a6040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526016908201527f4141323320726576657274656420286f72204f4f472900000000000000000000606082015260800190565b955073ffffffffffffffffffffffffffffffffffffffff821661345a5773ffffffffffffffffffffffffffffffffffffffff8316600090815260208190526040902080546dffffffffffffffffffffffffffff16808a111561341e578c6040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526017908201527f41413231206469646e2774207061792070726566756e64000000000000000000606082015260800190565b81547fffffffffffffffffffffffffffffffffffff000000000000000000000000000016908a90036dffffffffffffffffffffffffffff161790555b5a85039650505050505094509492505050565b73ffffffffffffffffffffffffffffffffffffffff8216600090815260016020908152604080832084821c808552925282208054849167ffffffffffffffff83169190856134ba83614404565b909155501495945050505050565b8251606081810151909160009184811161353e576040517f08c379a000000000000000000000000000000000000000000000000000000000815260206004820152601f60248201527f4141343120746f6f206c6974746c6520766572696669636174696f6e476173006044820152606401610702565b60a082015173ffffffffffffffffffffffffffffffffffffffff8116600090815260208190526040902080548784039291906dffffffffffffffffffffffffffff16898110156135f3578c6040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052601e908201527f41413331207061796d6173746572206465706f73697420746f6f206c6f770000606082015260800190565b8981038260000160006101000a8154816dffffffffffffffffffffffffffff02191690836dffffffffffffffffffffffffffff1602179055508273ffffffffffffffffffffffffffffffffffffffff1663f465c77e858e8e602001518e6040518563ffffffff1660e01b815260040161366e93929190614dc0565b60006040518083038160008887f1935050505080156136cd57506040513d6000823e601f3d9081017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe01682016040526136ca9190810190614e2a565b60015b613777576136d9614b9a565b806308c379a00361370a57506136ed614bb6565b806136f8575061370c565b8d816040516020016123cb9190614eb6565b505b8c6040517f220266b60000000000000000000000000000000000000000000000000000000081526004016107029181526040602082018190526016908201527f4141333320726576657274656420286f72204f4f472900000000000000000000606082015260800190565b909e909d509b505050505050505050505050565b600080826000036137a157506000928392509050565b60006137ac846138be565b9050806040015165ffffffffffff164211806137d35750806020015165ffffffffffff1642105b905194909350915050565b60608135602083013560006137fe6137f960408701876145df565b613cfa565b905060006138126137f960608801886145df565b9050608086013560a087013560c088013560e08901356101008a013560006138416137f96101208e018e6145df565b6040805173ffffffffffffffffffffffffffffffffffffffff9c909c1660208d01528b81019a909a5260608b019890985250608089019590955260a088019390935260c087019190915260e08601526101008501526101208401526101408084019190915281518084039091018152610160909201905292915050565b60408051606081018252600080825260208201819052918101919091528160a081901c65ffffffffffff81166000036138fa575065ffffffffffff5b6040805160608101825273ffffffffffffffffffffffffffffffffffffffff909316835260d09490941c602083015265ffffffffffff16928101929092525090565b600081831061394b578161394d565b825b9392505050565b8015610f815782515173ffffffffffffffffffffffffffffffffffffffff81163b156139e557846040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052601f908201527f414131302073656e64657220616c726561647920636f6e737472756374656400606082015260800190565b8351606001516040517f570e1a3600000000000000000000000000000000000000000000000000000000815260009173ffffffffffffffffffffffffffffffffffffffff7f0000000000000000000000000000000000000000000000000000000000000000169163570e1a369190613a63908890889060040161491e565b60206040518083038160008887f1158015613a82573d6000803e3d6000fd5b50505050506040513d601f19601f82011682018060405250810190613aa79190614932565b905073ffffffffffffffffffffffffffffffffffffffff8116613b2f57856040517f220266b6000000000000000000000000000000000000000000000000000000008152600401610702918152604060208201819052601b908201527f4141313320696e6974436f6465206661696c6564206f72204f4f470000000000606082015260800190565b8173ffffffffffffffffffffffffffffffffffffffff168173ffffffffffffffffffffffffffffffffffffffff1614613bcc57856040517f220266b600000000000000000000000000000000000000000000000000000000815260040161070291815260406020808301829052908201527f4141313420696e6974436f6465206d7573742072657475726e2073656e646572606082015260800190565b8073ffffffffffffffffffffffffffffffffffffffff163b600003613c5557856040517f220266b600000000000000000000000000000000000000000000000000000000815260040161070291815260406020808301829052908201527f4141313520696e6974436f6465206d757374206372656174652073656e646572606082015260800190565b6000613c6460148286886148ac565b613c6d916148d6565b60601c90508273ffffffffffffffffffffffffffffffffffffffff1686602001517fd51a9c61267aa6196961883ecf5ff2da6619c37dac0fa92122513fb32c032d2d83896000015160a00151604051613ce992919073ffffffffffffffffffffffffffffffffffffffff92831681529116602082015260400190565b60405180910390a350505050505050565b6000604051828085833790209392505050565b6040518060a00160405280613d8c604051806101000160405280600073ffffffffffffffffffffffffffffffffffffffff16815260200160008152602001600081526020016000815260200160008152602001600073ffffffffffffffffffffffffffffffffffffffff16815260200160008152602001600081525090565b8152602001600080191681526020016000815260200160008152602001600081525090565b600060208284031215613dc357600080fd5b813563ffffffff8116811461394d57600080fd5b803577ffffffffffffffffffffffffffffffffffffffffffffffff81168114613dff57600080fd5b919050565b600060208284031215613e1657600080fd5b61394d82613dd7565b73ffffffffffffffffffffffffffffffffffffffff81168114612ec457600080fd5b8035613dff81613e1f565b60008060408385031215613e5f57600080fd5b8235613e6a81613e1f565b9150613e7860208401613dd7565b90509250929050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b60a0810181811067ffffffffffffffff82111715613ed057613ed0613e81565b60405250565b610100810181811067ffffffffffffffff82111715613ed057613ed0613e81565b7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe0601f830116810181811067ffffffffffffffff82111715613f3b57613f3b613e81565b6040525050565b600067ffffffffffffffff821115613f5c57613f5c613e81565b50601f017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe01660200190565b6000818303610180811215613f9c57600080fd5b604051613fa881613eb0565b80925061010080831215613fbb57600080fd5b6040519250613fc983613ed6565b613fd285613e41565b83526020850135602084015260408501356040840152606085013560608401526080850135608084015261400860a08601613e41565b60a084015260c085013560c084015260e085013560e084015282825280850135602083015250610120840135604082015261014084013560608201526101608401356080820152505092915050565b60008083601f84011261406957600080fd5b50813567ffffffffffffffff81111561408157600080fd5b60208301915083602082850101111561409957600080fd5b9250929050565b6000806000806101c085870312156140b757600080fd5b843567ffffffffffffffff808211156140cf57600080fd5b818701915087601f8301126140e357600080fd5b81356140ee81613f42565b6040516140fb8282613ef7565b8281528a602084870101111561411057600080fd5b826020860160208301376000602084830101528098505050506141368860208901613f88565b94506101a087013591508082111561414d57600080fd5b5061415a87828801614057565b95989497509550505050565b60008083601f84011261417857600080fd5b50813567ffffffffffffffff81111561419057600080fd5b6020830191508360208260051b850101111561409957600080fd5b6000806000604084860312156141c057600080fd5b833567ffffffffffffffff8111156141d757600080fd5b6141e386828701614166565b90945092505060208401356141f781613e1f565b809150509250925092565b6000806040838503121561421557600080fd5b823561422081613e1f565b946020939093013593505050565b60006020828403121561424057600080fd5b813561394d81613e1f565b60008060008060006060868803121561426357600080fd5b853567ffffffffffffffff8082111561427b57600080fd5b61428789838a01614057565b90975095506020880135915061429c82613e1f565b909350604087013590808211156142b257600080fd5b506142bf88828901614057565b969995985093965092949392505050565b600080602083850312156142e357600080fd5b823567ffffffffffffffff8111156142fa57600080fd5b61430685828601614057565b90969095509350505050565b6000610160828403121561432557600080fd5b50919050565b60006020828403121561433d57600080fd5b813567ffffffffffffffff81111561435457600080fd5b612fd584828501614312565b6000806000806060858703121561437657600080fd5b843567ffffffffffffffff8082111561438e57600080fd5b61439a88838901614312565b9550602087013591506143ac82613e1f565b9093506040860135908082111561414d57600080fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b80820180821115610ffc57610ffc6143c2565b60007fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff8203614435576144356143c2565b5060010190565b60005b8381101561445757818101518382015260200161443f565b50506000910152565b6000815180845261447881602086016020860161443c565b601f017fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe0169290920160200192915050565b828152604060208201526000612fd56040830184614460565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b600082357ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffea183360301811261452657600080fd5b9190910192915050565b81810381811115610ffc57610ffc6143c2565b600082357fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffa183360301811261452657600080fd5b60008083357fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe18436030181126145ac57600080fd5b83018035915067ffffffffffffffff8211156145c757600080fd5b6020019150600581901b360382131561409957600080fd5b60008083357fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe184360301811261461457600080fd5b83018035915067ffffffffffffffff82111561462f57600080fd5b60200191503681900382131561409957600080fd5b60008083357fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe184360301811261467957600080fd5b830160208101925035905067ffffffffffffffff81111561469957600080fd5b80360382131561409957600080fd5b8183528181602085013750600060208284010152600060207fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe0601f840116840101905092915050565b600061016061471d8461470385613e41565b73ffffffffffffffffffffffffffffffffffffffff169052565b602083013560208501526147346040840184614644565b82604087015261474783870182846146a8565b925050506147586060840184614644565b858303606087015261476b8382846146a8565b925050506080830135608085015260a083013560a085015260c083013560c085015260e083013560e08501526101008084013581860152506101206147b281850185614644565b868403838801526147c48482846146a8565b93505050506101406147d881850185614644565b868403838801526147ea8482846146a8565b979650505050505050565b6040808252810184905260006060600586901b830181019083018783805b89811015614895577fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffa087860301845282357ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffea18c3603018112614873578283fd5b61487f868d83016146f1565b9550506020938401939290920191600101614813565b5050505082810360208401526147ea8185876146a8565b600080858511156148bc57600080fd5b838611156148c957600080fd5b5050820193919092039150565b7fffffffffffffffffffffffffffffffffffffffff00000000000000000000000081358181169160148510156149165780818660140360031b1b83161692505b505092915050565b602081526000612fd56020830184866146a8565b60006020828403121561494457600080fd5b815161394d81613e1f565b65ffffffffffff81811683821601908082111561496e5761496e6143c2565b5092915050565b8183823760009101908152919050565b868152856020820152600065ffffffffffff8087166040840152808616606084015250831515608083015260c060a08301526149c460c0830184614460565b98975050505050505050565b80518252602081015160208301526040810151151560408301526000606082015165ffffffffffff8082166060860152806080850151166080860152505060a082015160c060a0850152612fd560c0850182614460565b6000610140808352614a3b818401896149d0565b915050614a55602083018780518252602090810151910152565b845160608301526020948501516080830152835160a08301529284015160c0820152815173ffffffffffffffffffffffffffffffffffffffff1660e0820152908301518051610100830152909201516101209092019190915292915050565b60e081526000614ac760e08301876149d0565b9050614ae0602083018680518252602090810151910152565b8351606083015260208401516080830152825160a0830152602083015160c083015295945050505050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b600060038510614b73577f4e487b7100000000000000000000000000000000000000000000000000000000600052602160045260246000fd5b84825260606020830152614b8a6060830185614460565b9050826040830152949350505050565b600060033d1115614bb35760046000803e5060005160e01c5b90565b600060443d1015614bc45790565b6040517ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffc803d016004833e81513d67ffffffffffffffff8160248401118184111715614c1257505050505090565b8285019150815181811115614c2a5750505050505090565b843d8701016020828501011115614c445750505050505090565b614c5360208286010187613ef7565b509095945050505050565b7f4141353020706f73744f702072657665727465643a2000000000000000000000815260008251614c9681601685016020870161443c565b9190910160160192915050565b60006101c0808352614cb881840187896146a8565b9050845173ffffffffffffffffffffffffffffffffffffffff808251166020860152602082015160408601526040820151606086015260608201516080860152608082015160a08601528060a08301511660c08601525060c081015160e085015260e08101516101008501525060208501516101208401526040850151610140840152606085015161016084015260808501516101808401528281036101a08401526147ea8185614460565b600060208284031215614d7657600080fd5b5051919050565b606081526000614d916060830187896146a8565b73ffffffffffffffffffffffffffffffffffffffff8616602084015282810360408401526149c48185876146a8565b606081526000614dd360608301866146f1565b60208301949094525060400152919050565b7f414132332072657665727465643a200000000000000000000000000000000000815260008251614e1d81600f85016020870161443c565b91909101600f0192915050565b60008060408385031215614e3d57600080fd5b825167ffffffffffffffff811115614e5457600080fd5b8301601f81018513614e6557600080fd5b8051614e7081613f42565b604051614e7d8282613ef7565b828152876020848601011115614e9257600080fd5b614ea383602083016020870161443c565b6020969096015195979596505050505050565b7f414133332072657665727465643a200000000000000000000000000000000000815260008251614e1d81600f85016020870161443c56fea2646970667358221220c51c2a99e01b757fb8d83662dd9bd20a0c030b0e39db68dfcdce6a3881e7906964736f6c63430008120033608060405234801561001057600080fd5b50610213806100206000396000f3fe608060405234801561001057600080fd5b506004361061002b5760003560e01c8063570e1a3614610030575b600080fd5b61004361003e3660046100f9565b61006c565b60405173ffffffffffffffffffffffffffffffffffffffff909116815260200160405180910390f35b60008061007c601482858761016b565b61008591610195565b60601c90506000610099846014818861016b565b8080601f016020809104026020016040519081016040528093929190818152602001838380828437600092018290525084519495509360209350849250905082850182875af190506000519350806100f057600093505b50505092915050565b6000806020838503121561010c57600080fd5b823567ffffffffffffffff8082111561012457600080fd5b818501915085601f83011261013857600080fd5b81358181111561014757600080fd5b86602082850101111561015957600080fd5b60209290920196919550909350505050565b6000808585111561017b57600080fd5b8386111561018857600080fd5b5050820193919092039150565b7fffffffffffffffffffffffffffffffffffffffff00000000000000000000000081358181169160148510156101d55780818660140360031b1b83161692505b50509291505056fea26469706673582212207cb38f0e509618a7c5e854917a982995ec8685b1c5a038ff370747c3d56570a764736f6c63430008120033",
}

// EntrypointABI is the input ABI used to generate the binding from.
// Deprecated: Use EntrypointMetaData.ABI instead.
var EntrypointABI = EntrypointMetaData.ABI

// EntrypointBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use EntrypointMetaData.Bin instead.
var EntrypointBin = EntrypointMetaData.Bin

// DeployEntrypoint deploys a new Ethereum contract, binding an instance of Entrypoint to it.
func DeployEntrypoint(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *Entrypoint, error) {
	parsed, err := EntrypointMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(EntrypointBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &Entrypoint{EntrypointCaller: EntrypointCaller{contract: contract}, EntrypointTransactor: EntrypointTransactor{contract: contract}, EntrypointFilterer: EntrypointFilterer{contract: contract}}, nil
}

// Entrypoint is an auto generated Go binding around an Ethereum contract.
type Entrypoint struct {
	EntrypointCaller     // Read-only binding to the contract
	EntrypointTransactor // Write-only binding to the contract
	EntrypointFilterer   // Log filterer for contract events
}

// EntrypointCaller is an auto generated read-only Go binding around an Ethereum contract.
type EntrypointCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EntrypointTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EntrypointTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EntrypointFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EntrypointFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EntrypointSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EntrypointSession struct {
	Contract     *Entrypoint       // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EntrypointCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EntrypointCallerSession struct {
	Contract *EntrypointCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts     // Call options to use throughout this session
}

// EntrypointTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EntrypointTransactorSession struct {
	Contract     *EntrypointTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// EntrypointRaw is an auto generated low-level Go binding around an Ethereum contract.
type EntrypointRaw struct {
	Contract *Entrypoint // Generic contract binding to access the raw methods on
}

// EntrypointCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EntrypointCallerRaw struct {
	Contract *EntrypointCaller // Generic read-only contract binding to access the raw methods on
}

// EntrypointTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EntrypointTransactorRaw struct {
	Contract *EntrypointTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEntrypoint creates a new instance of Entrypoint, bound to a specific deployed contract.
func NewEntrypoint(address common.Address, backend bind.ContractBackend) (*Entrypoint, error) {
	contract, err := bindEntrypoint(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Entrypoint{EntrypointCaller: EntrypointCaller{contract: contract}, EntrypointTransactor: EntrypointTransactor{contract: contract}, EntrypointFilterer: EntrypointFilterer{contract: contract}}, nil
}

// NewEntrypointCaller creates a new read-only instance of Entrypoint, bound to a specific deployed contract.
func NewEntrypointCaller(address common.Address, caller bind.ContractCaller) (*EntrypointCaller, error) {
	contract, err := bindEntrypoint(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EntrypointCaller{contract: contract}, nil
}

// NewEntrypointTransactor creates a new write-only instance of Entrypoint, bound to a specific deployed contract.
func NewEntrypointTransactor(address common.Address, transactor bind.ContractTransactor) (*EntrypointTransactor, error) {
	contract, err := bindEntrypoint(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EntrypointTransactor{contract: contract}, nil
}

// NewEntrypointFilterer creates a new log filterer instance of Entrypoint, bound to a specific deployed contract.
func NewEntrypointFilterer(address common.Address, filterer bind.ContractFilterer) (*EntrypointFilterer, error) {
	contract, err := bindEntrypoint(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EntrypointFilterer{contract: contract}, nil
}

// bindEntrypoint binds a generic wrapper to an already deployed contract.
func bindEntrypoint(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EntrypointMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Entrypoint *EntrypointRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Entrypoint.Contract.EntrypointCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Entrypoint *EntrypointRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Entrypoint.Contract.EntrypointTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Entrypoint *EntrypointRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Entrypoint.Contract.EntrypointTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Entrypoint *EntrypointCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Entrypoint.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Entrypoint *EntrypointTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Entrypoint.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Entrypoint *EntrypointTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Entrypoint.Contract.contract.Transact(opts, method, params...)
}

// SIGVALIDATIONFAILED is a free data retrieval call binding the contract method 0x8f41ec5a.
//
// Solidity: function SIG_VALIDATION_FAILED() view returns(uint256)
func (_Entrypoint *EntrypointCaller) SIGVALIDATIONFAILED(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "SIG_VALIDATION_FAILED")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// SIGVALIDATIONFAILED is a free data retrieval call binding the contract method 0x8f41ec5a.
//
// Solidity: function SIG_VALIDATION_FAILED() view returns(uint256)
func (_Entrypoint *EntrypointSession) SIGVALIDATIONFAILED() (*big.Int, error) {
	return _Entrypoint.Contract.SIGVALIDATIONFAILED(&_Entrypoint.CallOpts)
}

// SIGVALIDATIONFAILED is a free data retrieval call binding the contract method 0x8f41ec5a.
//
// Solidity: function SIG_VALIDATION_FAILED() view returns(uint256)
func (_Entrypoint *EntrypointCallerSession) SIGVALIDATIONFAILED() (*big.Int, error) {
	return _Entrypoint.Contract.SIGVALIDATIONFAILED(&_Entrypoint.CallOpts)
}

// ValidateSenderAndPaymaster is a free data retrieval call binding the contract method 0x957122ab.
//
// Solidity: function _validateSenderAndPaymaster(bytes initCode, address sender, bytes paymasterAndData) view returns()
func (_Entrypoint *EntrypointCaller) ValidateSenderAndPaymaster(opts *bind.CallOpts, initCode []byte, sender common.Address, paymasterAndData []byte) error {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "_validateSenderAndPaymaster", initCode, sender, paymasterAndData)

	if err != nil {
		return err
	}

	return err

}

// ValidateSenderAndPaymaster is a free data retrieval call binding the contract method 0x957122ab.
//
// Solidity: function _validateSenderAndPaymaster(bytes initCode, address sender, bytes paymasterAndData) view returns()
func (_Entrypoint *EntrypointSession) ValidateSenderAndPaymaster(initCode []byte, sender common.Address, paymasterAndData []byte) error {
	return _Entrypoint.Contract.ValidateSenderAndPaymaster(&_Entrypoint.CallOpts, initCode, sender, paymasterAndData)
}

// ValidateSenderAndPaymaster is a free data retrieval call binding the contract method 0x957122ab.
//
// Solidity: function _validateSenderAndPaymaster(bytes initCode, address sender, bytes paymasterAndData) view returns()
func (_Entrypoint *EntrypointCallerSession) ValidateSenderAndPaymaster(initCode []byte, sender common.Address, paymasterAndData []byte) error {
	return _Entrypoint.Contract.ValidateSenderAndPaymaster(&_Entrypoint.CallOpts, initCode, sender, paymasterAndData)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_Entrypoint *EntrypointCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_Entrypoint *EntrypointSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _Entrypoint.Contract.BalanceOf(&_Entrypoint.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_Entrypoint *EntrypointCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _Entrypoint.Contract.BalanceOf(&_Entrypoint.CallOpts, account)
}

// Deposits is a free data retrieval call binding the contract method 0xfc7e286d.
//
// Solidity: function deposits(address ) view returns(uint112 deposit, bool staked, uint112 stake, uint32 unstakeDelaySec, uint48 withdrawTime)
func (_Entrypoint *EntrypointCaller) Deposits(opts *bind.CallOpts, arg0 common.Address) (struct {
	Deposit         *big.Int
	Staked          bool
	Stake           *big.Int
	UnstakeDelaySec uint32
	WithdrawTime    *big.Int
}, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "deposits", arg0)

	outstruct := new(struct {
		Deposit         *big.Int
		Staked          bool
		Stake           *big.Int
		UnstakeDelaySec uint32
		WithdrawTime    *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Deposit = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.Staked = *abi.ConvertType(out[1], new(bool)).(*bool)
	outstruct.Stake = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.UnstakeDelaySec = *abi.ConvertType(out[3], new(uint32)).(*uint32)
	outstruct.WithdrawTime = *abi.ConvertType(out[4], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// Deposits is a free data retrieval call binding the contract method 0xfc7e286d.
//
// Solidity: function deposits(address ) view returns(uint112 deposit, bool staked, uint112 stake, uint32 unstakeDelaySec, uint48 withdrawTime)
func (_Entrypoint *EntrypointSession) Deposits(arg0 common.Address) (struct {
	Deposit         *big.Int
	Staked          bool
	Stake           *big.Int
	UnstakeDelaySec uint32
	WithdrawTime    *big.Int
}, error) {
	return _Entrypoint.Contract.Deposits(&_Entrypoint.CallOpts, arg0)
}

// Deposits is a free data retrieval call binding the contract method 0xfc7e286d.
//
// Solidity: function deposits(address ) view returns(uint112 deposit, bool staked, uint112 stake, uint32 unstakeDelaySec, uint48 withdrawTime)
func (_Entrypoint *EntrypointCallerSession) Deposits(arg0 common.Address) (struct {
	Deposit         *big.Int
	Staked          bool
	Stake           *big.Int
	UnstakeDelaySec uint32
	WithdrawTime    *big.Int
}, error) {
	return _Entrypoint.Contract.Deposits(&_Entrypoint.CallOpts, arg0)
}

// GetDepositInfo is a free data retrieval call binding the contract method 0x5287ce12.
//
// Solidity: function getDepositInfo(address account) view returns((uint112,bool,uint112,uint32,uint48) info)
func (_Entrypoint *EntrypointCaller) GetDepositInfo(opts *bind.CallOpts, account common.Address) (IStakeManagerDepositInfo, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "getDepositInfo", account)

	if err != nil {
		return *new(IStakeManagerDepositInfo), err
	}

	out0 := *abi.ConvertType(out[0], new(IStakeManagerDepositInfo)).(*IStakeManagerDepositInfo)

	return out0, err

}

// GetDepositInfo is a free data retrieval call binding the contract method 0x5287ce12.
//
// Solidity: function getDepositInfo(address account) view returns((uint112,bool,uint112,uint32,uint48) info)
func (_Entrypoint *EntrypointSession) GetDepositInfo(account common.Address) (IStakeManagerDepositInfo, error) {
	return _Entrypoint.Contract.GetDepositInfo(&_Entrypoint.CallOpts, account)
}

// GetDepositInfo is a free data retrieval call binding the contract method 0x5287ce12.
//
// Solidity: function getDepositInfo(address account) view returns((uint112,bool,uint112,uint32,uint48) info)
func (_Entrypoint *EntrypointCallerSession) GetDepositInfo(account common.Address) (IStakeManagerDepositInfo, error) {
	return _Entrypoint.Contract.GetDepositInfo(&_Entrypoint.CallOpts, account)
}

// GetNonce is a free data retrieval call binding the contract method 0x35567e1a.
//
// Solidity: function getNonce(address sender, uint192 key) view returns(uint256 nonce)
func (_Entrypoint *EntrypointCaller) GetNonce(opts *bind.CallOpts, sender common.Address, key *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "getNonce", sender, key)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNonce is a free data retrieval call binding the contract method 0x35567e1a.
//
// Solidity: function getNonce(address sender, uint192 key) view returns(uint256 nonce)
func (_Entrypoint *EntrypointSession) GetNonce(sender common.Address, key *big.Int) (*big.Int, error) {
	return _Entrypoint.Contract.GetNonce(&_Entrypoint.CallOpts, sender, key)
}

// GetNonce is a free data retrieval call binding the contract method 0x35567e1a.
//
// Solidity: function getNonce(address sender, uint192 key) view returns(uint256 nonce)
func (_Entrypoint *EntrypointCallerSession) GetNonce(sender common.Address, key *big.Int) (*big.Int, error) {
	return _Entrypoint.Contract.GetNonce(&_Entrypoint.CallOpts, sender, key)
}

// GetUserOpHash is a free data retrieval call binding the contract method 0xa6193531.
//
// Solidity: function getUserOpHash((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) view returns(bytes32)
func (_Entrypoint *EntrypointCaller) GetUserOpHash(opts *bind.CallOpts, userOp UserOperation) ([32]byte, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "getUserOpHash", userOp)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetUserOpHash is a free data retrieval call binding the contract method 0xa6193531.
//
// Solidity: function getUserOpHash((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) view returns(bytes32)
func (_Entrypoint *EntrypointSession) GetUserOpHash(userOp UserOperation) ([32]byte, error) {
	return _Entrypoint.Contract.GetUserOpHash(&_Entrypoint.CallOpts, userOp)
}

// GetUserOpHash is a free data retrieval call binding the contract method 0xa6193531.
//
// Solidity: function getUserOpHash((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) view returns(bytes32)
func (_Entrypoint *EntrypointCallerSession) GetUserOpHash(userOp UserOperation) ([32]byte, error) {
	return _Entrypoint.Contract.GetUserOpHash(&_Entrypoint.CallOpts, userOp)
}

// NonceSequenceNumber is a free data retrieval call binding the contract method 0x1b2e01b8.
//
// Solidity: function nonceSequenceNumber(address , uint192 ) view returns(uint256)
func (_Entrypoint *EntrypointCaller) NonceSequenceNumber(opts *bind.CallOpts, arg0 common.Address, arg1 *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Entrypoint.contract.Call(opts, &out, "nonceSequenceNumber", arg0, arg1)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NonceSequenceNumber is a free data retrieval call binding the contract method 0x1b2e01b8.
//
// Solidity: function nonceSequenceNumber(address , uint192 ) view returns(uint256)
func (_Entrypoint *EntrypointSession) NonceSequenceNumber(arg0 common.Address, arg1 *big.Int) (*big.Int, error) {
	return _Entrypoint.Contract.NonceSequenceNumber(&_Entrypoint.CallOpts, arg0, arg1)
}

// NonceSequenceNumber is a free data retrieval call binding the contract method 0x1b2e01b8.
//
// Solidity: function nonceSequenceNumber(address , uint192 ) view returns(uint256)
func (_Entrypoint *EntrypointCallerSession) NonceSequenceNumber(arg0 common.Address, arg1 *big.Int) (*big.Int, error) {
	return _Entrypoint.Contract.NonceSequenceNumber(&_Entrypoint.CallOpts, arg0, arg1)
}

// AddStake is a paid mutator transaction binding the contract method 0x0396cb60.
//
// Solidity: function addStake(uint32 unstakeDelaySec) payable returns()
func (_Entrypoint *EntrypointTransactor) AddStake(opts *bind.TransactOpts, unstakeDelaySec uint32) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "addStake", unstakeDelaySec)
}

// AddStake is a paid mutator transaction binding the contract method 0x0396cb60.
//
// Solidity: function addStake(uint32 unstakeDelaySec) payable returns()
func (_Entrypoint *EntrypointSession) AddStake(unstakeDelaySec uint32) (*types.Transaction, error) {
	return _Entrypoint.Contract.AddStake(&_Entrypoint.TransactOpts, unstakeDelaySec)
}

// AddStake is a paid mutator transaction binding the contract method 0x0396cb60.
//
// Solidity: function addStake(uint32 unstakeDelaySec) payable returns()
func (_Entrypoint *EntrypointTransactorSession) AddStake(unstakeDelaySec uint32) (*types.Transaction, error) {
	return _Entrypoint.Contract.AddStake(&_Entrypoint.TransactOpts, unstakeDelaySec)
}

// DepositTo is a paid mutator transaction binding the contract method 0xb760faf9.
//
// Solidity: function depositTo(address account) payable returns()
func (_Entrypoint *EntrypointTransactor) DepositTo(opts *bind.TransactOpts, account common.Address) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "depositTo", account)
}

// DepositTo is a paid mutator transaction binding the contract method 0xb760faf9.
//
// Solidity: function depositTo(address account) payable returns()
func (_Entrypoint *EntrypointSession) DepositTo(account common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.DepositTo(&_Entrypoint.TransactOpts, account)
}

// DepositTo is a paid mutator transaction binding the contract method 0xb760faf9.
//
// Solidity: function depositTo(address account) payable returns()
func (_Entrypoint *EntrypointTransactorSession) DepositTo(account common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.DepositTo(&_Entrypoint.TransactOpts, account)
}

// GetSenderAddress is a paid mutator transaction binding the contract method 0x9b249f69.
//
// Solidity: function getSenderAddress(bytes initCode) returns()
func (_Entrypoint *EntrypointTransactor) GetSenderAddress(opts *bind.TransactOpts, initCode []byte) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "getSenderAddress", initCode)
}

// GetSenderAddress is a paid mutator transaction binding the contract method 0x9b249f69.
//
// Solidity: function getSenderAddress(bytes initCode) returns()
func (_Entrypoint *EntrypointSession) GetSenderAddress(initCode []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.GetSenderAddress(&_Entrypoint.TransactOpts, initCode)
}

// GetSenderAddress is a paid mutator transaction binding the contract method 0x9b249f69.
//
// Solidity: function getSenderAddress(bytes initCode) returns()
func (_Entrypoint *EntrypointTransactorSession) GetSenderAddress(initCode []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.GetSenderAddress(&_Entrypoint.TransactOpts, initCode)
}

// HandleAggregatedOps is a paid mutator transaction binding the contract method 0x4b1d7cf5.
//
// Solidity: function handleAggregatedOps(((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[],address,bytes)[] opsPerAggregator, address beneficiary) returns()
func (_Entrypoint *EntrypointTransactor) HandleAggregatedOps(opts *bind.TransactOpts, opsPerAggregator []IEntryPointUserOpsPerAggregator, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "handleAggregatedOps", opsPerAggregator, beneficiary)
}

// HandleAggregatedOps is a paid mutator transaction binding the contract method 0x4b1d7cf5.
//
// Solidity: function handleAggregatedOps(((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[],address,bytes)[] opsPerAggregator, address beneficiary) returns()
func (_Entrypoint *EntrypointSession) HandleAggregatedOps(opsPerAggregator []IEntryPointUserOpsPerAggregator, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.HandleAggregatedOps(&_Entrypoint.TransactOpts, opsPerAggregator, beneficiary)
}

// HandleAggregatedOps is a paid mutator transaction binding the contract method 0x4b1d7cf5.
//
// Solidity: function handleAggregatedOps(((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[],address,bytes)[] opsPerAggregator, address beneficiary) returns()
func (_Entrypoint *EntrypointTransactorSession) HandleAggregatedOps(opsPerAggregator []IEntryPointUserOpsPerAggregator, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.HandleAggregatedOps(&_Entrypoint.TransactOpts, opsPerAggregator, beneficiary)
}

// HandleOps is a paid mutator transaction binding the contract method 0x1fad948c.
//
// Solidity: function handleOps((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[] ops, address beneficiary) returns()
func (_Entrypoint *EntrypointTransactor) HandleOps(opts *bind.TransactOpts, ops []UserOperation, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "handleOps", ops, beneficiary)
}

// HandleOps is a paid mutator transaction binding the contract method 0x1fad948c.
//
// Solidity: function handleOps((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[] ops, address beneficiary) returns()
func (_Entrypoint *EntrypointSession) HandleOps(ops []UserOperation, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.HandleOps(&_Entrypoint.TransactOpts, ops, beneficiary)
}

// HandleOps is a paid mutator transaction binding the contract method 0x1fad948c.
//
// Solidity: function handleOps((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes)[] ops, address beneficiary) returns()
func (_Entrypoint *EntrypointTransactorSession) HandleOps(ops []UserOperation, beneficiary common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.HandleOps(&_Entrypoint.TransactOpts, ops, beneficiary)
}

// IncrementNonce is a paid mutator transaction binding the contract method 0x0bd28e3b.
//
// Solidity: function incrementNonce(uint192 key) returns()
func (_Entrypoint *EntrypointTransactor) IncrementNonce(opts *bind.TransactOpts, key *big.Int) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "incrementNonce", key)
}

// IncrementNonce is a paid mutator transaction binding the contract method 0x0bd28e3b.
//
// Solidity: function incrementNonce(uint192 key) returns()
func (_Entrypoint *EntrypointSession) IncrementNonce(key *big.Int) (*types.Transaction, error) {
	return _Entrypoint.Contract.IncrementNonce(&_Entrypoint.TransactOpts, key)
}

// IncrementNonce is a paid mutator transaction binding the contract method 0x0bd28e3b.
//
// Solidity: function incrementNonce(uint192 key) returns()
func (_Entrypoint *EntrypointTransactorSession) IncrementNonce(key *big.Int) (*types.Transaction, error) {
	return _Entrypoint.Contract.IncrementNonce(&_Entrypoint.TransactOpts, key)
}

// InnerHandleOp is a paid mutator transaction binding the contract method 0x1d732756.
//
// Solidity: function innerHandleOp(bytes callData, ((address,uint256,uint256,uint256,uint256,address,uint256,uint256),bytes32,uint256,uint256,uint256) opInfo, bytes context) returns(uint256 actualGasCost)
func (_Entrypoint *EntrypointTransactor) InnerHandleOp(opts *bind.TransactOpts, callData []byte, opInfo EntryPointUserOpInfo, context []byte) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "innerHandleOp", callData, opInfo, context)
}

// InnerHandleOp is a paid mutator transaction binding the contract method 0x1d732756.
//
// Solidity: function innerHandleOp(bytes callData, ((address,uint256,uint256,uint256,uint256,address,uint256,uint256),bytes32,uint256,uint256,uint256) opInfo, bytes context) returns(uint256 actualGasCost)
func (_Entrypoint *EntrypointSession) InnerHandleOp(callData []byte, opInfo EntryPointUserOpInfo, context []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.InnerHandleOp(&_Entrypoint.TransactOpts, callData, opInfo, context)
}

// InnerHandleOp is a paid mutator transaction binding the contract method 0x1d732756.
//
// Solidity: function innerHandleOp(bytes callData, ((address,uint256,uint256,uint256,uint256,address,uint256,uint256),bytes32,uint256,uint256,uint256) opInfo, bytes context) returns(uint256 actualGasCost)
func (_Entrypoint *EntrypointTransactorSession) InnerHandleOp(callData []byte, opInfo EntryPointUserOpInfo, context []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.InnerHandleOp(&_Entrypoint.TransactOpts, callData, opInfo, context)
}

// SimulateHandleOp is a paid mutator transaction binding the contract method 0xd6383f94.
//
// Solidity: function simulateHandleOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) op, address target, bytes targetCallData) returns()
func (_Entrypoint *EntrypointTransactor) SimulateHandleOp(opts *bind.TransactOpts, op UserOperation, target common.Address, targetCallData []byte) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "simulateHandleOp", op, target, targetCallData)
}

// SimulateHandleOp is a paid mutator transaction binding the contract method 0xd6383f94.
//
// Solidity: function simulateHandleOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) op, address target, bytes targetCallData) returns()
func (_Entrypoint *EntrypointSession) SimulateHandleOp(op UserOperation, target common.Address, targetCallData []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.SimulateHandleOp(&_Entrypoint.TransactOpts, op, target, targetCallData)
}

// SimulateHandleOp is a paid mutator transaction binding the contract method 0xd6383f94.
//
// Solidity: function simulateHandleOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) op, address target, bytes targetCallData) returns()
func (_Entrypoint *EntrypointTransactorSession) SimulateHandleOp(op UserOperation, target common.Address, targetCallData []byte) (*types.Transaction, error) {
	return _Entrypoint.Contract.SimulateHandleOp(&_Entrypoint.TransactOpts, op, target, targetCallData)
}

// SimulateValidation is a paid mutator transaction binding the contract method 0xee219423.
//
// Solidity: function simulateValidation((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) returns()
func (_Entrypoint *EntrypointTransactor) SimulateValidation(opts *bind.TransactOpts, userOp UserOperation) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "simulateValidation", userOp)
}

// SimulateValidation is a paid mutator transaction binding the contract method 0xee219423.
//
// Solidity: function simulateValidation((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) returns()
func (_Entrypoint *EntrypointSession) SimulateValidation(userOp UserOperation) (*types.Transaction, error) {
	return _Entrypoint.Contract.SimulateValidation(&_Entrypoint.TransactOpts, userOp)
}

// SimulateValidation is a paid mutator transaction binding the contract method 0xee219423.
//
// Solidity: function simulateValidation((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp) returns()
func (_Entrypoint *EntrypointTransactorSession) SimulateValidation(userOp UserOperation) (*types.Transaction, error) {
	return _Entrypoint.Contract.SimulateValidation(&_Entrypoint.TransactOpts, userOp)
}

// UnlockStake is a paid mutator transaction binding the contract method 0xbb9fe6bf.
//
// Solidity: function unlockStake() returns()
func (_Entrypoint *EntrypointTransactor) UnlockStake(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "unlockStake")
}

// UnlockStake is a paid mutator transaction binding the contract method 0xbb9fe6bf.
//
// Solidity: function unlockStake() returns()
func (_Entrypoint *EntrypointSession) UnlockStake() (*types.Transaction, error) {
	return _Entrypoint.Contract.UnlockStake(&_Entrypoint.TransactOpts)
}

// UnlockStake is a paid mutator transaction binding the contract method 0xbb9fe6bf.
//
// Solidity: function unlockStake() returns()
func (_Entrypoint *EntrypointTransactorSession) UnlockStake() (*types.Transaction, error) {
	return _Entrypoint.Contract.UnlockStake(&_Entrypoint.TransactOpts)
}

// WithdrawStake is a paid mutator transaction binding the contract method 0xc23a5cea.
//
// Solidity: function withdrawStake(address withdrawAddress) returns()
func (_Entrypoint *EntrypointTransactor) WithdrawStake(opts *bind.TransactOpts, withdrawAddress common.Address) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "withdrawStake", withdrawAddress)
}

// WithdrawStake is a paid mutator transaction binding the contract method 0xc23a5cea.
//
// Solidity: function withdrawStake(address withdrawAddress) returns()
func (_Entrypoint *EntrypointSession) WithdrawStake(withdrawAddress common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.WithdrawStake(&_Entrypoint.TransactOpts, withdrawAddress)
}

// WithdrawStake is a paid mutator transaction binding the contract method 0xc23a5cea.
//
// Solidity: function withdrawStake(address withdrawAddress) returns()
func (_Entrypoint *EntrypointTransactorSession) WithdrawStake(withdrawAddress common.Address) (*types.Transaction, error) {
	return _Entrypoint.Contract.WithdrawStake(&_Entrypoint.TransactOpts, withdrawAddress)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0x205c2878.
//
// Solidity: function withdrawTo(address withdrawAddress, uint256 withdrawAmount) returns()
func (_Entrypoint *EntrypointTransactor) WithdrawTo(opts *bind.TransactOpts, withdrawAddress common.Address, withdrawAmount *big.Int) (*types.Transaction, error) {
	return _Entrypoint.contract.Transact(opts, "withdrawTo", withdrawAddress, withdrawAmount)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0x205c2878.
//
// Solidity: function withdrawTo(address withdrawAddress, uint256 withdrawAmount) returns()
func (_Entrypoint *EntrypointSession) WithdrawTo(withdrawAddress common.Address, withdrawAmount *big.Int) (*types.Transaction, error) {
	return _Entrypoint.Contract.WithdrawTo(&_Entrypoint.TransactOpts, withdrawAddress, withdrawAmount)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0x205c2878.
//
// Solidity: function withdrawTo(address withdrawAddress, uint256 withdrawAmount) returns()
func (_Entrypoint *EntrypointTransactorSession) WithdrawTo(withdrawAddress common.Address, withdrawAmount *big.Int) (*types.Transaction, error) {
	return _Entrypoint.Contract.WithdrawTo(&_Entrypoint.TransactOpts, withdrawAddress, withdrawAmount)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_Entrypoint *EntrypointTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Entrypoint.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_Entrypoint *EntrypointSession) Receive() (*types.Transaction, error) {
	return _Entrypoint.Contract.Receive(&_Entrypoint.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_Entrypoint *EntrypointTransactorSession) Receive() (*types.Transaction, error) {
	return _Entrypoint.Contract.Receive(&_Entrypoint.TransactOpts)
}

// EntrypointAccountDeployedIterator is returned from FilterAccountDeployed and is used to iterate over the raw logs and unpacked data for AccountDeployed events raised by the Entrypoint contract.
type EntrypointAccountDeployedIterator struct {
	Event *EntrypointAccountDeployed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointAccountDeployedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointAccountDeployed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointAccountDeployed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointAccountDeployedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointAccountDeployedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointAccountDeployed represents a AccountDeployed event raised by the Entrypoint contract.
type EntrypointAccountDeployed struct {
	UserOpHash [32]byte
	Sender     common.Address
	Factory    common.Address
	Paymaster  common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterAccountDeployed is a free log retrieval operation binding the contract event 0xd51a9c61267aa6196961883ecf5ff2da6619c37dac0fa92122513fb32c032d2d.
//
// Solidity: event AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
func (_Entrypoint *EntrypointFilterer) FilterAccountDeployed(opts *bind.FilterOpts, userOpHash [][32]byte, sender []common.Address) (*EntrypointAccountDeployedIterator, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "AccountDeployed", userOpHashRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointAccountDeployedIterator{contract: _Entrypoint.contract, event: "AccountDeployed", logs: logs, sub: sub}, nil
}

// WatchAccountDeployed is a free log subscription operation binding the contract event 0xd51a9c61267aa6196961883ecf5ff2da6619c37dac0fa92122513fb32c032d2d.
//
// Solidity: event AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
func (_Entrypoint *EntrypointFilterer) WatchAccountDeployed(opts *bind.WatchOpts, sink chan<- *EntrypointAccountDeployed, userOpHash [][32]byte, sender []common.Address) (event.Subscription, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "AccountDeployed", userOpHashRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointAccountDeployed)
				if err := _Entrypoint.contract.UnpackLog(event, "AccountDeployed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseAccountDeployed is a log parse operation binding the contract event 0xd51a9c61267aa6196961883ecf5ff2da6619c37dac0fa92122513fb32c032d2d.
//
// Solidity: event AccountDeployed(bytes32 indexed userOpHash, address indexed sender, address factory, address paymaster)
func (_Entrypoint *EntrypointFilterer) ParseAccountDeployed(log types.Log) (*EntrypointAccountDeployed, error) {
	event := new(EntrypointAccountDeployed)
	if err := _Entrypoint.contract.UnpackLog(event, "AccountDeployed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointBeforeExecutionIterator is returned from FilterBeforeExecution and is used to iterate over the raw logs and unpacked data for BeforeExecution events raised by the Entrypoint contract.
type EntrypointBeforeExecutionIterator struct {
	Event *EntrypointBeforeExecution // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointBeforeExecutionIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointBeforeExecution)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointBeforeExecution)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointBeforeExecutionIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointBeforeExecutionIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointBeforeExecution represents a BeforeExecution event raised by the Entrypoint contract.
type EntrypointBeforeExecution struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterBeforeExecution is a free log retrieval operation binding the contract event 0xbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f972.
//
// Solidity: event BeforeExecution()
func (_Entrypoint *EntrypointFilterer) FilterBeforeExecution(opts *bind.FilterOpts) (*EntrypointBeforeExecutionIterator, error) {

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "BeforeExecution")
	if err != nil {
		return nil, err
	}
	return &EntrypointBeforeExecutionIterator{contract: _Entrypoint.contract, event: "BeforeExecution", logs: logs, sub: sub}, nil
}

// WatchBeforeExecution is a free log subscription operation binding the contract event 0xbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f972.
//
// Solidity: event BeforeExecution()
func (_Entrypoint *EntrypointFilterer) WatchBeforeExecution(opts *bind.WatchOpts, sink chan<- *EntrypointBeforeExecution) (event.Subscription, error) {

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "BeforeExecution")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointBeforeExecution)
				if err := _Entrypoint.contract.UnpackLog(event, "BeforeExecution", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseBeforeExecution is a log parse operation binding the contract event 0xbb47ee3e183a558b1a2ff0874b079f3fc5478b7454eacf2bfc5af2ff5878f972.
//
// Solidity: event BeforeExecution()
func (_Entrypoint *EntrypointFilterer) ParseBeforeExecution(log types.Log) (*EntrypointBeforeExecution, error) {
	event := new(EntrypointBeforeExecution)
	if err := _Entrypoint.contract.UnpackLog(event, "BeforeExecution", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointDepositedIterator is returned from FilterDeposited and is used to iterate over the raw logs and unpacked data for Deposited events raised by the Entrypoint contract.
type EntrypointDepositedIterator struct {
	Event *EntrypointDeposited // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointDepositedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointDeposited)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointDeposited)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointDepositedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointDepositedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointDeposited represents a Deposited event raised by the Entrypoint contract.
type EntrypointDeposited struct {
	Account      common.Address
	TotalDeposit *big.Int
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterDeposited is a free log retrieval operation binding the contract event 0x2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c4.
//
// Solidity: event Deposited(address indexed account, uint256 totalDeposit)
func (_Entrypoint *EntrypointFilterer) FilterDeposited(opts *bind.FilterOpts, account []common.Address) (*EntrypointDepositedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "Deposited", accountRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointDepositedIterator{contract: _Entrypoint.contract, event: "Deposited", logs: logs, sub: sub}, nil
}

// WatchDeposited is a free log subscription operation binding the contract event 0x2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c4.
//
// Solidity: event Deposited(address indexed account, uint256 totalDeposit)
func (_Entrypoint *EntrypointFilterer) WatchDeposited(opts *bind.WatchOpts, sink chan<- *EntrypointDeposited, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "Deposited", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointDeposited)
				if err := _Entrypoint.contract.UnpackLog(event, "Deposited", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDeposited is a log parse operation binding the contract event 0x2da466a7b24304f47e87fa2e1e5a81b9831ce54fec19055ce277ca2f39ba42c4.
//
// Solidity: event Deposited(address indexed account, uint256 totalDeposit)
func (_Entrypoint *EntrypointFilterer) ParseDeposited(log types.Log) (*EntrypointDeposited, error) {
	event := new(EntrypointDeposited)
	if err := _Entrypoint.contract.UnpackLog(event, "Deposited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointSignatureAggregatorChangedIterator is returned from FilterSignatureAggregatorChanged and is used to iterate over the raw logs and unpacked data for SignatureAggregatorChanged events raised by the Entrypoint contract.
type EntrypointSignatureAggregatorChangedIterator struct {
	Event *EntrypointSignatureAggregatorChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointSignatureAggregatorChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointSignatureAggregatorChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointSignatureAggregatorChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointSignatureAggregatorChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointSignatureAggregatorChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointSignatureAggregatorChanged represents a SignatureAggregatorChanged event raised by the Entrypoint contract.
type EntrypointSignatureAggregatorChanged struct {
	Aggregator common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterSignatureAggregatorChanged is a free log retrieval operation binding the contract event 0x575ff3acadd5ab348fe1855e217e0f3678f8d767d7494c9f9fefbee2e17cca4d.
//
// Solidity: event SignatureAggregatorChanged(address indexed aggregator)
func (_Entrypoint *EntrypointFilterer) FilterSignatureAggregatorChanged(opts *bind.FilterOpts, aggregator []common.Address) (*EntrypointSignatureAggregatorChangedIterator, error) {

	var aggregatorRule []interface{}
	for _, aggregatorItem := range aggregator {
		aggregatorRule = append(aggregatorRule, aggregatorItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "SignatureAggregatorChanged", aggregatorRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointSignatureAggregatorChangedIterator{contract: _Entrypoint.contract, event: "SignatureAggregatorChanged", logs: logs, sub: sub}, nil
}

// WatchSignatureAggregatorChanged is a free log subscription operation binding the contract event 0x575ff3acadd5ab348fe1855e217e0f3678f8d767d7494c9f9fefbee2e17cca4d.
//
// Solidity: event SignatureAggregatorChanged(address indexed aggregator)
func (_Entrypoint *EntrypointFilterer) WatchSignatureAggregatorChanged(opts *bind.WatchOpts, sink chan<- *EntrypointSignatureAggregatorChanged, aggregator []common.Address) (event.Subscription, error) {

	var aggregatorRule []interface{}
	for _, aggregatorItem := range aggregator {
		aggregatorRule = append(aggregatorRule, aggregatorItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "SignatureAggregatorChanged", aggregatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointSignatureAggregatorChanged)
				if err := _Entrypoint.contract.UnpackLog(event, "SignatureAggregatorChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSignatureAggregatorChanged is a log parse operation binding the contract event 0x575ff3acadd5ab348fe1855e217e0f3678f8d767d7494c9f9fefbee2e17cca4d.
//
// Solidity: event SignatureAggregatorChanged(address indexed aggregator)
func (_Entrypoint *EntrypointFilterer) ParseSignatureAggregatorChanged(log types.Log) (*EntrypointSignatureAggregatorChanged, error) {
	event := new(EntrypointSignatureAggregatorChanged)
	if err := _Entrypoint.contract.UnpackLog(event, "SignatureAggregatorChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointStakeLockedIterator is returned from FilterStakeLocked and is used to iterate over the raw logs and unpacked data for StakeLocked events raised by the Entrypoint contract.
type EntrypointStakeLockedIterator struct {
	Event *EntrypointStakeLocked // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointStakeLockedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointStakeLocked)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointStakeLocked)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointStakeLockedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointStakeLockedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointStakeLocked represents a StakeLocked event raised by the Entrypoint contract.
type EntrypointStakeLocked struct {
	Account         common.Address
	TotalStaked     *big.Int
	UnstakeDelaySec *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterStakeLocked is a free log retrieval operation binding the contract event 0xa5ae833d0bb1dcd632d98a8b70973e8516812898e19bf27b70071ebc8dc52c01.
//
// Solidity: event StakeLocked(address indexed account, uint256 totalStaked, uint256 unstakeDelaySec)
func (_Entrypoint *EntrypointFilterer) FilterStakeLocked(opts *bind.FilterOpts, account []common.Address) (*EntrypointStakeLockedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "StakeLocked", accountRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointStakeLockedIterator{contract: _Entrypoint.contract, event: "StakeLocked", logs: logs, sub: sub}, nil
}

// WatchStakeLocked is a free log subscription operation binding the contract event 0xa5ae833d0bb1dcd632d98a8b70973e8516812898e19bf27b70071ebc8dc52c01.
//
// Solidity: event StakeLocked(address indexed account, uint256 totalStaked, uint256 unstakeDelaySec)
func (_Entrypoint *EntrypointFilterer) WatchStakeLocked(opts *bind.WatchOpts, sink chan<- *EntrypointStakeLocked, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "StakeLocked", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointStakeLocked)
				if err := _Entrypoint.contract.UnpackLog(event, "StakeLocked", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseStakeLocked is a log parse operation binding the contract event 0xa5ae833d0bb1dcd632d98a8b70973e8516812898e19bf27b70071ebc8dc52c01.
//
// Solidity: event StakeLocked(address indexed account, uint256 totalStaked, uint256 unstakeDelaySec)
func (_Entrypoint *EntrypointFilterer) ParseStakeLocked(log types.Log) (*EntrypointStakeLocked, error) {
	event := new(EntrypointStakeLocked)
	if err := _Entrypoint.contract.UnpackLog(event, "StakeLocked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointStakeUnlockedIterator is returned from FilterStakeUnlocked and is used to iterate over the raw logs and unpacked data for StakeUnlocked events raised by the Entrypoint contract.
type EntrypointStakeUnlockedIterator struct {
	Event *EntrypointStakeUnlocked // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointStakeUnlockedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointStakeUnlocked)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointStakeUnlocked)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointStakeUnlockedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointStakeUnlockedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointStakeUnlocked represents a StakeUnlocked event raised by the Entrypoint contract.
type EntrypointStakeUnlocked struct {
	Account      common.Address
	WithdrawTime *big.Int
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterStakeUnlocked is a free log retrieval operation binding the contract event 0xfa9b3c14cc825c412c9ed81b3ba365a5b459439403f18829e572ed53a4180f0a.
//
// Solidity: event StakeUnlocked(address indexed account, uint256 withdrawTime)
func (_Entrypoint *EntrypointFilterer) FilterStakeUnlocked(opts *bind.FilterOpts, account []common.Address) (*EntrypointStakeUnlockedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "StakeUnlocked", accountRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointStakeUnlockedIterator{contract: _Entrypoint.contract, event: "StakeUnlocked", logs: logs, sub: sub}, nil
}

// WatchStakeUnlocked is a free log subscription operation binding the contract event 0xfa9b3c14cc825c412c9ed81b3ba365a5b459439403f18829e572ed53a4180f0a.
//
// Solidity: event StakeUnlocked(address indexed account, uint256 withdrawTime)
func (_Entrypoint *EntrypointFilterer) WatchStakeUnlocked(opts *bind.WatchOpts, sink chan<- *EntrypointStakeUnlocked, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "StakeUnlocked", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointStakeUnlocked)
				if err := _Entrypoint.contract.UnpackLog(event, "StakeUnlocked", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseStakeUnlocked is a log parse operation binding the contract event 0xfa9b3c14cc825c412c9ed81b3ba365a5b459439403f18829e572ed53a4180f0a.
//
// Solidity: event StakeUnlocked(address indexed account, uint256 withdrawTime)
func (_Entrypoint *EntrypointFilterer) ParseStakeUnlocked(log types.Log) (*EntrypointStakeUnlocked, error) {
	event := new(EntrypointStakeUnlocked)
	if err := _Entrypoint.contract.UnpackLog(event, "StakeUnlocked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointStakeWithdrawnIterator is returned from FilterStakeWithdrawn and is used to iterate over the raw logs and unpacked data for StakeWithdrawn events raised by the Entrypoint contract.
type EntrypointStakeWithdrawnIterator struct {
	Event *EntrypointStakeWithdrawn // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointStakeWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointStakeWithdrawn)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointStakeWithdrawn)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointStakeWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointStakeWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointStakeWithdrawn represents a StakeWithdrawn event raised by the Entrypoint contract.
type EntrypointStakeWithdrawn struct {
	Account         common.Address
	WithdrawAddress common.Address
	Amount          *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterStakeWithdrawn is a free log retrieval operation binding the contract event 0xb7c918e0e249f999e965cafeb6c664271b3f4317d296461500e71da39f0cbda3.
//
// Solidity: event StakeWithdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) FilterStakeWithdrawn(opts *bind.FilterOpts, account []common.Address) (*EntrypointStakeWithdrawnIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "StakeWithdrawn", accountRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointStakeWithdrawnIterator{contract: _Entrypoint.contract, event: "StakeWithdrawn", logs: logs, sub: sub}, nil
}

// WatchStakeWithdrawn is a free log subscription operation binding the contract event 0xb7c918e0e249f999e965cafeb6c664271b3f4317d296461500e71da39f0cbda3.
//
// Solidity: event StakeWithdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) WatchStakeWithdrawn(opts *bind.WatchOpts, sink chan<- *EntrypointStakeWithdrawn, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "StakeWithdrawn", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointStakeWithdrawn)
				if err := _Entrypoint.contract.UnpackLog(event, "StakeWithdrawn", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseStakeWithdrawn is a log parse operation binding the contract event 0xb7c918e0e249f999e965cafeb6c664271b3f4317d296461500e71da39f0cbda3.
//
// Solidity: event StakeWithdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) ParseStakeWithdrawn(log types.Log) (*EntrypointStakeWithdrawn, error) {
	event := new(EntrypointStakeWithdrawn)
	if err := _Entrypoint.contract.UnpackLog(event, "StakeWithdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointUserOperationEventIterator is returned from FilterUserOperationEvent and is used to iterate over the raw logs and unpacked data for UserOperationEvent events raised by the Entrypoint contract.
type EntrypointUserOperationEventIterator struct {
	Event *EntrypointUserOperationEvent // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointUserOperationEventIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointUserOperationEvent)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointUserOperationEvent)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointUserOperationEventIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointUserOperationEventIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointUserOperationEvent represents a UserOperationEvent event raised by the Entrypoint contract.
type EntrypointUserOperationEvent struct {
	UserOpHash    [32]byte
	Sender        common.Address
	Paymaster     common.Address
	Nonce         *big.Int
	Success       bool
	ActualGasCost *big.Int
	ActualGasUsed *big.Int
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterUserOperationEvent is a free log retrieval operation binding the contract event 0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f.
//
// Solidity: event UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
func (_Entrypoint *EntrypointFilterer) FilterUserOperationEvent(opts *bind.FilterOpts, userOpHash [][32]byte, sender []common.Address, paymaster []common.Address) (*EntrypointUserOperationEventIterator, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var paymasterRule []interface{}
	for _, paymasterItem := range paymaster {
		paymasterRule = append(paymasterRule, paymasterItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "UserOperationEvent", userOpHashRule, senderRule, paymasterRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointUserOperationEventIterator{contract: _Entrypoint.contract, event: "UserOperationEvent", logs: logs, sub: sub}, nil
}

// WatchUserOperationEvent is a free log subscription operation binding the contract event 0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f.
//
// Solidity: event UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
func (_Entrypoint *EntrypointFilterer) WatchUserOperationEvent(opts *bind.WatchOpts, sink chan<- *EntrypointUserOperationEvent, userOpHash [][32]byte, sender []common.Address, paymaster []common.Address) (event.Subscription, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}
	var paymasterRule []interface{}
	for _, paymasterItem := range paymaster {
		paymasterRule = append(paymasterRule, paymasterItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "UserOperationEvent", userOpHashRule, senderRule, paymasterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointUserOperationEvent)
				if err := _Entrypoint.contract.UnpackLog(event, "UserOperationEvent", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUserOperationEvent is a log parse operation binding the contract event 0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f.
//
// Solidity: event UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)
func (_Entrypoint *EntrypointFilterer) ParseUserOperationEvent(log types.Log) (*EntrypointUserOperationEvent, error) {
	event := new(EntrypointUserOperationEvent)
	if err := _Entrypoint.contract.UnpackLog(event, "UserOperationEvent", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointUserOperationRevertReasonIterator is returned from FilterUserOperationRevertReason and is used to iterate over the raw logs and unpacked data for UserOperationRevertReason events raised by the Entrypoint contract.
type EntrypointUserOperationRevertReasonIterator struct {
	Event *EntrypointUserOperationRevertReason // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointUserOperationRevertReasonIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointUserOperationRevertReason)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointUserOperationRevertReason)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointUserOperationRevertReasonIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointUserOperationRevertReasonIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointUserOperationRevertReason represents a UserOperationRevertReason event raised by the Entrypoint contract.
type EntrypointUserOperationRevertReason struct {
	UserOpHash   [32]byte
	Sender       common.Address
	Nonce        *big.Int
	RevertReason []byte
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterUserOperationRevertReason is a free log retrieval operation binding the contract event 0x1c4fada7374c0a9ee8841fc38afe82932dc0f8e69012e927f061a8bae611a201.
//
// Solidity: event UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
func (_Entrypoint *EntrypointFilterer) FilterUserOperationRevertReason(opts *bind.FilterOpts, userOpHash [][32]byte, sender []common.Address) (*EntrypointUserOperationRevertReasonIterator, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "UserOperationRevertReason", userOpHashRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointUserOperationRevertReasonIterator{contract: _Entrypoint.contract, event: "UserOperationRevertReason", logs: logs, sub: sub}, nil
}

// WatchUserOperationRevertReason is a free log subscription operation binding the contract event 0x1c4fada7374c0a9ee8841fc38afe82932dc0f8e69012e927f061a8bae611a201.
//
// Solidity: event UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
func (_Entrypoint *EntrypointFilterer) WatchUserOperationRevertReason(opts *bind.WatchOpts, sink chan<- *EntrypointUserOperationRevertReason, userOpHash [][32]byte, sender []common.Address) (event.Subscription, error) {

	var userOpHashRule []interface{}
	for _, userOpHashItem := range userOpHash {
		userOpHashRule = append(userOpHashRule, userOpHashItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "UserOperationRevertReason", userOpHashRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointUserOperationRevertReason)
				if err := _Entrypoint.contract.UnpackLog(event, "UserOperationRevertReason", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUserOperationRevertReason is a log parse operation binding the contract event 0x1c4fada7374c0a9ee8841fc38afe82932dc0f8e69012e927f061a8bae611a201.
//
// Solidity: event UserOperationRevertReason(bytes32 indexed userOpHash, address indexed sender, uint256 nonce, bytes revertReason)
func (_Entrypoint *EntrypointFilterer) ParseUserOperationRevertReason(log types.Log) (*EntrypointUserOperationRevertReason, error) {
	event := new(EntrypointUserOperationRevertReason)
	if err := _Entrypoint.contract.UnpackLog(event, "UserOperationRevertReason", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EntrypointWithdrawnIterator is returned from FilterWithdrawn and is used to iterate over the raw logs and unpacked data for Withdrawn events raised by the Entrypoint contract.
type EntrypointWithdrawnIterator struct {
	Event *EntrypointWithdrawn // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *EntrypointWithdrawnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EntrypointWithdrawn)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(EntrypointWithdrawn)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *EntrypointWithdrawnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EntrypointWithdrawnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EntrypointWithdrawn represents a Withdrawn event raised by the Entrypoint contract.
type EntrypointWithdrawn struct {
	Account         common.Address
	WithdrawAddress common.Address
	Amount          *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterWithdrawn is a free log retrieval operation binding the contract event 0xd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb.
//
// Solidity: event Withdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) FilterWithdrawn(opts *bind.FilterOpts, account []common.Address) (*EntrypointWithdrawnIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.FilterLogs(opts, "Withdrawn", accountRule)
	if err != nil {
		return nil, err
	}
	return &EntrypointWithdrawnIterator{contract: _Entrypoint.contract, event: "Withdrawn", logs: logs, sub: sub}, nil
}

// WatchWithdrawn is a free log subscription operation binding the contract event 0xd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb.
//
// Solidity: event Withdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) WatchWithdrawn(opts *bind.WatchOpts, sink chan<- *EntrypointWithdrawn, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _Entrypoint.contract.WatchLogs(opts, "Withdrawn", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EntrypointWithdrawn)
				if err := _Entrypoint.contract.UnpackLog(event, "Withdrawn", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseWithdrawn is a log parse operation binding the contract event 0xd1c19fbcd4551a5edfb66d43d2e337c04837afda3482b42bdf569a8fccdae5fb.
//
// Solidity: event Withdrawn(address indexed account, address withdrawAddress, uint256 amount)
func (_Entrypoint *EntrypointFilterer) ParseWithdrawn(log types.Log) (*EntrypointWithdrawn, error) {
	event := new(EntrypointWithdrawn)
	if err := _Entrypoint.contract.UnpackLog(event, "Withdrawn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
