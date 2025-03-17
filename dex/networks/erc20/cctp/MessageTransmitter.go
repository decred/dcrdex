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

// MessageTransmitterMetaData contains all meta data concerning the MessageTransmitter contract.
var MessageTransmitterMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"_localDomain\",\"type\":\"uint32\"},{\"internalType\":\"address\",\"name\":\"_attester\",\"type\":\"address\"},{\"internalType\":\"uint32\",\"name\":\"_maxMessageBodySize\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"_version\",\"type\":\"uint32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"attester\",\"type\":\"address\"}],\"name\":\"AttesterDisabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"attester\",\"type\":\"address\"}],\"name\":\"AttesterEnabled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousAttesterManager\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newAttesterManager\",\"type\":\"address\"}],\"name\":\"AttesterManagerUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newMaxMessageBodySize\",\"type\":\"uint256\"}],\"name\":\"MaxMessageBodySizeUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"sourceDomain\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"uint64\",\"name\":\"nonce\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"sender\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"messageBody\",\"type\":\"bytes\"}],\"name\":\"MessageReceived\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"MessageSent\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferStarted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[],\"name\":\"Pause\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newAddress\",\"type\":\"address\"}],\"name\":\"PauserChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"RescuerChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldSignatureThreshold\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newSignatureThreshold\",\"type\":\"uint256\"}],\"name\":\"SignatureThresholdUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[],\"name\":\"Unpause\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"acceptOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"attesterManager\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"attester\",\"type\":\"address\"}],\"name\":\"disableAttester\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newAttester\",\"type\":\"address\"}],\"name\":\"enableAttester\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"getEnabledAttester\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getNumEnabledAttesters\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"attester\",\"type\":\"address\"}],\"name\":\"isEnabledAttester\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"localDomain\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"maxMessageBodySize\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"nextAvailableNonce\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pause\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"paused\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pauser\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pendingOwner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"attestation\",\"type\":\"bytes\"}],\"name\":\"receiveMessage\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"originalMessage\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"originalAttestation\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"newMessageBody\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"newDestinationCaller\",\"type\":\"bytes32\"}],\"name\":\"replaceMessage\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"tokenContract\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"rescueERC20\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"rescuer\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"destinationDomain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"messageBody\",\"type\":\"bytes\"}],\"name\":\"sendMessage\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"destinationDomain\",\"type\":\"uint32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"destinationCaller\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"messageBody\",\"type\":\"bytes\"}],\"name\":\"sendMessageWithCaller\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newMaxMessageBodySize\",\"type\":\"uint256\"}],\"name\":\"setMaxMessageBodySize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newSignatureThreshold\",\"type\":\"uint256\"}],\"name\":\"setSignatureThreshold\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"signatureThreshold\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"unpause\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newAttesterManager\",\"type\":\"address\"}],\"name\":\"updateAttesterManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_newPauser\",\"type\":\"address\"}],\"name\":\"updatePauser\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newRescuer\",\"type\":\"address\"}],\"name\":\"updateRescuer\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"usedNonces\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"version\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// MessageTransmitterABI is the input ABI used to generate the binding from.
// Deprecated: Use MessageTransmitterMetaData.ABI instead.
var MessageTransmitterABI = MessageTransmitterMetaData.ABI

// MessageTransmitter is an auto generated Go binding around an Ethereum contract.
type MessageTransmitter struct {
	MessageTransmitterCaller     // Read-only binding to the contract
	MessageTransmitterTransactor // Write-only binding to the contract
	MessageTransmitterFilterer   // Log filterer for contract events
}

// MessageTransmitterCaller is an auto generated read-only Go binding around an Ethereum contract.
type MessageTransmitterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MessageTransmitterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type MessageTransmitterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MessageTransmitterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type MessageTransmitterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MessageTransmitterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type MessageTransmitterSession struct {
	Contract     *MessageTransmitter // Generic contract binding to set the session for
	CallOpts     bind.CallOpts       // Call options to use throughout this session
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// MessageTransmitterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type MessageTransmitterCallerSession struct {
	Contract *MessageTransmitterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts             // Call options to use throughout this session
}

// MessageTransmitterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type MessageTransmitterTransactorSession struct {
	Contract     *MessageTransmitterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts             // Transaction auth options to use throughout this session
}

// MessageTransmitterRaw is an auto generated low-level Go binding around an Ethereum contract.
type MessageTransmitterRaw struct {
	Contract *MessageTransmitter // Generic contract binding to access the raw methods on
}

// MessageTransmitterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type MessageTransmitterCallerRaw struct {
	Contract *MessageTransmitterCaller // Generic read-only contract binding to access the raw methods on
}

// MessageTransmitterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type MessageTransmitterTransactorRaw struct {
	Contract *MessageTransmitterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewMessageTransmitter creates a new instance of MessageTransmitter, bound to a specific deployed contract.
func NewMessageTransmitter(address common.Address, backend bind.ContractBackend) (*MessageTransmitter, error) {
	contract, err := bindMessageTransmitter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitter{MessageTransmitterCaller: MessageTransmitterCaller{contract: contract}, MessageTransmitterTransactor: MessageTransmitterTransactor{contract: contract}, MessageTransmitterFilterer: MessageTransmitterFilterer{contract: contract}}, nil
}

// NewMessageTransmitterCaller creates a new read-only instance of MessageTransmitter, bound to a specific deployed contract.
func NewMessageTransmitterCaller(address common.Address, caller bind.ContractCaller) (*MessageTransmitterCaller, error) {
	contract, err := bindMessageTransmitter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterCaller{contract: contract}, nil
}

// NewMessageTransmitterTransactor creates a new write-only instance of MessageTransmitter, bound to a specific deployed contract.
func NewMessageTransmitterTransactor(address common.Address, transactor bind.ContractTransactor) (*MessageTransmitterTransactor, error) {
	contract, err := bindMessageTransmitter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterTransactor{contract: contract}, nil
}

// NewMessageTransmitterFilterer creates a new log filterer instance of MessageTransmitter, bound to a specific deployed contract.
func NewMessageTransmitterFilterer(address common.Address, filterer bind.ContractFilterer) (*MessageTransmitterFilterer, error) {
	contract, err := bindMessageTransmitter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterFilterer{contract: contract}, nil
}

// bindMessageTransmitter binds a generic wrapper to an already deployed contract.
func bindMessageTransmitter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := MessageTransmitterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MessageTransmitter *MessageTransmitterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MessageTransmitter.Contract.MessageTransmitterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MessageTransmitter *MessageTransmitterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.MessageTransmitterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MessageTransmitter *MessageTransmitterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.MessageTransmitterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MessageTransmitter *MessageTransmitterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MessageTransmitter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MessageTransmitter *MessageTransmitterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MessageTransmitter *MessageTransmitterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.contract.Transact(opts, method, params...)
}

// AttesterManager is a free data retrieval call binding the contract method 0x9b0d94b7.
//
// Solidity: function attesterManager() view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) AttesterManager(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "attesterManager")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// AttesterManager is a free data retrieval call binding the contract method 0x9b0d94b7.
//
// Solidity: function attesterManager() view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) AttesterManager() (common.Address, error) {
	return _MessageTransmitter.Contract.AttesterManager(&_MessageTransmitter.CallOpts)
}

// AttesterManager is a free data retrieval call binding the contract method 0x9b0d94b7.
//
// Solidity: function attesterManager() view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) AttesterManager() (common.Address, error) {
	return _MessageTransmitter.Contract.AttesterManager(&_MessageTransmitter.CallOpts)
}

// GetEnabledAttester is a free data retrieval call binding the contract method 0xbeb673d8.
//
// Solidity: function getEnabledAttester(uint256 index) view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) GetEnabledAttester(opts *bind.CallOpts, index *big.Int) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "getEnabledAttester", index)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetEnabledAttester is a free data retrieval call binding the contract method 0xbeb673d8.
//
// Solidity: function getEnabledAttester(uint256 index) view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) GetEnabledAttester(index *big.Int) (common.Address, error) {
	return _MessageTransmitter.Contract.GetEnabledAttester(&_MessageTransmitter.CallOpts, index)
}

// GetEnabledAttester is a free data retrieval call binding the contract method 0xbeb673d8.
//
// Solidity: function getEnabledAttester(uint256 index) view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) GetEnabledAttester(index *big.Int) (common.Address, error) {
	return _MessageTransmitter.Contract.GetEnabledAttester(&_MessageTransmitter.CallOpts, index)
}

// GetNumEnabledAttesters is a free data retrieval call binding the contract method 0x51079a53.
//
// Solidity: function getNumEnabledAttesters() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCaller) GetNumEnabledAttesters(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "getNumEnabledAttesters")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNumEnabledAttesters is a free data retrieval call binding the contract method 0x51079a53.
//
// Solidity: function getNumEnabledAttesters() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterSession) GetNumEnabledAttesters() (*big.Int, error) {
	return _MessageTransmitter.Contract.GetNumEnabledAttesters(&_MessageTransmitter.CallOpts)
}

// GetNumEnabledAttesters is a free data retrieval call binding the contract method 0x51079a53.
//
// Solidity: function getNumEnabledAttesters() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCallerSession) GetNumEnabledAttesters() (*big.Int, error) {
	return _MessageTransmitter.Contract.GetNumEnabledAttesters(&_MessageTransmitter.CallOpts)
}

// IsEnabledAttester is a free data retrieval call binding the contract method 0x7af82f60.
//
// Solidity: function isEnabledAttester(address attester) view returns(bool)
func (_MessageTransmitter *MessageTransmitterCaller) IsEnabledAttester(opts *bind.CallOpts, attester common.Address) (bool, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "isEnabledAttester", attester)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsEnabledAttester is a free data retrieval call binding the contract method 0x7af82f60.
//
// Solidity: function isEnabledAttester(address attester) view returns(bool)
func (_MessageTransmitter *MessageTransmitterSession) IsEnabledAttester(attester common.Address) (bool, error) {
	return _MessageTransmitter.Contract.IsEnabledAttester(&_MessageTransmitter.CallOpts, attester)
}

// IsEnabledAttester is a free data retrieval call binding the contract method 0x7af82f60.
//
// Solidity: function isEnabledAttester(address attester) view returns(bool)
func (_MessageTransmitter *MessageTransmitterCallerSession) IsEnabledAttester(attester common.Address) (bool, error) {
	return _MessageTransmitter.Contract.IsEnabledAttester(&_MessageTransmitter.CallOpts, attester)
}

// LocalDomain is a free data retrieval call binding the contract method 0x8d3638f4.
//
// Solidity: function localDomain() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterCaller) LocalDomain(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "localDomain")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// LocalDomain is a free data retrieval call binding the contract method 0x8d3638f4.
//
// Solidity: function localDomain() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterSession) LocalDomain() (uint32, error) {
	return _MessageTransmitter.Contract.LocalDomain(&_MessageTransmitter.CallOpts)
}

// LocalDomain is a free data retrieval call binding the contract method 0x8d3638f4.
//
// Solidity: function localDomain() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterCallerSession) LocalDomain() (uint32, error) {
	return _MessageTransmitter.Contract.LocalDomain(&_MessageTransmitter.CallOpts)
}

// MaxMessageBodySize is a free data retrieval call binding the contract method 0xaf47b9bb.
//
// Solidity: function maxMessageBodySize() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCaller) MaxMessageBodySize(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "maxMessageBodySize")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MaxMessageBodySize is a free data retrieval call binding the contract method 0xaf47b9bb.
//
// Solidity: function maxMessageBodySize() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterSession) MaxMessageBodySize() (*big.Int, error) {
	return _MessageTransmitter.Contract.MaxMessageBodySize(&_MessageTransmitter.CallOpts)
}

// MaxMessageBodySize is a free data retrieval call binding the contract method 0xaf47b9bb.
//
// Solidity: function maxMessageBodySize() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCallerSession) MaxMessageBodySize() (*big.Int, error) {
	return _MessageTransmitter.Contract.MaxMessageBodySize(&_MessageTransmitter.CallOpts)
}

// NextAvailableNonce is a free data retrieval call binding the contract method 0x8371744e.
//
// Solidity: function nextAvailableNonce() view returns(uint64)
func (_MessageTransmitter *MessageTransmitterCaller) NextAvailableNonce(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "nextAvailableNonce")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// NextAvailableNonce is a free data retrieval call binding the contract method 0x8371744e.
//
// Solidity: function nextAvailableNonce() view returns(uint64)
func (_MessageTransmitter *MessageTransmitterSession) NextAvailableNonce() (uint64, error) {
	return _MessageTransmitter.Contract.NextAvailableNonce(&_MessageTransmitter.CallOpts)
}

// NextAvailableNonce is a free data retrieval call binding the contract method 0x8371744e.
//
// Solidity: function nextAvailableNonce() view returns(uint64)
func (_MessageTransmitter *MessageTransmitterCallerSession) NextAvailableNonce() (uint64, error) {
	return _MessageTransmitter.Contract.NextAvailableNonce(&_MessageTransmitter.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) Owner() (common.Address, error) {
	return _MessageTransmitter.Contract.Owner(&_MessageTransmitter.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) Owner() (common.Address, error) {
	return _MessageTransmitter.Contract.Owner(&_MessageTransmitter.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_MessageTransmitter *MessageTransmitterCaller) Paused(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "paused")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_MessageTransmitter *MessageTransmitterSession) Paused() (bool, error) {
	return _MessageTransmitter.Contract.Paused(&_MessageTransmitter.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_MessageTransmitter *MessageTransmitterCallerSession) Paused() (bool, error) {
	return _MessageTransmitter.Contract.Paused(&_MessageTransmitter.CallOpts)
}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) Pauser(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "pauser")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) Pauser() (common.Address, error) {
	return _MessageTransmitter.Contract.Pauser(&_MessageTransmitter.CallOpts)
}

// Pauser is a free data retrieval call binding the contract method 0x9fd0506d.
//
// Solidity: function pauser() view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) Pauser() (common.Address, error) {
	return _MessageTransmitter.Contract.Pauser(&_MessageTransmitter.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) PendingOwner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "pendingOwner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) PendingOwner() (common.Address, error) {
	return _MessageTransmitter.Contract.PendingOwner(&_MessageTransmitter.CallOpts)
}

// PendingOwner is a free data retrieval call binding the contract method 0xe30c3978.
//
// Solidity: function pendingOwner() view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) PendingOwner() (common.Address, error) {
	return _MessageTransmitter.Contract.PendingOwner(&_MessageTransmitter.CallOpts)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_MessageTransmitter *MessageTransmitterCaller) Rescuer(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "rescuer")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_MessageTransmitter *MessageTransmitterSession) Rescuer() (common.Address, error) {
	return _MessageTransmitter.Contract.Rescuer(&_MessageTransmitter.CallOpts)
}

// Rescuer is a free data retrieval call binding the contract method 0x38a63183.
//
// Solidity: function rescuer() view returns(address)
func (_MessageTransmitter *MessageTransmitterCallerSession) Rescuer() (common.Address, error) {
	return _MessageTransmitter.Contract.Rescuer(&_MessageTransmitter.CallOpts)
}

// SignatureThreshold is a free data retrieval call binding the contract method 0xa82f2e26.
//
// Solidity: function signatureThreshold() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCaller) SignatureThreshold(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "signatureThreshold")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// SignatureThreshold is a free data retrieval call binding the contract method 0xa82f2e26.
//
// Solidity: function signatureThreshold() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterSession) SignatureThreshold() (*big.Int, error) {
	return _MessageTransmitter.Contract.SignatureThreshold(&_MessageTransmitter.CallOpts)
}

// SignatureThreshold is a free data retrieval call binding the contract method 0xa82f2e26.
//
// Solidity: function signatureThreshold() view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCallerSession) SignatureThreshold() (*big.Int, error) {
	return _MessageTransmitter.Contract.SignatureThreshold(&_MessageTransmitter.CallOpts)
}

// UsedNonces is a free data retrieval call binding the contract method 0xfeb61724.
//
// Solidity: function usedNonces(bytes32 ) view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCaller) UsedNonces(opts *bind.CallOpts, arg0 [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "usedNonces", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// UsedNonces is a free data retrieval call binding the contract method 0xfeb61724.
//
// Solidity: function usedNonces(bytes32 ) view returns(uint256)
func (_MessageTransmitter *MessageTransmitterSession) UsedNonces(arg0 [32]byte) (*big.Int, error) {
	return _MessageTransmitter.Contract.UsedNonces(&_MessageTransmitter.CallOpts, arg0)
}

// UsedNonces is a free data retrieval call binding the contract method 0xfeb61724.
//
// Solidity: function usedNonces(bytes32 ) view returns(uint256)
func (_MessageTransmitter *MessageTransmitterCallerSession) UsedNonces(arg0 [32]byte) (*big.Int, error) {
	return _MessageTransmitter.Contract.UsedNonces(&_MessageTransmitter.CallOpts, arg0)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterCaller) Version(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _MessageTransmitter.contract.Call(opts, &out, "version")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterSession) Version() (uint32, error) {
	return _MessageTransmitter.Contract.Version(&_MessageTransmitter.CallOpts)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() view returns(uint32)
func (_MessageTransmitter *MessageTransmitterCallerSession) Version() (uint32, error) {
	return _MessageTransmitter.Contract.Version(&_MessageTransmitter.CallOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_MessageTransmitter *MessageTransmitterTransactor) AcceptOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "acceptOwnership")
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_MessageTransmitter *MessageTransmitterSession) AcceptOwnership() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.AcceptOwnership(&_MessageTransmitter.TransactOpts)
}

// AcceptOwnership is a paid mutator transaction binding the contract method 0x79ba5097.
//
// Solidity: function acceptOwnership() returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) AcceptOwnership() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.AcceptOwnership(&_MessageTransmitter.TransactOpts)
}

// DisableAttester is a paid mutator transaction binding the contract method 0x2d025080.
//
// Solidity: function disableAttester(address attester) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) DisableAttester(opts *bind.TransactOpts, attester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "disableAttester", attester)
}

// DisableAttester is a paid mutator transaction binding the contract method 0x2d025080.
//
// Solidity: function disableAttester(address attester) returns()
func (_MessageTransmitter *MessageTransmitterSession) DisableAttester(attester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.DisableAttester(&_MessageTransmitter.TransactOpts, attester)
}

// DisableAttester is a paid mutator transaction binding the contract method 0x2d025080.
//
// Solidity: function disableAttester(address attester) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) DisableAttester(attester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.DisableAttester(&_MessageTransmitter.TransactOpts, attester)
}

// EnableAttester is a paid mutator transaction binding the contract method 0xfae36879.
//
// Solidity: function enableAttester(address newAttester) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) EnableAttester(opts *bind.TransactOpts, newAttester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "enableAttester", newAttester)
}

// EnableAttester is a paid mutator transaction binding the contract method 0xfae36879.
//
// Solidity: function enableAttester(address newAttester) returns()
func (_MessageTransmitter *MessageTransmitterSession) EnableAttester(newAttester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.EnableAttester(&_MessageTransmitter.TransactOpts, newAttester)
}

// EnableAttester is a paid mutator transaction binding the contract method 0xfae36879.
//
// Solidity: function enableAttester(address newAttester) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) EnableAttester(newAttester common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.EnableAttester(&_MessageTransmitter.TransactOpts, newAttester)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_MessageTransmitter *MessageTransmitterTransactor) Pause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "pause")
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_MessageTransmitter *MessageTransmitterSession) Pause() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.Pause(&_MessageTransmitter.TransactOpts)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) Pause() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.Pause(&_MessageTransmitter.TransactOpts)
}

// ReceiveMessage is a paid mutator transaction binding the contract method 0x57ecfd28.
//
// Solidity: function receiveMessage(bytes message, bytes attestation) returns(bool success)
func (_MessageTransmitter *MessageTransmitterTransactor) ReceiveMessage(opts *bind.TransactOpts, message []byte, attestation []byte) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "receiveMessage", message, attestation)
}

// ReceiveMessage is a paid mutator transaction binding the contract method 0x57ecfd28.
//
// Solidity: function receiveMessage(bytes message, bytes attestation) returns(bool success)
func (_MessageTransmitter *MessageTransmitterSession) ReceiveMessage(message []byte, attestation []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.ReceiveMessage(&_MessageTransmitter.TransactOpts, message, attestation)
}

// ReceiveMessage is a paid mutator transaction binding the contract method 0x57ecfd28.
//
// Solidity: function receiveMessage(bytes message, bytes attestation) returns(bool success)
func (_MessageTransmitter *MessageTransmitterTransactorSession) ReceiveMessage(message []byte, attestation []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.ReceiveMessage(&_MessageTransmitter.TransactOpts, message, attestation)
}

// ReplaceMessage is a paid mutator transaction binding the contract method 0xb857b774.
//
// Solidity: function replaceMessage(bytes originalMessage, bytes originalAttestation, bytes newMessageBody, bytes32 newDestinationCaller) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) ReplaceMessage(opts *bind.TransactOpts, originalMessage []byte, originalAttestation []byte, newMessageBody []byte, newDestinationCaller [32]byte) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "replaceMessage", originalMessage, originalAttestation, newMessageBody, newDestinationCaller)
}

// ReplaceMessage is a paid mutator transaction binding the contract method 0xb857b774.
//
// Solidity: function replaceMessage(bytes originalMessage, bytes originalAttestation, bytes newMessageBody, bytes32 newDestinationCaller) returns()
func (_MessageTransmitter *MessageTransmitterSession) ReplaceMessage(originalMessage []byte, originalAttestation []byte, newMessageBody []byte, newDestinationCaller [32]byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.ReplaceMessage(&_MessageTransmitter.TransactOpts, originalMessage, originalAttestation, newMessageBody, newDestinationCaller)
}

// ReplaceMessage is a paid mutator transaction binding the contract method 0xb857b774.
//
// Solidity: function replaceMessage(bytes originalMessage, bytes originalAttestation, bytes newMessageBody, bytes32 newDestinationCaller) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) ReplaceMessage(originalMessage []byte, originalAttestation []byte, newMessageBody []byte, newDestinationCaller [32]byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.ReplaceMessage(&_MessageTransmitter.TransactOpts, originalMessage, originalAttestation, newMessageBody, newDestinationCaller)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) RescueERC20(opts *bind.TransactOpts, tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "rescueERC20", tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_MessageTransmitter *MessageTransmitterSession) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.RescueERC20(&_MessageTransmitter.TransactOpts, tokenContract, to, amount)
}

// RescueERC20 is a paid mutator transaction binding the contract method 0xb2118a8d.
//
// Solidity: function rescueERC20(address tokenContract, address to, uint256 amount) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) RescueERC20(tokenContract common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.RescueERC20(&_MessageTransmitter.TransactOpts, tokenContract, to, amount)
}

// SendMessage is a paid mutator transaction binding the contract method 0x0ba469bc.
//
// Solidity: function sendMessage(uint32 destinationDomain, bytes32 recipient, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterTransactor) SendMessage(opts *bind.TransactOpts, destinationDomain uint32, recipient [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "sendMessage", destinationDomain, recipient, messageBody)
}

// SendMessage is a paid mutator transaction binding the contract method 0x0ba469bc.
//
// Solidity: function sendMessage(uint32 destinationDomain, bytes32 recipient, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterSession) SendMessage(destinationDomain uint32, recipient [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SendMessage(&_MessageTransmitter.TransactOpts, destinationDomain, recipient, messageBody)
}

// SendMessage is a paid mutator transaction binding the contract method 0x0ba469bc.
//
// Solidity: function sendMessage(uint32 destinationDomain, bytes32 recipient, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterTransactorSession) SendMessage(destinationDomain uint32, recipient [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SendMessage(&_MessageTransmitter.TransactOpts, destinationDomain, recipient, messageBody)
}

// SendMessageWithCaller is a paid mutator transaction binding the contract method 0xf7259a75.
//
// Solidity: function sendMessageWithCaller(uint32 destinationDomain, bytes32 recipient, bytes32 destinationCaller, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterTransactor) SendMessageWithCaller(opts *bind.TransactOpts, destinationDomain uint32, recipient [32]byte, destinationCaller [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "sendMessageWithCaller", destinationDomain, recipient, destinationCaller, messageBody)
}

// SendMessageWithCaller is a paid mutator transaction binding the contract method 0xf7259a75.
//
// Solidity: function sendMessageWithCaller(uint32 destinationDomain, bytes32 recipient, bytes32 destinationCaller, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterSession) SendMessageWithCaller(destinationDomain uint32, recipient [32]byte, destinationCaller [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SendMessageWithCaller(&_MessageTransmitter.TransactOpts, destinationDomain, recipient, destinationCaller, messageBody)
}

// SendMessageWithCaller is a paid mutator transaction binding the contract method 0xf7259a75.
//
// Solidity: function sendMessageWithCaller(uint32 destinationDomain, bytes32 recipient, bytes32 destinationCaller, bytes messageBody) returns(uint64)
func (_MessageTransmitter *MessageTransmitterTransactorSession) SendMessageWithCaller(destinationDomain uint32, recipient [32]byte, destinationCaller [32]byte, messageBody []byte) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SendMessageWithCaller(&_MessageTransmitter.TransactOpts, destinationDomain, recipient, destinationCaller, messageBody)
}

// SetMaxMessageBodySize is a paid mutator transaction binding the contract method 0x92492c68.
//
// Solidity: function setMaxMessageBodySize(uint256 newMaxMessageBodySize) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) SetMaxMessageBodySize(opts *bind.TransactOpts, newMaxMessageBodySize *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "setMaxMessageBodySize", newMaxMessageBodySize)
}

// SetMaxMessageBodySize is a paid mutator transaction binding the contract method 0x92492c68.
//
// Solidity: function setMaxMessageBodySize(uint256 newMaxMessageBodySize) returns()
func (_MessageTransmitter *MessageTransmitterSession) SetMaxMessageBodySize(newMaxMessageBodySize *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SetMaxMessageBodySize(&_MessageTransmitter.TransactOpts, newMaxMessageBodySize)
}

// SetMaxMessageBodySize is a paid mutator transaction binding the contract method 0x92492c68.
//
// Solidity: function setMaxMessageBodySize(uint256 newMaxMessageBodySize) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) SetMaxMessageBodySize(newMaxMessageBodySize *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SetMaxMessageBodySize(&_MessageTransmitter.TransactOpts, newMaxMessageBodySize)
}

// SetSignatureThreshold is a paid mutator transaction binding the contract method 0xbbde5374.
//
// Solidity: function setSignatureThreshold(uint256 newSignatureThreshold) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) SetSignatureThreshold(opts *bind.TransactOpts, newSignatureThreshold *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "setSignatureThreshold", newSignatureThreshold)
}

// SetSignatureThreshold is a paid mutator transaction binding the contract method 0xbbde5374.
//
// Solidity: function setSignatureThreshold(uint256 newSignatureThreshold) returns()
func (_MessageTransmitter *MessageTransmitterSession) SetSignatureThreshold(newSignatureThreshold *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SetSignatureThreshold(&_MessageTransmitter.TransactOpts, newSignatureThreshold)
}

// SetSignatureThreshold is a paid mutator transaction binding the contract method 0xbbde5374.
//
// Solidity: function setSignatureThreshold(uint256 newSignatureThreshold) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) SetSignatureThreshold(newSignatureThreshold *big.Int) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.SetSignatureThreshold(&_MessageTransmitter.TransactOpts, newSignatureThreshold)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_MessageTransmitter *MessageTransmitterSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.TransferOwnership(&_MessageTransmitter.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.TransferOwnership(&_MessageTransmitter.TransactOpts, newOwner)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_MessageTransmitter *MessageTransmitterTransactor) Unpause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "unpause")
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_MessageTransmitter *MessageTransmitterSession) Unpause() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.Unpause(&_MessageTransmitter.TransactOpts)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) Unpause() (*types.Transaction, error) {
	return _MessageTransmitter.Contract.Unpause(&_MessageTransmitter.TransactOpts)
}

// UpdateAttesterManager is a paid mutator transaction binding the contract method 0xde7769d4.
//
// Solidity: function updateAttesterManager(address newAttesterManager) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) UpdateAttesterManager(opts *bind.TransactOpts, newAttesterManager common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "updateAttesterManager", newAttesterManager)
}

// UpdateAttesterManager is a paid mutator transaction binding the contract method 0xde7769d4.
//
// Solidity: function updateAttesterManager(address newAttesterManager) returns()
func (_MessageTransmitter *MessageTransmitterSession) UpdateAttesterManager(newAttesterManager common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdateAttesterManager(&_MessageTransmitter.TransactOpts, newAttesterManager)
}

// UpdateAttesterManager is a paid mutator transaction binding the contract method 0xde7769d4.
//
// Solidity: function updateAttesterManager(address newAttesterManager) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) UpdateAttesterManager(newAttesterManager common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdateAttesterManager(&_MessageTransmitter.TransactOpts, newAttesterManager)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) UpdatePauser(opts *bind.TransactOpts, _newPauser common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "updatePauser", _newPauser)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_MessageTransmitter *MessageTransmitterSession) UpdatePauser(_newPauser common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdatePauser(&_MessageTransmitter.TransactOpts, _newPauser)
}

// UpdatePauser is a paid mutator transaction binding the contract method 0x554bab3c.
//
// Solidity: function updatePauser(address _newPauser) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) UpdatePauser(_newPauser common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdatePauser(&_MessageTransmitter.TransactOpts, _newPauser)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_MessageTransmitter *MessageTransmitterTransactor) UpdateRescuer(opts *bind.TransactOpts, newRescuer common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.contract.Transact(opts, "updateRescuer", newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_MessageTransmitter *MessageTransmitterSession) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdateRescuer(&_MessageTransmitter.TransactOpts, newRescuer)
}

// UpdateRescuer is a paid mutator transaction binding the contract method 0x2ab60045.
//
// Solidity: function updateRescuer(address newRescuer) returns()
func (_MessageTransmitter *MessageTransmitterTransactorSession) UpdateRescuer(newRescuer common.Address) (*types.Transaction, error) {
	return _MessageTransmitter.Contract.UpdateRescuer(&_MessageTransmitter.TransactOpts, newRescuer)
}

// MessageTransmitterAttesterDisabledIterator is returned from FilterAttesterDisabled and is used to iterate over the raw logs and unpacked data for AttesterDisabled events raised by the MessageTransmitter contract.
type MessageTransmitterAttesterDisabledIterator struct {
	Event *MessageTransmitterAttesterDisabled // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterAttesterDisabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterAttesterDisabled)
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
		it.Event = new(MessageTransmitterAttesterDisabled)
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
func (it *MessageTransmitterAttesterDisabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterAttesterDisabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterAttesterDisabled represents a AttesterDisabled event raised by the MessageTransmitter contract.
type MessageTransmitterAttesterDisabled struct {
	Attester common.Address
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterAttesterDisabled is a free log retrieval operation binding the contract event 0x78e573a18c75957b7cadaab01511aa1c19a659f06ecf53e01de37ed92d3261fc.
//
// Solidity: event AttesterDisabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterAttesterDisabled(opts *bind.FilterOpts, attester []common.Address) (*MessageTransmitterAttesterDisabledIterator, error) {

	var attesterRule []interface{}
	for _, attesterItem := range attester {
		attesterRule = append(attesterRule, attesterItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "AttesterDisabled", attesterRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterAttesterDisabledIterator{contract: _MessageTransmitter.contract, event: "AttesterDisabled", logs: logs, sub: sub}, nil
}

// WatchAttesterDisabled is a free log subscription operation binding the contract event 0x78e573a18c75957b7cadaab01511aa1c19a659f06ecf53e01de37ed92d3261fc.
//
// Solidity: event AttesterDisabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchAttesterDisabled(opts *bind.WatchOpts, sink chan<- *MessageTransmitterAttesterDisabled, attester []common.Address) (event.Subscription, error) {

	var attesterRule []interface{}
	for _, attesterItem := range attester {
		attesterRule = append(attesterRule, attesterItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "AttesterDisabled", attesterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterAttesterDisabled)
				if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterDisabled", log); err != nil {
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

// ParseAttesterDisabled is a log parse operation binding the contract event 0x78e573a18c75957b7cadaab01511aa1c19a659f06ecf53e01de37ed92d3261fc.
//
// Solidity: event AttesterDisabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseAttesterDisabled(log types.Log) (*MessageTransmitterAttesterDisabled, error) {
	event := new(MessageTransmitterAttesterDisabled)
	if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterDisabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterAttesterEnabledIterator is returned from FilterAttesterEnabled and is used to iterate over the raw logs and unpacked data for AttesterEnabled events raised by the MessageTransmitter contract.
type MessageTransmitterAttesterEnabledIterator struct {
	Event *MessageTransmitterAttesterEnabled // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterAttesterEnabledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterAttesterEnabled)
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
		it.Event = new(MessageTransmitterAttesterEnabled)
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
func (it *MessageTransmitterAttesterEnabledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterAttesterEnabledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterAttesterEnabled represents a AttesterEnabled event raised by the MessageTransmitter contract.
type MessageTransmitterAttesterEnabled struct {
	Attester common.Address
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterAttesterEnabled is a free log retrieval operation binding the contract event 0x5b99bab45c72ce67e89466dbc47480b9c1fde1400e7268bbf463b8354ee4653f.
//
// Solidity: event AttesterEnabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterAttesterEnabled(opts *bind.FilterOpts, attester []common.Address) (*MessageTransmitterAttesterEnabledIterator, error) {

	var attesterRule []interface{}
	for _, attesterItem := range attester {
		attesterRule = append(attesterRule, attesterItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "AttesterEnabled", attesterRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterAttesterEnabledIterator{contract: _MessageTransmitter.contract, event: "AttesterEnabled", logs: logs, sub: sub}, nil
}

// WatchAttesterEnabled is a free log subscription operation binding the contract event 0x5b99bab45c72ce67e89466dbc47480b9c1fde1400e7268bbf463b8354ee4653f.
//
// Solidity: event AttesterEnabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchAttesterEnabled(opts *bind.WatchOpts, sink chan<- *MessageTransmitterAttesterEnabled, attester []common.Address) (event.Subscription, error) {

	var attesterRule []interface{}
	for _, attesterItem := range attester {
		attesterRule = append(attesterRule, attesterItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "AttesterEnabled", attesterRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterAttesterEnabled)
				if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterEnabled", log); err != nil {
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

// ParseAttesterEnabled is a log parse operation binding the contract event 0x5b99bab45c72ce67e89466dbc47480b9c1fde1400e7268bbf463b8354ee4653f.
//
// Solidity: event AttesterEnabled(address indexed attester)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseAttesterEnabled(log types.Log) (*MessageTransmitterAttesterEnabled, error) {
	event := new(MessageTransmitterAttesterEnabled)
	if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterEnabled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterAttesterManagerUpdatedIterator is returned from FilterAttesterManagerUpdated and is used to iterate over the raw logs and unpacked data for AttesterManagerUpdated events raised by the MessageTransmitter contract.
type MessageTransmitterAttesterManagerUpdatedIterator struct {
	Event *MessageTransmitterAttesterManagerUpdated // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterAttesterManagerUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterAttesterManagerUpdated)
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
		it.Event = new(MessageTransmitterAttesterManagerUpdated)
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
func (it *MessageTransmitterAttesterManagerUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterAttesterManagerUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterAttesterManagerUpdated represents a AttesterManagerUpdated event raised by the MessageTransmitter contract.
type MessageTransmitterAttesterManagerUpdated struct {
	PreviousAttesterManager common.Address
	NewAttesterManager      common.Address
	Raw                     types.Log // Blockchain specific contextual infos
}

// FilterAttesterManagerUpdated is a free log retrieval operation binding the contract event 0x0cee1b7ae04f3c788dd3a46c6fa677eb95b913611ef7ab59524fdc09d3460219.
//
// Solidity: event AttesterManagerUpdated(address indexed previousAttesterManager, address indexed newAttesterManager)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterAttesterManagerUpdated(opts *bind.FilterOpts, previousAttesterManager []common.Address, newAttesterManager []common.Address) (*MessageTransmitterAttesterManagerUpdatedIterator, error) {

	var previousAttesterManagerRule []interface{}
	for _, previousAttesterManagerItem := range previousAttesterManager {
		previousAttesterManagerRule = append(previousAttesterManagerRule, previousAttesterManagerItem)
	}
	var newAttesterManagerRule []interface{}
	for _, newAttesterManagerItem := range newAttesterManager {
		newAttesterManagerRule = append(newAttesterManagerRule, newAttesterManagerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "AttesterManagerUpdated", previousAttesterManagerRule, newAttesterManagerRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterAttesterManagerUpdatedIterator{contract: _MessageTransmitter.contract, event: "AttesterManagerUpdated", logs: logs, sub: sub}, nil
}

// WatchAttesterManagerUpdated is a free log subscription operation binding the contract event 0x0cee1b7ae04f3c788dd3a46c6fa677eb95b913611ef7ab59524fdc09d3460219.
//
// Solidity: event AttesterManagerUpdated(address indexed previousAttesterManager, address indexed newAttesterManager)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchAttesterManagerUpdated(opts *bind.WatchOpts, sink chan<- *MessageTransmitterAttesterManagerUpdated, previousAttesterManager []common.Address, newAttesterManager []common.Address) (event.Subscription, error) {

	var previousAttesterManagerRule []interface{}
	for _, previousAttesterManagerItem := range previousAttesterManager {
		previousAttesterManagerRule = append(previousAttesterManagerRule, previousAttesterManagerItem)
	}
	var newAttesterManagerRule []interface{}
	for _, newAttesterManagerItem := range newAttesterManager {
		newAttesterManagerRule = append(newAttesterManagerRule, newAttesterManagerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "AttesterManagerUpdated", previousAttesterManagerRule, newAttesterManagerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterAttesterManagerUpdated)
				if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterManagerUpdated", log); err != nil {
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

// ParseAttesterManagerUpdated is a log parse operation binding the contract event 0x0cee1b7ae04f3c788dd3a46c6fa677eb95b913611ef7ab59524fdc09d3460219.
//
// Solidity: event AttesterManagerUpdated(address indexed previousAttesterManager, address indexed newAttesterManager)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseAttesterManagerUpdated(log types.Log) (*MessageTransmitterAttesterManagerUpdated, error) {
	event := new(MessageTransmitterAttesterManagerUpdated)
	if err := _MessageTransmitter.contract.UnpackLog(event, "AttesterManagerUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterMaxMessageBodySizeUpdatedIterator is returned from FilterMaxMessageBodySizeUpdated and is used to iterate over the raw logs and unpacked data for MaxMessageBodySizeUpdated events raised by the MessageTransmitter contract.
type MessageTransmitterMaxMessageBodySizeUpdatedIterator struct {
	Event *MessageTransmitterMaxMessageBodySizeUpdated // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterMaxMessageBodySizeUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterMaxMessageBodySizeUpdated)
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
		it.Event = new(MessageTransmitterMaxMessageBodySizeUpdated)
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
func (it *MessageTransmitterMaxMessageBodySizeUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterMaxMessageBodySizeUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterMaxMessageBodySizeUpdated represents a MaxMessageBodySizeUpdated event raised by the MessageTransmitter contract.
type MessageTransmitterMaxMessageBodySizeUpdated struct {
	NewMaxMessageBodySize *big.Int
	Raw                   types.Log // Blockchain specific contextual infos
}

// FilterMaxMessageBodySizeUpdated is a free log retrieval operation binding the contract event 0xb13bf6bebed03d1b318e3ea32e4b2a3ad9f5e2312cdf340a2f4bbfaee39f928d.
//
// Solidity: event MaxMessageBodySizeUpdated(uint256 newMaxMessageBodySize)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterMaxMessageBodySizeUpdated(opts *bind.FilterOpts) (*MessageTransmitterMaxMessageBodySizeUpdatedIterator, error) {

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "MaxMessageBodySizeUpdated")
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterMaxMessageBodySizeUpdatedIterator{contract: _MessageTransmitter.contract, event: "MaxMessageBodySizeUpdated", logs: logs, sub: sub}, nil
}

// WatchMaxMessageBodySizeUpdated is a free log subscription operation binding the contract event 0xb13bf6bebed03d1b318e3ea32e4b2a3ad9f5e2312cdf340a2f4bbfaee39f928d.
//
// Solidity: event MaxMessageBodySizeUpdated(uint256 newMaxMessageBodySize)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchMaxMessageBodySizeUpdated(opts *bind.WatchOpts, sink chan<- *MessageTransmitterMaxMessageBodySizeUpdated) (event.Subscription, error) {

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "MaxMessageBodySizeUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterMaxMessageBodySizeUpdated)
				if err := _MessageTransmitter.contract.UnpackLog(event, "MaxMessageBodySizeUpdated", log); err != nil {
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

// ParseMaxMessageBodySizeUpdated is a log parse operation binding the contract event 0xb13bf6bebed03d1b318e3ea32e4b2a3ad9f5e2312cdf340a2f4bbfaee39f928d.
//
// Solidity: event MaxMessageBodySizeUpdated(uint256 newMaxMessageBodySize)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseMaxMessageBodySizeUpdated(log types.Log) (*MessageTransmitterMaxMessageBodySizeUpdated, error) {
	event := new(MessageTransmitterMaxMessageBodySizeUpdated)
	if err := _MessageTransmitter.contract.UnpackLog(event, "MaxMessageBodySizeUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterMessageReceivedIterator is returned from FilterMessageReceived and is used to iterate over the raw logs and unpacked data for MessageReceived events raised by the MessageTransmitter contract.
type MessageTransmitterMessageReceivedIterator struct {
	Event *MessageTransmitterMessageReceived // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterMessageReceivedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterMessageReceived)
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
		it.Event = new(MessageTransmitterMessageReceived)
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
func (it *MessageTransmitterMessageReceivedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterMessageReceivedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterMessageReceived represents a MessageReceived event raised by the MessageTransmitter contract.
type MessageTransmitterMessageReceived struct {
	Caller       common.Address
	SourceDomain uint32
	Nonce        uint64
	Sender       [32]byte
	MessageBody  []byte
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterMessageReceived is a free log retrieval operation binding the contract event 0x58200b4c34ae05ee816d710053fff3fb75af4395915d3d2a771b24aa10e3cc5d.
//
// Solidity: event MessageReceived(address indexed caller, uint32 sourceDomain, uint64 indexed nonce, bytes32 sender, bytes messageBody)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterMessageReceived(opts *bind.FilterOpts, caller []common.Address, nonce []uint64) (*MessageTransmitterMessageReceivedIterator, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "MessageReceived", callerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterMessageReceivedIterator{contract: _MessageTransmitter.contract, event: "MessageReceived", logs: logs, sub: sub}, nil
}

// WatchMessageReceived is a free log subscription operation binding the contract event 0x58200b4c34ae05ee816d710053fff3fb75af4395915d3d2a771b24aa10e3cc5d.
//
// Solidity: event MessageReceived(address indexed caller, uint32 sourceDomain, uint64 indexed nonce, bytes32 sender, bytes messageBody)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchMessageReceived(opts *bind.WatchOpts, sink chan<- *MessageTransmitterMessageReceived, caller []common.Address, nonce []uint64) (event.Subscription, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	var nonceRule []interface{}
	for _, nonceItem := range nonce {
		nonceRule = append(nonceRule, nonceItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "MessageReceived", callerRule, nonceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterMessageReceived)
				if err := _MessageTransmitter.contract.UnpackLog(event, "MessageReceived", log); err != nil {
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

// ParseMessageReceived is a log parse operation binding the contract event 0x58200b4c34ae05ee816d710053fff3fb75af4395915d3d2a771b24aa10e3cc5d.
//
// Solidity: event MessageReceived(address indexed caller, uint32 sourceDomain, uint64 indexed nonce, bytes32 sender, bytes messageBody)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseMessageReceived(log types.Log) (*MessageTransmitterMessageReceived, error) {
	event := new(MessageTransmitterMessageReceived)
	if err := _MessageTransmitter.contract.UnpackLog(event, "MessageReceived", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterMessageSentIterator is returned from FilterMessageSent and is used to iterate over the raw logs and unpacked data for MessageSent events raised by the MessageTransmitter contract.
type MessageTransmitterMessageSentIterator struct {
	Event *MessageTransmitterMessageSent // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterMessageSentIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterMessageSent)
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
		it.Event = new(MessageTransmitterMessageSent)
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
func (it *MessageTransmitterMessageSentIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterMessageSentIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterMessageSent represents a MessageSent event raised by the MessageTransmitter contract.
type MessageTransmitterMessageSent struct {
	Message []byte
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterMessageSent is a free log retrieval operation binding the contract event 0x8c5261668696ce22758910d05bab8f186d6eb247ceac2af2e82c7dc17669b036.
//
// Solidity: event MessageSent(bytes message)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterMessageSent(opts *bind.FilterOpts) (*MessageTransmitterMessageSentIterator, error) {

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "MessageSent")
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterMessageSentIterator{contract: _MessageTransmitter.contract, event: "MessageSent", logs: logs, sub: sub}, nil
}

// WatchMessageSent is a free log subscription operation binding the contract event 0x8c5261668696ce22758910d05bab8f186d6eb247ceac2af2e82c7dc17669b036.
//
// Solidity: event MessageSent(bytes message)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchMessageSent(opts *bind.WatchOpts, sink chan<- *MessageTransmitterMessageSent) (event.Subscription, error) {

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "MessageSent")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterMessageSent)
				if err := _MessageTransmitter.contract.UnpackLog(event, "MessageSent", log); err != nil {
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

// ParseMessageSent is a log parse operation binding the contract event 0x8c5261668696ce22758910d05bab8f186d6eb247ceac2af2e82c7dc17669b036.
//
// Solidity: event MessageSent(bytes message)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseMessageSent(log types.Log) (*MessageTransmitterMessageSent, error) {
	event := new(MessageTransmitterMessageSent)
	if err := _MessageTransmitter.contract.UnpackLog(event, "MessageSent", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterOwnershipTransferStartedIterator is returned from FilterOwnershipTransferStarted and is used to iterate over the raw logs and unpacked data for OwnershipTransferStarted events raised by the MessageTransmitter contract.
type MessageTransmitterOwnershipTransferStartedIterator struct {
	Event *MessageTransmitterOwnershipTransferStarted // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterOwnershipTransferStartedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterOwnershipTransferStarted)
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
		it.Event = new(MessageTransmitterOwnershipTransferStarted)
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
func (it *MessageTransmitterOwnershipTransferStartedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterOwnershipTransferStartedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterOwnershipTransferStarted represents a OwnershipTransferStarted event raised by the MessageTransmitter contract.
type MessageTransmitterOwnershipTransferStarted struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferStarted is a free log retrieval operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterOwnershipTransferStarted(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*MessageTransmitterOwnershipTransferStartedIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterOwnershipTransferStartedIterator{contract: _MessageTransmitter.contract, event: "OwnershipTransferStarted", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferStarted is a free log subscription operation binding the contract event 0x38d16b8cac22d99fc7c124b9cd0de2d3fa1faef420bfe791d8c362d765e22700.
//
// Solidity: event OwnershipTransferStarted(address indexed previousOwner, address indexed newOwner)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchOwnershipTransferStarted(opts *bind.WatchOpts, sink chan<- *MessageTransmitterOwnershipTransferStarted, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "OwnershipTransferStarted", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterOwnershipTransferStarted)
				if err := _MessageTransmitter.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
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
func (_MessageTransmitter *MessageTransmitterFilterer) ParseOwnershipTransferStarted(log types.Log) (*MessageTransmitterOwnershipTransferStarted, error) {
	event := new(MessageTransmitterOwnershipTransferStarted)
	if err := _MessageTransmitter.contract.UnpackLog(event, "OwnershipTransferStarted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the MessageTransmitter contract.
type MessageTransmitterOwnershipTransferredIterator struct {
	Event *MessageTransmitterOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterOwnershipTransferred)
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
		it.Event = new(MessageTransmitterOwnershipTransferred)
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
func (it *MessageTransmitterOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterOwnershipTransferred represents a OwnershipTransferred event raised by the MessageTransmitter contract.
type MessageTransmitterOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*MessageTransmitterOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterOwnershipTransferredIterator{contract: _MessageTransmitter.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *MessageTransmitterOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterOwnershipTransferred)
				if err := _MessageTransmitter.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_MessageTransmitter *MessageTransmitterFilterer) ParseOwnershipTransferred(log types.Log) (*MessageTransmitterOwnershipTransferred, error) {
	event := new(MessageTransmitterOwnershipTransferred)
	if err := _MessageTransmitter.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterPauseIterator is returned from FilterPause and is used to iterate over the raw logs and unpacked data for Pause events raised by the MessageTransmitter contract.
type MessageTransmitterPauseIterator struct {
	Event *MessageTransmitterPause // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterPauseIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterPause)
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
		it.Event = new(MessageTransmitterPause)
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
func (it *MessageTransmitterPauseIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterPauseIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterPause represents a Pause event raised by the MessageTransmitter contract.
type MessageTransmitterPause struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterPause is a free log retrieval operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_MessageTransmitter *MessageTransmitterFilterer) FilterPause(opts *bind.FilterOpts) (*MessageTransmitterPauseIterator, error) {

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "Pause")
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterPauseIterator{contract: _MessageTransmitter.contract, event: "Pause", logs: logs, sub: sub}, nil
}

// WatchPause is a free log subscription operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_MessageTransmitter *MessageTransmitterFilterer) WatchPause(opts *bind.WatchOpts, sink chan<- *MessageTransmitterPause) (event.Subscription, error) {

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "Pause")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterPause)
				if err := _MessageTransmitter.contract.UnpackLog(event, "Pause", log); err != nil {
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

// ParsePause is a log parse operation binding the contract event 0x6985a02210a168e66602d3235cb6db0e70f92b3ba4d376a33c0f3d9434bff625.
//
// Solidity: event Pause()
func (_MessageTransmitter *MessageTransmitterFilterer) ParsePause(log types.Log) (*MessageTransmitterPause, error) {
	event := new(MessageTransmitterPause)
	if err := _MessageTransmitter.contract.UnpackLog(event, "Pause", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterPauserChangedIterator is returned from FilterPauserChanged and is used to iterate over the raw logs and unpacked data for PauserChanged events raised by the MessageTransmitter contract.
type MessageTransmitterPauserChangedIterator struct {
	Event *MessageTransmitterPauserChanged // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterPauserChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterPauserChanged)
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
		it.Event = new(MessageTransmitterPauserChanged)
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
func (it *MessageTransmitterPauserChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterPauserChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterPauserChanged represents a PauserChanged event raised by the MessageTransmitter contract.
type MessageTransmitterPauserChanged struct {
	NewAddress common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterPauserChanged is a free log retrieval operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterPauserChanged(opts *bind.FilterOpts, newAddress []common.Address) (*MessageTransmitterPauserChangedIterator, error) {

	var newAddressRule []interface{}
	for _, newAddressItem := range newAddress {
		newAddressRule = append(newAddressRule, newAddressItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "PauserChanged", newAddressRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterPauserChangedIterator{contract: _MessageTransmitter.contract, event: "PauserChanged", logs: logs, sub: sub}, nil
}

// WatchPauserChanged is a free log subscription operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchPauserChanged(opts *bind.WatchOpts, sink chan<- *MessageTransmitterPauserChanged, newAddress []common.Address) (event.Subscription, error) {

	var newAddressRule []interface{}
	for _, newAddressItem := range newAddress {
		newAddressRule = append(newAddressRule, newAddressItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "PauserChanged", newAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterPauserChanged)
				if err := _MessageTransmitter.contract.UnpackLog(event, "PauserChanged", log); err != nil {
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

// ParsePauserChanged is a log parse operation binding the contract event 0xb80482a293ca2e013eda8683c9bd7fc8347cfdaeea5ede58cba46df502c2a604.
//
// Solidity: event PauserChanged(address indexed newAddress)
func (_MessageTransmitter *MessageTransmitterFilterer) ParsePauserChanged(log types.Log) (*MessageTransmitterPauserChanged, error) {
	event := new(MessageTransmitterPauserChanged)
	if err := _MessageTransmitter.contract.UnpackLog(event, "PauserChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterRescuerChangedIterator is returned from FilterRescuerChanged and is used to iterate over the raw logs and unpacked data for RescuerChanged events raised by the MessageTransmitter contract.
type MessageTransmitterRescuerChangedIterator struct {
	Event *MessageTransmitterRescuerChanged // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterRescuerChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterRescuerChanged)
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
		it.Event = new(MessageTransmitterRescuerChanged)
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
func (it *MessageTransmitterRescuerChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterRescuerChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterRescuerChanged represents a RescuerChanged event raised by the MessageTransmitter contract.
type MessageTransmitterRescuerChanged struct {
	NewRescuer common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRescuerChanged is a free log retrieval operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterRescuerChanged(opts *bind.FilterOpts, newRescuer []common.Address) (*MessageTransmitterRescuerChangedIterator, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterRescuerChangedIterator{contract: _MessageTransmitter.contract, event: "RescuerChanged", logs: logs, sub: sub}, nil
}

// WatchRescuerChanged is a free log subscription operation binding the contract event 0xe475e580d85111348e40d8ca33cfdd74c30fe1655c2d8537a13abc10065ffa5a.
//
// Solidity: event RescuerChanged(address indexed newRescuer)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchRescuerChanged(opts *bind.WatchOpts, sink chan<- *MessageTransmitterRescuerChanged, newRescuer []common.Address) (event.Subscription, error) {

	var newRescuerRule []interface{}
	for _, newRescuerItem := range newRescuer {
		newRescuerRule = append(newRescuerRule, newRescuerItem)
	}

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "RescuerChanged", newRescuerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterRescuerChanged)
				if err := _MessageTransmitter.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
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
func (_MessageTransmitter *MessageTransmitterFilterer) ParseRescuerChanged(log types.Log) (*MessageTransmitterRescuerChanged, error) {
	event := new(MessageTransmitterRescuerChanged)
	if err := _MessageTransmitter.contract.UnpackLog(event, "RescuerChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterSignatureThresholdUpdatedIterator is returned from FilterSignatureThresholdUpdated and is used to iterate over the raw logs and unpacked data for SignatureThresholdUpdated events raised by the MessageTransmitter contract.
type MessageTransmitterSignatureThresholdUpdatedIterator struct {
	Event *MessageTransmitterSignatureThresholdUpdated // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterSignatureThresholdUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterSignatureThresholdUpdated)
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
		it.Event = new(MessageTransmitterSignatureThresholdUpdated)
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
func (it *MessageTransmitterSignatureThresholdUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterSignatureThresholdUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterSignatureThresholdUpdated represents a SignatureThresholdUpdated event raised by the MessageTransmitter contract.
type MessageTransmitterSignatureThresholdUpdated struct {
	OldSignatureThreshold *big.Int
	NewSignatureThreshold *big.Int
	Raw                   types.Log // Blockchain specific contextual infos
}

// FilterSignatureThresholdUpdated is a free log retrieval operation binding the contract event 0x149153f58b4da003a8cfd4523709a202402182cb5aa335046911277a1be6eede.
//
// Solidity: event SignatureThresholdUpdated(uint256 oldSignatureThreshold, uint256 newSignatureThreshold)
func (_MessageTransmitter *MessageTransmitterFilterer) FilterSignatureThresholdUpdated(opts *bind.FilterOpts) (*MessageTransmitterSignatureThresholdUpdatedIterator, error) {

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "SignatureThresholdUpdated")
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterSignatureThresholdUpdatedIterator{contract: _MessageTransmitter.contract, event: "SignatureThresholdUpdated", logs: logs, sub: sub}, nil
}

// WatchSignatureThresholdUpdated is a free log subscription operation binding the contract event 0x149153f58b4da003a8cfd4523709a202402182cb5aa335046911277a1be6eede.
//
// Solidity: event SignatureThresholdUpdated(uint256 oldSignatureThreshold, uint256 newSignatureThreshold)
func (_MessageTransmitter *MessageTransmitterFilterer) WatchSignatureThresholdUpdated(opts *bind.WatchOpts, sink chan<- *MessageTransmitterSignatureThresholdUpdated) (event.Subscription, error) {

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "SignatureThresholdUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterSignatureThresholdUpdated)
				if err := _MessageTransmitter.contract.UnpackLog(event, "SignatureThresholdUpdated", log); err != nil {
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

// ParseSignatureThresholdUpdated is a log parse operation binding the contract event 0x149153f58b4da003a8cfd4523709a202402182cb5aa335046911277a1be6eede.
//
// Solidity: event SignatureThresholdUpdated(uint256 oldSignatureThreshold, uint256 newSignatureThreshold)
func (_MessageTransmitter *MessageTransmitterFilterer) ParseSignatureThresholdUpdated(log types.Log) (*MessageTransmitterSignatureThresholdUpdated, error) {
	event := new(MessageTransmitterSignatureThresholdUpdated)
	if err := _MessageTransmitter.contract.UnpackLog(event, "SignatureThresholdUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MessageTransmitterUnpauseIterator is returned from FilterUnpause and is used to iterate over the raw logs and unpacked data for Unpause events raised by the MessageTransmitter contract.
type MessageTransmitterUnpauseIterator struct {
	Event *MessageTransmitterUnpause // Event containing the contract specifics and raw log

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
func (it *MessageTransmitterUnpauseIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MessageTransmitterUnpause)
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
		it.Event = new(MessageTransmitterUnpause)
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
func (it *MessageTransmitterUnpauseIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MessageTransmitterUnpauseIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MessageTransmitterUnpause represents a Unpause event raised by the MessageTransmitter contract.
type MessageTransmitterUnpause struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterUnpause is a free log retrieval operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_MessageTransmitter *MessageTransmitterFilterer) FilterUnpause(opts *bind.FilterOpts) (*MessageTransmitterUnpauseIterator, error) {

	logs, sub, err := _MessageTransmitter.contract.FilterLogs(opts, "Unpause")
	if err != nil {
		return nil, err
	}
	return &MessageTransmitterUnpauseIterator{contract: _MessageTransmitter.contract, event: "Unpause", logs: logs, sub: sub}, nil
}

// WatchUnpause is a free log subscription operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_MessageTransmitter *MessageTransmitterFilterer) WatchUnpause(opts *bind.WatchOpts, sink chan<- *MessageTransmitterUnpause) (event.Subscription, error) {

	logs, sub, err := _MessageTransmitter.contract.WatchLogs(opts, "Unpause")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MessageTransmitterUnpause)
				if err := _MessageTransmitter.contract.UnpackLog(event, "Unpause", log); err != nil {
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

// ParseUnpause is a log parse operation binding the contract event 0x7805862f689e2f13df9f062ff482ad3ad112aca9e0847911ed832e158c525b33.
//
// Solidity: event Unpause()
func (_MessageTransmitter *MessageTransmitterFilterer) ParseUnpause(log types.Log) (*MessageTransmitterUnpause, error) {
	event := new(MessageTransmitterUnpause)
	if err := _MessageTransmitter.contract.UnpackLog(event, "Unpause", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
