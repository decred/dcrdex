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

// ERC20PredicateMetaData contains all meta data concerning the ERC20Predicate contract.
var ERC20PredicateMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_proxyTo\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"_new\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"_old\",\"type\":\"address\"}],\"name\":\"ProxyOwnerUpdate\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"_new\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"_old\",\"type\":\"address\"}],\"name\":\"ProxyUpdated\",\"type\":\"event\"},{\"stateMutability\":\"payable\",\"type\":\"fallback\"},{\"inputs\":[],\"name\":\"implementation\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxyOwner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxyType\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"proxyTypeId\",\"type\":\"uint256\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferProxyOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newProxyTo\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"updateAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newProxyTo\",\"type\":\"address\"}],\"name\":\"updateImplementation\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
}

// ERC20PredicateABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20PredicateMetaData.ABI instead.
var ERC20PredicateABI = ERC20PredicateMetaData.ABI

// ERC20Predicate is an auto generated Go binding around an Ethereum contract.
type ERC20Predicate struct {
	ERC20PredicateCaller     // Read-only binding to the contract
	ERC20PredicateTransactor // Write-only binding to the contract
	ERC20PredicateFilterer   // Log filterer for contract events
}

// ERC20PredicateCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20PredicateCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20PredicateTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20PredicateTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20PredicateFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20PredicateFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20PredicateSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20PredicateSession struct {
	Contract     *ERC20Predicate   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ERC20PredicateCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20PredicateCallerSession struct {
	Contract *ERC20PredicateCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// ERC20PredicateTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20PredicateTransactorSession struct {
	Contract     *ERC20PredicateTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// ERC20PredicateRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20PredicateRaw struct {
	Contract *ERC20Predicate // Generic contract binding to access the raw methods on
}

// ERC20PredicateCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20PredicateCallerRaw struct {
	Contract *ERC20PredicateCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20PredicateTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20PredicateTransactorRaw struct {
	Contract *ERC20PredicateTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20Predicate creates a new instance of ERC20Predicate, bound to a specific deployed contract.
func NewERC20Predicate(address common.Address, backend bind.ContractBackend) (*ERC20Predicate, error) {
	contract, err := bindERC20Predicate(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20Predicate{ERC20PredicateCaller: ERC20PredicateCaller{contract: contract}, ERC20PredicateTransactor: ERC20PredicateTransactor{contract: contract}, ERC20PredicateFilterer: ERC20PredicateFilterer{contract: contract}}, nil
}

// NewERC20PredicateCaller creates a new read-only instance of ERC20Predicate, bound to a specific deployed contract.
func NewERC20PredicateCaller(address common.Address, caller bind.ContractCaller) (*ERC20PredicateCaller, error) {
	contract, err := bindERC20Predicate(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20PredicateCaller{contract: contract}, nil
}

// NewERC20PredicateTransactor creates a new write-only instance of ERC20Predicate, bound to a specific deployed contract.
func NewERC20PredicateTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20PredicateTransactor, error) {
	contract, err := bindERC20Predicate(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20PredicateTransactor{contract: contract}, nil
}

// NewERC20PredicateFilterer creates a new log filterer instance of ERC20Predicate, bound to a specific deployed contract.
func NewERC20PredicateFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20PredicateFilterer, error) {
	contract, err := bindERC20Predicate(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20PredicateFilterer{contract: contract}, nil
}

// bindERC20Predicate binds a generic wrapper to an already deployed contract.
func bindERC20Predicate(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ERC20PredicateMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Predicate *ERC20PredicateRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Predicate.Contract.ERC20PredicateCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Predicate *ERC20PredicateRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.ERC20PredicateTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Predicate *ERC20PredicateRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.ERC20PredicateTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Predicate *ERC20PredicateCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Predicate.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Predicate *ERC20PredicateTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Predicate *ERC20PredicateTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.contract.Transact(opts, method, params...)
}

// Implementation is a free data retrieval call binding the contract method 0x5c60da1b.
//
// Solidity: function implementation() view returns(address)
func (_ERC20Predicate *ERC20PredicateCaller) Implementation(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ERC20Predicate.contract.Call(opts, &out, "implementation")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Implementation is a free data retrieval call binding the contract method 0x5c60da1b.
//
// Solidity: function implementation() view returns(address)
func (_ERC20Predicate *ERC20PredicateSession) Implementation() (common.Address, error) {
	return _ERC20Predicate.Contract.Implementation(&_ERC20Predicate.CallOpts)
}

// Implementation is a free data retrieval call binding the contract method 0x5c60da1b.
//
// Solidity: function implementation() view returns(address)
func (_ERC20Predicate *ERC20PredicateCallerSession) Implementation() (common.Address, error) {
	return _ERC20Predicate.Contract.Implementation(&_ERC20Predicate.CallOpts)
}

// ProxyOwner is a free data retrieval call binding the contract method 0x025313a2.
//
// Solidity: function proxyOwner() view returns(address)
func (_ERC20Predicate *ERC20PredicateCaller) ProxyOwner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ERC20Predicate.contract.Call(opts, &out, "proxyOwner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ProxyOwner is a free data retrieval call binding the contract method 0x025313a2.
//
// Solidity: function proxyOwner() view returns(address)
func (_ERC20Predicate *ERC20PredicateSession) ProxyOwner() (common.Address, error) {
	return _ERC20Predicate.Contract.ProxyOwner(&_ERC20Predicate.CallOpts)
}

// ProxyOwner is a free data retrieval call binding the contract method 0x025313a2.
//
// Solidity: function proxyOwner() view returns(address)
func (_ERC20Predicate *ERC20PredicateCallerSession) ProxyOwner() (common.Address, error) {
	return _ERC20Predicate.Contract.ProxyOwner(&_ERC20Predicate.CallOpts)
}

// ProxyType is a free data retrieval call binding the contract method 0x4555d5c9.
//
// Solidity: function proxyType() pure returns(uint256 proxyTypeId)
func (_ERC20Predicate *ERC20PredicateCaller) ProxyType(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Predicate.contract.Call(opts, &out, "proxyType")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProxyType is a free data retrieval call binding the contract method 0x4555d5c9.
//
// Solidity: function proxyType() pure returns(uint256 proxyTypeId)
func (_ERC20Predicate *ERC20PredicateSession) ProxyType() (*big.Int, error) {
	return _ERC20Predicate.Contract.ProxyType(&_ERC20Predicate.CallOpts)
}

// ProxyType is a free data retrieval call binding the contract method 0x4555d5c9.
//
// Solidity: function proxyType() pure returns(uint256 proxyTypeId)
func (_ERC20Predicate *ERC20PredicateCallerSession) ProxyType() (*big.Int, error) {
	return _ERC20Predicate.Contract.ProxyType(&_ERC20Predicate.CallOpts)
}

// TransferProxyOwnership is a paid mutator transaction binding the contract method 0xf1739cae.
//
// Solidity: function transferProxyOwnership(address newOwner) returns()
func (_ERC20Predicate *ERC20PredicateTransactor) TransferProxyOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.contract.Transact(opts, "transferProxyOwnership", newOwner)
}

// TransferProxyOwnership is a paid mutator transaction binding the contract method 0xf1739cae.
//
// Solidity: function transferProxyOwnership(address newOwner) returns()
func (_ERC20Predicate *ERC20PredicateSession) TransferProxyOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.TransferProxyOwnership(&_ERC20Predicate.TransactOpts, newOwner)
}

// TransferProxyOwnership is a paid mutator transaction binding the contract method 0xf1739cae.
//
// Solidity: function transferProxyOwnership(address newOwner) returns()
func (_ERC20Predicate *ERC20PredicateTransactorSession) TransferProxyOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.TransferProxyOwnership(&_ERC20Predicate.TransactOpts, newOwner)
}

// UpdateAndCall is a paid mutator transaction binding the contract method 0xd88ca2c8.
//
// Solidity: function updateAndCall(address _newProxyTo, bytes data) payable returns()
func (_ERC20Predicate *ERC20PredicateTransactor) UpdateAndCall(opts *bind.TransactOpts, _newProxyTo common.Address, data []byte) (*types.Transaction, error) {
	return _ERC20Predicate.contract.Transact(opts, "updateAndCall", _newProxyTo, data)
}

// UpdateAndCall is a paid mutator transaction binding the contract method 0xd88ca2c8.
//
// Solidity: function updateAndCall(address _newProxyTo, bytes data) payable returns()
func (_ERC20Predicate *ERC20PredicateSession) UpdateAndCall(_newProxyTo common.Address, data []byte) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.UpdateAndCall(&_ERC20Predicate.TransactOpts, _newProxyTo, data)
}

// UpdateAndCall is a paid mutator transaction binding the contract method 0xd88ca2c8.
//
// Solidity: function updateAndCall(address _newProxyTo, bytes data) payable returns()
func (_ERC20Predicate *ERC20PredicateTransactorSession) UpdateAndCall(_newProxyTo common.Address, data []byte) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.UpdateAndCall(&_ERC20Predicate.TransactOpts, _newProxyTo, data)
}

// UpdateImplementation is a paid mutator transaction binding the contract method 0x025b22bc.
//
// Solidity: function updateImplementation(address _newProxyTo) returns()
func (_ERC20Predicate *ERC20PredicateTransactor) UpdateImplementation(opts *bind.TransactOpts, _newProxyTo common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.contract.Transact(opts, "updateImplementation", _newProxyTo)
}

// UpdateImplementation is a paid mutator transaction binding the contract method 0x025b22bc.
//
// Solidity: function updateImplementation(address _newProxyTo) returns()
func (_ERC20Predicate *ERC20PredicateSession) UpdateImplementation(_newProxyTo common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.UpdateImplementation(&_ERC20Predicate.TransactOpts, _newProxyTo)
}

// UpdateImplementation is a paid mutator transaction binding the contract method 0x025b22bc.
//
// Solidity: function updateImplementation(address _newProxyTo) returns()
func (_ERC20Predicate *ERC20PredicateTransactorSession) UpdateImplementation(_newProxyTo common.Address) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.UpdateImplementation(&_ERC20Predicate.TransactOpts, _newProxyTo)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ERC20Predicate *ERC20PredicateTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _ERC20Predicate.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ERC20Predicate *ERC20PredicateSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.Fallback(&_ERC20Predicate.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_ERC20Predicate *ERC20PredicateTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _ERC20Predicate.Contract.Fallback(&_ERC20Predicate.TransactOpts, calldata)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_ERC20Predicate *ERC20PredicateTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Predicate.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_ERC20Predicate *ERC20PredicateSession) Receive() (*types.Transaction, error) {
	return _ERC20Predicate.Contract.Receive(&_ERC20Predicate.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_ERC20Predicate *ERC20PredicateTransactorSession) Receive() (*types.Transaction, error) {
	return _ERC20Predicate.Contract.Receive(&_ERC20Predicate.TransactOpts)
}

// ERC20PredicateProxyOwnerUpdateIterator is returned from FilterProxyOwnerUpdate and is used to iterate over the raw logs and unpacked data for ProxyOwnerUpdate events raised by the ERC20Predicate contract.
type ERC20PredicateProxyOwnerUpdateIterator struct {
	Event *ERC20PredicateProxyOwnerUpdate // Event containing the contract specifics and raw log

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
func (it *ERC20PredicateProxyOwnerUpdateIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20PredicateProxyOwnerUpdate)
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
		it.Event = new(ERC20PredicateProxyOwnerUpdate)
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
func (it *ERC20PredicateProxyOwnerUpdateIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20PredicateProxyOwnerUpdateIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20PredicateProxyOwnerUpdate represents a ProxyOwnerUpdate event raised by the ERC20Predicate contract.
type ERC20PredicateProxyOwnerUpdate struct {
	New common.Address
	Old common.Address
	Raw types.Log // Blockchain specific contextual infos
}

// FilterProxyOwnerUpdate is a free log retrieval operation binding the contract event 0xdbe5fd65bcdbae152f24ab660ea68e72b4d4705b57b16e0caae994e214680ee2.
//
// Solidity: event ProxyOwnerUpdate(address _new, address _old)
func (_ERC20Predicate *ERC20PredicateFilterer) FilterProxyOwnerUpdate(opts *bind.FilterOpts) (*ERC20PredicateProxyOwnerUpdateIterator, error) {

	logs, sub, err := _ERC20Predicate.contract.FilterLogs(opts, "ProxyOwnerUpdate")
	if err != nil {
		return nil, err
	}
	return &ERC20PredicateProxyOwnerUpdateIterator{contract: _ERC20Predicate.contract, event: "ProxyOwnerUpdate", logs: logs, sub: sub}, nil
}

// WatchProxyOwnerUpdate is a free log subscription operation binding the contract event 0xdbe5fd65bcdbae152f24ab660ea68e72b4d4705b57b16e0caae994e214680ee2.
//
// Solidity: event ProxyOwnerUpdate(address _new, address _old)
func (_ERC20Predicate *ERC20PredicateFilterer) WatchProxyOwnerUpdate(opts *bind.WatchOpts, sink chan<- *ERC20PredicateProxyOwnerUpdate) (event.Subscription, error) {

	logs, sub, err := _ERC20Predicate.contract.WatchLogs(opts, "ProxyOwnerUpdate")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20PredicateProxyOwnerUpdate)
				if err := _ERC20Predicate.contract.UnpackLog(event, "ProxyOwnerUpdate", log); err != nil {
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

// ParseProxyOwnerUpdate is a log parse operation binding the contract event 0xdbe5fd65bcdbae152f24ab660ea68e72b4d4705b57b16e0caae994e214680ee2.
//
// Solidity: event ProxyOwnerUpdate(address _new, address _old)
func (_ERC20Predicate *ERC20PredicateFilterer) ParseProxyOwnerUpdate(log types.Log) (*ERC20PredicateProxyOwnerUpdate, error) {
	event := new(ERC20PredicateProxyOwnerUpdate)
	if err := _ERC20Predicate.contract.UnpackLog(event, "ProxyOwnerUpdate", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20PredicateProxyUpdatedIterator is returned from FilterProxyUpdated and is used to iterate over the raw logs and unpacked data for ProxyUpdated events raised by the ERC20Predicate contract.
type ERC20PredicateProxyUpdatedIterator struct {
	Event *ERC20PredicateProxyUpdated // Event containing the contract specifics and raw log

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
func (it *ERC20PredicateProxyUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20PredicateProxyUpdated)
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
		it.Event = new(ERC20PredicateProxyUpdated)
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
func (it *ERC20PredicateProxyUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20PredicateProxyUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20PredicateProxyUpdated represents a ProxyUpdated event raised by the ERC20Predicate contract.
type ERC20PredicateProxyUpdated struct {
	New common.Address
	Old common.Address
	Raw types.Log // Blockchain specific contextual infos
}

// FilterProxyUpdated is a free log retrieval operation binding the contract event 0xd32d24edea94f55e932d9a008afc425a8561462d1b1f57bc6e508e9a6b9509e1.
//
// Solidity: event ProxyUpdated(address indexed _new, address indexed _old)
func (_ERC20Predicate *ERC20PredicateFilterer) FilterProxyUpdated(opts *bind.FilterOpts, _new []common.Address, _old []common.Address) (*ERC20PredicateProxyUpdatedIterator, error) {

	var _newRule []interface{}
	for _, _newItem := range _new {
		_newRule = append(_newRule, _newItem)
	}
	var _oldRule []interface{}
	for _, _oldItem := range _old {
		_oldRule = append(_oldRule, _oldItem)
	}

	logs, sub, err := _ERC20Predicate.contract.FilterLogs(opts, "ProxyUpdated", _newRule, _oldRule)
	if err != nil {
		return nil, err
	}
	return &ERC20PredicateProxyUpdatedIterator{contract: _ERC20Predicate.contract, event: "ProxyUpdated", logs: logs, sub: sub}, nil
}

// WatchProxyUpdated is a free log subscription operation binding the contract event 0xd32d24edea94f55e932d9a008afc425a8561462d1b1f57bc6e508e9a6b9509e1.
//
// Solidity: event ProxyUpdated(address indexed _new, address indexed _old)
func (_ERC20Predicate *ERC20PredicateFilterer) WatchProxyUpdated(opts *bind.WatchOpts, sink chan<- *ERC20PredicateProxyUpdated, _new []common.Address, _old []common.Address) (event.Subscription, error) {

	var _newRule []interface{}
	for _, _newItem := range _new {
		_newRule = append(_newRule, _newItem)
	}
	var _oldRule []interface{}
	for _, _oldItem := range _old {
		_oldRule = append(_oldRule, _oldItem)
	}

	logs, sub, err := _ERC20Predicate.contract.WatchLogs(opts, "ProxyUpdated", _newRule, _oldRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20PredicateProxyUpdated)
				if err := _ERC20Predicate.contract.UnpackLog(event, "ProxyUpdated", log); err != nil {
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

// ParseProxyUpdated is a log parse operation binding the contract event 0xd32d24edea94f55e932d9a008afc425a8561462d1b1f57bc6e508e9a6b9509e1.
//
// Solidity: event ProxyUpdated(address indexed _new, address indexed _old)
func (_ERC20Predicate *ERC20PredicateFilterer) ParseProxyUpdated(log types.Log) (*ERC20PredicateProxyUpdated, error) {
	event := new(ERC20PredicateProxyUpdated)
	if err := _ERC20Predicate.contract.UnpackLog(event, "ProxyUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
