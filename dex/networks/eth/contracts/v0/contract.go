// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package v0

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

// ETHSwapInitiation is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapInitiation struct {
	RefundTimestamp *big.Int
	SecretHash      [32]byte
	Participant     common.Address
	Value           *big.Int
}

// ETHSwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRedemption struct {
	Secret     [32]byte
	SecretHash [32]byte
}

// ETHSwapSwap is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapSwap struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"isRefundable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structETHSwap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"a8793f94": "initiate((uint256,bytes32,address,uint256)[])",
		"bfd2fd97": "isRedeemable(bytes32,bytes32)",
		"d0f761c0": "isRefundable(bytes32)",
		"f4fd17f9": "redeem((bytes32,bytes32)[])",
		"7249fbb6": "refund(bytes32)",
		"76467cbd": "swap(bytes32)",
		"eb84e7f2": "swaps(bytes32)",
	},
	Bin: "0x608060405234801561001057600080fd5b50610cb8806100206000396000f3fe6080604052600436106100705760003560e01c8063bfd2fd971161004e578063bfd2fd97146100e0578063d0f761c014610110578063eb84e7f214610130578063f4fd17f9146101ac57600080fd5b80637249fbb61461007557806376467cbd14610097578063a8793f94146100cd575b600080fd5b34801561008157600080fd5b50610095610090366004610a81565b6101cc565b005b3480156100a357600080fd5b506100b76100b2366004610a81565b610304565b6040516100c49190610ba8565b60405180910390f35b6100956100db3660046109a9565b6103df565b3480156100ec57600080fd5b506101006100fb366004610ab3565b6105d6565b60405190151581526020016100c4565b34801561011c57600080fd5b5061010061012b366004610a81565b6106ad565b34801561013c57600080fd5b5061019961014b366004610a81565b60006020819052908152604090208054600182015460028301546003840154600485015460059095015493949293919290916001600160a01b0391821691811690600160a01b900460ff1687565b6040516100c49796959493929190610b32565b3480156101b857600080fd5b506100956101c7366004610a1e565b6106f2565b3233146101f45760405162461bcd60e51b81526004016101eb90610b7e565b60405180910390fd5b6101fd816106ad565b61023a5760405162461bcd60e51b815260206004820152600e60248201526d6e6f7420726566756e6461626c6560901b60448201526064016101eb565b60008181526020819052604080822060058101805460ff60a01b1916600360a01b1790556004810154600182015492519193926001600160a01b03909116918381818185875af1925050503d80600081146102b1576040519150601f19603f3d011682016040523d82523d6000602084013e6102b6565b606091505b50909150506001811515146102ff5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101eb565b505050565b6103416040805160e081018252600080825260208201819052918101829052606081018290526080810182905260a081018290529060c082015290565b60008281526020818152604091829020825160e08101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b039081166080840152600584015490811660a084015291929160c0840191600160a01b90910460ff16908111156103c5576103c5610c56565b60038111156103d6576103d6610c56565b90525092915050565b3233146103fe5760405162461bcd60e51b81526004016101eb90610b7e565b6000805b8281101561059c573684848381811061041d5761041d610c6c565b90506080020190506000806000836020013581526020019081526020016000209050600082606001351161047b5760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016101eb565b81356104bd5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016101eb565b60006005820154600160a01b900460ff1660038111156104df576104df610c56565b146105175760405162461bcd60e51b8152602060048201526008602482015267064757020737761760c41b60448201526064016101eb565b436002820155813560038201556004810180546001600160a01b031916331790556105486060830160408401610980565b6005820180546060850135600185018190556001600160a01b03939093166001600160a81b031990911617600160a01b1790556105859085610c0d565b93505050808061059490610c25565b915050610402565b503481146102ff5760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b60448201526064016101eb565b60006001600084815260208190526040902060050154600160a01b900460ff16600381111561060757610607610c56565b14801561062d57506000838152602081905260409020600501546001600160a01b031633145b80156106a657508260028360405160200161064a91815260200190565b60408051601f198184030181529082905261066491610af7565b602060405180830381855afa158015610681573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106a49190610a9a565b145b9392505050565b600081815260208190526040812060016005820154600160a01b900460ff1660038111156106dd576106dd610c56565b1480156106a657506003015442101592915050565b3233146107115760405162461bcd60e51b81526004016101eb90610b7e565b6000805b828110156108e9573684848381811061073057610730610c6c565b6020604091820293909301838101356000908152938490529220919250600190506005820154600160a01b900460ff16600381111561077157610771610c56565b146107aa5760405162461bcd60e51b815260206004820152600960248201526862616420737461746560b81b60448201526064016101eb565b60058101546001600160a01b031633146107f85760405162461bcd60e51b815260206004820152600f60248201526e189859081c185c9d1a58da5c185b9d608a1b60448201526064016101eb565b81602001356002836000013560405160200161081691815260200190565b60408051601f198184030181529082905261083091610af7565b602060405180830381855afa15801561084d573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906108709190610a9a565b146108aa5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b60448201526064016101eb565b60058101805460ff60a01b1916600160a11b1790558135815560018101546108d29085610c0d565b9350505080806108e190610c25565b915050610715565b50604051600090339083908381818185875af1925050503d806000811461092c576040519150601f19603f3d011682016040523d82523d6000602084013e610931565b606091505b509091505060018115151461097a5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101eb565b50505050565b60006020828403121561099257600080fd5b81356001600160a01b03811681146106a657600080fd5b600080602083850312156109bc57600080fd5b823567ffffffffffffffff808211156109d457600080fd5b818501915085601f8301126109e857600080fd5b8135818111156109f757600080fd5b8660208260071b8501011115610a0c57600080fd5b60209290920196919550909350505050565b60008060208385031215610a3157600080fd5b823567ffffffffffffffff80821115610a4957600080fd5b818501915085601f830112610a5d57600080fd5b813581811115610a6c57600080fd5b8660208260061b8501011115610a0c57600080fd5b600060208284031215610a9357600080fd5b5035919050565b600060208284031215610aac57600080fd5b5051919050565b60008060408385031215610ac657600080fd5b50508035926020909101359150565b60048110610af357634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b81811015610b185760208186018101518583015201610afe565b81811115610b27576000828501525b509190910192915050565b8781526020810187905260408101869052606081018590526001600160a01b038481166080830152831660a082015260e08101610b7260c0830184610ad5565b98975050505050505050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b600060e08201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a0850152505060c0830151610c0660c0840182610ad5565b5092915050565b60008219821115610c2057610c20610c40565b500190565b6000600019821415610c3957610c39610c40565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea264697066735822122013715f6c21bd76ab717b09b07892f30fec24b97445914cbd2d297fa3c211b6a464736f6c63430008060033",
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

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRedeemable", secretHash, secret)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRedeemable(secretHash [32]byte, secret [32]byte) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, secretHash, secret)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ETHSwap *ETHSwapCallerSession) IsRedeemable(secretHash [32]byte, secret [32]byte) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, secretHash, secret)
}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRefundable", secretHash)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRefundable(secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.IsRefundable(&_ETHSwap.CallOpts, secretHash)
}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ETHSwap *ETHSwapCallerSession) IsRefundable(secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.IsRefundable(&_ETHSwap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ETHSwap *ETHSwapCaller) Swap(opts *bind.CallOpts, secretHash [32]byte) (ETHSwapSwap, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "swap", secretHash)

	if err != nil {
		return *new(ETHSwapSwap), err
	}

	out0 := *abi.ConvertType(out[0], new(ETHSwapSwap)).(*ETHSwapSwap)

	return out0, err

}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ETHSwap *ETHSwapSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ETHSwap *ETHSwapCallerSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, uint8 state)
func (_ETHSwap *ETHSwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "swaps", arg0)

	outstruct := new(struct {
		Secret               [32]byte
		Value                *big.Int
		InitBlockNumber      *big.Int
		RefundBlockTimestamp *big.Int
		Initiator            common.Address
		Participant          common.Address
		State                uint8
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Secret = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.Value = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.InitBlockNumber = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.RefundBlockTimestamp = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	outstruct.Initiator = *abi.ConvertType(out[4], new(common.Address)).(*common.Address)
	outstruct.Participant = *abi.ConvertType(out[5], new(common.Address)).(*common.Address)
	outstruct.State = *abi.ConvertType(out[6], new(uint8)).(*uint8)

	return *outstruct, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, uint8 state)
func (_ETHSwap *ETHSwapSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, uint8 state)
func (_ETHSwap *ETHSwapCallerSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", initiations)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, initiations)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, initiations)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactorSession) Redeem(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapTransactor) Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "refund", secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapTransactorSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, secretHash)
}
