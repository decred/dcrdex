// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package reentryattack

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
)

// ReentryAttackMetaData contains all meta data concerning the ReentryAttack contract.
var ReentryAttackMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"stateMutability\":\"payable\",\"type\":\"fallback\"},{\"inputs\":[],\"name\":\"allYourBase\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"areBelongToUs\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"es\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"sh\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"name\":\"setUsUpTheBomb\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50600080546001600160a01b0319163317905561029b806100326000396000f3fe60806040526004361061003f5760003560e01c8063627599ee146100595780638da5cb5b1461006e5780638f110770146100aa578063b9ce28a4146100bf575b674563918244f40000471015610057576100576100d2565b005b34801561006557600080fd5b5061005761013b565b34801561007a57600080fd5b5060005461008e906001600160a01b031681565b6040516001600160a01b03909116815260200160405180910390f35b3480156100b657600080fd5b506100576100d2565b6100576100cd36600461021f565b610178565b600254600154604051633924fddb60e11b81526001600160a01b0390921691637249fbb6916101079160040190815260200190565b600060405180830381600087803b15801561012157600080fd5b505af1158015610135573d6000803e3d6000fd5b50505050565b600080546040516001600160a01b03909116914780156108fc02929091818181858888f19350505050158015610175573d6000803e3d6000fd5b50565b600280546001600160a01b0319166001600160a01b03868116918217909255600185905560405163ae05214760e01b8152600481018590526024810186905291831660448301529063ae0521479034906064016000604051808303818588803b1580156101e457600080fd5b505af11580156101f8573d6000803e3d6000fd5b505050505050505050565b80356001600160a01b038116811461021a57600080fd5b919050565b6000806000806080858703121561023557600080fd5b61023e85610203565b9350602085013592506040850135915061025a60608601610203565b90509295919450925056fea2646970667358221220eaf6de1ca5654a3578dcda1ff161624f98e48b3da704bfd4621c51798f4a800d64736f6c634300080f0033",
}

// ReentryAttackABI is the input ABI used to generate the binding from.
// Deprecated: Use ReentryAttackMetaData.ABI instead.
var ReentryAttackABI = ReentryAttackMetaData.ABI

// ReentryAttackBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ReentryAttackMetaData.Bin instead.
var ReentryAttackBin = ReentryAttackMetaData.Bin

// DeployReentryAttack deploys a new Ethereum contract, binding an instance of ReentryAttack to it.
func DeployReentryAttack(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ReentryAttack, error) {
	parsed, err := ReentryAttackMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ReentryAttackBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ReentryAttack{ReentryAttackCaller: ReentryAttackCaller{contract: contract}, ReentryAttackTransactor: ReentryAttackTransactor{contract: contract}, ReentryAttackFilterer: ReentryAttackFilterer{contract: contract}}, nil
}

// ReentryAttack is an auto generated Go binding around an Ethereum contract.
type ReentryAttack struct {
	ReentryAttackCaller     // Read-only binding to the contract
	ReentryAttackTransactor // Write-only binding to the contract
	ReentryAttackFilterer   // Log filterer for contract events
}

// ReentryAttackCaller is an auto generated read-only Go binding around an Ethereum contract.
type ReentryAttackCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ReentryAttackTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ReentryAttackTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ReentryAttackFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ReentryAttackFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ReentryAttackSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ReentryAttackSession struct {
	Contract     *ReentryAttack    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ReentryAttackCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ReentryAttackCallerSession struct {
	Contract *ReentryAttackCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// ReentryAttackTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ReentryAttackTransactorSession struct {
	Contract     *ReentryAttackTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// ReentryAttackRaw is an auto generated low-level Go binding around an Ethereum contract.
type ReentryAttackRaw struct {
	Contract *ReentryAttack // Generic contract binding to access the raw methods on
}

// ReentryAttackCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ReentryAttackCallerRaw struct {
	Contract *ReentryAttackCaller // Generic read-only contract binding to access the raw methods on
}

// ReentryAttackTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ReentryAttackTransactorRaw struct {
	Contract *ReentryAttackTransactor // Generic write-only contract binding to access the raw methods on
}

// NewReentryAttack creates a new instance of ReentryAttack, bound to a specific deployed contract.
func NewReentryAttack(address common.Address, backend bind.ContractBackend) (*ReentryAttack, error) {
	contract, err := bindReentryAttack(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ReentryAttack{ReentryAttackCaller: ReentryAttackCaller{contract: contract}, ReentryAttackTransactor: ReentryAttackTransactor{contract: contract}, ReentryAttackFilterer: ReentryAttackFilterer{contract: contract}}, nil
}

// NewReentryAttackCaller creates a new read-only instance of ReentryAttack, bound to a specific deployed contract.
func NewReentryAttackCaller(address common.Address, caller bind.ContractCaller) (*ReentryAttackCaller, error) {
	contract, err := bindReentryAttack(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ReentryAttackCaller{contract: contract}, nil
}

// NewReentryAttackTransactor creates a new write-only instance of ReentryAttack, bound to a specific deployed contract.
func NewReentryAttackTransactor(address common.Address, transactor bind.ContractTransactor) (*ReentryAttackTransactor, error) {
	contract, err := bindReentryAttack(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ReentryAttackTransactor{contract: contract}, nil
}

// NewReentryAttackFilterer creates a new log filterer instance of ReentryAttack, bound to a specific deployed contract.
func NewReentryAttackFilterer(address common.Address, filterer bind.ContractFilterer) (*ReentryAttackFilterer, error) {
	contract, err := bindReentryAttack(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ReentryAttackFilterer{contract: contract}, nil
}

// bindReentryAttack binds a generic wrapper to an already deployed contract.
func bindReentryAttack(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(ReentryAttackABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ReentryAttack *ReentryAttackRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ReentryAttack.Contract.ReentryAttackCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ReentryAttack *ReentryAttackRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ReentryAttack.Contract.ReentryAttackTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ReentryAttack *ReentryAttackRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ReentryAttack.Contract.ReentryAttackTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ReentryAttack *ReentryAttackCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ReentryAttack.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ReentryAttack *ReentryAttackTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ReentryAttack.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ReentryAttack *ReentryAttackTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ReentryAttack.Contract.contract.Transact(opts, method, params...)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ReentryAttack *ReentryAttackCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ReentryAttack.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ReentryAttack *ReentryAttackSession) Owner() (common.Address, error) {
	return _ReentryAttack.Contract.Owner(&_ReentryAttack.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ReentryAttack *ReentryAttackCallerSession) Owner() (common.Address, error) {
	return _ReentryAttack.Contract.Owner(&_ReentryAttack.CallOpts)
}

// AllYourBase is a paid mutator transaction binding the contract method 0x8f110770.
//
// Solidity: function allYourBase() returns()
func (_ReentryAttack *ReentryAttackTransactor) AllYourBase(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ReentryAttack.contract.Transact(opts, "allYourBase")
}

// AllYourBase is a paid mutator transaction binding the contract method 0x8f110770.
//
// Solidity: function allYourBase() returns()
func (_ReentryAttack *ReentryAttackSession) AllYourBase() (*types.Transaction, error) {
	return _ReentryAttack.Contract.AllYourBase(&_ReentryAttack.TransactOpts)
}

// AllYourBase is a paid mutator transaction binding the contract method 0x8f110770.
//
// Solidity: function allYourBase() returns()
func (_ReentryAttack *ReentryAttackTransactorSession) AllYourBase() (*types.Transaction, error) {
	return _ReentryAttack.Contract.AllYourBase(&_ReentryAttack.TransactOpts)
}

// AreBelongToUs is a paid mutator transaction binding the contract method 0x627599ee.
//
// Solidity: function areBelongToUs() returns()
func (_ReentryAttack *ReentryAttackTransactor) AreBelongToUs(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ReentryAttack.contract.Transact(opts, "areBelongToUs")
}

// AreBelongToUs is a paid mutator transaction binding the contract method 0x627599ee.
//
// Solidity: function areBelongToUs() returns()
func (_ReentryAttack *ReentryAttackSession) AreBelongToUs() (*types.Transaction, error) {
	return _ReentryAttack.Contract.AreBelongToUs(&_ReentryAttack.TransactOpts)
}

// AreBelongToUs is a paid mutator transaction binding the contract method 0x627599ee.
//
// Solidity: function areBelongToUs() returns()
func (_ReentryAttack *ReentryAttackTransactorSession) AreBelongToUs() (*types.Transaction, error) {
	return _ReentryAttack.Contract.AreBelongToUs(&_ReentryAttack.TransactOpts)
}

// SetUsUpTheBomb is a paid mutator transaction binding the contract method 0xb9ce28a4.
//
// Solidity: function setUsUpTheBomb(address es, bytes32 sh, uint256 refundTimestamp, address participant) payable returns()
func (_ReentryAttack *ReentryAttackTransactor) SetUsUpTheBomb(opts *bind.TransactOpts, es common.Address, sh [32]byte, refundTimestamp *big.Int, participant common.Address) (*types.Transaction, error) {
	return _ReentryAttack.contract.Transact(opts, "setUsUpTheBomb", es, sh, refundTimestamp, participant)
}

// SetUsUpTheBomb is a paid mutator transaction binding the contract method 0xb9ce28a4.
//
// Solidity: function setUsUpTheBomb(address es, bytes32 sh, uint256 refundTimestamp, address participant) payable returns()
func (_ReentryAttack *ReentryAttackSession) SetUsUpTheBomb(es common.Address, sh [32]byte, refundTimestamp *big.Int, participant common.Address) (*types.Transaction, error) {
	return _ReentryAttack.Contract.SetUsUpTheBomb(&_ReentryAttack.TransactOpts, es, sh, refundTimestamp, participant)
}

// SetUsUpTheBomb is a paid mutator transaction binding the contract method 0xb9ce28a4.
//
// Solidity: function setUsUpTheBomb(address es, bytes32 sh, uint256 refundTimestamp, address participant) payable returns()
func (_ReentryAttack *ReentryAttackTransactorSession) SetUsUpTheBomb(es common.Address, sh [32]byte, refundTimestamp *big.Int, participant common.Address) (*types.Transaction, error) {
	return _ReentryAttack.Contract.SetUsUpTheBomb(&_ReentryAttack.TransactOpts, es, sh, refundTimestamp, participant)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ReentryAttack *ReentryAttackTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _ReentryAttack.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ReentryAttack *ReentryAttackSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _ReentryAttack.Contract.Fallback(&_ReentryAttack.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ReentryAttack *ReentryAttackTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _ReentryAttack.Contract.Fallback(&_ReentryAttack.TransactOpts, calldata)
}

// EthswapMetaData contains all meta data concerning the Ethswap contract.
var EthswapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// EthswapABI is the input ABI used to generate the binding from.
// Deprecated: Use EthswapMetaData.ABI instead.
var EthswapABI = EthswapMetaData.ABI

// Ethswap is an auto generated Go binding around an Ethereum contract.
type Ethswap struct {
	EthswapCaller     // Read-only binding to the contract
	EthswapTransactor // Write-only binding to the contract
	EthswapFilterer   // Log filterer for contract events
}

// EthswapCaller is an auto generated read-only Go binding around an Ethereum contract.
type EthswapCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EthswapTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EthswapTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EthswapFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EthswapFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EthswapSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EthswapSession struct {
	Contract     *Ethswap          // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EthswapCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EthswapCallerSession struct {
	Contract *EthswapCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts  // Call options to use throughout this session
}

// EthswapTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EthswapTransactorSession struct {
	Contract     *EthswapTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// EthswapRaw is an auto generated low-level Go binding around an Ethereum contract.
type EthswapRaw struct {
	Contract *Ethswap // Generic contract binding to access the raw methods on
}

// EthswapCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EthswapCallerRaw struct {
	Contract *EthswapCaller // Generic read-only contract binding to access the raw methods on
}

// EthswapTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EthswapTransactorRaw struct {
	Contract *EthswapTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEthswap creates a new instance of Ethswap, bound to a specific deployed contract.
func NewEthswap(address common.Address, backend bind.ContractBackend) (*Ethswap, error) {
	contract, err := bindEthswap(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Ethswap{EthswapCaller: EthswapCaller{contract: contract}, EthswapTransactor: EthswapTransactor{contract: contract}, EthswapFilterer: EthswapFilterer{contract: contract}}, nil
}

// NewEthswapCaller creates a new read-only instance of Ethswap, bound to a specific deployed contract.
func NewEthswapCaller(address common.Address, caller bind.ContractCaller) (*EthswapCaller, error) {
	contract, err := bindEthswap(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EthswapCaller{contract: contract}, nil
}

// NewEthswapTransactor creates a new write-only instance of Ethswap, bound to a specific deployed contract.
func NewEthswapTransactor(address common.Address, transactor bind.ContractTransactor) (*EthswapTransactor, error) {
	contract, err := bindEthswap(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EthswapTransactor{contract: contract}, nil
}

// NewEthswapFilterer creates a new log filterer instance of Ethswap, bound to a specific deployed contract.
func NewEthswapFilterer(address common.Address, filterer bind.ContractFilterer) (*EthswapFilterer, error) {
	contract, err := bindEthswap(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EthswapFilterer{contract: contract}, nil
}

// bindEthswap binds a generic wrapper to an already deployed contract.
func bindEthswap(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(EthswapABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Ethswap *EthswapRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Ethswap.Contract.EthswapCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Ethswap *EthswapRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Ethswap.Contract.EthswapTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Ethswap *EthswapRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Ethswap.Contract.EthswapTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Ethswap *EthswapCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Ethswap.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Ethswap *EthswapTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Ethswap.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Ethswap *EthswapTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Ethswap.Contract.contract.Transact(opts, method, params...)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_Ethswap *EthswapTransactor) Initiate(opts *bind.TransactOpts, refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _Ethswap.contract.Transact(opts, "initiate", refundTimestamp, secretHash, participant)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_Ethswap *EthswapSession) Initiate(refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _Ethswap.Contract.Initiate(&_Ethswap.TransactOpts, refundTimestamp, secretHash, participant)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_Ethswap *EthswapTransactorSession) Initiate(refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _Ethswap.Contract.Initiate(&_Ethswap.TransactOpts, refundTimestamp, secretHash, participant)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_Ethswap *EthswapTransactor) Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return _Ethswap.contract.Transact(opts, "refund", secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_Ethswap *EthswapSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _Ethswap.Contract.Refund(&_Ethswap.TransactOpts, secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_Ethswap *EthswapTransactorSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _Ethswap.Contract.Refund(&_Ethswap.TransactOpts, secretHash)
}
