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

// Erc20PredicateBurnOnlyMetaData contains all meta data concerning the Erc20PredicateBurnOnly contract.
var Erc20PredicateBurnOnlyMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_withdrawManager\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_depositManager\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"constant\":true,\"inputs\":[],\"name\":\"CHAINID\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"state\",\"type\":\"bytes\"}],\"name\":\"interpretStateUpdate\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"networkId\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"onFinalizeExit\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"startExitWithBurntTokens\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"exit\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"inputUtxo\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"challengeData\",\"type\":\"bytes\"}],\"name\":\"verifyDeprecation\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// Erc20PredicateBurnOnlyABI is the input ABI used to generate the binding from.
// Deprecated: Use Erc20PredicateBurnOnlyMetaData.ABI instead.
var Erc20PredicateBurnOnlyABI = Erc20PredicateBurnOnlyMetaData.ABI

// Erc20PredicateBurnOnly is an auto generated Go binding around an Ethereum contract.
type Erc20PredicateBurnOnly struct {
	Erc20PredicateBurnOnlyCaller     // Read-only binding to the contract
	Erc20PredicateBurnOnlyTransactor // Write-only binding to the contract
	Erc20PredicateBurnOnlyFilterer   // Log filterer for contract events
}

// Erc20PredicateBurnOnlyCaller is an auto generated read-only Go binding around an Ethereum contract.
type Erc20PredicateBurnOnlyCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Erc20PredicateBurnOnlyTransactor is an auto generated write-only Go binding around an Ethereum contract.
type Erc20PredicateBurnOnlyTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Erc20PredicateBurnOnlyFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type Erc20PredicateBurnOnlyFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Erc20PredicateBurnOnlySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type Erc20PredicateBurnOnlySession struct {
	Contract     *Erc20PredicateBurnOnly // Generic contract binding to set the session for
	CallOpts     bind.CallOpts           // Call options to use throughout this session
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// Erc20PredicateBurnOnlyCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type Erc20PredicateBurnOnlyCallerSession struct {
	Contract *Erc20PredicateBurnOnlyCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                 // Call options to use throughout this session
}

// Erc20PredicateBurnOnlyTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type Erc20PredicateBurnOnlyTransactorSession struct {
	Contract     *Erc20PredicateBurnOnlyTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                 // Transaction auth options to use throughout this session
}

// Erc20PredicateBurnOnlyRaw is an auto generated low-level Go binding around an Ethereum contract.
type Erc20PredicateBurnOnlyRaw struct {
	Contract *Erc20PredicateBurnOnly // Generic contract binding to access the raw methods on
}

// Erc20PredicateBurnOnlyCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type Erc20PredicateBurnOnlyCallerRaw struct {
	Contract *Erc20PredicateBurnOnlyCaller // Generic read-only contract binding to access the raw methods on
}

// Erc20PredicateBurnOnlyTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type Erc20PredicateBurnOnlyTransactorRaw struct {
	Contract *Erc20PredicateBurnOnlyTransactor // Generic write-only contract binding to access the raw methods on
}

// NewErc20PredicateBurnOnly creates a new instance of Erc20PredicateBurnOnly, bound to a specific deployed contract.
func NewErc20PredicateBurnOnly(address common.Address, backend bind.ContractBackend) (*Erc20PredicateBurnOnly, error) {
	contract, err := bindErc20PredicateBurnOnly(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Erc20PredicateBurnOnly{Erc20PredicateBurnOnlyCaller: Erc20PredicateBurnOnlyCaller{contract: contract}, Erc20PredicateBurnOnlyTransactor: Erc20PredicateBurnOnlyTransactor{contract: contract}, Erc20PredicateBurnOnlyFilterer: Erc20PredicateBurnOnlyFilterer{contract: contract}}, nil
}

// NewErc20PredicateBurnOnlyCaller creates a new read-only instance of Erc20PredicateBurnOnly, bound to a specific deployed contract.
func NewErc20PredicateBurnOnlyCaller(address common.Address, caller bind.ContractCaller) (*Erc20PredicateBurnOnlyCaller, error) {
	contract, err := bindErc20PredicateBurnOnly(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Erc20PredicateBurnOnlyCaller{contract: contract}, nil
}

// NewErc20PredicateBurnOnlyTransactor creates a new write-only instance of Erc20PredicateBurnOnly, bound to a specific deployed contract.
func NewErc20PredicateBurnOnlyTransactor(address common.Address, transactor bind.ContractTransactor) (*Erc20PredicateBurnOnlyTransactor, error) {
	contract, err := bindErc20PredicateBurnOnly(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &Erc20PredicateBurnOnlyTransactor{contract: contract}, nil
}

// NewErc20PredicateBurnOnlyFilterer creates a new log filterer instance of Erc20PredicateBurnOnly, bound to a specific deployed contract.
func NewErc20PredicateBurnOnlyFilterer(address common.Address, filterer bind.ContractFilterer) (*Erc20PredicateBurnOnlyFilterer, error) {
	contract, err := bindErc20PredicateBurnOnly(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &Erc20PredicateBurnOnlyFilterer{contract: contract}, nil
}

// bindErc20PredicateBurnOnly binds a generic wrapper to an already deployed contract.
func bindErc20PredicateBurnOnly(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := Erc20PredicateBurnOnlyMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Erc20PredicateBurnOnly.Contract.Erc20PredicateBurnOnlyCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.Erc20PredicateBurnOnlyTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.Erc20PredicateBurnOnlyTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Erc20PredicateBurnOnly.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.contract.Transact(opts, method, params...)
}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCaller) CHAINID(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Erc20PredicateBurnOnly.contract.Call(opts, &out, "CHAINID")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) CHAINID() (*big.Int, error) {
	return _Erc20PredicateBurnOnly.Contract.CHAINID(&_Erc20PredicateBurnOnly.CallOpts)
}

// CHAINID is a free data retrieval call binding the contract method 0xcc79f97b.
//
// Solidity: function CHAINID() view returns(uint256)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCallerSession) CHAINID() (*big.Int, error) {
	return _Erc20PredicateBurnOnly.Contract.CHAINID(&_Erc20PredicateBurnOnly.CallOpts)
}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCaller) InterpretStateUpdate(opts *bind.CallOpts, state []byte) ([]byte, error) {
	var out []interface{}
	err := _Erc20PredicateBurnOnly.contract.Call(opts, &out, "interpretStateUpdate", state)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) InterpretStateUpdate(state []byte) ([]byte, error) {
	return _Erc20PredicateBurnOnly.Contract.InterpretStateUpdate(&_Erc20PredicateBurnOnly.CallOpts, state)
}

// InterpretStateUpdate is a free data retrieval call binding the contract method 0x82e3464c.
//
// Solidity: function interpretStateUpdate(bytes state) view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCallerSession) InterpretStateUpdate(state []byte) ([]byte, error) {
	return _Erc20PredicateBurnOnly.Contract.InterpretStateUpdate(&_Erc20PredicateBurnOnly.CallOpts, state)
}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCaller) NetworkId(opts *bind.CallOpts) ([]byte, error) {
	var out []interface{}
	err := _Erc20PredicateBurnOnly.contract.Call(opts, &out, "networkId")

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) NetworkId() ([]byte, error) {
	return _Erc20PredicateBurnOnly.Contract.NetworkId(&_Erc20PredicateBurnOnly.CallOpts)
}

// NetworkId is a free data retrieval call binding the contract method 0x9025e64c.
//
// Solidity: function networkId() view returns(bytes)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyCallerSession) NetworkId() ([]byte, error) {
	return _Erc20PredicateBurnOnly.Contract.NetworkId(&_Erc20PredicateBurnOnly.CallOpts)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactor) OnFinalizeExit(opts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.contract.Transact(opts, "onFinalizeExit", data)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) OnFinalizeExit(data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.OnFinalizeExit(&_Erc20PredicateBurnOnly.TransactOpts, data)
}

// OnFinalizeExit is a paid mutator transaction binding the contract method 0x7bd94e03.
//
// Solidity: function onFinalizeExit(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactorSession) OnFinalizeExit(data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.OnFinalizeExit(&_Erc20PredicateBurnOnly.TransactOpts, data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactor) StartExitWithBurntTokens(opts *bind.TransactOpts, data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.contract.Transact(opts, "startExitWithBurntTokens", data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) StartExitWithBurntTokens(data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.StartExitWithBurntTokens(&_Erc20PredicateBurnOnly.TransactOpts, data)
}

// StartExitWithBurntTokens is a paid mutator transaction binding the contract method 0x7c5264b4.
//
// Solidity: function startExitWithBurntTokens(bytes data) returns()
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactorSession) StartExitWithBurntTokens(data []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.StartExitWithBurntTokens(&_Erc20PredicateBurnOnly.TransactOpts, data)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactor) VerifyDeprecation(opts *bind.TransactOpts, exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.contract.Transact(opts, "verifyDeprecation", exit, inputUtxo, challengeData)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlySession) VerifyDeprecation(exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.VerifyDeprecation(&_Erc20PredicateBurnOnly.TransactOpts, exit, inputUtxo, challengeData)
}

// VerifyDeprecation is a paid mutator transaction binding the contract method 0xec58410c.
//
// Solidity: function verifyDeprecation(bytes exit, bytes inputUtxo, bytes challengeData) returns(bool)
func (_Erc20PredicateBurnOnly *Erc20PredicateBurnOnlyTransactorSession) VerifyDeprecation(exit []byte, inputUtxo []byte, challengeData []byte) (*types.Transaction, error) {
	return _Erc20PredicateBurnOnly.Contract.VerifyDeprecation(&_Erc20PredicateBurnOnly.TransactOpts, exit, inputUtxo, challengeData)
}
