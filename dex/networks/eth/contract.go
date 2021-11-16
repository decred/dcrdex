// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package eth

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

// ETHSwapSwap is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapSwap struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structETHSwap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"a8793f94": "initiate((uint256,bytes32,address,uint256)[])",
		"b31597ad": "redeem(bytes32,bytes32)",
		"7249fbb6": "refund(bytes32)",
		"76467cbd": "swap(bytes32)",
		"eb84e7f2": "swaps(bytes32)",
	},
	Bin: "0x608060405234801561001057600080fd5b5061087b806100206000396000f3fe60806040526004361061004a5760003560e01c80637249fbb61461004f57806376467cbd14610071578063a8793f94146100a7578063b31597ad146100ba578063eb84e7f2146100da575b600080fd5b34801561005b57600080fd5b5061006f61006a36600461066e565b610154565b005b34801561007d57600080fd5b5061009161008c36600461066e565b610256565b60405161009e919061071f565b60405180910390f35b61006f6100b53660046105f9565b61032e565b3480156100c657600080fd5b5061006f6100d53660046106a0565b61045a565b3480156100e657600080fd5b506101416100f536600461066e565b60006020819052908152604090208054600182015460028301546003840154600485015460058601546006909601549495939492936001600160a01b0392831693929091169160ff1687565b60405161009e9796959493929190610784565b32331461016057600080fd5b80600160008281526020819052604090206006015460ff16600381111561018957610189610819565b1461019357600080fd5b6000818152602081905260409020600301546001600160a01b031633146101b957600080fd5b600081815260208190526040902060010154428111156101d857600080fd5b60008381526020819052604080822060068101805460ff191660031790556005015490513391908381818185875af1925050503d8060008114610237576040519150601f19603f3d011682016040523d82523d6000602084013e61023c565b606091505b509091505060018115151461025057600080fd5b50505050565b6102936040805160e081018252600080825260208201819052918101829052606081018290526080810182905260a081018290529060c082015290565b60008281526020818152604091829020825160e081018452815481526001820154928101929092526002810154928201929092526003808301546001600160a01b0390811660608401526004840154166080830152600583015460a0830152600683015491929160c084019160ff9091169081111561031457610314610819565b600381111561032557610325610819565b90525092915050565b32331461033a57600080fd5b6000805b8281101561044857368484838181106103595761035961082f565b90506080020190506000806000836020013581526020019081526020016000209050600082606001351161038c57600080fd5b813561039757600080fd5b6000600682015460ff1660038111156103b2576103b2610819565b146103bc57600080fd5b438155813560018201556003810180546001600160a01b031916331790556103ea60608301604084016105c9565b6004820180546001600160a01b0319166001600160a01b039290921691909117905560608201356005820181905560068201805460ff1916600117905561043190856107d0565b935050508080610440906107e8565b91505061033e565b5034811461045557600080fd5b505050565b32331461046657600080fd5b8082600160008381526020819052604090206006015460ff16600381111561049057610490610819565b1461049a57600080fd5b6000828152602081905260409020600401546001600160a01b031633146104c057600080fd5b816002826040516020016104d691815260200190565b60408051601f19818403018152908290526104f0916106e4565b602060405180830381855afa15801561050d573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906105309190610687565b1461053a57600080fd5b60008381526020819052604080822060068101805460ff191660021790556005015490513391908381818185875af1925050503d8060008114610599576040519150601f19603f3d011682016040523d82523d6000602084013e61059e565b606091505b50909150506001811515146105b257600080fd5b505050600090815260208190526040902060020155565b6000602082840312156105db57600080fd5b81356001600160a01b03811681146105f257600080fd5b9392505050565b6000806020838503121561060c57600080fd5b823567ffffffffffffffff8082111561062457600080fd5b818501915085601f83011261063857600080fd5b81358181111561064757600080fd5b8660208260071b850101111561065c57600080fd5b60209290920196919550909350505050565b60006020828403121561068057600080fd5b5035919050565b60006020828403121561069957600080fd5b5051919050565b600080604083850312156106b357600080fd5b50508035926020909101359150565b600481106106e057634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b8181101561070557602081860181015185830152016106eb565b81811115610714576000828501525b509190910192915050565b600060e082019050825182526020830151602083015260408301516040830152606083015160018060a01b038082166060850152806080860151166080850152505060a083015160a083015260c083015161077d60c08401826106c2565b5092915050565b87815260208101879052604081018690526001600160a01b0385811660608301528416608082015260a0810183905260e081016107c460c08301846106c2565b98975050505050505050565b600082198211156107e3576107e3610803565b500190565b60006000198214156107fc576107fc610803565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea2646970667358221220c1198903915a40464ee6e40ae955ebebaab320ead3535316f09c049d302591e364736f6c63430008060033",
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

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,address,address,uint256,uint8))
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
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,address,address,uint256,uint8))
func (_ETHSwap *ETHSwapSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,address,address,uint256,uint8))
func (_ETHSwap *ETHSwapCallerSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "swaps", arg0)

	outstruct := new(struct {
		InitBlockNumber      *big.Int
		RefundBlockTimestamp *big.Int
		Secret               [32]byte
		Initiator            common.Address
		Participant          common.Address
		Value                *big.Int
		State                uint8
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.InitBlockNumber = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.RefundBlockTimestamp = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Secret = *abi.ConvertType(out[2], new([32]byte)).(*[32]byte)
	outstruct.Initiator = *abi.ConvertType(out[3], new(common.Address)).(*common.Address)
	outstruct.Participant = *abi.ConvertType(out[4], new(common.Address)).(*common.Address)
	outstruct.Value = *abi.ConvertType(out[5], new(*big.Int)).(**big.Int)
	outstruct.State = *abi.ConvertType(out[6], new(uint8)).(*uint8)

	return *outstruct, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapSession) Swaps(arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapCallerSession) Swaps(arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
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

// Redeem is a paid mutator transaction binding the contract method 0xb31597ad.
//
// Solidity: function redeem(bytes32 secret, bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapTransactor) Redeem(opts *bind.TransactOpts, secret [32]byte, secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeem", secret, secretHash)
}

// Redeem is a paid mutator transaction binding the contract method 0xb31597ad.
//
// Solidity: function redeem(bytes32 secret, bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapSession) Redeem(secret [32]byte, secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, secret, secretHash)
}

// Redeem is a paid mutator transaction binding the contract method 0xb31597ad.
//
// Solidity: function redeem(bytes32 secret, bytes32 secretHash) returns()
func (_ETHSwap *ETHSwapTransactorSession) Redeem(secret [32]byte, secretHash [32]byte) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, secret, secretHash)
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
