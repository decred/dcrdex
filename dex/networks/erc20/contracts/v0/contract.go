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

// ERC20SwapInitiation is an auto generated low-level Go binding around an user-defined struct.
type ERC20SwapInitiation struct {
	RefundTimestamp *big.Int
	SecretHash      [32]byte
	Participant     common.Address
	Value           *big.Int
}

// ERC20SwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ERC20SwapRedemption struct {
	Secret     [32]byte
	SecretHash [32]byte
}

// ERC20SwapSwap is an auto generated low-level Go binding around an user-defined struct.
type ERC20SwapSwap struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	Token                common.Address
	State                uint8
}

// ERC20SwapMetaData contains all meta data concerning the ERC20Swap contract.
var ERC20SwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structERC20Swap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"isRefundable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"internalType\":\"structERC20Swap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structERC20Swap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"6f70b30a": "initiate((uint256,bytes32,address,uint256)[],address)",
		"bfd2fd97": "isRedeemable(bytes32,bytes32)",
		"d0f761c0": "isRefundable(bytes32)",
		"f4fd17f9": "redeem((bytes32,bytes32)[])",
		"7249fbb6": "refund(bytes32)",
		"76467cbd": "swap(bytes32)",
	},
	Bin: "0x608060405234801561001057600080fd5b50610c5c806100206000396000f3fe608060405234801561001057600080fd5b50600436106100625760003560e01c80636f70b30a146100675780637249fbb61461007c57806376467cbd1461008f578063bfd2fd97146100b8578063d0f761c0146100db578063f4fd17f9146100ee575b600080fd5b61007a61007536600461097e565b610101565b005b61007a61008a366004610a99565b610353565b6100a261009d366004610a99565b6104a2565b6040516100af9190610b28565b60405180910390f35b6100cb6100c6366004610acb565b610592565b60405190151581526020016100af565b6100cb6100e9366004610a99565b61065c565b61007a6100fc366004610a02565b6106bc565b32331461010d57600080fd5b6000805b83811015610236573685858381811061012c5761012c610c10565b90506080020190506000806000836020013581526020019081526020016000209050600082606001351161015f57600080fd5b813561016a57600080fd5b60006006820154600160a01b900460ff16600381111561018c5761018c610bfa565b1461019657600080fd5b436002820155813560038201556004810180546001600160a01b031916331790556101c76060830160408401610963565b6005820180546001600160a01b039283166001600160a01b03199091161790556060830135600183018190556006830180549288166001600160a81b031990931692909217600160a01b1790915561021f9085610bb1565b93505050808061022e90610bc9565b915050610111565b5060408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b0386169161029691610aed565b6000604051808303816000865af19150503d80600081146102d3576040519150601f19603f3d011682016040523d82523d6000602084013e6102d8565b606091505b5090925090508180156103035750805115806103035750808060200190518101906103039190610a77565b61034b5760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b60448201526064015b60405180910390fd5b505050505050565b32331461035f57600080fd5b6103688161065c565b61037157600080fd5b600081815260208181526040808320600681018054600360a01b60ff60a01b198216179091556001820154835133602482015260448082019290925284518082039092018252606401845293840180516001600160e01b031663a9059cbb60e01b17905291519093926060926001600160a01b0316916103f19190610aed565b6000604051808303816000865af19150503d806000811461042e576040519150601f19603f3d011682016040523d82523d6000602084013e610433565b606091505b50909250905081801561045e57508051158061045e57508080602001905181019061045e9190610a77565b61049c5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610342565b50505050565b6104e76040805161010081018252600080825260208201819052918101829052606081018290526080810182905260a0810182905260c081018290529060e082015290565b6000828152602081815260409182902082516101008101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b0390811660808401526005840154811660a0840152600684015490811660c084015291929160e0840191600160a01b90910460ff169081111561057857610578610bfa565b600381111561058957610589610bfa565b90525092915050565b600082815260208190526040812060016006820154600160a01b900460ff1660038111156105c2576105c2610bfa565b1480156105db575060058101546001600160a01b031633145b80156106545750836002846040516020016105f891815260200190565b60408051601f198184030181529082905261061291610aed565b602060405180830381855afa15801561062f573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106529190610ab2565b145b949350505050565b600081815260208190526040812060016006820154600160a01b900460ff16600381111561068c5761068c610bfa565b1480156106a5575060048101546001600160a01b031633145b80156106b5575080600301544210155b9392505050565b3233146106c857600080fd5b600080805b8381101561084257368585838181106106e8576106e8610c10565b602060409182029390930183810135600090815293849052922091925050826107205760068101546001600160a01b0316935061073c565b60068101546001600160a01b0385811691161461073c57600080fd5b60016006820154600160a01b900460ff16600381111561075e5761075e610bfa565b1461076857600080fd5b60058101546001600160a01b0316331461078157600080fd5b81602001356002836000013560405160200161079f91815260200190565b60408051601f19818403018152908290526107b991610aed565b602060405180830381855afa1580156107d6573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107f99190610ab2565b1461080357600080fd5b60068101805460ff60a01b1916600160a11b17905581358155600181015461082b9086610bb1565b94505050808061083a90610bc9565b9150506106cd565b5060408051336024820152604480820185905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b0385169161089c91610aed565b6000604051808303816000865af19150503d80600081146108d9576040519150601f19603f3d011682016040523d82523d6000602084013e6108de565b606091505b5090925090508180156109095750805115806109095750808060200190518101906109099190610a77565b61034b5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610342565b80356001600160a01b038116811461095e57600080fd5b919050565b60006020828403121561097557600080fd5b6106b582610947565b60008060006040848603121561099357600080fd5b833567ffffffffffffffff808211156109ab57600080fd5b818601915086601f8301126109bf57600080fd5b8135818111156109ce57600080fd5b8760208260071b85010111156109e357600080fd5b6020928301955093506109f99186019050610947565b90509250925092565b60008060208385031215610a1557600080fd5b823567ffffffffffffffff80821115610a2d57600080fd5b818501915085601f830112610a4157600080fd5b813581811115610a5057600080fd5b8660208260061b8501011115610a6557600080fd5b60209290920196919550909350505050565b600060208284031215610a8957600080fd5b815180151581146106b557600080fd5b600060208284031215610aab57600080fd5b5035919050565b600060208284031215610ac457600080fd5b5051919050565b60008060408385031215610ade57600080fd5b50508035926020909101359150565b6000825160005b81811015610b0e5760208186018101518583015201610af4565b81811115610b1d576000828501525b509190910192915050565b60006101008201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a08501528060c08601511660c0850152505060e083015160048110610ba457634e487b7160e01b600052602160045260246000fd5b8060e08401525092915050565b60008219821115610bc457610bc4610be4565b500190565b6000600019821415610bdd57610bdd610be4565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea26469706673582212209a5206b099b92a725b6e8f0ce2fc78c8868a43ad02b1733394fdb151a3a7247b64736f6c63430008060033",
}

// ERC20SwapABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20SwapMetaData.ABI instead.
var ERC20SwapABI = ERC20SwapMetaData.ABI

// Deprecated: Use ERC20SwapMetaData.Sigs instead.
// ERC20SwapFuncSigs maps the 4-byte function signature to its string representation.
var ERC20SwapFuncSigs = ERC20SwapMetaData.Sigs

// ERC20SwapBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ERC20SwapMetaData.Bin instead.
var ERC20SwapBin = ERC20SwapMetaData.Bin

// DeployERC20Swap deploys a new Ethereum contract, binding an instance of ERC20Swap to it.
func DeployERC20Swap(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ERC20Swap, error) {
	parsed, err := ERC20SwapMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ERC20SwapBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ERC20Swap{ERC20SwapCaller: ERC20SwapCaller{contract: contract}, ERC20SwapTransactor: ERC20SwapTransactor{contract: contract}, ERC20SwapFilterer: ERC20SwapFilterer{contract: contract}}, nil
}

// ERC20Swap is an auto generated Go binding around an Ethereum contract.
type ERC20Swap struct {
	ERC20SwapCaller     // Read-only binding to the contract
	ERC20SwapTransactor // Write-only binding to the contract
	ERC20SwapFilterer   // Log filterer for contract events
}

// ERC20SwapCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20SwapCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20SwapTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20SwapTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20SwapFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20SwapFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20SwapSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20SwapSession struct {
	Contract     *ERC20Swap        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ERC20SwapCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20SwapCallerSession struct {
	Contract *ERC20SwapCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// ERC20SwapTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20SwapTransactorSession struct {
	Contract     *ERC20SwapTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// ERC20SwapRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20SwapRaw struct {
	Contract *ERC20Swap // Generic contract binding to access the raw methods on
}

// ERC20SwapCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20SwapCallerRaw struct {
	Contract *ERC20SwapCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20SwapTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20SwapTransactorRaw struct {
	Contract *ERC20SwapTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20Swap creates a new instance of ERC20Swap, bound to a specific deployed contract.
func NewERC20Swap(address common.Address, backend bind.ContractBackend) (*ERC20Swap, error) {
	contract, err := bindERC20Swap(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20Swap{ERC20SwapCaller: ERC20SwapCaller{contract: contract}, ERC20SwapTransactor: ERC20SwapTransactor{contract: contract}, ERC20SwapFilterer: ERC20SwapFilterer{contract: contract}}, nil
}

// NewERC20SwapCaller creates a new read-only instance of ERC20Swap, bound to a specific deployed contract.
func NewERC20SwapCaller(address common.Address, caller bind.ContractCaller) (*ERC20SwapCaller, error) {
	contract, err := bindERC20Swap(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20SwapCaller{contract: contract}, nil
}

// NewERC20SwapTransactor creates a new write-only instance of ERC20Swap, bound to a specific deployed contract.
func NewERC20SwapTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20SwapTransactor, error) {
	contract, err := bindERC20Swap(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20SwapTransactor{contract: contract}, nil
}

// NewERC20SwapFilterer creates a new log filterer instance of ERC20Swap, bound to a specific deployed contract.
func NewERC20SwapFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20SwapFilterer, error) {
	contract, err := bindERC20Swap(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20SwapFilterer{contract: contract}, nil
}

// bindERC20Swap binds a generic wrapper to an already deployed contract.
func bindERC20Swap(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(ERC20SwapABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Swap *ERC20SwapRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Swap.Contract.ERC20SwapCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Swap *ERC20SwapRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Swap.Contract.ERC20SwapTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Swap *ERC20SwapRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Swap.Contract.ERC20SwapTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Swap *ERC20SwapCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Swap.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Swap *ERC20SwapTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Swap.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Swap *ERC20SwapTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Swap.Contract.contract.Transact(opts, method, params...)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ERC20Swap *ERC20SwapCaller) IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "isRedeemable", secretHash, secret)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ERC20Swap *ERC20SwapSession) IsRedeemable(secretHash [32]byte, secret [32]byte) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, secretHash, secret)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xbfd2fd97.
//
// Solidity: function isRedeemable(bytes32 secretHash, bytes32 secret) view returns(bool)
func (_ERC20Swap *ERC20SwapCallerSession) IsRedeemable(secretHash [32]byte, secret [32]byte) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, secretHash, secret)
}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ERC20Swap *ERC20SwapCaller) IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "isRefundable", secretHash)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ERC20Swap *ERC20SwapSession) IsRefundable(secretHash [32]byte) (bool, error) {
	return _ERC20Swap.Contract.IsRefundable(&_ERC20Swap.CallOpts, secretHash)
}

// IsRefundable is a free data retrieval call binding the contract method 0xd0f761c0.
//
// Solidity: function isRefundable(bytes32 secretHash) view returns(bool)
func (_ERC20Swap *ERC20SwapCallerSession) IsRefundable(secretHash [32]byte) (bool, error) {
	return _ERC20Swap.Contract.IsRefundable(&_ERC20Swap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,address,uint8))
func (_ERC20Swap *ERC20SwapCaller) Swap(opts *bind.CallOpts, secretHash [32]byte) (ERC20SwapSwap, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "swap", secretHash)

	if err != nil {
		return *new(ERC20SwapSwap), err
	}

	out0 := *abi.ConvertType(out[0], new(ERC20SwapSwap)).(*ERC20SwapSwap)

	return out0, err

}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,address,uint8))
func (_ERC20Swap *ERC20SwapSession) Swap(secretHash [32]byte) (ERC20SwapSwap, error) {
	return _ERC20Swap.Contract.Swap(&_ERC20Swap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,address,uint8))
func (_ERC20Swap *ERC20SwapCallerSession) Swap(secretHash [32]byte) (ERC20SwapSwap, error) {
	return _ERC20Swap.Contract.Swap(&_ERC20Swap.CallOpts, secretHash)
}

// Initiate is a paid mutator transaction binding the contract method 0x6f70b30a.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations, address token) returns()
func (_ERC20Swap *ERC20SwapTransactor) Initiate(opts *bind.TransactOpts, initiations []ERC20SwapInitiation, token common.Address) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "initiate", initiations, token)
}

// Initiate is a paid mutator transaction binding the contract method 0x6f70b30a.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations, address token) returns()
func (_ERC20Swap *ERC20SwapSession) Initiate(initiations []ERC20SwapInitiation, token common.Address) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, initiations, token)
}

// Initiate is a paid mutator transaction binding the contract method 0x6f70b30a.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations, address token) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Initiate(initiations []ERC20SwapInitiation, token common.Address) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, initiations, token)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ERC20SwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapSession) Redeem(redemptions []ERC20SwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Redeem(redemptions []ERC20SwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ERC20Swap *ERC20SwapTransactor) Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "refund", secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ERC20Swap *ERC20SwapSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, secretHash)
}

// Refund is a paid mutator transaction binding the contract method 0x7249fbb6.
//
// Solidity: function refund(bytes32 secretHash) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Refund(secretHash [32]byte) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, secretHash)
}
