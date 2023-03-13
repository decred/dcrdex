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

// ETHBondBondInfo is an auto generated low-level Go binding around an user-defined struct.
type ETHBondBondInfo struct {
	BondID          [32]byte
	Value           *big.Int
	InitBlockNumber *big.Int
	Locktime        uint64
	Owner           common.Address
}

// ETHBondMetaData contains all meta data concerning the ETHBond contract.
var ETHBondMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"bondID\",\"type\":\"bytes32\"}],\"name\":\"bondData\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"bondID\",\"type\":\"bytes32\"}],\"name\":\"bondRefundable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"}],\"name\":\"bondsForAccount\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"bondID\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"initBlockNumber\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"internalType\":\"structETHBond.BondInfo[]\",\"name\":\"\",\"type\":\"tuple[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"bondID\",\"type\":\"bytes32\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"name\":\"createBond\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"bondID\",\"type\":\"bytes32\"}],\"name\":\"refundBond\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[]\",\"name\":\"bondsToUpdate\",\"type\":\"bytes32[]\"},{\"internalType\":\"bytes32[]\",\"name\":\"newBondIDs\",\"type\":\"bytes32[]\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"}],\"name\":\"updateBonds\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"accountID\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[]\",\"name\":\"bondsToUpdate\",\"type\":\"bytes32[]\"},{\"internalType\":\"bytes32[]\",\"name\":\"newBondIDs\",\"type\":\"bytes32[]\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"locktime\",\"type\":\"uint64\"},{\"internalType\":\"uint256\",\"name\":\"msgValue\",\"type\":\"uint256\"}],\"name\":\"verifyBondUpdate\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b5061122b806100206000396000f3fe6080604052600436106100705760003560e01c8063c9db44541161004e578063c9db4454146100ec578063dacebc9c146100ff578063de2ec9231461019e578063ef6bb2d3146101cd57600080fd5b80632d27ff401461007557806374e3a3c6146100aa578063c0f5fa70146100bf575b600080fd5b34801561008157600080fd5b50610095610090366004610dc5565b6101ed565b60405190151581526020015b60405180910390f35b6100bd6100b8366004610e4e565b610231565b005b3480156100cb57600080fd5b506100df6100da366004610ee0565b6103ef565b6040516100a19190610ef9565b6100bd6100fa366004610f79565b610559565b34801561010b57600080fd5b5061016661011a366004610dc5565b6000918252602082815260408084209284526001928301909152909120805491810154600290910154919290916001600160401b03811691600160401b9091046001600160a01b031690565b6040516100a1949392919093845260208401929092526001600160401b031660408301526001600160a01b0316606082015260800190565b3480156101aa57600080fd5b506101be6101b9366004610fc4565b610630565b6040516100a19392919061111c565b3480156101d957600080fd5b506100bd6101e8366004610dc5565b61090b565b60008281526020818152604080832084845260018101909252822080541580159061022657506002810154426001600160401b03909116105b925050505b92915050565b3233146102595760405162461bcd60e51b815260040161025090611146565b60405180910390fd5b60008060006102a28a8a8a808060200260200160405190810160405280939291908181526020018383602002808284376000920191909152508c92508b91508a90508934610630565b9250925092508281906102c85760405162461bcd60e51b81526004016102509190611170565b5060008a81526020819052604090208583111561037d5760008160010160008c8c60008181106102fa576102fa61118a565b9050602002013581526020019081526020016000209050610352828a8a60008181106103285761032861118a565b600286015460209091029290920135916001600160401b0316905061034d8b896111b6565b610a5a565b610377828a8a60018181106103695761036961118a565b90506020020135888a610a5a565b506103a2565b6103a281898960008181106103945761039461118a565b905060200201358789610a5a565b60005b898110156103e1576103cf828c8c848181106103c3576103c361118a565b90506020020135610ad1565b806103d9816111c9565b9150506103a5565b505050505050505050505050565b60008181526020819052604081208054606092905b80156104335781610414816111c9565b6000928352600185016020526040909220600401549192506104049050565b6000826001600160401b0381111561044d5761044d610fae565b6040519080825280602002602001820160405280156104a657816020015b6040805160a08101825260008082526020808301829052928201819052606082018190526080820152825260001990920191018161046b5790505b5084549250905060005b8381101561054f57600083815260018681016020908152604092839020835160a081018552878152815492810192909252918201549281019290925260028101546001600160401b0381166060840152600160401b90046001600160a01b03166080830152835190919084908490811061052c5761052c61118a565b602090810291909101015260040154925080610547816111c9565b9150506104b0565b5095945050505050565b3233146105785760405162461bcd60e51b815260040161025090611146565b600083815260208181526040808320858452600181019092529091208054156105d95760405162461bcd60e51b8152602060048201526013602482015272626f6e6420616c72656164792065786973747360681b6044820152606401610250565b6000341161061d5760405162461bcd60e51b8152602060048201526011602482015270076616c7565206d757374206265203e203607c1b6044820152606401610250565b61062982858534610a5a565b5050505050565b60008781526020819052604081208190606090600101828080610654848e8b610b7d565b9250925092508261067a57600096506001600160401b038916955093506108fe92505050565b898211156107a95760008460008f60008151811061069a5761069a61118a565b6020026020010151815260200190815260200160002090508a836106be91906111b6565b8154101561070f576000836040518060400160405280601a81526020017f6561726c6965737420626f6e6420697320746f6f20736d616c6c00000000000081525097509750975050505050506108fe565b60028c14610760576000836040518060400160405280601d81526020017f696e636f7272656374206e756d626572206f66206e657720626f6e647300000081525097509750975050505050506108fe565b88156107a357600083604051806040016040528060118152602001701b9bc8199d5b991cc81c995c5d5a5c9959607a1b81525097509750975050505050506108fe565b50610846565b60018b146107f9576000826040518060400160405280601d81526020017f696e636f7272656374206e756d626572206f66206e657720626f6e6473000000815250965096509650505050506108fe565b89610804838a6111e2565b146108465760008260405180604001604052806012815260200171696e73756666696369656e742066756e647360701b815250965096509650505050506108fe565b60005b8b8110156108e1578460008e8e848181106108665761086661118a565b905060200201358152602001908152602001600020600001546000146108cf576000836040518060400160405280601781526020017f6e657720626f6e6420616c72656164792065786973747300000000000000000081525097509750975050505050506108fe565b806108d9816111c9565b915050610849565b505060408051602081019091526000815260019650909450925050505b9750975097945050505050565b32331461092a5760405162461bcd60e51b815260040161025090611146565b61093482826101ed565b6109795760405162461bcd60e51b8152602060048201526016602482015275626f6e64206973206e6f7420726566756e6461626c6560501b6044820152606401610250565b6000828152602081815260408083208484526001810190925290912080546002820154600160401b90046001600160a01b03166109b68486610ad1565b6000816001600160a01b03168360405160006040518083038185875af1925050503d8060008114610a03576040519150601f19603f3d011682016040523d82523d6000602084013e610a08565b606091505b5090915050600181151514610a515760405162461bcd60e51b815260206004820152600f60248201526e1d1c985b9cd9995c8819985a5b1959608a1b6044820152606401610250565b50505050505050565b6000838152600180860160205260409091208281554391810191909155600281018054600160401b33026001600160e01b03199091166001600160401b038616171790558454610aac57838555610629565b8454600081815260018701602052604090206004929092015560030183905550509055565b60008181526001830160205260409020600381015415610b0f5760048082015460038301546000908152600186016020526040902090910155610b17565b600481015483555b600481015415610b4157600380820154600483015460009081526001860160205260409020909101555b5060009081526001918201602052604081208181559182018190556002820180546001600160e01b031916905560038201819055600490910155565b600080606060008060005b8751811015610d01576000888281518110610ba557610ba561118a565b6020908102919091018101516000818152918c90526040822080549193509103610c085760008060405180604001604052806013815260200172189bdb9908191bd95cc81b9bdd08195e1a5cdd606a1b8152509750975097505050505050610dbc565b6002810154600160401b90046001600160a01b03163314610c6c576000806040518060400160405280601881526020017f626f6e64206f776e657220213d206d73672e73656e64657200000000000000008152509750975097505050505050610dbc565b60028101546001600160401b0380861691161015610ccd576000806040518060400160405280602081526020017f626f6e6473206d75737420626520736f72746564206279206c6f636b74696d658152509750975097505050505050610dbc565b8054610cd990866111e2565b6002909101549094506001600160401b03169250819050610cf9816111c9565b915050610b88565b5060008860008960018b51610d1691906111b6565b81518110610d2657610d2661118a565b602090810291909101810151825281019190915260400160002060028101549091506001600160401b03808916911610610da1576000806040518060400160405280601881526020017f6c6f636b74696d652063616e6e6f742064656372656173650000000000000000815250955095509550505050610dbc565b50506040805160208101909152600081526001945090925090505b93509350939050565b60008060408385031215610dd857600080fd5b50508035926020909101359150565b60008083601f840112610df957600080fd5b5081356001600160401b03811115610e1057600080fd5b6020830191508360208260051b8501011115610e2b57600080fd5b9250929050565b80356001600160401b0381168114610e4957600080fd5b919050565b600080600080600080600060a0888a031215610e6957600080fd5b8735965060208801356001600160401b0380821115610e8757600080fd5b610e938b838c01610de7565b909850965060408a0135915080821115610eac57600080fd5b50610eb98a828b01610de7565b90955093505060608801359150610ed260808901610e32565b905092959891949750929550565b600060208284031215610ef257600080fd5b5035919050565b602080825282518282018190526000919060409081850190868401855b82811015610f6c57815180518552868101518786015285810151868601526060808201516001600160401b0316908601526080908101516001600160a01b03169085015260a09093019290850190600101610f16565b5091979650505050505050565b600080600060608486031215610f8e57600080fd5b8335925060208401359150610fa560408501610e32565b90509250925092565b634e487b7160e01b600052604160045260246000fd5b600080600080600080600060c0888a031215610fdf57600080fd5b873596506020808901356001600160401b0380821115610ffe57600080fd5b818b0191508b601f83011261101257600080fd5b81358181111561102457611024610fae565b8060051b604051601f19603f8301168101818110858211171561104957611049610fae565b6040529182528381018501918581018f84111561106557600080fd5b948601945b83861015611081578535815294860194860161106a565b509a50505060408b013592508083111561109a57600080fd5b50506110a88a828b01610de7565b909650945050606088013592506110c160808901610e32565b915060a0880135905092959891949750929550565b6000815180845260005b818110156110fc576020818501810151868301820152016110e0565b506000602082860101526020601f19601f83011685010191505092915050565b831515815282602082015260606040820152600061113d60608301846110d6565b95945050505050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b60208152600061118360208301846110d6565b9392505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b8181038181111561022b5761022b6111a0565b6000600182016111db576111db6111a0565b5060010190565b8082018082111561022b5761022b6111a056fea26469706673582212209c57193479fbab98e7760f243c3e4b80a224d78b77e183459698ea178e47cef364736f6c63430008120033",
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

// BondData is a free data retrieval call binding the contract method 0xdacebc9c.
//
// Solidity: function bondData(bytes32 accountID, bytes32 bondID) view returns(uint256 value, uint256 initBlockNumber, uint64 locktime, address owner)
func (_ETHBond *ETHBondCaller) BondData(opts *bind.CallOpts, accountID [32]byte, bondID [32]byte) (struct {
	Value           *big.Int
	InitBlockNumber *big.Int
	Locktime        uint64
	Owner           common.Address
}, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "bondData", accountID, bondID)

	outstruct := new(struct {
		Value           *big.Int
		InitBlockNumber *big.Int
		Locktime        uint64
		Owner           common.Address
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Value = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.InitBlockNumber = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Locktime = *abi.ConvertType(out[2], new(uint64)).(*uint64)
	outstruct.Owner = *abi.ConvertType(out[3], new(common.Address)).(*common.Address)

	return *outstruct, err

}

// BondData is a free data retrieval call binding the contract method 0xdacebc9c.
//
// Solidity: function bondData(bytes32 accountID, bytes32 bondID) view returns(uint256 value, uint256 initBlockNumber, uint64 locktime, address owner)
func (_ETHBond *ETHBondSession) BondData(accountID [32]byte, bondID [32]byte) (struct {
	Value           *big.Int
	InitBlockNumber *big.Int
	Locktime        uint64
	Owner           common.Address
}, error) {
	return _ETHBond.Contract.BondData(&_ETHBond.CallOpts, accountID, bondID)
}

// BondData is a free data retrieval call binding the contract method 0xdacebc9c.
//
// Solidity: function bondData(bytes32 accountID, bytes32 bondID) view returns(uint256 value, uint256 initBlockNumber, uint64 locktime, address owner)
func (_ETHBond *ETHBondCallerSession) BondData(accountID [32]byte, bondID [32]byte) (struct {
	Value           *big.Int
	InitBlockNumber *big.Int
	Locktime        uint64
	Owner           common.Address
}, error) {
	return _ETHBond.Contract.BondData(&_ETHBond.CallOpts, accountID, bondID)
}

// BondRefundable is a free data retrieval call binding the contract method 0x2d27ff40.
//
// Solidity: function bondRefundable(bytes32 accountID, bytes32 bondID) view returns(bool)
func (_ETHBond *ETHBondCaller) BondRefundable(opts *bind.CallOpts, accountID [32]byte, bondID [32]byte) (bool, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "bondRefundable", accountID, bondID)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// BondRefundable is a free data retrieval call binding the contract method 0x2d27ff40.
//
// Solidity: function bondRefundable(bytes32 accountID, bytes32 bondID) view returns(bool)
func (_ETHBond *ETHBondSession) BondRefundable(accountID [32]byte, bondID [32]byte) (bool, error) {
	return _ETHBond.Contract.BondRefundable(&_ETHBond.CallOpts, accountID, bondID)
}

// BondRefundable is a free data retrieval call binding the contract method 0x2d27ff40.
//
// Solidity: function bondRefundable(bytes32 accountID, bytes32 bondID) view returns(bool)
func (_ETHBond *ETHBondCallerSession) BondRefundable(accountID [32]byte, bondID [32]byte) (bool, error) {
	return _ETHBond.Contract.BondRefundable(&_ETHBond.CallOpts, accountID, bondID)
}

// BondsForAccount is a free data retrieval call binding the contract method 0xc0f5fa70.
//
// Solidity: function bondsForAccount(bytes32 accountID) view returns((bytes32,uint256,uint256,uint64,address)[])
func (_ETHBond *ETHBondCaller) BondsForAccount(opts *bind.CallOpts, accountID [32]byte) ([]ETHBondBondInfo, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "bondsForAccount", accountID)

	if err != nil {
		return *new([]ETHBondBondInfo), err
	}

	out0 := *abi.ConvertType(out[0], new([]ETHBondBondInfo)).(*[]ETHBondBondInfo)

	return out0, err

}

// BondsForAccount is a free data retrieval call binding the contract method 0xc0f5fa70.
//
// Solidity: function bondsForAccount(bytes32 accountID) view returns((bytes32,uint256,uint256,uint64,address)[])
func (_ETHBond *ETHBondSession) BondsForAccount(accountID [32]byte) ([]ETHBondBondInfo, error) {
	return _ETHBond.Contract.BondsForAccount(&_ETHBond.CallOpts, accountID)
}

// BondsForAccount is a free data retrieval call binding the contract method 0xc0f5fa70.
//
// Solidity: function bondsForAccount(bytes32 accountID) view returns((bytes32,uint256,uint256,uint64,address)[])
func (_ETHBond *ETHBondCallerSession) BondsForAccount(accountID [32]byte) ([]ETHBondBondInfo, error) {
	return _ETHBond.Contract.BondsForAccount(&_ETHBond.CallOpts, accountID)
}

// VerifyBondUpdate is a free data retrieval call binding the contract method 0xde2ec923.
//
// Solidity: function verifyBondUpdate(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime, uint256 msgValue) view returns(bool, uint256, string)
func (_ETHBond *ETHBondCaller) VerifyBondUpdate(opts *bind.CallOpts, accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64, msgValue *big.Int) (bool, *big.Int, string, error) {
	var out []interface{}
	err := _ETHBond.contract.Call(opts, &out, "verifyBondUpdate", accountID, bondsToUpdate, newBondIDs, value, locktime, msgValue)

	if err != nil {
		return *new(bool), *new(*big.Int), *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)
	out1 := *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	out2 := *abi.ConvertType(out[2], new(string)).(*string)

	return out0, out1, out2, err

}

// VerifyBondUpdate is a free data retrieval call binding the contract method 0xde2ec923.
//
// Solidity: function verifyBondUpdate(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime, uint256 msgValue) view returns(bool, uint256, string)
func (_ETHBond *ETHBondSession) VerifyBondUpdate(accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64, msgValue *big.Int) (bool, *big.Int, string, error) {
	return _ETHBond.Contract.VerifyBondUpdate(&_ETHBond.CallOpts, accountID, bondsToUpdate, newBondIDs, value, locktime, msgValue)
}

// VerifyBondUpdate is a free data retrieval call binding the contract method 0xde2ec923.
//
// Solidity: function verifyBondUpdate(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime, uint256 msgValue) view returns(bool, uint256, string)
func (_ETHBond *ETHBondCallerSession) VerifyBondUpdate(accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64, msgValue *big.Int) (bool, *big.Int, string, error) {
	return _ETHBond.Contract.VerifyBondUpdate(&_ETHBond.CallOpts, accountID, bondsToUpdate, newBondIDs, value, locktime, msgValue)
}

// CreateBond is a paid mutator transaction binding the contract method 0xc9db4454.
//
// Solidity: function createBond(bytes32 accountID, bytes32 bondID, uint64 locktime) payable returns()
func (_ETHBond *ETHBondTransactor) CreateBond(opts *bind.TransactOpts, accountID [32]byte, bondID [32]byte, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.contract.Transact(opts, "createBond", accountID, bondID, locktime)
}

// CreateBond is a paid mutator transaction binding the contract method 0xc9db4454.
//
// Solidity: function createBond(bytes32 accountID, bytes32 bondID, uint64 locktime) payable returns()
func (_ETHBond *ETHBondSession) CreateBond(accountID [32]byte, bondID [32]byte, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.Contract.CreateBond(&_ETHBond.TransactOpts, accountID, bondID, locktime)
}

// CreateBond is a paid mutator transaction binding the contract method 0xc9db4454.
//
// Solidity: function createBond(bytes32 accountID, bytes32 bondID, uint64 locktime) payable returns()
func (_ETHBond *ETHBondTransactorSession) CreateBond(accountID [32]byte, bondID [32]byte, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.Contract.CreateBond(&_ETHBond.TransactOpts, accountID, bondID, locktime)
}

// RefundBond is a paid mutator transaction binding the contract method 0xef6bb2d3.
//
// Solidity: function refundBond(bytes32 accountID, bytes32 bondID) returns()
func (_ETHBond *ETHBondTransactor) RefundBond(opts *bind.TransactOpts, accountID [32]byte, bondID [32]byte) (*types.Transaction, error) {
	return _ETHBond.contract.Transact(opts, "refundBond", accountID, bondID)
}

// RefundBond is a paid mutator transaction binding the contract method 0xef6bb2d3.
//
// Solidity: function refundBond(bytes32 accountID, bytes32 bondID) returns()
func (_ETHBond *ETHBondSession) RefundBond(accountID [32]byte, bondID [32]byte) (*types.Transaction, error) {
	return _ETHBond.Contract.RefundBond(&_ETHBond.TransactOpts, accountID, bondID)
}

// RefundBond is a paid mutator transaction binding the contract method 0xef6bb2d3.
//
// Solidity: function refundBond(bytes32 accountID, bytes32 bondID) returns()
func (_ETHBond *ETHBondTransactorSession) RefundBond(accountID [32]byte, bondID [32]byte) (*types.Transaction, error) {
	return _ETHBond.Contract.RefundBond(&_ETHBond.TransactOpts, accountID, bondID)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0x74e3a3c6.
//
// Solidity: function updateBonds(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime) payable returns()
func (_ETHBond *ETHBondTransactor) UpdateBonds(opts *bind.TransactOpts, accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.contract.Transact(opts, "updateBonds", accountID, bondsToUpdate, newBondIDs, value, locktime)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0x74e3a3c6.
//
// Solidity: function updateBonds(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime) payable returns()
func (_ETHBond *ETHBondSession) UpdateBonds(accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.Contract.UpdateBonds(&_ETHBond.TransactOpts, accountID, bondsToUpdate, newBondIDs, value, locktime)
}

// UpdateBonds is a paid mutator transaction binding the contract method 0x74e3a3c6.
//
// Solidity: function updateBonds(bytes32 accountID, bytes32[] bondsToUpdate, bytes32[] newBondIDs, uint256 value, uint64 locktime) payable returns()
func (_ETHBond *ETHBondTransactorSession) UpdateBonds(accountID [32]byte, bondsToUpdate [][32]byte, newBondIDs [][32]byte, value *big.Int, locktime uint64) (*types.Transaction, error) {
	return _ETHBond.Contract.UpdateBonds(&_ETHBond.TransactOpts, accountID, bondsToUpdate, newBondIDs, value, locktime)
}
