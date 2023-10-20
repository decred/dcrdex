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

// ETHSwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRedemption struct {
	V      ETHSwapVector
	Secret [32]byte
}

// ETHSwapStatus is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapStatus struct {
	Step        uint8
	Secret      [32]byte
	BlockNumber *big.Int
}

// ETHSwapVector is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapVector struct {
	SecretHash      [32]byte
	Initiator       common.Address
	RefundTimestamp uint64
	Participant     common.Address
	Value           uint64
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50610e8e806100206000396000f3fe60806040526004361061007b5760003560e01c80637802689d1161004e5780637802689d1461011b578063d2544c061461013b578063eb84e7f21461015b578063ed7cbed71461019657600080fd5b8063428b16e11461008057806361a16e33146100a257806364a97bff146100d857806377d7e031146100eb575b600080fd5b34801561008c57600080fd5b506100a061009b366004610b6f565b6101b6565b005b3480156100ae57600080fd5b506100c26100bd366004610be4565b610447565b6040516100cf9190610c12565b60405180910390f35b6100a06100e6366004610c55565b610503565b3480156100f757600080fd5b5061010b610106366004610cb8565b61073b565b60405190151581526020016100cf565b34801561012757600080fd5b5061010b610136366004610be4565b6107b5565b34801561014757600080fd5b506100a0610156366004610be4565b6107e8565b34801561016757600080fd5b50610188610176366004610cda565b60006020819052908152604090205481565b6040519081526020016100cf565b3480156101a257600080fd5b506101886101b1366004610be4565b610a4a565b3233146101de5760405162461bcd60e51b81526004016101d590610cf3565b60405180910390fd5b6000805b828110156103b057368484838181106101fd576101fd610d1d565b60c0029190910191503390506102196080830160608401610d33565b6001600160a01b03161461025c5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b60448201526064016101d5565b6000808061026984610b43565b92509250925060008111801561027e57504381105b6102ba5760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b60448201526064016101d5565b6102c582853561073b565b156103055760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b60448201526064016101d5565b61031460a0850135853561073b565b6103515760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b60448201526064016101d5565b600083815260208190526040902060a0850180359091556103759060808601610d63565b61038390633b9aca00610da3565b6103979067ffffffffffffffff1687610dd3565b95505050505080806103a890610deb565b9150506101e2565b50604051600090339083908381818185875af1925050503d80600081146103f3576040519150601f19603f3d011682016040523d82523d6000602084013e6103f8565b606091505b50909150506001811515146104415760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101d5565b50505050565b60408051606081018252600080825260208201819052918101829052908061046e84610b43565b92509250506104986040805160608101909152806000815260006020820181905260409091015290565b816000036104bf578060005b908160038111156104b7576104b7610bfc565b9052506104fb565b600183016104cf578060036104a4565b6104da83863561073b565b156104ef5760028152602081018390526104fb565b60018152604081018290525b949350505050565b3233146105225760405162461bcd60e51b81526004016101d590610cf3565b6000805b828110156106fc573684848381811061054157610541610d1d565b905060a002019050600081608001602081019061055e9190610d63565b67ffffffffffffffff161161059d5760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016101d5565b60006105af6060830160408401610d63565b67ffffffffffffffff16116105fa5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016101d5565b600061060582610a4a565b60008181526020819052604090205490915080156106565760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b60448201526064016101d5565b504361066381843561073b565b156106a15760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b60448201526064016101d5565b60008281526020819052604090208190556106c260a0840160808501610d63565b6106d090633b9aca00610da3565b6106e49067ffffffffffffffff1686610dd3565b945050505080806106f490610deb565b915050610526565b503481146107365760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b60448201526064016101d5565b505050565b60008160028460405160200161075391815260200190565b60408051601f198184030181529082905261076d91610e04565b602060405180830381855afa15801561078a573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107ad9190610e3f565b149392505050565b60008060006107c384610b43565b9250925050806000141580156104fb57506107df82853561073b565b15949350505050565b3233146108075760405162461bcd60e51b81526004016101d590610cf3565b6108176060820160408301610d63565b67ffffffffffffffff164210156108675760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b60448201526064016101d5565b600080600061087584610b43565b92509250925060008111801561088b5750438111155b6108c95760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b60448201526064016101d5565b6108d482853561073b565b156109195760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b60448201526064016101d5565b600182016109615760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c99599d5b991959605a1b60448201526064016101d5565b600083815260208181526040808320600019905561098491908701908701610d33565b6001600160a01b031661099d60a0870160808801610d63565b6109ab90633b9aca00610da3565b67ffffffffffffffff1660405160006040518083038185875af1925050503d80600081146109f5576040519150601f19603f3d011682016040523d82523d6000602084013e6109fa565b606091505b5090915050600181151514610a435760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101d5565b5050505050565b600060028235610a606040850160208601610d33565b60601b846060016020810190610a769190610d33565b60601b610a8960a0870160808801610d63565b60c01b610a9c6060880160408901610d63565b6040805160208101969096526bffffffffffffffffffffffff19948516908601529190921660548401526001600160c01b0319918216606884015260c01b16607082015260780160408051601f1981840301815290829052610afd91610e04565b602060405180830381855afa158015610b1a573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610b3d9190610e3f565b92915050565b600080600080610b5285610a4a565b600081815260208190526040902054909690955085945092505050565b60008060208385031215610b8257600080fd5b823567ffffffffffffffff80821115610b9a57600080fd5b818501915085601f830112610bae57600080fd5b813581811115610bbd57600080fd5b86602060c083028501011115610bd257600080fd5b60209290920196919550909350505050565b600060a08284031215610bf657600080fd5b50919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110610c3757634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060208385031215610c6857600080fd5b823567ffffffffffffffff80821115610c8057600080fd5b818501915085601f830112610c9457600080fd5b813581811115610ca357600080fd5b86602060a083028501011115610bd257600080fd5b60008060408385031215610ccb57600080fd5b50508035926020909101359150565b600060208284031215610cec57600080fd5b5035919050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b634e487b7160e01b600052603260045260246000fd5b600060208284031215610d4557600080fd5b81356001600160a01b0381168114610d5c57600080fd5b9392505050565b600060208284031215610d7557600080fd5b813567ffffffffffffffff81168114610d5c57600080fd5b634e487b7160e01b600052601160045260246000fd5b600067ffffffffffffffff80831681851681830481118215151615610dca57610dca610d8d565b02949350505050565b60008219821115610de657610de6610d8d565b500190565b600060018201610dfd57610dfd610d8d565b5060010190565b6000825160005b81811015610e255760208186018101518583015201610e0b565b81811115610e34576000828501525b509190910192915050565b600060208284031215610e5157600080fd5b505191905056fea2646970667358221220d735868682c69bb69ed936f11e2f136e7da44cb347e0b1d67acd1e088ea13dd664736f6c634300080f0033",
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
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCaller) ContractKey(opts *bind.CallOpts, v ETHSwapVector) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "contractKey", v)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapSession) ContractKey(v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, v)
}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) ContractKey(v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) v) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRedeemable(opts *bind.CallOpts, v ETHSwapVector) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRedeemable", v)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) v) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRedeemable(v ETHSwapVector) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) v) view returns(bool)
func (_ETHSwap *ETHSwapCallerSession) IsRedeemable(v ETHSwapVector) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, v)
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

// Status is a free data retrieval call binding the contract method 0x61a16e33.
//
// Solidity: function status((bytes32,address,uint64,address,uint64) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapCaller) Status(opts *bind.CallOpts, v ETHSwapVector) (ETHSwapStatus, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "status", v)

	if err != nil {
		return *new(ETHSwapStatus), err
	}

	out0 := *abi.ConvertType(out[0], new(ETHSwapStatus)).(*ETHSwapStatus)

	return out0, err

}

// Status is a free data retrieval call binding the contract method 0x61a16e33.
//
// Solidity: function status((bytes32,address,uint64,address,uint64) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapSession) Status(v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, v)
}

// Status is a free data retrieval call binding the contract method 0x61a16e33.
//
// Solidity: function status((bytes32,address,uint64,address,uint64) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapCallerSession) Status(v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, v)
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
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(contracts []ETHSwapVector) (*types.Transaction, error) {
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
// Solidity: function refund((bytes32,address,uint64,address,uint64) v) returns()
func (_ETHSwap *ETHSwapTransactor) Refund(opts *bind.TransactOpts, v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "refund", v)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) v) returns()
func (_ETHSwap *ETHSwapSession) Refund(v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, v)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) v) returns()
func (_ETHSwap *ETHSwapTransactorSession) Refund(v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, v)
}
