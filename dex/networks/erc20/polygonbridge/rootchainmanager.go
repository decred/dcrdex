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

// RootChainManagerMetaData contains all meta data concerning the RootChainManager contract.
var RootChainManagerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"userAddress\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"addresspayable\",\"name\":\"relayerAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"functionSignature\",\"type\":\"bytes\"}],\"name\":\"MetaTransactionExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"tokenType\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"predicateAddress\",\"type\":\"address\"}],\"name\":\"PredicateRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"childToken\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"tokenType\",\"type\":\"bytes32\"}],\"name\":\"TokenMapped\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"DEPOSIT\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"ERC712_VERSION\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"ETHER_ADDRESS\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MAPPER_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MAP_TOKEN\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"checkpointManagerAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"childChainManagerAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"childToRootToken\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"childToken\",\"type\":\"address\"}],\"name\":\"cleanMapToken\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"}],\"name\":\"depositEtherFor\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"depositData\",\"type\":\"bytes\"}],\"name\":\"depositFor\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"userAddress\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"functionSignature\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"sigR\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"sigS\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"sigV\",\"type\":\"uint8\"}],\"name\":\"executeMetaTransaction\",\"outputs\":[{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"inputData\",\"type\":\"bytes\"}],\"name\":\"exit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getChainId\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getDomainSeperator\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"}],\"name\":\"getNonce\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"getRoleMember\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleMemberCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_owner\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"initializeEIP712\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"childToken\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"tokenType\",\"type\":\"bytes32\"}],\"name\":\"mapToken\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"processedExits\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"tokenType\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"predicateAddress\",\"type\":\"address\"}],\"name\":\"registerPredicate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"childToken\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"tokenType\",\"type\":\"bytes32\"}],\"name\":\"remapToken\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"rootToChildToken\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newCheckpointManager\",\"type\":\"address\"}],\"name\":\"setCheckpointManager\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newChildChainManager\",\"type\":\"address\"}],\"name\":\"setChildChainManagerAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newStateSender\",\"type\":\"address\"}],\"name\":\"setStateSender\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"setupContractId\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"stateSenderAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"tokenToType\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"typeToPredicate\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
}

// RootChainManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use RootChainManagerMetaData.ABI instead.
var RootChainManagerABI = RootChainManagerMetaData.ABI

// RootChainManager is an auto generated Go binding around an Ethereum contract.
type RootChainManager struct {
	RootChainManagerCaller     // Read-only binding to the contract
	RootChainManagerTransactor // Write-only binding to the contract
	RootChainManagerFilterer   // Log filterer for contract events
}

// RootChainManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type RootChainManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RootChainManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type RootChainManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RootChainManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type RootChainManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// RootChainManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type RootChainManagerSession struct {
	Contract     *RootChainManager // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// RootChainManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type RootChainManagerCallerSession struct {
	Contract *RootChainManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// RootChainManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type RootChainManagerTransactorSession struct {
	Contract     *RootChainManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// RootChainManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type RootChainManagerRaw struct {
	Contract *RootChainManager // Generic contract binding to access the raw methods on
}

// RootChainManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type RootChainManagerCallerRaw struct {
	Contract *RootChainManagerCaller // Generic read-only contract binding to access the raw methods on
}

// RootChainManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type RootChainManagerTransactorRaw struct {
	Contract *RootChainManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewRootChainManager creates a new instance of RootChainManager, bound to a specific deployed contract.
func NewRootChainManager(address common.Address, backend bind.ContractBackend) (*RootChainManager, error) {
	contract, err := bindRootChainManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &RootChainManager{RootChainManagerCaller: RootChainManagerCaller{contract: contract}, RootChainManagerTransactor: RootChainManagerTransactor{contract: contract}, RootChainManagerFilterer: RootChainManagerFilterer{contract: contract}}, nil
}

// NewRootChainManagerCaller creates a new read-only instance of RootChainManager, bound to a specific deployed contract.
func NewRootChainManagerCaller(address common.Address, caller bind.ContractCaller) (*RootChainManagerCaller, error) {
	contract, err := bindRootChainManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerCaller{contract: contract}, nil
}

// NewRootChainManagerTransactor creates a new write-only instance of RootChainManager, bound to a specific deployed contract.
func NewRootChainManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*RootChainManagerTransactor, error) {
	contract, err := bindRootChainManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerTransactor{contract: contract}, nil
}

// NewRootChainManagerFilterer creates a new log filterer instance of RootChainManager, bound to a specific deployed contract.
func NewRootChainManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*RootChainManagerFilterer, error) {
	contract, err := bindRootChainManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerFilterer{contract: contract}, nil
}

// bindRootChainManager binds a generic wrapper to an already deployed contract.
func bindRootChainManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := RootChainManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_RootChainManager *RootChainManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _RootChainManager.Contract.RootChainManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_RootChainManager *RootChainManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RootChainManager.Contract.RootChainManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_RootChainManager *RootChainManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _RootChainManager.Contract.RootChainManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_RootChainManager *RootChainManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _RootChainManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_RootChainManager *RootChainManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RootChainManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_RootChainManager *RootChainManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _RootChainManager.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _RootChainManager.Contract.DEFAULTADMINROLE(&_RootChainManager.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _RootChainManager.Contract.DEFAULTADMINROLE(&_RootChainManager.CallOpts)
}

// DEPOSIT is a free data retrieval call binding the contract method 0xd81c8e52.
//
// Solidity: function DEPOSIT() view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) DEPOSIT(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "DEPOSIT")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEPOSIT is a free data retrieval call binding the contract method 0xd81c8e52.
//
// Solidity: function DEPOSIT() view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) DEPOSIT() ([32]byte, error) {
	return _RootChainManager.Contract.DEPOSIT(&_RootChainManager.CallOpts)
}

// DEPOSIT is a free data retrieval call binding the contract method 0xd81c8e52.
//
// Solidity: function DEPOSIT() view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) DEPOSIT() ([32]byte, error) {
	return _RootChainManager.Contract.DEPOSIT(&_RootChainManager.CallOpts)
}

// ERC712VERSION is a free data retrieval call binding the contract method 0x0f7e5970.
//
// Solidity: function ERC712_VERSION() view returns(string)
func (_RootChainManager *RootChainManagerCaller) ERC712VERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "ERC712_VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// ERC712VERSION is a free data retrieval call binding the contract method 0x0f7e5970.
//
// Solidity: function ERC712_VERSION() view returns(string)
func (_RootChainManager *RootChainManagerSession) ERC712VERSION() (string, error) {
	return _RootChainManager.Contract.ERC712VERSION(&_RootChainManager.CallOpts)
}

// ERC712VERSION is a free data retrieval call binding the contract method 0x0f7e5970.
//
// Solidity: function ERC712_VERSION() view returns(string)
func (_RootChainManager *RootChainManagerCallerSession) ERC712VERSION() (string, error) {
	return _RootChainManager.Contract.ERC712VERSION(&_RootChainManager.CallOpts)
}

// ETHERADDRESS is a free data retrieval call binding the contract method 0xcf1d21c0.
//
// Solidity: function ETHER_ADDRESS() view returns(address)
func (_RootChainManager *RootChainManagerCaller) ETHERADDRESS(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "ETHER_ADDRESS")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ETHERADDRESS is a free data retrieval call binding the contract method 0xcf1d21c0.
//
// Solidity: function ETHER_ADDRESS() view returns(address)
func (_RootChainManager *RootChainManagerSession) ETHERADDRESS() (common.Address, error) {
	return _RootChainManager.Contract.ETHERADDRESS(&_RootChainManager.CallOpts)
}

// ETHERADDRESS is a free data retrieval call binding the contract method 0xcf1d21c0.
//
// Solidity: function ETHER_ADDRESS() view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) ETHERADDRESS() (common.Address, error) {
	return _RootChainManager.Contract.ETHERADDRESS(&_RootChainManager.CallOpts)
}

// MAPPERROLE is a free data retrieval call binding the contract method 0x568b80b5.
//
// Solidity: function MAPPER_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) MAPPERROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "MAPPER_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// MAPPERROLE is a free data retrieval call binding the contract method 0x568b80b5.
//
// Solidity: function MAPPER_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) MAPPERROLE() ([32]byte, error) {
	return _RootChainManager.Contract.MAPPERROLE(&_RootChainManager.CallOpts)
}

// MAPPERROLE is a free data retrieval call binding the contract method 0x568b80b5.
//
// Solidity: function MAPPER_ROLE() view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) MAPPERROLE() ([32]byte, error) {
	return _RootChainManager.Contract.MAPPERROLE(&_RootChainManager.CallOpts)
}

// MAPTOKEN is a free data retrieval call binding the contract method 0x886a69ba.
//
// Solidity: function MAP_TOKEN() view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) MAPTOKEN(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "MAP_TOKEN")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// MAPTOKEN is a free data retrieval call binding the contract method 0x886a69ba.
//
// Solidity: function MAP_TOKEN() view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) MAPTOKEN() ([32]byte, error) {
	return _RootChainManager.Contract.MAPTOKEN(&_RootChainManager.CallOpts)
}

// MAPTOKEN is a free data retrieval call binding the contract method 0x886a69ba.
//
// Solidity: function MAP_TOKEN() view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) MAPTOKEN() ([32]byte, error) {
	return _RootChainManager.Contract.MAPTOKEN(&_RootChainManager.CallOpts)
}

// CheckpointManagerAddress is a free data retrieval call binding the contract method 0x3138b6f1.
//
// Solidity: function checkpointManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerCaller) CheckpointManagerAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "checkpointManagerAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// CheckpointManagerAddress is a free data retrieval call binding the contract method 0x3138b6f1.
//
// Solidity: function checkpointManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerSession) CheckpointManagerAddress() (common.Address, error) {
	return _RootChainManager.Contract.CheckpointManagerAddress(&_RootChainManager.CallOpts)
}

// CheckpointManagerAddress is a free data retrieval call binding the contract method 0x3138b6f1.
//
// Solidity: function checkpointManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) CheckpointManagerAddress() (common.Address, error) {
	return _RootChainManager.Contract.CheckpointManagerAddress(&_RootChainManager.CallOpts)
}

// ChildChainManagerAddress is a free data retrieval call binding the contract method 0x04967702.
//
// Solidity: function childChainManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerCaller) ChildChainManagerAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "childChainManagerAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ChildChainManagerAddress is a free data retrieval call binding the contract method 0x04967702.
//
// Solidity: function childChainManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerSession) ChildChainManagerAddress() (common.Address, error) {
	return _RootChainManager.Contract.ChildChainManagerAddress(&_RootChainManager.CallOpts)
}

// ChildChainManagerAddress is a free data retrieval call binding the contract method 0x04967702.
//
// Solidity: function childChainManagerAddress() view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) ChildChainManagerAddress() (common.Address, error) {
	return _RootChainManager.Contract.ChildChainManagerAddress(&_RootChainManager.CallOpts)
}

// ChildToRootToken is a free data retrieval call binding the contract method 0x6e86b770.
//
// Solidity: function childToRootToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerCaller) ChildToRootToken(opts *bind.CallOpts, arg0 common.Address) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "childToRootToken", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ChildToRootToken is a free data retrieval call binding the contract method 0x6e86b770.
//
// Solidity: function childToRootToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerSession) ChildToRootToken(arg0 common.Address) (common.Address, error) {
	return _RootChainManager.Contract.ChildToRootToken(&_RootChainManager.CallOpts, arg0)
}

// ChildToRootToken is a free data retrieval call binding the contract method 0x6e86b770.
//
// Solidity: function childToRootToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) ChildToRootToken(arg0 common.Address) (common.Address, error) {
	return _RootChainManager.Contract.ChildToRootToken(&_RootChainManager.CallOpts, arg0)
}

// GetChainId is a free data retrieval call binding the contract method 0x3408e470.
//
// Solidity: function getChainId() pure returns(uint256)
func (_RootChainManager *RootChainManagerCaller) GetChainId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getChainId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetChainId is a free data retrieval call binding the contract method 0x3408e470.
//
// Solidity: function getChainId() pure returns(uint256)
func (_RootChainManager *RootChainManagerSession) GetChainId() (*big.Int, error) {
	return _RootChainManager.Contract.GetChainId(&_RootChainManager.CallOpts)
}

// GetChainId is a free data retrieval call binding the contract method 0x3408e470.
//
// Solidity: function getChainId() pure returns(uint256)
func (_RootChainManager *RootChainManagerCallerSession) GetChainId() (*big.Int, error) {
	return _RootChainManager.Contract.GetChainId(&_RootChainManager.CallOpts)
}

// GetDomainSeperator is a free data retrieval call binding the contract method 0x20379ee5.
//
// Solidity: function getDomainSeperator() view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) GetDomainSeperator(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getDomainSeperator")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetDomainSeperator is a free data retrieval call binding the contract method 0x20379ee5.
//
// Solidity: function getDomainSeperator() view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) GetDomainSeperator() ([32]byte, error) {
	return _RootChainManager.Contract.GetDomainSeperator(&_RootChainManager.CallOpts)
}

// GetDomainSeperator is a free data retrieval call binding the contract method 0x20379ee5.
//
// Solidity: function getDomainSeperator() view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) GetDomainSeperator() ([32]byte, error) {
	return _RootChainManager.Contract.GetDomainSeperator(&_RootChainManager.CallOpts)
}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address user) view returns(uint256 nonce)
func (_RootChainManager *RootChainManagerCaller) GetNonce(opts *bind.CallOpts, user common.Address) (*big.Int, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getNonce", user)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address user) view returns(uint256 nonce)
func (_RootChainManager *RootChainManagerSession) GetNonce(user common.Address) (*big.Int, error) {
	return _RootChainManager.Contract.GetNonce(&_RootChainManager.CallOpts, user)
}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address user) view returns(uint256 nonce)
func (_RootChainManager *RootChainManagerCallerSession) GetNonce(user common.Address) (*big.Int, error) {
	return _RootChainManager.Contract.GetNonce(&_RootChainManager.CallOpts, user)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _RootChainManager.Contract.GetRoleAdmin(&_RootChainManager.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _RootChainManager.Contract.GetRoleAdmin(&_RootChainManager.CallOpts, role)
}

// GetRoleMember is a free data retrieval call binding the contract method 0x9010d07c.
//
// Solidity: function getRoleMember(bytes32 role, uint256 index) view returns(address)
func (_RootChainManager *RootChainManagerCaller) GetRoleMember(opts *bind.CallOpts, role [32]byte, index *big.Int) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getRoleMember", role, index)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetRoleMember is a free data retrieval call binding the contract method 0x9010d07c.
//
// Solidity: function getRoleMember(bytes32 role, uint256 index) view returns(address)
func (_RootChainManager *RootChainManagerSession) GetRoleMember(role [32]byte, index *big.Int) (common.Address, error) {
	return _RootChainManager.Contract.GetRoleMember(&_RootChainManager.CallOpts, role, index)
}

// GetRoleMember is a free data retrieval call binding the contract method 0x9010d07c.
//
// Solidity: function getRoleMember(bytes32 role, uint256 index) view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) GetRoleMember(role [32]byte, index *big.Int) (common.Address, error) {
	return _RootChainManager.Contract.GetRoleMember(&_RootChainManager.CallOpts, role, index)
}

// GetRoleMemberCount is a free data retrieval call binding the contract method 0xca15c873.
//
// Solidity: function getRoleMemberCount(bytes32 role) view returns(uint256)
func (_RootChainManager *RootChainManagerCaller) GetRoleMemberCount(opts *bind.CallOpts, role [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "getRoleMemberCount", role)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRoleMemberCount is a free data retrieval call binding the contract method 0xca15c873.
//
// Solidity: function getRoleMemberCount(bytes32 role) view returns(uint256)
func (_RootChainManager *RootChainManagerSession) GetRoleMemberCount(role [32]byte) (*big.Int, error) {
	return _RootChainManager.Contract.GetRoleMemberCount(&_RootChainManager.CallOpts, role)
}

// GetRoleMemberCount is a free data retrieval call binding the contract method 0xca15c873.
//
// Solidity: function getRoleMemberCount(bytes32 role) view returns(uint256)
func (_RootChainManager *RootChainManagerCallerSession) GetRoleMemberCount(role [32]byte) (*big.Int, error) {
	return _RootChainManager.Contract.GetRoleMemberCount(&_RootChainManager.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_RootChainManager *RootChainManagerCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_RootChainManager *RootChainManagerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _RootChainManager.Contract.HasRole(&_RootChainManager.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_RootChainManager *RootChainManagerCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _RootChainManager.Contract.HasRole(&_RootChainManager.CallOpts, role, account)
}

// ProcessedExits is a free data retrieval call binding the contract method 0x607f2d42.
//
// Solidity: function processedExits(bytes32 ) view returns(bool)
func (_RootChainManager *RootChainManagerCaller) ProcessedExits(opts *bind.CallOpts, arg0 [32]byte) (bool, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "processedExits", arg0)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProcessedExits is a free data retrieval call binding the contract method 0x607f2d42.
//
// Solidity: function processedExits(bytes32 ) view returns(bool)
func (_RootChainManager *RootChainManagerSession) ProcessedExits(arg0 [32]byte) (bool, error) {
	return _RootChainManager.Contract.ProcessedExits(&_RootChainManager.CallOpts, arg0)
}

// ProcessedExits is a free data retrieval call binding the contract method 0x607f2d42.
//
// Solidity: function processedExits(bytes32 ) view returns(bool)
func (_RootChainManager *RootChainManagerCallerSession) ProcessedExits(arg0 [32]byte) (bool, error) {
	return _RootChainManager.Contract.ProcessedExits(&_RootChainManager.CallOpts, arg0)
}

// RootToChildToken is a free data retrieval call binding the contract method 0xea60c7c4.
//
// Solidity: function rootToChildToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerCaller) RootToChildToken(opts *bind.CallOpts, arg0 common.Address) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "rootToChildToken", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// RootToChildToken is a free data retrieval call binding the contract method 0xea60c7c4.
//
// Solidity: function rootToChildToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerSession) RootToChildToken(arg0 common.Address) (common.Address, error) {
	return _RootChainManager.Contract.RootToChildToken(&_RootChainManager.CallOpts, arg0)
}

// RootToChildToken is a free data retrieval call binding the contract method 0xea60c7c4.
//
// Solidity: function rootToChildToken(address ) view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) RootToChildToken(arg0 common.Address) (common.Address, error) {
	return _RootChainManager.Contract.RootToChildToken(&_RootChainManager.CallOpts, arg0)
}

// StateSenderAddress is a free data retrieval call binding the contract method 0xe2c49de1.
//
// Solidity: function stateSenderAddress() view returns(address)
func (_RootChainManager *RootChainManagerCaller) StateSenderAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "stateSenderAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// StateSenderAddress is a free data retrieval call binding the contract method 0xe2c49de1.
//
// Solidity: function stateSenderAddress() view returns(address)
func (_RootChainManager *RootChainManagerSession) StateSenderAddress() (common.Address, error) {
	return _RootChainManager.Contract.StateSenderAddress(&_RootChainManager.CallOpts)
}

// StateSenderAddress is a free data retrieval call binding the contract method 0xe2c49de1.
//
// Solidity: function stateSenderAddress() view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) StateSenderAddress() (common.Address, error) {
	return _RootChainManager.Contract.StateSenderAddress(&_RootChainManager.CallOpts)
}

// TokenToType is a free data retrieval call binding the contract method 0xe43009a6.
//
// Solidity: function tokenToType(address ) view returns(bytes32)
func (_RootChainManager *RootChainManagerCaller) TokenToType(opts *bind.CallOpts, arg0 common.Address) ([32]byte, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "tokenToType", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// TokenToType is a free data retrieval call binding the contract method 0xe43009a6.
//
// Solidity: function tokenToType(address ) view returns(bytes32)
func (_RootChainManager *RootChainManagerSession) TokenToType(arg0 common.Address) ([32]byte, error) {
	return _RootChainManager.Contract.TokenToType(&_RootChainManager.CallOpts, arg0)
}

// TokenToType is a free data retrieval call binding the contract method 0xe43009a6.
//
// Solidity: function tokenToType(address ) view returns(bytes32)
func (_RootChainManager *RootChainManagerCallerSession) TokenToType(arg0 common.Address) ([32]byte, error) {
	return _RootChainManager.Contract.TokenToType(&_RootChainManager.CallOpts, arg0)
}

// TypeToPredicate is a free data retrieval call binding the contract method 0xe66f9603.
//
// Solidity: function typeToPredicate(bytes32 ) view returns(address)
func (_RootChainManager *RootChainManagerCaller) TypeToPredicate(opts *bind.CallOpts, arg0 [32]byte) (common.Address, error) {
	var out []interface{}
	err := _RootChainManager.contract.Call(opts, &out, "typeToPredicate", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// TypeToPredicate is a free data retrieval call binding the contract method 0xe66f9603.
//
// Solidity: function typeToPredicate(bytes32 ) view returns(address)
func (_RootChainManager *RootChainManagerSession) TypeToPredicate(arg0 [32]byte) (common.Address, error) {
	return _RootChainManager.Contract.TypeToPredicate(&_RootChainManager.CallOpts, arg0)
}

// TypeToPredicate is a free data retrieval call binding the contract method 0xe66f9603.
//
// Solidity: function typeToPredicate(bytes32 ) view returns(address)
func (_RootChainManager *RootChainManagerCallerSession) TypeToPredicate(arg0 [32]byte) (common.Address, error) {
	return _RootChainManager.Contract.TypeToPredicate(&_RootChainManager.CallOpts, arg0)
}

// CleanMapToken is a paid mutator transaction binding the contract method 0x0c3894bb.
//
// Solidity: function cleanMapToken(address rootToken, address childToken) returns()
func (_RootChainManager *RootChainManagerTransactor) CleanMapToken(opts *bind.TransactOpts, rootToken common.Address, childToken common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "cleanMapToken", rootToken, childToken)
}

// CleanMapToken is a paid mutator transaction binding the contract method 0x0c3894bb.
//
// Solidity: function cleanMapToken(address rootToken, address childToken) returns()
func (_RootChainManager *RootChainManagerSession) CleanMapToken(rootToken common.Address, childToken common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.CleanMapToken(&_RootChainManager.TransactOpts, rootToken, childToken)
}

// CleanMapToken is a paid mutator transaction binding the contract method 0x0c3894bb.
//
// Solidity: function cleanMapToken(address rootToken, address childToken) returns()
func (_RootChainManager *RootChainManagerTransactorSession) CleanMapToken(rootToken common.Address, childToken common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.CleanMapToken(&_RootChainManager.TransactOpts, rootToken, childToken)
}

// DepositEtherFor is a paid mutator transaction binding the contract method 0x4faa8a26.
//
// Solidity: function depositEtherFor(address user) payable returns()
func (_RootChainManager *RootChainManagerTransactor) DepositEtherFor(opts *bind.TransactOpts, user common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "depositEtherFor", user)
}

// DepositEtherFor is a paid mutator transaction binding the contract method 0x4faa8a26.
//
// Solidity: function depositEtherFor(address user) payable returns()
func (_RootChainManager *RootChainManagerSession) DepositEtherFor(user common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.DepositEtherFor(&_RootChainManager.TransactOpts, user)
}

// DepositEtherFor is a paid mutator transaction binding the contract method 0x4faa8a26.
//
// Solidity: function depositEtherFor(address user) payable returns()
func (_RootChainManager *RootChainManagerTransactorSession) DepositEtherFor(user common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.DepositEtherFor(&_RootChainManager.TransactOpts, user)
}

// DepositFor is a paid mutator transaction binding the contract method 0xe3dec8fb.
//
// Solidity: function depositFor(address user, address rootToken, bytes depositData) returns()
func (_RootChainManager *RootChainManagerTransactor) DepositFor(opts *bind.TransactOpts, user common.Address, rootToken common.Address, depositData []byte) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "depositFor", user, rootToken, depositData)
}

// DepositFor is a paid mutator transaction binding the contract method 0xe3dec8fb.
//
// Solidity: function depositFor(address user, address rootToken, bytes depositData) returns()
func (_RootChainManager *RootChainManagerSession) DepositFor(user common.Address, rootToken common.Address, depositData []byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.DepositFor(&_RootChainManager.TransactOpts, user, rootToken, depositData)
}

// DepositFor is a paid mutator transaction binding the contract method 0xe3dec8fb.
//
// Solidity: function depositFor(address user, address rootToken, bytes depositData) returns()
func (_RootChainManager *RootChainManagerTransactorSession) DepositFor(user common.Address, rootToken common.Address, depositData []byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.DepositFor(&_RootChainManager.TransactOpts, user, rootToken, depositData)
}

// ExecuteMetaTransaction is a paid mutator transaction binding the contract method 0x0c53c51c.
//
// Solidity: function executeMetaTransaction(address userAddress, bytes functionSignature, bytes32 sigR, bytes32 sigS, uint8 sigV) payable returns(bytes)
func (_RootChainManager *RootChainManagerTransactor) ExecuteMetaTransaction(opts *bind.TransactOpts, userAddress common.Address, functionSignature []byte, sigR [32]byte, sigS [32]byte, sigV uint8) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "executeMetaTransaction", userAddress, functionSignature, sigR, sigS, sigV)
}

// ExecuteMetaTransaction is a paid mutator transaction binding the contract method 0x0c53c51c.
//
// Solidity: function executeMetaTransaction(address userAddress, bytes functionSignature, bytes32 sigR, bytes32 sigS, uint8 sigV) payable returns(bytes)
func (_RootChainManager *RootChainManagerSession) ExecuteMetaTransaction(userAddress common.Address, functionSignature []byte, sigR [32]byte, sigS [32]byte, sigV uint8) (*types.Transaction, error) {
	return _RootChainManager.Contract.ExecuteMetaTransaction(&_RootChainManager.TransactOpts, userAddress, functionSignature, sigR, sigS, sigV)
}

// ExecuteMetaTransaction is a paid mutator transaction binding the contract method 0x0c53c51c.
//
// Solidity: function executeMetaTransaction(address userAddress, bytes functionSignature, bytes32 sigR, bytes32 sigS, uint8 sigV) payable returns(bytes)
func (_RootChainManager *RootChainManagerTransactorSession) ExecuteMetaTransaction(userAddress common.Address, functionSignature []byte, sigR [32]byte, sigS [32]byte, sigV uint8) (*types.Transaction, error) {
	return _RootChainManager.Contract.ExecuteMetaTransaction(&_RootChainManager.TransactOpts, userAddress, functionSignature, sigR, sigS, sigV)
}

// Exit is a paid mutator transaction binding the contract method 0x3805550f.
//
// Solidity: function exit(bytes inputData) returns()
func (_RootChainManager *RootChainManagerTransactor) Exit(opts *bind.TransactOpts, inputData []byte) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "exit", inputData)
}

// Exit is a paid mutator transaction binding the contract method 0x3805550f.
//
// Solidity: function exit(bytes inputData) returns()
func (_RootChainManager *RootChainManagerSession) Exit(inputData []byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.Exit(&_RootChainManager.TransactOpts, inputData)
}

// Exit is a paid mutator transaction binding the contract method 0x3805550f.
//
// Solidity: function exit(bytes inputData) returns()
func (_RootChainManager *RootChainManagerTransactorSession) Exit(inputData []byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.Exit(&_RootChainManager.TransactOpts, inputData)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.GrantRole(&_RootChainManager.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.GrantRole(&_RootChainManager.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _owner) returns()
func (_RootChainManager *RootChainManagerTransactor) Initialize(opts *bind.TransactOpts, _owner common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "initialize", _owner)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _owner) returns()
func (_RootChainManager *RootChainManagerSession) Initialize(_owner common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.Initialize(&_RootChainManager.TransactOpts, _owner)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _owner) returns()
func (_RootChainManager *RootChainManagerTransactorSession) Initialize(_owner common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.Initialize(&_RootChainManager.TransactOpts, _owner)
}

// InitializeEIP712 is a paid mutator transaction binding the contract method 0x630fcbfb.
//
// Solidity: function initializeEIP712() returns()
func (_RootChainManager *RootChainManagerTransactor) InitializeEIP712(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "initializeEIP712")
}

// InitializeEIP712 is a paid mutator transaction binding the contract method 0x630fcbfb.
//
// Solidity: function initializeEIP712() returns()
func (_RootChainManager *RootChainManagerSession) InitializeEIP712() (*types.Transaction, error) {
	return _RootChainManager.Contract.InitializeEIP712(&_RootChainManager.TransactOpts)
}

// InitializeEIP712 is a paid mutator transaction binding the contract method 0x630fcbfb.
//
// Solidity: function initializeEIP712() returns()
func (_RootChainManager *RootChainManagerTransactorSession) InitializeEIP712() (*types.Transaction, error) {
	return _RootChainManager.Contract.InitializeEIP712(&_RootChainManager.TransactOpts)
}

// MapToken is a paid mutator transaction binding the contract method 0x9173b139.
//
// Solidity: function mapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerTransactor) MapToken(opts *bind.TransactOpts, rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "mapToken", rootToken, childToken, tokenType)
}

// MapToken is a paid mutator transaction binding the contract method 0x9173b139.
//
// Solidity: function mapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerSession) MapToken(rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.MapToken(&_RootChainManager.TransactOpts, rootToken, childToken, tokenType)
}

// MapToken is a paid mutator transaction binding the contract method 0x9173b139.
//
// Solidity: function mapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerTransactorSession) MapToken(rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.MapToken(&_RootChainManager.TransactOpts, rootToken, childToken, tokenType)
}

// RegisterPredicate is a paid mutator transaction binding the contract method 0x0c598220.
//
// Solidity: function registerPredicate(bytes32 tokenType, address predicateAddress) returns()
func (_RootChainManager *RootChainManagerTransactor) RegisterPredicate(opts *bind.TransactOpts, tokenType [32]byte, predicateAddress common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "registerPredicate", tokenType, predicateAddress)
}

// RegisterPredicate is a paid mutator transaction binding the contract method 0x0c598220.
//
// Solidity: function registerPredicate(bytes32 tokenType, address predicateAddress) returns()
func (_RootChainManager *RootChainManagerSession) RegisterPredicate(tokenType [32]byte, predicateAddress common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RegisterPredicate(&_RootChainManager.TransactOpts, tokenType, predicateAddress)
}

// RegisterPredicate is a paid mutator transaction binding the contract method 0x0c598220.
//
// Solidity: function registerPredicate(bytes32 tokenType, address predicateAddress) returns()
func (_RootChainManager *RootChainManagerTransactorSession) RegisterPredicate(tokenType [32]byte, predicateAddress common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RegisterPredicate(&_RootChainManager.TransactOpts, tokenType, predicateAddress)
}

// RemapToken is a paid mutator transaction binding the contract method 0xd233a3c7.
//
// Solidity: function remapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerTransactor) RemapToken(opts *bind.TransactOpts, rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "remapToken", rootToken, childToken, tokenType)
}

// RemapToken is a paid mutator transaction binding the contract method 0xd233a3c7.
//
// Solidity: function remapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerSession) RemapToken(rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.RemapToken(&_RootChainManager.TransactOpts, rootToken, childToken, tokenType)
}

// RemapToken is a paid mutator transaction binding the contract method 0xd233a3c7.
//
// Solidity: function remapToken(address rootToken, address childToken, bytes32 tokenType) returns()
func (_RootChainManager *RootChainManagerTransactorSession) RemapToken(rootToken common.Address, childToken common.Address, tokenType [32]byte) (*types.Transaction, error) {
	return _RootChainManager.Contract.RemapToken(&_RootChainManager.TransactOpts, rootToken, childToken, tokenType)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "renounceRole", role, account)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerSession) RenounceRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RenounceRole(&_RootChainManager.TransactOpts, role, account)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactorSession) RenounceRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RenounceRole(&_RootChainManager.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RevokeRole(&_RootChainManager.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_RootChainManager *RootChainManagerTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.RevokeRole(&_RootChainManager.TransactOpts, role, account)
}

// SetCheckpointManager is a paid mutator transaction binding the contract method 0xbc08452b.
//
// Solidity: function setCheckpointManager(address newCheckpointManager) returns()
func (_RootChainManager *RootChainManagerTransactor) SetCheckpointManager(opts *bind.TransactOpts, newCheckpointManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "setCheckpointManager", newCheckpointManager)
}

// SetCheckpointManager is a paid mutator transaction binding the contract method 0xbc08452b.
//
// Solidity: function setCheckpointManager(address newCheckpointManager) returns()
func (_RootChainManager *RootChainManagerSession) SetCheckpointManager(newCheckpointManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetCheckpointManager(&_RootChainManager.TransactOpts, newCheckpointManager)
}

// SetCheckpointManager is a paid mutator transaction binding the contract method 0xbc08452b.
//
// Solidity: function setCheckpointManager(address newCheckpointManager) returns()
func (_RootChainManager *RootChainManagerTransactorSession) SetCheckpointManager(newCheckpointManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetCheckpointManager(&_RootChainManager.TransactOpts, newCheckpointManager)
}

// SetChildChainManagerAddress is a paid mutator transaction binding the contract method 0xdc993a23.
//
// Solidity: function setChildChainManagerAddress(address newChildChainManager) returns()
func (_RootChainManager *RootChainManagerTransactor) SetChildChainManagerAddress(opts *bind.TransactOpts, newChildChainManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "setChildChainManagerAddress", newChildChainManager)
}

// SetChildChainManagerAddress is a paid mutator transaction binding the contract method 0xdc993a23.
//
// Solidity: function setChildChainManagerAddress(address newChildChainManager) returns()
func (_RootChainManager *RootChainManagerSession) SetChildChainManagerAddress(newChildChainManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetChildChainManagerAddress(&_RootChainManager.TransactOpts, newChildChainManager)
}

// SetChildChainManagerAddress is a paid mutator transaction binding the contract method 0xdc993a23.
//
// Solidity: function setChildChainManagerAddress(address newChildChainManager) returns()
func (_RootChainManager *RootChainManagerTransactorSession) SetChildChainManagerAddress(newChildChainManager common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetChildChainManagerAddress(&_RootChainManager.TransactOpts, newChildChainManager)
}

// SetStateSender is a paid mutator transaction binding the contract method 0x6cb136b0.
//
// Solidity: function setStateSender(address newStateSender) returns()
func (_RootChainManager *RootChainManagerTransactor) SetStateSender(opts *bind.TransactOpts, newStateSender common.Address) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "setStateSender", newStateSender)
}

// SetStateSender is a paid mutator transaction binding the contract method 0x6cb136b0.
//
// Solidity: function setStateSender(address newStateSender) returns()
func (_RootChainManager *RootChainManagerSession) SetStateSender(newStateSender common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetStateSender(&_RootChainManager.TransactOpts, newStateSender)
}

// SetStateSender is a paid mutator transaction binding the contract method 0x6cb136b0.
//
// Solidity: function setStateSender(address newStateSender) returns()
func (_RootChainManager *RootChainManagerTransactorSession) SetStateSender(newStateSender common.Address) (*types.Transaction, error) {
	return _RootChainManager.Contract.SetStateSender(&_RootChainManager.TransactOpts, newStateSender)
}

// SetupContractId is a paid mutator transaction binding the contract method 0xb4b4f63e.
//
// Solidity: function setupContractId() returns()
func (_RootChainManager *RootChainManagerTransactor) SetupContractId(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RootChainManager.contract.Transact(opts, "setupContractId")
}

// SetupContractId is a paid mutator transaction binding the contract method 0xb4b4f63e.
//
// Solidity: function setupContractId() returns()
func (_RootChainManager *RootChainManagerSession) SetupContractId() (*types.Transaction, error) {
	return _RootChainManager.Contract.SetupContractId(&_RootChainManager.TransactOpts)
}

// SetupContractId is a paid mutator transaction binding the contract method 0xb4b4f63e.
//
// Solidity: function setupContractId() returns()
func (_RootChainManager *RootChainManagerTransactorSession) SetupContractId() (*types.Transaction, error) {
	return _RootChainManager.Contract.SetupContractId(&_RootChainManager.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RootChainManager *RootChainManagerTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _RootChainManager.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RootChainManager *RootChainManagerSession) Receive() (*types.Transaction, error) {
	return _RootChainManager.Contract.Receive(&_RootChainManager.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_RootChainManager *RootChainManagerTransactorSession) Receive() (*types.Transaction, error) {
	return _RootChainManager.Contract.Receive(&_RootChainManager.TransactOpts)
}

// RootChainManagerMetaTransactionExecutedIterator is returned from FilterMetaTransactionExecuted and is used to iterate over the raw logs and unpacked data for MetaTransactionExecuted events raised by the RootChainManager contract.
type RootChainManagerMetaTransactionExecutedIterator struct {
	Event *RootChainManagerMetaTransactionExecuted // Event containing the contract specifics and raw log

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
func (it *RootChainManagerMetaTransactionExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerMetaTransactionExecuted)
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
		it.Event = new(RootChainManagerMetaTransactionExecuted)
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
func (it *RootChainManagerMetaTransactionExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerMetaTransactionExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerMetaTransactionExecuted represents a MetaTransactionExecuted event raised by the RootChainManager contract.
type RootChainManagerMetaTransactionExecuted struct {
	UserAddress       common.Address
	RelayerAddress    common.Address
	FunctionSignature []byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterMetaTransactionExecuted is a free log retrieval operation binding the contract event 0x5845892132946850460bff5a0083f71031bc5bf9aadcd40f1de79423eac9b10b.
//
// Solidity: event MetaTransactionExecuted(address indexed userAddress, address indexed relayerAddress, bytes functionSignature)
func (_RootChainManager *RootChainManagerFilterer) FilterMetaTransactionExecuted(opts *bind.FilterOpts, userAddress []common.Address, relayerAddress []common.Address) (*RootChainManagerMetaTransactionExecutedIterator, error) {

	var userAddressRule []interface{}
	for _, userAddressItem := range userAddress {
		userAddressRule = append(userAddressRule, userAddressItem)
	}
	var relayerAddressRule []interface{}
	for _, relayerAddressItem := range relayerAddress {
		relayerAddressRule = append(relayerAddressRule, relayerAddressItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "MetaTransactionExecuted", userAddressRule, relayerAddressRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerMetaTransactionExecutedIterator{contract: _RootChainManager.contract, event: "MetaTransactionExecuted", logs: logs, sub: sub}, nil
}

// WatchMetaTransactionExecuted is a free log subscription operation binding the contract event 0x5845892132946850460bff5a0083f71031bc5bf9aadcd40f1de79423eac9b10b.
//
// Solidity: event MetaTransactionExecuted(address indexed userAddress, address indexed relayerAddress, bytes functionSignature)
func (_RootChainManager *RootChainManagerFilterer) WatchMetaTransactionExecuted(opts *bind.WatchOpts, sink chan<- *RootChainManagerMetaTransactionExecuted, userAddress []common.Address, relayerAddress []common.Address) (event.Subscription, error) {

	var userAddressRule []interface{}
	for _, userAddressItem := range userAddress {
		userAddressRule = append(userAddressRule, userAddressItem)
	}
	var relayerAddressRule []interface{}
	for _, relayerAddressItem := range relayerAddress {
		relayerAddressRule = append(relayerAddressRule, relayerAddressItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "MetaTransactionExecuted", userAddressRule, relayerAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerMetaTransactionExecuted)
				if err := _RootChainManager.contract.UnpackLog(event, "MetaTransactionExecuted", log); err != nil {
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

// ParseMetaTransactionExecuted is a log parse operation binding the contract event 0x5845892132946850460bff5a0083f71031bc5bf9aadcd40f1de79423eac9b10b.
//
// Solidity: event MetaTransactionExecuted(address indexed userAddress, address indexed relayerAddress, bytes functionSignature)
func (_RootChainManager *RootChainManagerFilterer) ParseMetaTransactionExecuted(log types.Log) (*RootChainManagerMetaTransactionExecuted, error) {
	event := new(RootChainManagerMetaTransactionExecuted)
	if err := _RootChainManager.contract.UnpackLog(event, "MetaTransactionExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// RootChainManagerPredicateRegisteredIterator is returned from FilterPredicateRegistered and is used to iterate over the raw logs and unpacked data for PredicateRegistered events raised by the RootChainManager contract.
type RootChainManagerPredicateRegisteredIterator struct {
	Event *RootChainManagerPredicateRegistered // Event containing the contract specifics and raw log

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
func (it *RootChainManagerPredicateRegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerPredicateRegistered)
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
		it.Event = new(RootChainManagerPredicateRegistered)
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
func (it *RootChainManagerPredicateRegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerPredicateRegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerPredicateRegistered represents a PredicateRegistered event raised by the RootChainManager contract.
type RootChainManagerPredicateRegistered struct {
	TokenType        [32]byte
	PredicateAddress common.Address
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterPredicateRegistered is a free log retrieval operation binding the contract event 0x8643692ae1c12ec91fa18e50b82ed93fa314f580999a236824db6de9ae0d839b.
//
// Solidity: event PredicateRegistered(bytes32 indexed tokenType, address indexed predicateAddress)
func (_RootChainManager *RootChainManagerFilterer) FilterPredicateRegistered(opts *bind.FilterOpts, tokenType [][32]byte, predicateAddress []common.Address) (*RootChainManagerPredicateRegisteredIterator, error) {

	var tokenTypeRule []interface{}
	for _, tokenTypeItem := range tokenType {
		tokenTypeRule = append(tokenTypeRule, tokenTypeItem)
	}
	var predicateAddressRule []interface{}
	for _, predicateAddressItem := range predicateAddress {
		predicateAddressRule = append(predicateAddressRule, predicateAddressItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "PredicateRegistered", tokenTypeRule, predicateAddressRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerPredicateRegisteredIterator{contract: _RootChainManager.contract, event: "PredicateRegistered", logs: logs, sub: sub}, nil
}

// WatchPredicateRegistered is a free log subscription operation binding the contract event 0x8643692ae1c12ec91fa18e50b82ed93fa314f580999a236824db6de9ae0d839b.
//
// Solidity: event PredicateRegistered(bytes32 indexed tokenType, address indexed predicateAddress)
func (_RootChainManager *RootChainManagerFilterer) WatchPredicateRegistered(opts *bind.WatchOpts, sink chan<- *RootChainManagerPredicateRegistered, tokenType [][32]byte, predicateAddress []common.Address) (event.Subscription, error) {

	var tokenTypeRule []interface{}
	for _, tokenTypeItem := range tokenType {
		tokenTypeRule = append(tokenTypeRule, tokenTypeItem)
	}
	var predicateAddressRule []interface{}
	for _, predicateAddressItem := range predicateAddress {
		predicateAddressRule = append(predicateAddressRule, predicateAddressItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "PredicateRegistered", tokenTypeRule, predicateAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerPredicateRegistered)
				if err := _RootChainManager.contract.UnpackLog(event, "PredicateRegistered", log); err != nil {
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

// ParsePredicateRegistered is a log parse operation binding the contract event 0x8643692ae1c12ec91fa18e50b82ed93fa314f580999a236824db6de9ae0d839b.
//
// Solidity: event PredicateRegistered(bytes32 indexed tokenType, address indexed predicateAddress)
func (_RootChainManager *RootChainManagerFilterer) ParsePredicateRegistered(log types.Log) (*RootChainManagerPredicateRegistered, error) {
	event := new(RootChainManagerPredicateRegistered)
	if err := _RootChainManager.contract.UnpackLog(event, "PredicateRegistered", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// RootChainManagerRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the RootChainManager contract.
type RootChainManagerRoleAdminChangedIterator struct {
	Event *RootChainManagerRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *RootChainManagerRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerRoleAdminChanged)
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
		it.Event = new(RootChainManagerRoleAdminChanged)
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
func (it *RootChainManagerRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerRoleAdminChanged represents a RoleAdminChanged event raised by the RootChainManager contract.
type RootChainManagerRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_RootChainManager *RootChainManagerFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*RootChainManagerRoleAdminChangedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var previousAdminRoleRule []interface{}
	for _, previousAdminRoleItem := range previousAdminRole {
		previousAdminRoleRule = append(previousAdminRoleRule, previousAdminRoleItem)
	}
	var newAdminRoleRule []interface{}
	for _, newAdminRoleItem := range newAdminRole {
		newAdminRoleRule = append(newAdminRoleRule, newAdminRoleItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerRoleAdminChangedIterator{contract: _RootChainManager.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_RootChainManager *RootChainManagerFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *RootChainManagerRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var previousAdminRoleRule []interface{}
	for _, previousAdminRoleItem := range previousAdminRole {
		previousAdminRoleRule = append(previousAdminRoleRule, previousAdminRoleItem)
	}
	var newAdminRoleRule []interface{}
	for _, newAdminRoleItem := range newAdminRole {
		newAdminRoleRule = append(newAdminRoleRule, newAdminRoleItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerRoleAdminChanged)
				if err := _RootChainManager.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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

// ParseRoleAdminChanged is a log parse operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_RootChainManager *RootChainManagerFilterer) ParseRoleAdminChanged(log types.Log) (*RootChainManagerRoleAdminChanged, error) {
	event := new(RootChainManagerRoleAdminChanged)
	if err := _RootChainManager.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// RootChainManagerRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the RootChainManager contract.
type RootChainManagerRoleGrantedIterator struct {
	Event *RootChainManagerRoleGranted // Event containing the contract specifics and raw log

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
func (it *RootChainManagerRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerRoleGranted)
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
		it.Event = new(RootChainManagerRoleGranted)
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
func (it *RootChainManagerRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerRoleGranted represents a RoleGranted event raised by the RootChainManager contract.
type RootChainManagerRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*RootChainManagerRoleGrantedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerRoleGrantedIterator{contract: _RootChainManager.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *RootChainManagerRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerRoleGranted)
				if err := _RootChainManager.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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

// ParseRoleGranted is a log parse operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) ParseRoleGranted(log types.Log) (*RootChainManagerRoleGranted, error) {
	event := new(RootChainManagerRoleGranted)
	if err := _RootChainManager.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// RootChainManagerRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the RootChainManager contract.
type RootChainManagerRoleRevokedIterator struct {
	Event *RootChainManagerRoleRevoked // Event containing the contract specifics and raw log

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
func (it *RootChainManagerRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerRoleRevoked)
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
		it.Event = new(RootChainManagerRoleRevoked)
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
func (it *RootChainManagerRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerRoleRevoked represents a RoleRevoked event raised by the RootChainManager contract.
type RootChainManagerRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*RootChainManagerRoleRevokedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerRoleRevokedIterator{contract: _RootChainManager.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *RootChainManagerRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerRoleRevoked)
				if err := _RootChainManager.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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

// ParseRoleRevoked is a log parse operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_RootChainManager *RootChainManagerFilterer) ParseRoleRevoked(log types.Log) (*RootChainManagerRoleRevoked, error) {
	event := new(RootChainManagerRoleRevoked)
	if err := _RootChainManager.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// RootChainManagerTokenMappedIterator is returned from FilterTokenMapped and is used to iterate over the raw logs and unpacked data for TokenMapped events raised by the RootChainManager contract.
type RootChainManagerTokenMappedIterator struct {
	Event *RootChainManagerTokenMapped // Event containing the contract specifics and raw log

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
func (it *RootChainManagerTokenMappedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(RootChainManagerTokenMapped)
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
		it.Event = new(RootChainManagerTokenMapped)
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
func (it *RootChainManagerTokenMappedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *RootChainManagerTokenMappedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// RootChainManagerTokenMapped represents a TokenMapped event raised by the RootChainManager contract.
type RootChainManagerTokenMapped struct {
	RootToken  common.Address
	ChildToken common.Address
	TokenType  [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterTokenMapped is a free log retrieval operation binding the contract event 0x9e651a8866fbea043e911d816ec254b0e3c992c06fff32d605e72362d6023bd9.
//
// Solidity: event TokenMapped(address indexed rootToken, address indexed childToken, bytes32 indexed tokenType)
func (_RootChainManager *RootChainManagerFilterer) FilterTokenMapped(opts *bind.FilterOpts, rootToken []common.Address, childToken []common.Address, tokenType [][32]byte) (*RootChainManagerTokenMappedIterator, error) {

	var rootTokenRule []interface{}
	for _, rootTokenItem := range rootToken {
		rootTokenRule = append(rootTokenRule, rootTokenItem)
	}
	var childTokenRule []interface{}
	for _, childTokenItem := range childToken {
		childTokenRule = append(childTokenRule, childTokenItem)
	}
	var tokenTypeRule []interface{}
	for _, tokenTypeItem := range tokenType {
		tokenTypeRule = append(tokenTypeRule, tokenTypeItem)
	}

	logs, sub, err := _RootChainManager.contract.FilterLogs(opts, "TokenMapped", rootTokenRule, childTokenRule, tokenTypeRule)
	if err != nil {
		return nil, err
	}
	return &RootChainManagerTokenMappedIterator{contract: _RootChainManager.contract, event: "TokenMapped", logs: logs, sub: sub}, nil
}

// WatchTokenMapped is a free log subscription operation binding the contract event 0x9e651a8866fbea043e911d816ec254b0e3c992c06fff32d605e72362d6023bd9.
//
// Solidity: event TokenMapped(address indexed rootToken, address indexed childToken, bytes32 indexed tokenType)
func (_RootChainManager *RootChainManagerFilterer) WatchTokenMapped(opts *bind.WatchOpts, sink chan<- *RootChainManagerTokenMapped, rootToken []common.Address, childToken []common.Address, tokenType [][32]byte) (event.Subscription, error) {

	var rootTokenRule []interface{}
	for _, rootTokenItem := range rootToken {
		rootTokenRule = append(rootTokenRule, rootTokenItem)
	}
	var childTokenRule []interface{}
	for _, childTokenItem := range childToken {
		childTokenRule = append(childTokenRule, childTokenItem)
	}
	var tokenTypeRule []interface{}
	for _, tokenTypeItem := range tokenType {
		tokenTypeRule = append(tokenTypeRule, tokenTypeItem)
	}

	logs, sub, err := _RootChainManager.contract.WatchLogs(opts, "TokenMapped", rootTokenRule, childTokenRule, tokenTypeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(RootChainManagerTokenMapped)
				if err := _RootChainManager.contract.UnpackLog(event, "TokenMapped", log); err != nil {
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

// ParseTokenMapped is a log parse operation binding the contract event 0x9e651a8866fbea043e911d816ec254b0e3c992c06fff32d605e72362d6023bd9.
//
// Solidity: event TokenMapped(address indexed rootToken, address indexed childToken, bytes32 indexed tokenType)
func (_RootChainManager *RootChainManagerFilterer) ParseTokenMapped(log types.Log) (*RootChainManagerTokenMapped, error) {
	event := new(RootChainManagerTokenMapped)
	if err := _RootChainManager.contract.UnpackLog(event, "TokenMapped", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
