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
	ethv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
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
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structERC20Swap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structERC20Swap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumERC20Swap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structERC20Swap.Status\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"token_address\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x60a060405234801561001057600080fd5b5060405161110938038061110983398101604081905261002f91610040565b6001600160a01b0316608052610070565b60006020828403121561005257600080fd5b81516001600160a01b038116811461006957600080fd5b9392505050565b6080516110696100a06000396000818161010701528181610430015281816107a00152610a8001526110696000f3fe6080604052600436106100865760003560e01c80638cd8dd97116100595780638cd8dd9714610141578063a76f9f2d14610161578063d5cfd0491461018f578063db3b419c146101af578063eb84e7f2146101dc57600080fd5b806323f0388b1461008b5780633da59631146100ad57806377d7e031146100c05780638c8e8fee146100f5575b600080fd5b34801561009757600080fd5b506100ab6100a6366004610d69565b610209565b005b6100ab6100bb366004610dde565b61050c565b3480156100cc57600080fd5b506100e06100db366004610e41565b61087a565b60405190151581526020015b60405180910390f35b34801561010157600080fd5b506101297f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020016100ec565b34801561014d57600080fd5b506100ab61015c366004610e63565b6108f6565b34801561016d57600080fd5b5061018161017c366004610e63565b610b66565b6040519081526020016100ec565b34801561019b57600080fd5b506100e06101aa366004610e63565b610c4e565b3480156101bb57600080fd5b506101cf6101ca366004610e63565b610c82565b6040516100ec9190610e91565b3480156101e857600080fd5b506101816101f7366004610ed4565b60006020819052908152604090205481565b3233146102315760405162461bcd60e51b815260040161022890610eed565b60405180910390fd5b6000805b828110156103e0573684848381811061025057610250610f17565b60c00291909101915033905061026c60a0830160808401610f2d565b6001600160a01b0316146102af5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b6044820152606401610228565b600080806102bc84610d3d565b9250925092506000811180156102d157504381105b61030d5760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610228565b61031882853561087a565b156103585760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610228565b61036760a0850135853561087a565b6103a45760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610228565b60008381526020818152604090912060a086013590556103c79085013587610f73565b95505050505080806103d890610f86565b915050610235565b5060408051336024820152604480820184905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000169161045a91610f9f565b6000604051808303816000865af19150503d8060008114610497576040519150601f19603f3d011682016040523d82523d6000602084013e61049c565b606091505b5090925090508180156104c75750805115806104c75750808060200190518101906104c79190610fce565b6105055760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610228565b5050505050565b32331461052b5760405162461bcd60e51b815260040161022890610eed565b6000805b8281101561074a573684848381811061054a5761054a610f17565b905060a002019050600081602001351161058e5760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b6044820152606401610228565b60006105a06080830160608401610ff0565b67ffffffffffffffff16116105eb5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b6044820152606401610228565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf81350161066c5760405162461bcd60e51b815260206004820152602860248201527f696c6c6567616c2073656372657420686173682028726566756e64207265636f604482015267726420686173682960c01b6064820152608401610228565b600061067782610b66565b60008181526020819052604090205490915080156106c85760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b6044820152606401610228565b50436106d581843561087a565b156107135760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610228565b6000828152602081815260409091208290556107329084013586610f73565b9450505050808061074290610f86565b91505061052f565b5060408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016916107ca91610f9f565b6000604051808303816000865af19150503d8060008114610807576040519150601f19603f3d011682016040523d82523d6000602084013e61080c565b606091505b5090925090508180156108375750805115806108375750808060200190518101906108379190610fce565b6105055760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b6044820152606401610228565b60008160028460405160200161089291815260200190565b60408051601f19818403018152908290526108ac91610f9f565b602060405180830381855afa1580156108c9573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906108ec919061101a565b1490505b92915050565b3233146109155760405162461bcd60e51b815260040161022890610eed565b6109256080820160608301610ff0565b67ffffffffffffffff164210156109755760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b6044820152606401610228565b600080600061098384610d3d565b9250925092506000811180156109995750438111155b6109d75760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b6044820152606401610228565b6109e282853561087a565b15610a275760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b6044820152606401610228565b6000838152602081815260408083206000199055805133602482015287830135604480830191909152825180830390910181526064909101825291820180516001600160e01b031663a9059cbb60e01b179052516060917f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031691610ab39190610f9f565b6000604051808303816000865af19150503d8060008114610af0576040519150601f19603f3d011682016040523d82523d6000602084013e610af5565b606091505b509092509050818015610b20575080511580610b20575080806020019051810190610b209190610fce565b610b5e5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610228565b505050505050565b600060028235610b7c6060850160408601610f2d565b60601b610b8f60a0860160808701610f2d565b60601b856020013560001b866060016020810190610bad9190610ff0565b6040805160208101969096526bffffffffffffffffffffffff199485169086015292909116605484015260688301526001600160c01b031960c09190911b16608882015260900160408051601f1981840301815290829052610c0e91610f9f565b602060405180830381855afa158015610c2b573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906108f0919061101a565b6000806000610c5c84610d3d565b925092505080600014158015610c7a5750610c7882853561087a565b155b949350505050565b604080516060810182526000808252602082018190529181018290529080610ca984610d3d565b9250925050610cd36040805160608101909152806000815260006020820181905260409091015290565b81600003610cfa578060005b90816003811115610cf257610cf2610e7b565b905250610c7a565b60018301610d0a57806003610cdf565b610d1583863561087a565b15610d2a576002815260208101839052610c7a565b6001815260408101919091529392505050565b600080600080610d4c85610b66565b600081815260208190526040902054909690955085945092505050565b60008060208385031215610d7c57600080fd5b823567ffffffffffffffff80821115610d9457600080fd5b818501915085601f830112610da857600080fd5b813581811115610db757600080fd5b86602060c083028501011115610dcc57600080fd5b60209290920196919550909350505050565b60008060208385031215610df157600080fd5b823567ffffffffffffffff80821115610e0957600080fd5b818501915085601f830112610e1d57600080fd5b813581811115610e2c57600080fd5b86602060a083028501011115610dcc57600080fd5b60008060408385031215610e5457600080fd5b50508035926020909101359150565b600060a08284031215610e7557600080fd5b50919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110610eb657634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b600060208284031215610ee657600080fd5b5035919050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b634e487b7160e01b600052603260045260246000fd5b600060208284031215610f3f57600080fd5b81356001600160a01b0381168114610f5657600080fd5b9392505050565b634e487b7160e01b600052601160045260246000fd5b808201808211156108f0576108f0610f5d565b600060018201610f9857610f98610f5d565b5060010190565b6000825160005b81811015610fc05760208186018101518583015201610fa6565b506000920191825250919050565b600060208284031215610fe057600080fd5b81518015158114610f5657600080fd5b60006020828403121561100257600080fd5b813567ffffffffffffffff81168114610f5657600080fd5b60006020828403121561102c57600080fd5b505191905056fea2646970667358221220acfd007366a18ada193efab33a7139cee67cfd39d1c89075b5534cc57862baa464736f6c63430008120033",
}

// ERC20SwapABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20SwapMetaData.ABI instead.
var ERC20SwapABI = ERC20SwapMetaData.ABI

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

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapCaller) ContractKey(opts *bind.CallOpts, v ethv1.ETHSwapVector) ([32]byte, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "contractKey", v)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapSession) ContractKey(v ethv1.ETHSwapVector) ([32]byte, error) {
	return _ERC20Swap.Contract.ContractKey(&_ERC20Swap.CallOpts, v)
}

// ContractKey is a free data retrieval call binding the contract method 0xa76f9f2d.
//
// Solidity: function contractKey((bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapCallerSession) ContractKey(v ethv1.ETHSwapVector) ([32]byte, error) {
	return _ERC20Swap.Contract.ContractKey(&_ERC20Swap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ERC20Swap *ERC20SwapCaller) IsRedeemable(opts *bind.CallOpts, v ethv1.ETHSwapVector) (bool, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "isRedeemable", v)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ERC20Swap *ERC20SwapSession) IsRedeemable(v ethv1.ETHSwapVector) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0xd5cfd049.
//
// Solidity: function isRedeemable((bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ERC20Swap *ERC20SwapCallerSession) IsRedeemable(v ethv1.ETHSwapVector) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, v)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ERC20Swap *ERC20SwapCaller) SecretValidates(opts *bind.CallOpts, secret [32]byte, secretHash [32]byte) (bool, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "secretValidates", secret, secretHash)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ERC20Swap *ERC20SwapSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ERC20Swap.Contract.SecretValidates(&_ERC20Swap.CallOpts, secret, secretHash)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ERC20Swap *ERC20SwapCallerSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ERC20Swap.Contract.SecretValidates(&_ERC20Swap.CallOpts, secret, secretHash)
}

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapCaller) Status(opts *bind.CallOpts, v ethv1.ETHSwapVector) (ethv1.ETHSwapStatus, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "status", v)

	if err != nil {
		return *new(ethv1.ETHSwapStatus), err
	}

	out0 := *abi.ConvertType(out[0], new(ethv1.ETHSwapStatus)).(*ethv1.ETHSwapStatus)

	return out0, err

}

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapSession) Status(v ethv1.ETHSwapVector) (ethv1.ETHSwapStatus, error) {
	return _ERC20Swap.Contract.Status(&_ERC20Swap.CallOpts, v)
}

// Status is a free data retrieval call binding the contract method 0xdb3b419c.
//
// Solidity: function status((bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapCallerSession) Status(v ethv1.ETHSwapVector) (ethv1.ETHSwapStatus, error) {
	return _ERC20Swap.Contract.Status(&_ERC20Swap.CallOpts, v)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ERC20Swap *ERC20SwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "swaps", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ERC20Swap *ERC20SwapSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ERC20Swap *ERC20SwapCallerSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
}

// TokenAddress is a free data retrieval call binding the contract method 0x8c8e8fee.
//
// Solidity: function token_address() view returns(address)
func (_ERC20Swap *ERC20SwapCaller) TokenAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "token_address")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// TokenAddress is a free data retrieval call binding the contract method 0x8c8e8fee.
//
// Solidity: function token_address() view returns(address)
func (_ERC20Swap *ERC20SwapSession) TokenAddress() (common.Address, error) {
	return _ERC20Swap.Contract.TokenAddress(&_ERC20Swap.CallOpts)
}

// TokenAddress is a free data retrieval call binding the contract method 0x8c8e8fee.
//
// Solidity: function token_address() view returns(address)
func (_ERC20Swap *ERC20SwapCallerSession) TokenAddress() (common.Address, error) {
	return _ERC20Swap.Contract.TokenAddress(&_ERC20Swap.CallOpts)
}

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapTransactor) Initiate(opts *bind.TransactOpts, contracts []ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "initiate", contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapSession) Initiate(contracts []ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x3da59631.
//
// Solidity: function initiate((bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Initiate(contracts []ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, contracts)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapSession) Redeem(redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x23f0388b.
//
// Solidity: function redeem(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Redeem(redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ERC20Swap *ERC20SwapTransactor) Refund(opts *bind.TransactOpts, v ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "refund", v)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ERC20Swap *ERC20SwapSession) Refund(v ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, v)
}

// Refund is a paid mutator transaction binding the contract method 0x8cd8dd97.
//
// Solidity: function refund((bytes32,uint256,address,uint64,address) v) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Refund(v ethv1.ETHSwapVector) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, v)
}
