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
const ETHSwapABI = "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structETHSwap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"

// ETHSwapFuncSigs maps the 4-byte function signature to its string representation.
var ETHSwapFuncSigs = map[string]string{
	"ae052147": "initiate(uint256,bytes32,address)",
	"b31597ad": "redeem(bytes32,bytes32)",
	"7249fbb6": "refund(bytes32)",
	"76467cbd": "swap(bytes32)",
	"eb84e7f2": "swaps(bytes32)",
}

// ETHSwapBin is the compiled bytecode used for deploying new contracts.
var ETHSwapBin = "0x608060405234801561001057600080fd5b50610742806100206000396000f3fe60806040526004361061004a5760003560e01c80637249fbb61461004f57806376467cbd14610071578063ae052147146100a7578063b31597ad146100ba578063eb84e7f2146100da575b600080fd5b34801561005b57600080fd5b5061006f61006a36600461053b565b61015c565b005b34801561007d57600080fd5b5061009161008c36600461053b565b610243565b60405161009e9190610631565b60405180910390f35b61006f6100b536600461058f565b61032e565b3480156100c657600080fd5b5061006f6100d536600461056d565b6103f3565b3480156100e657600080fd5b506101486100f536600461053b565b6000602081905290815260409020805460018201546002830154600384015460048501546005860154600687015460079097015495969495939492936001600160a01b0392831693919092169160ff1688565b60405161009e9897969594939291906106a1565b32331461016857600080fd5b8033600160008381526020819052604090206007015460ff166003811115610192576101926106f6565b1461019c57600080fd5b6000828152602081905260409020600401546001600160a01b038281169116146101c557600080fd5b600082815260208190526040902060010154428111156101e457600080fd5b600084815260208190526040808220600601549051339282156108fc02929190818181858888f19350505050158015610221573d6000803e3d6000fd5b505050600091825250602081905260409020600701805460ff19166003179055565b6102886040805161010081018252600080825260208201819052918101829052606081018290526080810182905260a0810182905260c081018290529060e082015290565b6000828152602081815260409182902082516101008101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b03908116608084015260058401541660a0830152600683015460c0830152600783015491929160e084019160ff90911690811115610314576103146106f6565b6003811115610325576103256106f6565b90525092915050565b826000341161033c57600080fd5b6000811161034957600080fd5b32331461035557600080fd5b826000808281526020819052604090206007015460ff16600381111561037d5761037d6106f6565b1461038757600080fd5b6000848152602081905260409020438155600180820187905560028201869055600482018054336001600160a01b0319918216179091556005830180549091166001600160a01b0387161790553460068301556007909101805460ff1916828002179055505050505050565b3233146103ff57600080fd5b808233600160008481526020819052604090206007015460ff16600381111561042a5761042a6106f6565b1461043457600080fd5b6000838152602081905260409020600501546001600160a01b0382811691161461045d57600080fd5b8260028360405160200161047391815260200190565b60408051601f198184030181529082905261048d916105f6565b602060405180830381855afa1580156104aa573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906104cd9190610554565b146104d757600080fd5b600084815260208190526040808220600601549051339282156108fc02929190818181858888f19350505050158015610514573d6000803e3d6000fd5b50505060009182525060208190526040902060078101805460ff1916600217905560030155565b60006020828403121561054d57600080fd5b5035919050565b60006020828403121561056657600080fd5b5051919050565b6000806040838503121561058057600080fd5b50508035926020909101359150565b6000806000606084860312156105a457600080fd5b833592506020840135915060408401356001600160a01b03811681146105c957600080fd5b809150509250925092565b600481106105f257634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b8181101561061757602081860181015185830152016105fd565b81811115610626576000828501525b509190910192915050565b60006101008201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a0850152505060c083015160c083015260e083015161069a60e08401826105d4565b5092915050565b8881526020810188905260408101879052606081018690526001600160a01b038581166080830152841660a082015260c0810183905261010081016106e960e08301846105d4565b9998505050505050505050565b634e487b7160e01b600052602160045260246000fdfea2646970667358221220b555a7551bad7d0030b3e4c81ceaf6b3e8becbf457b8d5dfa3484a5eb822674864736f6c63430008060033"

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
