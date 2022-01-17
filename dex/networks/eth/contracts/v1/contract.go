// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package v1

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

// ETHSwapContract is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapContract struct {
	SecretHash      [32]byte
	Initiator       common.Address
	RefundTimestamp uint64
	Participant     common.Address
	Value           uint64
}

// ETHSwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRedemption struct {
	C      ETHSwapContract
	Secret [32]byte
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"state\",\"outputs\":[{\"internalType\":\"enumETHSwap.State\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"ed7cbed7": "contractKey((bytes32,address,uint64,address,uint64))",
		"64a97bff": "initiate((bytes32,address,uint64,address,uint64)[])",
		"7802689d": "isRedeemable((bytes32,address,uint64,address,uint64))",
		"428b16e1": "redeem(((bytes32,address,uint64,address,uint64),bytes32)[])",
		"d2544c06": "refund((bytes32,address,uint64,address,uint64))",
		"77d7e031": "secretValidates(bytes32,bytes32)",
		"38ec1768": "state((bytes32,address,uint64,address,uint64))",
		"eb84e7f2": "swaps(bytes32)",
	},
	Bin: "0x608060405234801561001057600080fd5b50610e0c806100206000396000f3fe60806040526004361061007b5760003560e01c80637802689d1161004e5780637802689d1461011b578063d2544c061461013b578063eb84e7f21461015b578063ed7cbed71461019657600080fd5b806338ec176814610080578063428b16e1146100b657806364a97bff146100d857806377d7e031146100eb575b600080fd5b34801561008c57600080fd5b506100a061009b366004610c78565b6101b6565b6040516100ad9190610cf5565b60405180910390f35b3480156100c257600080fd5b506100d66100d1366004610bc1565b610214565b005b6100d66100e6366004610b4c565b6104a5565b3480156100f757600080fd5b5061010b610106366004610c56565b610692565b60405190151581526020016100ad565b34801561012757600080fd5b5061010b610136366004610c78565b61070c565b34801561014757600080fd5b506100d6610156366004610c78565b61074c565b34801561016757600080fd5b50610188610176366004610c24565b60006020819052908152604090205481565b6040519081526020016100ad565b3480156101a257600080fd5b506101886101b1366004610c78565b6109f5565b60008060006101c484610af0565b925092505080600014156101dc575060009392505050565b6000198214156101f0575060039392505050565b6101fb828535610692565b1561020a575060029392505050565b5060019392505050565b32331461023c5760405162461bcd60e51b815260040161023390610d1d565b60405180910390fd5b6000805b8281101561040e573684848381811061025b5761025b610dc0565b60c0029190910191503390506102776080830160608401610b1c565b6001600160a01b0316146102ba5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b6044820152606401610233565b600080806102c784610af0565b9250925092506000811180156102dc57504381105b6103185760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610233565b610323828535610692565b156103635760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610233565b61037260a08501358535610692565b6103af5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610233565b600083815260208190526040902060a0850180359091556103d39060808601610c90565b6103e190633b9aca00610d5f565b6103f59067ffffffffffffffff1687610d47565b955050505050808061040690610d8f565b915050610240565b50604051600090339083908381818185875af1925050503d8060008114610451576040519150601f19603f3d011682016040523d82523d6000602084013e610456565b606091505b509091505060018115151461049f5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610233565b50505050565b3233146104c45760405162461bcd60e51b815260040161023390610d1d565b6000805b8281101561065357368484838181106104e3576104e3610dc0565b905060a00201905060008160800160208101906105009190610c90565b67ffffffffffffffff161161053f5760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b6044820152606401610233565b60006105516060830160408401610c90565b67ffffffffffffffff161161059c5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b6044820152606401610233565b60006105a7826109f5565b60008181526020819052604090205490915080156105f85760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b6044820152606401610233565b600082815260208190526040902043905561061960a0840160808501610c90565b61062790633b9aca00610d5f565b61063b9067ffffffffffffffff1686610d47565b9450505050808061064b90610d8f565b9150506104c8565b5034811461068d5760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b6044820152606401610233565b505050565b6000816002846040516020016106aa91815260200190565b60408051601f19818403018152908290526106c491610cba565b602060405180830381855afa1580156106e1573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107049190610c3d565b149392505050565b600080600061071a84610af0565b9250925050806000141580156107305750438111155b80156107445750610742828535610692565b155b949350505050565b32331461076b5760405162461bcd60e51b815260040161023390610d1d565b3361077c6040830160208401610b1c565b6001600160a01b0316146107c95760405162461bcd60e51b815260206004820152601460248201527339b2b73232b9103737ba1034b734ba34b0ba37b960611b6044820152606401610233565b6107d96060820160408301610c90565b67ffffffffffffffff164210156108295760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b6044820152606401610233565b600080600061083784610af0565b92509250925060008111801561084c57504381105b61088a5760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b6044820152606401610233565b610895828535610692565b156108da5760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b6044820152606401610233565b6000198214156109245760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c99599d5b991959605a1b6044820152606401610233565b600083815260208190526040812060001990553361094860a0870160808801610c90565b61095690633b9aca00610d5f565b67ffffffffffffffff1660405160006040518083038185875af1925050503d80600081146109a0576040519150601f19603f3d011682016040523d82523d6000602084013e6109a5565b606091505b50909150506001811515146109ee5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610233565b5050505050565b600060028235610a0b6040850160208601610b1c565b610a1b6080860160608701610b1c565b610a2b60a0870160808801610c90565b610a3b6060880160408901610c90565b6040805160208101969096526bffffffffffffffffffffffff19606095861b8116918701919091529290931b90911660548401526001600160c01b031960c091821b8116606885015291901b16607082015260780160408051601f1981840301815290829052610aaa91610cba565b602060405180830381855afa158015610ac7573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610aea9190610c3d565b92915050565b600080600080610aff856109f5565b600081815260208190526040902054909690955085945092505050565b600060208284031215610b2e57600080fd5b81356001600160a01b0381168114610b4557600080fd5b9392505050565b60008060208385031215610b5f57600080fd5b823567ffffffffffffffff80821115610b7757600080fd5b818501915085601f830112610b8b57600080fd5b813581811115610b9a57600080fd5b86602060a083028501011115610baf57600080fd5b60209290920196919550909350505050565b60008060208385031215610bd457600080fd5b823567ffffffffffffffff80821115610bec57600080fd5b818501915085601f830112610c0057600080fd5b813581811115610c0f57600080fd5b86602060c083028501011115610baf57600080fd5b600060208284031215610c3657600080fd5b5035919050565b600060208284031215610c4f57600080fd5b5051919050565b60008060408385031215610c6957600080fd5b50508035926020909101359150565b600060a08284031215610c8a57600080fd5b50919050565b600060208284031215610ca257600080fd5b813567ffffffffffffffff81168114610b4557600080fd5b6000825160005b81811015610cdb5760208186018101518583015201610cc1565b81811115610cea576000828501525b509190910192915050565b6020810160048310610d1757634e487b7160e01b600052602160045260246000fd5b91905290565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b60008219821115610d5a57610d5a610daa565b500190565b600067ffffffffffffffff80831681851681830481118215151615610d8657610d86610daa565b02949350505050565b6000600019821415610da357610da3610daa565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea26469706673582212208aaec700e02eed706282e47e5d50f9254a7f4d77235dbeafb89ff5d3e172578464736f6c63430008060033",
}

// ETHSwapABI is the input ABI used to generate the binding from.
// Deprecated: Use ETHSwapMetaData.ABI instead.
var ETHSwapABI = ETHSwapMetaData.ABI

// Deprecated: Use ETHSwapMetaData.Sigs instead.
// ETHSwapFuncSigs maps the 4-byte function signature to its string representation.
var ETHSwapFuncSigs = ETHSwapMetaData.Sigs

// ETHSwapBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ETHSwapMetaData.Bin instead.
var ETHSwapBin = ETHSwapMetaData.Bin

// DeployETHSwap deploys a new Ethereum contract, binding an instance of ETHSwap to it.
func DeployETHSwap(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ETHSwap, error) {
	parsed, err := ETHSwapMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ETHSwapBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ETHSwap{ETHSwapCaller: ETHSwapCaller{contract: contract}, ETHSwapTransactor: ETHSwapTransactor{contract: contract}, ETHSwapFilterer: ETHSwapFilterer{contract: contract}}, nil
}

// ETHSwap is an auto generated Go binding around an Ethereum contract.
type ETHSwap struct {
	ETHSwapCaller     // Read-only binding to the contract
	ETHSwapTransactor // Write-only binding to the contract
	ETHSwapFilterer   // Log filterer for contract events
}

// ETHSwapCaller is an auto generated read-only Go binding around an Ethereum contract.
type ETHSwapCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHSwapTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ETHSwapTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHSwapFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ETHSwapFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHSwapSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ETHSwapSession struct {
	Contract     *ETHSwap          // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ETHSwapCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ETHSwapCallerSession struct {
	Contract *ETHSwapCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts  // Call options to use throughout this session
}

// ETHSwapTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ETHSwapTransactorSession struct {
	Contract     *ETHSwapTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// ETHSwapRaw is an auto generated low-level Go binding around an Ethereum contract.
type ETHSwapRaw struct {
	Contract *ETHSwap // Generic contract binding to access the raw methods on
}

// ETHSwapCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ETHSwapCallerRaw struct {
	Contract *ETHSwapCaller // Generic read-only contract binding to access the raw methods on
}

// ETHSwapTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ETHSwapTransactorRaw struct {
	Contract *ETHSwapTransactor // Generic write-only contract binding to access the raw methods on
}

// NewETHSwap creates a new instance of ETHSwap, bound to a specific deployed contract.
func NewETHSwap(address common.Address, backend bind.ContractBackend) (*ETHSwap, error) {
	contract, err := bindETHSwap(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ETHSwap{ETHSwapCaller: ETHSwapCaller{contract: contract}, ETHSwapTransactor: ETHSwapTransactor{contract: contract}, ETHSwapFilterer: ETHSwapFilterer{contract: contract}}, nil
}

// NewETHSwapCaller creates a new read-only instance of ETHSwap, bound to a specific deployed contract.
func NewETHSwapCaller(address common.Address, caller bind.ContractCaller) (*ETHSwapCaller, error) {
	contract, err := bindETHSwap(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ETHSwapCaller{contract: contract}, nil
}

// NewETHSwapTransactor creates a new write-only instance of ETHSwap, bound to a specific deployed contract.
func NewETHSwapTransactor(address common.Address, transactor bind.ContractTransactor) (*ETHSwapTransactor, error) {
	contract, err := bindETHSwap(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ETHSwapTransactor{contract: contract}, nil
}

// NewETHSwapFilterer creates a new log filterer instance of ETHSwap, bound to a specific deployed contract.
func NewETHSwapFilterer(address common.Address, filterer bind.ContractFilterer) (*ETHSwapFilterer, error) {
	contract, err := bindETHSwap(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ETHSwapFilterer{contract: contract}, nil
}

// bindETHSwap binds a generic wrapper to an already deployed contract.
func bindETHSwap(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(ETHSwapABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ETHSwap *ETHSwapRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ETHSwap.Contract.ETHSwapCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ETHSwap *ETHSwapRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ETHSwap.Contract.ETHSwapTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ETHSwap *ETHSwapRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ETHSwap.Contract.ETHSwapTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ETHSwap *ETHSwapCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ETHSwap.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ETHSwap *ETHSwapTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ETHSwap.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ETHSwap *ETHSwapTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ETHSwap.Contract.contract.Transact(opts, method, params...)
}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ETHSwap *ETHSwapCaller) ContractKey(opts *bind.CallOpts, c ETHSwapContract) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "contractKey", c)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ETHSwap *ETHSwapSession) ContractKey(c ETHSwapContract) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, c)
}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) ContractKey(c ETHSwapContract) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, c)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRedeemable(opts *bind.CallOpts, c ETHSwapContract) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRedeemable", c)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRedeemable(c ETHSwapContract) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, c)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ETHSwap *ETHSwapCallerSession) IsRedeemable(c ETHSwapContract) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, c)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapCaller) SecretValidates(opts *bind.CallOpts, secret [32]byte, secretHash [32]byte) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "secretValidates", secret, secretHash)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.SecretValidates(&_ETHSwap.CallOpts, secret, secretHash)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapCallerSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.SecretValidates(&_ETHSwap.CallOpts, secret, secretHash)
}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns(uint8)
func (_ETHSwap *ETHSwapCaller) State(opts *bind.CallOpts, c ETHSwapContract) (uint8, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "state", c)

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns(uint8)
func (_ETHSwap *ETHSwapSession) State(c ETHSwapContract) (uint8, error) {
	return _ETHSwap.Contract.State(&_ETHSwap.CallOpts, c)
}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns(uint8)
func (_ETHSwap *ETHSwapCallerSession) State(c ETHSwapContract) (uint8, error) {
	return _ETHSwap.Contract.State(&_ETHSwap.CallOpts, c)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "swaps", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, contracts []ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(contracts []ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(contracts []ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, contracts)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactorSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ETHSwap *ETHSwapTransactor) Refund(opts *bind.TransactOpts, c ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "refund", c)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ETHSwap *ETHSwapSession) Refund(c ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, c)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ETHSwap *ETHSwapTransactorSession) Refund(c ETHSwapContract) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, c)
}
