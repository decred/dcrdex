// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package eth

import (
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
	SecretHash           [32]byte
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}

// ETHSwapABI is the input ABI used to generate the binding from.
const ETHSwapABI = "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"}],\"name\":\"initiateBatch\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structETHSwap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"

// ETHSwapFuncSigs maps the 4-byte function signature to its string representation.
var ETHSwapFuncSigs = map[string]string{
	"ae052147": "initiate(uint256,bytes32,address)",
	"58ac45f5": "initiateBatch((uint256,bytes32,address,uint256)[])",
	"b31597ad": "redeem(bytes32,bytes32)",
	"7249fbb6": "refund(bytes32)",
	"76467cbd": "swap(bytes32)",
	"eb84e7f2": "swaps(bytes32)",
}

// ETHSwapBin is the compiled bytecode used for deploying new contracts.
var ETHSwapBin = "0x608060405234801561001057600080fd5b50610bfe806100206000396000f3fe6080604052600436106100555760003560e01c806358ac45f51461005a5780637249fbb61461006f57806376467cbd1461008f578063ae052147146100c5578063b31597ad146100d8578063eb84e7f2146100f8575b600080fd5b61006d610068366004610933565b61017a565b005b34801561007b57600080fd5b5061006d61008a3660046109a8565b6104d4565b34801561009b57600080fd5b506100af6100aa3660046109a8565b6105d6565b6040516100bc9190610a8e565b60405180910390f35b61006d6100d33660046109fc565b6106c1565b3480156100e457600080fd5b5061006d6100f33660046109da565b610786565b34801561010457600080fd5b506101666101133660046109a8565b6000602081905290815260409020805460018201546002830154600384015460048501546005860154600687015460079097015495969495939492936001600160a01b0392831693919092169160ff1688565b6040516100bc989796959493929190610afe565b32331461018657600080fd5b6000805b828110156104c25760008484838181106101a6576101a6610bb2565b90506080020160600135116101ba57600080fd5b60008484838181106101ce576101ce610bb2565b90506080020160000135116101e257600080fd5b600080808686858181106101f8576101f8610bb2565b60206080909102929092018201358352508101919091526040016000206007015460ff16600381111561022d5761022d610b9c565b1461023757600080fd5b83838281811061024957610249610bb2565b905060800201606001358261025e9190610b53565b91504360008086868581811061027657610276610bb2565b905060800201602001358152602001908152602001600020600001819055508383828181106102a7576102a7610bb2565b905060800201600001356000808686858181106102c6576102c6610bb2565b905060800201602001358152602001908152602001600020600101819055508383828181106102f7576102f7610bb2565b9050608002016020013560008086868581811061031657610316610bb2565b905060800201602001358152602001908152602001600020600201819055503360008086868581811061034b5761034b610bb2565b90506080020160200135815260200190815260200160002060040160006101000a8154816001600160a01b0302191690836001600160a01b0316021790555083838281811061039c5761039c610bb2565b90506080020160400160208101906103b49190610911565b6000808686858181106103c9576103c9610bb2565b90506080020160200135815260200190815260200160002060050160006101000a8154816001600160a01b0302191690836001600160a01b0316021790555083838281811061041a5761041a610bb2565b9050608002016060013560008086868581811061043957610439610bb2565b90506080020160200135815260200190815260200160002060060181905550600160008086868581811061046f5761046f610bb2565b90506080020160200135815260200190815260200160002060070160006101000a81548160ff021916908360038111156104ab576104ab610b9c565b0217905550806104ba81610b6b565b91505061018a565b503481146104cf57600080fd5b505050565b3233146104e057600080fd5b80600160008281526020819052604090206007015460ff16600381111561050957610509610b9c565b1461051357600080fd5b6000818152602081905260409020600401546001600160a01b0316331461053957600080fd5b6000818152602081905260409020600101544281111561055857600080fd5b60008381526020819052604080822060078101805460ff191660031790556006015490513391908381818185875af1925050503d80600081146105b7576040519150601f19603f3d011682016040523d82523d6000602084013e6105bc565b606091505b50909150506001811515146105d057600080fd5b50505050565b61061b6040805161010081018252600080825260208201819052918101829052606081018290526080810182905260a0810182905260c081018290529060e082015290565b6000828152602081815260409182902082516101008101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b03908116608084015260058401541660a0830152600683015460c0830152600783015491929160e084019160ff909116908111156106a7576106a7610b9c565b60038111156106b8576106b8610b9c565b90525092915050565b82600034116106cf57600080fd5b600081116106dc57600080fd5b3233146106e857600080fd5b826000808281526020819052604090206007015460ff16600381111561071057610710610b9c565b1461071a57600080fd5b6000848152602081905260409020438155600180820187905560028201869055600482018054336001600160a01b0319918216179091556005830180549091166001600160a01b0387161790553460068301556007909101805460ff1916828002179055505050505050565b32331461079257600080fd5b8082600160008381526020819052604090206007015460ff1660038111156107bc576107bc610b9c565b146107c657600080fd5b6000828152602081905260409020600501546001600160a01b031633146107ec57600080fd5b8160028260405160200161080291815260200190565b60408051601f198184030181529082905261081c91610a53565b602060405180830381855afa158015610839573d6000803e3d6000fd5b5050506040513d601f19601f8201168201806040525081019061085c91906109c1565b1461086657600080fd5b60008381526020819052604080822060078101805460ff191660021790556006015490513391908381818185875af1925050503d80600081146108c5576040519150601f19603f3d011682016040523d82523d6000602084013e6108ca565b606091505b50909150506001811515146108de57600080fd5b505050600090815260208190526040902060030155565b80356001600160a01b038116811461090c57600080fd5b919050565b60006020828403121561092357600080fd5b61092c826108f5565b9392505050565b6000806020838503121561094657600080fd5b823567ffffffffffffffff8082111561095e57600080fd5b818501915085601f83011261097257600080fd5b81358181111561098157600080fd5b8660208260071b850101111561099657600080fd5b60209290920196919550909350505050565b6000602082840312156109ba57600080fd5b5035919050565b6000602082840312156109d357600080fd5b5051919050565b600080604083850312156109ed57600080fd5b50508035926020909101359150565b600080600060608486031215610a1157600080fd5b8335925060208401359150610a28604085016108f5565b90509250925092565b60048110610a4f57634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b81811015610a745760208186018101518583015201610a5a565b81811115610a83576000828501525b509190910192915050565b60006101008201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a0850152505060c083015160c083015260e0830151610af760e0840182610a31565b5092915050565b8881526020810188905260408101879052606081018690526001600160a01b038581166080830152841660a082015260c081018390526101008101610b4660e0830184610a31565b9998505050505050505050565b60008219821115610b6657610b66610b86565b500190565b6000600019821415610b7f57610b7f610b86565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea2646970667358221220a567f85339ee363efb9409cb1fca0a749430cbc4c39e6365b9f8164190f69dec64736f6c63430008060033"

// DeployETHSwap deploys a new Ethereum contract, binding an instance of ETHSwap to it.
func DeployETHSwap(auth *bind.TransactOpts, backend bind.ContractBackend) (common.Address, *types.Transaction, *ETHSwap, error) {
	parsed, err := abi.JSON(strings.NewReader(ETHSwapABI))
	if err != nil {
		return common.Address{}, nil, nil, err
	}

	address, tx, contract, err := bind.DeployContract(auth, parsed, common.FromHex(ETHSwapBin), backend)
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
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,bytes32,address,address,uint256,uint8))
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
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,bytes32,address,address,uint256,uint8))
func (_ETHSwap *ETHSwapSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swap is a free data retrieval call binding the contract method 0x76467cbd.
//
// Solidity: function swap(bytes32 secretHash) view returns((uint256,uint256,bytes32,bytes32,address,address,uint256,uint8))
func (_ETHSwap *ETHSwapCallerSession) Swap(secretHash [32]byte) (ETHSwapSwap, error) {
	return _ETHSwap.Contract.Swap(&_ETHSwap.CallOpts, secretHash)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secretHash, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	SecretHash           [32]byte
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
		SecretHash           [32]byte
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
	outstruct.SecretHash = *abi.ConvertType(out[2], new([32]byte)).(*[32]byte)
	outstruct.Secret = *abi.ConvertType(out[3], new([32]byte)).(*[32]byte)
	outstruct.Initiator = *abi.ConvertType(out[4], new(common.Address)).(*common.Address)
	outstruct.Participant = *abi.ConvertType(out[5], new(common.Address)).(*common.Address)
	outstruct.Value = *abi.ConvertType(out[6], new(*big.Int)).(**big.Int)
	outstruct.State = *abi.ConvertType(out[7], new(uint8)).(*uint8)

	return *outstruct, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secretHash, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapSession) Swaps(arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	SecretHash           [32]byte
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
// Solidity: function swaps(bytes32 ) view returns(uint256 initBlockNumber, uint256 refundBlockTimestamp, bytes32 secretHash, bytes32 secret, address initiator, address participant, uint256 value, uint8 state)
func (_ETHSwap *ETHSwapCallerSession) Swaps(arg0 [32]byte) (struct {
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	SecretHash           [32]byte
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", refundTimestamp, secretHash, participant)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, refundTimestamp, secretHash, participant)
}

// Initiate is a paid mutator transaction binding the contract method 0xae052147.
//
// Solidity: function initiate(uint256 refundTimestamp, bytes32 secretHash, address participant) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(refundTimestamp *big.Int, secretHash [32]byte, participant common.Address) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, refundTimestamp, secretHash, participant)
}

// InitiateBatch is a paid mutator transaction binding the contract method 0x58ac45f5.
//
// Solidity: function initiateBatch((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapTransactor) InitiateBatch(opts *bind.TransactOpts, initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiateBatch", initiations)
}

// InitiateBatch is a paid mutator transaction binding the contract method 0x58ac45f5.
//
// Solidity: function initiateBatch((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapSession) InitiateBatch(initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.Contract.InitiateBatch(&_ETHSwap.TransactOpts, initiations)
}

// InitiateBatch is a paid mutator transaction binding the contract method 0x58ac45f5.
//
// Solidity: function initiateBatch((uint256,bytes32,address,uint256)[] initiations) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) InitiateBatch(initiations []ETHSwapInitiation) (*types.Transaction, error) {
	return _ETHSwap.Contract.InitiateBatch(&_ETHSwap.TransactOpts, initiations)
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
