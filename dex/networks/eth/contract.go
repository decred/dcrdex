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
	Secret               [32]byte
	Initiator            common.Address
	Participant          common.Address
	Value                *big.Int
	State                uint8
}

// ETHSwapABI is the input ABI used to generate the binding from.
const ETHSwapABI = "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"}],\"name\":\"initiateBatch\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structETHSwap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"enumETHSwap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]"

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
var ETHSwapBin = "0x608060405234801561001057600080fd5b5061099a806100206000396000f3fe6080604052600436106100555760003560e01c806358ac45f51461005a5780637249fbb61461006f57806376467cbd1461008f578063ae052147146100c5578063b31597ad146100d8578063eb84e7f2146100f8575b600080fd5b61006d6100683660046106e3565b610172565b005b34801561007b57600080fd5b5061006d61008a366004610758565b61029e565b34801561009b57600080fd5b506100af6100aa366004610758565b6103a0565b6040516100bc919061083e565b60405180910390f35b61006d6100d33660046107ac565b610478565b3480156100e457600080fd5b5061006d6100f336600461078a565b610536565b34801561010457600080fd5b5061015f610113366004610758565b60006020819052908152604090208054600182015460028301546003840154600485015460058601546006909601549495939492936001600160a01b0392831693929091169160ff1687565b6040516100bc97969594939291906108a3565b32331461017e57600080fd5b6000805b8281101561028c573684848381811061019d5761019d61094e565b9050608002019050600080600083602001358152602001908152602001600020905060008260600135116101d057600080fd5b81356101db57600080fd5b6000600682015460ff1660038111156101f6576101f6610938565b1461020057600080fd5b438155813560018201556003810180546001600160a01b0319163317905561022e60608301604084016106c1565b6004820180546001600160a01b0319166001600160a01b039290921691909117905560608201356005820181905560068201805460ff1916600117905561027590856108ef565b93505050808061028490610907565b915050610182565b5034811461029957600080fd5b505050565b3233146102aa57600080fd5b80600160008281526020819052604090206006015460ff1660038111156102d3576102d3610938565b146102dd57600080fd5b6000818152602081905260409020600301546001600160a01b0316331461030357600080fd5b6000818152602081905260409020600101544281111561032257600080fd5b60008381526020819052604080822060068101805460ff191660031790556005015490513391908381818185875af1925050503d8060008114610381576040519150601f19603f3d011682016040523d82523d6000602084013e610386565b606091505b509091505060018115151461039a57600080fd5b50505050565b6103dd6040805160e081018252600080825260208201819052918101829052606081018290526080810182905260a081018290529060c082015290565b60008281526020818152604091829020825160e081018452815481526001820154928101929092526002810154928201929092526003808301546001600160a01b0390811660608401526004840154166080830152600583015460a0830152600683015491929160c084019160ff9091169081111561045e5761045e610938565b600381111561046f5761046f610938565b90525092915050565b826000341161048657600080fd5b6000811161049357600080fd5b32331461049f57600080fd5b826000808281526020819052604090206006015460ff1660038111156104c7576104c7610938565b146104d157600080fd5b60008481526020819052604090204381556001808201879055600382018054336001600160a01b0319918216179091556004830180549091166001600160a01b03871617905534600583015560068201805460ff191682800217905550505050505050565b32331461054257600080fd5b8082600160008381526020819052604090206006015460ff16600381111561056c5761056c610938565b1461057657600080fd5b6000828152602081905260409020600401546001600160a01b0316331461059c57600080fd5b816002826040516020016105b291815260200190565b60408051601f19818403018152908290526105cc91610803565b602060405180830381855afa1580156105e9573d6000803e3d6000fd5b5050506040513d601f19601f8201168201806040525081019061060c9190610771565b1461061657600080fd5b60008381526020819052604080822060068101805460ff191660021790556005015490513391908381818185875af1925050503d8060008114610675576040519150601f19603f3d011682016040523d82523d6000602084013e61067a565b606091505b509091505060018115151461068e57600080fd5b505050600090815260208190526040902060020155565b80356001600160a01b03811681146106bc57600080fd5b919050565b6000602082840312156106d357600080fd5b6106dc826106a5565b9392505050565b600080602083850312156106f657600080fd5b823567ffffffffffffffff8082111561070e57600080fd5b818501915085601f83011261072257600080fd5b81358181111561073157600080fd5b8660208260071b850101111561074657600080fd5b60209290920196919550909350505050565b60006020828403121561076a57600080fd5b5035919050565b60006020828403121561078357600080fd5b5051919050565b6000806040838503121561079d57600080fd5b50508035926020909101359150565b6000806000606084860312156107c157600080fd5b83359250602084013591506107d8604085016106a5565b90509250925092565b600481106107ff57634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b81811015610824576020818601810151858301520161080a565b81811115610833576000828501525b509190910192915050565b600060e082019050825182526020830151602083015260408301516040830152606083015160018060a01b038082166060850152806080860151166080850152505060a083015160a083015260c083015161089c60c08401826107e1565b5092915050565b87815260208101879052604081018690526001600160a01b0385811660608301528416608082015260a0810183905260e081016108e360c08301846107e1565b98975050505050505050565b6000821982111561090257610902610922565b500190565b600060001982141561091b5761091b610922565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea2646970667358221220adfd4cb419e368c3af5d1d3c6ec2039d2c5eb3b75584a75ee0226c403fb5a68164736f6c63430008060033"

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
