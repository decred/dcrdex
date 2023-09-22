// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package multibalance

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

// MultiBalanceV0MetaData contains all meta data concerning the MultiBalanceV0 contract.
var MultiBalanceV0MetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"acct\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"tokenAddresses\",\"type\":\"address[]\"}],\"name\":\"balances\",\"outputs\":[{\"internalType\":\"uint256[]\",\"name\":\"\",\"type\":\"uint256[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50610483806100206000396000f3fe608060405234801561001057600080fd5b506004361061002b5760003560e01c8063d3e5ca8714610030575b600080fd5b61004361003e3660046102a5565b610059565b604051610050919061032b565b60405180910390f35b60606000610068836001610385565b67ffffffffffffffff8111156100805761008061039e565b6040519080825280602002602001820160405280156100a9578160200160208202803683370190505b509050846001600160a01b031631816000815181106100ca576100ca6103b4565b60200260200101818152505060005b838110156102805760008585838181106100f5576100f56103b4565b905060200201602081019061010a91906103ca565b905060006060826001600160a01b03167f70a08231b98ef4ca268c9cc3f6b4590e4bfec28280db06bb5d45e689f2a360be8a60405160240161015b91906001600160a01b0391909116815260200190565b60408051601f198184030181529181526020820180516001600160e01b03166001600160e01b031990941693909317909252905161019991906103ec565b600060405180830381855afa9150503d80600081146101d4576040519150601f19603f3d011682016040523d82523d6000602084013e6101d9565b606091505b509092509050816102285760405162461bcd60e51b815260206004820152601560248201527418985b185b98d953d98818d85b1b0819985a5b1959605a1b604482015260640160405180910390fd5b60008180602001905181019061023e919061041b565b9050808661024d876001610385565b8151811061025d5761025d6103b4565b60200260200101818152505050505050808061027890610434565b9150506100d9565b50949350505050565b80356001600160a01b03811681146102a057600080fd5b919050565b6000806000604084860312156102ba57600080fd5b6102c384610289565b9250602084013567ffffffffffffffff808211156102e057600080fd5b818601915086601f8301126102f457600080fd5b81358181111561030357600080fd5b8760208260051b850101111561031857600080fd5b6020830194508093505050509250925092565b6020808252825182820181905260009190848201906040850190845b8181101561036357835183529284019291840191600101610347565b50909695505050505050565b634e487b7160e01b600052601160045260246000fd5b808201808211156103985761039861036f565b92915050565b634e487b7160e01b600052604160045260246000fd5b634e487b7160e01b600052603260045260246000fd5b6000602082840312156103dc57600080fd5b6103e582610289565b9392505050565b6000825160005b8181101561040d57602081860181015185830152016103f3565b506000920191825250919050565b60006020828403121561042d57600080fd5b5051919050565b6000600182016104465761044661036f565b506001019056fea26469706673582212207074e3e189a2692e2841f7943a9de24bcdbca943ee6bfcbd83a2fa6e43ec497b64736f6c63430008120033",
}

// MultiBalanceV0ABI is the input ABI used to generate the binding from.
// Deprecated: Use MultiBalanceV0MetaData.ABI instead.
var MultiBalanceV0ABI = MultiBalanceV0MetaData.ABI

// MultiBalanceV0Bin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use MultiBalanceV0MetaData.Bin instead.
var MultiBalanceV0Bin = MultiBalanceV0MetaData.Bin

// DeployMultiBalanceV0 deploys a new Ethereum contract, binding an instance of MultiBalanceV0 to it.
func DeployMultiBalanceV0(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *MultiBalanceV0, error) {
	parsed, err := MultiBalanceV0MetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(MultiBalanceV0Bin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &MultiBalanceV0{MultiBalanceV0Caller: MultiBalanceV0Caller{contract: contract}, MultiBalanceV0Transactor: MultiBalanceV0Transactor{contract: contract}, MultiBalanceV0Filterer: MultiBalanceV0Filterer{contract: contract}}, nil
}

// MultiBalanceV0 is an auto generated Go binding around an Ethereum contract.
type MultiBalanceV0 struct {
	MultiBalanceV0Caller     // Read-only binding to the contract
	MultiBalanceV0Transactor // Write-only binding to the contract
	MultiBalanceV0Filterer   // Log filterer for contract events
}

// MultiBalanceV0Caller is an auto generated read-only Go binding around an Ethereum contract.
type MultiBalanceV0Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MultiBalanceV0Transactor is an auto generated write-only Go binding around an Ethereum contract.
type MultiBalanceV0Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MultiBalanceV0Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type MultiBalanceV0Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MultiBalanceV0Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type MultiBalanceV0Session struct {
	Contract     *MultiBalanceV0   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// MultiBalanceV0CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type MultiBalanceV0CallerSession struct {
	Contract *MultiBalanceV0Caller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// MultiBalanceV0TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type MultiBalanceV0TransactorSession struct {
	Contract     *MultiBalanceV0Transactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// MultiBalanceV0Raw is an auto generated low-level Go binding around an Ethereum contract.
type MultiBalanceV0Raw struct {
	Contract *MultiBalanceV0 // Generic contract binding to access the raw methods on
}

// MultiBalanceV0CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type MultiBalanceV0CallerRaw struct {
	Contract *MultiBalanceV0Caller // Generic read-only contract binding to access the raw methods on
}

// MultiBalanceV0TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type MultiBalanceV0TransactorRaw struct {
	Contract *MultiBalanceV0Transactor // Generic write-only contract binding to access the raw methods on
}

// NewMultiBalanceV0 creates a new instance of MultiBalanceV0, bound to a specific deployed contract.
func NewMultiBalanceV0(address common.Address, backend bind.ContractBackend) (*MultiBalanceV0, error) {
	contract, err := bindMultiBalanceV0(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &MultiBalanceV0{MultiBalanceV0Caller: MultiBalanceV0Caller{contract: contract}, MultiBalanceV0Transactor: MultiBalanceV0Transactor{contract: contract}, MultiBalanceV0Filterer: MultiBalanceV0Filterer{contract: contract}}, nil
}

// NewMultiBalanceV0Caller creates a new read-only instance of MultiBalanceV0, bound to a specific deployed contract.
func NewMultiBalanceV0Caller(address common.Address, caller bind.ContractCaller) (*MultiBalanceV0Caller, error) {
	contract, err := bindMultiBalanceV0(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &MultiBalanceV0Caller{contract: contract}, nil
}

// NewMultiBalanceV0Transactor creates a new write-only instance of MultiBalanceV0, bound to a specific deployed contract.
func NewMultiBalanceV0Transactor(address common.Address, transactor bind.ContractTransactor) (*MultiBalanceV0Transactor, error) {
	contract, err := bindMultiBalanceV0(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &MultiBalanceV0Transactor{contract: contract}, nil
}

// NewMultiBalanceV0Filterer creates a new log filterer instance of MultiBalanceV0, bound to a specific deployed contract.
func NewMultiBalanceV0Filterer(address common.Address, filterer bind.ContractFilterer) (*MultiBalanceV0Filterer, error) {
	contract, err := bindMultiBalanceV0(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &MultiBalanceV0Filterer{contract: contract}, nil
}

// bindMultiBalanceV0 binds a generic wrapper to an already deployed contract.
func bindMultiBalanceV0(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(MultiBalanceV0ABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MultiBalanceV0 *MultiBalanceV0Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MultiBalanceV0.Contract.MultiBalanceV0Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MultiBalanceV0 *MultiBalanceV0Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MultiBalanceV0.Contract.MultiBalanceV0Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MultiBalanceV0 *MultiBalanceV0Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MultiBalanceV0.Contract.MultiBalanceV0Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MultiBalanceV0 *MultiBalanceV0CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MultiBalanceV0.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MultiBalanceV0 *MultiBalanceV0TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MultiBalanceV0.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MultiBalanceV0 *MultiBalanceV0TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MultiBalanceV0.Contract.contract.Transact(opts, method, params...)
}

// Balances is a free data retrieval call binding the contract method 0xd3e5ca87.
//
// Solidity: function balances(address acct, address[] tokenAddresses) view returns(uint256[])
func (_MultiBalanceV0 *MultiBalanceV0Caller) Balances(opts *bind.CallOpts, acct common.Address, tokenAddresses []common.Address) ([]*big.Int, error) {
	var out []interface{}
	err := _MultiBalanceV0.contract.Call(opts, &out, "balances", acct, tokenAddresses)

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

// Balances is a free data retrieval call binding the contract method 0xd3e5ca87.
//
// Solidity: function balances(address acct, address[] tokenAddresses) view returns(uint256[])
func (_MultiBalanceV0 *MultiBalanceV0Session) Balances(acct common.Address, tokenAddresses []common.Address) ([]*big.Int, error) {
	return _MultiBalanceV0.Contract.Balances(&_MultiBalanceV0.CallOpts, acct, tokenAddresses)
}

// Balances is a free data retrieval call binding the contract method 0xd3e5ca87.
//
// Solidity: function balances(address acct, address[] tokenAddresses) view returns(uint256[])
func (_MultiBalanceV0 *MultiBalanceV0CallerSession) Balances(acct common.Address, tokenAddresses []common.Address) ([]*big.Int, error) {
	return _MultiBalanceV0.Contract.Balances(&_MultiBalanceV0.CallOpts, acct, tokenAddresses)
}
