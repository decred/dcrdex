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
	ABI: "[{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"_entryPoint\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Initiated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"Redeemed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"}],\"name\":\"Refunded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"MAX_BATCH\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"entryPoint\",\"outputs\":[{\"internalType\":\"addresspayable\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"vectors\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeemAA\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"redeemPrepayments\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"s\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"missingAccountFunds\",\"type\":\"uint256\"}],\"name\":\"validateUserOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60a06040523480156200001157600080fd5b50604051620024fe380380620024fe833981016040819052620000349162000099565b60016000556001600160a01b038116620000875760405162461bcd60e51b815260206004820152601060248201526f1e995c9bc8195b9d1c9e481c1bda5b9d60821b604482015260640160405180910390fd5b6001600160a01b0316608052620000cb565b600060208284031215620000ac57600080fd5b81516001600160a01b0381168114620000c457600080fd5b9392505050565b608051612409620000f5600039600081816101e6015281816103270152610d4701526124096000f3fe6080604052600436106100c25760003560e01c8063950bff9f1161007f578063eb84e7f211610059578063eb84e7f214610220578063f0e3b8d51461024d578063f521b2eb1461027a578063f9f2e0f4146102a757600080fd5b8063950bff9f1461019f5780639ef07b4c146101b4578063b0d691fe146101d457600080fd5b806333a3bcb4146100c75780633a871cdd146100fc57806352145bc01461012a57806371b6d0111461013f57806377d7e0311461015f578063919835a41461017f575b600080fd5b3480156100d357600080fd5b506100e76100e2366004611c71565b6102c7565b60405190151581526020015b60405180910390f35b34801561010857600080fd5b5061011c610117366004611cb0565b61031a565b6040519081526020016100f3565b61013d610138366004611d03565b6106d8565b005b34801561014b57600080fd5b5061013d61015a366004611c71565b610a23565b34801561016b57600080fd5b506100e761017a366004611d88565b610cc2565b34801561018b57600080fd5b5061013d61019a366004611dee565b610d3c565b3480156101ab57600080fd5b5061011c601481565b3480156101c057600080fd5b5061011c6101cf366004611f38565b61116b565b3480156101e057600080fd5b506102087f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020016100f3565b34801561022c57600080fd5b5061011c61023b366004611f6c565b60016020526000908152604090205481565b34801561025957600080fd5b5061026d610268366004611c71565b611246565b6040516100f39190611f9b565b34801561028657600080fd5b5061011c610295366004611f6c565b60026020526000908152604090205481565b3480156102b357600080fd5b5061013d6102c2366004611fde565b6112e7565b600080806102e3856102de36879003870187612030565b6115fe565b9250925050806000141580156102fb57506000198214155b801561030f575061030d828535610cc2565b155b925050505b92915050565b6000336001600160a01b037f0000000000000000000000000000000000000000000000000000000000000000161461038a5760405162461bcd60e51b815260206004820152600e60248201526d1b9bdd08195b9d1c9e541bda5b9d60921b60448201526064015b60405180910390fd5b6004610399606086018661204c565b905010156103a9575060016106d1565b6324660d6960e21b6103be606086018661204c565b6103cd91600491600091612092565b6103d6916120bc565b6001600160e01b031916146103ed575060026106d1565b60006103fc606086018661204c565b61040a916004908290612092565b81019061041791906120ea565b905080516000148061042a575060148151115b156104395760099150506106d1565b600080600083516001600160401b0381111561045757610457611e2f565b604051908082528060200260200182016040528015610480578160200160208202803683370190505b50905060005b84518110156105b85760008582815181106104a3576104a36121bc565b6020026020010151905080602001518383815181106104c4576104c46121bc565b6020026020010181815250506104e68160200151826000015160000151610cc2565b6104f957600796505050505050506106d1565b60008061050b600084600001516115fe565b9250925050806000148061051f5750438110155b806105335750825151610533908390610cc2565b15610549576003985050505050505050506106d1565b8360000361055e578251608001519650610590565b866001600160a01b03168360000151608001516001600160a01b031614610590576004985050505050505050506106d1565b8251602001516105a090876121e8565b955050505080806105b0906121fb565b915050610486565b5060008383836040516020016105d093929190612214565b60408051601f19818403018152918152815160209283012060008181526002909352908220899055865190925061060a90613a9890612271565b6106169061c3506121e8565b9050808a60800135101561063357600896505050505050506106d1565b8388111561064a57600596505050505050506106d1565b6106968961065c6101408d018d61204c565b8080601f01602080910402602001604051908101604052809392919081815260200183838082843760009201919091525061162a92505050565b6001600160a01b0316856001600160a01b0316146106bd57600696505050505050506106d1565b6106c68861164e565b600096505050505050505b9392505050565b3233146106f75760405162461bcd60e51b815260040161038190612288565b6106ff6116a3565b60148111156107425760405162461bcd60e51b815260206004820152600f60248201526e626174636820746f6f206c6172676560881b6044820152606401610381565b6000805b828110156109ab5736848483818110610761576107616121bc565b905060a00201905060008160200135116107aa5760405162461bcd60e51b815260206004820152600a6024820152697a65726f2076616c756560b01b6044820152606401610381565b426107bb60808301606084016122b2565b6001600160401b0316116108035760405162461bcd60e51b815260206004820152600f60248201526e62616420726566756e642074696d6560881b6044820152606401610381565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf8135016108625760405162461bcd60e51b815260206004820152600c60248201526b0d2d8d8cacec2d840d0c2e6d60a31b6044820152606401610381565b6000610877876101cf36859003850185612030565b600081815260016020526040902054909150156108c75760405162461bcd60e51b815260206004820152600e60248201526d616c72656164792065786973747360901b6044820152606401610381565b436108d3818435610cc2565b156109115760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610381565b600082815260016020908152604090912082905561093290840135866121e8565b945061094460608401604085016122cd565b6001600160a01b0316886001600160a01b0316837fe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d546866020013560405161098d91815260200190565b60405180910390a450505080806109a3906121fb565b915050610746565b506001600160a01b0384166109fe578034146109f95760405162461bcd60e51b815260206004820152600d60248201526c626164204554482076616c756560981b6044820152606401610381565b610a13565b610a136001600160a01b0385163330846116fc565b50610a1e6001600055565b505050565b323314610a425760405162461bcd60e51b815260040161038190612288565b610a4a6116a3565b610a5a60808201606083016122b2565b6001600160401b0316421015610aa05760405162461bcd60e51b815260206004820152600b60248201526a1b9bdd08195e1c1a5c995960aa1b6044820152606401610381565b60008080610ab7856102de36879003870187612030565b92509250925060008111610afd5760405162461bcd60e51b815260206004820152600d60248201526c1b9bdd081a5b9a5d1a585d1959609a1b6044820152606401610381565b60018201610b405760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b6044820152606401610381565b610b4b828535610cc2565b15610b685760405162461bcd60e51b8152600401610381906122e8565b600083815260016020526040902060001990556001600160a01b038516610c39576000610b9b60608601604087016122cd565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d8060008114610be9576040519150601f19603f3d011682016040523d82523d6000602084013e610bee565b606091505b5050905080610c335760405162461bcd60e51b8152602060048201526011602482015270115512081c99599d5b990819985a5b1959607a1b6044820152606401610381565b50610c61565b610c61610c4c60608601604087016122cd565b6001600160a01b03871690602087013561176d565b610c7160608501604086016122cd565b6001600160a01b0316856001600160a01b0316847f2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad6360405160405180910390a4505050610cbe6001600055565b5050565b600081600284604051602001610cda91815260200190565b60408051601f1981840301815290829052610cf491612336565b602060405180830381855afa158015610d11573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610d349190612352565b149392505050565b336001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610da55760405162461bcd60e51b815260206004820152600e60248201526d1b9bdd08195b9d1c9e541bda5b9d60921b6044820152606401610381565b610dad6116a3565b8015801590610dbd575060148111155b610dfa5760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b6044820152606401610381565b60008080836001600160401b03811115610e1657610e16611e2f565b604051908082528060200260200182016040528015610e3f578160200160208202803683370190505b50905060005b8481101561107b5736868683818110610e6057610e606121bc565b905060c0020190508060a00135838381518110610e7f57610e7f6121bc565b60200260200101818152505081600003610eaa57610ea360a08201608083016122cd565b9350610f11565b6001600160a01b038416610ec460a08301608084016122cd565b6001600160a01b031614610f115760405162461bcd60e51b81526020600482015260146024820152730e0c2e4e8d2c6d2e0c2dce840dad2e6dac2e8c6d60631b6044820152606401610381565b60008080610f28816102de36879003870187612030565b925092509250600081118015610f3d57504381105b610f7a5760405162461bcd60e51b815260206004820152600e60248201526d6e6f742072656465656d61626c6560901b6044820152606401610381565b610f85828535610cc2565b15610fa25760405162461bcd60e51b8152600401610381906122e8565b610fb160a08501358535610cc2565b610fea5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b6044820152606401610381565b600083815260016020908152604090912060a0860135905561100f90850135896121e8565b9750866001600160a01b031660006001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161105c91815260200190565b60405180910390a4505050508080611073906121fb565b915050610e45565b50600082848360405160200161109393929190612214565b60408051601f19818403018152918152815160209283012060008181526002909352908220805490839055909250906001600160a01b0385166110d6838861236b565b604051600081818185875af1925050503d8060008114611112576040519150601f19603f3d011682016040523d82523d6000602084013e611117565b606091505b505090508061115b5760405162461bcd60e51b815260206004820152601060248201526f1050481c185e5bdd5d0819985a5b195960821b6044820152606401610381565b505050505050610cbe6001600055565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b6040516020016111ec969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091526001600160c01b031916606884015216607082015260840190565b60408051601f198184030181529082905261120691612336565b602060405180830381855afa158015611223573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106d19190612352565b604080516060810182526000808252602082018190529181018290529080611277856102de36879003870187612030565b9250925050806000036112a3578260005b9081600381111561129b5761129b611f85565b9052506112df565b600182016112b357826003611288565b6112be828535610cc2565b156112d35760028352602083018290526112df565b60018352604083018190525b505092915050565b3233146113065760405162461bcd60e51b815260040161038190612288565b61130e6116a3565b801580159061131e575060148111155b61135b5760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b6044820152606401610381565b6000805b82811015611547573684848381811061137a5761137a6121bc565b60c00291909101915033905061139660a08301608084016122cd565b6001600160a01b0316146113de5760405162461bcd60e51b815260206004820152600f60248201526e1b9bdd081c185c9d1a58da5c185b9d608a1b6044820152606401610381565b600080806113f5896102de36879003870187612030565b92509250925060008111801561140a57504381105b6114475760405162461bcd60e51b815260206004820152600e60248201526d6e6f742072656465656d61626c6560901b6044820152606401610381565b611452828535610cc2565b1561146f5760405162461bcd60e51b8152600401610381906122e8565b61147e60a08501358535610cc2565b6114b75760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b6044820152606401610381565b600083815260016020908152604090912060a086013590556114dc90850135876121e8565b9550336001600160a01b0316896001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161152891815260200190565b60405180910390a450505050808061153f906121fb565b91505061135f565b506001600160a01b0384166115ea57604051600090339083908381818185875af1925050503d8060008114611598576040519150601f19603f3d011682016040523d82523d6000602084013e61159d565b606091505b50509050806115e45760405162461bcd60e51b8152602060048201526013602482015272115512081d1c985b9cd9995c8819985a5b1959606a1b6044820152606401610381565b50610a13565b610a136001600160a01b038516338361176d565b600080600061160d858561116b565b600081815260016020526040902054909690955085945092505050565b6000806000611639858561179d565b91509150611646816117e2565b509392505050565b80156116a057604051600090339083908381818185875af1925050503d8060008114611696576040519150601f19603f3d011682016040523d82523d6000602084013e61169b565b606091505b505050505b50565b6002600054036116f55760405162461bcd60e51b815260206004820152601f60248201527f5265656e7472616e637947756172643a207265656e7472616e742063616c6c006044820152606401610381565b6002600055565b6040516001600160a01b03808516602483015283166044820152606481018290526117679085906323b872dd60e01b906084015b60408051601f198184030181529190526020810180516001600160e01b03166001600160e01b03199093169290921790915261192c565b50505050565b6040516001600160a01b038316602482015260448101829052610a1e90849063a9059cbb60e01b90606401611730565b60008082516041036117d35760208301516040840151606085015160001a6117c787828585611a01565b945094505050506117db565b506000905060025b9250929050565b60008160048111156117f6576117f6611f85565b036117fe5750565b600181600481111561181257611812611f85565b0361185f5760405162461bcd60e51b815260206004820152601860248201527f45434453413a20696e76616c6964207369676e617475726500000000000000006044820152606401610381565b600281600481111561187357611873611f85565b036118c05760405162461bcd60e51b815260206004820152601f60248201527f45434453413a20696e76616c6964207369676e6174757265206c656e677468006044820152606401610381565b60038160048111156118d4576118d4611f85565b036116a05760405162461bcd60e51b815260206004820152602260248201527f45434453413a20696e76616c6964207369676e6174757265202773272076616c604482015261756560f01b6064820152608401610381565b6000611981826040518060400160405280602081526020017f5361666545524332303a206c6f772d6c6576656c2063616c6c206661696c6564815250856001600160a01b0316611ac59092919063ffffffff16565b90508051600014806119a25750808060200190518101906119a2919061237e565b610a1e5760405162461bcd60e51b815260206004820152602a60248201527f5361666545524332303a204552433230206f7065726174696f6e20646964206e6044820152691bdd081cdd58d8d9595960b21b6064820152608401610381565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0831115611a385750600090506003611abc565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa158015611a8c573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b038116611ab557600060019250925050611abc565b9150600090505b94509492505050565b6060611ad48484600085611adc565b949350505050565b606082471015611b3d5760405162461bcd60e51b815260206004820152602660248201527f416464726573733a20696e73756666696369656e742062616c616e636520666f6044820152651c8818d85b1b60d21b6064820152608401610381565b600080866001600160a01b03168587604051611b599190612336565b60006040518083038185875af1925050503d8060008114611b96576040519150601f19603f3d011682016040523d82523d6000602084013e611b9b565b606091505b5091509150611bac87838387611bb7565b979650505050505050565b60608315611c26578251600003611c1f576001600160a01b0385163b611c1f5760405162461bcd60e51b815260206004820152601d60248201527f416464726573733a2063616c6c20746f206e6f6e2d636f6e74726163740000006044820152606401610381565b5081611ad4565b611ad48383815115611c3b5781518083602001fd5b8060405162461bcd60e51b815260040161038191906123a0565b80356001600160a01b0381168114611c6c57600080fd5b919050565b60008082840360c0811215611c8557600080fd5b611c8e84611c55565b925060a0601f1982011215611ca257600080fd5b506020830190509250929050565b600080600060608486031215611cc557600080fd5b83356001600160401b03811115611cdb57600080fd5b84016101608187031215611cee57600080fd5b95602085013595506040909401359392505050565b600080600060408486031215611d1857600080fd5b611d2184611c55565b925060208401356001600160401b0380821115611d3d57600080fd5b818601915086601f830112611d5157600080fd5b813581811115611d6057600080fd5b87602060a083028501011115611d7557600080fd5b6020830194508093505050509250925092565b60008060408385031215611d9b57600080fd5b50508035926020909101359150565b60008083601f840112611dbc57600080fd5b5081356001600160401b03811115611dd357600080fd5b60208301915083602060c0830285010111156117db57600080fd5b60008060208385031215611e0157600080fd5b82356001600160401b03811115611e1757600080fd5b611e2385828601611daa565b90969095509350505050565b634e487b7160e01b600052604160045260246000fd5b604080519081016001600160401b0381118282101715611e6757611e67611e2f565b60405290565b604051601f8201601f191681016001600160401b0381118282101715611e9557611e95611e2f565b604052919050565b80356001600160401b0381168114611c6c57600080fd5b600060a08284031215611ec657600080fd5b60405160a081018181106001600160401b0382111715611ee857611ee8611e2f565b80604052508091508235815260208301356020820152611f0a60408401611c55565b6040820152611f1b60608401611e9d565b6060820152611f2c60808401611c55565b60808201525092915050565b60008060c08385031215611f4b57600080fd5b611f5483611c55565b9150611f638460208501611eb4565b90509250929050565b600060208284031215611f7e57600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110611fc057634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b600080600060408486031215611ff357600080fd5b611ffc84611c55565b925060208401356001600160401b0381111561201757600080fd5b61202386828701611daa565b9497909650939450505050565b600060a0828403121561204257600080fd5b6106d18383611eb4565b6000808335601e1984360301811261206357600080fd5b8301803591506001600160401b0382111561207d57600080fd5b6020019150368190038213156117db57600080fd5b600080858511156120a257600080fd5b838611156120af57600080fd5b5050820193919092039150565b6001600160e01b031981358181169160048510156112df5760049490940360031b84901b1690921692915050565b600060208083850312156120fd57600080fd5b82356001600160401b038082111561211457600080fd5b818501915085601f83011261212857600080fd5b81358181111561213a5761213a611e2f565b612148848260051b01611e6d565b818152848101925060c091820284018501918883111561216757600080fd5b938501935b828510156121b05780858a0312156121845760008081fd5b61218c611e45565b6121968a87611eb4565b815260a0860135878201528452938401939285019261216c565b50979650505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b80820180821115610314576103146121d2565b60006001820161220d5761220d6121d2565b5060010190565b6001600160a01b038416815260208082018490526060604083018190528351908301819052600091848101916080850190845b8181101561226357845183529383019391830191600101612247565b509098975050505050505050565b8082028115828204841417610314576103146121d2565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b6000602082840312156122c457600080fd5b6106d182611e9d565b6000602082840312156122df57600080fd5b6106d182611c55565b60208082526010908201526f185b1c9958591e481c995919595b595960821b604082015260600190565b60005b8381101561232d578181015183820152602001612315565b50506000910152565b60008251612348818460208701612312565b9190910192915050565b60006020828403121561236457600080fd5b5051919050565b81810381811115610314576103146121d2565b60006020828403121561239057600080fd5b815180151581146106d157600080fd5b60208152600082518060208401526123bf816040850160208701612312565b601f01601f1916919091016040019291505056fea2646970667358221220917946c592dd0cf742fbfe6099d20a568ba90f13a66370e11cceccea5b0a6f5664736f6c63430008120033",
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
