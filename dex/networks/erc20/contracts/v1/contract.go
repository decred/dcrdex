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
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract\",\"name\":\"c\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structERC20Swap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"value\",\"type\":\"uint64\"}],\"internalType\":\"structERC20Swap.Contract\",\"name\":\"c\",\"type\":\"tuple\"}],\"name\":\"state\",\"outputs\":[{\"components\":[{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structERC20Swap.Record\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"token_address\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x60a060405234801561001057600080fd5b506040516111d63803806111d683398101604081905261002f91610040565b6001600160a01b0316608052610070565b60006020828403121561005257600080fd5b81516001600160a01b038116811461006957600080fd5b9392505050565b6080516111366100a060003960008181610158015281816105130152818161083e0152610b6801526111366000f3fe6080604052600436106100865760003560e01c80637802689d116100595780637802689d146101265780638c8e8fee14610146578063d2544c0614610192578063eb84e7f2146101b2578063ed7cbed7146101ed57600080fd5b806338ec17681461008b578063428b16e1146100c157806364a97bff146100e357806377d7e031146100f6575b600080fd5b34801561009757600080fd5b506100ab6100a6366004610df5565b61020d565b6040516100b89190610e23565b60405180910390f35b3480156100cd57600080fd5b506100e16100dc366004610e66565b6102c9565b005b6100e16100f1366004610edb565b6105ef565b34801561010257600080fd5b50610116610111366004610f3e565b610918565b60405190151581526020016100b8565b34801561013257600080fd5b50610116610141366004610df5565b610992565b34801561015257600080fd5b5061017a7f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020016100b8565b34801561019e57600080fd5b506100e16101ad366004610df5565b6109d1565b3480156101be57600080fd5b506101df6101cd366004610f60565b60006020819052908152604090205481565b6040519081526020016100b8565b3480156101f957600080fd5b506101df610208366004610df5565b610cd0565b60408051606081018252600080825260208201819052918101829052908061023484610dc9565b925092505061025e6040805160608101909152806000815260006020820181905260409091015290565b81600003610285578060005b9081600381111561027d5761027d610e0d565b9052506102c1565b600183016102955780600361026a565b6102a0838635610918565b156102b55760028152602081018390526102c1565b60018152604081018290525b949350505050565b3233146102f15760405162461bcd60e51b81526004016102e890610f79565b60405180910390fd5b6000805b828110156104c3573684848381811061031057610310610fa3565b60c00291909101915033905061032c6080830160608401610fb9565b6001600160a01b03161461036f5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b60448201526064016102e8565b6000808061037c84610dc9565b92509250925060008111801561039157504381105b6103cd5760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b60448201526064016102e8565b6103d8828535610918565b156104185760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b60448201526064016102e8565b61042760a08501358535610918565b6104645760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b60448201526064016102e8565b600083815260208190526040902060a0850180359091556104889060808601610fe9565b61049690633b9aca00611029565b6104aa9067ffffffffffffffff1687611059565b95505050505080806104bb90611071565b9150506102f5565b5060408051336024820152604480820184905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000169161053d9161108a565b6000604051808303816000865af19150503d806000811461057a576040519150601f19603f3d011682016040523d82523d6000602084013e61057f565b606091505b5090925090508180156105aa5750805115806105aa5750808060200190518101906105aa91906110c5565b6105e85760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016102e8565b5050505050565b32331461060e5760405162461bcd60e51b81526004016102e890610f79565b6000805b828110156107e8573684848381811061062d5761062d610fa3565b905060a002019050600081608001602081019061064a9190610fe9565b67ffffffffffffffff16116106895760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016102e8565b600061069b6060830160408401610fe9565b67ffffffffffffffff16116106e65760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016102e8565b60006106f182610cd0565b60008181526020819052604090205490915080156107425760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b60448201526064016102e8565b504361074f818435610918565b1561078d5760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b60448201526064016102e8565b60008281526020819052604090208190556107ae60a0840160808501610fe9565b6107bc90633b9aca00611029565b6107d09067ffffffffffffffff1686611059565b945050505080806107e090611071565b915050610612565b5060408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016916108689161108a565b6000604051808303816000865af19150503d80600081146108a5576040519150601f19603f3d011682016040523d82523d6000602084013e6108aa565b606091505b5090925090508180156108d55750805115806108d55750808060200190518101906108d591906110c5565b6105e85760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b60448201526064016102e8565b60008160028460405160200161093091815260200190565b60408051601f198184030181529082905261094a9161108a565b602060405180830381855afa158015610967573d6000803e3d6000fd5b5050506040513d601f19601f8201168201806040525081019061098a91906110e7565b149392505050565b60008060006109a084610dc9565b9250925050806000141580156109b65750438111155b80156102c157506109c8828535610918565b15949350505050565b3233146109f05760405162461bcd60e51b81526004016102e890610f79565b610a006060820160408301610fe9565b67ffffffffffffffff16421015610a505760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b60448201526064016102e8565b6000806000610a5e84610dc9565b925092509250600081118015610a7357504381105b610ab15760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b60448201526064016102e8565b610abc828535610918565b15610b015760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b60448201526064016102e8565b60018201610b495760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c99599d5b991959605a1b60448201526064016102e8565b6000838152602081905260408120600019905560606001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000167fa9059cbb2ab09eb219583f4a59a5d0623ade346d962bcd4e46b11da047c9049b33610bba60a08a0160808b01610fe9565b6040516001600160a01b03909216602483015267ffffffffffffffff16604482015260640160408051601f198184030181529181526020820180516001600160e01b03166001600160e01b0319909416939093179092529051610c1d919061108a565b6000604051808303816000865af19150503d8060008114610c5a576040519150601f19603f3d011682016040523d82523d6000602084013e610c5f565b606091505b509092509050818015610c8a575080511580610c8a575080806020019051810190610c8a91906110c5565b610cc85760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016102e8565b505050505050565b600060028235610ce66040850160208601610fb9565b60601b846060016020810190610cfc9190610fb9565b60601b610d0f60a0870160808801610fe9565b60c01b610d226060880160408901610fe9565b6040805160208101969096526bffffffffffffffffffffffff19948516908601529190921660548401526001600160c01b0319918216606884015260c01b16607082015260780160408051601f1981840301815290829052610d839161108a565b602060405180830381855afa158015610da0573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610dc391906110e7565b92915050565b600080600080610dd885610cd0565b600081815260208190526040902054909690955085945092505050565b600060a08284031215610e0757600080fd5b50919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110610e4857634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060208385031215610e7957600080fd5b823567ffffffffffffffff80821115610e9157600080fd5b818501915085601f830112610ea557600080fd5b813581811115610eb457600080fd5b86602060c083028501011115610ec957600080fd5b60209290920196919550909350505050565b60008060208385031215610eee57600080fd5b823567ffffffffffffffff80821115610f0657600080fd5b818501915085601f830112610f1a57600080fd5b813581811115610f2957600080fd5b86602060a083028501011115610ec957600080fd5b60008060408385031215610f5157600080fd5b50508035926020909101359150565b600060208284031215610f7257600080fd5b5035919050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b634e487b7160e01b600052603260045260246000fd5b600060208284031215610fcb57600080fd5b81356001600160a01b0381168114610fe257600080fd5b9392505050565b600060208284031215610ffb57600080fd5b813567ffffffffffffffff81168114610fe257600080fd5b634e487b7160e01b600052601160045260246000fd5b600067ffffffffffffffff8083168185168183048111821515161561105057611050611013565b02949350505050565b6000821982111561106c5761106c611013565b500190565b60006001820161108357611083611013565b5060010190565b6000825160005b818110156110ab5760208186018101518583015201611091565b818111156110ba576000828501525b509190910192915050565b6000602082840312156110d757600080fd5b81518015158114610fe257600080fd5b6000602082840312156110f957600080fd5b505191905056fea26469706673582212206ba6041ec0475bc866767df59ff486a34daa28bc1b0370980daf420814f1da9e64736f6c634300080f0033",
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

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapCaller) ContractKey(opts *bind.CallOpts, c ethv1.ETHSwapContract) ([32]byte, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "contractKey", c)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapSession) ContractKey(c ethv1.ETHSwapContract) ([32]byte, error) {
	return _ERC20Swap.Contract.ContractKey(&_ERC20Swap.CallOpts, c)
}

// ContractKey is a free data retrieval call binding the contract method 0xed7cbed7.
//
// Solidity: function contractKey((bytes32,address,uint64,address,uint64) c) pure returns(bytes32)
func (_ERC20Swap *ERC20SwapCallerSession) ContractKey(c ethv1.ETHSwapContract) ([32]byte, error) {
	return _ERC20Swap.Contract.ContractKey(&_ERC20Swap.CallOpts, c)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ERC20Swap *ERC20SwapCaller) IsRedeemable(opts *bind.CallOpts, c ethv1.ETHSwapContract) (bool, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "isRedeemable", c)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ERC20Swap *ERC20SwapSession) IsRedeemable(c ethv1.ETHSwapContract) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, c)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x7802689d.
//
// Solidity: function isRedeemable((bytes32,address,uint64,address,uint64) c) view returns(bool)
func (_ERC20Swap *ERC20SwapCallerSession) IsRedeemable(c ethv1.ETHSwapContract) (bool, error) {
	return _ERC20Swap.Contract.IsRedeemable(&_ERC20Swap.CallOpts, c)
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

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapCaller) State(opts *bind.CallOpts, c ethv1.ETHSwapContract) (ethv1.ETHSwapRecord, error) {
	var out []interface{}
	err := _ERC20Swap.contract.Call(opts, &out, "state", c)

	if err != nil {
		return *new(ethv1.ETHSwapRecord), err
	}

	out0 := *abi.ConvertType(out[0], new(ethv1.ETHSwapRecord)).(*ethv1.ETHSwapRecord)

	return out0, err

}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapSession) State(c ethv1.ETHSwapContract) (ethv1.ETHSwapRecord, error) {
	return _ERC20Swap.Contract.State(&_ERC20Swap.CallOpts, c)
}

// State is a free data retrieval call binding the contract method 0x38ec1768.
//
// Solidity: function state((bytes32,address,uint64,address,uint64) c) view returns((uint8,bytes32,uint256))
func (_ERC20Swap *ERC20SwapCallerSession) State(c ethv1.ETHSwapContract) (ethv1.ETHSwapRecord, error) {
	return _ERC20Swap.Contract.State(&_ERC20Swap.CallOpts, c)
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

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapTransactor) Initiate(opts *bind.TransactOpts, contracts []ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "initiate", contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapSession) Initiate(contracts []ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x64a97bff.
//
// Solidity: function initiate((bytes32,address,uint64,address,uint64)[] contracts) payable returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Initiate(contracts []ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Initiate(&_ERC20Swap.TransactOpts, contracts)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactor) Redeem(opts *bind.TransactOpts, redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "redeem", redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapSession) Redeem(redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0x428b16e1.
//
// Solidity: function redeem(((bytes32,address,uint64,address,uint64),bytes32)[] redemptions) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Redeem(redemptions []ethv1.ETHSwapRedemption) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Redeem(&_ERC20Swap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ERC20Swap *ERC20SwapTransactor) Refund(opts *bind.TransactOpts, c ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.contract.Transact(opts, "refund", c)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ERC20Swap *ERC20SwapSession) Refund(c ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, c)
}

// Refund is a paid mutator transaction binding the contract method 0xd2544c06.
//
// Solidity: function refund((bytes32,address,uint64,address,uint64) c) returns()
func (_ERC20Swap *ERC20SwapTransactorSession) Refund(c ethv1.ETHSwapContract) (*types.Transaction, error) {
	return _ERC20Swap.Contract.Refund(&_ERC20Swap.TransactOpts, c)
}
