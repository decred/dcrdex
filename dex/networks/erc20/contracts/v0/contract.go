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
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"refundTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"internalType\":\"structERC20Swap.Initiation[]\",\"name\":\"initiations\",\"type\":\"tuple[]\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"isRefundable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"internalType\":\"structERC20Swap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"swap\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"internalType\":\"structERC20Swap.Swap\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"refundBlockTimestamp\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"enumERC20Swap.State\",\"name\":\"state\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Sigs: map[string]string{
		"6f70b30a": "initiate((uint256,bytes32,address,uint256)[],address)",
		"bfd2fd97": "isRedeemable(bytes32,bytes32)",
		"d0f761c0": "isRefundable(bytes32)",
		"f4fd17f9": "redeem((bytes32,bytes32)[])",
		"7249fbb6": "refund(bytes32)",
		"76467cbd": "swap(bytes32)",
		"eb84e7f2": "swaps(bytes32)",
	},
	Bin: "0x608060405234801561001057600080fd5b50610f4b806100206000396000f3fe608060405234801561001057600080fd5b506004361061007d5760003560e01c8063bfd2fd971161005b578063bfd2fd97146100d3578063d0f761c0146100f6578063eb84e7f214610109578063f4fd17f91461018157600080fd5b80636f70b30a146100825780637249fbb61461009757806376467cbd146100aa575b600080fd5b610095610090366004610be2565b610194565b005b6100956100a5366004610cfd565b610494565b6100bd6100b8366004610cfd565b61062a565b6040516100ca9190610e2e565b60405180910390f35b6100e66100e1366004610d2f565b61071a565b60405190151581526020016100ca565b6100e6610104366004610cfd565b6107e4565b61016d610117366004610cfd565b600060208190529081526040902080546001820154600283015460038401546004850154600586015460069096015494959394929391926001600160a01b0391821692821691811690600160a01b900460ff1688565b6040516100ca989796959493929190610dae565b61009561018f366004610c66565b610844565b3233146101bc5760405162461bcd60e51b81526004016101b390610e04565b60405180910390fd5b6000805b8381101561037c57368585838181106101db576101db610eff565b9050608002019050600080600083602001358152602001908152602001600020905060008260600135116102395760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b60448201526064016101b3565b813561027b5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b60448201526064016101b3565b60006006820154600160a01b900460ff16600381111561029d5761029d610ee9565b146102dc5760405162461bcd60e51b815260206004820152600f60248201526e0c8eae040e6cac6e4cae840d0c2e6d608b1b60448201526064016101b3565b436002820155813560038201556004810180546001600160a01b0319163317905561030d6060830160408401610bc7565b6005820180546001600160a01b039283166001600160a01b03199091161790556060830135600183018190556006830180549288166001600160a81b031990931692909217600160a01b179091556103659085610ea0565b93505050808061037490610eb8565b9150506101c0565b5060408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b038616916103dc91610d73565b6000604051808303816000865af19150503d8060008114610419576040519150601f19603f3d011682016040523d82523d6000602084013e61041e565b606091505b5090925090508180156104495750805115806104495750808060200190518101906104499190610cdb565b61048c5760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b60448201526064016101b3565b505050505050565b3233146104b35760405162461bcd60e51b81526004016101b390610e04565b6104bc816107e4565b6104f95760405162461bcd60e51b815260206004820152600e60248201526d6e6f7420726566756e6461626c6560901b60448201526064016101b3565b600081815260208181526040808320600681018054600360a01b60ff60a01b198216179091556001820154835133602482015260448082019290925284518082039092018252606401845293840180516001600160e01b031663a9059cbb60e01b17905291519093926060926001600160a01b0316916105799190610d73565b6000604051808303816000865af19150503d80600081146105b6576040519150601f19603f3d011682016040523d82523d6000602084013e6105bb565b606091505b5090925090508180156105e65750805115806105e65750808060200190518101906105e69190610cdb565b6106245760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101b3565b50505050565b61066f6040805161010081018252600080825260208201819052918101829052606081018290526080810182905260a0810182905260c081018290529060e082015290565b6000828152602081815260409182902082516101008101845281548152600182015492810192909252600281015492820192909252600380830154606083015260048301546001600160a01b0390811660808401526005840154811660a0840152600684015490811660c084015291929160e0840191600160a01b90910460ff169081111561070057610700610ee9565b600381111561071157610711610ee9565b90525092915050565b600082815260208190526040812060016006820154600160a01b900460ff16600381111561074a5761074a610ee9565b148015610763575060058101546001600160a01b031633145b80156107dc57508360028460405160200161078091815260200190565b60408051601f198184030181529082905261079a91610d73565b602060405180830381855afa1580156107b7573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906107da9190610d16565b145b949350505050565b600081815260208190526040812060016006820154600160a01b900460ff16600381111561081457610814610ee9565b14801561082d575060048101546001600160a01b031633145b801561083d575080600301544210155b9392505050565b3233146108635760405162461bcd60e51b81526004016101b390610e04565b600080805b83811015610aa6573685858381811061088357610883610eff565b602060409182029390930183810135600090815293849052922091925050826108bb5760068101546001600160a01b0316935061090c565b60068101546001600160a01b0385811691161461090c5760405162461bcd60e51b815260206004820152600f60248201526e6d756c7469706c6520746f6b656e7360881b60448201526064016101b3565b60016006820154600160a01b900460ff16600381111561092e5761092e610ee9565b146109675760405162461bcd60e51b815260206004820152600960248201526862616420737461746560b81b60448201526064016101b3565b60058101546001600160a01b031633146109b55760405162461bcd60e51b815260206004820152600f60248201526e189859081c185c9d1a58da5c185b9d608a1b60448201526064016101b3565b8160200135600283600001356040516020016109d391815260200190565b60408051601f19818403018152908290526109ed91610d73565b602060405180830381855afa158015610a0a573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610a2d9190610d16565b14610a675760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b60448201526064016101b3565b60068101805460ff60a01b1916600160a11b179055813581556001810154610a8f9086610ea0565b945050508080610a9e90610eb8565b915050610868565b5060408051336024820152604480820185905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b03851691610b0091610d73565b6000604051808303816000865af19150503d8060008114610b3d576040519150601f19603f3d011682016040523d82523d6000602084013e610b42565b606091505b509092509050818015610b6d575080511580610b6d575080806020019051810190610b6d9190610cdb565b61048c5760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b60448201526064016101b3565b80356001600160a01b0381168114610bc257600080fd5b919050565b600060208284031215610bd957600080fd5b61083d82610bab565b600080600060408486031215610bf757600080fd5b833567ffffffffffffffff80821115610c0f57600080fd5b818601915086601f830112610c2357600080fd5b813581811115610c3257600080fd5b8760208260071b8501011115610c4757600080fd5b602092830195509350610c5d9186019050610bab565b90509250925092565b60008060208385031215610c7957600080fd5b823567ffffffffffffffff80821115610c9157600080fd5b818501915085601f830112610ca557600080fd5b813581811115610cb457600080fd5b8660208260061b8501011115610cc957600080fd5b60209290920196919550909350505050565b600060208284031215610ced57600080fd5b8151801515811461083d57600080fd5b600060208284031215610d0f57600080fd5b5035919050565b600060208284031215610d2857600080fd5b5051919050565b60008060408385031215610d4257600080fd5b50508035926020909101359150565b60048110610d6f57634e487b7160e01b600052602160045260246000fd5b9052565b6000825160005b81811015610d945760208186018101518583015201610d7a565b81811115610da3576000828501525b509190910192915050565b8881526020810188905260408101879052606081018690526001600160a01b03858116608083015284811660a0830152831660c08201526101008101610df760e0830184610d51565b9998505050505050505050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b60006101008201905082518252602083015160208301526040830151604083015260608301516060830152608083015160018060a01b0380821660808501528060a08601511660a08501528060c08601511660c0850152505060e0830151610e9960e0840182610d51565b5092915050565b60008219821115610eb357610eb3610ed3565b500190565b6000600019821415610ecc57610ecc610ed3565b5060010190565b634e487b7160e01b600052601160045260246000fd5b634e487b7160e01b600052602160045260246000fd5b634e487b7160e01b600052603260045260246000fdfea26469706673582212209d4ab136dc2aae36b4471acffd461990bcbb232bd433687b91328de48b13bd7664736f6c63430008060033",
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

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, address token, uint8 state)
func (_ERC20Swap *ERC20SwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	Token                common.Address
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
		Token                common.Address
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
	outstruct.Token = *abi.ConvertType(out[6], new(common.Address)).(*common.Address)
	outstruct.State = *abi.ConvertType(out[7], new(uint8)).(*uint8)

	return *outstruct, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, address token, uint8 state)
func (_ERC20Swap *ERC20SwapSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	Token                common.Address
	State                uint8
}, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32 secret, uint256 value, uint256 initBlockNumber, uint256 refundBlockTimestamp, address initiator, address participant, address token, uint8 state)
func (_ERC20Swap *ERC20SwapCallerSession) Swaps(arg0 [32]byte) (struct {
	Secret               [32]byte
	Value                *big.Int
	InitBlockNumber      *big.Int
	RefundBlockTimestamp *big.Int
	Initiator            common.Address
	Participant          common.Address
	Token                common.Address
	State                uint8
}, error) {
	return _ERC20Swap.Contract.Swaps(&_ERC20Swap.CallOpts, arg0)
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
