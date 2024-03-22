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
	_ = abi.ConvertType
)

// ETHBondBond is an auto generated low-level Go binding around an user-defined struct.
type ETHBondBond struct {
	Value    *big.Int
	Locktime uint64
}

// ETHBondMetaData contains all meta data concerning the ETHBond contract.
var ETHBondMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"acctID\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"indexed\":false,\"internalType\":\"structETHBond.Bond\",\"name\":\"bond\",\"type\":\"tuple\"}],\"name\":\"BondCreated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"acctID\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"indexed\":false,\"internalType\":\"structETHBond.Bond[]\",\"name\":\"oldBonds\",\"type\":\"tuple[]\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"indexed\":false,\"internalType\":\"structETHBond.Bond[]\",\"name\":\"newBonds\",\"type\":\"tuple[]\"}],\"name\":\"BondUpdated\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"bondCommits\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"acctID\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"internalType\":\"structETHBond.Bond\",\"name\":\"bond\",\"type\":\"tuple\"}],\"name\":\"createBond\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"internalType\":\"structETHBond.Bond[]\",\"name\":\"bonds\",\"type\":\"tuple[]\"},{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"hashBonds\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"acctID\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"internalType\":\"structETHBond.Bond[]\",\"name\":\"oldBonds\",\"type\":\"tuple[]\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"internalType\":\"structETHBond.Bond[]\",\"name\":\"newBonds\",\"type\":\"tuple[]\"},{\"internalType\":\"uint256\",\"name\":\"index\",\"type\":\"uint256\"}],\"name\":\"updateBonds\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50610f5b806100206000396000f3fe60806040526004361061003f5760003560e01c8063145bc8c01461004457806344d961cc1461007657806388cefcf91461008b578063c34db360146100ab575b600080fd5b34801561005057600080fd5b5061006461005f366004610b8b565b6100be565b60405190815260200160405180910390f35b610089610084366004610c4d565b6100f2565b005b34801561009757600080fd5b506100646100a6366004610c85565b610274565b6100896100b9366004610cf2565b6102a5565b600081836040516020016100d3929190610d73565b6040516020818303038152906040528051906020012090505b92915050565b3233146101395760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b60448201526064015b60405180910390fd5b3481351461017e5760405162461bcd60e51b8152602060048201526012602482015271696e73756666696369656e742066756e647360701b6044820152606401610130565b604080516001808252818301909252600091816020015b60408051808201909152600080825260208201528152602001906001900390816101955790505090506101cd36839003830183610ddc565b816000815181106101e0576101e0610dff565b602002602001018190525060008084815260200190815260200160002061020782336100be565b81546001818101845560009384526020808520909201929092558583528290526040909120546102379190610e2b565b837fccf60a1e2943f56dc23571a8ec5ebf81752de8f7f9a362f0a87ed78d06ce9c60846040516102679190610e60565b60405180910390a3505050565b6000602052816000526040600020818154811061029057600080fd5b90600052602060002001600091509150505481565b3233146102e75760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b6044820152606401610130565b80867f775bfb8f27aeb5ee1199727eed7c9db5bd83dc1de5abd12c71fbedb9778798cf8787878760405161031e9493929190610eac565b60405180910390a3600086815260208190526040812080548390811061034657610346610dff565b6000918252602090912001549050846103925760405162461bcd60e51b815260206004820152600e60248201526d6e656564206f6c6420626f6e647360901b6044820152606401610130565b806103ef8787808060200260200160405190810160405280939291908181526020016000905b828210156103e4576103d560408302860136819003810190610ddc565b815260200190600101906103b8565b5050505050336100be565b1461042f5760405162461bcd60e51b815260206004820152601060248201526f1a5b98dbdc9c9958dd0818dbdb5b5a5d60821b6044820152606401610130565b60008390036105c5576000805b868110156104fe574288888381811061045757610457610dff565b905060400201602001602081019061046f9190610ede565b6001600160401b0316106104c55760405162461bcd60e51b815260206004820152601c60248201527f63616e6e6f7420726566756e6420756e6578706972656420626f6e64000000006044820152606401610130565b8787828181106104d7576104d7610dff565b6104ea9260409091020135905083610ef9565b9150806104f681610f0c565b91505061043c565b50600088815260208190526040812080548590811061051f5761051f610dff565b6000918252602082200191909155604051339083908381818185875af1925050503d806000811461056c576040519150601f19603f3d011682016040523d82523d6000602084013e610571565b606091505b50909150506001811515146105bd5760405162461bcd60e51b8152602060048201526012602482015271199d5b1b081c99599d5b990819985a5b195960721b6044820152606401610130565b505050610ab4565b6000848460008181106105da576105da610dff565b90506040020160200160208101906105f29190610ede565b90506000806000808a8a808060200260200160405190810160405280939291908181526020016000905b828210156106485761063960408302860136819003810190610ddc565b8152602001906001019061061c565b5050505050905060005b8881101561085b5760008a8a8381811061066e5761066e610dff565b9050604002018036038101906106849190610ddc565b9050866001600160401b031681602001516001600160401b031611156106df5760405162461bcd60e51b815260206004820152601060248201526f189bdb991cc81b9bdd081cdbdc9d195960821b6044820152606401610130565b602081015181519097506106f38187610ef9565b95505b835185101561083a5781602001516001600160401b031684868151811061071f5761071f610dff565b6020026020010151602001516001600160401b031611156107825760405162461bcd60e51b815260206004820152601c60248201527f696c6c6567616c206c6f636b2074696d652073686f7274656e696e67000000006044820152606401610130565b8084868151811061079557610795610dff565b602002602001015160000151106107dd57808486815181106107b9576107b9610dff565b60200260200101516000018181516107d19190610e2b565b9052506000905061083a565b8385815181106107ef576107ef610dff565b602002602001015160000151816108069190610e2b565b9050600084868151811061081c5761081c610dff565b6020908102919091010151528461083281610f0c565b9550506106f6565b6108448188610ef9565b96505050808061085390610f0c565b915050610652565b5083341461089e5760405162461bcd60e51b815260206004820152601060248201526f1a5b98dbdc9c9958dd08185b5bdd5b9d60821b6044820152606401610130565b6108ef8989808060200260200160405190810160405280939291908181526020016000905b828210156103e4576108e060408302860136819003810190610ddc565b815260200190600101906108c3565b60008d815260208190526040902080548990811061090f5761090f610dff565b906000526020600020018190555060005b8151831015610a1657600082848151811061093d5761093d610dff565b6020026020010151600001511115610a04574282848151811061096257610962610dff565b6020026020010151602001516001600160401b0316106109d85760405162461bcd60e51b815260206004820152602b60248201527f756e657874656e64656420626f6e64206973206e6f74206f6c6420656e6f756760448201526a1a081d1bc81c99599d5b9960aa1b6064820152608401610130565b8183815181106109ea576109ea610dff565b60200260200101516000015181610a019190610ef9565b90505b82610a0e81610f0c565b935050610920565b8015610aac57604051600090339083908381818185875af1925050503d8060008114610a5e576040519150601f19603f3d011682016040523d82523d6000602084013e610a63565b606091505b5090915050600181151514610aaa5760405162461bcd60e51b815260206004820152600d60248201526c1c99599d5b990819985a5b1959609a1b6044820152606401610130565b505b505050505050505b505050505050565b634e487b7160e01b600052604160045260246000fd5b604051601f8201601f191681016001600160401b0381118282101715610afa57610afa610abc565b604052919050565b80356001600160401b0381168114610b1957600080fd5b919050565b600060408284031215610b3057600080fd5b604051604081018181106001600160401b0382111715610b5257610b52610abc565b60405282358152905080610b6860208401610b02565b60208201525092915050565b80356001600160a01b0381168114610b1957600080fd5b6000806040808486031215610b9f57600080fd5b83356001600160401b0380821115610bb657600080fd5b818601915086601f830112610bca57600080fd5b8135602082821115610bde57610bde610abc565b610bec818360051b01610ad2565b828152818101935060069290921b840181019189831115610c0c57600080fd5b938101935b82851015610c3257610c238a86610b1e565b84529385019392810192610c11565b9650610c3f888201610b74565b955050505050509250929050565b6000808284036060811215610c6157600080fd5b833592506040601f1982011215610c7757600080fd5b506020830190509250929050565b60008060408385031215610c9857600080fd5b50508035926020909101359150565b60008083601f840112610cb957600080fd5b5081356001600160401b03811115610cd057600080fd5b6020830191508360208260061b8501011115610ceb57600080fd5b9250929050565b60008060008060008060808789031215610d0b57600080fd5b8635955060208701356001600160401b0380821115610d2957600080fd5b610d358a838b01610ca7565b90975095506040890135915080821115610d4e57600080fd5b50610d5b89828a01610ca7565b979a9699509497949695606090950135949350505050565b6001600160a01b038316815260406020808301829052835183830181905260009291858101916060860190855b81811015610dce578451805184528401516001600160401b0316848401529383019391850191600101610da0565b509098975050505050505050565b600060408284031215610dee57600080fd5b610df88383610b1e565b9392505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b818103818111156100ec576100ec610e15565b803582526001600160401b03610e5660208301610b02565b1660208301525050565b604081016100ec8284610e3e565b81835260208301925060008160005b84811015610ea257610e8f8683610e3e565b6040958601959190910190600101610e7d565b5093949350505050565b604081526000610ec0604083018688610e6e565b8281036020840152610ed3818587610e6e565b979650505050505050565b600060208284031215610ef057600080fd5b610df882610b02565b808201808211156100ec576100ec610e15565b600060018201610f1e57610f1e610e15565b506001019056fea2646970667358221220af04925865e9f8c11251aaba57556ae3659c84311c01a161f4474ba5d6f478d464736f6c63430008120033",
}

// ETHBondABI is the input ABI used to generate the binding from.
// Deprecated: Use ETHBondMetaData.ABI instead.
var ETHBondABI = ETHBondMetaData.ABI

// ETHBondBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ETHBondMetaData.Bin instead.
var ETHBondBin = ETHBondMetaData.Bin

// DeployETHBond deploys a new Ethereum contract, binding an instance of ETHBond to it.
func DeployETHBond(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ETHBond, error) {
	parsed, err := ETHBondMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ETHBondBin), backend)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ETHBond{ETHBondCaller: ETHBondCaller{contract: contract}, ETHBondTransactor: ETHBondTransactor{contract: contract}, ETHBondFilterer: ETHBondFilterer{contract: contract}}, nil
}

// ETHBond is an auto generated Go binding around an Ethereum contract.
type ETHBond struct {
	ETHBondCaller     // Read-only binding to the contract
	ETHBondTransactor // Write-only binding to the contract
	ETHBondFilterer   // Log filterer for contract events
}

// ETHBondCaller is an auto generated read-only Go binding around an Ethereum contract.
type ETHBondCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHBondTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ETHBondTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHBondFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ETHBondFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ETHBondSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ETHBondSession struct {
	Contract     *ETHBond          // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ETHBondCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ETHBondCallerSession struct {
	Contract *ETHBondCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts  // Call options to use throughout this session
}

// ETHBondTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ETHBondTransactorSession struct {
	Contract     *ETHBondTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// ETHBondRaw is an auto generated low-level Go binding around an Ethereum contract.
type ETHBondRaw struct {
	Contract *ETHBond // Generic contract binding to access the raw methods on
}

// ETHBondCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ETHBondCallerRaw struct {
	Contract *ETHBondCaller // Generic read-only contract binding to access the raw methods on
}

// ETHBondTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ETHBondTransactorRaw struct {
	Contract *ETHBondTransactor // Generic write-only contract binding to access the raw methods on
}

// NewETHBond creates a new instance of ETHBond, bound to a specific deployed contract.
func NewETHBond(address common.Address, backend bind.ContractBackend) (*ETHBond, error) {
	contract, err := bindETHBond(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ETHBond{ETHBondCaller: ETHBondCaller{contract: contract}, ETHBondTransactor: ETHBondTransactor{contract: contract}, ETHBondFilterer: ETHBondFilterer{contract: contract}}, nil
}

// NewETHBondCaller creates a new read-only instance of ETHBond, bound to a specific deployed contract.
func NewETHBondCaller(address common.Address, caller bind.ContractCaller) (*ETHBondCaller, error) {
	contract, err := bindETHBond(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ETHBondCaller{contract: contract}, nil
}

// NewETHBondTransactor creates a new write-only instance of ETHBond, bound to a specific deployed contract.
func NewETHBondTransactor(address common.Address, transactor bind.ContractTransactor) (*ETHBondTransactor, error) {
	contract, err := bindETHBond(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ETHBondTransactor{contract: contract}, nil
}

// NewETHBondFilterer creates a new log filterer instance of ETHBond, bound to a specific deployed contract.
func NewETHBondFilterer(address common.Address, filterer bind.ContractFilterer) (*ETHBondFilterer, error) {
	contract, err := bindETHBond(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ETHBondFilterer{contract: contract}, nil
}

// bindETHBond binds a generic wrapper to an already deployed contract.
func bindETHBond(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ETHBondMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ETHBond *ETHBondRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ETHBond.Contract.ETHBondCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ETHBond *ETHBondRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ETHBond.Contract.ETHBondTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ETHBond *ETHBondRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ETHBond.Contract.ETHBondTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ETHBond *ETHBondCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ETHBond.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ETHBond *ETHBondTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ETHBond.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ETHBond *ETHBondTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ETHBond.Contract.contract.Transact(opts, method, params...)
}

// BondCommits is a free data retrieval call binding the contract method 0x88cefcf9.
//
// Solidity: function bondCommits(bytes32 , uint256 ) view returns(bytes32)
func (_ETHBond *ETHBondCaller) BondCommits(opts *bind.CallOpts, arg0 [32]byte, arg1 *big.Int) ([32]byte, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "bondCommits", arg0, arg1)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// BondCommits is a free data retrieval call binding the contract method 0x88cefcf9.
//
// Solidity: function bondCommits(bytes32 , uint256 ) view returns(bytes32)
func (_ETHBond *ETHBondSession) BondCommits(arg0 [32]byte, arg1 *big.Int) ([32]byte, error) {
	return _ETHBond.Contract.BondCommits(&_ETHBond.CallOpts, arg0, arg1)
}

// BondCommits is a free data retrieval call binding the contract method 0x88cefcf9.
//
// Solidity: function bondCommits(bytes32 , uint256 ) view returns(bytes32)
func (_ETHBond *ETHBondCallerSession) BondCommits(arg0 [32]byte, arg1 *big.Int) ([32]byte, error) {
	return _ETHBond.Contract.BondCommits(&_ETHBond.CallOpts, arg0, arg1)
}

// HashBonds is a free data retrieval call binding the contract method 0x145bc8c0.
//
// Solidity: function hashBonds((uint256,uint64)[] bonds, address sender) pure returns(bytes32)
func (_ETHBond *ETHBondCaller) HashBonds(opts *bind.CallOpts, bonds []ETHBondBond, sender common.Address) ([32]byte, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "hashBonds", bonds, sender)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// HashBonds is a free data retrieval call binding the contract method 0x145bc8c0.
//
// Solidity: function hashBonds((uint256,uint64)[] bonds, address sender) pure returns(bytes32)
func (_ETHBond *ETHBondSession) HashBonds(bonds []ETHBondBond, sender common.Address) ([32]byte, error) {
	return _ETHBond.Contract.HashBonds(&_ETHBond.CallOpts, bonds, sender)
}

// HashBonds is a free data retrieval call binding the contract method 0x145bc8c0.
//
// Solidity: function hashBonds((uint256,uint64)[] bonds, address sender) pure returns(bytes32)
func (_ETHBond *ETHBondCallerSession) HashBonds(bonds []ETHBondBond, sender common.Address) ([32]byte, error) {
	return _ETHBond.Contract.HashBonds(&_ETHBond.CallOpts, bonds, sender)
}

// CreateBond is a paid mutator transaction binding the contract method 0x44d961cc.
//
// Solidity: function createBond(bytes32 acctID, (uint256,uint64) bond) payable returns()
func (_ETHBond *ETHBondTransactor) CreateBond(opts *bind.TransactOpts, acctID [32]byte, bond ETHBondBond) (*types.Transaction, error) {
	return _ETHBond.contract.Transact(opts, "createBond", acctID, bond)
}

// CreateBond is a paid mutator transaction binding the contract method 0x44d961cc.
//
// Solidity: function createBond(bytes32 acctID, (uint256,uint64) bond) payable returns()
func (_ETHBond *ETHBondSession) CreateBond(acctID [32]byte, bond ETHBondBond) (*types.Transaction, error) {
	return _ETHBond.Contract.CreateBond(&_ETHBond.TransactOpts, acctID, bond)
}

// CreateBond is a paid mutator transaction binding the contract method 0x44d961cc.
//
// Solidity: function createBond(bytes32 acctID, (uint256,uint64) bond) payable returns()
func (_ETHBond *ETHBondTransactorSession) CreateBond(acctID [32]byte, bond ETHBondBond) (*types.Transaction, error) {
	return _ETHBond.Contract.CreateBond(&_ETHBond.TransactOpts, acctID, bond)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0xc34db360.
//
// Solidity: function updateBonds(bytes32 acctID, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds, uint256 index) payable returns()
func (_ETHBond *ETHBondTransactor) UpdateBonds(opts *bind.TransactOpts, acctID [32]byte, oldBonds []ETHBondBond, newBonds []ETHBondBond, index *big.Int) (*types.Transaction, error) {
	return _ETHBond.contract.Transact(opts, "updateBonds", acctID, oldBonds, newBonds, index)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0xc34db360.
//
// Solidity: function updateBonds(bytes32 acctID, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds, uint256 index) payable returns()
func (_ETHBond *ETHBondSession) UpdateBonds(acctID [32]byte, oldBonds []ETHBondBond, newBonds []ETHBondBond, index *big.Int) (*types.Transaction, error) {
	return _ETHBond.Contract.UpdateBonds(&_ETHBond.TransactOpts, acctID, oldBonds, newBonds, index)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0xc34db360.
//
// Solidity: function updateBonds(bytes32 acctID, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds, uint256 index) payable returns()
func (_ETHBond *ETHBondTransactorSession) UpdateBonds(acctID [32]byte, oldBonds []ETHBondBond, newBonds []ETHBondBond, index *big.Int) (*types.Transaction, error) {
	return _ETHBond.Contract.UpdateBonds(&_ETHBond.TransactOpts, acctID, oldBonds, newBonds, index)
}

// ETHBondBondCreatedIterator is returned from FilterBondCreated and is used to iterate over the raw logs and unpacked data for BondCreated events raised by the ETHBond contract.
type ETHBondBondCreatedIterator struct {
	Event *ETHBondBondCreated // Event containing the contract specifics and raw log

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
func (it *ETHBondBondCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHBondBondCreated)
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
		it.Event = new(ETHBondBondCreated)
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
func (it *ETHBondBondCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHBondBondCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHBondBondCreated represents a BondCreated event raised by the ETHBond contract.
type ETHBondBondCreated struct {
	AcctID [32]byte
	Index  *big.Int
	Bond   ETHBondBond
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterBondCreated is a free log retrieval operation binding the contract event 0xccf60a1e2943f56dc23571a8ec5ebf81752de8f7f9a362f0a87ed78d06ce9c60.
//
// Solidity: event BondCreated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64) bond)
func (_ETHBond *ETHBondFilterer) FilterBondCreated(opts *bind.FilterOpts, acctID [][32]byte, index []*big.Int) (*ETHBondBondCreatedIterator, error) {

	var acctIDRule []interface{}
	for _, acctIDItem := range acctID {
		acctIDRule = append(acctIDRule, acctIDItem)
	}
	var indexRule []interface{}
	for _, indexItem := range index {
		indexRule = append(indexRule, indexItem)
	}

	logs, sub, err := _ETHBond.contract.FilterLogs(opts, "BondCreated", acctIDRule, indexRule)
	if err != nil {
		return nil, err
	}
	return &ETHBondBondCreatedIterator{contract: _ETHBond.contract, event: "BondCreated", logs: logs, sub: sub}, nil
}

// WatchBondCreated is a free log subscription operation binding the contract event 0xccf60a1e2943f56dc23571a8ec5ebf81752de8f7f9a362f0a87ed78d06ce9c60.
//
// Solidity: event BondCreated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64) bond)
func (_ETHBond *ETHBondFilterer) WatchBondCreated(opts *bind.WatchOpts, sink chan<- *ETHBondBondCreated, acctID [][32]byte, index []*big.Int) (event.Subscription, error) {

	var acctIDRule []interface{}
	for _, acctIDItem := range acctID {
		acctIDRule = append(acctIDRule, acctIDItem)
	}
	var indexRule []interface{}
	for _, indexItem := range index {
		indexRule = append(indexRule, indexItem)
	}

	logs, sub, err := _ETHBond.contract.WatchLogs(opts, "BondCreated", acctIDRule, indexRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHBondBondCreated)
				if err := _ETHBond.contract.UnpackLog(event, "BondCreated", log); err != nil {
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

// ParseBondCreated is a log parse operation binding the contract event 0xccf60a1e2943f56dc23571a8ec5ebf81752de8f7f9a362f0a87ed78d06ce9c60.
//
// Solidity: event BondCreated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64) bond)
func (_ETHBond *ETHBondFilterer) ParseBondCreated(log types.Log) (*ETHBondBondCreated, error) {
	event := new(ETHBondBondCreated)
	if err := _ETHBond.contract.UnpackLog(event, "BondCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ETHBondBondUpdatedIterator is returned from FilterBondUpdated and is used to iterate over the raw logs and unpacked data for BondUpdated events raised by the ETHBond contract.
type ETHBondBondUpdatedIterator struct {
	Event *ETHBondBondUpdated // Event containing the contract specifics and raw log

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
func (it *ETHBondBondUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHBondBondUpdated)
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
		it.Event = new(ETHBondBondUpdated)
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
func (it *ETHBondBondUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHBondBondUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHBondBondUpdated represents a BondUpdated event raised by the ETHBond contract.
type ETHBondBondUpdated struct {
	AcctID   [32]byte
	Index    *big.Int
	OldBonds []ETHBondBond
	NewBonds []ETHBondBond
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterBondUpdated is a free log retrieval operation binding the contract event 0x775bfb8f27aeb5ee1199727eed7c9db5bd83dc1de5abd12c71fbedb9778798cf.
//
// Solidity: event BondUpdated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds)
func (_ETHBond *ETHBondFilterer) FilterBondUpdated(opts *bind.FilterOpts, acctID [][32]byte, index []*big.Int) (*ETHBondBondUpdatedIterator, error) {

	var acctIDRule []interface{}
	for _, acctIDItem := range acctID {
		acctIDRule = append(acctIDRule, acctIDItem)
	}
	var indexRule []interface{}
	for _, indexItem := range index {
		indexRule = append(indexRule, indexItem)
	}

	logs, sub, err := _ETHBond.contract.FilterLogs(opts, "BondUpdated", acctIDRule, indexRule)
	if err != nil {
		return nil, err
	}
	return &ETHBondBondUpdatedIterator{contract: _ETHBond.contract, event: "BondUpdated", logs: logs, sub: sub}, nil
}

// WatchBondUpdated is a free log subscription operation binding the contract event 0x775bfb8f27aeb5ee1199727eed7c9db5bd83dc1de5abd12c71fbedb9778798cf.
//
// Solidity: event BondUpdated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds)
func (_ETHBond *ETHBondFilterer) WatchBondUpdated(opts *bind.WatchOpts, sink chan<- *ETHBondBondUpdated, acctID [][32]byte, index []*big.Int) (event.Subscription, error) {

	var acctIDRule []interface{}
	for _, acctIDItem := range acctID {
		acctIDRule = append(acctIDRule, acctIDItem)
	}
	var indexRule []interface{}
	for _, indexItem := range index {
		indexRule = append(indexRule, indexItem)
	}

	logs, sub, err := _ETHBond.contract.WatchLogs(opts, "BondUpdated", acctIDRule, indexRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHBondBondUpdated)
				if err := _ETHBond.contract.UnpackLog(event, "BondUpdated", log); err != nil {
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

// ParseBondUpdated is a log parse operation binding the contract event 0x775bfb8f27aeb5ee1199727eed7c9db5bd83dc1de5abd12c71fbedb9778798cf.
//
// Solidity: event BondUpdated(bytes32 indexed acctID, uint256 indexed index, (uint256,uint64)[] oldBonds, (uint256,uint64)[] newBonds)
func (_ETHBond *ETHBondFilterer) ParseBondUpdated(log types.Log) (*ETHBondBondUpdated, error) {
	event := new(ETHBondBondUpdated)
	if err := _ETHBond.contract.UnpackLog(event, "BondUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
