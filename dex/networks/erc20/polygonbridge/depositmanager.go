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

// DepositManagerMetaData contains all meta data concerning the DepositManager contract.
var DepositManagerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"oldLimit\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"newLimit\",\"type\":\"uint256\"}],\"name\":\"MaxErc20DepositUpdate\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amountOrNFTId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"depositBlockId\",\"type\":\"uint256\"}],\"name\":\"NewDepositBlock\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"fallback\"},{\"constant\":true,\"inputs\":[],\"name\":\"childChain\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"_tokens\",\"type\":\"address[]\"},{\"internalType\":\"uint256[]\",\"name\":\"_amountOrTokens\",\"type\":\"uint256[]\"},{\"internalType\":\"address\",\"name\":\"_user\",\"type\":\"address\"}],\"name\":\"depositBulk\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"depositERC20\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_user\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amount\",\"type\":\"uint256\"}],\"name\":\"depositERC20ForUser\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_tokenId\",\"type\":\"uint256\"}],\"name\":\"depositERC721\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_user\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_tokenId\",\"type\":\"uint256\"}],\"name\":\"depositERC721ForUser\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"depositEther\",\"outputs\":[],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"deposits\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"depositHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"createdAt\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"governance\",\"outputs\":[{\"internalType\":\"contractIGovernance\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"isOwner\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"lock\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"locked\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"maxErc20Deposit\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"migrateMatic\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"\",\"type\":\"bytes\"}],\"name\":\"onERC721Received\",\"outputs\":[{\"internalType\":\"bytes4\",\"name\":\"\",\"type\":\"bytes4\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"registry\",\"outputs\":[{\"internalType\":\"contractRegistry\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"rootChain\",\"outputs\":[{\"internalType\":\"contractRootChain\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"stateSender\",\"outputs\":[{\"internalType\":\"contractStateSender\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_user\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"_amountOrNFTId\",\"type\":\"uint256\"}],\"name\":\"transferAssets\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"unlock\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"updateChildChainAndStateSender\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"maxDepositAmount\",\"type\":\"uint256\"}],\"name\":\"updateMaxErc20Deposit\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_rootChain\",\"type\":\"address\"}],\"name\":\"updateRootChain\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// DepositManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use DepositManagerMetaData.ABI instead.
var DepositManagerABI = DepositManagerMetaData.ABI

// DepositManager is an auto generated Go binding around an Ethereum contract.
type DepositManager struct {
	DepositManagerCaller     // Read-only binding to the contract
	DepositManagerTransactor // Write-only binding to the contract
	DepositManagerFilterer   // Log filterer for contract events
}

// DepositManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type DepositManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DepositManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type DepositManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DepositManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type DepositManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DepositManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type DepositManagerSession struct {
	Contract     *DepositManager   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// DepositManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type DepositManagerCallerSession struct {
	Contract *DepositManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// DepositManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type DepositManagerTransactorSession struct {
	Contract     *DepositManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// DepositManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type DepositManagerRaw struct {
	Contract *DepositManager // Generic contract binding to access the raw methods on
}

// DepositManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type DepositManagerCallerRaw struct {
	Contract *DepositManagerCaller // Generic read-only contract binding to access the raw methods on
}

// DepositManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type DepositManagerTransactorRaw struct {
	Contract *DepositManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewDepositManager creates a new instance of DepositManager, bound to a specific deployed contract.
func NewDepositManager(address common.Address, backend bind.ContractBackend) (*DepositManager, error) {
	contract, err := bindDepositManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &DepositManager{DepositManagerCaller: DepositManagerCaller{contract: contract}, DepositManagerTransactor: DepositManagerTransactor{contract: contract}, DepositManagerFilterer: DepositManagerFilterer{contract: contract}}, nil
}

// NewDepositManagerCaller creates a new read-only instance of DepositManager, bound to a specific deployed contract.
func NewDepositManagerCaller(address common.Address, caller bind.ContractCaller) (*DepositManagerCaller, error) {
	contract, err := bindDepositManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &DepositManagerCaller{contract: contract}, nil
}

// NewDepositManagerTransactor creates a new write-only instance of DepositManager, bound to a specific deployed contract.
func NewDepositManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*DepositManagerTransactor, error) {
	contract, err := bindDepositManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &DepositManagerTransactor{contract: contract}, nil
}

// NewDepositManagerFilterer creates a new log filterer instance of DepositManager, bound to a specific deployed contract.
func NewDepositManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*DepositManagerFilterer, error) {
	contract, err := bindDepositManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &DepositManagerFilterer{contract: contract}, nil
}

// bindDepositManager binds a generic wrapper to an already deployed contract.
func bindDepositManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := DepositManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_DepositManager *DepositManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _DepositManager.Contract.DepositManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_DepositManager *DepositManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_DepositManager *DepositManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_DepositManager *DepositManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _DepositManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_DepositManager *DepositManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_DepositManager *DepositManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _DepositManager.Contract.contract.Transact(opts, method, params...)
}

// ChildChain is a free data retrieval call binding the contract method 0x42fc47fb.
//
// Solidity: function childChain() view returns(address)
func (_DepositManager *DepositManagerCaller) ChildChain(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "childChain")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ChildChain is a free data retrieval call binding the contract method 0x42fc47fb.
//
// Solidity: function childChain() view returns(address)
func (_DepositManager *DepositManagerSession) ChildChain() (common.Address, error) {
	return _DepositManager.Contract.ChildChain(&_DepositManager.CallOpts)
}

// ChildChain is a free data retrieval call binding the contract method 0x42fc47fb.
//
// Solidity: function childChain() view returns(address)
func (_DepositManager *DepositManagerCallerSession) ChildChain() (common.Address, error) {
	return _DepositManager.Contract.ChildChain(&_DepositManager.CallOpts)
}

// Deposits is a free data retrieval call binding the contract method 0xb02c43d0.
//
// Solidity: function deposits(uint256 ) view returns(bytes32 depositHash, uint256 createdAt)
func (_DepositManager *DepositManagerCaller) Deposits(opts *bind.CallOpts, arg0 *big.Int) (struct {
	DepositHash [32]byte
	CreatedAt   *big.Int
}, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "deposits", arg0)

	outstruct := new(struct {
		DepositHash [32]byte
		CreatedAt   *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.DepositHash = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.CreatedAt = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// Deposits is a free data retrieval call binding the contract method 0xb02c43d0.
//
// Solidity: function deposits(uint256 ) view returns(bytes32 depositHash, uint256 createdAt)
func (_DepositManager *DepositManagerSession) Deposits(arg0 *big.Int) (struct {
	DepositHash [32]byte
	CreatedAt   *big.Int
}, error) {
	return _DepositManager.Contract.Deposits(&_DepositManager.CallOpts, arg0)
}

// Deposits is a free data retrieval call binding the contract method 0xb02c43d0.
//
// Solidity: function deposits(uint256 ) view returns(bytes32 depositHash, uint256 createdAt)
func (_DepositManager *DepositManagerCallerSession) Deposits(arg0 *big.Int) (struct {
	DepositHash [32]byte
	CreatedAt   *big.Int
}, error) {
	return _DepositManager.Contract.Deposits(&_DepositManager.CallOpts, arg0)
}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_DepositManager *DepositManagerCaller) Governance(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "governance")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_DepositManager *DepositManagerSession) Governance() (common.Address, error) {
	return _DepositManager.Contract.Governance(&_DepositManager.CallOpts)
}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_DepositManager *DepositManagerCallerSession) Governance() (common.Address, error) {
	return _DepositManager.Contract.Governance(&_DepositManager.CallOpts)
}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_DepositManager *DepositManagerCaller) IsOwner(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "isOwner")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_DepositManager *DepositManagerSession) IsOwner() (bool, error) {
	return _DepositManager.Contract.IsOwner(&_DepositManager.CallOpts)
}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_DepositManager *DepositManagerCallerSession) IsOwner() (bool, error) {
	return _DepositManager.Contract.IsOwner(&_DepositManager.CallOpts)
}

// Locked is a free data retrieval call binding the contract method 0xcf309012.
//
// Solidity: function locked() view returns(bool)
func (_DepositManager *DepositManagerCaller) Locked(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "locked")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Locked is a free data retrieval call binding the contract method 0xcf309012.
//
// Solidity: function locked() view returns(bool)
func (_DepositManager *DepositManagerSession) Locked() (bool, error) {
	return _DepositManager.Contract.Locked(&_DepositManager.CallOpts)
}

// Locked is a free data retrieval call binding the contract method 0xcf309012.
//
// Solidity: function locked() view returns(bool)
func (_DepositManager *DepositManagerCallerSession) Locked() (bool, error) {
	return _DepositManager.Contract.Locked(&_DepositManager.CallOpts)
}

// MaxErc20Deposit is a free data retrieval call binding the contract method 0xe7af7ba1.
//
// Solidity: function maxErc20Deposit() view returns(uint256)
func (_DepositManager *DepositManagerCaller) MaxErc20Deposit(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "maxErc20Deposit")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MaxErc20Deposit is a free data retrieval call binding the contract method 0xe7af7ba1.
//
// Solidity: function maxErc20Deposit() view returns(uint256)
func (_DepositManager *DepositManagerSession) MaxErc20Deposit() (*big.Int, error) {
	return _DepositManager.Contract.MaxErc20Deposit(&_DepositManager.CallOpts)
}

// MaxErc20Deposit is a free data retrieval call binding the contract method 0xe7af7ba1.
//
// Solidity: function maxErc20Deposit() view returns(uint256)
func (_DepositManager *DepositManagerCallerSession) MaxErc20Deposit() (*big.Int, error) {
	return _DepositManager.Contract.MaxErc20Deposit(&_DepositManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_DepositManager *DepositManagerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_DepositManager *DepositManagerSession) Owner() (common.Address, error) {
	return _DepositManager.Contract.Owner(&_DepositManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_DepositManager *DepositManagerCallerSession) Owner() (common.Address, error) {
	return _DepositManager.Contract.Owner(&_DepositManager.CallOpts)
}

// Registry is a free data retrieval call binding the contract method 0x7b103999.
//
// Solidity: function registry() view returns(address)
func (_DepositManager *DepositManagerCaller) Registry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "registry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Registry is a free data retrieval call binding the contract method 0x7b103999.
//
// Solidity: function registry() view returns(address)
func (_DepositManager *DepositManagerSession) Registry() (common.Address, error) {
	return _DepositManager.Contract.Registry(&_DepositManager.CallOpts)
}

// Registry is a free data retrieval call binding the contract method 0x7b103999.
//
// Solidity: function registry() view returns(address)
func (_DepositManager *DepositManagerCallerSession) Registry() (common.Address, error) {
	return _DepositManager.Contract.Registry(&_DepositManager.CallOpts)
}

// RootChain is a free data retrieval call binding the contract method 0x987ab9db.
//
// Solidity: function rootChain() view returns(address)
func (_DepositManager *DepositManagerCaller) RootChain(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "rootChain")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// RootChain is a free data retrieval call binding the contract method 0x987ab9db.
//
// Solidity: function rootChain() view returns(address)
func (_DepositManager *DepositManagerSession) RootChain() (common.Address, error) {
	return _DepositManager.Contract.RootChain(&_DepositManager.CallOpts)
}

// RootChain is a free data retrieval call binding the contract method 0x987ab9db.
//
// Solidity: function rootChain() view returns(address)
func (_DepositManager *DepositManagerCallerSession) RootChain() (common.Address, error) {
	return _DepositManager.Contract.RootChain(&_DepositManager.CallOpts)
}

// StateSender is a free data retrieval call binding the contract method 0xcb10f94c.
//
// Solidity: function stateSender() view returns(address)
func (_DepositManager *DepositManagerCaller) StateSender(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _DepositManager.contract.Call(opts, &out, "stateSender")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// StateSender is a free data retrieval call binding the contract method 0xcb10f94c.
//
// Solidity: function stateSender() view returns(address)
func (_DepositManager *DepositManagerSession) StateSender() (common.Address, error) {
	return _DepositManager.Contract.StateSender(&_DepositManager.CallOpts)
}

// StateSender is a free data retrieval call binding the contract method 0xcb10f94c.
//
// Solidity: function stateSender() view returns(address)
func (_DepositManager *DepositManagerCallerSession) StateSender() (common.Address, error) {
	return _DepositManager.Contract.StateSender(&_DepositManager.CallOpts)
}

// DepositBulk is a paid mutator transaction binding the contract method 0x7b1f7117.
//
// Solidity: function depositBulk(address[] _tokens, uint256[] _amountOrTokens, address _user) returns()
func (_DepositManager *DepositManagerTransactor) DepositBulk(opts *bind.TransactOpts, _tokens []common.Address, _amountOrTokens []*big.Int, _user common.Address) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositBulk", _tokens, _amountOrTokens, _user)
}

// DepositBulk is a paid mutator transaction binding the contract method 0x7b1f7117.
//
// Solidity: function depositBulk(address[] _tokens, uint256[] _amountOrTokens, address _user) returns()
func (_DepositManager *DepositManagerSession) DepositBulk(_tokens []common.Address, _amountOrTokens []*big.Int, _user common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositBulk(&_DepositManager.TransactOpts, _tokens, _amountOrTokens, _user)
}

// DepositBulk is a paid mutator transaction binding the contract method 0x7b1f7117.
//
// Solidity: function depositBulk(address[] _tokens, uint256[] _amountOrTokens, address _user) returns()
func (_DepositManager *DepositManagerTransactorSession) DepositBulk(_tokens []common.Address, _amountOrTokens []*big.Int, _user common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositBulk(&_DepositManager.TransactOpts, _tokens, _amountOrTokens, _user)
}

// DepositERC20 is a paid mutator transaction binding the contract method 0x97feb926.
//
// Solidity: function depositERC20(address _token, uint256 _amount) returns()
func (_DepositManager *DepositManagerTransactor) DepositERC20(opts *bind.TransactOpts, _token common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositERC20", _token, _amount)
}

// DepositERC20 is a paid mutator transaction binding the contract method 0x97feb926.
//
// Solidity: function depositERC20(address _token, uint256 _amount) returns()
func (_DepositManager *DepositManagerSession) DepositERC20(_token common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC20(&_DepositManager.TransactOpts, _token, _amount)
}

// DepositERC20 is a paid mutator transaction binding the contract method 0x97feb926.
//
// Solidity: function depositERC20(address _token, uint256 _amount) returns()
func (_DepositManager *DepositManagerTransactorSession) DepositERC20(_token common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC20(&_DepositManager.TransactOpts, _token, _amount)
}

// DepositERC20ForUser is a paid mutator transaction binding the contract method 0x8b9e4f93.
//
// Solidity: function depositERC20ForUser(address _token, address _user, uint256 _amount) returns()
func (_DepositManager *DepositManagerTransactor) DepositERC20ForUser(opts *bind.TransactOpts, _token common.Address, _user common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositERC20ForUser", _token, _user, _amount)
}

// DepositERC20ForUser is a paid mutator transaction binding the contract method 0x8b9e4f93.
//
// Solidity: function depositERC20ForUser(address _token, address _user, uint256 _amount) returns()
func (_DepositManager *DepositManagerSession) DepositERC20ForUser(_token common.Address, _user common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC20ForUser(&_DepositManager.TransactOpts, _token, _user, _amount)
}

// DepositERC20ForUser is a paid mutator transaction binding the contract method 0x8b9e4f93.
//
// Solidity: function depositERC20ForUser(address _token, address _user, uint256 _amount) returns()
func (_DepositManager *DepositManagerTransactorSession) DepositERC20ForUser(_token common.Address, _user common.Address, _amount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC20ForUser(&_DepositManager.TransactOpts, _token, _user, _amount)
}

// DepositERC721 is a paid mutator transaction binding the contract method 0xd29a4bf6.
//
// Solidity: function depositERC721(address _token, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerTransactor) DepositERC721(opts *bind.TransactOpts, _token common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositERC721", _token, _tokenId)
}

// DepositERC721 is a paid mutator transaction binding the contract method 0xd29a4bf6.
//
// Solidity: function depositERC721(address _token, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerSession) DepositERC721(_token common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC721(&_DepositManager.TransactOpts, _token, _tokenId)
}

// DepositERC721 is a paid mutator transaction binding the contract method 0xd29a4bf6.
//
// Solidity: function depositERC721(address _token, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerTransactorSession) DepositERC721(_token common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC721(&_DepositManager.TransactOpts, _token, _tokenId)
}

// DepositERC721ForUser is a paid mutator transaction binding the contract method 0x072b1535.
//
// Solidity: function depositERC721ForUser(address _token, address _user, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerTransactor) DepositERC721ForUser(opts *bind.TransactOpts, _token common.Address, _user common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositERC721ForUser", _token, _user, _tokenId)
}

// DepositERC721ForUser is a paid mutator transaction binding the contract method 0x072b1535.
//
// Solidity: function depositERC721ForUser(address _token, address _user, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerSession) DepositERC721ForUser(_token common.Address, _user common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC721ForUser(&_DepositManager.TransactOpts, _token, _user, _tokenId)
}

// DepositERC721ForUser is a paid mutator transaction binding the contract method 0x072b1535.
//
// Solidity: function depositERC721ForUser(address _token, address _user, uint256 _tokenId) returns()
func (_DepositManager *DepositManagerTransactorSession) DepositERC721ForUser(_token common.Address, _user common.Address, _tokenId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.DepositERC721ForUser(&_DepositManager.TransactOpts, _token, _user, _tokenId)
}

// DepositEther is a paid mutator transaction binding the contract method 0x98ea5fca.
//
// Solidity: function depositEther() payable returns()
func (_DepositManager *DepositManagerTransactor) DepositEther(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "depositEther")
}

// DepositEther is a paid mutator transaction binding the contract method 0x98ea5fca.
//
// Solidity: function depositEther() payable returns()
func (_DepositManager *DepositManagerSession) DepositEther() (*types.Transaction, error) {
	return _DepositManager.Contract.DepositEther(&_DepositManager.TransactOpts)
}

// DepositEther is a paid mutator transaction binding the contract method 0x98ea5fca.
//
// Solidity: function depositEther() payable returns()
func (_DepositManager *DepositManagerTransactorSession) DepositEther() (*types.Transaction, error) {
	return _DepositManager.Contract.DepositEther(&_DepositManager.TransactOpts)
}

// Lock is a paid mutator transaction binding the contract method 0xf83d08ba.
//
// Solidity: function lock() returns()
func (_DepositManager *DepositManagerTransactor) Lock(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "lock")
}

// Lock is a paid mutator transaction binding the contract method 0xf83d08ba.
//
// Solidity: function lock() returns()
func (_DepositManager *DepositManagerSession) Lock() (*types.Transaction, error) {
	return _DepositManager.Contract.Lock(&_DepositManager.TransactOpts)
}

// Lock is a paid mutator transaction binding the contract method 0xf83d08ba.
//
// Solidity: function lock() returns()
func (_DepositManager *DepositManagerTransactorSession) Lock() (*types.Transaction, error) {
	return _DepositManager.Contract.Lock(&_DepositManager.TransactOpts)
}

// MigrateMatic is a paid mutator transaction binding the contract method 0x889a0538.
//
// Solidity: function migrateMatic() returns()
func (_DepositManager *DepositManagerTransactor) MigrateMatic(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "migrateMatic")
}

// MigrateMatic is a paid mutator transaction binding the contract method 0x889a0538.
//
// Solidity: function migrateMatic() returns()
func (_DepositManager *DepositManagerSession) MigrateMatic() (*types.Transaction, error) {
	return _DepositManager.Contract.MigrateMatic(&_DepositManager.TransactOpts)
}

// MigrateMatic is a paid mutator transaction binding the contract method 0x889a0538.
//
// Solidity: function migrateMatic() returns()
func (_DepositManager *DepositManagerTransactorSession) MigrateMatic() (*types.Transaction, error) {
	return _DepositManager.Contract.MigrateMatic(&_DepositManager.TransactOpts)
}

// OnERC721Received is a paid mutator transaction binding the contract method 0x150b7a02.
//
// Solidity: function onERC721Received(address , address , uint256 , bytes ) returns(bytes4)
func (_DepositManager *DepositManagerTransactor) OnERC721Received(opts *bind.TransactOpts, arg0 common.Address, arg1 common.Address, arg2 *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "onERC721Received", arg0, arg1, arg2, arg3)
}

// OnERC721Received is a paid mutator transaction binding the contract method 0x150b7a02.
//
// Solidity: function onERC721Received(address , address , uint256 , bytes ) returns(bytes4)
func (_DepositManager *DepositManagerSession) OnERC721Received(arg0 common.Address, arg1 common.Address, arg2 *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _DepositManager.Contract.OnERC721Received(&_DepositManager.TransactOpts, arg0, arg1, arg2, arg3)
}

// OnERC721Received is a paid mutator transaction binding the contract method 0x150b7a02.
//
// Solidity: function onERC721Received(address , address , uint256 , bytes ) returns(bytes4)
func (_DepositManager *DepositManagerTransactorSession) OnERC721Received(arg0 common.Address, arg1 common.Address, arg2 *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _DepositManager.Contract.OnERC721Received(&_DepositManager.TransactOpts, arg0, arg1, arg2, arg3)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_DepositManager *DepositManagerTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_DepositManager *DepositManagerSession) RenounceOwnership() (*types.Transaction, error) {
	return _DepositManager.Contract.RenounceOwnership(&_DepositManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_DepositManager *DepositManagerTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _DepositManager.Contract.RenounceOwnership(&_DepositManager.TransactOpts)
}

// TransferAssets is a paid mutator transaction binding the contract method 0x49f4cc17.
//
// Solidity: function transferAssets(address _token, address _user, uint256 _amountOrNFTId) returns()
func (_DepositManager *DepositManagerTransactor) TransferAssets(opts *bind.TransactOpts, _token common.Address, _user common.Address, _amountOrNFTId *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "transferAssets", _token, _user, _amountOrNFTId)
}

// TransferAssets is a paid mutator transaction binding the contract method 0x49f4cc17.
//
// Solidity: function transferAssets(address _token, address _user, uint256 _amountOrNFTId) returns()
func (_DepositManager *DepositManagerSession) TransferAssets(_token common.Address, _user common.Address, _amountOrNFTId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.TransferAssets(&_DepositManager.TransactOpts, _token, _user, _amountOrNFTId)
}

// TransferAssets is a paid mutator transaction binding the contract method 0x49f4cc17.
//
// Solidity: function transferAssets(address _token, address _user, uint256 _amountOrNFTId) returns()
func (_DepositManager *DepositManagerTransactorSession) TransferAssets(_token common.Address, _user common.Address, _amountOrNFTId *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.TransferAssets(&_DepositManager.TransactOpts, _token, _user, _amountOrNFTId)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_DepositManager *DepositManagerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_DepositManager *DepositManagerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.TransferOwnership(&_DepositManager.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_DepositManager *DepositManagerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.TransferOwnership(&_DepositManager.TransactOpts, newOwner)
}

// Unlock is a paid mutator transaction binding the contract method 0xa69df4b5.
//
// Solidity: function unlock() returns()
func (_DepositManager *DepositManagerTransactor) Unlock(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "unlock")
}

// Unlock is a paid mutator transaction binding the contract method 0xa69df4b5.
//
// Solidity: function unlock() returns()
func (_DepositManager *DepositManagerSession) Unlock() (*types.Transaction, error) {
	return _DepositManager.Contract.Unlock(&_DepositManager.TransactOpts)
}

// Unlock is a paid mutator transaction binding the contract method 0xa69df4b5.
//
// Solidity: function unlock() returns()
func (_DepositManager *DepositManagerTransactorSession) Unlock() (*types.Transaction, error) {
	return _DepositManager.Contract.Unlock(&_DepositManager.TransactOpts)
}

// UpdateChildChainAndStateSender is a paid mutator transaction binding the contract method 0x42be8379.
//
// Solidity: function updateChildChainAndStateSender() returns()
func (_DepositManager *DepositManagerTransactor) UpdateChildChainAndStateSender(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "updateChildChainAndStateSender")
}

// UpdateChildChainAndStateSender is a paid mutator transaction binding the contract method 0x42be8379.
//
// Solidity: function updateChildChainAndStateSender() returns()
func (_DepositManager *DepositManagerSession) UpdateChildChainAndStateSender() (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateChildChainAndStateSender(&_DepositManager.TransactOpts)
}

// UpdateChildChainAndStateSender is a paid mutator transaction binding the contract method 0x42be8379.
//
// Solidity: function updateChildChainAndStateSender() returns()
func (_DepositManager *DepositManagerTransactorSession) UpdateChildChainAndStateSender() (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateChildChainAndStateSender(&_DepositManager.TransactOpts)
}

// UpdateMaxErc20Deposit is a paid mutator transaction binding the contract method 0x4b56c071.
//
// Solidity: function updateMaxErc20Deposit(uint256 maxDepositAmount) returns()
func (_DepositManager *DepositManagerTransactor) UpdateMaxErc20Deposit(opts *bind.TransactOpts, maxDepositAmount *big.Int) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "updateMaxErc20Deposit", maxDepositAmount)
}

// UpdateMaxErc20Deposit is a paid mutator transaction binding the contract method 0x4b56c071.
//
// Solidity: function updateMaxErc20Deposit(uint256 maxDepositAmount) returns()
func (_DepositManager *DepositManagerSession) UpdateMaxErc20Deposit(maxDepositAmount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateMaxErc20Deposit(&_DepositManager.TransactOpts, maxDepositAmount)
}

// UpdateMaxErc20Deposit is a paid mutator transaction binding the contract method 0x4b56c071.
//
// Solidity: function updateMaxErc20Deposit(uint256 maxDepositAmount) returns()
func (_DepositManager *DepositManagerTransactorSession) UpdateMaxErc20Deposit(maxDepositAmount *big.Int) (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateMaxErc20Deposit(&_DepositManager.TransactOpts, maxDepositAmount)
}

// UpdateRootChain is a paid mutator transaction binding the contract method 0xf2203711.
//
// Solidity: function updateRootChain(address _rootChain) returns()
func (_DepositManager *DepositManagerTransactor) UpdateRootChain(opts *bind.TransactOpts, _rootChain common.Address) (*types.Transaction, error) {
	return _DepositManager.contract.Transact(opts, "updateRootChain", _rootChain)
}

// UpdateRootChain is a paid mutator transaction binding the contract method 0xf2203711.
//
// Solidity: function updateRootChain(address _rootChain) returns()
func (_DepositManager *DepositManagerSession) UpdateRootChain(_rootChain common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateRootChain(&_DepositManager.TransactOpts, _rootChain)
}

// UpdateRootChain is a paid mutator transaction binding the contract method 0xf2203711.
//
// Solidity: function updateRootChain(address _rootChain) returns()
func (_DepositManager *DepositManagerTransactorSession) UpdateRootChain(_rootChain common.Address) (*types.Transaction, error) {
	return _DepositManager.Contract.UpdateRootChain(&_DepositManager.TransactOpts, _rootChain)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_DepositManager *DepositManagerTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _DepositManager.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_DepositManager *DepositManagerSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _DepositManager.Contract.Fallback(&_DepositManager.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_DepositManager *DepositManagerTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _DepositManager.Contract.Fallback(&_DepositManager.TransactOpts, calldata)
}

// DepositManagerMaxErc20DepositUpdateIterator is returned from FilterMaxErc20DepositUpdate and is used to iterate over the raw logs and unpacked data for MaxErc20DepositUpdate events raised by the DepositManager contract.
type DepositManagerMaxErc20DepositUpdateIterator struct {
	Event *DepositManagerMaxErc20DepositUpdate // Event containing the contract specifics and raw log

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
func (it *DepositManagerMaxErc20DepositUpdateIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(DepositManagerMaxErc20DepositUpdate)
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
		it.Event = new(DepositManagerMaxErc20DepositUpdate)
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
func (it *DepositManagerMaxErc20DepositUpdateIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *DepositManagerMaxErc20DepositUpdateIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// DepositManagerMaxErc20DepositUpdate represents a MaxErc20DepositUpdate event raised by the DepositManager contract.
type DepositManagerMaxErc20DepositUpdate struct {
	OldLimit *big.Int
	NewLimit *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterMaxErc20DepositUpdate is a free log retrieval operation binding the contract event 0x010c0265813c273963aa5e8683cf5c45a3b744ba6369c22af0958ec5fcf16b20.
//
// Solidity: event MaxErc20DepositUpdate(uint256 indexed oldLimit, uint256 indexed newLimit)
func (_DepositManager *DepositManagerFilterer) FilterMaxErc20DepositUpdate(opts *bind.FilterOpts, oldLimit []*big.Int, newLimit []*big.Int) (*DepositManagerMaxErc20DepositUpdateIterator, error) {

	var oldLimitRule []interface{}
	for _, oldLimitItem := range oldLimit {
		oldLimitRule = append(oldLimitRule, oldLimitItem)
	}
	var newLimitRule []interface{}
	for _, newLimitItem := range newLimit {
		newLimitRule = append(newLimitRule, newLimitItem)
	}

	logs, sub, err := _DepositManager.contract.FilterLogs(opts, "MaxErc20DepositUpdate", oldLimitRule, newLimitRule)
	if err != nil {
		return nil, err
	}
	return &DepositManagerMaxErc20DepositUpdateIterator{contract: _DepositManager.contract, event: "MaxErc20DepositUpdate", logs: logs, sub: sub}, nil
}

// WatchMaxErc20DepositUpdate is a free log subscription operation binding the contract event 0x010c0265813c273963aa5e8683cf5c45a3b744ba6369c22af0958ec5fcf16b20.
//
// Solidity: event MaxErc20DepositUpdate(uint256 indexed oldLimit, uint256 indexed newLimit)
func (_DepositManager *DepositManagerFilterer) WatchMaxErc20DepositUpdate(opts *bind.WatchOpts, sink chan<- *DepositManagerMaxErc20DepositUpdate, oldLimit []*big.Int, newLimit []*big.Int) (event.Subscription, error) {

	var oldLimitRule []interface{}
	for _, oldLimitItem := range oldLimit {
		oldLimitRule = append(oldLimitRule, oldLimitItem)
	}
	var newLimitRule []interface{}
	for _, newLimitItem := range newLimit {
		newLimitRule = append(newLimitRule, newLimitItem)
	}

	logs, sub, err := _DepositManager.contract.WatchLogs(opts, "MaxErc20DepositUpdate", oldLimitRule, newLimitRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(DepositManagerMaxErc20DepositUpdate)
				if err := _DepositManager.contract.UnpackLog(event, "MaxErc20DepositUpdate", log); err != nil {
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

// ParseMaxErc20DepositUpdate is a log parse operation binding the contract event 0x010c0265813c273963aa5e8683cf5c45a3b744ba6369c22af0958ec5fcf16b20.
//
// Solidity: event MaxErc20DepositUpdate(uint256 indexed oldLimit, uint256 indexed newLimit)
func (_DepositManager *DepositManagerFilterer) ParseMaxErc20DepositUpdate(log types.Log) (*DepositManagerMaxErc20DepositUpdate, error) {
	event := new(DepositManagerMaxErc20DepositUpdate)
	if err := _DepositManager.contract.UnpackLog(event, "MaxErc20DepositUpdate", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// DepositManagerNewDepositBlockIterator is returned from FilterNewDepositBlock and is used to iterate over the raw logs and unpacked data for NewDepositBlock events raised by the DepositManager contract.
type DepositManagerNewDepositBlockIterator struct {
	Event *DepositManagerNewDepositBlock // Event containing the contract specifics and raw log

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
func (it *DepositManagerNewDepositBlockIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(DepositManagerNewDepositBlock)
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
		it.Event = new(DepositManagerNewDepositBlock)
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
func (it *DepositManagerNewDepositBlockIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *DepositManagerNewDepositBlockIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// DepositManagerNewDepositBlock represents a NewDepositBlock event raised by the DepositManager contract.
type DepositManagerNewDepositBlock struct {
	Owner          common.Address
	Token          common.Address
	AmountOrNFTId  *big.Int
	DepositBlockId *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterNewDepositBlock is a free log retrieval operation binding the contract event 0x1dadc8d0683c6f9824e885935c1bec6f76816730dcec148dda8cf25a7b9f797b.
//
// Solidity: event NewDepositBlock(address indexed owner, address indexed token, uint256 amountOrNFTId, uint256 depositBlockId)
func (_DepositManager *DepositManagerFilterer) FilterNewDepositBlock(opts *bind.FilterOpts, owner []common.Address, token []common.Address) (*DepositManagerNewDepositBlockIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _DepositManager.contract.FilterLogs(opts, "NewDepositBlock", ownerRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return &DepositManagerNewDepositBlockIterator{contract: _DepositManager.contract, event: "NewDepositBlock", logs: logs, sub: sub}, nil
}

// WatchNewDepositBlock is a free log subscription operation binding the contract event 0x1dadc8d0683c6f9824e885935c1bec6f76816730dcec148dda8cf25a7b9f797b.
//
// Solidity: event NewDepositBlock(address indexed owner, address indexed token, uint256 amountOrNFTId, uint256 depositBlockId)
func (_DepositManager *DepositManagerFilterer) WatchNewDepositBlock(opts *bind.WatchOpts, sink chan<- *DepositManagerNewDepositBlock, owner []common.Address, token []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _DepositManager.contract.WatchLogs(opts, "NewDepositBlock", ownerRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(DepositManagerNewDepositBlock)
				if err := _DepositManager.contract.UnpackLog(event, "NewDepositBlock", log); err != nil {
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

// ParseNewDepositBlock is a log parse operation binding the contract event 0x1dadc8d0683c6f9824e885935c1bec6f76816730dcec148dda8cf25a7b9f797b.
//
// Solidity: event NewDepositBlock(address indexed owner, address indexed token, uint256 amountOrNFTId, uint256 depositBlockId)
func (_DepositManager *DepositManagerFilterer) ParseNewDepositBlock(log types.Log) (*DepositManagerNewDepositBlock, error) {
	event := new(DepositManagerNewDepositBlock)
	if err := _DepositManager.contract.UnpackLog(event, "NewDepositBlock", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// DepositManagerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the DepositManager contract.
type DepositManagerOwnershipTransferredIterator struct {
	Event *DepositManagerOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *DepositManagerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(DepositManagerOwnershipTransferred)
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
		it.Event = new(DepositManagerOwnershipTransferred)
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
func (it *DepositManagerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *DepositManagerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// DepositManagerOwnershipTransferred represents a OwnershipTransferred event raised by the DepositManager contract.
type DepositManagerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_DepositManager *DepositManagerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*DepositManagerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _DepositManager.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &DepositManagerOwnershipTransferredIterator{contract: _DepositManager.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_DepositManager *DepositManagerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *DepositManagerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _DepositManager.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(DepositManagerOwnershipTransferred)
				if err := _DepositManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_DepositManager *DepositManagerFilterer) ParseOwnershipTransferred(log types.Log) (*DepositManagerOwnershipTransferred, error) {
	event := new(DepositManagerOwnershipTransferred)
	if err := _DepositManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
