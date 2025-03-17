// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package cctp

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

// TokenMessengerMetaData contains all meta data concerning the TokenMessenger contract.
var TokenMessengerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_messageTransmitter\",\"type\":\"address\"},{\"internalType\":\"uint32\",\"name\":\"_messageBodyVersion\",\"type\":\"uint32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"burnToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"mintRecipient\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"destinationDomain\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"destinationTokenMessenger\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"destinationCaller\",\"type\":\"bytes32\"}],\"name\":\"DepositForBurn\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"localMinter\",\"type\":\"address\"}],\"name\":\"LocalMinterAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"localMinter\",\"type\":\"address\"}],\"name\":\"LocalMinterRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"mintRecipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"mintToken\",\"type\":\"address\"}],\"name\":\"MintAndWithdraw\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferStarted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"domain\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"tokenMessenger\",\"type\":\"bytes32\"}],\"name\":\"RemoteTokenMessengerAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"domain\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"tokenMessenger\",\"type\":\"bytes32\"}],\"name\":\"RemoteTokenMessengerRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"RescuerChanged\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"acceptOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newLocalMinter\",\"type\":\"address\"}],\"name\":\"addLocalMinter\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"domain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"tokenMessenger\",\"type\":\"bytes32\"}],\"name\":\"addRemoteTokenMessenger\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"destinationDomain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"mintRecipient\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"burnToken\",\"type\":\"address\"}],\"name\":\"depositForBurn\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"_nonce\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"destinationDomain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"mintRecipient\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"burnToken\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"destinationCaller\",\"type\":\"bytes32\"}],\"name\":\"depositForBurnWithCaller\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"remoteDomain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"sender\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"messageBody\",\"type\":\"bytes\"}],\"name\":\"handleReceiveMessage\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"localMessageTransmitter\",\"outputs\":[{\"internalType\":\"contractIMessageTransmitter\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"localMinter\",\"outputs\":[{\"internalType\":\"contractITokenMinter\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"messageBodyVersion\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pendingOwner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"name\":\"remoteTokenMessengers\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"removeLocalMinter\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"domain\",\"type\":\"uint32\"}],\"name\":\"removeRemoteTokenMessenger\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"originalMessage\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"originalAttestation\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"newDestinationCaller\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"newMintRecipient\",\"type\":\"bytes32\"}],\"name\":\"replaceDepositForBurn\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"tokenContract\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"rescueERC20\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"rescuer\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"updateRescuer\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// TokenMessengerABI is the input ABI used to generate the binding from.
// Deprecated: Use TokenMessengerMetaData.ABI instead.
var TokenMessengerABI = TokenMessengerMetaData.ABI

// TokenMessenger is an auto generated Go binding around an Ethereum contract.
type TokenMessenger struct {
	TokenMessengerCaller     // Read-only binding to the contract
	TokenMessengerTransactor // Write-only binding to the contract
	TokenMessengerFilterer   // Log filterer for contract events
}

// TokenMessengerCaller is an auto generated read-only Go binding around an Ethereum contract.
type TokenMessengerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenMessengerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type TokenMessengerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenMessengerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type TokenMessengerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TokenMessengerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type TokenMessengerSession struct {
	Contract     *TokenMessenger   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// TokenMessengerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type TokenMessengerCallerSession struct {
	Contract *TokenMessengerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// TokenMessengerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type TokenMessengerTransactorSession struct {
	Contract     *TokenMessengerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// TokenMessengerRaw is an auto generated low-level Go binding around an Ethereum contract.
type TokenMessengerRaw struct {
	Contract *TokenMessenger // Generic contract binding to access the raw methods on
}

// TokenMessengerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type TokenMessengerCallerRaw struct {
	Contract *TokenMessengerCaller // Generic read-only contract binding to access the raw methods on
}

// TokenMessengerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type TokenMessengerTransactorRaw struct {
	Contract *TokenMessengerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewTokenMessenger creates a new instance of TokenMessenger, bound to a specific deployed contract.
func NewTokenMessenger(address common.Address, backend bind.ContractBackend) (*TokenMessenger, error) {
	contract, err := bindTokenMessenger(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &TokenMessenger{TokenMessengerCaller: TokenMessengerCaller{contract: contract}, TokenMessengerTransactor: TokenMessengerTransactor{contract: contract}, TokenMessengerFilterer: TokenMessengerFilterer{contract: contract}}, nil
}

// NewTokenMessengerCaller creates a new read-only instance of TokenMessenger, bound to a specific deployed contract.
func NewTokenMessengerCaller(address common.Address, caller bind.ContractCaller) (*TokenMessengerCaller, error) {
	contract, err := bindTokenMessenger(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerCaller{contract: contract}, nil
}

// NewTokenMessengerTransactor creates a new write-only instance of TokenMessenger, bound to a specific deployed contract.
func NewTokenMessengerTransactor(address common.Address, transactor bind.ContractTransactor) (*TokenMessengerTransactor, error) {
	contract, err := bindTokenMessenger(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerTransactor{contract: contract}, nil
}

// NewTokenMessengerFilterer creates a new log filterer instance of TokenMessenger, bound to a specific deployed contract.
func NewTokenMessengerFilterer(address common.Address, filterer bind.ContractFilterer) (*TokenMessengerFilterer, error) {
	contract, err := bindTokenMessenger(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerFilterer{contract: contract}, nil
}

// bindTokenMessenger binds a generic wrapper to an already deployed contract.
func bindTokenMessenger(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := TokenMessengerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_TokenMessenger *TokenMessengerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _TokenMessenger.Contract.TokenMessengerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_TokenMessenger *TokenMessengerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TokenMessenger.Contract.TokenMessengerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_TokenMessenger *TokenMessengerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _TokenMessenger.Contract.TokenMessengerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_TokenMessenger *TokenMessengerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _TokenMessenger.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_TokenMessenger *TokenMessengerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TokenMessenger.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_TokenMessenger *TokenMessengerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _TokenMessenger.Contract.contract.Transact(opts, method, params...)
}

// LocalMessageTransmitter is a free data retrieval call binding the contract method 0x2c121921.
//
// Solidity: function localMessageTransmitter() view returns(address)
func (_TokenMessenger *TokenMessengerCaller) LocalMessageTransmitter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "localMessageTransmitter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// LocalMessageTransmitter is a free data retrieval call binding the contract method 0x2c121921.
//
// Solidity: function localMessageTransmitter() view returns(address)
func (_TokenMessenger *TokenMessengerSession) LocalMessageTransmitter() (common.Address, error) {
	return _TokenMessenger.Contract.LocalMessageTransmitter(&_TokenMessenger.CallOpts)
}

// LocalMessageTransmitter is a free data retrieval call binding the contract method 0x2c121921.
//
// Solidity: function localMessageTransmitter() view returns(address)
func (_TokenMessenger *TokenMessengerCallerSession) LocalMessageTransmitter() (common.Address, error) {
	return _TokenMessenger.Contract.LocalMessageTransmitter(&_TokenMessenger.CallOpts)
}

// LocalMinter is a free data retrieval call binding the contract method 0xcb75c11c.
//
// Solidity: function localMinter() view returns(address)
func (_TokenMessenger *TokenMessengerCaller) LocalMinter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "localMinter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// LocalMinter is a free data retrieval call binding the contract method 0xcb75c11c.
//
// Solidity: function localMinter() view returns(address)
func (_TokenMessenger *TokenMessengerSession) LocalMinter() (common.Address, error) {
	return _TokenMessenger.Contract.LocalMinter(&_TokenMessenger.CallOpts)
}

// LocalMinter is a free data retrieval call binding the contract method 0xcb75c11c.
//
// Solidity: function localMinter() view returns(address)
func (_TokenMessenger *TokenMessengerCallerSession) LocalMinter() (common.Address, error) {
	return _TokenMessenger.Contract.LocalMinter(&_TokenMessenger.CallOpts)
}

// MessageBodyVersion is a free data retrieval call binding the contract method 0x9cdbb181.
//
// Solidity: function messageBodyVersion() view returns(uint32)
func (_TokenMessenger *TokenMessengerCaller) MessageBodyVersion(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "messageBodyVersion")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// MessageBodyVersion is a free data retrieval call binding the contract method 0x9cdbb181.
//
// Solidity: function messageBodyVersion() view returns(uint32)
func (_TokenMessenger *TokenMessengerSession) MessageBodyVersion() (uint32, error) {
	return _TokenMessenger.Contract.MessageBodyVersion(&_TokenMessenger.CallOpts)
}

// MessageBodyVersion is a free data retrieval call binding the contract method 0x9cdbb181.
//
// Solidity: function messageBodyVersion() view returns(uint32)
func (_TokenMessenger *TokenMessengerCallerSession) MessageBodyVersion() (uint32, error) {
	return _TokenMessenger.Contract.MessageBodyVersion(&_TokenMessenger.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_TokenMessenger *TokenMessengerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_TokenMessenger *TokenMessengerSession) Owner() (common.Address, error) {
	return _TokenMessenger.Contract.Owner(&_TokenMessenger.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_TokenMessenger *TokenMessengerCallerSession) Owner() (common.Address, error) {
	return _TokenMessenger.Contract.Owner(&_TokenMessenger.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_TokenMessenger *TokenMessengerCaller) PendingOwner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "pendingOwner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_TokenMessenger *TokenMessengerSession) PendingOwner() (common.Address, error) {
	return _TokenMessenger.Contract.PendingOwner(&_TokenMessenger.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_TokenMessenger *TokenMessengerCallerSession) PendingOwner() (common.Address, error) {
	return _TokenMessenger.Contract.PendingOwner(&_TokenMessenger.CallOpts)
}

// RemoteTokenMessengers is a free data retrieval call binding the contract method 0x82a5e665.
//
// Solidity: function remoteTokenMessengers(uint32 ) view returns(bytes32)
func (_TokenMessenger *TokenMessengerCaller) RemoteTokenMessengers(opts *bind.CallOpts, arg0 uint32) ([32]byte, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "remoteTokenMessengers", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// RemoteTokenMessengers is a free data retrieval call binding the contract method 0x82a5e665.
//
// Solidity: function remoteTokenMessengers(uint32 ) view returns(bytes32)
func (_TokenMessenger *TokenMessengerSession) RemoteTokenMessengers(arg0 uint32) ([32]byte, error) {
	return _TokenMessenger.Contract.RemoteTokenMessengers(&_TokenMessenger.CallOpts, arg0)
}

// RemoteTokenMessengers is a free data retrieval call binding the contract method 0x82a5e665.
//
// Solidity: function remoteTokenMessengers(uint32 ) view returns(bytes32)
func (_TokenMessenger *TokenMessengerCallerSession) RemoteTokenMessengers(arg0 uint32) ([32]byte, error) {
	return _TokenMessenger.Contract.RemoteTokenMessengers(&_TokenMessenger.CallOpts, arg0)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_TokenMessenger *TokenMessengerCaller) Rescuer(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _TokenMessenger.contract.Call(opts, &out, "rescuer")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_TokenMessenger *TokenMessengerSession) Rescuer() (common.Address, error) {
	return _TokenMessenger.Contract.Rescuer(&_TokenMessenger.CallOpts)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_TokenMessenger *TokenMessengerCallerSession) Rescuer() (common.Address, error) {
	return _TokenMessenger.Contract.Rescuer(&_TokenMessenger.CallOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_TokenMessenger *TokenMessengerTransactor) AcceptOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "acceptOwnership")
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_TokenMessenger *TokenMessengerSession) AcceptOwnership() (*types.Transaction, error) {
	return _TokenMessenger.Contract.AcceptOwnership(&_TokenMessenger.TransactOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_TokenMessenger *TokenMessengerTransactorSession) AcceptOwnership() (*types.Transaction, error) {
	return _TokenMessenger.Contract.AcceptOwnership(&_TokenMessenger.TransactOpts)
}

// AddLocalMinter is a paid mutator transaction binding the contract method 0x8197beb9.
//
// Solidity: function addLocalMinter(address newLocalMinter) returns()
func (_TokenMessenger *TokenMessengerTransactor) AddLocalMinter(opts *bind.TransactOpts, newLocalMinter common.Address) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "addLocalMinter", newLocalMinter)
}

// AddLocalMinter is a paid mutator transaction binding the contract method 0x8197beb9.
//
// Solidity: function addLocalMinter(address newLocalMinter) returns()
func (_TokenMessenger *TokenMessengerSession) AddLocalMinter(newLocalMinter common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.AddLocalMinter(&_TokenMessenger.TransactOpts, newLocalMinter)
}

// AddLocalMinter is a paid mutator transaction binding the contract method 0x8197beb9.
//
// Solidity: function addLocalMinter(address newLocalMinter) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) AddLocalMinter(newLocalMinter common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.AddLocalMinter(&_TokenMessenger.TransactOpts, newLocalMinter)
}

// AddRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xda87e448.
//
// Solidity: function addRemoteTokenMessenger(uint32 domain, bytes32 tokenMessenger) returns()
func (_TokenMessenger *TokenMessengerTransactor) AddRemoteTokenMessenger(opts *bind.TransactOpts, domain uint32, tokenMessenger [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "addRemoteTokenMessenger", domain, tokenMessenger)
}

// AddRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xda87e448.
//
// Solidity: function addRemoteTokenMessenger(uint32 domain, bytes32 tokenMessenger) returns()
func (_TokenMessenger *TokenMessengerSession) AddRemoteTokenMessenger(domain uint32, tokenMessenger [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.AddRemoteTokenMessenger(&_TokenMessenger.TransactOpts, domain, tokenMessenger)
}

// AddRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xda87e448.
//
// Solidity: function addRemoteTokenMessenger(uint32 domain, bytes32 tokenMessenger) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) AddRemoteTokenMessenger(domain uint32, tokenMessenger [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.AddRemoteTokenMessenger(&_TokenMessenger.TransactOpts, domain, tokenMessenger)
}

// DepositForBurn is a paid mutator transaction binding the contract method 0x6fd3504e.
//
// Solidity: function depositForBurn(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken) returns(uint64 _nonce)
func (_TokenMessenger *TokenMessengerTransactor) DepositForBurn(opts *bind.TransactOpts, amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "depositForBurn", amount, destinationDomain, mintRecipient, burnToken)
}

// DepositForBurn is a paid mutator transaction binding the contract method 0x6fd3504e.
//
// Solidity: function depositForBurn(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken) returns(uint64 _nonce)
func (_TokenMessenger *TokenMessengerSession) DepositForBurn(amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.DepositForBurn(&_TokenMessenger.TransactOpts, amount, destinationDomain, mintRecipient, burnToken)
}

// DepositForBurn is a paid mutator transaction binding the contract method 0x6fd3504e.
//
// Solidity: function depositForBurn(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken) returns(uint64 _nonce)
func (_TokenMessenger *TokenMessengerTransactorSession) DepositForBurn(amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.DepositForBurn(&_TokenMessenger.TransactOpts, amount, destinationDomain, mintRecipient, burnToken)
}

// DepositForBurnWithCaller is a paid mutator transaction binding the contract method 0xf856ddb6.
//
// Solidity: function depositForBurnWithCaller(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken, bytes32 destinationCaller) returns(uint64 nonce)
func (_TokenMessenger *TokenMessengerTransactor) DepositForBurnWithCaller(opts *bind.TransactOpts, amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address, destinationCaller [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "depositForBurnWithCaller", amount, destinationDomain, mintRecipient, burnToken, destinationCaller)
}

// DepositForBurnWithCaller is a paid mutator transaction binding the contract method 0xf856ddb6.
//
// Solidity: function depositForBurnWithCaller(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken, bytes32 destinationCaller) returns(uint64 nonce)
func (_TokenMessenger *TokenMessengerSession) DepositForBurnWithCaller(amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address, destinationCaller [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.DepositForBurnWithCaller(&_TokenMessenger.TransactOpts, amount, destinationDomain, mintRecipient, burnToken, destinationCaller)
}

// DepositForBurnWithCaller is a paid mutator transaction binding the contract method 0xf856ddb6.
//
// Solidity: function depositForBurnWithCaller(uint256 amount, uint32 destinationDomain, bytes32 mintRecipient, address burnToken, bytes32 destinationCaller) returns(uint64 nonce)
func (_TokenMessenger *TokenMessengerTransactorSession) DepositForBurnWithCaller(amount *big.Int, destinationDomain uint32, mintRecipient [32]byte, burnToken common.Address, destinationCaller [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.DepositForBurnWithCaller(&_TokenMessenger.TransactOpts, amount, destinationDomain, mintRecipient, burnToken, destinationCaller)
}

// HandleReceiveMessage is a paid mutator transaction binding the contract method 0x96abeb70.
//
// Solidity: function handleReceiveMessage(uint32 remoteDomain, bytes32 sender, bytes messageBody) returns(bool)
func (_TokenMessenger *TokenMessengerTransactor) HandleReceiveMessage(opts *bind.TransactOpts, remoteDomain uint32, sender [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "handleReceiveMessage", remoteDomain, sender, messageBody)
}

// HandleReceiveMessage is a paid mutator transaction binding the contract method 0x96abeb70.
//
// Solidity: function handleReceiveMessage(uint32 remoteDomain, bytes32 sender, bytes messageBody) returns(bool)
func (_TokenMessenger *TokenMessengerSession) HandleReceiveMessage(remoteDomain uint32, sender [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.HandleReceiveMessage(&_TokenMessenger.TransactOpts, remoteDomain, sender, messageBody)
}

// HandleReceiveMessage is a paid mutator transaction binding the contract method 0x96abeb70.
//
// Solidity: function handleReceiveMessage(uint32 remoteDomain, bytes32 sender, bytes messageBody) returns(bool)
func (_TokenMessenger *TokenMessengerTransactorSession) HandleReceiveMessage(remoteDomain uint32, sender [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.HandleReceiveMessage(&_TokenMessenger.TransactOpts, remoteDomain, sender, messageBody)
}

// RemoveLocalMinter is a paid mutator transaction binding the contract method 0x91f17888.
//
// Solidity: function removeLocalMinter() returns()
func (_TokenMessenger *TokenMessengerTransactor) RemoveLocalMinter(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "removeLocalMinter")
}

// RemoveLocalMinter is a paid mutator transaction binding the contract method 0x91f17888.
//
// Solidity: function removeLocalMinter() returns()
func (_TokenMessenger *TokenMessengerSession) RemoveLocalMinter() (*types.Transaction, error) {
	return _TokenMessenger.Contract.RemoveLocalMinter(&_TokenMessenger.TransactOpts)
}

// RemoveLocalMinter is a paid mutator transaction binding the contract method 0x91f17888.
//
// Solidity: function removeLocalMinter() returns()
func (_TokenMessenger *TokenMessengerTransactorSession) RemoveLocalMinter() (*types.Transaction, error) {
	return _TokenMessenger.Contract.RemoveLocalMinter(&_TokenMessenger.TransactOpts)
}

// RemoveRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xf79fd08e.
//
// Solidity: function removeRemoteTokenMessenger(uint32 domain) returns()
func (_TokenMessenger *TokenMessengerTransactor) RemoveRemoteTokenMessenger(opts *bind.TransactOpts, domain uint32) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "removeRemoteTokenMessenger", domain)
}

// RemoveRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xf79fd08e.
//
// Solidity: function removeRemoteTokenMessenger(uint32 domain) returns()
func (_TokenMessenger *TokenMessengerSession) RemoveRemoteTokenMessenger(domain uint32) (*types.Transaction, error) {
	return _TokenMessenger.Contract.RemoveRemoteTokenMessenger(&_TokenMessenger.TransactOpts, domain)
}

// RemoveRemoteTokenMessenger is a paid mutator transaction binding the contract method 0xf79fd08e.
//
// Solidity: function removeRemoteTokenMessenger(uint32 domain) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) RemoveRemoteTokenMessenger(domain uint32) (*types.Transaction, error) {
	return _TokenMessenger.Contract.RemoveRemoteTokenMessenger(&_TokenMessenger.TransactOpts, domain)
}

// ReplaceDepositForBurn is a paid mutator transaction binding the contract method 0x29a78e33.
//
// Solidity: function replaceDepositForBurn(bytes originalMessage, bytes originalAttestation, bytes32 newDestinationCaller, bytes32 newMintRecipient) returns()
func (_TokenMessenger *TokenMessengerTransactor) ReplaceDepositForBurn(opts *bind.TransactOpts, originalMessage []byte, originalAttestation []byte, newDestinationCaller [32]byte, newMintRecipient [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "replaceDepositForBurn", originalMessage, originalAttestation, newDestinationCaller, newMintRecipient)
}

// ReplaceDepositForBurn is a paid mutator transaction binding the contract method 0x29a78e33.
//
// Solidity: function replaceDepositForBurn(bytes originalMessage, bytes originalAttestation, bytes32 newDestinationCaller, bytes32 newMintRecipient) returns()
func (_TokenMessenger *TokenMessengerSession) ReplaceDepositForBurn(originalMessage []byte, originalAttestation []byte, newDestinationCaller [32]byte, newMintRecipient [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.ReplaceDepositForBurn(&_TokenMessenger.TransactOpts, originalMessage, originalAttestation, newDestinationCaller, newMintRecipient)
}

// ReplaceDepositForBurn is a paid mutator transaction binding the contract method 0x29a78e33.
//
// Solidity: function replaceDepositForBurn(bytes originalMessage, bytes originalAttestation, bytes32 newDestinationCaller, bytes32 newMintRecipient) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) ReplaceDepositForBurn(originalMessage []byte, originalAttestation []byte, newDestinationCaller [32]byte, newMintRecipient [32]byte) (*types.Transaction, error) {
	return _TokenMessenger.Contract.ReplaceDepositForBurn(&_TokenMessenger.TransactOpts, originalMessage, originalAttestation, newDestinationCaller, newMintRecipient)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_TokenMessenger *TokenMessengerTransactor) RescueERC20(opts *bind.TransactOpts, tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "rescueERC20", tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_TokenMessenger *TokenMessengerSession) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _TokenMessenger.Contract.RescueERC20(&_TokenMessenger.TransactOpts, tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _TokenMessenger.Contract.RescueERC20(&_TokenMessenger.TransactOpts, tokenContract, to, amount)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_TokenMessenger *TokenMessengerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_TokenMessenger *TokenMessengerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.TransferOwnership(&_TokenMessenger.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.TransferOwnership(&_TokenMessenger.TransactOpts, newOwner)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_TokenMessenger *TokenMessengerTransactor) UpdateRescuer(opts *bind.TransactOpts, newRescuer common.Address) (*types.Transaction, error) {
	return _TokenMessenger.contract.Transact(opts, "updateRescuer", newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_TokenMessenger *TokenMessengerSession) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.UpdateRescuer(&_TokenMessenger.TransactOpts, newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_TokenMessenger *TokenMessengerTransactorSession) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _TokenMessenger.Contract.UpdateRescuer(&_TokenMessenger.TransactOpts, newRescuer)
}

// TokenMessengerDepositForBurnIterator is returned from FilterDepositForBurn and is used to iterate over the raw logs and unpacked data for DepositForBurn events raised by the TokenMessenger contract.
type TokenMessengerDepositForBurnIterator struct {
	Event *TokenMessengerDepositForBurn // Event containing the contract specifics and raw log

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
func (it *TokenMessengerDepositForBurnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerDepositForBurn)
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
		it.Event = new(TokenMessengerDepositForBurn)
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
func (it *TokenMessengerDepositForBurnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerDepositForBurnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerDepositForBurn represents a DepositForBurn event raised by the TokenMessenger contract.
type TokenMessengerDepositForBurn struct {
	Nonce                     uint64
	BurnToken                 common.Address
	Amount                    *big.Int
	Depositor                 common.Address
	MintRecipient             [32]byte
	DestinationDomain         uint32
	DestinationTokenMessenger [32]byte
	DestinationCaller         [32]byte
	Raw                       types.Log // Blockchain specific contextual infos
}

// FilterDepositForBurn is a free log retrieval operation binding the contract event 0x2fa9ca894982930190727e75500a97d8dc500233a5065e0f3126c48fbe0343c0.
//
// Solidity: event DepositForBurn(uint64 indexed nonce, address indexed burnToken, uint256 amount, address indexed depositor, bytes32 mintRecipient, uint32 destinationDomain, bytes32 destinationTokenMessenger, bytes32 destinationCaller)
func (_TokenMessenger *TokenMessengerFilterer) FilterDepositForBurn(opts *bind.FilterOpts, nonce []uint64, burnToken []common.Address, depositor []common.Address) (*TokenMessengerDepositForBurnIterator, error) {

	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}
	var burnTokenRule []interface{}
	for _, burnTokenItem := range burnToken {
		burnTokenRule = append(burnTokenRule, burnTokenItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "DepositForBurn", nonceRule, burnTokenRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerDepositForBurnIterator{contract: _TokenMessenger.contract, event: "DepositForBurn", logs: logs, sub: sub}, nil
}

// WatchDepositForBurn is a free log subscription operation binding the contract event 0x2fa9ca894982930190727e75500a97d8dc500233a5065e0f3126c48fbe0343c0.
//
// Solidity: event DepositForBurn(uint64 indexed nonce, address indexed burnToken, uint256 amount, address indexed depositor, bytes32 mintRecipient, uint32 destinationDomain, bytes32 destinationTokenMessenger, bytes32 destinationCaller)
func (_TokenMessenger *TokenMessengerFilterer) WatchDepositForBurn(opts *bind.WatchOpts, sink chan<- *TokenMessengerDepositForBurn, nonce []uint64, burnToken []common.Address, depositor []common.Address) (event.Subscription, error) {

	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}
	var burnTokenRule []interface{}
	for _, burnTokenItem := range burnToken {
		burnTokenRule = append(burnTokenRule, burnTokenItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "DepositForBurn", nonceRule, burnTokenRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerDepositForBurn)
				if err := _TokenMessenger.contract.UnpackLog(event, "DepositForBurn", log); err != nil {
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

// ParseDepositForBurn is a log parse operation binding the contract event 0x2fa9ca894982930190727e75500a97d8dc500233a5065e0f3126c48fbe0343c0.
//
// Solidity: event DepositForBurn(uint64 indexed nonce, address indexed burnToken, uint256 amount, address indexed depositor, bytes32 mintRecipient, uint32 destinationDomain, bytes32 destinationTokenMessenger, bytes32 destinationCaller)
func (_TokenMessenger *TokenMessengerFilterer) ParseDepositForBurn(log types.Log) (*TokenMessengerDepositForBurn, error) {
	event := new(TokenMessengerDepositForBurn)
	if err := _TokenMessenger.contract.UnpackLog(event, "DepositForBurn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerLocalMinterAddedIterator is returned from FilterLocalMinterAdded and is used to iterate over the raw logs and unpacked data for LocalMinterAdded events raised by the TokenMessenger contract.
type TokenMessengerLocalMinterAddedIterator struct {
	Event *TokenMessengerLocalMinterAdded // Event containing the contract specifics and raw log

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
func (it *TokenMessengerLocalMinterAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerLocalMinterAdded)
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
		it.Event = new(TokenMessengerLocalMinterAdded)
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
func (it *TokenMessengerLocalMinterAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerLocalMinterAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerLocalMinterAdded represents a LocalMinterAdded event raised by the TokenMessenger contract.
type TokenMessengerLocalMinterAdded struct {
	LocalMinter common.Address
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterLocalMinterAdded is a free log retrieval operation binding the contract event 0x109bb3e70cbf1931e295b49e75c67013b85ff80d64e6f1d321f37157b90c3830.
//
// Solidity: event LocalMinterAdded(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) FilterLocalMinterAdded(opts *bind.FilterOpts) (*TokenMessengerLocalMinterAddedIterator, error) {

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "LocalMinterAdded")
	if err != nil {
		return nil, err
	}
	return &TokenMessengerLocalMinterAddedIterator{contract: _TokenMessenger.contract, event: "LocalMinterAdded", logs: logs, sub: sub}, nil
}

// WatchLocalMinterAdded is a free log subscription operation binding the contract event 0x109bb3e70cbf1931e295b49e75c67013b85ff80d64e6f1d321f37157b90c3830.
//
// Solidity: event LocalMinterAdded(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) WatchLocalMinterAdded(opts *bind.WatchOpts, sink chan<- *TokenMessengerLocalMinterAdded) (event.Subscription, error) {

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "LocalMinterAdded")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerLocalMinterAdded)
				if err := _TokenMessenger.contract.UnpackLog(event, "LocalMinterAdded", log); err != nil {
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

// ParseLocalMinterAdded is a log parse operation binding the contract event 0x109bb3e70cbf1931e295b49e75c67013b85ff80d64e6f1d321f37157b90c3830.
//
// Solidity: event LocalMinterAdded(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) ParseLocalMinterAdded(log types.Log) (*TokenMessengerLocalMinterAdded, error) {
	event := new(TokenMessengerLocalMinterAdded)
	if err := _TokenMessenger.contract.UnpackLog(event, "LocalMinterAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerLocalMinterRemovedIterator is returned from FilterLocalMinterRemoved and is used to iterate over the raw logs and unpacked data for LocalMinterRemoved events raised by the TokenMessenger contract.
type TokenMessengerLocalMinterRemovedIterator struct {
	Event *TokenMessengerLocalMinterRemoved // Event containing the contract specifics and raw log

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
func (it *TokenMessengerLocalMinterRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerLocalMinterRemoved)
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
		it.Event = new(TokenMessengerLocalMinterRemoved)
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
func (it *TokenMessengerLocalMinterRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerLocalMinterRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerLocalMinterRemoved represents a LocalMinterRemoved event raised by the TokenMessenger contract.
type TokenMessengerLocalMinterRemoved struct {
	LocalMinter common.Address
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterLocalMinterRemoved is a free log retrieval operation binding the contract event 0x2db49fbf671271826a27b02ebc496209c85fffffb4bccc67430d2a0f22b4d1ac.
//
// Solidity: event LocalMinterRemoved(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) FilterLocalMinterRemoved(opts *bind.FilterOpts) (*TokenMessengerLocalMinterRemovedIterator, error) {

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "LocalMinterRemoved")
	if err != nil {
		return nil, err
	}
	return &TokenMessengerLocalMinterRemovedIterator{contract: _TokenMessenger.contract, event: "LocalMinterRemoved", logs: logs, sub: sub}, nil
}

// WatchLocalMinterRemoved is a free log subscription operation binding the contract event 0x2db49fbf671271826a27b02ebc496209c85fffffb4bccc67430d2a0f22b4d1ac.
//
// Solidity: event LocalMinterRemoved(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) WatchLocalMinterRemoved(opts *bind.WatchOpts, sink chan<- *TokenMessengerLocalMinterRemoved) (event.Subscription, error) {

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "LocalMinterRemoved")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerLocalMinterRemoved)
				if err := _TokenMessenger.contract.UnpackLog(event, "LocalMinterRemoved", log); err != nil {
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

// ParseLocalMinterRemoved is a log parse operation binding the contract event 0x2db49fbf671271826a27b02ebc496209c85fffffb4bccc67430d2a0f22b4d1ac.
//
// Solidity: event LocalMinterRemoved(address localMinter)
func (_TokenMessenger *TokenMessengerFilterer) ParseLocalMinterRemoved(log types.Log) (*TokenMessengerLocalMinterRemoved, error) {
	event := new(TokenMessengerLocalMinterRemoved)
	if err := _TokenMessenger.contract.UnpackLog(event, "LocalMinterRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerMintAndWithdrawIterator is returned from FilterMintAndWithdraw and is used to iterate over the raw logs and unpacked data for MintAndWithdraw events raised by the TokenMessenger contract.
type TokenMessengerMintAndWithdrawIterator struct {
	Event *TokenMessengerMintAndWithdraw // Event containing the contract specifics and raw log

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
func (it *TokenMessengerMintAndWithdrawIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerMintAndWithdraw)
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
		it.Event = new(TokenMessengerMintAndWithdraw)
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
func (it *TokenMessengerMintAndWithdrawIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerMintAndWithdrawIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerMintAndWithdraw represents a MintAndWithdraw event raised by the TokenMessenger contract.
type TokenMessengerMintAndWithdraw struct {
	MintRecipient common.Address
	Amount        *big.Int
	MintToken     common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterMintAndWithdraw is a free log retrieval operation binding the contract event 0x1b2a7ff080b8cb6ff436ce0372e399692bbfb6d4ae5766fd8d58a7b8cc6142e6.
//
// Solidity: event MintAndWithdraw(address indexed mintRecipient, uint256 amount, address indexed mintToken)
func (_TokenMessenger *TokenMessengerFilterer) FilterMintAndWithdraw(opts *bind.FilterOpts, mintRecipient []common.Address, mintToken []common.Address) (*TokenMessengerMintAndWithdrawIterator, error) {

	var mintRecipientRule []interface{}
	for _, mintRecipientItem := range mintRecipient {
		mintRecipientRule = append(mintRecipientRule, mintRecipientItem)
	}

	var mintTokenRule []interface{}
	for _, mintTokenItem := range mintToken {
		mintTokenRule = append(mintTokenRule, mintTokenItem)
	}

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "MintAndWithdraw", mintRecipientRule, mintTokenRule)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerMintAndWithdrawIterator{contract: _TokenMessenger.contract, event: "MintAndWithdraw", logs: logs, sub: sub}, nil
}

// WatchMintAndWithdraw is a free log subscription operation binding the contract event 0x1b2a7ff080b8cb6ff436ce0372e399692bbfb6d4ae5766fd8d58a7b8cc6142e6.
//
// Solidity: event MintAndWithdraw(address indexed mintRecipient, uint256 amount, address indexed mintToken)
func (_TokenMessenger *TokenMessengerFilterer) WatchMintAndWithdraw(opts *bind.WatchOpts, sink chan<- *TokenMessengerMintAndWithdraw, mintRecipient []common.Address, mintToken []common.Address) (event.Subscription, error) {

	var mintRecipientRule []interface{}
	for _, mintRecipientItem := range mintRecipient {
		mintRecipientRule = append(mintRecipientRule, mintRecipientItem)
	}

	var mintTokenRule []interface{}
	for _, mintTokenItem := range mintToken {
		mintTokenRule = append(mintTokenRule, mintTokenItem)
	}

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "MintAndWithdraw", mintRecipientRule, mintTokenRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerMintAndWithdraw)
				if err := _TokenMessenger.contract.UnpackLog(event, "MintAndWithdraw", log); err != nil {
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

// ParseMintAndWithdraw is a log parse operation binding the contract event 0x1b2a7ff080b8cb6ff436ce0372e399692bbfb6d4ae5766fd8d58a7b8cc6142e6.
//
// Solidity: event MintAndWithdraw(address indexed mintRecipient, uint256 amount, address indexed mintToken)
func (_TokenMessenger *TokenMessengerFilterer) ParseMintAndWithdraw(log types.Log) (*TokenMessengerMintAndWithdraw, error) {
	event := new(TokenMessengerMintAndWithdraw)
	if err := _TokenMessenger.contract.UnpackLog(event, "MintAndWithdraw", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerOwnershipTransferStartedIterator is returned from FilterOwnershipTransferStarted and is used to iterate over the raw logs and unpacked data for OwnershipTransferStarted events raised by the TokenMessenger contract.
type TokenMessengerOwnershipTransferStartedIterator struct {
	Event *TokenMessengerOwnershipTransferStarted // Event containing the contract specifics and raw log

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
func (it *TokenMessengerOwnershipTransferStartedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerOwnershipTransferStarted)
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
		it.Event = new(TokenMessengerOwnershipTransferStarted)
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
func (it *TokenMessengerOwnershipTransferStartedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerOwnershipTransferStartedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerOwnershipTransferStarted represents a OwnershipTransferStarted event raised by the TokenMessenger contract.
type TokenMessengerOwnershipTransferStarted struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferStarted is a free log retrieval operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) FilterOwnershipTransferStarted(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*TokenMessengerOwnershipTransferStartedIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerOwnershipTransferStartedIterator{contract: _TokenMessenger.contract, event: "OwnershipTransferStarted", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferStarted is a free log subscription operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) WatchOwnershipTransferStarted(opts *bind.WatchOpts, sink chan<- *TokenMessengerOwnershipTransferStarted, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerOwnershipTransferStarted)
				if err := _TokenMessenger.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
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

// ParseOwnershipTransferStarted is a log parse operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) ParseOwnershipTransferStarted(log types.Log) (*TokenMessengerOwnershipTransferStarted, error) {
	event := new(TokenMessengerOwnershipTransferStarted)
	if err := _TokenMessenger.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the TokenMessenger contract.
type TokenMessengerOwnershipTransferredIterator struct {
	Event *TokenMessengerOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *TokenMessengerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerOwnershipTransferred)
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
		it.Event = new(TokenMessengerOwnershipTransferred)
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
func (it *TokenMessengerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerOwnershipTransferred represents a OwnershipTransferred event raised by the TokenMessenger contract.
type TokenMessengerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*TokenMessengerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerOwnershipTransferredIterator{contract: _TokenMessenger.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *TokenMessengerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerOwnershipTransferred)
				if err := _TokenMessenger.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_TokenMessenger *TokenMessengerFilterer) ParseOwnershipTransferred(log types.Log) (*TokenMessengerOwnershipTransferred, error) {
	event := new(TokenMessengerOwnershipTransferred)
	if err := _TokenMessenger.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerRemoteTokenMessengerAddedIterator is returned from FilterRemoteTokenMessengerAdded and is used to iterate over the raw logs and unpacked data for RemoteTokenMessengerAdded events raised by the TokenMessenger contract.
type TokenMessengerRemoteTokenMessengerAddedIterator struct {
	Event *TokenMessengerRemoteTokenMessengerAdded // Event containing the contract specifics and raw log

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
func (it *TokenMessengerRemoteTokenMessengerAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerRemoteTokenMessengerAdded)
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
		it.Event = new(TokenMessengerRemoteTokenMessengerAdded)
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
func (it *TokenMessengerRemoteTokenMessengerAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerRemoteTokenMessengerAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerRemoteTokenMessengerAdded represents a RemoteTokenMessengerAdded event raised by the TokenMessenger contract.
type TokenMessengerRemoteTokenMessengerAdded struct {
	Domain         uint32
	TokenMessenger [32]byte
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterRemoteTokenMessengerAdded is a free log retrieval operation binding the contract event 0x4bba2b08298cf59661b4895e384cc2ac3962ce2d71f1b7c11bca52e1169f9599.
//
// Solidity: event RemoteTokenMessengerAdded(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) FilterRemoteTokenMessengerAdded(opts *bind.FilterOpts) (*TokenMessengerRemoteTokenMessengerAddedIterator, error) {

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "RemoteTokenMessengerAdded")
	if err != nil {
		return nil, err
	}
	return &TokenMessengerRemoteTokenMessengerAddedIterator{contract: _TokenMessenger.contract, event: "RemoteTokenMessengerAdded", logs: logs, sub: sub}, nil
}

// WatchRemoteTokenMessengerAdded is a free log subscription operation binding the contract event 0x4bba2b08298cf59661b4895e384cc2ac3962ce2d71f1b7c11bca52e1169f9599.
//
// Solidity: event RemoteTokenMessengerAdded(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) WatchRemoteTokenMessengerAdded(opts *bind.WatchOpts, sink chan<- *TokenMessengerRemoteTokenMessengerAdded) (event.Subscription, error) {

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "RemoteTokenMessengerAdded")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerRemoteTokenMessengerAdded)
				if err := _TokenMessenger.contract.UnpackLog(event, "RemoteTokenMessengerAdded", log); err != nil {
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

// ParseRemoteTokenMessengerAdded is a log parse operation binding the contract event 0x4bba2b08298cf59661b4895e384cc2ac3962ce2d71f1b7c11bca52e1169f9599.
//
// Solidity: event RemoteTokenMessengerAdded(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) ParseRemoteTokenMessengerAdded(log types.Log) (*TokenMessengerRemoteTokenMessengerAdded, error) {
	event := new(TokenMessengerRemoteTokenMessengerAdded)
	if err := _TokenMessenger.contract.UnpackLog(event, "RemoteTokenMessengerAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerRemoteTokenMessengerRemovedIterator is returned from FilterRemoteTokenMessengerRemoved and is used to iterate over the raw logs and unpacked data for RemoteTokenMessengerRemoved events raised by the TokenMessenger contract.
type TokenMessengerRemoteTokenMessengerRemovedIterator struct {
	Event *TokenMessengerRemoteTokenMessengerRemoved // Event containing the contract specifics and raw log

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
func (it *TokenMessengerRemoteTokenMessengerRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerRemoteTokenMessengerRemoved)
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
		it.Event = new(TokenMessengerRemoteTokenMessengerRemoved)
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
func (it *TokenMessengerRemoteTokenMessengerRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerRemoteTokenMessengerRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerRemoteTokenMessengerRemoved represents a RemoteTokenMessengerRemoved event raised by the TokenMessenger contract.
type TokenMessengerRemoteTokenMessengerRemoved struct {
	Domain         uint32
	TokenMessenger [32]byte
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterRemoteTokenMessengerRemoved is a free log retrieval operation binding the contract event 0x3dcea012093dbca2bb8ed7fd2b2ff90305ab70bddda8bbb94d4152735a98f0b1.
//
// Solidity: event RemoteTokenMessengerRemoved(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) FilterRemoteTokenMessengerRemoved(opts *bind.FilterOpts) (*TokenMessengerRemoteTokenMessengerRemovedIterator, error) {

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "RemoteTokenMessengerRemoved")
	if err != nil {
		return nil, err
	}
	return &TokenMessengerRemoteTokenMessengerRemovedIterator{contract: _TokenMessenger.contract, event: "RemoteTokenMessengerRemoved", logs: logs, sub: sub}, nil
}

// WatchRemoteTokenMessengerRemoved is a free log subscription operation binding the contract event 0x3dcea012093dbca2bb8ed7fd2b2ff90305ab70bddda8bbb94d4152735a98f0b1.
//
// Solidity: event RemoteTokenMessengerRemoved(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) WatchRemoteTokenMessengerRemoved(opts *bind.WatchOpts, sink chan<- *TokenMessengerRemoteTokenMessengerRemoved) (event.Subscription, error) {

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "RemoteTokenMessengerRemoved")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerRemoteTokenMessengerRemoved)
				if err := _TokenMessenger.contract.UnpackLog(event, "RemoteTokenMessengerRemoved", log); err != nil {
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

// ParseRemoteTokenMessengerRemoved is a log parse operation binding the contract event 0x3dcea012093dbca2bb8ed7fd2b2ff90305ab70bddda8bbb94d4152735a98f0b1.
//
// Solidity: event RemoteTokenMessengerRemoved(uint32 domain, bytes32 tokenMessenger)
func (_TokenMessenger *TokenMessengerFilterer) ParseRemoteTokenMessengerRemoved(log types.Log) (*TokenMessengerRemoteTokenMessengerRemoved, error) {
	event := new(TokenMessengerRemoteTokenMessengerRemoved)
	if err := _TokenMessenger.contract.UnpackLog(event, "RemoteTokenMessengerRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// TokenMessengerRescuerChangedIterator is returned from FilterRescuerChanged and is used to iterate over the raw logs and unpacked data for RescuerChanged events raised by the TokenMessenger contract.
type TokenMessengerRescuerChangedIterator struct {
	Event *TokenMessengerRescuerChanged // Event containing the contract specifics and raw log

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
func (it *TokenMessengerRescuerChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(TokenMessengerRescuerChanged)
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
		it.Event = new(TokenMessengerRescuerChanged)
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
func (it *TokenMessengerRescuerChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *TokenMessengerRescuerChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// TokenMessengerRescuerChanged represents a RescuerChanged event raised by the TokenMessenger contract.
type TokenMessengerRescuerChanged struct {
	NewRescuer common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRescuerChanged is a free log retrieval operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_TokenMessenger *TokenMessengerFilterer) FilterRescuerChanged(opts *bind.FilterOpts, newRescuer []common.Address) (*TokenMessengerRescuerChangedIterator, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _TokenMessenger.contract.FilterLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return &TokenMessengerRescuerChangedIterator{contract: _TokenMessenger.contract, event: "RescuerChanged", logs: logs, sub: sub}, nil
}

// WatchRescuerChanged is a free log subscription operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_TokenMessenger *TokenMessengerFilterer) WatchRescuerChanged(opts *bind.WatchOpts, sink chan<- *TokenMessengerRescuerChanged, newRescuer []common.Address) (event.Subscription, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _TokenMessenger.contract.WatchLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(TokenMessengerRescuerChanged)
				if err := _TokenMessenger.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
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

// ParseRescuerChanged is a log parse operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_TokenMessenger *TokenMessengerFilterer) ParseRescuerChanged(log types.Log) (*TokenMessengerRescuerChanged, error) {
	event := new(TokenMessengerRescuerChanged)
	if err := _TokenMessenger.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
