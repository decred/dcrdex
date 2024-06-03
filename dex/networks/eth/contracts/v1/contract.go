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
	Value           *big.Int
	Initiator       common.Address
	RefundTimestamp uint64
	Participant     common.Address
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50610df2806100206000396000f3fe60806040526004361061007b5760003560e01c8063a76f9f2d1161004e578063a76f9f2d1461010a578063d5cfd04914610138578063db3b419c14610158578063eb84e7f21461018557600080fd5b806323f0388b146100805780633da59631146100a257806377d7e031146100b55780638cd8dd97146100ea575b600080fd5b34801561008c57600080fd5b506100a061009b366004610b14565b6101b2565b005b6100a06100b0366004610b89565b610420565b3480156100c157600080fd5b506100d56100d0366004610bec565b61069d565b60405190151581526020015b60405180910390f35b3480156100f657600080fd5b506100a0610105366004610c0e565b610719565b34801561011657600080fd5b5061012a610125366004610c0e565b610911565b6040519081526020016100e1565b34801561014457600080fd5b506100d5610153366004610c0e565b6109f9565b34801561016457600080fd5b50610178610173366004610c0e565b610a2d565b6040516100e19190610c3c565b34801561019157600080fd5b5061012a6101a0366004610c7f565b60006020819052908152604090205481565b3233146101da5760405162461bcd60e51b81526004016101d190610c98565b60405180910390fd5b6000805b8281101561038957368484838181106101f9576101f9610cc2565b60c00291909101915033905061021560a0830160808401610cd8565b6001600160a01b0316146102585760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b60448201526064016101d1565b6000808061026584610ae8565b92509250925060008111801561027a57504381105b6102b65760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b60448201526064016101d1565b6102c182853561069d565b156103015760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b60448201526064016101d1565b61031060a0850135853561069d565b61034d5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b60448201526064016101d1565b60008381526020818152604090912060a086013590556103709085013587610d1e565b955050505050808061038190610d31565b9150506101de565b50604051600090339083908381818185875af1925050503d80600081146103cc576040519150601f19603f3d011682016040523d82523d6000602084013e6103d1565b606091505b509091505060018115151461041a5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101d1565b50505050565b32331461043f5760405162461bcd60e51b81526004016101d190610c98565b6000805b8281101561065e573684848381811061045e5761045e610cc2565b905060a00201905060008160200135116104a25760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016101d1565b60006104b46080830160608401610d4a565b67ffffffffffffffff16116104ff5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016101d1565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf8135016105805760405162461bcd60e51b815260206004820152602860248201527f696c6c6567616c2073656372657420686173682028726566756e64207265636f604482015267726420686173682960c01b60648201526084016101d1565b600061058b82610911565b60008181526020819052604090205490915080156105dc5760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b60448201526064016101d1565b50436105e981843561069d565b156106275760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b60448201526064016101d1565b6000828152602081815260409091208290556106469084013586610d1e565b9450505050808061065690610d31565b915050610443565b503481146106985760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b60448201526064016101d1565b505050565b6000816002846040516020016106b591815260200190565b60408051601f19818403018152908290526106cf91610d74565b602060405180830381855afa1580156106ec573d6000803e3d6000fd5b5050506040513d601f19601f8201168201806040525081019061070f9190610da3565b1490505b92915050565b3233146107385760405162461bcd60e51b81526004016101d190610c98565b6107486080820160608301610d4a565b67ffffffffffffffff164210156107985760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b60448201526064016101d1565b60008060006107a684610ae8565b9250925092506000811180156107bc5750438111155b6107fa5760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b60448201526064016101d1565b61080582853561069d565b1561084a5760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b60448201526064016101d1565b600083815260208190526040808220600019905561086e9060608701908701610cd8565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d80600081146108bc576040519150601f19603f3d011682016040523d82523d6000602084013e6108c1565b606091505b509091505060018115151461090a5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101d1565b5050505050565b6000600282356109276060850160408601610cd8565b60601b61093a60a0860160808701610cd8565b60601b856020013560001b8660600160208101906109589190610d4a565b6040805160208101969096526bffffffffffffffffffffffff199485169086015292909116605484015260688301526001600160c01b031960c09190911b16608882015260900160408051601f19818403018152908290526109b991610d74565b602060405180830381855afa1580156109d6573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107139190610da3565b6000806000610a0784610ae8565b925092505080600014158015610a255750610a2382853561069d565b155b949350505050565b604080516060810182526000808252602082018190529181018290529080610a5484610ae8565b9250925050610a7e6040805160608101909152806000815260006020820181905260409091015290565b81600003610aa5578060005b90816003811115610a9d57610a9d610c26565b905250610a25565b60018301610ab557806003610a8a565b610ac083863561069d565b15610ad5576002815260208101839052610a25565b6001815260408101919091529392505050565b600080600080610af785610911565b600081815260208190526040902054909690955085945092505050565b60008060208385031215610b2757600080fd5b823567ffffffffffffffff80821115610b3f57600080fd5b818501915085601f830112610b5357600080fd5b813581811115610b6257600080fd5b86602060c083028501011115610b7757600080fd5b60209290920196919550909350505050565b60008060208385031215610b9c57600080fd5b823567ffffffffffffffff80821115610bb457600080fd5b818501915085601f830112610bc857600080fd5b813581811115610bd757600080fd5b86602060a083028501011115610b7757600080fd5b60008060408385031215610bff57600080fd5b50508035926020909101359150565b600060a08284031215610c2057600080fd5b50919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110610c6157634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b600060208284031215610c9157600080fd5b5035919050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b634e487b7160e01b600052603260045260246000fd5b600060208284031215610cea57600080fd5b81356001600160a01b0381168114610d0157600080fd5b9392505050565b634e487b7160e01b600052601160045260246000fd5b8082018082111561071357610713610d08565b600060018201610d4357610d43610d08565b5060010190565b600060208284031215610d5c57600080fd5b813567ffffffffffffffff81168114610d0157600080fd5b6000825160005b81811015610d955760208186018101518583015201610d7b565b506000920191825250919050565b600060208284031215610db557600080fd5b505191905056fea2646970667358221220420ce9cfd5a39be8e719369517f704148d580fb33472b5b8947c77d61eee66d664736f6c63430008120033",
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

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCaller) ContractKey(opts *bind.CallOpts, v ETHSwapVector) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "contractKey", v)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapSession) ContractKey(v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, v)
}

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) ContractKey(v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRedeemable(opts *bind.CallOpts, v ETHSwapVector) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRedeemable", v)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRedeemable(v ETHSwapVector) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
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

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapCaller) Status(opts *bind.CallOpts, v ETHSwapVector) (ETHSwapStatus, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "status", v)

	if err != nil {
		return *new(ETHSwapStatus), err
	}

	out0 := *abi.ConvertType(out[0], new(ETHSwapStatus)).(*ETHSwapStatus)

	return out0, err

}

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapSession) Status(v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, v)
}

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
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

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, contracts)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactorSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapTransactor) Refund(opts *bind.TransactOpts, v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "refund", v)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapSession) Refund(v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, v)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapTransactorSession) Refund(v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, v)
}
