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
	ethv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
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

// ERC20SwapMetaData contains all meta data concerning the ERC20Swap contract.
var ERC20SwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"TOKEN_ADDRESS\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structERC20Swap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"isRefundable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"internalType\":\"structERC20Swap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structERC20Swap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"0bdf5300": "TOKEN_ADDRESS()",
		"a8793f94": "initiate((uint256,bytes32,address,uint256)[])",
		"bfd2fd97": "isRedeemable(bytes32,bytes32)",
		"d0f761c0": "isRefundable(bytes32)",
		"f4fd17f9": "redeem((bytes32,bytes32)[])",
		"7249fbb6": "refund(bytes32)",
		"76467cbd": "swap(bytes32)",
		"eb84e7f2": "swaps(bytes32)",
	},
	Bin: "0x60a060405234801561001057600080fd5b50604051610fac380380610fac83398101604081905261002f91610044565b60601b6001600160601b031916608052610074565b60006020828403121561005657600080fd5b81516001600160a01b038116811461006d57600080fd5b9392505050565b60805160601c610f066100a6600039600081816092015281816102b5015281816106850152610ad70152610f066000f3fe608060405234801561001057600080fd5b50600436106100885760003560e01c8063bfd2fd971161005b578063bfd2fd9714610119578063d0f761c01461013c578063eb84e7f21461014f578063f4fd17f9146101be57600080fd5b80630bdf53001461008d5780637249fbb6146100d157806376467cbd146100e6578063a8793f9414610106575b600080fd5b6100b47f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020015b60405180910390f35b6100e46100df366004610ccf565b6101d1565b005b6100f96100f4366004610ccf565b610390565b6040516100c89190610df6565b6100e4610114366004610bd5565b61046b565b61012c610127366004610d01565b610766565b60405190151581526020016100c8565b61012c61014a366004610ccf565b610830565b6101ab61015d366004610ccf565b60006020819052908152604090208054600182015460028301546003840154600485015460059095015493949293919290916001600160a01b0391821691811690600160a01b900460ff1687565b6040516100c89796959493929190610d80565b6100e46101cc366004610c4a565b610890565b3233146101f95760405162461bcd60e51b81526004016101f090610dcc565b60405180910390fd5b61020281610830565b61023f5760405162461bcd60e51b815260206004820152600e60248201526d6e6f7420726566756e6461626c6560901b60448201526064016101f0565b60008181526020818152604080832060058101805460ff60a01b1916600360a01b17905560018101548251336024820152604480820192909252835180820390920182526064018352928301805163a9059cbb60e01b6001600160e01b0390911617905290519092916060916001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016916102df91610d45565b6000604051808303816000865af19150503d806000811461031c576040519150601f19603f3d011682016040523d82523d6000602084013e610321565b606091505b50909250905081801561034c57508051158061034c57508080602001905181019061034c9190610cad565b61038a5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101f0565b50505050565b6103cd6040805160e081018252600080825260208201819052918101829052606081018290526080810182905260a081018290529060c082015290565b60008281526020818152604091829020825160e08101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b039081166080840152600584015490811660a084015291929160c0840191600160a01b90910460ff169081111561045157610451610ea4565b600381111561046257610462610ea4565b90525092915050565b32331461048a5760405162461bcd60e51b81526004016101f090610dcc565b6000805b8281101561062f57368484838181106104a9576104a9610eba565b9050608002019050600080600083602001358152602001908152602001600020905060008260600135116105075760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016101f0565b81356105495760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016101f0565b60006005820154600160a01b900460ff16600381111561056b5761056b610ea4565b146105aa5760405162461bcd60e51b815260206004820152600f60248201526e0c8eae040e6cac6e4cae840d0c2e6d608b1b60448201526064016101f0565b436002820155813560038201556004810180546001600160a01b031916331790556105db6060830160408401610bac565b6005820180546060850135600185018190556001600160a01b03939093166001600160a81b031990911617600160a01b1790556106189085610e5b565b93505050808061062790610e73565b91505061048e565b5060408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016916106af91610d45565b6000604051808303816000865af19150503d80600081146106ec576040519150601f19603f3d011682016040523d82523d6000602084013e6106f1565b606091505b50909250905081801561071c57508051158061071c57508080602001905181019061071c9190610cad565b61075f5760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b60448201526064016101f0565b5050505050565b600082815260208190526040812060016005820154600160a01b900460ff16600381111561079657610796610ea4565b1480156107af575060058101546001600160a01b031633145b80156108285750836002846040516020016107cc91815260200190565b60408051601f19818403018152908290526107e691610d45565b602060405180830381855afa158015610803573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906108269190610ce8565b145b949350505050565b600081815260208190526040812060016005820154600160a01b900460ff16600381111561086057610860610ea4565b148015610879575060048101546001600160a01b031633145b8015610889575080600301544210155b9392505050565b3233146108af5760405162461bcd60e51b81526004016101f090610dcc565b6000805b82811015610a8757368484838181106108ce576108ce610eba565b6020604091820293909301838101356000908152938490529220919250600190506005820154600160a01b900460ff16600381111561090f5761090f610ea4565b146109485760405162461bcd60e51b815260206004820152600960248201526862616420737461746560b81b60448201526064016101f0565b60058101546001600160a01b031633146109965760405162461bcd60e51b815260206004820152600f60248201526e189859081c185c9d1a58da5c185b9d608a1b60448201526064016101f0565b8160200135600283600001356040516020016109b491815260200190565b60408051601f19818403018152908290526109ce91610d45565b602060405180830381855afa1580156109eb573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610a0e9190610ce8565b14610a485760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b60448201526064016101f0565b60058101805460ff60a01b1916600160a11b179055813581556001810154610a709085610e5b565b935050508080610a7f90610e73565b9150506108b3565b5060408051336024820152604480820184905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001691610b0191610d45565b6000604051808303816000865af19150503d8060008114610b3e576040519150601f19603f3d011682016040523d82523d6000602084013e610b43565b606091505b509092509050818015610b6e575080511580610b6e575080806020019051810190610b6e9190610cad565b61075f5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101f0565b600060208284031215610bbe57600080fd5b81356001600160a01b038116811461088957600080fd5b60008060208385031215610be857600080fd5b823567ffffffffffffffff80821115610c0057600080fd5b818501915085601f830112610c1457600080fd5b813581811115610c2357600080fd5b8660208260071b8501011115610c3857600080fd5b60209290920196919550909350505050565b60008060208385031215610c5d57600080fd5b823567ffffffffffffffff80821115610c7557600080fd5b818501915085601f830112610c8957600080fd5b813581811115610c9857600080fd5b8660208260061b8501011115610c3857600080fd5b600060208284031215610cbf57600080fd5b8151801515811461088957600080fd5b600060208284031215610ce157600080fd5b5035919050565b600060208284031215610cfa57600080fd5b5051919050565b60008060408385031215610d1457600080fd5b50508035926020909101359150565b60048110610d4157634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b81811015610d665760208186018101518583015201610d4c565b81811115610d75576000828501525b509190910192915050565b8781526020810187905260408101869052606081018590526001600160a01b038481166080830152831660a082015260e08101610dc060c0830184610d23565b98975050505050505050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b600060e08201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a0850152505060c0830151610e5460c0840182610d23565b5092915050565b60008219821115610e6e57610e6e610e8e565b500190565b6000600019821415610e8757610e87610e8e565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea26469706673582212206ae7148a3a8b7e07ccfdea07ba89617856321dce1ea2656e5a569664d1f7cb2264736f6c63430008060033",
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
func DeployERC20Swap(auth *bind.TransactOpts, backend bind.ContractBackend, token common.Address) (common.Address, *types.Transaction, *ERC20Swap, error) {
	parsed, err := ERC20SwapMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ERC20SwapBin), backend, token)
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

// TOKENADDRESS is a free data retrieval call binding the contract method 0x0bdf5300.
//
// Solidity: function TOKEN_ADDRESS() view returns(address)
func (_ERC20Swap *ERC20SwapCaller) TOKENADDRESS(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "TOKEN_ADDRESS")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// TOKENADDRESS is a free data retrieval call binding the contract method 0x0bdf5300.
//
// Solidity: function TOKEN_ADDRESS() view returns(address)
func (_ERC20Swap *ERC20SwapSession) TOKENADDRESS() (common.Address, error) {
	return _ERC20Swap.Contract.TOKENADDRESS(&_ERC20Swap.CallOpts)
}

// TOKENADDRESS is a free data retrieval call binding the contract method 0x0bdf5300.
//
// Solidity: function TOKEN_ADDRESS() view returns(address)
func (_ERC20Swap *ERC20SwapCallerSession) TOKENADDRESS() (common.Address, error) {
	return _ERC20Swap.Contract.TOKENADDRESS(&_ERC20Swap.CallOpts)
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
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ERC20Swap *ERC20SwapCaller) Swap(opts *bind.CallOpts, secretHash [32]byte) (ethv0.ETHSwapSwap, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "swap", secretHash)

	if err != nil {
		return *new(ethv0.ETHSwapSwap), err
	}

	out0 := *abi.ConvertType(out[0], new(ethv0.ETHSwapSwap)).(*ethv0.ETHSwapSwap)

	return out0, err

}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ERC20Swap *ERC20SwapSession) Swap(secretHash [32]byte) (ethv0.ETHSwapSwap, error) {
	return _ERC20Swap.Contract.Swap(&_ERC20Swap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((bytes32,uint256,uint256,uint256,address,address,uint8))
func (_ERC20Swap *ERC20SwapCallerSession) Swap(secretHash [32]byte) (ethv0.ETHSwapSwap, error) {
	return _ERC20Swap.Contract.Swap(&_ERC20Swap.CallOpts, secretHash)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, uint8 state)
func (_ERC20Swap *ERC20SwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "swaps", arg0)

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
func (_ERC20Swap *ERC20SwapSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, uint8 state)
func (_ERC20Swap *ERC20SwapCallerSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	State                uint8
}, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) returns()
func (_ERC20Swap *ERC20SwapTransactor) Initiate(opts *bind.TransactOpts, initiations []ethv0.ETHSwapInitiation) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "initiate", initiations)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) returns()
func (_ERC20Swap *ERC20SwapSession) Initiate(initiations []ethv0.ETHSwapInitiation) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, initiations)
}

// Initiate is a paid mutator transaction binding the contract method 0xa8793f94.
//
// Solidity: function initiate((uint256,bytes32,address,uint256)[] initiations) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Initiate(initiations []ethv0.ETHSwapInitiation) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, initiations)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ethv0.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapSession) Redeem(redemptions []ethv0.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf4fd17f9.
//
// Solidity: function redeem((bytes32,bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Redeem(redemptions []ethv0.ETHSwapRedemption) (*types.Transaction, error) {
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
