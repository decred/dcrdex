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

// WithdrawManagerMetaData contains all meta data concerning the WithdrawManager contract.
var WithdrawManagerMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"}],\"name\":\"ExitCancelled\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"oldExitPeriod\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"newExitPeriod\",\"type\":\"uint256\"}],\"name\":\"ExitPeriodUpdate\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"exitor\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"isRegularExit\",\"type\":\"bool\"}],\"name\":\"ExitStarted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"age\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"signer\",\"type\":\"address\"}],\"name\":\"ExitUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"user\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"Withdraw\",\"type\":\"event\"},{\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"fallback\"},{\"constant\":true,\"inputs\":[],\"name\":\"HALF_EXIT_PERIOD\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"ON_FINALIZE_GAS_LIMIT\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"exitor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"childToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"rootToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"exitAmountOrTokenId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"txHash\",\"type\":\"bytes32\"},{\"internalType\":\"bool\",\"name\":\"isRegularExit\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"priority\",\"type\":\"uint256\"}],\"name\":\"addExitToQueue\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"age\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"utxoOwner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"addInput\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"exitId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"inputId\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"challengeData\",\"type\":\"bytes\"},{\"internalType\":\"address\",\"name\":\"adjudicatorPredicate\",\"type\":\"address\"}],\"name\":\"challengeExit\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"createExitQueue\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"exitNft\",\"outputs\":[{\"internalType\":\"contractExitNFT\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"exitWindow\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"exits\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"receiptAmountOrNFTId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"txHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"bool\",\"name\":\"isRegularExit\",\"type\":\"bool\"},{\"internalType\":\"address\",\"name\":\"predicate\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"exitsQueues\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"isOwner\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"ownerExits\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"_token\",\"type\":\"address\"}],\"name\":\"processExits\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"_tokens\",\"type\":\"address[]\"}],\"name\":\"processExitsBatch\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amountOrToken\",\"type\":\"uint256\"}],\"name\":\"startExitWithDepositedTokens\",\"outputs\":[],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"halfExitPeriod\",\"type\":\"uint256\"}],\"name\":\"updateExitPeriod\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"},{\"internalType\":\"uint8\",\"name\":\"offset\",\"type\":\"uint8\"},{\"internalType\":\"bool\",\"name\":\"verifyTxInclusion\",\"type\":\"bool\"}],\"name\":\"verifyInclusion\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// WithdrawManagerABI is the input ABI used to generate the binding from.
// Deprecated: Use WithdrawManagerMetaData.ABI instead.
var WithdrawManagerABI = WithdrawManagerMetaData.ABI

// WithdrawManager is an auto generated Go binding around an Ethereum contract.
type WithdrawManager struct {
	WithdrawManagerCaller     // Read-only binding to the contract
	WithdrawManagerTransactor // Write-only binding to the contract
	WithdrawManagerFilterer   // Log filterer for contract events
}

// WithdrawManagerCaller is an auto generated read-only Go binding around an Ethereum contract.
type WithdrawManagerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WithdrawManagerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type WithdrawManagerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WithdrawManagerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type WithdrawManagerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// WithdrawManagerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type WithdrawManagerSession struct {
	Contract     *WithdrawManager  // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// WithdrawManagerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type WithdrawManagerCallerSession struct {
	Contract *WithdrawManagerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts          // Call options to use throughout this session
}

// WithdrawManagerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type WithdrawManagerTransactorSession struct {
	Contract     *WithdrawManagerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts          // Transaction auth options to use throughout this session
}

// WithdrawManagerRaw is an auto generated low-level Go binding around an Ethereum contract.
type WithdrawManagerRaw struct {
	Contract *WithdrawManager // Generic contract binding to access the raw methods on
}

// WithdrawManagerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type WithdrawManagerCallerRaw struct {
	Contract *WithdrawManagerCaller // Generic read-only contract binding to access the raw methods on
}

// WithdrawManagerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type WithdrawManagerTransactorRaw struct {
	Contract *WithdrawManagerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewWithdrawManager creates a new instance of WithdrawManager, bound to a specific deployed contract.
func NewWithdrawManager(address common.Address, backend bind.ContractBackend) (*WithdrawManager, error) {
	contract, err := bindWithdrawManager(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &WithdrawManager{WithdrawManagerCaller: WithdrawManagerCaller{contract: contract}, WithdrawManagerTransactor: WithdrawManagerTransactor{contract: contract}, WithdrawManagerFilterer: WithdrawManagerFilterer{contract: contract}}, nil
}

// NewWithdrawManagerCaller creates a new read-only instance of WithdrawManager, bound to a specific deployed contract.
func NewWithdrawManagerCaller(address common.Address, caller bind.ContractCaller) (*WithdrawManagerCaller, error) {
	contract, err := bindWithdrawManager(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerCaller{contract: contract}, nil
}

// NewWithdrawManagerTransactor creates a new write-only instance of WithdrawManager, bound to a specific deployed contract.
func NewWithdrawManagerTransactor(address common.Address, transactor bind.ContractTransactor) (*WithdrawManagerTransactor, error) {
	contract, err := bindWithdrawManager(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerTransactor{contract: contract}, nil
}

// NewWithdrawManagerFilterer creates a new log filterer instance of WithdrawManager, bound to a specific deployed contract.
func NewWithdrawManagerFilterer(address common.Address, filterer bind.ContractFilterer) (*WithdrawManagerFilterer, error) {
	contract, err := bindWithdrawManager(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerFilterer{contract: contract}, nil
}

// bindWithdrawManager binds a generic wrapper to an already deployed contract.
func bindWithdrawManager(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := WithdrawManagerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_WithdrawManager *WithdrawManagerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _WithdrawManager.Contract.WithdrawManagerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_WithdrawManager *WithdrawManagerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _WithdrawManager.Contract.WithdrawManagerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_WithdrawManager *WithdrawManagerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _WithdrawManager.Contract.WithdrawManagerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_WithdrawManager *WithdrawManagerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _WithdrawManager.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_WithdrawManager *WithdrawManagerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _WithdrawManager.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_WithdrawManager *WithdrawManagerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _WithdrawManager.Contract.contract.Transact(opts, method, params...)
}

// HALFEXITPERIOD is a free data retrieval call binding the contract method 0xed4a0be8.
//
// Solidity: function HALF_EXIT_PERIOD() view returns(uint256)
func (_WithdrawManager *WithdrawManagerCaller) HALFEXITPERIOD(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "HALF_EXIT_PERIOD")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// HALFEXITPERIOD is a free data retrieval call binding the contract method 0xed4a0be8.
//
// Solidity: function HALF_EXIT_PERIOD() view returns(uint256)
func (_WithdrawManager *WithdrawManagerSession) HALFEXITPERIOD() (*big.Int, error) {
	return _WithdrawManager.Contract.HALFEXITPERIOD(&_WithdrawManager.CallOpts)
}

// HALFEXITPERIOD is a free data retrieval call binding the contract method 0xed4a0be8.
//
// Solidity: function HALF_EXIT_PERIOD() view returns(uint256)
func (_WithdrawManager *WithdrawManagerCallerSession) HALFEXITPERIOD() (*big.Int, error) {
	return _WithdrawManager.Contract.HALFEXITPERIOD(&_WithdrawManager.CallOpts)
}

// ONFINALIZEGASLIMIT is a free data retrieval call binding the contract method 0x96cbd812.
//
// Solidity: function ON_FINALIZE_GAS_LIMIT() view returns(uint32)
func (_WithdrawManager *WithdrawManagerCaller) ONFINALIZEGASLIMIT(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "ON_FINALIZE_GAS_LIMIT")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// ONFINALIZEGASLIMIT is a free data retrieval call binding the contract method 0x96cbd812.
//
// Solidity: function ON_FINALIZE_GAS_LIMIT() view returns(uint32)
func (_WithdrawManager *WithdrawManagerSession) ONFINALIZEGASLIMIT() (uint32, error) {
	return _WithdrawManager.Contract.ONFINALIZEGASLIMIT(&_WithdrawManager.CallOpts)
}

// ONFINALIZEGASLIMIT is a free data retrieval call binding the contract method 0x96cbd812.
//
// Solidity: function ON_FINALIZE_GAS_LIMIT() view returns(uint32)
func (_WithdrawManager *WithdrawManagerCallerSession) ONFINALIZEGASLIMIT() (uint32, error) {
	return _WithdrawManager.Contract.ONFINALIZEGASLIMIT(&_WithdrawManager.CallOpts)
}

// ExitNft is a free data retrieval call binding the contract method 0xedeca09b.
//
// Solidity: function exitNft() view returns(address)
func (_WithdrawManager *WithdrawManagerCaller) ExitNft(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "exitNft")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ExitNft is a free data retrieval call binding the contract method 0xedeca09b.
//
// Solidity: function exitNft() view returns(address)
func (_WithdrawManager *WithdrawManagerSession) ExitNft() (common.Address, error) {
	return _WithdrawManager.Contract.ExitNft(&_WithdrawManager.CallOpts)
}

// ExitNft is a free data retrieval call binding the contract method 0xedeca09b.
//
// Solidity: function exitNft() view returns(address)
func (_WithdrawManager *WithdrawManagerCallerSession) ExitNft() (common.Address, error) {
	return _WithdrawManager.Contract.ExitNft(&_WithdrawManager.CallOpts)
}

// ExitWindow is a free data retrieval call binding the contract method 0x1e29848b.
//
// Solidity: function exitWindow() view returns(uint256)
func (_WithdrawManager *WithdrawManagerCaller) ExitWindow(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "exitWindow")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ExitWindow is a free data retrieval call binding the contract method 0x1e29848b.
//
// Solidity: function exitWindow() view returns(uint256)
func (_WithdrawManager *WithdrawManagerSession) ExitWindow() (*big.Int, error) {
	return _WithdrawManager.Contract.ExitWindow(&_WithdrawManager.CallOpts)
}

// ExitWindow is a free data retrieval call binding the contract method 0x1e29848b.
//
// Solidity: function exitWindow() view returns(uint256)
func (_WithdrawManager *WithdrawManagerCallerSession) ExitWindow() (*big.Int, error) {
	return _WithdrawManager.Contract.ExitWindow(&_WithdrawManager.CallOpts)
}

// Exits is a free data retrieval call binding the contract method 0x342de179.
//
// Solidity: function exits(uint256 ) view returns(uint256 receiptAmountOrNFTId, bytes32 txHash, address owner, address token, bool isRegularExit, address predicate)
func (_WithdrawManager *WithdrawManagerCaller) Exits(opts *bind.CallOpts, arg0 *big.Int) (struct {
	ReceiptAmountOrNFTId *big.Int
	TxHash               [32]byte
	Owner                common.Address
	Token                common.Address
	IsRegularExit        bool
	Predicate            common.Address
}, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "exits", arg0)

	outstruct := new(struct {
		ReceiptAmountOrNFTId *big.Int
		TxHash               [32]byte
		Owner                common.Address
		Token                common.Address
		IsRegularExit        bool
		Predicate            common.Address
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ReceiptAmountOrNFTId = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.TxHash = *abi.ConvertType(out[1], new([32]byte)).(*[32]byte)
	outstruct.Owner = *abi.ConvertType(out[2], new(common.Address)).(*common.Address)
	outstruct.Token = *abi.ConvertType(out[3], new(common.Address)).(*common.Address)
	outstruct.IsRegularExit = *abi.ConvertType(out[4], new(bool)).(*bool)
	outstruct.Predicate = *abi.ConvertType(out[5], new(common.Address)).(*common.Address)

	return *outstruct, err

}

// Exits is a free data retrieval call binding the contract method 0x342de179.
//
// Solidity: function exits(uint256 ) view returns(uint256 receiptAmountOrNFTId, bytes32 txHash, address owner, address token, bool isRegularExit, address predicate)
func (_WithdrawManager *WithdrawManagerSession) Exits(arg0 *big.Int) (struct {
	ReceiptAmountOrNFTId *big.Int
	TxHash               [32]byte
	Owner                common.Address
	Token                common.Address
	IsRegularExit        bool
	Predicate            common.Address
}, error) {
	return _WithdrawManager.Contract.Exits(&_WithdrawManager.CallOpts, arg0)
}

// Exits is a free data retrieval call binding the contract method 0x342de179.
//
// Solidity: function exits(uint256 ) view returns(uint256 receiptAmountOrNFTId, bytes32 txHash, address owner, address token, bool isRegularExit, address predicate)
func (_WithdrawManager *WithdrawManagerCallerSession) Exits(arg0 *big.Int) (struct {
	ReceiptAmountOrNFTId *big.Int
	TxHash               [32]byte
	Owner                common.Address
	Token                common.Address
	IsRegularExit        bool
	Predicate            common.Address
}, error) {
	return _WithdrawManager.Contract.Exits(&_WithdrawManager.CallOpts, arg0)
}

// ExitsQueues is a free data retrieval call binding the contract method 0xd11f045c.
//
// Solidity: function exitsQueues(address ) view returns(address)
func (_WithdrawManager *WithdrawManagerCaller) ExitsQueues(opts *bind.CallOpts, arg0 common.Address) (common.Address, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "exitsQueues", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ExitsQueues is a free data retrieval call binding the contract method 0xd11f045c.
//
// Solidity: function exitsQueues(address ) view returns(address)
func (_WithdrawManager *WithdrawManagerSession) ExitsQueues(arg0 common.Address) (common.Address, error) {
	return _WithdrawManager.Contract.ExitsQueues(&_WithdrawManager.CallOpts, arg0)
}

// ExitsQueues is a free data retrieval call binding the contract method 0xd11f045c.
//
// Solidity: function exitsQueues(address ) view returns(address)
func (_WithdrawManager *WithdrawManagerCallerSession) ExitsQueues(arg0 common.Address) (common.Address, error) {
	return _WithdrawManager.Contract.ExitsQueues(&_WithdrawManager.CallOpts, arg0)
}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_WithdrawManager *WithdrawManagerCaller) IsOwner(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "isOwner")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_WithdrawManager *WithdrawManagerSession) IsOwner() (bool, error) {
	return _WithdrawManager.Contract.IsOwner(&_WithdrawManager.CallOpts)
}

// IsOwner is a free data retrieval call binding the contract method 0x8f32d59b.
//
// Solidity: function isOwner() view returns(bool)
func (_WithdrawManager *WithdrawManagerCallerSession) IsOwner() (bool, error) {
	return _WithdrawManager.Contract.IsOwner(&_WithdrawManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_WithdrawManager *WithdrawManagerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_WithdrawManager *WithdrawManagerSession) Owner() (common.Address, error) {
	return _WithdrawManager.Contract.Owner(&_WithdrawManager.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_WithdrawManager *WithdrawManagerCallerSession) Owner() (common.Address, error) {
	return _WithdrawManager.Contract.Owner(&_WithdrawManager.CallOpts)
}

// OwnerExits is a free data retrieval call binding the contract method 0x661429c8.
//
// Solidity: function ownerExits(bytes32 ) view returns(uint256)
func (_WithdrawManager *WithdrawManagerCaller) OwnerExits(opts *bind.CallOpts, arg0 [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "ownerExits", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// OwnerExits is a free data retrieval call binding the contract method 0x661429c8.
//
// Solidity: function ownerExits(bytes32 ) view returns(uint256)
func (_WithdrawManager *WithdrawManagerSession) OwnerExits(arg0 [32]byte) (*big.Int, error) {
	return _WithdrawManager.Contract.OwnerExits(&_WithdrawManager.CallOpts, arg0)
}

// OwnerExits is a free data retrieval call binding the contract method 0x661429c8.
//
// Solidity: function ownerExits(bytes32 ) view returns(uint256)
func (_WithdrawManager *WithdrawManagerCallerSession) OwnerExits(arg0 [32]byte) (*big.Int, error) {
	return _WithdrawManager.Contract.OwnerExits(&_WithdrawManager.CallOpts, arg0)
}

// VerifyInclusion is a free data retrieval call binding the contract method 0xad1d8069.
//
// Solidity: function verifyInclusion(bytes data, uint8 offset, bool verifyTxInclusion) view returns(uint256)
func (_WithdrawManager *WithdrawManagerCaller) VerifyInclusion(opts *bind.CallOpts, data []byte, offset uint8, verifyTxInclusion bool) (*big.Int, error) {
	var out []interface{}
	err := _WithdrawManager.contract.Call(opts, &out, "verifyInclusion", data, offset, verifyTxInclusion)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// VerifyInclusion is a free data retrieval call binding the contract method 0xad1d8069.
//
// Solidity: function verifyInclusion(bytes data, uint8 offset, bool verifyTxInclusion) view returns(uint256)
func (_WithdrawManager *WithdrawManagerSession) VerifyInclusion(data []byte, offset uint8, verifyTxInclusion bool) (*big.Int, error) {
	return _WithdrawManager.Contract.VerifyInclusion(&_WithdrawManager.CallOpts, data, offset, verifyTxInclusion)
}

// VerifyInclusion is a free data retrieval call binding the contract method 0xad1d8069.
//
// Solidity: function verifyInclusion(bytes data, uint8 offset, bool verifyTxInclusion) view returns(uint256)
func (_WithdrawManager *WithdrawManagerCallerSession) VerifyInclusion(data []byte, offset uint8, verifyTxInclusion bool) (*big.Int, error) {
	return _WithdrawManager.Contract.VerifyInclusion(&_WithdrawManager.CallOpts, data, offset, verifyTxInclusion)
}

// AddExitToQueue is a paid mutator transaction binding the contract method 0xd931a869.
//
// Solidity: function addExitToQueue(address exitor, address childToken, address rootToken, uint256 exitAmountOrTokenId, bytes32 txHash, bool isRegularExit, uint256 priority) returns()
func (_WithdrawManager *WithdrawManagerTransactor) AddExitToQueue(opts *bind.TransactOpts, exitor common.Address, childToken common.Address, rootToken common.Address, exitAmountOrTokenId *big.Int, txHash [32]byte, isRegularExit bool, priority *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "addExitToQueue", exitor, childToken, rootToken, exitAmountOrTokenId, txHash, isRegularExit, priority)
}

// AddExitToQueue is a paid mutator transaction binding the contract method 0xd931a869.
//
// Solidity: function addExitToQueue(address exitor, address childToken, address rootToken, uint256 exitAmountOrTokenId, bytes32 txHash, bool isRegularExit, uint256 priority) returns()
func (_WithdrawManager *WithdrawManagerSession) AddExitToQueue(exitor common.Address, childToken common.Address, rootToken common.Address, exitAmountOrTokenId *big.Int, txHash [32]byte, isRegularExit bool, priority *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.AddExitToQueue(&_WithdrawManager.TransactOpts, exitor, childToken, rootToken, exitAmountOrTokenId, txHash, isRegularExit, priority)
}

// AddExitToQueue is a paid mutator transaction binding the contract method 0xd931a869.
//
// Solidity: function addExitToQueue(address exitor, address childToken, address rootToken, uint256 exitAmountOrTokenId, bytes32 txHash, bool isRegularExit, uint256 priority) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) AddExitToQueue(exitor common.Address, childToken common.Address, rootToken common.Address, exitAmountOrTokenId *big.Int, txHash [32]byte, isRegularExit bool, priority *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.AddExitToQueue(&_WithdrawManager.TransactOpts, exitor, childToken, rootToken, exitAmountOrTokenId, txHash, isRegularExit, priority)
}

// AddInput is a paid mutator transaction binding the contract method 0x22f192af.
//
// Solidity: function addInput(uint256 exitId, uint256 age, address utxoOwner, address token) returns()
func (_WithdrawManager *WithdrawManagerTransactor) AddInput(opts *bind.TransactOpts, exitId *big.Int, age *big.Int, utxoOwner common.Address, token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "addInput", exitId, age, utxoOwner, token)
}

// AddInput is a paid mutator transaction binding the contract method 0x22f192af.
//
// Solidity: function addInput(uint256 exitId, uint256 age, address utxoOwner, address token) returns()
func (_WithdrawManager *WithdrawManagerSession) AddInput(exitId *big.Int, age *big.Int, utxoOwner common.Address, token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.AddInput(&_WithdrawManager.TransactOpts, exitId, age, utxoOwner, token)
}

// AddInput is a paid mutator transaction binding the contract method 0x22f192af.
//
// Solidity: function addInput(uint256 exitId, uint256 age, address utxoOwner, address token) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) AddInput(exitId *big.Int, age *big.Int, utxoOwner common.Address, token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.AddInput(&_WithdrawManager.TransactOpts, exitId, age, utxoOwner, token)
}

// ChallengeExit is a paid mutator transaction binding the contract method 0x9492b0b8.
//
// Solidity: function challengeExit(uint256 exitId, uint256 inputId, bytes challengeData, address adjudicatorPredicate) returns()
func (_WithdrawManager *WithdrawManagerTransactor) ChallengeExit(opts *bind.TransactOpts, exitId *big.Int, inputId *big.Int, challengeData []byte, adjudicatorPredicate common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "challengeExit", exitId, inputId, challengeData, adjudicatorPredicate)
}

// ChallengeExit is a paid mutator transaction binding the contract method 0x9492b0b8.
//
// Solidity: function challengeExit(uint256 exitId, uint256 inputId, bytes challengeData, address adjudicatorPredicate) returns()
func (_WithdrawManager *WithdrawManagerSession) ChallengeExit(exitId *big.Int, inputId *big.Int, challengeData []byte, adjudicatorPredicate common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ChallengeExit(&_WithdrawManager.TransactOpts, exitId, inputId, challengeData, adjudicatorPredicate)
}

// ChallengeExit is a paid mutator transaction binding the contract method 0x9492b0b8.
//
// Solidity: function challengeExit(uint256 exitId, uint256 inputId, bytes challengeData, address adjudicatorPredicate) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) ChallengeExit(exitId *big.Int, inputId *big.Int, challengeData []byte, adjudicatorPredicate common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ChallengeExit(&_WithdrawManager.TransactOpts, exitId, inputId, challengeData, adjudicatorPredicate)
}

// CreateExitQueue is a paid mutator transaction binding the contract method 0x9145e6df.
//
// Solidity: function createExitQueue(address token) returns()
func (_WithdrawManager *WithdrawManagerTransactor) CreateExitQueue(opts *bind.TransactOpts, token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "createExitQueue", token)
}

// CreateExitQueue is a paid mutator transaction binding the contract method 0x9145e6df.
//
// Solidity: function createExitQueue(address token) returns()
func (_WithdrawManager *WithdrawManagerSession) CreateExitQueue(token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.CreateExitQueue(&_WithdrawManager.TransactOpts, token)
}

// CreateExitQueue is a paid mutator transaction binding the contract method 0x9145e6df.
//
// Solidity: function createExitQueue(address token) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) CreateExitQueue(token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.CreateExitQueue(&_WithdrawManager.TransactOpts, token)
}

// ProcessExits is a paid mutator transaction binding the contract method 0x0f6795f2.
//
// Solidity: function processExits(address _token) returns()
func (_WithdrawManager *WithdrawManagerTransactor) ProcessExits(opts *bind.TransactOpts, _token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "processExits", _token)
}

// ProcessExits is a paid mutator transaction binding the contract method 0x0f6795f2.
//
// Solidity: function processExits(address _token) returns()
func (_WithdrawManager *WithdrawManagerSession) ProcessExits(_token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ProcessExits(&_WithdrawManager.TransactOpts, _token)
}

// ProcessExits is a paid mutator transaction binding the contract method 0x0f6795f2.
//
// Solidity: function processExits(address _token) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) ProcessExits(_token common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ProcessExits(&_WithdrawManager.TransactOpts, _token)
}

// ProcessExitsBatch is a paid mutator transaction binding the contract method 0xc74ab88a.
//
// Solidity: function processExitsBatch(address[] _tokens) returns()
func (_WithdrawManager *WithdrawManagerTransactor) ProcessExitsBatch(opts *bind.TransactOpts, _tokens []common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "processExitsBatch", _tokens)
}

// ProcessExitsBatch is a paid mutator transaction binding the contract method 0xc74ab88a.
//
// Solidity: function processExitsBatch(address[] _tokens) returns()
func (_WithdrawManager *WithdrawManagerSession) ProcessExitsBatch(_tokens []common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ProcessExitsBatch(&_WithdrawManager.TransactOpts, _tokens)
}

// ProcessExitsBatch is a paid mutator transaction binding the contract method 0xc74ab88a.
//
// Solidity: function processExitsBatch(address[] _tokens) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) ProcessExitsBatch(_tokens []common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.ProcessExitsBatch(&_WithdrawManager.TransactOpts, _tokens)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_WithdrawManager *WithdrawManagerTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_WithdrawManager *WithdrawManagerSession) RenounceOwnership() (*types.Transaction, error) {
	return _WithdrawManager.Contract.RenounceOwnership(&_WithdrawManager.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _WithdrawManager.Contract.RenounceOwnership(&_WithdrawManager.TransactOpts)
}

// StartExitWithDepositedTokens is a paid mutator transaction binding the contract method 0x144a03b3.
//
// Solidity: function startExitWithDepositedTokens(uint256 depositId, address token, uint256 amountOrToken) payable returns()
func (_WithdrawManager *WithdrawManagerTransactor) StartExitWithDepositedTokens(opts *bind.TransactOpts, depositId *big.Int, token common.Address, amountOrToken *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "startExitWithDepositedTokens", depositId, token, amountOrToken)
}

// StartExitWithDepositedTokens is a paid mutator transaction binding the contract method 0x144a03b3.
//
// Solidity: function startExitWithDepositedTokens(uint256 depositId, address token, uint256 amountOrToken) payable returns()
func (_WithdrawManager *WithdrawManagerSession) StartExitWithDepositedTokens(depositId *big.Int, token common.Address, amountOrToken *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.StartExitWithDepositedTokens(&_WithdrawManager.TransactOpts, depositId, token, amountOrToken)
}

// StartExitWithDepositedTokens is a paid mutator transaction binding the contract method 0x144a03b3.
//
// Solidity: function startExitWithDepositedTokens(uint256 depositId, address token, uint256 amountOrToken) payable returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) StartExitWithDepositedTokens(depositId *big.Int, token common.Address, amountOrToken *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.StartExitWithDepositedTokens(&_WithdrawManager.TransactOpts, depositId, token, amountOrToken)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_WithdrawManager *WithdrawManagerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_WithdrawManager *WithdrawManagerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.TransferOwnership(&_WithdrawManager.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _WithdrawManager.Contract.TransferOwnership(&_WithdrawManager.TransactOpts, newOwner)
}

// UpdateExitPeriod is a paid mutator transaction binding the contract method 0x433c76bf.
//
// Solidity: function updateExitPeriod(uint256 halfExitPeriod) returns()
func (_WithdrawManager *WithdrawManagerTransactor) UpdateExitPeriod(opts *bind.TransactOpts, halfExitPeriod *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.contract.Transact(opts, "updateExitPeriod", halfExitPeriod)
}

// UpdateExitPeriod is a paid mutator transaction binding the contract method 0x433c76bf.
//
// Solidity: function updateExitPeriod(uint256 halfExitPeriod) returns()
func (_WithdrawManager *WithdrawManagerSession) UpdateExitPeriod(halfExitPeriod *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.UpdateExitPeriod(&_WithdrawManager.TransactOpts, halfExitPeriod)
}

// UpdateExitPeriod is a paid mutator transaction binding the contract method 0x433c76bf.
//
// Solidity: function updateExitPeriod(uint256 halfExitPeriod) returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) UpdateExitPeriod(halfExitPeriod *big.Int) (*types.Transaction, error) {
	return _WithdrawManager.Contract.UpdateExitPeriod(&_WithdrawManager.TransactOpts, halfExitPeriod)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_WithdrawManager *WithdrawManagerTransactor) Fallback(opts *bind.TransactOpts, calldata []byte) (*types.Transaction, error) {
	return _WithdrawManager.contract.RawTransact(opts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_WithdrawManager *WithdrawManagerSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _WithdrawManager.Contract.Fallback(&_WithdrawManager.TransactOpts, calldata)
}

// Fallback is a paid mutator transaction binding the contract fallback function.
//
// Solidity: fallback() payable returns()
func (_WithdrawManager *WithdrawManagerTransactorSession) Fallback(calldata []byte) (*types.Transaction, error) {
	return _WithdrawManager.Contract.Fallback(&_WithdrawManager.TransactOpts, calldata)
}

// WithdrawManagerExitCancelledIterator is returned from FilterExitCancelled and is used to iterate over the raw logs and unpacked data for ExitCancelled events raised by the WithdrawManager contract.
type WithdrawManagerExitCancelledIterator struct {
	Event *WithdrawManagerExitCancelled // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerExitCancelledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerExitCancelled)
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
		it.Event = new(WithdrawManagerExitCancelled)
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
func (it *WithdrawManagerExitCancelledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerExitCancelledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerExitCancelled represents a ExitCancelled event raised by the WithdrawManager contract.
type WithdrawManagerExitCancelled struct {
	ExitId *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterExitCancelled is a free log retrieval operation binding the contract event 0x93a8052a01c184f88312af177ab8fae2e56a9973b6aa4bdc62dfcf744e09d041.
//
// Solidity: event ExitCancelled(uint256 indexed exitId)
func (_WithdrawManager *WithdrawManagerFilterer) FilterExitCancelled(opts *bind.FilterOpts, exitId []*big.Int) (*WithdrawManagerExitCancelledIterator, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "ExitCancelled", exitIdRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerExitCancelledIterator{contract: _WithdrawManager.contract, event: "ExitCancelled", logs: logs, sub: sub}, nil
}

// WatchExitCancelled is a free log subscription operation binding the contract event 0x93a8052a01c184f88312af177ab8fae2e56a9973b6aa4bdc62dfcf744e09d041.
//
// Solidity: event ExitCancelled(uint256 indexed exitId)
func (_WithdrawManager *WithdrawManagerFilterer) WatchExitCancelled(opts *bind.WatchOpts, sink chan<- *WithdrawManagerExitCancelled, exitId []*big.Int) (event.Subscription, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "ExitCancelled", exitIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerExitCancelled)
				if err := _WithdrawManager.contract.UnpackLog(event, "ExitCancelled", log); err != nil {
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

// ParseExitCancelled is a log parse operation binding the contract event 0x93a8052a01c184f88312af177ab8fae2e56a9973b6aa4bdc62dfcf744e09d041.
//
// Solidity: event ExitCancelled(uint256 indexed exitId)
func (_WithdrawManager *WithdrawManagerFilterer) ParseExitCancelled(log types.Log) (*WithdrawManagerExitCancelled, error) {
	event := new(WithdrawManagerExitCancelled)
	if err := _WithdrawManager.contract.UnpackLog(event, "ExitCancelled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// WithdrawManagerExitPeriodUpdateIterator is returned from FilterExitPeriodUpdate and is used to iterate over the raw logs and unpacked data for ExitPeriodUpdate events raised by the WithdrawManager contract.
type WithdrawManagerExitPeriodUpdateIterator struct {
	Event *WithdrawManagerExitPeriodUpdate // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerExitPeriodUpdateIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerExitPeriodUpdate)
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
		it.Event = new(WithdrawManagerExitPeriodUpdate)
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
func (it *WithdrawManagerExitPeriodUpdateIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerExitPeriodUpdateIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerExitPeriodUpdate represents a ExitPeriodUpdate event raised by the WithdrawManager contract.
type WithdrawManagerExitPeriodUpdate struct {
	OldExitPeriod *big.Int
	NewExitPeriod *big.Int
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterExitPeriodUpdate is a free log retrieval operation binding the contract event 0x06b98f3947a8966918fef150b41170e78ba1d91dd2b1d2fd48a59c91ffbd66a1.
//
// Solidity: event ExitPeriodUpdate(uint256 indexed oldExitPeriod, uint256 indexed newExitPeriod)
func (_WithdrawManager *WithdrawManagerFilterer) FilterExitPeriodUpdate(opts *bind.FilterOpts, oldExitPeriod []*big.Int, newExitPeriod []*big.Int) (*WithdrawManagerExitPeriodUpdateIterator, error) {

	var oldExitPeriodRule []interface{}
	for _, oldExitPeriodItem := range oldExitPeriod {
		oldExitPeriodRule = append(oldExitPeriodRule, oldExitPeriodItem)
	}
	var newExitPeriodRule []interface{}
	for _, newExitPeriodItem := range newExitPeriod {
		newExitPeriodRule = append(newExitPeriodRule, newExitPeriodItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "ExitPeriodUpdate", oldExitPeriodRule, newExitPeriodRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerExitPeriodUpdateIterator{contract: _WithdrawManager.contract, event: "ExitPeriodUpdate", logs: logs, sub: sub}, nil
}

// WatchExitPeriodUpdate is a free log subscription operation binding the contract event 0x06b98f3947a8966918fef150b41170e78ba1d91dd2b1d2fd48a59c91ffbd66a1.
//
// Solidity: event ExitPeriodUpdate(uint256 indexed oldExitPeriod, uint256 indexed newExitPeriod)
func (_WithdrawManager *WithdrawManagerFilterer) WatchExitPeriodUpdate(opts *bind.WatchOpts, sink chan<- *WithdrawManagerExitPeriodUpdate, oldExitPeriod []*big.Int, newExitPeriod []*big.Int) (event.Subscription, error) {

	var oldExitPeriodRule []interface{}
	for _, oldExitPeriodItem := range oldExitPeriod {
		oldExitPeriodRule = append(oldExitPeriodRule, oldExitPeriodItem)
	}
	var newExitPeriodRule []interface{}
	for _, newExitPeriodItem := range newExitPeriod {
		newExitPeriodRule = append(newExitPeriodRule, newExitPeriodItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "ExitPeriodUpdate", oldExitPeriodRule, newExitPeriodRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerExitPeriodUpdate)
				if err := _WithdrawManager.contract.UnpackLog(event, "ExitPeriodUpdate", log); err != nil {
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

// ParseExitPeriodUpdate is a log parse operation binding the contract event 0x06b98f3947a8966918fef150b41170e78ba1d91dd2b1d2fd48a59c91ffbd66a1.
//
// Solidity: event ExitPeriodUpdate(uint256 indexed oldExitPeriod, uint256 indexed newExitPeriod)
func (_WithdrawManager *WithdrawManagerFilterer) ParseExitPeriodUpdate(log types.Log) (*WithdrawManagerExitPeriodUpdate, error) {
	event := new(WithdrawManagerExitPeriodUpdate)
	if err := _WithdrawManager.contract.UnpackLog(event, "ExitPeriodUpdate", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// WithdrawManagerExitStartedIterator is returned from FilterExitStarted and is used to iterate over the raw logs and unpacked data for ExitStarted events raised by the WithdrawManager contract.
type WithdrawManagerExitStartedIterator struct {
	Event *WithdrawManagerExitStarted // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerExitStartedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerExitStarted)
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
		it.Event = new(WithdrawManagerExitStarted)
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
func (it *WithdrawManagerExitStartedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerExitStartedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerExitStarted represents a ExitStarted event raised by the WithdrawManager contract.
type WithdrawManagerExitStarted struct {
	Exitor        common.Address
	ExitId        *big.Int
	Token         common.Address
	Amount        *big.Int
	IsRegularExit bool
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterExitStarted is a free log retrieval operation binding the contract event 0xaa5303fdad123ab5ecaefaf69137bf8632257839546d43a3b3dd148cc2879d6f.
//
// Solidity: event ExitStarted(address indexed exitor, uint256 indexed exitId, address indexed token, uint256 amount, bool isRegularExit)
func (_WithdrawManager *WithdrawManagerFilterer) FilterExitStarted(opts *bind.FilterOpts, exitor []common.Address, exitId []*big.Int, token []common.Address) (*WithdrawManagerExitStartedIterator, error) {

	var exitorRule []interface{}
	for _, exitorItem := range exitor {
		exitorRule = append(exitorRule, exitorItem)
	}
	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "ExitStarted", exitorRule, exitIdRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerExitStartedIterator{contract: _WithdrawManager.contract, event: "ExitStarted", logs: logs, sub: sub}, nil
}

// WatchExitStarted is a free log subscription operation binding the contract event 0xaa5303fdad123ab5ecaefaf69137bf8632257839546d43a3b3dd148cc2879d6f.
//
// Solidity: event ExitStarted(address indexed exitor, uint256 indexed exitId, address indexed token, uint256 amount, bool isRegularExit)
func (_WithdrawManager *WithdrawManagerFilterer) WatchExitStarted(opts *bind.WatchOpts, sink chan<- *WithdrawManagerExitStarted, exitor []common.Address, exitId []*big.Int, token []common.Address) (event.Subscription, error) {

	var exitorRule []interface{}
	for _, exitorItem := range exitor {
		exitorRule = append(exitorRule, exitorItem)
	}
	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "ExitStarted", exitorRule, exitIdRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerExitStarted)
				if err := _WithdrawManager.contract.UnpackLog(event, "ExitStarted", log); err != nil {
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

// ParseExitStarted is a log parse operation binding the contract event 0xaa5303fdad123ab5ecaefaf69137bf8632257839546d43a3b3dd148cc2879d6f.
//
// Solidity: event ExitStarted(address indexed exitor, uint256 indexed exitId, address indexed token, uint256 amount, bool isRegularExit)
func (_WithdrawManager *WithdrawManagerFilterer) ParseExitStarted(log types.Log) (*WithdrawManagerExitStarted, error) {
	event := new(WithdrawManagerExitStarted)
	if err := _WithdrawManager.contract.UnpackLog(event, "ExitStarted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// WithdrawManagerExitUpdatedIterator is returned from FilterExitUpdated and is used to iterate over the raw logs and unpacked data for ExitUpdated events raised by the WithdrawManager contract.
type WithdrawManagerExitUpdatedIterator struct {
	Event *WithdrawManagerExitUpdated // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerExitUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerExitUpdated)
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
		it.Event = new(WithdrawManagerExitUpdated)
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
func (it *WithdrawManagerExitUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerExitUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerExitUpdated represents a ExitUpdated event raised by the WithdrawManager contract.
type WithdrawManagerExitUpdated struct {
	ExitId *big.Int
	Age    *big.Int
	Signer common.Address
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterExitUpdated is a free log retrieval operation binding the contract event 0x87d2daa6e85f166015ebbcf09f5ee4bc50f93677579339fe128e3561a6807cb6.
//
// Solidity: event ExitUpdated(uint256 indexed exitId, uint256 indexed age, address signer)
func (_WithdrawManager *WithdrawManagerFilterer) FilterExitUpdated(opts *bind.FilterOpts, exitId []*big.Int, age []*big.Int) (*WithdrawManagerExitUpdatedIterator, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var ageRule []interface{}
	for _, ageItem := range age {
		ageRule = append(ageRule, ageItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "ExitUpdated", exitIdRule, ageRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerExitUpdatedIterator{contract: _WithdrawManager.contract, event: "ExitUpdated", logs: logs, sub: sub}, nil
}

// WatchExitUpdated is a free log subscription operation binding the contract event 0x87d2daa6e85f166015ebbcf09f5ee4bc50f93677579339fe128e3561a6807cb6.
//
// Solidity: event ExitUpdated(uint256 indexed exitId, uint256 indexed age, address signer)
func (_WithdrawManager *WithdrawManagerFilterer) WatchExitUpdated(opts *bind.WatchOpts, sink chan<- *WithdrawManagerExitUpdated, exitId []*big.Int, age []*big.Int) (event.Subscription, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var ageRule []interface{}
	for _, ageItem := range age {
		ageRule = append(ageRule, ageItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "ExitUpdated", exitIdRule, ageRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerExitUpdated)
				if err := _WithdrawManager.contract.UnpackLog(event, "ExitUpdated", log); err != nil {
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

// ParseExitUpdated is a log parse operation binding the contract event 0x87d2daa6e85f166015ebbcf09f5ee4bc50f93677579339fe128e3561a6807cb6.
//
// Solidity: event ExitUpdated(uint256 indexed exitId, uint256 indexed age, address signer)
func (_WithdrawManager *WithdrawManagerFilterer) ParseExitUpdated(log types.Log) (*WithdrawManagerExitUpdated, error) {
	event := new(WithdrawManagerExitUpdated)
	if err := _WithdrawManager.contract.UnpackLog(event, "ExitUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// WithdrawManagerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the WithdrawManager contract.
type WithdrawManagerOwnershipTransferredIterator struct {
	Event *WithdrawManagerOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerOwnershipTransferred)
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
		it.Event = new(WithdrawManagerOwnershipTransferred)
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
func (it *WithdrawManagerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerOwnershipTransferred represents a OwnershipTransferred event raised by the WithdrawManager contract.
type WithdrawManagerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_WithdrawManager *WithdrawManagerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*WithdrawManagerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerOwnershipTransferredIterator{contract: _WithdrawManager.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_WithdrawManager *WithdrawManagerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *WithdrawManagerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerOwnershipTransferred)
				if err := _WithdrawManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_WithdrawManager *WithdrawManagerFilterer) ParseOwnershipTransferred(log types.Log) (*WithdrawManagerOwnershipTransferred, error) {
	event := new(WithdrawManagerOwnershipTransferred)
	if err := _WithdrawManager.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// WithdrawManagerWithdrawIterator is returned from FilterWithdraw and is used to iterate over the raw logs and unpacked data for Withdraw events raised by the WithdrawManager contract.
type WithdrawManagerWithdrawIterator struct {
	Event *WithdrawManagerWithdraw // Event containing the contract specifics and raw log

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
func (it *WithdrawManagerWithdrawIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(WithdrawManagerWithdraw)
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
		it.Event = new(WithdrawManagerWithdraw)
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
func (it *WithdrawManagerWithdrawIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *WithdrawManagerWithdrawIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// WithdrawManagerWithdraw represents a Withdraw event raised by the WithdrawManager contract.
type WithdrawManagerWithdraw struct {
	ExitId *big.Int
	User   common.Address
	Token  common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterWithdraw is a free log retrieval operation binding the contract event 0xfeb2000dca3e617cd6f3a8bbb63014bb54a124aac6ccbf73ee7229b4cd01f120.
//
// Solidity: event Withdraw(uint256 indexed exitId, address indexed user, address indexed token, uint256 amount)
func (_WithdrawManager *WithdrawManagerFilterer) FilterWithdraw(opts *bind.FilterOpts, exitId []*big.Int, user []common.Address, token []common.Address) (*WithdrawManagerWithdrawIterator, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _WithdrawManager.contract.FilterLogs(opts, "Withdraw", exitIdRule, userRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return &WithdrawManagerWithdrawIterator{contract: _WithdrawManager.contract, event: "Withdraw", logs: logs, sub: sub}, nil
}

// WatchWithdraw is a free log subscription operation binding the contract event 0xfeb2000dca3e617cd6f3a8bbb63014bb54a124aac6ccbf73ee7229b4cd01f120.
//
// Solidity: event Withdraw(uint256 indexed exitId, address indexed user, address indexed token, uint256 amount)
func (_WithdrawManager *WithdrawManagerFilterer) WatchWithdraw(opts *bind.WatchOpts, sink chan<- *WithdrawManagerWithdraw, exitId []*big.Int, user []common.Address, token []common.Address) (event.Subscription, error) {

	var exitIdRule []interface{}
	for _, exitIdItem := range exitId {
		exitIdRule = append(exitIdRule, exitIdItem)
	}
	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}

	logs, sub, err := _WithdrawManager.contract.WatchLogs(opts, "Withdraw", exitIdRule, userRule, tokenRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(WithdrawManagerWithdraw)
				if err := _WithdrawManager.contract.UnpackLog(event, "Withdraw", log); err != nil {
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

// ParseWithdraw is a log parse operation binding the contract event 0xfeb2000dca3e617cd6f3a8bbb63014bb54a124aac6ccbf73ee7229b4cd01f120.
//
// Solidity: event Withdraw(uint256 indexed exitId, address indexed user, address indexed token, uint256 amount)
func (_WithdrawManager *WithdrawManagerFilterer) ParseWithdraw(log types.Log) (*WithdrawManagerWithdraw, error) {
	event := new(WithdrawManagerWithdraw)
	if err := _WithdrawManager.contract.UnpackLog(event, "Withdraw", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
