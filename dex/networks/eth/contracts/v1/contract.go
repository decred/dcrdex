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

// ETHSwapRecord is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRecord struct {
	State       uint8
	Secret      [32]byte
	BlockNumber *big.Int
}

// ETHSwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRedemption struct {
	C      ETHSwapContract
	Secret [32]byte
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"state\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Record\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50610e8e806100206000396000f3fe60806040526004361061007b5760003560e01c80637802689d1161004e5780637802689d1461011b578063d2544c061461013b578063eb84e7f21461015b578063ed7cbed71461019657600080fd5b806338ec176814610080578063428b16e1146100b657806364a97bff146100d857806377d7e031146100eb575b600080fd5b34801561008c57600080fd5b506100a061009b366004610b6f565b6101b6565b6040516100ad9190610b9d565b60405180910390f35b3480156100c257600080fd5b506100d66100d1366004610be0565b610272565b005b6100d66100e6366004610c55565b610503565b3480156100f757600080fd5b5061010b610106366004610cb8565b61073b565b60405190151581526020016100ad565b34801561012757600080fd5b5061010b610136366004610b6f565b6107b5565b34801561014757600080fd5b506100d6610156366004610b6f565b6107e8565b34801561016757600080fd5b50610188610176366004610cda565b60006020819052908152604090205481565b6040519081526020016100ad565b3480156101a257600080fd5b506101886101b1366004610b6f565b610a4a565b6040805160608101825260008082526020820181905291810182905290806101dd84610b43565b92509250506102076040805160608101909152806000815260006020820181905260409091015290565b8160000361022e578060005b9081600381111561022657610226610b87565b90525061026a565b6001830161023e57806003610213565b61024983863561073b565b1561025e57600281526020810183905261026a565b60018152604081018290525b949350505050565b32331461029a5760405162461bcd60e51b815260040161029190610cf3565b60405180910390fd5b6000805b8281101561046c57368484838181106102b9576102b9610d1d565b60c0029190910191503390506102d56080830160608401610d33565b6001600160a01b0316146103185760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b6044820152606401610291565b6000808061032584610b43565b92509250925060008111801561033a57504381105b6103765760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610291565b61038182853561073b565b156103c15760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610291565b6103d060a0850135853561073b565b61040d5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610291565b600083815260208190526040902060a0850180359091556104319060808601610d63565b61043f90633b9aca00610da3565b6104539067ffffffffffffffff1687610dd3565b955050505050808061046490610deb565b91505061029e565b50604051600090339083908381818185875af1925050503d80600081146104af576040519150601f19603f3d011682016040523d82523d6000602084013e6104b4565b606091505b50909150506001811515146104fd5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610291565b50505050565b3233146105225760405162461bcd60e51b815260040161029190610cf3565b6000805b828110156106fc573684848381811061054157610541610d1d565b905060a002019050600081608001602081019061055e9190610d63565b67ffffffffffffffff161161059d5760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b6044820152606401610291565b60006105af6060830160408401610d63565b67ffffffffffffffff16116105fa5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b6044820152606401610291565b600061060582610a4a565b60008181526020819052604090205490915080156106565760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b6044820152606401610291565b504361066381843561073b565b156106a15760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610291565b60008281526020819052604090208190556106c260a0840160808501610d63565b6106d090633b9aca00610da3565b6106e49067ffffffffffffffff1686610dd3565b945050505080806106f490610deb565b915050610526565b503481146107365760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b6044820152606401610291565b505050565b60008160028460405160200161075391815260200190565b60408051601f198184030181529082905261076d91610e04565b602060405180830381855afa15801561078a573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107ad9190610e3f565b149392505050565b60008060006107c384610b43565b92509250508060001415801561026a57506107df82853561073b565b15949350505050565b3233146108075760405162461bcd60e51b815260040161029190610cf3565b6108176060820160408301610d63565b67ffffffffffffffff164210156108675760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b6044820152606401610291565b600080600061087584610b43565b92509250925060008111801561088b5750438111155b6108c95760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b6044820152606401610291565b6108d482853561073b565b156109195760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b6044820152606401610291565b600182016109615760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c99599d5b991959605a1b6044820152606401610291565b600083815260208181526040808320600019905561098491908701908701610d33565b6001600160a01b031661099d60a0870160808801610d63565b6109ab90633b9aca00610da3565b67ffffffffffffffff1660405160006040518083038185875af1925050503d80600081146109f5576040519150601f19603f3d011682016040523d82523d6000602084013e6109fa565b606091505b5090915050600181151514610a435760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610291565b5050505050565b600060028235610a606040850160208601610d33565b60601b846060016020810190610a769190610d33565b60601b610a8960a0870160808801610d63565b60c01b610a9c6060880160408901610d63565b6040805160208101969096526bffffffffffffffffffffffff19948516908601529190921660548401526001600160c01b0319918216606884015260c01b16607082015260780160408051601f1981840301815290829052610afd91610e04565b602060405180830381855afa158015610b1a573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610b3d9190610e3f565b92915050565b600080600080610b5285610a4a565b600081815260208190526040902054909690955085945092505050565b600060a08284031215610b8157600080fd5b50919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110610bc257634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060208385031215610bf357600080fd5b823567ffffffffffffffff80821115610c0b57600080fd5b818501915085601f830112610c1f57600080fd5b813581811115610c2e57600080fd5b86602060c083028501011115610c4357600080fd5b60209290920196919550909350505050565b60008060208385031215610c6857600080fd5b823567ffffffffffffffff80821115610c8057600080fd5b818501915085601f830112610c9457600080fd5b813581811115610ca357600080fd5b86602060a083028501011115610c4357600080fd5b60008060408385031215610ccb57600080fd5b50508035926020909101359150565b600060208284031215610cec57600080fd5b5035919050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b634e487b7160e01b600052603260045260246000fd5b600060208284031215610d4557600080fd5b81356001600160a01b0381168114610d5c57600080fd5b9392505050565b600060208284031215610d7557600080fd5b813567ffffffffffffffff81168114610d5c57600080fd5b634e487b7160e01b600052601160045260246000fd5b600067ffffffffffffffff80831681851681830481118215151615610dca57610dca610d8d565b02949350505050565b60008219821115610de657610de6610d8d565b500190565b600060018201610dfd57610dfd610d8d565b5060010190565b6000825160005b81811015610e255760208186018101518583015201610e0b565b81811115610e34576000828501525b509190910192915050565b600060208284031215610e5157600080fd5b505191905056fea2646970667358221220ff4aa3b731ad448a2ec9f367bc9ddb0be755eebc0f8e3159b55e2113545da62764736f6c634300080f0033",
}

// ETHSwapABI is the input ABI used to generate the binding from.
// Deprecated: Use ETHSwapMetaData.ABI instead.
var ETHSwapABI = ETHSwapMetaData.ABI

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
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapCaller) State(opts *bind.CallOpts, c ETHSwapContract) (ETHSwapRecord, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "state", c)

	if err != nil {
		return *new(ETHSwapRecord), err
	}

	out0 := *abi.ConvertType(out[0], new(ETHSwapRecord)).(*ETHSwapRecord)

	return out0, err

}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapSession) State(c ETHSwapContract) (ETHSwapRecord, error) {
	return _ETHSwap.Contract.State(&_ETHSwap.CallOpts, c)
}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapCallerSession) State(c ETHSwapContract) (ETHSwapRecord, error) {
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
