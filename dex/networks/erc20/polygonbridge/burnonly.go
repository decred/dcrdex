// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package polygonbridge

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

// ERC20BurnOnlyPredicateMetaData contains all meta data concerning the ERC20BurnOnlyPredicate contract.
var ERC20BurnOnlyPredicateMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_withdrawManager\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_depositManager\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"constant\":true,\"inputs\":[],\"name\":\"CHAINID\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"state\",\"type\":\"bytes\"}],\"name\":\"interpretStateUpdate\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"networkId\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"onFinalizeExit\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"startExitWithBurntTokens\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"exit\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"inputUtxo\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"challengeData\",\"type\":\"bytes\"}],\"name\":\"verifyDeprecation\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// ERC20BurnOnlyPredicateABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20BurnOnlyPredicateMetaData.ABI instead.
var ERC20BurnOnlyPredicateABI = ERC20BurnOnlyPredicateMetaData.ABI

// ERC20BurnOnlyPredicate is an auto generated Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicate struct {
	ERC20BurnOnlyPredicateCaller     // Read-only binding to the contract
	ERC20BurnOnlyPredicateTransactor // Write-only binding to the contract
	ERC20BurnOnlyPredicateFilterer   // Log filterer for contract events
}

// ERC20BurnOnlyPredicateCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicateCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20BurnOnlyPredicateTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicateTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20BurnOnlyPredicateFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20BurnOnlyPredicateFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20BurnOnlyPredicateSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20BurnOnlyPredicateSession struct {
	Contract     *ERC20BurnOnlyPredicate // Generic contract binding to set the session for
	CallOpts     bind.CallOpts           // Call options to use throughout this session
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// ERC20BurnOnlyPredicateCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20BurnOnlyPredicateCallerSession struct {
	Contract *ERC20BurnOnlyPredicateCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                 // Call options to use throughout this session
}

// ERC20BurnOnlyPredicateTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20BurnOnlyPredicateTransactorSession struct {
	Contract     *ERC20BurnOnlyPredicateTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                 // Transaction auth options to use throughout this session
}

// ERC20BurnOnlyPredicateRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicateRaw struct {
	Contract *ERC20BurnOnlyPredicate // Generic contract binding to access the raw methods on
}

// ERC20BurnOnlyPredicateCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicateCallerRaw struct {
	Contract *ERC20BurnOnlyPredicateCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20BurnOnlyPredicateTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20BurnOnlyPredicateTransactorRaw struct {
	Contract *ERC20BurnOnlyPredicateTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20BurnOnlyPredicate creates a new instance of ERC20BurnOnlyPredicate, bound to a specific deployed contract.
func NewERC20BurnOnlyPredicate(address common.Address, backend bind.ContractBackend) (*ERC20BurnOnlyPredicate, error) {
	contract, err := bindERC20BurnOnlyPredicate(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20BurnOnlyPredicate{ERC20BurnOnlyPredicateCaller: ERC20BurnOnlyPredicateCaller{contract: contract}, ERC20BurnOnlyPredicateTransactor: ERC20BurnOnlyPredicateTransactor{contract: contract}, ERC20BurnOnlyPredicateFilterer: ERC20BurnOnlyPredicateFilterer{contract: contract}}, nil
}

// NewERC20BurnOnlyPredicateCaller creates a new read-only instance of ERC20BurnOnlyPredicate, bound to a specific deployed contract.
func NewERC20BurnOnlyPredicateCaller(address common.Address, caller bind.ContractCaller) (*ERC20BurnOnlyPredicateCaller, error) {
	contract, err := bindERC20BurnOnlyPredicate(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20BurnOnlyPredicateCaller{contract: contract}, nil
}

// NewERC20BurnOnlyPredicateTransactor creates a new write-only instance of ERC20BurnOnlyPredicate, bound to a specific deployed contract.
func NewERC20BurnOnlyPredicateTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20BurnOnlyPredicateTransactor, error) {
	contract, err := bindERC20BurnOnlyPredicate(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20BurnOnlyPredicateTransactor{contract: contract}, nil
}

// NewERC20BurnOnlyPredicateFilterer creates a new log filterer instance of ERC20BurnOnlyPredicate, bound to a specific deployed contract.
func NewERC20BurnOnlyPredicateFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20BurnOnlyPredicateFilterer, error) {
	contract, err := bindERC20BurnOnlyPredicate(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20BurnOnlyPredicateFilterer{contract: contract}, nil
}

// bindERC20BurnOnlyPredicate binds a generic wrapper to an already deployed contract.
func bindERC20BurnOnlyPredicate(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ERC20BurnOnlyPredicateMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20BurnOnlyPredicate.Contract.ERC20BurnOnlyPredicateCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.ERC20BurnOnlyPredicateTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.ERC20BurnOnlyPredicateTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20BurnOnlyPredicate.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.contract.Transact(opts, method, params...)
}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCaller) CHAINID(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20BurnOnlyPredicate.contract.Call(opts, &out, "CHAINID")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) CHAINID() (*big.Int, error) {
	return _ERC20BurnOnlyPredicate.Contract.CHAINID(&_ERC20BurnOnlyPredicate.CallOpts)
}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCallerSession) CHAINID() (*big.Int, error) {
	return _ERC20BurnOnlyPredicate.Contract.CHAINID(&_ERC20BurnOnlyPredicate.CallOpts)
}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCaller) InterpretStateUpdate(opts *bind.CallOpts, state []byte) ([]byte, error) {
	var out []interface{}
	err := _ERC20BurnOnlyPredicate.contract.Call(opts, &out, "interpretStateUpdate", state)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) InterpretStateUpdate(state []byte) ([]byte, error) {
	return _ERC20BurnOnlyPredicate.Contract.InterpretStateUpdate(&_ERC20BurnOnlyPredicate.CallOpts, state)
}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCallerSession) InterpretStateUpdate(state []byte) ([]byte, error) {
	return _ERC20BurnOnlyPredicate.Contract.InterpretStateUpdate(&_ERC20BurnOnlyPredicate.CallOpts, state)
}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCaller) NetworkId(opts *bind.CallOpts) ([]byte, error) {
	var out []interface{}
	err := _ERC20BurnOnlyPredicate.contract.Call(opts, &out, "networkId")

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) NetworkId() ([]byte, error) {
	return _ERC20BurnOnlyPredicate.Contract.NetworkId(&_ERC20BurnOnlyPredicate.CallOpts)
}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateCallerSession) NetworkId() ([]byte, error) {
	return _ERC20BurnOnlyPredicate.Contract.NetworkId(&_ERC20BurnOnlyPredicate.CallOpts)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactor) OnFinalizeExit(opts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.contract.Transact(opts, "onFinalizeExit", data)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) OnFinalizeExit(data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.OnFinalizeExit(&_ERC20BurnOnlyPredicate.TransactOpts, data)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactorSession) OnFinalizeExit(data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.OnFinalizeExit(&_ERC20BurnOnlyPredicate.TransactOpts, data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactor) StartExitWithBurntTokens(opts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.contract.Transact(opts, "startExitWithBurntTokens", data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) StartExitWithBurntTokens(data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.StartExitWithBurntTokens(&_ERC20BurnOnlyPredicate.TransactOpts, data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactorSession) StartExitWithBurntTokens(data []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.StartExitWithBurntTokens(&_ERC20BurnOnlyPredicate.TransactOpts, data)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactor) VerifyDeprecation(opts *bind.TransactOpts, exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.contract.Transact(opts, "verifyDeprecation", exit, inputUtxo, challengeData)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateSession) VerifyDeprecation(exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.VerifyDeprecation(&_ERC20BurnOnlyPredicate.TransactOpts, exit, inputUtxo, challengeData)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_ERC20BurnOnlyPredicate *ERC20BurnOnlyPredicateTransactorSession) VerifyDeprecation(exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _ERC20BurnOnlyPredicate.Contract.VerifyDeprecation(&_ERC20BurnOnlyPredicate.TransactOpts, exit, inputUtxo, challengeData)
}
