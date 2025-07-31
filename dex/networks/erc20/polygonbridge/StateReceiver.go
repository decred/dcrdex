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

// StateReceiverMetaData contains all meta data concerning the StateReceiver contract.
var StateReceiverMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"_rootSetter\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"SYSTEM_ADDRESS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"TREE_DEPTH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"commitState\",\"inputs\":[{\"name\":\"syncTime\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"recordBytes\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"success\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"failedStateSyncs\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"failedStateSyncsRoot\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"lastStateId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"leafCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nullifier\",\"inputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"replayCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"replayFailedStateSync\",\"inputs\":[{\"name\":\"stateId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"replayHistoricFailedStateSync\",\"inputs\":[{\"name\":\"proof\",\"type\":\"bytes32[16]\",\"internalType\":\"bytes32[16]\"},{\"name\":\"leafIndex\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"stateId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"receiver\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"rootSetter\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setRootAndLeafCount\",\"inputs\":[{\"name\":\"_root\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"_leafCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"StateCommitted\",\"inputs\":[{\"name\":\"stateId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"success\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"StateSyncReplay\",\"inputs\":[{\"name\":\"stateId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false}]",
}

// StateReceiverABI is the input ABI used to generate the binding from.
// Deprecated: Use StateReceiverMetaData.ABI instead.
var StateReceiverABI = StateReceiverMetaData.ABI

// StateReceiver is an auto generated Go binding around an Ethereum contract.
type StateReceiver struct {
	StateReceiverCaller     // Read-only binding to the contract
	StateReceiverTransactor // Write-only binding to the contract
	StateReceiverFilterer   // Log filterer for contract events
}

// StateReceiverCaller is an auto generated read-only Go binding around an Ethereum contract.
type StateReceiverCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StateReceiverTransactor is an auto generated write-only Go binding around an Ethereum contract.
type StateReceiverTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StateReceiverFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type StateReceiverFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// StateReceiverSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type StateReceiverSession struct {
	Contract     *StateReceiver    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// StateReceiverCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type StateReceiverCallerSession struct {
	Contract *StateReceiverCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// StateReceiverTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type StateReceiverTransactorSession struct {
	Contract     *StateReceiverTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// StateReceiverRaw is an auto generated low-level Go binding around an Ethereum contract.
type StateReceiverRaw struct {
	Contract *StateReceiver // Generic contract binding to access the raw methods on
}

// StateReceiverCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type StateReceiverCallerRaw struct {
	Contract *StateReceiverCaller // Generic read-only contract binding to access the raw methods on
}

// StateReceiverTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type StateReceiverTransactorRaw struct {
	Contract *StateReceiverTransactor // Generic write-only contract binding to access the raw methods on
}

// NewStateReceiver creates a new instance of StateReceiver, bound to a specific deployed contract.
func NewStateReceiver(address common.Address, backend bind.ContractBackend) (*StateReceiver, error) {
	contract, err := bindStateReceiver(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &StateReceiver{StateReceiverCaller: StateReceiverCaller{contract: contract}, StateReceiverTransactor: StateReceiverTransactor{contract: contract}, StateReceiverFilterer: StateReceiverFilterer{contract: contract}}, nil
}

// NewStateReceiverCaller creates a new read-only instance of StateReceiver, bound to a specific deployed contract.
func NewStateReceiverCaller(address common.Address, caller bind.ContractCaller) (*StateReceiverCaller, error) {
	contract, err := bindStateReceiver(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &StateReceiverCaller{contract: contract}, nil
}

// NewStateReceiverTransactor creates a new write-only instance of StateReceiver, bound to a specific deployed contract.
func NewStateReceiverTransactor(address common.Address, transactor bind.ContractTransactor) (*StateReceiverTransactor, error) {
	contract, err := bindStateReceiver(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &StateReceiverTransactor{contract: contract}, nil
}

// NewStateReceiverFilterer creates a new log filterer instance of StateReceiver, bound to a specific deployed contract.
func NewStateReceiverFilterer(address common.Address, filterer bind.ContractFilterer) (*StateReceiverFilterer, error) {
	contract, err := bindStateReceiver(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &StateReceiverFilterer{contract: contract}, nil
}

// bindStateReceiver binds a generic wrapper to an already deployed contract.
func bindStateReceiver(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := StateReceiverMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_StateReceiver *StateReceiverRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _StateReceiver.Contract.StateReceiverCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_StateReceiver *StateReceiverRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _StateReceiver.Contract.StateReceiverTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_StateReceiver *StateReceiverRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _StateReceiver.Contract.StateReceiverTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_StateReceiver *StateReceiverCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _StateReceiver.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_StateReceiver *StateReceiverTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _StateReceiver.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_StateReceiver *StateReceiverTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _StateReceiver.Contract.contract.Transact(opts, method, params...)
}

// SYSTEMADDRESS is a free data retrieval call binding the contract method 0x3434735f.
//
// Solidity: function SYSTEM_ADDRESS() view returns(address)
func (_StateReceiver *StateReceiverCaller) SYSTEMADDRESS(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "SYSTEM_ADDRESS")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// SYSTEMADDRESS is a free data retrieval call binding the contract method 0x3434735f.
//
// Solidity: function SYSTEM_ADDRESS() view returns(address)
func (_StateReceiver *StateReceiverSession) SYSTEMADDRESS() (common.Address, error) {
	return _StateReceiver.Contract.SYSTEMADDRESS(&_StateReceiver.CallOpts)
}

// SYSTEMADDRESS is a free data retrieval call binding the contract method 0x3434735f.
//
// Solidity: function SYSTEM_ADDRESS() view returns(address)
func (_StateReceiver *StateReceiverCallerSession) SYSTEMADDRESS() (common.Address, error) {
	return _StateReceiver.Contract.SYSTEMADDRESS(&_StateReceiver.CallOpts)
}

// TREEDEPTH is a free data retrieval call binding the contract method 0xf1650536.
//
// Solidity: function TREE_DEPTH() view returns(uint256)
func (_StateReceiver *StateReceiverCaller) TREEDEPTH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "TREE_DEPTH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TREEDEPTH is a free data retrieval call binding the contract method 0xf1650536.
//
// Solidity: function TREE_DEPTH() view returns(uint256)
func (_StateReceiver *StateReceiverSession) TREEDEPTH() (*big.Int, error) {
	return _StateReceiver.Contract.TREEDEPTH(&_StateReceiver.CallOpts)
}

// TREEDEPTH is a free data retrieval call binding the contract method 0xf1650536.
//
// Solidity: function TREE_DEPTH() view returns(uint256)
func (_StateReceiver *StateReceiverCallerSession) TREEDEPTH() (*big.Int, error) {
	return _StateReceiver.Contract.TREEDEPTH(&_StateReceiver.CallOpts)
}

// FailedStateSyncs is a free data retrieval call binding the contract method 0x6757e5d9.
//
// Solidity: function failedStateSyncs(uint256 ) view returns(bytes)
func (_StateReceiver *StateReceiverCaller) FailedStateSyncs(opts *bind.CallOpts, arg0 *big.Int) ([]byte, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "failedStateSyncs", arg0)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// FailedStateSyncs is a free data retrieval call binding the contract method 0x6757e5d9.
//
// Solidity: function failedStateSyncs(uint256 ) view returns(bytes)
func (_StateReceiver *StateReceiverSession) FailedStateSyncs(arg0 *big.Int) ([]byte, error) {
	return _StateReceiver.Contract.FailedStateSyncs(&_StateReceiver.CallOpts, arg0)
}

// FailedStateSyncs is a free data retrieval call binding the contract method 0x6757e5d9.
//
// Solidity: function failedStateSyncs(uint256 ) view returns(bytes)
func (_StateReceiver *StateReceiverCallerSession) FailedStateSyncs(arg0 *big.Int) ([]byte, error) {
	return _StateReceiver.Contract.FailedStateSyncs(&_StateReceiver.CallOpts, arg0)
}

// FailedStateSyncsRoot is a free data retrieval call binding the contract method 0xabca2204.
//
// Solidity: function failedStateSyncsRoot() view returns(bytes32)
func (_StateReceiver *StateReceiverCaller) FailedStateSyncsRoot(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "failedStateSyncsRoot")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FailedStateSyncsRoot is a free data retrieval call binding the contract method 0xabca2204.
//
// Solidity: function failedStateSyncsRoot() view returns(bytes32)
func (_StateReceiver *StateReceiverSession) FailedStateSyncsRoot() ([32]byte, error) {
	return _StateReceiver.Contract.FailedStateSyncsRoot(&_StateReceiver.CallOpts)
}

// FailedStateSyncsRoot is a free data retrieval call binding the contract method 0xabca2204.
//
// Solidity: function failedStateSyncsRoot() view returns(bytes32)
func (_StateReceiver *StateReceiverCallerSession) FailedStateSyncsRoot() ([32]byte, error) {
	return _StateReceiver.Contract.FailedStateSyncsRoot(&_StateReceiver.CallOpts)
}

// LastStateId is a free data retrieval call binding the contract method 0x5407ca67.
//
// Solidity: function lastStateId() view returns(uint256)
func (_StateReceiver *StateReceiverCaller) LastStateId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "lastStateId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// LastStateId is a free data retrieval call binding the contract method 0x5407ca67.
//
// Solidity: function lastStateId() view returns(uint256)
func (_StateReceiver *StateReceiverSession) LastStateId() (*big.Int, error) {
	return _StateReceiver.Contract.LastStateId(&_StateReceiver.CallOpts)
}

// LastStateId is a free data retrieval call binding the contract method 0x5407ca67.
//
// Solidity: function lastStateId() view returns(uint256)
func (_StateReceiver *StateReceiverCallerSession) LastStateId() (*big.Int, error) {
	return _StateReceiver.Contract.LastStateId(&_StateReceiver.CallOpts)
}

// LeafCount is a free data retrieval call binding the contract method 0x30e69fc3.
//
// Solidity: function leafCount() view returns(uint256)
func (_StateReceiver *StateReceiverCaller) LeafCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "leafCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// LeafCount is a free data retrieval call binding the contract method 0x30e69fc3.
//
// Solidity: function leafCount() view returns(uint256)
func (_StateReceiver *StateReceiverSession) LeafCount() (*big.Int, error) {
	return _StateReceiver.Contract.LeafCount(&_StateReceiver.CallOpts)
}

// LeafCount is a free data retrieval call binding the contract method 0x30e69fc3.
//
// Solidity: function leafCount() view returns(uint256)
func (_StateReceiver *StateReceiverCallerSession) LeafCount() (*big.Int, error) {
	return _StateReceiver.Contract.LeafCount(&_StateReceiver.CallOpts)
}

// Nullifier is a free data retrieval call binding the contract method 0xd72a0b67.
//
// Solidity: function nullifier(bytes32 ) view returns(bool)
func (_StateReceiver *StateReceiverCaller) Nullifier(opts *bind.CallOpts, arg0 [32]byte) (bool, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "nullifier", arg0)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Nullifier is a free data retrieval call binding the contract method 0xd72a0b67.
//
// Solidity: function nullifier(bytes32 ) view returns(bool)
func (_StateReceiver *StateReceiverSession) Nullifier(arg0 [32]byte) (bool, error) {
	return _StateReceiver.Contract.Nullifier(&_StateReceiver.CallOpts, arg0)
}

// Nullifier is a free data retrieval call binding the contract method 0xd72a0b67.
//
// Solidity: function nullifier(bytes32 ) view returns(bool)
func (_StateReceiver *StateReceiverCallerSession) Nullifier(arg0 [32]byte) (bool, error) {
	return _StateReceiver.Contract.Nullifier(&_StateReceiver.CallOpts, arg0)
}

// ReplayCount is a free data retrieval call binding the contract method 0x942af179.
//
// Solidity: function replayCount() view returns(uint256)
func (_StateReceiver *StateReceiverCaller) ReplayCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "replayCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ReplayCount is a free data retrieval call binding the contract method 0x942af179.
//
// Solidity: function replayCount() view returns(uint256)
func (_StateReceiver *StateReceiverSession) ReplayCount() (*big.Int, error) {
	return _StateReceiver.Contract.ReplayCount(&_StateReceiver.CallOpts)
}

// ReplayCount is a free data retrieval call binding the contract method 0x942af179.
//
// Solidity: function replayCount() view returns(uint256)
func (_StateReceiver *StateReceiverCallerSession) ReplayCount() (*big.Int, error) {
	return _StateReceiver.Contract.ReplayCount(&_StateReceiver.CallOpts)
}

// RootSetter is a free data retrieval call binding the contract method 0x318926f7.
//
// Solidity: function rootSetter() view returns(address)
func (_StateReceiver *StateReceiverCaller) RootSetter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _StateReceiver.contract.Call(opts, &out, "rootSetter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// RootSetter is a free data retrieval call binding the contract method 0x318926f7.
//
// Solidity: function rootSetter() view returns(address)
func (_StateReceiver *StateReceiverSession) RootSetter() (common.Address, error) {
	return _StateReceiver.Contract.RootSetter(&_StateReceiver.CallOpts)
}

// RootSetter is a free data retrieval call binding the contract method 0x318926f7.
//
// Solidity: function rootSetter() view returns(address)
func (_StateReceiver *StateReceiverCallerSession) RootSetter() (common.Address, error) {
	return _StateReceiver.Contract.RootSetter(&_StateReceiver.CallOpts)
}

// CommitState is a paid mutator transaction binding the contract method 0x19494a17.
//
// Solidity: function commitState(uint256 syncTime, bytes recordBytes) returns(bool success)
func (_StateReceiver *StateReceiverTransactor) CommitState(opts *bind.TransactOpts, syncTime *big.Int, recordBytes []byte) (*types.Transaction, error) {
	return _StateReceiver.contract.Transact(opts, "commitState", syncTime, recordBytes)
}

// CommitState is a paid mutator transaction binding the contract method 0x19494a17.
//
// Solidity: function commitState(uint256 syncTime, bytes recordBytes) returns(bool success)
func (_StateReceiver *StateReceiverSession) CommitState(syncTime *big.Int, recordBytes []byte) (*types.Transaction, error) {
	return _StateReceiver.Contract.CommitState(&_StateReceiver.TransactOpts, syncTime, recordBytes)
}

// CommitState is a paid mutator transaction binding the contract method 0x19494a17.
//
// Solidity: function commitState(uint256 syncTime, bytes recordBytes) returns(bool success)
func (_StateReceiver *StateReceiverTransactorSession) CommitState(syncTime *big.Int, recordBytes []byte) (*types.Transaction, error) {
	return _StateReceiver.Contract.CommitState(&_StateReceiver.TransactOpts, syncTime, recordBytes)
}

// ReplayFailedStateSync is a paid mutator transaction binding the contract method 0x03112a17.
//
// Solidity: function replayFailedStateSync(uint256 stateId) returns()
func (_StateReceiver *StateReceiverTransactor) ReplayFailedStateSync(opts *bind.TransactOpts, stateId *big.Int) (*types.Transaction, error) {
	return _StateReceiver.contract.Transact(opts, "replayFailedStateSync", stateId)
}

// ReplayFailedStateSync is a paid mutator transaction binding the contract method 0x03112a17.
//
// Solidity: function replayFailedStateSync(uint256 stateId) returns()
func (_StateReceiver *StateReceiverSession) ReplayFailedStateSync(stateId *big.Int) (*types.Transaction, error) {
	return _StateReceiver.Contract.ReplayFailedStateSync(&_StateReceiver.TransactOpts, stateId)
}

// ReplayFailedStateSync is a paid mutator transaction binding the contract method 0x03112a17.
//
// Solidity: function replayFailedStateSync(uint256 stateId) returns()
func (_StateReceiver *StateReceiverTransactorSession) ReplayFailedStateSync(stateId *big.Int) (*types.Transaction, error) {
	return _StateReceiver.Contract.ReplayFailedStateSync(&_StateReceiver.TransactOpts, stateId)
}

// ReplayHistoricFailedStateSync is a paid mutator transaction binding the contract method 0x51950cd9.
//
// Solidity: function replayHistoricFailedStateSync(bytes32[16] proof, uint256 leafIndex, uint256 stateId, address receiver, bytes data) returns()
func (_StateReceiver *StateReceiverTransactor) ReplayHistoricFailedStateSync(opts *bind.TransactOpts, proof [16][32]byte, leafIndex *big.Int, stateId *big.Int, receiver common.Address, data []byte) (*types.Transaction, error) {
	return _StateReceiver.contract.Transact(opts, "replayHistoricFailedStateSync", proof, leafIndex, stateId, receiver, data)
}

// ReplayHistoricFailedStateSync is a paid mutator transaction binding the contract method 0x51950cd9.
//
// Solidity: function replayHistoricFailedStateSync(bytes32[16] proof, uint256 leafIndex, uint256 stateId, address receiver, bytes data) returns()
func (_StateReceiver *StateReceiverSession) ReplayHistoricFailedStateSync(proof [16][32]byte, leafIndex *big.Int, stateId *big.Int, receiver common.Address, data []byte) (*types.Transaction, error) {
	return _StateReceiver.Contract.ReplayHistoricFailedStateSync(&_StateReceiver.TransactOpts, proof, leafIndex, stateId, receiver, data)
}

// ReplayHistoricFailedStateSync is a paid mutator transaction binding the contract method 0x51950cd9.
//
// Solidity: function replayHistoricFailedStateSync(bytes32[16] proof, uint256 leafIndex, uint256 stateId, address receiver, bytes data) returns()
func (_StateReceiver *StateReceiverTransactorSession) ReplayHistoricFailedStateSync(proof [16][32]byte, leafIndex *big.Int, stateId *big.Int, receiver common.Address, data []byte) (*types.Transaction, error) {
	return _StateReceiver.Contract.ReplayHistoricFailedStateSync(&_StateReceiver.TransactOpts, proof, leafIndex, stateId, receiver, data)
}

// SetRootAndLeafCount is a paid mutator transaction binding the contract method 0xee3a87f2.
//
// Solidity: function setRootAndLeafCount(bytes32 _root, uint256 _leafCount) returns()
func (_StateReceiver *StateReceiverTransactor) SetRootAndLeafCount(opts *bind.TransactOpts, _root [32]byte, _leafCount *big.Int) (*types.Transaction, error) {
	return _StateReceiver.contract.Transact(opts, "setRootAndLeafCount", _root, _leafCount)
}

// SetRootAndLeafCount is a paid mutator transaction binding the contract method 0xee3a87f2.
//
// Solidity: function setRootAndLeafCount(bytes32 _root, uint256 _leafCount) returns()
func (_StateReceiver *StateReceiverSession) SetRootAndLeafCount(_root [32]byte, _leafCount *big.Int) (*types.Transaction, error) {
	return _StateReceiver.Contract.SetRootAndLeafCount(&_StateReceiver.TransactOpts, _root, _leafCount)
}

// SetRootAndLeafCount is a paid mutator transaction binding the contract method 0xee3a87f2.
//
// Solidity: function setRootAndLeafCount(bytes32 _root, uint256 _leafCount) returns()
func (_StateReceiver *StateReceiverTransactorSession) SetRootAndLeafCount(_root [32]byte, _leafCount *big.Int) (*types.Transaction, error) {
	return _StateReceiver.Contract.SetRootAndLeafCount(&_StateReceiver.TransactOpts, _root, _leafCount)
}

// StateReceiverStateCommittedIterator is returned from FilterStateCommitted and is used to iterate over the raw logs and unpacked data for StateCommitted events raised by the StateReceiver contract.
type StateReceiverStateCommittedIterator struct {
	Event *StateReceiverStateCommitted // Event containing the contract specifics and raw log

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
func (it *StateReceiverStateCommittedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(StateReceiverStateCommitted)
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
		it.Event = new(StateReceiverStateCommitted)
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
func (it *StateReceiverStateCommittedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *StateReceiverStateCommittedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// StateReceiverStateCommitted represents a StateCommitted event raised by the StateReceiver contract.
type StateReceiverStateCommitted struct {
	StateId *big.Int
	Success bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterStateCommitted is a free log retrieval operation binding the contract event 0x5a22725590b0a51c923940223f7458512164b1113359a735e86e7f27f44791ee.
//
// Solidity: event StateCommitted(uint256 indexed stateId, bool success)
func (_StateReceiver *StateReceiverFilterer) FilterStateCommitted(opts *bind.FilterOpts, stateId []*big.Int) (*StateReceiverStateCommittedIterator, error) {

	var stateIdRule []interface{}
	for _, stateIdItem := range stateId {
		stateIdRule = append(stateIdRule, stateIdItem)
	}

	logs, sub, err := _StateReceiver.contract.FilterLogs(opts, "StateCommitted", stateIdRule)
	if err != nil {
		return nil, err
	}
	return &StateReceiverStateCommittedIterator{contract: _StateReceiver.contract, event: "StateCommitted", logs: logs, sub: sub}, nil
}

// WatchStateCommitted is a free log subscription operation binding the contract event 0x5a22725590b0a51c923940223f7458512164b1113359a735e86e7f27f44791ee.
//
// Solidity: event StateCommitted(uint256 indexed stateId, bool success)
func (_StateReceiver *StateReceiverFilterer) WatchStateCommitted(opts *bind.WatchOpts, sink chan<- *StateReceiverStateCommitted, stateId []*big.Int) (event.Subscription, error) {

	var stateIdRule []interface{}
	for _, stateIdItem := range stateId {
		stateIdRule = append(stateIdRule, stateIdItem)
	}

	logs, sub, err := _StateReceiver.contract.WatchLogs(opts, "StateCommitted", stateIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(StateReceiverStateCommitted)
				if err := _StateReceiver.contract.UnpackLog(event, "StateCommitted", log); err != nil {
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

// ParseStateCommitted is a log parse operation binding the contract event 0x5a22725590b0a51c923940223f7458512164b1113359a735e86e7f27f44791ee.
//
// Solidity: event StateCommitted(uint256 indexed stateId, bool success)
func (_StateReceiver *StateReceiverFilterer) ParseStateCommitted(log types.Log) (*StateReceiverStateCommitted, error) {
	event := new(StateReceiverStateCommitted)
	if err := _StateReceiver.contract.UnpackLog(event, "StateCommitted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// StateReceiverStateSyncReplayIterator is returned from FilterStateSyncReplay and is used to iterate over the raw logs and unpacked data for StateSyncReplay events raised by the StateReceiver contract.
type StateReceiverStateSyncReplayIterator struct {
	Event *StateReceiverStateSyncReplay // Event containing the contract specifics and raw log

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
func (it *StateReceiverStateSyncReplayIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(StateReceiverStateSyncReplay)
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
		it.Event = new(StateReceiverStateSyncReplay)
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
func (it *StateReceiverStateSyncReplayIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *StateReceiverStateSyncReplayIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// StateReceiverStateSyncReplay represents a StateSyncReplay event raised by the StateReceiver contract.
type StateReceiverStateSyncReplay struct {
	StateId *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterStateSyncReplay is a free log retrieval operation binding the contract event 0x8797144948782adcede8e04bfa0bd8fd56941e0df7508bd02a629b477f7b073a.
//
// Solidity: event StateSyncReplay(uint256 indexed stateId)
func (_StateReceiver *StateReceiverFilterer) FilterStateSyncReplay(opts *bind.FilterOpts, stateId []*big.Int) (*StateReceiverStateSyncReplayIterator, error) {

	var stateIdRule []interface{}
	for _, stateIdItem := range stateId {
		stateIdRule = append(stateIdRule, stateIdItem)
	}

	logs, sub, err := _StateReceiver.contract.FilterLogs(opts, "StateSyncReplay", stateIdRule)
	if err != nil {
		return nil, err
	}
	return &StateReceiverStateSyncReplayIterator{contract: _StateReceiver.contract, event: "StateSyncReplay", logs: logs, sub: sub}, nil
}

// WatchStateSyncReplay is a free log subscription operation binding the contract event 0x8797144948782adcede8e04bfa0bd8fd56941e0df7508bd02a629b477f7b073a.
//
// Solidity: event StateSyncReplay(uint256 indexed stateId)
func (_StateReceiver *StateReceiverFilterer) WatchStateSyncReplay(opts *bind.WatchOpts, sink chan<- *StateReceiverStateSyncReplay, stateId []*big.Int) (event.Subscription, error) {

	var stateIdRule []interface{}
	for _, stateIdItem := range stateId {
		stateIdRule = append(stateIdRule, stateIdItem)
	}

	logs, sub, err := _StateReceiver.contract.WatchLogs(opts, "StateSyncReplay", stateIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(StateReceiverStateSyncReplay)
				if err := _StateReceiver.contract.UnpackLog(event, "StateSyncReplay", log); err != nil {
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

// ParseStateSyncReplay is a log parse operation binding the contract event 0x8797144948782adcede8e04bfa0bd8fd56941e0df7508bd02a629b477f7b073a.
//
// Solidity: event StateSyncReplay(uint256 indexed stateId)
func (_StateReceiver *StateReceiverFilterer) ParseStateSyncReplay(log types.Log) (*StateReceiverStateSyncReplay, error) {
	event := new(StateReceiverStateSyncReplay)
	if err := _StateReceiver.contract.UnpackLog(event, "StateSyncReplay", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
