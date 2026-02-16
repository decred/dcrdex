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

// ETHSwapRedemption is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapRedemption struct {
	V      ETHSwapVector
	Secret [32]byte
}

// ETHSwapStatus is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapStatus struct {
	Step        uint8
	Secret      [32]byte
	BlockNumber *big.Int
}

// ETHSwapVector is an auto generated low-level Go binding around an user-defined struct.
type ETHSwapVector struct {
	SecretHash      [32]byte
	Value           *big.Int
	Initiator       common.Address
	RefundTimestamp uint64
	Participant     common.Address
}

// UserOperation is an auto generated low-level Go binding around an user-defined struct.
type UserOperation struct {
	Sender               common.Address
	Nonce                *big.Int
	InitCode             []byte
	CallData             []byte
	CallGasLimit         *big.Int
	VerificationGasLimit *big.Int
	PreVerificationGas   *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	PaymasterAndData     []byte
	Signature            []byte
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"_entryPoint\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Initiated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"RedeemAATransferFailed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"Redeemed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"}],\"name\":\"Refunded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"MAX_BATCH\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"entryPoint\",\"outputs\":[{\"internalType\":\"addresspayable\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"vectors\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeemAA\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"redeemPrepayments\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"s\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"missingAccountFunds\",\"type\":\"uint256\"}],\"name\":\"validateUserOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60a06040523480156200001157600080fd5b506040516200257738038062002577833981016040819052620000349162000099565b60016000556001600160a01b038116620000875760405162461bcd60e51b815260206004820152601060248201526f1e995c9bc8195b9d1c9e481c1bda5b9d60821b604482015260640160405180910390fd5b6001600160a01b0316608052620000cb565b600060208284031215620000ac57600080fd5b81516001600160a01b0381168114620000c457600080fd5b9392505050565b608051612482620000f5600039600081816101e6015281816103270152610d5d01526124826000f3fe6080604052600436106100c25760003560e01c8063950bff9f1161007f578063eb84e7f211610059578063eb84e7f214610220578063f0e3b8d51461024d578063f521b2eb1461027a578063f9f2e0f4146102a757600080fd5b8063950bff9f1461019f5780639ef07b4c146101b4578063b0d691fe146101d457600080fd5b806333a3bcb4146100c75780633a871cdd146100fc57806352145bc01461012a57806371b6d0111461013f57806377d7e0311461015f578063919835a41461017f575b600080fd5b3480156100d357600080fd5b506100e76100e2366004611c98565b6102c7565b60405190151581526020015b60405180910390f35b34801561010857600080fd5b5061011c610117366004611cd7565b61031a565b6040519081526020016100f3565b61013d610138366004611d2a565b6106e2565b005b34801561014b57600080fd5b5061013d61015a366004611c98565b610a5c565b34801561016b57600080fd5b506100e761017a366004611daf565b610cd8565b34801561018b57600080fd5b5061013d61019a366004611e15565b610d52565b3480156101ab57600080fd5b5061011c601481565b3480156101c057600080fd5b5061011c6101cf366004611f5f565b611193565b3480156101e057600080fd5b506102087f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020016100f3565b34801561022c57600080fd5b5061011c61023b366004611f93565b60016020526000908152604090205481565b34801561025957600080fd5b5061026d610268366004611c98565b61126e565b6040516100f39190611fc2565b34801561028657600080fd5b5061011c610295366004611f93565b60026020526000908152604090205481565b3480156102b357600080fd5b5061013d6102c2366004612005565b61130f565b600080806102e3856102de36879003870187612057565b611625565b9250925050806000141580156102fb57506000198214155b801561030f575061030d828535610cd8565b155b925050505b92915050565b6000336001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000161461038a5760405162461bcd60e51b815260206004820152600e60248201526d1b9bdd08195b9d1c9e541bda5b9d60921b60448201526064015b60405180910390fd5b60046103996060860186612073565b905010156103a9575060016106db565b6324660d6960e21b6103be6060860186612073565b6103cd916004916000916120b9565b6103d6916120e3565b6001600160e01b031916146103ed575060026106db565b60006103fc6060860186612073565b61040a9160049082906120b9565b8101906104179190612111565b905080516000148061042a575060148151115b156104395760099150506106db565b600080600083516001600160401b0381111561045757610457611e56565b604051908082528060200260200182016040528015610480578160200160208202803683370190505b50905060005b84518110156105c45760008582815181106104a3576104a36121e3565b6020026020010151905080602001518383815181106104c4576104c46121e3565b6020026020010181815250506104e68160200151826000015160000151610cd8565b6104f957600796505050505050506106db565b60008061050b60008460000151611625565b9250925050806000148061051f5750438110155b8061052b575060001982145b8061053f575082515161053f908390610cd8565b15610555576003985050505050505050506106db565b8360000361056a57825160800151965061059c565b866001600160a01b03168360000151608001516001600160a01b03161461059c576004985050505050505050506106db565b8251602001516105ac908761220f565b955050505080806105bc90612222565b915050610486565b5060006161a885516105d6919061223b565b6105e390620124f861220f565b905080896080013510156105ff576008955050505050506106db565b82871115610615576005955050505050506106db565b610661886106276101408c018c612073565b8080601f01602080910402602001604051908101604052809392919081815260200183838082843760009201919091525061165192505050565b6001600160a01b0316846001600160a01b031614610687576006955050505050506106db565b600084848460405160200161069e93929190612252565b60408051601f19818403018152918152815160209283012060008181526002909352912089905590506106d088611675565b600096505050505050505b9392505050565b3233146107015760405162461bcd60e51b8152600401610381906122af565b6107096116ca565b8015801590610719575060148111155b6107355760405162461bcd60e51b8152600401610381906122d9565b6000805b8281101561099e5736848483818110610754576107546121e3565b905060a002019050600081602001351161079d5760405162461bcd60e51b815260206004820152600a6024820152697a65726f2076616c756560b01b6044820152606401610381565b426107ae6080830160608401612301565b6001600160401b0316116107f65760405162461bcd60e51b815260206004820152600f60248201526e62616420726566756e642074696d6560881b6044820152606401610381565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf8135016108555760405162461bcd60e51b815260206004820152600c60248201526b0d2d8d8cacec2d840d0c2e6d60a31b6044820152606401610381565b600061086a876101cf36859003850185612057565b600081815260016020526040902054909150156108ba5760405162461bcd60e51b815260206004820152600e60248201526d616c72656164792065786973747360901b6044820152606401610381565b436108c6818435610cd8565b156109045760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610381565b6000828152600160209081526040909120829055610925908401358661220f565b9450610937606084016040850161231c565b6001600160a01b0316886001600160a01b0316837fe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d546866020013560405161098091815260200190565b60405180910390a4505050808061099690612222565b915050610739565b506001600160a01b0384166109f1578034146109ec5760405162461bcd60e51b815260206004820152600d60248201526c626164204554482076616c756560981b6044820152606401610381565b610a4c565b3415610a375760405162461bcd60e51b815260206004820152601560248201527406e6f2045544820666f7220746f6b656e207377617605c1b6044820152606401610381565b610a4c6001600160a01b038516333084611723565b50610a576001600055565b505050565b323314610a7b5760405162461bcd60e51b8152600401610381906122af565b610a836116ca565b610a936080820160608301612301565b6001600160401b0316421015610ad95760405162461bcd60e51b815260206004820152600b60248201526a1b9bdd08195e1c1a5c995960aa1b6044820152606401610381565b60008080610af0856102de36879003870187612057565b92509250925060008111610b365760405162461bcd60e51b815260206004820152600d60248201526c1b9bdd081a5b9a5d1a585d1959609a1b6044820152606401610381565b60018201610b565760405162461bcd60e51b815260040161038190612337565b610b61828535610cd8565b15610b7e5760405162461bcd60e51b815260040161038190612361565b600083815260016020526040902060001990556001600160a01b038516610c4f576000610bb1606086016040870161231c565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d8060008114610bff576040519150601f19603f3d011682016040523d82523d6000602084013e610c04565b606091505b5050905080610c495760405162461bcd60e51b8152602060048201526011602482015270115512081c99599d5b990819985a5b1959607a1b6044820152606401610381565b50610c77565b610c77610c62606086016040870161231c565b6001600160a01b038716906020870135611794565b610c87606085016040860161231c565b6001600160a01b0316856001600160a01b0316847f2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad6360405160405180910390a4505050610cd46001600055565b5050565b600081600284604051602001610cf091815260200190565b60408051601f1981840301815290829052610d0a916123af565b602060405180830381855afa158015610d27573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610d4a91906123cb565b149392505050565b336001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610dbb5760405162461bcd60e51b815260206004820152600e60248201526d1b9bdd08195b9d1c9e541bda5b9d60921b6044820152606401610381565b610dc36116ca565b8015801590610dd3575060148111155b610def5760405162461bcd60e51b8152600401610381906122d9565b60008080836001600160401b03811115610e0b57610e0b611e56565b604051908082528060200260200182016040528015610e34578160200160208202803683370190505b50905060005b848110156110905736868683818110610e5557610e556121e3565b905060c0020190508060a00135838381518110610e7457610e746121e3565b60200260200101818152505081600003610e9f57610e9860a082016080830161231c565b9350610f06565b6001600160a01b038416610eb960a083016080840161231c565b6001600160a01b031614610f065760405162461bcd60e51b81526020600482015260146024820152730e0c2e4e8d2c6d2e0c2dce840dad2e6dac2e8c6d60631b6044820152606401610381565b60008080610f1d816102de36879003870187612057565b925092509250600081118015610f3257504381105b610f6f5760405162461bcd60e51b815260206004820152600e60248201526d6e6f742072656465656d61626c6560901b6044820152606401610381565b60018201610f8f5760405162461bcd60e51b815260040161038190612337565b610f9a828535610cd8565b15610fb75760405162461bcd60e51b815260040161038190612361565b610fc660a08501358535610cd8565b610fff5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b6044820152606401610381565b600083815260016020908152604090912060a08601359055611024908501358961220f565b9750866001600160a01b031660006001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161107191815260200190565b60405180910390a450505050808061108890612222565b915050610e3a565b5060008284836040516020016110a893929190612252565b60408051601f19818403018152918152815160209283012060008181526002909352908220805490839055909250906110e182876123e4565b90506000856001600160a01b03168260405160006040518083038185875af1925050503d8060008114611130576040519150601f19603f3d011682016040523d82523d6000602084013e611135565b606091505b505090508061118257856001600160a01b03167f29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae08360405161117991815260200190565b60405180910390a25b50505050505050610cd46001600055565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b604051602001611214969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091526001600160c01b031916606884015216607082015260840190565b60408051601f198184030181529082905261122e916123af565b602060405180830381855afa15801561124b573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106db91906123cb565b60408051606081018252600080825260208201819052918101829052908061129f856102de36879003870187612057565b9250925050806000036112cb578260005b908160038111156112c3576112c3611fac565b905250611307565b600182016112db578260036112b0565b6112e6828535610cd8565b156112fb576002835260208301829052611307565b60018352604083018190525b505092915050565b32331461132e5760405162461bcd60e51b8152600401610381906122af565b6113366116ca565b8015801590611346575060148111155b6113625760405162461bcd60e51b8152600401610381906122d9565b6000805b8281101561156e5736848483818110611381576113816121e3565b60c00291909101915033905061139d60a083016080840161231c565b6001600160a01b0316146113e55760405162461bcd60e51b815260206004820152600f60248201526e1b9bdd081c185c9d1a58da5c185b9d608a1b6044820152606401610381565b600080806113fc896102de36879003870187612057565b92509250925060008111801561141157504381105b61144e5760405162461bcd60e51b815260206004820152600e60248201526d6e6f742072656465656d61626c6560901b6044820152606401610381565b6001820161146e5760405162461bcd60e51b815260040161038190612337565b611479828535610cd8565b156114965760405162461bcd60e51b815260040161038190612361565b6114a560a08501358535610cd8565b6114de5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b6044820152606401610381565b600083815260016020908152604090912060a08601359055611503908501358761220f565b9550336001600160a01b0316896001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161154f91815260200190565b60405180910390a450505050808061156690612222565b915050611366565b506001600160a01b03841661161157604051600090339083908381818185875af1925050503d80600081146115bf576040519150601f19603f3d011682016040523d82523d6000602084013e6115c4565b606091505b505090508061160b5760405162461bcd60e51b8152602060048201526013602482015272115512081d1c985b9cd9995c8819985a5b1959606a1b6044820152606401610381565b50610a4c565b610a4c6001600160a01b0385163383611794565b60008060006116348585611193565b600081815260016020526040902054909690955085945092505050565b600080600061166085856117c4565b9150915061166d81611809565b509392505050565b80156116c757604051600090339083908381818185875af1925050503d80600081146116bd576040519150601f19603f3d011682016040523d82523d6000602084013e6116c2565b606091505b505050505b50565b60026000540361171c5760405162461bcd60e51b815260206004820152601f60248201527f5265656e7472616e637947756172643a207265656e7472616e742063616c6c006044820152606401610381565b6002600055565b6040516001600160a01b038085166024830152831660448201526064810182905261178e9085906323b872dd60e01b906084015b60408051601f198184030181529190526020810180516001600160e01b03166001600160e01b031990931692909217909152611953565b50505050565b6040516001600160a01b038316602482015260448101829052610a5790849063a9059cbb60e01b90606401611757565b60008082516041036117fa5760208301516040840151606085015160001a6117ee87828585611a28565b94509450505050611802565b506000905060025b9250929050565b600081600481111561181d5761181d611fac565b036118255750565b600181600481111561183957611839611fac565b036118865760405162461bcd60e51b815260206004820152601860248201527f45434453413a20696e76616c6964207369676e617475726500000000000000006044820152606401610381565b600281600481111561189a5761189a611fac565b036118e75760405162461bcd60e51b815260206004820152601f60248201527f45434453413a20696e76616c6964207369676e6174757265206c656e677468006044820152606401610381565b60038160048111156118fb576118fb611fac565b036116c75760405162461bcd60e51b815260206004820152602260248201527f45434453413a20696e76616c6964207369676e6174757265202773272076616c604482015261756560f01b6064820152608401610381565b60006119a8826040518060400160405280602081526020017f5361666545524332303a206c6f772d6c6576656c2063616c6c206661696c6564815250856001600160a01b0316611aec9092919063ffffffff16565b90508051600014806119c95750808060200190518101906119c991906123f7565b610a575760405162461bcd60e51b815260206004820152602a60248201527f5361666545524332303a204552433230206f7065726174696f6e20646964206e6044820152691bdd081cdd58d8d9595960b21b6064820152608401610381565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0831115611a5f5750600090506003611ae3565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa158015611ab3573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b038116611adc57600060019250925050611ae3565b9150600090505b94509492505050565b6060611afb8484600085611b03565b949350505050565b606082471015611b645760405162461bcd60e51b815260206004820152602660248201527f416464726573733a20696e73756666696369656e742062616c616e636520666f6044820152651c8818d85b1b60d21b6064820152608401610381565b600080866001600160a01b03168587604051611b8091906123af565b60006040518083038185875af1925050503d8060008114611bbd576040519150601f19603f3d011682016040523d82523d6000602084013e611bc2565b606091505b5091509150611bd387838387611bde565b979650505050505050565b60608315611c4d578251600003611c46576001600160a01b0385163b611c465760405162461bcd60e51b815260206004820152601d60248201527f416464726573733a2063616c6c20746f206e6f6e2d636f6e74726163740000006044820152606401610381565b5081611afb565b611afb8383815115611c625781518083602001fd5b8060405162461bcd60e51b81526004016103819190612419565b80356001600160a01b0381168114611c9357600080fd5b919050565b60008082840360c0811215611cac57600080fd5b611cb584611c7c565b925060a0601f1982011215611cc957600080fd5b506020830190509250929050565b600080600060608486031215611cec57600080fd5b83356001600160401b03811115611d0257600080fd5b84016101608187031215611d1557600080fd5b95602085013595506040909401359392505050565b600080600060408486031215611d3f57600080fd5b611d4884611c7c565b925060208401356001600160401b0380821115611d6457600080fd5b818601915086601f830112611d7857600080fd5b813581811115611d8757600080fd5b87602060a083028501011115611d9c57600080fd5b6020830194508093505050509250925092565b60008060408385031215611dc257600080fd5b50508035926020909101359150565b60008083601f840112611de357600080fd5b5081356001600160401b03811115611dfa57600080fd5b60208301915083602060c08302850101111561180257600080fd5b60008060208385031215611e2857600080fd5b82356001600160401b03811115611e3e57600080fd5b611e4a85828601611dd1565b90969095509350505050565b634e487b7160e01b600052604160045260246000fd5b604080519081016001600160401b0381118282101715611e8e57611e8e611e56565b60405290565b604051601f8201601f191681016001600160401b0381118282101715611ebc57611ebc611e56565b604052919050565b80356001600160401b0381168114611c9357600080fd5b600060a08284031215611eed57600080fd5b60405160a081018181106001600160401b0382111715611f0f57611f0f611e56565b80604052508091508235815260208301356020820152611f3160408401611c7c565b6040820152611f4260608401611ec4565b6060820152611f5360808401611c7c565b60808201525092915050565b60008060c08385031215611f7257600080fd5b611f7b83611c7c565b9150611f8a8460208501611edb565b90509250929050565b600060208284031215611fa557600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110611fe757634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060006040848603121561201a57600080fd5b61202384611c7c565b925060208401356001600160401b0381111561203e57600080fd5b61204a86828701611dd1565b9497909650939450505050565b600060a0828403121561206957600080fd5b6106db8383611edb565b6000808335601e1984360301811261208a57600080fd5b8301803591506001600160401b038211156120a457600080fd5b60200191503681900382131561180257600080fd5b600080858511156120c957600080fd5b838611156120d657600080fd5b5050820193919092039150565b6001600160e01b031981358181169160048510156113075760049490940360031b84901b1690921692915050565b6000602080838503121561212457600080fd5b82356001600160401b038082111561213b57600080fd5b818501915085601f83011261214f57600080fd5b81358181111561216157612161611e56565b61216f848260051b01611e94565b818152848101925060c091820284018501918883111561218e57600080fd5b938501935b828510156121d75780858a0312156121ab5760008081fd5b6121b3611e6c565b6121bd8a87611edb565b815260a08601358782015284529384019392850192612193565b50979650505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b80820180821115610314576103146121f9565b600060018201612234576122346121f9565b5060010190565b8082028115828204841417610314576103146121f9565b6001600160a01b038416815260208082018490526060604083018190528351908301819052600091848101916080850190845b818110156122a157845183529383019391830191600101612285565b509098975050505050505050565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b6020808252600e908201526d6261642062617463682073697a6560901b604082015260600190565b60006020828403121561231357600080fd5b6106db82611ec4565b60006020828403121561232e57600080fd5b6106db82611c7c565b60208082526010908201526f185b1c9958591e481c99599d5b99195960821b604082015260600190565b60208082526010908201526f185b1c9958591e481c995919595b595960821b604082015260600190565b60005b838110156123a657818101518382015260200161238e565b50506000910152565b600082516123c181846020870161238b565b9190910192915050565b6000602082840312156123dd57600080fd5b5051919050565b81810381811115610314576103146121f9565b60006020828403121561240957600080fd5b815180151581146106db57600080fd5b602081526000825180602084015261243881604085016020870161238b565b601f01601f1916919091016040019291505056fea264697066735822122056a0eb09d3500dcb68dd0e77e022ea6a848876a19fc5757511c254412b27dd5864736f6c63430008120033",
}

// ETHSwapABI is the input ABI used to generate the binding from.
// Deprecated: Use ETHSwapMetaData.ABI instead.
var ETHSwapABI = ETHSwapMetaData.ABI

// ETHSwapBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ETHSwapMetaData.Bin instead.
var ETHSwapBin = ETHSwapMetaData.Bin

// DeployETHSwap deploys a new Ethereum contract, binding an instance of ETHSwap to it.
func DeployETHSwap(auth *bind.TransactOpts, backend bind.ContractBackend, _entryPoint common.Address) (common.Address, *types.Transaction, *ETHSwap, error) {
	parsed, err := ETHSwapMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ETHSwapBin), backend, _entryPoint)
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
	parsed, err := ETHSwapMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
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

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_ETHSwap *ETHSwapCaller) MAXBATCH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "MAX_BATCH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_ETHSwap *ETHSwapSession) MAXBATCH() (*big.Int, error) {
	return _ETHSwap.Contract.MAXBATCH(&_ETHSwap.CallOpts)
}

// MAXBATCH is a free data retrieval call binding the contract method 0x950bff9f.
//
// Solidity: function MAX_BATCH() view returns(uint256)
func (_ETHSwap *ETHSwapCallerSession) MAXBATCH() (*big.Int, error) {
	return _ETHSwap.Contract.MAXBATCH(&_ETHSwap.CallOpts)
}

// ContractKey is a free data retrieval call binding the contract method 0x9ef07b4c.
//
// Solidity: function contractKey(address token, (bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCaller) ContractKey(opts *bind.CallOpts, token common.Address, v ETHSwapVector) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "contractKey", token, v)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ContractKey is a free data retrieval call binding the contract method 0x9ef07b4c.
//
// Solidity: function contractKey(address token, (bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapSession) ContractKey(token common.Address, v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, token, v)
}

// ContractKey is a free data retrieval call binding the contract method 0x9ef07b4c.
//
// Solidity: function contractKey(address token, (bytes32,uint256,address,uint64,address) v) pure returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) ContractKey(token common.Address, v ETHSwapVector) ([32]byte, error) {
	return _ETHSwap.Contract.ContractKey(&_ETHSwap.CallOpts, token, v)
}

// EntryPoint is a free data retrieval call binding the contract method 0xb0d691fe.
//
// Solidity: function entryPoint() view returns(address)
func (_ETHSwap *ETHSwapCaller) EntryPoint(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "entryPoint")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// EntryPoint is a free data retrieval call binding the contract method 0xb0d691fe.
//
// Solidity: function entryPoint() view returns(address)
func (_ETHSwap *ETHSwapSession) EntryPoint() (common.Address, error) {
	return _ETHSwap.Contract.EntryPoint(&_ETHSwap.CallOpts)
}

// EntryPoint is a free data retrieval call binding the contract method 0xb0d691fe.
//
// Solidity: function entryPoint() view returns(address)
func (_ETHSwap *ETHSwapCallerSession) EntryPoint() (common.Address, error) {
	return _ETHSwap.Contract.EntryPoint(&_ETHSwap.CallOpts)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x33a3bcb4.
//
// Solidity: function isRedeemable(address token, (bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ETHSwap *ETHSwapCaller) IsRedeemable(opts *bind.CallOpts, token common.Address, v ETHSwapVector) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "isRedeemable", token, v)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRedeemable is a free data retrieval call binding the contract method 0x33a3bcb4.
//
// Solidity: function isRedeemable(address token, (bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ETHSwap *ETHSwapSession) IsRedeemable(token common.Address, v ETHSwapVector) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, token, v)
}

// IsRedeemable is a free data retrieval call binding the contract method 0x33a3bcb4.
//
// Solidity: function isRedeemable(address token, (bytes32,uint256,address,uint64,address) v) view returns(bool)
func (_ETHSwap *ETHSwapCallerSession) IsRedeemable(token common.Address, v ETHSwapVector) (bool, error) {
	return _ETHSwap.Contract.IsRedeemable(&_ETHSwap.CallOpts, token, v)
}

// RedeemPrepayments is a free data retrieval call binding the contract method 0xf521b2eb.
//
// Solidity: function redeemPrepayments(bytes32 ) view returns(uint256)
func (_ETHSwap *ETHSwapCaller) RedeemPrepayments(opts *bind.CallOpts, arg0 [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "redeemPrepayments", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// RedeemPrepayments is a free data retrieval call binding the contract method 0xf521b2eb.
//
// Solidity: function redeemPrepayments(bytes32 ) view returns(uint256)
func (_ETHSwap *ETHSwapSession) RedeemPrepayments(arg0 [32]byte) (*big.Int, error) {
	return _ETHSwap.Contract.RedeemPrepayments(&_ETHSwap.CallOpts, arg0)
}

// RedeemPrepayments is a free data retrieval call binding the contract method 0xf521b2eb.
//
// Solidity: function redeemPrepayments(bytes32 ) view returns(uint256)
func (_ETHSwap *ETHSwapCallerSession) RedeemPrepayments(arg0 [32]byte) (*big.Int, error) {
	return _ETHSwap.Contract.RedeemPrepayments(&_ETHSwap.CallOpts, arg0)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapCaller) SecretValidates(opts *bind.CallOpts, secret [32]byte, secretHash [32]byte) (bool, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "secretValidates", secret, secretHash)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.SecretValidates(&_ETHSwap.CallOpts, secret, secretHash)
}

// SecretValidates is a free data retrieval call binding the contract method 0x77d7e031.
//
// Solidity: function secretValidates(bytes32 secret, bytes32 secretHash) pure returns(bool)
func (_ETHSwap *ETHSwapCallerSession) SecretValidates(secret [32]byte, secretHash [32]byte) (bool, error) {
	return _ETHSwap.Contract.SecretValidates(&_ETHSwap.CallOpts, secret, secretHash)
}

// Status is a free data retrieval call binding the contract method 0xf0e3b8d5.
//
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256) s)
func (_ETHSwap *ETHSwapCaller) Status(opts *bind.CallOpts, token common.Address, v ETHSwapVector) (ETHSwapStatus, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "status", token, v)

	if err != nil {
		return *new(ETHSwapStatus), err
	}

	out0 := *abi.ConvertType(out[0], new(ETHSwapStatus)).(*ETHSwapStatus)

	return out0, err

}

// Status is a free data retrieval call binding the contract method 0xf0e3b8d5.
//
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256) s)
func (_ETHSwap *ETHSwapSession) Status(token common.Address, v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, token, v)
}

// Status is a free data retrieval call binding the contract method 0xf0e3b8d5.
//
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256) s)
func (_ETHSwap *ETHSwapCallerSession) Status(token common.Address, v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, token, v)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapCaller) Swaps(opts *bind.CallOpts, arg0 [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "swaps", arg0)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Swaps is a free data retrieval call binding the contract method 0xeb84e7f2.
//
// Solidity: function swaps(bytes32 ) view returns(bytes32)
func (_ETHSwap *ETHSwapCallerSession) Swaps(arg0 [32]byte) ([32]byte, error) {
	return _ETHSwap.Contract.Swaps(&_ETHSwap.CallOpts, arg0)
}

// Initiate is a paid mutator transaction binding the contract method 0x52145bc0.
//
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] vectors) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, token common.Address, vectors []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", token, vectors)
}

// Initiate is a paid mutator transaction binding the contract method 0x52145bc0.
//
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] vectors) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(token common.Address, vectors []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, token, vectors)
}

// Initiate is a paid mutator transaction binding the contract method 0x52145bc0.
//
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] vectors) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(token common.Address, vectors []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, token, vectors)
}

// Redeem is a paid mutator transaction binding the contract method 0xf9f2e0f4.
//
// Solidity: function redeem(address token, ((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactor) Redeem(opts *bind.TransactOpts, token common.Address, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeem", token, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf9f2e0f4.
//
// Solidity: function redeem(address token, ((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapSession) Redeem(token common.Address, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, token, redemptions)
}

// Redeem is a paid mutator transaction binding the contract method 0xf9f2e0f4.
//
// Solidity: function redeem(address token, ((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactorSession) Redeem(token common.Address, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.Redeem(&_ETHSwap.TransactOpts, token, redemptions)
}

// RedeemAA is a paid mutator transaction binding the contract method 0x919835a4.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactor) RedeemAA(opts *bind.TransactOpts, redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeemAA", redemptions)
}

// RedeemAA is a paid mutator transaction binding the contract method 0x919835a4.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapSession) RedeemAA(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.RedeemAA(&_ETHSwap.TransactOpts, redemptions)
}

// RedeemAA is a paid mutator transaction binding the contract method 0x919835a4.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions) returns()
func (_ETHSwap *ETHSwapTransactorSession) RedeemAA(redemptions []ETHSwapRedemption) (*types.Transaction, error) {
	return _ETHSwap.Contract.RedeemAA(&_ETHSwap.TransactOpts, redemptions)
}

// Refund is a paid mutator transaction binding the contract method 0x71b6d011.
//
// Solidity: function refund(address token, (bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapTransactor) Refund(opts *bind.TransactOpts, token common.Address, v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "refund", token, v)
}

// Refund is a paid mutator transaction binding the contract method 0x71b6d011.
//
// Solidity: function refund(address token, (bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapSession) Refund(token common.Address, v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, token, v)
}

// Refund is a paid mutator transaction binding the contract method 0x71b6d011.
//
// Solidity: function refund(address token, (bytes32,uint256,address,uint64,address) v) returns()
func (_ETHSwap *ETHSwapTransactorSession) Refund(token common.Address, v ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Refund(&_ETHSwap.TransactOpts, token, v)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapTransactor) ValidateUserOp(opts *bind.TransactOpts, userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "validateUserOp", userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapSession) ValidateUserOp(userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapTransactorSession) ValidateUserOp(userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, userOpHash, missingAccountFunds)
}

// ETHSwapInitiatedIterator is returned from FilterInitiated and is used to iterate over the raw logs and unpacked data for Initiated events raised by the ETHSwap contract.
type ETHSwapInitiatedIterator struct {
	Event *ETHSwapInitiated // Event containing the contract specifics and raw log

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
func (it *ETHSwapInitiatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHSwapInitiated)
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
		it.Event = new(ETHSwapInitiated)
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
func (it *ETHSwapInitiatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHSwapInitiatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHSwapInitiated represents a Initiated event raised by the ETHSwap contract.
type ETHSwapInitiated struct {
	SwapKey   [32]byte
	Token     common.Address
	Initiator common.Address
	Value     *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterInitiated is a free log retrieval operation binding the contract event 0xe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d546.
//
// Solidity: event Initiated(bytes32 indexed swapKey, address indexed token, address indexed initiator, uint256 value)
func (_ETHSwap *ETHSwapFilterer) FilterInitiated(opts *bind.FilterOpts, swapKey [][32]byte, token []common.Address, initiator []common.Address) (*ETHSwapInitiatedIterator, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var initiatorRule []interface{}
	for _, initiatorItem := range initiator {
		initiatorRule = append(initiatorRule, initiatorItem)
	}

	logs, sub, err := _ETHSwap.contract.FilterLogs(opts, "Initiated", swapKeyRule, tokenRule, initiatorRule)
	if err != nil {
		return nil, err
	}
	return &ETHSwapInitiatedIterator{contract: _ETHSwap.contract, event: "Initiated", logs: logs, sub: sub}, nil
}

// WatchInitiated is a free log subscription operation binding the contract event 0xe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d546.
//
// Solidity: event Initiated(bytes32 indexed swapKey, address indexed token, address indexed initiator, uint256 value)
func (_ETHSwap *ETHSwapFilterer) WatchInitiated(opts *bind.WatchOpts, sink chan<- *ETHSwapInitiated, swapKey [][32]byte, token []common.Address, initiator []common.Address) (event.Subscription, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var initiatorRule []interface{}
	for _, initiatorItem := range initiator {
		initiatorRule = append(initiatorRule, initiatorItem)
	}

	logs, sub, err := _ETHSwap.contract.WatchLogs(opts, "Initiated", swapKeyRule, tokenRule, initiatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHSwapInitiated)
				if err := _ETHSwap.contract.UnpackLog(event, "Initiated", log); err != nil {
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

// ParseInitiated is a log parse operation binding the contract event 0xe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d546.
//
// Solidity: event Initiated(bytes32 indexed swapKey, address indexed token, address indexed initiator, uint256 value)
func (_ETHSwap *ETHSwapFilterer) ParseInitiated(log types.Log) (*ETHSwapInitiated, error) {
	event := new(ETHSwapInitiated)
	if err := _ETHSwap.contract.UnpackLog(event, "Initiated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ETHSwapRedeemAATransferFailedIterator is returned from FilterRedeemAATransferFailed and is used to iterate over the raw logs and unpacked data for RedeemAATransferFailed events raised by the ETHSwap contract.
type ETHSwapRedeemAATransferFailedIterator struct {
	Event *ETHSwapRedeemAATransferFailed // Event containing the contract specifics and raw log

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
func (it *ETHSwapRedeemAATransferFailedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHSwapRedeemAATransferFailed)
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
		it.Event = new(ETHSwapRedeemAATransferFailed)
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
func (it *ETHSwapRedeemAATransferFailedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHSwapRedeemAATransferFailedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHSwapRedeemAATransferFailed represents a RedeemAATransferFailed event raised by the ETHSwap contract.
type ETHSwapRedeemAATransferFailed struct {
	Recipient common.Address
	Amount    *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterRedeemAATransferFailed is a free log retrieval operation binding the contract event 0x29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae0.
//
// Solidity: event RedeemAATransferFailed(address indexed recipient, uint256 amount)
func (_ETHSwap *ETHSwapFilterer) FilterRedeemAATransferFailed(opts *bind.FilterOpts, recipient []common.Address) (*ETHSwapRedeemAATransferFailedIterator, error) {

	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _ETHSwap.contract.FilterLogs(opts, "RedeemAATransferFailed", recipientRule)
	if err != nil {
		return nil, err
	}
	return &ETHSwapRedeemAATransferFailedIterator{contract: _ETHSwap.contract, event: "RedeemAATransferFailed", logs: logs, sub: sub}, nil
}

// WatchRedeemAATransferFailed is a free log subscription operation binding the contract event 0x29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae0.
//
// Solidity: event RedeemAATransferFailed(address indexed recipient, uint256 amount)
func (_ETHSwap *ETHSwapFilterer) WatchRedeemAATransferFailed(opts *bind.WatchOpts, sink chan<- *ETHSwapRedeemAATransferFailed, recipient []common.Address) (event.Subscription, error) {

	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _ETHSwap.contract.WatchLogs(opts, "RedeemAATransferFailed", recipientRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHSwapRedeemAATransferFailed)
				if err := _ETHSwap.contract.UnpackLog(event, "RedeemAATransferFailed", log); err != nil {
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

// ParseRedeemAATransferFailed is a log parse operation binding the contract event 0x29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae0.
//
// Solidity: event RedeemAATransferFailed(address indexed recipient, uint256 amount)
func (_ETHSwap *ETHSwapFilterer) ParseRedeemAATransferFailed(log types.Log) (*ETHSwapRedeemAATransferFailed, error) {
	event := new(ETHSwapRedeemAATransferFailed)
	if err := _ETHSwap.contract.UnpackLog(event, "RedeemAATransferFailed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ETHSwapRedeemedIterator is returned from FilterRedeemed and is used to iterate over the raw logs and unpacked data for Redeemed events raised by the ETHSwap contract.
type ETHSwapRedeemedIterator struct {
	Event *ETHSwapRedeemed // Event containing the contract specifics and raw log

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
func (it *ETHSwapRedeemedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHSwapRedeemed)
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
		it.Event = new(ETHSwapRedeemed)
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
func (it *ETHSwapRedeemedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHSwapRedeemedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHSwapRedeemed represents a Redeemed event raised by the ETHSwap contract.
type ETHSwapRedeemed struct {
	SwapKey     [32]byte
	Token       common.Address
	Participant common.Address
	Secret      [32]byte
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterRedeemed is a free log retrieval operation binding the contract event 0xd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f.
//
// Solidity: event Redeemed(bytes32 indexed swapKey, address indexed token, address indexed participant, bytes32 secret)
func (_ETHSwap *ETHSwapFilterer) FilterRedeemed(opts *bind.FilterOpts, swapKey [][32]byte, token []common.Address, participant []common.Address) (*ETHSwapRedeemedIterator, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var participantRule []interface{}
	for _, participantItem := range participant {
		participantRule = append(participantRule, participantItem)
	}

	logs, sub, err := _ETHSwap.contract.FilterLogs(opts, "Redeemed", swapKeyRule, tokenRule, participantRule)
	if err != nil {
		return nil, err
	}
	return &ETHSwapRedeemedIterator{contract: _ETHSwap.contract, event: "Redeemed", logs: logs, sub: sub}, nil
}

// WatchRedeemed is a free log subscription operation binding the contract event 0xd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f.
//
// Solidity: event Redeemed(bytes32 indexed swapKey, address indexed token, address indexed participant, bytes32 secret)
func (_ETHSwap *ETHSwapFilterer) WatchRedeemed(opts *bind.WatchOpts, sink chan<- *ETHSwapRedeemed, swapKey [][32]byte, token []common.Address, participant []common.Address) (event.Subscription, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var participantRule []interface{}
	for _, participantItem := range participant {
		participantRule = append(participantRule, participantItem)
	}

	logs, sub, err := _ETHSwap.contract.WatchLogs(opts, "Redeemed", swapKeyRule, tokenRule, participantRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHSwapRedeemed)
				if err := _ETHSwap.contract.UnpackLog(event, "Redeemed", log); err != nil {
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

// ParseRedeemed is a log parse operation binding the contract event 0xd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f.
//
// Solidity: event Redeemed(bytes32 indexed swapKey, address indexed token, address indexed participant, bytes32 secret)
func (_ETHSwap *ETHSwapFilterer) ParseRedeemed(log types.Log) (*ETHSwapRedeemed, error) {
	event := new(ETHSwapRedeemed)
	if err := _ETHSwap.contract.UnpackLog(event, "Redeemed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ETHSwapRefundedIterator is returned from FilterRefunded and is used to iterate over the raw logs and unpacked data for Refunded events raised by the ETHSwap contract.
type ETHSwapRefundedIterator struct {
	Event *ETHSwapRefunded // Event containing the contract specifics and raw log

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
func (it *ETHSwapRefundedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ETHSwapRefunded)
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
		it.Event = new(ETHSwapRefunded)
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
func (it *ETHSwapRefundedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ETHSwapRefundedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ETHSwapRefunded represents a Refunded event raised by the ETHSwap contract.
type ETHSwapRefunded struct {
	SwapKey   [32]byte
	Token     common.Address
	Initiator common.Address
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterRefunded is a free log retrieval operation binding the contract event 0x2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad63.
//
// Solidity: event Refunded(bytes32 indexed swapKey, address indexed token, address indexed initiator)
func (_ETHSwap *ETHSwapFilterer) FilterRefunded(opts *bind.FilterOpts, swapKey [][32]byte, token []common.Address, initiator []common.Address) (*ETHSwapRefundedIterator, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var initiatorRule []interface{}
	for _, initiatorItem := range initiator {
		initiatorRule = append(initiatorRule, initiatorItem)
	}

	logs, sub, err := _ETHSwap.contract.FilterLogs(opts, "Refunded", swapKeyRule, tokenRule, initiatorRule)
	if err != nil {
		return nil, err
	}
	return &ETHSwapRefundedIterator{contract: _ETHSwap.contract, event: "Refunded", logs: logs, sub: sub}, nil
}

// WatchRefunded is a free log subscription operation binding the contract event 0x2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad63.
//
// Solidity: event Refunded(bytes32 indexed swapKey, address indexed token, address indexed initiator)
func (_ETHSwap *ETHSwapFilterer) WatchRefunded(opts *bind.WatchOpts, sink chan<- *ETHSwapRefunded, swapKey [][32]byte, token []common.Address, initiator []common.Address) (event.Subscription, error) {

	var swapKeyRule []interface{}
	for _, swapKeyItem := range swapKey {
		swapKeyRule = append(swapKeyRule, swapKeyItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var initiatorRule []interface{}
	for _, initiatorItem := range initiator {
		initiatorRule = append(initiatorRule, initiatorItem)
	}

	logs, sub, err := _ETHSwap.contract.WatchLogs(opts, "Refunded", swapKeyRule, tokenRule, initiatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ETHSwapRefunded)
				if err := _ETHSwap.contract.UnpackLog(event, "Refunded", log); err != nil {
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

// ParseRefunded is a log parse operation binding the contract event 0x2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad63.
//
// Solidity: event Refunded(bytes32 indexed swapKey, address indexed token, address indexed initiator)
func (_ETHSwap *ETHSwapFilterer) ParseRefunded(log types.Log) (*ETHSwapRefunded, error) {
	event := new(ETHSwapRefunded)
	if err := _ETHSwap.contract.UnpackLog(event, "Refunded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
