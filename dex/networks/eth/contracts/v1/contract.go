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

// PackedUserOperation is an auto generated low-level Go binding around an user-defined struct.
type PackedUserOperation struct {
	Sender             common.Address
	Nonce              *big.Int
	InitCode           []byte
	CallData           []byte
	AccountGasLimits   [32]byte
	PreVerificationGas *big.Int
	GasFees            [32]byte
	PaymasterAndData   []byte
	Signature          []byte
}

// ETHSwapMetaData contains all meta data concerning the ETHSwap contract.
var ETHSwapMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"_entryPoint\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"ReentrancyGuardReentrantCall\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"SafeERC20FailedOperation\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Initiated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"RedeemAATransferFailed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"name\":\"Redeemed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"swapKey\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"}],\"name\":\"Refunded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"MAX_BATCH\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"entryPoint\",\"outputs\":[{\"internalType\":\"addresspayable\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"vectors\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"},{\"internalType\":\"uint256\",\"name\":\"opNonce\",\"type\":\"uint256\"}],\"name\":\"redeemAA\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"redeemPrepayments\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"s\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"bytes32\",\"name\":\"accountGasLimits\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"gasFees\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structPackedUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"missingAccountFunds\",\"type\":\"uint256\"}],\"name\":\"validateUserOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60a06040523480156200001157600080fd5b506040516200251d3803806200251d833981016040819052620000349162000099565b60016000556001600160a01b038116620000875760405162461bcd60e51b815260206004820152601060248201526f1e995c9bc8195b9d1c9e481c1bda5b9d60821b604482015260640160405180910390fd5b6001600160a01b0316608052620000cb565b600060208284031215620000ac57600080fd5b81516001600160a01b0381168114620000c457600080fd5b9392505050565b608051612428620000f5600039600081816101f6015281816102e40152610cbe01526124286000f3fe6080604052600436106100d25760003560e01c8063950bff9f1161007f578063d0758daf11610059578063d0758daf14610230578063eb84e7f21461025d578063f0e3b8d51461028a578063f9f2e0f4146102b757600080fd5b8063950bff9f146101af5780639ef07b4c146101c4578063b0d691fe146101e457600080fd5b806368356d75116100b057806368356d751461014f57806371b6d0111461016f57806377d7e0311461018f57600080fd5b806319822f7c146100d757806333a3bcb41461010a57806352145bc01461013a575b600080fd5b3480156100e357600080fd5b506100f76100f2366004611d6c565b6102d7565b6040519081526020015b60405180910390f35b34801561011657600080fd5b5061012a610125366004611ddc565b6106f8565b6040519015158152602001610101565b61014d610148366004611e1b565b61074b565b005b34801561015b57600080fd5b5061014d61016a366004611eed565b610cb3565b34801561017b57600080fd5b5061014d61018a366004611ddc565b61112e565b34801561019b57600080fd5b5061012a6101aa366004611f39565b611442565b3480156101bb57600080fd5b506100f7601481565b3480156101d057600080fd5b506100f76101df366004612068565b6114bc565b3480156101f057600080fd5b506102187f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b039091168152602001610101565b34801561023c57600080fd5b506100f761024b36600461209c565b60026020526000908152604090205481565b34801561026957600080fd5b506100f761027836600461209c565b60016020526000908152604090205481565b34801561029657600080fd5b506102aa6102a5366004611ddc565b6115af565b60405161010191906120cb565b3480156102c357600080fd5b5061014d6102d236600461210e565b611650565b6000336001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016146103565760405162461bcd60e51b815260206004820152600e60248201527f6e6f7420656e747279506f696e7400000000000000000000000000000000000060448201526064015b60405180910390fd5b61035e611a4f565b600461036d6060860186612161565b9050101561037d575060016106e7565b7f68356d75000000000000000000000000000000000000000000000000000000006103ab6060860186612161565b6103ba916004916000916121a8565b6103c3916121d2565b7fffffffff0000000000000000000000000000000000000000000000000000000016146103f2575060016106e7565b6000806104026060870187612161565b6104109160049082906121a8565b81019061041d9190612218565b9150915085602001358114610437576001925050506106e7565b81511580610446575060148251115b15610456576001925050506106e7565b60008060005b84518110156105c5576000858281518110610479576104796122ef565b602002602001015190506000801b8160200151036104a057600196505050505050506106e7565b60208101518151516104b29190611442565b6104c557600196505050505050506106e7565b60008060006104d960008560000151611a92565b600083815260036020526040902054929550909350915060ff161561050a57600199505050505050505050506106e7565b801580610518575060001982145b8061052c575083515161052c908390611442565b1561054357600199505050505050505050506106e7565b6000838152600360205260408120805460ff191660011790558590036105705783516080015196506105a3565b866001600160a01b03168460000151608001516001600160a01b0316146105a357600199505050505050505050506106e7565b8351602001516105b3908761231b565b9550506001909301925061045c915050565b50808611156105db5760019450505050506106e7565b6105e486611abe565b60006161a885516105f5919061232e565b61060290620124f861231b565b90506fffffffffffffffffffffffffffffffff60808a013516811115610630576001955050505050506106e7565b60008061067f8a6106456101008e018e612161565b8080601f016020809104026020016040519081016040528093929190818152602001838380828437600092019190915250611b1392505050565b5090925090506000816003811115610699576106996120b5565b1415806106b85750846001600160a01b0316826001600160a01b031614155b156106cd5760019750505050505050506106e7565b505050600092835250506002602052604081208490559150505b6106f16001600055565b9392505050565b600080806107148561070f36879003870187612345565b611a92565b92509250508060001415801561072c57506000198214155b8015610740575061073e828535611442565b155b925050505b92915050565b32331461078d5760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b610795611a4f565b80158015906107a5575060148111155b6107e25760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b6000805b82811015610bdd5736848483818110610801576108016122ef565b905060a002019050600081602001351161085d5760405162461bcd60e51b815260206004820152600a60248201527f7a65726f2076616c756500000000000000000000000000000000000000000000604482015260640161034d565b600061086f6060830160408401612361565b6001600160a01b03161415801561089f5750600061089360a0830160808401612361565b6001600160a01b031614155b6108eb5760405162461bcd60e51b815260206004820152600960248201527f7a65726f20616464720000000000000000000000000000000000000000000000604482015260640161034d565b426108fc608083016060840161237c565b67ffffffffffffffff16116109535760405162461bcd60e51b815260206004820152600f60248201527f62616420726566756e642074696d650000000000000000000000000000000000604482015260640161034d565b80356109a15760405162461bcd60e51b815260206004820152600960248201527f7a65726f20686173680000000000000000000000000000000000000000000000604482015260640161034d565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf813501610a115760405162461bcd60e51b815260206004820152600c60248201527f696c6c6567616c20686173680000000000000000000000000000000000000000604482015260640161034d565b7f99978552079d428893703e74716071dff768eb7a911dcc4c6fd5a6e2f2a0d6db813501610a815760405162461bcd60e51b815260206004820152601060248201527f7a65726f20736563726574206861736800000000000000000000000000000000604482015260640161034d565b6000610a96876101df36859003850185612345565b60008181526001602052604090205490915015610af55760405162461bcd60e51b815260206004820152600e60248201527f616c726561647920657869737473000000000000000000000000000000000000604482015260640161034d565b43610b01818435611442565b15610b4e5760405162461bcd60e51b815260206004820152600e60248201527f6861736820636f6c6c6973696f6e000000000000000000000000000000000000604482015260640161034d565b6000828152600160209081526040909120829055610b6f908401358661231b565b9450610b816060840160408501612361565b6001600160a01b0316886001600160a01b0316837fe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d5468660200135604051610bca91815260200190565b60405180910390a45050506001016107e6565b506001600160a01b038416610c4057803414610c3b5760405162461bcd60e51b815260206004820152600d60248201527f626164204554482076616c756500000000000000000000000000000000000000604482015260640161034d565b610ca3565b3415610c8e5760405162461bcd60e51b815260206004820152601560248201527f6e6f2045544820666f7220746f6b656e20737761700000000000000000000000604482015260640161034d565b610ca36001600160a01b038516333084611b60565b50610cae6001600055565b505050565b336001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610d2b5760405162461bcd60e51b815260206004820152600e60248201527f6e6f7420656e747279506f696e74000000000000000000000000000000000000604482015260640161034d565b610d33611a4f565b8115801590610d43575060148211155b610d805760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b60008060005b8481101561105e5736868683818110610da157610da16122ef565b905060c00201905081600003610dc857610dc160a0820160808301612361565b9250610e38565b6001600160a01b038316610de260a0830160808401612361565b6001600160a01b031614610e385760405162461bcd60e51b815260206004820152601460248201527f7061727469636970616e74206d69736d61746368000000000000000000000000604482015260640161034d565b60008080610e4f8161070f36879003870187612345565b925092509250600081118015610e6457504381105b610eb05760405162461bcd60e51b815260206004820152600e60248201527f6e6f742072656465656d61626c65000000000000000000000000000000000000604482015260640161034d565b60018201610ef35760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b610efe828535611442565b15610f3e5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b60a0840135610f7d5760405162461bcd60e51b815260206004820152600b60248201526a1e995c9bc81cd958dc995d60aa1b604482015260640161034d565b610f8c60a08501358535611442565b610fc55760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b604482015260640161034d565b600083815260016020908152604080832060a0880135905560038252909120805460ff19169055610ff9908501358861231b565b9650856001600160a01b031660006001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161104691815260200190565b60405180910390a4505060019092019150610d869050565b5060008381526002602052604081208054908290559061107e8285612397565b90506000836001600160a01b03168260405160006040518083038185875af1925050503d80600081146110cd576040519150601f19603f3d011682016040523d82523d6000602084013e6110d2565b606091505b505090508061111f57836001600160a01b03167f29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae08360405161111691815260200190565b60405180910390a25b5050505050610cae6001600055565b3233146111705760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b611178611a4f565b611188608082016060830161237c565b67ffffffffffffffff164210156111e15760405162461bcd60e51b815260206004820152600b60248201527f6e6f742065787069726564000000000000000000000000000000000000000000604482015260640161034d565b600080806111f88561070f36879003870187612345565b9250925092506000811161124e5760405162461bcd60e51b815260206004820152600d60248201527f6e6f7420696e6974696174656400000000000000000000000000000000000000604482015260640161034d565b600182016112915760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b61129c828535611442565b156112dc5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b600083815260016020526040902060001990556001600160a01b0385166113b957600061130f6060860160408701612361565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d806000811461135d576040519150601f19603f3d011682016040523d82523d6000602084013e611362565b606091505b50509050806113b35760405162461bcd60e51b815260206004820152601160248201527f45544820726566756e64206661696c6564000000000000000000000000000000604482015260640161034d565b506113e1565b6113e16113cc6060860160408701612361565b6001600160a01b038716906020870135611be2565b6113f16060850160408601612361565b6001600160a01b0316856001600160a01b0316847f2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad6360405160405180910390a450505061143e6001600055565b5050565b60008160028460405160200161145a91815260200190565b60408051601f1981840301815290829052611474916123aa565b602060405180830381855afa158015611491573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906114b491906123d9565b149392505050565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b604051602001611555969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091527fffffffffffffffff00000000000000000000000000000000000000000000000016606884015216607082015260840190565b60408051601f198184030181529082905261156f916123aa565b602060405180830381855afa15801561158c573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106f191906123d9565b6040805160608101825260008082526020820181905291810182905290806115e08561070f36879003870187612345565b92509250508060000361160c578260005b90816003811115611604576116046120b5565b905250611648565b6001820161161c578260036115f1565b611627828535611442565b1561163c576002835260208301829052611648565b60018352604083018190525b505092915050565b3233146116925760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b61169a611a4f565b80158015906116aa575060148111155b6116e75760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b6000805b8281101561198e5736848483818110611706576117066122ef565b60c00291909101915033905061172260a0830160808401612361565b6001600160a01b0316146117785760405162461bcd60e51b815260206004820152600f60248201527f6e6f74207061727469636970616e740000000000000000000000000000000000604482015260640161034d565b6000808061178f8961070f36879003870187612345565b9250925092506000811180156117a457504381105b6117f05760405162461bcd60e51b815260206004820152600e60248201527f6e6f742072656465656d61626c65000000000000000000000000000000000000604482015260640161034d565b600182016118335760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b61183e828535611442565b1561187e5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b60a08401356118bd5760405162461bcd60e51b815260206004820152600b60248201526a1e995c9bc81cd958dc995d60aa1b604482015260640161034d565b6118cc60a08501358535611442565b6119055760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b604482015260640161034d565b600083815260016020908152604090912060a0860135905561192a908501358761231b565b9550336001600160a01b0316896001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161197691815260200190565b60405180910390a45050600190920191506116eb9050565b506001600160a01b038416611a3b57604051600090339083908381818185875af1925050503d80600081146119df576040519150601f19603f3d011682016040523d82523d6000602084013e6119e4565b606091505b5050905080611a355760405162461bcd60e51b815260206004820152601360248201527f455448207472616e73666572206661696c656400000000000000000000000000604482015260640161034d565b50610ca3565b610ca36001600160a01b0385163383611be2565b600260005403611a8b576040517f3ee5aeb500000000000000000000000000000000000000000000000000000000815260040160405180910390fd5b6002600055565b6000806000611aa185856114bc565b600081815260016020526040902054909690955085945092505050565b8015611b1057604051600090339083908381818185875af1925050503d8060008114611b06576040519150601f19603f3d011682016040523d82523d6000602084013e611b0b565b606091505b505050505b50565b60008060008351604103611b4d5760208401516040850151606086015160001a611b3f88828585611c13565b955095509550505050611b59565b50508151600091506002905b9250925092565b6040516001600160a01b038481166024830152838116604483015260648201839052611bdc9186918216906323b872dd906084015b604051602081830303815290604052915060e01b6020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff8381831617835250505050611ce2565b50505050565b6040516001600160a01b03838116602483015260448201839052610cae91859182169063a9059cbb90606401611b95565b600080807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0841115611c4e5750600091506003905082611cd8565b604080516000808252602082018084528a905260ff891692820192909252606081018790526080810186905260019060a0016020604051602081039080840390855afa158015611ca2573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b038116611cce57506000925060019150829050611cd8565b9250600091508190505b9450945094915050565b600080602060008451602086016000885af180611d05576040513d6000823e3d81fd5b50506000513d91508115611d1d578060011415611d2a565b6001600160a01b0384163b155b15611bdc576040517f5274afe70000000000000000000000000000000000000000000000000000000081526001600160a01b038516600482015260240161034d565b600080600060608486031215611d8157600080fd5b833567ffffffffffffffff811115611d9857600080fd5b84016101208187031215611dab57600080fd5b95602085013595506040909401359392505050565b80356001600160a01b0381168114611dd757600080fd5b919050565b60008082840360c0811215611df057600080fd5b611df984611dc0565b925060a0601f1982011215611e0d57600080fd5b506020830190509250929050565b600080600060408486031215611e3057600080fd5b611e3984611dc0565b9250602084013567ffffffffffffffff80821115611e5657600080fd5b818601915086601f830112611e6a57600080fd5b813581811115611e7957600080fd5b87602060a083028501011115611e8e57600080fd5b6020830194508093505050509250925092565b60008083601f840112611eb357600080fd5b50813567ffffffffffffffff811115611ecb57600080fd5b60208301915083602060c083028501011115611ee657600080fd5b9250929050565b600080600060408486031215611f0257600080fd5b833567ffffffffffffffff811115611f1957600080fd5b611f2586828701611ea1565b909790965060209590950135949350505050565b60008060408385031215611f4c57600080fd5b50508035926020909101359150565b634e487b7160e01b600052604160045260246000fd5b6040805190810167ffffffffffffffff81118282101715611f9457611f94611f5b565b60405290565b604051601f8201601f1916810167ffffffffffffffff81118282101715611fc357611fc3611f5b565b604052919050565b803567ffffffffffffffff81168114611dd757600080fd5b600060a08284031215611ff557600080fd5b60405160a0810181811067ffffffffffffffff8211171561201857612018611f5b565b8060405250809150823581526020830135602082015261203a60408401611dc0565b604082015261204b60608401611fcb565b606082015261205c60808401611dc0565b60808201525092915050565b60008060c0838503121561207b57600080fd5b61208483611dc0565b91506120938460208501611fe3565b90509250929050565b6000602082840312156120ae57600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b81516060820190600481106120f057634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060006040848603121561212357600080fd5b61212c84611dc0565b9250602084013567ffffffffffffffff81111561214857600080fd5b61215486828701611ea1565b9497909650939450505050565b6000808335601e1984360301811261217857600080fd5b83018035915067ffffffffffffffff82111561219357600080fd5b602001915036819003821315611ee657600080fd5b600080858511156121b857600080fd5b838611156121c557600080fd5b5050820193919092039150565b7fffffffff0000000000000000000000000000000000000000000000000000000081358181169160048510156116485760049490940360031b84901b1690921692915050565b6000806040838503121561222b57600080fd5b823567ffffffffffffffff8082111561224357600080fd5b818501915085601f83011261225757600080fd5b813560208282111561226b5761226b611f5b565b612279818360051b01611f9a565b828152818101935060c092830285018201928984111561229857600080fd5b948201945b838610156122e15780868b0312156122b55760008081fd5b6122bd611f71565b6122c78b88611fe3565b815260a0870135848201528552948501949382019361229d565b509896013596505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b8082018082111561074557610745612305565b808202811582820484141761074557610745612305565b600060a0828403121561235757600080fd5b6106f18383611fe3565b60006020828403121561237357600080fd5b6106f182611dc0565b60006020828403121561238e57600080fd5b6106f182611fcb565b8181038181111561074557610745612305565b6000825160005b818110156123cb57602081860181015185830152016123b1565b506000920191825250919050565b6000602082840312156123eb57600080fd5b505191905056fea264697066735822122019331ac4db084c07d34ba4a0b52e35a03cbbb46eef7713e226abdf89ee722b5364736f6c63430008170033",
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

// RedeemPrepayments is a free data retrieval call binding the contract method 0xd0758daf.
//
// Solidity: function redeemPrepayments(uint256 ) view returns(uint256)
func (_ETHSwap *ETHSwapCaller) RedeemPrepayments(opts *bind.CallOpts, arg0 *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _ETHSwap.contract.Call(opts, &out, "redeemPrepayments", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// RedeemPrepayments is a free data retrieval call binding the contract method 0xd0758daf.
//
// Solidity: function redeemPrepayments(uint256 ) view returns(uint256)
func (_ETHSwap *ETHSwapSession) RedeemPrepayments(arg0 *big.Int) (*big.Int, error) {
	return _ETHSwap.Contract.RedeemPrepayments(&_ETHSwap.CallOpts, arg0)
}

// RedeemPrepayments is a free data retrieval call binding the contract method 0xd0758daf.
//
// Solidity: function redeemPrepayments(uint256 ) view returns(uint256)
func (_ETHSwap *ETHSwapCallerSession) RedeemPrepayments(arg0 *big.Int) (*big.Int, error) {
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

// RedeemAA is a paid mutator transaction binding the contract method 0x68356d75.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions, uint256 opNonce) returns()
func (_ETHSwap *ETHSwapTransactor) RedeemAA(opts *bind.TransactOpts, redemptions []ETHSwapRedemption, opNonce *big.Int) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "redeemAA", redemptions, opNonce)
}

// RedeemAA is a paid mutator transaction binding the contract method 0x68356d75.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions, uint256 opNonce) returns()
func (_ETHSwap *ETHSwapSession) RedeemAA(redemptions []ETHSwapRedemption, opNonce *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.RedeemAA(&_ETHSwap.TransactOpts, redemptions, opNonce)
}

// RedeemAA is a paid mutator transaction binding the contract method 0x68356d75.
//
// Solidity: function redeemAA(((bytes32,uint256,address,uint64,address),bytes32)[] redemptions, uint256 opNonce) returns()
func (_ETHSwap *ETHSwapTransactorSession) RedeemAA(redemptions []ETHSwapRedemption, opNonce *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.RedeemAA(&_ETHSwap.TransactOpts, redemptions, opNonce)
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

// ValidateUserOp is a paid mutator transaction binding the contract method 0x19822f7c.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,bytes32,uint256,bytes32,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapTransactor) ValidateUserOp(opts *bind.TransactOpts, userOp PackedUserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "validateUserOp", userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x19822f7c.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,bytes32,uint256,bytes32,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapSession) ValidateUserOp(userOp PackedUserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x19822f7c.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,bytes32,uint256,bytes32,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256)
func (_ETHSwap *ETHSwapTransactorSession) ValidateUserOp(userOp PackedUserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
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
